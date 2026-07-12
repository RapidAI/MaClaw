# Requirements Document

## Introduction

本功能将 MaClaw Agent 的编程任务处理流程从当前的"需求确认 → 执行 → RFO"三阶段模式，升级为完整的五阶段 Spec 驱动编程工作流：

1. **需求确认阶段**（用户交互）：Agent 理解需求后生成需求文档（PDF），通过 IM 发给用户确认/修改
2. **技术设计阶段**（用户交互）：基于确认的需求生成技术设计文档（PDF），发给用户确认/修改
3. **任务分解阶段**（用户交互）：基于设计生成任务列表（含 TDD 验收测试用例），生成文档（PDF）发给用户确认/修改
4. **任务执行阶段**（自动）：按任务列表逐个执行，每个任务完成后跑对应测试用例验证
5. **完成检查阶段**（自动）：所有任务完成后做整体验收检查

当前架构中，Agent 行为由 `buildSystemPrompt()` 中的系统提示词驱动。本功能通过修改 `gui/im_message_handler.go` 中的 `buildSystemPrompt()` 方法，用纯提示词方式替换现有的"编程任务工作流"部分，不新增状态机或 Go 代码逻辑。Agent 已有 `create_session`、`send_and_observe`、`send_file`、`craft_tool`（生成脚本）、`bash`（执行命令）等工具，足以支撑文档生成（通过编程工具或脚本生成 PDF）和文件发送。

## Glossary

- **MaClaw_Agent**: IMMessageHandler 中的 LLM Agent 循环（`runAgentLoop`），负责理解用户意图、调用工具、生成回复
- **System_Prompt**: `buildSystemPrompt()` 生成的系统提示词，指导 Agent 的行为模式和工作流程
- **Remote_Coding_Tool**: 通过 `create_session` 启动的远程编程工具（Claude Code、Gemini CLI、Codex 等），由 RemoteSessionManager 管理
- **IM_User**: 通过飞书、QQ 等 IM 平台向 MaClaw 发送消息的用户
- **Coding_Task**: 需要调用 Remote_Coding_Tool 执行的编程类需求（如写代码、重构、修 bug、添加功能等）
- **Spec_Workflow**: 五阶段 Spec 驱动编程工作流的总称
- **Requirements_Phase**: 第一阶段，Agent 生成需求文档并发送给用户确认
- **Design_Phase**: 第二阶段，Agent 基于确认的需求生成技术设计文档并发送给用户确认
- **TaskBreakdown_Phase**: 第三阶段，Agent 基于设计生成任务列表（含 TDD 测试用例）并发送给用户确认
- **Execution_Phase**: 第四阶段，Agent 按任务列表逐个执行并用测试用例验证，自动进行
- **Verification_Phase**: 第五阶段，所有任务完成后做整体验收检查，自动进行
- **Phase_Document**: 每个用户交互阶段生成的 PDF 文档，通过 IM 发送给用户
- **Skip_Signal**: 用户发出的跳过所有确认阶段的信号，如"直接做"、"不用问了"等
- **TDD_Test_Case**: 任务分解阶段为每个任务定义的验收测试用例，用于 Execution_Phase 中验证任务完成情况
- **Phase_Confirmation**: 用户对某阶段文档的明确确认（如"确认"、"没问题"、"通过"）
- **Phase_Revision**: 用户对某阶段文档提出的修改意见，Agent 需更新文档后重新发送确认

## Requirements

### Requirement 1: 编程任务识别与工作流触发

**User Story:** As an IM_User, I want MaClaw_Agent to follow a structured spec-driven workflow for coding tasks, so that requirements are clearly defined, designs are reviewed, and tasks are properly planned before execution begins.

#### Acceptance Criteria

1. WHEN IM_User sends a message that constitutes a Coding_Task, THE MaClaw_Agent SHALL enter Spec_Workflow instead of directly calling `create_session`
2. THE MaClaw_Agent SHALL distinguish Coding_Task from non-coding requests (simple questions, configuration, file operations, information retrieval, translation, document generation) and only trigger Spec_Workflow for Coding_Task
3. WHEN IM_User sends a non-coding request, THE MaClaw_Agent SHALL execute directly without entering Spec_Workflow
4. THE MaClaw_Agent SHALL execute the five phases of Spec_Workflow in strict sequential order: Requirements_Phase → Design_Phase → TaskBreakdown_Phase → Execution_Phase → Verification_Phase

### Requirement 2: 需求确认阶段（Requirements Phase）

**User Story:** As an IM_User, I want MaClaw_Agent to generate a requirements document and send it to me for review, so that I can verify the Agent's understanding before any design or coding begins.

#### Acceptance Criteria

