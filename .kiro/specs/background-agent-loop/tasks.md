# Implementation Tasks: 后台 Agent Loop 隔离

## Task 1: LoopContext 与 StatusEvent 数据结构

- [x] 创建 `agent_loop_context.go`，实现 `LoopContext` 结构体（含 ID, Kind, SlotKind, Description, MaxIterations, Iteration, Status, Conversation, History, ContinueC, StatusC, CancelC, HTTPClient, SessionID, StartedAt）
- [x] 实现 `LoopKind`（Chat/Background）和 `SlotKind`（Coding/Scheduled/Auto）枚举
- [x] 实现 `StatusEvent` 和 `StatusEventType` 枚举（SessionCompleted, SessionFailed, ApproachingLimit, Stopped, Progress）
- [x] 实现 `LoopContext.State()` 方法返回当前状态字符串
- [x] 创建 `agent_loop_context_test.go`，编写 Property 1（隔离性）、Property 3（续命信号）、Property 4（channel close graceful shutdown）、Property 5（超时退出）的属性测试

## Task 2: BackgroundLoopManager（含 Slot 并发控制）

- [x] 创建 `background_loop_manager.go`，实现 `BackgroundLoopManager` 结构体（含 mu, loops, handler, statusC, slotLimits, queues）
- [x] 实现 `pendingTask` 结构体和 `BackgroundLoopView` 前端快照结构体
- [x] 实现 `NewBackgroundLoopManager(handler, statusC)` 构造函数，初始化默认 slot 限制（编程1/定时1/自动1）
- [x] 实现 `Spawn(slotKind, userID, systemPrompt, userText, maxIter, onProgress)` — slot 检查、排队、goroutine 启动
- [x] 实现 `Stop(loopID)` — 优雅停止 + 自动 dequeue 下一个 pendingTask
- [x] 实现 `SendContinue(loopID, additionalRounds)` — 向 paused loop 发送续命信号
- [x] 实现 `Get(loopID)`, `List()`, `ListViews()`, `QueueLength(kind)` 查询方法
- [x] 创建 `background_loop_manager_test.go`，编写 Property 7（并发安全）和 Property 8（slot 并发控制）的属性测试

## Task 3: SessionMonitor（轻量级会话状态轮询）

- [x] 创建 `session_monitor.go`，实现 `SessionMonitor` 结构体（含 mu, watches, manager, statusC, interval）
- [x] 实现 `sessionWatch` 结构体（sessionID, loopID, lastStatus, cancelCh）
- [x] 实现 `StartWatching(sessionID, loopID)` — 启动 goroutine 定期轮询会话状态
- [x] 实现 `StopWatching(sessionID)` 和 `Close()` — 停止轮询、清理资源
- [x] 轮询逻辑：检测 busy → waiting_input/exited 状态变化，推送 StatusEvent
- [x] 轮询失败处理：session 被清理时自动 StopWatching + 推送 SessionFailed
- [x] 创建 `session_monitor_test.go`，编写 Property 2（不消耗 LLM tokens）的属性测试

## Task 4: runAgentLoop 重构 — 接受 LoopContext

- [x] 修改 `runAgentLoop` 签名：接受 `*LoopContext` 替代散落的 history/minIterations/httpClient 参数
- [x] 将 `h.loopMaxOverride` 替换为 `ctx.MaxIterations`
- [x] 将局部 iteration counter 移到 `ctx.Iteration`（可被外部观察）
- [x] 后台 loop 在 `ctx.MaxIterations - 2` 时检查 `ctx.ContinueC`，进入 paused 状态等待续命/关闭/超时
- [x] Chat Loop 在每轮 LLM 调用前通过 `select` 检查 `ctx.StatusC`，注入后台事件到 conversation
- [x] 更新所有 `runAgentLoop` 调用点（`HandleIMMessageWithProgress` 等）使用新签名
- [x] 从 `IMMessageHandler` 移除 `loopMaxOverride` 字段
- [x] 创建 `im_message_handler_bgloop_test.go`，编写 Property 6（向后兼容）的属性测试

## Task 5: HandleIMMessageWithProgress 路由 + buildSystemPrompt 增强

- [x] 在 `IMMessageHandler` 新增 `bgManager *BackgroundLoopManager` 和 `sessionMonitor *SessionMonitor` 字段
- [x] 修改 `HandleIMMessageWithProgress`：`msg.IsBackground` 时路由到 `bgManager.Spawn()`
- [x] 实现 `newChatLoopContext(msg, systemPrompt)` 为前台聊天创建 LoopContext
- [x] 在 `buildSystemPrompt()` 末尾注入后台任务状态信息（活跃 loop 列表 + 警告提示）
- [x] 在 `NewIMMessageHandler` 中初始化 bgManager 和 sessionMonitor

## Task 6: Wails Bindings + App 集成

- [x] 在 `app_wails_bindings.go` 新增 `ListBackgroundLoops() []BackgroundLoopView`
- [x] 在 `app_wails_bindings.go` 新增 `StopBackgroundLoop(loopID string) error`
- [x] 在 `app_wails_bindings.go` 新增 `ContinueBackgroundLoop(loopID string, additionalRounds int) error`
- [x] 实现 `getIMHandler()` 辅助方法从 App 获取当前 IMMessageHandler
- [x] 在 BackgroundLoopManager 状态变化时调用 `runtime.EventsEmit(a.ctx, "background-loops-changed")`
- [x] 在 `app.go` 的 `ensureRemoteInfra` / `createAndWireHubClient` 中初始化 bgManager 和 sessionMonitor

## Task 7: 前端 — 侧边栏改名 + Sub-Tab 重构

- [x] `App.tsx`：侧边栏 `navTab === 'remote'` 的标签文字从"远程"改为"任务"（英文 "Tasks"，繁体 "任務"）
- [x] `RemoteSessionList.tsx`：`sessionTab` 类型从 `"human" | "ai"` 改为 `"remote" | "background"`
- [x] "远程" sub-tab：显示 `remoteSessions.filter(s => s.launch_source !== "ai")`（即现有"人类"tab 内容）
- [x] "后台" sub-tab：分两个区域 — Agent Loop 任务列表 + AI 编程会话列表
- [x] Agent Loop 区域：调用 `ListBackgroundLoops()` Wails binding，显示类型标签（/⏰/）、描述、轮次进度、状态
- [x] Agent Loop 操作按钮：停止（`StopBackgroundLoop`）、续命（`ContinueBackgroundLoop`，仅 paused 状态显示）
- [x] 编程类 Agent Loop 和 AI 编程会话：点击"查看终端"打开 `RemoteSessionConsole` with `readOnly={true}`
- [x] 数据刷新：`EventsOn("background-loops-changed")` 监听 + 每 5 秒轮询 `ListBackgroundLoops()` 兜底
