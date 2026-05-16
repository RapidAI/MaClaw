# Implementation Plan

## Overview

Implement Project Tab isolation + archive experience feature. When users click a task in the "Recent Tasks" list, open an independent Project Tab in the AI assistant panel with isolated conversation history and project context. Support "Archive" operation to preserve task experience in long-term memory.

## Tasks

- [x] 1. AITabTypes extension and createProjectTab
  - Add `"project"` Tab type and related fields (`projectPath`, `projectName`, `archived`) in `AITabTypes.ts`
  - Add `createProjectTab(projectPath, projectName)` method in `useAITabManager.ts`, max tabs changed to 16, dedup by projectPath
  - Files: `gui/frontend/src/components/ai/AITabTypes.ts`, `gui/frontend/src/components/ai/useAITabManager.ts`
  - _Requirements: 1, 9_

- [x] 2. ProjectSearchPanel wiring to createProjectTab
  - Change `ProjectSearchPanel.onSelect` from calling `ResumeProject` + `sendMessage` to calling `createProjectTab`
  - Add `onCreateProjectTab` prop passed from `AIAssistantPanel`
  - Archived tasks show read-only experience summary on click
  - Files: `gui/frontend/src/components/ai/ProjectSearchPanel.tsx`, `gui/frontend/src/components/ai/AIAssistantPanel.tsx`
  - _Requirements: 1, 7_

- [x] 3. AITabBar overflow dropdown menu
  - `AITabBar` dynamically calculates visible Tab count (based on container width, each Tab min 100px)
  - Tabs exceeding visible area go into a "More(N)" dropdown button
  - Dropdown list shows Tab name + close button, sorted by lastActiveAt descending
  - local Tab always occupies the first visible position
  - Requires ResizeObserver + dynamic calculation
  - Files: `gui/frontend/src/components/ai/AITabBar.tsx`, `gui/frontend/src/components/ai/AITabItem.tsx`
  - _Requirements: 9_

- [x] 4. Project Tab frontend conversation state management
  - Maintain independent messages/scrollTop/inputText state for project Tabs in `AIAssistantPanel`
  - Save and restore state via `saveTabState`/`getTabState` when switching Tabs
  - Render independent conversation area when Project Tab is active (reuse `AssistantConversationBody` + `AssistantInputStack`)
  - Files: `gui/frontend/src/components/ai/AIAssistantPanel.tsx`, `gui/frontend/src/components/ai/AssistantActiveTabContent.tsx`
  - _Requirements: 2_

- [x] 5. Backend ProjectTabSessionManager (existing mechanism covers this)
  - Backend uses `SendAIAssistantMessage` ProjectPath param to synthesize per-project userID (`desktop-user:{projectPath}`)
  - ConversationMemory, WorkflowEngine, DriftDetector auto-isolate by userID
  - No new SessionManager needed
  - Files: `gui/app_wails_bindings.go`
  - _Requirements: 8_

- [x] 6. Backend session persistence
  - Create `gui/project_tab_session_persist.go` implementing session disk read/write
  - Contains `_index.json` (Tab list index) and individual session files (`tab_{id}.json`)
  - Provide `SaveSession`, `LoadSession`, `LoadIndex`, `SaveIndex`, `CleanupStale(30days)` methods
  - Files: `gui/project_tab_session_persist.go` (new file)
  - _Requirements: 2_

- [x] 7. Wails Bindings registration
  - Register new Wails binding methods: `CreateProjectTabSession(tabID, projectPath string) string`
  - `CloseProjectTabSession(tabID string)`
  - `SendMessageForTab(tabID, text string)`
  - `LoadProjectTabIndex() []TabIndexEntry`
  - `ArchiveProject(projectPath string) string`
  - Files: `gui/app_project_search.go`, `gui/app_wails_bindings.go`
  - _Requirements: 2, 5, 8_

- [x] 8. Backend message routing SendMessageForTab (existing mechanism covers this)
  - `SendAIAssistantMessage` already accepts `project_path` parameter
  - Frontend just needs to pass `project_path` when sending messages from Project Tab
  - Backend auto-synthesizes `desktop-user:{projectPath}` as userID, all downstream auto-isolates
  - Files: `gui/app_wails_bindings.go`, `gui/frontend/src/components/ai/useAIAssistant.ts`
  - _Requirements: 8_

- [x] 9. RecallDynamic strict project filter mode
  - Add `strictProject ...bool` variadic parameter to `RecallDynamic`
  - When `strictProject=true`: ScopeProject entries must have tags matching current projectPath; other projects' project_knowledge excluded; ScopeGlobal + user_fact + preference always allowed
  - Project Tab path passes `true`, other paths pass `false` (default behavior unchanged)
  - Files: `corelib/memory/store.go`, `corelib/memory/semantic_graph.go`
  - _Requirements: 3_

