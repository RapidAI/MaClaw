# Implementation Plan: Workflow Document Project Persistence

## Overview

Implement dual-layer document persistence for MacLaw's workflow engine. The main file to modify is `gui/workflow_adapter_persistence.go`. Implementation proceeds bottom-up: pure helper functions first, then methods that depend on them, then integration into the existing workflow lifecycle, and finally tests.

## Tasks

- [x] 1. Implement helper functions and data types
  - [x] 1.1 Add `sanitizeWorkflowPhaseFileStem` function
    - Implement the sanitization logic: lowercase, replace runs of non-`[a-z0-9]` with single hyphen, no leading/trailing/consecutive hyphens
    - Return empty string for inputs that produce no valid characters
    - _Requirements: 6.3, 6.5_

  - [x] 1.2 Add `workflowTypeToKebab` function
    - Convert `WorkflowType` string constants to kebab-case by replacing underscores with hyphens
    - _Requirements: 8.2_

  - [x] 1.3 Add `knownPhaseFileNames` map and `workflowPhaseFileName` function
    - Define the complete mapping table (~110 phase IDs across 22 templates) as shown in design
    - Implement `workflowPhaseFileName`: lookup canonical phase ID in map, fall back to sanitized stem + `.md`, fall back to `"workflow-phase.md"`
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.6_

  - [x] 1.4 Add `resolveCollisionFreeDir` function
    - Scan existing date directories, return candidate path without suffix if not exists
    - Append numeric suffix `-2`, `-3`, ... up to `-100` for collisions
    - Fall back to Unix nanosecond timestamp suffix for extreme cases
    - _Requirements: 8.4_

  - [x] 1.5 Add `WorkflowManifest` and `ManifestPhaseEntry` structs
    - Define JSON-serializable structs with fields: workflow_type, template_name, started_at, completed_at, status, phases
    - `ManifestPhaseEntry`: phase_id, file_name, title
    - _Requirements: 2.3, 2.4_

  - [x] 1.6 Add new fields to `GUIWorkflowAdapter`
    - Add `activeWorkflowType WorkflowType` field
    - Add `workflowStartDate time.Time` field
    - Add `projectStorageDir string` field (cached resolved path)
    - These fields are protected by the existing `mu sync.RWMutex`
    - _Requirements: 4.3, 8.1_

- [x] 2. Implement Project_Storage methods
  - [x] 2.1 Implement `resolveProjectStorageDir` method
    - Read cached value under RLock, return if non-empty
    - Resolve project path via `workflowProjectPath()`; return empty if no workingDir
    - Read `activeWorkflowType` and `workflowStartDate` under RLock; return empty if type is empty
    - Compute `typeDir` via `workflowTypeToKebab`, `dateStr` via `Format("2006-01-02")`
    - Call `resolveCollisionFreeDir(baseDir, dateStr)` and cache result under Lock
    - _Requirements: 8.1, 8.3, 8.4_

  - [x] 2.2 Implement `publishToProjectStorage` method
    - Skip if content is empty (TrimSpace)
    - Resolve dir via `resolveProjectStorageDir()`; skip if empty
    - `os.MkdirAll(dir, 0755)` — log and return on error (non-blocking)
    - Resolve file name via `workflowPhaseFileName(phaseID)`
    - `os.WriteFile(filePath, []byte(content), 0644)` — log on error
    - Log success with path and byte count
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

  - [x] 2.3 Implement `writeWorkflowManifest` method
    - Resolve dir via `resolveProjectStorageDir()`; skip if empty
    - Build `WorkflowManifest` struct with all fields (type, template_name, timestamps in RFC3339, status, phases)
    - `json.MarshalIndent` with 2-space indent
    - `os.MkdirAll` + `os.WriteFile` to `workflow-manifest.json`
    - Log errors non-blocking
    - _Requirements: 2.3, 2.4, 2.5, 8.5_

