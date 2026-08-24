# Computer Use：按 LongHorizon MEA 改进（第五次审查修订）

> 状态：第五次对照代码审查后修订，**未实施**。不提交。
> Caption / OmniParser 已完成，本方案不改感知栈。
> R1–R53 已吸收为正文约束；本节只保留本轮新缺陷。

---

## 1. 目的

吸收 LongHorizon-Harness 的 **MEA 工程结构**（Manage / Execute / Audit），不吸收其点击栈、npm 插件，也不把 MacLaw 做成 lh-harness 后端。

CU 今天是单模型闭环：observe → act → 自报 `computer_done`。目标是：**有契约才机械拦完成；无契约保持今天行为；长任务走 Horizon GUI 短环（LLM Auditor）。**

## 2. 明确不做

- 不改 Caption / YOLO / a11y sidecar / `pierce.go`
- 不把用户 Goal 当 OCR 验收
- 不让执行模型写验收（禁止 `computer_set_contract`）
- 不以用户手打「验收:」作为产品主入口（WP7 可整项延后）
- P0 不改生产 `computer_done` 语义（尚无真正的验收生产者）
- P0 不改 `im_post_loop.go`、不接 Horizon、不改全局 sticky、不碰 `pendingModelImage`
- WP3 只接 `Next=gui`；不从普通聊天 spawn CU SubAgent
- CLI ToolSurface 继续禁止 `computer_*`（`forbiddenExecutorTools` 已列 `computer_observe` 等）
- 不把操作员 UI 的全局 `cuLastObserveMetrics` 当审计语料

## 3. 现状（与实施相关）

| 点 | 代码 |
|---|---|
| `computer_done` 仍读 `globalComputerUse.session` | `gui/tools_computer_use.go` 约 384–387 行。**P0 必改 `cuSession()`** |
| 工具执行时 owner = SessionKey，否则 UserID | `gui/im_agent_loop_shared.go` `ExecuteToolCall` |
| SessionKey **不是** UserID | `runtimeSessionKey` → `channel:provider:conversationID:actorID`（`gui/runtime_context.go`） |
| epilogue 的 `promptUserID` 是 PolicyOwnerID / UserID | `promptRuntimeUserID`。**与 CU owner 不是同一把钥匙** |
| Sticky 在**第一次成功的** `computer_*` 之后才置位 | `markComputerUseSessionActive`（observe/click/type/…），不是 gate 打开时 |
| System prompt 同一轮会重建 | `UpgradeLightPromptToFull`、`RefreshAfterToolExecution` 都会再走 epilogue |
| `shouldActivateComputerUse` 丢掉 `fresh` | `gui/computer_use_routing.go` 200–202 行 |
| 工具注入已拿到 `cuFresh` | `prepareAgentLoopTools`；语义路由成功时**整段跳过**（`im_agent_loop_start.go`） |
| 工具 gate 文本 ≠ playbook gate 文本 | tools：`computerUseRoutingText`；playbook：`CompactQueryForEmbedding(msg)` |
| Light prompt 不走 GUI epilogue | `buildLightIMSystemPrompt`；CU 工具仍可能被注入 |
| Playbook 在第一轮模型调用前写入 | `appendGUIEpilogue` |
| Observe `window=` 只取矩形，不 FocusWindow | `gui/computer_use_capture.go` |
| 成功动作 `RecordAction(..., invalidate=true)` 会 `refsValid=false` | `corelib/computeruse/session.go` |
| `ComputerUseReset` 已对**全部** session `ResetControl` | `resetComputerUseSessionsLocked`，不是当前 owner |
| Horizon 活跃时 IM 在预检前被截走 | `handleHorizonIMRoute`；`horizonActive(userID)` |
| `AssembleEpisodeContext` 无 GUI 分支 | 未知 role → CLI 提示词 + **空工具面** |
| `DefaultSurfaceForRole("gui_executor")` 返回 nil | `corelib/longhorizon/tools.go` |
| 无 `RoleGUIAuditor` | `corelib/longhorizon/types.go` 只有 `RoleCLIAuditor` |
| Horizon `Acceptance` 是段落 | `assemble.go` / `parse.go` |

