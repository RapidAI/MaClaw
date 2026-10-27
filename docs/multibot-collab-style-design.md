# MaClaw 借鉴 DeepSeekBot：多 Bot 协作 + 与人对话风格（设计稿 v0）

> 状态：设计已定（见 §11 Daniel 决定），按 §8 分阶段实现。日期 2026-10-27。
> 参考：BotHarness/DeepSeekBot @ a3b9b11（MIT, © 2026 BotHarness），本地 `/workspace/dsbot`。
> MaClaw 基线：RapidAI/MaClaw main @ 52df8605；现有设计 `docs/multibot-design.md`、`docs/desktop-per-bot.md`。
> 范围（Daniel 15:22）：**电脑操作层保持不动**（desktopd、浏览器/CDP 原语、publish 流水线、desktop 工具）；只改 Bot↔Bot 协作和与人的对话风格。每个 Bot 有自己的桌面（独立屏幕/浏览器）。

---

## 0. 结论摘要

1. **协作模型**：沿用 MaClaw 已有的"Hub 是唯一 broker、信箱持久化"的骨架（multibot-design §2.9/2.10），按 DeepSeekBot 补齐四个根机制：
   - **Assignment（委派单）= 一次 run 的持久目录项**，有 continuity key、状态、验收；不是新的任务实体，而是对 Hub run + 信箱的读模型（DSB ADR-0017/0045）。
   - **报告协议**：子 Bot 只发 `progress / completed / blocked / waiting_human / failed` 五种状态 + `expects_reply`；问问题 = 结束本轮，答复 = 恢复同一 run（DSB ADR-0059）。
   - **验收在委派方**：主 Bot（Orchestrator）收到 completed 先对照原始要求和证据检查，不达标就退回（同一 continuity key），达标才告诉人；可以指派另一 Bot 做 peer review。
   - **防噪/防循环**：因果链 `root/parent/hop`（hop ≤ 32 硬上限，现有 8 条/4 跳的"无人发言"上限保留）、Bot 间不寒暄不致谢、群里"无新增不发言"、外部 Bot 回合上限。
2. **风格层**：每个 Bot 一份 `SOUL.md`（人格/语气/长期原则）+ `MEMORY.md`（核心记忆索引），存在 Hub、会话内冻结；所有写死的 "Reply …" 话术改成**只给事实**的结构化结果，由模型按 SOUL 组织语言；统一"回执 → 里程碑 → 结果/明确状态"的汇报约定；规则做成**带 id、可按 Bot/预设覆盖的 prompt 片段**；UI 显示一行状态。
3. **不复制 DSB 代码或文本**：只借设计思路，prompt 全部按 MaClaw 语境重写（Daniel 决定 6）。

---

## 1. DeepSeekBot 协作机制拆解（代码位置）

