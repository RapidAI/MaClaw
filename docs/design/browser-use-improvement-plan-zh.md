# Browser Use 改进计划

> 状态：WP0–WP10 已落地。后续控制环（WP12–WP15 已落地，第六次审查方案）见 `docs/design/browser-use-longhorizon-loop-zh.md`。

## 1. 决策摘要

Browser Use 只保留一条控制面：合并工具 `browser` + `browser-session-*` + 自研 CDP。不重新打开 Playwright / Puppeteer / 裸 `eval`。

当前失败主因不是“缺工具”，而是 **observe 只扫主文档 light DOM**，动作后再把 **未压缩的 CSS ref 全量倒给模型**。改进顺序必须是 **稳 → 准 → 狠**：先让观察与等待不再撒谎，再提高定位精度，最后补齐难页面能力。

工具名与现有 action（`session_start` / `observe` / `click` / `type` / …）保持兼容。新能力优先藏进现有 action；P1 才新增 `hover` / `press` / `dialog` 三个动词。

## 2. 目标、非目标、不变量

### 2.1 目标

1. **稳**：Shadow DOM、同域/跨域 iframe、SPA 动态页上，observe 能给出可点击的 ref；等待不再用正文长度 ±5% 当稳定信号。
2. **准**：模型只看到 compact SoM（`@eN` + role + name）；执行器用内部 locator handle，不再让模型抄 CSS。点错、点到第一个 contains-match、点到 `nth-of-type` 第一项，视为缺陷。
3. **狠**：登录后菜单、表单、富文本、OAuth iframe、下载/弹窗类任务能走完；canvas/验证码才允许一次性视觉，而不是每步截图。
4. 网页任务走 `browser`，桌面 GUI 走 Computer Use；禁止用像素点 Chrome 窗口来完成网页操作。

### 2.2 非目标

1. 不引入第二套浏览器运行时（Playwright MCP、skill 脚本、`connect_over_cdp`）。
2. 不把 `eval` / `click_at` / 每步截图加回合并工具。
3. 不在 P0 扩大 LLM 可见工具数量。
4. 不把 Browser Use 做成通用 RPA 编排器；`task_run` 只复用同一套 locator/expect。

### 2.3 不变量

1. LLM 只看见一个 `browser` 工具定义。
2. 页面动作必须带 `session_id`；禁止用 CDP target id 冒充 session。
3. Ref 对模型是不透明句柄；`selector_candidates`、bbox、backend node id 不得进入工具返回给模型的主字段。
4. 非唯一 CSS 不得作为成功点击路径；只能失败并要求重新 observe。
5. 发布/提交类点击继续保持幂等窗口（现有 3 分钟 guard）。
6. 策略字段 `AllowUpload` / `AllowDownload` / `AllowPopup` 必须真正拦截，不能只存档。

## 3. 目标控制环

```text
Observe(AX + pierced DOM + frames)
  → Compact SoM (@eN, role, name, enabled)
  → LLM(ref + intent [+ optional expect])
  → Locator(role/name/test-id → unique CSS → backend node)
  → Actuator(CDP input, frame-aware)
  → Verifier(expect or structural delta)
  → compact SoM or compact error
```

失败路径：Verifier → LLM compact delta，而不是再倒一次 200 条 ref。

## 4. 工作包与依赖

依赖只能向下。P0 未完成前不开始 P2 视觉。

```text
WP0 契约与测试夹具
  ├─ WP1 Observe：AX + shadow + compact SoM
  ├─ WP2 Frames：frame_id + attachToTarget
  ├─ WP3 Wait：network quiet + mutation quiet
  └─ WP4 动作对齐：select/scroll/set_files 吃 ref；enforce policy
        └─ WP5 Locator：role/name；去掉非唯一 legacy
              ├─ WP6 新原语：hover / press / dialog
              ├─ WP7 expect= 与 compact 失败
              └─ WP8 playbook + 路由边界
                    └─ WP9 视觉-on-empty / 登录墙 / 下载弹窗
                          └─ WP10 真站点验收闸
```

### WP0 — 契约与夹具（所有阶段前置）

- 扩展 `livesmoke` 测试页：open shadow、disabled 按钮、重复可见文本、跨域 iframe、`aria-label` 菜单。
- 约定工具返回 JSON：`ok`、`display`、`data.snapshot_id`、`data.refs`（compact）、`data.page_state`、`data.delta`（仅失败/变化）。
- 文件：`corelib/browser/live_automation_smoke_test.go`、`corelib/browser/observe_test.go`、`docs/browser_replay.md`（删掉已禁用的 screenshot 示例）。

**完成标准**：单测能断言 compact ref 形状；livesmoke 夹具覆盖 shadow / iframe / 歧义文本。

### WP1 — Observe：AX + shadow + compact SoM（P0）

