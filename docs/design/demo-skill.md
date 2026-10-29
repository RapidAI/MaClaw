# 演示即技能（Demonstrate-and-Narrate to Skill）

状态：阶段 1、2 已实现（特性开关 `MACLAW_DEMO_SKILL`，默认关闭）。灵感来自豆包 Work 的录制与回放，但不是回放。

## 0. 第一原则

**bot 的核心是能持续推进任务，交付结果。**

下面每个设计选择都用这一条来检验：技能、检查、暂停都只为“持续推进到一个有证据的交付物”服务，
任何机制都不能让 bot 卡住，或停在半成品状态。具体落到：

- 录下来的是**意图和指导**，不是坐标脚本。执行时 bot 继续推理，界面或状态不同就调整。
- 每步有成功检查；检查不通过 = **重新规划并继续**，不是中止。
- 结束条件 = 交付物已验证且有证据（截图、文件、链接、页面状态），复用现有结果判定。
- 只有确实缺用户才知道的信息或需要用户做选择时才问。没有“仅确认”的关卡。
- 暂停（人类接管）是暂时让路，用户离开后自动继续，而不是结束。

## 1. 采集（Capture）

- 地点：bot 自己的桌面（desktopd 容器，经 hub 的 VNC/noVNC）。
- 时间线 `demoskill.Timeline`：带时间戳的事件
  - `screenshot`（Ref 指向存储的图，Text 为 OCR/说明）
  - `input`（click/type/key/scroll/drag，坐标只作参考）
  - `focus`（应用/窗口切换）
  - `narration`（讲解：语音转文字可用时用 asr，否则在 GUI 里打字备注）
- 钩子：desktopd 已有截图和输入注入；采集端在 desktopd 增加一个录制会话（开始/停止），
  从 X 事件流记输入和焦点，按焦点切换和固定间隔截图。hub 转发开始/停止和讲解。

## 2. 提炼（Distill）

- `demoskill.Compact` 去噪：重复输入、焦点抖动、空讲解、无说明截图；讲解永远保留，超长时只稀疏化其他事件。
- `demoskill.Distill`：LLM 把时间线 + 讲解变成技能。讲解权重高于原始事件；丢弃误操作和打断。
  输出：目标、前置条件、参数（每次会变的值）、步骤（意图 + 检查 + 演示做法提示 + 是否需等待 + 是否发布/付款）、交付物和证据。
- 模型输出不可用时自动修复一轮，不让流程停在这里。
- 用户可以随时编辑技能，但不需要确认才能用（Daniel 的规则：没有仅确认的关卡）。

## 3. 存储

- 跟随现有技能存储：技能目录 + `skill.yaml`（`agentservice.persistImportedEntries`，含安全扫描）。
- 作为 `type: knowledge` 的 `NLSkillEntry`，`source: demo`，`requires_gui: true`，Content 为执行指导。
  这样现有的技能匹配和注入路径无需改动。原始时间线（截图等）在阶段 2 放入 blob 存储，技能里只留 `source_demo` id。

## 4. 执行（Execute）

- 技能作为指导注入正常 agent 循环（`Skill.Guidance`），明确写出：看当前状态再行动、按意图调整、
  每步检查、失败就重新规划不中止、已满足的步骤可跳过、等待步骤自选巡检间隔、结束必须有证据。
- `demoskill.Progress`：执行端的步骤跟踪。检查失败返回重新规划指令（第 3 次起要求换完全不同的方法），
  等待步骤要求拉长巡检间隔（“巡逻”），全部步骤通过后要求验证交付物；无证据不算完成。
- 交付物判定复用现有的结果判定（outcome judge）；阶段 2 把 `Progress` 接到工具循环里，每个桌面动作后更新。

## 5. 人类接管

- hub 已跟踪 desktop view hold（`HoldDesktopView`、`userDesktopViewActive`，见 `TestHoldDesktopView*`）。
- 阶段 2：hub 在 hold 生效期间把“用户在场”状态下发给 srv/desktopd；bot 的桌面动作工具在该状态下等待（不报错、不结束任务），
  hold 过期（用户离开）后自动继续，并先重新截图观察用户可能改动过的状态再重新规划。

## 6. 安全

- 发布/付款步骤（`risky`）照常走现有审核/询问规则。
- 不做反检测或模拟人类的随机化。
- 技能入库走现有安全扫描。

