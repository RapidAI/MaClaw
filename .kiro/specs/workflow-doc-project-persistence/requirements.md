# Requirements Document

## Introduction

Improve MacLaw's workflow phase document persistence to make deliverables accessible, version-controllable, and useful as project documentation after workflow completion. Currently, workflow documents are stored in a hidden `.maclaw/workflow/{workflowID}/` directory, deleted when a new workflow starts, and effectively orphaned after the workflow completes. This feature introduces a user-accessible project-level persistence layer that copies finalized workflow documents to a predictable, visible location within the project directory.

## Glossary

- **WorkflowEngine**: The core state machine managing workflow lifecycle (creation, phase advancement, cancellation, persistence)
- **GUIWorkflowAdapter**: The GUI-layer workflow adapter bridging WorkflowEngine to the frontend, responsible for event emission and document persistence
- **workingDir**: The locked working directory in GUIWorkflowAdapter, determining the root path for document persistence
- **activeWorkflowID**: The ID of the currently active workflow instance, used for namespace isolation of documents from different workflow sessions
- **PhaseOutput**: The content produced by a workflow phase, stored in `WorkflowState.PhaseOutputs` by phase ID
- **phaseFileName**: The function mapping phase IDs to predictable file names (e.g., requirements → 01-requirements.md)
- **projectDocsDir**: The user-accessible directory where finalized workflow documents are published: `{projectPath}/docs/workflow/{workflowType}/`
- **workflowManifest**: A JSON index file (`workflow-manifest.json`) in projectDocsDir recording metadata about the workflow run (timestamps, phases completed, template type)
- **Internal_Storage**: The hidden `.maclaw/workflow/{workflowID}/` directory used during active workflow execution for intermediate persistence
- **Project_Storage**: The visible `docs/workflow/{workflowType}/` directory where finalized documents are published for user access and version control

## Requirements

### Requirement 1: Publish Finalized Documents to User-Accessible Location

**User Story:** As a user, I want workflow phase documents to be published to a visible project directory when each phase is confirmed, so that I can find, reference, and version-control them alongside my project code.

#### Acceptance Criteria

1. WHEN a workflow phase is confirmed by the user (advancePhase), THE GUIWorkflowAdapter SHALL copy the phase document from Internal_Storage to Project_Storage at `{workingDir}/docs/workflow/{workflowType}/{YYYY-MM-DD}/{phaseFileName}`
2. WHEN the document is published to Project_Storage, THE GUIWorkflowAdapter SHALL create the target directory hierarchy (MkdirAll with 0755 permissions) if it does not exist
3. THE published document SHALL be an exact byte-for-byte copy of the confirmed phase output content (no truncation, no preamble stripping beyond what was already applied during internal persistence)
4. WHEN a phase document is updated after initial confirmation (user requests modifications and re-confirms), THE GUIWorkflowAdapter SHALL overwrite the previously published file in Project_Storage with the updated content
5. IF workingDir is empty (no project context), THEN THE GUIWorkflowAdapter SHALL skip publishing to Project_Storage and only persist to Internal_Storage
6. IF writing to Project_Storage fails (directory creation or file write error), THEN THE GUIWorkflowAdapter SHALL log the error with the full target path and continue workflow execution without interruption (publishing failure SHALL NOT block phase advancement or workflow completion)

### Requirement 2: Preserve Completed Workflow Documents Across Sessions

**User Story:** As a user, I want completed workflow documents to survive across workflow sessions, so that starting a new workflow does not destroy deliverables from previous workflows.

#### Acceptance Criteria

1. WHEN CleanPersistedWorkflowDocs is called (new workflow starting), THE GUIWorkflowAdapter SHALL only remove files from Internal_Storage (`.maclaw/workflow/` directory), not from Project_Storage (`docs/workflow/`)
2. WHEN a new workflow of the same type starts, THE GUIWorkflowAdapter SHALL publish to a timestamped subdirectory within Project_Storage: `{workingDir}/docs/workflow/{workflowType}/{YYYY-MM-DD}/` to avoid overwriting previous runs
3. WHEN a workflow completes all phases successfully, THE GUIWorkflowAdapter SHALL write a workflow-manifest.json to the Project_Storage date subdirectory containing: workflow type (string), start timestamp (ISO 8601), completion timestamp (ISO 8601), list of phase files (array of objects each containing `phase_id`, `file_name`, and `title`), and workflow status set to "completed"
4. THE workflow-manifest.json SHALL use ISO 8601 format (e.g., `2026-05-01T14:30:00+08:00`) for all timestamp fields and include a `template_name` field containing the workflow template display name resolved from the application's current locale setting
5. IF a workflow is cancelled before completion, THEN THE GUIWorkflowAdapter SHALL preserve any already-published phase documents in Project_Storage and write a workflow-manifest.json with status set to "cancelled" and the list of phase files limited to those phases that were confirmed before cancellation
6. IF writing to Project_Storage fails (directory creation or file write error), THEN THE GUIWorkflowAdapter SHALL log the error with the full target path and continue workflow execution without interruption (publishing failure SHALL NOT block phase advancement or workflow completion)

