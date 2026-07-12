# 记忆召回增强 — 技术设计

## 问题模型

用户消息与记忆条目之间存在三种匹配模式，当前系统只覆盖了第一种：

| 匹配模式 | 示例 | 当前覆盖 |
|---------|------|---------|
| 直接关键词匹配 | 用户说"4090"，记忆含"4090" | BM25 能匹配，但被噪声稀释 |
| 实体引用匹配 | 用户说"测试环境"，记忆含"test.example.com" | BM25 匹配不上 |
| 语义关联匹配 | 用户说"部署"，记忆含"deploy.sh 脚本" | 需要向量或 LLM 理解 |

本设计通过四层机制覆盖所有三种模式。

## 架构概览

```
用户消息: "登录测试环境看看服务状态"
    │
    ▼
┌──────────────────────────┐
│  Layer 1: Query Expand   │  提取实体: ["测试环境"]
│  (纯规则, <5ms)          │  生成 tokens: ["登录","测试","环境","测试环境","服务","状态"]
└──────────┬───────────────┘
           │
           ▼
┌──────────────────────────┐
│  Layer 2: Multi-Signal   │  信号 A: BM25(原句) + BM25("测试环境") → 取 max
│  Retrieval               │  信号 B: Vector(原句)
│                          │  信号 C: Tag 交叉匹配(tokens vs entry.Tags)
└──────────┬───────────────┘
           │ RRF 三路融合
           ▼
┌──────────────────────────┐
│  Layer 3: Scoring &      │  memoryStreamScore(recency + importance + relevance)
│  Budget                  │  动态 token 预算 + 类型配额保障
│  + Graph Expand          │  1-hop BFS 扩展关联记忆
└──────────┬───────────────┘
           │ 候选集 (15-30 条)
           ▼
┌──────────────────────────┐
│  Layer 4: LLM Reranker   │  可选：LLM 从候选集精选 top-N
│  (可选, 默认关闭)        │  不可用时直接输出 Layer 3 结果
└──────────┬───────────────┘
           │ 最终结果 (5-8 条)
           ▼
┌──────────────────────────┐
│  Proactive Injection     │  注入 system prompt "## 相关记忆"
│  (新增)                  │  LLM 无需手动调 recall 即可获得上下文
└──────────────────────────┘
```

---

## 模块设计

### 1. Query Expand (`corelib/memory/query_expand.go` 新增)

目标：从自然语言中提取「值得单独搜索」的实体和短语。

```go
package memory

// ExpandResult 包含查询扩展的结果。
type ExpandResult struct {
    Entities    []string // 提取出的实体短语，用于独立 BM25 查询
    QueryTokens []string // 分词结果，用于 Tag 交叉匹配
}

// ExpandQuery 从用户消息中提取关键实体和分词。
// 纯规则实现，不依赖 LLM，延迟 < 5ms。
func ExpandQuery(userMessage string) ExpandResult
```

提取策略（按优先级）：

| 模式 | 正则/规则 | 示例 |
|------|----------|------|
| 引号内容 | `["「『](.{2,30})["」』]` | "测试环境" → 测试环境 |
| IP 地址 | `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}` | 192.168.1.100 |
| 域名 | `[a-zA-Z0-9][-a-zA-Z0-9]*\.[a-zA-Z]{2,}` | test.example.com |
| 文件路径 | `/[a-zA-Z0-9_./\-]+` 或 `[A-Z]:\\...` | /opt/scripts/deploy.sh |
| 数字+中文名词 | `(\d{2,})\s*([一-龥]{2,6})` | 4090服务器, 3080机器 |
| 中文名词+数字 | `([一-龥]{2,6})\s*(\d{2,})` | 服务器4090 |
| 英文专有名词 | `[A-Z][a-zA-Z]+(?:\s+[A-Z][a-zA-Z]+)*` | Claude API, Visual Studio |
| 英文技术词 | `[a-zA-Z][-a-zA-Z0-9_.]+[a-zA-Z0-9]` (≥4字符) | deploy.sh, nginx, pytest |
| 中文复合名词 | 相邻中文字符序列 ≥3字符，排除停用动词 | 测试环境, 部署脚本 |

