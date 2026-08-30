# MaClaw GUI Skill 自进化逻辑评审与优化方案

> 范围：`gui/`、`corelib/skill/`、`corelib/tool/`、`corelib/agentservice/` 与 GUI Skill 管理页。评审基线：2026-08-30；文档修订：2026-08-30（事务语义、入口边界与证据边界优化）。本文区分当前实现、目标设计和上线前优化项；“已落地”只表示代码和回归测试已覆盖，不等于所有写盘路径都具备同等级事务保证。

> 本轮实现进展：核心 pipeline、GUI 创建/更新、GUI repair、reviewed-draft apply，以及 staged 激活已实际调用共享 `SkillCommitter`；reviewed-draft disable/reject、maintenance、重命名和删除也已接入共享提交器的主要事务边界。ClawHub/GitHub mixed-install 的 config-only 注册、managed capability external/Hub 的新目录安装、能力缺口 Hub 新目录安装、能力缺口 GitHub 配置注册、IM install-only 新目录安装和 IM tool 新目录安装已迁移到共享提交器；AgentService 的 GitHub/Hub/Market/ZIP 导入现已通过共享目录事务提交，使用 `.prev`、checked 目录验证、最终审计和提交后清理；AgentService `DeleteSkill` 也使用隔离删除事务，并由 `NewService` 在重启时恢复删除隔离目录；传统 GUI `App.AddSkill` 已增加 `legacy_gui_skill_add` durable compensation、checked index、最终审计、**审计后先持久化 `committed + cleanup_status=pending` 再清理**，并在启动/操作前 fail-closed 恢复；`App.DeleteSkill` 已增加同等的 checked index 发布与回滚恢复；`App.InstallSkill` 现已在目录/settings mutation 前创建唯一 `legacy_gui_skill_install` 外层 durable 记录，在最终审计后先标记 committed 再清理，但仍未达到共享 `SkillCommitter` 的统一提交器/结构化 API 语义。补偿记录新增 `transaction_state` 与 `cleanup_status`，跨重启恢复会区分“已提交但清理待办”与“需要回滚”，避免误恢复已审计版本；BM25 路由已支持可替换的 checked index provider，provider 故障会向提交器返回错误。不能据此宣称 GUI 全入口已迁移。

> **使用方式**：开发实现以“当前实现”列为准，验收以“必须/不变量/量化门槛”为准。若本文与代码行为不一致，应先补测试和审计证据，再更新文档，不能用文档替代安全门禁。

> **本次文档优化重点**：统一“外部目录发布”和“Skill 配置提交”的先后语义；明确 `committed`、运行状态和提交后清理是三条独立状态轴；把“共享提交器已覆盖”与“入口仍有局部编排”分开描述；补充生产实例不得省略最终审计/checked 索引的 fail-closed 约束。以下目标模板不表示代码已经暴露同名公共 API。

> **精简口径（2026-08-30）**：本文后续若出现旧版“多个入口已统一”与当前入口矩阵不一致，以第 3.5、10.2.2 和 14.2 的逐入口证据为准。新目录安装与已有版本更新是两种不同风险等级；前者已迁移的事实不能外推到后者。传统 GUI ZIP/插件安装仍未统一；AgentService 导入已具备 `NewService` 启动恢复基础，并按 `RecoveryScope`/目录路径校验服务归属，但多包/多租户/清理故障注入尚未完成。所有自动安装/自动写盘继续关闭，直到 P0 退出条件全部满足。

> **本轮代码复核证据（2026-08-30）**：已通过提交器/补偿、Hub 更新、managed Hub 回滚、安装与能力市场定向组合测试；还覆盖了 capability-gap、IM 安装、AgentService 导入/启动恢复和 legacy ZIP 路径解析相关定向测试。传统 GUI 已新增 `InstallSkillDetailed`、`AddSkillDetailed`、`DeleteSkillDetailed` 结构化结果视图，并通过 settings 写入失败回滚测试；兼容 Wails API 仍保留 error-only 签名。具体命令见第 14.2；这些结果只证明列出的提交器、回滚和安装边界，不代表 maintenance、传统 GUI ZIP/插件完整事务或所有导入更新入口已达到同等级覆盖。

## 一页决策摘要

当前版本的正确发布结论只有一句：**允许观察、生成草案和人工审批；禁止把未完成统一事务迁移的入口恢复为自动写盘。**

对任意 Skill，执行、上传和下一次自动写盘必须同时满足以下准入条件：

```text
admit(skill) =
  runtime_status == active
  && latest_transaction_state(skill) == committed
  && latest_cleanup_status(skill) == clear
  && compensation_queue_healthy(skill)
  && audit_available
  && index_digest == definition_digest
  && dependency_ready_if_declared
  && no cancellation/timeout/shutdown
```

这里的 `latest_transaction_state`/`latest_cleanup_status` 指该 Skill 最近一次事务的结构化结果，不要求把事务字段复制进 YAML。依赖未声明时 `dependency_ready_if_declared` 为真；声明了运行时依赖时必须单独检查依赖任务状态。

任一条件无法证明即拒绝（fail-closed），而不是根据“最近一次日志成功”推断放行。`compensation_queue_healthy(skill)` 至少检查全局队列可读，并确认该 Skill 没有 `audit_pending`、`needs_review` 或 `cleanup_status!=clear` 的记录；若队列损坏或无法解析，则全局阻断。`committed` 是业务提交结果，不等于 `active`；`cleanup_status=clear` 也不等于已获得激活授权。

本文后续每个“已落地”结论都应能对应到代码路径和自动化断言；只有设计文字、手工验证或日志输出时，最高只能标记为“部分落地”。

## 目录

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
| 什么时候允许上传？ | 仅提交器返回 `committed`、最终审计成功、补偿队列健康且 `cleanup_status=clear`；`rolled_back`、`audit_pending`、`needs_review`、`cleanup_pending` 一律禁止上传。|
| 当前是否适合开启自动写盘？ | 不适合。核心 pipeline、创建/更新、reviewed-draft、staged 激活及多个新目录安装分支已切换到共享 `SkillCommitter`，但 maintenance 边缘分支、已有版本更新/GitHub 导入和入口级故障注入仍未完成；默认保持观察/草案/人工审批模式。|

## 1. 结论

> **实现状态修订（2026-08-30）**：直接 `InstallHubSkill`、mixed-install 配置注册、managed capability external/Hub 新目录安装、能力缺口 Hub 新目录与 GitHub 配置注册、IM SkillMarket/Hub 新目录安装及 IM tool 新目录安装已接入共享 `SkillCommitter`/`commitStagedSkillInstall`。这些迁移不覆盖已有版本更新及其它 legacy adapter；相关入口仍必须按人工/灰度和 fail-closed 规则处理，自动安装整体继续关闭。

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

**已落地的安全基线（不等于 P0 全部关闭）**：高风险安装无确认时拒绝；无效 YAML 不注册；Gate 为 `passed/failed/unverified` 三态；未知错误不自动修复；自动发现默认为 `staged` 且隔离于路由、执行和上传；普通状态 API 不能绕过激活门禁；审计健康、失败聚合、队列合并、同 Skill 串行和跨 Skill 并行可用；repair context 已贯通 GUI LLM、Gate、扫描、写盘和上传复核的取消链路；失败尝试在 pipeline、GUI 和 reviewed-draft 路径统一计数，达到 3 次自动转 `needs_review`。这些能力只能说明“已具备安全护栏”，不能替代统一提交器、真实可失败索引 provider 和补偿运维闭环。

