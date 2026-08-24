> **Superseded for IM/Core/GUI first-turn inject.** New contract: [design/clean-working-set-on-demand-retrieval.md](design/clean-working-set-on-demand-retrieval.md) (overview: [design/clean-working-set-on-demand-retrieval-zh.md](design/clean-working-set-on-demand-retrieval-zh.md)). This file remains a historical note for the old silent-inject path.

# 知识库自动召回策略设计

## 问题

用户与 AI 助手聊天时，知识库中可能有高度相关的内容（之前导入的论文、文档、网页等），但用户不知道知识库里有什么，不会主动说"查知识库"。当前设计要求用户有明确的"查知识库"意图才触发，导致知识库形同虚设。

## 设计目标

- 用户问任何问题时，系统**自动判断**是否有相关知识库内容
- 有相关内容时**静默注入**到 LLM context，LLM 自然引用
- 无相关内容时**零 token 开销**
- 不依赖 LLM 做"要不要查"的判断（太慢），用本地检索信号决策
- 不维护规则/关键词列表（会退化为 workaround）

---

## 核心设计原则：让数据说话

**不用规则猜测"这条消息需不需要查知识库"，让检索结果本身决定。**

FTS 查询是最好的过滤器：
- 用户说"好" → BM25 给"好"极低的 IDF（高频词）→ 无高分结果 → 不注入
- 用户说"transformer attention" → BM25 匹配到导入的论文 → 高分结果 → 注入

唯一的 guard 是**硬性约束**（不是语义猜测）：
1. 知识库为空 → 连 DB 都不打开
2. iteration > 0 → 已在工具调用循环中，不重复注入

---

## 架构

```
用户消息
    │
    ▼
[Guard] 知识库有内容？（缓存 sourceCount，<1ms）
    │ 无 → return
    ▼
[Guard] iteration == 0？
    │ 否 → return
    ▼
[Retrieve] knowledge.Search（FTS5 + scoring，<50ms）
    │ 无结果 → return
    ▼
[Quality Gate] top-1 score >= dynamicThreshold？
    │ 否 → return
    ▼
[Inject] 格式化 top-N 结果注入 system prompt（~400 token）
```

---

## Score 语义分析（关键）

`knowledge.Search` 的 `scoreSearchResult` 计算方式：

```
finalScore = max(0, -bm25_rank)     // BM25 base（FTS5 rank 取反）
           + typeBonus              // card=0.30, fact=0.18, node=0.08
           + trustBonus             // source_trust * 0.12（0~0.12）
           + projectBonus           // 同项目 +0.15
           + termOverlap            // 0.22 * matchCount
```

**典型 score 分布**：

| 场景 | BM25 base | type | trust | project | terms | total |
|------|-----------|------|-------|---------|-------|-------|
| 强相关 card（多词命中） | 2.0-5.0 | 0.30 | 0.10 | 0.15 | 0.44+ | **3.0-6.0** |
| 弱相关 card（单词命中） | 0.3-1.0 | 0.30 | 0.10 | 0.15 | 0.22 | **1.1-1.8** |
| 偶然匹配 card（停用词） | 0.01-0.1 | 0.30 | 0.10 | 0 | 0 | **0.4-0.5** |
| 无关 node（单字匹配） | 0.01 | 0.08 | 0 | 0 | 0 | **0.09** |

**Threshold 校准**：
- `threshold = 1.0`：只注入"至少有一个核心词在文档中有意义地出现"的结果
- 低于 1.0 的结果大多是 type bonus + trust bonus 凑出来的偶然匹配
- 这个值需要在实际数据上验证，可通过配置调整

---

## 动态 Threshold（自适应）

固定 threshold 有一个问题：知识库内容量不同时，BM25 的 IDF 分布不同。10 篇文档时 score 分布和 10000 篇时完全不同。

**自适应策略**：
- 基础 threshold = 1.0
- 如果 top-1 score > 3.0（强相关），注入 top-3
- 如果 top-1 score 在 1.0-3.0（中等相关），只注入 top-1
- 如果 top-1 score < 1.0，不注入

这比固定 threshold 更灵活：强相关时多给信息，弱相关时保守只给一条。

---

## 注入策略

### 注入格式

```
## 知识库参考（自动检索）
以下内容来自你的知识库，与当前问题可能相关。请自然引用相关内容；不相关则忽略。

- [transformer-architecture.pdf] Attention 机制通过 Query、Key、Value 三个矩阵...
- [bert-paper.md] BERT 使用双向 Transformer 编码器进行预训练...
```

