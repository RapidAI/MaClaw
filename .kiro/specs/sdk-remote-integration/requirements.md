# Requirements Document

## Introduction

本需求文档描述对桌面程序（MaClaw）的工具集成改造，涵盖以下核心变更：
1. 移除 Qoder 工具（前端 UI、后端 catalog、所有引用）
2. 将 Cursor Agent 添加到前端 TOOL_NAMES 并确认后端 catalog 完整性
3. 将具备 SDK/headless 模式的工具（Gemini、Cursor、iFlow、OpenCode、Kilo）从 PTY 模式升级为 SDK 模式
4. 远程模式仅对有 SDK 集成的工具开放，纯 PTY 工具不支持远程
5. 不支持远程的工具在启动区隐藏远程相关选项与界面

## Glossary

- **App**: MaClaw 桌面应用程序主体，包含前端 React UI 和后端 Go 服务
- **Tool_Catalog**: 后端 `remoteToolCatalog` map，存储所有工具的 `RemoteToolMetadata` 元数据
- **TOOL_NAMES**: 前端 `App.tsx` 中定义的工具名称常量数组，控制 UI 中显示哪些工具 tab
- **Remote_Capable_Tools**: 前端 `remoteCapableTools` 常量数组，控制哪些工具显示远程模式选项
- **Provider_Adapter**: 后端 Go 接口，每个工具实现 `ProviderName()`、`ExecutionMode()`、`BuildCommand()` 方法
- **Execution_Strategy**: 后端执行策略，根据 `ExecutionMode` 选择 PTY、SDK 或 Codex-SDK 策略启动工具进程
- **SDK_Mode**: 工具以 headless 方式运行，通过结构化 JSON/JSONL stdin/stdout 通信，而非原始 PTY 字节流
- **PTY_Mode**: 工具以伪终端交互方式运行，通过原始终端字节流通信
- **Execution_Handle**: 后端进程句柄接口，提供 `Write()`、`Output()`、`Exit()`、`Kill()` 等方法
- **Session_Manager**: 后端 `RemoteSessionManager`，根据 `ExecutionMode()` 选择执行策略并管理会话生命周期
- **Gemini_CLI**: Google Gemini CLI 工具，headless 模式使用 `--output-format stream-json` 输出 JSONL 事件流
- **Cursor_Agent**: Cursor Agent CLI 工具，headless 模式使用 `-p --output-format stream-json`，协议类似 Claude
- **IFlow_CLI**: iFlow CLI 工具，SDK 模式通过 ACP WebSocket 协议通信
- **OpenCode_CLI**: OpenCode CLI 工具，SDK 模式通过 HTTP server + SSE 事件流通信
- **Kilo_CLI**: Kilo CLI 工具（OpenCode fork），有 `kilo run` 和 `kilo serve` headless server 模式
- **CodeBuddy_CLI**: CodeBuddy CLI 工具（腾讯云代码助手），headless 模式使用 `-p --output-format stream-json`，协议与 Claude Code 完全兼容
- **Kode_CLI**: Kode CLI 工具，SDK 成熟度存疑，保持 PTY 模式并标记为实验性
- **Output_Pipeline**: 后端输出处理管道，将工具输出转换为预览行、摘要和事件
- **SupportsRemote**: `RemoteToolMetadata` 中的布尔字段，标识工具是否支持远程模式

## Requirements

### Requirement 1: 移除 Qoder 工具

**User Story:** 作为开发者，我希望从应用中完全移除 Qoder 工具，以便简化工具列表并减少维护负担。

#### Acceptance Criteria

1. WHEN the App starts, THE TOOL_NAMES SHALL NOT contain the "qoder" entry
2. WHEN the App renders the sidebar, THE App SHALL NOT display a Qoder tab or icon
3. THE Tool_Catalog SHALL NOT contain a "qoder" entry in the `remoteToolCatalog` map
4. WHEN a user opens the global settings panel, THE App SHALL NOT display a Qoder visibility toggle
5. THE App SHALL remove all Qoder-specific translation keys ("qoder", "qoderDesc") from every language bundle
6. THE App SHALL remove the Qoder icon import (`qoderIcon`) and associated image asset reference
7. WHEN the App resolves provider protocol for a tool, THE App SHALL NOT reference "qoder" in any conditional branch

