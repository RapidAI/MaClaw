# 多 Agent 实例性能优化计划

## 背景

目标：主 agent、最近任务打开的 agent、多个最近任务 agent，可以并行工作。不同实例之间不共享忙状态、不共享输入队列、不互相取消、不因为后处理或共享 runner 排队而拖慢可见结果。

用户感知问题：

- 单 agent 查询天气原本 1 分钟内完成，最近变慢。
- 主 agent 已经出结果，最近任务 agent 仍长时间“正在思考”。
- 多 agent 同时跑同类请求时，像串行执行。
- 已有结果后，后处理继续占用请求链路，输入体验差。
- 不打开最近任务 tab 时也变慢，说明不只是 tab UI 问题。

## 已确认现象

最近日志里，单 agent 天气请求暴露三类问题：

1. `agent-loop` 本体约 65s 完成，但 Wails 层 `async done` 记录约 120s。中间有约 60s `handler_tail/stream_tail`。说明可见结果后仍有同步尾部工作或计时口径把后续工作算进用户等待。
2. 简单“北京天气”请求中出现 stale `InFlightTask`，project 指向测试临时目录：`TestDirectCodingSubAgentRouteAllowed...`。这会污染上下文、扩大 prompt、触发不该触发的任务恢复/编码上下文。
3. 同一时间存在大量 `caller="unknown"` 后台 LLM 请求和 `memory_sync`。这些不是前台 agent loop 本体，但会争用 LLM/provider/内存管线资源。

## 已完成修复基线

这些已经做过或正在当前改动集中体现，后续计划以它们为前提：

- `session_key` 贯穿 Wails 和前端事件，主 agent 用 `desktop-user`，最近任务实例用 `desktop-user:{projectPath}`。
- 前端 busy、streaming、progress、input queue、attachments 已逐步按 `sessionKey` 分离。
- IM serialization 改为 per-session mutex，不同 `userID` 不共用同一把 loop 锁。
- `activeAIAssistantLoopUserID` 不再用 legacy `lastUserID` 猜最近任务 owner。
- project tab 发送路由改成读取最新 active tab 和 session key，避免 stale closure 发错 agent。
- 终端响应丢失 placeholder 时，前端会按 `session_key` 重建占位消息。
- `InFlightTask` project 不再从主 agent 的全局 current project 猜，避免主 agent 天气请求被项目路径污染。
- in-flight recovery 只在同 session 没有活跃 loop 时消费 marker，避免活跃请求期间恢复 stale task。
- active remote session bonus round 改为按 project scope 判断，主 agent 不再因为其他 project session active 而进入额外 bonus round。
- 后处理开始拆出关键路径：conversation sediment、FTS transcript、semantic dedup、online extraction、session-start extraction、compression summary、post-loop evidence/workflow/experience 进入后台。
- 增加诊断日志：`[agent-loop tool] done`、`[post-conversation] start/done`、`[post-loop] done`、AI assistant enqueue/start/done、stream emit route 等。

## 性能原则

1. 可见结果优先：用户看到最终回答后，不应再被非必要后处理阻塞。
2. owner 是隔离边界：所有状态、取消、队列、runner、MCP session、browser session、memory work 都必须能追溯到 owner。
3. 同 owner 串行，不同 owner 并行：只允许同一 agent 实例内部排队。
4. 后台 LLM 低优先级：后台抽取、去重、沉淀不能和前台 agent 抢同一个 owner 的关键请求；新前台输入应取消同 owner 后台任务。
5. 共享资源短锁化：锁内只做状态读写，不能持锁执行 LLM、skill、MCP、browser、磁盘长 I/O。
6. 没 owner 就 fail closed：agent loop 工具路径不能 fallback 到 `lastUserID/currentLoopCtx` 猜 owner。

## 当前主要根因判断

### 1. 后处理仍可能影响用户等待

已做第一轮异步化，但还需完整审计所有 `saveConversationHistoryTimed` 和 `finalizeIMAgentLoopResponse` 调用点。

风险点：

