# Requirements Document

## Title: GUI 启动与消息响应体验优化

## Introduction

优化 GUI 桌面 AI 助手面板的启动和消息响应体验。当前系统存在严重的启动阻塞（53 秒界面卡死）和消息响应延迟（47 秒才看到第一个 token）问题，核心诉求是消除"界面卡死无反馈"的体验。

## Glossary

- **Startup_Callback**: Wails 框架的 `startup()` 生命周期回调函数，不返回则前端无法交互
- **Hub_Client**: 与远程 Hub 服务器通信的客户端组件，负责设备注册、状态同步等
- **Send_Hello**: Hub 连接建立后的设备信息同步操作，包含 Skill 列表、在线状态等
- **Skill_Scanner**: 扫描本地 Skill 目录并解析 SKILL.md/skill.yaml 的组件
- **IMMessageHandler**: IM 通道和桌面面板共用的消息处理管道
- **UIC**: Unified Intent Classifier，统一意图分类器，用于消息意图判断
- **Task_Context_LLM**: 判断用户消息是 continue（继续当前任务）还是 new（新任务）的 LLM 调用
- **Entry_Context**: 消息处理前的上下文解析阶段，包含 UIC 分类和 task-context 判断
- **First_Token**: 用户发送消息后，LLM 流式响应中第一个可见 token 到达前端的时刻
- **Progress_Callback**: 通过 `onProgress` 回调向前端发送中间状态提示的机制
- **Wails_Binding**: Wails 框架暴露给前端的 Go 方法绑定，`SendAIAssistantMessage` 是同步阻塞的

## Requirements

### Requirement 1: Hub 连接异步化——消除启动阻塞

**User Story:** As a 桌面用户, I want 应用启动后界面立即可交互, so that 我不需要等待远程服务连接完成就能开始使用。

#### Acceptance Criteria

1. WHEN the Startup_Callback is invoked, THE Startup_Callback SHALL return control to the Wails framework within 3 seconds, measured from invocation to return, under the condition that local configuration files are readable from disk and no synchronous network I/O is on the critical path.
2. WHEN Hub credentials (RemoteMachineID, RemoteMachineToken, RemoteHubURL) are all configured, THE Hub_Client SHALL initiate WebSocket dial and authentication in a background goroutine spawned before or during Startup_Callback execution, without blocking the Startup_Callback return.
3. IF Hub credentials are not fully configured (any of RemoteMachineID, RemoteMachineToken, or RemoteHubURL is empty), THEN THE System SHALL mark the AI assistant as ready immediately at the end of Startup_Callback without attempting Hub connection.
4. WHEN Hub authentication completes successfully (auth.ok response received from Hub), THE System SHALL mark the AI assistant as ready and emit an event named "ai-assistant-init-progress" with payload "ready" to the frontend.
5. WHEN Send_Hello is required after successful authentication, THE Hub_Client SHALL execute Send_Hello in a background goroutine after the Startup_Callback has returned, ensuring Send_Hello network latency does not delay UI interactivity.
6. IF Hub WebSocket dial fails or Hub authentication fails or no auth response is received within 10 seconds of dial initiation, THEN THE System SHALL mark the AI assistant as ready with degraded mode, where degraded mode means: LLM requests via locally-configured API keys remain functional, local tool execution (bash, write_file, read_file, edit_file) remains functional, and only Hub-dependent features (IM message relay, remote session sync, scheduled task push to IM channels) are unavailable until reconnection succeeds.
7. WHEN the AI assistant is marked ready (regardless of whether Hub connection succeeded or failed), THE Frontend SHALL enable the user input area for text entry and display a visual readiness indicator distinguishable from the prior "connecting" or "loading" state.

### Requirement 8: Send_Hello 工具版本检测优化

**User Story:** As a 桌面用户, I want Hub 连接的 send_hello 阶段不因工具版本检测而耗时几十秒, so that Hub 状态同步能在合理时间内完成。

#### Acceptance Criteria

