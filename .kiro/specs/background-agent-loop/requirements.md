# Requirements Document: 后台 Agent Loop 隔离

## Introduction

当前 MaClaw 的所有任务（用户聊天、编程会话监控、定时任务、ClawNet 自动任务）共享同一个 `IMMessageHandler` 实例和 `runAgentLoop` 入口。这导致以下问题：

1. **编程会话监控被用户消息打断**：当编程会话处于 busy 状态时，maclaw 在 agent loop 中轮询 `get_session_output`。此时用户发新消息会触发新的 `runAgentLoop`，LLM 可能被新消息带偏，忘记监控编程会话，甚至调用无关工具（如 bash），导致编程会话无人看护而失败。
2. **定时任务与用户聊天互相干扰**：重型定时任务（MinIterations=50）和用户聊天共享 `loopMaxOverride` 字段（代码注释已标注 TODO: not goroutine-safe），存在 race condition。
3. **轮数耗尽时前后台无法通信**：后台任务（编程会话监控、定时任务）推理轮数快用尽时，需要通过前台 chat loop 询问用户是否继续，但当前没有前后台通信机制，后台只能硬停。

本功能将 agent loop 拆分为前台（chat）和后台（background）两个独立的执行通道，通过 channel 通信实现前后台协调，解决上述并发和隔离问题。

## Glossary

- **Chat_Loop**: 前台 agent loop，负责处理用户 IM 消息，快进快出
- **Background_Loop**: 后台 agent loop，独立 goroutine 运行，负责编程会话监控、定时任务、ClawNet 自动任务等长时间运行的任务
- **Session_Monitor**: 轻量级后台 goroutine，纯代码逻辑轮询 busy 的编程会话状态，不走 LLM
- **Continue_Signal**: 前台通过 channel 向后台发送的"续命"信号，包含新增的推理轮数
- **Status_Event**: 后台通过 channel 向前台推送的状态变化事件（轮数即将耗尽、任务完成、任务失败等）
- **Loop_Context**: 每个 agent loop 实例独立的上下文，包含自己的 conversation history、iteration counter、max iterations 等，替代当前挂在 handler 上的共享字段

## Requirements

### Requirement 1: 前后台 Agent Loop 隔离

**User Story:** As an IM_User, I want my chat messages to be processed independently from background tasks, so that sending a message while a coding session is running doesn't disrupt the session monitoring.

#### Acceptance Criteria

1. WHEN a Background_Loop is running (monitoring a coding session or executing a scheduled task), AND IM_User sends a new message, THE Chat_Loop SHALL process the message independently without affecting the Background_Loop's execution
2. THE Background_Loop SHALL have its own Loop_Context (conversation history, iteration counter, max iterations) that is NOT shared with the Chat_Loop
3. THE Chat_Loop SHALL have access to read (but not write) the Background_Loop's status, so it can inform the user about ongoing background tasks
4. THE `loopMaxOverride` field SHALL be moved from the shared `IMMessageHandler` struct to the per-loop `Loop_Context`, eliminating the race condition

### Requirement 2: 编程会话后台监控

**User Story:** As an IM_User, I want coding session monitoring to continue reliably in the background, so that I can chat with MaClaw about other things while a coding task is running.

#### Acceptance Criteria

1. WHEN the Chat_Loop creates a coding session via `create_session` and sends the initial instruction via `send_and_observe`, THE system SHALL spawn a Session_Monitor goroutine to track the session's progress
2. THE Session_Monitor SHALL periodically poll `get_session_output` (every 15-30 seconds) for busy sessions without consuming LLM inference rounds
3. WHEN the Session_Monitor detects a session status change (busy → waiting_input, busy → exited), IT SHALL push a Status_Event to notify the Chat_Loop
4. WHEN the Chat_Loop receives a session completion Status_Event, IT SHALL notify the user and trigger the RFO Phase (if applicable)
5. THE Session_Monitor SHALL coexist with the existing StallDetector — StallDetector handles nudging stalled sessions, Session_Monitor handles status polling and user notification

### Requirement 3: 后台任务独立执行与并发控制

**User Story:** As a system administrator, I want scheduled tasks and ClawNet auto-tasks to run in isolated Background_Loops with controlled concurrency, so that they don't interfere with user chat, with each other, or overwhelm system resources.

#### Acceptance Criteria