- response 已有 text，但函数返回前还做 trace finalization、TTS、workflow auto advance、pending reply classification、conversation trim、memory flush。
- 部分测试暴露 Windows `knowledge.db` cleanup 锁，说明后台 goroutine 可能持有 DB handle 超过测试生命周期。生产上不是直接失败，但说明后台任务生命周期需要可控。

计划：

- 将后处理分级：`critical before return`、`async immediate`、`async idle`。
- `critical before return` 只保留：response 字段修正、必要 history save、必要 session state cleanup。
- `async immediate`：trace terminal event、recent task sediment、workflow doc capture、transcript index。
- `async idle`：semantic dedup、online extraction、session start extraction、memory pipeline、experience extraction。
- 为后台任务引入 owner-scoped worker/queue，避免无限 goroutine 和测试 DB 锁。

### 2. 后台 LLM 仍可能和前台 LLM 竞争

日志中 `caller="unknown" owner=""` 太多，无法判断来源。LLM 支持多并发不代表本地代理、网关、限流、连接池、token 预算没有竞争。

计划：

- 所有 LLM 请求必须带 trace：`caller`、`owner`、`request_id`、`loop_id`、`priority`。
- `caller="unknown"` 视为诊断缺陷，逐步清零。
- LLM dispatch 增加轻量 priority：foreground > tool-followup > background-memory。
- 同 owner 新前台请求取消旧后台 LLM；跨 owner 不取消，但后台请求可被全局低优先级限流。
- 日志增加 `queue_wait`、`http_do`、`first_sse`、`stream_tail`、`provider_status`，区分 LLM 慢还是 agent 慢。

### 3. SkillRunner/MCP/Browser 共享层可能隐藏串行点

用户关注 skillrunner 是对的。即使 skill 本身是外部慢，runner 也是 agent 的一部分，必须多实例安全。

待审计点：

- skill runner 是否有全局执行锁、全局 env lock、全局 stats/save 锁。
- skill install/scan/cache 是否在运行时持锁，影响 run。
- MCP local/remote 是否按 owner 建 session，还是共享单 client。
- browser 默认 session 是否 owner-scoped，还是所有 agent 复用同一 browser page/profile。

计划：

- SkillRunner：执行阶段不得持全局锁；stats/save 改短锁快照 + 后台 flush；process env 用 per-command env，不用全局 env mutation。
- MCP：local process/session key 必须包含 owner；remote session init 用 `(serverID, ownerID)` singleflight，只串行同 owner 同 server 初始化。
- Browser：默认 session 改为 owner-scoped singleton。同 owner 复用，不同 owner 独立 page/session，必要时独立 profile。
- 每个工具执行日志统一输出：`owner`、`tool`、`wait`、`exec`、`result_len`、`session_id`。

### 4. Legacy global loop 仍是交叉污染风险

`currentLoopCtx`、`lastUserID`、`lastUserText` 仍存在。并发 loop 下，最后启动的 loop 会覆盖全局字段。任何工具或辅助逻辑读这些字段，都会错 owner、错 working dir、错 cancellation、错 busy 状态。

计划：

- 全量搜索 legacy runtime 读取点：`currentRuntimePolicyOwnerState`、`currentRuntimePlatform`、`getCurrentProjectPath`、`lastUserID`、`currentLoopCtx`。
- agent loop 路径内，工具必须通过 `LoopContext.Runtime.PolicyOwnerID` 或显式参数取 owner。
- 桌面手动工具面板可以保留 legacy fallback，但 agent loop 内禁止。
- 增加测试：两个 owner 同时运行，工具看到的 owner/project 不会互换。

### 5. 最近任务恢复和实例身份必须稳定

性能问题和身份问题会互相放大。如果同一最近任务反复 fork，新实例会创建新上下文、新 recent task、新 memory/task sediment，导致上下文越来越重。

计划：

- 最近任务条目持久化 `task_instance_id`、`source_project_path`、`fork_project_path`、`display_name`。
- 同一 recent task 重复打开：如果已有 fork instance，激活或恢复同一 instance，不再创建新 fork。
- 关闭 tab 只 detach，不删除 task instance。
- 删除 recent task 才清理映射和输入队列。
- tab title 从 persisted task metadata 恢复，不依赖运行时内存。

## 分阶段实施计划

### Phase A: 诊断闭环