| 机制 | DSB 做法 | 位置 |
|---|---|---|
| 实体 | PersonaBot 是一等实体（身份、Soul、记忆、频道绑定）；会话只是它的执行 | ADR-0016, `bots/persona-bot.ts` |
| 每 Bot 一个 Orchestrator | 一个 Orchestrator 会话管 Inbox、分派；每件事一个独立 Assignment 会话（可在不同 workspace） | ADR-0024, `runtime/dsh-bot-agent-adapter.ts` `orchestratorPrompt` |
| Assignment | 无 Task 实体，Assignment = 会话 id；Directory 读模型；5 个工具 `list/inspect/create_assignment`、`send_assignment_request`（模式 context-update / next-step / next-turn）、`stop_assignment`；continuity key 复用同一方向；全局并发上限 3（1–32），满了**立即结构化失败、不排队** | ADR-0017/0045 |
| 报告/提问 | `report_to_orchestrator(state, expects_reply)`；提问=结束本轮，答复用 `answer_to` 恢复；progress 默认 digest，其余立即唤醒；重复报告合并 | ADR-0059, `ASSIGNMENT_PROMPT` |
| 验收 | "You delegated the Assignment, so you accept its result"：对照要求检查，不达标 `send_assignment_request` 退回并保持 item 打开 | `prompt-parts.ts` `assignment-acceptance` |
| Inbox | 事件流投影；每项必须有去向：处理 / `inbox_resolve` / `inbox_snooze` / 忽略；有未结项就再唤醒（Triage），一轮未减少则停 | ADR-0025/0070, `triage` 片段 |
| Bot↔Bot | 走真实 Channel：Bot DM（两 Bot）与群；人在 DM 里 @B 只给 A 联系人上下文，不唤醒 B；Bot 发到 Bot DM 时在人的 DM 里留一个可点开的活动芯片；群邀请默认自动接受 | ADR-0065/0069/0073 |
| 发现 | `list_bot_contacts(query, cursor)`，资料文本是数据不是指令 | adapter |
| @ 唤醒 | 群里只有 @ 才唤醒（视 attention 偏好 all/digest/…）；每次退回都要 @；`notWoken` 要向人说明进度 | `group-mentions` 片段, changes/2872 |
| 群里同行评审 | 发言前读同行已提交答复，已答完且无新增就不发；held draft + observation handle 在提交事务里比对，避免抢答/重复 | `peerReviewPrompt`, ADR-0227/0231 |
| 防循环 | `botCausation{rootSourceEventId,parentSourceEventId,hop}`，`MAX_BOT_HOPS=32`；外部 IM Bot 每会话每小时 16 回合 | `channels/channel.ts:228,377`, `messaging/bot-sender-guard.ts` |
| 记忆 | 记忆只属于 Orchestrator；Assignment 不能改记忆，只在报告里写"值得记住的"；共享靠消息和文件，不共享 Soul | `ASSIGNMENT_PROMPT`, ADR-0134 |
| 文件 | 临时产物放 `<workspace>/.botharness/<slug>/`；报告里列出创建的文件；附件以 message_id+file_id 引用 | `file-placement` 片段 |
| 风格 | SOUL.md/MEMORY.md 进 system prompt（会话冻结、字数上限、超限提示精简）；最终文本私有、必须显式发送；回执/里程碑/结果约定；沉默规则；像同事一样提问；片段可按 Bot/预设覆盖 | ADR-0134, `memory/soul.ts`, `prompts/prompt-parts.ts`, `prompt-presets.ts` |

## 2. MaClaw 现状对照（已有 vs 缺口）

已有（保留）：Hub 信箱（report kinds done/blocked/progress/question/artifact/info/reply、7 天保留、去重、限速、乒乓 8 条/4 跳）、Hub dispatcher（delegate 由 Hub 启动、空闲 Bot 被唤醒、每用户一个桌面 run、6 次/小时唤醒上限）、`notify_main`/`check_inbox`/`delegate` 桌面动作、GUI `multibot_tools.go`（list_bots/assign_task/send_to_bot/stop/pause/bot_events/share_file_with_bot）、文件随消息内容寻址、per-bot 隔离桌面（`desktop_mode=isolated`）、login sync、publish 状态机。

缺口：
1. 没有"委派单"概念：delegate 是一次性 note，没有 continuity key、状态目录、`answer_to` 恢复、验收闭环。
2. 只有主 Bot 能控制；Bot 间没有群/房间，也没有 peer review 角色。
3. "每用户一个桌面 run"与 per-bot 桌面矛盾 → 并行受限。
4. 话术写死：`desktop_publish_record.go:158` `desktopPublishRecordText`、`desktop_coach.go:476/588/628/631` 的 "Reply to the user… / Give the final answer now…"；`desktop_bot_ack.go` 结构化状态→人设句子，但无每 Bot 人格。
5. 无统一汇报约定：用户常只看到最终回复；`notify_main` 只发主 Bot。
6. 规则写死在 Go 字符串，不可按 Bot 调。

## 3. 协作模型（采用方案）

