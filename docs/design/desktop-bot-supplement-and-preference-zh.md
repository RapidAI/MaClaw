# 桌面 Bot 任务补充信息与用户偏好演进设计

状态：**已实施**（v6，代码落地并通过编译/测试/守卫；待端到端联调与 P5 探针）

> **v6 实施记录**（详见文末「七、实施结果」）
> - P0/P-negative/P2/P3/P4 全部落地并通过 `go build` + 8 个不变量测试。
> - 新增 UI 守卫 11 条钉住四跳链路，`check-main-ui-guards.mjs` 反向验证可捕获回归。
> - ⏳ **未完成**：P5 端到端探针（硬伤 8）、跨进程联调、GUI 手工验收。

> **变更历史**
> - v2：3.1c 第一轮三个硬伤（P3 拿不到 runID、P5 写错机器、loopID 不可做 key）；
>   插入 P0；P2 补向下兼容；P3 补并发约束；P5 改双写。
> - v3：3.1b 硬伤 4（上下文污染 → 单条轮转 SUMMARY 重写钩子）；
>   3.1d 问题 5（AskUser 提前杀死 run，回答静默丢失）、问题 6（missed 时序）。
> - **v5（本轮，收口）**：①**待拍板第 6 条结案——P5 路径成立**
>   （`inst.DataDir` 与 `userDataRoot` 同源，已 traced 到 `service_instances.go:42`
>   → `core_agent_executor.go:615` → `cb.memory`）；②但因此**新发现硬伤 8**：
>   两侧同目录却可能以不同 mode 打开，且 worker 侧 store 是**永不过期的进程内缓存**
>   （`core_agent_executor.go:958-973`）→ 偏好写进去 worker 可能读不到，
>   症状间歇且难定位；③不再逐个补洞，改用 **3.1g 的六条不变量**重述 contract，
>   并把它直接翻译成 P3 的 Definition of Done。
>
> **开工前仍需确认**：硬伤 8 的跨实例可见性（唯一剩余的技术不确定性）。
> **其余待拍板已全部结案**。

范围严格限定在**桌面 Bot**（`guiapp/desktop_bot_*` 链路）。
IM / QQ bot / 本地 AI 助手的 `pendingInjection` 已实现，不在本次范围内，仅作参考实现。

---

## 一、需求与结论速览

| # | 需求 | 现状 | 本次要做的 |
|---|---|---|---|
| 1 | 规划阶段补充信息 | ✅ 已可用（prompt 融合） | 加结构化断言，防模型偷懒 |
| 2 | 执行中注入 + 状态报告 | ❌ 完全不通 | P1→P5 分期打通 |
| 3 | 用户偏好越用越聪明 | ⚠️ 有存储无闭环 | 沉淀到 memory + 执行侧可见 |

---

## 二、架构事实（已逐条核实）

### 2.1 四跳调用链

```
GUI  guiapp/desktop_bot_hub.go:435
     POST {hub}/api/v1/bots/{botID}/messages   Prefer: respond-async
  ↓ HTTP
Hub  hub/internal/httpapi/router.go:435 → bot_user_handler.go:185
     PostBotMessageHandler → svc.AdmitDesktopMessage
     hub/internal/botmgmt/access.go:509  authAsOwner POST /instances/{id}/messages
  ↓ HTTP
MaClawSrv  MaClawSrv/http_routes_user.go:104 → http_agent.go:183 handleSendMessage
     :257 SendMessageAsync
  ↓
     corelib/agentservice/service_messaging.go:625 PostMessageAsync
     :651  go func(){ ... s.PostMessage(...) }        ← goroutine
     :312  buildEffectiveUserContent(sess, content)
     :315  run := Run{ID: NewID("run"), Status: running}   ← run_id 生成点
     :337  saveRunAdmissionWithEvents → SQLite
     :404  execCtx, cancelExec := context.WithCancel(ctx)
     :450  registerRunCancel(run.ID, cancelExec)
     :480  s.executor.Execute(execCtx, execReq)
  ↓
     corelib/agentservice/core_agent_executor.go:534 Execute
     :609  executeDirectWithRuntimeBinding → 构造 coreAgentCallbacks
     :746  agentruntime.RunAgentTurnWithUserContent(cb, userText, userContent, history, client)
  ↓
     corelib/agentruntime/turn.go:18 → corelib/agent/loop.go:783 RunLoopWithUserContent
     :1067  for iteration := 0; iteration < maxIter; iteration++
     :1094  h.TransformConversation(conversation)      ← 每轮注入点（目前空转）
  ↓ docker exec
     容器 maclaw-gui
```

**决定性事实**：真正的 agent loop 在 **MaClawSrv 进程**。
GUI 那个 `go`（`desktop_bot_hub.go:2542`）只负责 HTTP 轮询。
因此「运行中注入」必须在服务端实现，GUI 本地做不了。

### 2.2 需求 1 现状：已经可以

`guiapp/desktop_bot_understand.go`

- `:84` `desktopBotUnderstandEarlierMax = 12`
- `:146-148` prompt 要求：前面有正在规划的任务、最新消息是补充时，
  **必须融合成完整更新后的工单**，不得当作孤立请求
- `:411` `desktopBotPhase()`：非 `execute` 一律 `plan`
- `:321-334` `desktopBotPace()`：plan 阶段缺省 `confirm`

软肋（非阻断）：纯靠模型遵守 prompt，无结构化校验；
`desktopBotProtocolUnderstanding`（`:418`）对确认句原样转发，不参与融合。

### 2.3 需求 2 现状：三个缺口

**缺口 A — 无跨进程写通道**

全仓库 `/runs/` 只有 GET 与 cancel（已核实 `MaClawSrv/http_routes_user.go:118-121`）：

```
GET  /api/v1/instances/{i}/runs
GET  /api/v1/instances/{i}/runs/{r}
GET  /api/v1/instances/{i}/runs/{r}/events
POST /api/v1/instances/{i}/runs/{r}/cancel
```

cancel **未注册**到 Hub 的 `/api/v1/bots/` 路由（已核实 `router.go:430-442`），
GUI 侧根本调不到 —— 用户连「停掉当前任务」都做不到。

**缺口 B — 每轮钩子是空转**

`loop.go:1094` 每轮调 `h.TransformConversation(conversation)`，是天然注入点。
但 `core_agent_executor.go:746` **没传 hooks**（已核实，只有 5 个实参）：

```go
result := agentruntime.RunAgentTurnWithUserContent(cb, req.Message.Content, userContent, cb.history, cb.httpClient)
```

`RunAgentTurnWithUserContent` 的 hooks 是可变参数（`turn.go:18`），
缺参走 `loop.go:785` 的 `DefaultLoopHooks{}`，
其 `TransformConversation` 返回 `nil`（`loop.go:727`）——纯空转。
`LLMReplanAware`（`loop.go:1087`）在 `agentservice` / `MaClawSrv` 中零命中。

**缺口 C — GUI 三个半成品互不相连**

- `desktop_bot_hub.go:317` `InjectDesktopBotSupplement` → 只往
  `ActiveDesktopTask.Supplements` append，**全仓库无消费点**（死代码）
- `desktop_bot_understand.go:632` `EvaluateDesktopTaskSupplement` **从未被调用**
- 前端 `DesktopBotWorkspace.tsx:10,14` import 了这两个函数，**全文件 0 处调用**
- `desktop_bot_hub.go:340` `registerActiveDesktopTask` 返回值被丢弃，没存 runID

副产品隐患：`ActiveDesktopTask` 内嵌 `sync.Mutex`（`:268`），
而 `GetActiveDesktopTask:311` 做 `copied := *task` —— 复制含锁结构体，`go vet` copylocks 会报。

### 2.4 需求 3 现状：存储有，闭环缺

**已有**：

- `corelib/memory/prompt_context.go:103` `UserPreferenceSummaryForPrompt`
  扫描 `CategoryPreference` / `CategoryInstruction` / `"feedback"`
- `desktop_bot_understand.go:214` 已调用，注入前台 prompt（`:437`）
- system prompt `:144` 已要求「用户喜欢多图则要求 worker 多截图」
- `DesktopBotUnderstanding.Persona`（`:36`）承载身份+偏好，`-` 表示撤销
- 写入 API 现成：`Service.SaveUserMemory`
  （`corelib/agentservice/service_user_memory.go:115`，
  内部 `store.UpsertEntriesByID`，`normalizeUserMemoryCategory` 支持 `CategoryPreference`）

**缺失**：

1. **沉淀断链**：LLM 产出的 persona 只进前端 localStorage
   （`desktopBots.ts:485` `saveBotPersona`），**不写 `corelib/memory`**，
   所以 `UserPreferenceSummaryForPrompt` 下一轮读不到本轮学到的内容。