### 注入位置

在 system prompt 中，`appendMemorySection` 之后、steering 之前。与 proactive memory recall 平行。

### Token 预算

| 条件 | 注入量 | 预算 |
|------|--------|------|
| top-1 score >= 3.0 | 最多 3 条 × 200 字符 | ~400 token |
| top-1 score 1.0-3.0 | 最多 1 条 × 200 字符 | ~150 token |
| top-1 score < 1.0 | 不注入 | 0 token |

### LLM 引导措辞

"请自然引用相关内容；不相关则忽略"——不强制引用，让 LLM 自行判断。

如果 LLM 发现注入的内容不够，system prompt 中的触发规则告诉它可以主动调用 `knowledge_search` / `knowledge_context_pack` 做更深入的查询。自动注入是"开胃菜"，工具调用是"正餐"。

---

## 实现

### `appendKnowledgeAutoRecall`（`gui/im_system_prompt.go`）

```go
func (h *IMMessageHandler) appendKnowledgeAutoRecall(b *strings.Builder, msg string) {
    if msg == "" || !h.hasKnowledgeSources() {
        return
    }

    store, err := h.app.openKnowledgeStore()
    if err != nil {
        return
    }
    defer store.Close()

    results, err := store.Search(context.Background(), knowledge.SearchOptions{
        Query:       msg,
        Limit:       5, // 多取几条，后面按 score 筛选
        ProjectPath: h.getCurrentProjectPath(),
    })
    if err != nil || len(results) == 0 {
        return
    }

    // 动态 threshold + 注入量控制
    topScore := results[0].Score
    var maxInject int
    switch {
    case topScore >= 3.0:
        maxInject = 3 // 强相关：注入 top-3
    case topScore >= 1.0:
        maxInject = 1 // 中等相关：只注入 top-1
    default:
        return // 弱相关：不注入
    }

    b.WriteString("\n## 知识库参考（自动检索）\n")
    b.WriteString("以下内容来自你的知识库，与当前问题可能相关。请自然引用相关内容；不相关则忽略。\n")
    b.WriteString("如需更多信息，可调用 knowledge_search 或 knowledge_context_pack 深入检索。\n\n")

    injected := 0
    for _, r := range results {
        if injected >= maxInject {
            break
        }
        if r.Score < 1.0 {
            break // 不注入低于 1.0 的结果
        }
        source := r.Source.Title
        if source == "" {
            source = r.Source.RelativePath
        }
        if source == "" {
            source = r.Source.URI
        }
        text := bestSnippet(r)
        if len([]rune(text)) > 200 {
            text = string([]rune(text)[:200]) + "..."
        }
        b.WriteString(fmt.Sprintf("- [%s] %s\n", source, text))
        injected++
    }
    log.Printf("[knowledge_auto_recall] query=%q injected=%d topScore=%.2f", 
        truncateLog(msg, 50), injected, topScore)
}

func bestSnippet(r knowledge.SearchResult) string {
    if r.Snippet != "" {
        return r.Snippet
    }
    if r.Summary != "" {
        return r.Summary
    }
    if r.Claim != "" {
        return r.Claim
    }
    // fact: subject predicate object
    if r.Subject != "" && r.Predicate != "" {
        return r.Subject + " " + r.Predicate + " " + r.Object
    }
    return ""
}
```

### `hasKnowledgeSources`（缓存）

```go
var (
    knowledgeSourceCountCache int64  // atomic
    knowledgeSourceCountTime  int64  // atomic, unix seconds
)

func (h *IMMessageHandler) hasKnowledgeSources() bool {
    now := time.Now().Unix()
    lastCheck := atomic.LoadInt64(&knowledgeSourceCountTime)
    if now-lastCheck < 30 { // 30s TTL
        return atomic.LoadInt64(&knowledgeSourceCountCache) > 0
    }
    // Refresh cache
    store, err := h.app.openKnowledgeStore()
    if err != nil {
        return false
    }
    defer store.Close()
    count, _ := store.SourceCount(context.Background())
    atomic.StoreInt64(&knowledgeSourceCountCache, int64(count))
    atomic.StoreInt64(&knowledgeSourceCountTime, now)
    return count > 0
}
```

### 调用位置

