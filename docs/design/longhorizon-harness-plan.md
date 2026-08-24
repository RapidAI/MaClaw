# 借鉴 LongHorizon-Harness 的长程执行闭环改进计划

> 状态：P0–P5 已实施
> 日期：2026-08-16
> 修订：P2 Browser / P3 GUI 短循环；P4 TaskState 投影事件；P5 Manager 经验检索与混合模态探针
> 参考：LongHorizon-Harness v0.1.5；`gui/im_message_handler.go`；`gui/im_entry_serialization.go`；`gui/app_wails_bindings.go`

## 1. 决策摘要

**外环负责下一件做什么、以及哪些事实值得记住；内环每次只拿一份已验证的短上下文，把那一件事做完就丢掉。**

外环是每个 UserID 一条 Supervisor 后台状态机。IM 只做准入、取消、把文本放进 inbox。

开了 Horizon 之后，消息不得进入预检、序列化锁等待、`buildIMEntrySystemPrompt`、`runAgentLoop`。截获点不在 `executePreparedIMEntry`，而在 `handleIMMessageWithLoop` **靠前**：`CancelCtx` 检查之后、`prepareIMMessagePreflight` / `enterIMMessageSerializationBoundary` 之前。否则确认门、embedding 预热、以及「有活跃 loop 就排队」会把续言卡住。

**禁止 `RunTaskWithSubAgent`。** 它会 `SetFullEnvironment(true)`、`SetKnowledgeStores`，成功时走 `persistLocalizationExperience`。CLI 走 `runHorizonCodingEpisode`。

CodingSubAgent 相对 IM 干净，相对 Horizon 仍脏。默认短聊天不变。移植协议，不移植 Python CLI Agent。

## 2. 两层 + 一道墙

```
HandleIMMessage
  -> handleHorizonIMRoute（预检/序列化/提示词之前）
        | 否 -> 今日路径
        | 是 -> 入队或启动 Supervisor，立刻返回（不持有 session 序列化锁）
        v
独立 registry：horizonSessions[UserID]
  不是 sessionLoops 里的 IM Agent loop
  因此不会触发 TryInterrupt 合并，也不会让下一条消息在序列化锁上等到任务结束
        |
        v
Supervisor goroutine
  落盘 TaskState；max_rounds；写闸；Next fail-closed
  进度/心跳/最终回复：ai-assistant-progress / ai-assistant-response（带 EventScopeID）
        |
        | EpisodeContext 组装器
        v
干净 episode
  Manager/Auditor  无工具 LLM 补全
  CLI              runHorizonEpisode
  GUI/Browser      runHorizonEpisode + 模态探针/审计
```

不要复用 `bgManager`、`sessionLoops` 里的 IM loop、`RunTaskWithSubAgent`。

## 3. 目标与非目标

目标：有界调度；审计后才写经验；角色只读组装上下文；完成权在 harness；崩溃可恢复外环；未开外环路径不变。

非目标：把三角色塞进一次 HandleIMMessage；把 Supervisor 登记成 IM session loop；复用 background IM；替换 Workflow V2；Executor 检索记忆；Attempt 完成即写知识；P1 自动长任务检测。

## 4. 不变量

调度与完成：
1. Manager `done` 且最近一次真实审计 `complete + clean + aligned` 才完成。Attempt 终态不是完成，也不是知识资格。
2. 不得 AdvancePhase。CU Reset 不擦 TaskState / PolicySnapshot。
3. 每 UserID 一条 Supervisor。非控制文本进 inbox，当前 episode 继续。
4. Next fail-closed → `ask`，不得开 Executor。`max_rounds = 25`（上限 1000）。
5. 单次 HandleIMMessage 不得跑完外环，也不得握着序列化锁等待 Supervisor。
6. Horizon 不进入 `sessionLoops`；`TryInterrupt` / `hasActiveLoopForUser` 不得把它当成 IM loop。
7. `CancelSessionForUser` / 停止按钮 / `/clear` 必须取消 Horizon episode（uncertain，不提炼）。
8. 无任务正文、无消息工作区路径，不得开 CLI Executor，改为 `ask`。