2. **执行侧看不见偏好**：偏好只影响前台理解阶段（`desktop_bot_understand.go`），
   真正干活的 MaClawSrv agent loop **完全不知道**用户喜欢多图。
3. **无结构化**：偏好没有来源 / 置信度 / 生效范围，
   无法区分显式声明与行为推断，也难以单条撤销。

### 2.5 可复用资产

`corelib/agentservice/ask_user.go` 的 pending-ask 机制证明两件事：

- `buildEffectiveUserContent`（`ask_user.go:18`）在 `service_messaging.go:312`
  把用户回答改写成「这是对当前任务的补充/纠正」语义，措辞可直接复用
- **`Session.Metadata` 是这条链路跨 turn 传状态的标准载体**
  （`setPendingAskUserMetadata` / `clearPendingAskUserMetadata`）

局限：它是 **turn 级**的 —— 上一次 turn 必须已终止并留下 `pending_ask_user=true`，
下一次 turn 入口才读。不是运行中注入。

---

## 三、实现方案

### 3.1 总体：一条「补充总线」，两种投递

核心抽象：**run 级补充队列（Supplement Queue）**。
一个 run 从 admit 到 terminal 之间，可以接收任意多条用户补充。
按运行状态自动选择投递方式：

```
                用户新消息
                    ↓
        EvaluateDesktopTaskSupplement（已有分类器）
                    ↓
          ┌─────────┴─────────┐
      is_supplement        new task
          ↓                    ↓
   有活跃 run？             照常 SendDesktopBotTask
    ┌────┴────┐
   有      无 / 已结束
   ↓          ↓
 注入总线   降级为新一轮融合（走 plan 期融合）
```

- **热路径**（run 正在跑）：写入队列 → 下一轮 `TransformConversation` 取走 → 生效
- **冷路径**（run 已结束 / 未启动）：走 plan 期 prompt 融合（P2 加固版）

两条路径共用同一个 `RunSupplement` 数据结构，保证语义一致。

### 3.1a 第三轮 review：并发与结构假设不成立（v4，最高优先级）

> 前几轮假设「一个 bot 同一时刻只有一个活跃 run」。**这条不成立**，
> 而它同时支撑着 P1 的 `map[botID]` 和 P3 的队列设计。必须先把这个地基问题说清楚。

**硬伤 7 — 同一 bot 可以并发多个 run，一对一假设不成立**

核实结果（`PostMessage` 入口 `service_messaging.go:199-230`）：

- 入口**只有** `GetTenant` / `GetUser` / `GetInstance` / `inst.Ready` 校验，
  **没有任何 running-run 互斥检查**
- 既存的 `hasRunningRuns`（`service_runs.go:209`）只被**删除 instance**
  的 blocker 检查调用（`:225` `collectRunningRunBlockers`），
  **不用于阻止新消息**
- `PostMessageAsync:625` 直接 `go func()` 起新 goroutine，无串行队列
- `service.go` / `botmgmt` 侧均无 semaphore / singleflight 类原语
  （grep `Semaphore|singleflight` 零业务命中）

再看 GUI：`SendDesktopBotTask`（`app_wails_bindings.go:2519`）
拿到 text 后直接 `go a.finishDesktopBotTask(...)`，**发送侧也没有互斥**。

而 `registerActiveDesktopTask` 是 `activeDesktopTasks[botID] = task` ——
**map 单值覆盖**。于是：

```
用户快速连发 3 条 → 3 个 run 并发
  → activeDesktopTasks[botID] 只留下最后一个的前台 requestID
  → register/unregister 的 requestID 守卫让前两个任务的活跃标记被提前抹掉
  → 补充信息投向一個"看起来活跃、实际已完成"的 run
```

后果：P1 的路由基准本身就是错的，P3 的注入目标也可能选错。

**修法（必须做，且要早于 P1 落地）**：

把 key 从 `botID` 改成 **`botID + runID`**，结构改为多值：

```go
// key 必须到 run 粒度。一个 bot 可以同时有多个 run 在跑：业务层没有任何
// 互斥（见 3.1a 硬伤 7），GUI 侧也不阻止连发，单值 map 会让后到的 unregister
// 提前抹掉先到任务的活跃标记。
activeDesktopRuns map[string]*ActiveDesktopRun  // key: botID + "\x00" + runID

type ActiveDesktopRun struct {
    BotID, RunID, RequestID string
    SessionKey, TaskText, Phase string
    StartedAt time.Time
    // terminal 到达即摘除：含 succeeded+WaitingForUser（AskUser，见问题 5）
}
```

并明确两条语义：

1. **是否禁止并发** —— 建议本期**不禁止**（连发是用户习惯，禁了体验更差），
   而是支持多 run，由分类器决定补充投给哪一个。
2. **多 run 时如何选目标** —— 默认投给**最近一个仍在 running 的 run**；
   若不止一个，用 `EvaluateDesktopTaskSupplement` 的 activeTask 文本做相似度粗筛，
   无法确定时**反问用户**（"这条补充是给 XX 任务还是 YY 任务？"），
   **绝不静默挑一个** —— 挑错等于用户的话进了另一个任务的上下文，不可撤销。

> 这一条改变了 P1 的数据结构定义，属于设计级修正，不是实现细节。

---

### 3.1b Review 发现的三个硬伤（已修正，动工前必读）

> **第二轮 review 追加（v3）**：以上三条修完后我又核了一遍时序，
> 发现**第四条更隐蔽的问题**，见下方硬伤 4。它会让 P3 上线后
> 在长任务上逐步放大 token 消耗直至撑爆上下文，**属于上线后才暴露的类型**。

**硬伤 4 — 注入的消息会永久驻留并每轮重复（上下文污染，v3 新增）**

原方案的钩子是「每轮 append 一条 system 消息」：

```go
out = append([]interface{}(nil), conv...)   // ← 起点是"上一轮遗留的 conversation"
out = append(out, map[string]string{"role":"system", "content": ...})
```

致命点在于 **`conversation` 是跨轮持久的变量**（`loop.go:879` 构造一次，
之后只有 append，`2106-2540` 全是 append，无重建）。所以：

- 第 5 轮注入的补充，会随 `conversation` 一直活到第 6、7、8…轮
- 若用户补了 10 条，**每轮都带着这 10 条 system 消息进 LLM**
- 迭代上限可达 **300**（`config/manager.go:14` `MaxAgentIterationsCap`，
  下限也有 30，`:17`）→ 尾部每轮都在重复烧这 10 条的 token
- `FoldComputerUseObserves`（`:1097`）和 `redactConversationInterface`（`:1101`）
  **都不会清理它** —— 后者明确跳过 user/system（`memory_retraction.go:348`），
  前者只处理 vision 图片

结果是「一轮注入，永久占位」。短任务看不出来，长任务会明显拖慢、
飘 cost，并且噪声累积会稀释模型的注意力。

⚠️ 还要注意：这也解释了为什么 **IM 侧的做法是安全的** ——
`appendPendingSteerInjections`（`guiapp/im_agent_loop_iteration.go:52`）
用 `pendingInjection.LoadAndDelete`，**取走即删**，
每条只进一次，不会重放。我们恰好漏抄了这个最关键的性质。

**修法（三选一，推荐 A）**：

- **A（推荐）· 单条轮转 SUMMARY**：维护「已累计注入摘要」一条 system 消息，
  每轮**替换**而非 append。新补充到来时把内容并入这一条。
  恒定占一条，语义不丢，天然幂等，也不需要 `seen`。
- B · **追加后立即标记，末尾一次清理**：复杂且易漏，不推荐。
- C · **N 轮后折叠**：会有窗口期，不如 A 干净。

采用 A 后，钩子退化为「用一条不断更新的 system 消息表达全部补充」，
`TransformConversation` 的实现变成：找到那条消息 → 更新 content；
找不到 → 追加一条。**这个形态比原方案简单，且不需要 `seen` 去重。**

---

### 3.1d 第二轮 review 追加的两个可靠性问题（v3）

**问题 5 — AskUser 会提前杀死 run，用户回答被误判成新任务**

这是「补充 vs 新任务」判定里一个隐蔽但高频的坑。

核实 `service_messaging.go:531`：

```go
run.Status = RunStatusSucceeded          // ← AskUser 也是 succeeded
run.WaitingForUser = normalizeResponseSourceKind(...).IsWaitingForUser()
```

即 **AskUser 并非「暂停」，而是以一个 terminal 状态结束**，靠
`WaitingForUser=true` + `Session.Metadata[pending_ask_user]` 在**下一轮
新 run**里重建上下文。

后果：**bot 举手提问的那一刻，`run` 就已经死了**。此时：

- `GetActiveDesktopTask` 若仍持有旧 runID → `AppendRunSupplement` 返回
  `ErrRunNotRunning` → 走冷路径（P2 融合），勉强可用