### Requirement 3: Internal Storage Lifecycle Management

**User Story:** As a user, I want the hidden internal storage to be managed automatically without manual cleanup, so that stale intermediate files do not accumulate indefinitely.

#### Acceptance Criteria

1. WHEN a new workflow starts, THE GUIWorkflowAdapter SHALL remove all files and subdirectories from Internal_Storage (`.maclaw/workflow/`) to prevent stale documents from leaking into the new workflow
2. WHILE a workflow is active, THE GUIWorkflowAdapter SHALL persist phase documents to `{workingDir}/.maclaw/workflow/{activeWorkflowID}/{phaseFileName}` for intermediate storage and preview panel display
3. WHEN a workflow completes successfully AND all confirmed phase documents have been published to Project_Storage, THE GUIWorkflowAdapter SHALL remove the workflow-ID subdirectory from Internal_Storage
4. IF the application crashes during a workflow, THEN THE Internal_Storage files SHALL remain on disk for recovery (cleanup only happens on explicit new workflow start or successful completion)
5. IF removal of Internal_Storage files or subdirectories fails (permission error, file locked), THEN THE GUIWorkflowAdapter SHALL log the error and continue without blocking workflow startup or completion
6. WHEN removing subdirectories from Internal_Storage, THE GUIWorkflowAdapter SHALL use os.RemoveAll for workflow-ID subdirectories; WHEN removing legacy flat-path files, THE GUIWorkflowAdapter SHALL only remove files with `.md` or `.txt` extensions

### Requirement 4: workingDir Correctly Set from User-Specified Path

**User Story:** As a user, I want to specify the project directory in my message (e.g., "在 d:\workprj\snake 下开发贪吃蛇游戏"), so that all workflow documents are saved in the correct project location.

#### Acceptance Criteria

1. WHEN a user specifies a project target directory in their message, THE GUIWorkflowAdapter SHALL set workingDir to the specified directory path before the first phase agent loop executes
2. WHEN a workflow starts via the confirmation panel, THE GUIWorkflowAdapter SHALL set workingDir to the confirmed project path (LastProjectPath from pending confirmation)
3. WHILE a workflow is active, THE GUIWorkflowAdapter SHALL preserve the workingDir value across all phase transitions without allowing unrelated operations to overwrite it
4. WHEN a workflow is cancelled or completed, THE GUIWorkflowAdapter SHALL reset workingDir to empty string via ResetWorkingDir()
5. IF the user does not specify a project directory, THEN THE GUIWorkflowAdapter SHALL fall back to GetCurrentProjectPath() as the workingDir
6. WHEN SetWorkingDir is called, THE GUIWorkflowAdapter SHALL also call engine.SetProjectPath(userID, dir) to persist the project path in the workflow state for recovery after crash

### Requirement 5: activeWorkflowID Instance Isolation

**User Story:** As a user, I want each workflow instance to have its own internal document directory, so that concurrent or sequential workflow sessions do not contaminate each other's intermediate files.

#### Acceptance Criteria

1. WHEN EmitPhaseUpdate is called with a non-nil WorkflowState whose Status is WorkflowActive and whose ID is non-empty, THE GUIWorkflowAdapter SHALL set activeWorkflowID to the workflow state's ID
2. WHEN EmitPhaseUpdate is called with a nil WorkflowState OR a WorkflowState whose Status is not WorkflowActive, THE GUIWorkflowAdapter SHALL clear activeWorkflowID to empty string
3. THE activeWorkflowID SHALL be used as a subdirectory name under `.maclaw/workflow/` to isolate intermediate documents, producing the full path `{workingDir}/.maclaw/workflow/{activeWorkflowID}/{phaseFileName}`
4. WHEN workingDir is set but activeWorkflowID is empty, THE workflowDocDir() method SHALL return `{workingDir}/.maclaw/workflow/` as backward-compatible fallback
5. IF workingDir is empty, THEN THE workflowDocDir() method SHALL return empty string and persistWorkflowDoc SHALL skip writing without error

