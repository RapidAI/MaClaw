# Tasks: Code Preview Panel

## Task 1: Go 后端 CodeEventEmitter 模块

### Description
新增 `gui/code_event_emitter.go`，实现代码文件事件发射功能。

- [x] 1.1 Create `CodeFileEvent` struct with fields: SessionID, FilePath, FileName, Content, Original, OpType, Language
- [x] 1.2 Create `CodeEventEmitter` struct with `app *App` field and constructor `NewCodeEventEmitter(app *App)`
- [x] 1.3 Implement `EmitCodeFileEvent(evt CodeFileEvent)` — emit `code:file_update` via `runtime.EventsEmit`, skip silently if `app.ctx == nil`
- [x] 1.4 Implement `EmitSessionStart(sessionID string)` — emit `code:session_start` event with session_id payload
- [x] 1.5 Implement `EmitSessionEnd(sessionID string)` — emit `code:session_end` event with session_id payload
- [x] 1.6 Implement `detectLanguageFromExt(fileName string) string` — map file extension to language identifier (go, typescript, python, etc.), return "plaintext" for unknown
- [x] 1.7 Write unit tests for CodeEventEmitter: nil context safety, correct payload construction, language detection mapping

## Task 2: 集成 CodeEventEmitter 到 Session 事件流

### Description
在 `RemoteSessionManager` 的事件处理流程中，当检测到文件操作事件时调用 CodeEventEmitter。

- [x] 2.1 Add `codeEventEmitter *CodeEventEmitter` field to `App` struct, initialize in `NewApp()` or startup
- [x] 2.2 In `RemoteSessionManager` event processing (where `file.change` / `file.read` events are extracted), call `codeEventEmitter.EmitCodeFileEvent` with file path, read file content from disk, and determine opType
- [x] 2.3 For `file.change` events, attempt to read original content via `git show HEAD:<relative_path>`, fall back to empty string if git unavailable
- [x] 2.4 Emit `code:session_start` when a new coding session is created (in `toolCreateSession` or session start handler)
- [x] 2.5 Emit `code:session_end` when a coding session exits (in session exit/cleanup handler)
- [x] 2.6 Add file size guard: skip event emission for files > 1MB

## Task 3: Frontend — useCodePreviewState hook

### Description
新增 `gui/frontend/src/components/ai/useCodePreviewState.ts`，管理代码预览面板状态。

- [x] 3.1 Define `CodeFile` interface (filePath, fileName, content, original, opType, language, updatedAt)
- [x] 3.2 Define `CodePreviewUIState` interface (active, files Map, activeFilePath, sessionActive, userClosed)
- [x] 3.3 Implement `useCodePreviewState` hook with state management via `useState`
- [x] 3.4 Add `EventsOn("code:file_update")` listener — update files map, auto-open panel if not userClosed, auto-select latest file
- [x] 3.5 Add `EventsOn("code:session_start")` listener — reset files map, set sessionActive=true, reset userClosed
- [x] 3.6 Add `EventsOn("code:session_end")` listener — set sessionActive=false
- [x] 3.7 Implement `closePanel()` — set active=false, userClosed=true
- [x] 3.8 Implement `reopenPanel()` — set active=true, userClosed=false
- [x] 3.9 Implement `selectFile(filePath)` — set activeFilePath
- [x] 3.10 Implement `resetSession()` — clear all state
- [x] 3.11 Write property tests for: idempotent open (Property 1), user close suppression (Property 2), file map completeness (Property 3), no duplicate files (Property 5)

## Task 4: Frontend — Diff 计算模块

### Description
新增 `gui/frontend/src/components/ai/diffCompute.ts`，实现行级 diff 计算。

- [x] 4.1 Define `DiffLine` interface (type: add/delete/unchanged, content, oldLineNum, newLineNum)
- [x] 4.2 Implement `computeDiff(original: string, modified: string): DiffLine[]` using Myers diff algorithm (line-level)
- [x] 4.3 Implement line number assignment: oldLineNum for unchanged+delete lines, newLineNum for unchanged+add lines
- [x] 4.4 Handle edge cases: empty original (all adds), empty modified (all deletes), identical content (all unchanged)
- [x] 4.5 Write property tests for: diff round-trip correctness (Property 7), dual line number monotonicity (Property 9)

## Task 5: Frontend — 语法高亮模块

### Description
新增 `gui/frontend/src/components/ai/syntaxHighlight.ts`，轻量级语法高亮。

