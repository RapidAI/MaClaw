# Requirements Document

## Introduction

This feature bridges the gap between the CodingSubAgent's file tracking capabilities and the WorkflowDocPreview panel in the desktop AI assistant. During the implementation phase of the coding workflow, the right-side document preview panel will display a file list with content diffs (additions, deletions, modifications) produced by the SubAgent, replacing the current empty "暂无文档内容" placeholder.

The backend already tracks file changes (FilesModified, FilesCreated) and emits structured CodingAgentEvent JSON with diff summaries. The frontend WorkflowDocPreview panel currently only renders Markdown documents for requirements/design/task phases. This feature adds a new "file change preview" display mode for the implementation phase.

## Glossary

- **CodingSubAgent**: A lightweight coding executor that runs tasks in a clean context using `corelib/agent.RunLoop`, tracking all file modifications and creations during execution.
- **WorkflowDocPreview**: The right-side panel in the desktop AI assistant that displays workflow phase documents (requirements, design, tasks) as rendered Markdown.
- **SubAgentTaskRunner**: The bridge between `TaskExecutionOrchestrator` and `CodingSubAgent`, executing tasks sequentially and collecting results.
- **CodeFileEvent**: A structured event emitted per file after SubAgent task completion, containing file path, change type, and content metadata.
- **DiffSummary**: A structured summary of all file changes produced by the SubAgent, containing file list with change types (added/modified/deleted).
- **Implementation_Phase**: The coding workflow phase (`implementation`) where the SubAgent executes tasks and produces file changes.
- **File_Change_Preview**: The new display mode in WorkflowDocPreview that shows a file tree with inline diffs during the implementation phase.
- **Unified_Diff**: A standard diff format showing added lines (prefixed with `+`), removed lines (prefixed with `-`), and context lines around changes.

## Requirements

### Requirement 1: Emit File Change Events to WorkflowDocPreview

**User Story:** As a developer using the desktop AI assistant, I want to see file changes produced by the CodingSubAgent displayed in the right-side preview panel, so that I can review code modifications without switching to a file explorer.

#### Acceptance Criteria

1. WHEN the CodingSubAgent completes a task and produces file changes, THE WorkflowAdapter SHALL emit a `workflow:file_changes` event containing the list of changed files (maximum 200 files per event) with their change types (added, modified, deleted) within 2 seconds of task completion.
2. WHEN the CodingSubAgent modifies a file, THE WorkflowAdapter SHALL compute a unified diff between the file's content before and after modification and include the diff in the emitted event.
3. WHEN the CodingSubAgent creates a new file, THE WorkflowAdapter SHALL include the full file content as an addition diff in the emitted event, subject to the 500-line truncation limit defined in Requirement 2.
4. WHEN the CodingSubAgent deletes a file, THE WorkflowAdapter SHALL include the full former content as a deletion diff in the emitted event, subject to the 500-line truncation limit defined in Requirement 2.
5. THE WorkflowAdapter SHALL emit file change events incrementally after each individual task completion in the order tasks complete, not only after all tasks finish.
6. IF the CodingSubAgent completes a task without producing any file changes, THEN THE WorkflowAdapter SHALL emit a `workflow:file_changes` event with an empty files array and the corresponding task_id and task_title.
7. WHILE the workflow is in the implementation phase and no file changes have been produced yet, THE WorkflowDocPreview SHALL display a "waiting for code changes" placeholder instead of "暂无文档内容".

### Requirement 2: Capture File Content Before Modification

**User Story:** As a developer, I want to see what changed in each file (before vs after), so that I can understand the impact of each coding task.

#### Acceptance Criteria

