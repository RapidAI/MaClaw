# Browser Use 控制环改进（借鉴 LongHorizon-Harness）

> 状态：已实施 WP12–WP15（第六次审查方案）
> 日期：2026-08-16
> 修订：ask 必须穿过 executeStepWithRetry 且不可重试；task_run Handler 必须输出 `__ASK_USER__`；变异动作先拦后点；playbook 仍在教模型对 login/MFA 停；WP11 的 expect 只约束合并 click，不塞进 task_run StepSpec
> 前置：`docs/design/browser-use-improvement-plan-zh.md` WP0–WP10
> 产品外环：`docs/design/longhorizon-harness-plan.md`（P0/P1 已实施，P2 未做）。本切片消费 P2，不实现 Supervisor
> 参考：[AMAP-ML/LongHorizon-Harness](https://github.com/AMAP-ML/LongHorizon-Harness) `v0.1.5` · [arXiv:2608.01964](https://arxiv.org/abs/2608.01964)

## 0. 审查记录

B1–B50 已吸收：不强制全部 activating；login_wall/OTP 不自动停；弱合同不当 ok；AskUser 与阻塞 resumeC 互斥；doAgentStep 必须回传 ActionResult；Press 无 ref 按页级停；Paused 要回收；CaptchaWidget 从文案验证码拆开；blocked > ask > expect_failed；本地夹具不是真站 recaptcha。

第六次对照代码（这些会让第五版 WP12 **看起来做了、task_run 仍然不停**）：

| 编号 | 问题 | 修订 |
| --- | --- | --- |
| B51 | `executeOneStep` 的 channel 只传 `error`。Click 若 `(result{ask}, nil)`，`executeStepWithRetry` 当成功，继续下一步。若改成 `error`，`RetryStrategy` 默认再试 3 次，会连点验证码 | 贯穿 `stepOutcome{result, err}`。`ask`/`blocked` **禁止**走 ClassifyFailure。不是失败重试，也不是成功续跑 |
| B52 | `task_run` Handler 成功路径只 `json.Marshal({status,task_id,step,total})`，失败路径也是 JSON。`ParseAskUserResult` 只认整串以 `__ASK_USER__` 开头 | Handler 在 Paused+ask 时必须返回标记串，禁止外包 `{"ok":false}` 或 `{"status":"paused"}` |
| B53 | Click/Type/Press 先 `clickWithCandidates` 再 observe。现在拦 captcha 会变成「已经点了再问人」 | 变异动作 **先** 看 CaptchaWidget：last snapshot 的 flags 仍有效则用；否则 flags-only peek（不截图、不把 SoM 丢给 LLM）。未过门不得 mutate |
| B54 | `playbook.go` 仍写「captcha / login wall / MFA 停并问人」和「Optional expect=」。合并工具 description 同样 | WP12 同期改 playbook：只对 CaptchaWidget 问人；login_wall/OTP 不问。expect 句子留给 WP11 |
| B55 | 停机列表漏了 `dialog`。`HandleDialog` 会 accept 原生 alert | 页级停：click/type/press/select/set_files/**dialog**。允许 navigate/observe/extract/hover/wait/scroll（可以离开验证码页） |
| B56 | `Execute` 有 `defer cancel()`。返回 Paused 时 ctx 已取消。`TaskState` 没有 Steps | P0：Execute 结束即回收上下文，不 resume。P1 `resume_task_id` 必须把 `TaskSpec` 留在 `taskEntry`，从 `CurrentStep` **新**跑 Execute，不是解开旧 goroutine |
| B57 | `applyExpect` 只在 `marshalActionResult`（逐步 click）。`task_run` 的 StepSpec 没有 `expect=`，终态是 `success_criteria` / per-step `Verify` | WP11 只约束合并工具 click/type/select。禁止把 `expect=` 塞进 task_run 步骤 |

## 1. 决策摘要

借控制环，不借 GUI 栈。让 **逐步 `browser(click)` 与 `task_run` 看见同一套 status**，并且 AskUser 能进 RunLoop。

| 路径 | 做什么 |
| --- | --- |
| 默认聊天 | CaptchaWidget 先拦后停；导航类 fingerprint；目标类无 expect 不能 `ok` |
| `task_run` | 同步跑；ask/blocked 则 Execute 返回 Paused；Handler 输出 AskUser 标记。不 wait resumeC |
| Horizon | 短循环读结构体；人机门归 Supervisor `ai-assistant-response` |

不新增 P0 工具名。不实现外环组装器。不和 Supervisor PR 绑定。

## 2. 现状

- `completeAction` → `unchanged`；`marshalActionResult` 仅 `Status=="ok"` 或空才 `ok=true`；`applyExpect` 只在这条 marshal 路径
- `BrowserActionResult` 无 `AskUser`
- `doAgentStep` 丢弃 result；`executeOneStep` 只传 error；`executeStepWithRetry` 把 nil error 当成功、把 error 交给最多 3 次重试
- `Execute` `defer cancel()`；完成后任务仍留在 `s.tasks`
- `task_run` Handler 从不返回 `__ASK_USER__`
- `Press`/`HandleDialog` 无 ref；Click 先点后 observe
- `shouldUseVision`：`Captcha \|\| Canvas`（observe 时烧掉唯一 vision 槽）
- `rememberSubmitClick` 在 completeAction 之前（Click ~436，ClickText ~482）
- Pause/Resume 不是 LLM 工具；AskUser 恢复只注入 tool_result
- `playbook.go` 与合并工具 description 仍教 Optional expect、login/MFA 停
- Horizon 短循环不是 `RunLoop`

## 3. 目标、非目标、不变量

目标：CaptchaWidget **先拦后停**；逐步动作与 task_run 同一判定；AskUser 能被 ParseAskUserResult 看见；目标类无 expect 不能 `ok`（仅合并工具）；verify pierce；complete 只由核验器写；Paused 不泄漏。

非目标：默认聊天三角色；P0 新工具名；导航类强制 expect；login_wall/OTP 自动停；弱合同当 ok；resumeC 接 AskUser；真站 recaptcha 当 P0 闸；本切片实现 Supervisor / EpisodeContext；把 expect= 塞进 task_run StepSpec。

不变量：

1. 一个 `browser` 名；合并入口与 `browser_task_*` 同一 status 语义。
2. compact 无 CSS / bbox / backend node / `frame_id`。
3. 合并工具目标类 `ok` 当且仅当非平凡显式 `expect=` 通过。
4. RunLoop 的 ask 字符串以 `__ASK_USER__` 开头（整串）。逐步 click 走 `marshalActionResult`；task_run 走 Handler 同一标记。
5. complete 不能由 click 写。只来自 `task_verify` / Horizon auditor。
6. `rememberSubmitClick` 仅 `ok`（Click 与 ClickText）。
7. CaptchaWidget 不占用 vision。`shouldUseVision` 不得因 widget 为 true。
8. 网页走 `browser`，桌面走 Computer Use。连续缺 expect 不得建议 `computer_*`。
9. 同步 Execute 一旦为了 ask 返回，不得再占 goroutine 等 resumeC。现有 Pause/Resume 保持 UI-only。
10. 判定顺序固定为 §4。ask/blocked 禁止当 error 重试，也禁止当成功续跑。变异动作未过 CaptchaWidget 门不得 mutate。

## 4. 状态、优先级、目标类

判定顺序（先匹配先停；**mutate 之前**）：

1. policy deny / blocked_domains / AllowUpload·AllowDownload·AllowPopup 否决 → `blocked`
2. 当前页 `CaptchaWidget` 且动作属于 click/type/press/select/set_files/dialog → `ask`（不点击）
3. 目标类且无合法 expect → `expect_failed`（`reason=missing_expect`）——仅合并工具路径
4. 有 expect 但未满足 → `expect_failed`（`reason=mismatch`）
5. 导航类且 fingerprint 未变 → `unchanged`
6. 否则 `ok`（此时才 remember 提交窗）

目标类：ref 的 name/role/`input type=submit` 命中现有 `submitClickKey`。只从 ref 字段判定。链接 / tab / menuitem 不是目标类。Horizon Acceptance 不得把菜单点击升级为目标类。

平凡 expect 拒绝：空、长度 < 2、`url_contains` 为 `/` 或 `http`。`applyExpect` 不得把平凡 expect 把 `unchanged` 升成 `ok`。

LLM 可见准则：`url_contains`、`url_matches`、`text`、`ref_appears`、`ref_gone`、`checked`、`select_value`、`dialog`、`no_flag`。

不发明 `untrusted`。保持 `ok` / `unchanged` / `expect_failed`，新增 `ask` / `blocked`。

## 5. AskUser、task_run、回收

`CaptchaWidget`：iframe src 含 recaptcha/hcaptcha/funcaptcha，或「拖动滑块」；class/id 含 captcha 仅当同时满足上者之一。OTP「验证码」→ `MFA`，允许 type。`login_wall` / OTP **不得**自动 AskUser。

先拦：有效 last snapshot 的 `PageFlags.CaptchaWidget`，否则 flags-only peek。peek 失败 fail-closed 为 ask（宁可停，不要盲点）。

结构体：`BrowserActionResult` 增加 `AskUser`。

两条出口都必须是整串 `__ASK_USER__`+JSON：

```text
逐步 click → marshalActionResult
task_run   → Handler（Paused 时，不是 status JSON）
```

task_run 管道：

```text
doAgentStep → executeOneStep → executeStepWithRetry → Execute → Handler
ask/blocked：
  不重试、不点、不 wait resumeC
  Execute 标 Paused 后返回（defer cancel 可跑；P0 不 resume）
  Handler 输出 AskUser 标记或 blocked 的 ok=false
用户「继续」→ tool_result；不调用 Resume()
playbook：继续后必须先 observe
```

回收：`StopAgentSession`、inactivity timeout、同 session 下一次 `Execute` 启动前，Cancel 并删除 Running/Paused（Completed/Failed 一并清掉，避免 map 泄漏）。

P1 resume：`taskEntry` 保留 `TaskSpec` + `CurrentStep`，新 Execute 从下一步开始。禁止解开已 `defer cancel` 的旧调用。

Horizon：读 `Status/AskUser`；Supervisor 发 `ai-assistant-response`。禁止 `finalizeSharedLoopAskUser`。禁止第二张 IM AskUser 卡。

## 6. 控制环

```text
Observe（CaptchaWidget 不截图；OTP/login_wall 只 flags）
  → mutate 前 §4
  → 默认聊天：ask 则 RunLoop 停；继续后 observe
  → task_run：ask 则 Execute 结束；Handler 发标记；任务 Paused 待回收
终态 task_verify pierce / success_criteria；失败不得 Completed
applyExpect 只更新 last_expect，不写 complete
```

## 7. 工作包

```text
WP12  CaptchaWidget + 先拦 + 视觉 + 优先级 + stepOutcome 贯穿 + Handler 标记 + 任务回收 + 幂等窗 + playbook
  └─ WP11 合并工具缺 expect=expect_failed + 平凡 expect（不改 task_run StepSpec）
        └─ WP13 Verifier pierce；合并工具禁止 doStep；task_verify compact
              └─ WP14 账本投影
                    └─ WP15 默认聊天裁剪；可选 resume_task_id（需持久化 TaskSpec）
```

从 WP12 开始。WP12 完成前不合并 WP11。

### WP12 — 停机（P0）

加 `CaptchaWidget`；改 `shouldUseVision`；mutate 前闸门；`BrowserActionResult.AskUser`；`stepOutcome` 贯穿 doAgentStep / executeOneStep / executeStepWithRetry / Execute；task_run Handler 输出标记；Click/ClickText 仅 ok 时 remember；session_stop 回收；本地 captcha iframe 夹具；改 `playbook.go` 与合并工具 description 里「login/MFA 也问人」那句。

**完成标准**：

- 夹具 iframe 上 **尚未点击** 即 click → ParseAskUserResult
- `doAgentStep` / Execute 已返回；`executeStepWithRetry` 重试次数仍为 0
- `task_run` Handler 返回值 `HasPrefix(__ASK_USER__)`，不是 `{"status":"paused"}`
- OTP type 不 AskUser；Press Enter 与 dialog accept 在 widget 页为 ask
- navigate 离开验证码页允许
- session_stop 后 GetState 不到残留任务
- observe CaptchaWidget 不截图
- unchanged 后 3 分钟内可再 submit
- playbook 不再要求对 login_wall/MFA 自动停

### WP11 — 目标类合同（P0）

仅合并工具。缺 expect → `missing_expect`。Playbook 一行示例。widget+提交按 §4 先 ask。连续 `missing_expect` 只 nudge 一次。平凡 expect 不得被 `applyExpect` 升成 `ok`。

**完成标准**：链接无 expect、URL 变 → `ok`。提交无 expect、无 widget → `expect_failed`。`url_contains:/` 被拒。widget+提交 → `ask` 优先。task_run 步骤不要求 expect= 字段。

### WP13 — 核验（P1）

不重写 doAgentStep 的动作派发。合并工具 CSS `doStep` fail-closed；`TaskVerifier` 仍 `WaitForSelector`；`task_verify` dump 生 JSON。`Verify` 走 agent session pierce。verify 零 click。

### WP14 — 账本（P1）

complete 仅 task_verify / Horizon auditor。`applyExpect` 只写 `last_expect`。

### WP15 — 裁剪（P1）

pending ask 或 Paused 不裁。可选 `resume_task_id`：从 `taskEntry.Spec` 的下一步新 Execute。

## 8. 对外环

Horizon P2 消费同一 status；完成门吃 verify digest；ask 走 Supervisor 事件。本切片不实现组装器、不进 `sessionLoops`、不调用 `RunTaskWithSubAgent`。外环文档状态以 `longhorizon-harness-plan.md` 为准，不在本文件跟踪其审查次数。

## 9. 阶段

| 阶段 | WP | 可见变化 |
| --- | --- | --- |
| P0a | WP12 | 先拦后问；task_run 与 click 都能被 ParseAskUserResult；不重试；Paused 可回收 |
| P0b | WP11 | 合并工具提交无 expect 不能 ok |
| P1a | WP13 | pierce 验收 |
| P1b | WP14 | ledger excerpt |
| P1c | WP15 | 长聊天变短；可选新 Execute resume |

WP12 未完成前不合并 WP11。不和 Supervisor PR 绑定。

## 10. 明确不做

不恢复 eval/click_at/每步截图；不对全部 activating 强制 expect；login_wall/OTP 不自动 AskUser；弱合同不当 ok；不把 resumeC 接成 AskUser；不以真站 recaptcha 作为 P0 完成；click `ok` 不写经验；不把 PNG 放进 tool result；不复制 LongHorizon GUI 栈 / 三 LLM 工具 / Claude Code executor / 25×1800s；不把 expect= 塞进 task_run 步骤；不把 ask 当成 error 去重试。

## 11. 测试

- WP12：本地 iframe **click 前**即 AskUser；doAgentStep Status=ask；executeStepWithRetry retries=0；task_run 返回值可 ParseAskUserResult；OTP type 不问；Press/dialog 在 widget 页问；navigate 允许；session_stop 清空 map；observe 无 vision；ClickText 幂等与 Click 相同；同批第二调用跳过；`class*=captcha` 单独不触发；playbook 测试不再含 login_wall 自动停
- WP11：导航 URL 变 → ok；提交 missing_expect；平凡 expect 拒绝且不能升 ok；widget+提交 → ask 优先；连续 missing_expect 无 computer_*；task_run 无 expect 字段仍可跑完 success_criteria
- WP13：shadow verify；合并工具不走 doStep
- WP15：pending ask 不裁；resume 是新 Execute
- 回归：compact SoM、unique locator、frame/OOPIF、WaitForStable、policy 默认 false

真站点通过率仍是 WP10 闸，不是 WP12 闸。

## 12. 实施入口

**P0a / WP12**：先加 `CaptchaWidget` + 本地夹具 + **mutate 前闸门**，再让 Click 产出 ask（不点），立刻把 `stepOutcome` 穿到 Execute，**改 task_run Handler 输出标记**，再改 playbook、marshal、任务回收。不要先改 expect，不要接 resumeC，不要把 ask 当成 error。
