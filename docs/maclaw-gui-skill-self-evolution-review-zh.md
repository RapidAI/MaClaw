# MaClaw GUI Skill 自进化逻辑评审与优化方案

> 范围：`gui/`、`tui/`、`corelib/skill/`、`corelib/tool/`、`corelib/agentservice/` 与 GUI/TUI Skill 管理入口。评审基线：2026-08-30；文档修订：2026-09-01（完整性 manifest 路径/类型/清单闭合校验、GUI ZIP 导入 RecoveryScope 隔离、回滚阶段错误诊断、reviewed-draft 提交并发边界与 cleanup 恢复矩阵、detached executor fail-closed、重命名别名解析证据同步）。本文区分当前实现、目标设计和上线前优化项；“已落地”只表示代码和回归测试已覆盖，不等于所有写盘路径都具备同等级事务保证。

> 本轮实现进展：核心 pipeline、GUI 创建/更新、GUI repair、reviewed-draft apply，以及 staged 激活已实际调用共享 `SkillCommitter`；reviewed-draft disable/reject、maintenance、重命名和删除也已接入共享提交器的主要事务边界。ClawHub/GitHub mixed-install 的 config-only 注册、managed capability external/Hub 的新目录安装、能力缺口 Hub 新目录安装、能力缺口 GitHub 配置注册、IM install-only 和 IM tool 的 config-only/新目录/已有版本路径均已迁移到共享提交器；AgentService 的 GitHub/Hub/Market/ZIP 导入现已通过共享目录事务提交，使用 `.prev`、checked 目录验证、最终审计和提交后清理；AgentService `DeleteSkill` 也使用隔离删除事务，并由 `NewService` 在重启时恢复删除隔离目录；传统 GUI `App.AddSkill` 已增加 `legacy_gui_skill_add` durable compensation、checked index、最终审计、**审计后先持久化 `committed + cleanup_status=pending` 再清理**，并在启动/操作前 fail-closed 恢复；`App.DeleteSkill` 已增加同等的 checked index 发布与回滚恢复；`App.InstallSkill` 现已在目录/settings mutation 前创建唯一 `legacy_gui_skill_install` 外层 durable 记录，**默认 marketplace 注册也在该记录的 settings 快照范围内**，在最终审计后先标记 committed 再清理，但仍未达到共享 `SkillCommitter` 的统一提交器/结构化 API 语义；`ImportNLSkillZip` 现以同一 `installMutex` 串行化传统导入，并在新导入前按 `import` action 前缀执行 durable 补偿恢复，批量记录保留 `AffectedSkills` 与最终审计类型，避免跨重启后只恢复首个 Skill。TUI `manage_skill install/uninstall` 已增加 staging/quarantine、共享补偿记录和严格最终审计，TUI patch 仍采用独立 CAS 事务；GUI 状态/上传元数据与 legacy registry writer 也统一受 `installMutex` 保护。补偿记录新增 `transaction_state` 与 `cleanup_status`，跨重启恢复会区分“已提交但清理待办”与“需要回滚”，避免误恢复已审计版本；BM25 路由已支持可替换的 checked index provider，provider 故障会向提交器返回错误。不能据此宣称 GUI/TUI 全入口已迁移。

> **使用方式**：开发实现以“当前实现”列为准，验收以“必须/不变量/量化门槛”为准。若本文与代码行为不一致，应先补测试和审计证据，再更新文档，不能用文档替代安全门禁。

> **本次文档优化重点**：统一“外部目录发布”和“Skill 配置提交”的先后语义；明确 `committed`、运行状态和提交后清理是三条独立状态轴；把“共享提交器已覆盖”与“入口仍有局部编排”分开描述；补充生产实例不得省略最终审计/checked 索引的 fail-closed 约束。以下目标模板不表示代码已经暴露同名公共 API。

> **并发边界修订（2026-09-01）**：目录型 `commitStagedSkillInstall*` 与 config-only `commitNLSkillDefinitionAfterAdmission` 现在在 App 适配边界统一获取 `installMutex`；mixed config-only 安装和 capability-gap GitHub 注册也复用该 config-only 适配器。EvolutionPipeline 通过 `MutationMutex + MutationAdmission` 将 repair/optimize 的完整 config→YAML→index→audit 提交包在同一锁内，并在锁后重验队列；运行统计、备份恢复和 legacy registry writer 也遵循 `installMutex → skillListMutateMu` 顺序。这样 Wails、IM、能力市场、后台更新和 legacy metadata 写入不会同时持有过期 registry 快照或竞争 `.prev` 目录。该锁只保护最终本地提交阶段，网络下载和安全扫描仍在锁外执行；因此它是进程内串行化，不替代跨重启 durable compensation，也不代表传统 GUI ZIP/插件路径已迁移到共享提交器。

> **精简口径（2026-09-01）**：本文后续若出现旧版“多个入口已统一”与当前入口矩阵不一致，以第 3.5、10.2.2 和 14.2 的逐入口证据为准。新目录安装与已有版本更新是两种不同风险等级；前者已迁移的事实不能外推到后者。传统 GUI ZIP/插件安装仍未统一；AgentService 导入已具备 `NewService` 启动恢复、`RecoveryScope`/目录路径归属校验、跨重启清理、清理重试耗尽后的 `needs_review` 保留、不同 `DataRoot` 隔离，以及多包最终审计失败、external contract 部分恢复失败的定向回归。剩余重点是运行/上传门禁、多租户和其它入口级异常矩阵。GUI 与 IM 的 config-only maintenance 已接入 no-op 短路和结构化状态输出，但 IM 文件型契约与批量边缘分支仍需完整故障注入。所有自动安装、自动定义写盘和自动上传均为显式 opt-in；缺失或不可读配置必须 fail-closed，直到 P0 退出条件全部满足。

> **本轮代码复核证据（2026-09-01）**：已通过提交器/补偿、Hub 更新、managed Hub 回滚、安装与能力市场定向组合测试；还覆盖了 capability-gap、IM 安装、AgentService 导入/启动恢复和 legacy ZIP 路径解析相关定向测试。传统 GUI 已新增 `InstallSkillDetailed`、`AddSkillDetailed`、`DeleteSkillDetailed` 结构化结果视图，并通过 settings 写入失败回滚测试；安装和删除管理页已切换到详细结果并严格检查 `committed + cleanup_status=clear`；兼容 Wails API 仍保留 error-only 签名。共享 `SkillCommitter` 现对候选与权威配置完全一致的变更返回 `state=skipped`/`failure_reason=no_change`，不创建补偿记录、不写 YAML、不刷新索引或最终审计；GUI config-only maintenance 也复用该短路。新增 committed cleanup 重试耗尽后的 `needs_review` 保留/阻断测试，确认不回滚已审计目录且不再自动重试；managed Hub 回滚增加 Windows 竞态后的目录/`.prev` 后置条件验证；文件快照恢复增加 post-image 摘要围栏，并新增恢复前对 YAML/draft/文件快照/目录目标的全量只读预检，避免损坏补偿先改目录后失败；本地 MCP 配置提交后使用可取消的 checked runtime sync，启动/工具发现失败会显式返回并保持 fail-closed；启动期间本地 MCP 的失败/成功发现也会对 marketplace-managed Stdio 记录或更新同一 durable runtime 状态（成功写入 `ready` marker），但不会为手工本地服务器制造全局阻断；配置更新会保留阻断并重新触发严格探测，缺失状态的 managed entry 也默认显示/执行阻断，避免新配置或崩溃窗口沿用旧健康状态；严格 MCP 探测进一步校验 `initialize`/`tools/list` 的 JSON-RPC `result` 与 `tools` 数组，拒绝 HTTP 200 但携带协议级 `error`、缺失 `result` 或缺失/错误类型 `tools` 的假成功。新增 managed Stdio 启动失败持久化回归，确认后台启动也不会丢失 runtime 阻断；**新增启动恢复协调器，对跨重启且已到退避时间的 managed MCP 逐实例执行独立 15 秒 checked probe，成功写入 `ready`，失败继续递增 durable blocker，互不阻塞其它实例**。本轮还将传统 GUI `InstallDefaultMarketplace` 纳入独立 settings durable 事务，并增加写入失败、最终审计失败回滚与重复调用 no-op 证据；新增 `InstallSkill` marketplace+插件 settings 联合回滚回归，证明默认 marketplace 写入不会在外层安装审计失败后遗留。**新增 GUI 补偿人工处置闭环：管理页只展示脱敏摘要，retry/clear 必须 request ID + Skill + action 精确匹配并二次确认；retry/clear 分别写结构化 `compensation_manual_retry`/`compensation_manual_clear` 审计，clear 仅允许 committed 且清理目标已验证的记录；模型和只读命令仍无恢复权限。**另修复启动恢复对本地 MCP 失败状态重复递增 attempts 及过期目标列表覆盖终态 ready 的问题，只有 manager 实际确认运行的实例才会写入 ready。具体命令见第 14.2；这些结果只证明列出的提交器、回滚和安装边界，不代表 maintenance 其它分支、传统 GUI ZIP/插件完整事务或所有导入更新入口已达到同等级覆盖。

> **补充安全修复（2026-08-31）**：runtime 状态写盘失败时，进程内会记录按状态文件路径隔离的 persistence-failure fence；即使旧文件仍标记 `ready`，执行、工具发现和 AutoStart 也立即 fail-closed，直到下一次状态成功持久化或配置 revision 显式重置。

> **本轮补充（2026-09-01）**：RemoteSession 成功退出后异步触发的 ExperienceExtractor 不再经 `SkillExecutor.Register/Update` 直接写入配置。每次 register/update 都重新读取 `skill_evolution_enabled` 并检查 `MACLAW_DISABLE_SKILL_EVOLUTION`；缺失、读取失败或 kill switch 均拒绝写盘。获显式授权的低风险候选会标记为 `agent_created`，经专用非交互扫描后进入共享 `SkillCommitter`，使用 `automatic_experience` 审计来源、补偿、checked index、最终审计和回滚；这只收敛该后台入口，不改变自动写盘整体仍默认关闭的发布结论。

## 文档状态卡（先于正文阅读）

| 项目 | 当前结论 |
|---|---|
| 文档用途 | 评审、发布准入和故障处置依据；不是 API 设计草案的自动实现声明 |
| 当前发布模式 | 只读观察、生成草案、人工审批；自动写盘/自动安装保持关闭 |
| 可依赖的统一能力 | 核心 pipeline、GUI 创建/更新/repair、reviewed-draft、staged 激活、重命名、`DeleteNLSkill`，已列明的 Skill 新目录安装、AgentService 导入主路径，以及能力市场 MCP 配置的基础 durable 事务边界 |
| 仍属上线阻断 | maintenance 边缘分支、已有版本更新、传统 GUI ZIP/插件安装、能力市场 MCP 运行时同步与完整故障注入、部分 GitHub/IM 更新（IM `toolPatchSkill` text/structured 主路径已统一，但其它更新分支仍待故障注入）、批次/多租户/清理故障注入；MCP runtime sync 失败虽已持久化并由启动协调器按退避重试，现已提供受控人工 retry/reset 与 UI 入口，仍需远程指标和完整故障矩阵 |
| 结果判定 | 只有 `state=committed && cleanup_status=clear` 才是可对外完成；`skipped` 是零副作用幂等终态，不等于首次提交 |
| 冲突处理 | 若正文不同章节结论冲突，以第 3.5（入口矩阵）、第 10.2.2（准入矩阵）和第 14.2（测试证据）为准；其它段落只作解释 |

> **入口名限定**：下文单独写“GitHub 导入/更新”而未注明 AgentService 时，均指传统 GUI 或其它 legacy adapter；AgentService 的 GitHub/Hub/Market/ZIP 导入以第 3.5、10.2.2 和 14.2 的“已接入共享目录事务”结论为准。
> 能力缺口路径中的 GitHub **配置注册/新目录安装** 已迁移；P0 所称“能力缺口 GitHub 导入”仅指已有版本更新、非桌面写盘或未覆盖的 legacy 分支。

### 一页式决策树

```text
是否会改变 Skill 定义、目录、状态、索引或外部 contract？
 ├─ 否 → 允许只读观察/经验采集
 └─ 是 → 是否有人审、来源、Schema、扫描、Gate 和审计可用性证据？
          ├─ 否 → rejected/unverified，禁止写盘
          └─ 是 → 是否已有待恢复补偿、队列异常或目录/.prev 冲突？
                   ├─ 是 → blocked，先恢复或人工处置
                   └─ 否 → 走对应提交器/受控 legacy adapter
                            ├─ skipped → 零写盘，结束
                            ├─ committed + clear → 可进入后续准入检查
                            └─ 其它结果 → 保留补偿，禁止执行/上传/自动重试
```

## 一页决策摘要

当前版本的正确发布结论只有一句：**允许观察、生成草案和人工审批；禁止把未完成统一事务迁移的入口恢复为自动写盘。**

对任意 Skill，执行、上传和下一次自动写盘必须同时满足以下准入条件：

```text
admit(skill) =
  runtime_status == active
  && latest_committed_transaction_state(skill) == committed
  && latest_cleanup_status(skill) == clear
  && compensation_queue_healthy(skill)
  && audit_available
  && index_digest == definition_digest
  && dependency_ready_if_declared
  && no cancellation/timeout/shutdown
```

这里的 `latest_committed_transaction_state` 指最近一次真正产生变更且已提交的事务；`skipped/no_change` 或 `skipped/already_current` 是零副作用结果，不得覆盖该提交指针，也不得单独授予执行或上传权限。已有 Skill 若此前存在健康的 `committed + cleanup_status=clear` 提交，no-op 之后继续沿用该提交；首次导入若没有可证明的提交，仍按 fail-closed 处理。`latest_cleanup_status` 指该 Skill 最近一次真正变更事务的清理结果，不要求把事务字段复制进 YAML。依赖未声明时 `dependency_ready_if_declared` 为真；声明了运行时依赖时必须单独检查依赖任务状态。

任一条件无法证明即拒绝（fail-closed），而不是根据“最近一次日志成功”推断放行。`compensation_queue_healthy(skill)` 至少检查全局队列可读，并确认该 Skill 没有 `audit_pending`、`needs_review` 或 `cleanup_status!=clear` 的记录；若队列损坏或无法解析，则全局阻断。`committed` 是业务提交结果，不等于 `active`；`skipped` 只是幂等短路，也不产生新的授权；`cleanup_status=clear` 也不等于已获得激活授权。

本文后续每个“已落地”结论都应能对应到代码路径和自动化断言；只有设计文字、手工验证或日志输出时，最高只能标记为“部分落地”。

## 目录

