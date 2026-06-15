# Implementation Plan: Coding Phase File Preview

## Overview

This implementation connects the CodingSubAgent's file tracking infrastructure to the WorkflowDocPreview panel, enabling real-time file change visualization during the implementation phase. The approach follows a layered strategy: Backend utilities (path normalization, binary detection) → Snapshot store → Diff computation → Event emission via WorkflowAdapter → Frontend components (FileChangePanel, DiffViewer) → Integration with existing WorkflowDocPreview. Each layer builds on the previous with checkpoints to validate integration.

## Tasks

- [x] 1. Implement path normalization and binary detection utilities
  - [x] 1.1 Create `gui/file_path_normalize.go` with path normalization and binary detection
    - Implement `NormalizeFilePathForEvent(filePath, projectPath string) string` — converts absolute paths to project-relative forward-slash format, returns empty string if path is outside project root
    - Implement `IsBinaryFile(content []byte) bool` — checks for null bytes in first 8192 bytes
    - Handle Windows backslash conversion, symlink resolution, `..` segment removal
    - _Requirements: 6.4, 6.5, 7.2_

  - [ ]* 1.2 Write property tests for path normalization (Property 10)
    - **Property 10: Path Normalization and Safety**
    - Generate paths with various formats (Windows backslash, relative, absolute, with `..` segments, symlinks)
    - Verify output uses forward slashes, is relative to project root, contains no `..` segments
    - Verify paths resolving outside project root are excluded
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 6.4, 6.5**

  - [ ]* 1.3 Write property test for binary file detection (Property 12)
    - **Property 12: Binary File Detection**
    - Generate byte slices with null bytes at various positions within first 8192 bytes
    - Verify detection returns true when null byte present in first 8192 bytes
    - Verify detection returns false for pure UTF-8 text content
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 7.2**

- [x] 2. Implement FileSnapshotStore
  - [x] 2.1 Create `gui/file_snapshot_store.go` with pre-modification content capture
    - Implement `FileSnapshotStore` struct with `sync.RWMutex`, `map[string]fileSnapshot`, and `maxFiles int` (default 50)
    - Implement `CaptureSnapshots(projectPath string, filePaths []string)` — reads files from disk, skips files > 2MB, binary files, and unreadable files
    - Implement `GetSnapshot(absPath string) (fileSnapshot, bool)`
    - Implement `Clear()` to reset store for next task
    - Store error reasons: "permission_denied", "file_too_large", "not_found", "binary"
    - _Requirements: 2.1, 2.2, 2.6_

  - [ ]* 2.2 Write property test for snapshot map size invariant (Property 3)
    - **Property 3: Snapshot Map Size Invariant**
    - Generate task file lists of size 0-200
    - Verify snapshot store contains at most 50 entries after CaptureSnapshots completes
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 2.2**

  - [ ]* 2.3 Write unit tests for FileSnapshotStore edge cases
    - Test permission denied handling
    - Test file > 2MB limit (store error "file_too_large")
    - Test binary file detection during capture
    - Test Clear() resets all state
    - _Requirements: 2.2, 2.6_