## 4. 本轮新缺陷（必须吸收）

| ID | 问题 | 修订 |
|---|---|---|
| **R54** | 方案写「Reset 清该 owner 的 TaskState」。今日 Reset 已经 `ResetControl` **全部** session | P0 Reset = 今日行为 + 清 **全部** CU TaskState。不要做成按 owner 的局部 Reset |
| **R55** | 方案写 epilogue 里 `setComputerUseOwner(userID)`。工具路径 owner 是 SessionKey（`im:desktop:…:actor`），epilogue 的 userID 是 PolicyOwnerID | **抽与 `ExecuteToolCall` 完全相同的 `computerUseOwnerFromLoop(loopCtx, fallback)`。** Begin / TaskState / `cuSession` 只用这把钥匙。禁止用 `promptUserID` |
| **R56** | 方案在每次 epilogue 见 `fresh` 就 Begin。sticky 要等第一次 `computer_*`；此前同一轮 prompt 会重建多次，Begin 会把 FailedDone/契约清掉 | **按 `Runtime.RequestID` 闩一次**（`LoopContext.ComputerUseBegun` 或 TaskState.RequestID）。同 request 重建 prompt 不得再 Begin |
| **R57** | 把 `fresh` 只写在 `prepareAgentLoopTools`。语义路由 handled 时不调用它 | **`fresh` 在始终会跑的 CU 判定处写入 LoopContext**（epilogue 或 loop start）。工具注入只读取，不独占 |
| **R58** | 同一轮两次 gate 输入不同，可能一个开 CU、一个不开 | LoopContext 存本轮 `ComputerUseRoutingText`（与 `im_agent_loop_start` 已算的 `toolRoutingText` 相同）。playbook 与工具注入共用，禁止再用 CompactQuery 单独 gate |
| **R59** | Begin/playbook 只挂 epilogue 时，light 回合没有契约注入，但可能仍有 `computer_*` | **允许。** light 保持无契约、今天的 `computer_done`。P0 不为此改 light prompt |
| **R60** | 「OCR 子串」未定义语料。视觉模式 `TextForModel` 可能几乎没有 OCR；全局 `cuLastObserveMetrics` 会串 tab | 语料 = `LastObserve.OCRExcerpt` + 元素 Name/Value + `TextForModel`。语料空且有契约 → fail closed。禁止读 `cuLastObserveMetrics` |
| **R61** | P0 TaskState 仍留 Fingerprint，没有对应用例 | **删掉。** 只留 Owner、Goal、Acceptance[]、LastAudit、FailedDone、RequestID |
| **R62** | WP3 若直接 `AssembleEpisodeContext("gui_executor")`，今天得到 CLI 提示词和空工具 | WP3 必须：加 `RoleGUIAuditor`；`DefaultSurfaceForRole` 给出 `computer_*` 白名单；assemble 为 GUI executor/auditor 写独立提示词。GUI Budget **不要**照抄 `CLIMaxIterations=80` |
| **R63** | 「`horizonActive` 时 chat 的 `computer_done` 抢终态」不成立：Horizon 已截走该 UserID 的 IM | 抢终态点是 **GUI episode 与普通 CU 共用的 `computer_done` 处理器**。WP3 给 LoopContext 打 `HorizonRole=gui_executor`：此时 `computer_done` 只报 claim，不把 Horizon TaskState 标完成。P0 不打这面旗，行为与今天相同 |

## 5. 已吸收原则（实施时直接当约束）

1. 无 host 验收 → `computer_done` 与今天相同，`LastAudit=skipped`。
2. `completed` 只来自 `ApplyAudit`（或 WP3 LLM Auditor pass）；`summary` 是声称。
3. 操作员 Reset 清全部 CU TaskState，**禁止**顺带清 Horizon TaskState。
4. `always` 已验证完成 + 空验收 = 仍 `skipped`，不回落到 Goal。
5. 动作后必须再 observe（已有 `refsValid=false`）；Auditor **禁止** `FocusWindow`。
6. 跨轮只保留最后一次 observe 全文 + 最后一张视觉图。
7. CU Surface = `desktop | browser | unset`，无 office。`@computer` 强制 desktop。
8. Sticky P0 保持进程全局；隔离范围是 LastObserve / TaskState。
9. Caption 正交。Office 跟进不释放 sticky：沿用现状。
10. 真正的验收生产者是 Horizon GUI Manager，不是用户 DSL。
11. 泛化铬文 denylist（保存/确定/取消/打开/关闭/OK/Save/Cancel），不用最短长度 4。
12. `fresh` 只活在本轮 LoopContext，不写全局 Session。

