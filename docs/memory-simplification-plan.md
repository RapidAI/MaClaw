# 记忆机制精简计划

## 前提

- SubconsciousEngine 正在接线，不算冗余，保留
- InferenceEngine + SemanticGraph 是 SubconsciousEngine 的依赖，保留
- 所有核心召回路径（RecallDynamic、RecallAdaptiveHier）不动
- 所有核心写入路径（Store.Save、OnlineExtractor）不动
- 所有安全层（InjectionScanner、Redact）不动

---

## 精简项 1: 删除 TopicClusterer

### 现状

- `corelib/memory/topic_cluster.go`（~250 行）
- `corelib/memory/topic_cluster_test.go`
- Pipeline Step 6 先调用 `TopicClusterer.Cluster()`，再调用 `ThemeManager.Rebuild()`
- Pipeline 注释原文："TopicClusterer remains as a lightweight fallback; ThemeManager is the xMemory-style layer used by adaptive recall"

### 冗余证据

1. **零生产消费方**：`TopicClusterer.Clusters()` 在非测试代码中零调用（grep 确认）
2. **ThemeManager 已完全覆盖**：`ThemeManager.buildFallbackTagThemes()` 在无 embedding 时回退到标签聚类——逻辑与 TopicClusterer 相同（按 tag 分组，≥3 条 entries 成簇，LLM 生成 summary）
3. **Pipeline 中的 TopicClusterer 结果无下游**：重建后只打日志 `log.Printf("[pipeline] topic clusters: %d clusters discovered")`，不注入召回、不注入 prompt、不被任何 API 返回

### 功能对比

| 能力 | TopicClusterer | ThemeManager.buildFallbackTagThemes |
|------|---------------|-------------------------------------|
| 按 tag 分组 | `tagEntries[tag]` | `tagToIDs[tag]` |
| 最小簇大小 ≥3 | `len(ids) >= 3` | `len(ids) < 3 → continue` |
| 合并重叠簇 | `>50% overlap → merge` | 不合并（每个 tag 独立成簇） |
| LLM summary | `GenerateSummaries` | `summarizeTheme` |
| 消费方 | 无 | diversityRerank + AdaptiveRecall |

唯一差异是"合并重叠簇"——但这个能力的结果没有任何消费方，等于不存在。

### 修改清单

| 文件 | 操作 |
|------|------|
| `corelib/memory/topic_cluster.go` | 删除 |
| `corelib/memory/topic_cluster_test.go` | 删除 |
| `corelib/memory/store.go` | 删除 `topicClusterer` 字段、`NewTopicClusterer()` 初始化、`TopicClusterer()` accessor |
| `corelib/memory/pipeline.go` | 删除 Step 6 中 `topicClusterer.Cluster()` + `GenerateSummaries()` 代码块（~20 行） |

### 风险评估

- **零风险**：无生产消费方，删除后无任何行为变化
- **编译验证**：删除后 `go build ./...` 通过即可

---

## 精简项 2: 合并 Promoter + Reflector → Synthesizer

### 现状

- `corelib/memory/promoter.go`（~150 行有效代码）
- `corelib/memory/reflector.go`（~130 行有效代码）
- Pipeline Step 2 调用 `Promoter.Promote()`，Step 3 调用 `Reflector.Reflect()`
- 两者在同一个 Pipeline 周期内顺序执行

### 冗余证据

1. **输入相同**：两者都从 `s.entries` 中筛选 `Category.Tier() == TierEpisodic && IsActive()`
2. **输入范围重叠**：Promoter 取最近 50 条，Reflector 取最近 30 条——Reflector 的输入是 Promoter 输入的子集
3. **输出 Category 相同**：两者都输出 `preference`/`instruction`/`user_fact`
4. **LLM prompt 目标相同**：从情景记忆中提取语义知识
5. **区别仅在 prompt 措辞**：
   - Promoter："identify facts that appear in N or more separate entries"
   - Reflector："extract high-level insights about the user"

### 合并设计

新建 `corelib/memory/synthesizer.go`，单一 `Synthesize(ctx)` 方法：

