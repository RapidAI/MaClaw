# OpenHuman 借鉴改进方案

**来源**：[tinyhumansai/openhuman](https://github.com/tinyhumansai/openhuman) — 开源个人 AI 超级智能助手（Rust 70% + TypeScript 26%，Tauri 桌面端，5.1K stars）

**日期**：2026-05-14

---

## 一、项目概览与核心差异

| 维度 | MacLaw | OpenHuman |
|------|--------|-----------|
| 语言 | Go + TypeScript | Rust + TypeScript |
| 桌面框架 | Wails | Tauri |
| 记忆存储 | JSON 文件 + 内存 BM25 | SQLite + FTS5 + 向量 + 图谱 |
| 记忆结构 | 扁平 `[]Entry` | 三棵同心树（source/topic/global） |
| Token 管理 | 零散 truncate/sanitize | 独立 TokenJuice 模块 |
| 后台思考 | 6h Pipeline 批处理 | 持续 Subconscious 引擎 |
| 学习系统 | UsageTracker + KnowledgeExtractor | 独立 Learning 域 |
| 外部数据 | 被动（用户触发） | 主动 Auto-Fetch（20min 循环） |
| 模型路由 | 全局单一模型 | 按任务类型路由 |
| 子 Agent | 硬编码 2 个 | TOML 声明式注册表 |
| 成本追踪 | 无 | USD cost tracking + stop hooks |
| 安全 | 工具参数检查 | 独立 Prompt Injection Guard |
| 集成 | SSH + IM + Skill | 118+ OAuth（Composio） |

---

## 二、改进项清单（按优先级排序）

### Phase A：短期高收益（1-2 周内可完成）

| # | 改进项 | 来源模块 | 预期收益 |
|---|--------|---------|---------|
| A1 | TokenJuice 统一压缩层 | `tokenjuice/` | context token 减少 30-50% |
| A2 | Per-Tool Result Caps | `tools/traits.rs` | 防止 context 膨胀 |
| A3 | Model Routing | `providers/`, `routing/` | 降低延迟+成本 |
| A4 | Tool-Scoped Memory | `memory/tool_memory/` | 工具调用成功率提升 |
| A5 | Situation Report 注入 | `subconscious/situation_report/` | agent 全局视野 |

### Phase B：中期架构升级（2-4 周）

| # | 改进项 | 来源模块 | 预期收益 |
|---|--------|---------|---------|
| B1 | Knowledge Stability Detector | `learning/stability_detector.rs` | 召回质量提升 |
| B2 | Heartbeat 主动通知 | `heartbeat/` | IM 通道主动推送 |
| B3 | Agent Definition Registry | `agent/agents/`, `harness/definition.rs` | 声明式子 Agent |
| B4 | Event Bus 解耦 | `core/event_bus/` | 减少 sync.Map 耦合 |
| B5 | USD Cost Tracking | `agent/cost.rs`, `stop_hooks.rs` | 成本感知 |

### Phase C：长期架构演进（1-2 月）

| # | 改进项 | 来源模块 | 预期收益 |
|---|--------|---------|---------|
| C1 | Memory Tree 分层摘要 | `memory/tree/` | 长期记忆质量 |
| C2 | Subconscious 持续引擎 | `subconscious/engine.rs` | 实时后台反思 |
| C3 | Auto-Fetch 主动数据拉取 | `integrations/`, `composio/` | 预加载上下文 |
| C4 | Fork Context (KV-cache 复用) | `agent/harness/fork_context.rs` | SubAgent 延迟降低 |
| C5 | Prompt Injection Guard | `prompt_injection/` | 安全纵深 |

---

## 三、Phase A 详细设计

### A1: TokenJuice 统一压缩层

**OpenHuman 做法**：独立的 `tokenjuice/` 模块，对所有工具输出在进入 LLM 前做统一压缩：
- `classify.rs` — 内容类型分类（HTML/JSON/terminal/plain）
- `reduce.rs` — 按类型应用压缩规则
- `rules/` — 可配置的压缩规则集
- `tool_integration.rs` — 与工具系统的集成点

**MacLaw 实现方案**：

新增 `corelib/tool/token_compress.go`：

```go
// CompressToolResult 对工具返回结果做统一压缩
// 在 executeTool 返回后、结果注入 conversation 前调用
func CompressToolResult(toolName string, result string, maxTokens int) string {
    contentType := classifyContent(result)
    switch contentType {
    case ContentHTML:
        return compressHTML(result, maxTokens)      // HTML→Markdown + 去噪
    case ContentJSON:
        return compressJSON(result, maxTokens)      // 深层嵌套折叠 + 数组截断
    case ContentTerminal:
        return compressTerminal(result, maxTokens)  // ANSI 剥离 + 重复行合并
    case ContentPlain:
        return compressPlain(result, maxTokens)     // URL 缩短 + 空白合并
    }
    return truncateToTokens(result, maxTokens)
}
```

**压缩规则**：
- HTML → Markdown（已有 `sanitizeHTMLBody`，提升为通用模块）
- 长 URL → `[domain.com/...path]`（保留域名和最后路径段）
- JSON 数组 >10 项 → 保留前 3 + 后 2 + `[...省略 N 项]`
- 终端输出重复行 → `[重复 N 次]`
- 连续空行 → 单空行
- Base64 数据 → `[base64 data, N bytes]`

**接入点**：`gui/im_tool_execution.go` 的 `executeTool()` 返回后统一调用。

**预期效果**：web_fetch 返回的 HTML 从 ~8K token 压缩到 ~2K；bash 编译输出从 ~5K 压缩到 ~1K。

---

### A2: Per-Tool Result Caps（工具输出硬截断）

**OpenHuman 做法**：`tools/traits.rs` 中每个工具有 `result_cap` 属性，agent harness 在工具执行后强制截断。

**MacLaw 实现方案**：

新增 `corelib/tool/result_caps.go`：

```go
// DefaultResultCaps 按工具类型定义默认 token 上限
var DefaultResultCaps = map[string]int{
    "web_fetch":       3000,  // 网页内容
    "web_search":      2000,  // 搜索结果
    "bash":            4000,  // 命令输出
    "read_file":       5000,  // 文件内容
    "ssh":             3000,  // SSH 输出
    "list_directory":  1500,  // 目录列表
    "screenshot":      500,   // 截图描述
    "manage_skill":    2000,  // Skill 操作结果
    "_default":        4000,  // 未配置的工具
}

// ApplyResultCap 对工具结果应用 token 上限
// 先经过 CompressToolResult 压缩，仍超限则硬截断
func ApplyResultCap(toolName, result string) string {
    cap := DefaultResultCaps[toolName]
    if cap == 0 { cap = DefaultResultCaps["_default"] }
    compressed := CompressToolResult(toolName, result, cap)
    if estimateTokens(compressed) > cap {
        return truncateWithNotice(compressed, cap)
    }
    return compressed
}
```

**与 TokenJuice 的关系**：TokenJuice 做智能压缩（保留信息密度），Result Cap 做硬截断（最后防线）。流程：`工具返回 → TokenJuice 压缩 → Result Cap 截断 → 注入 conversation`。

---

### A3: Model Routing（按任务类型路由模型）

**OpenHuman 做法**：`routing/` 模块根据任务类型（reasoning/fast/vision）自动选择 LLM，`model_routes` 配置在 settings 中。

**MacLaw 实现方案**：

新增 `corelib/llm/model_router.go`：

```go
type ModelRoute struct {
    Task     string // "intent" | "coding" | "vision" | "fast" | "reasoning" | "default"
    Provider string // provider name
    Model    string // model name
}

type ModelRouter struct {
    routes map[string]ModelRoute
    fallback ModelRoute
}

// Route 根据任务类型返回对应的 LLM 配置
func (r *ModelRouter) Route(task string) (provider, model string) {
    if route, ok := r.routes[task]; ok {
        return route.Provider, route.Model
    }
    return r.fallback.Provider, r.fallback.Model
}
```

**任务类型定义**：

| Task | 用途 | 推荐模型特征 |
|------|------|------------|
| `intent` | 意图理解、工作流确认 | 快速、便宜 |
| `fast` | 简单问答、闲聊 | 快速、便宜 |
| `reasoning` | 编码、复杂分析 | 强推理 |
| `vision` | 图片识别、截图分析 | 多模态 |
| `summary` | 摘要、压缩 | 快速、便宜 |
| `default` | 主 agent loop | 平衡 |

**配置方式**：`config.json` 新增 `model_routes` 字段：
```json
{
  "model_routes": {
    "intent": {"provider": "zhipu", "model": "glm-4-flash"},
    "fast": {"provider": "zhipu", "model": "glm-4-flash"},
    "reasoning": {"provider": "deepseek", "model": "deepseek-coder"},
    "vision": {"provider": "zhipu", "model": "glm-4v-plus"},
    "default": {"provider": "zhipu", "model": "glm-5.1"}
  }
}
```

**接入点**：
- `gui/im_message_handler_workflow.go`：意图理解 LLM 调用 → `Route("intent")`
- `gui/coding_subagent.go`：SubAgent → `Route("reasoning")`
- `gui/im_message_handler.go`：主 agent loop → `Route("default")`
- `gui/im_conversation_trim.go`：摘要 LLM → `Route("summary")`

---

### A4: Tool-Scoped Memory（工具级持久记忆）

**OpenHuman 做法**：`memory/tool_memory/` 让每个工具有自己的 KV 命名空间，工具执行中学到的规则被持久化，下次调用时自动注入。

**MacLaw 实现方案**：

新增 `corelib/tool/tool_memory.go`：

```go
// ToolMemoryStore 工具级持久记忆
type ToolMemoryStore struct {
    mu    sync.RWMutex
    rules map[string][]ToolRule  // toolName → rules
    path  string                 // 持久化路径
}

type ToolRule struct {
    Key       string    // 规则标识（如 "ssh:api.rapidai.tech:profile_source"）
    Content   string    // 规则内容（如 "连接后需要 source /etc/profile"）
    LearnedAt time.Time
    UsedCount int
    Confidence float64  // 0-1，多次验证后提升
}

// InjectRules 在工具执行前注入相关规则到 system message
func (s *ToolMemoryStore) InjectRules(toolName string, args map[string]interface{}) string

// LearnRule 工具执行后学习新规则
func (s *ToolMemoryStore) LearnRule(toolName, key, content string)

// ConfirmRule 规则被验证有效时提升置信度
func (s *ToolMemoryStore) ConfirmRule(toolName, key string)
```

**学习触发场景**：
- SSH 连接后发现需要 `source /etc/profile` → 学习规则
- `write_file` 在某目录需要先 `mkdir -p` → 学习规则
- `web_fetch` 某网站需要特定 User-Agent → 学习规则
- `bash` 在某项目需要先 `cd` 到特定目录 → 学习规则

**注入方式**：`executeTool` 调用前，将匹配的规则作为 tool_result 的前缀注入：
```
[工具记忆] 上次连接 api.rapidai.tech 时发现：连接后需要执行 source /etc/profile 才能使用 go 命令
```

---

### A5: Situation Report 注入（情境摘要）

**OpenHuman 做法**：`subconscious/situation_report/` 定期生成用户当前情境的结构化报告，注入 system prompt。

**MacLaw 实现方案**：

新增 `gui/im_situation_report.go`：

```go
// BuildSituationReport 从多个数据源综合生成情境摘要
// 在 appendProactiveRecall 之前注入，最多 200 token
func (h *IMMessageHandler) BuildSituationReport(userID string) string {
    var sections []string

    // 1. 活跃任务（从 in-flight marker 或 unfinished slot）
    if task := h.getInFlightTask(userID); task != "" {
        sections = append(sections, "进行中任务: "+task)
    }

    // 2. 最近完成的任务（从 task_artifact 记忆）
    recent := h.recallRecentArtifacts(userID, 3)
    if len(recent) > 0 {
        sections = append(sections, "最近完成: "+strings.Join(recent, "; "))
    }

    // 3. 活跃 SSH 会话
    if sessions := h.getActiveSSHSessions(userID); len(sessions) > 0 {
        sections = append(sections, "活跃SSH: "+strings.Join(sessions, ", "))
    }

    // 4. 活跃工作流阶段
    if ws := h.getActiveWorkflowState(userID); ws != nil {
        sections = append(sections, fmt.Sprintf("工作流: %s/%s", ws.Type, ws.CurrentPhase))
    }

    // 5. 当前时间上下文
    sections = append(sections, "当前时间: "+time.Now().Format("2006-01-02 15:04 Mon"))

    if len(sections) == 0 { return "" }
    return "[当前情境]\n" + strings.Join(sections, "\n")
}
```

**注入位置**：`buildSystemPromptBase()` 中，在核心原则之后、记忆之前。

---

## 四、Phase B 详细设计

### B1: Knowledge Stability Detector（知识稳定性检测）

**OpenHuman 做法**：`learning/stability_detector.rs` 追踪每条知识的稳定性——多次确认 → stable，被纠正 → volatile。

**MacLaw 实现方案**：

`corelib/memory/types.go` 的 `Entry` 新增字段：

```go
type Entry struct {
    // ... 现有字段 ...
    Stability      StabilityLevel `json:"stability,omitempty"`       // stable/volatile/unverified
    ConfirmCount   int            `json:"confirm_count,omitempty"`   // 被验证次数
    ContradictCount int           `json:"contradict_count,omitempty"` // 被矛盾次数
    LastVerifiedAt time.Time      `json:"last_verified_at,omitempty"`
}

type StabilityLevel string
const (
    StabilityUnverified StabilityLevel = ""          // 默认，新写入
    StabilityStable     StabilityLevel = "stable"    // ConfirmCount >= 3 且 ContradictCount == 0
    StabilityVolatile   StabilityLevel = "volatile"  // ContradictCount > 0
)
```

**稳定性更新触发**：
- LLM 调用 `memory(action=save)` 时，检查新内容是否与已有 entry 矛盾或确认
- `KnowledgeExtractor` 提取知识时，与已有 entry 做语义比对
- 用户明确纠正（"不对，应该是 XXX"）时标记 volatile

**召回权重调整**：
- `RecallDynamic` 的 RRF 融合后，对 stable entry 加 +2.0 boost，volatile entry 加 -1.0 penalty
- proactive recall 优先注入 stable 知识

---

### B2: Heartbeat 主动通知

**OpenHuman 做法**：`heartbeat/` 模块定期检查并主动推送通知（会议提醒、重要邮件等）。

**MacLaw 实现方案**：

新增 `gui/heartbeat.go`：

```go
type HeartbeatEngine struct {
    interval time.Duration  // 默认 5 分钟
    checks   []HeartbeatCheck
    notifier HeartbeatNotifier
}

type HeartbeatCheck interface {
    Name() string
    Check(ctx context.Context) []HeartbeatAlert
}

type HeartbeatAlert struct {
    Priority  string // "high" | "medium" | "low"
    Title     string
    Body      string
    ActionURL string
}
```

**内置检查项**：
- `SSHBackgroundTaskCheck` — 检查 SSH 后台任务是否完成/失败
- `LocalBackgroundTaskCheck` — 检查本机后台任务状态
- `SkillExecutionCheck` — 检查异步 Skill 执行结果
- `UnfinishedTaskCheck` — 提醒长时间未继续的任务
- `ScheduledTaskCheck` — 定时任务触发

**通知通道**：
- 桌面面板：Toast 通知 + 系统通知
- IM 通道（飞书/微信）：主动发送消息

---

### B3: Agent Definition Registry（声明式子 Agent 注册表）

**OpenHuman 做法**：从 TOML 文件加载子 Agent 定义，包含 `SandboxMode` 和 `ToolScope`。

**MacLaw 实现方案**：

新增 `corelib/agent/definition.go`：

```go
type AgentDefinition struct {
    Name        string   `yaml:"name"`
    Description string   `yaml:"description"`
    SystemPrompt string  `yaml:"system_prompt"`
    Tools       []string `yaml:"tools"`        // 可用工具白名单
    MaxRounds   int      `yaml:"max_rounds"`   // 最大迭代次数
    Model       string   `yaml:"model"`        // 使用的模型路由 task
    Sandbox     string   `yaml:"sandbox"`      // "full" | "readonly" | "none"
}

type AgentRegistry struct {
    mu          sync.RWMutex
    definitions map[string]*AgentDefinition
    dirs        []string  // 扫描目录列表
}

// LoadFromDirs 从目录加载 YAML 定义
func (r *AgentRegistry) LoadFromDirs() error

// Get 获取定义
func (r *AgentRegistry) Get(name string) *AgentDefinition
```

**目录结构**：
```
~/.maclaw/agents/
├── code_reviewer.yaml
├── doc_translator.yaml
├── test_generator.yaml
└── research_assistant.yaml
```

**YAML 示例**：
```yaml
name: code_reviewer
description: "代码审查专家，检查代码质量、安全性和最佳实践"
system_prompt: |
  你是一个代码审查专家。审查时关注：
  1. 安全漏洞（SQL注入、XSS、路径遍历等）
  2. 性能问题（N+1查询、内存泄漏等）
  3. 代码风格和可维护性
  4. 测试覆盖率
tools:
  - read_file
  - list_directory
  - bash
max_rounds: 20
model: reasoning
sandbox: readonly
```

**与现有 `delegate_task` 的关系**：`builtinSubAgents` 改为从 registry 加载，硬编码的 `coding_workflow` 和 `help` 作为内置默认定义。

---

### B4: Event Bus 解耦

**OpenHuman 做法**：`core/event_bus/` 全局事件总线，模块间通过类型化事件通信。

**MacLaw 实现方案**：

新增 `corelib/eventbus/bus.go`：

```go
type Event interface {
    Domain() string  // "memory" | "agent" | "workflow" | "tool" | "session"
    Type() string    // "saved" | "recalled" | "started" | "completed" | "failed"
}

type Bus struct {
    mu          sync.RWMutex
    subscribers map[string][]chan Event  // domain → subscribers
}

func (b *Bus) Publish(evt Event)
func (b *Bus) Subscribe(domain string) <-chan Event
func (b *Bus) Unsubscribe(domain string, ch <-chan Event)
```

**替代的 sync.Map 状态传递**：

| 现有 sync.Map | 替代事件 |
|--------------|---------|
| `workflowAgentLoopMarker` | `WorkflowEvent{Type: "agent_loop_requested"}` |
| `pendingAskUser` | `AgentEvent{Type: "ask_user_pending"}` |
| `workflowPendingConfirmOther` | `WorkflowEvent{Type: "confirm_bypassed"}` |
| `pendingCapabilityGap` | `ToolEvent{Type: "capability_gap_detected"}` |
| `sessionDriftReplanCount` | `AgentEvent{Type: "drift_detected"}` |

**渐进迁移**：不一次性替换所有 sync.Map，先对新增功能使用 Event Bus，逐步迁移旧代码。

---

### B5: USD Cost Tracking（成本追踪）

**OpenHuman 做法**：`agent/cost.rs` 追踪每次 LLM 调用的美元成本，`stop_hooks.rs` 在超限时停止。

**MacLaw 实现方案**：

新增 `corelib/llm/cost_tracker.go`：

```go
type CostTracker struct {
    mu           sync.Mutex
    sessionCost  float64           // 当前会话累计成本
    dailyCost    float64           // 今日累计成本
    dailyDate    string            // 今日日期
    priceTable   map[string]Price  // model → price
    budgetLimit  float64           // 日预算上限（0=无限制）
}

type Price struct {
    InputPerMToken  float64  // 每百万 input token 价格（USD）
    OutputPerMToken float64  // 每百万 output token 价格（USD）
}

// Record 记录一次 LLM 调用的成本
func (t *CostTracker) Record(model string, inputTokens, outputTokens int) float64

// IsOverBudget 检查是否超出日预算
func (t *CostTracker) IsOverBudget() bool

// SessionSummary 返回当前会话的成本摘要
func (t *CostTracker) SessionSummary() string
```

**价格表**（内置默认值，可通过 config 覆盖）：
```go
var DefaultPriceTable = map[string]Price{
    "glm-5.1":          {InputPerMToken: 5.0, OutputPerMToken: 15.0},   // ¥ 转 USD
    "glm-4-flash":      {InputPerMToken: 0.1, OutputPerMToken: 0.3},
    "deepseek-coder":   {InputPerMToken: 1.0, OutputPerMToken: 2.0},
    "claude-sonnet":    {InputPerMToken: 3.0, OutputPerMToken: 15.0},
}
```

**展示**：
- Agent loop 结束后在日志中记录 `[cost] session=$0.023 (input=45K, output=8K)`
- 桌面面板状态栏显示当日累计成本
- 超出日预算时提示用户

---

## 五、Phase C 详细设计

### C1: Memory Tree 分层摘要

**OpenHuman 做法**：`memory/tree/` 实现三棵同心树：
- `tree_source` — 按数据源分桶（Gmail/Slack/GitHub/对话等）
- `tree_topic` — 按主题聚合（跨源的同一话题）
- `tree_global` — 全局摘要（用户画像级别）

每棵树的节点定期 "seal"（摘要压缩到父节点），形成层级结构。

**MacLaw 实现方案**：

新增 `corelib/memory/tree/` 包：

```
corelib/memory/tree/
├── types.go          // TreeNode, TreeLevel, SealConfig
├── source_tree.go    // 按来源分桶（对话/工具/工作流/外部）
├── topic_tree.go     // 按主题聚合（BM25 + embedding 聚类）
├── global_tree.go    // 全局摘要（用户画像）
├── sealer.go         // 定期 seal 逻辑（LLM 摘要压缩）
└── retrieval.go      // 从树中检索（自顶向下展开）
```

**核心数据结构**：
```go
type TreeNode struct {
    ID        string
    Level     TreeLevel    // L0=原始chunk, L1=日摘要, L2=周摘要, L3=月摘要
    Content   string       // 摘要内容（≤3K token）
    Children  []string     // 子节点 ID
    Source    string       // 数据来源
    Topic     string       // 主题标签
    SealedAt  time.Time
    TokenCount int
}

type TreeLevel int
const (
    TreeLevelChunk   TreeLevel = 0  // 原始 chunk（≤500 token）
    TreeLevelDaily   TreeLevel = 1  // 日摘要
    TreeLevelWeekly  TreeLevel = 2  // 周摘要
    TreeLevelMonthly TreeLevel = 3  // 月摘要
)
```

**Seal 策略**：
- L0 → L1：每天凌晨，将当天的 chunks 按主题聚类后 LLM 摘要
- L1 → L2：每周日，将本周的日摘要 LLM 压缩
- L2 → L3：每月 1 日，将本月的周摘要 LLM 压缩

**检索策略**：
- 先在 L3（全局）中找到相关主题
- 展开到 L2（周）获取更多细节
- 必要时展开到 L1（日）获取具体事件
- 极少展开到 L0（原始 chunk）

**与现有 `memory.Store` 的关系**：
- `memory.Store` 保留为"热存储"（最近 500 条，快速读写）
- `memory/tree/` 作为"温存储"（结构化长期记忆，按需检索）
- `ArchiveStore` 保留为"冷存储"（被淘汰的条目）

---

### C2: Subconscious 持续引擎

**OpenHuman 做法**：`subconscious/engine.rs` 是持续运行的后台引擎，在用户不交互时：
- 反思最近对话，提取洞察
- 检测知识矛盾
- 更新用户画像
- 生成情境报告

**MacLaw 实现方案**：

新增 `corelib/memory/subconscious.go`：

```go
type SubconsciousEngine struct {
    store       *Store
    llmClient   LLMClient
    interval    time.Duration  // 默认 30 分钟
    stopCh      chan struct{}

    // 子系统
    reflector   *Reflector       // 对话反思
    consolidator *Consolidator   // 知识整合
    profiler    *UserProfiler    // 画像更新
    detector    *ContradictionDetector  // 矛盾检测
}

// Start 启动后台引擎
func (e *SubconsciousEngine) Start()

// 每个周期执行的任务
func (e *SubconsciousEngine) tick(ctx context.Context) {
    // 1. 反思最近对话（如果有新对话）
    e.reflector.ReflectRecent(ctx)

    // 2. 检测知识矛盾
    e.detector.ScanContradictions(ctx)

    // 3. 整合碎片知识
    e.consolidator.ConsolidateFragments(ctx)

    // 4. 更新用户画像（每 6 个周期一次）
    if e.tickCount % 6 == 0 {
        e.profiler.UpdateProfile(ctx)
    }
}
```

**与现有 Pipeline 的关系**：
- 现有 6h Pipeline（`decay → compress → promote → reflect → consolidate → profile`）改为由 SubconsciousEngine 调度
- 各步骤从"6h 批量执行"改为"增量执行"（每 30 分钟处理新增的 entries）
- Pipeline 的 `RunFull()` 保留为手动触发的全量重建

---

### C3: Auto-Fetch 主动数据拉取

**OpenHuman 做法**：每 20 分钟从所有已连接的集成拉取新数据，摄入到 Memory Tree。

**MacLaw 实现方案**：

新增 `gui/auto_fetch.go`：

```go
type AutoFetchEngine struct {
    interval    time.Duration  // 默认 20 分钟
    connectors  []DataConnector
    memoryStore *memory.Store
    stopCh      chan struct{}
}

type DataConnector interface {
    Name() string
    IsConfigured() bool
    FetchNew(ctx context.Context, since time.Time) ([]DataItem, error)
}

type DataItem struct {
    Source    string    // "gmail" | "github" | "notion" | ...
    Title     string
    Content   string
    Timestamp time.Time
    URL       string
}
```

**初期支持的 Connector**：
- `GitHubConnector` — 拉取 starred repos 的新 release、关注的 issue 更新
- `RSSConnector` — 拉取配置的 RSS 源
- `FileWatchConnector` — 监控指定目录的文件变化

**摄入流程**：
```
DataConnector.FetchNew() → TokenJuice 压缩 → memory.SaveWithContext() → Tree seal
```

**配置**：`config.json` 新增 `auto_fetch` 字段：
```json
{
  "auto_fetch": {
    "enabled": true,
    "interval_minutes": 20,
    "connectors": {
      "github": {"token": "ghp_xxx", "watch_repos": ["owner/repo"]},
      "rss": {"feeds": ["https://example.com/feed.xml"]},
      "file_watch": {"dirs": ["~/Documents/notes"]}
    }
  }
}
```

---

### C4: Fork Context（KV-Cache 复用）

**OpenHuman 做法**：`agent/harness/fork_context.rs` 让子任务继承父 context 的 KV cache，减少重复 prefill。

**MacLaw 实现方案**：

当 LLM 提供商支持 prompt caching（如 Anthropic 的 cache_control、OpenAI 的 cached tokens）时：

```go
type ForkableContext struct {
    // 可缓存的前缀（system prompt + 工具定义 + 记忆）
    CacheablePrefix []llm.Message
    PrefixHash      string  // 用于判断 cache 是否可复用

    // 每个 fork 独有的后缀（对话历史）
    forks map[string][]llm.Message
}

// Fork 创建子上下文，共享 CacheablePrefix
func (fc *ForkableContext) Fork(forkID string) *ForkedContext

// 子上下文的 LLM 调用自动复用父 prefix 的 KV cache
func (f *ForkedContext) BuildMessages() []llm.Message {
    return append(f.parent.CacheablePrefix, f.ownMessages...)
}
```

**适用场景**：
- `CodingSubAgent` 的多个任务共享同一个 system prompt + 工具定义（~7K token）
- 主 Agent 和 SubAgent 共享记忆注入部分
- 工作流多阶段共享需求/设计文档

**预期收益**：SubAgent 首 token 延迟从 ~3s 降到 ~1s（prefix cache hit 时跳过 prefill）。

---

### C5: Prompt Injection Guard（提示注入防护）

**OpenHuman 做法**：`prompt_injection/` 在模型调用和工具执行前强制检查输入是否包含注入攻击。

**MacLaw 实现方案**：

新增 `corelib/security/injection_guard.go`：

```go
type InjectionGuard struct {
    patterns []InjectionPattern
    enabled  bool
}

type InjectionPattern struct {
    Name    string
    Pattern *regexp.Regexp
    Risk    string  // "high" | "critical"
}

// Check 检查文本是否包含注入攻击
func (g *InjectionGuard) Check(text string) *InjectionAlert

// CheckToolResult 检查工具返回结果是否包含注入指令
func (g *InjectionGuard) CheckToolResult(toolName, result string) *InjectionAlert
```

**检测模式**：
- `ignore previous instructions` / `忽略之前的指令`
- `you are now` / `你现在是` + 角色切换
- `system:` / `[SYSTEM]` 伪系统消息
- `<|im_start|>` / `<|endoftext|>` 特殊 token
- Base64 编码的指令（解码后再检查）
- Markdown 链接中隐藏的指令 `[click](data:text/html,...)`

**检查点**：
- 用户消息进入 agent loop 前
- 工具返回结果注入 conversation 前
- web_fetch / web_search 结果注入前
- 文件内容读取后

**处理策略**：
- 检测到注入 → 剥离注入部分 + 在 system prompt 中追加警告
- 不直接拒绝（避免误报影响正常使用），而是让 LLM 知道"这段内容可能包含注入尝试"

---

## 六、实施路线图

```
Week 1-2 (Phase A):
├── A1: TokenJuice 统一压缩层
│   └── corelib/tool/token_compress.go + gui/im_tool_execution.go 接入
├── A2: Per-Tool Result Caps
│   └── corelib/tool/result_caps.go + 与 A1 串联
└── A3: Model Routing
    └── corelib/llm/model_router.go + config.json 配置 + 4 个接入点

Week 3-4 (Phase A continued + Phase B start):
├── A4: Tool-Scoped Memory
│   └── corelib/tool/tool_memory.go + gui/im_tool_execution.go 注入
├── A5: Situation Report
│   └── gui/im_situation_report.go + im_system_prompt.go 注入
├── B1: Stability Detector
│   └── corelib/memory/stability.go + store.go 集成
└── B5: USD Cost Tracking
    └── corelib/llm/cost_tracker.go + gui/im_message_handler.go 记录

Week 5-6 (Phase B):
├── B2: Heartbeat 主动通知
│   └── gui/heartbeat.go + hub/ IM 推送
├── B3: Agent Definition Registry
│   └── corelib/agent/definition.go + ~/.maclaw/agents/ 扫描
└── B4: Event Bus（设计 + 新功能使用）
    └── corelib/eventbus/bus.go + 新增功能优先使用

Week 7-12 (Phase C):
├── C1: Memory Tree 分层摘要
│   └── corelib/memory/tree/ 包 + seal 调度
├── C2: Subconscious 持续引擎
│   └── corelib/memory/subconscious.go + Pipeline 改造
├── C3: Auto-Fetch
│   └── gui/auto_fetch.go + GitHub/RSS/FileWatch connector
├── C4: Fork Context
│   └── corelib/llm/fork_context.go + SubAgent 接入
└── C5: Prompt Injection Guard
    └── corelib/security/injection_guard.go + 4 个检查点
```

---

## 七、与现有改进的关系

| 现有改进（steering 中记录） | OpenHuman 借鉴项 | 关系 |
|--------------------------|-----------------|------|
| #62 高价值产出物实时沉淀 | C1 Memory Tree | Tree 是 #62 的架构升级 |
| #63 上下文感知标签增强 | A4 Tool-Scoped Memory | 互补——A4 是工具级，#63 是对话级 |
| #64 写入时增量子串去重 | C1 Memory Tree seal | seal 时自然去重 |
| #66 对话历史智能压缩 | A1 TokenJuice | TokenJuice 在压缩前减少输入量 |
| #67 OwnerID 多用户隔离 | — | 已完成，不受影响 |
| #74 Context 膨胀修复 | A2 Result Caps | Result Caps 是 #74 的预防层 |
| #75 编码 SubAgent | C4 Fork Context | Fork Context 优化 SubAgent 性能 |
| #85 LLM 429 重试 | B5 Cost Tracking | 成本追踪可预警即将触发 rate limit |
| #88 write_file 截断 | A1 TokenJuice | 压缩后工具参数更短，减少截断 |
| #89 Compaction 质量 | C1 Memory Tree | Tree 替代 compaction 的扁平摘要 |

---

## 八、风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| TokenJuice 过度压缩丢失关键信息 | LLM 误解工具结果 | 保留原始结果的 hash，LLM 可请求展开 |
| Model Routing 配置错误 | 意图理解用了慢模型 | fallback 到 default 模型 |
| Tool-Scoped Memory 学到错误规则 | 工具调用失败 | 规则有 confidence 衰减，3 次失败后自动删除 |
| Subconscious 引擎消耗过多 LLM 调用 | 成本增加 | 使用 fast/summary 模型 + 每日 LLM 调用预算 |
| Memory Tree seal 质量差 | 检索不到关键信息 | 保留 L0 原始 chunk，seal 失败时不删除源 |
| Event Bus 引入后调试困难 | 事件丢失难排查 | 事件日志 + 死信队列 |

---

## 九、验收标准

### Phase A 验收
- [ ] web_fetch 返回的 HTML 经 TokenJuice 压缩后 token 减少 ≥40%
- [ ] bash 编译输出经压缩后 token 减少 ≥50%
- [ ] 所有工具结果不超过 `DefaultResultCaps` 定义的上限
- [ ] 意图理解 LLM 调用使用 fast 模型，延迟 <3s
- [ ] SubAgent 使用 reasoning 模型
- [ ] SSH 工具学到的规则在下次连接时自动注入
- [ ] Situation Report 在 system prompt 中可见

### Phase B 验收
- [ ] stable 知识在 proactive recall 中排名高于 volatile
- [ ] SSH 后台任务完成后 IM 通道收到主动通知
- [ ] `~/.maclaw/agents/` 中的 YAML 定义可被 `delegate_task` 调用
- [ ] 日成本超过预算时显示警告
- [ ] 新增功能使用 Event Bus 通信

### Phase C 验收
- [ ] Memory Tree L1 日摘要每天自动生成
- [ ] Subconscious 引擎每 30 分钟执行一次增量反思
- [ ] GitHub connector 每 20 分钟拉取新 release
- [ ] SubAgent 首 token 延迟在 cache hit 时 <1.5s
- [ ] web_fetch 结果中的注入尝试被检测并标记