## 6. 与 Horizon / CLI 共存

```
CLI 长任务     → Horizon Next=cli      （已落地）
桌面短操作     → CU 单环 + 可选机械闸   （P0；无契约则闸不生效）
桌面长任务     → Horizon Next=gui       （WP3；LLM Auditor）
浏览器短操作   → Browser 单环
浏览器长任务   → Horizon Next=browser
```

普通聊天 **不** spawn CU 编码式 SubAgent，**不**走 `RunTaskWithSubAgent`。

Horizon 活跃时该 UserID 不再进普通 IM loop。WP3 的 GUI episode 自己跑 `computer_*`，用 `HorizonRole` 与普通 CU 共用处理器而不共用终态。

## 7. 数据

### 7.1 本轮（LoopContext）

```
ComputerUseFresh        bool
ComputerUseBegun        bool    // 本 RequestID 已 Begin
ComputerUseRoutingText  string  // 与工具注入同一份 gate 输入
HorizonRole             string  // P0 空；WP3 才写 gui_executor / gui_auditor
```

### 7.2 CU TaskState（按 ExecuteToolCall 的 owner）

```
Owner        string    // SessionKey，否则 UserID
RequestID    string    // 闩 Begin
Goal         string    // 只给人看，不当验收
Acceptance   []string  // 短 host bullet；空 = 无契约
LastAudit    skipped | passed | failed
FailedDone   int
```

不要 Requirements / Artifacts / Facts / Evidence / Fingerprint。

### 7.3 `computer_done`

| 条件 | 行为 |
|---|---|
| `HorizonRole=gui_executor`（仅 WP3） | 返回 claim，不清 Horizon 完成态 |
| Acceptance 空 | 今天的完成路径（清 sticky、`LastAudit=skipped`） |
| Acceptance 非空且未通过 | 拒绝；`RecordAction("done", …, false, …, false)`；sticky 保留 |
| Acceptance 非空且通过 | 完成，清 sticky，`LastAudit=passed` |

P0 没有 Acceptance 生产者、也不打 HorizonRole → 永远走「空契约」行。

## 8. 工作包

### WP0 — 契约与闸（P0）

1. `computer_done` → `cuSession()`。
2. 抽出 `computerUseOwnerFromLoop`，与 `ExecuteToolCall` 共用。
3. loop start 把 `toolRoutingText` 写入 `LoopContext.ComputerUseRoutingText`；gate 只读这份。
4. `fresh` 写入 `LoopContext`（语义路由跳过 tools 时仍要写）。
5. owner 已确定后：若 `fresh && !ComputerUseBegun` 则 Begin；置 `ComputerUseBegun`。
6. playbook 读该 owner 的 TaskState；Acceptance 非空才追加「未满足禁止 computer_done」。
7. `ApplyAudit` 仅当 Acceptance 非空；语料见 R60；失败 `RecordAction(false)`。
8. `ComputerUseReset`：今日 `ResetControl` 全部 session + 清全部 CU TaskState。
9. 不改 `im_post_loop.go`、light prompt、Horizon。

**P0 生产行为与今天相同。** 不要宣传「已阻止虚假完成」。

### WP1 — 机械审计细节（P0 测试面）

- 只匹配短 bullet，不匹配 Goal。
- 泛化铬文 denylist，不用 min-length=4。
- 有契约且 `!RefsValid()` → fail（必须先 observe）。
- Auditor 只读 `cuSession().LastObserve()`，不 FocusWindow。

### WP2a — LastObserve 按 owner

`cuSession()` 已按 owner 分 Session。测两个 SessionKey 交错 observe 不串图。Sticky 仍全局。不拆 `cuLastObserveMetrics`。

