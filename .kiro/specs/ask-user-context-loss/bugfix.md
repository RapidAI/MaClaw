# Bugfix Requirements Document

## Introduction

编程工作流的需求确认阶段，LLM 通过 `ask_user` 工具向用户展示需求文档并请求确认（显示"确认需求"/"我要修改需求"按钮）。当用户不点按钮而是直接输入文本（如 `c++ cmake` 表示补充需求），系统丢失了任务上下文，将用户输入当作全新请求处理。

根因链路：
1. Agent loop 中 LLM 生成需求文档后调用 `ask_user` 工具
2. `ask_user` 返回 `__ASK_USER__` 标记，agent loop 检测到后立即 `return resp`
3. **关键缺陷**：`return resp` 前没有调用 `saveConversationHistoryTimed()`，导致本轮累积的 `history`（包含需求文档、工具调用、ask_user 问题）未持久化到 `conversationMemory`
4. 用户发送下一条消息 `"c++ cmake"` 时，`h.memory.load(userID)` 加载的是 ask_user 之前的旧历史，不包含需求文档
5. LLM 看到的上下文中没有需求文档和确认问题，将 `"c++ cmake"` 当作独立请求处理
6. 同时，系统没有"pending ask_user"状态追踪，无法告知 LLM "这是对上一个 ask_user 问题的回答"

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN agent loop 中 LLM 调用 `ask_user` 工具后 agent loop 通过 `return resp` 提前退出 THEN 本轮累积的 conversation history（包含 LLM 输出的需求文档、工具调用记录、ask_user 问题文本）未被持久化到 `conversationMemory`，下一条消息加载的历史缺失这些内容

1.2 WHEN 用户在 ask_user 确认阶段直接输入文本（而非点击按钮）THEN 系统将用户输入当作全新请求处理，丢失了"开发贪吃蛇游戏"的任务上下文，显示"你是想让我帮你处理 C++/CMake 哪一类问题？"

1.3 WHEN ask_user 返回后用户发送下一条消息 THEN 系统没有机制标识"上一条 assistant 消息是 ask_user 问题，当前用户消息是对该问题的回答"，LLM 无法区分"新请求"和"对确认问题的补充回答"

1.4 WHEN 用户点击 ask_user 按钮（如"确认需求"）THEN 按钮文本作为消息发送，恰好匹配 workflow engine 的 `confirmWords`（如果 workflow 活跃）或被 LLM 正确理解为确认（因为文本明确），所以按钮路径偶尔能工作——但这依赖于巧合而非正确的状态管理

### Expected Behavior (Correct)

2.1 WHEN agent loop 因 `ask_user` 提前退出时 THEN 系统 SHALL 在 `return resp` 前调用 `saveConversationHistoryTimed(userID, history, resp)` 将本轮累积的完整 history 持久化，确保下一条消息能加载到包含需求文档和 ask_user 问题的完整上下文

2.2 WHEN ask_user 返回后 THEN 系统 SHALL 在 `conversationMemory` 中标记"pending ask_user"状态（包含 ask_user 的问题文本和选项），使得下一条消息处理时能识别这是对 ask_user 的回答

2.3 WHEN 用户在 ask_user pending 状态下发送任何文本消息 THEN 系统 SHALL 在 system prompt 中注入上下文提示（如"用户正在回答之前的确认问题：[问题内容]。用户的回答是：[用户文本]。请将此视为对当前任务的补充/修改，而非新请求。"），帮助 LLM 正确理解用户意图

2.4 WHEN 用户在 ask_user pending 状态下发送消息 THEN topic switch detector SHALL 跳过检测（即使消息词数 >= 4），避免误判为新话题并清除历史

2.5 WHEN ask_user pending 状态下用户发送消息后 THEN 系统 SHALL 清除 pending 状态（一次性消费），后续消息恢复正常处理流程

### Unchanged Behavior (Regression Prevention)

3.1 WHEN 用户点击 ask_user 按钮 THEN 系统 SHALL CONTINUE TO 正常处理按钮文本（按钮路径不受影响）

3.2 WHEN agent loop 正常结束（非 ask_user 提前退出）THEN 系统 SHALL CONTINUE TO 通过现有路径保存 history（不重复保存）

3.3 WHEN 没有 pending ask_user 状态时 THEN topic switch detector SHALL CONTINUE TO 正常工作

3.4 WHEN ask_user 用于非编程工作流场景（如普通问答确认）THEN 修复 SHALL CONTINUE TO 正确工作（不限于编程工作流）

3.5 WHEN 多个用户同时使用系统 THEN pending ask_user 状态 SHALL 按 userID 隔离，不互相干扰