- [x] 10. Project Tab initial context loading
  - In `CreateProjectTabSession`, recall project's `task_artifact` + `project_knowledge` from memoryStore (using strictProject=true)
  - Generate project context summary as initial system message injected into session.conversation
  - Summary includes project name, recent progress, key artifact paths
  - Files: `gui/project_tab_session.go`
  - _Requirements: 4_

- [x] 11. Frontend Project Tab message send/receive wiring
  - When Project Tab is active, `sendMessage` calls `SendMessageForTab(tabID, text)` instead of `SendAIAssistantMessage`
  - Listen to `ai-assistant-tab-response:{tabID}` event for streaming responses and final results
  - Handle progress/streaming/token event Tab routing
  - Files: `gui/frontend/src/components/ai/AIAssistantPanel.tsx`, `gui/frontend/src/components/ai/useAIAssistant.ts`
  - _Requirements: 2, 8_

- [x] 12. Frontend Tab state persistence and restoration
  - On app startup call `LoadProjectTabIndex()` to restore Tab bar state (don't immediately load conversation history)
  - Load conversation history on-demand when Tab is activated
  - On Tab close call `CloseProjectTabSession` to trigger backend persistence
  - Files: `gui/frontend/src/components/ai/AIAssistantPanel.tsx`, `gui/frontend/src/components/ai/useAITabManager.ts`
  - _Requirements: 2_

- [x] 13. Context menu add Archive option
  - Add "Archive" option to `ProjectSearchPanel` context menu
  - Show confirmation dialog on click
  - On confirm call backend `ArchiveProject(projectPath)`
  - On success refresh list + close corresponding Tab (if open)
  - Files: `gui/frontend/src/components/ai/ProjectSearchPanel.tsx`
  - _Requirements: 5_

- [x] 14. Backend archive logic and LLM experience extraction
  - Create `gui/project_tab_archive.go` implementing `ArchiveProject(projectPath)`
  - Collect all task_artifact + project_knowledge for project -> call LLM to generate experience summary
  - Save as ScopeGlobal project_knowledge entry (tags include "archived_experience" + original path)
  - Mark archived=true in ProjectIndex
  - When LLM unavailable only mark status
  - Files: `gui/project_tab_archive.go` (new file), `corelib/memory/project_index.go`
  - _Requirements: 6_

- [x] 15. ProjectIndex archive status support
  - Add `Archived bool` field to `ProjectRecord`
  - Add `SetArchived(projectPath, bool)` and `IsArchived(projectPath) bool` methods to `ProjectIndex`
  - `ListRecent` excludes archived projects by default
  - `Search` includes archived projects when query contains "archived"
  - Files: `corelib/memory/project_index.go`
  - _Requirements: 7_

- [x] 16. Archived task read-only display
  - Clicking archived task shows read-only experience summary card instead of creating editable Tab
  - Recall "archived_experience" entries from memory
  - Card contains structured experience content with "This task is archived, cannot continue" notice at bottom
  - Files: `gui/frontend/src/components/ai/ProjectSearchPanel.tsx`, `gui/frontend/src/components/ai/ArchivedExperienceCard.tsx` (new file)
  - _Requirements: 7_

- [x] 17. buildMemoryIndex project filtering
  - `buildMemoryIndex` in Project Tab scenario only counts entries related to current project (tags matching projectPath + ScopeGlobal)
  - Determine if currently in Project Tab via `IMMessageHandler` context
  - Files: `gui/im_system_prompt.go`
  - _Requirements: 3_

- [x] 18. 30-day stale session cleanup
  - On app startup call `CleanupStaleSessions(30*24*time.Hour)` to delete sessions inactive for over 30 days
  - Also remove corresponding entries from `_index.json`
  - Files: `gui/project_tab_session_persist.go`, `gui/app.go`
  - _Requirements: 2_

## Task Dependency Graph

```
1 (done) --> 3
1 (done) --> 4
2 (done) --> 4
5 (done) --> 6
6 --> 7
6 --> 18
7 --> 11
7 --> 12
7 --> 13
7 --> 14
8 (done) --> 11
4 --> 11
9 --> 10
9 --> 17
5 (done) --> 10
8 (done) --> 17
15 --> 16
13 --> 16
14 --> 16
```

## Notes

- Tasks 1, 2, 5, 8 are already completed (code exists in the codebase)
- Tasks 5 and 8 required no new code - existing mechanisms already cover the functionality
- Priority order: P0 tasks (6, 7, 9, 4, 11) then P1 tasks (3, 10, 12, 13, 14, 15) then P2 tasks (16, 17, 18)