### 3.1 角色
- **主 Bot = Orchestrator**（GUI 本地主 Bot，或服务器端 primary）：唯一面对人的入口（人也可直接和任何 Bot 私聊，那时该 Bot 就是自己那件事的 Orchestrator）。职责：分诊、委派、验收、向人汇报。
- **子 Bot = Assignment 执行者**：在**自己的桌面**上跑一个 run；只通过报告协议和委派方说话；不直接对人发结果（除非委派方让它接管对话，或人在它私聊里）。
- **Reviewer**：任何 Bot 都可被指派为某个 Assignment 的评审，只读对方产物+证据，返回 `approve / changes_requested + 理由`。

### 3.2 Assignment（Hub 读模型，无新任务实体）
`Assignment{id = 首个 run_id, owner_bot, target_bot, continuity_key, title(≤40字，人话，动词开头), description, state(queued|running|waiting_human|blocked|completed|failed|cancelled|accepted|returned), parent_assignment, root_request_id, artifacts[], evidence{}, review{reviewer, verdict, notes}, created_at, updated_at, runs[]}`
- 底层仍是现有 delegate note + run；Hub 把同一 continuity key 的多次 run 归为一个 Assignment（**退回 = 同 key 新 run，带上一次报告与退回理由**）。
- 工具（主 Bot / 任何 Bot 对自己委派的单）：`list_assignments(query,state)`、`inspect_assignment(id, tail)`、`create_assignment(bot, title, description, continuity_key, artifacts, acceptance)`（非阻塞）、`send_assignment_request(id, mode=context|next_step|next_turn, text, answer_to?)`、`stop_assignment`、`accept_assignment(id, note)` / `return_assignment(id, missing)`、`request_review(id, reviewer_bot)`。
- `acceptance`：委派时写明验收条件（例："知乎文章链接 /p/<id>，回读含 3 张图"）。completed 报告必须逐条对应。
- 子 Bot 侧：`desktop{action:"report", state, expects_reply, text, paths[], evidence{}}`（`notify_main` 保留为别名，kind 映射：done→completed, blocked→blocked, question→waiting(expects_reply), progress→progress, artifact→progress+paths）。提问 = 报告 `expects_reply=true` 后**结束本轮**；Hub 收到 `answer_to` 的请求后用同一 run 的 pause/resume 或同 key 新 run 继续。
- 并发：每用户全局 `assignment_concurrency`（默认 3，1–8），满了**立即返回结构化错误**（active/limit/retryable），不静默排队；主 Bot 告诉人或稍后重试。

### 3.3 子任务在各自桌面上并行
- 前提：参与协作的 Bot 必须 `desktop_mode=isolated`（自己的 display + Chromium profile）。Hub dispatcher 的"每用户一个桌面 run"改为**每桌面一个 run**（key = 桌面用户 `<owner>::desk::<bot>`）；shared 模式 Bot 仍共用 `:20` 并串行。
- 桌面争用：同一桌面严格单 run；`MACLAW_DESKTOP_MAX_BOTS`（默认 3）满时，新委派返回 `desktop_capacity` 结构化错误（不驱逐正在跑/被接管的 Bot），与并发上限同一处检查。人接管某 Bot 时只锁该 Bot 的桌面。
- 不新增桌面控制代码：只改 Hub 的派发键和容量检查。

### 3.4 文件/产物交接
- 统一 artifact 引用：`{sha256, name, size, from_bot, assignment_id}`，沿用 Hub `bot_mail_files` 内容寻址存储（延长到 Assignment 存活期+8 天）。
- 委派时 `artifacts` → Hub 先把文件放到目标 Bot 桌面（现有 `desktop/files` 分片上传），任务说明里给出桌面路径；完成报告里的 `paths` → Hub 收文件，报告只带引用。
- 桌面临时产物约定：`~/maclaw-work/<assignment-slug>/`，报告必须列出创建的文件。

### 3.5 登录共享
- 沿用 login sync（stage 2 已上线代码：bot 屏幕丢 cookie 不登出、owner 屏幕登出才传播）。委派前 `inspect` 目标 Bot 对目标站点的登录状态（从站点经验的 per-account block 读，stage 3）；未登录 → 子 Bot 报 `waiting_human`，主 Bot 请人接管**该 Bot 的桌面**登录，登录后经 login sync 共享给其他 Bot。不新增登录逻辑。

