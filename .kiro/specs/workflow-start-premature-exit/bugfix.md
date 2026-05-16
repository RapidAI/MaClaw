# Bug: 编码任务执行阶段 agent loop 提前退出

## 症状

用户确认需求文档后说"开工"，LLM 开始写代码但中途停下来，需要用户再次输入才继续。

## 根因（两层）

### 根因 1: confirmWords 关键词列表无法覆盖所有确认表达

`HandleInput` 用 `confirmWords` 短语列表匹配用户确认。"开工"不在列表中，工作流卡在 requirements 阶段不推进。

### 根因 2: needsConfirmFromSteering 不感知当前阶段

`needsConfirmFromSteering = gateConfig.active && iteration > 0` 在 implementation 阶段（NeedsConfirm=false）仍然触发 force-return。

## 修复方案：Kiro 式 LLM 委托确认

### 核心思路

去掉引擎层的 confirm/modify 关键词匹配，把意图判断交给 LLM。

### HandleInput 改动（`corelib/workflow/engine.go`）

当 NeedsConfirm 阶段已有 output 时：
1. 保留 `confirmWords` 作为快速路径（显式确认词仍然零延迟推进）
2. 不再做 modify 关键词匹配或排除法
3. 返回 `PendingConfirm=true` + `RunAgentLoop=true` + 包含用户消息的 PhasePrompt
4. PhasePrompt 告诉 LLM 判断用户意图（确认/修改/无关）

### 后处理自动推进（`gui/im_message_handler.go`）

agent loop 结束后检查 LLM 输出：
- LLM 输出了新的阶段文档（`SavePhaseOutput` 捕获到）→ 修改，等待再次确认
- LLM 没有输出新文档（短回复如"好的，进入下一阶段"）→ 确认，自动调用 `AdvancePhase()`

### needsConfirmFromSteering 阶段感知

有 WorkflowEngine 活跃工作流时，委托给 `IsPhaseNeedsConfirm()` 判断。

## 修改文件

- `corelib/workflow/types.go`：WorkflowResponse 新增 `PendingConfirm` 字段
- `corelib/workflow/engine.go`：HandleInput 新增 LLM 委托路径 + 公开 `AdvancePhase()` 方法
- `gui/im_message_handler.go`：新增 `pendingWorkflowConfirm` 字段 + 后处理自动推进逻辑 + needsConfirmFromSteering 阶段感知
- `gui/im_message_handler_workflow.go`：handleActiveWorkflow 处理 PendingConfirm 标记

## 交互流程

```
用户: "开工"
  → HandleInput: NeedsConfirm=true, hasOutput=true, 不匹配 confirmWords
  → 返回 PendingConfirm=true + PhasePrompt(含用户消息)
  → agent loop: LLM 看到 "用户回复了：开工"
  → LLM 输出: "好的，进入下一阶段" (短文本，不是阶段文档)
  → 后处理: docCaptured=false → AdvancePhase() → 推进到技术设计

用户: "把数据库换成 PostgreSQL"
  → HandleInput: 同上，PendingConfirm=true
  → agent loop: LLM 看到修改请求
  → LLM 输出: 更新后的需求文档 (长文本)
  → 后处理: docCaptured=true → 不推进，等待再次确认
```

## 测试

- `go test ./corelib/workflow/` — 全部通过
- `go test ./gui/ -run "CodingGate|NeedsConfirm|WorkflowGate|AgentLoop"` — 全部通过