### Requirement 6: File Name Mapping Consistency Across All Templates

**User Story:** As a user, I want phase document file names to be predictable and consistent across all workflow templates, so that I can easily locate specific deliverables in Project_Storage.

#### Acceptance Criteria

1. THE workflowPhaseFileName function SHALL map known coding phase IDs to numbered file names: requirements → 01-requirements.md, tech_design → 02-technical-design.md, task_breakdown → 03-task-breakdown.md
2. THE workflowPhaseFileName function SHALL map ops-maintenance phase IDs to numbered file names: ops_intake → 01-ops-intake.md, readonly_collection → 02-readonly-collection.md, artifact_plan → 03-maintenance-artifacts.md, risk_policy → 04-risk-policy.md, controlled_execution → 05-controlled-execution.md
3. WHEN a phase ID is not in the known mapping, THE workflowPhaseFileName function SHALL sanitize the phase ID by converting to lowercase, replacing each run of one or more non-lowercase-letter non-digit characters with a single hyphen, stripping leading and trailing hyphens, and appending the .md extension
4. IF the sanitization of a phase ID produces an empty string (e.g., the input contains only whitespace or non-ASCII characters), THEN THE workflowPhaseFileName function SHALL return the fallback file name "workflow-phase.md"
5. THE sanitizeWorkflowPhaseFileStem function SHALL produce file stems containing only lowercase ASCII letters (a-z), digits (0-9), and hyphens, with no leading hyphen, no trailing hyphen, and no consecutive hyphens
6. FOR every phase ID defined in any registered workflow template, THE workflowPhaseFileName function SHALL produce a non-empty file name that contains only lowercase ASCII letters, digits, hyphens, a single dot separator, and the extension "md"

### Requirement 7: Document Read-Back Verification for Preview Panel

**User Story:** As a user, I want the preview panel to always show the actual persisted content, so that what I see matches what is saved on disk.

#### Acceptance Criteria

1. WHEN persistWorkflowDoc successfully writes a file (os.WriteFile returns no error), THE EmitDocUpdate method SHALL read back the file content via readPersistedDoc within the same method invocation
2. WHEN readPersistedDoc returns non-empty content, THE EmitDocUpdate method SHALL use the read-back content (not the in-memory content) as the payload for the frontend event
3. IF readPersistedDoc fails (file not found or read error) or returns an empty string, THEN THE EmitDocUpdate method SHALL fall back to the in-memory content for the frontend event
4. THE readPersistedDoc method SHALL resolve the file path using the same workflowDocDir() and workflowPhaseFileName() functions as persistWorkflowDoc, with the phaseID canonicalized via canonicalWorkflowPhaseID before being passed to either function
5. WHEN persistWorkflowDoc writes a file, THE GUIWorkflowAdapter SHALL log the full file path and byte count at info level for debugging purposes
6. WHEN EmitDocUpdate receives content, THE method SHALL strip conversational preamble (text before the first Markdown heading) from the content before passing it to persistWorkflowDoc

### Requirement 8: Project_Storage Directory Structure Convention

**User Story:** As a user, I want a clear, predictable directory structure for published workflow documents, so that I can navigate them without needing the application.

#### Acceptance Criteria

1. THE Project_Storage directory structure SHALL follow the pattern: `{projectPath}/docs/workflow/{workflowType}/{YYYY-MM-DD}/{phaseFileName}`
2. THE workflowType directory name SHALL be the kebab-case workflow type identifier (e.g., coding, product-design, presentation-design, business-plan)
3. THE date subdirectory SHALL use the workflow start date in local timezone YYYY-MM-DD format to distinguish multiple runs of the same workflow type
4. IF two workflows of the same type start on the same date, THEN THE System SHALL scan existing date directories and append the next available numeric suffix (e.g., 2026-05-01-2, 2026-05-01-3) to avoid collisions
5. THE workflow-manifest.json SHALL be placed in the date subdirectory alongside the phase document files
