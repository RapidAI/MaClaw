# Implementation Plan: VE File Sharing Directories

## Overview

This implementation enables the Virtual Employee (VE) to send files from configured local directories to requesting users. The approach follows defense-in-depth: AppConfig extension → Path Validator → Tool Policy conditional unblocking → Wails Bindings → VE Agent Callbacks integration → Frontend UI. Each layer is independently testable and builds on the previous.

## Tasks

- [x] 1. Extend AppConfig and implement Path Validator
  - [x] 1.1 Add `VEAllowedDirectories` field to AppConfig
    - Add `VEAllowedDirectories []string` field with `json:"ve_allowed_directories,omitempty"` tag to `corelib/app_config.go`
    - Ensure the field is not synced to Hub (local-only, already handled by local config persistence)
    - _Requirements: 2.3, 2.4_

  - [ ]* 1.2 Write property test for configuration serialization round-trip
    - **Property 7: Configuration serialization round-trip**
    - Generate random lists of valid absolute directory path strings, marshal to JSON via AppConfig, unmarshal back, verify identical list
    - Use `rapid` library, minimum 100 iterations
    - Test file: `corelib/app_config_test.go`
    - **Validates: Requirements 2.3**

  - [x] 1.3 Implement Path Validator (`gui/ve_path_validator.go`)
    - Implement `ValidateVEFilePath(requestedPath string, allowedDirs []string) (canonicalPath string, err error)`
    - Implement `IsWithinAllowedDirs(requestedPath string, allowedDirs []string) (canonicalPath string, err error)`
    - Use `filepath.EvalSymlinks` + `filepath.Abs` for canonical resolution
    - Perform case-insensitive prefix comparison on Windows using `strings.EqualFold` on normalized paths
    - Return specific error messages in Chinese per the design error table
    - Handle: empty path, non-existent file, path resolving outside allowed dirs, directory path (for send_file)
    - _Requirements: 3.3, 3.4, 3.5, 3.6, 5.1, 5.2, 5.3, 5.4, 5.5, 5.6_

  - [ ]* 1.4 Write property test for path containment after canonical resolution
    - **Property 2: Path containment after canonical resolution**
    - Generate random directory trees, create files inside/outside allowed dirs, verify accept/reject regardless of `..` segments, symlinks, or relative components
    - Test file: `gui/ve_path_validator_test.go`
    - **Validates: Requirements 3.3, 3.4, 3.5, 3.6, 5.1, 5.2, 5.5**

  - [ ]* 1.5 Write property test for Windows path format and case-insensitivity
    - **Property 3: Windows path format and case-insensitivity**
    - Generate paths with random casing, mixed forward/backward slashes, drive letters; verify validation result unchanged
    - Test file: `gui/ve_path_validator_windows_test.go` (build-tagged for Windows)
    - **Validates: Requirements 5.3, 5.4**

- [x] 2. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 3. Implement VE Tool Policy extension and sensitive file integration
  - [x] 3.1 Extend VE Tool Policy to conditionally unblock `send_file`
    - Modify `gui/ve_tool_policy.go` to add `filterToolsForVEWithConfig(tools []map[string]interface{}, allowedDirs []string) []map[string]interface{}`
    - When `len(allowedDirs) > 0`, remove `send_file` from the blocked set; also unblock `list_directory` and `read_file` scoped to allowed dirs
    - When `len(allowedDirs) == 0`, keep `send_file` blocked (zero-config = zero-risk)
    - _Requirements: 3.1, 3.2, 6.1, 6.2_

  - [ ]* 3.2 Write property test for conditional tool unblocking
    - **Property 1: Conditional tool unblocking**
    - Generate random non-empty/empty directory lists, verify `send_file` presence/absence in output tool list
    - Test file: `gui/ve_tool_policy_test.go`
    - **Validates: Requirements 3.1, 3.2, 6.1, 6.2**

  - [x] 3.3 Integrate sensitive file check with path validator
    - In the execution layer, after `ValidateVEFilePath` passes, call existing `vePathIsSensitive` (from `app_ve_tools.go`)
    - Reject with Chinese error message `[error] 该文件包含敏感信息，无法发送` if sensitive
    - Ensure case-insensitive pattern matching (`.ENV`, `.Pem`, `.KEY` all blocked)
    - _Requirements: 8.1, 8.2, 8.3, 8.4_

  - [ ]* 3.4 Write property test for sensitive file blocking within allowed directories
    - **Property 4: Sensitive file blocking within allowed directories**
    - Generate sensitive file names (random casing of .env/.pem/.key/id_rsa) within allowed dirs, verify rejection
    - Test file: `gui/ve_path_validator_test.go`
    - **Validates: Requirements 8.1, 8.2, 8.3, 8.4**

