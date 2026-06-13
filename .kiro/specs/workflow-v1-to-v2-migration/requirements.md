# 工作流引擎 V1→V2 统一迁移

## 背景

当前存在两套并行的工作流引擎：
- **V1**（`corelib/workflow/engine_stub.go`）：名为 "stub" 但实际是运行时引擎，GUI 30+ 处调用它做状态管理、工具策略、阶段推进
- **V2**（`corelib/workflow/v2/`）：负责路由决策（BM25 模板匹配）、confirm classification、状态持久化（SQLite）

这造成：
1. 模板需要在两处重复注册（V1 `RegisterBuiltinTemplates` + V2 `RegisterBuiltinTemplates`）
2. 数据结构重复定义（V1 `WorkflowTemplate`/`PhaseTemplate` vs V2 同名类型，字段不同）
3. "engine_stub" 命名误导，让人以为它是可删除的 stub
4. 新增模板需要同时修改两处代码

## 目标

将 V1 engine 的**运行时功能**迁移到 V2 `StateMachine`，让 V2 成为唯一的工作流引擎。GUI 所有调用点统一使用 V2 接口。最终删除 `engine_stub.go`。

## 需求

### R1: V2 StateMachine 补全运行时能力

V2 需要新增或增强以下能力（对标 V1 当前 GUI 消费的接口）：

| V1 接口 | 用途 | V2 现状 |
|---------|------|---------|
| `StartWorkflowWithOptions(userID, intent, options)` | 创建工作流实例 | V2 有 `StartWorkflow` 但参数不同 |
| `HasActiveWorkflow(userID)` | 检查是否有活跃工作流 | V2 有 `GetActive` |
| `GetActiveWorkflow(userID)` | 获取活跃工作流状态 | V2 有 `GetActive` |
| `GetActivePhaseToolFilter(userID)` | 获取当前阶段工具策略 | V2 无 |
| `GetPhaseToolFilter(userID)` | 获取阶段工具策略（含 fallback） | V2 无 |
| `IsPhaseExecutionBlocked(userID)` | 检查阶段执行是否被阻塞 | V2 无 |
| `SavePhaseOutputAndMaybeAdvance(userID, doc)` | 保存阶段产出物并可能推进 | V2 无 |
| `GetOpsApprovedCommands(userID)` | 获取运维白名单命令 | V2 无 |
| `GetUnderstanding()` | 获取 IUM 实例 | V2 无（IUM 独立） |
| `CancelWorkflow(userID)` | 取消工作流 | V2 无 |

### R2: GUI 调用点迁移

将 GUI 中所有 `h.app.workflowEngine.*` 调用迁移到 V2 接口。预计 30+ 处，分布在：
- `im_message_handler_workflow.go`
- `im_agent_loop_tools.go`
- `im_post_loop.go`
- `im_tool_execution.go`
- `workflow_coding_main_loop_policy.go`

### R3: 模板注册统一

删除 V1 的 `RegisterBuiltinTemplates`，所有模板只在 V2 `templates.go` 中注册。V2 的 `WorkflowTemplate` 结构体需要兼容 V1 的 `Prompt`/`Kind`/`MutationScope`/`InputSchema`/`DisableOrchestrator` 等字段。

### R4: 数据结构统一

V1 和 V2 各自定义了 `WorkflowTemplate`、`PhaseTemplate`、`WorkflowState` 等类型。统一为一套类型，放在 `corelib/workflow/v2/` 中。`corelib/workflow/types.go` 保留常量定义和接口。

### R5: IUM 集成

IUM（`IntentUnderstandingManager`）目前通过 V1 engine 的 `GetUnderstanding()` 暴露。迁移后 IUM 应该作为独立模块存在，不依赖 engine 实例。

### R6: 测试迁移

现有 50+ 个 GUI 测试通过 `setupWorkflowTestHandler(&mockLLMCallerGUI{})` 创建 V1 engine。需要切换到 V2 测试基础设施。

## 约束

- 渐进式迁移（可分多个 PR）
- 每步完成后所有现有测试通过
- 不改变用户可见行为
- 兼容已持久化的工作流状态（SQLite 中的数据）

## 风险

- GUI 调用点分散，容易遗漏
- V1 和 V2 的 `WorkflowState` 字段不完全一致
- 测试基础设施耦合 V1 接口

## 优先级

P2（技术债务清理，不影响用户功能）
