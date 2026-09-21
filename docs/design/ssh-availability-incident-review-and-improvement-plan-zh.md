# SSH 可用性事件链复盘与改进计划

日期：2026-09-18
范围：语义工具迁移（semantic managed surface）中的能力可用性设计
状态：事故链已闭合，补偿层全部就位；本计划面向防复发与框架演进

## 修订记录

| 版本 | 改动 |
|------|------|
| v2 | 初版：背景/根因/计划 A–D/风险/附录 1 变更集 |
| v3 | A2 增加外来改动排除警示 + 附录 2；B4/D1 合并为单一事件源；C1 验收精确化为五项枚举；B3 三档分级；新增跟踪与责任节 |
| v4 | 附录 3 实测验证命令；语言一致性修复；B3 P1 与 §2 风险评估对齐；预先声明两个已知预先存在测试失败 |
| v5 | C1 增加 C2 依赖声明；B2 增加按家族的场景适用性（N/A）规则；D1 增加分母计数 ssh_turn_total；A1 动态 IP 缓解写入正文 |
| v6 | 事实核查轮：对照仓库实况逐条核对全部可验证声明（测试名、文件清单、债务上限、事件时间线），全部成立；仅附录 2 外来清单补入 `AppSidebarShell.tsx` 并强调"提交前重新核对"（外来集合随时间增删） |
| v7 | 跨文档一致性轮：对照 `semantic-tool-routing-design-zh.md` §11.60 修正两处——① §2 增加"与既有设计文档的关系"，明确 reviewed 表预算依赖已下沉的共用暴露闭包、不存在 Hub 一次性下发 N 授权的风险，并注明早期分析引用的对齐决议已被 §11.60 取代；② B3 增加审计范围声明（暴露闭包 corelib 共用、headless 差异只在准入层，不重写矩阵） |
| v8 | 执行轮：① B1 交付 `docs/design/managed-surface-migration-checklist-zh.md`；② B4/D1 落地——单一事件源 `[ssh-availability]`（turn_total 分母 + rescue 三层分子）、trajectory 机器可读层标记 `[ssh-rescue:<layer>]`；③ B3 审计完成，结果见附录 4（4 项立项 + 2 项注释修复） |
| v9 | 追加批变更入附录 1：观测事件流、报告可见性修复（assistantRoundProse）、凭据防线（write_file 描述 + fs.write cue）、browser/CU 注释修正；文件数 20→25；追加"报告可见性与凭据防线"分组 |
| v10 | 附录 3 补前端 vitest 命令（追加批的报告可见性修复只靠 vitest 覆盖，缺命令则评审者无法验证该部分） |
| v11 | 17:42 事故追加修：① 分类器 collapse 保留 RunnerUp 证据（激活死字段）；② 规划器入口提升 RunnerUp-ssh（修 imSemanticIntentIsManaged 早退第二缺陷）；③ 路由 miss 只读地板（knowledge_search/memory_recall 保底 + 群边界门控）；④ 新增 §7"宁慢勿乱"设计原则 + A4（tree 时限 12s→30s，用户定向） |
| v12 | 附录 1 同步至 35 文件（RunnerUp/地板/时限批次 10 个新文件入册）；标注 `corelib/intent/classifier.go` 为与并行会话的共用文件；附录 2 补 `hub/internal/httpapi/llm_usage_reports.go` |

## 1. 背景

2026-09-18 一天内，桌面用户通过 IM 助手连接驱网服务器（`www.driverdevelop.com`）的过程中连续暴露 7 个独立故障，全部表现为"SSH 工具不可用"或"无法完成服务器工作"。逐层修复、逐层暴露的顺序如下：

