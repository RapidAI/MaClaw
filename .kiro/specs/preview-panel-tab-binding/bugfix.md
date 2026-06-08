# Bugfix Requirements Document

## Introduction

AI助手面板的右侧预览区域（工作流进度看板、文档预览、源码预览）存在显示不稳定问题。核心缺陷是预览面板状态为全局单例而非 per-tab 绑定，导致切换 tab 后面板内容不变或消失、工作流结束后面板立即隐藏、事件时序竞态导致文档更新被丢弃。

涉及的全局单例状态：
- `useWorkflowState()` 在 `AIAssistantPanel` 顶层调用，返回全局工作流预览状态
- `useCodePreviewState()` 在 `AIAssistantPanel` 顶层调用，返回全局源码预览状态
- `AgentView`（表单）也是全局的，不绑定到具体 tab

后端事件（`workflow:phase_update`、`workflow:doc_update`、`code:file_update`）已携带 `project_path` 路由信息，但前端状态层未利用这一信息做 per-tab 路由。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN the user switches from Tab A (with active workflow preview) to Tab B THEN the system continues displaying Tab A's workflow documents in the preview pane, or the preview pane disappears entirely

1.2 WHEN a workflow completes (status transitions from "active" to "completed" or "cancelled") THEN the system immediately sets `splitMode=false` and hides the preview pane, preventing the user from reviewing final documents

1.3 WHEN a `workflow:doc_update` event arrives after a `workflow:phase_update(completed)` event (race condition) THEN the system discards the document update because `workflowActiveRef.current` guard returns false

1.4 WHEN a `code:file_update` event arrives for Tab A's project while Tab B is active THEN the system updates the global code preview state visible on Tab B, or the update is lost when switching back to Tab A

1.5 WHEN the user switches tabs THEN the system only saves/restores left-side chat messages and scrollTop but does not save/restore the right-side preview panel state (splitMode, activePreviewMode, phaseDocuments, codePreviewFiles)

1.6 WHEN an AgentView form is displayed for Tab A and the user switches to Tab B THEN the system still shows Tab A's AgentView form overlaying Tab B's content

### Expected Behavior (Correct)

2.1 WHEN the user switches from Tab A to Tab B THEN the system SHALL save Tab A's complete preview state (workflowState + codePreviewState + activePreviewMode) and restore Tab B's previously saved preview state

2.2 WHEN a workflow completes THEN the system SHALL keep the preview pane visible with the final documents so the user can review them, only clearing on explicit user action or new workflow start

2.3 WHEN a `workflow:doc_update` event arrives after workflow completion THEN the system SHALL accept and display the document update regardless of workflow active status (the document is the final output and should not be discarded)

2.4 WHEN a `code:file_update` event arrives with a `project_path` field THEN the system SHALL route the update to the corresponding tab's code preview state based on the project_path, not to the global state

2.5 WHEN the user switches tabs THEN the system SHALL save the complete right-side panel state for the current tab and restore the target tab's panel state, including splitMode, split ratio, active preview mode tab (workflow/code), phase documents, code preview files, and active file path

2.6 WHEN an AgentView form is associated with a specific tab THEN the system SHALL only display it when that tab is active, hiding it when the user switches to another tab

### Unchanged Behavior (Regression Prevention)

3.1 WHEN only a single local tab exists (no other tabs) THEN the system SHALL CONTINUE TO behave identically to the current implementation (global singleton state with no save/restore overhead)

3.2 WHEN backend events (`workflow:phase_update`, `workflow:doc_update`, `code:file_update`) arrive without a `project_path` field THEN the system SHALL CONTINUE TO route them to the currently active tab's state (backward compatible fallback)

3.3 WHEN the user manually closes the preview pane (userClosed) THEN the system SHALL CONTINUE TO suppress auto-open behavior for that tab until the user explicitly reopens

3.4 WHEN a workflow emits `workflow:suggest_maximize` THEN the system SHALL CONTINUE TO show the fullscreen suggestion banner (with per-tab scoping if applicable)

3.5 WHEN the `code:session_start` event fires THEN the system SHALL CONTINUE TO reset the code preview files map and close the panel until the first file update arrives

3.6 WHEN the split ratio is adjusted via drag handle THEN the system SHALL CONTINUE TO persist the ratio and apply it consistently within the same tab