分词策略（`QueryTokens`）：
- 中文：按字符边界切分，保留 ≥2 字符的连续中文片段
- 英文：按空格/标点切分，转小写
- 数字：保留 ≥2 位的数字
- 过滤停用词：的、了、吗、呢、把、给、在、是、有、我、你、他、帮、看、用、下、一下

去重：Entities 最多 5 个，QueryTokens 最多 20 个。

### 2. Tag 交叉匹配 (`corelib/memory/store.go` 修改)

新增独立的 tag 匹配评分函数：

```go
// tagCrossScore 计算用户消息 tokens 与记忆条目 tags 的交叉匹配分数。
// 匹配策略：
//   - 精确匹配：token == tag (忽略大小写) → +2.0
//   - 包含匹配：tag 包含 token 或 token 包含 tag → +1.0
//   - 上限 6.0
func tagCrossScore(entry Entry, queryTokens []string) float64
```

修改 `rrfFuseScores` 为三路融合：

```go
// 当前: RRF(BM25_rank, Vec_rank) + project_boost
// 改为: RRF(BM25_rank, Vec_rank, Tag_rank) + project_boost
//
// Tag_rank 基于 tagCrossScore 排序。
// 如果 queryTokens 为空，Tag 信号不参与融合（向后兼容）。
func rrfFuseScores(bm25Scores, vecScores []float64, entries []Entry,
                   projectLower string, queryTokens []string) []float64
```

三路 RRF 公式：
```
score = 1/(k+bm25_rank) + 1/(k+vec_rank) + α/(k+tag_rank) + project_boost
```
其中 α=0.8（tag 信号权重略低于 BM25/Vec，因为 tag 匹配是精确但稀疏的）。

### 3. Multi-Query BM25 合并 (`corelib/memory/store.go` 修改)

修改 `RecallForProject` 的 BM25 打分阶段：

```go
// 现有: bm25Scores := s.bm25.score(userMessage)
// 改为:
func (s *Store) multiQueryBM25(userMessage string, entities []string) map[string]float64 {
    primary := s.bm25.score(userMessage)
    
    // 对每个提取出的实体单独打分，取每个 entry 的最高分
    merged := make(map[string]float64, len(primary))
    for id, score := range primary {
        merged[id] = score
    }
    for _, entity := range entities {
        entityScores := s.bm25.score(entity)
        for id, score := range entityScores {
            if score > merged[id] {
                merged[id] = score
            }
        }
    }
    return merged
}
```

这解决了核心问题：当用户说「登录 4090 服务器检查 GPU 占用率」时，"4090 服务器" 作为独立查询去匹配，不再被 "登录"、"检查"、"占用率" 稀释。

### 4. 动态 Token 预算 (`corelib/memory/store.go` 修改)

```go
// dynamicBudget 根据活跃记忆数量计算 token 预算和最大条目数。
func dynamicBudget(activeCount int) (maxTokens, maxEntries int) {
    switch {
    case activeCount <= 30:
        return 2000, 20  // 现状
    case activeCount <= 100:
        return 3000, 25
    case activeCount <= 250:
        return 4000, 28
    default:
        return 5000, 30
    }
}
```

类型配额保障：在结果组装阶段，为非 user_fact 类型保留至少 40% 的 token 预算。

```go
// 组装逻辑伪代码：
// 1. self_identity → 无限制（Protected 类型）
// 2. user_fact → 最多占 totalBudget * 0.6
// 3. 其他类型 → 剩余预算（至少 totalBudget * 0.4）
```

这确保即使 user_fact 很多，project_knowledge 和 instruction 也不会被完全挤出。

### 5. RecallForProject 重构

将以上改动整合到 `RecallForProject`：

