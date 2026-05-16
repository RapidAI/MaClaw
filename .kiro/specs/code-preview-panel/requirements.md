# Requirements Document

## Introduction

为 maclaw 桌面端 AI 助手面板添加代码预览区功能。当 AI 生成或修改代码时，面板右侧打开代码预览区，顶部显示文件名列表，预览区显示当前选中文件的内容。对于修改的文件，以 diff 视图标记添加/删除的代码行，方便用户审查代码变更。

该功能与现有的 `WorkflowDocPreview`（工作流文档预览）是独立的面板，专门用于代码生成场景。复用现有的 Wails 事件通信机制和分屏布局模式。

## Glossary

- **Code_Preview_Panel**: 代码预览面板组件，在 AI 助手面板右侧显示，用于展示 AI 生成或修改的代码文件内容
- **AI_Assistant_Panel**: maclaw 桌面端的 AI 助手主面板，包含聊天区和右侧预览区
- **File_Tab_Bar**: 代码预览面板顶部的文件名标签栏，列出所有当前生成/修改的文件
- **Diff_View**: 差异视图，以行级标记（添加行/删除行）展示文件修改前后的变化
- **Code_Event_Emitter**: Go 后端模块，负责在代码生成/修改事件发生时通过 Wails 事件通知前端
- **Wails_Runtime**: Wails 框架提供的 Go↔前端双向通信机制，通过 `runtime.EventsEmit` 和 `EventsOn` 实现
- **Session_Tool**: maclaw 的编程会话工具（`create_session`/`send_and_observe`），AI 通过该工具执行代码生成任务
- **Syntax_Highlighter**: 代码语法高亮渲染器，根据文件扩展名识别语言并着色显示

## Requirements

### Requirement 1: 代码预览面板打开与关闭

**User Story:** As a maclaw 用户, I want 在 AI 生成代码时自动打开右侧代码预览面板, so that 我可以实时查看生成的代码内容而不离开聊天界面。

#### Acceptance Criteria

1. WHEN the Code_Event_Emitter emits a code file event, THE AI_Assistant_Panel SHALL open the Code_Preview_Panel in a split-pane layout on the right side of the chat area
2. WHEN the Code_Preview_Panel is open and the user clicks the close button, THE Code_Preview_Panel SHALL close and the chat area SHALL expand to full width
3. WHEN no code file events have been emitted in the current session, THE Code_Preview_Panel SHALL remain hidden
4. WHILE the Code_Preview_Panel is open, THE AI_Assistant_Panel SHALL display the chat area and the Code_Preview_Panel side by side with a draggable split ratio
5. IF the Code_Preview_Panel is already open and a new code file event arrives, THEN THE Code_Preview_Panel SHALL remain open and update its content without re-triggering the open animation
6. WHEN the user manually closes the Code_Preview_Panel, THE Code_Preview_Panel SHALL not auto-reopen for subsequent code file events in the same session until the user explicitly reopens it

### Requirement 2: 文件名标签栏

**User Story:** As a maclaw 用户, I want 在代码预览区顶部看到所有生成/修改的文件名列表, so that 我可以快速切换查看不同文件的内容。

#### Acceptance Criteria

1. WHEN the Code_Preview_Panel opens, THE File_Tab_Bar SHALL display the file names of all code files received from the Code_Event_Emitter
2. WHEN a new code file event arrives for a file not yet in the File_Tab_Bar, THE File_Tab_Bar SHALL append the new file name as a tab
3. THE File_Tab_Bar SHALL highlight the currently selected file tab with a distinct visual style
4. WHEN the user clicks a file tab in the File_Tab_Bar, THE Code_Preview_Panel SHALL display the content of the selected file
5. THE File_Tab_Bar SHALL display only the file name (not the full path) as the tab label
6. WHEN the user hovers over a file tab, THE File_Tab_Bar SHALL display the full file path as a tooltip
7. WHILE the File_Tab_Bar contains more tabs than can fit in the visible width, THE File_Tab_Bar SHALL provide horizontal scrolling to access all tabs
8. WHEN a code file event arrives for a file already in the File_Tab_Bar, THE File_Tab_Bar SHALL update the content of that file without adding a duplicate tab

### Requirement 3: 代码内容显示

**User Story:** As a maclaw 用户, I want 在预览区看到当前选中文件的代码内容并带有语法高亮, so that 我可以方便地阅读和审查代码。

#### Acceptance Criteria

