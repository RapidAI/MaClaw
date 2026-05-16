# 设计文档：智能工具路由 v2

## Overview

三层递进架构升级 Maclaw 工具选择管线。所有改进基于现有 `corelib/tool/` 架构增量演进，不改变 Router/DynamicToolBuilder 的公共 API。

## Architecture

```
用户消息
    ↓
DynamicToolBuilder.Build(userMessage)
  ├─ Registry.ListAvailable()
  ├─ EnrichmentStore.GetSearchText(tool)     ← Layer 1
  ├─ BM25 index (enriched text)
  ├─ [Optional] HybridRetriever.FuseScores
  ├─ UsageTracker.ExperienceScore()          ← Layer 2
  └─ 三信号融合: α*retrieval + β*experience + γ*priority
      ↓
  Top-K tools + discover_tool                ← Layer 3
    ↓
Router.Route(userMessage, tools)
  ├─ Core tools (永远包含)
  ├─ Session tools (discover_tool 激活的)    ← Layer 3
  └─ Scored candidates
    ↓
LLM → tool_call → 执行 → UsageTracker.Record()  ← Layer 2
```

## Components

### 1. EnrichmentStore (`corelib/tool/enrichment.go`)

```go
type ToolEnrichment struct {
    ToolName         string   `json:"tool_name"`
    SyntheticQueries []string `json:"synthetic_queries"`
    UpdatedAt        time.Time `json:"updated_at"`
}

type EnrichmentStore struct {
    mu          sync.RWMutex
    enrichments map[string]*ToolEnrichment
    path        string
}

func NewEnrichmentStore(path string) (*EnrichmentStore, error)
func (s *EnrichmentStore) GetSearchText(t RegisteredTool) string
func (s *EnrichmentStore) Set(toolName string, queries []string) error
func (s *EnrichmentStore) Load() error
func (s *EnrichmentStore) save() error
```

内置工具 enrichment 通过 `BuiltinEnrichments` map 硬编码。
MCP/Skill 工具通过 `GenerateEnrichment(name, desc string, llm LLMCaller) []string` 异步生成。

### 2. UsageTracker (`corelib/tool/usage_tracker.go`)

```go
type UsageRecord struct {
    ToolName    string    `json:"tool_name"`
    QueryTokens []string  `json:"query_tokens"`
    Success     bool      `json:"success"`
    Timestamp   time.Time `json:"timestamp"`
}

type UsageTracker struct {
    mu       sync.RWMutex
    records  []UsageRecord
    path     string
    maxItems int
}

func NewUsageTracker(path string) (*UsageTracker, error)
func (t *UsageTracker) Record(toolName string, queryTokens []string, success bool)
func (t *UsageTracker) ExperienceScore(toolName string, queryTokens []string) float64
```

ExperienceScore 算法：
1. 筛选 toolName 匹配的 records
2. 对每条 record 计算 Jaccard(queryTokens, record.QueryTokens)
3. 乘以 recency weight: exp(-0.01 * hours_since)
4. 乘以 success weight: success=1.0, failure=-0.3
5. 求和后 clamp 到 [0, 1]

### 3. Session Tools (Router 扩展)

Router 新增字段：
```go
type Router struct {
    // ... 现有字段
    sessionTools map[string]bool
    enrichStore  *EnrichmentStore
    tracker      *UsageTracker
}
```

新增方法：
- `SetEnrichmentStore(*EnrichmentStore)`
- `SetUsageTracker(*UsageTracker)`
- `ActivateSessionTool(name string)`
- `ResetSession()`

### 4. discover_tool

加入 CoreToolNames，handler 在 agent loop 中实现：
- 输入: `{"need": "describe capability needed"}`
- 执行: 从 Registry 全量工具中 BM25 检索 top-5
- 输出: 匹配工具列表（名称+描述）
- 副作用: 调用 Router.ActivateSessionTool() 将匹配工具加入 session

### 5. 打分公式

Router.Route() 和 DynamicToolBuilder.Build() 中的打分改为：

```
final_score = α * retrieval_score + β * experience_score + γ * priority_bonus

α = 0.6, β = 0.3, γ = 0.1
retrieval_score = minmax_normalized(bm25 或 hybrid fused)
experience_score = UsageTracker.ExperienceScore(toolName, queryTokens)  // [0,1]
priority_bonus = clamp(tool.Priority * 0.1, 0, 1)
```

当 tracker == nil 时，β 的权重归入 α：`final = 0.9 * retrieval + 0.1 * priority`

## 设计决策

- D1: enrichment 用 JSON 文件而非 SQLite，因为数据量小（<200 条）且读多写少
- D2: usage tracker 用 ring buffer 而非无限增长，2000 条足够覆盖近期使用模式
- D3: experience score 用 token overlap 而非向量相似度，避免对每条 record 做嵌入
- D4: discover_tool 返回文本描述而非直接注入工具定义，因为 OpenAI 协议要求工具定义在请求开始时确定
- D5: session tools 不持久化，每次新会话清空，避免工具列表无限膨胀

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `corelib/tool/enrichment.go` | 新建 | EnrichmentStore + BuiltinEnrichments |
| `corelib/tool/usage_tracker.go` | 新建 | UsageTracker + ExperienceScore |
| `corelib/tool/router.go` | 修改 | 加入 enrichStore/tracker/sessionTools，修改打分逻辑 |
| `corelib/tool/builder.go` | 修改 | 加入 enrichStore/tracker，修改打分逻辑 |
| `gui/tool_router.go` | 修改 | 透传 enrichStore/tracker 到 corelib Router |
| `gui/tool_builder.go` | 修改 | 透传 enrichStore/tracker 到 corelib Builder |