- 但**更糟的是** GUI 若没及时清理 `activeDesktopTasks`，
  分类器会把用户那句回答判成「对已死 run 的补充」，
  注入失败，用户的回答**静默丢失**

而 AskUser 场景恰恰是补充信息最高频的时刻之一
（bot 问「要哪种格式」，用户答「要 markdown」）。

**修法（两件事，缺一不可）**：

1. **把 AskUser 的回答优先交给既有 pending-ask 通道**，不走补充总线。
   `EvaluateDesktopTaskSupplement` 应在首行判：
   若 `GetActiveDesktopTask` 对应的 run 已是 `WaitingForUser`，
   直接返回 `is_supplement=false, reason="pending_ask"`，
   让它落回 `SendDesktopBotTask` ——
   `buildEffectiveUserContent`（`service_messaging.go:312`）会自动把这句话
   重铸成「这是对上一问题的回答」。**这条已实现，别绕开它。**
2. GUI 的 `activeDesktopTasks` 必须在 run 到达任何 terminal
   （含 succeeded+WaitingForUser）时清理，不能只清理 failed/cancelled。

> 判据写进单测：run 处于 WaitingForUser 时的用户输入，
> 必须走 AskUser 重建路径，而不是 printed supplement 注入。

**问题 6 — `missed` 必须在 terminal 判定时扫描，且要给 GUI 一个补救出口**

原方案说「run 到 terminal 时扫未消费的补充标记 missed」，方向对但没说清挂点。
明确如下：`reapRunSupplements(runID)` 必须挂在
**`saveRunTerminalWithEvents` / `saveRunCompletionWithEvents` 之后**（即
`service_messaging.go` 各 terminal 分支），而不是只挂在正常完成那条路 ——
cancelled（`service_runs.go:71`）、failed、panicked（`failPanickedRun:128`）
都要覆盖，否则这些路径会漏掉 missed 上报。

同时给 GUI 留补救出口：`RunSupplements(runID)` 返回的票据里，
`missed` 项要带上**足以让前端一键重发**的内容原文，
让用户「复制重发」而不是只能眼看丢失。

---

### 3.1e 观测与隐私（v4 新增，原文档完全缺失）

前几版只讲了功能，没讲"上线后怎么知道它工作正常"以及"会不会泄露用户的话"。
这两块必须补，否则 P3 上线后是黑盒。

**观测（一律用 `corelib/botlog`，GUI/Srv 两侧都要打）**

`botlog`（`corelib/botlog/botlog.go:71` `Write` / `:78` `WriteOnce`）
已经是这条链路的标准观测通道，且有 per-bot 独立日志文件。
必打的 stage：

| stage | 位置 | 关键字段 |
|---|---|---|
| `gui.supplement_classified` | GUI 分类后 | run_id、is_supplement、reason、**是否走了 pending_ask 分支** |
| `gui.supplement_dispatched` | GUI 派发前 | run_id、target_count、selected_run_id |
| `srv.supplement_queued` | `AppendRunSupplement` | run_id、supplement_id、queue_len |
| `srv.supplement_applied` | 钩子取走时 | run_id、supplement_id、**iteration**（关键：验证真在跑） |
| `srv.supplement_missed` | reap 时 | run_id、supplement_id、terminal_status |

⚠️ `srv.supplement_applied` 的 **iteration 字段是 P3 唯一的"真的生效"证据**。
没有它就无法区分「入队后被消费」和「入队后烂在队里」。这一条必须在
验收时 grep 出来才算 P3 通过。

**隐私（必须做）**

1. 补充内容是**用户原话**，会进入模型上下文，也可能被落进 session metadata。
   `botlog 打的时候只打长度和 id，**绝不打 content 原文**` ——
   现有 `gui.enqueue`（`app_wails_bindings.go:2535`）就是这么做的
   （只打 `text_len`），沿用即可。
2. `botlog.redact`（`botlog.go:146`）只拦 bearer / sk- / json secret / handoff URL
   这几类正则，**拦不住用户随手贴的密码**。所以更稳的做法是**根本不把 content
   传进日志字段**，而不是指望脱敏。
3. **`missed` 票据里的"原文一键重发"要重新审视**（3.1d 问题 6）：
   原文会经 Hub → GUI 落到前端。建议只在**内存票据**里带原文供本次会话重发，
   **不落库**，避免用户说过的敏感内容长期躺在服务端。

---

### 3.1f 降级与开关（v4 新增）

新增能力必须有熔断，否则一旦出问题只能回滚版本。建议内置三级降级：

| 级别 | 触发 | 行为 |
|---|---|---|
| 关 | 环境变量 `MACLAW_SUPPLEMENT=off` | 分类器短路为「新任务」，全部走既有 `SendDesktopBotTask` |
| 保守 | 拿不到 runID / 目标 run 不唯一 | **不注入**，返回「请在新一轮里说明」，宁可保守 |
| 开 | 正常 | 走总线 |

⚠️ 「拿不到 runID 就保守降级」这条**必须与硬伤 1 的修法配套**：
即使哪天 `ExecuteRequest.RunID` 被重构下线，也只是功能静默失效，
而**不会退化成按 SessionID 猜测导致串台**。把 fail-safe 钉死在代码里，
不要靠人工记得。

---

### 3.1g 第五轮 review：从不变量视角重构 contract（v5）

> 前四轮的思路是「逐个补洞」。每次补完又发现新洞，说明**上面的抽象层次不对**。
> v5 换成**先定义不变量、再由不变量推导结构**的顺序，并顺手验证了两条悬置假设。

#### 悬置假设已结案：**第 6 条成立，P5 路径可用**（附一个新风险）

`ExecuteRequest.DataDir`（`service_messaging.go:413`）来自 `inst.DataDir`，
而 `inst.DataDir` 在实例创建时被写成 `s.userDataRoot(...)`
（`service_instances.go:42`）。于是：

```
Service.openUserMemoryStore(p)  → userDataRoot(tenant,user)   [service.go:576]
CoreAgentExecutor.resourcesForUser(..., req.DataDir)
                                → 同一个 inst.DataDir          [core_agent_executor.go:615]
cb.memory ← resources                                          [:659]
```

**两者指向同一目录同一份 `agent_memory.json` → 服务端写入 worker 可读，
P5 主路径成立。**第 6 条可以从"待拍板"结转为"已核实"，不必再阻塞开工。

⚠️ **但发现一个新风险（硬伤 8）**：两者用**不同的 mode** 打开同一目录。

| 打开方 | mode | 位置 |
|---|---|---|
| `Service.openUserMemoryStore` | `StoreModeAuto` | `service_user_memory.go:226` |
| `CoreAgentExecutor.resourcesForUser` | `StoreModeAuto` | `core_agent_executor.go:967` |
| GUI 本机 | `StoreModeSQLite`（**显式**） | `app.go:1058` |

两边虽都是 Auto，但 `resolveMode`（`store_factory.go:175`）的判定是
**看 `memory.db` 在不在**：存在走 SQLite，**不存在降级 JSON**。
且 `CoreAgentExecutor.userMemory` 是**长期缓存 map**（`:958-973`，
进程内永不失效），而 `Service` 侧每次都新开一个新 `*memory.Store` 实例
（`:224` open → `defer store.Stop()`）。

后果：

1. **写后读不到**：Service 侧写入 SQLite 并提交，worker 那个缓存 store
   若持有旧快照，不重新加载就读不到新偏好 —— 这正是"越用越聪明"
   最容易卡住的一环，且**症状是间歇性的**（取决于 warm-up 顺序），很难定位。
2. **双写冲突**：两个 `*Store` 实例同时持有同一 SQLite 文件的写句柄。
3. GUI 侧显式 SQLite 而服务端可能落 JSON，**两份数据格式都可能不同**。

**修法**：不要靠运气。二选一，建议 **A**：

- **A（推荐）**：给 `CoreAgentExecutor.resourcesForUser` 的缓存加**强制同 mode**
  —— 不允许依赖 `resolveMode` 的结果推断，改为显式指定与 Service 侧一致的模式；
  并检查/confirm SQLite backend 的跨实例可见性（WAL？每次 new 实例？）。
  P5 首个任务必须做一次**跨实例读写验证**：Service 写一条 → worker 立即读，
  读不到就要引入失效/重开机制。
- B：worker 不用缓存 store，每次迭代读 Service 侧那份（成本高，但一致）。

> 判据写进 P5 验收：「服务端写偏好 → worker 在同一进程后续 turn 内能读到」
> 必须有一条端到端测试，不能只测写入成功。

#### 用不变量重述整体 contract（这才是主线）

前面四个硬伤其实是同一件事的四个侧面：**我们把"用户补充"当成"往对话里塞内容"，
而它真正的语义是一次有状态的、可失败的状态转移。** 用不变量表达更稳：

