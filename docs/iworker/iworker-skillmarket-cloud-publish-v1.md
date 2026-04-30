# iWorker Skill Market 与云端发布闭环 v1

## 1. 产品边界

iWorkerCenter 拥有企业自己的内部 Skill Market。它不是 iWorkerCloud 的一个缓存镜像，而是企业 AI Native 组织沉淀能力、复用能力、评价能力和治理能力的核心层。这个市场应该是一套自运营、自评价、自进化的能力生态，而不是由管理员人工维护每一个 skill 的传统目录。

iWorkerCloud 是我们公司作为 iWorker 系统厂商提供的云端管控平台，只负责授权、算力分发、顶级 Skill Market、云端多租户 iWorkerCenter 管理和商业结算。iWorkerCloud 不参与客户公司的经营决策、任务推进、组织调度和企业记忆沉淀。

## 2. Center 内部 Skill 的来源

企业内部 Skill Market 的 skill 分为三类：

- `cloud_imported`：iWorker 或管理员按业务需求从 iWorkerCloud 搜索、购买或获取免费 skill 后导入到本企业 iWorkerCenter。
- `iworker_summary`：iWorker 在长期业务执行、A2A 协商、流程复盘和经验沉淀中总结出来的企业自有 skill。
- `local`：管理员或企业内部人员手工创建、导入、维护的本地 skill。

无论来源如何，skill 在企业内的权威状态都保存在 iWorkerCenter。本地 iWorker 只能缓存和执行，不能成为能力资产的权威存储点。

## 3. 从 Cloud 导入到 Center

当 iWorker 发现自己缺少某项能力时，可以通过 iWorkerCenter 搜索 iWorkerCloud Skill Market。Center 使用已注册的 `center_id/secret` 与 Cloud 通讯，Cloud 只校验授权和市场访问权限，不读取企业经营数据。

导入后，Center 会把 cloud skill 保存为本企业的 `capability_packages`，并缓存 package artifact。导入 skill 默认进入 `pending_review`，由企业管理员决定是否启用、安装和绑定给具体 iWorker。

## 4. iWorker 自总结 Skill

iWorker 在执行任务时会持续产生能力使用反馈，包括成功率、失败原因、耗时、质量评分和人工复核意见。Center 可以基于这些反馈把重复出现、可抽象复用的经验沉淀为新的 skill。

这类 skill 的核心价值在于企业自有 know-how。人类员工可以作为高价值 judgement node、工具或 skill 参与验证，但组织真正沉淀下来的能力应保存在 Center 的记忆、流程、skill 和审计系统中。

## 5. 成熟度评价

成熟 skill 才允许上传到 iWorkerCloud。成熟度评价借鉴 HubCenter / SkillMarket 的评分机制，不只看是否存在 package，还要看真实执行表现。

当前 Center 侧规则包括：

- `min_usage_count`：最少使用次数。
- `min_success_rate`：最低成功率。
- `min_average_quality`：最低平均质量分。
- `require_package_cached`：是否要求已有可上传 package。
- `allow_cloud_imported`：是否允许把 cloud 导入 skill 二次上传。

后续可以继续引入下载量、评分、复购、版本稳定性、人工审核和退款率等市场信号。

## 6. 上传规则与收益归属

每个企业租户必须有管理员邮箱。企业把成熟 skill 上传到 iWorkerCloud 时，Center 会把管理员邮箱写入 `author_email`，Cloud 用它关联收入归属。

管理员的主要工作不是日常运营 skill，而是配置上传与安全规则：

- 是否启用自动或半自动上传。
- 什么成熟度阈值允许上传。
- 是否允许 cloud 导入 skill 再上传。
- 默认免费还是收费。
- 默认价格是多少。

Center 上传 skill 到 Cloud 后，会在本地记录 `cloud_publish_status`、`cloud_skill_id`、`cloud_published_at` 和 `cloud_publish_error`，方便管理员界面展示发布状态和失败原因。

## 7. Center 内部市场接口

iWorkerCenter 提供面向管理员 GUI 的内部市场接口，便于复用 HubCenter / Maclaw GUI 的市场页风格：

