# 需求文档：浏览器回放接入后台任务管理

## 简介

当前 `browser_task_replay`（浏览器操作回放）在 agent loop 中同步阻塞执行，回放期间对话被完全阻塞，用户无法与 AI 助手交互。由于回放是确定性操作（步骤已固定，不需要 LLM 参与），天然适合在后台执行。本功能将回放任务接入 `BackgroundLoopManager` 后台任务管理体系，使回放在后台异步执行，不阻塞对话；同时在 GUI 任务列表中展示回放进度，支持暂停/取消操作，并在完成后推送通知。此外，支持通过 `ScheduledTaskManager` 创建定时回放任务。

## 术语表

- **FlowReplayer**：浏览器操作回放引擎，位于 `corelib/browser/replayer.go`，负责将 `RecordedFlow` 转换为 `TaskSpec` 并通过 `BrowserTaskSupervisor` 执行
- **BrowserTaskSupervisor**：浏览器任务监督器，位于 `corelib/browser/task_supervisor.go`，管理浏览器任务的执行、验证和重试
- **BackgroundLoopManager**：后台循环管理器，位于 `corelib/agent/background_loop_manager.go`，管理后台 agent loop 的生命周期，支持 slot 并发控制和排队
- **AgentActivityStore**：活跃任务存储，位于 `gui/agent_activity.go`，跟踪当前活跃的 agent 任务，供 GUI 和 IM 通道互相感知
- **ScheduledTaskManager**：定时任务管理器，位于 `corelib/scheduler/task.go`，支持基于时间的定时任务调度和持久化
- **RecordedFlow**：录制的浏览器操作流程，包含步骤序列、起始 URL、成功标准等信息，以 JSON 文件持久化在 `~/.maclaw/browser_flows/` 目录
- **SlotKind**：后台任务槽位类型，`BackgroundLoopManager` 用于并发控制，当前浏览器类型为 `SlotKindBrowser`（限制 2 个并发）
- **TaskState**：浏览器任务状态对象，包含任务 ID、状态、当前步骤、总步骤数、重试次数、检查点截图等信息
- **GUI_Task_Panel**：GUI 前端的任务列表面板，展示后台任务的状态和进度

## 需求

### 需求 1：后台异步回放

**用户故事：** 作为用户，我希望浏览器回放任务在后台执行，这样回放期间我可以继续与 AI 助手对话。

#### 验收标准

1. WHEN 用户触发浏览器回放（通过 `browser_task_replay` 工具或自然语言指令），THE BackgroundLoopManager SHALL 以 `SlotKindBrowser` 类型创建一个后台任务来执行回放，而非在当前 agent loop 中同步执行
2. WHEN 回放任务提交成功，THE browser_task_replay 工具 SHALL 立即返回任务 ID 和"已提交后台执行"的确认信息，使 agent loop 可以继续处理用户输入
3. WHILE 回放任务在后台执行，THE Agent_Loop SHALL 保持可用状态，能够正常接收和处理用户的新消息
4. WHEN 浏览器 slot 已满（已有 2 个浏览器任务在运行），THE BackgroundLoopManager SHALL 将新的回放任务加入等待队列，并向用户返回排队位置信息
5. WHEN 排队中的回放任务获得可用 slot，THE BackgroundLoopManager SHALL 自动开始执行该任务，无需用户再次触发

### 需求 2：GUI 任务列表展示

**用户故事：** 作为用户，我希望在 GUI 的任务列表中看到正在执行的回放任务，这样我可以随时了解回放进度。

#### 验收标准

1. WHILE 回放任务在后台执行，THE GUI_Task_Panel SHALL 在任务列表中显示该任务，包含以下信息：任务名称（流程名称）、当前步骤/总步骤数、任务状态（排队中/执行中/已完成/失败）
2. WHEN BrowserTaskSupervisor 完成一个步骤并发出进度事件，THE GUI_Task_Panel SHALL 在 3 秒内更新对应任务的进度显示
3. WHEN 回放任务状态发生变化（开始/暂停/完成/失败），THE AgentActivityStore SHALL 同步更新任务状态，使 IM 通道也能感知到回放任务的存在

