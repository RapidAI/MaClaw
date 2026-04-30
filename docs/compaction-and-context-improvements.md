# Compaction 质量 + Context 膨胀改进方案

## 来源

用户截图 + `maclaw.log` 分析（2026-04-30 12:08-12:21）。

HuggingFace 论文采集任务中，agent loop 跑了 35 轮迭代（5 分 42 秒），context 从 25K 膨胀到 55K token。
每次 agent loop 结束后 compaction 触发（entries 44→40/41），第 2 次 compaction 就触发了 quality warning。
用户后续消息时 LLM 丢失了之前的工具产出物上下文。

同时 `online_extractor` 报 JSON 解析错误：`json: cannot unmarshal array into Go struct field ExtractedFact.entities of type string`。

---

## 问题清单

| # | 优先级 | 问题 | 根因 | 状态 |
|---|--------|------|------|------|
| 1 | P0 | compactHistory 的 token 阈值触发过于频繁——每次 agent loop 结束都触发 | `MaxMemoryTokenEstimate=60000` 对 35 轮工具调用的对话来说太低，entries 数量在 40 以内但 token 超 60K | ✅ 已修复 |
| 2 | P1 | compactHistory 的 split 点在 `len(entries)/2`——丢弃了前半部分的所有工具调用细节 | 固定 50% 分割不考虑信息密度，前半部分可能包含关键的搜索结果和文件路径 | ✅ 已修复 |
| 3 | P1 | compactHistory 的 summarizer 输入缺少工具操作语义 | 只有"产出了什么文件"，没有"做了什么操作" | ✅ 已修复 |
| 4 | P1 | online_extractor 的 `ExtractedFact.Entities` 字段 JSON 反序列化失败 | LLM 返回嵌套数组 `[["entity:X", "relation:Y", "entity:Z"]]`，但 Go 类型是 `[]string`（期望扁平数组） | ✅ 已修复 |
| 5 | P2 | compaction 后 LLM 的 "好的，我已了解之前的对话上下文。" 是空洞的确认 | 固定文本不包含任何实际上下文信息，浪费一条 assistant entry | ✅ 已修复 |

---

## 修复方案

### 问题 1 (P0): compactHistory token 阈值过低——从固定阈值到动态阈值

**根因**：`MaxMemoryTokenEstimate=60000` 是一个保守的固定值。35 轮工具调用的对话轻松超过 60K token（每轮工具调用 ~1500 token：assistant tool_calls + tool result + 下一轮 assistant 响应）。每次 agent loop 结束后 `saveConversationHistoryTimed` 调用 `compactHistory`，发现 token 超 60K，触发 compaction。下一轮 agent loop 又积累到 60K+，又触发。

**修复**：compactHistory 的触发阈值从固定的 `MaxMemoryTokenEstimate` 改为动态计算，基于模型的 effective context window。

```go
// gui/im_message_handler.go — compactHistory

func (h *IMMessageHandler) compactHistory(...) []agent.ConversationEntry {
    // 动态阈值：模型 effective context 的 50%，最低 60K，最高 100K。
    // 原理：compaction 的目的是防止下一轮 agent loop 的 context 超限。
    // 下一轮需要 system prompt (~15K) + 工具定义 (~5K) + 新的工具调用 (~10K)，
    // 所以对话历史应该占 context 的 ~50%。
    threshold := agent.MaxMemoryTokenEstimate // 60K 默认值
    if h.app != nil {
        cfg := h.app.GetMaclawLLMConfig()
        if cfg.EffectiveContextTokens > 0 {
            dynamic := cfg.EffectiveContextTokens / 2
            if dynamic > 100_000 {
                dynamic = 100_000
            }
            if dynamic > threshold {
                threshold = dynamic
            }
        }
    }
    if estimateConversationEntryTokens(entries) < threshold {
        return entries
    }
    // ... 后续逻辑不变
}
```

**效果**：128K context 的模型，阈值从 60K 提升到 64K（128K×80%×50%）。对话历史在 64K 以内不触发 compaction，减少不必要的压缩。

**修改文件**：`gui/im_message_handler.go`

---

### 问题 2 (P1): compactHistory split 点改为信息密度感知

**根因**：`split = len(entries) / 2` 是固定的 50% 分割。前半部分可能包含关键的搜索结果（web_fetch 返回的论文数据）、文件写入确认（write_file 的路径）、数据统计（"99 篇论文"）。这些信息被 summarizer 压缩后可能丢失细节。

**修复**：split 点从固定 50% 改为"保留最近 N 条 entries 使 token 在预算内"。从后往前扫描，找到使 recent 部分 token 在预算内的分割点。

```go
// gui/im_message_handler.go — compactHistory

// 从后往前找 split 点：保留尽可能多的最近 entries，
// 使 recent 部分的 token 在 threshold 的 70% 以内
// （留 30% 给 compacted summary + 下一轮新增）。
recentBudget := threshold * 7 / 10
split := 0
runningTokens := 0
for i := len(entries) - 1; i >= 0; i-- {
    entryTokens := estimateSingleEntryTokens(entries[i])
    if runningTokens + entryTokens > recentBudget {
        split = i + 1
        break
    }
    runningTokens += entryTokens
}
if split <= 0 {
    return entries // 全部在预算内
}
// Group-align split point（已有逻辑不变）
```

