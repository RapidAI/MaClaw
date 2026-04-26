# 工作流阶段上下文隔离——从原理上解决 PPT 任务漂移

## 问题复现

用户在桌面 AI 助手面板中：
1. 先执行 SSH 任务（查看驱网服务器状态）
2. 发送"根据 readme.md 做一份面向普通用户的产品宣传 PPT"
3. IUM（意图理解管理器）启动多轮澄清，返回确认问题
4. 用户回复"没有其它信息了"
5. IUM 完成 → StartWorkflow(presentation_design) → 进入 audience_goal 阶段
6. Agent loop 运行，但 LLM 忽略阶段指令，回复了关于驱网服务器的内容
7. 用户说"开工"→ 推进到 content_outline → LLM 回复"开工做什么呢伯伯？"

**用户观察**：maclaw 能记得很早之前的 iWorker PPT 项目，但记不住刚才的 PPT 任务。

## 根因分析

### 真正的根因：工作流首阶段 agent loop 的 userText 是 IUM 完成消息，不是原始任务请求

**数据流追踪**（从日志和 trajectory 确认）：

```
17:11:14  Call 1: msg="根据 readme.md 做 PPT"
          → UIC: non_coding → IUM.Start() 创建多轮理解会话
          → 返回 IUM 的澄清问题给用户
          → handleIMMessageWithLoop 返回（无 agent loop）

17:11:55  Call 2: msg="没有其它信息了"
          → filter.Classify → FilterActiveUnderstanding
          → handleActiveUnderstanding → IUM.HandleInput("没有其它信息了")
          → IUM: ready=true, intent={category: presentation_design}
          → StartWorkflow → handlePostStartWorkflow → handleActiveWorkflow
          → engine.HandleInput → RunAgentLoop=true + PhasePrompt
          → 设置 markers → return nil → fall through to agent loop

17:11:57  Agent loop starts: task="没有其它信息了"  ← 这是问题所在
          → LLM 收到:
            - system prompt: 阶段指令（"生成受众与目标定义"）
            - 用户意图摘要（来自 WorkflowState.Intent）
            - conversation history: SSH 任务上下文
            - userText: "没有其它信息了"  ← 无任务语义
          → LLM 被 SSH 上下文 + 无语义的 userText 带偏
          → 输出关于驱网服务器的内容
```

**原理分析**：

当工作流通过 IUM 多轮澄清启动时，触发 `StartWorkflow` 的消息是 IUM 的**完成消息**（如"没有其它信息了"、"好的"、"就这些"），不是用户的**原始任务请求**（如"根据 readme.md 做 PPT"）。

`handlePostStartWorkflow` 调用 `handleActiveWorkflow(engine, userID, text)`，其中 `text` 是 Call 2 的 `msg.Text = "没有其它信息了"`。`handleActiveWorkflow` 返回 nil，agent loop 以 `msg.Text = "没有其它信息了"` 作为 `userText` 运行。

原始任务请求被保存在 `WorkflowState.Intent`（`Goals: ["根据 readme.md 做 PPT"]`），通过 `BuildPhaseSystemPrompt` 注入为"用户意图摘要"。但这只是 system prompt 末尾的一小段文字，信号强度远低于：
- conversation history 中的 SSH 内容（多条 tool_call/tool_result）
- userText "没有其它信息了"（LLM 将其理解为"关于驱网服务器没有其他信息了"）

**这不是 prompt 措辞问题，不是信噪比问题，不是 lost-in-the-middle 问题。这是 userText 传错了。**

### 为什么旧项目能记住、新任务记不住

- **旧项目（iWorker PPT）**：已完成并沉淀到长期记忆（`task_artifact`），通过 `RecallDynamic` 注入 system prompt。
- **新任务（码卡龙 PPT）**：原始请求在 Call 1 中被 IUM 消费，Call 2 的 `msg.Text` 是"没有其它信息了"。`WorkflowState.Intent.Goals` 中有原始请求，但只作为 system prompt 中的摘要注入，被 SSH 上下文淹没。

## 修复方案

### 原理性修复：agent loop 的 userText 应该是原始任务请求，不是 IUM 完成消息

当工作流通过 IUM 启动时，`handlePostStartWorkflow` 传给 `handleActiveWorkflow` 的 `text` 应该是 `WorkflowState.Intent` 中的原始请求，而不是当前消息的文本。

**修改位置**：`gui/im_message_handler_workflow.go` 的 `handlePostStartWorkflow`

**修改内容**：当 `handlePostStartWorkflow` 调用 `handleActiveWorkflow` 时，用 `state.Intent.Goals[0]`（原始任务请求）替代 `text`（IUM 完成消息）。这样 agent loop 的 `userText` 就是"根据 readme.md 做 PPT"而不是"没有其它信息了"。

同时，`handleIMMessageWithLoop` 中 `msg.Text` 仍然是"没有其它信息了"，但 `workflowAgentLoopMarker` 被设置后，agent loop 应该使用工作流引擎提供的原始请求作为 userText，而不是当前消息。

### 补充修复：SavePhaseOutput 最低质量门禁 + advanceAndRespond 产出物验证

这两个修复作为纵深防御保留——即使 userText 修复后 LLM 仍可能偶尔产出无效内容（模型能力限制），质量门禁和产出物验证能防止垃圾数据污染后续阶段。

## 修改文件

| 文件 | 说明 |
|------|------|
| `gui/im_message_handler_workflow.go` | `extractOriginalRequest()`: 从 Intent 中提取原始请求（Goals[0] → Summary fallback） |
| `gui/im_message_handler_workflow.go` | `handlePostStartWorkflow`: 工作流启动时 stash 原始请求 |
| `gui/im_message_handler_workflow.go` | `advanceAndRespond`: 验证产出物 + 重触发时 re-stash 原始请求 |
| `gui/im_message_handler.go` | `workflowOriginalRequest` sync.Map 声明 + agent loop 前 userText 替换 + 非工作流路径清理 |
| `gui/im_session_state.go` | `clearPerUserSessionState` 清理 `workflowOriginalRequest` |
| `corelib/workflow/engine.go` | `SavePhaseOutput` + `passesMinimumQualityGate()` |