```go
func (s *Store) RecallForProject(userMessage, projectPath string) []Entry {
    // === Phase 1: Query Understanding ===
    expanded := ExpandQuery(userMessage)
    
    // === Phase 2: Multi-Signal Scoring ===
    // BM25: 多查询合并
    bm25Scores := s.multiQueryBM25(userMessage, expanded.Entities)
    // Vector: 仍用原始消息（向量模型理解语义，不需要拆分）
    vecScores := s.vecIndex.score(s.queryEmbeddingCached(userMessage))
    
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    // === Phase 3: 动态预算 ===
    activeCount := s.activeCountLocked()
    maxTokens, maxEntries := dynamicBudget(activeCount)
    
    projectLower := strings.ToLower(projectPath)
    now := time.Now()
    
    // 分类收集
    var selfIdentity, userFacts []Entry
    var candidates []candidate  // 其他类型
    
    for _, e := range s.entries {
        // ... 现有过滤逻辑（inactive、scope）...
        
        switch {
        case e.Category == CategorySelfIdentity:
            selfIdentity = append(selfIdentity, e)
        case e.Category == CategoryUserFact || MapToCanonical(e.Category) == CategoryUserFact:
            userFacts = append(userFacts, e)
        default:
            candidates = append(candidates, ...)
        }
    }
    
    // === Phase 4: 三路 RRF 融合 ===
    rrfScores := rrfFuseScores(bm25Arr, vecArr, entryArr, projectLower, expanded.QueryTokens)
    
    // memoryStreamScore + sort + graphExpand（不变）
    
    // === Phase 5: 类型配额组装 ===
    tokenBudget := maxTokens
    userFactBudgetCap := int(float64(maxTokens) * 0.6)
    
    var result []Entry
    // 1. self_identity（无限制）
    for _, e := range selfIdentity { ... }
    // 2. user_fact（上限 60%）
    userFactUsed := 0
    for _, e := range userFacts {
        tokens := len(e.Content) / 4
        if userFactUsed + tokens > userFactBudgetCap { continue }
        userFactUsed += tokens
        tokenBudget -= tokens
        result = append(result, e)
    }
    // 3. 其他类型（剩余预算）
    for _, sc := range others { ... }
    
    return result
}
```

### 6. Proactive Recall 注入 (`gui/im_system_prompt.go` 修改)

这是解决「意图-记忆 gap」的关键一步：不再依赖 LLM 自己决定是否调 recall，而是在构建 system prompt 时就把相关记忆注入进去。

修改 `appendMemorySection`：

```go
func (h *IMMessageHandler) appendMemorySection(b *strings.Builder, userMessage string, isFirstTurn bool) {
    if h.memoryStore == nil {
        return
    }

    b.WriteString("\n" + corememory.PromptSectionUserMemory + "\n")
    
    // 1. user_fact summary（始终注入，现有逻辑不变）
    summary := h.memoryStore.UserFactSummary(400)
    if summary != "" {
        b.WriteString(fmt.Sprintf("用户信息: %s\n", summary))
    }

    // 2. Proactive Recall: 基于用户消息自动召回相关记忆
    if userMessage != "" {
        projectPath, _ := h.contextResolver.ResolveProject()
        recalled := h.memoryStore.RecallForProject(userMessage, projectPath)
        
        // 过滤掉已通过 summary 注入的 user_fact 和 self_identity
        var relevant []corememory.Entry
        for _, e := range recalled {
            canonical := corememory.MapToCanonical(e.Category)
            if canonical == corememory.CategoryUserFact || canonical == corememory.CategorySelfIdentity {
                continue
            }
            relevant = append(relevant, e)
        }
        
        // 最多注入 8 条，控制 prompt 膨胀
        if len(relevant) > 8 {
            relevant = relevant[:8]
        }
        
        if len(relevant) > 0 {
            b.WriteString("\n相关记忆（自动召回）:\n")
            for _, e := range relevant {
                text := e.CompactForm
                if text == "" {
                    text = e.Content
                }
                // 截断过长的条目
                if len([]rune(text)) > 200 {
                    text = string([]rune(text)[:200]) + "…"
                }
                b.WriteString(fmt.Sprintf("- [%s] %s\n", e.Category, text))
            }
        }
    }

    // 3. 手动 recall 提示（保留，作为补充）
    b.WriteString("如需更多记忆，可通过 " + corememory.PromptActionRecallColon + ", query: \"关键词\") 召回。\n")

    if isFirstTurn {
        b.WriteString("\n" + corememory.BuildIMMemoryGuidePrompt() + "\n")
    }
}
```

