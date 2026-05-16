# Implementation Plan: SDK Remote Integration

## Overview

This plan implements the SDK remote integration feature in dependency order: first backend type/constant changes, then adapter modifications, then new execution strategies, then session manager routing, then catalog updates, then frontend changes, and finally wiring and validation.

## Tasks

- [x] 1. Remove Qoder and add ExecutionMode constants
  - [x] 1.1 Remove Qoder entry from `remoteToolCatalog` in `remote_tool_catalog.go`
    - Delete the `"qoder"` key from the `remoteToolCatalog` map (if any residual reference exists)
    - Remove any Qoder-specific helper functions or conditional branches referencing "qoder"
    - _Requirements: 1.3, 1.7_

  - [x] 1.2 Add new ExecutionMode constants in `remote_sdk_types.go`
    - Add `ExecModeIFlowSDK ExecutionMode = "iflow-sdk"`
    - Add `ExecModeOpenCodeSDK ExecutionMode = "opencode-sdk"`
    - Add `ExecModeKiloSDK ExecutionMode = "kilo-sdk"`
    - _Requirements: 11.2, 11.3, 11.4_

  - [ ]* 1.3 Write property test for ExecutionMode constant uniqueness
    - **Property 8: SupportsRemote consistency with ExecutionMode**
    - Verify all defined ExecutionMode constants are distinct string values
    - **Validates: Requirements 11.2, 11.3, 11.4**

- [x] 2. Upgrade Gemini, Cursor, and CodeBuddy adapters to ExecModeSDK
  - [x] 2.1 Modify `GeminiAdapter` in `remote_tool_gemini.go`
    - Change `ExecutionMode()` to return `ExecModeSDK` instead of `ExecModePTY`
    - In `BuildCommand()`, add `--output-format`, `stream-json` to args
    - Add `-p` flag when prompt is provided (non-empty spec context)
    - Remove `Cols`/`Rows` from CommandSpec (not needed for SDK mode)
    - _Requirements: 3.1, 3.2, 3.3_

  - [x] 2.2 Modify `CursorAdapter` in `remote_tool_cursor.go`
    - Change `ExecutionMode()` to return `ExecModeSDK` instead of `ExecModePTY`
    - In `BuildCommand()`, add `-p`, `--output-format`, `stream-json` to args
    - Remove `Cols`/`Rows` from CommandSpec
    - Set `SupportsRemote: true` in the cursor catalog entry in `remote_tool_catalog.go`
    - _Requirements: 4.1, 4.2, 9.1_

  - [x] 2.3 Create `CodeBuddyAdapter` in `remote_tool_codebuddy.go` and add catalog entry
    - Create new `CodeBuddyAdapter` struct implementing `ProviderAdapter` interface
    - `ProviderName()` returns `"codebuddy"`
    - `ExecutionMode()` returns `ExecModeSDK`
    - `BuildCommand()` includes `-p`, `--output-format`, `stream-json` in args; binary name `codebuddy` (fallback `codebuddy-code`)
    - Add `"codebuddy"` entry to `remoteToolCatalog` in `remote_tool_catalog.go` with `SupportsRemote: true`, `UsesOpenAICompat: true`, `RequiresSessionConfig: true`
    - Add `"codebuddy"` to the `order` slice in `listRemoteToolMetadataForApp()`
    - Add `case "codebuddy"` to `remoteToolVisible()` returning `cfg.ShowCodeBuddy` (or equivalent config field)
    - _Requirements: 4b.1, 4b.2, 4b.3, 4b.5_

  - [ ]* 2.4 Write property test for stream-json CLI arguments
    - **Property 2: Stream-json tools include required CLI arguments**
    - Generate random LaunchSpec values, call BuildCommand() on Gemini, Cursor, and CodeBuddy adapters
    - Verify `--output-format` and `stream-json` are present in Args
    - Verify Cursor and CodeBuddy always include `-p`
    - **Validates: Requirements 3.2, 3.3, 4.2, 4b.3**