- [x] 3. Implement DiffComputer
  - [x] 3.1 Create `gui/diff_computer.go` with unified diff computation
    - Implement `DiffComputer` struct with `contextLines int` (default 3) and `maxDiffLines int` (default 500)
    - Implement `ComputeFileDiffs(projectPath string, snapshots *FileSnapshotStore, filesModified []string, filesCreated []string) []FileChangeDiff`
    - For modified files: compare snapshot content with current disk content
    - For created files: generate full-addition diff
    - For files in snapshot but missing on disk: generate full-deletion diff
    - Infer language from file extension
    - Truncate diffs exceeding maxDiffLines, set `Truncated=true` and `TotalLines`
    - Use `NormalizeFilePathForEvent` for all paths in output
    - _Requirements: 1.2, 1.3, 1.4, 2.3, 2.4, 2.5_

  - [ ]* 3.2 Write property test for diff round-trip (Property 2)
    - **Property 2: Diff Computation Round-Trip**
    - Generate random text file pairs (original, modified) under 2MB
    - Compute unified diff and apply it to original
    - Verify result equals modified content
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 1.2, 2.3**

  - [ ]* 3.3 Write property test for diff truncation invariant (Property 4)
    - **Property 4: Diff Truncation Invariant**
    - Generate file pairs producing diffs of 1-2000 lines
    - Verify diffs exceeding 500 lines are truncated at exactly 500 lines
    - Verify `Truncated` field is true and `TotalLines` reflects actual count
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 2.5, 3.6, 7.3**

  - [ ]* 3.4 Write unit tests for DiffComputer
    - Test modified file with known input/output pair
    - Test created file (all lines are additions)
    - Test deleted file (all lines are deletions)
    - Test empty result when no changes
    - Test binary file produces "Binary file changed" placeholder
    - _Requirements: 1.2, 1.3, 1.4, 7.2_

- [x] 4. Checkpoint - Ensure all backend utility tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Implement event emission via GUIWorkflowAdapter
  - [x] 5.1 Add `EmitFileChanges` and `EmitFileActivity` methods to `gui/workflow_adapter.go`
    - Define `FileChangesPayload` struct with `UserID`, `PhaseID` ("implementation"), `TaskID`, `TaskTitle` (max 200 chars), `Files` (max 200 entries), `Truncated` bool
    - Define `FileChangeItem` struct with `Path` (max 500 chars), `ChangeType`, `Diff`, `Language`
    - Define `FileActivityPayload` struct with `UserID`, `PhaseID`, `TaskID`, `FilePath`, `ChangeType`
    - Implement `EmitFileChanges(userID string, payload FileChangesPayload) error` — emits `workflow:file_changes` event via `runtime.EventsEmit`
    - Implement `EmitFileActivity(userID string, payload FileActivityPayload) error` — emits `workflow:file_activity` event via `runtime.EventsEmit`
    - Truncate `Files` array to 200 entries if exceeded, set `Truncated=true`
    - Truncate `TaskTitle` to 200 characters
    - Skip emission if `a.app.ctx == nil`
    - _Requirements: 1.1, 1.5, 1.6, 6.1, 6.2, 6.3, 6.6_

  - [ ]* 5.2 Write property test for event payload structural completeness (Property 1)
    - **Property 1: Event Payload Structural Completeness**
    - Generate random `FileChangesPayload`, serialize to JSON, deserialize
    - Verify all required fields present with correct types
    - Verify `phase_id` always equals "implementation"
    - Verify `task_title` does not exceed 200 characters
    - Verify each file entry has valid `path` (max 500 chars, forward slashes, no `..`), `change_type`, `diff`, `language`
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 6.1, 6.2, 6.3**

  - [ ]* 5.3 Write property test for file list truncation (Property 11)
    - **Property 11: File List Truncation at 200 Entries**
    - Generate file lists of size 1-500
    - Verify emitted `files` array contains at most 200 entries
    - Verify `truncated` field is true when source list > 200
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 6.6**

  - [ ]* 5.4 Write property test for task title truncation (Property 8)
    - **Property 8: Task Title Truncation**
    - Generate strings of length 0-500
    - Verify displayed title is full string if length ≤ 80, or first 80 chars + "…" if > 80
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 3.9**