### 3.6 Peer review 与验收
- 委派方必验：收到 completed → 检查 acceptance 条目和证据（publish 的 `PublishReport.State/Evidence/Method`、回读结果、文件哈希），缺什么就 `return_assignment`；只有 `accepted` 后才向人报"完成"。
- 可选 peer review：`request_review(id, reviewer)` 建一个只读 Assignment 给评审 Bot（它可在自己桌面打开链接核对），返回 verdict；`changes_requested` 自动作为退回理由。评审 Bot 不得修改原产物。
- Hub 规则：向人报告"已发布/已完成"的消息若引用的 Assignment 不是 accepted，Hub 在 UI 上标注"未验收"（不拦截，防误报）。

### 3.7 群房间（第二阶段）
- Hub 新增 `Room{id, owner, members[bots+humans], attention per member: all|mentions|digest|muted}`；消息仍存 Hub 信箱模型（target=room）。@ 唤醒；每次退回都要 @；`notWoken` 返回给发送者让它向人说明。
- 发言前读已提交的同行答复；无新增不发（prompt 规则 + Hub 侧"同一 root 已有 accepted 答复时，给后发者返回 `already_answered` 提示"）。不做 DSB 的 held-draft 事务（复杂度高，后续视需要）。

### 3.8 防循环 / 防噪
- 每条 Bot 消息带 `causation{root, parent, hop}`；hop>32 Hub 拒收（`collaboration_hop_limit`）；现有"无人发言 8 条 / 回复链 4 跳"保留为更紧的默认，可配置。
- Prompt 规则：对另一个 Bot 只在推进工作时回复，不致谢、不确认、不复述；progress 不唤醒（digest）；去重/限速沿用。
- Inbox 去向：主 Bot 每条来信必须"处理 / 结清 / 稍后 / 忽略"，未结清的项在下一轮以 `[Open]` 块列出；一轮未减少则停止再唤醒。

### 3.9 记忆共享
- 每 Bot 自有 `MEMORY.md`；子 Bot 不改主 Bot 记忆，只在 completed 报告里写 `remember:` 段，由委派方决定写入。
- 账号级共享事实（站点经验、per-account block）继续放 Hub 现有存储（stage 3），不进人格文件。

## 4. 风格层

### 4.1 SOUL.md / MEMORY.md（每 Bot，存在 Hub）
- Hub `bot_profile` 新字段 `soul`（≤5000 字）、`core_memory`（≤3000 字）、`prompt_preset`、`prompt_overrides{part_id: {text, base_hash}}`；版本化（沿用 stage 3 的 history+rollback 模式）。
- 每次 run 开始读一次放进 system prompt 固定前缀（会话内冻结，利于缓存）；超限截断并附"请精简"提示。
- 默认 SOUL：从 `docs/maclaw-persona.md` 抽文字版原则——不客服腔、不谄媚、做错直说、不编造、先结论后细节；去掉语音专属的"≤15 字短句"。主 Bot 可经工具 `update_soul`/`update_core_memory`（需人确认）修改。

### 4.2 话术模板 → 只给事实
- `desktopPublishRecordText` 改为返回结构化事实块，不再含 "Reply …"：
  `PUBLISH_RESULT {state, link, missing, notice, evidence, next_allowed: none|fix_and_retry|resolve_notice}`
- `desktop_coach.go` 的 refused 文案只保留"拒绝原因 + 允许的下一步"（例：`refused: already_published; result_link=…; next_allowed=final_answer`），删除"Give the final answer now (title, link, date)"之类措辞指令。
- 必须包含的事实由通用"结果交付"片段规定：链接、缺失项、未确认状态、失败原因、需要人做什么——不按站点写。
- 回归保护：测试断言事实字段存在，不断言句子。

