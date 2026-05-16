# 意图理解与工作流引擎 — 技术设计文档

## 1. 架构概览

### 1.1 系统分层

```
┌─────────────────────────────────────────────────┐
│                  core.go HandleMessage           │  命令层（/workflow 等）
├─────────────────────────────────────────────────┤
│                  QuickFilter                     │  快速分流层（纯规则，<5ms）
├─────────────────────────────────────────────────┤
│          IntentUnderstanding (多轮 LLM)          │  意图理解层
├─────────────────────────────────────────────────┤
│          WorkflowEngine (阶段执行)               │  工作流执行层
├─────────────────────────────────────────────────┤
│          WorkflowRegistry (模板注册)             │  模板管理层
├─────────────────────────────────────────────────┤
│          SQLite (持久化)                         │  存储层
└─────────────────────────────────────────────────┘
```

### 1.2 消息处理流程

```
用户消息 → core.go HandleMessage
  ├─ /command → 现有命令处理（不变）
  ├─ /workflow → 新增工作流命令
  └─ 非命令 → coordinator.Coordinate
       ├─ SpaceWorkflow → workflowEngine.HandleWorkflowInput
       ├─ SpacePrivate → 现有私聊处理（不变）
       ├─ SpaceMeeting → 现有会议处理（不变）
       └─ SpaceLobby → QuickFilter.Filter
            ├─ FilterCommand → 不可达（已在 core.go 处理）
            ├─ FilterActiveWorkflow → workflowEngine.HandleWorkflowInput
            ├─ FilterActiveUnderstanding → understandingMgr.HandleInput
            ├─ FilterSmallTalk → hubDirectAnswer
            ├─ FilterSimpleDirective → 现有路由逻辑
            └─ FilterNeedsUnderstanding → understandingMgr.StartSession
                 └─ ready=true → workflowEngine.StartWorkflow
```

## 2. 核心数据结构

### 2.1 工作流类型与模板

```go
// workflow_types.go
type WorkflowType string
const (
    WorkflowCoding        WorkflowType = "coding"
    WorkflowProductDesign WorkflowType = "product_design"
    WorkflowInnovation    WorkflowType = "innovation"
    WorkflowBusinessPlan  WorkflowType = "business_plan"
)

type PhaseTemplate struct {
    ID          string
    Name        string
    Description string
    Prompt      string
    Deliverable string
    Checklist   []string
    NeedsConfirm bool
    NeedsDevice  bool
    CanSkip      bool
}

type WorkflowTemplate struct {
    Type        WorkflowType
    Name        string
    Description string
    Keywords    []string
    Phases      []PhaseTemplate
}
```

### 2.2 结构化意图

```go
type StructuredIntent struct {
    Category      WorkflowType `json:"category"`
    Summary       string       `json:"summary"`
    Goals         []string     `json:"goals"`
    Constraints   []string     `json:"constraints"`
    OpenQuestions []string     `json:"open_questions"`
    Confidence    float64      `json:"confidence"`
    Ready         bool         `json:"ready"`
}
```

### 2.3 会话与状态

```go
type UnderstandingRound struct {
    UserText      string    `json:"user_text"`
    AssistantText string    `json:"assistant_text"`
    Timestamp     time.Time `json:"timestamp"`
}

type UnderstandingState string
const (
    UnderstandingActive    UnderstandingState = "active"
    UnderstandingConfirmed UnderstandingState = "confirmed"
    UnderstandingCancelled UnderstandingState = "cancelled"
)

type UnderstandingSession struct {
    ID        string
    UserID    string
    Intent    StructuredIntent
    Rounds    []UnderstandingRound
    State     UnderstandingState
    CreatedAt time.Time
    UpdatedAt time.Time
}

type WorkflowState struct {
    ID           string
    UserID       string
    Type         WorkflowType
    TemplateRef  WorkflowType
    Intent       StructuredIntent
    CurrentPhase string
    PhaseOutputs map[string]string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type WorkflowResponse struct {
    Text        string
    Advance     bool
    Complete    bool
    RouteAction *RouteAction
}
```

## 3. 模块设计

### 3.1 WorkflowRegistry

