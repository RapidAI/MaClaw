# Requirements Document

## Introduction

MaClaw 的 LLM Agent 在处理编程任务时存在一个顽固问题：即使系统 prompt 中包含严格的 HARD GATE 约束（要求先输出需求文档、等用户确认后再编码），LLM 仍然在第一轮输出需求文档的同时，附带调用 `create_session`、`bash`、`write_file` 等编码工具。这是因为 prompt 级约束本质上是"建议"而非"强制"——LLM 可以选择忽略。

本功能在代码层面（`runAgentLoop` 的 agent loop 中）加入硬性拦截机制，作为 prompt 约束的最后防线。当 intent classifier 判定为编程任务（`intentCoding`）时，在第一轮 agent loop（iteration 0）中，如果 LLM 同时输出了文本内容和编码工具调用，拦截器将丢弃编码工具调用，只保留文本回复（需求文档）。后续轮次（用户确认后）不受影响。

当前架构：
- Intent classifier（`gui/im_intent_classifier.go`）已能判定 `intentCoding` / `intentSSH` / `intentNonCoding`
- Agent loop（`gui/im_message_handler.go` 的 `runAgentLoop`）在每轮迭代中处理 LLM 返回的 `choice.Message.ToolCalls`
- System prompt（`gui/im_system_prompt.go`）包含 HARD GATE 文字约束但 LLM 不总是遵守

## Glossary

- **Agent_Loop**: `runAgentLoop` 方法中的主循环，每轮迭代调用 LLM、处理回复、执行工具调用
- **Coding_Tool_Gate**: 本功能新增的代码级拦截器，在 iteration 0 丢弃编码工具调用
- **Intent_Classifier**: `classifyTaskIntent` 和 `classifyTaskIntentForExecution` 函数，判定用户消息的任务类型
- **Coding_Task**: intent classifier 返回 `intentCoding` 的任务
- **Coding_Tool**: 编码相关工具，包括 `create_session`、`bash`、`write_file`、`edit_file`、`craft_tool`、`send_and_observe`、`control_session`
- **Delivery_Tool**: 文档交付工具，包括 `generate_pdf`、`send_file`、`memory`、`open`，不应被拦截
- **Skip_Signal**: 用户消息中包含的跳过确认信号，如"直接做"、"不用问了"、"just do it"等
- **First_Iteration**: agent loop 中 `iteration == 0` 的第一轮迭代
- **Tool_Call_Stripping**: 从 LLM 回复中移除编码工具调用但保留文本内容和非编码工具调用的操作

## Requirements

### Requirement 1: 编码工具拦截核心逻辑

**User Story:** As a developer, I want the agent loop to enforce the coding workflow gate at the code level, so that the LLM cannot bypass the three-phase workflow by simultaneously outputting requirements text and coding tool calls.

#### Acceptance Criteria

1. WHEN Intent_Classifier returns `intentCoding` for the user message AND the current iteration is First_Iteration AND the user message does not contain a Skip_Signal, THE Coding_Tool_Gate SHALL strip all Coding_Tool calls from the LLM response
2. WHEN Coding_Tool_Gate strips tool calls, THE Agent_Loop SHALL preserve the text content of the LLM response and return it to the user as the final reply for that iteration
3. WHEN Coding_Tool_Gate strips tool calls AND the LLM response also contains Delivery_Tool calls (such as `generate_pdf` or `send_file`), THE Coding_Tool_Gate SHALL preserve the Delivery_Tool calls and only strip Coding_Tool calls
4. WHEN the current iteration is not First_Iteration (iteration > 0), THE Coding_Tool_Gate SHALL not intercept any tool calls regardless of intent classification
5. WHEN Intent_Classifier returns a non-coding intent (`intentSSH`, `intentNonCoding`, `intentAmbiguous`, `intentUnknown`), THE Coding_Tool_Gate SHALL not intercept any tool calls regardless of iteration number

### Requirement 2: 编码工具黑名单定义

**User Story:** As a developer, I want a clear and maintainable list of coding tools that should be intercepted, so that the gate can be updated when new tools are added.

#### Acceptance Criteria

1. THE Coding_Tool_Gate SHALL define a blocklist of coding tool names that includes at minimum: `create_session`, `bash`, `write_file`, `edit_file`, `craft_tool`, `send_and_observe`, `control_session`
2. THE Coding_Tool_Gate SHALL define an allowlist of delivery tool names that are never intercepted, including at minimum: `generate_pdf`, `send_file`, `memory`, `open`, `set_nickname`, `manage_config`
3. WHEN a tool call's function name matches the blocklist AND does not match the allowlist, THE Coding_Tool_Gate SHALL classify the tool call as a coding tool subject to stripping
4. WHEN a new tool is added to the system, THE Coding_Tool_Gate SHALL default to allowing the tool (not intercepting) unless explicitly added to the blocklist