### Requirement 2: 添加 Cursor Agent 到前端工具列表

**User Story:** 作为用户，我希望在前端 UI 中看到 Cursor Agent 工具 tab，以便可以配置和启动 Cursor Agent。

#### Acceptance Criteria

1. THE TOOL_NAMES SHALL contain the "cursor" entry
2. WHEN the App renders the sidebar, THE App SHALL display a Cursor Agent tab with the appropriate icon when `config.show_cursor` is true
3. WHEN a user selects the Cursor Agent tab, THE App SHALL display the Cursor Agent configuration panel with provider settings
4. THE App SHALL include Cursor Agent translation keys ("cursor", "cursorDesc") in all language bundles (en, zh-Hans, zh-Hant)

### Requirement 3: Gemini CLI 升级为 SDK 模式

**User Story:** 作为用户，我希望 Gemini CLI 使用 SDK 模式运行，以便获得结构化输出和远程控制能力。

#### Acceptance Criteria

1. THE Provider_Adapter for Gemini SHALL return `ExecModeSDK` from `ExecutionMode()`
2. WHEN the Provider_Adapter for Gemini builds a command, THE Provider_Adapter SHALL include `--output-format stream-json` in the command arguments
3. WHEN the Provider_Adapter for Gemini builds a command with a non-empty prompt, THE Provider_Adapter SHALL include `-p` flag in the command arguments
4. WHEN the Session_Manager creates a Gemini session, THE Session_Manager SHALL select the SDK Execution_Strategy based on `ExecModeSDK`
5. THE Execution_Handle for Gemini SDK sessions SHALL parse JSONL events (init, message, tool_use, tool_result, result) from stdout
6. THE Execution_Handle for Gemini SDK sessions SHALL convert parsed events to human-readable text for the Output_Pipeline
7. IF the Gemini CLI process exits with a non-zero code, THEN THE Execution_Handle SHALL report the exit code through the Exit channel

### Requirement 4: Cursor Agent 升级为 SDK 模式

**User Story:** 作为用户，我希望 Cursor Agent 使用 SDK 模式运行，以便获得结构化输出和远程控制能力。

#### Acceptance Criteria

1. THE Provider_Adapter for Cursor SHALL return `ExecModeSDK` from `ExecutionMode()`
2. WHEN the Provider_Adapter for Cursor builds a command, THE Provider_Adapter SHALL include `-p` and `--output-format stream-json` in the command arguments
3. WHEN the Session_Manager creates a Cursor session, THE Session_Manager SHALL select the SDK Execution_Strategy based on `ExecModeSDK`
4. THE Execution_Handle for Cursor SDK sessions SHALL parse stream-json events from stdout using the same protocol as Claude
5. THE Execution_Handle for Cursor SDK sessions SHALL convert parsed events to human-readable text for the Output_Pipeline

### Requirement 4b: CodeBuddy 升级为 SDK 模式并加入远程 Catalog

**User Story:** 作为用户，我希望 CodeBuddy 使用 SDK 模式运行并支持远程模式，以便获得结构化输出和远程控制能力。

#### Acceptance Criteria

1. THE Tool_Catalog SHALL contain a "codebuddy" entry in the `remoteToolCatalog` map with appropriate metadata
2. THE Provider_Adapter for CodeBuddy SHALL return `ExecModeSDK` from `ExecutionMode()`
3. WHEN the Provider_Adapter for CodeBuddy builds a command, THE Provider_Adapter SHALL include `-p` and `--output-format stream-json` in the command arguments
4. WHEN the Session_Manager creates a CodeBuddy session, THE Session_Manager SHALL select the SDK Execution_Strategy based on `ExecModeSDK`
5. THE Tool_Catalog entry for CodeBuddy SHALL set `SupportsRemote` to true
6. THE Execution_Handle for CodeBuddy SDK sessions SHALL parse stream-json events from stdout using the same protocol as Claude