### WP2b — 跨轮 observe 折叠

接到现有 `trimConversation` / `CheckpointConversation`，不要只压当前 loop。历史 `computer_observe` 只留最后一条全文；更早的压成指纹。最后一张视觉图保留。`StructuredPreview` 可加 `computer_observe` 分支作第一刀，但不能代替跨轮折叠。

### WP3 — Horizon `Next=gui`

1. 新增 `RoleGUIAuditor`；`AssembleEpisodeContext` 增加 GUI executor / auditor 分支（禁止落入 CLI 默认）。
2. `DefaultSurfaceForRole(RoleGUIExecutor)` = `computer_*` 白名单（observe/find/click/type/key/scroll/focus/select/drag/done/playbook）。Auditor 无动作工具；**禁止** `computer_focus`。
3. GUI Auditor = LLM + 最新 observe 文本（可加只读 probe）。Manager 段落不当机械子串前提。短 `Acceptance[]` 只可加速 pass，不可单独判 fail。
4. GUI episode `LoopContext.HorizonRole=gui_executor`；`computer_done` 只报 claim。
5. GUI Budget 单独设，默认远小于 CLI 80。
6. Uncertain 中止 / 与操作员 Reset 的关系在本 WP 定义；**不要改 P0 Reset**。
7. 不引入 chat spawn CU SubAgent。

### WP4 / WP5

- 不新增 `computer_set_contract`。有契约时 `computer_done` 失败可 retry。
- Surface：`desktop | browser | unset`。
- 停等沿用 `ask_user` / `NeedsConfirm`。不把 CU 塞进 `PendingAskUser`。

### WP6 — 测试夹具

| 夹具 | 期望 |
|---|---|
| 无 Acceptance | `computer_done` 成功，`LastAudit=skipped` |
| 注入 Acceptance，语料不含 | 拒绝；sticky 在；`RecordAction` ok=false |
| 注入 Acceptance，语料含 | 通过 |
| owner = SessionKey 而非 UserID | TaskState / LastObserve 打在 SessionKey 上 |
| 两个 SessionKey 交错 observe | 不串图 |
| 同 RequestID 两次 epilogue | 只 Begin 一次 |
| sticky 续上（fresh=false） | 不 Begin |
| fresh=true 且有契约 | **第一轮** playbook 已含契约 |
| Reset | **全部** CU TaskState 空；Horizon 夹具不被清 |
| 泛化「确定」单独一条 | 丢弃 |
| 「已保存」 | 可保留 |
| 语义路由 handled | 不依赖 `prepareAgentLoopTools` 也能记下 fresh（或明确跳过 CU Begin） |
| 工具与 playbook 的 gate 文本 | 同一份 routing text，结论一致 |
| WP2b | 两条历史 observe，压缩后一条全文；重载后仍折叠 |
| WP3 | `AssembleEpisodeContext(gui_executor)` 不是 CLI 提示词；Auditor 段落型 Acceptance 走 LLM |

### WP7 — 可选「验收:」糖（可整项延后）

只认独立行 `验收:` / `验收：`，从**未 CompactQuery** 的原文解析，写入本轮 playbook。denylist 见原则 11。不是 P1 主路径。

## 9. 实施入口（只做这些才算 P0 开工）

1. `computer_done` → `cuSession()`
2. `computerUseOwnerFromLoop` 与 ExecuteToolCall 共用
3. LoopContext：routing text、fresh、Begun；按 RequestID Begin
4. 瘦 TaskState + `ApplyAudit`（空契约 = 今天）
5. Reset 清全部 CU TaskState
6. WP6 中 P0 行测试

**不要**在 P0 改：`im_post_loop.go`、Horizon、`HorizonRole`、全局 sticky、`pendingModelImage`、Caption、light prompt、WP7。

## 10. 成功标准

P0：无契约 CU 与今天一致；`computer_done` 不再读全局 session；测试夹具能拦住「有契约却假完成」；owner 与 ExecuteToolCall 一致。

WP3：桌面长任务由 Horizon GUI Auditor 收口；GUI episode 的 `computer_done` 不能单独结束 Horizon 任务。