| # | 现象 | 根因 | 修复 |
|---|------|------|------|
| 1 | "SSH 工具配额用尽"，一回合只能执行 1 条命令 | `LabelSSH` 规则未声明 `MaxInvocations`（默认 1） | 两张规则表声明 8（与本地 shell 对齐），parity 测试钉住 |
| 2 | 旧参数形状（`session_id`/`action`）连续被拒，浪费迭代 | 语义 ssh 是闭 `{command}` schema，对话历史仍是 legacy 形状 | 准入前参数清洗 `semanticSSHInvocationArgs`（exec 模式剥宿主绑定装饰键） |
| 3 | 无存活会话时整个回合没有任何 connect 路径 | connect 能力只存在于 legacy 工具，而 legacy 只在非降级 LabelSSH 回露面出现 | connect 模式 provider（会话-less 时发布，schema `{host,user,password,port,label,command?}`）+ 规划 gate 放行 + petition 门放行 |
| 4 | 降级回合 petition 扩展死在解析器置信地板 | "降级变更型家族一律 miss"姿态无例外 | 地板豁免 + 保护性投影（只保留 ssh 与只读家族，其余变更型标签丢弃） |
| 5 | 纯 fallback 回合（无托管表面）petition 直接 false | `PetitionToolCall` 第一行守卫要求 `semanticSurface != nil` | `grantLegacySSHPetition`：overlay live legacy ssh 工具，同 effectful 预算与群聊门控 |
| 6 | 授权后重发调用仍被拒（overlay 被重建抹除） | `BuildToolsForModelRequest` 无表面分支从策略面重建 `c.tools` | 重建改道 `setVisibleToolDefinitions`（重放 overlay） |
| 7 | connect 成功后同回合无法执行命令；模型拿到数据却说"无法执行" | catalog 按回合开始时绑定（connect 模式），同回合不切换 | connect schema 增加可选 `command`，dispatch 连接后立即执行 |

另有 1 个非代码问题：服务器端 fail2ban/云防护对该出口 IP 间歇封禁（13:47 失败 → 14:07 成功 → 14:41 失败 → 手动 unban 后恢复），属外部诱因。

## 2. 根因（设计错误）

**把有状态生命周期能力（connect→exec→close + 宿主会话绑定）塞进了为无状态能力设计的每回合授予模型，并在撤除恒可用 legacy 表面时未为"分类器降级"与"宿主状态缺席"提供等价退路。**

后果：ssh 可用性同时耦合五个此前不存在的依赖——UIC 置信/降级标志、provider 发布状态（会话存在性）、兄弟预算、解析器地板、跨请求 overlay 状态。每个依赖按序在生产中变成故障模式。"修完一层暴露下一层"不是运气差，是同一设计假设的连锁投影。

同类风险评估：当前受管家族中仅 ssh 是"跨连接+执行生命周期 + 远程有状态"的能力，因此受害最深。`browser.control.web`/`computer.control.desktop` 是本地桌面绑定（有 bound-runtime 测试守护）；`database` 已有自己的 petition 先例；`shell.execute.local`/`fs.read.local` 等无状态家族风险低。

**与既有设计文档的关系**：reviewed 表上的 ssh 预算（MaxInvocations 8）依赖 `semantic-tool-routing-design-zh.md` §11.60 的框架级修复——暴露闭包已下沉到 `coretool.NextRepeatSelections` 由两个宿主共用，"每族同时只暴露一个"在 GUI 与 Hub/headless 两侧一致执行，因此不存在该文档 §1746 行警告过的"Hub 一次性下发 N 个授权"风险。本计划的分析过程中曾引用 `maclaw-srv-gui-architecture-review.zh-CN.md` 的 2026-09-05 对齐决议作为先例，该决议所述风险已由 §11.60 在框架层消除，以 §11.60 为准。

设计不变量（已写入可执行文档 `guiapp/semantic_ssh_connect_test.go` 的 `TestSSHAvailabilityIndependentOfClassifierAndSession`）：

> ssh 可用性绝不依赖分类器健康度与会话状态。四条独立退路：自信分类规划 / 降级地板豁免 / 无表面 petition 救援 / leftover 保留，任一单层改动破坏整体保证即红灯。

## 3. 改进计划

每个条目带**验收标准**；无验收标准的计划项等于没有计划项。