1. WHEN MaClaw_Agent enters Requirements_Phase, THE MaClaw_Agent SHALL generate a requirements document containing: (a) 需求背景与目标, (b) 功能需求列表（每条需求有编号和验收标准）, (c) 非功能需求（如有）, (d) 约束与假设
2. WHEN the requirements document is generated, THE MaClaw_Agent SHALL convert the document to PDF format and send the PDF to IM_User via the `send_file` tool with `forward_to_im=true`
3. WHILE in Requirements_Phase, THE MaClaw_Agent SHALL wait for IM_User's Phase_Confirmation before proceeding to Design_Phase
4. WHEN IM_User provides Phase_Revision during Requirements_Phase, THE MaClaw_Agent SHALL update the requirements document, regenerate the PDF, and resend to IM_User for confirmation
5. THE MaClaw_Agent SHALL also send a brief text summary of the requirements document in the IM message alongside the PDF, so that IM_User can quickly preview the content

### Requirement 3: 技术设计阶段（Design Phase）

**User Story:** As an IM_User, I want MaClaw_Agent to generate a technical design document based on confirmed requirements, so that I can review the implementation approach before coding starts.

#### Acceptance Criteria

1. WHEN IM_User confirms the requirements document, THE MaClaw_Agent SHALL enter Design_Phase and generate a technical design document containing: (a) 架构设计（涉及的模块和文件）, (b) 接口设计（关键函数/方法签名）, (c) 数据模型变更（如有）, (d) 实现方案概述
2. WHEN the design document is generated, THE MaClaw_Agent SHALL convert the document to PDF format and send the PDF to IM_User via the `send_file` tool with `forward_to_im=true`
3. WHILE in Design_Phase, THE MaClaw_Agent SHALL wait for IM_User's Phase_Confirmation before proceeding to TaskBreakdown_Phase
4. WHEN IM_User provides Phase_Revision during Design_Phase, THE MaClaw_Agent SHALL update the design document, regenerate the PDF, and resend to IM_User for confirmation
5. THE MaClaw_Agent SHALL also send a brief text summary of the design document in the IM message alongside the PDF

### Requirement 4: 任务分解阶段（TaskBreakdown Phase）

**User Story:** As an IM_User, I want MaClaw_Agent to break down the design into a task list with TDD test cases, so that I can review the execution plan and each task has clear acceptance criteria.

#### Acceptance Criteria

1. WHEN IM_User confirms the design document, THE MaClaw_Agent SHALL enter TaskBreakdown_Phase and generate a task list document containing: (a) 编号的任务列表（按执行顺序排列）, (b) 每个任务的描述和涉及的文件, (c) 每个任务的 TDD 验收测试用例（测试名称、测试步骤、预期结果）
2. WHEN the task list document is generated, THE MaClaw_Agent SHALL convert the document to PDF format and send the PDF to IM_User via the `send_file` tool with `forward_to_im=true`
3. WHILE in TaskBreakdown_Phase, THE MaClaw_Agent SHALL wait for IM_User's Phase_Confirmation before proceeding to Execution_Phase
4. WHEN IM_User provides Phase_Revision during TaskBreakdown_Phase, THE MaClaw_Agent SHALL update the task list document, regenerate the PDF, and resend to IM_User for confirmation
5. THE MaClaw_Agent SHALL also send a brief text summary of the task list in the IM message alongside the PDF

### Requirement 5: 任务执行阶段（Execution Phase）

**User Story:** As an IM_User, I want MaClaw_Agent to automatically execute each task and verify it with the corresponding TDD test case, so that I don't need to intervene during the coding process.

#### Acceptance Criteria

1. WHEN IM_User confirms the task list, THE MaClaw_Agent SHALL enter Execution_Phase and begin executing tasks sequentially without further user interaction
2. WHEN executing a task, THE MaClaw_Agent SHALL call `create_session` to start a Remote_Coding_Tool and send the task description along with the confirmed requirements and design context via `send_and_observe`
3. WHEN a task's coding is complete, THE MaClaw_Agent SHALL instruct the Remote_Coding_Tool to run the corresponding TDD_Test_Case to verify the task
4. IF a TDD_Test_Case fails, THEN THE MaClaw_Agent SHALL instruct the Remote_Coding_Tool to fix the issue and re-run the test, up to 3 retry attempts
5. IF a task fails after 3 retry attempts, THEN THE MaClaw_Agent SHALL record the failure, skip to the next task, and include the failure in the final verification report
6. WHILE in Execution_Phase, THE MaClaw_Agent SHALL send progress updates to IM_User after each task completes (e.g., "任务 3/8 完成 " or "任务 4/8 失败 ")

### Requirement 6: 完成检查阶段（Verification Phase）

**User Story:** As an IM_User, I want MaClaw_Agent to perform an overall acceptance check after all tasks are done, so that I can be confident the entire feature works correctly.

#### Acceptance Criteria

1. WHEN all tasks in Execution_Phase are completed (or skipped due to failure), THE MaClaw_Agent SHALL enter Verification_Phase automatically
2. WHILE in Verification_Phase, THE MaClaw_Agent SHALL instruct the Remote_Coding_Tool to run all TDD_Test_Cases together as a full regression suite
3. WHEN the full regression suite completes, THE MaClaw_Agent SHALL generate a completion report containing: (a) 总任务数和成功/失败数, (b) 每个任务的执行结果, (c) 全量测试运行结果, (d) 失败任务的错误摘要（如有）
4. THE MaClaw_Agent SHALL send the completion report to IM_User as a text message in IM
5. IF all tasks and tests pass, THEN THE MaClaw_Agent SHALL report the feature as successfully completed
6. IF any tasks or tests failed, THEN THE MaClaw_Agent SHALL list the failures and suggest next steps to IM_User