- [x] 3. Checkpoint — Verify Gemini/Cursor/CodeBuddy SDK adapters compile
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Modify iFlow adapter for ExecModeIFlowSDK
  - [x] 4.1 Modify `IFlowAdapter` in `remote_tool_iflow.go`
    - Change `ExecutionMode()` to return `ExecModeIFlowSDK`
    - In `BuildCommand()`, add `--experimental-acp` and `--port <PORT>` to args (use a free port finder or fixed offset)
    - Remove `Cols`/`Rows` from CommandSpec
    - _Requirements: 5.1, 5.2_

  - [x] 4.2 Create `remote_execution_iflow.go` with `IFlowSDKExecutionStrategy` and `IFlowSDKExecutionHandle`
    - Implement `IFlowSDKExecutionStrategy.Start()`: launch iFlow process, wait for ACP server ready, connect WebSocket to `ws://localhost:<PORT>/acp`
    - Implement `IFlowSDKExecutionHandle` with `PID()`, `Write()`, `Interrupt()`, `Kill()`, `Output()`, `Exit()`, `Close()`
    - `Write()` sends user text as ACP-formatted WebSocket message
    - `Output()` parses ACP messages (AssistantMessage, ToolCallMessage, PlanMessage, TaskFinishMessage) into human-readable text
    - Handle WebSocket connection failure with timeout/retry
    - _Requirements: 5.3, 5.4, 5.5, 5.6, 5.7, 12.1, 12.2, 12.3, 12.4_

  - [ ]* 4.3 Write property test for iFlow ACP arguments
    - **Property 12: iFlow BuildCommand includes ACP arguments**
    - Generate random LaunchSpec, verify BuildCommand includes `--experimental-acp` and `--port`
    - **Validates: Requirements 5.2**

  - [ ]* 4.4 Write property test for iFlow ACP message parsing
    - **Property 5: iFlow ACP WebSocket message parsing**
    - Generate random valid ACP messages, verify Output() emits non-empty text
    - **Validates: Requirements 5.5**

- [x] 5. Modify OpenCode adapter for ExecModeOpenCodeSDK
  - [x] 5.1 Modify `OpencodeAdapter` in `remote_tool_opencode.go`
    - Change `ExecutionMode()` to return `ExecModeOpenCodeSDK`
    - In `BuildCommand()`, configure server mode startup arguments
    - Remove `Cols`/`Rows` from CommandSpec
    - _Requirements: 6.1, 6.2_

  - [x] 5.2 Create `remote_execution_opencode.go` with `OpenCodeSDKExecutionStrategy` and `OpenCodeSDKExecutionHandle`
    - Implement `OpenCodeSDKExecutionStrategy.Start()`: launch OpenCode server, wait for HTTP ready, subscribe to SSE
    - Implement `OpenCodeSDKExecutionHandle` with full `ExecutionHandle` interface
    - `Write()` sends user prompt via HTTP API
    - `Output()` parses SSE events into human-readable text
    - Handle HTTP connection failure with timeout/retry
    - _Requirements: 6.3, 6.4, 6.5, 6.6, 6.7, 12.1, 12.2, 12.3, 12.4_

  - [ ]* 5.3 Write property test for OpenCode server mode arguments
    - **Property 13: OpenCode/Kilo BuildCommand configures server mode**
    - Generate random LaunchSpec, verify OpenCode BuildCommand configures server mode
    - **Validates: Requirements 6.2**

  - [ ]* 5.4 Write property test for OpenCode SSE event parsing
    - **Property 6: OpenCode/Kilo SSE event parsing**
    - Generate random valid SSE events, verify Output() emits non-empty text for meaningful events
    - **Validates: Requirements 6.5**

