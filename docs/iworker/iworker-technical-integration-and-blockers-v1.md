# iWorker / iWorkerCenter / iWorkerCloud 技术打通与堵点清单 v1

## 1. 产品边界

`iWorker` 是数字员工在本地电脑上的身体与交互容器，负责 IM / 语音 / 桌面工具调用、本地缓存、前端体验与必要的离线加速。它不保存企业能力的权威沉淀，记忆、能力、任务状态与组织关系都应以注册的 `iWorkerCenter` 为准。

`iWorkerCenter` 是客户企业自己的 AI Native 组织运行时，负责多租户、公司 / 部门 / 个人三级记忆、虚拟组织、iWorker 编排、A2A 协商、Goal Watch 推动、Workflow、Capability 与审计。没有老板 / 董事会重大决策时，Center 应可以自行运行；老板只是决策者，不是公司控制中心。

`iWorkerCloud` 是我们作为 iWorker 系统厂商的云端管理平台，只做授权、算力分发、Skill Market、Center 多租户授权与管理。Cloud 不参与客户公司的经营决策、任务推进、组织调度和企业记忆沉淀。

## 2. Skill Market 统一方向

Skill Market 不再重新设计一套独立模型，而是参考现有 `hubcenter` 与 `maclaw gui` 的实现：搜索返回 `results`，条目字段包含 `id/name/description/tags/score/price/status/avg_rating/download_count/version/author/created_at`。

已下沉到 `corelib/skillmarket` 的共享协议：

- `Skill`：HubCenter、Maclaw GUI、iWorkerCenter、iWorkerCloud 共用的市场条目 DTO。
- `SearchResponse`：统一返回 `{ "results": [...] }`。
- `CatalogResponse`：管理端列表返回 `{ "skills": [...] }`。
- `SkillInput`：Cloud 管理端创建 / 更新技能的输入结构。

当前代码已调整为：

- `iWorkerCloud` 的 Center 搜索接口返回 `corelib/skillmarket.SearchResponse`。
- `iWorkerCenter` 的 Cloud 导入器解析 `results`，并使用 `X-Center-Secret` 鉴权。
- `iWorkerCenter` 能在租户注册 Cloud 后按当前租户动态解析 `center_id/secret`，不再依赖启动时静态 importer。

## 3. 已打通链路

1. iWorkerCenter 租户注册到 iWorkerCloud 后，Cloud 返回 `center_id/secret` 并保存到 Center 租户记录。
2. Center 管理端搜索 Cloud Skill Market 时，按当前租户解析 Cloud 凭证。
3. Center 使用 `X-Center-Secret` 请求 Cloud 的 `/api/centers/{id}/skills/search`。
4. Cloud 校验 Center 身份与 license module，只有包含 `skill_market/skills/skill/all` 的授权才能访问技能市场。
5. Center 导入技能后，在本租户内创建 `capability_packages`，初始状态为 `pending_review`。
6. Center 审批通过后，能力进入 `active`，再绑定给 iWorker / colleague 使用。

## 4. 仍需继续打通的堵点

### 4.1 Skill 包内容还没有真正下载 / 安装

当前导入的是 skill 元数据，并未像 Maclaw GUI 那样下载加密 zip、验签、解包、安装到可执行 skill runtime。下一步应复用 `corelib/skill` 包格式与 HubCenter 下载/签名逻辑，避免 Cloud 自己再定义一套包协议。

### 4.2 Cloud Skill Market 仍是轻量目录，不是完整市场

Cloud 现在有管理端 CRUD、授权搜索、市场字段，但还没有提交、审核、评分、下载次数、结算、包签名、版本升级和灰度发布。建议优先把共享 DTO 与包协议下沉到 `corelib`，再决定 Cloud 是复用 HubCenter 服务能力，还是只作为企业级渠道分发层。

### 4.3 Center Capability 与 iWorker runtime 的闭环还要增强

Center 已能导入 capability，但 iWorker 执行任务时如何发现、选择、调用、反馈该能力，还需要进一步接到 agent runtime / workflow / memory。成功执行后的经验应沉淀回个人、部门或公司记忆。

### 4.4 记忆权威在 Center，本地缓存需要一致性协议

iWorker 本地只能缓存记忆和技能索引，权威数据必须在注册的 iWorkerCenter。需要补齐缓存版本、失效、离线写入回放、冲突处理与审计，避免本地电脑变成事实上的企业记忆孤岛。

### 4.5 多实例 Goal Watch 需要全局租约与分片

Center 可以多机热备，Goal Watch 不能只有一个实例，也不能多实例重复 push。需要用共享数据库租约、租约过期抢占、按 iWorker / goal 分片、卡死任务回收，保证规模随 iWorker 数量自动扩展。

### 4.6 A2A 协商协议需要和任务 / 记忆 / 决策记录合流

iWorker 之间可以通过 Center 做 agent2agent 协商，但协商结果需要落到方案、任务、审批、记忆和审计中，而不是停留在聊天记录里。适合继续下沉部分消息协议到 `corelib`。

### 4.7 Cloud 管理 Center，但不碰经营数据

Cloud 可以管理 Center 授权、算力、技能、版本、健康状态和多租户开通，但不应读取客户企业的任务、记忆、经营指标、A2A 内容和具体工作流。需要在 API、DB、文档和 UI 上持续强化这个边界。

## 5. 下一步开发建议

1. 将 Skill 包下载 / 安装协议对齐 HubCenter + Maclaw GUI，并把公共 DTO、签名、包元数据下沉到 `corelib`。
2. 在 iWorkerCenter 中增加 capability package 的“包安装状态”，区分 `metadata imported`、`package downloaded`、`verified`、`installed`、`active`。
3. 将 iWorker runtime 的技能发现接口从 Center 拉取，禁止依赖本地长期权威配置。
4. 为 Goal Watch 增加 DB 租约与分片 worker，支持多机热备和自动扩缩。
5. 把 Skill 使用结果写入 experience / memory pipeline，形成“能力调用 -> 结果评价 -> 记忆沉淀 -> 下次更好”的闭环。