1. WHEN sendMachineHelloLocked constructs the hello payload, THE System SHALL NOT execute external processes (e.g., `claude --version`, `codex --version`) synchronously to obtain tool version information.
2. WHEN tool version information is needed for the hello payload, THE System SHALL use cached version data from the last successful version check, or omit version information if no cache is available.
3. WHEN tool installation status is checked via GetToolStatus, THE System SHALL only verify binary existence via `exec.LookPath` or file stat (< 1ms per tool), NOT execute the binary to obtain version output.
4. WHEN tool version information needs to be refreshed, THE System SHALL perform version checks asynchronously in a background goroutine after the hello message has been sent.
5. THE sendMachineHelloLocked function SHALL complete within 500 milliseconds under normal conditions (local config readable, no external process execution).
6. WHEN multiple tools need version checking, THE System SHALL execute version checks concurrently (parallel goroutines) rather than serially, with a combined timeout of 10 seconds for all tools.

### Requirement 2: Skill 扫描异步化与缓存

**User Story:** As a 桌面用户, I want Skill 扫描不阻塞启动和消息处理, so that 启动速度和消息响应速度不受 Skill 数量影响。

#### Acceptance Criteria

1. WHEN the Skill_Scanner is initialized during Startup_Callback, THE Skill_Scanner SHALL NOT perform synchronous file system scanning and SHALL return control to the caller within 50 milliseconds.
2. WHEN the Skill_Scanner is initialized, THE Skill_Scanner SHALL record Skill directory paths and initiate a background scan goroutine.
3. WHILE the background scan is in progress, THE Skill_Scanner SHALL return an empty Skill list to callers requesting the Skill list (graceful degradation).
4. WHEN the background scan completes successfully, THE Skill_Scanner SHALL cache the results in memory with a validity period of 30 seconds.
5. WHEN a cached Skill list is available and within the 30-second validity period, THE Skill_Scanner SHALL return the cached results without re-scanning the file system.
6. WHEN the cache validity period expires and a caller requests the Skill list, THE Skill_Scanner SHALL return the stale cached results immediately and initiate a new background scan to refresh the cache.
7. WHEN a Skill is installed or deleted via the SkillExecutor API (Install, Delete, or invalidateSkillCache call), THE Skill_Scanner SHALL invalidate the cache within 100 milliseconds.
8. IF the background scan encounters file system errors for individual Skill directories, THEN THE Skill_Scanner SHALL skip the errored directory, log the error, and continue scanning remaining directories.
9. THE Skill_Scanner SHALL NOT perform more than one concurrent scan operation (deduplication via sync.Once or mutex).
10. WHEN the cache is invalidated and a caller requests the Skill list before the new background scan completes, THE Skill_Scanner SHALL return an empty Skill list.

### Requirement 3: 首条消息立即进度反馈

**User Story:** As a 桌面用户, I want 发送消息后立即看到系统反馈, so that 我知道系统正在处理我的请求而不是卡死了。

#### Acceptance Criteria

1. WHEN a user message is received by the IMMessageHandler, THE System SHALL emit a progress event to the frontend within 100 milliseconds of message receipt.
2. WHEN the progress event is emitted, THE Frontend SHALL display a "正在思考..." status indicator in the message area where the assistant response will appear.
3. WHILE the message is being processed (preflight, entry_context, agent loop), THE System SHALL maintain the progress indicator until the first LLM token arrives, an error occurs, or 120 seconds have elapsed since message receipt.
4. WHEN the first LLM streaming token arrives, THE Frontend SHALL replace the progress indicator with the streaming content within the same message container.
5. IF message processing encounters an error before the first token, THEN THE System SHALL replace the progress indicator with an error message indicating the failure category (network error, LLM service unavailable, or request timeout).
6. IF 120 seconds elapse without the first LLM token arriving or an error being reported, THEN THE System SHALL replace the progress indicator with a timeout error message.
7. IF the user sends a subsequent message while the progress indicator is displayed, THE System SHALL retain the progress indicator for the original message and process the new message according to the message scheduling mechanism.

### Requirement 4: UIC 分类超时缩短与短消息跳过

**User Story:** As a 桌面用户, I want 消息处理的前置分类步骤尽可能快, so that 我的消息能更快到达 LLM 生成阶段。

#### Acceptance Criteria