- `Accessibility.enable` + `Accessibility.getFullAXTree` 作为主交互列表。
- 注入脚本递归 `open` shadow root；closed shadow 用 AX 补。
- 过滤：`disabled`、`aria-hidden`、`display:none`、面积为 0；默认只要可见。
- 上限约 80 条交互 ref；超出按可见 + 在视口 + 有名字排序截断，并设 `refs_truncated=true`。
- 给模型的字段：`ref`、`role`、`name`、`tag`、`enabled`。内部仍保留 candidates / bbox / backend node。
- 文件：`corelib/browser/observe.go`、`corelib/browser/types_agent.go`、`corelib/browser/refs.go`、`corelib/browser/actions.go`（post-action observe 改走 compact）。

**完成标准**：shadow 内按钮能拿到 `@eN`；工具返回不含 `selector_candidates`；超过 80 条时截断可测。

### WP2 — Frames（P0）

- `Page.getFrameTree` 写入真正的 `frame_id` / parent。
- 跨域 iframe：`Target.attachToTarget` 后在子 session 上 observe/click/type。
- `clickAtLocked` / `TypeContent` 按 ref.frame_id 选执行上下文，禁止只在 top `querySelector`。
- 文件：`corelib/browser/browser.go`（SwitchPage / Eval context）、`corelib/browser/agent_session.go`、`corelib/browser/cdp.go`。

**完成标准**：同域 iframe 与跨域 iframe 内按钮都能用 ref 点中；observe 的 `frame_tree` 不再恒为 `main`。

### WP3 — Wait（P0）

- 废弃“正文长度 ±5%”作为唯一稳定信号。
- 默认：`Network` 在 quiet 窗口内无未完成请求 + DOM mutation observer 安静；超时返回当前 readyState，不伪装成功。
- `wait` 继续支持 `ref` / `selector` / `duration_ms`；navigate/click 后的 settle 走新实现。
- 文件：`corelib/browser/browser.go` `WaitForStable`、`corelib/browser/wait_stable_test.go`、`corelib/browser/actions.go` `waitForActionSettle`。

**完成标准**：带 1s 变一次的时钟页面能在 300ms quiet 内 settle；无限加载的请求不会空等到死（有上限并返回部分稳定）。

### WP4 — 动作对齐与策略（P0）

- `select` / `set_files` / `scroll` 接受 `ref`；成功后 compact observe（scroll 可只返回 excerpt + 新视口 ref）。
- 执行 `set_files` 前检查 `AllowUpload`；下载检查 `AllowDownload`；新 tab 检查 `AllowPopup`。
- 文件：`corelib/browser/tools.go`、`corelib/browser/policy.go`、`gui/tools_browser_merged.go` schema。

**完成标准**：无 ref 的 select 仍可用 selector；有 ref 时不要求模型发明 CSS。默认 policy 拒绝未授权上传/下载。

### WP5 — Locator（P1）

- 解析顺序：backend node（同 snapshot）→ role+name 唯一 → test-id → label/placeholder → 唯一 CSS。
- 删除 observe 脚本里“即使不唯一也 `pushRaw(legacy)`”。
- 文本点击：精确唯一才点；多个 contains 匹配返回候选列表，状态 `ok=false`。
- 文件：`corelib/browser/observe.go` `selectorCandidatesFor`、`corelib/browser/refs.go`、新建 `corelib/browser/locator.go`。

**完成标准**：两个“发布”按钮时 `click text=发布` 失败并列出 `@eA`/`@eB`；带 `data-testid` 的控件不走 nth-of-type。

### WP6 — hover / press / dialog（P1）

- 合并工具新增 action：`hover`、`press`、`dialog`。
- `press`：`Enter` / `Escape` / `Tab` / 方向键 / 常见快捷键，走 `Input.dispatchKeyEvent`。
- `dialog`：`Page.javascriptDialogOpening` 监听；`accept` / `dismiss` + optional prompt text。
- 文件：`gui/browser_tool_action.go`、`gui/tools_browser_merged.go`、`corelib/browser/tools.go`、`corelib/browser/actions.go`。

**完成标准**：hover 后菜单项出现在下一次 compact observe；`alert` 可 dismiss；无新的独立工具名。

### WP7 — expect= 与诚实失败（P1）

- click/type/navigate/select 可选 `expect`：`url_contains` / `text` / `ref_appears` / `dialog`。
- 未传 expect 时默认：结构变化或目标态改变；否则 `ok=false` + `data.delta`。
- 禁止“点了就算成功”。
- 文件：`corelib/browser/actions.go`、`corelib/browser/task_verifier.go`（复用 criterion 类型）。

**完成标准**：点击被遮挡且 JS fallback 失败时返回 occluded error，不返回 `clicked @eN`。