### 4.3 汇报约定（prompt 片段 `progress-reporting`，改写自 DSB）
- 快任务直接答；答案即确认。
- 长任务（多步工具、委派、等人）：第一个动作发**一行回执**（看到了 / 要做什么 / 下一步）。
- 中途只在真实里程碑发一行（阶段完成+结果、部分结果、计划变化、卡住、问题）；不叙述每个工具调用、不定时刷屏、也不从回执沉默到结束。
- 结束必须是：完成（含结果）/ 失败及原因 / 在等什么。被迫提前停时说清已做、未做、继续需要什么。
- 机制：子 Bot 面向人的进度走 `report(progress)`，主 Bot 汇总转述；直接与人对话的 Bot 用 `say(progress)`（新动作，进人对话，限 1 条/阶段，按 run 计数）。

### 4.4 沉默与诚实
- 无需回复就不发；取消就停，不确认。
- "已发布/已送达/已完成"只能依据工具/证据结果；委派结果未验收不得说完成；发送失败不得说成功，不盲目重发。
- 问人像同事：说明为什么需要；简单缺项直接用平常话问。

### 4.5 可覆盖片段与预设
- `corelib/agentruntime/promptparts`：`[]Part{ID, Kind(system|turn), Section, Default}`，id 如 `coordination, progress-reporting, silence, honesty, asking-humans, assignment-acceptance, peer-replies, external-bot-sender, triage, result-facts`。
- 覆盖存 Hub（全局预设 + 每 Bot 覆盖），记录 `base_hash`；默认文本更新后 UI 显示"默认已变化"。

### 4.6 UI 状态行
- 每 Bot 一行：`thinking | working(阶段) | waiting_human | blocked | done`（与 botregistry 状态对齐），聊天输入框上方最多 3 个 + "等 N 个"；思考内容默认折叠，不作为回复。

## 5. 数据模型与 API 变更（Hub / MaClawSrv / GUI）

| 层 | 变更 |
|---|---|
| Hub | `assignments` 读模型（基于 delegate notes + runs，按 continuity_key 归组）；`GET/POST /api/v1/assignments`、`/{id}/request|stop|accept|return|review`；report note 新字段 `state, expects_reply, answer_to, causation, evidence, acceptance_ref`；dispatcher 派发键改为桌面用户；`assignment_concurrency` 设置；`bot_profile.soul/core_memory/prompt_*`（`GET/PUT /api/v1/bots/{id}/soul` 等，带 history/rollback）；Rooms（阶段 4） |
| MaClawSrv | 动作 `report`（notify_main 别名映射）、`say`；run 开始拉 SOUL/MEMORY/片段覆盖拼 prompt；`desktopPublishRecordText` 与 coach 文案改事实块；`answer_to` 恢复同 run（复用 pause/resume） |
| GUI | `multibot_tools.go` 新增 assignment 工具（assign_task 保留为 create_assignment 别名）；Bot 管理对话框加「灵魂/核心记忆/提示词」页；Assignment 面板（标题、状态、验收、评审、产物）；聊天里 Bot↔Bot 消息显示为可点开的活动芯片；状态行 |

## 6. guiapp UI 改动
1. 主聊天：Bot↔Bot 交流以芯片显示（"→ 发布员：发布知乎文章 · 运行中"），点开只读看往来。
2. 右侧 Assignment 面板：列表（标题/目标 Bot/状态/验收✓/评审），详情含报告时间线、产物下载、"退回/接受/停止/请评审"按钮（人也可操作）。
3. Bot 编辑对话框：新增 SOUL.md、MEMORY.md 编辑（字数/上限）、提示词预设与片段覆盖（显示"已修改/默认已变化"）。
4. 每 Bot 桌面缩略图上标注当前 Assignment 标题，接管仍按 Bot 桌面。

## 7. 迁移
- 现有 Bot：`soul` 为空时用默认 SOUL；`description` 拷入 SOUL 开头。
- 旧 `notify_main` kinds 映射到 state；旧 delegate note 视为无 key 的单次 Assignment。
- 旧 GUI/MaClawSrv：新字段可选；Hub 对旧客户端仍返回旧形状。
- 开关：`MACLAW_ASSIGNMENTS=off`、`MACLAW_STYLE_LAYER=off` 回退。