| # | 不变量 | 由谁保证 | 破了会怎样 |
|---|---|---|---|
| I1 | 一条补充**至多被投递一次** | `take()` 取走即标记 applied | 重复注入 → 模型看到重复指令 |
| I2 | 一条补充**至多影响一个 run**，且必须是用户选定的那个 | runID 复合校验 + 多候选反问 | 串台 → 不可撤销地污染他人/他任务上下文 |
| I3 | 补充**在对话中恒定占 O(1) 空间** | 单条轮转 SUMMARY | 上下文污染 → 长任务成本失控（硬伤 4） |
| I4 | 补充的**终局对用户可见**（applied / missed 二值，无第三种） | reap 挂所有 terminal 分支 | 用户以为生效其实没生效（问题 6） |
| I5 | 拿不到 runID 或目标不唯一时**宁可不注入** | 降级开关 3.1f | 退化成猜测 → 串台 |
| I6 | 补充内容**不进日志、不落库**（仅内存票据） | 3.1e 隐私约束 | 用户隐私长期躺在服务端 |

**这六条不变量应当直接翻译成六个测试用例**，比逐个功能测试更能防回归。
建议 P3 的 Definition of Done 就是这六条全绿。

---

### 3.1c 第一轮 review 的三个硬伤（v2，已修正）

第一轮方案有三处经核实站不住，已在下文对应小节就地修正。留档以便 review 溯源：

**硬伤 1 — P3 的钩子拿不到 runID（会导致串台）**

原方案写「`TakePendingRunSupplements(runID)`」，但核实后发现：
`ExecuteRequest`（`types.go:1078`）**根本没有 RunID 字段**（grep 计数为 0），
`coreAgentCallbacks`（`core_agent_executor.go:338`）**也没有**。
2864 行的 `loopID` 是 `srv:{sessionID}:{userID}`，**不含 runID**。

即：在 `core_agent_executor.go:746` 那个位置，**没有任何东西能告诉你当前是哪个 run**。
若按原方案退而用 `SessionID` 做 key，同一会话先后两个 run 会互相串台 ——
补充会投给错误的 run。**这是数据安全级别的错误，不是小瑕疵。**

修法：`ExecuteRequest` 增加 `RunID string \`json:"-"\``（沿 `TaskRelation` 同款
host-only 约定），由 `service_messaging.go:404` 构造 `execReq` 时填 `run.ID`。
详见 3.4 第 0 步。

**硬伤 2 — P5 的 GUI 直连 memoryStore 写到了另一台机器**

原方案说「GUI 侧 `a.memoryStore` 已存在，可直连」。核实后两者**不是同一份数据**：

| | GUI | MaClawSrv worker |
|---|---|---|
| 构造 | `guiapp/app.go:1058` `memory.OpenDataDirStore(baseDir, StoreModeSQLite)` | `service_user_memory.go:226` `OpenDataDirStore(dataDir, StoreModeAuto, <dataDir>/agent_memory.json)` |
| 位置 | 用户本机 | **云桌面服务端** |

桌面 bot 的执行体在服务端。GUI 在本机写偏好 = **写到用户电脑上，
干活的 worker 在那个文件的另一份副本里，永远读不到**。原方案这一步白做。

好消息：`MaClawSrv/http_routes_user.go:51` **已有**
`POST /api/v1/memory` → `handleCreateMemory`。走这条现成 HTTP 路由即可，
不必造新 API。详见 3.6 断点 1（已重写）。

**硬伤 3 — `loopID` 不能当 key**

承硬伤 1，`loopID` = `srv:{sessionID}:{userID}` 在同一会话的两个 run 间恒定。
任何用 `loopID` 或 `SessionID` 给补充队列分桶的做法都会串台。必须以真 runID 为准。

---

### 3.2 P1 — GUI 侧接线（清死代码，建最小闭环）

**`guiapp/desktop_bot_hub.go`**

> ⚠️ **本节的①~④已被 3.1a 硬伤 7 取代**。原写法建立在「一个 bot 只有一个活跃 run」
> 的错误假设上（已证伪）。下面①~④保留作历史留档，**实现请以 3.1a 为准**
> —— key 必须是 `botID+runID` 多值结构，而不是这里的 `map[botID]` 单值。

1. 修 copylocks：`ActiveDesktopTask` 去掉内嵌 `sync.Mutex`，
   改由 `activeDesktopTasksMu` 统一保护（这本就是它的职责）。
2. `ActiveDesktopTask` 增加 `RunID` 字段（**3.1a 改为并入 key，不再只是字段**）：

```go
type ActiveDesktopTask struct {
    BotID       string
    RunID       string    // 新增：admit 后回填
    RequestID   string
    SessionKey  string
    TaskText    string
    Phase       string
    StartedAt   time.Time
    Supplements []string  // 保留，作为本地审计快照
}
```

3. `finishDesktopBotTask:340` 接住返回值：

```go
task := a.registerActiveDesktopTask(botID, requestID, sessionKey, text, phase)
defer a.unregisterActiveDesktopTask(botID, requestID)
```

runID 回填点在 `relayDesktopBot:439-445`（该处已拿到 `out.RunID`），
回填后通知持有的 `task`。

4. 新增导出方法 `AppendDesktopBotSupplement(botID, supplement string) (*DesktopBotSupplementReceipt, error)`
   取代无人调用的 `InjectDesktopBotSupplement`。返回**三级状态票据**：

```go
type DesktopBotSupplementReceipt struct {
    Accepted     bool   `json:"accepted"`     // 已进入队列
    RunID        string `json:"run_id"`
    Stage        string `json:"stage"`        // queued | dispatched | no_active_run
    Message      string `json:"message"`      // 给用户看的一句话
    SupplementID string `json:"supplement_id"`
}
```

**`guiapp/frontend/src/components/bots/DesktopBotWorkspace.tsx`**（3635 行）

在发送流程里插入分类分支：

```ts
// ⚠️ 见 3.1a 硬伤 7：可能返回多个候选，不能取第一个就用
const candidates = await GetActiveDesktopRuns(botId)
if (candidates.length > 0) {
  const verdict = await EvaluateDesktopTaskSupplement(candidates[0].TaskText, text, lang)
  if (verdict.is_supplement) {
    // 多候选时必须让用户选，禁止静默挑一个
    if (candidates.length > 1) { /* 走 Ambiguous 反问分支 */ }
    const receipt = await AppendDesktopBotSupplement(botId, targetRunId, verdict.instruction || text)
    pushBotMessage({ role: 'assistant', content: receipt.Message })
    return                       // 不发新任务
  }
}
// 否则走现有发送路径
```

**`guiapp/desktop_bot_understand.go`**

`EvaluateDesktopTaskSupplement:632` 已有完整实现（LLM +
`heuristicDesktopTaskSupplement:677` 关键词兜底），**保持不动**，只接进调用链。

**交付标准**：P1 结束时补充信息有正确路由与诚实回执。
此时尚无真正执行中注入，回执必须如实说「已记录，将在下一步生效」——
**不许假装已生效**。

### 3.3 P2 — 规划阶段融合加固（低成本，立竿见影）

1. **扩大窗口**：`desktopBotUnderstandEarlierMax` 12 → 20（`:84`）。
   规划阶段的递进补充往往需要更长上下文。
2. **加结构化输出字段**：给 `DesktopBotUnderstanding` 增加

```go
// SupplementOf is non-empty when this message was read as supplementary
// information for an earlier planned task rather than a new request.
SupplementOf string `json:"supplement_of,omitempty"`
```

   让模型显式声明「这是对前一条的补充」，便于前端区分渲染
   （显示"已合并到上一个任务"）与后续审计，也倒逼模型不做孤立处理。

   ⚠️ **必须同时写好向下兼容**：该字段是**新增**的，而模型可能在很长一段时间里
   不输出它（尤其 reasoning 模型的输出不稳定）。因此：
   - `SupplementOf` 为空 **不等于**「不是补充」—— 老行为照旧，不得因为缺这个字段
     就把消息判成新任务，**否则是一次静默的功能回退**
   - 它只作为**增强信号**（命中则前端可渲染"已合并"标记），不作为判定依据
   - 真判定仍靠 `desktopBotUnderstandEarlierMax` 窗口 + prompt 融合 + P1 的分类器

3. **加解析守卫**：在 `lastDesktopBotUnderstanding:503` 里，
   若 `SupplementOf != ""` 而 `Instruction == ""`，视为无效 reading
   （补充必须产出合并后的工单，空工单说明模型没融合）。
   ⚠️ 注意此守卫**必须硬失败成 `unread`**，而不是悄悄降级 ——
   宁可让用户看到"这次没读懂，请再说一遍"，也不要发出一个没融合的残缺工单。