1. WHEN a scheduled task triggers, THE system SHALL create a new Background_Loop with its own Loop_Context, separate from the Chat_Loop
2. WHEN a ClawNet auto-task triggers, THE system SHALL create a new Background_Loop with its own Loop_Context
3. EACH Background_Loop SHALL use the existing `taskClient` (separate HTTP connection pool) for LLM requests
4. MULTIPLE Background_Loops MAY run concurrently without sharing mutable state (no shared `loopMaxOverride`, no shared conversation history)
5. THE BackgroundLoopManager SHALL enforce a configurable maximum number of concurrent Background_Loops (default: 3), categorized as:
   - 编程任务 loop: 最多 1 个（用户主动发起的编程会话监控）
   - 定时任务 loop: 最多 1 个（定时任务按时触发，不因编程任务阻塞）
   - 自动任务 loop: 最多 1 个（ClawNet 自动接单）
6. WHEN a user initiates a new coding task while a coding Background_Loop is already running, THE Chat_Loop SHALL inform the user that a coding task is in progress and queue the new request until the current one completes, rather than spawning a second coding loop
7. WHEN a scheduled task triggers while the scheduled-task slot is occupied, THE system SHALL queue the task and execute it when the slot becomes available, ensuring no scheduled task is silently dropped
8. FOR Swarm mode (multi-session orchestration), THE SwarmOrchestrator SHALL manage its own session pool internally — each swarm session is monitored by the shared SessionMonitor (lightweight, no LLM), but the swarm's LLM reasoning runs in a single Background_Loop that coordinates all sessions, not one loop per session

### Requirement 4: 前后台通信 — 轮数续命

**User Story:** As an IM_User, I want to be asked whether to continue when a background task is running low on inference rounds, so that I can decide whether to spend more tokens.

#### Acceptance Criteria

1. WHEN a Background_Loop reaches `maxIterations - 2` (approaching the limit), IT SHALL push a Status_Event ("轮数即将耗尽") to the Chat_Loop and enter a paused state
2. WHEN the Chat_Loop receives a "轮数即将耗尽" Status_Event, IT SHALL ask the user whether to continue
3. WHEN the user confirms continuation, THE Chat_Loop SHALL send a Continue_Signal with additional rounds (e.g., +20) to the Background_Loop
4. WHEN the Background_Loop receives a Continue_Signal, IT SHALL add the specified rounds to its Loop_Context and resume execution
5. WHEN the user declines continuation, THE Chat_Loop SHALL close the Continue_Signal channel, causing the Background_Loop to gracefully exit
6. IF the Background_Loop does not receive a Continue_Signal within a configurable timeout (default 5 minutes), IT SHALL gracefully exit and push a "超时退出" Status_Event

### Requirement 5: 后台任务状态可见性与前端面板

**User Story:** As an IM_User, I want to see and manage background tasks from both IM commands and a visual panel, so that I can monitor and control what MaClaw is doing in the background.

#### Acceptance Criteria

1. WHEN IM_User sends `/sessions` or asks about current tasks, THE Chat_Loop SHALL include background task status (running, paused, completed) in the response
2. THE system prompt SHALL be updated to inform the LLM about active Background_Loops, so it can reference them in conversation
3. WHEN a Background_Loop completes (success or failure), THE system SHALL push a proactive notification to the user via IM
4. THE sidebar navigation entry currently labeled "远程" SHALL be renamed to "任务"
5. THE "任务" panel SHALL contain two sub-tabs: "远程"（人工启动的远程会话，即现有"人类"tab）和 "后台"（合并现有"AI"tab 与新增的后台 Agent Loop 列表，统一展示所有 MaClaw 驱动的任务）
6. THE "后台" sub-tab SHALL display each task with: task type tag (编程 / ⏰ 定时 / ClawNet), task description, iteration progress (current/max), status (running/paused/completed/failed), and associated session ID (if any)
7. THE "后台" sub-tab SHALL provide operation buttons per task: "停止" (graceful stop), "续命" (send additional rounds when paused), and for coding sessions: "查看终端" (open read-only console)
8. THE "后台" sub-tab data SHALL be fetched via Wails bindings from `BackgroundLoopManager`, with periodic auto-refresh (every 5 seconds)
9. WHEN a user clicks "查看终端" on a coding background task, THE system SHALL open `RemoteSessionConsole` in read-only mode (`readOnly={true}`), displaying real-time terminal output without allowing input

### Requirement 6: 向后兼容

**User Story:** As a developer, I want the refactoring to preserve all existing behavior for simple (non-background) interactions, so that nothing breaks for users who don't trigger background tasks.

#### Acceptance Criteria

1. WHEN IM_User sends a simple message (question, file operation, configuration), THE Chat_Loop SHALL process it exactly as before, with no behavioral change
2. THE existing `HandleIMMessage` and `HandleIMMessageWithProgress` API signatures SHALL remain unchanged
3. THE existing conversation memory (`conversationMemory`) SHALL continue to work for Chat_Loop interactions
4. THE existing StallDetector integration SHALL continue to function alongside the new Session_Monitor
5. ALL existing tests SHALL continue to pass without modification