**效果**：保留尽可能多的最近 entries（包含最新的工具调用结果），只压缩最早的部分。对于 35 轮迭代的对话，可能只压缩前 10 轮而非前 17 轮。

**修改文件**：`gui/im_message_handler.go`

---

### 问题 3 (P1): summarizer 输入增强——工具调用摘要 section

**根因**：`extractKeyDataFromEntries` 只提取文件路径、URL、数据统计三种模式。但工具调用的**操作语义**（"搜索了 HuggingFace Daily Papers"、"抓取了 5 个论文页面"、"生成了 PDF 报告"）丢失了。summarizer 的输入缺少"做了什么"的信息，只有"产出了什么文件"。

**修复**：新增 Section 2.5——工具调用操作摘要。从 old entries 中提取 tool call 的工具名 + 参数摘要。

```go
// gui/im_message_handler.go — compactHistory 的 summarizer 输入构建

// Section 2.5: Tool call operation summary.
// Captures WHAT was done (tool names + key args), not just WHAT was produced.
toolOps := extractToolOperationSummary(old, 15)
if len(toolOps) > 0 {
    sb.WriteString("## 执行的工具操作\n\n")
    for _, op := range toolOps {
        sb.WriteString("- ")
        sb.WriteString(op)
        sb.WriteString("\n")
    }
    sb.WriteString("\n")
}
```

```go
// extractToolOperationSummary extracts a concise summary of tool calls
// from conversation entries. Returns lines like:
//   "web_fetch: https://huggingface.co/papers"
//   "write_file: D:\workprj\hf_papers.json"
//   "generate_pdf: HF_World_日报_2026-04-30.pdf"
func extractToolOperationSummary(entries []agent.ConversationEntry, maxOps int) []string {
    var ops []string
    seen := make(map[string]bool)
    for _, e := range entries {
        if len(ops) >= maxOps {
            break
        }
        if e.Role != "assistant" {
            continue
        }
        // Extract tool calls from assistant entries
        toolCalls := extractToolCallsFromEntry(e)
        for _, tc := range toolCalls {
            if len(ops) >= maxOps {
                break
            }
            summary := tc.Name
            // Extract key argument (first string arg that looks meaningful)
            keyArg := extractKeyToolArg(tc.Name, tc.Arguments)
            if keyArg != "" {
                summary += ": " + keyArg
            }
            if !seen[summary] {
                seen[summary] = true
                ops = append(ops, summary)
            }
        }
    }
    return ops
}
```

`extractKeyToolArg` 按工具名提取最有意义的参数：
- `web_fetch` / `web_search` → URL 或 query
- `write_file` / `read_file` → path
- `generate_pdf` → title 或 output
- `bash` → command 的前 80 字符
- `send_file` → file_path
- `manage_skill` → name + action
- 其他 → 第一个非空字符串参数的前 80 字符

**效果**：summarizer 的输入从"对话轮次 + 文件路径 + 任务结果"增强为"对话轮次 + 文件路径 + 工具操作 + 任务结果"，LLM 能生成更完整的交接摘要。

**修改文件**：`gui/im_message_handler.go`

---

### 问题 4 (P1): ExtractedFact.Entities JSON 反序列化——容忍嵌套数组

**根因**：LLM prompt 说 `"entities": Entity-relation triples in the format ["entity:Name", "relation:relationship", "entity:Name2"]`。LLM 可能理解为"每个 triple 是一个数组"，返回 `[["entity:Alice", "relation:lives_in", "entity:Shanghai"]]`（数组的数组）。Go 的 `[]string` 无法反序列化嵌套数组。

**修复**：`Entities` 字段改为 `json.RawMessage`，手动解析时容忍两种格式。

```go
// corelib/memory/types.go

type ExtractedFact struct {
    Content   string          `json:"content"`
    Category  string          `json:"category"`
    Entities  json.RawMessage `json:"entities,omitempty"` // 改为 RawMessage
    ValidAt   string          `json:"valid_at,omitempty"`
    InvalidAt string          `json:"invalid_at,omitempty"`
}

// ParsedEntities returns the entities as a flat []string,
// tolerating both flat arrays and nested arrays from LLM output.
func (f ExtractedFact) ParsedEntities() []string {
    if len(f.Entities) == 0 {
        return nil
    }
    // Try flat array first: ["entity:X", "relation:Y", "entity:Z"]
    var flat []string
    if err := json.Unmarshal(f.Entities, &flat); err == nil {
        return flat
    }
    // Try nested array: [["entity:X", "relation:Y", "entity:Z"]]
    var nested [][]string
    if err := json.Unmarshal(f.Entities, &nested); err == nil {
        var result []string
        for _, arr := range nested {
            result = append(result, arr...)
        }
        return result
    }
    // Try single string: "entity:X"
    var single string
    if err := json.Unmarshal(f.Entities, &single); err == nil && single != "" {
        return []string{single}
    }
    return nil
}
```