4. **`desktopBotProtocolUnderstanding:418` 放行补充句**：目前确认句是原样转发，
   补充句若恰好等于协议常量会被误吞。加一条 plan 期豁免。

### 3.4 P3 — 服务端每轮注入（核心）

这是「向正在执行的任务注入」的本体，改动面比想象小。

**第 0 步（新增，原方案遗漏）—— 让 runID 可达**

这是整个 P3 的前提，原方案缺了这一步会让实现直接串行。

`corelib/agentservice/types.go:1078` 的 `ExecuteRequest` 增加字段，
沿用 `TaskRelation`（`:1106`）同款 host-only 约定 —— 必须显式排除 JSON，
否则请求正文、客户端工具或模型就能伪造 runID 去污染别人的会话：

```go
// RunID names the admitted run this turn belongs to. It is deliberately
// excluded from transport JSON: request text, a client tool, or a model must
// not be able to select another principal's run. The authenticated service
// ingress sets it from the run it created.
RunID string `json:"-"`
```

`corelib/agentservice/service_messaging.go:404` 构造 `execReq` 时填：

```go
execReq := ExecuteRequest{
    // ...既有字段...
    RunID: run.ID,        // ← 新增，紧邻 Session
}
```

⚠️ 注意 `service_messaging.go:462` 的 `agentruntime.Scope{... RunID: run.ID}`
是**另一条路**（runtime 分支），别只看到它就以为已经有传了 ——
当 `s.Runtime()` 为 nil 走 `:480` 的老 executor 分支时拿不到。

有了它，`core_agent_executor.go:746` 才能拿到 `req.RunID` 喂给钩子。
**不要用 `loopID` 或 `SessionID` 代替**（见 3.1b 硬伤 3）。

**第 1 步：建 run 级补充队列**

新文件 `corelib/agentservice/run_supplement.go`：

```go
// RunSupplement is one supplementary instruction aimed at a run that has
// already been admitted.
type RunSupplement struct {
    ID        string
    RunID     string
    Content   string
    CreatedAt time.Time
    State     string      // queued | applied | missed
    AppliedAt *time.Time
}
```

存储选型：**进程内 map 为主 + Session.Metadata 为审计留痕**。理由：

- run 本身就在 MaClawSrv 单进程内跑（GUI→Hub→Srv 三跳里只有 Srv 跑 loop），
  进程内存足够
- 纯进程内存重启即丢，但 run 重启也活不下来，语义自洽
- Session.Metadata 写持久层用于审计与「任务完成后回看收到了哪些补充」

（若后续确认 HA 多活部署会让同一 run 漂移，再升级为 SQLite 表 ——
与既有 `RunEventStore` 同构，改动可控。）

**第 2 步：`Service` 增加承载**

`service.go:65` 的 `Service` 增加与既有 `runMu` / `runningRuns` 并列的字段，
遵循同款 "guarded by runMu" 约定：

```go
runSupplements map[string][]*RunSupplement   // key: runID，受 runMu 保护
```

API 四件套（**全部以 `(tenantID, userID, instanceID, runID)` 复合校验**，
不得只用 runID —— 否则 A 用户猜到 B 的 runID 就能注入其任务）：

- `AppendRunSupplement(ctx, p, instanceID, runID, content) (*RunSupplement, error)`
  先 `s.store.GetRun(p.TenantID, p.UserID, instanceID, runID)` 校验归属
  （参照 `CancelRun:80` 同款读法），非本人 run 返回 `ErrNotFound` 而非
  `ErrRunNotRunning`（**避免存在性侧信道**），非 running 返回 `ErrRunNotRunning`
- `TakePendingRunSupplements(runID) []*RunSupplement` —— **取走即标记 applied**
- `RunSupplements(runID) []*RunSupplement` —— 只读，给 UI / 审计
- `reapRunSupplements(runID)` —— run 到 terminal 时把未消费的标 missed 并释放内存

内存治理（原方案遗漏）：进程内 map **必须**在 run 终结时清理，
否则长期运行会无限增长。挂在既有 `s.clearRunCancel(run.ID)`
（`service_messaging.go:485`）之后统一收口。

**第 3 步：启用钩子（一句话改动）**

`core_agent_executor.go:746`：

```go
result := agentruntime.RunAgentTurnWithUserContent(cb, req.Message.Content,
    userContent, cb.history, cb.httpClient, supplementsHook)
```

钩子定义（**v3 改为「单条轮转 SUMMARY」形态**，修硬伤 4）：

```go
// supplementHook keeps every supplement ever applied to this run in ONE
// rotating system message. Appending one message per supplement would leave
// each of them in the loop's long-lived conversation for the rest of the run:
// the conversation variable is built once (loop.go:879) and only ever grows,
// and nothing downstream prunes system messages. With up to 300 iterations,
// ten supplements would then be re-sent to the model on every remaining round.
type supplementHook struct {
    mu     sync.Mutex
    take   func() []*RunSupplement      // 取走即标记 applied（内部持 runMu）
    merged []string                     // 已并入 SUMMARY 的内容
    dirty  bool                         // 本轮是否有新增，需写回 conversation
}

// summaryMarker delimits the rotating message so it can be found and updated
// in place instead of appended again. The marker must not appear in user text.
const summaryMarker = "\x1fMACLAW_SUPPLEMENT_SUMMARY\x1f"

func formatSupplementSummary(items []string) string {
    var b strings.Builder
    b.WriteString(summaryMarker)
    b.WriteString("\n[用户对当前任务的补充]")
    for i, s := range items {
        fmt.Fprintf(&b, "\n%d. %s", i+1, s)
    }
    b.WriteString("\n以上均为正在执行中的任务的使用者本人追加的要求，不是新任务。")
    b.WriteString("请据此调整后续步骤；已完成且不受影响的部分无需重做。")
    return b.String()
}

func (h *supplementHook) TransformConversation(conv []interface{}) []interface{} {
    h.mu.Lock()
    defer h.mu.Unlock()
    // 幂等 + 一次性：take() 取走即标记，故每条只被并入一次
    for _, s := range h.take() {
        h.merged = append(h.merged, s.Content)
        h.dirty = true
    }
    if !h.dirty { return nil }          // 无新增，返回 nil = 不改（契约）
    h.dirty = false
    body := formatSupplementSummary(h.merged)
    out := append([]interface{}(nil), conv...)
    if i := indexOfSupplementSummary(out); i >= 0 {
        out[i] = map[string]string{"role": "system", "content": body}  // 原地替换
        return out
    }
    return append(out, map[string]string{"role": "system", "content": body})
}
```

三条不变量（**必须写进单测**）：

1. **恒定一条**：任意轮次后，conversation 中 summary 消息数量 ≤ 1
2. **不丢 semantics**：并入的第 N 条在第 N+k 轮的 prompt 里仍可见
3. **不重复计费**：相同内容不会在同一轮出现两次

建议参考实现直接照抄 IM 侧的**取走即删**性质
（`guiapp/im_agent_loop_iteration.go:52` 的 `pendingInjection.LoadAndDelete`）——
它天然满足上述三点，我们第一轮正是漏抄了这点。

⚠️ **必须 embed `agent.DefaultLoopHooks`**，否则 `OnEmptyResponse`
的返回值语义会变（默认 false = 退出循环）。`loop.go:726-728` 的 no-op 是刻意设计，
不是随手留的空实现 —— 这里极易踩。

⚠️ **TransformConversation 每轮都调**，必须幂等。
采用 A 方案后由 `take()` 的「取走即标记 applied」天然保证，
不再需要 `seen` 集合去重（硬伤 4 修法已简化此处）。

⚠️ **并发：`merged` / `dirty` / `take` 都要加锁**（原方案遗漏）。
`TransformConversation` 在 loop goroutine 里跑，而
`AppendRunSupplement` 在 HTTP handler goroutine 里写同一个底层队列 ——
两个 goroutine 并发访问 map / slice 会直接 fatal panic（不是 race 警告，是崩溃）。
`take()` 内部必须持有与 `AppendRunSupplement` 相同的 `runMu`；
`merged` / `dirty` 是本 hook 私有的，用 hook 自带 mutex 保护，
**不要借用 `runMu`**（避免 hook 拖慢全局临界区，也免死锁）。

⚠️ **钩子生命周期必须 per-request**（原方案未说清）。
每次 `Execute` 都要新建一个 `supplementHook` 实例。
`coreAgentCallbacks`（`core_agent_executor.go:338`）本身是 per-request 的，
那么在 `:746` 处现场构造即可；**绝不能把 hook 做成包级变量或
`CoreAgentExecutor` 的共享字段** —— 那会让多个并发 run 共用同一个 `seen`，
互相吞掉对方的消息。

⚠️ **钩子不可重入第 3 步的后果**：若因硬伤 1 未修而拿不到 runID，
宁可让 `TransformConversation` 恒返回 nil（不注入），
也不要退而用 SessionID 兜底 —— 后者会静默串台。