### Requirement 7: Skip Signal 跳过所有确认阶段

**User Story:** As an IM_User, I want to skip all confirmation phases when I trust the Agent's judgment, so that I can get faster execution without reviewing each document.

#### Acceptance Criteria

1. WHEN IM_User includes a Skip_Signal in the coding request (such as "直接做", "不用问了", "按你的想法来", "just do it"), THE MaClaw_Agent SHALL skip Requirements_Phase, Design_Phase, and TaskBreakdown_Phase, and proceed directly to internal planning followed by Execution_Phase
2. WHEN Skip_Signal is detected, THE MaClaw_Agent SHALL still internally generate the requirements understanding, design approach, and task breakdown, but without generating PDFs or waiting for user confirmation
3. THE MaClaw_Agent SHALL recognize Skip_Signal in both Chinese and English expressions
4. WHEN IM_User sends a Skip_Signal during any of the three confirmation phases (Requirements_Phase, Design_Phase, or TaskBreakdown_Phase), THE MaClaw_Agent SHALL skip the remaining confirmation phases and proceed to Execution_Phase

### Requirement 8: PDF 文档生成与发送

**User Story:** As an IM_User, I want each phase document to be delivered as a PDF, so that I can comfortably read it on my mobile IM client without dealing with long text blocks.

#### Acceptance Criteria

1. THE MaClaw_Agent SHALL generate Phase_Document in PDF format for each of the three confirmation phases (Requirements_Phase, Design_Phase, TaskBreakdown_Phase)
2. THE MaClaw_Agent SHALL use available tools (such as `craft_tool` to generate a Markdown-to-PDF conversion script, or `bash` to invoke a PDF generation command) to produce the PDF file
3. WHEN a Phase_Document PDF is generated, THE MaClaw_Agent SHALL send the PDF to IM_User via the `send_file` tool with `forward_to_im=true`
4. THE MaClaw_Agent SHALL name each PDF file descriptively (e.g., "需求文档_feature_name.pdf", "设计文档_feature_name.pdf", "任务列表_feature_name.pdf")
5. IF PDF generation fails, THEN THE MaClaw_Agent SHALL fall back to sending the document content as formatted text in the IM message and inform IM_User of the PDF generation failure

### Requirement 9: 阶段间上下文传递

**User Story:** As a developer, I want each phase to build upon the confirmed output of the previous phase, so that the workflow maintains consistency from requirements through execution.

#### Acceptance Criteria

1. THE MaClaw_Agent SHALL pass the confirmed requirements document content as input context to Design_Phase
2. THE MaClaw_Agent SHALL pass both the confirmed requirements and confirmed design document content as input context to TaskBreakdown_Phase
3. THE MaClaw_Agent SHALL pass the confirmed requirements, design, and task list as context when sending coding instructions to Remote_Coding_Tool during Execution_Phase
4. THE MaClaw_Agent SHALL store all phase documents in the conversation memory (`conversationMemory`) so that context is preserved across Agent loop iterations
5. IF IM_User revises a document in an earlier phase, THEN THE MaClaw_Agent SHALL use the revised version (not the original) as input to subsequent phases

### Requirement 10: 系统提示词工作流更新

**User Story:** As a developer, I want the system prompt to encode the complete five-phase spec-driven workflow, so that the LLM Agent follows the structured process consistently.

#### Acceptance Criteria

1. THE System_Prompt SHALL contain a revised "编程任务工作流" section that instructs MaClaw_Agent to follow the five-phase Spec_Workflow for Coding_Task
2. THE System_Prompt SHALL contain detailed instructions for each of the five phases including document content requirements, PDF generation steps, and phase transition conditions
3. THE System_Prompt SHALL list recognized Skip_Signal patterns for both Chinese and English
4. THE System_Prompt SHALL instruct MaClaw_Agent to distinguish between Coding_Task (requiring Spec_Workflow) and non-coding tasks (direct execution)
5. THE System_Prompt SHALL preserve all existing workflow rules (session failure stop-loss, execution verification, busy session handling, auto-resume)
6. THE System_Prompt SHALL be implemented by modifying the `buildSystemPrompt()` method in `gui/im_message_handler.go` without adding new state machine logic or Go code structures

### Requirement 11: 用户在阶段间的修改回退

**User Story:** As an IM_User, I want to request changes to a previous phase's document even after moving to a later phase, so that I can correct issues discovered during review of subsequent documents.

#### Acceptance Criteria

1. WHEN IM_User requests to revisit a previous phase during Design_Phase or TaskBreakdown_Phase (e.g., "需求文档需要改一下", "回到需求阶段"), THE MaClaw_Agent SHALL return to the requested phase and present the document for revision
2. WHEN a previous phase document is revised, THE MaClaw_Agent SHALL regenerate all subsequent phase documents based on the updated content
3. THE MaClaw_Agent SHALL inform IM_User when returning to a previous phase and explain that subsequent documents will be regenerated