### A. 立即（运维与收尾，本周）

| 项 | 内容 | 验收标准 |
|----|------|---------|
| A1 | 服务器 fail2ban 增加 `ignoreip = 114.249.214.255`（当前观察到的出口 IP，见风险表"动态出口"缓解：优先云安全组白名单，fail2ban ignoreip 为双保险；若出口变化，D1 的 connect 失败率回升会提前暴露） | 连续 3 天不再出现 `no supported methods remain`；`fail2ban-client status sshd` 该 IP 不在封禁列表 |
| A2 | 本次代码改动（变更集见附录 1）走代码评审后提交。**注意：工作区混有并行会话的在途改动（llm 层、分类器、hubcenter、前端 SidebarNavRail 等，见附录 2），提交时必须逐文件显式指定附录 1 清单，禁止 `git add -A` 式提交** | 评审通过且合入；不变量测试在 CI 通过；`git show --stat` 的变更文件集与附录 1 完全一致 |
| A3 | 完整验收：重启 → "连接驱网服务器，查看状态" → 同回合拿到状态报告 | 一次真实回合内完成 connect + ≥1 条命令，模型不再让用户去 SecureCRT |
| A4 | **tree 分类时限 12s→30s**（`app_embedding.go`、`hardware_agent_runtime.go` 的 `FusionTreeDeadline`）。原则（用户原话）：**"用户需要稳定，宁愿慢，也不愿意乱"**——12 秒到点即降级是今天全部"缺工具"事故的 chaos 根源；等到 LLM 超时才降级，用首 token 延迟换正确分类与稳定表面。验收：`[UnifiedIntentClassifier] Layer 3 failed ... timed out` 日志频率显著下降；D1 的 rescue 事件率随之下降（分子分母同源可比） | 慢 hub 时段（如今日）降级回合占比下降；首 token 延迟增加 ≤18s 被用户明确接受 |

### B. 短期（防同类债复发，1–2 周）

| 项 | 内容 | 验收标准 |
|----|------|---------|
| B1 | **迁移评估清单**（交付物 = `docs/design/managed-surface-migration-checklist-zh.md`）：legacy 工具迁往受管表面前必须书面回答四个问题——① 分类器降级（Degraded/亚地板）时该能力从哪条路径可达？② 宿主状态缺席（会话/文档/配置绑定）时 provider 是否仍能发布？③ 跨请求状态（overlay/petition 授予）挂在哪个字段、`BuildToolsForModelRequest` 重建时谁重放？④ 每回合预算是多少、耗尽时模型看到什么？任一问无答案即不准迁移 | 清单文档合入；下一个迁移工具（候选：无）按清单走一遍并留记录 |
| B2 | **不变量测试先行**规范（并入 B1 清单的第 ① 问）：迁移前先写"可用性不变量"四场景矩阵测试（自信规划/降级豁免/无表面救援/回露保留），再动表面。**注意按家族声明场景适用性**：场景 3（无表面救援）依赖该家族存在 legacy 等价物可 overlay——`browser.control.web`/`computer.control.desktop` 等无 legacy 工具的家族，此场景标注 N/A 而非判失败；场景 4（leftover 保留）同理只适用于有 builtin 工具残留的家族 | 规范合入 B1；ssh 的 `TestSSHAvailabilityIndependentOfClassifierAndSession` 作为范式引用；B3 每家族的矩阵结果中明确标注各场景适用/N-A |
| B3 | **受管家族退路审计**（方法 = 把 B2 的四场景矩阵逐家族执行一遍）。按风险分三档：**P0 宿主绑定家族**（`document.write.office` 的 spreadsheet qualifier、`business.data.mis`/`database` 的 profile 绑定、`browser.control.web`/`computer.control.desktop` 的桌面绑定）——与 ssh 同构风险，逐个写全四场景矩阵测试；**P1 有状态但本地家族**（`shell.execute.local`、`fs.write.local`）——§2 评估为低风险，此处为确认性检查：跑矩阵、缺口立项；**P2 无状态家族**（`information.search.web`、`information.fetch.web`、`artifact.acquire.remote`、`fs.read.local`）——只过 B1 检查表的四问，不写矩阵。**审计范围声明**：暴露闭包（预算/兄弟）是 corelib 共用代码（§11.60），GUI 侧不变量测试已覆盖其语义，headless 差异只在宿主各自的准入层——本计划的清洗（wash）仅存在于 GUI 准入路径，reviewed headless 执行器（`corelib/agentservice/dynamic_host_ssh.go`）未改动，B3 对 headless 只核查"准入差异是否引入新缺口"，不重写矩阵 | 每家族一份矩阵/检查结果（headless 差异核查单列一行）；发现的缺口各自立项（不在本计划内静默修） |
| B4 | **petition 救援可观测事件流**（B4 与 D1 是同一事件源的两个消费者，**只埋一次点**）：在 grant/overlay 路径写一个结构化事件（事件名、层、工具名、预算消耗、模式），trajectory 写入与 `maclaw.log` 计数都从该事件派生 | 任一救援后的 trajectory 能机器读出"哪层接住了回合"；D1 的计数有数据源；不存在第二套平行埋点 |