### 需求 3：暂停与取消控制

**用户故事：** 作为用户，我希望能暂停或取消正在执行的回放任务，这样我可以在需要时中断回放。

#### 验收标准

1. WHEN 用户在 GUI 任务列表中点击"取消"按钮，THE BrowserTaskSupervisor SHALL 取消对应的回放任务，释放浏览器 slot，并将任务状态标记为"已取消"
2. WHEN 用户通过对话发送取消指令（如"取消回放"），THE Agent_Loop SHALL 识别该指令并调用 BackgroundLoopManager 的 Stop 方法终止对应的回放任务
3. WHEN 回放任务被取消，THE BackgroundLoopManager SHALL 释放占用的 slot 并自动调度等待队列中的下一个任务
4. WHEN 用户在 GUI 任务列表中点击"暂停"按钮，THE BrowserTaskSupervisor SHALL 在当前步骤完成后暂停执行，保留任务上下文以便后续恢复
5. WHEN 用户对已暂停的回放任务点击"恢复"按钮，THE BrowserTaskSupervisor SHALL 从暂停的步骤继续执行回放

### 需求 4：完成通知

**用户故事：** 作为用户，我希望回放完成后收到通知，这样我不需要一直盯着任务列表。

#### 验收标准

1. WHEN 回放任务成功完成，THE Notification_System SHALL 向用户推送通知，包含：流程名称、执行耗时、最终页面截图（来自最后一个 Checkpoint 的 ScreenshotB64）
2. WHEN 回放任务执行失败，THE Notification_System SHALL 向用户推送通知，包含：流程名称、失败步骤编号、错误信息、失败时的页面截图
3. WHEN 回放任务完成（成功或失败）且用户当前在 AI 助手对话界面，THE Agent_Loop SHALL 在对话中插入一条系统消息，告知用户回放结果
4. IF 通知推送失败（如系统通知权限被禁用），THEN THE Notification_System SHALL 将通知内容写入日志，并在用户下次打开 GUI 时以对话消息形式补发

### 需求 5：定时回放

**用户故事：** 作为用户，我希望能设置定时回放任务，这样我可以让浏览器操作在指定时间自动执行（如每天定时签到）。

#### 验收标准

1. WHEN 用户通过对话创建定时回放任务（如"每天早上 9 点回放 daily_checkin"），THE ScheduledTaskManager SHALL 创建一个 `task_type` 为 "process" 的定时任务，其 `action` 字段包含回放流程名称和参数覆盖信息
2. WHEN 定时回放任务到达触发时间，THE ScheduledTaskManager SHALL 通过 executor 回调触发回放，回放任务 SHALL 通过 BackgroundLoopManager 以后台任务方式执行
3. WHEN 定时回放任务触发时浏览器 slot 已满，THE BackgroundLoopManager SHALL 将该任务加入等待队列，而非丢弃
4. THE ScheduledTaskManager SHALL 持久化定时回放任务配置，使应用重启后定时任务自动恢复
5. WHEN 定时回放任务执行完成（成功或失败），THE ScheduledTaskManager SHALL 更新任务的 `last_result` 和 `last_error` 字段，并触发 `onChange` 回调通知 GUI 更新

### 需求 6：回放任务状态查询

**用户故事：** 作为用户，我希望能通过对话查询回放任务的状态，这样我可以用自然语言了解回放进展。

#### 验收标准

1. WHEN 用户通过对话询问回放状态（如"回放进度怎么样了"），THE Agent_Loop SHALL 查询 BackgroundLoopManager 中 `SlotKindBrowser` 类型的活跃任务，并以自然语言回复当前进度
2. THE browser_task_status 工具 SHALL 支持查询后台回放任务的状态，返回信息包含：任务 ID、流程名称、当前步骤/总步骤数、状态、已用时间、重试次数
3. IF 用户查询的回放任务不存在或已完成，THEN THE browser_task_status 工具 SHALL 返回明确的提示信息，说明任务不存在或已完成及其最终结果
