# Requirements Document

## Introduction

本功能为 MaClaw 的编程任务处理流程增加两个交互环节：

1. **需求交互确认**：当用户通过 IM 提出编程需求时，MaClaw Agent 不再直接调用 `create_session` 启动远程编程工具，而是先与用户交互确认需求理解、实现方案和边界情况，只有用户明确同意后才开始执行。
2. **任务完成后 Review/Fix/Optimize 询问**：编程任务完成并通过测试后，MaClaw Agent 询问用户是否需要进行代码质量提升（Review/Fix/Optimize），用户可选择执行或跳过以节省 tokens。

当前架构中，用户 IM 消息经 Hub IM Adapter (`hub/internal/im/core.go`) → MessageRouter (`hub/internal/im/router.go`) → WebSocket → IMMessageHandler (`im_message_handler.go`) → `runAgentLoop` 处理。Agent 的行为由 `buildSystemPrompt()` 中的系统提示词和注册的工具集（`tool_registry_builtin.go`）驱动。本功能主要通过修改系统提示词中的编程任务工作流指令来实现行为变更，并可选地增加工具支持。

## Glossary

- **MaClaw_Agent**: IMMessageHandler 中的 LLM Agent 循环（`runAgentLoop`），负责理解用户意图、调用工具、生成回复
- **System_Prompt**: `buildSystemPrompt()` 生成的系统提示词，指导 Agent 的行为模式和工作流程
- **Remote_Coding_Tool**: 通过 `create_session` 启动的远程编程工具（Claude Code、Gemini CLI、Codex 等），由 RemoteSessionManager 管理
- **IM_User**: 通过飞书、QQ 等 IM 平台向 MaClaw 发送消息的用户
- **Coding_Task**: 需要调用 Remote_Coding_Tool 执行的编程类需求（如写代码、重构、修 bug 等）
- **Confirmation_Phase**: Agent 在调用 Remote_Coding_Tool 之前与用户进行的需求确认交互阶段
- **RFO_Phase**: Review/Fix/Optimize 阶段，编程任务完成后询问用户是否需要代码质量提升
- **Skip_Signal**: 用户发出的跳过确认的信号，如"直接做"、"不用问了"、"按你的想法来"等

## Requirements

### Requirement 1: 编程需求识别与确认触发

**User Story:** As an IM_User, I want MaClaw_Agent to confirm my coding requirements before executing, so that I can verify the understanding is correct and avoid wasted effort on misunderstood tasks.

#### Acceptance Criteria

1. WHEN IM_User sends a message that constitutes a Coding_Task, THE MaClaw_Agent SHALL enter Confirmation_Phase before calling `create_session` or any Remote_Coding_Tool
2. WHILE in Confirmation_Phase, THE MaClaw_Agent SHALL present a structured confirmation message containing: (a) 需求复述 — Agent 对需求的理解, (b) 实现方案 — 涉及的文件和大致思路, (c) 边界情况 — 需要用户决定的设计决策点（如有）
3. WHILE in Confirmation_Phase, THE MaClaw_Agent SHALL wait for IM_User's explicit approval before proceeding to call `create_session`
4. IF IM_User provides corrections during Confirmation_Phase, THEN THE MaClaw_Agent SHALL update the understanding and present a revised confirmation
5. THE MaClaw_Agent SHALL distinguish Coding_Task from non-coding requests (simple questions, configuration, file operations) and only trigger Confirmation_Phase for Coding_Task

### Requirement 2: 跳过确认的快捷机制

**User Story:** As an IM_User, I want to skip the confirmation phase when I'm confident about my request, so that I can get faster execution without unnecessary back-and-forth.

#### Acceptance Criteria

1. WHEN IM_User includes a Skip_Signal in the coding request (such as "直接做", "不用问了", "按你的想法来", "just do it"), THE MaClaw_Agent SHALL skip Confirmation_Phase and proceed directly to execution
2. WHEN IM_User responds to a Confirmation_Phase message with a Skip_Signal, THE MaClaw_Agent SHALL immediately proceed to execution using the current understanding
3. THE MaClaw_Agent SHALL recognize Skip_Signal in both Chinese and English expressions

### Requirement 3: 确认阶段的对话记忆集成