### C. 中期（框架修订，下次语义框架迭代时）

| 项 | 内容 | 验收标准 |
|----|------|---------|
| C1 | **生命周期能力一等公民**：统一 action 化 adapter（一个 always-published 的 ssh 适配器按 action 分发），消灭 connect/exec 模式分裂。**依赖：必须在 C2 之后或与 C2 同批落地**——C1 删除的地板豁免与 overlay 救援由 C2 策略表承接，C2 未就绪时 C1 单独落地会造成可用性回退 | 精确可数：以下补偿机制被删除或降级为配置——① connect 模式分裂（provider 双模式挂接、`semanticSSHSchemaIsConnectMode`）删除；② connect schema 的 `command` 字段删除（action=exec 天然覆盖）；③ 救援消息特判删除（统一 schema 无形状差异）；④ 参数清洗收缩为纯装饰键剥离（`action` 已是声明字段）；⑤ 地板豁免与 legacy overlay 救援不删，改由 C2 策略表驱动。预算（`MaxInvocations`）与跨请求 overlay 重放是任何设计都保留的基础设施，不计入补偿层。不变量测试原样转绿；schema gate 基线中 ssh 的 `host`/`password` 穿越条目经单次统一 schema 评审后合并或删除 |
| C2 | **可用性策略声明式化**：把散落的三个特判收敛为每能力的声明式策略，由框架统一执行。策略形状（草案）：<br>`availability_policy: { degraded: miss \| keep \| petitionable, host_absent: publish_connector \| unpublished, budget: N, legacy_overlay: none \| on_fallback }`<br>注意：降级豁免的**保护性投影**（只保留请愿家族与只读家族、丢弃其余变更型标签）是执行器行为而非每能力配置，策略表不表达它——避免每个变更型家族各自声明"丢弃谁"导致组合爆炸 | 现有三个特判（`leftoverKeepsRemoteHostClassification`、地板豁免、`grantLegacySSHPetition`）各自替换为策略表条目，行为由同一执行器驱动；三个旧特判的定向测试原样转绿 |
| C3 | **降级回合表面语义化**：配合分类器域 fail-fast 窗口工作（并行会话已在 `corelib/intent/classifier.go` 推进），评估降级回合是否可携带"continuation of managed work"信号 | 评估报告一份；若做，降级回合的 petition 触发率下降（D1 计数对比） |

### D. 度量

| 项 | 内容 | 验收标准 |
|----|------|---------|
| D1 | `maclaw.log` 增加结构化事件：`ssh_rescue_layer={plan_exemption\|petition_expand\|legacy_overlay}`、`ssh_mode={connect\|exec}`，**以及分母计数 `ssh_turn_total`（分类为 ssh 家族的回合总数）**——只有分子没有分母时，"救援次数下降"无法区分"补偿层被消灭"与"ssh 回合本身消失（回归）"两种相反结论。随版本统计每层触发率 = 各层计数 / ssh_turn_total | 一个发布周期内能产出"各层触发率分布"报表，作为 C1/C2 收益评估基线；报表必须同时展示分子与分母 |
| D2 | 不变量测试矩阵扩展到 B3 确认有缺口的其他能力 | 同 B3 验收 |

