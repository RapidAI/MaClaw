# 需求文档：SkillHub 自主发现与安装

## 简介

Maclaw 已支持本地 NL Skill 管理（手动创建/编辑/删除/执行）和 SkillHub 地址配置（`AppConfig.SkillHubURLs`），但尚未实现从 SkillHub 自动搜索、下载和安装 Skill 的能力。本需求定义 Maclaw 如何通过已配置的 SkillHub（官方 OpenClaw SkillHub、国内镜像等）自主发现新工具，并在运行时动态扩展自身能力。

核心理念：Maclaw 不需要预先知道所有可能的 Skill，而是在运行时根据用户需求动态扩展能力。SkillHub 是"能力商店"，Maclaw 是会自己逛商店的 Agent。

## 术语表

- **SkillHub**: 远程 Skill 注册中心，提供 Skill 目录搜索、元数据查询和包下载的 HTTP API 服务
- **SkillHub_Client**: 客户端侧的 SkillHub 访问组件，负责并发查询多个 Hub、缓存结果、下载安装 Skill
- **Hub_Skill_Meta**: SkillHub 返回的 Skill 元数据，包含名称、描述、标签、版本、作者、信任等级等
- **Skill_Manifest**: Skill 包的清单文件，描述 Skill 的依赖、权限需求和兼容性信息
- **Capability_Gap**: 当 Maclaw Agent 判断现有工具和 Skill 无法满足用户请求时产生的能力缺口
- **Trust_Level**: Skill 的信任等级（official/community/unknown），影响安装时的安全审查严格程度
- **Maclaw_Agent**: 运行在桌面客户端上的 AI Agent（对应 `IMMessageHandler`）
- **Skill_Executor**: Skill 执行器（对应 `SkillExecutor`）
- **Risk_Assessor**: 风险评估器（对应 `RiskAssessor`）
- **Tool_Router**: 工具路由器（对应 `ToolRouter`）

## 需求

### 需求 1：SkillHub Catalog API（Hub 侧）

**用户故事：** 作为 SkillHub 运营者，我希望 Hub 提供标准化的 Skill 目录 API，以便 Maclaw 客户端能搜索和下载 Skill。

#### 验收标准

1. THE SkillHub SHALL 提供 `GET /api/v1/skills/search` 端点，接受 `q`（关键词）、`tags`（标签过滤）、`page`（分页）查询参数，返回匹配的 Hub_Skill_Meta 列表
2. THE SkillHub SHALL 提供 `GET /api/v1/skills/{id}` 端点，返回单个 Skill 的完整元数据，包含 steps 定义和 Skill_Manifest
3. THE SkillHub SHALL 提供 `GET /api/v1/skills/{id}/download` 端点，返回 Skill 的 JSON 包内容
4. THE SkillHub SHALL 在搜索结果中包含每个 Skill 的 trust_level 字段（official/community/unknown），官方审核通过的 Skill 标记为 "official"
5. THE SkillHub SHALL 支持语义搜索，搜索范围覆盖 Skill 的 name、description 和 tags 字段
6. WHEN 搜索请求包含中文关键词时，THE SkillHub SHALL 正确处理中文分词并返回相关结果

### 需求 2：SkillHub 客户端

**用户故事：** 作为开发者，我希望 Maclaw 能同时查询多个已配置的 SkillHub，以便从最快的源获取 Skill。

#### 验收标准

1. THE SkillHub_Client SHALL 读取 `AppConfig.SkillHubURLs` 中配置的所有 Hub 地址，并发向所有 Hub 发起搜索请求
2. WHEN 多个 Hub 返回相同 ID 的 Skill 时，THE SkillHub_Client SHALL 按 Hub 延迟从低到高去重，保留延迟最低的 Hub 来源
3. THE SkillHub_Client SHALL 缓存搜索结果，缓存 TTL 为 5 分钟，相同查询在 TTL 内直接返回缓存结果
4. WHEN 某个 Hub 的请求在 8 秒内未返回时，THE SkillHub_Client SHALL 超时放弃该 Hub 的结果，不影响其他 Hub 的返回
5. THE SkillHub_Client SHALL 提供 `Search(ctx, query) ([]HubSkillMeta, error)` 和 `Install(ctx, skillID, hubURL) (*NLSkillEntry, error)` 方法
6. WHEN 所有配置的 Hub 均不可达时，THE SkillHub_Client SHALL 返回空结果而非错误，不阻塞 Agent 的正常流程

### 需求 3：LLM 驱动的能力缺口检测

**用户故事：** 作为开发者，我希望 Maclaw 在发现自己无法处理某个请求时，能自动去 SkillHub 搜索合适的 Skill，而不是简单地说"我做不到"。

#### 验收标准

1. WHEN Maclaw_Agent 的 LLM 推理结果表明现有工具和 Skill 均无法满足用户请求时，THE Maclaw_Agent SHALL 触发 Capability_Gap 检测流程
2. THE Maclaw_Agent SHALL 调用 LLM 从用户请求中提炼出能力需求描述（而非直接使用用户原文），作为 SkillHub 搜索查询
3. WHEN SkillHub_Client 返回候选 Skill 列表时，THE Maclaw_Agent SHALL 调用 LLM 对候选列表进行语义排序，选出与用户意图最匹配的 Skill
4. THE Maclaw_Agent SHALL 在搜索 SkillHub 前向用户发送一条状态消息（如"正在搜索可用的 Skill..."），搜索完成后告知结果
5. IF SkillHub 搜索无结果或所有候选 Skill 均不匹配，THEN THE Maclaw_Agent SHALL 向用户说明当前无法满足该请求，并建议手动创建 Skill 或等待 Hub 更新

### 需求 4：自动安装与即时使用