```go
type Synthesizer struct {
    store     *Store
    llm       LLMChatCaller
    threshold int // 重复阈值（原 Promoter 的 threshold=3）
}

func (s *Synthesizer) Synthesize(ctx context.Context) (*SynthesizeResult, error) {
    // 1. 收集最近 50 条 episodic entries（覆盖原 Promoter 50 + Reflector 30）
    // 2. 单次 LLM 调用，prompt 同时要求：
    //    a) 识别重复出现 ≥threshold 次的模式（原 Promoter）
    //    b) 提取高层洞察（原 Reflector）
    // 3. 解析 JSON 结果，保存到 Store
}
```

合并后的 prompt 结构：
```
You are a memory synthesis assistant. Analyze the following episodic memories and:

1. RECURRING PATTERNS: Identify facts/preferences that appear in {threshold} or more entries.
   For each, output: {"source": "recurring", "content": "...", "category": "...", "evidence_count": N}

2. HIGH-LEVEL INSIGHTS: Extract user preferences, habits, and decision patterns.
   For each, output: {"source": "insight", "content": "...", "category": "..."}

[existing category rules and experience protection rules unchanged]
```

### 修改清单

| 文件 | 操作 |
|------|------|
| `corelib/memory/synthesizer.go` | 新建（合并 Promoter + Reflector 逻辑） |
| `corelib/memory/promoter.go` | 删除 |
| `corelib/memory/reflector.go` | 删除 |
| `corelib/memory/pipeline.go` | Step 2+3 合并为单一 `Synthesizer.Synthesize()` 调用 |
| `gui/app.go` | `NewPromoter` + `NewReflector` 替换为 `NewSynthesizer` |

### 风险评估

- **中等风险**：需要验证合并后的 prompt 质量不低于分开调用
- **缓解措施**：
  1. 合并后的 `SynthesizeResult` 分别统计 `RecurringPromoted` 和 `InsightsGenerated`，与原来的 `PromoteResult.Promoted` 和 `ReflectResult.InsightsGenerated` 对比
  2. 保留 `ExperienceProtection` prompt 注入（原两者都有）
  3. 保留 `SetLLM()` 动态接线模式

### 收益

- 每 Pipeline 周期少一次 LLM 调用（~2-5s + ~500-1000 token）
- 消除重复的 episodic entries 收集逻辑
- 减少 ~280 行代码

---

## 精简项 3: ThemeManager 脏标记缓存

### 现状

- `rebuildIMMemoryThemes(h.memoryStore)` 在每次 `memory(action=recall, mode=adaptive)` 前调用
- `ThemeManager.Rebuild()` 遍历全部 entries，计算 centroid/cohesion/neighbors
- Pipeline 每 6h 已经重建一次
- TUI CLI `memory recall --mode adaptive` 也在每次调用前重建

### 问题

两次 adaptive recall 之间 entries 变化通常 <5 条，但每次都全量重建。对 500+ entries 的 Store，Rebuild 涉及：
- 遍历所有 entries 构建 `entryByID` map
- 对每个 entry 计算 embedding centroid（如果有 embedding）
- 对每对 theme 计算 cosine similarity（`recomputeThemeNeighbors`）
- 排序、合并、分裂

### 修改设计

```go
// ThemeManager 新增字段
type ThemeManager struct {
    // ...existing...
    dirty     bool   // true when entries changed since last Rebuild
    lastGen   uint64 // store generation at last Rebuild
}

// Store.Save/Delete/Update 后标记 dirty
func (s *Store) markThemeDirty() {
    if s.themeManager != nil {
        s.themeManager.mu.Lock()
        s.themeManager.dirty = true
        s.themeManager.mu.Unlock()
    }
}

// 新增 EnsureUpToDate 方法（替代外部的 rebuildIMMemoryThemes）
func (tm *ThemeManager) EnsureUpToDate(entries []Entry, llm LLMChatCaller) {
    tm.mu.RLock()
    if !tm.dirty {
        tm.mu.RUnlock()
        return
    }
    tm.mu.RUnlock()
    tm.Rebuild(entries, llm)
}
```

### 修改清单