## 4. 风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| B3 审计发现更多缺口 | 工作量膨胀、产生"顺手静默修掉"的诱惑 | 计划明确：缺口各自立项，本计划只做审计与测试 |
| C1/C2 重构期间回归 | 二十余个刚稳定的测试被推翻 | 不变量测试是重构的安全网，先迁测试再迁实现；灰度期新旧执行器并行跑、diff 行为 |
| 降级豁免被后续维护者泛化到其他变更型家族 | 安全姿态腐蚀 | 豁免注释已写明"ssh 专属评审例外"；C2 策略表落地前任何泛化必须走安全评审 |
| fail2ban 白名单 IP 变化（动态出口） | 间歇封禁复发 | A1 验收含"连续 3 天"观察；复发时 D1 计数会显示 connect 层失败率回升 |

## 5. 跟踪与责任

| 项 | 责任 | 检查点 |
|----|------|--------|
| A1–A3 | 运维 + 原开发 | A2 合入后一周内 |
| B1–B4 | 语义框架维护者 | 每完成一项在本文件打勾并附链接；全部完成时开一次 30 分钟回顾，核对"是否有迁移绕过清单"。**进度（2026-09-18）：B1 ✅（检查表已交付）、B4 ✅（事件流落地）、B3 ✅（审计完成，见附录 4；L1–L4 立项）、B2 随 B1 交付** |
| C1–C3 | 语义框架维护者 | 框架修订立项时从本表生成子任务，不另行排期 |
| D1–D2 | 语义框架维护者（分别随 B4、B3 完成） | 第一个发布周期出基线报表 |

责任人在首次计划评审会上指定；本表"语义框架维护者"为角色占位。

## 6. 明确的非目标

- **现在不做 C1 大爆炸重构**：它会重开全部安全门评审、推翻二十余个刚稳定的测试，收益仅是补偿层减薄；框架自身修订时再做。
- **不放宽降级姿态的通用性**：地板豁免是 ssh 专属评审例外，B3 审计若发现其他家族需要同等待遇，须逐个走安全评审，不做泛化默认。

## 7. 设计原则：宁慢勿乱

用户原话（2026-09-18）："用户肯定是需要稳定，宁愿慢，也不愿意乱。"

本计划的所有取舍以此为准绳，已落地与待评审的具体化：

1. **已落地（A4）**：tree 分类时限 12s→30s——慢 hub 时宁可首 token 多等 18 秒，不让回合带着猜错的表面开始。
2. **已落地的稳定层**：四层可用性机制 + 只读地板——即使降级真的发生，表面也是确定性的（有保底的工具集），不是随机的。
3. ** tension 记录（与并行会话协商）**：tree 超时后的 10 秒 fail-fast 窗口（`corelib/intent/classifier.go`，并行会话所有）与宁慢勿乱存在方向张力——窗口内其他回合**立即**降级而非等待 30 秒。窗口的 burst 保护价值真实，建议其权衡时以本原则为输入（例如窗口内改为"等待但不上报失败"，或缩短窗口）。
4. **C1/C2 修订时的验收追加**：统一 adapter 与策略表落地后，rescue 事件率（D1）应趋近于零；非零即"还有乱"的信号。

## 附录 1：本次变更集（A2 评审输入）

**规则与预算**（corelib）
- `corelib/agentservice/intent_capability_rules.go` — LabelSSH `MaxInvocations: 8`（IM 表）
- `corelib/agentservice/reviewed_dynamic_capabilities.go` — 同上（reviewed 表，parity 由测试钉住）
- `corelib/agentservice/reviewed_dynamic_capabilities_test.go` — 预算断言更新（8 兄弟 + 全家族能力校验）
- `corelib/agentservice/intent_capability_rules_test.go` — ssh 预算漂移显式 pin
- `corelib/tool/semantic_repeat.go` — 预算耗尽提示去 office 专属文案（能力中立化）