- [ ] 6. Integrate snapshot capture and diff computation into SubAgentTaskRunner
  - [ ] 6.1 Extend `gui/coding_subagent_orchestrator.go` — add snapshot and diff logic to `runTaskHandle`
    - Before `runTaskWithRecover`: create `FileSnapshotStore`, call `CaptureSnapshots` with task's file list
    - After task completion (in existing `artifactsRecorded` block): create `DiffComputer`, call `ComputeFileDiffs` with snapshots + result's `FilesModified`/`FilesCreated`
    - Build `FileChangesPayload` from diffs and call `adapter.EmitFileChanges`
    - Handle task failure/cancellation: emit partial file changes with appropriate task status
    - _Requirements: 1.1, 1.5, 2.1, 2.3_

  - [ ] 6.2 Add `workflow:file_activity` emission during tool execution in `codingSubAgentCallbacks`
    - In `executeToolWithOutcome` (or equivalent tool execution callback), detect `write_file`/`edit_file` tool calls
    - Extract file path from tool arguments, normalize with `NormalizeFilePathForEvent`
    - Emit `workflow:file_activity` event with path and change type ("added" for write_file new files, "modified" for edit_file)
    - _Requirements: 4.1_

  - [ ]* 6.3 Write unit tests for integration
    - Test snapshot capture before task execution
    - Test diff computation after task completion
    - Test file_activity emission during tool execution
    - Test empty file changes event when task produces no changes
    - _Requirements: 1.1, 1.5, 1.6, 4.1_

- [ ] 7. Checkpoint - Ensure all backend tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 8. Implement frontend FileChangePanel component
  - [ ] 8.1 Create `gui/frontend/src/components/ai/FileChangePanel.tsx`
    - Implement `FileChangeState` with `taskGroups`, `pendingFiles`, `isExecuting`, `currentTaskTitle`
    - Render flat file list grouped by change type: added (green `+`), modified (yellow `~`), deleted (red `-`)
    - Display summary header: "N files added, M files modified, K files deleted"
    - Display task title above each file change group, truncated to 80 chars with ellipsis
    - Render file entries collapsed by default (path + change type indicator only)
    - Handle click to expand/collapse entries
    - Accumulate file changes across tasks with latest-wins semantics per file path
    - Display "waiting for code changes" placeholder when no changes yet
    - Display "in progress" spinner during task execution
    - Display pending files (dimmed appearance) from `workflow:file_activity` events
    - Replace pending entries with actual diffs when `workflow:file_changes` arrives
    - Handle task failure/cancellation indicators
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.7, 3.8, 3.9, 4.2, 4.3, 4.4, 7.1, 7.4_

  - [ ]* 8.2 Write property test for file list grouping order (Property 5)
    - **Property 5: File List Grouping Order**
    - Generate random file change sets with mixed change types
    - Verify rendered order is: all "added" first, then "modified", then "deleted"
    - **Validates: Requirements 3.1**

  - [ ]* 8.3 Write property test for summary header count correctness (Property 6)
    - **Property 6: Summary Header Count Correctness**
    - Generate multi-task file change sequences
    - Verify summary counts equal actual unique file paths per category after latest-wins dedup
    - **Validates: Requirements 3.7**

  - [ ]* 8.4 Write property test for file accumulation latest-state semantics (Property 7)
    - **Property 7: File Accumulation Latest-State Semantics**
    - Generate sequences where same file path appears in multiple events
    - Verify displayed diff is from most recently received event
    - **Validates: Requirements 3.8**

  - [ ]* 8.5 Write property test for pending file deduplication (Property 9)
    - **Property 9: Pending File Deduplication**
    - Generate file_activity event sequences with repeated paths
    - Verify pending list contains at most one entry per unique path with latest change type
    - **Validates: Requirements 4.3**

- [ ] 9. Implement frontend DiffViewer component
  - [ ] 9.1 Create `gui/frontend/src/components/ai/DiffViewer.tsx`
    - Accept `diff` string, `language` string, and theme props
    - Parse unified diff format line by line
    - Render additions (`+` prefix) with green background
    - Render deletions (`-` prefix) with red background
    - Render hunk headers (`@@`) with blue/muted styling
    - Render context lines with default styling
    - Display truncation message when diff is truncated ("Diff truncated: showing first 500 of N lines")
    - Handle malformed diff gracefully (display raw text)
    - _Requirements: 3.4, 3.6, 7.3_

  - [ ]* 9.2 Write unit tests for DiffViewer
    - Test addition lines get green background
    - Test deletion lines get red background
    - Test hunk headers get muted styling
    - Test truncation message display
    - Test malformed diff renders as raw text
    - _Requirements: 3.4, 3.6_