## 8. 分阶段实施（每阶段单独提交、测试、不部署直到确认）
1. **风格层 A**：promptparts 包 + 默认片段；publish/coach 文案改事实块；汇报/沉默/诚实片段接入桌面 run 与主 Bot。（无数据模型改动，风险最低）
2. **风格层 B**：Hub SOUL/MEMORY/覆盖存储 + API + 拼 prompt；GUI 编辑页；状态行。
3. **协作 A**：Assignment 读模型 + 报告协议（state/expects_reply/answer_to/causation）+ 主 Bot 验收工具 + 并发上限；dispatcher 改每桌面派发（并行）。
4. **协作 B**：artifact 引用统一、委派前登录状态检查、peer review。
5. **协作 C**：Rooms（群）+ attention 偏好 + 已答复提示。

## 9. 测试计划
- 单元：promptparts 覆盖/hash；事实块字段（不测句子）；报告 kind 映射；causation hop 拒收；乒乓与 hop 两级上限；并发/桌面容量结构化错误；continuity key 归组与退回；SOUL 超限截断。
- Hub 集成：委派→子 Bot 运行→waiting_human→answer_to 恢复同 run→completed→主 Bot return→同 key 二次 run→accept；两个 isolated Bot 并行跑；共享桌面 Bot 串行。
- MaClawSrv chrome 夹具：publish 事实块 + 验收（缺图→return）。
- 真实验收（部署后、Daniel 确认）：主 Bot 委派"发布员"发知乎、"采集员"同时导出数据（并行）；文件从采集员→主 Bot→发布员；评审 Bot 打开链接核对后 accept；检查聊天只出现回执、里程碑各一行、最终结果。

## 10. 风险
- 并行跑多个 Chromium：内存（~0.5 GB/活跃 Bot），受 `MACLAW_DESKTOP_MAX_BOTS` 限制。
- 模型不遵守汇报节奏 → 刷屏或沉默：用 run 级计数（每阶段 1 条）和"结束无状态"检测兜底。
- 去掉写死话术后最终回复漏链接 → 事实块 + "必须包含事实"片段 + 测试；上线前对比几次真实 run。
- 平台/站点特例诱惑：所有规则保持通用，站点差异只进站点经验数据。
- prompt 前缀变长影响延迟/成本：SOUL/MEMORY 上限 + 冻结前缀命中缓存。
- 与正在进行的部署/验收冲突：本设计全部改动在第 1 阶段后才开始，且每阶段独立开关。

## 11. Daniel 决定（2026-10-27 15:26 UTC+8）
1. **子 Bot 可以直接对人说话**（`say`），遵守同样的风格/沉默/诚实规则；不重复主 Bot 已说的内容（Hub 对同一 root 请求做"已告知"去重提示，主 Bot 转述前先看子 Bot 已对人说过什么）。
2. **子 Bot 之间不能互相委派**；只有主 Bot 能 create_assignment / request / stop / accept / return / request_review。子 Bot 之间可以发消息（含群里），但不能派活。
3. **Peer review 默认开启：发布类与支付类任务**（按 Assignment 的 `kind` 判断：publish / payment；kind 由主 Bot 声明，Hub 在 completed 时若无评审则自动建评审单，评审 Bot 由主 Bot 选或取空闲的另一 isolated Bot）；其余任务由主 Bot 决定。
4. **群房间本轮就做**（不推迟）。
5. 默认 SOUL 从 `docs/maclaw-persona.md` 改写（只取文字聊天部分）。
6. **只按思路重写 prompt，不复制 DeepSeekBot 文本**（因此无需 MIT 声明；代码也不复用）。
7. 保留 `/workspace/dsbot`。

上文 §3.1 "子 Bot 不直接对人发结果"、§3.7 "第二阶段"、§8 第 5 阶段顺序据此调整：群房间与协作同轮实施；§4.3 的 `say` 对所有 Bot 开放。
