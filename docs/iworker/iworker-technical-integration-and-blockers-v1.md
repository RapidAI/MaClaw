# iWorker / iWorkerCenter / iWorkerCloud 技术打通与堵点清单 v1

## 1. 产品边界

`iWorker` 是数字员工在本地电脑上的身体与交互容器，负责 IM / 语音 / 桌面工具调用、本地缓存、前端体验与必要的离线加速。它不保存企业能力的权威沉淀；记忆、能力、任务状态与组织关系都以注册的 `iWorkerCenter` 为准。本地只能做缓存和加速，不能成为事实上的企业记忆孤岛。

`iWorkerCenter` 是客户企业自己的 AI Native 组织运行时，负责多租户、公司 / 部门 / 个人三级记忆、虚拟组织、iWorker 编排、A2A 协商、Goal Watch 推动、Workflow、Capability 与审计。没有老板 / 董事会重大决策时，Center 应该可以自行运行；老板是决策者，不是公司控制中心。

`iWorkerCloud` 是我们作为 iWorker 系统厂商的云端管理平台，只做授权、算力分发、Skill Market、Center 多租户授权与管理。Cloud 不参与客户公司的经营决策、任务推进、组织调度和企业记忆沉淀。

## 2. 已打通链路

1. `iWorkerCenter` 租户注册到 `iWorkerCloud` 后，Cloud 返回 `center_id/secret` 并保存到 Center 租户记录。
2. Center 管理端搜索 Cloud Skill Market 时，按当前租户解析 Cloud 凭证。
3. Center 使用 `X-Center-Secret` 请求 Cloud 的 `/api/centers/{id}/skills/search`，避免 query secret 泄露。
4. Cloud 校验 Center 身份和 license module，只有包含 `skill_market/skills/skill/all` 的授权才能访问技能市场。
5. Center 导入技能后，在本租户内创建 `capability_packages`，初始状态为 `pending_review`。
6. Center 可下载并缓存 Cloud skill package artifact，校验 base64、size、SHA256；缺包不阻断元数据导入，但会标记为 `package_unavailable`。
7. Center 可将已缓存的 `skill.md` package 安装为运行时 `NLSkillEntry`，并通过 client runtime-entry 接口提供给 iWorker 拉取。
8. iWorker 可按 `colleague_id` 批量拉取已绑定、已安装、已审批的 runtime skills，也可在 heartbeat 响应中获得自己的 runtime skills。
9. Workflow 分派步骤时已接入能力优先路由：固定人员优先，其次选择同角色中已绑定并安装匹配 runtime skill 的 iWorker，最后回退到角色路由。
10. Workflow 完成步骤时可上报 `capability_id/status/error/latency_ms`，Center 会记录 capability 使用反馈。
11. Capability 路由已开始参考历史使用反馈：近期成功会轻微加权，近期失败过多会降权，避免持续把任务推给表现不稳定的能力。

## 3. 执行反馈闭环

本轮新增 `capability_usage_events`，用于记录每次 workflow/iWorker 执行中 capability 的真实表现：租户、能力、执行者、workflow instance、step instance、成功/失败、结果摘要、错误信息、耗时和创建时间。

这条链路的意义是：技能不是静态“已安装”就结束，而是进入组织经验系统。Center 可以逐步知道某个 iWorker 使用某个 capability 在什么类型任务上稳定、在哪里失败、是否需要升级、替换、降权或沉淀为公司/部门/个人记忆。

当前实现是轻量闭环：workflow 完成接口负责接收执行反馈；capability 模块负责持久化；路由器开始使用历史反馈做简单加权。后续可以把它升级为语义级评估、质量评分、自动复盘和 memory/experience pipeline 的深度融合。

## 4. 仍需继续解决的堵点

### 4.1 Skill package 生命周期还不完整

Center 已支持下载、缓存、校验、安装 `skill.md`，但还没有像 HubCenter / Maclaw GUI 那样完整处理签名、解包目录、依赖安装、风险确认、版本升级和灰度发布。建议继续把公共包协议、签名、风险元数据和安装策略下沉到 `corelib`。

### 4.2 Skill Market 仍是轻量目录

Cloud 现在有管理端 CRUD、授权搜索、市场字段和包下载，但还没有完整的提交、审核、评分、下载次数、结算、包签名、版本升级和灰度发布。Cloud 仍必须保持厂商管理边界，不读取客户经营数据。

### 4.3 记忆权威仍需统一协议

iWorker 本地只能缓存记忆和技能索引，权威数据必须在注册的 iWorkerCenter。还需要补齐缓存版本、失效、离线写入回放、冲突处理与审计，避免本地电脑变成事实上的企业记忆孤岛。

### 4.4 Goal Watch 多实例仍需租约与分片

Center 可以多机热备，Goal Watch 不能只有一个实例，也不能多实例重复 push。需要使用共享数据库租约、租约过期抢占、按 iWorker / goal 分片、卡死任务回收，保证规模随 iWorker 数量自动扩展。

### 4.5 A2A 协商需要和任务 / 记忆 / 决策记录合流

iWorker 之间可以通过 Center 做 agent2agent 协商，但协商结果需要落到方案、任务、审批、记忆和审计中，而不是停留在聊天记录里。适合继续下沉部分消息协议到 `corelib`。

## 5. 下一步开发建议

1. 将 capability 使用结果进一步写入 experience / memory pipeline，形成“能力调用 -> 结果评价 -> 记忆沉淀 -> 下次更好”的闭环。
2. 将 workflow 的轻量技能匹配升级为语义匹配 + 历史成功率 + 当前负载 + 近期失败降权。
3. 为 Skill package 增加签名、依赖、风险确认、版本升级与灰度机制。
4. 为 Goal Watch 增加 DB 租约与分片 worker，支持多机热备和自动扩缩。
5. 在 iWorker 端补齐执行反馈上报协议，确保本地执行完工具/技能后把结果回传给注册的 Center。
