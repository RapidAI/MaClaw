# J-Space 思想吸收：与 MacLaw 原生整合

**灵感来源**：[J-Space Cognition Suite](https://github.com/Tiger3807861189/J-Space-Cognition-Suite-V3.6) 的问题定义与控制思想，不是它的仓库、Skill、模块或运行时名词。

**日期**：2026-08-17（第七次修订合同；同日 P0–P5 落地）

**状态**：已落地。`RunLoop` 回合内工作前提与控制合同；默认按 `ShouldAttachWorkingState` 挂段。不是可选插件，也不是第二套 goal 存储。回滚：`MACLAW_WORKING_STATE=off`。

---

## 0. 这次审查改了什么

第六版解决了「谁写账本」，但对着 `loop.go` 再过一遍，还有这些会在实现时卡住的洞：

1. **`SelectAction` 表里全是「或」**，纯函数无法单测，也不是控制。
2. **任何带 `path` 键的工具都能上台**（`list_directory` / `send_file` / 语义 grant 都有 path），Live 会被刷爆；语义工具名也不是稳定身份。
3. **状态创建和挂段混在一个布尔里**。AskUser 可能发生在第一次 splice 之前；压缩也不通过 `TransformConversation` 的返回值暴露「本轮 compacted」。
4. **steer 没有结构化文本**。loop 只看见 `LLMReplanRequested()`，把 Goal 改成「最新意图」又是暗 NLP。
5. **一批多个工具时，每工具跑一次 SelectAction 会把 Next 打飞**；steer 跳过 / 策略拒绝若也计入签名，会误伤。
6. **只靠 path 才能 Settled / 关 Open**。纯 `bash` 任务第一次失败就会留下 Open，done-check 把收束卡死。
7. **done-check 没有循环内出口**。「就这样」是下一轮用户话；同一次 `RunLoop` 里模型连说两次完成会空转。

本版把这些写成确定函数。核心思想不变：台上少、前提只认一处、先上台再证实、监控必须改 Next、无新约束就逃逸。

---

## 一、对照原仓库

| 原仓库 | MacLaw 合同 | 判定 |
|---|---|---|
| 工作集过载 | Live 最多 2；无 Fact 不上台；满员走 `AdmitLiveEvictOldest`（点名换下最旧 Label） | 吸收 |
| 表征漂移 | 回合内状态；压缩 / 工具 / 暂停续跑后由 loop 再 splice | 吸收 |
| 无控制重试 | 失败才加 SigCount；第 3 次同签 → `seek_user` 注入，不 HardExit | 吸收 |
| 过早完成 | 未关闭 Open 则拦截一次；同 loop 第二次完成放行；用户话在放行表则放行 | 吸收 |
| 1 选择性工作台 | 仅白名单文件工具 + 规范化 path；上台后函数改 Next 点名 Label | 吸收 |
| 2 广播枢纽 | 同一 Label 只一条，改 Fact 即更新该槽；协议：台上只认这一处。不扫文件是否重推 | 吸收（可执行部分） |
| 3 稠密轨 | 不采用内部符号；不进气泡 / HistoryDelta | **有意不吸收** |
| 4 结论前桥接 | `AdmitSettled` 必须已有对应 Live。关 Open 靠 `CloseOpenOnTrust`（工具名），不靠完成句 NLP | 吸收（可执行部分） |
| 5 元认知控制 | 确定的 `SelectAction`；`trust` 拒写 Open | 吸收 |
| 6 经验逃逸 | 空回复第 1 次 `empiric`，第 2 次 `seek_user`；empiric 后再同签失败 → `seek_user` | 吸收 |
| 7 功能性回响 | 启用后 Next 必有；关 Open 写 ClosedBy。不用 I/we | 吸收 |
| 只使用挣来的机制 | light / off 不挂；无 Carrier、无已执行工具、无**本回合活跃** horizon 则不挂 | 吸收 |
| `fast/full/loop` | 不引进产品词 | 改写 |

不借鉴：上游代码、SKILL.md、九模块、觉醒叙事、`.jspace/`、口令「j-space」。

---

## 二、原则

1. 不新建 `corelib/taskledger`、`CognitionPass`、带 Store 的 Host。
2. 状态寿命 = 一次用户任务回合（可跨该回合内多次 `RunLoop`）。
3. 注入只由 `RunLoop` 改 `conversation[0]` 尾部。
4. **唯一写入者是 loop**。不解析 assistant 散文，不新增更新账本工具。
5. LongHorizon / `goal.Store` 只投影 Goal，不回写；只认**本回合活跃**的投影。
6. `Next` / `ControlAction` 不授权、不重放工具。
7. light 零增量。总闸 `MACLAW_WORKING_STATE=off`。P5 按 `ShouldAttachWorkingState` 挂段，不是所有 full 都挂。

---

## 三、两段寿命：先有状态，再决定挂不挂

| 函数 | 何时为真 | 作用 |
|---|---|---|
| `EnsureWorkingState` | Carrier 非空；或本 loop **已执行**工具（`toolExecuted==true`）；或本回合有活跃 horizon/goal 投影 | 创建或复用 `*WorkingState` |
| `ShouldAttachWorkingState` | 非 off、非 light，且 `state != nil` | 本轮 LLM 前 splice |

因此：第一次工具返回后，状态已在，**下一轮循环头**才出现 `[任务状态]`。AskUser 若发生在第一次 splice 前，`finish()` 仍带上指针，续跑时 Carrier 非空，开场即可挂段。

`TransformConversation` 不报告是否压缩。**挂段条件不再使用 compacted**。P1 门禁改为：已执行工具之后若发生压缩，下一轮头 Goal 仍在。

---

## 四、写入权

| 层 | 谁写 | 写什么 |
|---|---|---|
| 机械层 | `RunLoop` 纯函数 | Goal、Live、LastAction、Next 模板、Open、SigCount、Settled、FinishNudges |
| 可见层 | `ApplyWorkingStateSection` | system 尾部。不进 HistoryDelta、气泡、user 注入 |

### 4.1 Goal

- 无 Carrier：`userText` 按 `。.!?\n` 取首段，最多 80 rune；切完为空则用「当前任务」。
- 有 Carrier：不把新答当成新 Goal。
- **steer / replan：不改 Goal**（没有结构化 steer 文本）。只清空 Live 与未关闭 Open；Settled 保留。
- 活跃 horizon / goal：仅当 host 标明**本回合活跃**时投影；Carrier 已有 Goal 则不覆盖。不得因「商店里有旧目标」而挂段。

### 4.2 Live

`ExtractFocus(name, argsJSON, outcome) (FocusItem, bool)`

白名单（稳定内建名，不是语义 grant）：`read_file`、`write_file`、`edit_file`、`edit_lines`。只读 JSON 键 `path`。`list_directory` / `send_file` / `read_tool_result` / 不透明 grant **不上台**。

- Label = `filepath.ToSlash(Clean(path))`，超过 40 rune 则保留尾段。同一规范化 path = 同一 Label。
- Fact = `path=...；结果=ok|timeout|error`，失败可带 outcome 短约束（截断），不抄整段输出。
- `AdmitLive`：缺 Label/Fact 拒绝；同 Label 已在则更新 Fact；已有 2 条且 Label 为新 → 拒绝，调用方走 `AdmitLiveEvictOldest`（点名换下 `Live[0].Label`）。这是唯一自动换下策略，禁止调用方静默 FIFO。
- 上台后**函数改 Next**，点名新 Label。

### 4.3 Settled、Open、桥接

`AdmitSettled`：`settled.Label` 必须已在 Live；Verifier、Coverage 必填（工具名+outcome / 本回合该 path）。只在 `trust` 且该焦点仍在 Live 时由 loop 调用。

`CloseOpenOnTrust(state, toolName, settledID)`：关闭 `OpenItem.Tool == toolName` 且未关闭的项；`ClosedBy` = settledID，无 Settled 时填 `"trust:"+toolName`。

这样纯 bash 任务也能在随后一次成功时关掉 Open，不会被 done-check 卡死。`PremiseBeforeUse` 仍只服务 `AdmitSettled`，不对完成句抽词。

Open 最多 2 条未关闭。将满时再失败 → `SelectAction` 出 `seek_user`，不加第三条。

`trust` 不得新增 Open。

### 4.4 不更新状态的调用

下列**不**跑 ExtractFocus / 不加 SigCount / 不跑 SelectAction：

- `syntheticFailure`（steer 使批次作废）
- 策略拒绝（含 light deny，升级成功前）
- `ask_user` / `record_audio` 暂停（可把 `LastAction` 标成 `seek_user`，但不当工具失败）

已执行工具计数只含 `toolExecuted==true`。

---

## 五、数据

放在 `corelib/agent`，与 `situation_report.go` 并列。

```go
type ControlAction string

const (
    ActionTrust         ControlAction = "trust"
    ActionRetryDiagnose ControlAction = "retry_diagnose"
    ActionReroute       ControlAction = "reroute"
    ActionEmpiric       ControlAction = "empiric"
    ActionSeekUser      ControlAction = "seek_user"
)

type RoundSignal struct {
    Kind         string // tool_ok | tool_timeout | tool_error | llm_empty
    ToolName     string
    SameSigCount int // 本次失败入账后的计数；成功为 0
    EmptyCount   int // 连续 llm_empty
    Prev         ControlAction
    OpenCount    int // 当前未关闭 Open 数
}

type FocusItem struct {
    Label string
    Fact  string
}

type Settled struct {
    ID       string
    Label    string
    Claim    string
    Verifier string
    Coverage string
}

type OpenItem struct {
    Tool     string
    Question string
    SettleBy string
    ClosedBy string
}

type WorkingState struct {
    Goal         string
    Live         []FocusItem
    Settled      []Settled
    Open         []OpenItem
    Next         string
    LastAction   ControlAction
    LastSig      string
    SigCount     int
    FinishNudges int
    Updated      time.Time
}

// 可选 host 接口，形如 PromptProfileProvider。
type WorkingStateHolder interface {
    LoadWorkingState() *WorkingState
    SaveWorkingState(*WorkingState) // nil = 清除
}

type WorkingStateGoalSource interface {
    ActiveWorkingStateGoal() string // 仅本回合活跃投影；空则不因此 Ensure
}
```

`LoopResult.WorkingState *WorkingState`。`finish()` 补上当前指针。

渲染硬顶 400 rune。裁剪：**先丢旧 Settled，再缩短 Live.Fact，永不丢 Goal / Next / LastAction**。切段：最后一次**行首** `[任务状态]` 到文末。稳定段合同**不得**自带该标记。

`ApplyWorkingStateSection`：`[0]` 为 system 的两种 map，否则 no-op；不该挂或 state 为空则删旧段。

`replaceConversationSystemPrompt` 会抹尾段。不在替换函数里二次拼接。下一轮循环头再 splice。

user 注入（空回复恢复、同签禁止、done-check 提示）**只带 Next 一句**，不复制整段 `[任务状态]`。

---

## 六、核心合同

### 6.1 挂段

见第三节。P1 典型：首次已执行工具之后的下一轮 LLM 才挂段。

### 6.2 一批工具

按顺序：对白名单工具 `ExtractFocus` → `AdmitLive` / `AdmitLiveEvictOldest`；失败则更新 `LastSig` / `SigCount`，成功则 SigCount=0。

`SelectAction` **每批只跑一次**。信号 = 最后一个失败；若无失败则最后一个成功。同签禁止提示与现有同工具失败提示一样，**整批 tool 结果追加完再注入**。

### 6.3 `SelectAction`（确定，无「或」）

按从上到下第一条命中：

| 条件 | 动作 | loop 同时写 |
|---|---|---|
| `tool_ok` | `trust` | `CloseOpenOnTrust`；有 Live 则 `AdmitSettled`；Next=按 Live 继续；不新增 Open |
| `Prev==empiric` 且本次失败且同签 | `seek_user` | Open.Tool=该工具 |
| `tool_error\|timeout` 且 `OpenCount>=2` | `seek_user` | 不加第三条 Open |
| 同签失败第 1 次 | `retry_diagnose` | Open：Tool + 原因=outcome 种类；Next 点名将改范围 |
| 同签失败第 2 次 | `reroute` | Next=换参或换工具 |
| 同签失败第 ≥3 次 | `seek_user` | 注入「禁止再同签名」，不 HardExit |
| `llm_empty` 且 EmptyCount==1 | `empiric` | Next=列出≤3 候选并写核验步骤 |
| `llm_empty` 且 EmptyCount≥2 | `seek_user` | 禁止 trust |

同签名：`name + canonical JSON(args)`；JSON 失败则 SHA-256(原始 args)。**仅失败递增**；`tool_ok` 清零。成功重读同一文件不是一次打击。

这是动作层。exact drift 与同工具 8/12 HardExit 不动。

`ActionSeekUser` 不代调 `ask_user`，不授权工具。

扩展 `buildEmptyResponseRecovery`：走 `SelectAction(llm_empty)`，把 Next 写进恢复提示。

### 6.4 收束 `ShouldBlockFinish(state, userText, attached) bool`

- `!attached` 或 `state==nil` 或 HardExit / max-iter / cancelled → false（与今日一致）
- `userText` 去掉空白后属于放行表（`就这样` / `就这样吧` / `不用了`）→ false
- 没有「ClosedBy 为空」的 Open → false
- `FinishNudges>=1` → false（同任务回合只拦一次）
- 否则 true：注入一句「还有未关闭问题，先核验或问用户」，`FinishNudges++`，继续 loop

不解析完成句。

---

## 七、接线

```text
RunLoop 开场
    Holder.Load（若实现）
    活跃 horizon 投影（仅当 Load 为空）
    每轮：Transform → Fold → ApplyWorkingStateSection → observe → MoA 克隆 → LLM
    工具批：跳过项不入账；已执行项更新 Live/签名；批末 SelectAction
    light→full / RefreshAfterToolExecution：只换 system 正文
    无工具文本：ShouldBlockFinish？注入并 continue : finish
    AskUser / RecordAudio：finish；Holder.Save(state)
    正常结束 / HardExit / max-iter / cancel：Holder.Save(nil)
续跑：Load 同一状态，不改 Goal
```

子 Agent：自己的 `RunLoop`，通常无 Holder，不写回父会话。

P2 只把指针挂在 `pendingAskUserState` / `pendingRecordAudioState`。不挂 `pendingUserReplyState`，不进 unfinished slot，不进 `persistedSession`，不改 `InFlightCheckpoint` 的 Sequence / LastToolName / SideEffectState。

无 `PromptProfileProvider` 时按 full，但仍受 `ShouldAttachWorkingState` 约束。

---

## 八、权威边界

| 对象 | 它管 | WorkingState |
|---|---|---|
| 本回合多次 `RunLoop` | 前提与动作 | 唯一写入 owner |
| LongHorizon / `goal.Store` | 跨会话目标 | 仅活跃投影，不回写 |
| `UnfinishedTaskSlot` | 会话如何续上 | 不抄 Goal，P2 不塞状态 |
| `[当前情境]` | SSH / 后台 | 不重复条款，预算独立 |
| operation ledger | 副作用与授权 | 只读结果类 |
| exact drift / 同工具 8/12 | HardExit | 工作状态只改动作 |

---

## 九、阶段

总闸：`MACLAW_WORKING_STATE=off`。**P0–P5 已于 2026-08-17 落地。**

### P0 — 类型、渲染、纯函数（已完成）

`working_state.go`、`working_state_section.go`、`working_state_rules.go`。不改 `RunLoop`。

单测：空渲染；400 rune 保 Goal/Next/LastAction；两种 system map；idempotent splice；行首标记；ExtractFocus 白名单命中 / `list_directory` 与无 path 失败；Label 规范化后同一 path 合并；AdmitLive 满员拒绝；AdmitLiveEvictOldest 点名最旧；SelectAction 全表（含 empiric 后再同签、OpenCount≥2、empty 第 2 次）；trust 拒写 Open；CloseOpenOnTrust 无 Settled 也能关；ShouldAttach：light / 无 state 为 false；ShouldBlockFinish：无 Open 放行、有 Open 拦一次、放行表放行、nudged 后放行。

门禁：`go test ./corelib/agent -run WorkingState`

### P1 — 长进 RunLoop（已完成）

- Ensure + Attach；P1 只投影 Goal / Next / LastAction，不上 Live/Open/Settled
- 循环头：Fold 之后、observe / MoA 之前 splice
- `finish()` 带指针；不改 host `BuildSystemPrompt`；不在 `replaceConversationSystemPrompt` 里 splice
- P1 的 Next =「根据工具 {name} 的 {outcome} 继续完成 Goal」

门禁：已执行工具后压缩，下一轮头 Goal 仍在；HistoryDelta 与 user 注入无 `[任务状态]`；AskUser 在已 Ensure 时指针非空，off 时为 nil；`go test ./corelib/agent`；adaptive 脚本绿。

### P2 — 暂停（已完成）

- `WorkingStateHolder` 读写两个 pending 结构
- 正常结束与 HardExit：`Save(nil)`
- 活跃 horizon 开场投影
- 散文追问不带状态

门禁：答完 AskUser 后 Goal/Next 还在；新任务无旧 Goal。

### P3 — 上台与广播（已完成）

- ExtractFocus / AdmitLive / AdmitLiveEvictOldest
- full 稳定段：「台上只认一处」（约 120 字，无标记）
- 已执行工具后才渲染 Live

门禁：light 增量 0；非白名单不上台；满员换下最旧；Next 由函数点名 Label。

### P4 — 动作、逃逸、收束（已完成）

- SelectAction + 同签名；批末注入
- 空回复走 empty 信号
- AdmitSettled + CloseOpenOnTrust
- ShouldBlockFinish；不改 8/12 与 drift

门禁：纯问答不核对；三次同签有注入、无新 HardExit；bash 失败后再成功能结束；ledger 回归不破。

### P5 — 默认按 ShouldAttach 挂段（已完成）

清单：P0–P4 绿；agent/doctor 绿；adaptive 绿；气泡无标记；light 增量 0；off 时金测试与改前一致。

---

## 十、不做

1. vendoring / 自动加载 `j-space/`
2. Skill 或 index 出现 j-space
3. 运行时名称使用 Ledger / CognitionPass / j-space / Dense Track
4. 觉醒、I/we 人格
5. 并行 `.jspace/` 或第二份磁盘账本
6. 从工作状态恢复 grant / receipt / 重放
7. 九模块改写成九个包
8. 第三套跨会话 goal
9. 写入 `InFlightCheckpoint` 现有三字段
10. NLP 抽前提 / 候选 / 诊断 / steer 意图
11. `update_working_state` 工具
12. 替代 exact drift 或 8/12 HardExit
13. P2 写入 `pendingUserReplyState` 或 unfinished slot
14. 用「任意 JSON path 键」或语义 grant 名上台
15. 把 compacted 当成 `TransformConversation` 的隐式输出

---

## 十一、风险

| 风险 | 缓解 |
|---|---|
| 平行框架 | 只进 `corelib/agent`；loop 独写 |
| 合同太重 | 未 Ensure 则零成本；P1 不上台 |
| 暗 NLP | 不解析散文；steer 不改 Goal |
| Live 被刷爆 | 工具名白名单 + 规范化 Label |
| bash 卡死收束 | CloseOpenOnTrust 不依赖 path |
| AskUser 丢状态 | Result + Holder + 两段寿命 |
| 跨任务污染 | 非暂停 Save(nil)；散文追问不带 |
| 与 drift 双杀 | 只注入，不抢 HardExit |
| 400 rune | 裁剪保 Goal/Next/Action |
| mid-loop 丢尾段 | 只在循环头 splice |

回滚：`MACLAW_WORKING_STATE=off`。

---

## 十二、落地记录

合同已合入，不再从 P0 起步。实现落在现有 `corelib/agent` 与 GUI 共享 loop，没有 `j-space/`、Skill 或第二套磁盘账本。

### 代码

| 路径 | 职责 |
|---|---|
| `corelib/agent/working_state.go` | 类型、Ensure / ShouldAttach、环境闸 |
| `corelib/agent/working_state_section.go` | 渲染、splice、可见层去标记 |
| `corelib/agent/working_state_rules.go` | Live / Settled / Open / SelectAction / done-check |
| `corelib/agent/working_state_loop.go` | 投影、批末动作、空回复、Holder 存取 |
| `corelib/agent/loop.go` | 循环头 splice、批注、finish、done-check |
| `corelib/agent/prompt_blocks.go` | full 稳定段合同（无 `[任务状态]` 标记） |
| `gui/im_agent_loop_shared.go` | `WorkingStateHolder` + `WorkingStateGoalSource`；气泡再剥一层 |
| `gui/im_pending_reply.go` | AskUser / RecordAudio 只挂指针 |
| `corelib/doctor/working_state.go` | doctor 检查 `agent.working_state` |

GUI 投影：有 `HorizonRole` 时取本回合 horizon `UserGoal`；`platform=goal-continuation` 且 `ShouldContinue()` 时取 `goal.Store` 目标。普通聊天不因商店里的旧目标而挂段。TUI 和其他 RunLoop 宿主不实现 GoalSource，避免旧目标误挂。子 Agent 不回写会话。

### 已跑门禁

- `go test ./corelib/agent`
- `go test ./corelib/doctor`
- `scripts/test-adaptive-shared-loop.ps1`

light 不 splice（合同只进 full）。`MACLAW_WORKING_STATE=off` 时不创建、不挂段。HistoryDelta / `LoopResult.Text` / GUI 气泡会去掉行首 `[任务状态]` 块。

### 验收备注

- 真机聊天需人工确认气泡无 `[任务状态]`。
- 第九节里这几条机制已接线，RunLoop 级单测薄于原文：压缩后下一轮头 Goal 仍在、答完 AskUser 后续跑 Goal/Next、三次同签注入、bash 失败再成功能结束。