同步修改 `buildSystemPromptBase` 和 `buildSystemPromptWithMemory`，将 `userMessage` 传递到 `appendMemorySection`。

### 7. LLM Reranker（可选增强）

扩展 `LLMRelevanceFilter` 接口：

```go
type LLMRelevanceFilter interface {
    SelectRelevant(query string, candidates []MemoryCandidate, maxResults int) ([]string, error)
    IsAvailable() bool  // 新增
}
```

新增 `RecallSmart` 作为可选的增强入口：

```go
// RecallSmart 在 RecallForProject 基础上增加可选的 LLM rerank。
// llmFilter 为 nil 时等价于 RecallForProject。
func (s *Store) RecallSmart(userMessage, projectPath string, llmFilter LLMRelevanceFilter) []Entry {
    candidates := s.RecallForProject(userMessage, projectPath)
    
    if llmFilter == nil || !llmFilter.IsAvailable() || len(candidates) <= 5 {
        return candidates
    }
    
    // LLM rerank...（复用现有 RecallWithLLMFilter 的逻辑）
}
```

Proactive Recall 注入默认使用 `RecallForProject`（不调 LLM），可通过配置切换到 `RecallSmart`。

---

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `corelib/memory/query_expand.go` | 新增 | ExpandQuery 实体提取 + 分词 |
| `corelib/memory/query_expand_test.go` | 新增 | 覆盖各类实体提取场景 |
| `corelib/memory/store.go` | 修改 | multiQueryBM25、tagCrossScore、三路 rrfFuseScores、dynamicBudget、类型配额、RecallSmart |
| `corelib/memory/types.go` | 修改 | LLMRelevanceFilter 增加 IsAvailable() |
| `gui/im_system_prompt.go` | 修改 | appendMemorySection 增加 proactive recall |
| `gui/im_message_handler.go` | 修改 | 传递 userMessage 到 system prompt 构建 |

---

## 效果预期（通用场景）

| 场景 | 现状 | 改进后 |
|------|------|--------|
| 「登录 4090 服务器」 | BM25 被噪声稀释，未命中 | Entity "4090服务器" 独立搜索命中；Tag "4090" 交叉匹配 boost |
| 「用上次那个部署脚本」 | "上次"、"那个"、"用" 是噪声 | Entity "部署脚本" 独立搜索；Tag "deploy" 匹配 |
| 「连上测试环境跑一下」 | "连上"、"跑一下" 是噪声 | Entity "测试环境" 独立搜索；Tag "test" 匹配 |
| 「帮我看看张三的项目」 | "帮我看看" 是噪声 | Entity "张三" 独立搜索；Tag "张三" 精确匹配 |
| 「用之前配好的 Claude key」 | "之前配好的" 是噪声 | Entity "Claude key" 独立搜索 |
| 所有场景 | LLM 需自己决定调 recall | Proactive Recall 自动注入 system prompt |

---

## 风险与缓解

| 风险 | 缓解措施 |
|------|---------|
| Query Expand 提取出无意义片段 | 最小长度过滤；停用词过滤；最多 5 个 entity |
| Tag 匹配过度 boost 不相关条目 | boost 上限 6.0；RRF 中 tag 权重 α=0.8 低于 BM25/Vec |
| Proactive Recall 注入增大 prompt | 最多 8 条，每条截断 200 字符；总增量 < 2000 字符 |
| rrfFuseScores 签名变更 | queryTokens 传 nil 时行为不变（向后兼容） |
| 动态预算增大导致召回过多 | 上限 5000 tokens / 30 条 |
| LLM Reranker 延迟 | 默认关闭；启用时超时 3s 降级 |