### WP8 — Playbook 与路由（P1）

- 仿 `computerUsePlaybookSection`：仅当 `LabelBrowser` 激活时注入短 playbook（observe → ref → click/type → 再 observe；禁止 screenshot/eval；网页不要走 computer_*）。
- UIC：弱词 `click` 不足以单独激活 Computer Use；“打开网页/Chrome 点购买”主标签为 browser。
- 文件：`gui/im_system_prompt_gui_sections.go`、`corelib/computeruse` playbook 交叉提示、`corelib/intent/calibration_cases.go`、`gui/computer_use_routing.go`。

**完成标准**：browser 任务的系统提示含 playbook；校准集新增“打开 Chrome 点购买 → browser”。

### WP9 — 狠：视觉、登录墙、生命周期（P2）

- 仅当 AX+DOM 交互 ref < N，或 observe 命中 captcha/canvas 标记时，才截一次图并走现有 `VisionFirstOCRProvider`。
- observe `display` 增加 `login_wall` / `captcha` / `mfa` 提示，停止盲点。
- 等待 popup target、`Browser.downloadProgress`、file chooser；结果挂到 session。
- `task_run` 的 retry 改为 locator+expect，删除对已禁用 OCR 的依赖。
- 文件：`corelib/browser/ocr_vision_first.go`、`corelib/browser/search.go`（captcha 检测上收）、`corelib/browser/download.go`、`corelib/browser/retry_strategy.go`、`corelib/browser/task_supervisor.go`。

**完成标准**：普通表单零截图；canvas 页允许一次视觉；验证码页会停并请求用户。

### WP10 — 真站点闸（P2，P0/P1 也要有最小集）

本地 `livesmoke` 不够。每个阶段至少保留可重复的 fixture；P2 用 5 类页面做通过率门槛：

| 夹具 | 覆盖 | 最低通过 |
| --- | --- | --- |
| 带 label 的表单 | type + select + submit expect | P0 |
| SPA hover 菜单 | hover → click 子项 | P1 |
| 跨域 iframe | OAuth/支付框内按钮 | P0 |
| open shadow 控件 | shadow 内 click | P0 |
| contenteditable + markdown | type content_format=markdown | 已有，回归 |

P2 再加：登录墙检测、下载完成、canvas 视觉-on-empty。没有通过率证据不得宣称该阶段完成。

## 5. 阶段切片与建议提交

每个阶段可独立合并。不要把 P0–P2 做成一个巨型 PR。

| 阶段 | 工作包 | 建议提交切分 | 模型可见变化 |
| --- | --- | --- | --- |
| P0a | WP0 + WP1 | observe compact + AX/shadow | 返回更短的 refs |
| P0b | WP2 | frame_id 实装 | refs 带 frame，调用方式不变 |
| P0c | WP3 + WP4 | wait + select/scroll/set_files/policy | schema 多 `ref`；无新 action |
| P1a | WP5 + WP7 | locator + expect | 可选 `expect`；歧义文本会失败 |
| P1b | WP6 + WP8 | hover/press/dialog + playbook | 三个新 action |
| P2 | WP9 + WP10 | 视觉/生命周期/真站点 | 默认仍无截图 |

## 6. 风险与回退

| 风险 | 缓解 |
| --- | --- |
| AX 树在部分站点过慢或过深 | 超时回退到 pierced DOM；限制 AX 深度；保留 CSS fallback |
| `attachToTarget` 增加会话复杂度 | 按 frame_id 懒 attach，session 关闭时 detach |
| compact SoM 截断导致模型看不见目标 | `refs_truncated` + `observe query=` 后续过滤（P1 可加 query）；先做视口优先 |
| 新 wait 在长轮询站点永不静默 | 硬超时 + 返回 `partial_stable` |
| hover/press 被滥用 | 只进合并工具；playbook 写清先 observe |
| 视觉回退变成每步截图 | 硬开关：仅 empty/captcha/canvas；单步最多一张图 |

回退：每个 WP 用 build tag / 内部 flag 可关掉 AX 或 iframe attach，回到当前 CSS observe。默认上线路径必须是新实现，flag 只用于诊断。

## 7. 明确不做

- 不恢复 `browser_eval`、`click_at`、`record_start`、`task_replay` 给模型。
- 不让 Computer Use 的 `computer_click x,y` 成为网页操作的备用方案。
- 不在合并 schema 里继续鼓励 `selector` 作为主路径；selector 仅兼容与调试。

## 8. 实施入口

从 **P0a（WP0+WP1）** 开始：改 `corelib/browser/observe.go` 的注入脚本与 `Observe()` 的返回投影，并补 compact 单测 + livesmoke shadow 夹具。后续 WP 不得在 observe 仍只返回 light DOM CSS 时扩张 action。