经验：
9. 自动经验带 HorizonTaskID + RoundIndex + AuditDigest。ConfirmCandidate 重算 digest。
10. Horizon 活跃时不得 `EventTaskSucceeded`。
11. 只由 `horizonExperienceWriter` 在审计后写。Horizon 名下 no-op：`extractAndSaveExperience`、`persistLocalizationExperience`、`agentLoopTerminalExperienceEvent`。

内环墙：
12. 不得 `buildSystemPromptWithMemory`、ConversationMemory、父 GoalAnchor、知识库注入。
13. ToolSurface 在 BuildTools 和 ExecuteTool 都生效；白名单外拒绝。
14. 禁止 `RunTaskWithSubAgent`、`fullEnvironment`、`SetKnowledgeStores`、动态 MCP/skill/spawn。
15. Horizon 与 `ShouldUseSubAgent` 互斥。verify-fix 不是外环完成。
16. 网页走 browser，桌面走 CU；GUI 每轮新 Session。
17. ProjectRoot 来自**这条消息的工作区**（`req.ProjectPath` / `EffectiveWorkingDirForOwner`），不是 `GetCurrentProjectPath()`（多 Tab 会指错项目）。

## 5. EpisodeContext

字段与 cap 同前：Goal 8_000、Acceptance 4_000、RelatedAudits 3×1_200、verified 60_000、Manager history 100_000、auditor 24_000。
PolicySnapshot 含 OwnerID、ProjectRoot、WriteSet、Untrusted、HorizonTaskID、RoundIndex、EventScopeID。

P1 CLI ToolSurface：文件读写/编辑、shell、Glob/ripgrep、code navigation、report localization、todo。
禁止：`computer_*`、知识检索、MCP、skill、spawn、`/goal`。
Manager/Auditor ToolSurface 为空。

Shell / 写文件必须落在 ProjectRoot；越界硬拒绝（P1 可不做交互式 scope 面板）。

## 6. IM 截获与 Supervisor

### 6.1 handleHorizonIMRoute

插入 `handleIMMessageWithLoop`：绑定校验与 `CancelCtx` 之后，immediate command / 预检 / 序列化之前。

| 入站 | 行为 |
| --- | --- |
| 无活跃任务且非 `@horizon` | 不处理 |
| `@horizon` 且已有任务 | 拒绝第二条 |
| `@horizon` 无正文 | ask：要做什么 |
| `@horizon` 无工作区路径 | ask：先打开项目 |
| `@horizon` 可准入 | 冻结 PolicySnapshot，启动 Supervisor，回「已开始」，立即返回 |
| 活跃 + 停止/取消 | 取消 episode |
| 活跃 + asking | 文本作为回答，唤醒 Manager |
| 活跃 + 其它 | 写入 inbox，回「已记录」，立即返回 |

`@horizon` 前缀剥掉后才是目标。不要把该行送进 IM 记忆。

`/clear`、停止按钮、`ClearAIAssistantHistoryForSession` 走 `CancelSessionForUser`，其中先停 Horizon 再清 IM loop。

### 6.2 进度与心跳

第一次 HandleIMMessage 返回后，请求级 `OnProgress` 和 heartbeat 会被 defer 停掉。Supervisor 必须用会话级事件，不能借用那次回调：

- `ai-assistant-progress`（含心跳，防止前端活动超时）
- `ai-assistant-token` / `ai-assistant-response`（ask、blocked、完成摘要）
- 带上 admit 时冻结的 EventScopeID

### 6.3 Supervisor

`corelib/longhorizon` 调度 + 落盘；gui Host：LLM 补全、CLI episode、事件、ask。
状态：`idle | managing | executing | auditing | asking | blocked | done`。
目录：`horizon/{taskID}/`。崩溃不自动重跑 Executor。下次该 UserID 的消息若发现未完成 TaskState：询问是否恢复，确认后再起 Supervisor。