### Requirement 5: iFlow 升级为 SDK 模式（ACP WebSocket）

**User Story:** 作为用户，我希望 iFlow 使用 SDK 模式运行，以便通过 ACP WebSocket 协议获得结构化通信。

#### Acceptance Criteria

1. THE Provider_Adapter for iFlow SHALL return a new `ExecModeIFlowSDK` from `ExecutionMode()`
2. WHEN the Provider_Adapter for iFlow builds a command, THE Provider_Adapter SHALL include `--experimental-acp --port <PORT>` in the command arguments to start the ACP server
3. THE Session_Manager SHALL recognize `ExecModeIFlowSDK` and select the iFlow SDK Execution_Strategy
4. THE iFlow SDK Execution_Strategy SHALL start the iFlow CLI process and connect to the ACP WebSocket endpoint at `ws://localhost:<PORT>/acp`
5. THE Execution_Handle for iFlow SDK sessions SHALL parse ACP WebSocket messages (AssistantMessage, ToolCallMessage, PlanMessage, TaskFinishMessage) and convert them to human-readable text for the Output_Pipeline
6. WHEN the iFlow CLI process exits, THE Execution_Handle SHALL close the WebSocket connection and report the exit through the Exit channel
7. THE Execution_Handle for iFlow SDK sessions SHALL send user prompts as ACP-formatted WebSocket messages

### Requirement 6: OpenCode 升级为 SDK 模式（HTTP Server + SSE）

**User Story:** 作为用户，我希望 OpenCode 使用 SDK 模式运行，以便通过 HTTP server 和 SSE 事件流获得结构化通信。

#### Acceptance Criteria

1. THE Provider_Adapter for OpenCode SHALL return a new `ExecModeOpenCodeSDK` from `ExecutionMode()`
2. WHEN the Provider_Adapter for OpenCode builds a command, THE Provider_Adapter SHALL configure the command to start OpenCode in server mode
3. THE Session_Manager SHALL recognize `ExecModeOpenCodeSDK` and select the OpenCode SDK Execution_Strategy
4. THE OpenCode SDK Execution_Strategy SHALL start the OpenCode server process and connect to the HTTP API endpoint
5. THE Execution_Handle for OpenCode SDK sessions SHALL subscribe to SSE events from the OpenCode server and convert them to human-readable text for the Output_Pipeline
6. WHEN the OpenCode server process exits, THE Execution_Handle SHALL report the exit through the Exit channel
7. THE Execution_Handle for OpenCode SDK sessions SHALL send user prompts via the OpenCode HTTP API

### Requirement 7: Kilo 升级为 SDK 模式

**User Story:** 作为用户，我希望 Kilo 使用 SDK 模式运行，以便通过 headless server 获得结构化通信。

#### Acceptance Criteria

1. THE Provider_Adapter for Kilo SHALL return a new `ExecModeKiloSDK` from `ExecutionMode()`
2. WHEN the Provider_Adapter for Kilo builds a command, THE Provider_Adapter SHALL configure the command to use `kilo serve` for headless server mode
3. THE Session_Manager SHALL recognize `ExecModeKiloSDK` and select the Kilo SDK Execution_Strategy
4. THE Kilo SDK Execution_Strategy SHALL start the Kilo server process and connect to the HTTP API endpoint (similar to OpenCode)
5. THE Execution_Handle for Kilo SDK sessions SHALL subscribe to SSE events from the Kilo server and convert them to human-readable text for the Output_Pipeline
6. WHEN the Kilo server process exits, THE Execution_Handle SHALL report the exit through the Exit channel

### Requirement 8: Kode 保持 PTY 模式并标记为实验性

**User Story:** 作为用户，我希望 Kode 工具保持当前 PTY 模式运行，因为其 SDK 成熟度不足，同时在 UI 中标记为实验性。

#### Acceptance Criteria