1. WHEN the CodingSubAgent begins executing a task that involves modifying existing files, THE SubAgentTaskRunner SHALL capture a snapshot of each target file's content before the task executes by reading the file from disk and storing its content in memory.
2. THE SubAgentTaskRunner SHALL store pre-modification snapshots in a map keyed by absolute file path, limited to files listed in the task's `Files` field (from the task breakdown document), with a maximum of 50 files per task.
3. WHEN a task completes, THE SubAgentTaskRunner SHALL compute unified diffs by comparing pre-modification snapshots with post-modification file content read from disk, using 3 lines of context around each change hunk.
4. IF a file listed in `FilesModified` no longer exists on disk after task completion, THEN THE SubAgentTaskRunner SHALL treat the file as deleted and generate a full-deletion diff from the pre-modification snapshot.
5. THE SubAgentTaskRunner SHALL limit individual file diff output to 500 lines; if the computed diff exceeds 500 lines, it SHALL be truncated at line 500 with a trailing marker indicating the total line count.
6. IF a pre-modification snapshot cannot be captured (file does not exist, permission denied, or file is larger than 2MB), THEN THE SubAgentTaskRunner SHALL skip the snapshot for that file and generate a "content unavailable" placeholder diff after task completion.

### Requirement 3: File Change Preview Panel Display

**User Story:** As a developer, I want the preview panel to show a navigable file list with expandable diffs, so that I can quickly scan which files changed and drill into specific changes.

#### Acceptance Criteria

1. WHEN the WorkflowDocPreview receives a `workflow:file_changes` event during the implementation phase, THE WorkflowDocPreview SHALL render a flat file list showing all changed files grouped by change type in the order: added, modified, deleted.
2. THE WorkflowDocPreview SHALL display each file entry with a change type indicator: green `+` icon for added files, yellow `~` icon for modified files, red `-` icon for deleted files.
3. THE WorkflowDocPreview SHALL render all file entries in a collapsed state by default, showing only the file path and change type indicator.
4. WHEN a user clicks on a collapsed file entry, THE WorkflowDocPreview SHALL expand the entry to show the unified diff content with additions highlighted with a green background and deletions highlighted with a red background.
5. WHEN a user clicks on an expanded file entry, THE WorkflowDocPreview SHALL collapse the entry back to showing only the file path and change type indicator.
6. IF a diff exceeds 500 lines, THEN THE WorkflowDocPreview SHALL display only the first 500 lines followed by a truncation message indicating the total line count and the file's full path for manual inspection.
7. THE WorkflowDocPreview SHALL display a summary header showing total counts: "N files added, M files modified, K files deleted".
8. WHEN multiple tasks have completed, THE WorkflowDocPreview SHALL accumulate file changes across tasks, showing the latest state of each file (most recent diff replaces earlier diffs for the same file path).
9. THE WorkflowDocPreview SHALL display the task title above each file change group, truncated to 80 characters with an ellipsis if the title exceeds that length.

### Requirement 4: Real-Time Progress During Task Execution

**User Story:** As a developer, I want to see file changes appear in the preview panel as they happen during task execution, so that I can monitor coding progress in real time.

#### Acceptance Criteria

1. WHEN the CodingSubAgent writes or modifies a file during task execution, THE system SHALL emit a lightweight `workflow:file_activity` event containing the file path and change type within 2 seconds of the file operation completing.
2. WHILE a task is executing, THE WorkflowDocPreview SHALL display an "in progress" spinner indicator next to the current task title in the file change panel.
3. WHEN a `workflow:file_activity` event is received during task execution, THE WorkflowDocPreview SHALL add the file to the file tree with a "pending" state (dimmed appearance, no diff content) until the full diff is available after task completion. IF the same file path already exists in the pending list, THE WorkflowDocPreview SHALL update the existing entry's change type without creating a duplicate.
4. WHEN the task completes and full diffs are available via `workflow:file_changes`, THE WorkflowDocPreview SHALL replace all pending file entries for that task with their actual diff content.
5. IF a `workflow:file_activity` event is not received within 30 seconds of the last event while a task is still executing, THE WorkflowDocPreview SHALL continue displaying the "in progress" indicator without timeout (the task may be performing long-running operations like compilation).

### Requirement 5: Integration with Existing Workflow Phase Navigation

**User Story:** As a developer, I want the file change preview to coexist with the existing phase document display, so that I can switch between viewing requirements/design documents and implementation file changes.

#### Acceptance Criteria