**当前版本的发布判断**：核心 pipeline、GUI `CreateNLSkill`/`UpdateNLSkill`、GUI repair、reviewed-draft、staged 激活、重命名和 `DeleteNLSkill` 已通过共享 `SkillCommitter` 获得统一结果协议与持久化补偿；AgentService 的 GitHub/Hub/SkillMarket/ZIP 导入也已经进入共享目录事务。传统 GUI `App.AddSkill`/`App.DeleteSkill` 已具备独立 durable compensation、checked index 刷新与最终审计；`App.InstallSkill` 现已在外层建立唯一 durable 恢复记录、登记目录路径、纳入 checked index 刷新并在最终审计后先标记 committed 再清理。其内部仍由 `addSkillLockedWithRecord` 完成 metadata/package 编排，但复用外层记录且不再产生嵌套 durable 记录或中间“已安装”审计；这属于“单一恢复记录、局部编排”，不是共享 `SkillCommitter` 语义，返回值也未统一为共享提交结果。maintenance 的部分分支、已有版本更新仍存在独立编排。因而自动改写/安装入口仍按“上线阻断”处理，默认只允许观察、生成草案和人工审批；只有 S1 退出条件全部满足后，才可逐步恢复自动写盘。这里的“上线阻断”按入口执行，不影响只读观察、审计查询和人工审批。

**仍需限制性解读的能力**：worker timeout 已配置化并可在 GUI 编辑（默认 180 秒，范围 30–1800 秒），取消、超时和 shutdown 已产生结构化事件；staged 重启/扫描、overlay 防提升和 GUI request-level 任务列表已补充。核心 pipeline、创建/更新、GUI repair、reviewed-draft、重命名、删除和 staged 激活已将主要 config/YAML/index/最终审计步骤纳入共享提交器；新目录安装分支也已接入共享安装提交器，但 maintenance 边缘分支、已有版本更新、GitHub 导入和其它 legacy adapter 仍未完全统一，补偿恢复事件和人工处置 UI 仍不完整。人工路径已改为使用策略摘要生成的 `config_revision`，不再使用固定 `manual` 占位值。能力缺口/IM 的非桌面实例仍缺少同等级持久配置快照，且自动安装继续关闭。非 Bash replay 目前已有显式接口和边界测试，真实生产隔离 adapter 尚未实现。统一提交器、真实可失败索引 provider 和补偿运维闭环完成前，自动改写入口应按“上线阻断”处置。

### 当前实现与目标的差异矩阵

| 能力 | 当前状态 | 主要限制 | 临时处置 |
|---|---|---|---|
| 自动 repair / optimize | **部分落地（主路径已补偿）** | file-backed repair 只生成 draft；pipeline 的内存型 repair/optimize、GUI repair 和 reviewed-draft apply 已对 config、YAML、索引和最终审计做失败补偿；maintenance 仍有边缘分支未完全统一 | 保留冷却；统一提交器验收前关闭自动写盘 |
| `staged → active` | **部分落地（共享提交器 + 入口门禁）** | 必须走 `VerifyAndActivateNLSkill`；普通状态 API 不能绕过；提交器已覆盖主事务，但真实索引故障注入和跨重启验收仍不足 | 失败保持 `staged`；回滚不完整进入补偿；补偿清理失败继续阻断 |
| 取消 / timeout | **已落地（核心 pipeline + GUI repair）** | request、attempt、termination、failure_reason 已进入 pipeline 事件；GUI repair 使用 worker context 并在写盘前后检查取消 | 取消后禁止 Apply；写盘前再次检查 context |
| strict 审计补偿 | **部分落地** | 核心 pipeline、创建/更新、GUI repair、reviewed-draft、staged 激活以及重命名/删除主路径已通过 `SkillCommitter` 纳入最终审计与 durable compensation；AgentService 的 GitHub/Hub/SkillMarket/ZIP 导入亦已迁移到共享目录事务，并在 `NewService` 启动阶段按 action prefix 恢复。maintenance 边缘分支、已有版本更新和传统 GUI 安装仍是多套编排，字段校验、批次故障注入和多租户恢复尚未完全统一 | 审计不可用、队列不可读、恢复失败或存在待恢复记录时 fail-closed |
| 非 Bash replay | **部分落地** | 已有 `NonBashReplayAdapter` 契约和 mock 边界；尚无真实隔离生产 adapter，无 adapter 时只能 `unverified` | 禁止将 mock 结果当真实通过 |
| GUI 任务列表 | **已落地（基础版）** | 展示 pending/running、request ID 和取消；终态历史仍依赖审计列表；补偿摘要已在管理页展示（只读） | 增加终态结果、取消确认和失败原因过滤 |

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
| NudgePromoter | `corelib/skill/nudge_promoter.go` | 工具序列沉淀为 staged Skill |
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