```go
// im_system_prompt.go — buildSystemPromptBase 中
h.appendMemorySection(&b, includeMemoryGuide, msg)
h.appendKnowledgeAutoRecall(&b, msg.Text)  // 新增
h.appendSteeringSection(&b, msg)
```

### System Prompt 规则更新

```go
// 原有的"知识库外脑触发规则"改为：
## 知识库外脑规则
- 查询：系统已自动检索知识库，相关内容显示在上方「知识库参考」section 中。
  如果自动检索的内容不够或需要更精确的查询，可主动调用 knowledge_search、
  knowledge_context_pack、knowledge_explain 等工具。
- 写入：当用户明确说"保存到知识库"、"记住这份资料"、"加入外脑"等时，
  调用 knowledge_save_url / knowledge_save_text / knowledge_import_files 等写入工具。
- 不要因为用户只是让你"看看这个链接/总结这个文件"就自动写入知识库。
```

---

## 性能

| 操作 | 耗时 | 条件 |
|------|------|------|
| `hasKnowledgeSources` 缓存命中 | <1ms | 每条消息 |
| `hasKnowledgeSources` 缓存 miss | ~10ms | 每 30s 一次 |
| `openKnowledgeStore` | ~5ms | 仅知识库非空时 |
| FTS5 查询（limit=5） | ~10-30ms | 仅知识库非空时 |
| 格式化注入 | <1ms | 仅有高分结果时 |
| **总计（最坏）** | **~40ms** | 远小于 LLM 的 2-30s |
| **知识库为空** | **<1ms** | 缓存命中 |

与 proactive memory recall（~5-20ms/条消息）在同一量级。

---

## 边界情况

| 场景 | 行为 | 原因 |
|------|------|------|
| 知识库为空 | 不查询 | `hasKnowledgeSources` 缓存返回 false |
| 用户说"好"/"ok" | 不注入 | BM25 对高频词/停用词给极低分，score < 1.0 |
| 用户说"打开文件" | 不注入 | 除非知识库里真有"打开文件"相关文档且 score >= 1.0 |
| 用户说"transformer" | 注入 | 知识库有相关论文，BM25 高分 |
| 用户明确说"查知识库" | 自动注入 + LLM 可能再调工具 | 不冲突——自动注入是快速预览，工具调用是精确查询 |
| 多轮对话中 | 每条用户消息都查一次 | 不同消息可能关联不同知识 |
| 大 PDF（100 页）| 注入最相关的 card snippet | 不注入完整文档 |
| 刚导入的文件 | 立即可查 | FTS5 索引实时更新 |

---

## 与现有机制的关系

| 机制 | 数据源 | 触发 | 深度 | 用途 |
|------|--------|------|------|------|
| Proactive Memory Recall | memory store | 每条消息自动 | 浅（snippet） | 用户偏好、项目配置 |
| **Knowledge Auto Recall** | knowledge store | 每条消息自动 | 浅（snippet） | 导入的文档/网页 |
| Knowledge Tool Call | knowledge store | LLM 主动调用 | 深（full context） | 精确查询、facets、写入 |

三者形成漏斗：
1. Auto Recall 给 LLM 一个"提示"——知识库里有相关内容
2. LLM 判断是否需要更多信息 → 调用 `knowledge_context_pack` 获取完整上下文
3. 用户明确要求写入 → LLM 调用写入工具

---

## 未来演进

1. **Embedding fallback**：当 FTS 无结果但 embedding 模型可用时，做向量相似度检索作为补充。`knowledge.Search` 内部已支持 hybrid 模式，只需传 `UseEmbedding: true`。
2. **对话上下文增强**：当前只用单条用户消息做 query。未来可以拼接最近 2-3 轮对话的关键词，提高多轮对话中的召回率。
3. **反馈学习**：如果 LLM 在回答中引用了自动注入的内容（通过检测回答中是否包含 source title），说明注入有效，可以降低该类内容的 threshold。反之如果多次注入但从未被引用，可以提高 threshold。

---

## 实现优先级

| 优先级 | 改动 | 工作量 |
|--------|------|--------|
| P0 | `appendKnowledgeAutoRecall`（检索 + 动态 threshold + 注入） | ~60 行 |
| P0 | `hasKnowledgeSources` 缓存 | ~20 行 |
| P0 | system prompt 规则更新 | ~10 行 |
| P0 | `bestSnippet` 辅助函数 | ~15 行 |
| P2 | 可配置 threshold（设置面板） | 前端 ~20 行 + 后端 ~10 行 |
| P2 | 自动召回开关 | 前端 ~10 行 + 后端 ~5 行 |
| P3 | Embedding fallback | 后端 ~20 行（传参数即可） |
| P3 | 对话上下文增强 query | 后端 ~15 行 |