**语义 ssh 适配器与模式**（guiapp）
- `guiapp/semantic_ssh.go` — connect 模式（provider 门控/schema/args 校验）、模式检测、参数清洗（双模式）、exec schema 字段描述、§B4/D1 观测事件源（`logSSHAvailabilityEvent` + 分母去重 `sshAvailabilityCountEligible`）
- `guiapp/semantic_tool_routing.go` — 1750 gate 放行 connect 模式、地板豁免 + 保护性投影、catalog 双模式挂接、turn_total 分母事件
- `guiapp/semantic_tools_search.go` — petition 门放行 connect 模式
- `guiapp/im_agent_loop_shared.go` — 清洗按模式分派接入两条准入路径、connect+command dispatch、`grantLegacySSHPetition` 无表面救援（含 §B4 层标记）、overlay 跨请求重放、救援消息指明 action 形状、petition_expand 救援事件
- `guiapp/coding_durable_dynamic_surface.go` — durable 路径同等清洗（按 schema 模式）
- `guiapp/im_ssh_tools.go` — root 认证失败增加服务器侧核查指引

**报告可见性与凭据防线**（2026-09-18 追加批）
- `guiapp/frontend/src/components/ai/assistantRoundProse.ts`、`guiapp/frontend/src/components/ai/assistantRoundProse.test.ts` — 实质中间正文（≥40 字符）不再被下一轮清空：修复"模型已汇报但用户不可见"（2026-09-18 生产事故）
- `guiapp/tool_registry_builtin.go` — write_file 描述增加"凭据不落盘"安全规则
- `corelib/tool/semantic_renderer.go` — `fs.write.local` 渲染 cue 同步凭据规则
- `guiapp/tools_browser.go`、`guiapp/tools_computer_use.go` — 过时注释修正（"no intent rule"描述，B3 审计发现）

**RunnerUp 信号链与宁慢勿乱批次**（2026-09-18 17:42 事故追加）
- `corelib/intent/classifier.go` — `lookupHintOrUnknownFromL2` 的 collapse 保留 RunnerUp/RunnerUpScore（**与并行会话共用文件**：他们的 fail-fast 窗口 + 本线的 RunnerUp 激活，提交前与对方核对分割）
- `corelib/intent/classifier_fusion_deadline_test.go` — collapse 保留证据的测试（含不泄漏进声明标签的负向）
- `guiapp/semantic_tool_routing.go` —（既有条目追加）`classificationHasSSHSignal`、入口 RunnerUp 提升、豁免消费
- `guiapp/semantic_routing_miss.go` — 路由 miss 只读地板（`routingMissReadOnlyFloor` + `lansengerGroupFloorAllowed` 群边界门控 + `routingMissFloorDefinitions`）
- `guiapp/im_agent_loop_tools.go`、`guiapp/im_agent_loop_tool_augment.go`、`guiapp/im_agent_loop_tool_restore.go` — 三个 leftover 调用点接入地板
- `guiapp/app_embedding.go`、`guiapp/hardware_agent_runtime.go`、`guiapp/im_handler_standalone.go` — §7 宁慢勿乱：tree 分类时限 12s→30s（三处分类器构造）
- `guiapp/semantic_tool_routing_test.go` —（既有条目追加）只读地板正/负测试

**安全门**
- `guiapp/semantic_schema_gate_test.go` — connect schema 的 host/password 穿越注册 + 债务上限 14→15