- `GET /admin/skillmarket`：列出企业内部 skill，支持 `q`、`origin`、`status`、`package_status`、`cloud_publish_status`、`mature` 筛选。
- `GET /admin/skillmarket/{id}`：查看单个 skill 的详情、使用统计、成熟度、安全状态和云端发布状态。
- `GET /admin/skillmarket/evolution-candidates`：查看自运营演化队列，返回继续学习、可自动发布、已发布监控、安全阻断等推荐。
- `GET /admin/skillmarket/evolution-status`：查看当前租户演化执行状态，包含是否存在活跃租约、租约归属、过期时间以及上一轮真实执行摘要。
- `GET/PUT /admin/skillmarket/evolution-automation-rule`：读取或更新当前租户的自动演化规则，包括是否启用、运行间隔和每轮最多处理数量。
- `GET /admin/skillmarket/evolution-monitor-status`：查看 Center 后台自演化 Monitor 的全局运行状态、配置和各租户最近一轮自动演化结果。
- `GET /admin/skillmarket/evolution-history`：查看 SkillMarket 自演化专用历史，只返回后台自动演化和手工真实演化运行记录，不混入普通模型调用审计；支持 `source=monitor|manual_run`、`status=ok|error` 和 `limit`，并返回结构化 `detail_fields` 供 GUI 与报表直接使用。
- `GET /admin/skillmarket/evolution-metrics`：基于自演化历史聚合运行指标，返回来源分布、状态分布、扫描/尝试/发布/跳过/失败总量和跳过原因分布，也支持 `source`、`status`、`limit` 筛选。
- `GET /admin/skillmarket/evolution-health`：基于最近自演化历史返回 `healthy`、`warning` 或 `critical`，并给出原因和建议动作，例如无近期历史、失败过多、全部被间隔跳过、自动演化关闭，或自动化开启但最近运行时间已经超过停滞阈值。返回值会包含 `automation_rule`、`expected_interval_seconds`、`last_run_age_seconds` 和 `stale_threshold_seconds`，便于 GUI、告警器和热备节点判断 SkillMarket 自进化是否已经卡住。
- `POST /admin/skillmarket/evolution-run`：手工触发一轮自运营演化任务，支持 `dry_run` 和 `limit`，自动发布成熟且安全的候选 skill；真实执行会获取租户级租约，避免多机热备或定时任务重复运行。
- `GET/PUT /admin/capabilities-cloud-publish-rule`：读取或更新上传规则。
- `POST /admin/capabilities/{id}/publish-cloud`：按规则或强制把成熟 skill 发布到 iWorkerCloud。
- `POST /admin/skillmarket/{id}/safety`：关键安全时刻由人类管理员执行 `quarantine`、`restore` 或 `delete`。

接口返回的 skill item 包含 `usage_summary`、`mature`、`maturity_reasons`、`safety_status` 和 `safety_reason`，GUI 可以直接展示“为什么还不能上传”“为什么已经可以商业化”以及“是否被人类安全裁决隔离”。演化候选接口额外返回 `recommendation`、`autonomous` 和 `human_intervention_required`，用于把正常 skill 的演化交给系统，把人类注意力留给少数安全裁决。演化执行器只处理 `publish_to_cloud_candidate`，并且单个 skill 发布失败不会阻断整轮任务；同一租户同一时间只允许一轮真实执行，dry-run 不加锁。`evolution-automation-rule` 决定某个租户是否交给系统自动演化、多久检查一次、每轮最多处理多少个候选；`evolution-status` 为 GUI 和定时器提供单租户“正在运行/上次运行结果”的观测点，`evolution-monitor-status` 则提供后台 Monitor 的全局视角。Monitor 每轮租户级执行、跳过和失败都会写入审计日志，避免管理员误判系统停滞，也方便多机热备节点判断是否需要等待或接力。

## 8. 推荐治理原则

企业默认不应把所有内部 skill 都上传到 Cloud。真正适合上传的，是可产品化、可脱敏、可复用、不会暴露客户经营数据或企业核心机密的成熟 skill。日常发现、沉淀、评价、成熟度判断和发布候选都应由系统自运行，人类只在规则设定和安全风险上介入。

Cloud 上传是商业化通道，不是企业运营通道。企业运营闭环仍然在 iWorkerCenter 内完成：目标、任务、A2A 协商、执行反馈、记忆沉淀、能力评价和审计都留在企业自己的 Center。`evolution-candidates` 是管理员面向生态的观察窗，不是人工排班表；`evolution-run` 是系统自运营执行器；后台 `SkillEvolutionMonitor` 会按租户规则定时触发真实演化，并把自动发布、跳过原因、失败原因写入审计；`evolution-history` 为管理员提供面向 SkillMarket 的专用追溯视图，并把 scanned、attempted、published、skip_reason、failed 等字段结构化返回；`evolution-metrics` 给出一段历史窗口内的汇总视图，`evolution-health` 则把这些指标转成健康等级、原因和建议动作，并能识别自动化开启但长时间没有新运行记录的停滞状态。管理员只需要调整自动演化规则、成熟度规则和安全裁决，默认应让系统持续学习、筛选和推进。