### Requirement 3: 跳过信号检测

**User Story:** As an IM_User, I want to bypass the coding workflow gate when I explicitly signal that I want immediate execution, so that experienced users are not slowed down.

#### Acceptance Criteria

1. WHEN the user message contains a Chinese Skip_Signal (including but not limited to: "直接做", "不用问了", "按你的想法来", "直接开始", "不用确认", "马上做", "赶紧做", "跳过文档", "不需要文档"), THE Coding_Tool_Gate SHALL not intercept any tool calls
2. WHEN the user message contains an English Skip_Signal (including but not limited to: "just do it", "skip confirmation", "go ahead", "do it now"), THE Coding_Tool_Gate SHALL not intercept any tool calls
3. THE Coding_Tool_Gate SHALL perform case-insensitive matching for Skip_Signal detection
4. THE Coding_Tool_Gate SHALL detect Skip_Signal as substring matches within the user message, not requiring exact full-message match

### Requirement 4: 拦截后的响应处理

**User Story:** As an IM_User, I want to receive the requirements document text when the gate intercepts coding tools, so that the three-phase workflow proceeds normally.

#### Acceptance Criteria

1. WHEN Coding_Tool_Gate strips all tool calls from the LLM response AND the remaining text content is non-empty, THE Agent_Loop SHALL return the text content as the final response to the user
2. WHEN Coding_Tool_Gate strips some tool calls but Delivery_Tool calls remain, THE Agent_Loop SHALL continue executing the remaining Delivery_Tool calls normally (e.g., generating and sending PDF)
3. WHEN Coding_Tool_Gate strips all tool calls AND the remaining text content is empty, THE Agent_Loop SHALL inject a system message prompting the LLM to generate the requirements document and continue the loop
4. WHEN Coding_Tool_Gate activates, THE Agent_Loop SHALL log the interception event including: iteration number, number of stripped tool calls, names of stripped tools, and whether text content was preserved

### Requirement 5: Intent 分类集成

**User Story:** As a developer, I want the coding workflow gate to integrate with the existing intent classifier, so that the gate decision is based on the same classification logic used elsewhere.

#### Acceptance Criteria

1. THE Coding_Tool_Gate SHALL invoke the intent classifier once at the start of `runAgentLoop` (before the iteration loop begins) and cache the result for the duration of the loop
2. WHEN the intent classifier is invoked, THE Coding_Tool_Gate SHALL use `classifyTaskIntent` (rule-based fast path) for the gate decision, not the LLM-based classifier, to avoid adding latency
3. THE Coding_Tool_Gate SHALL pass the original user text to the intent classifier, not a modified or truncated version
4. IF the intent classifier returns `intentAmbiguous`, THEN THE Coding_Tool_Gate SHALL not activate (conservative approach — only gate on clear `intentCoding`)

### Requirement 6: 可观测性与调试

**User Story:** As a developer, I want visibility into when and why the coding workflow gate activates, so that I can debug issues and tune the gate behavior.

#### Acceptance Criteria

1. WHEN Coding_Tool_Gate activates and strips tool calls, THE Agent_Loop SHALL emit a log message at INFO level containing: the gate decision reason, stripped tool names, and preserved tool names (if any)
2. WHEN Coding_Tool_Gate is evaluated but does not activate (e.g., non-coding intent, skip signal detected, iteration > 0), THE Agent_Loop SHALL emit a log message at DEBUG level explaining why the gate did not activate
3. WHEN the trace service is available, THE Coding_Tool_Gate SHALL append a trace event with type "gate.coding_tool_stripped" including the stripped tool names and the gate activation reason

### Requirement 7: 现有功能不受影响

**User Story:** As an IM_User, I want all existing non-coding workflows to continue working exactly as before, so that the gate does not introduce regressions.

#### Acceptance Criteria

1. WHEN IM_User sends a non-coding request (e.g., "帮我截个屏", "查看会话列表", "搜索一下XX"), THE Agent_Loop SHALL execute all tool calls without any interception
2. WHEN IM_User sends a coding request with a Skip_Signal, THE Agent_Loop SHALL execute all tool calls without any interception, identical to pre-gate behavior
3. WHEN IM_User confirms the requirements document and the agent loop enters iteration > 0, THE Agent_Loop SHALL execute all tool calls without any interception
4. THE Coding_Tool_Gate SHALL not modify the system prompt, tool definitions, or any other aspect of the agent loop beyond the tool call stripping in First_Iteration
5. THE Coding_Tool_Gate SHALL not affect background loops (`LoopKindBackground`), only foreground IM/desktop loops
