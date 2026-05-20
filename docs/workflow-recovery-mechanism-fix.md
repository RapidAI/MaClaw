# 工作流恢复机制性修复——从记忆恢复的编码项目自动激活 SubAgent

## 问题本质

工作流的三阶段产出物（需求/设计/任务分解）和 Orchestrator 状态是 **ephemeral** 的：

1. **WorkflowState** 已持久化到 SQLite（`PersistenceStore`），包含 `PhaseOutputs` map（完整的阶段文档）
2. **TaskExecutionOrchestrator** 是纯内存的（`sync.Map` 中的 `*TaskExecutionOrchestrator`），进程重启后丢失
3. **WorkflowEngine.RestoreFromStore()** 在启动时恢复 `WorkflowState`，但不恢复 Orchestrator

当工作流已经完成三阶段（`PhaseOutputs` 中有 `task_breakdown` 文档），进入 `implementation` 阶段后进程重启：
- `WorkflowState` 被正确恢复（`CurrentPhase="implementation"`，`PhaseOutputs` 含完整文档）
- `TaskExecutionOrchestrator` 丢失（`ShouldUseSubAgent` 返回 false）
- 用户说"继续" → `handleActiveWorkflow` → `HandleInput` → 但 `ActivateOrchestrator` 只在 **阶段转换时** 设置（`advancePhase` 路径），不在 **恢复已有阶段时** 设置

## 根因

`ActivateOrchestrator=true` 只在 `advancePhase()` 的返回路径中设置——即工作流从 `task_breakdown` **转换到** `implementation` 的那一刻。这是一次性事件。进程重启后，工作流已经在 `implementation` 阶段，不会再次触发 `advancePhase`，所以 `ActivateOrchestrator` 永远不会被设置。

`HandleInput` 对已在 `implementation` 阶段的工作流走 default 分支：`RunAgentLoop=true` + `PhasePrompt`，但 `ActivateOrchestrator=false`。

## 机制性修复

### 核心原则

**Orchestrator 的激活不是一次性事件，而是一个可恢复的状态。** 当 WorkflowState 表明"当前在执行阶段且有 task_breakdown 产出物"时，系统应该能在任何时刻重建 Orchestrator。

### 修改 1：HandleInput default 分支对执行阶段设置 ActivateOrchestrator

**位置**：`corelib/workflow/engine.go`，`HandleInput` 的 default 分支

当前阶段满足执行条件（`ToolFilterFull && !NeedsConfirm && !DisableOrchestrator`）时，设置 `ActivateOrchestrator=true` + 上下文，与 `advancePhase` 路径完全一致。

`handleActiveWorkflow` 中已有 `!taskOrch.IsActive()` 守卫，防止重复激活——已激活的 orchestrator 不会被重新激活。

### 修改 2：提取 `buildOrchestratorActivationContext` 共享函数

将 `advancePhase` 和 default 分支中重复的上下文构建逻辑提取为共享函数，消除代码重复。

## 完整恢复链路（修复后）

```
应用重启
  → WorkflowEngine.RestoreFromStore()
  → WorkflowState 恢复: Type=coding, CurrentPhase=implementation, PhaseOutputs 含 task_breakdown

用户: "继续"
  → handleWorkflowInterception
  → engine.HasActiveWorkflow(userID) = true（短消息 < 10 rune 仍路由到活跃工作流）
  → handleActiveWorkflow(engine, userID, "继续")
  → engine.HandleInput(userID, "继续")
  → default 分支: currentPhase=implementation, ToolFilterFull, !NeedsConfirm
  → ActivateOrchestrator=true, TaskBreakdownText=PhaseOutputs["task_breakdown"]
  → handleActiveWorkflow 消费 resp.ActivateOrchestrator
  → ParseTaskListFromText(resp.TaskBreakdownText) → tasks
  → taskOrch.Activate(tasks, reqCtx, designCtx, projectPath, "")
  → workflowAgentLoopMarker=true
  → return nil → agent loop 启动
  → routeSubAgentExecution: ShouldUseSubAgent(taskOrch) = true
  → SubAgent 逐任务执行
```

## 修改文件

- `corelib/workflow/engine.go`：
  - `HandleInput` default 分支新增执行阶段检测 + `ActivateOrchestrator` 设置
  - `advancePhase` 中的上下文构建逻辑提取为 `buildOrchestratorActivationContext()` 共享函数
  - 两处调用点（default 分支 + advancePhase）统一使用共享函数

## 验收标准

- 工作流在 implementation 阶段恢复后，用户说"继续" → orchestrator 被激活 → SubAgent 执行
- 工作流首次进入 implementation 阶段（advancePhase）→ 行为不变
- orchestrator 已激活时收到消息 → `!taskOrch.IsActive()` 守卫阻止重复激活
- `TaskBreakdownText` 为空时（不应发生）→ `len(tasks) > 0` 守卫 fall through 到 normal agent loop
- 所有 corelib/workflow 测试通过
- 所有 gui 测试通过（CodingGate / Orchestrator / SubAgent / Workflow）
- GUI 编译通过
