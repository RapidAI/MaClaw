# Requirements Document

## Introduction

This feature enables the Virtual Employee (VE / 数字员工) to send files from the machine owner's local filesystem to requesting users. Currently, the VE refuses all file-sending requests because `send_file` is blocked by the VE tool policy. This feature adds a configuration UI for the machine owner to declare which directories the VE is allowed to access, and conditionally unblocks file-sending capability within those boundaries.

## Glossary

- **VE**: Virtual Employee (数字员工) — an AI assistant running on the machine owner's computer that serves remote users via IM channels
- **Allowed_Directories**: The list of local filesystem directories that the machine owner has explicitly authorized the VE to access and send files from
- **Settings_Panel**: The VE settings UI component (`VirtualEmployeeSettingsPanel.tsx`) in the application's Settings → 数字员工 tab
- **Directory_Picker**: The native OS directory browser dialog provided by the Wails runtime for selecting filesystem directories
- **VE_Tool_Policy**: The deny-list based tool filtering system (`ve_tool_policy.go`) that controls which tools are available to VE sessions
- **Send_File_Tool**: The `send_file` tool that reads a local file and sends it to a user via IM channel
- **Path_Validator**: The component responsible for verifying that a requested file path falls within the Allowed_Directories (and their subdirectories)
- **Local_Config**: The machine-specific configuration storage (JSON file) for settings that should not be synced to the Hub

## Requirements

### Requirement 1: Directory List Configuration UI

**User Story:** As a machine owner, I want to configure which directories the VE can access, so that I can control what files the VE is allowed to send to remote users.

#### Acceptance Criteria

1. THE Settings_Panel SHALL display an "允许访问目录" (Allowed Access Directories) section within the VE settings tab
2. WHEN the machine owner clicks the "添加目录" (Add Directory) button, THE Settings_Panel SHALL open the native OS Directory_Picker dialog
3. WHEN the machine owner selects a directory via the Directory_Picker, THE Settings_Panel SHALL add the selected directory path to the Allowed_Directories list and persist the change
4. WHEN the machine owner cancels the Directory_Picker dialog without selecting a directory, THE Settings_Panel SHALL take no action and leave the Allowed_Directories list unchanged
5. WHEN the machine owner clicks the remove button next to a directory entry, THE Settings_Panel SHALL remove that directory from the Allowed_Directories list and persist the change
6. IF the machine owner selects a directory that is already in the Allowed_Directories list (case-insensitive comparison on Windows), THEN THE Settings_Panel SHALL display a duplicate warning and not add the entry
7. THE Settings_Panel SHALL display each directory in the Allowed_Directories list with its full absolute path
8. THE Settings_Panel SHALL NOT impose a maximum limit on the number of directories in the Allowed_Directories list

### Requirement 2: Directory Configuration Persistence

**User Story:** As a machine owner, I want my allowed directory configuration to persist across application restarts, so that I don't have to reconfigure it every time.

#### Acceptance Criteria

1. WHEN the Allowed_Directories list is modified (add or remove), THE Local_Config SHALL persist the updated list to the local configuration file within 1 second
2. WHEN the application starts, THE Settings_Panel SHALL load the Allowed_Directories list from the Local_Config and display it
3. THE Local_Config SHALL store the Allowed_Directories as a JSON array of absolute path strings under a dedicated key (e.g., `ve_allowed_directories`) in the local configuration file
4. THE Local_Config SHALL NOT sync the Allowed_Directories to the Hub API (directories are machine-specific)
5. IF the local configuration file is missing or contains invalid JSON for the Allowed_Directories field, THEN THE application SHALL treat the Allowed_Directories list as empty and not crash
6. IF a persisted directory path no longer exists on the filesystem at application startup, THEN THE application SHALL retain it in the list (the owner may reconnect the drive or restore the directory later)

### Requirement 3: VE File Access Authorization

**User Story:** As a machine owner, I want the VE to only access files within my configured directories, so that my other files remain private and secure.

#### Acceptance Criteria

1. WHEN the Allowed_Directories list contains one or more entries, THE VE_Tool_Policy SHALL conditionally unblock the Send_File_Tool for VE sessions
2. WHILE the Allowed_Directories list is empty, THE VE_Tool_Policy SHALL keep the Send_File_Tool blocked for VE sessions
3. WHEN the VE attempts to send a file, THE Path_Validator SHALL resolve the requested file path to its canonical absolute form (resolving symbolic links and `..` segments) and verify that the resulting path starts with the canonical absolute form of one of the Allowed_Directories
4. IF the resolved canonical path of the requested file does not have any Allowed_Directory's canonical path as a prefix, THEN THE Path_Validator SHALL reject the request and return an error message indicating the file is not in an allowed directory
5. IF the resolved canonical path of the requested file falls outside all Allowed_Directories after canonicalization (including cases where `..` segments or symbolic links would escape the allowed boundaries), THEN THE Path_Validator SHALL reject the request
6. THE Path_Validator SHALL resolve both the requested file path and each Allowed_Directory entry to their canonical absolute forms (resolving symbolic links in both) before performing the directory prefix containment check
7. IF the requested file does not exist on the filesystem, THEN THE Path_Validator SHALL reject the request and return an error message indicating the file was not found

### Requirement 4: VE File Search and Send Capability

**User Story:** As a remote user, I want to ask the VE for files (e.g., templates, documents), so that I can receive them without bothering the machine owner directly.

#### Acceptance Criteria