交付：能从日志判断慢在哪一层。

- 清理 `caller="unknown"`：至少覆盖 agent loop、task context、memory extraction、dedup、skill parser、capability search、ping/warmup。
- 增加 per-owner timeline：Wails accept、serialization wait、loop start、LLM start/first token/end、tool start/end、post-loop start/end、emit response。
- 增加 tool duration 统一日志，包含 skill/MCP/browser 子类型。
- 增加后台任务日志：开始、结束、取消、是否抢占、队列等待。

验收：一次主 agent + 两个最近任务 agent 同时跑天气/skill/MCP/browser，日志能画出三条独立 timeline。

### Phase B: 后处理完全退出用户关键路径

交付：最终 token/response emit 后，非必要工作不阻塞输入、不占用 active busy。

- 定义 `PostConversationScheduler`，owner-scoped queue，支持 foreground cancel。
- `saveConversationHistoryTimed` 只同步做必要保存和 pending state。
- trace、sediment、experience、memory extraction、dedup 进入 scheduler。
- 后台任务不直接改当前 response，只写持久状态或发独立事件。
- 测试模式下 scheduler 可 drain 或关闭，避免临时 DB 锁。

验收：`handler_tail` 正常接近 `memory_save + finalize_trace`，不再出现无解释 60s 尾巴。

### Phase C: owner 显式传递和 legacy global 收口

交付：agent loop 内不再靠全局 current loop 猜 owner。

- 审计并改造所有工具 owner 获取点。
- `executeToolDetailedWithRuntimeState` 必须从 runtime state 注入 owner/platform。
- `getCurrentProjectPath` 在 agent loop 内改为从 owner/projectPath 解析。
- 对缺 owner 的 agent-loop tool call 打警告并 fail closed。

验收：并发两个 project agent 时，工具日志 owner/project 全部正确，无 legacy fallback 命中。

### Phase D: SkillRunner/MCP/Browser 并发安全

交付：共享 runner 不造成跨 owner 串行。

- SkillRunner 拆锁：run 不持全局 registry/stats 锁；stats 异步 flush。
- 移除或缩小 process env 全局锁，改 per-command env。
- MCP local/remote session 全部 owner-scoped，并记录 init wait。
- Browser 默认 owner-scoped session，跨 owner 不复用同一 page。

验收：两个 owner 同时调用同一 skill/MCP/browser，除外部服务自身耗时外，本地 wait 不超过可接受阈值。

### Phase E: 最近任务实例身份稳定化

交付：同一最近任务不重复 fork，重启后名字和实例都稳定。

- 持久化 recent task instance map。
- 重复打开已有任务激活同 instance。
- 关闭 tab 后再次打开仍用同 instance。
- 删除任务清理映射、队列、tab 状态。

验收：重复双击、关闭再打开、重启再打开，recent task 列表不新增重复项，tab title 保持用户可理解名称。

### Phase F: 并发压测和回归

交付：可重复证明并行。

- Go：per-session serialization 并发测试、owner runtime 传递测试、post scheduler 不阻塞测试。
- Vitest：busy/input/queue/progress/session map 分离测试。
- 集成压测：主 agent + 两个 recent-task agent 同时跑同类任务。
- 日志断言：不同 owner 无同一 mutex wait，无错误 owner，无 unknown LLM caller。

## 首批建议开工项

建议确认后先做这四项，收益最大：

1. 补齐 LLM/tool/background trace 字段，清理 `caller="unknown"`。
2. 把后处理收敛成 owner-scoped scheduler，彻底从用户返回路径拆开。
3. 审计 SkillRunner/MCP/Browser owner-scoped 并发点，先修 browser default singleton 和 skillrunner 全局 env/flush 锁。
4. 加并发压测，主 agent + 两个最近任务 agent 同时跑，作为后续每次修改的验收线。

## 不做的事

- 不用前端 setTimeout 或 loading 文案掩盖慢。
- 不通过随机新 fork 避免状态冲突。
- 不把所有后台任务全局停掉，只做 owner-scoped 取消和低优先级调度。
- 不把 skill/MCP/browser 慢简单归为外部原因，runner 层仍要可并发、可诊断。