1. WHEN the workflow transitions from the task_breakdown phase to the implementation phase, THE WorkflowDocPreview SHALL automatically switch from Markdown document display mode to file change preview mode within 500 milliseconds of the phase transition event.
2. WHEN the user clicks on a previous phase card (requirements, tech_design, task_breakdown) in the progress board, THE WorkflowDocPreview SHALL switch to Markdown document display mode showing that phase's document within 300 milliseconds of the click event.
3. WHEN the user clicks on the implementation phase card, THE WorkflowDocPreview SHALL switch to file change preview mode showing accumulated file changes within 300 milliseconds of the click event.
4. THE WorkflowDocPreview SHALL preserve the file change state (expanded/collapsed entries, scroll position) when the user navigates away from the implementation phase and returns by clicking the implementation phase card.
5. IF the user clicks the implementation phase card and no file changes have been produced yet, THEN THE WorkflowDocPreview SHALL display the "waiting for code changes" placeholder defined in Requirement 1 criterion 7.
6. WHEN the workflow is reset or a new workflow starts, THE WorkflowDocPreview SHALL clear all preserved file change state (expanded/collapsed entries, scroll position, accumulated diffs) and revert to the initial display mode appropriate for the first phase.

### Requirement 6: Event Data Structure and Transport

**User Story:** As a frontend developer, I want well-defined event structures for file change data, so that I can reliably parse and render the information.

#### Acceptance Criteria

1. THE `workflow:file_changes` event payload SHALL contain: `user_id` (string), `phase_id` ("implementation"), `task_id` (string), `task_title` (string, maximum 200 characters), and `files` (array of file change objects, maximum 200 entries).
2. Each file change object SHALL contain: `path` (string, project-relative, maximum 500 characters), `change_type` ("added" | "modified" | "deleted"), `diff` (string, unified diff format), and `language` (string, inferred from file extension, or "plaintext" when the extension is unrecognized or absent).
3. THE `workflow:file_activity` event payload SHALL contain: `user_id` (string), `phase_id` ("implementation"), `task_id` (string), `file_path` (string, project-relative, maximum 500 characters), and `change_type` ("added" | "modified" | "deleted").
4. THE system SHALL normalize all file paths to use forward slashes and be relative to the project root before including them in events, resolving any symbolic links and removing any `..` segments.
5. IF a file's resolved absolute path does not start with the project root directory path, THEN THE system SHALL exclude the file from emitted events.
6. IF the `files` array in a `workflow:file_changes` event exceeds 200 entries, THEN THE system SHALL truncate the array to 200 entries and include a `truncated` boolean field set to `true` in the event payload.

### Requirement 7: Error Handling and Edge Cases

**User Story:** As a developer, I want the file change preview to handle edge cases gracefully, so that the panel remains useful even when unexpected situations occur.

#### Acceptance Criteria

1. IF the CodingSubAgent fails a task (status=failed), THEN THE WorkflowDocPreview SHALL display the partial file changes produced before the failure with a visible "task failed" indicator next to the task title, and if zero file changes were captured before the failure, SHALL display the "task failed" indicator with a "No changes captured" label in place of the file tree.
2. IF a file contains one or more null bytes within the first 8192 bytes, THEN THE system SHALL display "Binary file changed" with the file path instead of attempting to render a diff.
3. IF a diff exceeds 500 lines, THEN THE system SHALL truncate the diff at line 500 and display a "Diff truncated (showing first 500 of N lines)" message with the file's full project-relative path for manual inspection.
4. IF the SubAgent is cancelled mid-task, THEN THE WorkflowDocPreview SHALL retain any file changes already captured and display a visible "task cancelled" indicator next to the task title, and if zero file changes were captured before cancellation, SHALL display the "task cancelled" indicator with a "No changes captured" label in place of the file tree.
5. WHEN the workflow is reset or a new workflow starts, THE WorkflowDocPreview SHALL clear all accumulated file change state including the file tree entries, diff content, task status indicators, and the summary header counts.
6. IF a file cannot be read during diff computation (due to permission denial, file lock, or the file being unavailable), THEN THE system SHALL display "Unable to read file: [reason]" for that file entry and continue processing remaining files without interruption.