## 7. 分阶段

| 阶段 | 内容 | 状态 |
|---|---|---|
| 1 | 技能格式、去噪、LLM 提炼（含修复）、指导渲染、步骤跟踪/重新规划、入库到现有技能存储、单测（fixture 时间线） | 完成 |
| 2 | 人类接管自动暂停/恢复（hub hold → desktopd viewer → srv 桌面动作等待）；`Progress` 接入结果判定 | 完成 |
| 3 | desktopd 录制会话（截图/输入/焦点）+ hub API + 讲解（asr 或打字） | 已完成（见 §9） |
| 4 | GUI/Web 录制面板与技能编辑页 | 待做 |

## 8. 阶段 2 实现说明

**步骤检查接入结果判定**（`corelib/agentservice/demo_skill_review.go`）
- 技能指导末尾带一行机器可读的 `[demoskill-spec] {json}`。本轮历史里出现它（技能被加载）时，
  `ReviewDesktopOutcome` 先按顺序用现有判定（`JudgeDesktopOutcomeWithReply`，同一证据与截图）检查每步的 check，
  第一处不通过 → `Continue`，消息是 `Progress` 的重新规划指令（不中止）；全部通过后判定交付物 + 证据。
- 判定调用出错的步骤先跳过，不让判定故障卡住进度。演示技能的修复轮上限 6（通用为 2），用完后回落到通用判定。

**人类接管自动暂停/恢复**
- hub：`HoldDesktopView`（隔离 bot）向 desktopd 发 `view_hold`（TTL 60s，20s 内不重复发）；`ReleaseUserDesktopView` 发 `view_release`。
- desktopd：`view_hold/view_release` 由 Go 侧在内存表处理（TTL 上限 10 分钟），`bot_state` 回包附加 `viewer:true`。
  bot 的 MCP 通道不能发这两个 op。
- srv：开关打开时，桌面动作（截图除外）前若有人在场（接管、近期人工输入租约、或 viewer），
  按 2s/4s/8s/15s 递增间隔巡检等待，不结束本轮；用户离开后**不执行旧动作**，返回“先截图观察、按意图重新规划”。
  等满 15 分钟仍有人，返回“先做不需要桌面的部分，稍后再看，不要结束任务”。开关关闭时保持原有让步行为。

## 9. 阶段 3 实现说明

- **ctl op**（`corelib/desktop/ctl.go`）：`demo_begin` / `demo_narrate` / `demo_end`（desktopd 在 Go 里处理）和 `demo_input_begin` / `demo_input_end`（supervisor）。全部是 bot op，并由 `IsCtlHubOnlyOp` 挡住 MCP：bot 自己不能开录制。
- **supervisor**：录制打开时（`/desktops/<key>/demo_input.on`），`handle_input` 把 H.264 viewer 的输入追加到 `demo_input.jsonl`：按下/抬起（归一化坐标）、滚轮、按键。**可打印键只记成 `char`**，打出的内容（含密码）不落盘；具体值靠讲解给出。上限 2 万条。
- **desktopd**（`desktopd/demo_record.go`）：每个 bot key 一个会话。每 2s 读一次 `windows` 拿当前焦点；焦点变了就记 focus 事件并截图，另外每 20s 截一次，开始和结束时各截一次。截图最多 120 张，存在 `$TMPDIR/maclaw-demo/<id>/NNN.png`，24h 后清理；单次录制最长 30 分钟，到时只停止采样，会话保留到 stop。结束时合并 supervisor 的输入：连续字符键合成一条 `type`（只写字符数），点击坐标按 0..1000 记录。最后按时间排序，返回 `demoskill.Timeline`。
- **hub**：`POST /api/v1/bots/{id}/demo/start|narrate|stop`（`botmgmt.DemoRecord*`）。只对 owner 本人和 isolated bot 开放，开关关闭时返回 ErrDisabled。narrate 收 `{"text"}`，或 `{"audio_base64","mime"}`：Hub 的 ASR 可用时转写，不可用时返回 422 `ASR_UNAVAILABLE` 让用户改为打字。start 会返回 `asr_available`。
- **已知限制**：只能采到 H.264 viewer 的输入，noVNC 直连 x11vnc 的输入采不到，但截图和焦点照样有。录制期间 stage 2 的 viewer 暂停行为不变。