- [x] 4. Implement Wails Bindings for directory management
  - [x] 4.1 Implement `SelectVEAllowedDirectory` Wails binding
    - Add to `gui/app_ve.go` (or appropriate VE bindings file)
    - Open native OS directory picker via Wails runtime dialog
    - Return selected path on success, empty string on cancel, error on failure
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

  - [x] 4.2 Implement `GetVEAllowedDirectories` and `SetVEAllowedDirectories` bindings
    - `GetVEAllowedDirectories()` returns current list from AppConfig
    - `SetVEAllowedDirectories(dirs []string)` persists to local config file
    - Accept paths that don't exist on filesystem (owner may add for disconnected drives)
    - _Requirements: 7.5, 7.6, 7.7, 2.1, 2.5, 2.6_

  - [ ]* 4.3 Write property test for duplicate directory detection (case-insensitive)
    - **Property 6: Duplicate directory detection (case-insensitive)**
    - Generate random paths, attempt to add twice with different casing, verify rejection and list unchanged
    - Test file: `gui/ve_allowed_dirs_test.go`
    - **Validates: Requirements 1.6**

- [x] 5. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. Extend VE Agent Callbacks for file operations
  - [x] 6.1 Modify `BuildTools` to conditionally include file tools
    - In `veAgentCallbacks.BuildTools()`, read `VEAllowedDirectories` from config
    - Call `filterToolsForVEWithConfig` instead of the current filter to conditionally include `send_file`, `list_directory`, `read_file`
    - _Requirements: 4.1, 4.2, 6.1_

  - [x] 6.2 Modify `BuildSystemPrompt` to declare file-sending capability
    - When `VEAllowedDirectories` is non-empty, inject capability declaration into VE system prompt
    - List each configured directory path
    - Include instructions: use `list_directory` to browse, `read_file` to inspect, then `send_file` to deliver
    - Include 50 MB size limit notice and sensitive file restriction notice
    - _Requirements: 4.5_

  - [ ]* 6.3 Write property test for system prompt capability declaration
    - **Property 8: System prompt capability declaration**
    - Generate random non-empty dir lists, verify prompt contains file-sending capability and lists all paths
    - Test file: `gui/app_ve_handler_test.go`
    - **Validates: Requirements 4.5**

  - [x] 6.4 Modify `ExecuteTool` for defense-in-depth path validation
    - For `send_file`: validate path param not empty → `ValidateVEFilePath` → `vePathIsSensitive` → file size ≤ 50 MB → delegate to handler
    - For `read_file`: validate path → `ValidateVEFilePath` → `vePathIsSensitive` → delegate
    - For `list_directory`: validate path → `IsWithinAllowedDirs` → delegate
    - Return Chinese error messages per the design error table for each failure case
    - _Requirements: 4.3, 4.6, 6.3, 6.4, 6.5_

  - [ ]* 6.5 Write property test for execution-layer path validation
    - **Property 5: Execution-layer path validation for all VE file operations**
    - Generate random paths inside/outside allowed dirs, verify ExecuteTool behavior (reject outside, allow inside)
    - Test file: `gui/app_ve_handler_test.go`
    - **Validates: Requirements 4.1, 4.2, 6.3, 6.4**

  - [ ]* 6.6 Write property test for file size limit enforcement
    - **Property 9: File size limit enforcement**
    - Generate random file sizes around 50 MB boundary, verify accept/reject
    - Test file: `gui/ve_path_validator_test.go`
    - **Validates: Requirements 4.3**

- [x] 7. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. Implement Frontend Directory Configuration UI
  - [x] 8.1 Add "允许访问目录" section to VirtualEmployeeSettingsPanel
    - Add new section in `gui/frontend/src/components/settings/VirtualEmployeeSettingsPanel.tsx`
    - Display directory list with full absolute paths
    - Add "添加目录" button that calls `SelectVEAllowedDirectory` Wails binding
    - Add remove button per directory entry that calls `SetVEAllowedDirectories` with updated list
    - Implement duplicate detection (case-insensitive on Windows) with warning display
    - Load directories on mount via `GetVEAllowedDirectories`
    - No maximum limit on directory count
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 2.2_

  - [ ]* 8.2 Write unit tests for directory configuration UI
    - Test add directory flow (mock Wails binding)
    - Test remove directory flow
    - Test cancel dialog (no change)
    - Test duplicate warning display
    - Test empty state rendering
    - Test file: `gui/frontend/src/components/settings/VirtualEmployeeSettingsPanel.test.tsx`
    - _Requirements: 1.1-1.8_

- [x] 9. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- The implementation follows defense-in-depth: directory containment check → sensitive file check → execution-layer validation
- All error messages are in Chinese as specified in the design
- The `rapid` library (already used in the codebase) is used for property-based testing

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "1.3"] },
    { "id": 2, "tasks": ["1.4", "1.5", "3.1"] },
    { "id": 3, "tasks": ["3.2", "3.3", "4.1"] },
    { "id": 4, "tasks": ["3.4", "4.2"] },
    { "id": 5, "tasks": ["4.3", "6.1"] },
    { "id": 6, "tasks": ["6.2", "6.4"] },
    { "id": 7, "tasks": ["6.3", "6.5", "6.6"] },
    { "id": 8, "tasks": ["8.1"] },
    { "id": 9, "tasks": ["8.2"] }
  ]
}
```