**测试**
- `guiapp/semantic_ssh_connect_test.go`（新增）— 8 个用例 + 四场景不变量矩阵 `TestSSHAvailabilityIndependentOfClassifierAndSession`
- `guiapp/semantic_invocation_wash_test.go` — exec/connect 双模式清洗用例
- `guiapp/coding_durable_dynamic_surface_test.go` — durable 路径清洗集成
- `guiapp/semantic_baseline_workspace_test.go`、`semantic_capability_families_test.go`、`semantic_capability_families_s2b2_test.go`、`semantic_remaining_families_test.go` — 4 个旧行为钉桩翻转为 connect 表面断言

**顺带修复**（同一批构建带入）
- `guiapp/im_history_persistence.go` — post-conversation panic 日志增加堆栈

## 附录 2：工作区中必须排除的非本线改动（A2 提交时勿混入）

工作区存在并行会话的在途改动，截至 2026-09-18 包括（清单会随时间增长，提交前重新核对）：

- `corelib/intent/classifier.go` + `classifier_fusion_deadline_test.go` — 分类器融合超时 fail-fast（并行会话）
- `corelib/agent/loop.go` — 共享 LLM HTTP 传输
- `corelib/llm/client.go`、`openai_sdk.go`、`request_retry_guard.go` — LLM 传输/重试
- `corelib/tool/hybrid.go` — 工具嵌入磁盘缓存并发修复
- `guiapp/llm_request_helper.go`、`hub_http_client.go` — LLM 请求助手
- `hub/internal/httpapi/llm_usage_reports.go` — hub 用量报表（并行会话）
- `guiapp/frontend/src/App.css`、`components/layout/SidebarNavRail*`、`components/layout/AppSidebarShell.tsx` — 前端侧栏（并行会话；注意 foreign 文件集随时间增删，提交前必须重新核对）
- `hubcenter/internal/llmservice/*` — hubcenter 分类头
- `build_number`、`guiapp/frontend/src/version.ts`、`guiapp/vscode_ext_asset/*.vsix`、`guiapp/frontend/package.json.md5`、`go.sum` — 构建产物与依赖变动

**提交方法**：`git add` 逐文件指定附录 1 的 35 个文件（含新增的 `guiapp/semantic_ssh_connect_test.go`）+ 本计划文档 + `docs/design/managed-surface-migration-checklist-zh.md`；提交后 `git show --stat HEAD` 必须与附录 1 完全一致。（文件数随追加批次更新，v12 起为 35；`corelib/intent/classifier.go` 与并行会话共用，须与其核对分割后再提交；提交前以 `git status` 实查为准。）

## 附录 3：评审验证命令（A2 评审者照此执行）

```bash
# 1. 不变量矩阵 + ssh 全部用例（核心，必须通过）
cd guiapp && go test . -count=1 -vet=off -run \
  "TestSSHAvailabilityIndependentOfClassifierAndSession|TestSemanticSSH|TestSemanticToolCallPetitionRescuesDegradedTurnSSH|TestIMSemantic|TestSemanticToolsSearchPetitionsUnboundSSHForConnect|TestSemanticS2b2CatalogOnlyFamiliesStayUnmanaged|TestSemanticExternalEffectMixedRequestPlansConnectSurface|TestLeftover|TestUnbound|Wash"

# 2. 安全门（host/password 穿越注册与债务上限）
go test . -count=1 -vet=off -run "TestManagedSchemaGate|TestManagedCallSurface"

# 3. 规则表 parity（IM/reviewed 预算一致性）
cd ../corelib && go test ./agentservice/ -count=1 \
  -run "TestIMAndReviewedIntentRulesShareRepeatBudgets|TestReviewedDynamicIntentRulesResolveSSHWithoutLocalShell"

# 4. 全量回归（两个包）
cd ../guiapp && go test . -count=1 -vet=off -timeout 2400s
cd ../corelib && go test ./agentservice/ ./tool/ ./agent/ -count=1

# 5. 前端单测（追加批的报告可见性修复，2026-09-18 后新增）
cd ../guiapp/frontend && npx vitest run src/components/ai/assistantRoundProse.test.ts

# 6. 构建产物（可选，验证 wails 无关、纯 go build 路径）
cd ../.. && powershell -NoProfile -ExecutionPolicy Bypass -File rebuild_windows_gui.ps1
```