1. WHEN the Allowed_Directories list is non-empty, THE VE SHALL have access to the `list_directory` tool scoped to the Allowed_Directories for file discovery
2. WHEN the Allowed_Directories list is non-empty, THE VE SHALL have access to the `read_file` tool scoped to the Allowed_Directories for file content inspection
3. WHEN the VE identifies a matching file within the Allowed_Directories and the file size is at most 50 MB, THE VE SHALL use the Send_File_Tool to send the file to the requesting user
4. WHEN the VE cannot find a matching file within the Allowed_Directories, THE VE SHALL inform the requesting user that the file was not found in the available directories
5. IF the Allowed_Directories list is non-empty, THEN THE VE system prompt SHALL declare the file-sending capability, listing the configured directory paths and instructing the VE to use `list_directory` and `read_file` within those directories before sending
6. IF the Send_File_Tool fails to deliver a file (due to file access error, size exceeding 50 MB, or IM channel delivery failure), THEN THE VE SHALL inform the requesting user that the file could not be sent and include the reason for the failure

### Requirement 5: Path Traversal Prevention

**User Story:** As a machine owner, I want the system to prevent any path traversal attacks, so that the VE cannot be tricked into accessing files outside the allowed directories.

#### Acceptance Criteria

1. THE Path_Validator SHALL convert all requested paths to their canonical absolute form using OS-level path resolution (e.g., `filepath.EvalSymlinks` + `filepath.Abs` in Go) before performing containment checks
2. THE Path_Validator SHALL reject paths that, after canonicalization, resolve outside all Allowed_Directories — regardless of whether the original path contained `..` segments
3. THE Path_Validator SHALL handle Windows path formats correctly, including drive letters (e.g., `C:\`), UNC paths (e.g., `\\server\share`), and mixed forward/backward slashes
4. THE Path_Validator SHALL perform case-insensitive path prefix comparison on Windows (where the filesystem is case-insensitive by default)
5. IF a symbolic link within an Allowed_Directory resolves to a target outside all Allowed_Directories, THEN THE Path_Validator SHALL reject access to that symbolic link target
6. IF the requested path does not exist on the filesystem (and therefore cannot be canonicalized), THEN THE Path_Validator SHALL reject the request and return an error indicating the file was not found

### Requirement 6: VE Tool Policy Integration

**User Story:** As a system developer, I want the file-sending capability to integrate cleanly with the existing VE tool policy, so that security is maintained through defense-in-depth.

#### Acceptance Criteria

1. WHEN the Allowed_Directories list is non-empty, THE VE_Tool_Policy SHALL remove `send_file` from the `veBlockedTools` deny-list for the current session
2. WHILE the Allowed_Directories list is empty, THE VE_Tool_Policy SHALL keep `send_file` in the `veBlockedTools` deny-list
3. WHEN the VE_Tool_Policy execution layer (`ExecuteTool` in `veAgentCallbacks`) receives a `send_file` tool call, THE execution layer SHALL invoke the Path_Validator to verify the file path before delegating to the Send_File_Tool handler
4. IF the Path_Validator rejects a file path at the execution layer, THEN THE execution layer SHALL return an error message in Chinese (e.g., "[error] 文件不在允许访问的目录中") and SHALL NOT execute the Send_File_Tool handler
5. IF the `send_file` tool call contains an empty or missing `path` parameter, THEN THE execution layer SHALL return an error message indicating the path is required

### Requirement 7: Wails Backend Binding for Directory Selection

**User Story:** As a frontend developer, I want a Wails binding to open the native directory picker, so that the UI can let users select directories without manual text input.

#### Acceptance Criteria

1. THE application SHALL expose a Wails binding (e.g., `SelectVEAllowedDirectory`) that opens the native OS directory picker dialog and returns a result to the calling frontend code
2. WHEN the user selects a directory in the dialog, THE binding SHALL return the selected directory's absolute path as a string
3. WHEN the user cancels the dialog, THE binding SHALL return an empty string and no error
4. IF the native directory picker dialog fails to open, THEN THE binding SHALL return an empty string and an error value indicating the failure reason
5. THE application SHALL expose a Wails binding (e.g., `GetVEAllowedDirectories`) that returns the current Allowed_Directories list as an array of absolute path strings
6. THE application SHALL expose a Wails binding (e.g., `SetVEAllowedDirectories`) that accepts an array of absolute path strings and persists the updated Allowed_Directories list to Local_Config
7. IF `SetVEAllowedDirectories` receives paths that do not exist on the filesystem, THEN THE binding SHALL still accept and persist them (the owner may add directories for drives not currently connected)

### Requirement 8: Sensitive File Protection Within Allowed Directories

**User Story:** As a machine owner, I want the VE to still block access to sensitive files even within allowed directories, so that credentials and private keys are never exposed.

#### Acceptance Criteria

1. WHILE a file is within an Allowed_Directory, IF the file matches the existing `vePathIsSensitive` patterns (`.env`, `.env.local`, `.env.production`, private keys, `.pem`, `.key`, `.p12`, `.pfx`, `id_rsa`, `id_ed25519`, etc.), THEN THE VE SHALL reject access to that file
2. THE existing sensitive file detection logic (`vePathIsSensitive` in `app_ve_tools.go`) SHALL apply as a second validation layer to all VE file operations including the Send_File_Tool, after the Path_Validator directory containment check passes
3. IF the VE attempts to send a file that passes the Path_Validator directory check but matches a sensitive file pattern, THEN THE VE SHALL return an error message in Chinese (e.g., "[error] 该文件包含敏感信息，无法发送") and SHALL NOT send the file
4. THE sensitive file check SHALL apply regardless of the file extension casing (e.g., `.ENV`, `.Pem`, `.KEY` are all blocked)