| 文件 | 操作 |
|------|------|
| `corelib/memory/theme.go` | ThemeManager 新增 `dirty bool`；`Rebuild()` 末尾设 `dirty=false`；新增 `EnsureUpToDate()` |
| `corelib/memory/store.go` | `Save()`/`Delete()`/`Update()` 路径调用 `markThemeDirty()` |
| `gui/im_tools_misc.go` | `rebuildIMMemoryThemes` 改为 `store.ThemeManager().EnsureUpToDate(...)` |
| `tui/commands/memory.go` | `rebuildStoreThemes` 改为 `store.ThemeManager().EnsureUpToDate(...)` |
| `corelib/agent/tool_memory.go` | `rebuildAgentMemoryThemes` 改为 `store.ThemeManager().EnsureUpToDate(...)` |

### 风险评估

- **低风险**：缓存只影响 adaptive recall 路径，proactive recall 不使用 ThemeManager
- **最坏情况**：dirty 标记丢失（并发竞态）→ 使用略过时的 themes → 多样性重排结果略有偏差，不影响正确性
- **Pipeline 兜底**：每 6h Pipeline 无条件 Rebuild，确保 themes 最终一致

### 收益

- adaptive recall 延迟从 ~50-200ms 降到 <1ms（缓存命中时）
- 减少 CPU 开销（不再每次 recall 都全量遍历 entries）

---

## 精简项 4: KnowledgeExtractor 与 OnlineExtractor 互斥

### 现状

- **OnlineExtractor**：`triggerOnlineExtraction()` 在每次 agent loop 退出后异步触发，3min 冷却
- **KnowledgeExtractor**：`ConversationArchiver.Archive()` 在会话过期时调用，10min 冷却
- 两者对同一段对话可能各提取一次，产出语义重复的 entries
- KnowledgeExtractor 注释已明确："serves as a fallback for when the online extractor is unavailable"

### 修改设计

在 `KnowledgeExtractor.Extract()` 开头新增检查：

```go
func (ke *KnowledgeExtractor) Extract(userID string, msgs []ConversationMessage) error {
    // 互斥检查：如果 OnlineExtractor 在过去 60min 内已成功提取过，跳过
    if ke.store != nil {
        if oe := ke.store.OnlineExtractor(); oe != nil && oe.HasRecentSuccess(60*time.Minute) {
            log.Printf("[knowledge_extractor] skipped: online extractor active in last 60min")
            return nil
        }
    }
    // ...existing logic...
}
```

OnlineExtractor 新增 `HasRecentSuccess(window)` 方法：

```go
func (oe *OnlineExtractor) HasRecentSuccess(window time.Duration) bool {
    oe.mu.Lock()
    defer oe.mu.Unlock()
    return !oe.lastExtract.IsZero() && time.Since(oe.lastExtract) < window
}
```

### 修改清单

| 文件 | 操作 |
|------|------|
| `corelib/memory/online_extractor.go` | 新增 `HasRecentSuccess(window)` 方法 |
| `corelib/memory/knowledge_extractor.go` | `Extract()` 开头新增互斥检查 |

### 风险评估

- **低风险**：KnowledgeExtractor 是 fallback，跳过它不影响主路径
- **安全网**：OnlineExtractor 不可用时（LLM=nil），`HasRecentSuccess` 返回 false，KnowledgeExtractor 正常执行
- **边界情况**：OnlineExtractor 3min 冷却内多次 agent loop 退出只提取一次，但 KnowledgeExtractor 在会话过期时仍可能触发——互斥检查用 60min 窗口覆盖这个间隙

### 收益

- 减少 50-80% 的重复 LLM 提取调用
- 减少语义重复 entries 占用 Store 容量

---

## 精简项 5: RecallGating 从 proactive recall 热路径移除

### 现状

- `RecallGating.Filter()` 在两个路径中被调用：
  1. `RecallForProject`（旧路径，line 714）
  2. `RecallDynamic`（主路径，line 1574）
- 触发条件：`len(candidates) > recallGatingThreshold(15)`
- `RecallDynamic` 的 `maxEntries=15`，但 gating 在 `maxEntries` 截断**之前**执行
- gating 之前的 candidates 数量 = graphExpand 后的全部候选（可能 20-50 条）