- [文档状态卡](#文档状态卡先于正文阅读)
- [一页决策摘要](#一页决策摘要)
- [阅读约定](#阅读约定)
- [结论](#1-结论)
- [组件职责](#2-组件职责)
- [当前工作原理](#3-当前工作原理)
- [状态模型与激活门禁](#4-状态模型与激活门禁)
- [统一决策协议](#5-统一决策协议)
- [Schema、扫描与 Gate](#6-schema扫描与-gate)
- [配置与治理开关](#7-配置与治理开关)
- [风险与失败语义](#8-风险与失败语义)
- [队列、取消、超时与重试](#9-队列取消超时与重试)
- [写盘事务与回滚](#10-写盘事务与回滚)
- [补偿记录生命周期](#1021-补偿记录生命周期)
- [外部快照契约](#外部快照契约新增约束)
- [审计与可观测性](#11-审计与可观测性)
- [重启、扫描与配置 overlay 一致性](#12-重启扫描与配置-overlay-一致性)
- [非 Bash mock/replay 边界](#13-非-bash-mockreplay-边界)
- [测试与验收](#14-测试与验收)
- [优先级与运行处置](#15-优先级与运行处置)
- [不可变安全不变量](#16-不可变安全不变量)
- [版本化实施清单](#17-版本化实施清单)
- [文档维护与变更记录](#18-文档维护与变更记录)

## 阅读约定

文中使用以下标记，防止“设计要求”和“当前实现”混淆：

| 标记 | 含义 |
|---|---|
| **已落地** | 代码和回归测试已经覆盖，允许作为当前行为依赖 |
| **部分落地** | 主流程可用，但仍存在边界缺口；不得据此宣称完整合规 |
| **待实现** | 设计约束或验收项，当前版本不能默认具备 |
| **上线阻断** | 未完成前应关闭对应自动写盘入口，保留只读观察和人工操作 |

本文的“必须”表示安全不变量；“建议”表示可在不改变安全边界的前提下排期优化。

### 术语与判定口径

| 术语 | 严格含义 |
|---|---|
| 权威定义 | `skill.yaml`/`skill.yml` 与持久化配置中的 Skill 定义；索引和 scan cache 都是可重建派生数据 |
| 提交（commit） | 配置/目录、内存、checked index 和最终审计均成功，并已持久化 `transaction_state=committed` |
| 清理（cleanup） | 删除 staging、旧备份、draft 和补偿记录等提交后产物；不改变已审计业务结果 |
| 补偿（compensation） | 可跨进程恢复的快照与操作意图，不是普通错误日志 |
| 自动写盘 | 无人工确认、由 pipeline/worker/重试任务直接改变 Skill 定义或状态 |
| 灰度 | 对指定入口、指定 Skill 集合和限定比例开启，并可独立关闭；不等同于全局开启 |

除非特别注明，文中的“成功”均指 `state=committed && cleanup_status=clear`；“事务提交成功但清理未完成”必须写作“已提交/清理待办”，不能简称为成功。

### 快速判断（先看这里）

| 问题 | 当前结论 |
|---|---|
| 哪些 Skill 可以普通执行？ | 只有 YAML 权威状态为 `active`、索引与摘要一致、且不存在待恢复补偿的 Skill。|
| LLM 能否直接修改 Skill？ | 不能。LLM 只生成候选；Schema、扫描、Gate、策略和审批决定候选是否可提交。|
| 什么时候允许上传？ | 仅本次确有变更且提交器返回 `committed`、最终审计成功、补偿队列健康且 `cleanup_status=clear`；`skipped` 不触发新上传，也不覆盖既有提交状态；`rolled_back`、`audit_pending`、`needs_review`、`cleanup_pending` 一律禁止上传。|
| 当前是否适合开启自动写盘？ | 不适合。核心 pipeline、创建/更新、reviewed-draft、staged 激活及多个新目录安装分支已切换到共享 `SkillCommitter`，但 maintenance 边缘分支、已有版本更新、传统 GUI ZIP/插件路径和入口级故障注入仍未完成；默认保持观察/草案/人工审批模式。|

## 1. 结论

> **实现状态修订（2026-08-30）**：直接 `InstallHubSkill`、mixed-install 配置注册、managed capability external/Hub 新目录安装、能力缺口 Hub 新目录与 GitHub 配置注册、IM SkillMarket/Hub 新目录安装及 IM tool 的 config-only/目录安装（含已有版本替换）已接入共享 `SkillCommitter`/`commitStagedSkillInstall`。这些迁移不覆盖传统 GUI ZIP/插件及其它 legacy adapter；相关入口仍必须按人工/灰度和 fail-closed 规则处理，自动安装整体继续关闭。

### 1.1 证据等级与文档判定规则

本评审把“代码存在”与“能力可发布”分开判定。一个能力只有同时满足以下三项，才能标记为“已落地”：

1. 存在唯一生产调用路径，而不是仅有测试辅助函数或备用实现；
2. 失败、取消、重启至少有一条自动化断言；
3. 结果进入结构化审计，并能影响执行/上传准入。

仅有接口、注释、日志、手工演练或“理论上可回滚”，统一标记为“部分落地”。本文所称“统一提交器”目前指 `SkillCommitter.Commit` 内部执行的阶段化流程；`prepare`、`commit`、`rollback`、`cleanup` 尚未作为可独立调用的公共 API 暴露，不能把文中的模板误读为已完成的四个独立服务。

MaClaw Skill 自进化是基于执行证据的配置维护，不是模型参数自动训练。闭环为：

```text
请求 → 路由/策略 → 受控执行 → 记录结果与参数摘要 → 归因
→ repair/optimize/promote/install 决策 → LLM 候选
→ Schema → 安全扫描 → Gate → 审批 → 备份/原子写盘
→ 索引刷新 → 最终审计 → 成功确认或回滚 → 经验回流
```

**已落地的安全基线（不等于 P0 全部关闭）**：高风险安装无确认时拒绝；无效 YAML 不注册；Gate 为 `passed/failed/unverified` 三态；未知错误不自动修复；核心自动发现若被其它受控调用方使用只能产出隔离的 `staged` 候选，GUI 当前完全禁止 Nudge 自动提升；普通状态 API 不能绕过激活门禁；审计健康、失败聚合、队列合并、同 Skill 串行和跨 Skill 并行可用；repair context 已贯通 GUI LLM、Gate、扫描、写盘和上传复核的取消链路；失败尝试在 pipeline、GUI 和 reviewed-draft 路径统一计数，达到 3 次自动转 `needs_review`。这些能力只能说明“已具备安全护栏”，不能替代统一提交器、真实可失败索引 provider 和补偿运维闭环。

**当前版本的发布判断**：核心 pipeline、GUI `CreateNLSkill`/`UpdateNLSkill`、GUI repair、reviewed-draft、staged 激活、重命名和 `DeleteNLSkill` 已通过共享 `SkillCommitter` 获得统一结果协议与持久化补偿；直接 Hub 安装和 `UpdateHubSkill` 的已有目录替换同样复用 `commitStagedSkillInstallWithExisting`，保留 `.prev`、checked index 与最终审计；AgentService 的 GitHub/Hub/SkillMarket/ZIP 导入也已经进入共享目录事务。传统 GUI `App.AddSkill`/`App.DeleteSkill` 已具备独立 durable compensation、checked index 刷新与最终审计；`App.InstallSkill` 现已在外层建立唯一 durable 恢复记录、登记目录路径、纳入 checked index 刷新并在最终审计后先标记 committed 再清理。其内部仍由 `addSkillLockedWithRecord` 完成 metadata/package 编排，但复用外层记录且不再产生嵌套 durable 记录或中间“已安装”审计；这属于“单一恢复记录、局部编排”，不是共享 `SkillCommitter` 语义，返回值也未统一为共享提交结果。能力市场 MCP 配置安装现已通过配置型 durable 事务完成文件快照、严格预审计/最终审计、失败恢复、no-op 短路和提交后清理状态持久化；它没有 Skill 目录索引步骤，运行时同步现已通过可取消的 checked 边界贯穿进程启动、initialize 与 tools/list，失败会显式返回但不反向回滚已提交配置。审计与 operator summary 不暴露 endpoint、AuthSecret 或 headers；恢复失败、远程同步持久化阻断和跨重启异常矩阵尚未补齐，不能据此放开自动安装。maintenance 的部分分支、除 Hub 更新外的剩余更新/导入仍存在独立编排。因而自动改写/安装入口仍按“上线阻断”处理，默认只允许观察、生成草案和人工审批；只有 S1 退出条件全部满足后，才可逐步恢复自动写盘。这里的“上线阻断”按入口执行，不影响只读观察、审计查询和人工审批。

**仍需限制性解读的能力**：worker timeout 已配置化并可在 GUI 编辑（默认 180 秒，范围 30–1800 秒），取消、超时和 shutdown 已产生结构化事件；staged 重启/扫描、overlay 防提升和 GUI request-level 任务列表已补充。核心 pipeline、创建/更新、GUI repair、reviewed-draft、重命名、删除和 staged 激活已将主要 config/YAML/index/最终审计步骤纳入共享提交器；直接 Hub 更新也走共享目录安装提交器，但 maintenance 边缘分支、其它已有版本更新、传统 GUI GitHub/ZIP/插件导入和其它 legacy adapter 仍未完全统一，补偿恢复事件以及损坏队列/运行时 blocker 的人工处置仍不完整。人工路径已改为使用策略摘要生成的 `config_revision`，不再使用固定 `manual` 占位值。能力缺口/IM 的非桌面实例仍缺少同等级持久配置快照，且自动安装继续关闭。非 Bash replay 目前已有显式接口和边界测试，真实生产隔离 adapter 尚未实现。统一提交器、真实可失败索引 provider 和补偿运维闭环完成前，自动改写入口应按“上线阻断”处置。

### 当前实现与目标的差异矩阵

| 能力 | 当前状态 | 主要限制 | 临时处置 |
|---|---|---|---|
| 自动 repair / optimize | **部分落地（主路径已补偿）** | file-backed repair 只生成 draft；pipeline 的内存型 repair/optimize、GUI repair 和 reviewed-draft apply 已对 config、YAML、索引和最终审计做失败补偿；maintenance 仍有边缘分支未完全统一 | 保留冷却；统一提交器验收前关闭自动写盘 |
| `staged → active` | **部分落地（共享提交器 + 入口门禁）** | 必须走 `VerifyAndActivateNLSkill`；普通状态 API 不能绕过；提交器已覆盖主事务，但真实索引故障注入和跨重启验收仍不足 | 失败保持 `staged`；回滚不完整进入补偿；补偿清理失败继续阻断 |
| 取消 / timeout | **已落地（核心 pipeline + GUI repair）** | request、attempt、termination、failure_reason 已进入 pipeline 事件；GUI repair 使用 worker context 并在写盘前后检查取消 | 取消后禁止 Apply；写盘前再次检查 context |
| strict 审计补偿 | **部分落地** | 核心 pipeline、创建/更新、GUI repair、reviewed-draft、staged 激活以及重命名/删除主路径已通过 `SkillCommitter` 纳入最终审计与 durable compensation；直接 Hub 安装及 `UpdateHubSkill` 的目录替换、AgentService 的 GitHub/Hub/SkillMarket/ZIP 导入亦已迁移到共享目录事务，并在 `NewService` 启动阶段按 action prefix 恢复。maintenance 边缘分支、其它已有版本更新和传统 GUI 安装仍是多套编排，字段校验、批次故障注入和多租户恢复尚未完全统一 | 审计不可用、队列不可读、恢复失败或存在待恢复记录时 fail-closed |
| 非 Bash replay | **部分落地** | 已有 `NonBashReplayAdapter` 契约和 mock 边界；尚无真实隔离生产 adapter，无 adapter 时只能 `unverified` | 禁止将 mock 结果当真实通过 |
| GUI 任务列表 | **已落地（基础版）** | 展示 pending/running、request ID 和取消；终态历史仍依赖审计列表；补偿摘要支持按 request ID + Skill + action 精确人工 retry/clear，快照正文仍不可见 | 增加终态结果、取消确认和失败原因过滤 |

## 2. 组件职责

| 组件 | 文件 | 职责 |
|---|---|---|
| SkillRunner | `gui/skill_runner.go` | 执行、统计、经验回流 |
| Evolution wiring | `gui/app.go` | 组装进化组件、配置和 GUI 回调 |
| CapabilityGapDetector | `gui/capability_gap_detector.go` | 缺口检测、搜索、风险判断、安装 |
| EvolutionPipeline | `corelib/skill/evolution_pipeline.go` | 通知合并、队列、并发、取消 |
| SkillCommitter | `corelib/skill/skill_committer.go` | 共享 durable 提交边界；统一补偿快照、配置/YAML、checked index、最终审计及提交后清理结果 |
| AgentService 导入 | `corelib/agentservice/skills.go` | GitHub/Hub/SkillMarket/ZIP 导入通过共享 `SkillCommitter` 执行 staging、`.prev`、checked 目录校验、最终审计和提交后清理；AgentService 注册以文件扫描为权威来源 |
| SelfRepair/Optimizer | `corelib/skill/self_repair.go`、`optimizer.go` | 生成修复与受限优化候选 |
| RepairGate | `corelib/skill/repair_gate.go` | 沙箱重放和证据判定 |
| NudgePromoter | `corelib/skill/nudge_promoter.go` | 核心工具序列候选提升能力；因其历史直写路径，GUI 当前未接入 |
| Audit/Usage | `corelib/skill/evolution_audit.go`、`corelib/tool/usage_tracker.go` | 审计和运行证据 |
| GUI 控制面 | `gui/frontend/src/components/remote/SkillsManagementPanelView.tsx` | 开关、审批、状态、取消、回滚 |

## 3. 当前工作原理

### 3.1 异步反馈管道

Runner 在 Skill 结束后记录成功/失败、错误类别、实际参数摘要和经验；调用方不等待进化任务。Pipeline 默认延迟 5 秒，同一 Skill 的待处理通知合并为最新请求；同一 Skill 始终串行，不同 Skill 可并行，全局上限由 `skill_evolution_max_concurrent_workers` 控制（默认 2，限制 1–16）。任务按“失败记录 → repair → optimize → promote”处理，每个任务有独立 context。

取消只影响进化任务，不影响用户当前 Skill 执行。状态 API 已暴露 pending/active、队列年龄、失败摘要、取消数和超时数；GUI 已订阅取消/超时事件并展示 request-level 任务。pipeline 事件统一写入 `request_id`、`attempt`、`config_revision`、`schema_version=2`，取消、deadline 和 shutdown 分别使用 `operator_cancelled`、`worker_timeout`、`shutdown` 终止原因。非-pipeline 人工维护动作已开始写入同类字段，但仍需统一校验和回归断言。

### 3.2 变更边界

- 普通内存型 Skill：满足可归因错误、用量阈值、冷却时间和最大尝试次数后才 repair；Gate 非 `passed` 不得自动写盘。
- file-backed Skill：自动路径只生成 `.evolution-drafts/*.json`，由 GUI 人审后应用，不直接覆盖 YAML。
- optimize 仅修改白名单字段（步骤参数、`on_error`、描述等）；应用前备份，应用后重新扫描/验证。
- high/critical 安装在确认缺失、扫描失败、审计不可用或策略不明确时 fail-closed。
- 任何上传入口（自动上传、手动上传、队列重试）在对应 Skill 存在待恢复补偿、补偿队列不可读或 schema 不支持时 fail-closed；补偿恢复完成前不得提交远端。AgentService 的运行与上传路径还必须按 `RecoveryScope=dataRoot` 做服务/租户隔离检查；scope 缺失、路径越界或无法证明归属时一律阻断，不能只依赖记录中的 scope 字段。
- 安装、激活、合并、退役、发布必须统一记录动作、原因、风险、证据摘要、Gate 状态、备份版本和人工复核要求。

### 3.3 Nudge 与 staged

核心 `NudgePromoter` 的历史流程为 `候选名/根目录校验 → LLM YAML → 解析 → 语义校验 → 安全扫描 → 写最终目录 → staged 注册`。候选名必须是受限的单层可移植目录名；同名目录一律拒绝，不能覆盖既有 Skill，也不能借由路径穿越写出 `SkillsDir`。但“写最终目录后再登记 registry overlay”不是 GUI 可接受的事务边界：它绕开共享提交器的补偿、checked index、最终审计与提交后清理。

因此 GUI 当前不实例化 `NudgePromoter`，并强制 `EnablePromoter=false`；即使 `skill_evolution_enabled=true` 也只保留工具序列的观测证据，不生成 YAML、不创建目录、不登记 config。需要沉淀时由人工在 Skills 流程中创建并审核。只有核心接口改为将**受管 staging 中的候选**交给共享 `SkillCommitter`，并补齐目录/config/index/audit/cleanup 的故障注入和回滚证明后，才可重新评估自动 staged 候选；届时仍不得自动激活为 `active`。

### 3.4 一次进化请求的生命周期

```text
created
  ├─(disabled / duplicate / cooldown)→ skipped
  └─ queued → running
                ├─ cancelled(operator|shutdown) → cancelled
                ├─ deadline exceeded             → timed_out
                ├─ candidate rejected             → rejected
                ├─ draft written                  → review_pending
                └─ gate passed + approval → apply → committed
                                           └─ write/audit error → rolled_back | audit_pending
```

每个请求必须使用稳定的 `request_id`。重试只能复用原请求的授权范围，并增加 `attempt` 序号；不得通过生成新请求绕过冷却、风险确认或最大失败次数。`skipped`、`cancelled`、`timed_out` 均属于有结果的终态，不能被前端显示为“仍在运行”。

### 3.5 全量写盘入口清单

以下清单是发布审计的边界。任何新增入口都必须加入清单，并满足共享 `SkillCommitter` 或受控 legacy adapter 的同等事务不变量；不能只在日志中声明“已回滚”。legacy adapter 只能作为迁移过渡，必须明确 action、补偿 schema、checked index、最终审计和结构化结果，且默认不得开启自动写盘。

| 入口 | 典型操作 | 当前保护 | 发布要求 |
|---|---|---|---|
| 创建/更新 | `CreateNLSkill`、`UpdateNLSkill` | **已接入共享 `SkillCommitter`：**预审计、持久补偿快照、YAML/config/index/最终审计顺序，失败时回滚或保留补偿；status overlay 在事务期间暂停 | 仍需补 GUI 入口级索引故障注入、YAML/重启恢复测试 |
| 状态/激活 | `SetNLSkillStatus`、`VerifyAndActivateNLSkill` | `VerifyAndActivateNLSkill` 与状态写入均经过共享 `SkillCommitter`；active 门禁、验证元数据、checked 索引和最终审计失败补偿统一；将状态设置为当前值会在写盘/审计前短路为零副作用 no-op | 补充状态入口 cleanup 失败、跨重启和最终审计故障注入 |
| YAML 版本恢复 | `RestoreSkillYAMLBackup` | **已接入共享 `SkillCommitter`：**版本恢复、config/index/final-audit 统一；索引或审计失败恢复旧 YAML/config，失败时保留 durable compensation | 补充多版本恢复、cleanup 失败和跨重启故障注入 |
| 备份 ZIP 恢复 | `SkillExecutor.RestoreSkills` / Wails `RestoreSkills` | **部分落地：**恢复入口在解析、扫描和配置追加期间由 `installMutex → skillListMutateMu` 串行，并在写入前复核全局/Skill compensation admission；队列不可读或目标有待恢复记录时零写盘，保存失败可观测 | 仍是 legacy config-only 入口，尚未接入共享 `SkillCommitter` 的 durable 快照、最终审计和跨重启回滚；自动恢复保持关闭，完成前仅允许人工核对 |
| 运行统计/工作区记录 | `SkillExecutor.recordSkillExecution`、`SkillRunner.updateUsageStats`、`RecordWorkaround` | **已收敛为受控 metadata 写入：**普通与 stable-binding 统计、workaround 记录均在 `installMutex → skillListMutateMu` 顺序下复核全局/Skill compensation admission；队列不可读、目标有 pending compensation 或保存失败时跳过写盘并记录诊断，不发出“已更新”事件；不会把运行统计误当作定义提交或激活授权 | 补充统计写入失败、别名冲突和跨重启丢失语义的入口级故障注入；对外仅承诺 fail-closed，不承诺统计必达 |
| 草案审核 | reviewed-draft apply | **已接入共享 `SkillCommitter`：**应用期间暂停状态 overlay，保留草案快照，config/YAML/index/final-audit 统一；draft 删除为提交后清理，失败保持 `committed + cleanup_status=pending`。入口级 cleanup failure、跨重启幂等清理和后续恢复器证据已覆盖 | 仍需补清理重试耗尽后的 `needs_review`、最终审计失败矩阵和前端异常分支；不得据此开启自动 apply |
| 草案审核 | reviewed-draft disable/reject | **已接入共享 `SkillCommitter`：**暂停状态 overlay，config/index/final-audit 统一，draft 删除为提交后清理；disable/reject 的 cleanup failure 均保持已提交 overlay 与 durable pending，并可由恢复器跨重启清理 | 仍需补清理重试耗尽后的 `needs_review`、最终审计失败矩阵和前端异常分支；自动化调用仍保持关闭 |
| 维护动作 | `ApplySkillMaintenanceAction` | **已接入共享 `SkillCommitter`（主要路径）：**事务期间暂停状态 overlay，config/YAML/index/final-audit 统一，checked 索引失败可恢复；config-only 无变化候选在补偿快照/YAML/index/audit 前返回 `skipped/no_change`；文件型契约补丁将本次新建 `skill.yaml.vN` 登记为 rollback cleanup，提交成功保留备份、回滚只清理本次产物；批量 mutator 发布完整更新列表，并以首个实际变化的稳定 Skill 名称作为审计/补偿主键，不再默认使用 `updated[0]`；IM 非 dry-run 即使最终为 refresh-only/no-op 也先检查补偿队列健康；旧 `CleanupStaleNLSkills` 无确认/无补偿/无审计边界，现已降为零写盘兼容入口，过期 Skill 必须走已审批 maintenance plan | 补充 merge 多条目、cleanup 失败和跨重启故障注入；自动维护仍保持灰度 |
| 重命名 | `RenameNLSkill` | **部分落地：**已使用共享 `SkillCommitter`；目录移动、YAML 原子写回、config/索引/最终审计失败可恢复，回滚不完整时进入 durable compensation；入口级最终审计失败和索引持续失败证据已覆盖，稳定别名统一按 canonical Name 关联 | 补充重启补偿、Windows 锁定、持续 cleanup pending 和完整入口矩阵；清理 worker 与统一 API 仍待完善 |
| 删除 | `DeleteNLSkill` / AgentService `DeleteSkill` / 传统 GUI `App.DeleteSkill` | **部分落地：**GUI `DeleteNLSkill` 已使用共享 `SkillCommitter`；AgentService 删除先移动到 `.skill-delete-pending-*` 隔离目录，再在最终审计前撤销 contract 并完成目录/索引校验，最后清理隔离目录；传统 GUI `App.DeleteSkill` 已在 legacy 边界内先持久化 metadata 快照、再隔离目录和原子更新 metadata，随后执行 checked routing index 刷新，并在最终审计成功后标记 `committed + cleanup_status=pending`。索引、撤销、审计或后续事务失败时，回滚会恢复目录、metadata 与索引，并尽力恢复原 contract，避免留下“已删除”审计却仍可路由的矛盾状态；传统 GUI 删除现已提供 `DeleteSkillDetailed` 结构化结果，并在包/队列清理失败时递增 attempts，达到 3 次持久化 `committed + cleanup_status=needs_review`，不再自动重试；`DeleteNLSkill` 已覆盖入口级最终审计失败回滚、queue-clear failure 后的缓存失效和跨重启清理，但仍未接入共享提交器的统一结构化 API | 补充 contract 恢复失败升级、Windows 锁定及完整跨重启矩阵；对外不得把 legacy `nil` 返回解释为统一 `committed` |
| 导入/安装 | ZIP、Hub/GitHub 导入 | 直接 `InstallHubSkill` 与 `UpdateHubSkill` 的已有目录替换均使用 `commitStagedSkillInstallWithExisting`；ClawHub/GitHub mixed-install 的 config-only 注册、managed capability external/Hub 新目录发布、能力缺口 Hub 新目录与 GitHub 配置注册、IM install-only/SkillMarket/Hub 目录安装（含 config-only 与已有版本替换）及 IM tool 目录/config-only/替换发布已迁移到共享 `SkillCommitter`；AgentService GitHub/Hub/Market/ZIP 导入也已迁移到共享目录事务；传统 GUI ZIP/插件安装、其它已有版本更新、HubCenter SkillMarket 更新和其它导入分支仍为入口级 durable compensation/legacy adapter；`App.InstallSkill` 已在目录/settings mutation 前持久化唯一外层 `legacy_gui_skill_install` 记录，并在最终审计后先写 committed 再清理；安装期间暂停 status overlay；已有版本更新保留 `.prev` 恢复证据；`maclaw.app` 依赖更新现已在最终审计后显式持久化 `committed + cleanup_status=pending`，清理失败委托 bounded-retry helper，不再回滚已审计目录 | 仍是多套编排，尚未覆盖所有目录型更新/导入入口；`App.InstallSkill` 尚未复用共享 `SkillCommitter` 或统一结构化返回，内部 `addSkillLockedWithRecord` 复用外层记录且不再产生中间 legacy 安装审计；AgentService 已具备启动恢复、scope 隔离、跨重启清理、清理重试耗尽后的 `needs_review` 保留，以及多包最终审计失败时恢复所有可恢复 sibling contract 的证据。剩余缺口集中在运行/上传、多租户和其它入口级异常矩阵；IM `toolPatchSkill` 主路径已接入但仍需入口级故障注入；能力缺口/IM 的非桌面实例仍缺少同等级持久配置快照；真实故障注入与剩余入口迁移仍待完成 |
| TUI `manage_skill` | `install`、`uninstall`、`upload`、`validate(auto_fix=true)`、`patch` | `install` 的 SkillHub 文件先写入 `.tui-skill-stage-*`，再通过共享 `SkillCommitter` 的 config/目录补偿、严格最终审计和提交后清理发布；ClawHub/GitHub config-only 安装也复用同一提交器；`uninstall` 先把所有匹配目录隔离到 `.tui-delete-pending-*`，再更新 config，跨重启保留 `EvolutionCompensationRecord`，最终审计成功后才清理；TUI 启动（交互、pipe、RPC 和 CLI）按 `action prefix + RecoveryScope` 只恢复本数据根记录，未归属旧记录不接管；配置 pre-image 即使为空也显式标记，避免恢复时只删目录而残留 registry；`validate(auto_fix=true)` 现在先把数据根内 Skill 目录移动到 `.tui-validate-prev-*` 并持久化 `tui_validate_auto_fix` 补偿，再复制工作树执行修复；严格开始/完成审计后标记 committed，清理失败保留 durable blocker，启动恢复可跨重启还原原目录；上传预检也先将数据根内 Skill 目录移动到 `.tui-upload-prev-*` 并持久化 `tui_upload` 补偿，远端提交前失败可恢复；远端返回 submission ID 后先写入外部提交标记，回执或审计失败不撤销远端结果而保留 pending blocker，启动恢复不会误删已提交的本地包；上传成功后原子写入 `upload_status.json` 并要求严格上传审计；ZIP 打包拒绝符号链接并传播关闭错误；text/structured patch 现在以进程内互斥 + YAML compare-and-swap 提交，校验 Skill 名称稳定身份，patch history 损坏或审计写入失败会回滚定义文件，不再 best-effort 报成功；`force=true` 跳过自动修复与快照 | TUI 安装/卸载、上传预检以及 `validate(auto_fix=true)` 已具备受限共享提交/跨重启补偿，但仍没有桌面级 checked routing index；上传远端提交后的本地持久化失败仍需人工核对，patch 尚未统一 `SkillCommitter` 的完整目录/索引补偿；自动上传保持关闭，失败后应展示“已回滚/回滚失败”并转人工核对 |
| 能力市场 MCP 配置 | `InstallHubCapability` → `ensureHubMCPInstalledLocal/Remote` | **部分落地：**本地 Stdio 与远程 HTTP MCP 通过 `commitMarketplaceMCPConfig` 统一完成配置文件 durable 快照、严格预审计/最终审计、no-op 短路、失败恢复和 `committed/cleanup_status` 持久化；配置型入口没有 Skill 目录索引步骤，提交后本地/远程运行时同步现有可取消的 checked 边界，取消会贯穿进程启动、initialize 与 tools/list；managed 远程 readiness 使用 strict initialize + tools/list，并校验 JSON-RPC `result`，HTTP 200 + `error` 或缺失 `result` 均视为失败；启动/工具发现失败会显式返回，并写入独立的非敏感 `mcp_runtime_sync.json`，状态为 `pending`→`needs_review`（3 次失败后停止自动重试），成功探测后写入 `ready` marker 并清除失败字段；已提交配置不能反向回滚。审计和 operator summary 不包含 endpoint、AuthSecret 或 headers；恢复器损坏/权限拒绝、跨重启重试和远程 runtime 可观测性仍不足；GUI 已提供受 `confirm=true` 保护的 `RetryMCPRuntimeSync(serverID, true)`，本地/远程均先重置待同步状态再执行 checked probe，失败保持 blocker。 | P0：补齐远程指标、恢复器失败与清理失败测试；完成前禁止自动 MCP 安装，仅允许人工确认 |
| 传统 GUI 安装/注册 | `App.AddSkill`、`App.InstallSkill`、`ImportNLSkillZip`、`App.DeleteSkill`、`InstallDefaultMarketplace`（`metadata.json`、ZIP 解压、插件/marketplace settings） | **仍未纳入共享 `SkillCommitter`，但已收紧 legacy 边界：**安装/删除/ZIP 导入共用 `installMutex`；`AddSkill` 与 `InstallSkill` 在任何 metadata/settings/目录副作用前先通过全局补偿队列健康检查，损坏队列下零写盘；`SetNLSkillStatus`、`SkillExecutor.Register/Update/UpdateLearnedSource/UpdateStatus/UpdateVerification/MarkUploaded` 以及 `DeleteNLSkill`/`RenameNLSkill` 现在也在同一 App 级写锁下执行，避免状态/上传元数据与目录事务交错；`App.AddSkill` 在首次破坏性移动前持久化 metadata/目录补偿，写入后刷新 checked index，最终审计成功后先写 `transaction_state=committed` 再清理；重复的同内容 Add 返回 `skipped/no_change`，不重写 metadata 或制造补偿（ZIP 路径比较按 basename 规范化）；`App.DeleteSkill` 先持久化 metadata 快照并隔离包目录，写入后刷新 checked index，索引失败会先恢复目录/metadata 再重建索引；删除不存在的 Skill 返回 `skipped/no_change`；插件已启用且元数据一致时 `App.InstallSkill` 返回 `skipped/already_current`，不改 settings 或创建补偿；ZIP 文件先写保留源 basename 的临时快照；既有空 skills 目录可安全接收根级文件，并将新文件逐项登记到 `CreatedDirs`，非空目录/已有顶层包更新仍 fail-closed；`App.InstallSkill` 其它路径在目录/settings mutation 前持久化外层 `legacy_gui_skill_install` 记录，**默认 marketplace 与 `enabledPlugins` 共用该外层 settings 快照**，解压后补写新目录路径，且 scan cache 仅写入本 ZIP 实际发布的 Skill 目录，不覆盖共享根下既有 Skill 的扫描证据，最终审计成功后先标记 committed 再清理；`ImportNLSkillZip` 在解压/发布前恢复 action=`import` 的历史补偿，批量记录保留 `AffectedSkills` 与 `FinalAuditKind`，跨重启可恢复所有目录并重建索引；独立 `InstallDefaultMarketplace` 使用 `legacy_gui_marketplace` durable `FileSnapshots`，严格最终审计后再标记 committed，失败恢复 settings，重复调用为零写盘 no-op，已存在但来源冲突时 fail-closed；metadata/settings 使用原子写入和严格 JSON 解析。新增 `InstallSkillDetailed`、`AddSkillDetailed`、`DeleteSkillDetailed` 结构化结果视图，但兼容 API 仍返回 `error`，且结果只代表 legacy adapter 的分类，不等同共享提交器承诺 | 自动安装和自动更新保持关闭；仅允许人工确认并在失败后人工核对目录、配置、索引和审计；legacy 结果必须同时检查 `state` 与 `cleanup_status`，清理失败需人工处置 |
| 非桌面导入 | `corelib/agentservice.persistImportedEntries` 及其 `SkillInstall` 调用链 | **已纳入共享 `SkillCommitter`（目录型边界）。**多包导入作为一个批次提交；目录发布前持久化移动意图，更新保留 `.prev`，发布后执行 checked 扫描和最终审计，清理失败返回 `committed + cleanup_status=pending`；同内容重试（`overwrite=true/false`）均为零写盘 no-op；候选定义比较还会检查脚本/Markdown/资产 payload，避免只比较 YAML 而漏掉执行内容变化；最终审计失败会先全量验证 external snapshot，再稳定顺序恢复 contract，单个 provider 失败不短路其他合法 sibling；`NewService` 启动阶段按 `agentservice_install` 前缀和 `RecoveryScope` 执行专用恢复，旧记录仅在所有目录路径都证明属于当前服务根目录时才接管；已有重启清理、`DataRoot` 隔离及清理重试耗尽后 `needs_review` 保留回归 | 仍需补运行/上传、多租户和其它入口级异常矩阵；恢复失败、队列不可读、scope 越界或 pending/needs_review 记录会 fail-closed，自动导入继续关闭 |

## 4. 状态模型与激活门禁

持久化状态以 `staged`、`active`、`disabled`、`archived` 为主；`discovered`、`scanned`、`verified`、`unverified`、`needs_review` 是治理/验证标签，不代表可执行。

| 状态/标签 | 路由 | 执行 | 说明 |
|---|---:|---:|---|
| `discovered` / `staged` | 否 | 否 | 候选，等待扫描、验证、审批 |
| `scanned` / `verified` | 否 | 否 | 有扫描或证明，仍需显式激活 |
| `unverified` / `needs_review` | 否（默认） | 否（默认） | 证据不足或等待人工处理 |
| `active` | 是 | 是 | 已批准且审计完整 |
| `disabled` / `archived` | 否 | 否 | 停用或归档 |

`VerifyAndActivateNLSkill`（含 GUI 参数入口）是 `staged → active` 唯一提交路径：读取最新定义并确认来源；重扫 Schema/安全；获取真实参数并校验 `required_args`；Gate 必须 `passed`；保存 `verified_at`、`verification_run_id`、`verification_digest`（参数只保存摘要）；strict 审计预检；暂停 status overlay 异步写入；在第一次 YAML/config 变更前持久化补偿快照；备份后原子更新 YAML、内存和索引；写入最终 `status_applied` 事件；最后才清理补偿记录。当前实现已通过共享 `SkillCommitter` 覆盖上述顺序；最终审计失败必须恢复 YAML/内存/索引，或显式进入 `audit_pending`/`needs_review` 补偿状态。补偿清理失败时不得重新反向回滚已审计版本，而应保留队列并继续阻断该 Skill。

普通 `SetNLSkillStatus(name, "active")` 必须拒绝 auto-discovered staged Skill。空参数、缺少必填项、无最近证据或无法重放时保持 `staged`。

### 4.1 权威数据源与合并规则

为避免重启后状态漂移，读取顺序固定为：

1. `skill.yaml`/`skill.yml`：定义、步骤、来源、版本和验证元数据的权威来源；
2. 配置 overlay：仅补充运行统计、冷却时间、失败摘要等治理数据；
3. 内存索引：启动扫描的派生缓存，可随时由前两者重建。

当 overlay 与 YAML 冲突时，安全状态取更严格者：`active` 不能覆盖 YAML 的 `staged`、`unverified` 或 `needs_review`。检测到 YAML、索引或版本摘要不一致时，应将候选降级为 `staged`/`needs_review`，记录 `state_reconciled`，而不是尝试“就地修正”为 `active`。

## 5. 统一决策协议

```json
{
  "request_id": "evo_20260829_...",
  "trigger": "execution_failed|manual|maintenance|nudge",
  "action": "repair",
  "skill": "example",
  "decision": "apply|draft|review|reject|unverified",
  "reason": "success_rate_below_threshold",
  "evidence": {"usage_count": 12, "success_rate": 0.42, "error_class": "dependency", "recent_args_digest": "sha256:..."},
  "risk": "low|medium|high|critical",
  "gate": {"status": "passed|failed|unverified", "verification_run_id": "...", "evidence_mode": "real|mock|none"},
  "rollback": {"backup_version": 3},
  "requires_human_review": true
}
```

字段约束：`request_id`、`skill`、`action`、`decision`、`reason`、`config_revision` 为关联和审计必填；`evidence_digest` 只能是脱敏摘要；`gate.status=passed` 时必须同时存在真实 `verification_run_id` 和 `evidence_mode=real`。`decision=apply` 需要 `requires_human_review=false` 或可验证的审批记录；`draft/review/reject/unverified` 不得改变可执行定义。

所有动作遵循 `Observe → Attribute → Decide → Generate → Validate → Apply → Verify → Learn`。LLM 只能生成候选，不能替代 Gate、策略或审批。`request_id`、触发来源和配置版本用于幂等、去重和跨事件关联；重试不能创建新的授权范围。

## 6. Schema、扫描与 Gate

校验覆盖 `required_args`/占位符双向契约、参数格式和重复项；`on_error` 枚举；`capture` 正则及捕获名；`poll/loop` 边界；`mode/operations/pipeline` 一致性；ToolSequence 名称/顺序/参数映射；路径、命令、网络目标和权限扫描。

| Gate | 含义 | 处置 |
|---|---|---|
| `passed` | 真实参数重放达标 | 低风险可按策略应用 |
| `failed` | 重放、动作或校验失败 | 拒绝并保留证据 |
| `unverified` | 缺 Executor/参数或动作被跳过 | 仅草案/人工复核 |

`craft_tool`、MCP、浏览器、poll/loop 等非 Bash 动作需要显式隔离 Replay Adapter。当前 `NonBashReplayAdapter` 已定义证据契约：无 adapter 或 `evidence_mode=mock/none` 只能得到 `unverified`；只有真实隔离执行且证据模式为 `real` 才可能 `passed`。生产 adapter 尚未完成前，不得把 mock 结果用于写盘、激活、上传或发布。

## 7. 配置与治理开关

```json
{"skill_evolution_enabled":false,"skill_auto_upload_enabled":false,"skill_maintenance_observation_enabled":true,"skill_evolution_max_concurrent_workers":2,"skill_evolution_worker_timeout_seconds":180}
```

上面的值是当前安全默认值，不是目标配置。只有 P0 退出条件全部满足后，才允许按入口和 Skill 灰度把 `skill_evolution_enabled` 改为 `true`；自动安装仍需独立的人工确认和来源策略。

- `skill_evolution_enabled=false`（或字段缺失/配置不可读）：停止自动 repair/optimize/promote/install，保留查询、人工操作和治理审计；GUI 已启动的 pipeline 必须即时关闭这些 mutating stage。
- `skill_auto_upload_enabled=false`（或字段缺失）：停止自动上传。它与自进化开关独立，但同样只能由显式 `true` 授权；不能因成功次数阈值或旧配置缺字段而自动发布到远端。
- `skill_maintenance_observation_enabled=false`：停止只读观察、维护计划和经验采集，不关闭审计、状态查询或安全门禁。
- 并发上限实时生效，按 1–16 约束。`skill_evolution_worker_timeout_seconds` 已支持配置，默认 180 秒，范围 30–1800 秒，并在 GUI 状态中展示。

## 8. 风险与失败语义

| 场景 | 默认决策 | 业务写盘 |
|---|---|---:|
| 只读维护/经验采集 | allow | 否 |
| 低风险优化且 Gate=passed | candidate/apply | 默认关闭自动写盘；仅人工/灰度 |
| 文件型修复 | draft | 否 |
| Nudge 新 Skill | 人工创建/审核 | GUI 当前禁止自动提升；仅保留观测证据 |
| Hub/GitHub 安装 | review | 否，待确认 |
| 合并/退役/市场上传 | draft/publish_pending | 否，待确认 |

关键写盘采用“预检 → 备份 → 原子写盘 → 刷新索引 → 最终审计”。预检失败阻止写盘；最终审计失败回滚，无法回滚时进入补偿队列并显式标记，不能只记日志。删除 reviewed-draft 文件属于提交后的清理动作，不能先于最终审计；拒绝 draft 也必须先完成审计预检，并在审计失败时恢复 config 计数和 draft 文件。

> 当前发布策略：即使某条低风险路径具备局部回滚能力，也不能据此恢复全局自动写盘或自动上传。只有入口完成共享提交器迁移、故障注入和跨重启验收，并满足 S1 退出条件后，才允许按 Skill/入口灰度显式开启。

**No-op 规则**：计划为空、候选摘要与当前权威摘要相同、或维护动作的 `ExecutedCount=0` 时，不创建补偿记录、不刷新索引、不写 `applied/committed` 事件，也不增加失败次数；应返回 `skipped/no_change`，并可写入一条轻量的 `decision=skipped` 审计事件（不得携带新的版本号或备份）。这样可以避免“空计划伪提交”以及无实际变更却生成版本备份。

安装入口也必须遵守幂等规则：下载包摘要与当前已安装版本相同则返回 `skipped/already_current`，不得重复发布目录、刷新索引或发送“新版本已安装”事件；同名但摘要不同才进入更新事务。发现目标目录或 `.prev` 冲突时必须拒绝并转人工处置，不能通过删除旧目录来“恢复安装”。

> **实现边界**：上述 no-op 是统一提交器的验收要求。当前共享目录安装提交器已在直接 Hub 安装和 `UpdateHubSkill`、mixed-install 新目录、managed capability 新目录、能力缺口 Hub 新目录、IM install-only/IM tool 的目录及 config-only 路径执行同版本短路；GUI config-only maintenance（如 lifecycle/contract metadata 已经一致）同样在写盘前短路。传统 GUI ZIP/插件及其它 legacy adapter 仍需逐入口补充“摘要相同即跳过”的回归断言。在断言补齐前，不得把 legacy 路径的返回成功或同名冲突错误解释为 `already_current`。

## 9. 队列、取消、超时与重试

### 已实现

- 同 Skill 串行、跨 Skill 并行、worker 上限和通知合并；
- `CancelSkillEvolution` 可移除 pending 或取消 active context；
- API/GUI 暴露 active、cancelled、timed-out、队列年龄和失败摘要。

### 已补强

- Pipeline 通过 `RepairHookWithContext` 将 worker context 传入 GUI repair；GUI LLM 请求、Gate、扫描、写盘和上传复核均响应取消。
- worker 使用配置化 timeout（默认 180 秒，范围 30–1800 秒），并区分 operator cancel 与 shutdown；GUI 展示取消/超时计数和事件提示。

### 取消与超时语义

| 结束原因 | context 错误 | 是否允许 Apply | 事件 |
|---|---|---:|---|
| 操作者取消 | `context.Canceled` | 否 | `skill:evolution_cancelled`, `reason=operator_requested` |
| worker deadline | `context.DeadlineExceeded` | 否 | `skill:evolution_timed_out`, `reason=worker_deadline` |
| 应用关闭 | shutdown context canceled | 否 | `skill:evolution_cancelled`, `reason=shutdown` |

规则：以 context 为最终裁决；即使 LLM 已返回或 Gate 已通过，写盘前仍必须再次检查 context。取消发生在 LLM 调用前不增加 repair 次数；已经消耗真实 LLM/Gate 资源的失败是否计次必须由统一审计策略记录，不能由入口自行解释。

建议将取消/超时统一编码为以下审计字段，而不是只依赖自由文本：

| 字段 | 示例 | 规则 |
|---|---|---|
| `request_id` | `evo_...` | 同一请求全链路不变 |
| `termination` | `operator_cancelled` / `worker_timeout` / `shutdown` | 与 context 来源一一对应 |
| `failure_reason` | `context_canceled` / `deadline_exceeded` | 稳定枚举，供统计聚合 |
| `config_revision` | `cfg-42` | 记录实际采用的 timeout、并发和开关 |
| `attempt` | `1` | 同一请求内单调递增 |

字段落盘前应完成长度限制和敏感信息脱敏；写入失败时，关键写盘必须停止或回滚。

### 必须继续补强

1. 为 GUI/manual/reviewed-draft 入口补齐与 pipeline 相同的 `request_id`、`config_revision`、`evidence_mode`、`failure_reason` 字段校验和回归断言；已有字段不得只写日志而不进入结构化审计。
2. pipeline、GUI、manual force 三条 repair 路径统一遵守当前核心 3 次最大尝试；达到上限后稳定进入 `needs_review`，不得重复调度。达到上限后必须同时阻断自动入口和 `force=true` 入口，除非人工显式重置治理记录。
3. GUI 任务列表已实现基础版；下一步增加终态（committed/rolled_back/audit_pending/timed_out/cleanup_pending）查询、取消确认和失败原因过滤，避免只依赖 toast。

## 10. 写盘事务与回滚

### 10.0 优化后的统一逻辑（先区分两类状态）

评审中最容易产生误判的是把“Skill 运行状态”和“写盘事务结果”混为一谈。两者必须分开：

| 维度 | 允许的值 | 作用 | 失败后的默认值 |
|---|---|---|---|
| Skill 运行状态 | `staged`、`active`、`disabled`、`archived` | 决定是否能被普通路由和执行器使用 | 保持旧状态；无法证明一致时降为 `staged`/`needs_review` |
| 事务结果 | `skipped`、`prepared`、`committed`、`rolled_back`、`audit_pending` | 描述本次写盘是否完成、是否因无变化而跳过、是否仍需补偿 | `rolled_back` 或 `audit_pending` |
| 提交后清理 | `clear`、`pending`、`needs_review` | 描述 `.prev`、staging、draft、补偿记录等提交后清理是否完成 | `pending`（保持阻断） |

`skipped` 只表示候选与权威状态完全一致的幂等终态，不产生新版本或副作用；它不是 `committed`，也不能替代首次提交的最终审计。`committed` 只表示本次定义、配置、（适用时）目录发布、索引和最终审计均成功；它不是把 Skill 自动设为 `active` 的授权。相反，`active` 必须再满足对应的验证和审批门禁。`committed` 但 `cleanup_status=pending` 也不能对外宣称“完全完成”：允许只读查询，不允许执行、上传或下一次自动写盘，直到清理幂等重试成功或人工处置完成。`audit_pending`、`needs_review` 或不可读的补偿队列同样属于 fail-closed。

准入实现不得把一次 `skipped` 当成新的提交版本：no-op 结果仅记录请求级幂等事实，并保留此前最近一次健康的 `committed` 指针与其清理状态。这样重复维护或同版本安装不会制造副作用，也不会让已经验证且仍一致的 Skill 因 no-op 被误判为不可执行；若不存在此前健康提交，则 no-op 仍不能单独授予执行或上传权限。

三条状态轴必须分别展示和判断，不能压缩成一个 `status` 字段：

| 状态轴 | 示例 | 谁负责改变 | 是否单独授予执行权 |
|---|---|---|---:|
| Skill 运行状态 | `staged`、`active`、`disabled` | 验证/审批/生命周期入口 | 否；仍需满足准入公式 |
| 事务结果 | `prepared`、`committed`、`rolled_back`、`audit_pending` | `SkillCommitter` 或安装适配器 | 否；`committed` 也不等于 active |
| 提交后清理 | `clear`、`pending`、`needs_review` | 清理 worker/启动恢复 | 否；非 `clear` 时保持阻断 |

对外 API 应同时返回三条状态轴及 `request_id`。前端不得仅依据 Skill 的 `status=active`、toast 文案或最近一条成功日志放行执行/上传。

所有入口必须遵守同一条单向链，但配置型变更和目录型安装的物理顺序不同，不能用一条“全入口固定顺序”掩盖差异。两类流程都必须先持久化补偿快照，且都以 checked index 和最终审计作为提交门槛：

```text
候选/安装包
  → 预检（Schema、来源、扫描、Gate、审计、补偿队列）
  → prepared（先持久化完整补偿快照）
  → 原子写盘（配置型：config → YAML；目录型：发布目录 → config/YAML）
  → 重建内存与索引（失败立即回滚）
  → 最终审计（失败回滚或 audit_pending）
  → committed
  → 提交后清理（.prev、staging、draft、补偿记录）
       ├─ clear       → 可进入后续准入检查
       └─ pending     → 保持阻断，等待幂等清理/人工处置
```

清理属于 `committed` 之后的幂等动作，清理失败不能重新走反向回滚；但在清理完成前仍应对受影响 Skill 保持阻断，并通过重试或人工处置清除残留。`skipped/no_change` 是无副作用终态，不经过上述写盘链，也不应刷新索引或产生版本备份。任何入口若无法在“首次持久化变更前”写入补偿记录，必须停止写盘，而不是事后补记日志。

**Legacy GUI 例外边界必须显式标注**：`App.AddSkill`/`App.DeleteSkill` 当前使用独立的 `legacy_gui_skill_*` 补偿记录，能够保护 `metadata.json` 和 ZIP 隔离移动，并在 metadata 恢复后重建 checked index，最终审计后先持久化 `committed` 再清理；但它们尚未纳入共享 `SkillCommitter`。`App.InstallSkill` 在外层持有唯一 `legacy_gui_skill_install` 恢复记录，目录解压、scan cache、插件 settings 与 `addSkillLockedWithRecord` 均受该记录保护；内部 metadata/package 编排不再创建嵌套 durable 记录或中间“已安装”审计，外层再执行 checked index 与唯一最终审计，因此不能把任何单步成功拼接为共享提交器语义。迁移完成前，InstallSkill 的 ZIP/插件路径必须保持人工确认、禁止已有目录覆盖，并将任意失败视为需要人工核对的 legacy 结果。

所有会改变 Skill 定义的路径都必须遵循同一事务边界。配置型入口（创建、更新、repair、状态、draft、维护）采用 `config → YAML → index → audit`；目录型入口（ZIP、Hub/GitHub、managed capability）采用 `publish directory → config/YAML → index → audit`。目录发布前必须记录 `CreatedDirs`、旧目录和 `.prev` 意图，不能先移动目录再补写补偿记录：

1. 读取权威定义并计算版本/digest；
2. 校验 Schema、安全扫描、Gate 和审计可用性；
3. 保存可恢复备份；
4. 按入口类型原子写入配置/YAML，或发布目录后写入配置/YAML；
5. 刷新内存和索引；
6. 执行最终审计；
7. 任一步失败则按备份恢复，并把恢复结果写入 `rollback` 或 `audit_pending` 事件。

取消、deadline 或 shutdown 在第 4 步之前发生时不得写盘；第 4 步之后发生时必须完成回滚或进入显式补偿状态，不能仅依赖日志。

当前实现分为以下几类；下表是“现状”而非目标设计。`audit_pending` 是跨重启的补偿队列，不是普通审计日志：记录包含 YAML、配置 overlay 和（适用时）reviewed-draft 快照，目录型入口还必须包含目录移动/新建路径。对于需要回滚的记录，只有在 YAML、配置、目录、内存和索引均恢复并完成校验后才能原子移除；对于已经完成最终审计、仅提交后清理失败的记录，必须保留到幂等清理成功后再移除，不能用“恢复成功”提前删除。连续 3 次恢复失败后保留为 `needs_review`，并阻断该 Skill 的执行和上传。staged 激活现已把 YAML、overlay、索引刷新和最终审计纳入同一回滚边界；索引或审计导致回滚不完整时写入该队列。提交成功后的清理失败不应改写为 `rolled_back`，而应记录 `cleanup_status=pending` 并继续阻断。队列 schema（当前为 v1）与审计事件 schema（当前为 v2）相互独立，升级时必须分别校验和迁移。**AgentService 导入和删除已使用上述目录型补偿边界，`NewService` 启动阶段会按 `agentservice_install*` action prefix 与 `RecoveryScope=dataRoot` 尝试恢复；恢复会再次验证所有 durable 路径均在当前服务根目录内。恢复失败、队列不可读、scope 缺失/越界或仍有 pending 记录时设置内存 fail-closed 门禁。传统 GUI 安装仍不属于该统一边界。**

| 路径 | 当前行为 | 主要缺口 |
|---|---|---|
| `VerifyAndActivateNLSkill` | **已接入共享 `SkillCommitter`**：YAML、overlay、索引刷新和最终审计失败时恢复/降级；事务前写入 durable compensation；checked 索引边界已接入；提交后清理失败保留队列 | 真实索引 provider 故障注入、启动补偿验收和最终审计失败的入口级回归仍需完善；仍只允许显式人工激活 |
| GUI `persistRepairResultWithContext` | **已接入共享 `SkillCommitter`**：context 取消、状态 overlay 暂停、YAML/config/index/final-audit 统一；失败恢复或保留 durable compensation；scan cache 仍按非权威派生缓存处理 | 补充 cleanup 重试耗尽、跨重启恢复和生产索引 provider 故障注入；成功/阻断事件的字段校验仍需统一 |
| GUI `RestoreSkillYAMLBackup` | **已接入共享 `SkillCommitter`**：版本恢复写入、config/index/final-audit 统一；索引或审计失败恢复旧 YAML/config，失败时保留 durable compensation | 补充多版本、cleanup 失败和跨重启故障注入 |
| GUI reviewed-draft apply | **已接入共享 `SkillCommitter`**：config/YAML/index/final-audit 统一；draft 删除为提交后清理，失败保持 `committed + cleanup_status=pending`；入口级 cleanup failure 与跨重启幂等清理已有回归 | 仍需补清理重试耗尽后的 `needs_review`、最终审计失败矩阵和前端异常分支；不得据此开启自动 apply |
| GUI reviewed-draft disable/reject | **已接入共享 `SkillCommitter`**：config/index/final-audit 统一，draft 删除为提交后清理；disable/reject 两条路径的 cleanup failure 都保留 durable queue 并可在恢复器中清理 | 仍需补清理重试耗尽后的 `needs_review`、最终审计失败矩阵和前端异常分支；不得据此开启自动 disable/reject |
| managed capability / Enterprise install | Hub、外部源的新目录安装经过共享 `SkillCommitter` 的安全扫描、预审计、durable compensation、配置恢复、checked 索引和最终审计；已有版本更新仍由入口级适配器完成相同的 committed/cleanup 边界，发布前写入初始目录补偿，更新保留 `.prev`；最终审计后才进入提交后清理；已有 `.prev` 或备份移动失败时拒绝覆盖；maclaw.app 依赖更新也纳入同样的目录补偿与最终审计顺序 | 更新路径仍有重复安装编排，尚未全部复用共享目录安装提交器；真实故障注入和跨重启安装恢复测试待完成 |
| 能力缺口 / IM 自动安装 | **Skill 新目录、config-only 与已有版本替换分支已迁移**：能力缺口 Hub 和 GitHub 配置注册、IM install-only/SkillMarket/Hub 及 IM tool 的所有写盘分支调用共享 `SkillCommitter`，并按稳定身份/版本执行 `skipped/already_current` 幂等短路；无 App 的非桌面文件写盘现在直接拒绝。能力市场 MCP（本地 Stdio/远程 HTTP）已具备独立配置 durable 事务，但运行时同步和完整故障注入仍未完成 | capability-gap 异步自动安装由永久 rollout fence 关闭；MCP 配置安装、其它自动安装继续人工确认并 fail-closed，直至补齐同步失败/恢复失败/跨重启证据 |
| AgentService GitHub/Hub/Market/ZIP 导入 | **已接入共享 `SkillCommitter`（目录型）。**导入先在目标根目录 staging，再以批次发布；覆盖安装保留 `.prev`，陈旧 `.prev` 仅在确认内容确有变化后拒绝；最终检查每个目录都能重新加载并写严格审计，提交后才清理 `.prev`。同内容导入在有无 overwrite 标志下均是零写盘 no-op，且会比较非 YAML 的脚本/Markdown/资产 payload；批量发布中途失败和最终审计失败均会回滚整批目录；external snapshot 在回滚前全量验证，单个 contract provider 恢复失败会保留 `audit_pending` 但不会阻断其他合法 sibling 恢复；提交后清理失败保留 `committed + cleanup_status=pending`，并已覆盖多包清理跨重启恢复及重试耗尽后的 `needs_review` 保留；`NewService` 启动阶段按 `agentservice_install` 前缀和 `RecoveryScope=dataRoot` 执行专用补偿恢复；运行与上传入口复用 scope-aware pending 检查；已有重启清理与不同 `DataRoot` 恢复隔离回归。 | scope 缺失/路径越界、恢复失败或 pending/needs_review 记录会 fail-closed，自动导入、执行和上传继续关闭；仍需补运行/上传、多租户和其它入口级异常矩阵 |
| pipeline `runOptimize` / core-only repair | 通过 `persistDefinitionChange` 统一处理 config、YAML、索引和最终审计失败补偿；提交失败恢复内存 entry；仅 `committed` 且 `cleanup_status=clear` 触发成功事件/upload；不完整回滚可重启恢复 | maintenance 文件型契约、批量边缘分支的入口级故障注入与跨重启仍需补齐；索引 provider 的真实失败仍需生产实现 |

因此“写盘事务”目前标记为“核心 pipeline、GUI 生命周期主要入口和 AgentService 目录导入已统一到共享提交器；新目录安装分支已迁移，但已有版本更新、传统 GUI ZIP/插件安装及部分维护路径仍各自具备局部可恢复边界，全路径仍部分落地”。`SkillCommitter` 返回 `committed`、`rolled_back`、`audit_pending` 三态，并附带 `request_id`、`backup_version`、`config_revision`、`rollback_complete` 与独立的 `cleanup_status`；调用方只有在 `committed + cleanup_status=clear` 时才能发送成功事件或 upload。最终审计通过 `FinalAuditor` 作为提交步骤执行。所有已接入路径的不完整回滚都会写入 durable queue；目录发布前还会记录确定性的 `CreatedDirs`，已有版本更新保留 `.prev`，避免崩溃窗口丢失恢复路径；存在陈旧 `.prev` 或无法移动旧目录时，安装会拒绝覆盖而不是删除旧版本。最终审计成功后才允许清理旧备份和补偿记录；清理失败不得反向回滚已审计版本，而是继续阻断并等待幂等清理。能力缺口/IM 的非桌面实例仍缺少同等级持久配置快照，因此自动安装必须保持关闭或改为人工确认。

> 口径修订：上一句中的“多个 GUI 入口仍各自具备局部可恢复边界”仅描述全文评审基线。以当前代码为准，GUI repair、`RestoreSkillYAMLBackup`、`reviewed-draft`、`RenameNLSkill` 和 `DeleteNLSkill` 已迁移到共享提交器的主要路径；managed capability external/Hub、能力缺口 Hub/GitHub 配置注册、IM SkillMarket/Hub 与 IM tool 的目录/config-only/替换路径也已迁移，但传统 GUI ZIP/插件及其它导入路径仍为入口级适配器；剩余未统一范围是 maintenance 边缘分支及安装/导入更新路径。它们仍按 P0 上线阻断处理，直到入口级故障注入和跨重启验收完成。

> **本轮补充口径**：`managed capability` 的 external/Hub 新目录安装、能力缺口 Hub/GitHub 配置注册以及 IM 新目录安装现在直接调用共享提交器；已有版本更新仍保留入口级 `.prev`/补偿适配器。因此“统一提交器已覆盖安装”仅适用于已迁移的新目录/配置注册分支，不能外推到所有安装更新路径。

### 10.1 推荐的统一提交模板（目标 API）

所有自动 repair、optimize、reviewed-draft apply、maintenance、创建/更新、staged 激活、重命名、删除以及新目录安装都应复用同一类提交器。当前新目录安装分支已接入，已有版本更新、GitHub/ZIP 导入和部分 maintenance 分支仍由 legacy adapter 编排；在这些入口完成共享提交器覆盖前必须保持自动写盘关闭。提交器不接收 LLM 原文，只接收已经过 Schema、扫描和 Gate 的候选，以及当前权威版本摘要。

```text
prepare(ctx, candidate)
  ├─ 校验 ctx、审计可用性、来源版本和 candidate digest
  ├─ 读取 YAML、config、内存 entry、索引摘要
  └─ 创建带 request_id/config_revision 的 backup bundle

commit(bundle, kind)
  ├─ 再次检查 ctx.Err()（取消/超时立即返回 cancelled/timed_out）
  ├─ 若 candidate digest 与权威版本相同，返回 skipped/already_current（零写盘）
  ├─ 配置型：原子写 config，再写 YAML；目录型：发布目录，再写 config/YAML
  ├─ 刷新内存 entry 和 checked index；失败则进入 rollback
  ├─ 写最终审计事件；失败则进入 rollback 或 audit_pending
  ├─ 持久化 transaction_state=committed
  └─ 执行提交后清理；失败只置 cleanup_status=pending，不反向回滚

rollback(bundle)
  ├─ 目录型先恢复目录，再恢复 YAML、config、内存和索引
  ├─ 仅删除本事务登记的 rollback cleanup artifacts
  ├─ 校验恢复后的 digest 一致
  ├─ 写 rollback 事件（含 failure_reason）
  └─ 任一恢复动作失败：标记 needs_review/audit_pending，禁止执行和上传
```

**当前实现映射：**生产代码目前由一次 `SkillCommitter.Commit(ctx, ...)` 依次完成上述阶段；`prepare` 不是对外可调用对象，补偿记录由提交器在首次写盘前持久化，回滚和提交后清理由内部闭包及启动恢复流程完成。后续若拆分公共 API，必须保持相同的幂等键（`request_id + action`）和 fail-closed 门禁，不能让调用方自行拼接阶段而重新产生多套事务语义。

提交器的返回值应包含 `state`、`request_id`、`backup_version`、`config_revision`、`failure_reason`、`rollback_complete` 和 `cleanup_status`。调用方必须以 `state + cleanup_status` 做唯一分支：只有确有变更且 `state=committed`、`cleanup_status=clear` 才能发 `skill:repaired`/`skill:optimized` 或调用 `UploadTrigger`；`state=skipped` 只能发幂等跳过事件，不得触发新上传，也不得覆盖此前的提交状态指针；`rolled_back`、`audit_pending` 或 `cleanup_status=pending|needs_review` 只能发失败/补偿事件。

### 10.2 事务结果判定

| 结果 | YAML | 内存/索引 | 对外决策 |
|---|---|---|---|
| 预检失败 | 不变 | 不变 | `rejected` |
| 原子写盘失败 | 不变或恢复备份 | 不得发布新版本 | `rolled_back` |
| 索引刷新失败 | 恢复 YAML 与旧索引 | 保持旧定义 | `rolled_back` |
| 最终审计失败且可恢复 | 恢复备份 | 恢复旧索引 | `rolled_back` |
| 最终审计失败且不可恢复 | 可能已写盘 | 标记 `audit_pending`/`needs_review`，禁止执行 | `audit_pending` |
| 全部成功且清理完成 | 新版本 | 新索引 | `committed` + `cleanup_status=clear` |
| 业务已提交但清理失败 | 新版本 | 新索引 | `committed` + `cleanup_status=pending`；保持阻断 |

“记录了错误但继续运行”不属于合格回滚。`audit_pending` 是补偿状态，不是成功状态，也不能被普通状态接口提升为 `active`。

### 10.2.1 补偿记录生命周期

补偿记录不是“失败日志”，而是可执行的恢复凭据。所有可能跨越进程崩溃窗口的写盘入口，都必须在第一次持久化变更前写入完整快照；快照至少包括 YAML、配置 overlay、操作标识和（涉及目录移动时）目录恢复信息。记录还应保存预期的 `final_audit_kind`：若进程在严格最终审计已经追加、但尚未把队列状态改写为 `committed` 时崩溃，启动恢复只能在同时匹配 `request_id + skill + action + audit kind + 非前置决策` 的真实审计行后，才把记录升级为 `committed + cleanup_status=pending` 并执行幂等清理；不能仅凭字段存在或普通日志推断已提交。

```text
prepared
  └─ persist compensation
       ├─ mutation/audit succeeds → committed → post-commit cleanup
       │                              ├─ clear
       │                              └─ pending/needs_review → execution blocked
       ├─ mutation fails           → rollback → rollback complete → clear
       │                              └─ cleanup failure → audit_pending → execution blocked
       └─ rollback/cleanup fails   → audit_pending / needs_review → execution blocked
```

这里有三个容易混淆的边界：

1. **写入补偿记录成功，不代表业务变更成功。** 在记录清理前，相关 Skill 仍按“存在待恢复补偿”处理，执行和上传入口必须保持 fail-closed。
2. **回滚完成后才允许清理记录。** 回滚路径应恢复 YAML、配置、内存和索引，并校验旧摘要；任一步失败都必须保留记录并降级为 `audit_pending`，不能为了“队列为空”而强制删除。
3. **业务已提交但清理失败不是回滚。** 若最终审计已经成功、仅补偿文件清理失败，应保留记录、阻断该 Skill，并返回“committed-but-cleanup-pending”类错误，等待幂等清理或人工处置；不得重新执行反向回滚覆盖已提交版本。

补偿记录清理必须以 `request_id` 为主键，`skill+action` 仅可作为无 request ID 的历史兼容匹配。清理操作应幂等、原子重写 JSONL，并在重启后可重复执行。队列不可读、版本不支持或记录字段非法时，系统应把“无法证明已恢复”视同“存在待恢复补偿”。涉及动态 Skill contract、远端绑定或其它目录外授权状态时，记录还必须携带最小化的 `external_snapshots` 与 `external_applied` 标记；没有对应恢复器时不得吞掉该记录，必须保持 pending 并 fail-closed。

#### 外部快照契约（新增约束）

`external_snapshots` 不是任意日志字段，而是可跨进程恢复的最小 pre-image。为避免不同服务写入不可解释的数据，生产实现必须满足以下约束：

> **实现状态**：AgentService 动态 contract 已使用下表的 v1 envelope，并在恢复前校验 schema、kind、stable ID、tenant/user、payload digest、request ID 及 contract 内容；缺少恢复器或校验失败时保留 pending 并 fail-closed。其它外部状态类型仍不得复用该 envelope，除非先注册恢复器和对应故障注入测试。

| 字段 | 约束 |
|---|---|
| `schema` | 固定为 `maclaw.external-snapshot/v1`；未知版本拒绝恢复并保留 pending |
| `kind` | 由拥有状态的服务注册（当前为 `dynamic_skill_contract`）；禁止调用方自由扩展为未审计类型 |
| `stable_id` | 与 Skill 稳定身份绑定，恢复前必须再次比对当前记录和目录定义 |
| `tenant_id` / `user_id` | 必须与 `RecoveryScope` 和当前服务主体一致；任一缺失或不一致即阻断 |
| `payload_digest` | 对脱敏后的快照正文做 SHA-256；恢复前后都校验，禁止仅凭日志判断成功 |
| `pre_image` | 只保存恢复所需的最小授权状态，不保存 token、密钥或完整用户输入 |
| `captured_at` / `request_id` | 用于追踪和幂等去重；同一 request 重放不得生成第二份授权状态 |

外部恢复器必须实现“先幂等恢复外部状态，再恢复目录/config/index”的顺序，并返回结构化结果（`restored`、`already_restored`、`rejected`、`failed`）。`rejected` 和 `failed` 均保持补偿记录，不得继续执行目录删除、清理或上传。恢复完成后，必须在同一审计关联中记录 `external_restore_result`、快照 digest 和最终目录摘要；若外部状态已恢复但目录恢复失败，记录仍为 pending，下一次重试不得把已恢复的外部状态当作“未执行”而重复创建。

外部快照只解决“恢复依据持久化”，不等价于跨系统原子提交。若 contract 注册表和文件系统无法提供同一事务，发布门禁必须采用两阶段可观测语义：先持久化 intent，再标记 `external_applied=true`，最后写最终审计；任一步骤缺失都按未证明提交处理。人工处置只能依据快照 digest、stable ID、scope 和审计关联核对，不得直接编辑快照正文。

### 10.2.2 入口准入矩阵（优化后）

为了避免“某个入口有回滚”被误读为“所有入口都可以自动写盘”，发布时按入口逐项判定：

| 入口类别 | 当前允许 | 必须具备的证据 | 缺证据时处置 |
|---|---:|---|---|
| 核心 pipeline repair/optimize | 受策略控制 | 统一补偿快照、checked 索引、最终审计、回滚结果 | 仅生成 draft 或停止调度 |
| GUI 创建/更新/激活/维护 | 仅人工或灰度 | request/config_revision、补偿快照、旧摘要校验 | 保持旧版本并阻断 |
| Nudge 自动提升（`NudgePromoter`） | **禁止自动** | GUI 必须保持 `EnablePromoter=false` 且 `Promoter=nil`，不得注入直写 `SkillsDir` 后登记 config 的 registrar；只有 core 支持“staging 候选 → 共享提交器”并具备目录/config/index/audit/cleanup 失败回滚证据后才可重新评估 | 仅保留 UsageTracker 观测证据；由人工创建、扫描和审批，不生成 YAML、不创建目录、不写 config |
| RemoteSession 自动经验定义 | **默认禁止；仅显式 opt-in 后受限启用** | 每次写入重新检查 `skill_evolution_enabled` 与 `MACLAW_DISABLE_SKILL_EVOLUTION`；配置缺失/不可读即拒绝；`agent_created` 候选经非交互扫描后走共享提交器，要求补偿、checked index、最终审计和 `cleanup_status=clear` | 保留只读经验/审计；不得调用旧 `SkillExecutor.Register/Update` 直接写入 |
| craft_tool 自动注册 | **默认禁止；仅显式 opt-in 后受限启用** | `save_as_skill`/`register_policy=auto` 只表达候选意图，不构成授权；注册前必须重新检查 `skill_evolution_enabled` 与 `MACLAW_DISABLE_SKILL_EVOLUTION`。获授权时仅经共享定义提交器注册 `Source=crafted`，不得再异步写第二份目录或 config overlay | 保留本次执行生成的脚本路径和结果；不注册、不中途启动异步持久化 |
| 轨迹自动摘要（`SkillAutoSummaryPipeline`） | **默认禁止；仅显式 opt-in 后仅限新增定义** | `LLMTrajectoryLogging` 仅负责记录，不能授权写盘；在任何相似度匹配、目录创建前重新检查 `skill_evolution_enabled`、`MACLAW_DISABLE_SKILL_EVOLUTION` 与补偿队列健康。新候选只可先写入所属 App 的 `data/skills_staging`、以 `agent_created` 身份完成非交互扫描，再经 `commitAutoSummaryStagedSkillInstall` 完成目录发布、config、checked index、最终审计、补偿与 cleanup；审计 `via=automatic_auto_summary`，提交前再次复核开关与队列 | 删除 staging，不写 config/YAML/索引。已有 Skill 的自动替换当前明确 defer，保留旧定义，直到补齐“保留目录资产”的 staged replacement + 同一提交器故障注入；不得回退到 `Versioner` 写 YAML 后再 `UpdateLearnedSource` 的双写路径 |
| GUI 手工录制保存（`ResolveSkillRecording(save)`） | 人工确认 | 仅在所属 App 的 `data/skills_staging` 生成 `skill.yaml`/模板，完成手工安装扫描后经 `commitStagedSkillInstall` 发布；必须具备补偿、checked index、最终审计与 `cleanup_status=clear`，才可返回 `saved` | 删除 staging，不写 config/YAML/索引；不得调用 `UpdateLearnedSource` 作非致命的后置 overlay。`SkillOperationRecorder.Stop` 仅保留给非 App 兼容调用，GUI 绑定必须使用 `StopToDirectory` |
| Agent Skill 目录导入 | 人工确认 | 导入后扫描，再以共享提交器写 config/YAML（适用时）、刷新 checked index 并完成最终审计；索引或审计失败必须恢复注册表 | 不注册导入结果；保留原目录，返回可诊断错误 |
| reviewed-draft apply | 人工确认 | 共享提交器；draft 快照、审计预检、提交后清理 | 仅 `committed + cleanup_status=clear` 可完成；否则保留 draft 并阻断 |
| reviewed-draft disable/reject | 人工确认 | 共享提交器；config/index/audit 回滚，draft 提交后清理 | 仅 `committed + cleanup_status=clear` 完成；否则保留 draft 并阻断 |
| Hub/Enterprise/ZIP/GitHub 导入 | 人工确认 | 来源与完整性、扫描、目录 `.prev`、最终审计、发布前补偿意图；新目录及 mixed SkillMarket/Hub 已有版本替换均走同一目录提交器，并需相同摘要 no-op 与索引失败恢复旧包断言。能力市场 MCP 配置安装另行按配置事务验收，不得复用 Skill 目录证据 | 不发布 staging 目录；MCP 不写入配置 |
| 能力缺口 / IM 自动安装 | **禁止自动** | Skill 新目录与 GitHub 配置注册分支已具备共享提交器和最终审计，但已有版本更新及非桌面持久快照仍未完成；能力市场 MCP（本地 Stdio/远程 HTTP）已纳入独立配置 durable 事务，但运行时同步和完整故障注入仍未完成 | 只读提示，转人工安装；MCP 配置安装继续 fail-closed，直至补齐同步失败/恢复失败/跨重启证据 |
| 传统 GUI ZIP/插件/marketplace 安装 | **禁止自动** | `App.AddSkill`/`App.DeleteSkill` 具备 legacy durable compensation、原子 metadata、checked index、最终审计前后分层和失败保留原目录；`App.InstallSkill` 已在解压/settings 前持久化唯一外层 `legacy_gui_skill_install` 记录，并在最终审计后先标记 committed，随后先删除显式 post-commit staging/备份再清理队列；其内部复用该记录完成 metadata/package 编排，不创建嵌套 durable 记录或中间安装审计；独立 `InstallDefaultMarketplace` 使用 `legacy_gui_marketplace` settings `FileSnapshots`、post-image 围栏和严格最终审计，重复调用为 no-op；三个 `*Detailed` API 已提供统一结构化结果，管理页对安装/删除结果严格执行 `committed + cleanup_status=clear` 检查，但仍未统一共享提交器语义 | 只读或人工确认；失败保持原目录/settings 并阻断后续自动操作；迁移前拒绝已有目录覆盖 |
| AgentService GitHub/Hub/Market/ZIP 导入 | **禁止自动** | `persistImportedEntries` 已经使用共享目录事务：staging、`.prev`、批次回滚、checked 目录扫描、严格审计和提交后清理；同内容导入在有无 overwrite 标志下均为 no-op，并比较脚本/Markdown/资产 payload；覆盖更新时，旧 Skill contract 在目录发布事务内撤销，任何中途失败由回滚恢复目录并通过 durable external snapshot 恢复 contract；`NewService` 启动阶段按 action prefix + `RecoveryScope` 尝试恢复目录与 contract；执行/上传复用同一 scope-aware pending 检查；已验证 committed 清理跨重启和不同 `DataRoot` 隔离 | 恢复失败、队列不可读、scope 缺失/路径越界或仍有 pending 记录时拒绝写盘、执行和上传；已覆盖多包 cleanup 失败升级为 `needs_review` 的共享恢复语义，入口级异常矩阵仍需继续扩展。迁移完成不等于放开自动导入，仍仅人工灰度 |
| 上传/发布/重试 | 仅 `committed` 且 `cleanup_status=clear` | 补偿队列健康且目标 Skill 无 pending/needs_review/cleanup_pending | 保持 `blocked` |

矩阵中的“当前允许”是运行处置，不是目标架构的放宽。任何入口只要出现队列不可读、审计不可写、索引刷新错误、目录备份冲突、`cleanup_status!=clear` 或取消/超时，就必须 fail-closed。

#### 动态 contract 与目录事务的边界

动态 Skill contract 属于可路由授权，不是目录扫描的派生缓存。删除或覆盖更新时，contract 变化必须与目录事务建立可恢复关联：

1. 删除：先将目录移入隔离区；在最终审计前撤销 contract，撤销成功后才写 `skill.deleted`；若撤销、审计、索引或后续步骤失败，回滚目录并恢复原 contract（恢复失败则保留补偿记录并阻断执行）。
2. 覆盖更新：在目录发布事务内撤销旧 contract；任何中途失败先恢复旧目录，再恢复旧 contract；不得在事务外提前永久撤销。
3. 提交后清理：contract 已撤销且 `skill.deleted` 已审计后，清理失败只能产生 `committed + cleanup_status=pending`，不得重新发布旧 contract 或反向覆盖已审计结果。

若 contract 注册表不可用、恢复失败或无法证明与当前 Skill 的 stable ID 绑定，运行和上传均按 fail-closed 处理。日志中的“撤销成功”不构成授权变更证据，必须以注册表快照和结构化审计共同证明。

### 10.3 索引和 scan cache 的边界

索引是可由 YAML 与 config 重建的派生数据，但在一次提交期间仍必须保持与权威定义一致：刷新接口应返回错误，刷新失败就回滚新定义。核心和 GUI pipeline 已使用 checked 刷新边界；底层当前 BM25 实现不会产生错误，仍需真实可失败 provider 的集成验收。配置型补偿记录（例如 `legacy_gui_marketplace`）显式标记 `skip_index_refresh`，恢复 settings pre-image 时不调用 Skill 路由索引，避免把无目录/无 Skill 定义的配置回滚错误地绑定到索引可用性。

仅有 `refresh_index` 的 maintenance 请求没有权威定义变更，不应强行进入 config/YAML 提交器，也不应因为找不到“变化的 Skill”而失败。IM 入口对此采用 checked-only 刷新：先写入开始审计，再调用失败可传播的索引 provider，最后写完成审计；任一步不可证明时返回 fail-closed 结果，但不制造无法恢复的空补偿记录。

`writeSkillScanCacheForInstalledEntry` 生成的是扫描报告缓存，不是 Skill 定义的权威来源。GUI repair 当前有一条写入发生在事务函数返回之后的路径，因此缓存失败不会改变已提交状态，只会留下可重建的陈旧缓存；代码已将其作为非权威派生缓存，并产生结构化 `skill:scan_cache_failed` 告警。传统 GUI ZIP 安装虽发生在提交前，但现在会依据归档顶层目录仅写入本次发布的 Skill；不得遍历共享 `skills/` 根并把当前包的报告重盖到既有 Skill。仍需补充告警聚合、重建重试和“缓存不得决定 verified/active”的自动断言。可选边界为：

1. 将 scan cache 纳入提交器，失败时和 YAML/config 一起回滚；或
2. （当前采用）明确其为非权威派生缓存，提交后异步重建，缓存失败产生结构化告警，并禁止把缓存状态当作 `verified` 或 `active` 的依据。

### 10.4 提交后的外部副作用（依赖安装）

安装完成后执行 `npm install`/`pip install` 等依赖准备，属于独立的外部副作用，不能被“Skill 已提交”这一结果隐含覆盖。当前代码在目录提交后异步触发依赖安装，失败主要以日志记录；因此它不应改变 `transaction_state`，但必须单独具备任务 ID、超时、来源与完整性策略。依赖任务既不能回滚或重写已提交事务，也不能绕过执行准入：当 Skill 声明运行时依赖且任务尚未 `succeeded` 时，执行/上传必须保持阻断；不声明依赖的 Skill 不应因无关任务失败而被误阻断。

上线前应将依赖准备改为受策略控制的后置任务：默认关闭自动执行；仅在存在锁文件/固定版本和哈希校验、网络与路径白名单、隔离工作目录、资源限额及审计事件时运行。依赖任务失败或被取消时，Skill 保持 `committed + cleanup_status=clear`，同时标记 `dependency_status=failed`；若运行时需要该依赖则保持不可执行并提示人工处置。不得通过重装依赖覆盖已审计目录，也不得把依赖任务的日志成功当作 Gate 证据。

## 11. 审计与可观测性

事件至少包含时间、Skill、动作、决策、原因、风险、Gate、证据摘要、备份版本、来源、操作者、触发器和 `schema_version`；参数正文只保存脱敏摘要。建议统一增加 `request_id`、`config_revision`、`evidence_mode`、`failure_reason`、`transaction_state` 和 `cleanup_status`，避免仅凭自由文本区分失败。关键事件包括 `discovered`、`staged`、`scanned`、`verified`、`rejected`、`repaired`、`optimized`、`rollback`、`queue_full`、`cancelled`、`timed_out`、`audit_failed`、`cleanup_pending`、`compensation_recovered` 和 `compensation_needs_review`。

`EvolutionAuditHealthSnapshot` 暴露可用性、失败次数、最后错误和最后成功时间。审计不可用、队列增长或连续失败时 GUI 告警；失败摘要限制数量、长度和保留期，并提供成功率、失败率、回滚率、拒绝率和错误类别趋势。

当前 pipeline、staged 验证、YAML restore、maintenance、状态变更和 reviewed-draft apply/reject 路径已持久化 `schema_version=2`、`request_id`、`attempt`、`config_revision`、`evidence_mode`、`failure_reason`（取消路径另含 `termination`）；新目录安装分支已覆盖主要桌面事件字段，但 legacy 更新/导入路径仍需统一字段校验；审计健康快照和待恢复数量也已暴露给 GUI。`audit_pending` 已有独立 JSONL 快照、原子重写、启动恢复和 3 次失败后的 `needs_review` 降级；核心 pipeline、GUI repair、YAML restore、reviewed-draft、maintenance 及已迁移安装分支的不完整回滚均可写入同一队列。补偿记录当前使用独立的 `schema_version=1`（审计事件使用 `schema_version=2`，两者不可混用）；读取器兼容缺少 `schema_version` 的旧记录，但对显式未知版本、非法状态、负尝试次数、缺失 Skill、重复文件快照、外部 transition 无快照或 JSONL 损坏直接报错；状态、执行和上传入口随后 fail-closed，不能把不可识别记录当作已恢复。恢复文件快照会先完成路径、目标类型、父目录和 base64 内容的全量预检，再执行任何替换，避免第二个损坏快照导致第一个文件已恢复而队列仍 pending。动态 contract 的 pre-image 以 `external_snapshots` 保存在同一记录中，AgentService 启动恢复会先幂等恢复授权状态，再恢复目录；缺少外部恢复器或快照格式非法时记录保留 pending。staged 激活在事务开始前写入补偿快照，成功完成最终审计后才清理；若仅清理失败，保留记录并阻断后续操作，不能把它误判为回滚失败。`needs_review` 终态事件已要求 strict sink；恢复成功事件仍为 best-effort，不能替代队列状态。仍需将 maintenance 边缘分支、已有版本更新和导入路径逐步迁移到统一提交器，并补充损坏队列与运行时 blocker 的人工处置/终态体验。

补偿恢复会追加 `skill:compensation_recovered` 或 `skill:compensation_needs_review` 事件；恢复成功事件可 best-effort 记录，但不能替代队列快照，也不能把恢复失败伪装成成功。进入终态 `needs_review` 时，事件必须经 strict sink 写入；strict sink 失败应向调用方返回错误，同时保留已持久化的 `needs_review` 队列记录和 fail-closed 门禁。队列文件不可读时，状态接口显示 `compensation_queue_healthy=false` 和错误摘要，执行/上传入口继续 fail-closed。`RetryBlocked` 也属于上传入口：它只能在队列可读且目标 Skill 没有待恢复补偿时把条目移回 `pending`；补偿仍存在时条目保持 `blocked`，不得通过手动重试绕过门禁。

### 11.1 事件字段最小集

| 场景 | 必填关联字段 | 结果字段 |
|---|---|---|
| pipeline repair/optimize | `request_id`、`attempt`、`skill`、`config_revision` | `decision`、`gate_status`、`evidence_mode`、`failure_reason` |
| staged 激活 | 上述字段 + `verification_run_id`/`verification_digest` | `status`、`backup_version`、`rollback_complete`、`cleanup_status` |
| 人工状态/维护动作 | `request_id`（无异步请求时生成）、`skill`、`config_revision` | `via`、`decision`、`reason`、`failure_reason` |
| 取消/超时/shutdown | 上述字段 | `termination` 必须分别为 `operator_cancelled`、`worker_timeout`、`shutdown` |
| 提交后清理 | `request_id`、`skill`、`config_revision` | `cleanup_status`、`failure_reason`、`retry_at` |

字段缺失时，读取器显示 `unknown`；写入关键状态前，缺少 `request_id` 或 `config_revision` 应直接拒绝，而不是生成无法关联的“成功”记录。

## 12. 重启、扫描与配置 overlay 一致性

重启验收必须验证：

1. staged Skill 写入 verification metadata 后，新建 `SkillExecutor`，执行 `loadSkills()` 和 `scanSkillYAMLFiles()`，状态、来源、digest 和验证记录保持一致；
2. 配置 overlay 只能补充运行统计和治理标签，不能把 YAML 的 `staged` 覆盖成 `active`；
3. YAML、内存、索引任一版本不一致时，启动阶段回退到安全状态（通常为 `staged` 或 `needs_review`），并产生日志和审计事件；
4. 重启不会重复消费已完成的 repair request，也不会重新触发冷却期内的自动修复。

已补充真实文件重启/重新扫描和 overlay 防提升回归测试；仍建议在发布验收中覆盖多版本 YAML、索引损坏和升级迁移场景。补偿队列当前版本使用 `schema_version=1`：未知字段保持兼容，缺失版本号的历史记录可由 `MigrateEvolutionCompensationQueue` 原子规范化为当前 v1 表示；迁移只处理结构完整且可验证的旧记录，显式不支持的版本、非法状态或 JSONL 损坏不会被覆盖，队列健康检查仍 fail-closed。检测到损坏队列时，读取器会保留 canonical 阻断文件，并额外生成限频的本地 forensic copy/reason 文件，便于人工修复且不因隔离动作误放行；可解析记录已提供 GUI 显式 retry/clear，模型和只读接口不得触发恢复或编辑快照；版本不支持或 JSONL 损坏时仍必须离线人工修复 canonical 队列。`cleanup_status=pending` 的记录必须跨重启保留并幂等重试，不得因进程重启被误判为已清理。HubCenter 候选列表也遵循同一 overlay 边界：首次写配置时磁盘字段为 `nil`，允许建立用户提供的候选；一旦后端已持久化候选，普通整配置保存不得用旧前端快照覆盖该列表，避免故障转移状态回退。

## 13. 非 Bash mock/replay 边界

`craft_tool`、MCP、浏览器、poll/loop 等非 Bash 动作只有在存在显式隔离适配器时才允许 replay。适配器输入必须包含脱敏参数、预期副作用及允许的文件/网络范围，输出必须包含 `passed|failed|unverified` 和 `evidence_mode=real|mock|none`。默认无适配器时为 `unverified`；mock 不能写生产文件、改变 Skill 状态或触发上传/发布。

适配器接口及其边界测试已落地，真实隔离 adapter 属于 P2 工作。在生产 adapter 落地前，`mock` 只能用于观察和草案生成，不能触发 config/YAML 写盘、状态激活、上传或发布。

## 14. 测试与验收

已覆盖：风险安装确认、无效定义不注册、Gate 三态、真实参数 staged 验证、激活旁路拦截、staged 隔离、队列合并/串并行、审计健康/失败摘要/等待时间、配置开关、基础写盘回滚、核心 pipeline 的 context cancel/timeout/shutdown 事件、repair 失败计数上限、request-level 状态、非 Bash mock/real 边界，以及 staged 重启/扫描和 overlay 防提升。提交器定向测试还覆盖了 GUI 创建/更新、reviewed-draft apply/disable/reject 和 staged 激活的提交成功、索引失败回滚、最终审计失败与提交后清理状态；reviewed-draft 三条路径均覆盖 cleanup failure 的 `committed + cleanup_status=pending` 与后续恢复；Hub 安装新增同版本 no-op 回归（目录、`.prev` 和注册表保持不变）；能力市场 MCP 已有配置事务主路径、no-op、审计/摘要脱敏和配置快照跨重启恢复测试；新增回滚清理失败不误标 `committed` 的状态隔离测试，且补偿恢复现会在任何目录/YAML/文件快照变更前完成只读载荷与目标预检；MCP runtime 新增受控 `RetryMCPRuntimeSync(serverID, confirm)` 的确认门禁、远程 strict probe 与本地 checked sync 回归；持续权限拒绝、远程指标和完整跨重启异常矩阵仍不足；其它 Skill 安装入口仍只有代表性目录/索引边界测试，均不能替代全量安装矩阵。

上线前必须补齐：maintenance 剩余分支、已有版本更新/传统 GUI GitHub 导入等 legacy adapter，以及传统 GUI `App.InstallSkill` 接入共享提交器；`App.AddSkill`/`App.DeleteSkill` 的 `*Detailed` 结果已落地，仍需更完整的入口级故障注入、清理失败/跨重启断言和 Wails 前端异常分支验证；`App.InstallSkill` 的 settings 写入失败、跨重启回滚以及恢复器损坏/目标形状不合法时的 fail-closed 阻断已有断言，但持续权限拒绝和更多入口级故障矩阵仍待补齐；GUI reviewed-draft apply/disable/reject 的 cleanup failure、跨重启幂等清理和入口级注入已有回归，仍需补清理重试耗尽后的 `needs_review`、最终审计失败和前端异常分支；YAML 版本恢复、新目录安装和 AgentService 导入仍需补齐各自的 cleanup 失败与跨重启入口矩阵；`RenameNLSkill`/`DeleteNLSkill` 已覆盖最终审计失败回滚、索引失败回滚及（删除路径）queue-clear failure 的缓存失效与跨重启清理，剩余缺口是 Windows 文件锁、持续清理失败和完整入口矩阵；AgentService 的基本启动恢复、scope 隔离和跨重启清理已有回归，已覆盖多包清理再次失败后的 `needs_review` 降级；仍需扩展入口级异常恢复矩阵；补偿队列的跨版本迁移、损坏检测和损坏队列离线人工处置闭环（可解析记录已有 GUI retry/clear，但模型仍不得触发恢复或编辑快照）；MCP runtime 的远程指标、持续权限拒绝和其它入口级恢复矩阵仍待补齐；恢复成功事件的可靠性与字段统一仍需补强，`needs_review` 终态事件已经使用 strict sink；非-pipeline 审计字段统一校验；审计不可写时阻止/回滚；GUI 终态结果和回滚原因；多版本升级迁移及索引损坏恢复。

当前可通过 `manage_skill(action="evolution_compensations")` 或 GUI Wails `ListSkillEvolutionCompensations()` 查看脱敏队列摘要；该接口只读，不允许模型直接恢复或编辑快照。GUI 对可解析记录提供带二次确认的 `RetrySkillEvolutionCompensation()`/`ClearSkillEvolutionCompensation()`，后端仍强制三元身份匹配、状态和清理目标校验；启动恢复继续负责无人值守的 bounded replay。Hub 与 AgentService 等已迁移的目录入口在最终审计之后若清理失败，会以 `committed + cleanup_status=pending` 返回；AgentService 还会在 `NewService` 启动阶段按其 action prefix 执行恢复，失败或 pending 时保持导入 fail-closed。传统 GUI ZIP 导入仍保留入口级解压/注册编排：`App.AddSkill`/`App.DeleteSkill`/`App.InstallSkill` 的 legacy 记录可跨重启恢复，并在恢复时重建 checked routing index；其中 InstallSkill 只保证外层记录的恢复顺序，内部 legacy 审计事件不构成独立提交。三个 `*Detailed` API 现在提供 `state`、`cleanup_status`、`request_id`、`failure_reason` 和 `rollback_complete` 结构化视图，但仍不等同共享 `SkillCommitter` 的提交承诺；兼容 error-only API 也不应把 `nil` 解释为统一 `committed + cleanup_status=clear`。

建议新增以下定向测试，并把它们绑定到对应审计断言：

| 测试 | 必须断言 |
|---|---|
| YAML 写回失败 | config 恢复；无 `skill:optimized`；无 upload |
| config 保存失败 | YAML 恢复；内存/索引保持旧 digest |
| 索引刷新失败 | YAML、config、内存全部恢复；产生 `rolled_back` |
| 最终审计失败且可恢复 | 状态恢复；产生 rollback 事件 |
| 最终审计失败且不可恢复 | 状态为 `needs_review`/`audit_pending`；禁止执行和上传 |
| 提交成功但清理失败 | 保持新版本但 `cleanup_status=pending`；禁止执行、上传和下一次自动写盘，直到幂等清理或人工处置完成 |
| 安装 no-op（摘要相同） | 新目录共享提交器返回 `skipped/already_current`；目录、config、index 和审计版本均不变；legacy 更新/导入入口逐项补齐同等断言 |
| 目标目录或 `.prev` 冲突 | 拒绝发布并保留原目录；不得删除冲突物，进入人工处置 |
| 传统 GUI ZIP/插件安装失败 | 解压、settings 或 `metadata.json` 任一步失败时保持原目录/配置；删除包失败不得写入新 metadata；无 `committed` 成功事件或 upload |
| 能力市场 MCP 配置写入/同步失败 | 配置文件写入已受 `commitMarketplaceMCPConfig` durable 边界保护；PatchConfig、严格审计或快照恢复失败必须回滚或保留 pending 补偿，运行时管理器同步失败不得伪报全链路成功，继续按人工核对和 fail-closed 处置 |
| AgentService 多包中途发布失败 | 已发布的前序包一并回滚；原 `.prev` 恢复；没有任何 `committed` 成功审计 |
| AgentService 导入最终审计/清理失败 | 审计失败恢复目录及旧 contract；清理失败保持 `committed + cleanup_status=pending` 且拒绝后续自动写盘；已增加注入审计失败后目录回滚回归；批量 contract 恢复会继续尝试其它 stable ID，失败项仍保持 external snapshot `audit_pending`，不得因一个失败而放弃可恢复 sibling |
| AgentService 导入跨重启 | `NewService` 已调用 AgentService 专用恢复回调；prepared/committed/cleanup 记录必须在恢复成功后清理，恢复失败、队列不可读、scope 缺失或目录越界时保持 fail-closed；运行/上传门禁使用同一 `RecoveryScope`；删除隔离目录也使用 `agentservice_install_delete` action 并可在重启后恢复；动态 contract pre-image 已纳入补偿恢复；恢复先校验全量外部快照，再按稳定顺序尝试所有 contract，单项 provider 失败不阻断可验证 sibling 的恢复，记录仍保持 pending；已覆盖 committed 清理跨重启、不同 `DataRoot` 隔离和 contract 快照跨重启回归；仍需补最终审计失败/清理失败的多包异常矩阵 |
| 取消发生在写盘前/后 | 前者零写盘；后者完成回滚或补偿 |
| 重启后重复请求 | 已完成 request 不重复消费；冷却期不重复调用 LLM |

### 14.1 验收门槛（可量化）

发布前至少满足：

- 未经确认的 high/critical 安装：`0`；
- `gate.status=passed` 但缺真实证据：`0`；
- `staged`、`unverified`、`needs_review` 进入普通执行路由：`0`；
- 取消、超时或 shutdown 后发生 Apply：`0`；
- `state=committed` 但 `cleanup_status!=clear` 时发生执行、上传或下一次自动写盘：`0`；
- 关键写盘无最终审计事件：`0`；
- 同一请求重复消费：`0`；
- 单 Skill 连续失败超过 `SelfRepairMaxAttempts` 仍自动调用 LLM：`0`；
- 回滚后 YAML、config、内存和索引摘要不一致：`0`；
- YAML 写回失败后仍发送 `skill:optimized` 或触发 upload：`0`。

每项都应有自动化回归测试和一条可检索的审计证据；只有日志截图而没有断言，不视为通过。

建议命令（按风险从低到高分层执行）：

```powershell
go test ./corelib/skill -count=1 -timeout 180s
go test ./gui -run 'TestPatchConfigFieldsSkillEvolution|TestSkillRunnerPersistRepairResult|TestSkillRunnerScanRepairedSkill|TestSkillRunnerBlockedRepair' -vet=off -count=1 -timeout 120s
go test ./tui/... -count=1 -timeout 180s
go run scripts/check_wails_bindings.go
git diff --check
```

GUI 全量测试还可能受 Windows 浮动窗口、资源架构文件和工作区并行测试负载影响；若全量命令失败，应区分环境失败与本变更回归，不得直接把环境失败写成产品结论。此前阻断 GUI 编译的 MaClaw Hub WebSearch 标识/超时常量缺失已补齐，并由 `go test ./corelib/websearch` 回归覆盖。

### 14.2 本次评审的验证证据

评审基线环境中已有的验证证据包括：

- GUI 定向 SkillRunner、repair-draft、状态与维护测试（`-vet=off`）；
- `go test ./gui -run '^TestPersistRepairResultRollsBackOn(IndexRefreshFailure|FinalAuditFailure)$' -count=1 -vet=off`：验证 repair 在索引/最终审计失败且回滚索引再次失败时保持 `audit_pending`/`cleanup_status=pending`，同时在返回错误中保留原始失败阶段（不被 `rollback_cleanup_failed` 覆盖）；
- `go test ./gui -run 'TestApplySkillRepairDraft|TestRejectSkillRepairDraft' -count=1 -vet=off`：验证 reviewed-draft apply/reject 的共享提交器最终边界在 App `installMutex` 下串行执行，并在配置写入失败时返回明确的“save config failed / rolled back”诊断；
- `go test ./gui -run '^TestSkillRunnerStartRunDoesNotWaitForExecutorMutationLock$' -count=1 -vet=off`：预热一次性 memory/SQLite 后验证持有 `skillListMutateMu` 不会阻塞独立 Skill 启动，并在 Windows 测试清理阶段释放资源；
- `go run scripts/check_wails_bindings.go`（17 个动态前端引用、1240 个生成绑定均有对应 App 方法）；
- 本文件的 fenced-code 配对检查和 `git diff --check`。

本轮新增/复核的提交器证据：

- `go test ./corelib/skill -run 'TestSkillCommitter_|Test.*Compensation' -count=1 -timeout 240s`：覆盖提交成功、正向/回滚索引回调分离、最终审计失败恢复、已提交但清理待办的重启语义，以及 cleanup 重试耗尽后保持 `committed + cleanup_status=needs_review` 且不再自动重试；
- `go test ./corelib/skill -run '^TestEvolutionPipelineMutationBoundaryRechecksAdmission$' -count=1 -vet=off`：验证平台注入的 `MutationMutex`/`MutationAdmission` 会在锁前、锁后各检查一次，且 config/YAML/index/audit 回调均在事务锁内执行。
- `go test ./corelib/skill -run '^TestSkillCommitterRollbackRestoresDefinitionSidecarSnapshot$' -count=1 -vet=off`：验证共享提交器在索引失败回滚时同时恢复 YAML 与 definition writer 写入的 `.patches.json` sidecar；
- `go test ./gui -run '^TestIMPatchTextUsesSharedCommitter$' -count=1 -vet=off`：验证 IM text/structured patch 的解析、config 身份保留、YAML/patch history 写入、严格审计及补偿清理；
- `go test ./gui -run '^TestIMPatchRollsBackWhenCheckedIndexRefreshFails$' -count=1 -vet=off`：故障注入 checked 索引失败，验证 IM patch 同步恢复 YAML、`.patches.json` 和派生索引，不遗留补偿快照。
- `go test ./corelib/skill -run '^TestMarkEvolutionCompensationCleanupFailure' -count=1 -vet=off -timeout 1200s`：直接验证提交后清理 helper 的首次失败计数、第三次 `needs_review` 升级、审计事件和持久化失败返回；共享提交器首个清理失败不会丢失 attempt 计数。
- `go test ./gui -run '^TestMaclawAppDependencyUpdatePreservesSkillMarketSource$' -count=1 -vet=off -timeout 1200s` 还覆盖 `.prev` 陈旧备份冲突：发现旧备份时更新 fail-closed，只有人工移除冲突后才允许继续发布。
- `go test ./corelib/skill -run '^TestCompensationRecoveryUsesDurableFinalAuditMarker$' -count=1 -vet=off -timeout 240s`：验证严格最终审计已落盘但 `transaction_state` 尚未持久化时，恢复逻辑依据 `request_id + skill + action + final_audit_kind` 识别已提交状态，只执行清理而不回滚业务文件。
- `go test ./corelib/skill -run '^TestReplaceEvolutionCompensationKeepsOneAuthoritativeSnapshot$' -count=1 -vet=off`：验证已持久化补偿记录的状态变更会替换原行，而不是追加重复 live snapshot。
- `go test ./corelib/skill -run '^TestEvolutionCompensationReadModifyWriteIsSerialized$' -count=1 -vet=off -timeout 120s`：并发执行 append/replace，验证队列读-改-写不会因旧快照覆盖而丢失其它事务记录。
- `go test ./corelib/tool -count=1 -timeout 180s`：覆盖 checked index provider 的错误传播；
- `go test ./corelib/skill ./corelib/agentservice -run 'Test(PersistImportedEntries|InstallSkill|SkillCommitter_|EvolutionCompensation)' -count=1 -vet=off -timeout 300s`：覆盖 AgentService 单包/多包导入、同内容 no-op、payload 差异和陈旧 `.prev` 拒绝，以及共享提交器/补偿的定向组合边界；
- `go test ./corelib/agentservice -run 'TestPersistImportedEntries(SamePackageIsNoOpWithoutOverwrite|BatchPublishFailureRollsBackEarlierPackages|CleanupFailureKeepsCommittedPending)' -count=1 -vet=off -timeout 600s`：验证无 overwrite 幂等重试、批量第二个包发布失败时回滚首包，以及提交后清理失败保留 `committed + cleanup_status=pending`。
- `go test ./corelib/agentservice -run 'Test(AgentSkillPackageRoundTripPreservesDefinitionFields|PersistImportedEntriesChangedPackagePayloadRequiresOverwrite)' -count=1 -vet=off -timeout 600s`：验证导入 round-trip 不丢失 `id/version/requires/params/stateful/pipeline/produces_artifact` 等执行契约，并在 YAML 相同但脚本/资产 payload 改变时拒绝 `overwrite=false` 的更新。
- `go test ./corelib/agentservice -run 'Test(NewServiceCommittedCleanupAcrossRestartKeepsPublishedDirectory|AgentServiceRecoveryIsolatedByDataRoot)' -count=1 -vet=off -timeout 300s`：验证 AgentService 重启后对 `committed` 记录只执行幂等清理而不回滚新目录，并确认不同 `DataRoot` 的补偿记录不会被相互领取或删除。
- `go test ./corelib/skill ./corelib/agentservice -run 'Test(RecoverPendingCompensationsScopedByServiceRoot|RecoverPendingCompensationsScopeRejectsOutOfRootPaths|NewServiceRecoversAgentSkillDirectoryCompensation|AgentServiceRuntimeAndUploadBlockOnScopedCompensation|AgentServiceDeleteSkillUsesQuarantineTransaction)' -count=1 -vet=off -timeout 420s`：覆盖 AgentService 启动恢复的服务 scope 隔离、scope 与目录路径不一致时的拒绝、运行/上传门禁和删除隔离事务；
- `go test ./tui -run 'TestSkillDirSnapshot' -count=1 -vet=off`：验证 TUI 上传快照 helper 拒绝符号链接；`go test ./tui -run '^Test(TUIValidation|TUIUploadTransaction)' -count=1 -vet=off`：验证 `validate(auto_fix=true)` 和上传预检的 durable compensation 在重启时回滚未完成本地变更，远端已返回 submission ID 时保留本地目录并保持 pending blocker，最终审计已落盘时只执行提交后清理；`go test ./tui -count=1 -vet=off -timeout 900s` 复核 TUI 全量回归。
- `go test ./tui -run 'TestZipDirectoryTUIRejects' -count=1 -vet=off`：验证 TUI ZIP 打包在根目录或子项为符号链接时均 fail-closed。
- `go test ./tui -run '^TestCommitTUIPatch' -count=1 -vet=off`：验证 TUI patch 的定义 compare-and-swap、稳定名称校验、损坏 history 阻断和并发覆盖保护。
- `go test ./corelib/agentservice -run 'TestNewServiceRecoversAgentSkillDeleteQuarantine' -count=1 -vet=off -timeout 180s`：覆盖删除隔离目录在进程崩溃后由 `NewService` 启动恢复；
- `go test ./corelib/agentservice -run 'TestZipDirectoryBytesRejects' -count=1 -vet=off`：验证 AgentService 导出/上传 ZIP 拒绝符号链接根及子项，避免跟随外部路径。
- `go test ./corelib/skill -run 'TestRecoverPendingCompensationsWithExternalSnapshotFailsClosedWithoutRestorer' -count=1 -vet=off -timeout 240s`：验证带 external snapshot 但无恢复器时不触碰目录、记录保持 pending，防止缺少外部恢复能力却误删/误恢复；
- `go test ./corelib/skill -run '^TestRecoverExternalCompensationExhaustionAuditsNeedsReview$' -count=1 -vet=off -timeout 240s`：验证 external contract 恢复连续失败达到上限时持久化 `needs_review` 并写入 strict 审计事件，队列记录继续保留以阻断后续操作。
- `go test ./corelib/skill -run '^TestRecoverPersistsFailureBeforeProcessingQueueTail$' -count=1 -vet=off -timeout 240s`：模拟恢复处理首条记录后进程崩溃，验证失败 attempts 与尚未处理的队列尾部均已先行持久化。
- `go test ./corelib/skill -run 'TestPrepareSkillForUpload_(GeneratesManifestWhenPortable|ManifestWriteFailureBlocksPortableResult|NoManifestWhenNotPortable)' -count=1 -vet=off -timeout 300s`：验证便携上传候选必须成功写入完整性 manifest；manifest 生成/写入失败时 fail-closed，不返回可上传结果。
- `go test ./corelib/skill -run 'Test(GenerateAndVerifyPackageManifest|VerifyPackageIntegrity)' -count=1 -vet=off`：验证完整性 manifest 读取/校验拒绝空清单、绝对或越界路径、符号链接、重复规范化路径和未登记文件；生成的 manifest 仍允许运行时缓存目录并可正常回验。
- `go test ./corelib/agentservice -run '^TestImproveSkillAuditFailureRollsBackAutoFix$' -count=1 -vet=off -timeout 300s`：验证 AgentService 自动修复后的最终审计失败会恢复 Skill 目录 pre-image，不再吞掉审计错误或留下未审计改写。
- `go test ./corelib/agentservice -run 'Test(UploadSkillAuditFailureReturnsSubmittedResultAndError|ImproveSkillAuditFailureRollsBackAutoFix)' -count=1 -vet=off -timeout 300s`：验证上传已跨远端边界后，本地状态/最终审计失败会返回提交凭据与显式错误；自动修复审计失败会回滚目录。
- `go test ./corelib/agentservice -run '^TestUploadSkillRollsBackAutoFixWhenSubmitFails$' -count=1 -vet=off -timeout 600s`：验证上传预检自动修复后远端提交失败会恢复目录 pre-image，并清理 `.bak`，不会把半成品留给下一次重试。
- `go test ./corelib/agentservice -run '^TestPersistImportedEntriesRejectsCaseOnlyDuplicateNamesBeforeWriting$' -count=1 -vet=off -timeout 600s`：验证多包导入在 staging 前拒绝大小写重复身份，不产生安装目录或残留 staging。
- `go test ./gui -run 'TestSkillLifecycleUploadNow|TestPrepareSkillDirForMarket' -count=1 -vet=off -timeout 900s`：复核 GUI 生命周期预检在自动修复/安全扫描失败时 fail-closed，并验证正常上传质量门禁不回归。
- `go test ./gui -run '^TestSkillLifecycleUploadDirChecksAdmissionBeforePortabilityAutofix$' -count=1 -vet=off -timeout 600s`：验证目录上传在 `prepareSkillDirForMarket` 可能改写 `skill.yaml` 前先执行补偿队列准入；队列不可读时零写盘并保持 fail-closed。
- `go test ./gui -run 'TestSkillLifecycleRetryBlocked(AllKeepsPendingSkillBlocked|MovesItemsPending|RespectsEvolutionCompensation|KeepsUnreadySkillBlocked|RequeuesRepairedSkill)' -count=1 -vet=off`：验证上传队列手动 retry（含 retry-all）逐项执行 Skill compensation admission；有待恢复记录的条目保持 `blocked`，不会被 portability auto-fix/quality 写入或移回 `pending`，其它条目仍可独立重试。
- `go test ./gui -run '^TestSkillExecutorCompatibilityMutationsBlockOnUnreadableCompensationQueue$' -count=1 -vet=off`：验证遗留 `SkillExecutor.Register/Update/UpdateStatus` 兼容写入口在补偿队列损坏时零写盘并返回显式阻断，而非绕过 App-owned lifecycle admission。
- `go test ./gui -run '^TestNormalizeInstalledSkillEntryBlocksOnUnreadableCompensationQueue$' -count=1 -vet=off`：验证 load/导入后的 portability normalization 与 `quality_status.json` 写入在队列不可读时被阻断，权威 `skill.yaml` 保持不变。
- `go test ./gui -run '^TestSkillExecutorDeleteRemovesExternalSkillDirs$' -count=1 -vet=off`：验证 scanner 冷缓存或无可运行步骤时，兼容删除入口仍能发现 external Skill 并通过 `App.DeleteNLSkill` 的 durable quarantine/审计边界完成删除。
- `go test ./gui -run '^TestToolExecuteSkillMaintenancePlanReviewAuditFailureIsNotReportedOK$' -count=1 -vet=off`：注入 review-trace 审计写入失败，验证 maintenance 即使业务状态已 `committed` 也返回 `ok=false`、`failure_reason=review_execution_audit_failed`，且不触发后续自动 repair。
- `go test ./gui -run '^TestAuditInstalledSkillQualityBlocksBeforePortabilityAutofixOnUnreadableQueue$' -count=1 -vet=off -timeout 600s`：验证质量审计/规范化入口在自动修复和 `quality_status.json` 写入前检查补偿队列；队列不可读时不改写 YAML、不写质量状态。
- `go test ./gui -run '^TestPrepareUploadSourceWithCheckedIndexRollsBack$' -count=1 -vet=off`：验证生命周期上传对权威源目录自动修复后执行 checked 索引发布；索引失败时目录与派生索引同步恢复并阻断上传。
- `go test ./gui -run '^TestOneClickSkillMarketUploadHonorsEvolutionAdmission$' -count=1 -vet=off`：验证 App Studio 一键 SkillMarket 上传同样检查补偿队列/Skill 准入，待恢复记录存在时在网络提交前 fail-closed。
- `go test ./gui -run '^TestOneClickAppPackUploadFailsClosedOnUnreadableCompensationQueue$' -count=1 -vet=off`：验证生成式 App pack 上传在补偿队列损坏/不可读时同样在网络边界前阻断。
- `go test ./corelib/skill -run '^TestEnsureSkillIDBeforeUpload' -count=1 -vet=off` 与 `go test ./gui -run '^TestEnsureSkillIDForUploadRollsBackOnCheckedIndexFailure$' -count=1 -vet=off`：验证上传前自动生成 `skill_id` 时，定义写入失败会 fail-closed 并恢复内存身份；checked 索引发布失败会同步回滚 YAML 与索引，不保留仅内存或半提交身份。
- `go test ./corelib/skill -run '^TestMarkCompensationNeedsReviewDemotesAllAffectedSkills$' -count=1 -vet=off`：验证批次补偿升级 `needs_review` 时会同时降级全部 `AffectedSkills`，不会因首个 Skill 已处于 `needs_review` 而提前返回、遗漏其它成员。
- `go test ./corelib/skill -run '^TestSummarizeEvolutionCompensationErrorRedactsPaths$' -count=1 -vet=off`：验证队列读取/恢复错误通过 Wails、TUI 或 IM 暴露前会去除绝对路径等本地敏感诊断。
- `go test ./corelib/agentservice -run 'TestAgentService(DeleteSkillAuditFailureRestoresDirectory|ImportAuditFailureRollsBackPublishedDirectory)' -count=1 -vet=off -timeout 240s`：覆盖删除与导入最终审计失败时恢复原目录，禁止留下半提交目录；
- `go test ./corelib/agentservice -run '^TestValidateAgentSkillRootRejectsMalformedVisibleSkill$' -count=1 -vet=off -timeout 300s`：验证 AgentService 回滚后的 registry 重建不会沿用 best-effort scanner 静默跳过损坏的可见 Skill；隐藏 staging 与 `.prev` 目录仍按非权威产物处理。
- `go test ./corelib/agentservice -run 'TestAgentServiceDeleteSkillContractRevokeFailureLeavesContractAndDirectory' -count=1 -vet=off -timeout 240s`：故障注入 contract 撤销失败，确认删除事务不写“已删除”审计、原目录保持可见且 contract 仍可解析；
- `go test ./corelib/agentservice -run 'TestAgentServiceImportContractRevokeFailureRollsBackDirectoryAndContract' -count=1 -vet=off -timeout 240s`：覆盖覆盖导入在 contract 撤销失败时恢复旧目录和旧 contract，防止提前撤销造成不可逆漂移；
- `go test ./corelib/agentservice -run 'TestAgentServiceImportAuditFailureContractRestoreFailureStaysPending' -count=1 -vet=off -timeout 240s`：覆盖导入最终审计失败且 contract 恢复再次失败时保留 `audit_pending` durable 记录，并确认旧目录仍被恢复；
- `go test ./corelib/agentservice -run 'TestAgentServiceBatchImportAuditFailureRestoresSiblingContractsAfterOneRestoreFailure' -count=1 -vet=off -timeout 240s`：覆盖批量更新最终审计失败时，一个 contract 恢复失败不会短路其它 stable ID 的恢复；失败项仍保持 `audit_pending` 并 fail-closed，已恢复 sibling 不会被一并遗留为 revoked。
- `go test ./corelib/agentservice -run 'TestRestoreSkillExternalCompensationContinuesAfterOneContractFailure' -count=1 -vet=off -timeout 240s`：覆盖启动/人工恢复使用的 external snapshot 回调先完成全量 schema/scope/digest 校验，再在一个 provider 恢复失败时继续发布其它合法 contract；返回聚合错误以保留 pending 门禁。
- `go test ./corelib/agentservice -run 'TestRestoreSkillExternalCompensationValidatesAllSnapshotsBeforePublishing' -count=1 -vet=off -timeout 240s`：验证同一补偿记录中任一 external snapshot 结构损坏时，不会因 map 迭代顺序先发布其它合法 contract；整条记录保持 fail-closed，等待人工/离线处置。
- `go test ./corelib/agentservice -run 'TestAgentServiceDeleteSkillAuditFailureContractRestoreFailureStaysPending' -count=1 -vet=off -timeout 240s`：覆盖最终审计失败且 contract 恢复器再次失败时保留 durable pending，并阻断后续执行/上传；
- `go test ./corelib/agentservice -run 'TestNewServiceRecoversAgentSkillContractSnapshot' -count=1 -vet=off -timeout 240s`：覆盖动态 contract pre-image 随 AgentService 补偿记录跨重启恢复，并验证恢复后队列清空；
- `go test ./gui -run '^TestInstallManagedHubSkillIndexFailureRestoresPreviousVersion$' -vet=off -count=1 -timeout 300s`：覆盖已有目录更新在索引失败时恢复旧版本；
- `go test ./gui -run '^TestInstallManagedHubSkillIndexFailureRestoresPreviousVersion$' -vet=off -count=5 -timeout 1200s`：复核 Windows 文件占用/扫描竞态下 `.prev` 回滚的最终后置条件；每次均确认旧目录恢复且 `.prev` 已消失。
- `go test ./corelib/skill -run 'TestRestoreEvolutionCompensation(PostImageFence|PreflightsCorruptFileSnapshots|PreflightsCorruptYAMLBeforeDirectoryMutation)' -vet=off -count=1 -timeout 1200s`：验证文件快照 post-image 匹配时允许恢复，文件被并发修改、删除或摘要不匹配时 fail-closed 且不覆盖新写入；恢复前会预检损坏的文件/YAML 载荷，确保目录不会先被删除再因解码失败而留下半回滚。
- `go test ./corelib/skill -run 'TestRestoreEvolutionCompensation(RemovesAllNewMultiPackageDirectories|MixedExistingAndNewMultiPackageDirectories|ProtectsExistingPathInCreatedDirs|DoesNotDeleteUnpublishedIntentTarget|RejectsRelativeDurablePaths)' -vet=off -count=1`：验证多包补偿同时携带 `DirectoryMoves` 与 `CreatedDirs` 时，所有已发布的新目录都会清理、已有目录从 `.prev` 恢复且不会被重复删除；仅有预发布 intent 时不误删可能由并发方创建的目标，且拒绝相对路径目标。
- `go test ./corelib/skill -run '^TestRestoreEvolutionCompensationCanSkipIndexForConfigOnlyRecord$' -vet=off -count=1`：验证配置型补偿恢复显式跳过 Skill 索引刷新，不把 marketplace settings 回滚错误绑定到目录索引。
- `go test ./gui -run '^TestLocalMCPManagerSyncFromConfigChecked(ReportsStartupFailure|ContextHonorsCancellation)$' -vet=off -count=1 -timeout 1200s`：验证本地 MCP 启动失败与调用方取消会通过 checked 同步边界显式返回，取消会贯穿进程启动、initialize 和 tools/list，失败/取消过程不会被标记为运行中；后台/启动兼容入口仍保留 best-effort 行为。
- `go test ./gui -run 'TestCommitMarketplaceMCPConfig(AuditAndSummaryDoNotExposeSecrets|RecoveryAcrossRestartRestoresPreImage)' -vet=off -count=1 -timeout 1200s`：验证能力市场 MCP 配置审计/摘要不泄露 endpoint、AuthSecret、headers，并验证配置快照在模拟重启后可恢复且队列清理完成。
- `go test ./gui -run '^TestReconcileManagedMCPRuntimeSync(LocalFailureDoesNotDoubleCount|OnStartupRetriesDueHTTPState)$' -vet=off -count=1`：验证启动恢复对本地 managed MCP 的失败状态由 `LocalMCPManager` 按实例记录，协调器不会对同一失败重复递增 attempts，避免误触发 `needs_review`。
- `go test ./gui -run '^TestFinalizeLocalMCPRuntimeSyncPreservesTerminalState$' -vet=off -count=1`：验证启动目标列表过期时不会把较新的 `needs_review`/`ready` 状态覆盖为 ready；只有实际仍运行的本地进程才可完成 ready 标记。
- `go test ./gui -run '^TestLocalMCPManagerReadinessPersistenceFailureIsReturned$' -vet=off -count=1`：验证本地 MCP readiness 状态写入失败会向 checked 调用方返回错误，而不是仅记录日志后报告成功。
- `go test ./corelib/skill -run 'TestMarkEvolutionCompensation(RollbackFailure|CleanupFailure)' -vet=off -count=1 -timeout 1200s`：验证回滚阶段的队列清理失败保持 `transaction_state=audit_pending`（不越过已提交边界），重试达到上限后进入 `needs_review`；提交后清理失败仍保持 `committed + cleanup_status=needs_review`，两条状态路径互不混淆。
- `go test ./corelib/skill -run '^TestSkillCommitter_RollbackCleanupFailureUsesBoundedRetryState$' -vet=off -count=1 -timeout 300s`：验证共享 `SkillCommitter` 在回滚后清理队列失败时也会递增 `attempts` 并保留 `rolled_back + cleanup_status=pending`，不再静默覆盖为零次重试。
- `go test ./gui -run '^(TestMCPRuntimeSync|TestRetryMCPRuntimeSync|TestCheckMCPServerHealthManaged)' -vet=off -count=1 -timeout 1200s`：验证 MCP runtime sync 失败持久化到独立非敏感状态文件，三次失败升级 `needs_review`、不再自动重试，状态损坏时 fail-closed，成功 checked 探测后清除阻断；人工 retry 缺少确认时拒绝，确认后会重置指定托管远程实例并完成 strict probe。
- `node_modules/.bin/vitest.cmd run src/components/remote/__tests__/MCPManagementPanelMarketplace.test.tsx`：验证受管 marketplace MCP 出现 runtime blocker 时，管理页只有在二次确认后才调用 `RetryMCPRuntimeSync(serverID, true)`。
- `go test ./gui -run '^TestMCPRuntimeSyncPendingResetStartsFreshBoundedCycle$' -vet=off -count=1 -timeout 1200s`：验证配置修订后的 runtime 状态会保留阻断但清零旧重试预算，避免新配置继承旧失败次数。
- `go test ./gui -run '^TestMCPRuntimeSyncReadyMarkerPersistsForManagedServer$' -vet=off -count=1 -timeout 1200s`：验证 managed MCP 的首次成功探测会保留非敏感 `ready` 证据，重启后不会因缺失状态被再次阻断。
- `go test ./gui -run '^TestReconcileManagedMCPRuntimeSyncOnStartupRetriesDueHTTPState$' -vet=off -count=1 -timeout 1200s`：验证进程重启后已到退避时间的 managed HTTP MCP 会由启动协调器重新执行 strict probe，成功写入 `ready` marker。
- `go test ./gui -run 'Test(UpdateLocalMCPManagedToManualClearsRuntimeBlocker|UnregisterLocalMCPClearsRuntimeState|MCPRuntimeSyncStateInvalidTimestampFailsClosed|MCPRuntimeSyncMissingManagedStateFailsClosed)' -vet=off -count=1 -timeout 1200s`：验证配置身份转换/删除与 durable runtime 状态同步，损坏时间戳和 managed 缺失状态均保持 fail-closed。
- `go test ./gui -run 'Test(UpdateMCPManagedToManualClearsRuntimeBlocker|UpdateMCPPartialEntryPreservesManagedIdentity|UpdateMCPServerHeaderChangeInvalidatesSessionAndTools)' -vet=off -count=1`：验证远程 managed→manual 转换会清除 marketplace runtime blocker；部分更新不会丢失 managed 身份或认证契约；认证/租户 header 变更会同时失效旧 session 与工具缓存，避免沿用旧凭据或 stale inventory。
- `go test ./gui -run 'Test(InstallHubSkill|InstallMixedSkill|.*InstallOnly|.*ToolInstall|InstallSkill|CapabilityGap|AddSkill|DeleteSkill)' -count=1 -vet=off`：复核已迁移/legacy 安装适配器在回滚路径上的结构化结果；已持久化补偿记录的回滚失败使用替换写入，避免队列出现重复 live snapshot。
- `go test ./gui -run '^TestMCPRuntimeSyncPendingResetStartsFreshBoundedCycle$' -vet=off -count=1`：验证配置修订重置 readiness 预算时同时清除旧 request ID，避免新探测沿用旧配置关联。
- `go test ./gui -run 'TestMaclawAppDependencyUpdate(ClearsCompensationAfterRollback|PreservesSkillMarketSource)' -count=1 -vet=off -timeout 300s`：验证 maclaw.app 依赖更新在 checked index 失败后完成 durable 回滚并清理补偿记录，同时保留 SkillMarket 来源/UUID。
- `go test ./gui -run '^TestLocalMCPManagerManagedFailurePersistsRuntimeBlocker$' -vet=off -count=1 -timeout 1200s`：验证 marketplace-managed Stdio 在后台/启动同步失败时会持久化 runtime blocker，而手工本地服务器不写入该全局状态。
- `go test ./gui -run '^TestLocalMCPManagerOwnerStartupFailurePersistsManagedRuntimeBlocker$' -vet=off -count=1 -timeout 1200s`：验证 owner-scoped 专用本地 MCP 进程启动失败同样持久化 runtime blocker，避免共享客户端与专用客户端状态分叉。
- `go test ./gui -run '^TestGetServerToolsHidesManagedStaleCacheWhileRuntimeBlocked$' -vet=off -count=1 -timeout 1200s`：验证 managed runtime 未通过 strict readiness 时，Wails 工具查询和服务器列表都不泄露旧缓存；恢复 `ready marker` 后才允许只读查看。
- `go test ./gui -run '^TestHealthCheckStrictContextHonorsIndependentDeadline$' -vet=off -count=1 -timeout 1200s`：验证 strict MCP 探测遵守调用方 deadline，不会被远端慢响应拖过独立超时边界。
- `go test ./gui -run 'TestValidateMCPJSONRPCSuccess|TestHealthCheckStrictRejectsJSONRPCError' -vet=off -count=1 -timeout 1200s`：验证严格 MCP 探测不会把 HTTP 200 的 JSON-RPC `error` 或缺少 `result` 当作成功。
- `go test ./gui -run '^TestCheckMCPServerHealthManagedUsesStrictProbeAndPersistsFailure$' -vet=off -count=1 -timeout 1200s`：验证管理页的 managed 远程“Check now”走 strict 探测，并把协议失败写入 durable runtime blocker。
- `go test ./gui -run '^TestValidateMCPToolsListResult$' -vet=off -count=1 -timeout 1200s`：验证严格 `tools/list` 必须返回数组类型的 `tools`，允许空数组但拒绝缺失或错误类型。
- `go test ./gui -run 'Test(InstallHubSkill|InstallManagedHubSkill|InstallMixedSkill|.*InstallOnly|.*ToolInstall|InstallHubCapability|CapabilityMarketplace)' -vet=off -count=1 -timeout 300s`：覆盖列出的安装/能力市场定向组合路径。
- `go test ./gui -run '^TestInstallSkill' -count=1 -vet=off -timeout 300s`：复核 Wails staged 安装在提交失败时显式传播 staging 清理错误，不再使用 best-effort 丢弃异常。
- `go test ./gui -run 'Test(AddSkill|InstallSkill|DeleteSkill|SkillCache|DeleteNLSkill|ProjectInstallSkill)' -count=1 -vet=off -timeout 300s`：复核传统 GUI metadata/ZIP/settings 边界、删除隔离、项目安装和缓存失效；该结果不代表 `App.InstallSkill` 已具备共享提交器语义。
- `go test ./gui -run 'TestImportNLSkillZipPath|TestImportNLSkillZipRollsBackWhenIndexRefreshFails|TestImportNLSkillZipDoesNotClaimOtherRecoveryScope' -count=1 -vet=off -timeout 300s`：复核传统 `ImportNLSkillZip` 在导入前按 `RecoveryScope=primarySkillsDir` 恢复 `import` action 补偿、批量目录回滚和 checked 索引失败处置，并确认不会领取其它 scope 的同 action 记录；该入口仍属 legacy adapter，不能据此开启自动导入。
- `go test ./gui -run '^TestAddSkillLockedWithRecordPreservesOuterCleanupTargets$' -count=1 -vet=off -timeout 300s`：验证 legacy `InstallSkill` 的内层包更新不会覆盖外层已登记的 ZIP staging 清理目标，避免最终审计后跨重启泄漏临时文件。
- `go test ./gui -run 'TestApplySkillMaintenanceAction|TestCleanupStaleNLSkillsDoesNotBypassApprovedMaintenanceCommit' -count=1 -vet=off -timeout 300s`：复核 file-backed maintenance 的提交、no-op 和失败边界；版本化契约产物会与 planner 已登记的 rollback cleanup 目标合并保存；旧无确认 stale-cleanup 入口不会绕过 approved plan 直接改写 registry。
- `go test ./gui -run 'TestInstallSkill(UsesOneOuterCompensationAndOneCommitAudit|IndexFailureRestoresProjectDirectoryAndMetadata|RootFilesInEmptyDestinationRollbackRemovesOnlyCreatedFiles)' -count=1 -vet=off -timeout 300s`：验证 `InstallSkill` 只使用一条外层 durable 记录、失败时项目目录与 metadata 一并恢复，且 checked index 失败不产生 committed 审计；既有空目录接收根级文件时只删除本次创建的文件。
- `go test ./gui -run '^TestDeleteSkillIndexFailureRestoresPackageAndMetadata$' -count=1 -vet=off -timeout 300s`：验证传统 GUI 删除在 checked index 首次发布失败时恢复包目录与 metadata，随后重建索引并清理补偿队列，且不产生 `legacy_deleted` 审计。
- `go test ./gui -run '^TestDeleteSkillDetailedReportsCommittedCleanupPending$' -count=1 -vet=off -timeout 600s`：注入传统 GUI 删除提交后清理失败，验证 `DeleteSkillDetailed` 返回 `committed + cleanup_status=pending`，已删除目录不被反向恢复，且 durable 补偿记录继续保留。
- `go test ./gui -run '^TestInstallSkillFinalAuditFailureRollsBackWithoutCommittedResult$' -count=1 -vet=off -timeout 300s`：通过使 strict audit sink 不可写，验证 InstallSkill 最终审计失败时恢复项目目录和 metadata，且不会把错误标记为 `committed`。
- `go test ./gui -run '^TestInstallSkillDetailedReportsSettingsFailureAndRollsBack$' -count=1 -vet=off -timeout 420s`：通过 settings 写入故障注入，验证 `InstallSkillDetailed` 返回结构化 `rolled_back` 结果、保留原 settings、清理补偿队列，并携带 request/error 关联字段。
- `go test ./gui -run '^TestInstallSkillMarketplaceMutationRollsBackWithOuterInstall$' -count=1 -vet=off -timeout 420s`：验证插件安装触发的默认 marketplace 注册与 `enabledPlugins` 共用同一外层 durable 快照；最终审计失败时两者均恢复，避免嵌套 settings 写入遗留。
- `go test ./gui -run '^TestInstallSkillDetailedReportsCommittedCleanupPending$' -count=1 -vet=off -timeout 600s`：验证 InstallSkill 已提交后先清理显式 post-commit 产物，再尝试删除补偿记录；清理或队列删除失败均保留 `committed + cleanup_status=pending`，不回滚已审计结果。
- `go test ./gui -run '^(TestInstallSkillDoesNotRewriteExistingSkillScanCache|TestInstallSkillRejectsUnknownLocationBeforeAnyMutation)$' -count=1 -vet=off -timeout 300s`：验证 ZIP 安装只写本归档发布目录的 scan cache，绝不改写共享根已有 Skill 的证据；未知安装位置在创建补偿、解压、metadata/package 写入前 fail-closed。
- `go test ./gui -run '^TestApplySkillMaintenanceActionReturnsSkippedForNoChange$' -count=1 -vet=off -timeout 600s`：验证 confirmed maintenance no-op 不写入 `maintenance_apply_started` 审计、不创建补偿、不刷新索引，并返回 `skipped/clear`。
- `go test ./corelib/skill -run '^TestSkillCommitter_CleansDurablePostCommitArtifactsBeforeClearingQueue$' -count=1 -vet=off -timeout 600s`：验证共享提交器会在清理补偿队列前删除记录中的 post-commit staging/备份目标；即使未提供额外 cleanup callback，也不会留下孤儿产物。
- `go test ./corelib/skill -run '^TestSkillCommitter_RejectsNilMutatorResultForNonEmptyRegistry$' -count=1 -vet=off -timeout 300s`：验证 maintenance/full-list mutator 对非空注册表返回 nil 时 fail-closed，不会把错误规划当成“删除全部 Skill”写入。
- `go test ./corelib/skill -run '^TestSkillCommitter_RejectsCreateIdentityMismatch$' -count=1 -vet=off -timeout 300s`：验证简单 `AllowCreate` 事务拒绝请求名与候选定义名不一致，避免审计/补偿主键与实际注册项分叉。
- `go test ./corelib/skill -run 'TestEvolutionPipeline_tryRepair_Disable' -count=1 -vet=off -timeout 300s`：验证不可修复 Skill 的 `needs_review` 转换经过 durable `SkillCommitter`；持久化失败时不发成功事件并保留 `audit_pending` 补偿。
- `go test ./corelib/skill -run '^TestRecoverPendingCommittedCleanupRunsDeclaredTargetsAfterCustomHook$' -count=1 -vet=off -timeout 600s`：验证重启恢复即使使用入口自定义 cleanup hook，仍会继续执行 durable 记录声明的清理目标后才移除队列。
- `go test ./corelib/skill -run 'Test(RetryEvolutionCompensationByIdentity|ClearEvolutionCompensationManually)' -count=1 -vet=off`：验证人工 retry 必须按 request ID + Skill + action 精确定位并写审计，提交后清理可重试且不会领取 sibling 记录；manual clear 拒绝未提交记录，并在清理目标仍存在时保持队列不变。
- `go test ./gui -run '^TestSkillEvolutionCompensationManualEndpointsRequireConfirmation$' -count=1 -vet=off`：验证 Wails 人工处置端点缺少 `confirm=true` 时 fail-closed，不触碰补偿队列。
- `go test ./gui -run '^TestInstallDefaultMarketplace(WriteFailureRollsBackDurableState|AuditFailureRollsBack|SecondCallIsNoOp|ConflictingEntryFailsClosed)$' -count=1 -vet=off -timeout 420s`：验证传统 GUI 默认 marketplace 独立 settings 事务在写入失败、最终审计失败时恢复 pre-image 并清理补偿；重复调用不写盘、不新增补偿或提交审计；已存在但来源冲突的条目 fail-closed 且不写盘。
- `go test ./gui -run '^TestInstallSkillPendingRollbackRecoversAfterRestart$' -count=1 -vet=off -timeout 600s`：模拟 InstallSkill 前向索引发布和进程内回滚索引重建连续失败，验证唯一外层 legacy 补偿记录跨重启保留，并由新 App 实例恢复目录、metadata 和索引后才清理队列。
- `go test ./gui -run '^TestInstallSkillBlocksOnCorruptDurableRollback$' -count=1 -vet=off -timeout 300s`：注入可解析但不可恢复的 YAML 快照，验证 InstallSkill 在恢复失败时保留 durable compensation、连续三次升级 `needs_review`，后续 mutation 继续被阻断且不会删除已存在目录。
- `go test ./gui -run '^TestAddSkillRunsLegacyRecoveryBeforeAdmission$' -count=1 -vet=off -timeout 300s`：验证 metadata-only AddSkill 先执行 legacy recovery 再做 skill admission，损坏回滚记录可累计三次尝试并升级 `needs_review`，不会因过早门禁而保持 `attempts=0`。
- `go test ./gui -run '^TestInstallSkillBlocksBeforeMutationWhenEvolutionQueueUnreadable$' -count=1 -vet=off -timeout 300s`：验证传统 GUI `InstallSkill` 在恢复 legacy 记录前即检查全局补偿队列；队列损坏时不会写入 metadata、settings 或 Skill 目录。
- `go test ./gui -run '^TestSetNLSkillStatusSameValueIsSideEffectFree$' -count=1 -vet=off -timeout 300s`：验证状态设置为当前值时在写盘、审计和索引刷新前短路，既不创建补偿记录也不产生审计事件。
- `go test ./gui -run '^TestDefinitionMutationBlocksWhenCompensationQueueUnreadableEvenIfDisabled$' -count=1 -vet=off -timeout 300s`：验证定义提交即使目标状态为 `disabled` 也必须先通过补偿队列健康检查，损坏队列下零写盘并 fail-closed。
- `go test ./gui -run '^TestInstallSkillDetailedReportsCommittedCleanupPending$' -count=1 -vet=off -timeout 600s`：注入提交后补偿清理失败，验证 InstallSkill 保留已审计业务结果并返回 `committed + cleanup_status=pending`，不误报为回滚。
- `go test ./gui -run 'Test(InstallSkill|AddSkill|DeleteSkill|LegacyDetailedMutationReportsNoChangeWithoutDurableSideEffects|MaclawAppDependencyUpdatePreservesSkillMarketSource)' -count=1 -vet=off -timeout 1200s`：复核 legacy GUI 安装/添加/删除、无变化短路及 maclaw.app 依赖更新边界。
- `go test ./gui -run '^TestMaclawAppDependencyUpdatePreservesSkillMarketSource$' -count=1 -vet=off -timeout 300s`：复核 `maclaw.app` SkillMarket 依赖更新的目录替换、来源/UUID 保留、checked 索引和最终审计；提交后先持久化 `committed`，备份或队列清理失败由 bounded-retry helper 记录并升级 `needs_review`。
- `go test ./gui -run 'Test(InstallSkill|DeleteSkill|AddSkill|ProjectInstallSkill|InstallSkillDetailed|ApplySkillMaintenanceAction)' -count=1 -vet=off -timeout 600s`：复核传统 GUI 详细结果契约及 maintenance 入口；config-only maintenance 的无变化候选返回 `skipped/no_change`，不创建补偿、索引或最终审计写入。
- `go test ./gui -run '^TestRenameNLSkill' -count=1 -vet=off -timeout 1200s`：验证 GUI 重命名在目录移动前登记 durable intent、移动后持久化 `dir_published`，并保存 YAML pre-image；索引持续失败时原目录/YAML 仍同步回滚，补偿记录保留 `oldDir → newDir` 恢复证据，拒绝 `.`/`..` 等路径型目标名，并确认 `SkillID` 等稳定别名会先解析到 canonical Name 且不能绕过待恢复补偿门禁。
- `go test ./gui -run '^TestDeleteNLSkill' -count=1 -vet=off -timeout 1200s`：验证删除先隔离目录、索引失败可回滚；queue-clear/cleanup failure 返回 `committed + cleanup_status=pending` 时不恢复已删除 Skill，executor/scanner 缓存立即失效，config-only 别名删除按 canonical Name 清缓存，重启恢复只清理队列并保持删除结果。
- `go test ./gui -run '^TestSkillExecutorRenameRejectsDetachedExecutors$' -count=1 -vet=off`：验证兼容 `SkillExecutor.Rename` 只接受 App-owned executor；detached/stale 引用不会再执行旧的直接 YAML/目录/config 写入。
- `go test ./gui -run 'Test(ApplySkillRepairDraft(CleanupFailurePersistsAndRecovers|Disable|DisableCleanupFailurePersistsAndRecovers|TOCTOUConflict|RollsBackOnIndexRefreshFailure)|RejectSkillRepairDraft(CleanupFailurePersistsAndRecovers|CountsAndAudits|UnreadableCountsOnly|PersistsCompensationWhenConfigRollbackFails))' -count=1 -vet=off -timeout 1200s`：验证 reviewed-draft apply/disable/reject 在 cleanup 失败时保留 `committed + cleanup_status=pending`、已提交 overlay/YAML 与 draft，并可由后续恢复器清理声明目标；同时复核 disable、reject、TOCTOU、配置失败和索引失败回滚边界。
- `go test ./gui -run '^TestAutoStartLocalMCPServersHonorsDurableRuntimeBlocker$' -count=1 -vet=off`：验证本地 marketplace MCP 的 AutoStart 不会绕过 durable pending 退避或 terminal `needs_review` 状态，避免启动后台进程重新触发被阻断的运行时探测。
- `go test ./gui -run '^TestMCPRuntimeSyncPersistenceFailureFenceBlocksStaleReady$' -count=1 -vet=off`：验证 runtime 状态写盘失败时，即使旧 `ready` marker 仍可读，进程内 persistence-failure fence 也会阻断执行、自动重试和 AutoStart。
- `go test ./gui -run '^TestLocalMCPManagerSyncSkipsBlockedManagedSibling$' -count=1 -vet=off`：验证同一次本地同步中，已阻断的 managed MCP 不会因其它 sibling 可启动而被重新启动；健康 sibling 仍可独立恢复。
- `go test ./gui -run '^TestSyncLocalMCPServersReportsBlockedManagedRuntime$' -count=1 -vet=off`：验证前台 checked sync 对仍处于 durable blocker 的 managed MCP 返回显式错误，不会把“未启动”误报为 ready。
- `go test ./gui -run 'TestToolManageSkillExecuteMaintenancePlan|TestApplySkillMaintenanceActionReturnsSkippedForNoChange|TestToolExecuteSkillMaintenancePlanNoOpStillBlocksOnUnreadableCompensationQueue' -count=1 -vet=off -timeout 600s`：复核 IM maintenance 的提交状态字段、config-only no-op 短路、无论是否 no-op 都必须先通过补偿队列健康检查，以及文件型契约草案不会绕过 reviewed desktop apply；桌面单动作在 merge/delete 目标被移出列表时使用原始 pre-image 作为稳定审计主键，不再依赖 `updated[0]`。
- `go test ./gui -run 'TestToolManageSkillExecuteMaintenancePlan' -count=1 -vet=off -timeout 600s` 同时覆盖批量 maintenance 的完整列表提交与主 Skill 归属；审计/补偿关联使用首个实际变化项，而非固定 `updated[0]`。桌面 `ApplySkillMaintenanceAction` 同样在删除/合并场景回退到原始条目，避免审计归属漂移。
- `go test ./corelib/skill -run 'TestApplyTargetedMaintenanceAction_MergeRejectsSameSkill|TestApplyTargetedMaintenanceAction_MergeRequiresFlags|TestExecuteSkillMaintenancePlan.*Merge' -vet=off -count=1`：验证 duplicate merge 对主/关联 Skill 的 distinct identity 约束，防止别名碰撞或 malformed plan 自禁用。
- `go test ./gui -run 'TestMaintenanceEntryByName' -vet=off -count=1`：验证 maintenance 提交主键按稳定别名（`HubSkillID`/`SkillID`）解析，空身份不会误匹配。
- `go test ./gui -run 'TestToolExecuteSkillMaintenancePlanRefreshIndexOnly' -vet=off -count=1`：验证 IM `refresh_index` 仅刷新 checked 派生索引并返回结构化状态；provider 失败时 fail-closed，不创建无意义的配置补偿；最终审计故障仍需独立注入测试。
- `go test ./gui -run 'TestLegacyDetailedMutationReportsNoChangeWithoutDurableSideEffects|TestInstallSkillDetailedReportsAlreadyCurrentPlugin|TestAddSkillDetailedReportsCommittedCleanupPending|TestDeleteSkillDetailedReportsCommittedCleanupPending|TestSnapshotSkillZipForInstallPreservesSourceBasename' -count=1 -vet=off -timeout 600s`：验证传统 GUI Add/Delete 无变化和插件已启用时的 `skipped` 结果，不写补偿、不改 settings；ZIP 绝对路径重复添加按 basename 幂等，快照保留源 basename；Add/Delete 提交后清理失败返回 `committed + pending` 并保留 durable 记录。
- `go test ./gui -run 'TestSkillExecutor(MarkUploadedBlocksOnUnreadableCompensationQueue|CompatibilityMutationsBlockOnUnreadableCompensationQueue)|TestMarkUploadedFileSkillEagerHubSkillIDFromPackageSubmission' -count=1 -vet=off`：验证兼容执行器的上传回执写入同样受补偿队列健康门禁，空/不可读队列下零写盘；并复核正常文件 Skill 回执仍原子写入并更新稳定 Hub 身份。
- `go test ./gui -run '^TestSkillLifecycleAdHocUploadReceiptChecksCompensationAdmission$' -count=1 -vet=off`：验证未注册目录的上传回执路径不会绕过补偿队列准入，损坏队列下 `upload_status.json` 零写盘，并拒绝空 submission ID。
- `go test ./gui -run '^TestPrepareSkillDirForMarketBlocksAdHocAutofixOnUnreadableQueue$' -count=1 -vet=off`：验证未注册目录的 portability auto-fix 也受 `installMutex` 与补偿队列门禁保护，损坏队列下不会先改写 `skill.yaml`。
- `go test ./gui -run '^TestSkillExecutorUsagePersistenceBlocksOnUnreadableCompensationQueue$' -count=1 -vet=off`：验证执行后的 usage/success 统计写回同样受补偿队列门禁，恢复窗口中不会更新权威 config。
- `go test ./gui -run '^TestSkillRunnerUsageStatsBlocksOnUnreadableCompensationQueue$' -count=1 -vet=off`：验证 `SkillRunner.updateUsageStats` 在运行后写回前重新检查全局补偿队列；队列损坏时不递增 usage/success，也不发出成功更新事件。
- `go test ./gui -run '^TestRestoreSkillsBlocksOnUnreadableCompensationQueue$' -count=1 -vet=off`：验证传统备份 ZIP 恢复入口在配置写入前受 `installMutex` 与 compensation admission 保护，队列不可读时不追加 registry 条目。
- `go test ./corelib/skill -run 'TestRetryEvolutionCompensationByIdentityCleansCommittedRecord|TestRetryEvolutionCompensationReturnsUpdatedPendingSummary|TestClearEvolutionCompensationManuallyReturnsClearedTerminalSummary' -count=1 -vet=off`：验证人工 retry/clear 成功时返回明确终态摘要；清理失败时立即反映 durable attempts/cleanup 状态，避免 UI 使用过期快照。
- `go run scripts/check_wails_bindings.go`：确认新增/现有详细 API 与前端绑定仍完整（17 个动态引用、1240 个生成绑定）。
- `go test ./corelib/agentservice -run 'TestReviewedHostDelegateCancellationAndStartedSemantics|TestReviewedHostSSHCancellationAndDisconnectSemantics|TestProjectedDynamicProvidersUseCommonPlannerAndRenderer' -count=1 -vet=off`：确认预取消请求返回 `dynamic_execution_cancelled`，只有外部副作用已发生但结果不可观测时才标记 `Unknown`，并同步当前 renderer 描述协议。
- `go test ./corelib/skill -run '^TestRecoverPendingCompensationsForActionPrefixAndSkillDoesNotClaimSibling$' -count=1 -vet=off -timeout 240s`：验证 GUI 按 action prefix + Skill 过滤恢复，不会误领取同队列中的其它 Skill 记录。
- `go test ./tui -run '^TestRecoverTUIEvolutionCompensationsRestoresEmptyConfigAndRemovesCreatedDir$' -count=1 -vet=off -timeout 300s`：验证 TUI 启动恢复按 action/scope 接管，并在空 registry pre-image 下同时移除已发布目录与恢复配置。
- `go test ./tui/commands -run '^TestCommitSkillHubConfigBatchIsAtomicAndAudited$' -count=1 -vet=off -timeout 300s`：验证 CLI GitHub 批量注册使用单一补偿快照、严格最终审计并在成功后清理队列。
- `gui/frontend/node_modules/.bin/tsc.cmd --noEmit` 与 `gui/frontend/node_modules/.bin/vite.cmd build`：在 npm CLI 不可用时直接使用已锁定的本地工具完成 TypeScript 类型检查和生产构建；两项均通过，Vite 仅提示少数大 chunk 的性能优化建议。

说明：本节较早条目中“人工处置 UI 尚未提供/仅只读”的表述属于历史基线；当前可解析队列已支持 GUI 显式 retry/clear。损坏或未知 schema 的 canonical 队列仍无在线清除入口，只能保留证据并离线修复。

这些测试只证明列出的路径，不证明所有 GUI 入口已统一；reviewed-draft 和新目录安装仍需补清理重试耗尽与跨重启矩阵，重命名/删除的最终审计与主要回滚路径已有定向回归，但 Windows 文件锁、持续清理失败和完整入口矩阵仍待补齐；maintenance、能力缺口 GitHub/更新、IM 更新及 managed install 更新仍需入口级故障注入与跨重启矩阵。AgentService 已有单包/多包中途回滚、同内容 no-op、非 YAML payload 差异检测、陈旧 `.prev`、共享提交器、`NewService` 启动恢复、scope 越界拒绝、committed 清理跨重启和不同 `DataRoot` 隔离，以及多包最终审计失败、external contract 部分恢复失败的定向回归；仍需扩展运行/上传、多租户和其它入口级异常矩阵。传统 `App.AddSkill`/`App.DeleteSkill` 已增加 checked-index 失败后的目录/metadata 回滚断言，DeleteSkill 还覆盖提交后清理失败的 pending 结果；`App.InstallSkill` 已增加单一外层补偿、settings 写入失败、索引失败、最终审计失败、跨重启 pending 恢复、损坏恢复预检和提交后清理 pending 断言；能力市场 MCP 还新增“回滚清理失败不误标 committed”的状态隔离断言。持续权限拒绝、损坏 canonical 队列的离线人工处置以及其它入口级恢复矩阵仍是 P0，不能由 Hub 或 AgentService 测试代替。

当前环境未提供 npm CLI，因此未执行 `npm run build` 原始命令；已使用 `node_modules/.bin/tsc.cmd --noEmit` 与 `node_modules/.bin/vite.cmd build` 完成等价类型检查和生产构建。Vite 报告的少数大 chunk 属性能优化建议，不影响构建正确性。GUI 全量测试仍可能被工作区其它资源或并行负载阻断；若出现与 `h.forgetAgentGuidedSkill undefined` 类似的既有错误，应单独记录，不归因于本次自进化改动。

指标：未经确认的 high/critical 安装为 0；无证据却 `passed` 为 0；staged 未验证进入 active 为 0；关键写盘无审计为 0；冷却期重复修复为 0；取消后继续写盘为 0；修复后真实成功率高于修复前。

## 15. 优先级与运行处置

- **P0（上线阻断）**：为 maintenance 全部分支、已有版本更新/GitHub 导入等 legacy adapter，以及传统 GUI `App.InstallSkill` 完成共享提交器迁移；对 `App.AddSkill`/`App.DeleteSkill` 保持 `*Detailed` 结果契约，并补齐入口级故障注入、清理失败/跨重启断言和前端异常分支验证；`App.InstallSkill` 已覆盖单一外层审计、settings 写入、重启恢复、空根目录和未知安装位置拒绝，仍需扩大目录型更新及前端异常矩阵；AgentService 已覆盖多包最终审计失败、清理再次失败升级及 external contract 部分恢复，仍需运行/上传和多租户入口级断言；让索引 provider 返回真实错误并为各入口补故障注入；补偿队列跨版本迁移、人工 retry/clear 的权限与审计闭环；统一 config/YAML/索引/最终审计的失败补偿；阻断未验证激活；为动态 contract 落地外部快照 schema/摘要校验及恢复失败处置。当前核心 pipeline、Create/Update、GUI repair、YAML 版本恢复、reviewed-draft、staged 激活、重命名、`DeleteNLSkill`、多个新目录安装分支及 AgentService GitHub/Hub/Market/ZIP 导入已接入 `SkillCommitter`；maintenance 边缘分支、安装/导入更新、能力缺口 GitHub 导入和传统 GUI 安装仍有独立编排。队列健康状态、schema 损坏 fail-closed、上传门禁、恢复事件、安装初始目录补偿和 `.prev` 保护属于风险收敛，不代表 P0 已关闭。
- **P1（近期）**：非-pipeline 审计字段校验；GUI 终态任务查询；失败趋势指标；升级迁移和索引损坏验收；已迁移入口的 cleanup/跨重启故障注入。
- **P2（持续）**：真实非 Bash replay adapter、组织策略覆盖、版本化审计 Schema、误修复率和长期回滚分析。

当审计不可用、队列持续增长或 Skill 连续失败时，先关闭 `skill_evolution_enabled`，保留必要的只读观察和证据采集；待审计恢复、队列年龄下降并完成归因后再恢复自动改写。`unverified` 候选只能导出草案，不能通过修改状态字段强行激活。

### 15.0 下一轮优化顺序

按风险和收益排序，实施顺序固定为：

1. **进行中**：将 `SkillCommitter`（prepare/commit/rollback/cleanup）继续覆盖 maintenance 边缘分支、已有版本更新/GitHub 导入及剩余非主路径；GUI repair、YAML 版本恢复、reviewed-draft、重命名、`DeleteNLSkill`、apply、staged 激活和新目录安装已完成基础迁移。传统 `App.AddSkill`/`App.DeleteSkill` 已具备 legacy durable compensation、checked-index 回滚和 `*Detailed` 结构化结果；仍需入口级故障注入和前端调用迁移。`App.InstallSkill` 已建立唯一外层 durable 记录并在 mutation 后补写路径，最终审计失败已能回滚且不再伪报 committed，下一步是收敛内部 legacy 审计为单一最终审计、接入共享提交器和完整外层故障注入；提交器必须返回 `state` 与 `cleanup_status`；
2. **已完成基础能力**：索引层增加可替换的失败注入 provider；仍需将“索引失败后 YAML/config/目录均恢复”扩展为每个 GUI 入口的发布阻断测试；
3. **基础能力已完成，运维闭环进入受控人工阶段**：补偿队列已支持缺失 schema 的原子迁移、损坏隔离和 forensic copy；迁移失败或损坏时保留 canonical 文件并进入只读安全模式。GUI 管理页现提供按 request ID + Skill + action 精确匹配的显式 retry/clear；retry 必须确认并沿用原记录，clear 仅允许 committed 且清理目标已验证的记录，模型、迁移工具和只读接口仍不得直接恢复或编辑快照正文；后续仍需补入口级故障注入和跨重启演练；
4. 将能力缺口 GitHub/更新、IM 更新和传统 GUI ZIP/插件安装改为调用统一安装提交器；为 AgentService 扩展入口级最终审计/清理异常和多租户恢复断言（`NewService` 启动恢复、scope 隔离和基础清理重试已存在）。为非桌面实例补齐 durable config/目录快照；完成剩余路径前仍关闭自动安装，仅保留提示和人工确认；
5. **external snapshot v1 基础校验已完成**：当前已具备 schema、digest、stable ID、tenant/user 绑定、scope 校验和重试耗尽 strict 审计；仍需补齐恢复器持续失败/权限拒绝的入口级矩阵、远程审计关联和人工处置闭环，禁止编辑快照正文绕过门禁；
6. 最后补齐终态任务查询、趋势指标和真实非 Bash replay adapter。

### 15.1 运维操作顺序（建议固定）

发生写盘、审计或补偿异常时，按以下顺序处理，避免人工操作扩大不一致范围：

1. **冻结自动变更**：将 `skill_evolution_enabled` 设为 `false`；不要使用 `force=true` 绕过失败次数、冷却或 `needs_review`。
2. **确认健康度**：读取 `GetSkillEvolutionStatus()`，确认 `audit_available=true` 且 `compensation_queue_healthy=true`；任一为 false 都按 fail-closed 处理。
3. **读取安全摘要**：通过 `ListSkillEvolutionCompensations()` 或 `manage_skill(action="evolution_compensations")` 获取 `request_id/skill/action/status/attempts/failure_reason`。这些接口不返回恢复快照，模型和只读命令没有恢复权限。
4. **保留证据后恢复**：先保存相关审计事件和摘要，再由启动恢复或 GUI 人工 retry 触发有限次数的补偿；人工 retry 必须复用原 request ID，并同时提供 Skill/action 与 `confirm=true`，成功应看到 `skill:compensation_recovered`，失败继续保留队列。
5. **人工复核/清除**：`needs_review` 只允许人工依据 YAML、config、索引摘要和审计记录进行处置。GUI 的 `RetrySkillEvolutionCompensation` 仅重置该精确记录的有限重试预算并写 `compensation_manual_retry` 审计；`ClearSkillEvolutionCompensation` 仅接受 `transaction_state=committed` 且已验证清理目标的记录，并写 `compensation_manual_clear` 审计。迁移工具只负责 schema 升级；禁止模型直接调用 retry/clear、编辑 `audit_pending.jsonl` 或删除队列记录。
6. **小范围解冻**：仅在队列为空、审计可写、索引可重建且对应回归测试通过后，重新开启自动进化；优先灰度低风险 Skill。

### 15.2 当前实现中已修正的不合理点

| 原有倾向 | 优化后的规则 | 原因 |
|---|---|---|
| 把 scan cache 当作 Skill 定义的一部分 | cache 仅为可重建派生数据，失败产生告警，不改变 `verified/active` | 避免缓存故障导致错误激活或错误回滚 |
| 用日志表示“已回滚” | 回滚必须返回明确状态；不完整回滚写入 durable compensation | 日志不可作为恢复凭据 |
| 允许前端/模型直接重试补偿 | 仅 GUI 显式确认后按 request ID + Skill + action 调用人工 retry；模型和只读接口仍无权限 | 保留人工意图、原授权范围和审计链，防止批量/越权恢复 |
| 用新 request 绕过失败上限 | 重试沿用原授权范围并递增 `attempt` | 防止通过换 ID 规避治理策略 |
| 把 `staged/unverified/needs_review` 当成可执行状态 | 只有 `active` 可进入普通路由 | 降低重启、overlay 和证据缺失风险 |
| 取消后仍继续 Apply | context 是最终裁决，写盘前后都检查并回滚/补偿 | 消除取消与写盘竞态 |

### 15.3 依赖任务与运行准入

依赖安装是独立生命周期，不得把它混入 `transaction_state` 或用日志替代证据。建议增加 `dependency_status`（`not_required|pending|running|succeeded|failed|cancelled`）：不需要依赖时忽略该轴；声明需要依赖时必须为 `succeeded`，否则保持阻断并进入人工复核。该状态必须跨重启可恢复、可取消、可超时且可观测。

## 16. 不可变安全不变量

1. **未验证不执行**：除 `active` 外的候选默认不得进入普通路由、执行器或自动上传。
2. **无证据不通过**：缺少真实参数、Executor 或可重放证据时 Gate 只能为 `unverified`。
3. **授权不由模型产生**：LLM 只能生成候选。
4. **状态可追溯**：每次业务状态变更都有请求、决策、结果和错误事件。
5. **失败可恢复**：写盘、索引或最终审计失败不得留下不可解释的半状态。
6. **并发有边界**：同 Skill 不并发；取消、超时和 shutdown 语义不同且可观测。
7. **外部副作用可控**：依赖安装、上传和发布均是独立任务；必须有白名单、超时、取消、审计和失败后的阻断语义，不能以日志成功替代 Gate 或提交证据。

## 17. 版本化实施清单

| 阶段 | 交付物 | 退出条件 |
|---|---|---|
| S0（当前） | Gate 三态、staged 隔离、失败次数上限、重启/overlay 基础测试、timeout 配置、pipeline request 审计、GUI 基础任务列表 | 核心回归测试通过 |
| S1（上线前） | config/YAML/索引统一事务；最终审计补偿；非-pipeline 审计字段；cancel/timeout/shutdown 全路径验收；重命名/删除/导入纳入可恢复边界 | 14.1 全部为 0 风险项；所有写盘入口均返回统一提交结果 |
| S2（近期） | GUI 终态结果页、取消确认、回滚原因筛选；完善补偿人工处置 UI（损坏队列/运行时 blocker）；升级迁移和损坏恢复 | 冷启动、迁移和人工处置演练通过 |
| S3（持续） | 非 Bash replay adapter、策略中心、误修复率和长期回滚分析 | adapter 覆盖率达发布目标 |

若 S1 任一退出条件不满足，应将 `skill_evolution_enabled` 设为 `false`，仅保留只读观察、审计查询和人工审批入口。即使单次事务返回 `committed`，只要 `cleanup_status` 不是 `clear`，也不得恢复自动写盘或上传。

## 18. 文档维护与变更记录

### 18.0 本轮已优化的不合理表述

| 原表述/隐含假设 | 优化后的表述 | 解决的问题 |
|---|---|---|
| “已接入共享提交器”即可视为入口完整合规 | 必须同时具备唯一生产路径、故障注入/重启断言和可检索审计；否则标记为“部分落地” | 避免把代码存在误判为可发布能力 |
| 新目录安装已迁移，因而安装更新也已迁移 | 新目录创建和已有目录替换分开计量；后者仍按 legacy adapter 逐入口验收 | 避免更新路径绕过 `.prev`、补偿或 no-op 规则 |
| 一个 `status` 字段同时表示 active、committed 和清理完成 | 使用 `skill_status`、`transaction_state`、`cleanup_status` 三条独立状态轴 | 避免清理失败后错误放行执行或上传 |
| 最终审计后清理失败可以反向回滚 | 审计已成功时保留新版本，设置 `committed + cleanup_status=pending`，只做幂等清理 | 避免恢复逻辑覆盖已审计版本 |
| 日志成功或 scan cache 成功即可证明可执行 | 只有 checked index、最终审计、准入式和真实 Gate 证据共同满足才可执行 | 避免派生缓存或日志成为错误权威来源 |
| no-op 只在某一个安装入口判断 | 在共享提交器和入口调用方双重判断，并按稳定身份、版本、定义指纹比较 | 避免重复发布、重复注册和无意义备份 |
| 依赖安装隐含在 Skill 提交结果中 | 依赖安装单独使用 `dependency_status`，失败不篡改事务结果，但按运行需要阻断 | 避免外部副作用污染提交语义 |
| 非桌面 IM/能力缺口实例可以复用桌面写盘流程 | 缺少持久化补偿上下文时直接拒绝文件写盘，只允许只读提示或人工转桌面 | 避免 fallback 绕过 durable compensation |
| `App.AddSkill`/`App.InstallSkill`/`App.DeleteSkill` 返回 `nil` 即视为安装提交成功 | 仅将统一提交器的 `committed + cleanup_status=clear` 视为成功；legacy 返回值必须标记为未证明。InstallSkill 的外层记录可恢复不等于已获得统一提交器结果 | 避免 ZIP 解压、`metadata.json` 和 settings 分步成功造成假提交 |
| AgentService 的 rename/backup 能在进程内回滚即可发布 | 已改为共享提交器的 staging/`.prev`/checked scan/最终审计边界，并在 `NewService` 启动阶段按 action prefix 恢复；仍必须补跨重启异常、批次和多租户断言，否则自动导入继续 fail-closed | 避免崩溃窗口、批次半安装或只在下一次导入时才发现遗留事务 |
| 成功 RemoteSession 的经验提取可以直接调用 `SkillExecutor.Register/Update` | 自动经验候选必须在每次写盘前重新检查显式开关和环境 kill switch；获授权后仍走专用扫描与共享提交器，并以 `automatic_experience` 记录审计来源 | 避免异步后台任务绕开 opt-in、补偿、checked index 与最终审计 |

以上修订只改变文档判定口径，不会放宽任何运行时安全门禁。

### 18.1 代码变更同步规则

涉及以下任一内容的代码变更，必须在同一变更中更新本文对应章节，并至少增加一条自动化断言：

1. 写盘入口、状态转换或上传门禁；
2. 补偿队列 schema、恢复策略或 fail-closed 条件；
3. 审计事件必填字段、Gate 证据语义或重试上限；
4. 索引 provider、扫描缓存权威性或 replay adapter 边界。

文档更新不得单独降低状态等级。若实现尚未有故障注入测试，只能标记为“部分落地”或“待实现”，并列出临时处置。

### 18.2 评审修订记录

| 日期 | 修订 | 目的 |
|---|---|---|
| 2026-09-01 | TUI 交互、pipe、RPC 与 CLI 启动统一执行按 `action prefix + RecoveryScope` 的补偿恢复；TUI/CLI 配置型提交显式标记 `config_backup_captured`，空 registry 也能在崩溃后恢复；TUI 安装/卸载审计 action 与补偿 identity 对齐 | 关闭 TUI 跨重启未恢复、空配置只恢复目录不恢复 registry，以及最终审计存在但 action 不匹配导致错误回滚的窗口；继续保留未归属旧记录，不跨服务猜测接管 |
| 2026-09-01 | 将 `skill_evolution_enabled` 与 `skill_auto_upload_enabled` 改为显式 opt-in：缺失字段、配置读取失败和 GUI 初始/热更新 pipeline 均 fail-closed；设置页与 Skills 进化页同步显示默认关闭 | 关闭“评审文档默认关闭、实际 nil/default 或 UI 默认开启”的策略漂移，防止首次运行、旧配置或配置读取异常触发自动写盘或远端上传 |
| 2026-09-01 | Expert Package 在后续依赖或 Expert 写入失败时，不再调用 `SkillExecutor.Delete` 直接删除已导入目录；现在逐项走 `DeleteNLSkill` 共享提交器，并将 cleanup 失败回传给调用方。遗留的 `SkillExecutor.Delete` 也改为仅转发该 durable lifecycle API，缺少已绑定 App/Executor 时直接拒绝 | 防止 Expert Package 或其它旧调用绕过 durable compensation、checked index 与最终审计；目录/配置/索引回滚失败会保持可见并 fail-closed |
| 2026-09-01 | ExperienceExtractor 的后台 register/update 改为每次读取 `skill_evolution_enabled` 并检查 `MACLAW_DISABLE_SKILL_EVOLUTION`；默认、配置读取失败和 kill switch 全部拒绝写盘。已获授权的候选标记为 `agent_created`，经专用扫描后通过共享 `SkillCommitter` 提交，审计来源为 `automatic_experience` | 关闭成功会话异步经验学习直接 `saveSkills` 的旁路，使配置/YAML、checked index、最终审计和补偿回滚遵循同一生命周期边界 |
| 2026-09-01 | `ImportAgentSkillDir` 在完成导入扫描后改为使用共享 `SkillCommitter`，不再直接 `SkillExecutor.Register`；新增索引发布失败时恢复注册表的回归 | 关闭 Agent Skill 目录导入绕过补偿、checked index 与最终审计的写盘路径 |
| 2026-09-01 | MaClaw App 捆绑依赖 Skill 的暂存目录改为创建在受管理的数据卷，并通过 `commitStagedSkillInstall` 发布；不再先复制目录再直接注册。新增 checked index 失败时同时回滚目录与注册表的回归 | 避免 Windows 跨卷重命名失败，并关闭捆绑依赖安装绕过目录补偿、最终审计和提交后清理的路径 |
| 2026-09-01 | `craft_tool` 的 `save_as_skill=true` 不再在全局策略关闭时自动注册；每次注册前重新检查 `skill_evolution_enabled` 和 `MACLAW_DISABLE_SKILL_EVOLUTION`，并删除成功后异步写入第二份 crafted 目录/config overlay 的旁路 | 关闭模型参数默认值触发的隐式写盘，以及一个注册结果对应两个互不一致持久化事务的问题；生成脚本仍作为本次任务产物保留 |
| 2026-09-01 | `SkillAutoSummaryPipeline` 的后台轨迹学习新增写盘前及发布前双重 opt-in/kill-switch/补偿队列检查；新定义的 Quality Gate 改在受管 staging 中运行，扫描后经 `commitStagedSkillInstall` 统一发布，索引失败会恢复目录和 config。删除旧 `RunQualityGate → UpdateLearnedSource` 旁路；相似已有 Skill 的 `Versioner` 自动替换暂时 defer，不再直接改 YAML | 将“打开轨迹日志”与“允许自动写盘”分离，收敛新定义的目录/config/index/audit/cleanup 原子边界；用保守 defer 避免自动更新在尚未具备资产保留 staged replacement 事务时损坏旧定义 |
| 2026-09-01 | `ResolveSkillRecording(save)` 不再让 `SkillOperationRecorder.Stop` 直写主目录后以 non-fatal `UpdateLearnedSource` 补登记。现在仅在受管 staging 生成录制的 YAML/模板，扫描后通过共享目录提交器发布；新增成功提交与 checked index 失败后目录/config 均不遗留的回归 | 关闭显式人工保存仍可留下“目录已创建、配置未登记”的事务裂缝；人工确认继续是授权来源，但成功返回现在同样要求 `committed + cleanup_status=clear` |
| 2026-09-01 | mixed SkillMarket/Hub 安装的已有版本替换改走 `commitStagedSkillInstallWithExisting`；不再保留手写目录备份、直接注册、索引和审计串联事务。新增 checked index 失败后 config 与旧目录均恢复的回归 | 统一创建与替换的 `.prev`、补偿、checked index、最终审计和 cleanup 语义，消除替换分支与新装分支的事务漂移 |
| 2026-09-01 | MaClaw App 的已安装 Hub/SkillMarket 依赖更新，标准目录布局已改走同一共享目录提交器；配置中的非规范旧目录不再自动回落到手写更新事务，而是保持原包并要求人工重装。新增标准更新、索引失败回滚和非规范目录零写盘回归 | 收敛自动更新的目录发布、config、索引、审计和 cleanup 边界；避免无法证明目标归属的旧路径继续执行直接注册/写盘 |
| 2026-09-01 | GUI 不再配置 `NudgePromoter` 或其直写 registry 的 registrar，并在初始化及 `skill_evolution_enabled` 热更新时强制 `EnablePromoter=false`；状态接口同步显示不可用 | 关闭“先写最终 `SkillsDir`、再登记 config”的自动发现旁路。自动 nudge 仅保留观测证据，待 core 提供 staging→共享提交器事务及故障注入证明后再评估启用 |
| 2026-09-01 | 自动摘要与手工录制的 staging 根目录改为使用所属 `App` 的 `data/skills_staging`，不再依赖进程全局路径 | 使 staging 与最终 Skill 目录同属受管理数据根，避免多实例/配置切换导致跨根发布，并减少 Windows 跨卷移动或遗留清理错位的风险 |
| 2026-09-01 | `CapabilityGapDetector.Resolve` 已停止模型触发的自动远程安装，返回明确的人工 Marketplace/Skills 审核提示；无 App 或空 staged 目录时的历史直接注册后备也 fail-closed。managed capability 的历史 `registerOrReplace` helper 同样拒绝直接写盘，生产安装仅允许 staged 提交器路径 | 使“能力缺口 / IM 自动安装当前禁止”的运行行为与入口矩阵一致，避免非桌面或未来调用重新绕过补偿、checked index 和最终审计 |
| 2026-08-31 | 修复 GUI 人工 retry/clear 返回陈旧补偿摘要：恢复失败后重新读取 durable attempts/status/cleanup 状态，成功 retry 或 clear 后返回明确终态（`recovered`/`cleared_manually` + `cleanup_status=clear`）；新增成功与失败回归测试 | 避免管理页把已递增的重试次数或 `needs_review` 仍显示为旧状态，确保人工处置结果与队列真实状态一致 |
| 2026-08-31 | 收紧本地 marketplace MCP 的启动门禁：AutoStart 与启动恢复不再绕过 durable pending 退避或 `needs_review` 终态；新增启动阻断回归测试 | 防止后台通用同步在退避窗口内重复启动进程，或在人工复核前重新触发已耗尽的运行时探测 |
| 2026-08-31 | 将 durable runtime blocker 门禁下沉到 `LocalMCPManager` 同步层，并停止受阻 managed server 的 owner-scoped 客户端；新增 sibling 隔离测试 | 防止非启动入口的通用同步绕过 AutoStart 门禁，确保同批其它服务器健康时也不会误启动被阻断实例 |
| 2026-08-31 | `SyncLocalMCPServers` 对未到重试时间或 `needs_review` 的 managed runtime 返回显式阻断错误，避免 checked sync 将跳过的实例误标为 ready | 让前台同步结果与 durable blocker 一致，阻止 UI 把“同步完成但实例未启动”误报为健康 |
| 2026-08-31 | 共享 `SkillCommitter` 在清理 durable 队列前统一执行 `CleanupCommittedEvolutionCompensation`，删除记录声明的 staging/`.prev` 产物；cleanup callback 与声明式目标均保持幂等 | 关闭“已提交但队列已清除、临时产物未删”的崩溃窗口，让所有迁移入口继承同一提交后清理顺序 |
| 2026-08-31 | maintenance confirmed no-op 将 `maintenance_apply_started` 审计延后到有效变更确认之后；无变化请求现在不产生任何审计/补偿/索引副作用 | 使 maintenance no-op 与统一提交器的零副作用契约一致，避免“仅开始事件”被误解为一次真实写盘事务 |
| 2026-08-31 | 收紧 legacy `InstallSkill` 的提交后清理顺序：先调用 `CleanupCommittedEvolutionCompensation` 删除登记的 ZIP staging/备份，再清理 durable 队列；任一步失败都保留 `committed` 记录并进入 bounded cleanup 状态 | 防止清理队列先被删除而临时产物因进程崩溃永久遗留，确保跨重启仍有可执行的清理计划且不回滚已审计结果 |
| 2026-08-31 | `InstallSkill` 复用 marketplace 预解析得到的 settings pre-image，避免为同一文件重复读取造成竞态；marketplace 与插件写入共用 post-image 围栏 | 确保 durable 快照确实对应本次写入前的权威版本，并在并发 settings 修改时保持恢复 fail-closed |
| 2026-08-31 | 修复传统 GUI `InstallSkill` 的嵌套 settings 事务：默认 marketplace 注册改为使用外层 `legacy_gui_skill_install` 的 durable 快照，先持久化补偿再写 marketplace 与 `enabledPlugins`，最终审计失败时统一恢复；新增联合回滚故障注入测试 | 避免 marketplace 独立事务在插件安装后续 metadata/index/audit 失败时已清理记录却遗留 settings，确保一次安装的配置变更具备单一恢复边界 |
| 2026-08-31 | 传统 GUI `InstallDefaultMarketplace` 改为独立 `legacy_gui_marketplace` settings durable 事务：写盘前保存 `FileSnapshots`，写盘后记录 post-image，严格最终审计成功后才标记 `committed` 并清理；写入/审计失败恢复原 settings，重复调用短路为 no-op，已存在但来源冲突的条目 fail-closed | 关闭默认 marketplace 注册绕过补偿与最终审计的配置写盘窗口，避免插件安装与独立注册之间出现不一致或静默接受错误来源 |
| 2026-08-31 | 补偿详情面板新增 `transaction_state` 与 `cleanup_status` 展示，并将 `cleanup_status=needs_review` 同等标记为阻断；恢复器对配置型记录支持 `skip_index_refresh` | 让运维界面区分“已提交但清理待办”和“需要回滚/人工复核”，避免仅显示 `status` 导致已审计版本被误判为可清理或可执行 |
| 2026-08-31 | 能力市场 MCP 配置事务与传统 marketplace 注册均设置 `skip_index_refresh`，保持配置型恢复不依赖 Skill 路由索引 | 防止 generic pipeline 启动恢复把无 Skill 目录的配置 pre-image 错误送入索引刷新，降低无关索引故障造成的阻断 |
| 2026-08-31 | 强化 managed Hub 更新回滚的 Windows 竞态处理：对事务拥有的发布目录采用有界重试，并在 `.prev` 恢复后验证“目标存在且备份消失”的后置条件；文件型补偿新增 post-image 摘要围栏测试，检测并发修改/删除时 fail-closed | 避免扫描器短暂占用或重新创建目录导致旧版本恢复不完整、`.prev` 残留，防止快照恢复覆盖并发写入 |
| 2026-08-30 | 优化事务模型表述：区分配置型与目录型提交顺序，明确三条状态轴和 no-op 规则；补充目录发布必须先登记 `CreatedDirs`/`.prev` 意图、提交后清理不得反向回滚 | 消除“所有入口都按同一物理顺序写盘”、空计划伪提交以及 `active`/`committed`/清理状态混用造成的误判 |
| 2026-08-30 | 将共享提交器覆盖范围与代码对齐：明确核心 pipeline、创建/更新、reviewed-draft apply 和 staged 激活已迁移；拆分 reviewed-draft disable/reject 的局部事务状态；同步入口准入矩阵、P0/P1 清单与测试证据边界 | 避免把 apply 的迁移进展误读为 disable/reject、maintenance、安装等入口也已统一，确保发布门禁按实际入口判定 |
| 2026-08-29 | 补充快速判断、全量写盘入口清单、提交器覆盖范围；补齐创建/更新、ZIP、mixed-install 与 managed capability 的 durable compensation；区分补偿队列 v1 与审计事件 v2 | 消除“入口门禁已完成”与“全路径事务已完成”的歧义，给发布和运维提供单一判断依据 |
| 2026-08-29 | 优化补偿记录生命周期、提交后清理失败语义；统一 staged 激活的 `config_revision` 为策略摘要，并同步绑定检查证据 | 明确“已提交”“已回滚”“补偿待恢复”三类状态边界，避免清理失败导致错误回滚或错误放行 |
| 2026-08-29 | 补强 managed capability/Enterprise 安装：暂停 overlay、安装前校验补偿队列、发布目录初始补偿、保留 `.prev` 恢复证据，并将最终审计后的清理失败定义为提交后待处置 | 缩小远端安装崩溃窗口，避免更新失败时丢失旧版本或误回滚已审计版本 |
| 2026-08-29 | 新增“运行状态 vs 事务结果”分层、入口准入矩阵和下一轮优化顺序；将 `staged → active` 明确标为局部事务能力，并明确能力缺口/IM 自动安装保持关闭 | 防止把单一路径的回滚能力误读为全路径统一提交，给发布决策提供可操作的准入条件 |
| 2026-08-29 | 能力缺口与 IM 安装路径补入桌面 durable compensation、checked 索引和最终严格审计；补偿快照覆盖目录发布前后，失败不再直接删除已发布目录 | 缩小自动安装路径的崩溃窗口，并保持共享提交器迁移前的 P0 限制 |
| 2026-08-29 | 禁止覆盖已有 `.prev`，禁止备份移动失败时删除旧安装；补偿记录增加目录备份意图与发布边界字段 | 防止陈旧备份被静默删除，以及跨设备/锁定场景下的不可恢复数据损失 |
| 2026-08-29 | 明确 `committed` 与提交后清理分层：新增 `cleanup_status`，清理失败保持阻断且不得反向回滚；补充非桌面能力缺口/IM 安装快照缺口和入口级发布准入 | 避免将“业务已提交”误报为“可执行/可上传”，并使自动安装与清理异常的处置边界可验证 |
| 2026-08-29 | IM 文件写盘在缺少 App/持久化事务上下文时 fail-closed；所有残留索引刷新改为 checked API；maclaw.app 依赖更新纳入目录补偿、`.prev`、checked 索引与最终审计顺序 | 消除非桌面 fallback 绕过，确保依赖更新与 Skill 安装遵循同一 durable compensation 边界 |
| 2026-08-29 | 核心 pipeline 与 GUI `CreateNLSkill`/`UpdateNLSkill` 接入共享 `SkillCommitter`；补偿记录增加 `transaction_state`/`cleanup_status`，重启恢复对已提交记录只执行幂等清理；新增提交成功、最终审计失败、已提交清理恢复和 GUI 索引故障测试 | 统一核心及创建/更新写盘协议，防止“最终审计已成功但进程崩溃”后错误回滚，并为剩余 GUI 入口迁移提供可验证基线 |
| 2026-08-30 | 增加一页式准入判定、证据等级规则和提交器实际映射；将持久化顺序明确为 config → YAML/目录 → index → audit，并列出本轮可复核测试范围 | 消除“接口模板=已完成 API”“日志成功=可执行”和事务顺序含糊造成的误判，明确当前仍需入口级迁移与故障注入 |
| 2026-08-30 | maintenance 文件型契约补丁记录事务级 rollback cleanup 路径；回滚时只清理本次新建的 `skill.yaml.vN`，提交成功保留用户可见版本备份；Windows YAML 回滚增加受限重试；补充“新备份残留”回归断言 | 关闭索引/最终审计失败后残留新版本备份、以及 Windows 文件占用导致误进入 `audit_pending` 的风险 |
| 2026-08-30 | `SetNLSkillStatus` 改用共享 `SkillCommitter`，状态 overlay 不再触发 YAML 重写；IM/非桌面维护执行改用提交器统一保存、checked 索引和最终审计边界；补充状态回滚与维护备份测试 | 消除状态 API 和 IM 维护路径绕过统一提交器、重复写盘或在索引失败后误报成功的风险 |
| 2026-08-30 | Hub 直接安装入口将 durable compensation 明确分为 prepared、committed 和 cleanup pending；最终审计后只允许清理队列，不再因清理失败反向删除已发布目录 | 统一直接 `InstallHubSkill` 与其它安装路径的提交后清理语义，降低崩溃/清理失败造成的误删风险 |
| 2026-08-30 | 补充传统 `App.AddSkill`/`App.InstallSkill` 与 `agentservice.persistImportedEntries` 入口审计；明确其仍属 legacy、未接入统一补偿/checked index/最终审计，并将无事务上下文 fail-closed、成功返回不可等同 `committed` 写入准入矩阵和测试门槛 | 防止 Hub/新目录迁移证据外推到 ZIP、插件和非桌面导入路径，避免分步写盘或 best-effort 审计造成假提交 |
| 2026-08-30 | AgentService `persistImportedEntries` 已迁移到共享目录提交器：增加 staging、批次提交、同内容 no-op、`.prev` 冲突拒绝、checked 目录扫描、最终审计和提交后清理；同一权威定义在 `overwrite=true/false` 下均可幂等重试；`NewService` 增加 `agentservice_install` 前缀的启动恢复与 fail-closed 门禁 | 防止 ZIP/GitHub/Hub/Market 多包导入产生部分安装或覆盖时丢失旧目录；明确启动恢复已具备基础实现，但多包/多租户/异常清理故障注入仍是上线阻断项 |
| 2026-08-30 | 继续收紧 legacy GUI：统一裸文件名 ZIP 的路径解析；`DeleteSkill` 增加安装锁、严格 metadata 解析、删除失败即停和原子 metadata 写入；AgentService 补充 `RecoveryScope` 与多租户路径归属校验，并同步修正故障注入缺口和发布口径 | 避免工作目录变化导致误读 ZIP、删除失败后 metadata 与目录不一致，以及共享队列在多服务场景误恢复其它服务目录 |
| 2026-08-30 | AgentService `DeleteSkill` 改为共享提交器隔离删除；contract 撤销纳入最终审计门，并在回滚时恢复原 contract；补偿记录支持 external snapshot，`NewService` 可跨重启恢复 contract；运行与上传入口增加 scope-aware 补偿门禁，并补充删除、越界恢复、contract 撤销失败、contract 快照恢复和运行/上传阻断回归 | 避免删除/导入在崩溃窗口丢失恢复路径，防止“已删除”审计与实际可路由 contract 不一致，以及未恢复或跨服务补偿记录继续执行/上传 |
| 2026-08-30 | 传统 GUI `App.AddSkill` 在最终审计成功后先持久化 `transaction_state=committed`/`cleanup_status=pending`，再清理包备份和补偿记录；`App.InstallSkill` 改为唯一外层 durable 记录并由 `addSkillLockedWithRecord` 复用，不再创建嵌套 durable 记录；文档同步区分 Add/Delete 的 legacy 补偿能力与 InstallSkill 的半统一事务 | 消除审计成功后的崩溃窗口误回滚，避免把内部局部审计误读为独立提交，同时明确仍缺统一 checked-index/结构化 API |
| 2026-08-30 | 传统 GUI `App.DeleteSkill` 在 metadata 更新后增加 checked routing index 发布；索引失败时先恢复目录与 metadata，再重建索引，确认恢复成功后才清理 durable compensation；新增删除索引故障回滚测试，并隔离 action prefix + Skill 的恢复范围 | 避免删除后索引仍暴露已删除 Skill，或在索引二次失败时提前清理补偿导致跨重启无法恢复 |
| 2026-08-30 | 修正 `App.InstallSkill` 最终审计失败的结果语义：只有审计已成功才返回 `legacySkillCommitError(committed=true)`；审计失败按未提交处理并由外层 durable compensation 回滚；新增 strict audit sink 故障测试 | 避免最终审计失败却被调用方误判为已提交，导致错误放行或错误处置 |
| 2026-08-30 | 补偿记录增加 `final_audit_kind`，启动恢复可通过严格审计行的 `request_id + skill + action + kind` 识别“审计已写入但 committed 标记未落盘”的崩溃窗口，仅执行提交后清理；新增 durable final-audit marker 回归测试 | 避免状态标记落盘失败后错误回滚已经审计的业务变更，同时拒绝以普通日志或未知决策推断提交 |
| 2026-08-30 | 将动态 contract 外部快照明确为版本化恢复契约：补充 schema、stable ID、tenant/user、digest、幂等结果和两阶段可观测语义；同步 P0 清单、入口矩阵、故障注入证据与后续实施顺序 | 避免把 opaque snapshot 或“撤销成功”日志误当作跨系统原子提交，明确恢复失败、格式未知和无法证明归属时的 fail-closed 处置 |
| 2026-08-30 | maintenance config-only 路径启用 `SkipIfUnchanged`，统一返回 `skipped/no_change`；补充 GUI maintenance 定向测试证据并修正 Restore YAML 提交器的字段边界 | 消除维护动作无实际变更时产生备份、补偿、索引和“已应用”审计的副作用，避免 no-op 配置误注入文件型恢复逻辑 |
| 2026-08-30 | IM maintenance 批处理补充逐 Skill 准入检查、config-only 提交器 no-op 短路和结构化 `state/cleanup_status/request_id` 返回；文件型契约草案明确转回桌面 reviewed flow | 防止批量维护只校验首个 Skill、将文件型草案误当配置变更，以及调用方把普通 `ok` 误判为可继续执行 |
| 2026-08-30 | 新增“文档状态卡”和一页式决策树，集中标明当前发布模式、可依赖能力、上线阻断范围、结果判定和章节冲突优先级 | 降低长文档中的重复结论与阅读歧义，避免把目标模板、局部迁移或单次测试误读为全入口已完成 |
| 2026-08-30 | 为传统 GUI `App.InstallSkill` 增加跨重启 pending 回滚和提交后清理失败回归，覆盖前向索引/回滚索引连续失败后的新实例恢复，以及 `committed + cleanup_status=pending` 结果 | 关闭“进程内 defer 可恢复”与“崩溃后 durable 恢复”之间的证据缺口，避免清理失败被误报为回滚，继续保持 legacy 安装自动化阻断 |
| 2026-08-30 | 修正 IM maintenance 批量提交的主 Skill 选择：完整列表仍由 mutator 原子发布，但审计/补偿主键改为首个实际变化项，并增加入口级保护说明 | 避免 `updated[0]` 未变化或与批次目标不一致时造成审计归属、补偿恢复和后续准入判断偏差 |
| 2026-08-30 | 修正桌面 `ApplySkillMaintenanceAction` 的删除/合并主键：当 mutator 移除目标时使用原始 pre-image 作为 `SkillCommitter` 输入；新增 AgentService committed 清理跨重启与不同 `DataRoot` 恢复隔离测试 | 防止单动作 maintenance 依赖 `updated[0]` 导致审计/补偿归属漂移，并证明重启清理不会回滚已提交目录或误领取其它服务记录 |
| 2026-08-31 | 补强 committed 清理重试语义：清理失败达到上限后持久化 `cleanup_status=needs_review`，保留已审计目录与补偿记录且停止自动重试；新增可注入清理失败和升级后保持阻断的回归测试 | 防止提交后清理异常被误回滚、无限重试或在无人工处置时重新放行执行/上传 |
| 2026-08-31 | 同步入口级证据边界：明确 AgentService 多包清理升级已在共享恢复层覆盖，但仍缺最终审计/contract 恢复失败的入口矩阵；传统 GUI `InstallSkill` 已覆盖 settings 写入失败与基础跨重启恢复，新增恢复器失败/快照损坏作为 P0 缺口；reviewed-draft 与 repair 的清理重试耗尽统一要求升级为 `needs_review` | 避免把共享层单测外推为所有入口已完成，并明确下一轮故障注入的最小闭环 |
| 2026-08-31 | AgentService 导入 hydration 继续补齐 `version`、运行时依赖、`requires_tools`/`fallback_for_tools`、toolset 与 fallback toolset 等权威字段，并扩展 round-trip 断言 | 防止下载适配器的部分运行时 entry 覆盖或丢失 YAML 中的执行契约，导致发布后行为与扫描结果不一致 |
| 2026-08-31 | 传统 GUI `AddSkill`/`DeleteSkill` 的提交后清理失败纳入 bounded retry：每次失败递增 `attempts`，达到 3 次持久化 `committed + cleanup_status=needs_review`；详细 API 保留该状态并新增回归测试 | 防止已审计结果被错误回滚、清理无限重试或兼容 API 将清理异常误报为普通失败 |
| 2026-08-31 | 将 capability-gap、混合安装与 IM 安装入口的提交后清理失败统一委托 `MarkEvolutionCompensationCleanupFailure`，共享递增次数、`needs_review` 升级和审计事件语义 | 防止不同安装入口在清理失败时各自覆盖状态、无限重试或遗漏人工复核信号 |
| 2026-08-31 | 补齐 `maclaw.app` 依赖更新的提交后清理边界：最终审计后先持久化 `committed`，`.prev`/队列清理失败统一进入 bounded retry 与 `needs_review`，并保留来源/UUID 更新回归证据 | 防止已审计依赖更新因清理异常被错误回滚或无限挂起 |
| 2026-08-31 | 共享 `SkillCommitter` 的提交后清理失败改用统一 helper，首次失败即持久化 `attempts=1`；`needs_review` 审计改用 strict sink，观测失败向调用方显式返回但不降低已提交记录的 fail-closed 等级；同步 legacy Add/Install/Delete 清理路径 | 防止进程内首次清理失败在重启后重新从零计数，并避免关键人工复核事件被 best-effort 日志吞掉 |
| 2026-08-31 | 将 committed 清理恢复路径也改为统一 helper，确保每次启动失败立即持久化 attempt；`needs_review` 记录保持幂等不重复计数；maclaw.app 依赖更新拒绝陈旧 `.prev`，避免覆盖未知归属的旧备份 | 收敛进程内、跨重启和依赖更新三类清理边界，防止重试预算重置或误删除用户备份 |
| 2026-08-31 | 将能力市场 MCP 配置安装改为独立 durable 配置事务：本地 Stdio/远程 HTTP 均记录文件快照、严格预审计/最终审计、no-op、失败恢复和 cleanup 状态；本地 runtime sync 的取消已贯穿启动、initialize 与 tools/list，审计与 operator summary 明确脱敏 endpoint/AuthSecret/headers；新增回滚清理失败保持 `audit_pending`、重试耗尽升级 `needs_review` 的状态隔离；恢复失败、远程同步持久化阻断和跨重启故障注入仍列为 P0 | 避免把配置型 MCP 写盘和目录型 Skill 提交混为一谈，同时防止运行时依赖未就绪或敏感字段泄露导致错误放行 |
| 2026-08-31 | 为能力市场 MCP runtime sync 增加独立非敏感持久化状态：失败进入 `pending` 并按退避重试，连续 3 次升级 `needs_review` 且阻断 `call_mcp_tool`/语义目录；成功 checked initialize + tools/list 后清除状态，状态损坏时 fail-closed；补充状态损坏、重试上限和恢复清除测试 | 使“配置已提交但运行时未就绪”跨重启可观测、可阻断且不会被 no-op 或后台 best-effort 健康检查误报为成功 |
| 2026-08-31 | 收紧 MCP 生命周期边界：managed entry 缺失 runtime 观测时默认 pending；配置更新重新建立 checked readiness；本地 managed 启动失败写入持久化 blocker，删除服务器同步清理状态；严格探测拒绝 JSON-RPC 协议级假成功；本地列表补充 pending/review 展示 | 关闭配置提交与运行时探测之间的崩溃窗口，避免旧健康缓存、重复 ID 或孤立状态导致错误放行 |
| 2026-08-31 | 修正 managed MCP 首次成功探测的状态生命周期：持久化 `ready` marker 作为跨重启的已验证证据；删除时使用与 registry 解耦的状态清理；managed 更新避免重复并发探测，手工服务器仍走兼容路径 | 防止“成功后删除状态又被 missing-state 规则再次阻断”，同时避免删除/更新期间的锁重入和重复网络探测 |
| 2026-08-31 | 收紧 MCP runtime 状态文件校验：`next_retry_at`/`updated_at` 非空时必须是 RFC3339；本地 MCP 从 marketplace-managed 切换为手工配置时同步删除旧 runtime blocker；补充缺失状态、时间戳损坏、删除清理和托管→手工转换回归 | 防止损坏状态绕过退避策略或把旧企业阻断错误传播到手工服务器，确保配置身份变更与运行时门禁同步 |
| 2026-08-31 | 本地 MCP 管理页改为先执行 checked sync、再刷新列表，并显式显示同步失败；不再吞掉启动/工具发现错误导致的 stale-ready 视图 | 确保持久化 runtime blocker 与前端可见状态一致，人工处置不会被“保存成功但运行时失败”的假象掩盖 |
| 2026-08-31 | owner-scoped 本地 MCP 客户端纳入同一 runtime readiness 边界：专用进程启动/工具发现失败会持久化 blocker，成功 checked discovery 会写入 `ready` marker | 防止共享客户端健康而专用客户端失败时只返回一次性错误，避免后续执行继续绕过 durable fail-closed 状态 |
| 2026-08-31 | managed 远程 MCP 的“Check now”改用 strict `initialize` + `tools/list`，失败写入 runtime blocker；人工检查可显式重置 `needs_review` 的重试预算，成功后由 strict probe 写入 `ready` marker | 提供受控人工处置入口，同时禁止普通 tools/list 健康结果绕过托管运行时门禁 |
| 2026-08-31 | 启动/unknown background remote probe 与 health loop 对 marketplace-managed MCP 统一改用 strict 探测并持久化失败；probe 使用可取消的 15 秒 context，避免超时后旧请求继续写入健康状态 | 关闭后台探测绕过 `initialize` 或超时请求迟到覆盖状态的窗口，保持运行时同步与人工检查同一协议 |
| 2026-08-31 | health loop 为每个 MCP 探测建立独立 15 秒 deadline，并在请求结束后立即释放 context；手工/普通服务器也不再受 30 秒 HTTP client 超时拖慢整轮同步 | 防止单个慢响应阻塞后续 managed readiness 修复，确保取消、超时和状态写入边界按服务器独立收敛 |
| 2026-08-31 | MCP 工具列表与 semantic inventory 在 runtime blocker 存在时停止网络发现/目录发布；只有已验证的 runtime 才能进入可用工具清单 | 防止“配置存在或缓存存在”被误报为可执行能力，令 inventory 与 `call_mcp_tool` 的 fail-closed 门禁一致 |
| 2026-08-31 | Wails 管理面隐藏 managed MCP 的 stale tools，并为 health loop 的每个探测增加独立 15 秒 deadline；新增缓存阻断和 deadline 回归 | 让管理面、semantic inventory 与实际调用共享同一 runtime readiness 证据，避免单个慢服务器阻塞整轮健康修复 |
| 2026-08-31 | 队列读取/恢复错误的对外诊断统一经过路径脱敏；`GetSkillEvolutionStatus` 与 `ListSkillEvolutionCompensations` 不再向 Wails/TUI/IM 泄露绝对路径，新增脱敏回归 | 保持补偿队列 fail-closed 的同时，避免本地目录结构、用户目录或快照路径通过错误摘要外泄 |
| 2026-08-31 | Wails `GetMCPServerTools` 与 `ListMCPServers` 在 managed runtime blocker 存在时隐藏旧缓存并禁止刷新；恢复 `ready` marker 后才允许只读查看；同步 AgentService 预取消测试与当前 renderer 描述协议 | 消除 semantic inventory、IM 工具查询和 Wails 管理面之间的 stale-ready 差异；明确取消发生在 host-owned work 启动前应返回 `dynamic_execution_cancelled`，外部副作用已发生但结果不可观测时才返回 `Unknown` |
| 2026-08-31 | 新增 managed MCP 启动恢复协调器：读取 durable runtime sync 状态，仅对退避到期的实例并行执行独立 15 秒 strict probe；成功持久化 `ready`，失败继续 bounded retry/`needs_review`，不让单个慢实例阻塞其它实例 | 关闭“配置已提交但重启后只等 60 秒 health loop、期间状态不可收敛”的窗口，保持跨重启 fail-closed 与可观测重试语义 |
| 2026-08-31 | external contract 补偿恢复达到重试上限时改用 strict `compensation_needs_review` 审计，并保留 durable 队列记录；新增外部恢复失败升级回归 | 防止 contract 恢复失败仅停留在普通错误日志，确保人工处置事件可观测且不会错误清理或放行 |
| 2026-08-31 | 桌面 maintenance 回滚改用 `restoreSkillsSnapshot`，绕过普通保存的 anti-wipe/overlay 投影，保证失败事务恢复完整 pre-image | 避免回滚沿用正常保存规则而合并新条目或丢失磁盘 Skill 字段，确保配置快照与索引恢复一致 |
| 2026-08-31 | 文件快照 post-image 围栏允许“已恢复到原 pre-image 但后续索引仍失败”的幂等重试，同时继续拒绝第三方并发摘要 | 关闭跨重启回滚第二次尝试被自身已完成的文件恢复误判为并发修改的缺口，保留 stale-writer fail-closed 保护 |
| 2026-08-31 | maintenance `merge_duplicate` 在 targeted 与批量执行层增加 distinct identity 校验；maintenance 审计主条目查找统一支持稳定别名；IM `refresh_index` 分支改为 checked-only 派生索引刷新；MCP 配置修订清除旧 probe request ID，并拒绝旧配置响应覆盖新 readiness | 防止别名或 malformed plan 将主 Skill 自身当作退休对象，避免 refresh-only 动作因不存在配置变更而制造假提交/假失败，并防止新配置沿用旧探测关联或被旧探测误标 ready |
| 2026-08-31 | 修复 legacy `InstallSkill` 内层包更新覆盖外层 `PostCommitCleanupPaths` 的问题：现在追加 package `.prev`，保留 ZIP staging 等既有清理目标；新增回归测试 | 防止外层最终审计后的跨重启清理遗漏输入 staging 文件，降低临时文件泄漏和补偿记录不完整风险 |
| 2026-08-31 | 修复 file-backed maintenance 的 `DefinitionWriterWithCompensation` 覆盖 planner rollback cleanup 列表的问题：版本化 `skill.yaml.vN` 现在追加到既有目标，而非替换 | 防止维护事务回滚时遗漏先前登记的临时产物，确保批量/文件型补偿清理计划完整 |
| 2026-08-31 | 收紧 Wails staged 安装失败路径：`CleanupStagingChecked` 错误不再被忽略，而是与未提交状态一起返回调用方 | 防止 staging 清理失败被误报为普通提交失败，确保调用方能够触发人工处置或后续 durable 恢复 |
| 2026-08-31 | 修复 `maclaw.app` 依赖更新在 pre-commit 回滚成功后未清理 durable compensation 的问题；现在清理失败会保留/升级补偿，清理成功才结束事务 | 避免已恢复的依赖因遗留 pending 记录长期阻断执行，并保持回滚清理失败的 fail-closed 证据 |
| 2026-08-31 | 文档口径修订：补偿队列缺失 `schema_version` 的记录已由 `MigrateEvolutionCompensationQueue` 原子迁移到 v1；损坏/未知版本仍保留 canonical 阻断文件并 fail-closed，人工处置 UI 已提供基础 retry/clear，损坏队列离线处置继续列为 P0 | 消除“已实现迁移”与旧版“尚无迁移工具”描述冲突，明确迁移只做可验证 schema 规范化，不改变恢复或清除权限 |
| 2026-08-31 | 补偿队列迁移读/校验/重写纳入同一进程级互斥区，避免并发事务在迁移期间追加的记录被旧快照覆盖；空字符串/`null` schema 也会触发规范化 | 收紧跨线程迁移的一致性边界，避免“迁移成功但丢失并发补偿记录”的静默数据损坏 |
| 2026-08-31 | 收紧补偿队列所有读-改-写边界：`ReplaceEvolutionCompensation`、`ClearEvolutionCompensation` 以及队列级恢复现在和 append、迁移一样在同一互斥区内完成读取、压缩和原子重写；新增并发替换回归测试，确认不同事务记录不会因旧快照覆盖而丢失 | 消除并发状态更新/清理/恢复与新事务追加交错时的静默丢记录风险；队列仍保持单一权威快照和 fail-closed 语义 |
| 2026-08-31 | 收紧传统 GUI `DeleteSkill` 的回滚失败路径：目录/metadata 恢复失败、索引重建失败或补偿清理失败统一递增 attempts 并委托 `MarkEvolutionCompensationRollbackFailure`，达到上限后进入 `needs_review` | 避免 legacy 删除路径在恢复失败时只覆盖状态而不消耗重试预算，导致跨重启无限重试或人工复核信号缺失 |
| 2026-08-31 | 将传统 GUI `InstallSkill`、ZIP 导入及其外层 settings/metadata/index 回滚失败路径统一接入 `MarkEvolutionCompensationRollbackFailure`；不再仅覆盖 `audit_pending` 状态，恢复重试预算和 `needs_review` 语义一致 | 避免不同 legacy 适配器在恢复失败时重试计数漂移，确保跨重启恢复最终可升级到人工处置而非无限自动尝试 |
| 2026-08-31 | 将 capability-gap、IM SkillMarket/Hub/install-only/tool 安装在“回滚已完成但补偿清理失败”分支统一接入 `MarkEvolutionCompensationRollbackFailure`；清理失败现在递增 attempts，并在上限后保留 `needs_review` | 避免安装入口只写入普通 pending 而绕过 bounded retry，统一进程内与跨重启人工处置边界 |
| 2026-08-31 | 补齐 capability-gap 与 IM 安装预审计失败后的补偿清理异常：清理队列失败不再只拼接文本，而是持久化回滚失败、递增 attempts，并向调用方保留完整错误上下文 | 防止预审计失败后的 durable 记录进入无预算的 pending 状态，确保队列损坏/清理不可用时继续 fail-closed 并可升级人工复核 |
| 2026-08-31 | `ApplySkillMaintenanceAction` 增加 `installMutex` 事务锁，将 maintenance 规划快照与安装/删除/其它 legacy 写入串行化，避免提交器使用过期的完整 Skill 列表覆盖并发变更 | 消除 maintenance 在规划与提交之间的 stale snapshot 覆盖窗口；仍需后续把更多 maintenance 分支迁移到共享提交器并补并发故障注入 |
| 2026-08-31 | IM `toolExecuteSkillMaintenancePlan` 的真实执行路径同步持有桌面 `installMutex`，覆盖从计划构建到批量提交的完整窗口 | 防止 IM 批量维护与 GUI 安装/删除或其它维护并发时互相覆盖完整 registry 快照；dry-run 仍保持只读，不获取写锁 |
| 2026-08-31 | 共享 `SkillCommitter` 的回滚清理失败改用 `MarkEvolutionCompensationRollbackFailure`，首次失败即持久化 `attempts=1`，并在达到上限时返回 `cleanup_status=needs_review`；新增清理失败回归测试 | 防止共享提交器与 legacy/恢复路径的重试预算漂移，避免回滚清理失败无限重试或被误报为已完成 |
| 2026-08-31 | 删除 IM 文件型安装路径中已被共享 `commitStagedSkillInstall` 覆盖的不可达重复事务编排，仅保留 config-only 注册分支 | 避免维护两套永远不会执行的目录提交逻辑，降低未来修复遗漏和状态语义分叉风险 |
| 2026-08-31 | 修正传统 GUI ZIP 安装的输入快照命名与空目录根文件回滚计划：快照保留源文件 basename，避免重复安装生成随机 metadata 包名；允许向既有空 skills 目录合并根级文件，并将每个新文件登记到 `CreatedDirs`，非空目录仍 fail-closed | 保持 legacy metadata 的幂等身份稳定，同时在不扩大删除范围的前提下支持安全的空目录导入；关闭“注释声称允许、实现却拒绝”的语义漂移 |
| 2026-08-31 | 修正 `App.AddSkill` ZIP no-op 判定：比较前将输入路径规范化为 metadata 使用的 basename，并补充绝对路径重复调用的零副作用回归 | 避免同一 ZIP 因调用方传入绝对路径而被误判为更新，重复写包、刷新索引并制造不必要的补偿记录 |
| 2026-08-31 | 新增 GUI 补偿人工处置闭环：`RetrySkillEvolutionCompensation` 与 `ClearSkillEvolutionCompensation` 均要求 `confirm=true`，按 request ID + Skill + action 精确匹配并写结构化审计；retry 仅重置该记录的有限预算，clear 仅允许 committed 且声明式清理目标已验证的记录；管理页继续隐藏快照正文 | 在不允许模型或只读接口越权的前提下提供可审计的人工恢复/清除，避免 `needs_review` 被启动自动重试绕过或被误删 |
| 2026-08-31 | 修复多包补偿恢复同时携带 `DirectoryMoves` 与 `CreatedDirs` 时的孤儿目录窗口：逐 move 恢复已有 `.prev`，再清理已发布的新目录；对仅有预发布 intent 且未标记 `Published` 的目标保持 fail-closed，不因并发出现同名目录而误删；新增全新、多包混合、已有路径保护和 intent-only 回归 | 防止批量导入在 durable enrichment 尚未落盘的崩溃窗口留下可执行孤儿目录，或把并发方创建的同名目录误当成本事务产物删除 |
| 2026-08-31 | 补偿队列读取阶段统一校验所有持久化文件目标为绝对路径，并拒绝 `CreatedDirs`/`DirectoryMoves` 中的相对路径；新增恶意相对路径回归 | 防止全局或启动恢复把相对路径解释到进程工作目录，意外删除工作目录/越权文件；队列异常继续保持 fail-closed |
| 2026-08-31 | `needs_review` 降级改为遍历并更新补偿记录中的全部 `AffectedSkills`，不再在首个成员已降级时提前返回；新增批次降级回归 | 防止多包事务只阻断首个 Skill，遗漏同一批次其它成员而继续执行或上传 |
| 2026-08-31 | 补偿恢复增加全量只读预检：在任何目录/YAML/文件快照写入或删除前校验 base64 载荷、文件目标类型、父目录和目录型备份对象；新增损坏 YAML 及 legacy InstallSkill 阻断回归 | 防止损坏或形状异常的 durable 记录先删除已发布目录、随后才因解码/权限错误失败，确保恢复失败保留队列并按 bounded retry 升级 `needs_review` |
| 2026-09-01 | IM `toolPatchSkill`（text/structured）统一改用 `SkillCommitter`：解析后的定义与运行时字段合并、YAML 原始字节和 `.patches.json` 同步写入，checked 索引与严格最终审计纳入事务；patch history 使用 `FileSnapshots`，共享提交器回滚时同步恢复 sidecar；损坏的 patch history 拒绝继续写入；新增 IM text/structured patch 成功、审计与补偿清理回归 | 消除 IM 更新路径直接写 YAML/patch history、非 checked 索引和 best-effort 审计造成的部分提交；索引/审计/sidecar 写入失败可恢复，崩溃后由 durable compensation 接管 |
| 2026-09-01 | 目录型 `commitStagedSkillInstall*` 与 config-only `commitNLSkillDefinitionAfterAdmission` 在 App 适配边界统一获取 `installMutex`；mixed config-only 安装和 capability-gap GitHub 注册改用同一 config 提交适配器 | 防止 Wails、IM、能力市场、后台更新和 legacy metadata 写入并发时互相覆盖完整 registry/`.prev` 状态；消除重复 committer 编排，确保 no-op、补偿、checked 索引和最终审计语义一致 |
| 2026-09-01 | IM patch 将 `installMutex` 下沉到最终 `commitIMSkillDefinitionPatch` 边界，移除 text/structured 入口的重复加锁；共享提交器锁边界纳入定向回归 | 保持 patch 的 YAML、sidecar、registry、索引和审计原子边界，同时让昂贵的解析/校验在锁外完成；避免共享提交器锁与入口锁嵌套死锁，统一进程内并发语义 |
| 2026-09-01 | `installedSkillForInstall` 在持锁提交阶段按 `HubSkillID`（并在已存在时校验 `SkillID`）解析单次 registry 快照；同名但身份冲突的 Skill 现在 fail-closed | 防止下载/扫描期间 registry 发生变更后，以过期指针或同名 legacy 条目误替换其它发布者的 Skill；兼容缺少历史 `SkillID` 的旧安装，并缩小双重读取造成的不一致窗口 |
| 2026-09-01 | 补充 IM patch checked 索引失败故障注入，验证 text 路径回滚 YAML、`.patches.json` 与派生索引；同步覆盖结构化/文本成功与失败语义 | 将 IM patch 的“已统一”从代码审阅扩展到入口级失败证据，避免只验证成功路径而漏掉索引 provider 故障 |
| 2026-09-01 | TUI `manage_skill patch` 增加进程内互斥、定义文件 compare-and-swap 与严格 patch history 校验；patch 审计写入失败时自动恢复 YAML/sidecar，并向调用方返回阻断错误 | 消除 TUI patch 并发覆盖及“定义已写入但审计失败仍返回成功”的部分提交；该修复不等同于接入共享 `SkillCommitter` 或 durable compensation |
| 2026-09-01 | TUI `manage_skill validate(auto_fix=true)` 增加数据根内目录移动式 durable compensation（`.tui-validate-prev-*`）、按 Skill 的待恢复补偿准入、互斥和严格开始/完成审计，启动可跨重启恢复；上传预检同样改为 `.tui-upload-prev-*` durable compensation，远端 submission ID 先写入外部提交标记，避免崩溃恢复误回滚已接受上传；成功后原子写入 `upload_status.json` 并要求严格上传审计；`skillhub install/update/install-github` CLI 的 config-only 写入改用共享 `SkillCommitter` 与进程内串行化，GitHub 批量导入采用单一补偿快照 | 降低 TUI 自动修复/上传在崩溃窗口和并发待恢复状态下的部分提交风险；上传远端提交后的回执/审计失败仍保留人工复核 blocker，缺 checked index，CLI 目录型 Skill 仍需后续迁移 |
| 2026-09-01 | IM 上传前可移植性自动修复与 `validate(auto_fix=true)` 的索引刷新改为 checked API；缺少 App 或索引失败时恢复目录快照并返回阻断，不再吞掉索引错误 | 防止上传/校验自动改写成功但派生索引未更新，或非桌面/失效上下文继续保留未经确认的写盘结果 |
| 2026-08-31 | `SetNLSkillStatus` 对已是目标状态的请求增加写盘前 no-op 短路，并在提交器层启用 `SkipIfUnchanged`；新增零副作用回归测试 | 避免重复状态操作产生无意义的补偿、索引刷新和审计事件，同时继续在入口前检查队列健康与准入门禁 |
| 2026-08-31 | `SkillCommitter` 拒绝非空注册表的 full-list mutator 返回 nil，新增回归测试并在提交前 fail-closed | 防止维护规划异常把 nil 误写成“删除全部 Skill”，避免一次错误计划造成不可逆的全量配置丢失 |
| 2026-08-31 | 核心 pipeline 对 LLM `should_disable` 结果改走 durable `SkillCommitter`，新增专用 `skill:repair_disabled` 事件及持久化失败回归 | 防止不可修复 Skill 只在内存中变为 `needs_review` 或吞掉保存错误，确保失败保留补偿并阻断后续自动执行 |
| 2026-08-31 | `SkillCommitter` 在简单 `AllowCreate` 路径增加请求身份与候选 `Name` 一致性校验，并补回归测试 | 防止审计/补偿按请求名记录而配置写入另一 Skill 名称，关闭 malformed create 的身份分叉窗口 |
| 2026-08-31 | 补偿恢复在每次外部/文件回滚失败后立即持久化 `attempts`、`LastError` 与 `needs_review` 状态，并保留尚未处理的队列尾部；新增模拟崩溃回归测试 | 关闭恢复循环在最终压缩写盘前的崩溃窗口，避免重启后重置重试预算或丢失其它事务记录 |
| 2026-08-31 | `PrepareSkillForUpload` 不再忽略完整性 manifest 的生成/写入错误；manifest 为空或写入失败时直接返回错误并阻断上传；新增 manifest 目标冲突回归测试 | 防止“便携检查通过但完整性证据未落盘”导致上传包与安装校验依据不一致 |
| 2026-08-31 | 完整性 manifest 改用原子写入，并在写入前拒绝 Skill 根目录、目录项或同名目标为符号链接/目录，避免 Windows 原子替换回退误删冲突目录；新增 manifest 目标形状保护 | 防止崩溃或恶意路径形状造成 manifest 截断、越权替换或删除目录 |
| 2026-08-31 | AgentService `ImproveSkill` 对无变更结果执行 no-op 短路，并将 `skill.improved` 审计改为严格错误处理；审计失败时恢复自动修复前的目录快照；新增回滚回归测试 | 防止自动修复已写盘但最终审计失败仍返回成功，保持改写、审计和可重试状态一致 |
| 2026-08-31 | 收紧 AgentService 与 GUI 生命周期的可移植性自动修复：修复器/预检/重新验证/安全扫描或打包提交在远端副作用前失败时统一恢复目录 pre-image；GUI 生命周期不再以“非致命”方式继续使用部分修复源目录 | 防止自动修复只写入 YAML 或 manifest 后即因后续失败留下半成品，确保上传重试从同一权威版本开始 |
| 2026-08-31 | AgentService 多包导入在创建 staging 前拒绝重复 Skill 身份或重复目标目录，并清理已创建的前序 staging | 防止同批次两个包争用同一发布路径，避免补偿计划出现重复 `DirectoryMoves` 或回滚顺序歧义 |
| 2026-08-31 | TUI `manage_skill upload` 在可移植性预检前建立同目录快照；预检、质量门禁、打包、Hub 地址解析或提交失败时恢复自动修复前置状态；打包器拒绝符号链接并传播关闭错误；新增快照恢复与符号链接回归测试 | 补齐 TUI 与 GUI/AgentService 的失败语义，避免 TUI 上传失败留下半修复目录或通过链接打包外部文件 |
| 2026-08-31 | AgentService `SkillToolBridge` 的 list/install/run/search/maintenance 入口增加 nil service 防御，避免宿主 wiring 缺失时 panic；动态目录仍保持 `catalog_incomplete` | 将宿主装配错误转换为可观测、fail-closed 的能力不可用结果，不让异常绕过 Skill 执行门禁 |
| 2026-08-31 | AgentService 删除/覆盖已有 Skill 前强制要求 dynamic capability registry 可用；registry 缺失时在目录移动或 staging 发布前拒绝操作 | 无法证明 contract 已撤销或可恢复时不允许改变权威目录，避免授权状态与文件状态分叉 |

| 2026-09-01 | 收紧完整性 manifest 验证：读取前检查 Skill 根目录和 manifest 目标类型；校验拒绝空清单、绝对/越界路径、重复规范化路径、非 SHA-256 哈希、符号链接、目录/特殊文件及清单外新增文件；安装器不再吞掉 manifest 读取错误；补充生成 manifest 的回验与攻击形状测试 | 防止 manifest 被利用读取 Skill 目录外文件、跟随链接或隐藏未登记可执行载荷；完整性证据不闭合时保持安装/执行 fail-closed |
| 2026-09-01 | 共享 staged GUI 安装在目录发布前执行 manifest 严格读取/校验；损坏或不可读清单会清理 staging 并阻断提交，避免新目录路径绕过完整性门禁 | 让 shared-create 与 legacy replacement 的完整性语义一致，避免仅在发布后校验导致半提交目录或“无清单即成功”的假象 |
| 2026-09-01 | 修复 managed MCP 启动恢复的本地批量错误计数：`LocalMCPManager` 已按实例记录启动/发现失败时，恢复协调器不再对整批重复递增 attempts；仅对尚未产生状态的条目补写失败 | 防止一个本地实例失败导致健康或已处理实例错误消耗重试预算，过早进入 `needs_review`；新增启动恢复回归测试 |
| 2026-09-01 | 本地 MCP checked 同步现在传播 readiness 状态写入失败；owner-scoped 客户端在无法持久化 ready 证据时会移除并停止未被跟踪的进程；能力市场本地/远程 runtime blocker 持久化错误也不再被吞掉 | 防止“进程已启动但 durable readiness 不可写”被报告为成功，确保 persistence failure fence 与调用方错误语义一致 |
| 2026-09-01 | 新增 Wails `RetryMCPRuntimeSync(serverID, confirm)` 受控人工处置 API 与管理页按钮：必须显式确认并限定 marketplace-managed MCP；按 server ID 重置 bounded blocker 后执行远程 strict probe 或本地 checked sync，失败继续保留 pending/needs_review，成功才写入 `ready` | 关闭本地 managed MCP 进入 `needs_review` 后只能等待后台重试的运维缺口，避免无确认或普通刷新绕过运行时门禁 |
| 2026-09-01 | AgentService 批量导入及其启动恢复的 external contract 回滚改为“先全量验证快照、后稳定顺序逐项恢复并聚合错误”；单个 Skill contract 恢复失败时，其他 stable ID 仍会恢复，durable compensation 保持 `audit_pending` | 防止 map 迭代中的首个 contract 恢复错误短路整批回滚或重启恢复，导致本可恢复的 sibling contract 无故继续 revoked；未恢复项仍 fail-closed 并保留跨重启处置证据 |
| 2026-09-01 | 传统 GUI `InstallSkill` 的 ZIP scan cache 改为只写本次归档实际发布的 Skill 目录，新增共享根已有 Skill 缓存不被覆盖回归 | 防止新包的扫描报告或状态误覆盖同一 `skills/` 根下既有 Skill 的独立安全证据，保持派生缓存最小副作用边界 |
| 2026-09-01 | 传统 GUI `InstallSkill` 现在拒绝未知安装位置，且检查发生在快照、补偿、解压、metadata/package 写入之前；新增零副作用回归 | 防止 malformed location 请求被误提交为“仅注册 metadata/package、未发布目标 Skill”的不一致安装 |
| 2026-09-01 | 禁用旧 `CleanupStaleNLSkills`/`SkillExecutor.CleanupStaleSkills` 的无确认直接写盘；入口保留兼容返回但不再保存 registry，stale retirement 只能走 approved maintenance plan | 防止自动 stale cleanup 绕过 durable compensation、checked index 与最终审计，避免静默状态改写或保存错误被吞掉 |
| 2026-09-01 | capability-gap 异步自动安装增加永久 rollout fence（`automaticCapabilityGapInstallEnabled=false`），不再启动后台搜索/下载/安装；`installSkillOnly` 在入口处 fail-closed，并保留人工 Marketplace/Skills 审核提示 | 防止“检测到缺口”被误当成用户授权，关闭后台自动联网和写盘副作用，直到完整灰度、租户隔离和故障注入证据齐备 |
| 2026-09-01 | IM `install-only` 与 `install_skill_hub` 的 config-only、目录新装和已有版本替换统一改走 App-owned `commitNLSkillDefinitionAfterAdmission`/`commitStagedSkillInstallWithExisting`；删除直接 `SkillExecutor.Register`、手写补偿、索引和审计串联，新增 config-only 提交回归 | 消除 IM 入口未来重新启用时的旁路事务，确保 config/目录、checked index、最终审计、清理和 bounded compensation 具有单一结果边界 |
| 2026-09-01 | 上传前自动生成 `skill_id` 改为原子持久化、写入失败即拒绝，并在 checked 索引发布失败时恢复 YAML 与派生索引；新增生成失败回滚与索引故障注入回归 | 防止 Skill 身份只存在于内存、重试产生不同 stable ID，或索引未发布却继续上传，收敛上传身份与路由状态的一致性 |
| 2026-09-01 | GUI 生命周期 `UploadNow` 的源目录可移植性修复纳入 install mutex 与 checked 索引发布；索引失败恢复源目录和索引后才返回，移除 repair/uninstall 完成后的重复 best-effort 刷新 | 防止直接生命周期上传与 IM 门禁语义分叉，并避免已提交事务之后的静默二次刷新掩盖派生状态错误 |
| 2026-09-01 | `prepareSkillDirForMarket` 对受管源目录的自动修复增加 install mutex、checked 索引发布与失败回滚；临时打包目录不触发本地索引更新 | 区分权威源目录与 staging 副本，避免生命周期/队列上传修复后索引陈旧，同时不制造临时目录的伪索引变更 |
| 2026-09-01 | App Studio 一键 SkillMarket 上传补接统一 evolution upload admission，并在打包前复用受管源目录可移植性/checked 索引门禁；新增待恢复补偿阻断回归 | 防止一键发布旁路队列健康、待恢复 Skill 或源目录索引一致性检查，避免网络提交绕过上传准入 |
| 2026-09-01 | `SkillLifecycleManager.UploadDirNow` 将 evolution upload admission 前移到 `prepareSkillDirForMarket` 之前；当调用方省略 Skill 名称时仅做只读定义解析后再门禁，并新增不可读队列下 `skill.yaml` 零写盘回归 | 防止目录上传的 portability auto-fix 在补偿队列损坏或待恢复时先改写权威 Skill，确保所有上传前置写盘都受 fail-closed 准入保护 |
| 2026-09-01 | `SkillExecutor.Rename` 兼容入口仅允许 App-owned executor 转发 `App.RenameNLSkill`；detached/stale executor 直接 fail-closed，不再执行旧的 best-effort YAML/目录/config 写入 | 收敛遗留执行器引用与 Wails 重命名入口的事务语义，防止直接 YAML/目录写入形成不可恢复旁路 |
| 2026-09-01 | App Studio 生成式 SkillMarket pack 上传增加全局补偿队列健康门禁；损坏/不可读队列在打包上传前直接 fail-closed，并新增回归 | 防止无具体 Skill 名称的生成式上传路径绕过补偿恢复状态，避免在队列异常时继续产生远端副作用 |
| 2026-09-01 | GUI 生命周期上传统一由 `MarkUploaded`/原子回执写入记录 `upload_status.json`，移除重复 best-effort 写盘；远端全部提交但本地回执失败时返回 `submission_id + error`，队列持久化 `remote_submitted` 并仅重试本地回执，成功后再转 `uploaded`；新增直接上传和队列恢复回归 | 防止远端已接收却被误报为本地完整成功，或普通重试再次向远端提交同一包；重启后可从队列保留的 submission ID 完成本地可观测状态 |
| 2026-09-01 | IM 安装后的首次执行失败改经 `App.SetNLSkillStatus(..., needs_setup)` 提交，缺少 App 事务上下文或提交失败时明确返回“状态未持久化、需人工复核”；不再直接调用 `SkillExecutor.UpdateStatus` | 防止首次运行失败仅更新 config 而绕过 checked 索引、严格审计和 durable compensation，并避免 UI 错报“已标记 needs_setup” |
| 2026-09-01 | GUI ZIP 导入恢复改用 `action=import + RecoveryScope=primarySkillsDir`；补充跨 scope 隔离回归，并修正事务测试夹具使全局补偿队列与 App 有效数据根保持一致 | 防止进程全局队列在配置加载后切换路径造成测试/运行时误判，同时确保 ZIP 导入不会领取其它服务或租户的待恢复记录 |
| 2026-09-01 | 共享 `SkillCommitter` 在回滚清理失败时保留原始提交阶段（`index_refresh_failed`/`final_audit_failed` 等）并将二次 rollback 错误附加到结果；GUI repair 映射据此继续输出可执行的阶段诊断，同时 durable 队列仍使用稳定的 `rollback_cleanup_failed` 标记 | 防止回滚失败把索引或审计根因隐藏成通用清理错误，便于人工处置且不改变 `audit_pending`/fail-closed 语义 |
| 2026-09-01 | 收紧 GUI `RenameNLSkill` 的 durable 外部目录契约：补偿记录在目录移动前登记 `oldDir → newDir` intent，移动越过边界后持久化 `dir_published`；同时保存原始 `skill.yaml` pre-image，并在同步/跨重启回滚时恢复目录与 YAML。最终提交不会把新的业务目录误列为 cleanup 目标；`RenameNLSkill`/`DeleteNLSkill` 均拒绝相对路径、文件系统根、符号链接和非目录，重命名额外拒绝 `.`/`..` 等路径型目标名；新增索引持续失败下的补偿记录回归 | 关闭重命名在“目录已移动、进程崩溃或索引/审计失败”窗口缺少目录/YAML 恢复证据的风险，避免提交后清理误删除重命名后的有效目录或把父目录当作 Skill 移动 |
| 2026-09-01 | 修复 GUI `RenameNLSkill` 的稳定别名事务：先将 `SkillID`/Hub/目录别名解析为 canonical `Name` 再交给 `SkillCommitter`，YAML 顶层 `name` 同样按 canonical 身份替换；准入检查同时遍历别名，待恢复补偿不能通过别名绕过；新增别名重命名与 canonical pending compensation 回归 | 防止别名请求被提交器误判为 `skill_not_found`，或在 canonical 补偿待恢复时借助别名继续改写目录/config |
| 2026-09-01 | reviewed-draft apply/disable/reject 的 draft 删除改为可注入 cleanup 边界；三条路径均新增提交后清理失败与恢复回归，确认返回 `committed + cleanup_status=pending`、保留已提交 overlay/YAML 与 draft，恢复器后续重试时先清理声明目标再移除队列 | 将 reviewed-draft 的提交后清理从“共享层理论覆盖”扩展为完整入口级可验证证据，避免 draft 删除失败被误报为回滚或成功 |
| 2026-09-01 | `DeleteNLSkill` 增加入口级 queue-clear 故障注入与跨重启回归；提交后 cleanup pending 时立即清除 executor/scanner 中可能包含隔离目录的缓存，并按 canonical Name 清除 config-only Skill 的别名缓存，重启恢复只清理 durable 队列、不重新创建已删除 Skill | 防止索引刷新曾短暂发布隔离目录、或别名删除只清除请求键时，已提交删除仍从内存暴露；保持 `committed + cleanup_status=pending` 的 fail-closed 语义 |
| 2026-09-01 | `TestSkillRunnerStartRunDoesNotWaitForExecutorMutationLock` 在持锁断言前预热一次性 memory/SQLite 初始化，避免把冷启动延迟误判为互斥锁阻塞；保留 Windows `app.shutdown` 清理 | 将并发回归聚焦于锁边界本身，降低 CI 机器冷启动和 memory.db 文件占用造成的非确定性失败 |
| 2026-09-01 | 新增 GUI `RenameNLSkill`/`DeleteNLSkill` 最终审计失败故障注入：验证目录、YAML 与 registry 在 final-audit 失败时同步回滚，且不会遗留错误的已提交补偿记录 | 将重命名/删除的“入口级最终审计失败”从共享提交器推断提升为可复核的 App 回归证据；Windows 锁定、持续清理失败和完整跨重启矩阵仍保持 P0 |
| 2026-09-01 | IM `toolExecuteSkillMaintenancePlan` 在非 dry-run 入口先检查补偿队列健康，即使计划最终是 refresh-only 或 no-op 也不会绕过 unreadable/pending recovery 门禁；新增 malformed queue no-op 回归 | 防止 no-op 快速路径掩盖损坏队列并错误返回可继续写盘的终态；dry-run 仍保持只读观察语义 |
| 2026-09-01 | 传统 GUI `AddSkill`/`InstallSkill` 在任何 metadata、settings 或目录副作用前统一检查补偿队列；legacy recovery 先于 skill admission 执行，以便损坏回滚记录累计 bounded attempts 并升级 `needs_review`；新增损坏队列零写盘与 `InstallSkill` 重试升级回归 | 防止 legacy 入口绕过共享进化队列门禁，在已有 YAML/目录补偿待恢复时继续注册或安装；同时避免 admission 过早短路导致恢复重试计数永远为 0 |
| 2026-09-01 | 收紧 Skill 生命周期的兼容/辅助写入口：`SkillExecutor.Register/Update/UpdateLearnedSource/UpdateStatus/UpdateVerification` 在持久化前统一执行 evolution compensation admission；load-time status overlay 与 `normalizeInstalledSkillEntry` 的 portability/quality 写入在队列不可读或目标 Skill 有 pending compensation 时 fail-closed；`RetryBlocked` 的 retry-all 逐项检查 Skill admission，并在 portability autofix/quality 写入前后复核队列 | 消除遗留执行器、异步状态叠加和安装后 normalization 绕过 durable compensation 的旁路；避免 retry-all 仅因省略 Skill 名称而重新处理待恢复条目，保证权威 YAML、config overlay 与上传队列在恢复期间保持零写盘/阻断语义 |
| 2026-09-01 | `SkillExecutor.Delete` 继续只转发 `App.DeleteNLSkill`；删除入口在异步 scanner 尚未发现或缺少可运行步骤的 external Skill 时，改用 `ScanSkillDirAll` 发现目录后仍走隔离目录、config/index/audit/cleanup durable 提交流程 | 保持兼容删除 API 不回退到 `RemoveAll` 直接删除，同时覆盖 external 根目录冷缓存和“无步骤定义”场景，避免用户可见 Skill 因 scanner 时序而无法安全删除 |
| 2026-09-01 | IM maintenance 的 review-trace 执行审计改为结果级 failure fence：memory audit 写入失败时即使 Skill 业务提交已完成也返回 `ok=false`/`review_execution_audit_failed`，并禁止触发后续自动 repair；增加可注入回归 | 避免“业务已提交但审计证据未落盘”被误报为成功，防止缺少 review 证据时继续扩大自动修复副作用 |
| 2026-09-01 | 收紧 `SkillExecutor` 兼容写入口：Register/Update/UpdateLearnedSource/UpdateStatus/UpdateVerification/MarkUploaded 在等待 `installMutex` 后重新执行补偿准入；上传回执校验非空 submission ID，并在损坏队列下阻断 `upload_status.json`/config 写入；status overlay 持久化也纳入同一互斥边界 | 关闭“准入检查后才出现 pending compensation”导致的竞态旁路，避免空回执或异步 overlay 在恢复窗口污染权威配置/上传状态 |
| 2026-09-01 | 生命周期未注册目录的 `persistUploadedSkillReceipt` 也纳入补偿准入与 `installMutex`，并统一由原子回执写入器拒绝空 submission ID；新增 ad-hoc 上传回执故障回归 | 防止 queue retry 的 ad-hoc 分支绕过恢复阻断，避免远端回执缺少 submission ID 仍被本地标记为已上传 |
| 2026-09-01 | `prepareSkillDirForMarket(autoFix=true)` 对所有 App 受管/未注册目录统一持有 `installMutex`，锁后再次检查补偿队列；新增 ad-hoc preflight 损坏队列零写盘回归 | 关闭未注册目录 portability 自动修复在并发恢复窗口绕过队列门禁的旁路，避免先改写权威 `skill.yaml` 再发现队列异常 |
| 2026-09-01 | `SkillExecutor` 执行统计（普通与 stable-binding 路径）改为在 `installMutex` 内复核 compensation admission，保存失败改为可观测日志而非静默吞错；新增 usage 写回阻断回归 | 防止执行后的统计 RMW 与安装/回滚并发，或在补偿队列损坏时继续改写 config，避免“运行成功但证据写盘未受治理” |
| 2026-09-01 | `normalizeInstalledSkillEntry` 在 portability 预检后、quality_status 写入前再次取得 `installMutex` 并复核准入 | 避免预检结束到派生质量证据落盘之间出现新的 pending compensation，保持 normalization 的 fail-closed 语义 |
| 2026-09-01 | 共享 staged 安装提交边界在取得 `installMutex` 后再次执行全局/Skill compensation admission；`SkillRunner.updateUsageStats`、`RecordWorkaround` 与 stable-binding 统计释放锁后才发送 UI 事件，保存失败不再伪报更新；`RestoreSkills`、`RestoreSkillYAMLBackup` 和 legacy maclaw.app registry 更新补齐相同锁顺序与队列门禁 | 关闭“下载/扫描期间队列状态变化后仍发布目录或回写统计”的竞态，避免运行时回调、备份恢复和遗留依赖更新绕过安装事务；统计写入失败现在可观测且 fail-closed，已提交业务不会因清理/指标失败被误报为可执行授权 |

> **2026-08-31 代码同步**：传统 GUI `InstallSkill` 的 settings 回滚不再依赖进程内 best-effort defer；恢复统一由 durable `FileSnapshots` 执行，回滚错误会保留补偿记录并阻断后续操作，新增跨重启 settings 恢复回归测试。另新增 managed MCP 启动恢复协调器：跨重启且退避到期的 runtime blocker 会按实例并行执行 bounded strict probe，成功才写入 `ready`，失败继续沿用 durable retry/`needs_review` 门禁。远程 MCP 的 managed→manual 更新会清除 marketplace-only blocker；认证/租户 headers 变更会失效旧 session 与工具缓存。安装/导入适配器在已持久化补偿记录上的回滚失败统一走 bounded `MarkEvolutionCompensationRollbackFailure`，并用 `ReplaceEvolutionCompensation` 更新原记录，避免追加重复 live snapshot；共享 `SkillCommitter` 现在显式返回补偿状态持久化失败，不再静默吞掉 rollback queue 写入错误；传统 GUI legacy metadata、外层 `InstallSkill`、ZIP 导入，以及能力缺口/IM 安装回滚清理路径也不再忽略补偿状态持久化错误，失败会继续向结构化结果或安全日志传播。补偿队列检测到 malformed JSONL 时保留 canonical 阻断文件，并创建限频 forensic copy/reason；显式缺失 `schema_version` 的旧记录可通过 `MigrateEvolutionCompensationQueue` 原子升级到当前 v1 表示。恢复队列始终保持单一权威记录并在重试耗尽时升级 `needs_review`。新增审计证据：预审计失败时，能力缺口/IM 安装会同时报告补偿清理失败，避免把未清理队列误报为已回滚。maintenance 的 targeted/批量 duplicate merge 现在拒绝同一稳定身份的主/关联 Skill，且审计主条目查找支持稳定别名，避免别名碰撞自禁用或把合法请求误报为主条目消失。可解析补偿记录现支持 GUI 显式 retry/clear（需二次确认与三元身份匹配）；任何“仍仅只读”的旧描述仅适用于本轮实现前的历史基线，损坏队列仍只能离线人工修复。