- [x] 5.1 Define `HighlightToken` interface (text, type: keyword/string/comment/number/operator/function/type/plain)
- [x] 5.2 Implement `detectLanguage(fileName: string): string` — map file extension to language identifier
- [x] 5.3 Implement `tokenizeLine(line: string, language: string): HighlightToken[]` — regex-based tokenization for supported languages
- [x] 5.4 Add language rules for: Go, TypeScript/JavaScript, Python, Rust, Java, C/C++, HTML, CSS, JSON, YAML, Shell
- [x] 5.5 Write property test for language detection (Property 6)
- [x] 5.6 Write unit tests for tokenization of each supported language

## Task 6: Frontend — FileTabBar 组件

### Description
新增文件名标签栏子组件，显示在代码预览面板顶部。

- [x] 6.1 Create `FileTabBar` component with props: files Map, activeFilePath, onSelectFile, theme
- [x] 6.2 Render tab for each file — display fileName only as label, full filePath as tooltip (title attribute)
- [x] 6.3 Highlight active tab with distinct background/text color from theme
- [x] 6.4 Add visual indicator (icon/badge) for modify vs create opType on each tab
- [x] 6.5 Implement horizontal scroll with `overflow-x: auto` when tabs overflow container width
- [x] 6.6 Write property test for tab indicator matching opType (Property 8)
- [x] 6.7 Write property test for file name extraction from path (Property 4)

## Task 7: Frontend — CodePreviewPanel 主组件

### Description
新增 `gui/frontend/src/components/ai/CodePreviewPanel.tsx`，代码预览面板主组件。

- [x] 7.1 Create `CodePreviewPanel` component with props: files, activeFilePath, onSelectFile, onClose, theme
- [x] 7.2 Render `FileTabBar` at top
- [x] 7.3 Render code content area with line numbers, monospace font, vertical scrolling
- [x] 7.4 Apply syntax highlighting to code content using `tokenizeLine`
- [x] 7.5 When active file has `original` content, render `DiffView` instead of plain code view
- [x] 7.6 DiffView: render dual line numbers (old/new), green background + "+" for added lines, red background + "-" for deleted lines, no highlight for unchanged
- [x] 7.7 Add close button in panel header
- [x] 7.8 Implement scroll position preservation on content update (save scrollTop, restore after re-render)
- [x] 7.9 Define `CodePreviewTheme` with dark and light variants matching existing theme system

## Task 8: AIAssistantPanel 集成

### Description
在现有 `AIAssistantPanel.tsx` 中集成代码预览面板。

- [x] 8.1 Import and call `useCodePreviewState` hook in `AIAssistantPanel`
- [x] 8.2 In the split-pane render area, conditionally render `CodePreviewPanel` when code preview is active and workflow preview is not active
- [x] 8.3 Share `splitMode` / `splitRatio` between workflow preview and code preview — extend `useWorkflowState` or coordinate via parent state
- [x] 8.4 Implement mutual exclusion: when `workflow:doc_update` arrives while code preview is active, close code preview; when `code:file_update` arrives while workflow preview is active, suppress code preview opening
- [x] 8.5 Pass correct theme (dark/light CodePreviewTheme) based on current themeMode
- [x] 8.6 Write property test for mutual exclusion (Property 10)

## Task 9: 主题适配

### Description
确保代码预览面板在明暗主题下正确显示。

- [x] 9.1 Define `darkCodePreviewTheme` and `lightCodePreviewTheme` constants with appropriate colors for: backgrounds, text, line numbers, tab bar, diff markers, syntax highlighting tokens
- [x] 9.2 Wire theme selection to `themeMode` state in `AIAssistantPanel` — pass correct theme to `CodePreviewPanel`
- [x] 9.3 Verify theme switches immediately without panel reopen (React reactivity via props)
- [x] 9.4 Write unit tests verifying dark/light theme color values are distinct

## Task 10: 端到端验证

### Description
验证完整的数据流：编程会话文件操作 → Go 事件发射 → 前端面板更新。

- [x] 10.1 Manual integration test: trigger a coding session, verify code preview panel opens with correct file content
- [x] 10.2 Verify diff view renders correctly for modified files
- [x] 10.3 Verify file tab switching works across multiple files
- [x] 10.4 Verify panel mutual exclusion with workflow doc preview during workflow execution
- [x] 10.5 Verify theme switching while code preview is active