- [x] 3. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Integration into workflow lifecycle
  - [x] 4.1 Set `activeWorkflowType` and `workflowStartDate` on workflow start
    - In the workflow start path (where `activeWorkflowID` is set), also set `activeWorkflowType` from the workflow state's Type and `workflowStartDate` to `time.Now()`
    - Reset `projectStorageDir` to empty string (force re-resolution for new workflow)
    - _Requirements: 4.2, 4.3, 5.1_

  - [x] 4.2 Call `publishToProjectStorage` from phase confirmation path
    - In `EmitDocUpdate` (or the equivalent phase confirmation handler), after `persistWorkflowDoc` succeeds, call `publishToProjectStorage(phaseID, content)`
    - Publishing failure must not block workflow execution
    - _Requirements: 1.1, 1.6_

  - [x] 4.3 Call `writeWorkflowManifest` on workflow completion/cancellation
    - On workflow completion: call `writeWorkflowManifest("completed", allConfirmedPhases)`
    - On workflow cancellation: call `writeWorkflowManifest("cancelled", confirmedPhasesBeforeCancellation)`
    - Build `[]ManifestPhaseEntry` from the workflow state's confirmed phases
    - _Requirements: 2.3, 2.5_

  - [x] 4.4 Modify `CleanPersistedWorkflowDocs` to clean Internal_Storage on successful completion
    - After all phases are published to Project_Storage on successful completion, remove the workflow-ID subdirectory from Internal_Storage via `os.RemoveAll`
    - Ensure `CleanPersistedWorkflowDocs` does NOT touch `docs/workflow/` (Project_Storage)
    - _Requirements: 2.1, 3.1, 3.3_

  - [x] 4.5 Reset workflow fields on completion/cancellation
    - Clear `activeWorkflowType`, `workflowStartDate`, and `projectStorageDir` when workflow ends
    - _Requirements: 4.4_

- [x] 5. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. Unit tests for helper functions
  - [x] 6.1 Write unit tests for `sanitizeWorkflowPhaseFileStem`
    - Test cases: normal input, all-whitespace, all-unicode, mixed case, consecutive special chars, leading/trailing specials, empty string
    - _Requirements: 6.3, 6.5_

  - [x] 6.2 Write unit tests for `workflowTypeToKebab`
    - Test all WorkflowType constants produce valid kebab-case output
    - _Requirements: 8.2_

  - [x] 6.3 Write unit tests for `workflowPhaseFileName`
    - Test known phase IDs from all 22 templates return correct numbered file names
    - Test unknown phase IDs are sanitized correctly
    - Test empty/whitespace-only input returns fallback
    - _Requirements: 6.1, 6.2, 6.4, 6.6_

  - [x] 6.4 Write unit tests for `resolveCollisionFreeDir`
    - Test with 0 existing dirs (no suffix)
    - Test with 1 existing dir (suffix -2)
    - Test with 5 existing dirs (suffix -6)
    - Use `t.TempDir()` for filesystem isolation
    - _Requirements: 8.4_

  - [x] 6.5 Write unit tests for `publishToProjectStorage` and `resolveProjectStorageDir`
    - Test publish creates correct directory structure and file content
    - Test publish with empty content is skipped
    - Test publish with empty workingDir is skipped
    - Test overwrite behavior (same phaseID, different content)
    - _Requirements: 1.1, 1.3, 1.4, 1.5_

  - [x] 6.6 Write unit tests for `writeWorkflowManifest`
    - Test manifest JSON structure correctness (fields, timestamps, phases array)
    - Test manifest with "completed" and "cancelled" status
    - _Requirements: 2.3, 2.4, 2.5_

  - [x] 6.7 Write unit tests for `CleanPersistedWorkflowDocs` preservation of Project_Storage
    - Create files in both Internal_Storage and Project_Storage paths
    - Call `CleanPersistedWorkflowDocs`
    - Verify Internal_Storage files are removed
    - Verify Project_Storage files are untouched
    - _Requirements: 2.1, 3.1_