说明：guiapp 全量套件存在两个已确认的预先存在失败（`TestSemanticDynamicBindingStale...` 偶发、`TestGUIAdvertisedNonCoreTools...` 在干净树同样失败），评审时以"未修改代码树上复现相同失败"为准，不作为本变更集的阻塞项。

## 附录 4：B3 受管家族退路审计结果（2026-09-18 执行）

审计方法：按 B2 四场景矩阵逐家族核查，证据到 file:line；只审计不修复（缺口立项）。

### P0 宿主绑定家族

**document.write.office — ✅ 健康（无缺口）**
- ① 降级可达：变更型家族中唯一有正式通道——降级 office 提示 ≥0.70 经 `semanticOfficeGovernedHint` 规划（`semantic_tool_routing.go:616-621`），有 `TestSemanticDegradedOfficeHint*` 三测试钉住
- ② 无发布门控（无条件 `ready:true`）；宿主状态在执行期 fail-closed
- ③ petition 扩展替换 surface 为 child revision，每请求重渲染，授予存活；无 overlay（设计内）
- ④ 预算 8，两表一致
- 场景覆盖：S1/S2/S4 有测试；S3 表面无 legacy 等价物标 N/A（设计内）

**business.data.mis / database — ⚠️ 4 项立项**
- L1（立项）：降级回合 business.data.mis 无任何可达路径也无测试——若是刻意的 fail-closed 姿态，需要一个像 `TestSemanticDegradedOfficeHintPlansBelowResolverFloor` 的反向钉桩测试
- L2（立项）：mis_data/database 缺 leftover 保留的专属测试（office 有 `TestRoutingMissLeftoverDropsPrivilegeAndGovernedGenerate` 同款）
- L3（立项）：mis_data 的托管 petition 只有 label 解析级测试，缺 `PetitionToolCall("mis_data")` 端到端授予测试
- L4（信息项）：`business.data.mis` 未声明 MaxInvocations（同族 read 腿与 office 均为 8），变更型回合调用无上限——需一次评审确认是否有意

**browser.control.web / computer.control.desktop — ✅ 姿态确认为刻意（无违规）**
- 无宿主运行时 → HostReject(unmet)，`TestIMSemanticSSHBrowserCURequireBoundRuntime` 钉住；CU 的 legacy 侧还有 `computerUseIntentActivated` 降级 fail-closed 双保险
- 降级无豁免是评审过的刻意姿态（`semantic_tool_routing.go:1795-1799` 注释明示 ssh 是唯一例外）
- 附注（已修，v8）：`tools_browser.go`/`tools_computer_use.go` 中"no intent rule maps LabelBrowser/LabelComputerUse"注释已过时（规则早已存在），按代码现状重写
- 残余观察：browser 的 legacy 侧无 CU 那样的降级 fail-closed 门，也无测试钉住——低风险（leftover 是迁移前旧表面），若在意可并入 L2 立项

### P1 有状态本地家族 — ✅ 全部 OK

- shell.execute.local：降级不可规划是刻意姿态（变更型，注释明示）；工作区绑定有供给回退链；预算 8；petitionable
- fs.write.local：同构 OK；附注：未声明 MaxInvocations（与 fs.read 相同的非对称，随 L4 一并评审）

### P2 无状态家族 — ✅ 全部 OK（仅过检查表四问）

- search(5)/fetch(5)/download(3) 有预算且降级经只读通道规划；fs.read 无预算（随 L4 评审）
- 审计澄清一处上下文：petition 全名可达（`semanticPetitionableCapabilities`）对所有家族适用；仅"无托管表面"的救援限定 SQL 名 + ssh

### 审计结论

ssh 是当前体系中唯一需要五层补偿的能力，其余家族要么姿态刻意（browser/CU）、要么通道完备（office/P1/P2）。真正的后续工作是 **L1–L4 四个立项**（mis 家族测试覆盖 + 预算非对称评审），均不涉及行为变更。