### 问题分析

RecallGating 在 proactive recall 热路径中的问题：
1. **延迟**：LLM 调用 2-5s，阻塞 system prompt 构建
2. **频率**：每条用户消息都触发 proactive recall → 每条消息可能触发 gating LLM 调用
3. **收益有限**：proactive recall 已有截断（最多 12 条 × 200 字符），混入 1-2 条不相关记忆的影响远小于 2-5s 延迟

### 修改设计

不删除 RecallGating 模块，而是将其从 `RecallDynamic` 和 `RecallForProject` 中移除，仅保留在 `RecallAdaptiveHier` 中（用户主动调用 memory tool 时质量要求更高，且不在热路径上）。

```go
// store.go — RecallDynamic 中
// 删除:
// if s.gating != nil {
//     candidates = s.gating.Filter(query, candidates)
// }

// store.go — RecallForProject 中
// 删除:
// if s.gating != nil {
//     others = s.gating.Filter(query, others)
// }

// adaptive_recall.go — RecallAdaptiveHierDebug 中
// 保留 gating（或新增，如果当前没有）
```

### 修改清单

| 文件 | 操作 |
|------|------|
| `corelib/memory/store.go` | 删除 `RecallDynamic` 中的 `gating.Filter` 调用（line ~1574） |
| `corelib/memory/store.go` | 删除 `RecallForProject` 中的 `gating.Filter` 调用（line ~714） |
| `corelib/memory/adaptive_recall.go` | 在 `RecallAdaptiveHierDebug` 的结果组装前新增 `gating.Filter`（可选） |

### 风险评估

- **低风险**：proactive recall 的下游已有多层过滤（category 过滤、token 截断、最多 12 条）
- **最坏情况**：proactive recall 注入 1-2 条不太相关的记忆 → LLM 忽略它们（system prompt 中的记忆是参考信息，不是指令）
- **收益确定**：消除每条消息可能的 2-5s LLM 阻塞

### 收益

- proactive recall 延迟减少 2-5s（当 candidates > 15 时）
- 减少 LLM 调用频率（从"每条消息可能触发"到"仅 memory tool adaptive 模式触发"）

---

## 精简项 6: Archiver 与 OnlineExtractor 互斥

### 现状

- `ConversationArchiver.Archive()` 在会话过期时执行两步：
  1. LLM 生成对话摘要 → 保存为 `conversation_summary`
  2. 调用 `KnowledgeExtractor.Extract()` → 保存为各类知识点
- `conversation_summary` 在 proactive recall 中被**硬过滤**（#89.1 修复后 RecallDynamic 统一过滤）
- 如果 OnlineExtractor 已经在会话期间实时提取了知识点，Archiver 的步骤 1（摘要）产出的 entry 会被过滤掉，步骤 2（KnowledgeExtractor）与 OnlineExtractor 重复

### 修改设计

在 `ConversationArchiver.Archive()` 中，当 OnlineExtractor 活跃时，跳过步骤 1（摘要生成），只保留步骤 2（KnowledgeExtractor，已有精简项 4 的互斥保护）：

```go
func (a *ConversationArchiver) Archive(userID string, entries []agent.ConversationEntry) error {
    // ...existing length check...
    
    // 如果 OnlineExtractor 在会话期间已活跃提取，跳过摘要生成
    // （摘要会被 RecallDynamic 过滤掉，浪费 LLM 调用和容量）
    skipSummary := false
    if a.memoryStore != nil {
        if oe := a.memoryStore.OnlineExtractor(); oe != nil && oe.HasRecentSuccess(60*time.Minute) {
            skipSummary = true
        }
    }
    
    if !skipSummary {
        // ...existing summary generation and save logic...
    }
    
    // KnowledgeExtractor 仍然执行（有自己的互斥检查，精简项 4）
    if a.knowledgeExtractor != nil {
        // ...existing extraction logic...
    }
    return nil
}
```

### 修改清单

| 文件 | 操作 |
|------|------|
| `gui/conversation_archiver.go` | `Archive()` 新增 `skipSummary` 逻辑 |

### 风险评估