1. WHEN a file is selected in the File_Tab_Bar, THE Code_Preview_Panel SHALL display the full content of that file with line numbers
2. THE Syntax_Highlighter SHALL apply syntax highlighting based on the file extension of the selected file
3. THE Code_Preview_Panel SHALL display code content in a monospace font with appropriate line spacing
4. WHILE the code content exceeds the visible area, THE Code_Preview_Panel SHALL provide vertical scrolling
5. WHEN a new code file event arrives and the file is currently selected, THE Code_Preview_Panel SHALL update the displayed content to reflect the latest version
6. THE Code_Preview_Panel SHALL preserve the user's scroll position when the content of the currently viewed file is updated, unless the update appends content beyond the current viewport

### Requirement 4: Diff 视图（修改文件标记）

**User Story:** As a maclaw 用户, I want 对于修改的文件看到添加/删除的代码行标记, so that 我可以快速理解 AI 对现有代码做了哪些变更。

#### Acceptance Criteria

1. WHEN a code file event includes both original content and modified content, THE Diff_View SHALL compute and display a line-level diff
2. THE Diff_View SHALL mark added lines with a green background color and a "+" prefix indicator
3. THE Diff_View SHALL mark deleted lines with a red background color and a "-" prefix indicator
4. THE Diff_View SHALL display unchanged lines with no background highlight and no prefix indicator
5. WHEN a code file event includes only new content (no original content), THE Code_Preview_Panel SHALL display the content as a new file without diff markers
6. THE File_Tab_Bar SHALL display a visual indicator (badge or icon) on tabs for files that contain modifications versus newly created files
7. WHILE the Diff_View is active for a file, THE Code_Preview_Panel SHALL display line numbers for both the original and modified versions

### Requirement 5: Go 后端代码事件发射

**User Story:** As a maclaw 开发者, I want Go 后端在代码生成/修改时通过 Wails 事件通知前端, so that 前端代码预览面板可以实时更新。

#### Acceptance Criteria

1. WHEN the Session_Tool completes a file write operation, THE Code_Event_Emitter SHALL emit a Wails event containing the file path, file content, and operation type (create or modify)
2. WHEN the Session_Tool completes a file modify operation, THE Code_Event_Emitter SHALL include the original file content in the event payload for diff computation
3. THE Code_Event_Emitter SHALL emit events using the Wails `runtime.EventsEmit` function, consistent with the existing `workflow:doc_update` event pattern
4. THE Code_Event_Emitter SHALL emit a session-start event when a coding session begins, to signal the Code_Preview_Panel to prepare for incoming file events
5. THE Code_Event_Emitter SHALL emit a session-end event when a coding session completes, to signal the Code_Preview_Panel that no more file events are expected
6. IF the Wails runtime context is not available, THEN THE Code_Event_Emitter SHALL skip event emission without causing errors

### Requirement 6: 与现有工作流预览面板的共存

**User Story:** As a maclaw 用户, I want 代码预览面板和工作流文档预览面板互不干扰, so that 两种预览功能可以在各自的场景中正常工作。

#### Acceptance Criteria

1. WHILE the WorkflowDocPreview is active (displaying workflow documents), THE Code_Preview_Panel SHALL not open or interfere with the WorkflowDocPreview display
2. WHEN the workflow transitions from a document phase to an implementation phase, THE Code_Preview_Panel SHALL be allowed to open for code file events
3. WHEN the Code_Preview_Panel is active and a workflow document event arrives, THE AI_Assistant_Panel SHALL switch to the WorkflowDocPreview and close the Code_Preview_Panel
4. THE Code_Preview_Panel and the WorkflowDocPreview SHALL share the same split-pane layout mechanism in the AI_Assistant_Panel

### Requirement 7: 主题适配

**User Story:** As a maclaw 用户, I want 代码预览面板适配当前的明暗主题, so that 预览区的视觉风格与整体界面一致。

#### Acceptance Criteria

1. WHILE the AI_Assistant_Panel is in dark mode, THE Code_Preview_Panel SHALL use a dark color scheme for backgrounds, text, syntax highlighting, and diff markers
2. WHILE the AI_Assistant_Panel is in light mode, THE Code_Preview_Panel SHALL use a light color scheme for backgrounds, text, syntax highlighting, and diff markers
3. WHEN the user toggles the theme mode, THE Code_Preview_Panel SHALL immediately update its color scheme without requiring a panel reopen
