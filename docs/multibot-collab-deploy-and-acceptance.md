# 多机器人协作（S1–S5）部署计划与验收测试计划

提交：S1 59b95f84 · S2 87ef2084 · S3 eb9dcf35 · S4 35128a62 · S5 b4bd882a（都在 main 上）。
本文只是计划，编写时没有部署，也没有接触任何主机。

## 一、部署计划

### 0. 前提
- 等另一位 worker 在 papers 上的部署和验收结束，并确认主机空闲后再开始。
- 构建一律用 `CGO_ENABLED=0`。产物要能说明来自哪次提交：`go version -m <bin>` 显示 `vcs.revision=b4bd882a…` 且 `vcs.modified=false`。不能从有未提交改动的 checkout 构建（这次 15:36 出现的 59b95f84 + modified=true 那种情况就是反例）。
- 组件：maclaw-hub（hub/cmd/hub）、MaClawSrv、guiapp（Windows 客户端，只是 S2 风格对话框要用；服务端功能不依赖它）。
- 数据：只给 record JSON 加了可选字段（styles、rooms，以及 cross_notes 上的 state/root_id/reviewer 等），不需要迁移。老版本读到新数据会忽略这些字段，所以可以回滚。

### 1. 开关（全部默认开启，设为 off 就关闭）
| 变量 | 作用于 | 关闭后的效果 |
|---|---|---|
| MACLAW_STYLE_LAYER | MaClawSrv / guiapp | 提示词中不再加入风格层，事实行恢复为旧文本 |
| MACLAW_ASSIGNMENTS | hub + MaClawSrv | 不校验 state、不记录因果字段、不能评审，相关工具隐藏 |
| MACLAW_DISPATCH_PARALLEL | hub | 恢复为每轮只派一个任务（串行） |
| MACLAW_PEER_REVIEW | hub | 发布/付款类任务不再自动指定评审人（主机器人手动指定的仍然有效） |
| MACLAW_BOT_SAY | hub + MaClawSrv | 子机器人不能直接对人说话；群聊也随之关闭 |
| MACLAW_BOT_ROOMS | hub | 群聊关闭 |

### 2. 分步上线（先上 papers，再上 top）
1. **影子期**：在 hub 和 MaClawSrv 上把 MACLAW_ASSIGNMENTS、MACLAW_BOT_SAY、MACLAW_BOT_ROOMS 设为 off，只开 S1/S2 的风格层。部署顺序：先 hub，再 MaClawSrv（MaClawSrv 的新动作依赖 hub 的新接口）。观察 30 分钟：发布流水线的验收用例必须全部照旧通过（S1 改动了 PUBLISH_RESULT 和 REFUSED 行）。
2. **打开 S3**：把 MACLAW_ASSIGNMENTS 改为 on（hub 和 MaClawSrv 都要改），并逐个重启。检查 `/api/v1/bots/assignments` 能返回结果，并行派发日志里能看到 `stage=hub.hub_dispatch_delegate` 同时出现两个不同的 bot。
3. **打开 S4**：开启 MACLAW_PEER_REVIEW 和 MACLAW_BOT_SAY。
4. **打开 S5**：开启 MACLAW_BOT_ROOMS。
5. 每一步做完都跑一遍下面验收计划里对应的用例，通过后再进入下一步。

### 3. 回滚
- 首选关开关，然后重启对应进程，不用换二进制。
- 如果需要换回旧二进制：数据层是向后兼容的，直接换回 59b95f84 之前的构建就行，多出来的字段会被忽略。
- 每次替换前先备份旧二进制（例如 `maclaw-hub.bak-<rev>`），并记下 sha256。

### 4. 已知限制（部署前须知）
- 子机器人直接说的话（say）和群聊消息，目前只能通过 `GET /api/v1/bots/said[?room=]` 读取。GUI 和移动端还没有展示界面，需要单独排期做 UI。在 UI 做好之前，验收只能看 API 返回。
- 并发上限：每轮派发最多启动 8 个任务，每个 bot 一个，而且每个 bot 有自己的桌面；还没有做成按用户配置的环境变量。
- 发布/付款类任务靠通用的关键词正则识别，没有任何针对特定站点的逻辑。漏判时，主机器人可以用 `delegate reviewer=` 手动指定评审人。

## 二、验收测试计划
准备工作：一个测试账号，下面有 3 个 bot：主（primary）、A、B。A 和 B 各自绑定独立的桌面。所有时间按 UTC+8 记录。每条用例都要留下 API 返回和截图作为证据。