- **低风险**：`conversation_summary` 已被 RecallDynamic 过滤，跳过生成不影响召回质量
- **安全网**：OnlineExtractor 不可用时，Archiver 正常生成摘要（向后兼容）
- **边界情况**：极短会话（<4 entries）已被 Archiver 跳过，不受影响

### 收益

- 减少会话过期时的 LLM 调用（~2-5s + ~500 token）
- 减少被过滤掉的无用 `conversation_summary` entries 占用 Store 容量（maxItems=2000）

---

## 实施顺序与依赖

```
精简项 1 (TopicClusterer)     ← 已完成
精简项 3 (ThemeManager 缓存)  ← 已完成
精简项 4 (KE/OE 互斥)        ← 已完成
精简项 5 (RecallGating 移除)  ← 已完成
精简项 6 (Archiver 互斥)      ← 已完成（依赖精简项 4 的 HasRecentSuccess 方法）
精简项 2 (Promoter+Reflector) ← 已完成（合并为 Synthesizer）
```

---

## 验收标准

### 全局验收

- `go build ./corelib/memory/...` 通过
- `go build ./gui/...` 通过
- `go build ./tui/...` 通过
- `go test ./corelib/memory/ -short` 全部通过（删除的测试文件除外）
- `go test ./gui/ -short` 全部通过

### 各项验收

| 精简项 | 验收标准 |
|--------|---------|
| 1 | `TopicClusterer` 在代码库中零引用；Pipeline 日志不再输出 "topic clusters" |
| 2 | Pipeline 日志输出 `[pipeline] synthesize: recurring=N insights=M`；`SynthesizeResult` 数值与原 Promote+Reflect 之和相当 |
| 3 | 连续两次 `memory(action=recall, mode=adaptive)` 调用，第二次 <5ms（日志确认） |
| 4 | OnlineExtractor 活跃时，`[knowledge_extractor] skipped` 日志出现 |
| 5 | proactive recall 路径中不再有 `[recall_gating]` 日志 |
| 6 | OnlineExtractor 活跃时，`conversation_summary` entries 不再增长 |

---

## 预期总收益

| 指标 | 精简前 | 精简后 |
|------|--------|--------|
| Pipeline 每周期 LLM 调用 | 5-7 次 | 3-5 次 |
| 代码行数减少 | — | ~700 行（含测试） |
| proactive recall 最坏延迟 | +2-5s（gating LLM） | 无额外延迟 |
| adaptive recall 延迟 | +50-200ms（重建） | <1ms（缓存命中） |
| 重复提取 LLM 调用 | KE + OE 各一次 | 互斥，只执行一次 |
| 无用 conversation_summary | 持续增长 | OnlineExtractor 活跃时不生成 |
| 概念复杂度 | TopicClusterer + Promoter + Reflector 三个独立模块 | ThemeManager（已有）+ Synthesizer（合并） |

---

## 不动的模块清单

| 模块 | 保留原因 |
|------|---------|
| SubconsciousEngine | 正在接线 |
| InferenceEngine + SemanticGraph | SubconsciousEngine 依赖 |
| EntityIndex | InferenceEngine 依赖 |
| Store + BM25 + VectorIndex + MemoryGraph | 核心基础设施 |
| RecallDynamic + RecallAdaptiveHier | 核心召回路径 |
| OnlineExtractor | 主力写入路径 |
| Compressor + Pipeline | 容量管理核心 |
| Consolidator + ProfileConsolidator | TiMem 时间维度整合 |
| ExperienceDistiller | 保护高价值经验 |
| ThemeManager | diversity rerank + adaptive recall |
| RecallGating（模块本身） | 保留代码，仅从热路径移除 |
| KnowledgeExtractor（模块本身） | 保留为 fallback，仅加互斥 |
| Archiver（模块本身） | 保留，仅加条件跳过 |
| Partition + Archive | 存储基础设施 |
| InjectionScanner + Redact | 安全层 |
| TemporalTree | Consolidator 依赖 |
| Stability | 辅助信号，开销极低 |
| SemanticDedup | 异步去重，不在热路径 |
| Diversity | 仅复杂查询触发，开销低 |
| Forgetting | 衰减是容量管理核心 |