1. WHEN the UIC tree channel classification is invoked, THE UIC SHALL use a timeout of 1.5 seconds (reduced from 3 seconds).
2. IF the UIC tree channel classification times out, THEN THE UIC SHALL return a result with label "ambiguous", confidence 0.0, and Degraded=true, allowing downstream processing to continue without blocking.
3. WHEN the user message contains fewer than 10 Unicode code points (runes) after trimming leading and trailing whitespace, THE Entry_Context resolver SHALL skip UIC fusion classification (L2 embedding + L3 tree) and proceed directly to the agent loop.
4. WHEN UIC fusion is skipped for short messages, THE Entry_Context resolver SHALL still execute L1 keyword matching to preserve fast-path intent detection for known patterns (e.g., continuation signals, SSH keywords).
5. WHEN UIC fusion is skipped for short messages, THE System SHALL log at DEBUG level the skip reason including the original message length and the threshold value.

### Requirement 5: Task-Context LLM 调用优化

**User Story:** As a 桌面用户, I want task-context 判断不成为消息处理的瓶颈, so that 消息能更快进入 LLM 生成阶段。

#### Acceptance Criteria

1. WHEN the conversation history contains fewer than 5 entries, THE Entry_Context resolver SHALL skip the Task_Context_LLM call and default to "new task" (TaskNew action).
2. WHEN the conversation history contains 5 or more entries, THE Task_Context_LLM call SHALL execute in parallel with system prompt construction, using a separate goroutine with result communicated via channel.
3. WHEN the Task_Context_LLM call is invoked, THE Task_Context_LLM SHALL use a timeout of 2 seconds (reduced from the current 8 seconds in DefaultTaskContextConfig).
4. IF the Task_Context_LLM call times out or returns an error, THEN THE Entry_Context resolver SHALL default to "continue" (TaskContinue action) as the conservative assumption.
5. WHEN the Task_Context_LLM call completes before the system prompt construction, THE result SHALL be consumed immediately without additional waiting.

### Requirement 6: 消息处理不依赖 Send_Hello 完成

**User Story:** As a 桌面用户, I want 在 Hub 后台同步完成之前就能正常发送消息并获得 AI 响应, so that Hub 的慢速同步不影响我的使用体验。

#### Acceptance Criteria

1. WHEN a user message is received and the AI assistant is marked ready (warmupDone=true, imHandler instantiated, interactionInfra ready), THE IMMessageHandler SHALL process the message regardless of Send_Hello completion status.
2. THE IMMessageHandler SHALL only depend on locally available resources for message processing: LLM configuration (local config file), conversation history (local file), and the IMMessageHandler instance itself.
3. WHILE Send_Hello is in progress in the background, THE System SHALL NOT block or queue user messages; message processing latency SHALL NOT increase by more than 100ms compared to the latency observed after Send_Hello has completed.
4. IF Send_Hello fails after the user has already started interacting, THEN THE System SHALL log the failure at warning level and continue operating without interrupting the active user session; all features that depend only on local LLM configuration, local conversation history, and local tool execution SHALL remain functional.
5. WHEN the application starts, THE System SHALL execute sendMachineHelloLocked in a background goroutine after markAIAssistantReady has been called, ensuring no shared mutex or synchronization primitive between Send_Hello execution and the IMMessageHandler message processing path can cause blocking.
6. IF Send_Hello has not yet completed when a user message arrives, THEN THE System SHALL process the message using locally available resources and SHALL NOT wait for or depend on any data returned by Send_Hello (device profile sync, Skill list sync, online status sync).

### Requirement 7: 端到端响应时间目标

**User Story:** As a 桌面用户, I want 从输入消息到看到第一个可见反馈的时间尽可能短, so that 交互体验流畅自然。

#### Acceptance Criteria

1. WHEN a user sends a message and the system is in ready state (Startup_Callback completed, LLM configuration loaded, tool router initialized), THE System SHALL display a spinning progress indicator in the chat message area within 200 milliseconds of the message submission event.
2. WHEN a user sends a message and the system is in ready state, IF the LLM API responds within 10 seconds, THEN THE System SHALL deliver the first LLM streaming token to the frontend within 5 seconds measured from message submission.
3. IF the first LLM streaming token is not received within 5 seconds of message submission, THEN THE System SHALL continue displaying the progress indicator and append a status hint indicating extended processing time.
4. IF local configuration file is available on disk AND network round-trip to the configured LLM endpoint is below 500 milliseconds, THEN THE System startup (from Startup_Callback invocation to ready state) SHALL complete within 3 seconds.
5. WHILE the system is in ready state, IF the Skill cache has been populated and has not exceeded its time-to-live expiry, THEN THE tool routing operation (Route function) SHALL complete within 10 milliseconds.