## 落地状态（2026-07）

| 路径 | 状态 |
|------|------|
| IM / 桌面 system prompt（`gui/im_knowledge_auto_recall.go`） | 已用 `corelib/agent` 共享常量 + header / NoMatchHint |
| TUI（`tui/knowledge_autorecall.go`） | 已对齐共享常量 |
| Agent service（`corelib/agentservice`） | 已对齐共享常量 |
| 数字员工 VE（`gui/app_ve_handler.go`） | **已对齐**共享常量（此前硬编码 threshold/注入量/英文 header，已统一） |

共享策略见 `corelib/agent/prompt_blocks.go`：

- `KnowledgeAutoRecallScoreThreshold = 0.3`
- `KnowledgeAutoRecallMaxInject`：≥3.0 → 5；≥1.0 → 3；≥0.3 → 2
- `KnowledgeAutoRecallSearchLimit = 8`，snippet 上限 400 runes

### P2 落地（2026-07，本轮）

| 项 | 实现 |
|----|------|
| 开关 | `AppConfig.knowledge_auto_recall_enabled`（`*bool`，默认 true）；GUI 知识库总览可关 |
| 最低分 | `AppConfig.knowledge_auto_recall_min_score`（0=默认 0.3）；`KnowledgeAutoRecallMaxInjectWithMin` |
| 路径 | IM / VE / TUI / agentservice 均读取 `IsKnowledgeAutoRecallEnabled` + min score |
| Patch | `PatchConfigFields` 白名单字段 `knowledge_auto_recall_enabled` / `knowledge_auto_recall_min_score` |

关闭后仅停用**自动注入**；`knowledge_search` / `knowledge_context_pack` 等工具不受影响。

### P3a 落地（2026-07）：Embedding hybrid fallback

`knowledge.Search` 在 store 挂有非 noop embedder 时，**已内置** hybrid：

- FTS 为空，或 FTS 最佳分 < 2.0 → 调用 `searchByEmbedding` + RRF 融合
- 向量分约 `1.0 + (sim-0.3)*4.3`，可过 auto-recall 默认阈值 0.3

本轮补齐的是 **接线**（此前 auto-recall 打开的 store 经常拿不到 embedder）：

| 点 | 改动 |
|----|------|
| `openKnowledgeStore` | `attachKnowledgeEmbedder`（memory/intent 共享模型） |
| `getAutoRecallStore` | 打开后再 attach 一次（防竞态） |
| `activateEmbedderAsync` | 继续写入已缓存的 auto-recall store |
| `SearchOptions.PreferEmbedding` | 可选强制 hybrid（auto-recall 默认 false，仍走“空/低分”触发） |
| 测试 | `TestSearchEmbeddingFallbackWhenFTSEmpty` |

### P3b 落地（2026-07）：多轮 query 增强

具名目标：**Multi-turn auto-recall query**（在冻结文档 Out of freeze 第 1 项下开窗）。

| 点 | 实现 |
|----|------|
| 纯函数 | `PriorUserMessagesFromHistory` + `ExpandKnowledgeAutoRecallQuery`（`corelib/agent`） |
| 预算 | 总 query ≤ 200 runes；当前句优先；每 prior 最多 80 runes |
| 过滤 | 跳过「好的 / ok / 继续」等低信号短句 |
| IM | `LoopContext.History` → prior |
| VE | `veAgentCallbacks.history` |
| TUI | `tuiCallbacks.history` 在 `RunLoop` 前赋值 |
| agentservice | `coreAgentCallbacks.history` 来自 `req.History` |

### 仍开放（P3c+）

1. TUI 配置页字段（当前与 GUI 共用 config.json，可在 GUI 改后 TUI 生效）  
2. agentservice/TUI 侧 embedder 接线（若部署路径独立加载 embedding 模型）  
3. 反馈学习（引用率调阈值）

## 冻结（2026-07）

本 track **已冻结观察**（P0–P3a + 开关/阈值）。**P3b 多轮 query** 为冻结后的具名增量，完成后仍归 auto-recall 观察范围。

→ [knowledge-auto-recall-track-freeze-2026.md](./knowledge-auto-recall-track-freeze-2026.md)