所有消费 `fact.Entities` 的地方改为调用 `fact.ParsedEntities()`：
- `online_extractor.go`：`classifyAndIntegrate` 中 6 处 `fact.Entities` → `fact.ParsedEntities()`
- `online_extractor.go`：`buildFactTags` 中 `fact.Entities` → `fact.ParsedEntities()`
- `memory.Entry` 的 `Entities` 字段保持 `[]string` 不变（它是已解析的数据）

**修改文件**：
- `corelib/memory/types.go`：`ExtractedFact.Entities` 改为 `json.RawMessage` + 新增 `ParsedEntities()` 方法
- `corelib/memory/online_extractor.go`：所有 `fact.Entities` 改为 `fact.ParsedEntities()`

---

### 问题 5 (P2): compaction 后的 assistant 确认改为包含关键上下文

**根因**：`"好的，我已了解之前的对话上下文。"` 是固定文本，不包含任何实际信息。这条 assistant entry 占据一个 slot 但零信息量。

**修复**：assistant 确认改为包含 compacted summary 的关键数据摘要（从 summary 中提取前 3 条关键数据引用）。

```go
// gui/im_message_handler.go — compactHistory

summaryContent := resp.Choices[0].Message.Content

// 从 summary 中提取关键数据引用，构建有信息量的确认
ackText := "好的，我已了解之前的对话上下文。"
summaryRefs := extractKeyDataRefsFromText(summaryContent)
if len(summaryRefs) > 3 {
    summaryRefs = summaryRefs[:3]
}
if len(summaryRefs) > 0 {
    ackText += "关键数据：" + strings.Join(summaryRefs, "；") + "。"
}

compacted := []agent.ConversationEntry{
    {Role: "user", Content: compactionRecoveryPrefix + summaryContent},
    {Role: "assistant", Content: ackText},
}
```

**效果**：assistant 确认从空洞的"我已了解"变为"我已了解。关键数据：文件路径: D:\workprj\hf_papers.json；数据统计: 99篇Agent论文；URL: https://huggingface.co/papers。"

**修改文件**：`gui/im_message_handler.go`

---

## 验收标准

- 35 轮工具调用的对话（~55K token）在 128K context 模型上不触发 compaction（阈值提升到 64K）
- compaction 触发时，split 点保留尽可能多的最近 entries
- summarizer 输入包含工具操作摘要（"web_fetch: https://..."、"write_file: D:\..."）
- `online_extractor` 不再因 entities 嵌套数组报 JSON 解析错误
- compaction 后的 assistant 确认包含关键数据引用
- 所有现有 compaction / trimHistory / online_extractor 测试通过


---

## Review 发现的机制性问题（已修复）

### Review 问题 1 (P0): 两条压缩路径阈值不一致

**根因**：对话历史有两条独立压缩路径：

| 路径 | 触发时机 | 触发条件 | 作用 |
|------|---------|---------|------|
| `compactHistory` | 消息处理前（Load 后） | token 数 > threshold | LLM 摘要压缩前半部分 |
| `trimHistoryWithSummary` | 消息处理后（Save 前） | entry 数 > `MaxConversationTurns`(40) | 三层截断 |

日志中的 `entries=44->40` 是 `trimHistoryWithSummary` 触发的。提高 `compactHistory` 的 token 阈值对实际问题没有效果。

**修复**：
- `trimHistoryWithSummary` 新增 `maxEntries` 参数
- `saveConversationHistoryTimed` 计算动态 limit = `EffectiveContextTokens / 1500`，clamped [40, 80]
- 128K context 模型：limit 从 40 提升到 ~68，44 条 entries 不再触发压缩

### Review 问题 3 (P1): extractToolOperationSummary dedup 粒度过细

**根因**：20 次 `web_fetch`（每次不同 URL）产出 15 条 op，挤掉了其他工具。

**修复**：两遍扫描——先统计频率，再按工具名限制每个工具最多 2 条示例 + 高频工具追加 `(共N次)` 计数。

### 修改文件汇总

| 文件 | 变更 |
|------|------|
| `corelib/memory/types.go` | `ExtractedFact.Entities` → `RawEntities json.RawMessage` + `ParsedEntities()` 方法 |
| `corelib/memory/online_extractor.go` | 9 处 `fact.Entities` → `fact.ParsedEntities()` + 3 处编码修复 |
| `corelib/memory/extracted_fact_test.go` | 7 个新增测试 |
| `gui/im_conversation_trim.go` | `trimHistoryWithSummary` 新增 `maxEntries` 参数，内部 `MaxConversationTurns` → `limit` |
| `gui/im_message_handler.go` | `compactHistory` 重写（动态阈值 + 密度感知 split + 工具操作 section + 有信息量的确认）+ `saveConversationHistoryTimed` 动态 entry limit + `extractToolOperationSummary` 频率感知 dedup + 3 个新函数 |
| `docs/compaction-and-context-improvements.md` | 改进方案文档 |
| `.kiro/steering/maclaw-improvements.md` | #89 改进记录 |