- [x] 6. Modify Kilo adapter for ExecModeKiloSDK
  - [x] 6.1 Modify `KiloAdapter` in `remote_tool_kilo.go`
    - Change `ExecutionMode()` to return `ExecModeKiloSDK`
    - In `BuildCommand()`, use `kilo serve` subcommand for headless server mode
    - Remove `Cols`/`Rows` from CommandSpec
    - _Requirements: 7.1, 7.2_

  - [x] 6.2 Create `remote_execution_kilo.go` with `KiloSDKExecutionStrategy` and `KiloSDKExecutionHandle`
    - Implement `KiloSDKExecutionStrategy.Start()`: launch `kilo serve`, wait for HTTP ready, subscribe to SSE
    - Implement `KiloSDKExecutionHandle` with full `ExecutionHandle` interface (similar to OpenCode)
    - `Write()` sends user prompt via HTTP API
    - `Output()` parses SSE events into human-readable text
    - _Requirements: 7.3, 7.4, 7.5, 7.6, 12.1, 12.2, 12.3, 12.4_

  - [ ]* 6.3 Write property test for Kilo serve arguments
    - **Property 13: OpenCode/Kilo BuildCommand configures server mode**
    - Generate random LaunchSpec, verify Kilo BuildCommand includes `serve`
    - **Validates: Requirements 7.2**

- [x] 7. Checkpoint — Verify all new execution strategies compile
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. Update Session Manager routing and output loops
  - [x] 8.1 Extend strategy selection switch in `RemoteSessionManager.Create()` in `remote_session_manager.go`
    - Add cases for `ExecModeIFlowSDK` → `NewIFlowSDKExecutionStrategy()`
    - Add cases for `ExecModeOpenCodeSDK` → `NewOpenCodeSDKExecutionStrategy()`
    - Add cases for `ExecModeKiloSDK` → `NewKiloSDKExecutionStrategy()`
    - _Requirements: 5.3, 6.3, 7.3, 11.5_

  - [x] 8.2 Add output loop dispatching for new SDK handle types in `RemoteSessionManager.Create()`
    - After session creation, detect `IFlowSDKExecutionHandle`, `OpenCodeSDKExecutionHandle`, `KiloSDKExecutionHandle`
    - Route each to an appropriate output loop (can reuse `runSDKOutputLoop` pattern or create dedicated loops)
    - _Requirements: 3.4, 4.3, 5.3, 6.3, 7.3_

  - [ ]* 8.3 Write property test for Session Manager routing
    - **Property 1: Session Manager routes ExecutionMode to correct strategy**
    - For each tool in catalog, verify the strategy type matches the ExecutionMode
    - **Validates: Requirements 3.4, 4.3, 5.3, 6.3, 7.3, 11.5**

- [x] 9. Update Tool Catalog for SupportsRemote and Kode experimental
  - [x] 9.1 Update `remoteToolCatalog` entries in `remote_tool_catalog.go`
    - Set `SupportsRemote: true` for cursor entry
    - Confirm `SupportsRemote: true` for codebuddy entry (set in task 2.3)
    - Confirm `SupportsRemote: false` for iflow, opencode, kilo, kode entries (initial phase)
    - Update `ReadinessHint` and `SmokeHint` for tools that changed to SDK mode (gemini, cursor, codebuddy)
    - _Requirements: 9.1, 9.2, 9.3_

  - [x] 9.2 Verify `remoteToolSupported()` rejects non-remote tools
    - Confirm the existing `remoteToolSupported()` function correctly uses `meta.SupportsRemote`
    - Verify `StartRemoteSessionForProject()` in `remote_mobile_launch.go` calls `remoteToolSupported()` before creating sessions
    - _Requirements: 9.4_

  - [x] 9.3 Add experimental indicator for Kode in catalog
    - Update Kode's `DisplayName` to include "(Experimental)" or add a metadata field for the frontend to render a badge
    - Confirm Kode adapter returns `ExecModePTY` and `SupportsRemote: false`
    - _Requirements: 8.1, 8.2, 8.3_

  - [ ]* 9.4 Write property test for SupportsRemote consistency
    - **Property 8: SupportsRemote consistency with ExecutionMode**
    - For all catalog entries, verify: if SupportsRemote=true then ExecutionMode != ExecModePTY; if ExecutionMode=ExecModePTY then SupportsRemote=false
    - **Validates: Requirements 9.1, 9.2**

  - [ ]* 9.5 Write property test for remote session rejection
    - **Property 9: Remote session rejection for non-remote tools**
    - For all catalog entries with SupportsRemote=false, verify remoteToolSupported() returns false
    - **Validates: Requirements 9.4**