- [x] 7. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 8. Property-based tests
  - [ ]* 8.1 Write property test for publish path correctness
    - **Property 1: Publish path correctness**
    - Generate random valid workingDir, workflowType, start date, phaseID
    - Verify published file path matches `{workingDir}/docs/workflow/{kebab(workflowType)}/{YYYY-MM-DD}/{workflowPhaseFileName(phaseID)}`
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - **Validates: Requirements 1.1, 8.1**

  - [ ]* 8.2 Write property test for publish content round-trip
    - **Property 2: Publish content round-trip**
    - Generate random non-empty content strings
    - Publish and read back, verify byte-for-byte identity
    - Publish same phaseID twice with different content, verify only latest is on disk
    - **Validates: Requirements 1.3, 1.4**

  - [ ]* 8.3 Write property test for clean preserves Project_Storage
    - **Property 3: Clean preserves Project_Storage**
    - Generate random files in Project_Storage directory
    - Call `CleanPersistedWorkflowDocs`
    - Verify all Project_Storage files unchanged (same paths, content, count)
    - **Validates: Requirements 2.1**

  - [ ]* 8.4 Write property test for manifest structure correctness
    - **Property 4: Manifest structure correctness**
    - Generate random workflow metadata (type, start time, completion time, phase list)
    - Call `writeWorkflowManifest`, read back and unmarshal JSON
    - Verify all fields present, timestamps in ISO 8601, status correct, phases array matches input
    - **Validates: Requirements 2.3, 2.4**

  - [ ]* 8.5 Write property test for workingDir invariant across phase transitions
    - **Property 5: workingDir invariant across phase transitions**
    - Simulate sequence of phase transitions (set workingDir, then call publish N times)
    - Verify workingDir value remains constant throughout
    - **Validates: Requirements 4.3**

  - [ ]* 8.6 Write property test for known phase ID file name completeness
    - **Property 6: Known phase ID file name completeness**
    - For every phase ID in all 22 registered templates, verify `workflowPhaseFileName` returns a non-empty string matching `^[0-9]{2}-[a-z][a-z0-9-]*\.md$`
    - **Validates: Requirements 6.1, 6.2, 6.6**

  - [ ]* 8.7 Write property test for sanitization output invariant
    - **Property 7: Sanitization output invariant**
    - Generate arbitrary strings via `rapid.String()`
    - Verify `sanitizeWorkflowPhaseFileStem` returns either empty string or matches `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`
    - **Validates: Requirements 6.3, 6.5**

  - [ ]* 8.8 Write property test for read/write path consistency
    - **Property 8: Read/write path consistency**
    - For any phaseID, verify `readPersistedDoc` resolves the same file path as `persistWorkflowDoc` given identical adapter state
    - **Validates: Requirements 7.4**

  - [ ]* 8.9 Write property test for WorkflowType kebab-case output
    - **Property 9: WorkflowType kebab-case output**
    - For all WorkflowType constants, verify `workflowTypeToKebab` output matches `^[a-z]+(-[a-z]+)*$`
    - **Validates: Requirements 8.2**

  - [ ]* 8.10 Write property test for date collision avoidance
    - **Property 10: Date collision avoidance**
    - Generate N (0 ≤ N ≤ 99) pre-existing date directories
    - Verify `resolveCollisionFreeDir` returns a path that does not exist on disk
    - For N=0, verify no suffix; for N≥1, verify suffix is `-{N+1}`
    - **Validates: Requirements 8.4**

- [x] 9. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- All new code goes in `gui/workflow_adapter_persistence.go` (and a corresponding `_test.go` file)
- The project uses `pgregory.net/rapid` for property-based testing (already a dependency)
- Error handling follows log-and-continue pattern — publishing failures never block workflow execution

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.4", "1.5", "1.6"] },
    { "id": 1, "tasks": ["1.3"] },
    { "id": 2, "tasks": ["2.1"] },
    { "id": 3, "tasks": ["2.2", "2.3"] },
    { "id": 4, "tasks": ["4.1"] },
    { "id": 5, "tasks": ["4.2", "4.3", "4.4", "4.5"] },
    { "id": 6, "tasks": ["6.1", "6.2", "6.3", "6.4", "6.5", "6.6", "6.7"] },
    { "id": 7, "tasks": ["8.1", "8.2", "8.3", "8.4", "8.5", "8.6", "8.7", "8.8", "8.9", "8.10"] }
  ]
}
```