- [ ] 10. Extend WorkflowDocPreview with dual-mode display
  - [ ] 10.1 Extend `gui/frontend/src/components/ai/WorkflowDocPreview.tsx` with file-change mode
    - Add `displayMode` state: "markdown" | "file-changes"
    - Switch to "file-changes" when `activePhaseID === "implementation"`
    - Switch to "markdown" when user clicks previous phase cards (requirements, tech_design, task_breakdown)
    - Switch to "file-changes" when user clicks implementation phase card
    - Subscribe to `workflow:file_changes` and `workflow:file_activity` Wails events
    - Render `FileChangePanel` when in "file-changes" mode
    - Preserve file change state (expanded/collapsed, scroll position) across phase navigation
    - Clear all state on workflow reset or new workflow start
    - Display "waiting for code changes" placeholder when in implementation phase with no changes
    - _Requirements: 1.7, 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 7.5_

  - [ ]* 10.2 Write unit tests for WorkflowDocPreview mode switching
    - Test automatic switch to file-changes mode on implementation phase
    - Test switch to markdown mode on previous phase card click
    - Test state preservation across navigation
    - Test state clearing on workflow reset
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.6_

- [ ] 11. Checkpoint - Ensure all frontend tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 12. Final integration and wiring
  - [ ] 12.1 Wire WorkflowAdapter to SubAgentTaskRunner for event emission
    - Ensure `SubAgentTaskRunner` has access to `GUIWorkflowAdapter` (via `IMMessageHandler` or direct reference)
    - Pass `userID` through the task execution chain for event emission
    - Verify `workflow:file_changes` events are emitted after each task completion
    - Verify `workflow:file_activity` events are emitted during tool execution
    - _Requirements: 1.1, 1.5, 4.1_

  - [ ] 12.2 Verify end-to-end flow
    - Verify: SubAgent executes task → snapshots captured → diffs computed → event emitted → frontend renders
    - Verify: Multi-task accumulation with overlapping file paths shows latest state
    - Verify: Task failure emits partial changes with failure indicator
    - Verify: Phase navigation between markdown and file-change modes works correctly
    - _Requirements: 1.1, 1.5, 3.8, 5.2, 5.3, 7.1_

  - [ ]* 12.3 Write integration tests
    - Test full flow from task execution to frontend rendering
    - Test multi-task accumulation with overlapping file paths
    - Test task failure with partial file changes
    - Test phase navigation between modes
    - _Requirements: 1.1, 3.8, 5.2, 7.1_

- [ ] 13. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document using `pgregory.net/rapid`
- Unit tests validate specific examples and edge cases
- Go backend tests use `*_test.go` files alongside source
- TypeScript frontend tests use `__tests__/` directories
- The implementation uses Wails `runtime.EventsEmit` for backend→frontend communication and `EventsOn` for frontend event subscription

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "2.1"] },
    { "id": 2, "tasks": ["2.2", "2.3", "3.1"] },
    { "id": 3, "tasks": ["3.2", "3.3", "3.4", "5.1"] },
    { "id": 4, "tasks": ["5.2", "5.3", "5.4", "6.1"] },
    { "id": 5, "tasks": ["6.2", "6.3", "8.1"] },
    { "id": 6, "tasks": ["8.2", "8.3", "8.4", "8.5", "9.1"] },
    { "id": 7, "tasks": ["9.2", "10.1"] },
    { "id": 8, "tasks": ["10.2", "12.1"] },
    { "id": 9, "tasks": ["12.2", "12.3"] }
  ]
}
```