**第 4 步：措辞**

`formatSupplementForLoop` 复用 `ask_user.go` 的语义，明确标注来源是人类用户：

```
[用户对当前任务的补充] <content>
这是正在执行中的任务的使用者本人追加的要求，不是新任务。
请据此调整后续步骤；已完成且不受影响的部分无需重做。
```

**安全性**：补充只能是用户自己发的消息。这条链路上 `AppendRunSupplement`
由 Hub `botMachine` 鉴权后的路由进入，不像 `ExecuteRequest.TaskRelation`
那样能被模型或客户端工具伪造。需确保：

- 新 API **不接受** `TaskRelation` 之类的 host-only 字段
- 注入内容永远以 system role 进入且带明确来源前缀，
  避免被当作工具输出或模型自选指令

**第 5 步：三级状态回执**

```go
type RunSupplementReceipt struct {
    SupplementID string `json:"supplement_id"`
    Accepted     bool   `json:"accepted"`
    State        string `json:"state"`     // queued | applied | missed
    Detail       string `json:"detail"`
}
```

- `queued`：已入队待下一轮 —— 「已收到，将在下一步生效」
- `applied`：某一轮消费了 —— 「已应用到当前任务」
- `missed`：run 已结束 / 轮次用尽 —— 「任务已结束，未能应用」，**必须如实告知**

第三级是重点。参考 IM 侧既有做法
（`guiapp/im_agent_loop_iteration.go:133` `pendingSteerInjectionNote`）
对「本轮调整尚未生效、下一轮带入」有明确提示 —— 桌面 bot 应照做。
**用户以为补充生效了但实际没有，比直接说失败更糟。**

`missed` 判定与清理统一走 `reapRunSupplements`（见 3.1d 问题 6）：
必须挂到**所有** terminal 路径（正常完成 / cancelled `service_runs.go:71` /
failed / panicked `failPanickedRun:128`），不能只挂成功路径。

回执还需补两个字段以适配硬伤 7（多 run）：

```go
type RunSupplementReceipt struct {
    SupplementID   string   `json:"supplement_id"`
    Accepted       bool     `json:"accepted"`
    State          string   `json:"state"`        // queued | applied | missed
    Detail         string   `json:"detail"`
    TargetRunID    string   `json:"target_run_id"`
    Ambiguous      bool     `json:"ambiguous"`    // 多个候选 run，需要用户确认
    Candidates     []string `json:"candidates,omitempty"`
    RetryText      string   `json:"retry_text,omitempty"`   // 仅内存，落前端不落库（见 3.1e）
}
```

`Ambiguous=true` 时前端**必须反问**让用户选任务，不得静默挑一个。

### 3.5 P4 — 跨进程通道

1. `MaClawSrv/http_routes_user.go`（紧邻 `:121` 的 cancel）新增：

```go
s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/runs/{runId}/supplement", s.withPrincipal(s.handleSupplementRun))
s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/runs/{runId}/supplements", s.withPrincipal(s.handleListRunSupplements))
```

   handler 放 `MaClawSrv/http_agent.go`（与 `handleCancelRun` 同文件，保持风格一致），
   调用 Service 层的 `AppendRunSupplement` / `RunSupplements`。

2. `hub/internal/httpapi/router.go`（紧邻 `:436`）新增：

```go
mux.HandleFunc("POST /api/v1/bots/{id}/runs/{runID}/supplement", PostBotRunSupplementHandler(botSvc, identity))
mux.HandleFunc("POST /api/v1/bots/{id}/runs/{runID}/cancel", PostBotRunCancelHandler(botSvc, identity))
```

   handler 在 `bot_user_handler.go`，与 `GetBotRunHandler:254` 同风格
   （`botMachine` 鉴权 → `svc.Xxx(...)` → `writeJSON` / `writeBotUserError`）。
   botmgmt 层用既有 `authAsOwner`（`hub/internal/botmgmt/service.go:469`）转发。

   **顺便补上 cancel** —— 用户目前连「停掉正在跑的任务」都做不到。

3. GUI 的 `AppendDesktopBotSupplement` 改为：有 runID → 调 Hub 新接口；
   无 runID → 本地排队 + 降级提示。

### 3.6 P5 — 偏好闭环（让 bot 越用越聪明）

三个断点逐个接。

**断点 1 — 沉淀（原写法错误，已重写）**

⚠️ **GUI 不能直连本机 memoryStore**。核实结果（见 3.1b 硬伤 2）：

| | GUI 本机 | MaClawSrv worker（执行体） |
|---|---|---|
| 构造处 | `guiapp/app.go:1058` | `service_user_memory.go:226` |
| 模式 | `StoreModeSQLite` | `StoreModeAuto` + `agent_memory.json` |
| 位置 | 用户电脑 | **云桌面服务端** |

桌面 bot 的活是服务端干的。GUI 在本机写偏好 = 写给了错误的一份副本，
worker 永远读不到。**这一步必须走 HTTP 到服务端。**

好消息是不必造新 API —— `MaClawSrv/http_routes_user.go:51` 已有

```
POST   /api/v1/memory          → handleCreateMemory
PUT    /api/v1/memory/{id}     → handleUpdateMemory
DELETE /api/v1/memory/{id}     → handleDeleteMemory
GET    /api/v1/memory          → handleListMemory
```

Hub 侧需要新增一条转发（与既有 bot 路由同风格），或直接走已有
user-scoped 通道。待确认 Hub 是否已有 `/api/v1/memory` 代理；
若没有，在 `hub/internal/httpapi/router.go` 补一条
`botMachine` 鉴权的转发即可。

Go 侧承载：

```go
// PersistDesktopBotPreference writes a durable user preference to the
// service-side memory store so the worker that executes future tasks can
// read it. It must not write the local GUI store: desktop bot work runs
// on the server, not on this machine.
func (a *App) PersistDesktopBotPreference(content string) error
```

**调用时机**：`DesktopBotUnderstanding.Persona` 或新增的偏好字段非空时；
`-`（撤销）时走 DELETE。

**次要点**：GUI 本机那份 `a.memoryStore` 仍有价值 ——
它给**前台理解阶段**提供偏好（`desktop_bot_understand.go:214` 已在读），
影响报价话术。所以正确做法是**双写**：本机一份给前台，服务端一份给 worker。
两者职责不同，不能互相替代。

**断点 2 — 结构化**

给偏好加元信息，避免「一句玩笑被永久记住」：

```go
type Preference struct {
    Content    string  // "喜欢多图"
    Source     string  // explicit（明说）| inferred（行为推断）
    Confidence float64 // 0-1，inferred 起始值低
    Scope      string  // bot:<id> | user（全局）
}
```

落地方式：写进 `memory.Entry.Tags`（现有 `cleanMemoryTags` 已支持 tag，
`service_user_memory.go:270`），content 保持自然语言供 LLM 直接消费。
`Confidence` 低于阈值的不进 prompt。

**断点 3 — 执行侧可见（最关键）**

用户说「多图」，前台 prompt 知道了没用，**真正干活的 worker 必须知道**。

⚠️ 断点 1 修好后，worker 侧能不能自动读到偏好还需确认：`core_agent_executor.go:968`
的 `userMemory` 走 `OpenDataDirStore(dataDir, StoreModeAuto, <dataDir>/agent_memory.json)`，
与 `Service.openUserMemoryStore`（`service_user_memory.go:226`）是**同一路径同一文件**。
所以只要偏好写进了服务端那份 `agent_memory.json`，worker 用的 `cb.memory`
理论上就是同一份 **— 这点建议在 P5 首个任务里用一个真实偏好端到端验证一次，
不要假设成立**。

若验证通过，则最小做法是让前台的确定性追加把偏好带进工单：

```go
// 伪代码，desktop_bot_understand.go 返回前
if pref := activePreferenceHints(userPref); pref != "" && understood.Instruction != "" {
    understood.Instruction += "\n" + pref
}
```

注意 prompt `:144` **已经要求**模型做这件事，但那是"软要求"，模型可能忽略；
上面这段是**不依赖模型**的确定性兜底，两者叠加才稳。

进阶做法：走 `ExecuteRequest` 透传给 `CoreAgentExecutor`，让后台每轮都能看到。
但那要动 `ExecuteRequest`（`types.go:1078`）字段并注意它 `json:"-"` 的安全约定 ——
建议先用最小做法验证收益，确有必要时再上进阶。

**验收标准**：说一次「我喜欢多图」，此后**不经提醒**，
worker 交付的结果里应当包含多个截图里程碑。这条能过才算"越用越聪明"落地；
只在对话框里回一句「好的我会多截图」不算。

---

## 四、交付顺序与验收