Manager/Auditor：`RunRoleCompletion`，无工具。模型走编码路由配置，不走带记忆的聊天提示词。无探针 digest 不得用 `clean` 完成任务。

## 7. runHorizonCodingEpisode

```
NewCodingSubAgent
不要 SetFullEnvironment(true)，不要 SetKnowledgeStores
Horizon LoopContext 只用于取消，不登记进 sessionLoops
ExecuteTask(Goal, 边界, Acceptance, RelatedAudits)
ExecuteTool 开头：不在 ToolSurface 则拒绝（含动态工具名）
不要 persistLocalizationExperience
```

P1 可不挂 codingruntime ledger；若挂，Attempt 带 HorizonTaskID 且知识资格为 false。

## 8. 经验

审计后最多 2 条 candidate。P1 只写不读。截图与命令全文不得入库。

## 9. 互斥与插入点

- 截获：`gui/im_message_handler.go`（序列化之前）
- 取消：`CancelSessionForUser`
- 调度：`corelib/longhorizon`
- CLI：`gui` 新函数，不碰 `RunTaskWithSubAgent`
- 写闸：experience / localization / `im_post_loop.go`
- P1 宿主：GUI 桌面助手。TUI/MaClawSrv/ACP 复用 Supervisor，但 P1 不接

## 10. 工作包

P0：完成门、经验资格、组装器、Next、ToolSurface 拒绝、posture 断言。无 LLM。

P1：提前截获 + 独立 registry + Supervisor 事件/心跳 + CLI episode + 写闸 + `@horizon`。
完成标准：HandleIMMessage 在启动后返回；后续消息不进预检/IM 提示词、不等序列化锁；停止按钮能取消；多 Tab 用消息工作区而不是全局当前项目。

P2 Browser；P3 GUI 短循环；P4 投影（`horizon:projection` + EventScopeID）；P5 Manager 经验检索与 GUI/Browser observe 探针（无截图 base64）。

完成标准：`Next=gui|browser` 走独立短循环与对应 auditor；`computer_done` 在 Horizon GUI 下只报 claim；进度事件带 admit 时冻结的 EventScopeID。

## 11. 测试、风险、PR

测试：截获在 `enterIMMessageSerializationBoundary` 之前；活跃期第二条消息不调用 `buildSystemPromptWithMemory`、不 `TryInterrupt`；`CancelSessionForUser` 取消 Horizon；不调用 `RunTaskWithSubAgent`；ExecuteTool 白名单；进度事件在首次返回之后仍能发出；ProjectRoot 用消息路径。

风险：截获放在 `executePreparedIMEntry` 会撞上确认门和序列化等待；登记进 `sessionLoops` 会让续言变成 interrupt/排队；借用本次 OnProgress 会在返回后静默。

PR：P0 → P1 → P4 → P2 → P3 → P5。未开外环路径不变。

P1 用户可见成功：`@horizon` 立即「已开始」；之后自动多 round；中途输入只被记录；点停止即结束；没打 `@horizon` 与今日一致。

## 附录 审查记录

R1–R30：写权、完成门、组装器、posture、禁止 RunTaskWithSubAgent、后台状态机、组提示词之前截获。
R31（本次）：截获必须在预检和 `enterIMMessageSerializationBoundary` 之前，不在 `executePreparedIMEntry`。
R32（本次）：Horizon 不得进 `sessionLoops`，否则 TryInterrupt/序列化锁会吞掉续言。
R33（本次）：`CancelSessionForUser` 必须停 Horizon；停止按钮不发 `@horizon` 文本。
R34（本次）：首次返回后 OnProgress 已死，进度/心跳改走 `ai-assistant-*` 事件。
R35（本次）：ProjectRoot 来自消息工作区，禁止 `GetCurrentProjectPath()`。
R36（本次）：崩溃不自动重跑 Executor；下次消息询问是否恢复。