流程为 `LLM YAML → 解析 → 语义校验 → 安全扫描 → staged 注册`。自动发现定义固定为 `status: staged`，排除于 Prompt/BM25 路由、GUI Runner、动态目录、Coding SubAgent、Agent Service 和自动上传。只有显式验证、审批和原子提交完成后才可 `active`。

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
| 状态/激活 | `SetNLSkillStatus`、`VerifyAndActivateNLSkill` | `VerifyAndActivateNLSkill` 与状态写入均经过共享 `SkillCommitter`；active 门禁、验证元数据、checked 索引和最终审计失败补偿统一 | 补充状态入口 cleanup 失败、跨重启和最终审计故障注入 |
| YAML 版本恢复 | `RestoreSkillYAMLBackup` | **已接入共享 `SkillCommitter`：**版本恢复、config/index/final-audit 统一；索引或审计失败恢复旧 YAML/config，失败时保留 durable compensation | 补充多版本恢复、cleanup 失败和跨重启故障注入 |
| 草案审核 | reviewed-draft apply | **已接入共享 `SkillCommitter`：**应用期间暂停状态 overlay，保留草案快照，config/YAML/index/final-audit 统一；draft 删除为提交后清理 | 补偿清理失败时保持 `cleanup_status=pending`；仍需跨重启清理回归 |
| 草案审核 | reviewed-draft disable/reject | **已接入共享 `SkillCommitter`：**暂停状态 overlay，config/index/final-audit 统一，draft 删除为提交后清理 | 补清理失败/跨重启故障注入；自动化调用仍保持关闭 |
| 维护动作 | `ApplySkillMaintenanceAction` | **已接入共享 `SkillCommitter`（主要路径）：**事务期间暂停状态 overlay，config/YAML/index/final-audit 统一，checked 索引失败可恢复；文件型契约补丁将本次新建 `skill.yaml.vN` 登记为 rollback cleanup，提交成功保留备份、回滚只清理本次产物 | 补充 merge 多条目、cleanup 失败和跨重启故障注入；自动维护仍保持灰度 |
| 重命名 | `RenameNLSkill` | **部分落地：**已使用共享 `SkillCommitter`；目录移动、YAML 原子写回、config/索引/最终审计失败可恢复，回滚不完整时进入 durable compensation | 补充审计失败、重启补偿、Windows 锁定和 cleanup pending 故障注入；清理 worker 与统一 API 仍待完善 |
| 删除 | `DeleteNLSkill` / AgentService `DeleteSkill` / 传统 GUI `App.DeleteSkill` | **部分落地：**GUI `DeleteNLSkill` 已使用共享 `SkillCommitter`；AgentService 删除先移动到 `.skill-delete-pending-*` 隔离目录，再在最终审计前撤销 contract 并完成目录/索引校验，最后清理隔离目录；传统 GUI `App.DeleteSkill` 已在 legacy 边界内先持久化 metadata 快照、再隔离目录和原子更新 metadata，随后执行 checked routing index 刷新，并在最终审计成功后标记 `committed + cleanup_status=pending`。索引、撤销、审计或后续事务失败时，回滚会恢复目录、metadata 与索引，并尽力恢复原 contract，避免留下“已删除”审计却仍可路由的矛盾状态 | 补充 contract 恢复失败、清理失败、跨重启和入口级故障注入；传统 GUI 删除仍未接入共享提交器与统一结构化返回，对外不得把 `nil` 返回解释为统一 `committed` |
| 导入/安装 | ZIP、Hub/GitHub 导入 | 直接 `InstallHubSkill` 的新目录发布、ClawHub/GitHub mixed-install 的 config-only 注册、managed capability external/Hub 新目录发布、能力缺口 Hub 新目录与 GitHub 配置注册、IM install-only/SkillMarket/Hub 新目录发布及 IM tool 新目录发布已迁移到共享 `SkillCommitter`；AgentService GitHub/Hub/Market/ZIP 导入也已迁移到共享目录事务；传统 GUI ZIP/插件安装、已有版本更新、HubCenter SkillMarket 更新和其它导入分支仍为入口级 durable compensation/legacy adapter；`App.InstallSkill` 已在目录/settings mutation 前持久化唯一外层 `legacy_gui_skill_install` 记录，并在最终审计后先写 committed 再清理；安装期间暂停 status overlay；已有版本更新保留 `.prev` 恢复证据 | 仍是多套编排，尚未覆盖所有目录型更新/导入入口；`App.InstallSkill` 尚未复用共享 `SkillCommitter` 或统一结构化返回，内部 `addSkillLockedWithRecord` 复用外层记录且不再产生中间 legacy 安装审计；AgentService 已具备 `NewService` 启动恢复基础，但完整故障注入、多租户隔离和异常清理验收未完成；能力缺口/IM 的非桌面实例仍缺少同等级持久配置快照；真实故障注入与剩余入口迁移仍待完成 |
| 传统 GUI 安装 | `App.AddSkill`、`App.InstallSkill`、`App.DeleteSkill`（`metadata.json`、ZIP 解压、插件 settings） | **仍未纳入共享 `SkillCommitter`，但已收紧 legacy 边界：**安装/删除串行锁；`App.AddSkill` 在首次破坏性移动前持久化 metadata/目录补偿，写入后刷新 checked index，最终审计成功后先写 `transaction_state=committed` 再清理；`App.DeleteSkill` 先持久化 metadata 快照并隔离包目录，写入后刷新 checked index，索引失败会先恢复目录/metadata 再重建索引；ZIP 文件先写临时快照；`App.InstallSkill` 在目录/settings mutation 前持久化外层 `legacy_gui_skill_install` 记录，解压后补写新目录路径，最终审计成功后先标记 committed 再清理；metadata/settings 使用原子写入和严格 JSON 解析；已有 ZIP 目录/顶层包更新在迁移前直接拒绝。新增 `InstallSkillDetailed`、`AddSkillDetailed`、`DeleteSkillDetailed` 结构化结果视图，但兼容 API 仍返回 `error`，且结果只代表 legacy adapter 的分类，不等同共享提交器承诺 | 自动安装和自动更新保持关闭；仅允许人工确认并在失败后人工核对目录、配置、索引和审计；legacy 结果必须同时检查 `state` 与 `cleanup_status`，清理失败需人工处置 |
| 非桌面导入 | `corelib/agentservice.persistImportedEntries` 及其 `SkillInstall` 调用链 | **已纳入共享 `SkillCommitter`（目录型边界）。**多包导入作为一个批次提交；目录发布前持久化移动意图，更新保留 `.prev`，发布后执行 checked 扫描和最终审计，清理失败返回 `committed + cleanup_status=pending`；`NewService` 启动阶段按 `agentservice_install` 前缀和 `RecoveryScope` 执行专用恢复，旧记录仅在所有目录路径都证明属于当前服务根目录时才接管 | 仍缺多包中途失败、最终审计/清理失败、多租户目录隔离和跨重启异常场景的完整故障注入断言；恢复失败、队列不可读或 pending 记录会 fail-closed，自动导入继续关闭 |

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
{"skill_evolution_enabled":false,"skill_maintenance_observation_enabled":true,"skill_evolution_max_concurrent_workers":2,"skill_evolution_worker_timeout_seconds":180}
```

上面的值是当前安全默认值，不是目标配置。只有 P0 退出条件全部满足后，才允许按入口和 Skill 灰度把 `skill_evolution_enabled` 改为 `true`；自动安装仍需独立的人工确认和来源策略。

- `skill_evolution_enabled=false`：停止自动 repair/optimize/promote/install，保留查询、人工操作和治理审计。
- `skill_maintenance_observation_enabled=false`：停止只读观察、维护计划和经验采集，不关闭审计、状态查询或安全门禁。
- 并发上限实时生效，按 1–16 约束。`skill_evolution_worker_timeout_seconds` 已支持配置，默认 180 秒，范围 30–1800 秒，并在 GUI 状态中展示。

## 8. 风险与失败语义

| 场景 | 默认决策 | 业务写盘 |
|---|---|---:|
| 只读维护/经验采集 | allow | 否 |
| 低风险优化且 Gate=passed | candidate/apply | 默认关闭自动写盘；仅人工/灰度 |
| 文件型修复 | draft | 否 |
| Nudge 新 Skill | staged | 仅 staged 候选 |
| Hub/GitHub 安装 | review | 否，待确认 |
| 合并/退役/市场上传 | draft/publish_pending | 否，待确认 |

关键写盘采用“预检 → 备份 → 原子写盘 → 刷新索引 → 最终审计”。预检失败阻止写盘；最终审计失败回滚，无法回滚时进入补偿队列并显式标记，不能只记日志。删除 reviewed-draft 文件属于提交后的清理动作，不能先于最终审计；拒绝 draft 也必须先完成审计预检，并在审计失败时恢复 config 计数和 draft 文件。

> 当前发布策略：即使某条低风险路径具备局部回滚能力，也不能据此恢复全局自动写盘。只有入口完成共享提交器迁移、故障注入和跨重启验收，并满足 S1 退出条件后，才允许按 Skill/入口灰度开启。

**No-op 规则**：计划为空、候选摘要与当前权威摘要相同、或维护动作的 `ExecutedCount=0` 时，不创建补偿记录、不刷新索引、不写 `applied/committed` 事件，也不增加失败次数；应返回 `skipped/no_change`，并可写入一条轻量的 `decision=skipped` 审计事件（不得携带新的版本号或备份）。这样可以避免“空计划伪提交”以及无实际变更却生成版本备份。

安装入口也必须遵守幂等规则：下载包摘要与当前已安装版本相同则返回 `skipped/already_current`，不得重复发布目录、刷新索引或发送“新版本已安装”事件；同名但摘要不同才进入更新事务。发现目标目录或 `.prev` 冲突时必须拒绝并转人工处置，不能通过删除旧目录来“恢复安装”。

> **实现边界**：上述 no-op 是统一提交器的验收要求。当前共享新目录安装提交器已在直接 Hub 安装、mixed-install 新目录、managed capability 新目录、能力缺口 Hub 新目录、IM install-only 新目录和 IM tool 新目录路径执行同版本短路；仍需为已有版本更新、GitHub/ZIP 导入及其它 legacy adapter 逐入口补充“摘要相同即跳过”的回归断言。在断言补齐前，不得把 legacy 路径的返回成功或同名冲突错误解释为 `already_current`。

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
| 事务结果 | `prepared`、`committed`、`rolled_back`、`audit_pending` | 描述本次写盘是否完成、是否仍需补偿 | `rolled_back` 或 `audit_pending` |
| 提交后清理 | `clear`、`pending`、`needs_review` | 描述 `.prev`、staging、draft、补偿记录等提交后清理是否完成 | `pending`（保持阻断） |

`committed` 只表示本次定义、配置、（适用时）目录发布、索引和最终审计均成功；它不是把 Skill 自动设为 `active` 的授权。相反，`active` 必须再满足对应的验证和审批门禁。`committed` 但 `cleanup_status=pending` 也不能对外宣称“完全完成”：允许只读查询，不允许执行、上传或下一次自动写盘，直到清理幂等重试成功或人工处置完成。`audit_pending`、`needs_review` 或不可读的补偿队列同样属于 fail-closed。

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

清理属于 `committed` 之后的幂等动作，清理失败不能重新走反向回滚；但在清理完成前仍应对受影响 Skill 保持阻断，并通过重试或人工处置清除残留。任何入口若无法在“首次持久化变更前”写入补偿记录，必须停止写盘，而不是事后补记日志。

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
| GUI `persistRepairResultWithContext` | **已接入共享 `SkillCommitter`**：context 取消、状态 overlay 暂停、YAML/config/index/final-audit 统一；失败恢复或保留 durable compensation；scan cache 仍按非权威派生缓存处理 | 补充 cleanup 失败、跨重启恢复和生产索引 provider 故障注入；成功/阻断事件的字段校验仍需统一 |
| GUI `RestoreSkillYAMLBackup` | **已接入共享 `SkillCommitter`**：版本恢复写入、config/index/final-audit 统一；索引或审计失败恢复旧 YAML/config，失败时保留 durable compensation | 补充多版本、cleanup 失败和跨重启故障注入 |
| GUI reviewed-draft apply | **已接入共享 `SkillCommitter`**：config/YAML/index/final-audit 统一；draft 删除为提交后清理，失败保持 `cleanup_status=pending` | 仍需补清理重试、跨重启和入口级故障注入；不得据此开启自动 apply |
| GUI reviewed-draft disable/reject | **已接入共享 `SkillCommitter`**：config/index/final-audit 统一，draft 删除为提交后清理；失败可写 durable queue | 仍需补清理失败/跨重启和入口级故障注入；不得据此开启自动 disable/reject |
| managed capability / Enterprise install | Hub、外部源的新目录安装经过共享 `SkillCommitter` 的安全扫描、预审计、durable compensation、配置恢复、checked 索引和最终审计；已有版本更新仍由入口级适配器完成相同的 committed/cleanup 边界，发布前写入初始目录补偿，更新保留 `.prev`；最终审计后才进入提交后清理；已有 `.prev` 或备份移动失败时拒绝覆盖；maclaw.app 依赖更新也纳入同样的目录补偿与最终审计顺序 | 更新路径仍有重复安装编排，尚未全部复用共享目录安装提交器；真实故障注入和跨重启安装恢复测试待完成 |
| 能力缺口 / IM 自动安装 | **新目录与 GitHub 配置注册分支已迁移**：能力缺口 Hub 和 GitHub 配置注册、IM install-only/SkillMarket/Hub 和 IM tool 的新目录发布调用共享 `SkillCommitter`，并按稳定身份/版本执行 `skipped/already_current` 幂等短路；已有版本更新仍由入口级适配器负责目录补偿、checked 索引和最终审计；无 App 的非桌面文件写盘现在直接拒绝 | 非桌面实例不提供持久补偿，因此不能写盘；所有自动安装仍保持人工确认，补偿清理或索引/审计异常时 fail-closed |
| AgentService GitHub/Hub/Market/ZIP 导入 | **已接入共享 `SkillCommitter`（目录型）。**导入先在目标根目录 staging，再以批次发布；覆盖安装保留 `.prev`，陈旧 `.prev` 直接拒绝；最终检查每个目录都能重新加载并写严格审计，提交后才清理 `.prev`。同内容导入是零写盘 no-op；`NewService` 启动阶段按 `agentservice_install` 前缀和 `RecoveryScope=dataRoot` 执行专用补偿恢复；运行与上传入口复用 scope-aware pending 检查 | 仍缺多包中途失败、最终审计/清理失败、多租户目录隔离和跨重启异常场景的完整故障注入断言；scope 缺失/路径越界、恢复失败或 pending 记录会 fail-closed，自动导入、执行和上传继续关闭 |
| pipeline `runOptimize` / core-only repair | 通过 `persistDefinitionChange` 统一处理 config、YAML、索引和最终审计失败补偿；提交失败恢复内存 entry；仅 `committed` 且 `cleanup_status=clear` 触发成功事件/upload；不完整回滚可重启恢复 | maintenance 部分分支尚未完全复用共享提交器；索引 provider 的真实失败仍需生产实现 |

因此“写盘事务”目前标记为“核心 pipeline、GUI 生命周期主要入口和 AgentService 目录导入已统一到共享提交器；新目录安装分支已迁移，但已有版本更新、传统 GUI ZIP/插件安装及部分维护路径仍各自具备局部可恢复边界，全路径仍部分落地”。`SkillCommitter` 返回 `committed`、`rolled_back`、`audit_pending` 三态，并附带 `request_id`、`backup_version`、`config_revision`、`rollback_complete` 与独立的 `cleanup_status`；调用方只有在 `committed + cleanup_status=clear` 时才能发送成功事件或 upload。最终审计通过 `FinalAuditor` 作为提交步骤执行。所有已接入路径的不完整回滚都会写入 durable queue；目录发布前还会记录确定性的 `CreatedDirs`，已有版本更新保留 `.prev`，避免崩溃窗口丢失恢复路径；存在陈旧 `.prev` 或无法移动旧目录时，安装会拒绝覆盖而不是删除旧版本。最终审计成功后才允许清理旧备份和补偿记录；清理失败不得反向回滚已审计版本，而是继续阻断并等待幂等清理。能力缺口/IM 的非桌面实例仍缺少同等级持久配置快照，因此自动安装必须保持关闭或改为人工确认。

> 口径修订：上一句中的“多个 GUI 入口仍各自具备局部可恢复边界”仅描述全文评审基线。以当前代码为准，GUI repair、`RestoreSkillYAMLBackup`、`reviewed-draft`、`RenameNLSkill` 和 `DeleteNLSkill` 已迁移到共享提交器的主要路径；managed capability external/Hub、能力缺口 Hub/GitHub 配置注册、IM SkillMarket/Hub 新目录和 IM tool 新目录安装也已迁移，但已有版本更新和其它导入路径仍为入口级适配器；剩余未统一范围是 maintenance 边缘分支及安装/导入更新路径。它们仍按 P0 上线阻断处理，直到入口级故障注入和跨重启验收完成。

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

提交器的返回值应包含 `state`、`request_id`、`backup_version`、`config_revision`、`failure_reason`、`rollback_complete` 和 `cleanup_status`。调用方必须以 `state + cleanup_status` 做唯一分支：只有 `state=committed` 且 `cleanup_status=clear` 可以发 `skill:repaired`/`skill:optimized` 或调用 `UploadTrigger`；`rolled_back`、`audit_pending` 或 `cleanup_status=pending|needs_review` 只能发失败/补偿事件。

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
| reviewed-draft apply | 人工确认 | 共享提交器；draft 快照、审计预检、提交后清理 | 仅 `committed + cleanup_status=clear` 可完成；否则保留 draft 并阻断 |
| reviewed-draft disable/reject | 人工确认 | 共享提交器；config/index/audit 回滚，draft 提交后清理 | 仅 `committed + cleanup_status=clear` 完成；否则保留 draft 并阻断 |
| Hub/Enterprise/ZIP/GitHub 导入 | 人工确认 | 来源与完整性、扫描、目录 `.prev`、最终审计、发布前补偿意图；新目录分支还需相同摘要 no-op 断言 | 不发布 staging 目录 |
| 能力缺口 / IM 自动安装 | **禁止自动** | 新目录与 GitHub 配置注册分支已具备共享提交器和最终审计，但已有版本更新及非桌面持久快照仍未完成 | 只读提示，转人工安装 |
| 传统 GUI ZIP/插件安装 | **禁止自动** | `App.AddSkill`/`App.DeleteSkill` 具备 legacy durable compensation、原子 metadata、checked index、最终审计前后分层和失败保留原目录；`App.InstallSkill` 已在解压/settings 前持久化唯一外层 `legacy_gui_skill_install` 记录，并在最终审计后先标记 committed 再清理；其内部复用该记录完成 metadata/package 编排，不创建嵌套 durable 记录或中间安装审计，但仍未统一结构化返回和共享提交器语义 | 只读或人工确认；失败保持原目录并阻断后续自动操作；迁移前拒绝已有目录覆盖 |
| AgentService GitHub/Hub/Market/ZIP 导入 | **禁止自动** | `persistImportedEntries` 已经使用共享目录事务：staging、`.prev`、批次回滚、checked 目录扫描、严格审计和提交后清理；同内容导入 no-op；覆盖更新时，旧 Skill contract 在目录发布事务内撤销，任何中途失败由回滚恢复目录并通过 durable external snapshot 恢复 contract；`NewService` 启动阶段按 action prefix + `RecoveryScope` 尝试恢复目录与 contract；执行/上传复用同一 scope-aware pending 检查 | 恢复失败、队列不可读、scope 缺失/路径越界或仍有 pending 记录时拒绝写盘、执行和上传；多包/多租户/清理故障注入尚未完成。迁移完成不等于放开自动导入，仍仅人工灰度 |
| 上传/发布/重试 | 仅 `committed` 且 `cleanup_status=clear` | 补偿队列健康且目标 Skill 无 pending/needs_review/cleanup_pending | 保持 `blocked` |

矩阵中的“当前允许”是运行处置，不是目标架构的放宽。任何入口只要出现队列不可读、审计不可写、索引刷新错误、目录备份冲突、`cleanup_status!=clear` 或取消/超时，就必须 fail-closed。

#### 动态 contract 与目录事务的边界

动态 Skill contract 属于可路由授权，不是目录扫描的派生缓存。删除或覆盖更新时，contract 变化必须与目录事务建立可恢复关联：

1. 删除：先将目录移入隔离区；在最终审计前撤销 contract，撤销成功后才写 `skill.deleted`；若撤销、审计、索引或后续步骤失败，回滚目录并恢复原 contract（恢复失败则保留补偿记录并阻断执行）。
2. 覆盖更新：在目录发布事务内撤销旧 contract；任何中途失败先恢复旧目录，再恢复旧 contract；不得在事务外提前永久撤销。
3. 提交后清理：contract 已撤销且 `skill.deleted` 已审计后，清理失败只能产生 `committed + cleanup_status=pending`，不得重新发布旧 contract 或反向覆盖已审计结果。

若 contract 注册表不可用、恢复失败或无法证明与当前 Skill 的 stable ID 绑定，运行和上传均按 fail-closed 处理。日志中的“撤销成功”不构成授权变更证据，必须以注册表快照和结构化审计共同证明。

### 10.3 索引和 scan cache 的边界

索引是可由 YAML 与 config 重建的派生数据，但在一次提交期间仍必须保持与权威定义一致：刷新接口应返回错误，刷新失败就回滚新定义。核心和 GUI pipeline 已使用 checked 刷新边界；底层当前 BM25 实现不会产生错误，仍需真实可失败 provider 的集成验收。

`writeSkillScanCacheForInstalledEntry` 生成的是扫描报告缓存，不是 Skill 定义的权威来源。GUI repair 当前有一条写入发生在事务函数返回之后的路径，因此缓存失败不会改变已提交状态，只会留下可重建的陈旧缓存；代码已将其作为非权威派生缓存，并产生结构化 `skill:scan_cache_failed` 告警。仍需补充告警聚合、重建重试和“缓存不得决定 verified/active”的自动断言。可选边界为：

1. 将 scan cache 纳入提交器，失败时和 YAML/config 一起回滚；或
2. （当前采用）明确其为非权威派生缓存，提交后异步重建，缓存失败产生结构化告警，并禁止把缓存状态当作 `verified` 或 `active` 的依据。

### 10.4 提交后的外部副作用（依赖安装）

安装完成后执行 `npm install`/`pip install` 等依赖准备，属于独立的外部副作用，不能被“Skill 已提交”这一结果隐含覆盖。当前代码在目录提交后异步触发依赖安装，失败主要以日志记录；因此它不应改变 `transaction_state`，但必须单独具备任务 ID、超时、来源与完整性策略，并且不能阻塞或绕过 Skill 的执行准入。

上线前应将依赖准备改为受策略控制的后置任务：默认关闭自动执行；仅在存在锁文件/固定版本和哈希校验、网络与路径白名单、隔离工作目录、资源限额及审计事件时运行。依赖任务失败或被取消时，Skill 保持 `committed + cleanup_status=clear` 但标记 `dependency_status=failed`，若运行时需要该依赖则保持不可执行并提示人工处置；不得通过重装依赖覆盖已审计目录，也不得把依赖任务的日志成功当作 Gate 证据。

## 11. 审计与可观测性

事件至少包含时间、Skill、动作、决策、原因、风险、Gate、证据摘要、备份版本、来源、操作者、触发器和 `schema_version`；参数正文只保存脱敏摘要。建议统一增加 `request_id`、`config_revision`、`evidence_mode`、`failure_reason`、`transaction_state` 和 `cleanup_status`，避免仅凭自由文本区分失败。关键事件包括 `discovered`、`staged`、`scanned`、`verified`、`rejected`、`repaired`、`optimized`、`rollback`、`queue_full`、`cancelled`、`timed_out`、`audit_failed`、`cleanup_pending`、`compensation_recovered` 和 `compensation_needs_review`。

`EvolutionAuditHealthSnapshot` 暴露可用性、失败次数、最后错误和最后成功时间。审计不可用、队列增长或连续失败时 GUI 告警；失败摘要限制数量、长度和保留期，并提供成功率、失败率、回滚率、拒绝率和错误类别趋势。

当前 pipeline、staged 验证、YAML restore、maintenance、状态变更和 reviewed-draft apply/reject 路径已持久化 `schema_version=2`、`request_id`、`attempt`、`config_revision`、`evidence_mode`、`failure_reason`（取消路径另含 `termination`）；新目录安装分支已覆盖主要桌面事件字段，但 legacy 更新/导入路径仍需统一字段校验；审计健康快照和待恢复数量也已暴露给 GUI。`audit_pending` 已有独立 JSONL 快照、原子重写、启动恢复和 3 次失败后的 `needs_review` 降级；核心 pipeline、GUI repair、YAML restore、reviewed-draft、maintenance 及已迁移安装分支的不完整回滚均可写入同一队列。补偿记录当前使用独立的 `schema_version=1`（审计事件使用 `schema_version=2`，两者不可混用）；读取器兼容缺少 `schema_version` 的旧记录，但对显式未知版本、非法状态、负尝试次数、缺失 Skill 或 JSONL 损坏直接报错；状态、执行和上传入口随后 fail-closed，不能把不可识别记录当作已恢复。动态 contract 的 pre-image 以 `external_snapshots` 保存在同一记录中，AgentService 启动恢复会先幂等恢复授权状态，再恢复目录；缺少外部恢复器或快照格式非法时记录保留 pending。staged 激活在事务开始前写入补偿快照，成功完成最终审计后才清理；若仅清理失败，保留记录并阻断后续操作，不能把它误判为回滚失败。仍需将 maintenance 边缘分支、已有版本更新和导入路径逐步迁移到统一提交器，并补充人工处置 UI。

补偿恢复会追加 `skill:compensation_recovered` 或 `skill:compensation_needs_review` 事件；这些事件采用 best-effort 记录，不会替代队列快照，也不会把恢复失败伪装成成功。队列文件不可读时，状态接口显示 `compensation_queue_healthy=false` 和错误摘要，执行/上传入口继续 fail-closed。`RetryBlocked` 也属于上传入口：它只能在队列可读且目标 Skill 没有待恢复补偿时把条目移回 `pending`；补偿仍存在时条目保持 `blocked`，不得通过手动重试绕过门禁。

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

已补充真实文件重启/重新扫描和 overlay 防提升回归测试；仍建议在发布验收中覆盖多版本 YAML、索引损坏和升级迁移场景。补偿队列当前版本使用 `schema_version=1`：未知字段保持兼容，缺失版本号的历史记录按当前版本读取；显式不支持的版本、非法状态或 JSONL 损坏会使队列健康检查失败并触发 fail-closed。当前尚未提供跨版本迁移工具，遇到版本不支持或损坏时必须转人工处置。`cleanup_status=pending` 的记录必须跨重启保留并幂等重试，不得因进程重启被误判为已清理。

## 13. 非 Bash mock/replay 边界

`craft_tool`、MCP、浏览器、poll/loop 等非 Bash 动作只有在存在显式隔离适配器时才允许 replay。适配器输入必须包含脱敏参数、预期副作用及允许的文件/网络范围，输出必须包含 `passed|failed|unverified` 和 `evidence_mode=real|mock|none`。默认无适配器时为 `unverified`；mock 不能写生产文件、改变 Skill 状态或触发上传/发布。

适配器接口及其边界测试已落地，真实隔离 adapter 属于 P2 工作。在生产 adapter 落地前，`mock` 只能用于观察和草案生成，不能触发 config/YAML 写盘、状态激活、上传或发布。

## 14. 测试与验收

已覆盖：风险安装确认、无效定义不注册、Gate 三态、真实参数 staged 验证、激活旁路拦截、staged 隔离、队列合并/串并行、审计健康/失败摘要/等待时间、配置开关、基础写盘回滚、核心 pipeline 的 context cancel/timeout/shutdown 事件、repair 失败计数上限、request-level 状态、非 Bash mock/real 边界，以及 staged 重启/扫描和 overlay 防提升。提交器定向测试还覆盖了 GUI 创建/更新、reviewed-draft apply 和 staged 激活的提交成功、索引失败回滚、最终审计失败与提交后清理状态；Hub 安装新增同版本 no-op 回归（目录、`.prev` 和注册表保持不变），其它安装入口仍只有代表性目录/索引边界测试，不能替代全量安装矩阵。

上线前必须补齐：maintenance 剩余分支、已有版本更新/GitHub 导入等 legacy adapter，以及传统 GUI `App.InstallSkill` 接入统一提交器（`App.AddSkill`/`App.DeleteSkill` 已具备 checked index 与外层补偿，但仍需统一结构化返回和更完整的入口级故障注入）；为已迁移的 GUI repair、YAML 版本恢复、reviewed-draft、重命名、删除、新目录安装和 AgentService 导入补充 cleanup 失败与跨重启故障注入；AgentService 已在 `NewService` 增加启动阶段恢复，但仍需验证多租户目录快照和异常恢复场景；补偿队列的跨版本迁移、损坏检测和人工处置 UI（当前已提供只读安全摘要和列表展示，不提供模型触发恢复）；补偿恢复/`needs_review` 事件写入审计；非-pipeline 审计字段统一校验；审计不可写时阻止/回滚；GUI 终态结果和回滚原因；多版本升级迁移及索引损坏恢复。

当前可通过 `manage_skill(action="evolution_compensations")` 或 GUI Wails `ListSkillEvolutionCompensations()` 查看脱敏队列摘要；该接口只读，不允许模型或前端直接强制恢复。GUI 的恢复由 pipeline 启动流程执行，以避免绕过备份、索引刷新和审计门禁。Hub 与 AgentService 等已迁移的目录入口在最终审计之后若清理失败，会以 `committed + cleanup_status=pending` 返回；AgentService 还会在 `NewService` 启动阶段按其 action prefix 执行恢复，失败或 pending 时保持导入 fail-closed。传统 GUI ZIP 导入仍保留入口级解压/注册编排：`App.AddSkill`/`App.DeleteSkill`/`App.InstallSkill` 的 legacy 记录可跨重启恢复，并在恢复时重建 checked routing index；其中 InstallSkill 只保证外层记录的恢复顺序，内部 legacy 审计事件不构成独立提交。三者尚未统一为共享 `SkillCommitter` 的结构化返回，因此成功返回都只代表入口操作完成，不代表满足统一 `committed + cleanup_status=clear` 语义。

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
| AgentService 多包中途发布失败 | 已发布的前序包一并回滚；原 `.prev` 恢复；没有任何 `committed` 成功审计 |
| AgentService 导入最终审计/清理失败 | 审计失败恢复目录及旧 contract；清理失败保持 `committed + cleanup_status=pending` 且拒绝后续自动写盘；已增加注入审计失败后目录回滚回归；contract 恢复失败必须保持 external snapshot pending |
| AgentService 导入跨重启 | `NewService` 已调用 AgentService 专用恢复回调；prepared/committed/cleanup 记录必须在恢复成功后清理，恢复失败、队列不可读、scope 缺失或目录越界时保持 fail-closed；运行/上传门禁使用同一 `RecoveryScope`；删除隔离目录也使用 `agentservice_install_delete` action 并可在重启后恢复；动态 contract pre-image 已纳入补偿恢复，新增 contract 快照跨重启回归；仍需补多租户与异常场景断言 |
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

GUI 全量测试还受 Windows 浮动窗口、资源架构文件和工作区并行测试负载影响；若全量命令失败，应区分环境失败与本变更回归，不得直接把环境失败写成产品结论。

### 14.2 本次评审的验证证据

评审基线环境中已有的验证证据包括：

- GUI 定向 SkillRunner、repair-draft、状态与维护测试（`-vet=off`）；
- `go run scripts/check_wails_bindings.go`（17 个动态前端引用、1224 个生成绑定均有对应 App 方法）；
- 本文件的 fenced-code 配对检查和 `git diff --check`。

本轮新增/复核的提交器证据：

- `go test ./corelib/skill -run 'TestSkillCommitter_|Test.*Compensation' -count=1 -timeout 240s`：覆盖提交成功、正向/回滚索引回调分离、最终审计失败恢复、已提交但清理待办的重启语义；
- `go test ./corelib/skill -run '^TestCompensationRecoveryUsesDurableFinalAuditMarker$' -count=1 -vet=off -timeout 240s`：验证严格最终审计已落盘但 `transaction_state` 尚未持久化时，恢复逻辑依据 `request_id + skill + action + final_audit_kind` 识别已提交状态，只执行清理而不回滚业务文件。
- `go test ./corelib/tool -count=1 -timeout 180s`：覆盖 checked index provider 的错误传播；
- `go test ./corelib/skill ./corelib/agentservice -run 'Test(PersistImportedEntries|InstallSkill|SkillCommitter_|EvolutionCompensation)' -count=1 -vet=off -timeout 300s`：覆盖 AgentService 单包导入、同内容 no-op、陈旧 `.prev` 拒绝，以及共享提交器/补偿的定向组合边界；
- `go test ./corelib/skill ./corelib/agentservice -run 'Test(RecoverPendingCompensationsScopedByServiceRoot|RecoverPendingCompensationsScopeRejectsOutOfRootPaths|NewServiceRecoversAgentSkillDirectoryCompensation|AgentServiceRuntimeAndUploadBlockOnScopedCompensation|AgentServiceDeleteSkillUsesQuarantineTransaction)' -count=1 -vet=off -timeout 420s`：覆盖 AgentService 启动恢复的服务 scope 隔离、scope 与目录路径不一致时的拒绝、运行/上传门禁和删除隔离事务；
- `go test ./corelib/agentservice -run 'TestNewServiceRecoversAgentSkillDeleteQuarantine' -count=1 -vet=off -timeout 180s`：覆盖删除隔离目录在进程崩溃后由 `NewService` 启动恢复；
- `go test ./corelib/skill -run 'TestRecoverPendingCompensationsWithExternalSnapshotFailsClosedWithoutRestorer' -count=1 -vet=off -timeout 240s`：验证带 external snapshot 但无恢复器时不触碰目录、记录保持 pending，防止缺少外部恢复能力却误删/误恢复；
- `go test ./corelib/agentservice -run 'TestAgentService(DeleteSkillAuditFailureRestoresDirectory|ImportAuditFailureRollsBackPublishedDirectory)' -count=1 -vet=off -timeout 240s`：覆盖删除与导入最终审计失败时恢复原目录，禁止留下半提交目录；
- `go test ./corelib/agentservice -run 'TestAgentServiceDeleteSkillContractRevokeFailureLeavesContractAndDirectory' -count=1 -vet=off -timeout 240s`：故障注入 contract 撤销失败，确认删除事务不写“已删除”审计、原目录保持可见且 contract 仍可解析；
- `go test ./corelib/agentservice -run 'TestAgentServiceImportContractRevokeFailureRollsBackDirectoryAndContract' -count=1 -vet=off -timeout 240s`：覆盖覆盖导入在 contract 撤销失败时恢复旧目录和旧 contract，防止提前撤销造成不可逆漂移；
- `go test ./corelib/agentservice -run 'TestAgentServiceDeleteSkillAuditFailureContractRestoreFailureStaysPending' -count=1 -vet=off -timeout 240s`：覆盖最终审计失败且 contract 恢复器再次失败时保留 durable pending，并阻断后续执行/上传；
- `go test ./corelib/agentservice -run 'TestNewServiceRecoversAgentSkillContractSnapshot' -count=1 -vet=off -timeout 240s`：覆盖动态 contract pre-image 随 AgentService 补偿记录跨重启恢复，并验证恢复后队列清空；
- `go test ./gui -run '^TestInstallManagedHubSkillIndexFailureRestoresPreviousVersion$' -vet=off -count=1 -timeout 300s`：覆盖已有目录更新在索引失败时恢复旧版本；
- `go test ./gui -run 'Test(InstallHubSkill|InstallManagedHubSkill|InstallMixedSkill|.*InstallOnly|.*ToolInstall|InstallHubCapability|CapabilityMarketplace)' -vet=off -count=1 -timeout 300s`：覆盖列出的安装/能力市场定向组合路径。
- `go test ./gui -run 'Test(AddSkill|InstallSkill|DeleteSkill|SkillCache|DeleteNLSkill|ProjectInstallSkill)' -count=1 -vet=off -timeout 300s`：复核传统 GUI metadata/ZIP/settings 边界、删除隔离、项目安装和缓存失效；该结果不代表 `App.InstallSkill` 已具备共享提交器语义。
- `go test ./gui -run 'TestInstallSkill(UsesOneOuterCompensationAndOneCommitAudit|IndexFailureRestoresProjectDirectoryAndMetadata)' -count=1 -vet=off -timeout 300s`：验证 `InstallSkill` 只使用一条外层 durable 记录、失败时项目目录与 metadata 一并恢复，且 checked index 失败不产生 committed 审计。
- `go test ./gui -run '^TestDeleteSkillIndexFailureRestoresPackageAndMetadata$' -count=1 -vet=off -timeout 300s`：验证传统 GUI 删除在 checked index 首次发布失败时恢复包目录与 metadata，随后重建索引并清理补偿队列，且不产生 `legacy_deleted` 审计。
- `go test ./gui -run '^TestInstallSkillFinalAuditFailureRollsBackWithoutCommittedResult$' -count=1 -vet=off -timeout 300s`：通过使 strict audit sink 不可写，验证 InstallSkill 最终审计失败时恢复项目目录和 metadata，且不会把错误标记为 `committed`。
- `go test ./gui -run '^TestInstallSkillDetailedReportsSettingsFailureAndRollsBack$' -count=1 -vet=off -timeout 420s`：通过 settings 写入故障注入，验证 `InstallSkillDetailed` 返回结构化 `rolled_back` 结果、保留原 settings、清理补偿队列，并携带 request/error 关联字段。
- `go test ./corelib/skill -run '^TestRecoverPendingCompensationsForActionPrefixAndSkillDoesNotClaimSibling$' -count=1 -vet=off -timeout 240s`：验证 GUI 按 action prefix + Skill 过滤恢复，不会误领取同队列中的其它 Skill 记录。

这些测试只证明列出的路径，不证明所有 GUI 入口已统一；reviewed-draft、重命名、删除和新目录安装仍需补 cleanup 失败与跨重启矩阵，maintenance、能力缺口 GitHub/更新、IM 更新及 managed install 更新仍需入口级故障注入与跨重启矩阵。AgentService 已有单包导入、同内容 no-op、陈旧 `.prev`、共享提交器、`NewService` 启动恢复和 scope 越界拒绝定向回归，并新增删除 contract 撤销失败、contract 恢复失败时“目录 + contract + 审计”一致性断言；但缺少多包中途失败、最终审计/清理失败的完整批次矩阵及多租户恢复测试。运行/上传门禁的 scope-aware 检查仍需补入口级断言。传统 `App.AddSkill`/`App.DeleteSkill` 已增加 checked-index 失败后的目录/metadata 回滚断言，`App.InstallSkill` 已增加单一外层补偿、索引失败和最终审计失败回滚断言，但三者仍必须继续补充 settings 写入失败、提交后清理失败和跨重启测试，不能由 Hub 或 AgentService 测试代替。

前端 npm build/typecheck 因当前环境未提供 npm 未执行。GUI 全量测试仍可能被工作区既有的 `h.forgetAgentGuidedSkill undefined` 编译错误阻断；该错误不应归因于本次自进化改动。

指标：未经确认的 high/critical 安装为 0；无证据却 `passed` 为 0；staged 未验证进入 active 为 0；关键写盘无审计为 0；冷却期重复修复为 0；取消后继续写盘为 0；修复后真实成功率高于修复前。

## 15. 优先级与运行处置

- **P0（上线阻断）**：为 maintenance 全部分支、已有版本更新/GitHub 导入等 legacy adapter，以及传统 GUI `App.InstallSkill` 完成共享提交器迁移；为 `App.AddSkill`/`App.DeleteSkill` 补统一结构化返回和更完整的入口级故障注入（checked-index 回滚已覆盖）；为 `App.InstallSkill` 补外层最终审计失败、settings 写入失败、重启恢复和多包中途失败测试，并将内部 legacy 审计收敛为外层单一最终审计语义；为 AgentService 补齐多包回滚、最终审计与清理故障注入、多租户恢复断言；让索引 provider 返回真实错误并为各入口补故障注入；补偿队列跨版本迁移和人工处置闭环；统一 config/YAML/索引/最终审计的失败补偿；阻断未验证激活；为动态 contract 落地外部快照 schema/摘要校验及恢复失败处置。当前核心 pipeline、Create/Update、GUI repair、YAML 版本恢复、reviewed-draft、staged 激活、重命名、`DeleteNLSkill`、多个新目录安装分支及 AgentService GitHub/Hub/Market/ZIP 导入已接入 `SkillCommitter`；maintenance 边缘分支、安装/导入更新、能力缺口 GitHub 导入和传统 GUI 安装仍有独立编排。队列健康状态、schema 损坏 fail-closed、上传门禁、恢复事件、安装初始目录补偿和 `.prev` 保护属于风险收敛，不代表 P0 已关闭。
- **P1（近期）**：非-pipeline 审计字段校验；GUI 终态任务查询；失败趋势指标；升级迁移和索引损坏验收；已迁移入口的 cleanup/跨重启故障注入。
- **P2（持续）**：真实非 Bash replay adapter、组织策略覆盖、版本化审计 Schema、误修复率和长期回滚分析。

当审计不可用、队列持续增长或 Skill 连续失败时，先关闭 `skill_evolution_enabled`，保留必要的只读观察和证据采集；待审计恢复、队列年龄下降并完成归因后再恢复自动改写。`unverified` 候选只能导出草案，不能通过修改状态字段强行激活。

### 15.0 下一轮优化顺序

按风险和收益排序，实施顺序固定为：

1. **进行中**：将 `SkillCommitter`（prepare/commit/rollback/cleanup）继续覆盖 maintenance 边缘分支、已有版本更新/GitHub 导入及剩余非主路径；GUI repair、YAML 版本恢复、reviewed-draft、重命名、`DeleteNLSkill`、apply、staged 激活和新目录安装已完成基础迁移。传统 `App.AddSkill`/`App.DeleteSkill` 已具备 legacy durable compensation 与 checked-index 回滚，但仍需统一结构化 API 结果和入口级故障注入；`App.InstallSkill` 已建立唯一外层 durable 记录并在 mutation 后补写路径，最终审计失败已能回滚且不再伪报 committed，下一步是收敛内部 legacy 审计为单一最终审计、接入共享提交器和完整外层故障注入；提交器必须返回 `state` 与 `cleanup_status`；
2. **已完成基础能力**：索引层增加可替换的失败注入 provider；仍需将“索引失败后 YAML/config/目录均恢复”扩展为每个 GUI 入口的发布阻断测试；
3. 为补偿队列增加版本迁移、损坏隔离和人工处置命令/UI；迁移失败时保留原文件并进入只读安全模式；
4. 将能力缺口 GitHub/更新、IM 更新和传统 GUI ZIP/插件安装改为调用统一安装提交器；为 AgentService 补齐批次失败、最终审计/清理故障注入和多租户恢复断言（`NewService` 启动恢复基础能力已存在）。为非桌面实例补齐 durable config/目录快照；完成剩余路径前仍关闭自动安装，仅保留提示和人工确认；
5. 落地 external snapshot v1 的 schema、digest、stable ID 与 tenant/user 绑定校验；增加恢复器失败后的人工处置和审计关联，禁止编辑快照正文绕过门禁；
6. 最后补齐终态任务查询、趋势指标和真实非 Bash replay adapter。

### 15.1 运维操作顺序（建议固定）

发生写盘、审计或补偿异常时，按以下顺序处理，避免人工操作扩大不一致范围：

1. **冻结自动变更**：将 `skill_evolution_enabled` 设为 `false`；不要使用 `force=true` 绕过失败次数、冷却或 `needs_review`。
2. **确认健康度**：读取 `GetSkillEvolutionStatus()`，确认 `audit_available=true` 且 `compensation_queue_healthy=true`；任一为 false 都按 fail-closed 处理。
3. **读取安全摘要**：通过 `ListSkillEvolutionCompensations()` 或 `manage_skill(action="evolution_compensations")` 获取 `request_id/skill/action/status/attempts/failure_reason`。这些接口不返回恢复快照，也不提供直接恢复能力。
4. **保留证据后恢复**：先保存相关审计事件和摘要，再重启 pipeline 触发有限次数的自动补偿；恢复成功必须看到 `skill:compensation_recovered`，三次失败必须看到 `skill:compensation_needs_review`。
5. **人工复核**：`needs_review` 只允许人工依据 YAML、config、索引摘要和审计记录进行处置。完成统一提交器和迁移工具前，不应直接编辑 `audit_pending.jsonl` 或删除队列记录。
6. **小范围解冻**：仅在队列为空、审计可写、索引可重建且对应回归测试通过后，重新开启自动进化；优先灰度低风险 Skill。

### 15.2 当前实现中已修正的不合理点

| 原有倾向 | 优化后的规则 | 原因 |
|---|---|---|
| 把 scan cache 当作 Skill 定义的一部分 | cache 仅为可重建派生数据，失败产生告警，不改变 `verified/active` | 避免缓存故障导致错误激活或错误回滚 |
| 用日志表示“已回滚” | 回滚必须返回明确状态；不完整回滚写入 durable compensation | 日志不可作为恢复凭据 |
| 允许前端/模型直接重试补偿 | 只读摘要；恢复由 pipeline 启动流程执行 | 防止绕过备份、索引和审计门禁 |
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
| S2（近期） | GUI 终态结果页、取消确认、回滚原因筛选；补偿人工处置 UI；升级迁移和损坏恢复 | 冷启动、迁移和人工处置演练通过 |
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
| 2026-08-30 | AgentService `persistImportedEntries` 已迁移到共享目录提交器：增加 staging、批次提交、同内容 no-op、`.prev` 冲突拒绝、checked 目录扫描、最终审计和提交后清理；`NewService` 增加 `agentservice_install` 前缀的启动恢复与 fail-closed 门禁 | 防止 ZIP/GitHub/Hub/Market 多包导入产生部分安装或覆盖时丢失旧目录；明确启动恢复已具备基础实现，但多包/多租户/异常清理故障注入仍是上线阻断项 |
| 2026-08-30 | 继续收紧 legacy GUI：统一裸文件名 ZIP 的路径解析；`DeleteSkill` 增加安装锁、严格 metadata 解析、删除失败即停和原子 metadata 写入；AgentService 补充 `RecoveryScope` 与多租户路径归属校验，并同步修正故障注入缺口和发布口径 | 避免工作目录变化导致误读 ZIP、删除失败后 metadata 与目录不一致，以及共享队列在多服务场景误恢复其它服务目录 |
| 2026-08-30 | AgentService `DeleteSkill` 改为共享提交器隔离删除；contract 撤销纳入最终审计门，并在回滚时恢复原 contract；补偿记录支持 external snapshot，`NewService` 可跨重启恢复 contract；运行与上传入口增加 scope-aware 补偿门禁，并补充删除、越界恢复、contract 撤销失败、contract 快照恢复和运行/上传阻断回归 | 避免删除/导入在崩溃窗口丢失恢复路径，防止“已删除”审计与实际可路由 contract 不一致，以及未恢复或跨服务补偿记录继续执行/上传 |
| 2026-08-30 | 传统 GUI `App.AddSkill` 在最终审计成功后先持久化 `transaction_state=committed`/`cleanup_status=pending`，再清理包备份和补偿记录；`App.InstallSkill` 改为唯一外层 durable 记录并由 `addSkillLockedWithRecord` 复用，不再创建嵌套 durable 记录；文档同步区分 Add/Delete 的 legacy 补偿能力与 InstallSkill 的半统一事务 | 消除审计成功后的崩溃窗口误回滚，避免把内部局部审计误读为独立提交，同时明确仍缺统一 checked-index/结构化 API |
| 2026-08-30 | 传统 GUI `App.DeleteSkill` 在 metadata 更新后增加 checked routing index 发布；索引失败时先恢复目录与 metadata，再重建索引，确认恢复成功后才清理 durable compensation；新增删除索引故障回滚测试，并隔离 action prefix + Skill 的恢复范围 | 避免删除后索引仍暴露已删除 Skill，或在索引二次失败时提前清理补偿导致跨重启无法恢复 |
| 2026-08-30 | 修正 `App.InstallSkill` 最终审计失败的结果语义：只有审计已成功才返回 `legacySkillCommitError(committed=true)`；审计失败按未提交处理并由外层 durable compensation 回滚；新增 strict audit sink 故障测试 | 避免最终审计失败却被调用方误判为已提交，导致错误放行或错误处置 |
| 2026-08-30 | 补偿记录增加 `final_audit_kind`，启动恢复可通过严格审计行的 `request_id + skill + action + kind` 识别“审计已写入但 committed 标记未落盘”的崩溃窗口，仅执行提交后清理；新增 durable final-audit marker 回归测试 | 避免状态标记落盘失败后错误回滚已经审计的业务变更，同时拒绝以普通日志或未知决策推断提交 |
| 2026-08-30 | 将动态 contract 外部快照明确为版本化恢复契约：补充 schema、stable ID、tenant/user、digest、幂等结果和两阶段可观测语义；同步 P0 清单、入口矩阵、故障注入证据与后续实施顺序 | 避免把 opaque snapshot 或“撤销成功”日志误当作跨系统原子提交，明确恢复失败、格式未知和无法证明归属时的 fail-closed 处置 |