1. THE Provider_Adapter for Kode SHALL continue to return `ExecModePTY` from `ExecutionMode()`
2. THE Tool_Catalog entry for Kode SHALL set `SupportsRemote` to false
3. WHEN the App displays the Kode tool metadata, THE App SHALL include an "experimental" indicator in the display name or badge

### Requirement 9: 远程模式仅对 SDK 工具开放

**User Story:** 作为用户，我希望只有支持 SDK 模式的工具才能使用远程模式，以确保远程会话的可靠性。

#### Acceptance Criteria

1. THE Tool_Catalog SHALL set `SupportsRemote` to true only for tools whose Provider_Adapter returns an SDK-based ExecutionMode (claude, codex, gemini, cursor, codebuddy)
2. THE Tool_Catalog SHALL set `SupportsRemote` to false for tools whose Provider_Adapter returns `ExecModePTY` (kode)
3. THE Tool_Catalog SHALL set `SupportsRemote` to false for iFlow, OpenCode, and Kilo initially, with the field updatable after further investigation confirms remote stability
4. WHEN a remote session start request specifies a tool with `SupportsRemote` set to false, THE Session_Manager SHALL reject the request with an error message indicating the tool does not support remote mode
5. THE Remote_Capable_Tools list in the frontend SHALL include "claude", "codex", "gemini", "cursor", and "codebuddy"
6. THE Remote_Capable_Tools list in the frontend SHALL NOT include tools with `SupportsRemote` set to false

### Requirement 10: 不支持远程的工具隐藏远程界面

**User Story:** 作为用户，当我选择一个不支持远程模式的工具时，我不希望看到远程相关的选项和界面，以避免混淆。

#### Acceptance Criteria

1. WHEN a user selects a tool that is not in the Remote_Capable_Tools list, THE App SHALL hide the remote mode toggle in the launch area
2. WHEN a user selects a tool that is not in the Remote_Capable_Tools list, THE App SHALL hide the remote settings panel
3. WHEN a user selects a tool that is in the Remote_Capable_Tools list, THE App SHALL display the remote mode toggle and remote settings panel
4. THE App SHALL dynamically determine remote capability from the `supports_remote` field in the tool metadata returned by the backend, rather than using a hardcoded frontend list

### Requirement 11: 新增 ExecutionMode 常量

**User Story:** 作为开发者，我希望为每种新的 SDK 协议定义独立的 ExecutionMode 常量，以便 Session_Manager 能正确路由到对应的执行策略。

#### Acceptance Criteria

1. THE App SHALL define `ExecModeGeminiSDK` constant for Gemini CLI stream-json protocol, OR reuse `ExecModeSDK` if the protocol is compatible with Claude's SDK execution strategy
2. THE App SHALL define `ExecModeIFlowSDK` constant for iFlow ACP WebSocket protocol
3. THE App SHALL define `ExecModeOpenCodeSDK` constant for OpenCode HTTP+SSE protocol
4. THE App SHALL define `ExecModeKiloSDK` constant for Kilo HTTP+SSE protocol
5. THE Session_Manager SHALL handle all new ExecutionMode constants in its strategy selection switch statement

### Requirement 12: SDK 执行句柄统一接口

**User Story:** 作为开发者，我希望所有 SDK 执行句柄实现统一的 `ExecutionHandle` 接口，以便 Session_Manager 能以一致的方式管理所有会话。

#### Acceptance Criteria

1. THE Execution_Handle for each SDK tool (Gemini, Cursor, iFlow, OpenCode, Kilo) SHALL implement the `PID()`, `Write()`, `Interrupt()`, `Kill()`, `Output()`, `Exit()`, and `Close()` methods
2. THE `Output()` channel of each SDK Execution_Handle SHALL emit human-readable text lines suitable for the Output_Pipeline
3. THE `Write()` method of each SDK Execution_Handle SHALL accept user text input and deliver it to the tool process using the tool-specific protocol
4. THE `Exit()` channel of each SDK Execution_Handle SHALL emit a `PTYExit` struct containing the process exit code upon termination