### T1 主机器人并行委派给两个 bot，各用各的桌面
1. 对主机器人说：“让 A 去整理本月订单截图，同时让 B 写一段产品介绍。”
2. 预期：主机器人调用了两次 `delegate`。hub 日志在几秒内先后出现 bot_a 和 bot_b 的 `hub_dispatch_delegate`，后一个不用等前一个结束。两台桌面同时在操作。
3. 在 A 的桌面忙碌时再委派一个任务给 B：B 不受影响，A 的任务排队等待（`state=queued`）。
4. `GET /api/v1/bots/assignments` 显示两条任务：先是 `running`，结束后变为 `completed`。
5. 越权检查：让 A 去委派任务给 B，返回 403 NOT_PRIMARY（子机器人不能委派）。
6. 主机器人调用 `accept_result` 或 `return_result`。如果是退回，会生成一条 parent_id 指向原任务的新任务，A 重新执行。

### T2 子机器人直接向人报告
1. 委派一个任务给 A。任务完成时，A 调用 `action=say` 直接对人说结果。
2. 预期：`GET /api/v1/bots/said` 里出现一条 `from=bot_a` 的消息。
3. 去重检查：主机器人随后再把同样的内容说一遍，应被拒绝（409 ALREADY_SAID，工具返回 `SKIPPED already_said`），人不会看到重复的话。
4. 提问路径：让 A 遇到二选一的情况时，用 `report state=waiting_human expects_reply=true` 提问。A 这一轮结束；`assignments` 里显示 `waiting_human` 和 `open_question`。回答之后 A 被唤醒，继续执行。
5. 登录墙：让 A 打开一个需要登录的站点。预期 A 发出 `report state=needs_login` 后停止，不输入任何密码。

### T3 发布任务的同伴评审
1. 对主机器人说：“让 A 把这篇稿子发布到 <测试站点>。”
2. 预期：返回的委派记录里 `reviewer=bot_b`（第三个 bot）。
3. A 完成后报告 completed（附截图或链接）。B 的邮箱收到一条“请审核 assignment=…”的消息，并带有产物。此时 `assignments` 显示 `awaiting_review`。
4. B 调用 `accept_result`：状态变为 accepted，主机器人收到“审核 accepted”的通知。另跑一次，让 B 调用 `return_result note=标题太长`：A 收到退回后的新任务。
5. 只有 2 个 bot 的账号：评审人回落为主机器人。设置 MACLAW_PEER_REVIEW=off 后，不再自动指定评审人。
6. 发布流水线本身的行为（计划阶段、已发布后拒绝再发、PUBLISH_RESULT 事实行）与改动前一致。

### T4 群聊
1. 创建群：`POST /api/v1/bot-rooms {"name":"项目群","members":["主","A","B"]}`。
2. 人发“今天进度怎么样？”：只唤醒主机器人（`woken=[bot_main]`）。主机器人用 `say room=<id>` 回复。
3. 人发“@A 稿子写到哪了”：只唤醒 A。A 被唤醒时收到的内容里带有最近的对话记录。
4. 把 B 的 attention 设为 all：人发“大家有什么问题”，主和 B 都会听到。B 先回答；主机器人如果再说同样的话会被拒绝；如果说的是别的内容，返回里会带 `already_answered_by=bot_b`。
5. `GET /api/v1/bots/said?room=<id>` 返回的对话顺序正确。设置 MACLAW_BOT_ROOMS=off 后，建群和发消息都会被拒绝。

### T5 风格检查
1. 在 GUI 里打开某个 bot 的“风格”对话框，修改 SOUL（例如“说话简短，不用感叹号”），保存后对话一轮：回复符合这个设定。然后回滚到上一版：回复恢复原样。
2. 对照 maclaw-persona 的文字聊天规则：不说“好的，我马上去做”之类的空确认；进度只在有里程碑时汇报；没有新内容时保持沉默；结果诚实，失败就说失败，不夸大。
3. 发布完成后，提示词里只出现事实行（`PUBLISH_RESULT state=… link=…`），机器人用自己的话转述，不照搬模板。
4. 子机器人说话不重复主机器人已经说过的内容（与 T2 第 3 步、T4 第 4 步交叉验证）。
5. 设置 MACLAW_STYLE_LAYER=off 后，行为与 S1 之前一致。

### 通过标准
T1–T5 全部通过，发布流水线原有的验收用例不出现回归，hub/MaClawSrv 日志中没有新增 panic 或 5xx。