**用户故事：** 作为开发者，我希望 Maclaw 找到合适的 Skill 后能自动安装并立即用它处理我的请求，无需我手动操作。

#### 验收标准

1. WHEN Maclaw_Agent 选定一个候选 Skill 后，THE Maclaw_Agent SHALL 通过 SkillHub_Client 下载该 Skill 并注册到本地 Skill_Executor
2. THE Maclaw_Agent SHALL 将从 Hub 安装的 Skill 的 source 字段设为 "hub"，source_project 字段设为 Hub URL
3. WHEN Skill 安装成功后，THE Maclaw_Agent SHALL 立即使用该 Skill 处理当前用户请求，无需用户再次发送消息
4. THE Maclaw_Agent SHALL 在安装完成后向用户发送通知，包含已安装 Skill 的名称、描述和来源 Hub
5. IF Skill 安装失败（网络错误、格式无效等），THEN THE Maclaw_Agent SHALL 向用户报告失败原因并回退到正常处理流程

### 需求 5：安装前安全审查

**用户故事：** 作为开发者，我希望从 Hub 自动安装的 Skill 经过安全检查，以防止恶意 Skill 执行危险操作。

#### 验收标准

1. WHEN SkillHub_Client 下载一个 Skill 后，THE Maclaw_Agent SHALL 在安装前对该 Skill 的 steps 进行风险评估
2. THE Risk_Assessor SHALL 扫描 Skill 的所有 steps，检查是否包含高风险操作（如 `rm -rf`、`sudo`、访问敏感路径等）
3. WHEN Skill 的 trust_level 为 "official" 时，THE Maclaw_Agent SHALL 降低安全审查严格程度，允许 medium 风险操作自动通过
4. WHEN Skill 的 trust_level 为 "unknown" 时，THE Maclaw_Agent SHALL 对所有 medium 及以上风险操作请求用户确认
5. WHEN 安全审查判定 Skill 包含 critical 风险操作时，THE Maclaw_Agent SHALL 拒绝自动安装并向用户展示风险详情，用户可手动确认安装
6. THE Audit_Log SHALL 记录所有从 Hub 安装 Skill 的操作，包含 Skill 名称、来源 Hub、trust_level 和安全审查结果

### 需求 6：已安装 Hub Skill 的生命周期管理

**用户故事：** 作为开发者，我希望能管理从 Hub 安装的 Skill，包括查看来源、更新和卸载。

#### 验收标准

1. WHEN 用户列出 NL Skill 时，THE Skill_Executor SHALL 对 source 为 "hub" 的 Skill 额外展示来源 Hub URL 和 trust_level
2. THE Maclaw_Agent SHALL 支持通过自然语言指令管理 Hub Skill（如"更新所有 Hub Skill"、"卸载 xxx Skill"）
3. WHEN 用户请求更新 Hub Skill 时，THE SkillHub_Client SHALL 查询该 Skill 在 Hub 上的最新版本，如有更新则下载并替换本地版本
4. THE Skill_Executor SHALL 在 NLSkillEntry 中增加 `hub_skill_id` 和 `hub_version` 字段，用于追踪 Hub 来源和版本

### 需求 7：后台推荐与预热

**用户故事：** 作为开发者，我希望 Maclaw 能在空闲时主动从 Hub 获取推荐 Skill，以便在我需要时已经准备好。

#### 验收标准

1. THE SkillHub_Client SHALL 支持定时（每 24 小时）从配置的 Hub 拉取"热门 Skill"列表
2. THE Maclaw_Agent SHALL 将推荐 Skill 的元数据缓存到本地索引，但不自动安装
3. WHEN Tool_Router 执行工具筛选时，THE Tool_Router SHALL 将本地缓存的推荐 Skill 元数据纳入匹配范围，如匹配度高则建议用户安装
4. THE Maclaw_Agent SHALL 在推荐安装时向用户展示 Skill 的描述、下载量和 trust_level，由用户决定是否安装

### 需求 8：Hub 镜像与容错

**用户故事：** 作为中国区开发者，我希望 Maclaw 能优先使用国内镜像 Hub，以获得更快的下载速度。

#### 验收标准

1. THE SkillHub_Client SHALL 利用 `PingSkillHub()` 的延迟数据，在下载 Skill 时优先选择延迟最低的 Hub
2. WHEN 首选 Hub 下载失败时，THE SkillHub_Client SHALL 自动回退到下一个可用的 Hub 重试下载
3. THE SkillHub_Client SHALL 对不同 Hub 返回的相同 Skill（相同 ID）视为等价，客户端无需区分官方 Hub 和镜像 Hub
4. WHEN 用户新增一个 Hub 地址时，THE SkillHub_Client SHALL 在 30 秒内对该 Hub 执行一次连通性测试并缓存延迟数据

### 需求 9：前端 SkillHub 浏览界面

**用户故事：** 作为开发者，我希望在 Maclaw 的设置界面中能浏览和搜索 SkillHub 上的 Skill，以便手动发现和安装感兴趣的 Skill。

#### 验收标准

1. THE 前端 SHALL 在 SkillsManagementPanel 中新增"Hub 市场"标签页，展示 SkillHub 搜索界面
2. THE 前端 SHALL 提供搜索框，用户输入关键词后调用 SkillHub_Client 的搜索 API 并展示结果列表
3. THE 前端 SHALL 为每个搜索结果展示 Skill 名称、描述、标签、trust_level 徽章和"安装"按钮
4. WHEN 用户点击"安装"按钮时，THE 前端 SHALL 调用后端安装接口，安装完成后刷新本地 Skill 列表
5. THE 前端 SHALL 对已安装的 Hub Skill 展示"已安装"状态和"更新"按钮（如有新版本）