- 文件：`hub/internal/im/workflow_registry.go`
- 线程安全的模板注册表，`sync.RWMutex` 保护
- `NewWorkflowRegistry()` 自动注册 4 种内置模板
- `Match(wt)` 按 WorkflowType 精确匹配
- `AllDescriptions()` 返回所有模板摘要供 LLM prompt 使用

### 3.2 内置模板

- 文件：`hub/internal/im/workflow_templates.go`
- 4 个 `builtin*Template()` 函数，每个返回 `*WorkflowTemplate`
- 每个阶段的 Prompt 字段包含 LLM 指令，指导生成该阶段产出物
- Checklist 字段包含质量检查项

### 3.3 QuickFilter

- 文件：`hub/internal/im/quick_filter.go`
- 纯规则判断，无 I/O，延迟 < 5ms
- `isSmallTalk`：短消息 + 问候词匹配
- `isSimpleDirective`：翻译/格式化/总结等无需多阶段的指令模式
- 依赖 WorkflowEngine 引用检查活跃会话

### 3.4 IntentUnderstanding

- 文件：`hub/internal/im/intent_understanding.go`
- `UnderstandingManager` 持有内存缓存 + SQLite 持久化
- LLM 调用复用 `agent.DoSimpleLLMRequest`，超时 10s
- 每轮输出 JSON：`{intent: StructuredIntent, reply: string, ready: bool}`
- `ready=true` 时返回特殊标记，由 WorkflowEngine 接管

### 3.5 WorkflowEngine

- 文件：`hub/internal/im/workflow_engine.go`
- 核心引擎，协调模板、意图理解、阶段执行
- `executePhase`：构建 prompt → LLM 生成 → checklist 自检 → 格式化输出
- `advancePhase`：推进到下一阶段或标记完成
- `modifyCurrentPhase`：用修改请求重新生成当前阶段产出
- 设备路由阶段复用 Coordinator 的路由方法

### 3.6 SQLite 持久化

- 文件：`hub/internal/store/sqlite/workflow_repo.go`
- `understanding_sessions` 表：intent_json + rounds_json 存储 JSON
- `workflow_states` 表：intent_json + phase_outputs_json 存储 JSON
- `CleanupExpired`：清理已完成/已取消且超过 7 天的记录

### 3.7 SpaceState 扩展

- 新增 `SpaceWorkflow` 常量
- 新增 `EnterWorkflow` / `ExitWorkflow` 方法
- Coordinator 的 `Coordinate` 新增 `case SpaceWorkflow` 分支

## 4. 集成设计

### 4.1 Coordinator 集成

`NewCoordinator` 新增 `workflowEngine *WorkflowEngine` 参数。在 `handleLobbyMessage` 中插入 QuickFilter 调用，在 `Coordinate` 中新增 SpaceWorkflow 分支。

### 4.2 Bootstrap 集成

在 `bootstrap.go` 的 `Bootstrap` 函数中：
1. 创建 `WorkflowRegistry`（自动注册内置模板）
2. 创建 `WorkflowEngine`，注入依赖
3. 将 `WorkflowEngine` 传入 `NewCoordinator`
4. 启动后台 goroutine 清理过期会话

### 4.3 数据库迁移

在 SQLite 初始化流程中添加 `CREATE TABLE IF NOT EXISTS` 语句，与现有迁移机制兼容。

## 5. 错误处理与降级

- LLM 未配置 → 跳过意图理解，直接透传设备
- LLM 调用超时 → 意图理解降级为 passthrough
- CircuitBreaker 熔断 → 同上
- 工作流阶段 LLM 失败 → 保留当前状态，提示用户重试

## 6. 正确性属性

### P1: QuickFilter 确定性
- 相同输入始终产生相同的 FilterResult
- 分流判断不依赖外部 I/O

### P2: 工作流状态一致性
- 一个用户同一时间最多一个活跃工作流
- 一个用户同一时间最多一个活跃意图理解会话
- 工作流完成/取消后 SpaceState 回到 lobby

### P3: 模板注册幂等性
- 重复注册同类型模板覆盖旧模板
- 注册/匹配操作线程安全

### P4: 持久化完整性
- Hub 重启后活跃工作流可恢复
- 过期记录定期清理