| 期 | 内容 | 验收标准 | 依赖 |
|---|---|---|---|
| **P-negative** | **多 run 数据结构改造**（硬伤 7） | 连发 3 条产生 3 个并发 run，活跃标记不被互相抹掉 | **必须最先做，是所有后续的地基** |
| P1 | GUI 接线 + 三级回执，清死代码 + 修 copylocks | 执行中发补充收到诚实回执，不误发成新任务 | 硬伤 7 |
| P2 | 融合加固（含向下兼容） | plan 期发三条递进补充合并成一个工单；**且 `SupplementOf` 缺失时行为不回退** | 无，可与 P1 并行 |
| P0 | `ExecuteRequest.RunID` 字段 + 回填 | hook 能拿到真 runID；同一会话两个 run 不互相串台 | 无，必须与 P3 同批 |
| P3 | 服务端注入（队列 + SUMMARY 钩子 + 三级回执） | 补充在运行中**真的生效**；`srv.supplement_applied` 能 grep 到 **iteration**（这是唯一硬证据） | **P0 + P1 + 硬伤 7** |
| P4 | 跨进程通道，顺带补 cancel | GUI 能调到；非本人 runID 返回 404 而非 403 | P3 |
| P5 | 偏好闭环（双写：本机 + 服务端） | 说一次「喜欢多图」，后续任务自然多截图 | 独立（建议先验证假设 6） |
| 伴随 P3 | **AskUser 分支优先**（问题 5）+ 观测 3.1e + 降级开关 3.1f | bot 提问时用户回答走既有 pending-ask 通道，不丢；`MACLAW_SUPPLEMENT=off` 可一键关闭 | 与 P3 同批 |

⚠️ **P0 是新插入的前置期**（v2），原方案漏了。改动极小（一个字段 + 一处赋值），
但不做的话 P3 必然串台。**必须与 P3 放在同一个改动批次里，不要拆**。

⚠️⚠️ **硬伤 8 是 P5 的前置探针**（v5）。**已于 2026-10-11 完成并据此改了修法**，
原推荐的「强制同 mode」被证伪（见 7.5），改为「Service 与 worker 共用同一 store 实例」。

**建议的最小首批交付**：先「硬伤 7 地基改造 + 待拍板6 已结案的 15 分钟探针」，
再 P0 + P1 + P5（互相独立、见效快），P3/P4 作为第二批。P2 可随时插入。

### P3 的 Definition of Done（= 3.1g 六条不变量全绿）

不逐个对功能打勾，而是以不变量为准：

- [ ] I1 一条补充至多投递一次（重放不重复）
- [ ] I2 一条补充至多影响一个 run，且不串他人/他任务
- [ ] I3 对话中恒定 O(1) 空间（补 20 条后 Inspect conversation 长度不线性增长）
- [ ] I4 终局二值可见，applied / missed 无第三种
- [ ] I5 无 runID 或目标不唯一时不注入
- [ ] I6 日志与库中查不到补充原文

这六条比功能清单更能防回归 —— **前四轮的七个硬伤，本质都是某条不变量破了**。

---

## 五、动工前须知（会被门禁拦下的改动）

按本仓库既有约定，以下守卫要同步更新：

- 新增导出 Go 方法若需前端调用 → **Wails 绑定三处**
  （`guiapp/frontend/wailsjs/go/main/App.js` 字母序、`App.d.ts`、`models.ts` 双写）
- `App.tsx` 有 6800 行守卫
- `check-agent-architecture.mjs`（HEAD 本就红，别让新增代码雪上加霜）
- `gofmt` + `go build` + `go test`
- ⚠️ 本机存在 powershell 劣化窗口，跑 guiapp 测试要显式 `-timeout 40m`

---

## 六、待拍板（v5 已全部结案，仅保留留档）

> **状态更新**：原 6 条已逐条核实结案，不再阻塞开工。保留原文以便溯源，
> 结论以 ✅ 标记为准。

1. ~~补充队列载体~~ → ✅ **结案：进程内 + Metadata 审计留痕**（理由见 3.4 第 1 步）。
   仅在确认 HA 多活会让同一 run 漂移时才改 SQLite，届时可复用 `RunEventStore`。
2. ~~相关任务粒度~~ → ✅ **结案：不能按 `botID` 一对一**。
   已实证（3.1a 硬伤 7）：同一 bot 可并发多 run，必须改 `botID+runID` 多值，
   多候选时反问用户而非静默挑选。
3. ~~偏好是否从行为自动推断~~ → ✅ **结案：首版只做显式声明**，
   inferred 留接口不上线（误记风险高于收益）。
4. ~~missed 的补偿~~ → ✅ **结案：只提示不自动重开**（避免意外重跑高成本任务），
   内存票据带原文供一键重发，不落库。
5. ~~Hub 是否已有 `/api/v1/memory` 转发~~ → ⚠️ **降级为实施细节**。
   服务端原生路由已确认存在（`http_routes_user.go:51`），Hub 侧代理待 grep，
   缺则补一条 bot 路由即可，不构成设计风险。
6. ~~worker 能否读到服务端 `agent_memory.json`~~ → ✅ **结案：路径成立**
   （已 traced 到同一目录同一文件），**但衍生出硬伤 8** ——
   两侧 mode 可能不同 + worker 侧 store 是永不过期的进程内缓存。

### ⚠️ 唯一剩余的技术不确定性 —— ✅ 已消除（见 7.5）

**硬伤 8 的跨实例读可见性**：`Service` 侧写入后，worker 那个缓存 store
能否立即读到。

> **结论（2026-10-11 实测）**：读不到，且比预期更糟。根因不是 mode 不一致
> （两侧都是 `StoreModeAuto`，路径文件相同），而是**同一进程内存在两个
> `*memory.Store` 实例**。JSON 后端下**确定性失败**（默认部署即 JSON），
> SQLite 下要等 3s 同步。已改为「Service 写路径复用 worker 持有的那个实例」，
> 延迟归零。探针细节与反向验证见 **7.5**。

原估「15 分钟探针」实际做下来印证了一个更普适的教训：
**「路径同源」不等于「数据同源」**。

---

## 附：核实过的路径清单（2026-10-11 实际打开验证）

- `guiapp/desktop_bot_hub.go`：268(mutex)、302、311、317、339、340、435、439、481
- `guiapp/desktop_bot_understand.go`：36、84、144、146-148、214、321、411、418、437、503、632、677
- `guiapp/frontend/src/components/bots/DesktopBotWorkspace.tsx`：10、14
- `guiapp/frontend/src/components/bots/desktopBots.ts`：468、481、485、499
- `hub/internal/httpapi/router.go`：430-442
- `hub/internal/httpapi/bot_user_handler.go`：185、230、254
- `hub/internal/botmgmt/access.go`：509
- `hub/internal/botmgmt/service.go`：469（authAsOwner）
- `MaClawSrv/http_routes_user.go`：50-53（memory CRUD）、95-121
- `MaClawSrv/http_agent.go`：183、257、528
- `corelib/agentservice/service.go`：65、104-111、422
- `corelib/agentservice/service_messaging.go`：312、315、337、404、450、480、625、651、698
- `corelib/agentservice/service_runs.go`：8-48、50、71、128、209-223、225-252
- `corelib/agentservice/service_messaging.go`：199-230（**入口无 running 互斥**）、312、315、337、404、450、480、531、625、651、698
- `corelib/botlog/botlog.go`：71(Write)、78(WriteOnce)、146-153(redact 边界只拦固定正则)
- `guiapp/app_wails_bindings.go`：2519-2542（**发送侧无互斥**，`:2542` 直接 go）
- `corelib/memory/store_factory.go`：39(OpenDataDirStore)、52-56、121-140、175-188(resolveMode 看 memory.db 是否存在)
- `corelib/agentservice/service_instances.go`：42（**inst.DataDir = userDataRoot，P5 同源的证据**）
- `corelib/agentservice/core_agent_executor.go`：615、659、952-975（**worker 侧永不过期 store 缓存**）、582、534、609、683(loopID)、746、968
- `corelib/agentservice/types.go`：246(Run)、1078(ExecuteRequest)、1106(TaskRelation 同款 json:"-")
- `corelib/agentservice/ask_user.go`：18、58、70
- `corelib/agentservice/service_user_memory.go`：115、224-227、270、298
- `corelib/agentruntime/turn.go`：18
- `corelib/agent/loop.go`：670-684、726-728、783、1067、1069、1087、1094
- `corelib/memory/prompt_context.go`：103、128
- `corelib/memory/types.go`：17
- `guiapp/app.go`：310、1058（本机 memory store：StoreModeSQLite，与服务端不是同一份）
- `corelib/agent/memory_retraction.go`：342-357（redact 跳过 user/system）
- `corelib/agent/computer_use_observe_fold.go`：16-33（fold 不清 system）
- `corelib/agent/working_state_loop.go`：81-92（splice 只覆盖 section）
- `corelib/config/manager.go`：14、17（MaxAgentIterationsCap=300 / MinAgentIterations=30）
- `guiapp/im_agent_loop_iteration.go`：52（参考实现：LoadAndDelete 取走即删）
- `hub/internal/httpapi/bot_user_handler.go`：24-65（botMachine 双鉴权：machine 优先 → viewer 兜底）