- [x] 10. Checkpoint — Verify backend changes compile and pass tests
  - Ensure all tests pass, ask the user if questions arise.

- [x] 11. Frontend: Remove Qoder and add Cursor Agent
  - [x] 11.1 Update `TOOL_NAMES` in `frontend/src/App.tsx`
    - Remove `"qoder"` from TOOL_NAMES array
    - Add `"cursor"` to TOOL_NAMES array (if not already present)
    - _Requirements: 1.1, 2.1_

  - [x] 11.2 Remove Qoder UI elements from `frontend/src/App.tsx`
    - Remove Qoder sidebar tab rendering block
    - Remove Qoder icon import (`qoderIcon`) and image asset reference
    - Remove Qoder visibility toggle from global settings panel
    - _Requirements: 1.2, 1.4, 1.6_

  - [x] 11.3 Add Cursor Agent sidebar tab in `frontend/src/App.tsx`
    - Add Cursor Agent tab with appropriate icon, gated by `config.show_cursor`
    - Add Cursor Agent configuration panel rendering
    - _Requirements: 2.2, 2.3_

  - [x] 11.4 Update translation keys in all language bundles
    - Remove `"qoder"` and `"qoderDesc"` keys from en, zh-Hans, zh-Hant bundles
    - Add `"cursor"` and `"cursorDesc"` keys to all bundles with appropriate translations
    - _Requirements: 1.5, 2.4_

  - [ ]* 11.5 Write property test for translation key consistency
    - **Property 10: Translation key consistency across language bundles**
    - Verify no bundle contains "qoder"/"qoderDesc" and all bundles contain "cursor"/"cursorDesc"
    - **Validates: Requirements 1.5, 2.4**

- [x] 12. Frontend: Dynamic remote capability from backend metadata
  - [x] 12.1 Replace hardcoded `remoteCapableTools` with dynamic `supports_remote` check
    - In `frontend/src/App.tsx` (or relevant remote panel components), remove the hardcoded `remoteCapableTools` array
    - Use `RemoteToolMetadataView.supports_remote` from backend to determine remote capability
    - _Requirements: 10.4_

  - [x] 12.2 Hide remote UI for non-remote tools
    - When selected tool's `supports_remote` is false, hide remote mode toggle in launch area
    - When selected tool's `supports_remote` is false, hide remote settings panel
    - When `supports_remote` is true, show both toggle and settings panel
    - Update `useRemotePanel.ts` or relevant hook to incorporate `supports_remote` logic
    - _Requirements: 10.1, 10.2, 10.3_

  - [x] 12.3 Update `RemoteToolName` type in `frontend/src/components/remote/types.ts`
    - Ensure the type union does not include `"qoder"` (remove if present)
    - Confirm `"cursor"` is included in the union
    - _Requirements: 1.1, 2.1_

  - [ ]* 12.4 Write property test for remote UI visibility
    - **Property 11: Remote UI visibility based on supports_remote**
    - For each tool, verify remote UI visibility matches supports_remote field
    - **Validates: Requirements 10.1, 10.2, 10.3**

- [x] 13. Final checkpoint — Full integration verification
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Gemini, Cursor 和 CodeBuddy 复用现有 `SDKExecutionStrategy`（Claude 的 stream-json 协议）— 无需新策略
- CodeBuddy 需要新建 `ProviderAdapter` 和 catalog 条目（之前不在远程 catalog 中）
- iFlow, OpenCode, and Kilo each get their own ExecutionStrategy due to distinct protocols (ACP WebSocket, HTTP+SSE)
- Kode stays PTY with SupportsRemote=false per design decision D3
- iFlow, OpenCode, Kilo start with SupportsRemote=false; can be flipped to true after remote stability validation
- Property tests validate universal correctness properties from the design document
- Each task references specific requirements for traceability