**User Story:** As an IM_User, I want the confirmation context to be preserved in conversation memory, so that the Agent can reference it when sending instructions to the coding tool.

#### Acceptance Criteria

1. THE MaClaw_Agent SHALL store the confirmed requirements understanding in the conversation memory (`conversationMemory`)
2. WHEN sending instructions to Remote_Coding_Tool via `send_and_observe`, THE MaClaw_Agent SHALL incorporate the confirmed understanding and implementation plan into the coding instruction
3. IF IM_User provided corrections during Confirmation_Phase, THEN THE MaClaw_Agent SHALL use the corrected version (not the original) when sending instructions to Remote_Coding_Tool

### Requirement 4: 任务完成后 Review/Fix/Optimize 询问

**User Story:** As an IM_User, I want MaClaw_Agent to ask me whether to review/fix/optimize the code after a coding task completes, so that I can choose to improve code quality or skip to save tokens.

#### Acceptance Criteria

1. WHEN a Coding_Task completes and the Remote_Coding_Tool session reaches `waiting_input` or `exited` status with exit code 0, THE MaClaw_Agent SHALL ask IM_User whether to perform RFO_Phase
2. THE MaClaw_Agent SHALL present three options in the RFO inquiry: (a) Review — 审查代码质量、命名、结构, (b) Fix — 修复潜在问题、边界情况, (c) Optimize — 性能优化、代码简化
3. THE MaClaw_Agent SHALL inform IM_User that RFO_Phase will consume additional tokens
4. WHEN IM_User selects one or more RFO options, THE MaClaw_Agent SHALL send the corresponding instructions to the Remote_Coding_Tool via `send_and_observe`
5. WHEN IM_User declines RFO (e.g., "不需要", "跳过", "skip"), THE MaClaw_Agent SHALL proceed to report the task as complete without further action
6. IF the Coding_Task failed (non-zero exit code or error status), THEN THE MaClaw_Agent SHALL skip RFO_Phase and report the failure directly

### Requirement 5: RFO 阶段的多选与顺序执行

**User Story:** As an IM_User, I want to select multiple RFO options at once, so that I can get comprehensive code quality improvement in one go.

#### Acceptance Criteria

1. WHEN IM_User selects multiple RFO options (e.g., "review 和 optimize", "全部"), THE MaClaw_Agent SHALL execute the selected options sequentially in the order: Review → Fix → Optimize
2. WHEN executing an RFO option, THE MaClaw_Agent SHALL send a specific instruction to the Remote_Coding_Tool describing the scope of that RFO action
3. AFTER each RFO option completes, THE MaClaw_Agent SHALL report the result to IM_User before proceeding to the next option

### Requirement 6: 系统提示词工作流更新

**User Story:** As a developer, I want the system prompt to encode the new coding interaction workflow, so that the LLM Agent follows the confirmation and RFO patterns consistently.

#### Acceptance Criteria

1. THE System_Prompt SHALL contain a revised "编程任务工作流" section that instructs MaClaw_Agent to perform Confirmation_Phase before calling `create_session`
2. THE System_Prompt SHALL contain instructions for the RFO_Phase workflow after coding task completion
3. THE System_Prompt SHALL list recognized Skip_Signal patterns for both Chinese and English
4. THE System_Prompt SHALL instruct MaClaw_Agent to distinguish between Coding_Task (requiring confirmation) and simple operations (direct execution)
5. THE System_Prompt SHALL preserve all existing workflow rules (session failure stop-loss, execution verification, busy session handling)

### Requirement 7: 非编程任务不受影响

**User Story:** As an IM_User, I want non-coding operations to remain fast and direct, so that simple tasks like file listing, configuration, or general questions are not slowed down by unnecessary confirmation.

#### Acceptance Criteria

1. WHEN IM_User sends a non-coding request (e.g., "查看当前会话", "帮我截个屏", "配置 LLM"), THE MaClaw_Agent SHALL execute directly without entering Confirmation_Phase
2. WHEN IM_User sends a simple file operation via bash/read_file/write_file tools, THE MaClaw_Agent SHALL execute directly without entering Confirmation_Phase
3. THE MaClaw_Agent SHALL only trigger Confirmation_Phase for requests that would result in calling `create_session` to start a Remote_Coding_Tool