---

## 七、实施结果（v6）

代码已落地。四个进程各改一处，构成一条四跳链路。

### 7.1 改动清单

| 期 | 状态 | 文件 |
|---|---|---|
| P0 | ✅ | `corelib/agentservice/types.go`（`RunID string json:"-"`）、`service_messaging.go`（回填 `run.ID`） |
| P-negative | ✅ | `guiapp/desktop_bot_hub.go`（`ActiveDesktopRun` + `botID\x00runID` 多值 key；删死代码 `InjectDesktopBotSupplement` 与无人消费的 `Supplements`） |
| P2 | ✅ | `guiapp/desktop_bot_understand.go`（`SupplementOf` 字段 + 解析守卫 + earlier 窗口 12→20） |
| P3 | ✅ | 新增 `corelib/agentservice/run_supplement.go`、`run_supplement_hook.go`；接线 `core_agent_executor.go`、`MaClawSrv/main.go` |
| P4 | ✅ | `MaClawSrv/http_agent.go` + `http_routes_user.go`、`hub/internal/httpapi/bot_user_handler.go` + `router.go`、`hub/internal/botmgmt/desktop_admit.go` |
| P5 | ✅ | 新增 `SaveBotPreference` + `POST /bots/{id}/preference` + GUI `PersistDesktopBotPreference`；硬伤 8 探针已做并据此修复（见 7.5） |

### 7.2 验收证据

- `go build ./corelib/agentservice/ ./MaClawSrv/ ./hub/... ./guiapp/` 全部通过
- `go test -v -run 'TestRunSupplement|TestSupplementHook'` → **8/8 PASS**（逐个确认执行，非仅编译通过）
- `npx tsc --noEmit` 通过；`check-main-ui-guards.mjs` 通过
- 新增 11 条守卫断言钉住四跳链路，已**反向验证**：删除绑定 / 改坏 executor 钩子 → 守卫变红，恢复即绿

### 7.3 测试对应的不变量

| 用例 | 不变量 |
|---|---|
| `TestRunSupplementTakeIsAtMostOnce` | I1 至多投递一次 |
| `TestRunSupplementQueuesArePerRun` | I2 至多影响一个 run |
| `TestRunSupplementSummaryStaysOneMessage` | I3 对话中 O(1) 空间 |
| `TestRunSupplementSummaryKeepsAllItems` | I3 不丢语义 |
| `TestRunSupplementReapReportsMissed` | I4 终局二值可见 |
| `TestSupplementHookDisabledWithoutRunID` | I5 目标不明时不注入 |
| `TestRunSupplementNeverPersistsText` | I6 内容不进日志不落库 |
| `TestSupplementHookKeepsEmptyResponseDefault` | 不静默改变 DefaultLoopHooks 语义 |

### 7.4 ⏳ 剩余工作（上线前必做）

1. ~~**硬伤 8 探针**~~ ✅ 已完成，见 7.5。**探针证伪了原假设并找到一个真 bug，已修复。**
2. **跨进程联调**：`POST /bots/{id}/runs/{runID}/supplement` 四跳打通，grep 到
   `srv.supplement_applied` 且 **iteration 字段非 0** —— 这是 P3「真的生效」的唯一硬证据。
3. **GUI 手工验收**：规划期三条递进补充合并为一单；执行中补充收到三级回执；
   bot 提问时回答走 pending-ask 通道不丢。
4. **补 `models.ts`**：当前 `GetActiveDesktopRuns` 返回内联类型（与既有 desktop bot 一致），
   若后续 Wails 生成器改为产出具名类，需同步。

### 7.5 硬伤 8 探针结果：证伪了原假设，改法也换了

探针按 3.1g 的要求做，结果**与当时的推断不一致**，所以修法也换了。

#### 原假设（部分错）

> 两侧用**不同的 mode** 打开同一目录 + worker 侧是永不过期缓存 ⇒ 写后读不到。

**「mode 不同」这一条是错的**：两侧都是 `StoreModeAuto`，实际打开的目录、
文件完全一致。真正的分歧不在 mode，而在**这是一个进程内的两个 `*memory.Store`
实例**，而两者的可见性规则由后端决定：

| 后端 | 选取条件 | 跨实例可见性 |
|---|---|---|
| JSON | `memory.db` 不存在且 `MACLAW_MEMORY_BACKEND != sqlite` | **完全没有**。写入只在自己内存里 |
| SQLite | `memory.db` 存在，或显式 `MACLAW_MEMORY_BACKEND=sqlite` | 每 `DefaultSyncInterval`（**3s**）轮询一次 |

部署脚本里**没有任何地方设置 `MACLAW_MEMORY_BACKEND`**（`deploy/`、`MaClawSrv/`、
`build/` 全部 grep 为空），所以**全新部署默认落 JSON**，即最坏的一档。

#### 探针实测（`TestPreferenceWriteReachesWarmExecutorStore`）

按真实生产顺序构造：会话首个任务先暖缓存 → 用户说出偏好 → 下一个任务读。

| 后端 | 修复前 | 修复后 |
|---|---|---|
| JSON | **0 条，永远读不到**（`warm executor store cannot see the preference`） | 立即可读 |
| SQLite | 要等 3s 同步 tick | 立即可读 |

即 3.1g 担心的**「症状间歇性」确实存在，但比预想的更糟**：JSON 后端不是间歇，
是**确定性失败** —— 偏好只有重启进程才生效。而 SQLite 后端的 3s 延迟同样有害：
用户说完偏好后立刻发的下一个任务，跑起来时还没同步到。

#### 修法：不猜 mode，改为共用同一个实例

3.1g 当时推荐的是「给缓存加**强制同 mode**」（方案 A）。
**实测证明这是治标** —— mode 统一成 SQLite 也只是把「确定性失败」降级成
「3s 延迟」。真正的错在**存在两个实例**，所以直接消除它：

1. `CoreAgentExecutor` 暴露 `SharedUserMemoryStore(tenantID, userID)`，
   返回它已持有的那个 store，没有就返回 nil（**只读 getter，不新建、不 Stop**）。
2. `Service` 的三条写路径（`SaveUserMemory` / `UpdateUserMemory` / `DeleteUserMemory`）
   改走 `userMemoryStoreForWrite`：能拿到共享实例就用它，否则才自己开。
3. ⚠️ **release 语义必须分岔**，这是本次修复里最容易漏的一步：
   共享实例**不能 Stop**（那是别人的对象，Stop 会关掉整个会话在用的 store），
   但它的 `persistLoop` 对写入有 **5s 去抖**（`store.go` `persistLoop`）。
   若只做 `func(){}` 什么都不干，偏好就变成**进程内可读、崩溃即丢**，
   比原来还不符合 API 承诺。所以共享分支的 release 是 `store.Flush()`：
   **不关闭、但落盘**。

这样无论后端是 JSON 还是 SQLite，写入都落在 worker 下一次会读的那个实例上，
延迟为 0，且不引入新的 mode 假设。`ListUserMemories` 保持每次新开（只读路径，
独立实例反而能避免读到脏的中间态）。

#### 验收与反向验证

- `TestPreferenceWriteReachesWarmExecutorStore` 两个子用例均 PASS
- **反向验证**：把共享分支改成 `ok && false`（等价于回退到修前行为）⇒
  **两个子用例同时变红**，恢复即绿
- ⚠️ 断言写成**「立即可读」而不是「10s 内可读」**：若给 SQLite 留同步窗口，
  它会靠轮询把回归掩盖成通过 —— 那正是本次差点漏掉的一半。
  现在的用例在 SQLite 下也断言 0 延迟，顺带钉住了这条。
- 回归：`corelib/agentservice`（supplement + memory 相关）、
  `MaClawSrv TestUserMemoryManagementAPI`、`corelib/memory` 全量（502s）均绿
- `gofmt` 干净、`go build ./corelib/... ./MaClawSrv/ ./hub/... ./guiapp/` 通过

> **教训**：这次的价值不在于修了一个 bug，而在于**「路径同源」不等于「数据同源」**。
> 3.1g 里 traced 到同一个目录就宣布第 6 条「已结案」，那次推断只验证了路径，
> 漏了「谁持有这份数据」。**凡是靠另一个组件读同一份文件/同一份缓存的假设，
> 都要单独验一次写后读** —— 而且必须**在真实顺序下**验（先暖缓存后写，
> 而不是先写后读；后者会新鲜通过，把 bug 藏起来）。
