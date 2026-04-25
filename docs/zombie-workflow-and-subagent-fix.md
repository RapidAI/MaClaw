# 僵尸工作流 + SubAgent 激活失败——机制性修复

## 根因

整个工作流引擎建立在**单工作流独占模型**上：一个用户同一时间只有一个工作流，所有消息都属于这个工作流。`QuickFilter.Classify` 在检测到活跃工作流时无条件返回 `FilterActiveWorkflow`，所有消息被路由到 `handleActiveWorkflow`。用户无法启动新工作流，也无法隐式放弃当前工作流。

当用户在 PPT 工作流期间发送编码任务时，消息被 PPT 工作流拦截 → `handlePendingConfirm` 分类为 `"other"` → fall through → Coding Tool Gate 正确执行三阶段 → 但 SubAgent 无法激活（活跃工作流是 PPT 类型，Path 1 硬编码检查 `PhaseCodingImplementation`）。

## 四个修复

### 修复 1（核心）：`handleActiveWorkflow` 用 UIC 做跨类型检测

**位置**：`gui/im_message_handler_workflow.go` — `handleActiveWorkflow` 入口处

在调用 `engine.HandleInput` **之前**，用 UIC 检查消息是否是一个不同类型的工作流任务。这个位置是唯一正确的拦截点——`HandleInput` 有多个分支（PendingConfirm、WaitingForInput、default），跨类型检测必须在所有分支之前生效。

当 UIC 返回的 `WorkflowType` 与活跃工作流类型不同且置信度 ≥ 0.70 时，取消当前工作流并调用 `handleWorkflowInterception` 重新路由。

### 修复 2（核心）：`advancePhase` 声明式激活 orchestrator

**位置**：`corelib/workflow/engine.go` — `advancePhase`

`WorkflowResponse` 新增 `ActivateOrchestrator`/`TaskBreakdownText`/`RequirementsContext`/`DesignContext` 字段。

`advancePhase` 在推进到执行阶段（`ToolFilterFull && !NeedsConfirm`）时设置信号。context 从模板的阶段顺序结构化提取——第一个阶段的产出物是 requirements context，其余是 design context——不硬编码阶段 ID。

`im_message_handler.go` 中删除了 Path 1 的硬编码 `PhaseCodingImplementation` 检查。

### 修复 3（纵深防御）：`handlePendingConfirm` 新增 `cancel` 类别

LLM 分类 prompt 新增 `"cancel"` 类别，处理显式取消意图。

### 修复 4（纵深防御）：`RestoreFromStore` 新增 24 小时过期检查

应用启动时，超过 24 小时未更新的活跃工作流被自动取消。

## 修改文件

| 文件 | 修改 |
|------|------|
| `corelib/workflow/types.go` | `WorkflowResponse` 新增 `ActivateOrchestrator`/`TaskBreakdownText`/`RequirementsContext`/`DesignContext` 字段 |
| `corelib/workflow/engine.go` | `advancePhase` 声明执行阶段（结构化 context 提取）；`RestoreFromStore` 24h 过期；新增 `workflowStaleTimeout` 常量 |
| `gui/im_message_handler_workflow.go` | `handleActiveWorkflow` 入口处 UIC 跨类型检测；`handlePendingConfirm` cancel 类别 |
| `gui/im_message_handler.go` | 删除 Path 1 硬编码 `PhaseCodingImplementation` 检查 |

## Review 修正记录

1. **跨类型检测位置**：从 `handlePendingConfirm` 移到 `handleActiveWorkflow` 入口。原位置只在 `NeedsConfirm && hasOutput` 时触发，无产出物的阶段不经过 `PendingConfirm`，跨类型检测不生效。
2. **context 提取**：从硬编码阶段 ID 列表改为按模板阶段顺序结构化提取。删除 `collectPhaseContext` 函数。
3. **关键词匹配**：从 `HandleInput`（corelib 层）中删除了 `MatchTemplateByKeywords` 跨类型检测——关键词匹配有 false positive 问题（如 "DD" 匹配 "add"），且 corelib 层不应该做这种决策。
4. **`ActivateOrchestrator` 语义澄清**：信号含义从"激活 orchestrator"改为"进入了执行阶段"。调用方尝试解析 `TaskBreakdownText` 为任务列表，成功才激活 orchestrator，失败则 fall through 到正常 agent loop。PPT 工作流的 `slide_scripting` 产出物不是任务列表，不会误激活 orchestrator。
5. **context 去重**：`TaskBreakdownText`（前一个阶段的产出物）从 `designParts` 中排除，避免同一内容在 `TaskBreakdownText` 和 `DesignContext` 中重复。

## 测试结果

- `corelib/workflow/` 全部通过
- `gui/` CodingGate/RouteTools/SubAgent 测试全部通过
- 4 个 pre-existing 失败与本次修改无关
