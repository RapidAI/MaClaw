# Hub / HubCenter 能力市场设计草案

## 1. 背景与目标

当前 Skill Market 可以升级为更通用的 **能力市场**（Capability Market）。Skill、MCP 不再是两套割裂的市场，而是能力市场中的不同能力类型。

能力市场的目标是：

- 让 HubCenter 提供公共能力市场，承载官方、社区、商业能力的发现、购买和分发。
- 让企业 Hub 提供企业内部能力市场，承载企业可控的能力引入、审核、沉淀、分发和审计。
- 让 Maclaw 在企业状态下优先使用企业 Hub 上的能力，必要时按企业策略搜索 HubCenter、ClawHub、GitHub 等外部来源。
- 让免费能力能够低摩擦试用和沉淀，让收费能力必须先经过企业购买审批和入库，再开放给企业用户使用。
- 让 MCP 不只是“下载资源”，也可以由用户上传、管理员编辑、模板生成，成为企业 MCP 配置中心。

核心抽象：

```text
Capability Market
  ├─ Skill
  ├─ MCP
  ├─ Plugin
  ├─ Agent
  ├─ Workflow
  ├─ Prompt / Template
  └─ Connector
```

首期重点支持 `skill` 和 `mcp`。

## 1.1 统一 URL

为了简化访问入口，HubCenter 和企业 Hub 上的能力市场统一使用：

```text
https://{host}/marketplace
```

也就是：

- HubCenter 能力市场：`https://{hubcenter-host}/marketplace`
- 企业 Hub 能力市场：`https://{hub-host}/marketplace`

API 可以继续放在 `/api/capability-market` 或 `/api/capabilities` 下，但面向用户和管理员的市场入口统一为 `/marketplace`。

## 2. 核心概念

### 2.1 Capability

`Capability` 是市场中的统一资源对象。

```yaml
capability:
  id: string
  type: skill | mcp | plugin | agent | workflow | prompt | connector
  name: string
  version: string
  publisher: string
  source: enterprise_hub | hubcenter | clawhub | github
  pricing: free | paid | freemium | subscription
  license: object
  permissions: object
  package: object
  config: object
  status: draft | pending_review | approved | published | disabled | revoked
```

Skill 偏向可安装、可执行的能力包。MCP 偏向工具服务配置、连接能力和运行声明。

### 2.2 Market Source

能力可以来自多个来源：

- `enterprise_hub`：企业 Hub 内部能力市场，企业用户默认优先使用。
- `hubcenter`：公共 HubCenter 能力市场。
- `clawhub`：现有 ClawHub 搜索源。
- `github`：现有 GitHub 搜索源。

搜索、安装、沉淀和审批都必须被企业策略约束，不能只由 Maclaw 本地设置决定。

### 2.3 Origin

企业 Hub 中的能力需要保留来源链路，便于追踪、更新、撤回和授权。

```yaml
origin:
  source: hubcenter
  capability_id: skill.code-review-pro
  version: 1.2.3
  checksum: sha256:...
  imported_at: 2026-05-14T10:20:00Z
  imported_by: user_or_hub
  acquisition_request_id: req_xxx
```

## 3. 企业状态下的 Maclaw 访问策略

Maclaw 进入企业状态后，通过 Hub 下发的策略决定能力市场行为。

默认访问顺序：

```text
Enterprise Hub Capability Market
  ↓ fallback / merged search if allowed
HubCenter Capability Market
  ↓ optional
ClawHub / GitHub
```

安装决策逻辑：

```text
if capability exists in Enterprise Hub and is published:
    install from Enterprise Hub

else if external capability is free and policy allows direct install:
    install from external source
    verify runtime success
    promote/import result to Enterprise Hub according to policy

else if external capability is paid:
    create acquisition request on Enterprise Hub
    wait for admin approval, purchase, import, publish

else:
    block or require admin review
```

关键原则：

- 企业 Hub 已发布能力优先。
- 免费能力可以配置为“先用后沉淀”。
- 收费能力必须“先审批购买、再入库使用”。
- 免费 MCP 允许直接安装，但需要配置校验、health check 和权限记录。
- 管理员“下发”和“推荐”是两个维度：下发是必须安装，推荐是用户自主选择安装。

### 3.1 企业下发与推荐

企业 Hub 能力市场可以对能力设置两类企业意图：

- `managed_deployments`：管理员下发，属于企业强制能力。Maclaw 进入企业状态后必须 best-effort 自动安装，并在失败时持续重试和审计。
- `recommended_capabilities`：管理员推荐，属于建议能力。Maclaw 只在市场首页、MCP 管理页或推荐区域提示用户安装，不自动安装，最终由用户选择。

```yaml
managed_deployments:
  - capability_id: company.security-baseline
    capability_type: skill
    version_policy: pinned
    version: 2.0.0-enterprise.1
    deployment_policy: required
    scope:
      departments: [engineering]

  - capability_id: company.jira-mcp
    capability_type: mcp
    version_policy: pinned
    version: 1.2.0-enterprise.1
    deployment_policy: required
    post_install:
      require_user_secrets: true
      required_secrets:
        - JIRA_TOKEN

recommended_capabilities:
  - capability_id: company.release-helper
    capability_type: skill
    version_policy: latest_approved
    recommendation_reason: 推荐用于发布检查
    scope:
      departments: [engineering]
```

下发规则：

- 只针对企业 Hub 中已经 `published` 且企业可用的能力。
- 收费能力必须已经由 Hub 完成购买、license 获取和企业发布，Maclaw 才能自动下发安装。
- 已安装同版本：不重复安装。
- 已安装旧版本：如果来源是企业 Hub，默认自动升级到企业 Hub 当前批准版本；如果来源是 HubCenter/SkillMarket，免费能力自动更新，收费能力遵守收费控制策略。
- 用户卸载下发能力：下次同步应重新安装。
- 安装失败：记录失败原因，支持后台重试，不阻塞用户进入主流程。
- MCP 缺少用户 secret：安装可以完成，但状态显示为“需要配置”，直到用户补齐认证信息。

推荐规则：

- 推荐能力不自动安装，也不强制重装。
- 用户可以安装、忽略或稍后处理。
- 用户卸载推荐能力后，不自动重装；可以继续在推荐列表中展示，或按策略允许用户隐藏。
- 推荐可以按部门、角色、项目、设备类型、Maclaw 版本范围展示。


### 3.2 管理员主动扩展能力市场

Hub / HubCenter 管理员也可以在自己的 `/marketplace` 中主动搜索外部来源，并把能力安装/导入到本市场，用于扩展能力市场内容。该流程复用 Maclaw 客户端同一套搜索、安装、导入、购买和收费控制逻辑。

来源范围不同：

- 企业 Hub 管理员：可以搜索企业 HubCenter Marketplace、ClawHub、GitHub。
- HubCenter 管理员：只能搜索 ClawHub、GitHub，不能把 HubCenter 自己作为外部来源。

```yaml
admin_marketplace_sources:
  hub:
    - hubcenter
    - clawhub
    - github
  hubcenter:
    - clawhub
    - github
```

规则：

- 免费能力：按导入策略进入本市场，可自动导入或进入待审核。
- 收费能力：企业 Hub 面向 HubCenter 时仍是客户，必须走 HubCenter 收费控制策略；HubCenter 从 ClawHub/GitHub 导入时按对应来源规则处理。
- 管理员主动导入后的能力仍需生成本市场自己的 capability 记录，并保留 origin/provenance。
- 该能力发布到本市场后，普通 Maclaw 用户再从企业 Hub 安装。
## 4. 搜索范围控制

现有 Skill Market 已有是否搜索 ClawHub / GitHub 的设置。能力市场需要把这些设置升级为统一的 `source scope policy`。

示例：

```yaml
capability_market_policy:
  view_mode: merged # enterprise_only | merged | separated
  enterprise_only_install: true
  enterprise_only_search: false
  managed_deployment:
    enabled: true
    retry_interval_minutes: 60
    reinstall_if_removed: true
  recommended_capability:
    enabled: true
    allow_user_dismiss: true

  source_priority:
    enterprise_hub: 100
    hubcenter: 80
    clawhub: 40
    github: 20

  resource_types:
    skill:
      allowed_sources:
        - enterprise_hub
        - hubcenter
        - clawhub
        - github
      default_sources:
        - enterprise_hub
        - hubcenter
      user_configurable_sources:
        - clawhub
        - github

    mcp:
      allowed_sources:
        - enterprise_hub
        - hubcenter
      default_sources:
        - enterprise_hub
      user_configurable_sources: []
```

规则：

- Hub 强制策略优先于用户本地偏好。
- 管理员可以设置 `enterprise_only_search=true`，此时 Maclaw 只允许搜索企业 Hub 能力市场，不访问 HubCenter、ClawHub、GitHub 等外部来源。
- 管理员可以设置 `enterprise_only_install=true`，此时 Maclaw 只允许从企业 Hub 下载和安装能力；即使外部搜索结果可见，也只能发起引入/购买申请，不能直接安装。
- 用户只能在企业允许的范围内启用或关闭搜索源。
- 合并视图只合并策略允许的来源。
- 搜索结果必须保留 `source`、`capability_type`、`pricing`、`enterprise_status` 等字段。
- 同 ID 或同包名冲突时，默认按 `source_priority` 选择企业 Hub 版本。

## 5. 免费能力的下载、使用与沉淀

当 Maclaw 在企业状态下从 HubCenter 或其他允许来源发现免费能力时，可以按策略直接下载和使用。

推荐流程：

```text
Discover free capability
  → Install / Download
  → Runtime verify
  → Promote metadata or package to Enterprise Hub
  → Pending review or auto publish
```

验证条件可以包括：

- Skill 安装成功、至少一次执行成功。
- MCP 配置校验成功、server 启动成功、握手成功、工具列表拉取成功。
- 包 checksum、签名、依赖和权限声明校验通过。

Hub 对沉淀结果的处理模式：

```yaml
free_capability_promotion:
  mode: disabled | manual | auto_metadata | auto_package_pending_review | auto_publish_trusted_sources
```

建议默认：

- 免费 Skill 可以 `auto_package_pending_review` 或可信来源自动发布。
- 免费 MCP 允许直接安装和使用，但必须完成配置校验、runtime health check 和权限记录；成功后按策略沉淀到 Hub。

沉淀不等于发布。推荐状态流：

```text
used_successfully
  → imported
  → pending_review
  → approved
  → published
```

## 6. 收费能力的购买审批

收费 Skill/MCP 都复用现有 Skill Market 的收费基础设施，只是 `capability_type` 多了 `mcp`。购买、审批、支付、授权、结算、退款、续费、收益归属和订单状态流不应为 MCP 另做一套。企业态 Maclaw 不能绕过 Hub 直接购买或下载收费能力；用户点击安装收费能力时，Maclaw 应创建 Hub 上的购买/引入申请。

流程：

```text
Maclaw sees paid capability from HubCenter
  → user clicks Request / Install
  → Hub creates acquisition request
  → admin reviews request
  → Hub purchases from HubCenter
  → Hub imports package/license
  → optional security review
  → publish to Enterprise Hub Capability Market
  → enterprise Maclaw installs from Hub
```

建议统一称为 `acquisition request`，其中可以包含免费导入、收费购买、License 审批等子流程。MCP 使用同一套订单和 license 表，只在资源详情中增加 MCP 专属字段。对 MCP 来说，收费对象可以是 MCP JSON 配置、远程 MCP 服务访问权、第三方 API 代理额度、工具调用套餐或企业席位授权。

### 6.1 Hub 管理端购买审批界面

Hub 管理后台需要新增“能力购买审批”页面，支持按类型、来源、状态筛选。

列表字段：

```yaml
request:
  id: req_xxx
  capability_type: skill
  capability_name: Code Review Pro
  capability_id: skill.code-review-pro
  version: 1.2.3
  source: hubcenter
  price:
    amount: 99
    currency: USD
    billing: one_time | monthly | yearly | seat_based
  license:
    type: enterprise | team | seat | usage_based
    seats_requested: 12
  requester:
    user_id: u_xxx
    name: Alice
    department: Engineering
  reason: 用于代码审查流程
  status: pending_review
  requested_at: 2026-05-14T10:20:00Z
```

详情页需要展示：

- 能力描述、版本、发布者、评分、下载量、更新时间。
- 价格、计费方式、席位数、试用期、续费规则。
- 权限声明、依赖、网络访问、文件访问、环境变量和 secret 需求。
- 申请人、申请理由、部门、已有同类能力、历史申请数量。
- 来源签名、checksum、license、供应商信息。
- 审批、采购、导入、发布历史。

管理员动作：

- 批准采购。
- 拒绝。
- 要求补充理由。
- 修改采购参数，例如席位数、版本、部门范围、预算中心。
- 批准但进入安全审核。
- 批准并发布到企业能力市场。
- 暂停、取消或重新发起采购。

推荐状态流：

```text
pending_review
  → approved
  → purchasing
  → purchased
  → importing
  → security_review
  → published
```

## 7. MCP 能力的创建、上传与编辑

MCP Market 不应只支持现成 MCP 的安装，还应支持用户上传、管理员编辑、模板生成和测试发布。

Maclaw 侧不应把 MCP Market 做成一个孤立页面，而应整合进现有 MCP 管理能力：在 MCP 管理中增加“搜索/下载 MCP”和“已安装 MCP”视图，复用现有 MCP 列表、启停、健康状态、工具列表和编辑能力；新增部分主要是市场来源、安装状态、授权状态和用户可手工编辑的 JSON/表单配置。

MCP 能力来源：

```text
MCP Capability
  ├─ 从 HubCenter 引入
  ├─ 用户从本地配置上传
  ├─ 管理员手工创建
  ├─ 管理员基于模板生成
  └─ Maclaw 成功使用后沉淀
```

MCP 资源形态：

```yaml
mcp:
  format: json_config | package | remote_server | template
  runtime: stdio | http | sse | docker | node | python | gateway
```

MCP 也支持和 Skill 类似的收费方式，方便第三方作者提供商业服务：

```yaml
mcp_commercial:
  pricing: free | paid | freemium | subscription
  billing: one_time | monthly | yearly | seat_based | usage_based | quota_based
  charge_target: config | remote_service | api_proxy | tool_calls | enterprise_license
  license_scope: user | team | department | enterprise
```

典型场景：

- 第三方作者发布一个收费 MCP JSON，用户购买后获得配置和更新权。
- 第三方作者运营远程 MCP 服务，企业购买后获得访问 license。
- MCP 代理第三方 API，按调用量、额度或席位计费。
- MCP 本身免费，但高级工具、企业 SLA 或更高额度收费。

基础 JSON 示例：

```json
{
  "name": "internal-jira",
  "displayName": "Internal Jira",
  "description": "Enterprise Jira MCP server",
  "command": "npx",
  "args": ["-y", "@company/jira-mcp"],
  "env": {
    "JIRA_BASE_URL": "https://jira.company.com",
    "JIRA_TOKEN": "${secret:jira_token}"
  },
  "permissions": {
    "network": ["jira.company.com"],
    "secrets": ["jira_token"]
  }
}
```

Hub 管理后台和 Maclaw 现有 MCP 管理界面需要整合 MCP 编辑器能力。市场安装的 MCP、用户手工创建的 MCP、企业 Hub 下发的 MCP，都应进入同一套已安装 MCP 列表，只用 `source` 和 `managed_by` 区分来源与可编辑范围。编辑器能力包括：

- MCP 搜索/下载视图：搜索 Hub、HubCenter 或策略允许的来源，展示免费/收费、已购/待审批、可安装/已安装状态。
- 已安装 MCP 视图：展示本地手工 MCP、Hub 下发 MCP、HubCenter 安装 MCP，复用现有启停、健康状态、工具列表和删除/禁用操作。
- JSON 编辑模式。
- 表单编辑模式。
- Schema 校验。
- command / args / env 编辑。
- 权限声明编辑。
- secret 占位符管理。
- 安装后 secret / API Key / token 设置界面，允许用户填写自己的认证信息。
- Secret 来源选择：Hub 托管、用户本地托管，或按企业策略固定。
- 缺失必填 secret 时显示“需要配置”，并提供一键进入配置界面。
- 测试连接和工具列表探测。
- 版本发布和权限 diff。
- 手工编辑范围控制：用户可编辑本地 MCP；企业下发 MCP 默认只允许编辑用户级 secret、默认参数和本地覆盖项；管理员可在 Hub 修改企业版本。

Secret 不能直接进入能力市场。MCP 配置只能引用 secret 占位符：

```text
${secret:name}
${user_secret:name}
${org_secret:name}
${hub_managed_secret:name}
```

MCP 安装后，Maclaw 需要根据 MCP 声明渲染认证配置界面：

```yaml
required_secrets:
  - name: JIRA_TOKEN
    label: Jira API Token
    scope: user
    storage: local_or_hub # local | hub | local_or_hub | hub_required
    required: true
    help_url: https://example.com/jira-token-help
```

用户可以在已安装 MCP 详情中填写、更新、测试自己的 API Key / token。Hub 托管 secret 适合企业统一凭据；本地托管 secret 适合个人 token、离线使用或企业不希望集中保存的凭据。配置完成后应立即触发 health check 和工具列表刷新。

管理员每次编辑 MCP 后应生成新版本，并记录 command、args、env、network、secret 和权限变化。

### 7.1 HubCenter 与企业 Hub 的 MCP 发布边界

HubCenter 可以支持开发者、社区用户或认证发布者上传 MCP，也可以支持第三方作者发布收费 MCP 服务。平台应明确标识发布者类型、验证状态和商业模式：

- 官方认证 MCP。
- 认证发布者 MCP。
- 社区上传 MCP。
- 未验证 MCP。
- 免费 MCP。
- 收费 MCP。
- Freemium / 订阅型 MCP。

企业 Hub 可以从 HubCenter 引入免费或收费 MCP，也可以由企业管理员直接创建私有 MCP。二者的差异是：HubCenter MCP 面向公共分发，企业 Hub MCP 面向企业内部治理和分发。企业 Hub 引入 HubCenter MCP 后，应保留 origin；企业管理员基于内部系统创建的 MCP，则默认属于企业私有能力，不自动上传到 HubCenter。

如果未来允许企业把私有 MCP 发布到 HubCenter，应走单独的对外发布审核，避免把内部域名、命令、secret 名称或系统结构暴露到公共市场。

## 8. 安全、授权与审计

### 8.1 安全扫描

能力进入企业 Hub 前，应进行自动检查：

- 包 checksum 和签名。
- 发布者可信度。
- 依赖项扫描。
- command、args、env、network、file、secret 权限声明。
- 新旧版本权限 diff。
- 是否请求敏感能力。
- 是否来自企业允许的来源。

MCP 的安全审核应比 Skill 更严格，因为 MCP 可能启动本地进程、访问网络、读取文件或使用企业凭据。

### 8.2 收费授权生命周期

收费能力入库后，Hub 需要管理：

- 购买类型：买断、订阅、试用、按量、按席位、按调用量或额度。
- 授权范围：全企业、部门、用户、设备、并发。
- 到期时间、续费提醒、超席位处理。
- 离职或解绑用户的席位释放。
- Maclaw 离线时的授权宽限期。

### 8.3 审计日志

能力市场需要记录：

- 谁搜索了什么。
- 谁申请了什么。
- 谁审批、拒绝或修改了什么。
- 谁安装、运行、卸载了什么。
- 执行时使用的能力版本、来源和权限。
- 哪些能力被撤回、下架或隔离。

审计应是能力市场基础设施的一部分，而不是 UI 后补功能。

## 9. 更新、下架与撤回

能力更新按来源分两类处理：

- 企业 Hub 上已经发布、下发或推荐的能力包，由企业 Hub 控制更新节奏。Maclaw 从企业 Hub 安装的 Skill/MCP 默认自动更新到企业 Hub 当前批准版本。
- HubCenter / SkillMarket 上的免费能力可以自动更新；收费能力、订阅能力、license 变更、价格变更和大版本升级必须遵守既有收费控制和授权策略。企业 Hub 面对 HubCenter 收费包时只是一个客户，不能绕过 HubCenter 对客户的收费、授权、额度、续费、退款、撤回和风控策略。

推荐更新策略：

```yaml
update_policy:
  enterprise_hub:
    default: auto_update_approved
    apply_to:
      - managed_deployments
      - installed_enterprise_capabilities
      - recommended_capabilities_installed_by_user
  hubcenter:
    free_capability: auto_update
    paid_capability: require_license_and_purchase_policy
    license_or_price_changed: require_admin_or_purchase_policy
    options:
      - auto_update_disabled
      - notify_admin
      - auto_import_pending_review
      - auto_update_patch_only
      - auto_update_trusted_publisher
```

企业 Hub 自动更新的含义是：Hub 管理员已经在企业市场发布了新版本，Maclaw 客户端应按策略自动拉取。HubCenter 有新版本时，免费能力可以自动更新；收费能力仍需按 SkillMarket 既有收费控制策略决定是否续费、补购、重新授权、审核或发布。企业 Hub 对 HubCenter 来说是购买方/客户，其购买、更新和使用都必须通过 HubCenter 的 license、订单、额度和风控校验。

HubCenter 发现资源风险或下架时，应向企业 Hub 发出撤回通知。Hub 可执行：

- 标记风险。
- 禁止新安装。
- 通知已安装用户。
- 禁用已安装版本。
- 回滚到安全版本。
- 提供替代能力。

## 10. UI 体验要求

Maclaw 能力市场合并视图中必须清楚表达来源和状态：

- 企业已批准，可安装。
- 来自 HubCenter，免费可用。
- 来自 HubCenter，收费，需要申请购买。
- 已有人申请，等待审批。
- 审批通过，采购中。
- 已发布到企业 Hub，可安装。
- 被组织策略禁用。
- 组织仅允许从企业 Hub 能力市场安装。
- 组织仅允许搜索企业 Hub 能力市场。
- 源自 HubCenter，但由企业 Hub 提供。

Maclaw MCP 管理部分需要和现有 MCP 管理界面整合：

- 增加 MCP 搜索/下载入口。
- 增加已安装 MCP 的市场来源、授权状态、更新状态展示。
- 保留现有 MCP 手工创建、编辑、启停、健康检查和工具列表能力。
- 市场安装 MCP 进入同一已安装列表，而不是另建一套运行时。
- 已安装 MCP 详情页提供 secret / API Key / token 设置、测试连接、工具列表刷新。
- 对缺少必填 secret 的 MCP 显示“需要配置”，不要把它当作安装失败。
- 用户可手工编辑本地 MCP；对企业/市场托管 MCP，只允许按策略编辑本地覆盖项和 secret 绑定。

Hub 管理后台至少需要：

- 能力市场管理页。
- 免费能力沉淀候选页。
- 收费能力购买审批页。
- MCP 创建/编辑/测试/发布页。
- 能力安全审核页。
- 授权和席位管理页。
- 审计日志页。

## 11. 协议与 API 草案

建议能力市场基础协议包含以下对象和操作：

```text
Capability Search
Capability Detail
Capability Download
Capability Import
Capability Promotion
Acquisition Request
Purchase / License Grant
Hub Customer Account / Billing Admin
Security Review
Update Check
Revocation Notice
Audit Event
Managed Deployment Policy
Recommended Capability Policy
MCP Secret Requirement / Binding
```

Hub 侧 API 草案：

```text
# Web entry: GET /marketplace
GET  /api/capabilities
GET  /api/capabilities/{id}
POST /api/capabilities/{id}/install-intent
POST /api/capabilities/promotions
GET  /api/capabilities/managed-deployments
GET  /api/capabilities/recommended
GET  /api/mcp/servers/{id}/secret-requirements
PUT  /api/mcp/servers/{id}/secret-bindings
GET  /api/admin/capabilities
POST /api/admin/capabilities
PUT  /api/admin/capabilities/{id}
POST /api/admin/capabilities/{id}/publish
POST /api/admin/capabilities/{id}/disable
PUT  /api/admin/capabilities/{id}/deployment-policy
PUT  /api/admin/capabilities/{id}/recommendation-policy
GET  /api/admin/acquisition-requests
GET  /api/admin/acquisition-requests/{id}
POST /api/admin/acquisition-requests/{id}/approve
POST /api/admin/acquisition-requests/{id}/reject
POST /api/admin/acquisition-requests/{id}/purchase
GET  /api/admin/billing/customer-account
GET  /api/admin/capabilities/external-search
POST /api/admin/capabilities/import-intent
```

HubCenter 侧 API 草案：

```text
# Web entry: GET /marketplace
GET  /api/capability-market/search
GET  /api/capability-market/capabilities/{id}
GET  /api/capability-market/capabilities/{id}/versions/{version}
GET  /api/capability-market/customer-account
GET  /api/capability-market/billing/licenses
POST /api/capability-market/acquisitions
GET  /api/capability-market/acquisitions/{id}
POST /api/capability-market/acquisitions/{id}/purchase
GET  /api/capability-market/licenses/{license_id}
GET  /api/capability-market/revocations
GET  /api/admin/capability-market/external-search
```

## 12. 分阶段落地建议

### Phase 1：统一市场抽象

- 将 Skill Market 概念升级为 Capability Market。
- 保留现有 Skill Market 入口兼容，但内部模型增加 `capability_type`。
- 支持企业 Hub 优先、HubCenter fallback、搜索范围策略。
- 支持 Hub 管理员从 HubCenter/ClawHub/GitHub 主动搜索并导入能力。
- 支持 HubCenter 管理员从 ClawHub/GitHub 主动搜索并导入能力。
- 支持企业 Hub 来源能力自动更新到当前批准版本。
- 支持企业下发清单，Maclaw best-effort 自动安装必须下发的 Skill/MCP。
- 支持企业推荐清单，Maclaw 展示推荐能力，由用户自主选择安装。
- 免费 Skill 支持下载、使用成功后沉淀到 Hub。

### Phase 2：MCP 能力市场

- 增加 `type=mcp`。
- 在现有 MCP 管理界面中增加 MCP 搜索/下载、已安装 MCP、市场来源和授权状态。
- 支持 MCP JSON 上传、管理员编辑、用户手工编辑、schema 校验和测试。
- 支持已安装 MCP 的 secret / API Key / token 配置界面。
- 支持 MCP 安全审核和权限 diff。
- 企业 Hub 发布 MCP 后，Maclaw 可从 Hub 安装配置。

### Phase 3：收费能力审批

- Hub 管理后台增加购买审批界面。
- 收费 Skill/MCP 点击安装时生成 acquisition request。
- Hub 代表企业向 HubCenter 采购、导入、入库、发布。
- 增加 License、席位和订阅生命周期管理。

### Phase 4：更新、撤回与治理增强

- 支持企业 Hub 当前批准版本自动更新到 Maclaw。
- 支持 HubCenter 免费能力自动更新。
- 支持 HubCenter 收费能力按 SkillMarket 既有收费控制策略更新。
- 支持撤回通知、安全隔离和回滚。
- 增强审计日志、指标、推荐和批量治理。

## 13. Review：待确认与风险点

### 13.1 命名边界

建议产品层统一叫“能力市场”，协议层使用 `Capability Market`。保留 `Skill Market` 作为旧入口或类型筛选名称，避免用户认知断裂。

待确认：现有 HubCenter / Maclaw UI 中的 Skill Market 是否立即改名，还是先在管理后台和协议中升级，前台逐步迁移。

### 13.2 免费 MCP 是否允许直装

免费 Skill 和免费 MCP 都允许直接安装并沉淀。MCP 风险更高，因此直装前后至少要有 schema 校验、runtime health check、权限记录和企业策略约束。

已确认：免费 MCP 允许直接安装。企业仍可通过策略限制来源、记录权限，并在成功使用后沉淀到 Hub；高风险 MCP 可进入安全审核队列。

### 13.3 收费能力的商业状态字段

HubCenter 搜索结果必须返回准确商业状态，否则 Maclaw 无法判断“直接安装”还是“申请购买”。

待确认字段：`pricing`、`billing`、`license_type`、`trial_available`、`enterprise_purchase_required`、`seat_model`、`usage_quota`、`charge_target`。这些字段同时适用于 Skill 和 MCP，并应复用现有 Skill Market 商业化架构。

### 13.4 Hub 代购与支付边界

“Hub 向 HubCenter 购买”需要明确支付主体、发票、退款、续费和企业合同关系。技术流程可以先抽象为 acquisition，但商业闭环要提前留字段。

已确认：收费能力只支持在线购买。管理员审批通过后，Hub 作为 HubCenter 的企业客户发起在线购买、获取 license、导入并发布。Hub 不能绕过 HubCenter 的客户收费/控制策略，包括价格、席位、额度、试用、续费、退款、撤回和风控。

### 13.4.1 HubCenter 企业客户登录与续费

Hub 用户在 HubCenter 上购买、续费和管理 license 时，使用 Hub 管理员邮箱登录，但登录后的商业主体是企业 Hub 客户账号，不是管理员个人账号。

```text
Hub 管理后台
  → 能力市场 / 购买与续费
  → 跳转 HubCenter /marketplace 或 billing 页面
  → 管理员邮箱验证码 / magic link / SSO 登录
  → HubCenter 校验 admin_email 是否管理该 hub_id
  → 展示该 hub_id 对应的订单、license、订阅、额度、发票
  → 管理员购买 / 续费 / 调整席位 / 查看额度
  → HubCenter 回写 license 状态
  → 企业 Hub 同步授权并更新企业市场状态
```

首版身份模型：

```yaml
hub_customer_account:
  hub_id: hub_xxx
  hub_name: Acme Hub
  admin_email: admin@acme.com
  customer_id: cust_xxx
  billing_account_id: bill_xxx
  status: active
  billing_admins:
    - admin@acme.com
    - finance@acme.com
```

规则：

- `admin_email` 可以作为首个 owner/admin 登录 HubCenter。
- HubCenter 必须校验该邮箱是否拥有或管理 `hub_id`。
- 购买、续费、发票、license、额度都归属到 `hub_id` / `customer_id`，不归属到个人 Maclaw 用户。
- 普通 Maclaw 用户不直接向 HubCenter 续费，只向企业 Hub 发起购买/使用申请。
- 后续可支持多个 `billing_admins`、企业 SSO、财务角色和只读审计角色。

### 13.5 MCP Secret 管理

MCP JSON 只应保存 secret 引用，不能保存明文凭据。Hub、Maclaw、本地系统如何注入 secret 需要单独设计。

已确认：MCP Secret 同时支持 Hub 托管和用户本地托管。MCP 配置只保存 secret 引用，运行时由策略决定从 Hub 注入、从本地读取，或两者组合。Maclaw 离线使用本地托管 secret；Hub 托管 secret 的离线缓存需要单独策略控制。

### 13.6 更新策略复杂度

能力更新涉及版本锁定、权限变化、授权变化和兼容性。特别是 MCP，更新可能改变命令和访问范围。

待确认：首版是否仅提示管理员有新版本，不自动更新；后续再做可信发布者自动更新。

### 13.7 审计数据量与隐私

记录搜索、安装和执行审计有治理价值，但也可能产生大量行为数据。

待确认：审计粒度默认到什么级别；是否记录执行参数；是否需要脱敏和保留期策略。

### 13.8 与现有 ClawHub / GitHub 设置兼容

现有设置需要平滑迁移到 source scope policy。

已确认：非企业模式继续使用本地设置；企业模式下，本地设置只能在企业允许范围内生效。管理员可进一步开启企业 Hub 限制模式：

- `enterprise_only_search=true`：只允许搜索企业 Hub 能力市场。
- `enterprise_only_install=true`：只允许从企业 Hub 下载和安装能力。企业 Hub 默认开启该设置。

默认情况下只开启 `enterprise_only_install`、不开启 `enterprise_only_search`：Maclaw 可以展示 HubCenter 等外部来源作为发现入口，但安装动作必须转成企业 Hub 的引入、审批或购买流程。管理员如果希望完全封闭外部发现入口，再开启 `enterprise_only_search=true`。

### 13.9 能力 ID 与 Fork 规则

定稿规则：能力身份分层管理，镜像不改上游身份，fork 必须生成企业身份，origin 永远保留。

#### 13.9.1 三层身份

能力市场使用三层 ID，避免“展示合并”和“真实身份”混在一起：

```text
registry_identity   = source + publisher + capability_id
version_identity    = registry_identity + version
enterprise_identity = enterprise_hub + enterprise_capability_id
```

其中：

- `registry_identity` 表示外部市场里的原始能力身份，例如 `hubcenter/acme/jira-mcp`。
- `version_identity` 唯一锁定某个可下载、可校验、可购买的版本，例如 `hubcenter/acme/jira-mcp@1.2.0`。
- `enterprise_identity` 表示企业 Hub 内部发布出来的能力身份，例如 `enterprise_hub/company/jira-mcp`。

能力对象中建议同时保留这几个字段：

```yaml
capability_identity:
  source: enterprise_hub
  publisher: company
  capability_id: jira-mcp
  capability_type: mcp
  version: 1.2.0-enterprise.1
  global_key: enterprise_hub/company/jira-mcp
  version_key: enterprise_hub/company/jira-mcp@1.2.0-enterprise.1
  origin_key: hubcenter/acme/jira-mcp@1.2.0
```

`global_key` 和 `version_key` 用于内部唯一性、审计和授权；展示名称、slug、标题可以改，但不能作为唯一身份。

#### 13.9.2 Origin 与 Provenance

企业从 HubCenter 引入能力时，不直接复用 HubCenter 的数据库主键，而是在企业 Hub 中生成企业自己的 capability 记录，同时保留完整来源链路。

```yaml
enterprise_capability:
  id: company.jira-mcp
  type: mcp
  version: 1.2.0-enterprise.1
  relation_to_origin: mirror
  origin:
    source: hubcenter
    publisher: acme
    capability_id: jira-mcp
    version: 1.2.0
    version_key: hubcenter/acme/jira-mcp@1.2.0
    checksum: sha256:...
    signature: sig_...
  provenance:
    imported_by: user_or_hub
    imported_at: 2026-05-14T10:20:00Z
    acquisition_request_id: req_xxx
    license_id: lic_xxx
```

如果能力经过多次流转，例如 GitHub → HubCenter → Enterprise Hub，`origin` 指直接来源，`provenance.chain` 保留完整链路：

```yaml
provenance:
  chain:
    - source: github
      publisher: acme
      capability_id: jira-mcp
      version: 1.0.0
    - source: hubcenter
      publisher: acme
      capability_id: jira-mcp
      version: 1.2.0
    - source: enterprise_hub
      publisher: company
      capability_id: jira-mcp
      version: 1.2.0-enterprise.1
```

#### 13.9.3 Mirror、Overlay、Fork、Derived

是否生成新企业 ID 取决于能力本体是否变化：

- `mirror/import`：未修改原包、Skill 行为或 MCP JSON，只是企业缓存、审核、授权和发布。企业 Hub 生成企业记录，保留 origin；展示上标记“源自 HubCenter”。
- `policy_overlay`：只改企业策略、可见范围、secret 绑定、默认参数、本地覆盖项，不改变能力本体。仍视为同一 origin 的企业发布版本，不算 fork。
- `fork`：修改 Skill 包内容、执行步骤、MCP command/args/schema、工具行为、协议语义、默认远程服务地址等能力本体。必须生成企业 fork ID，并保留 `forked_from`。
- `derived`：基于模板、多个能力组合、OpenAPI 生成、数据库连接生成等方式产生新能力。生成全新企业 ID，可保留 `derived_from` 列表。

示例：

```yaml
forked_capability:
  id: company.jira-mcp-secure
  relation_to_origin: fork
  forked_from:
    source: hubcenter
    publisher: acme
    capability_id: jira-mcp
    version: 1.2.0
  changes:
    - replaced remote endpoint with enterprise gateway
    - restricted tool list
    - added org secret binding
```

MCP 的特殊规则：

- 只绑定 `${user_secret:*}` 或 `${org_secret:*}` 不算 fork。
- 只修改本地显示名、默认启用状态、超时、重试、部门可见范围不算 fork。
- 修改 command、args、transport、工具 schema、远程 URL、权限边界或工具语义，算 fork。
- 用户手工创建的本地 MCP 没有外部 origin，上传 Hub 后成为企业自有 capability；如果来自某个市场条目的编辑副本，则保留 `forked_from`。

#### 13.9.4 搜索合并与冲突处理

搜索合并时，先按 `origin_key` / `registry_identity` 识别同源能力，再按来源优先级展示：

```text
enterprise_hub published mirror/fork
  > enterprise_hub pending/approved candidate
  > hubcenter result
  > clawhub/github result
```

合并视图规则：

- 如果企业 Hub 已发布某个 HubCenter 能力的 mirror，Maclaw 展示企业 Hub 条目，状态为“企业已批准，源自 HubCenter”。
- 如果企业 Hub 已发布 fork，Maclaw 展示企业 fork 条目，同时提示“企业改造版”，详情页展示上游版本。
- 如果 HubCenter 有新版本，但企业 Hub 仍锁定旧版本，Maclaw 展示企业版本，并在详情中提示“上游有新版本，等待管理员处理”；不会直接绕过企业 Hub 更新。
- 如果多个来源存在同名能力但 origin 不同，不自动合并，只在 UI 中提示“可能相似”。
- 如果同一来源同一 publisher 同一 capability_id 出现多个版本，按企业策略选择已批准版本，不能因为 HubCenter 最新就覆盖企业锁定版本。

#### 13.9.5 更新兼容性

企业 mirror 可以接收上游更新提示；HubCenter/SkillMarket 免费能力可以自动导入或更新；收费能力是否更新由既有收费控制和授权策略决定。企业 Hub 一旦发布新批准版本，Maclaw 对企业来源安装包默认自动更新。

企业 fork 也可以接收上游更新提示，但只能生成 diff / merge 候选，不能自动覆盖企业 fork。

更新检查需要比较：

- 版本号和 checksum。
- Skill 包内容或 MCP JSON 内容。
- command、args、transport、env、secret、network、file 权限变化。
- 商业授权变化，例如价格、席位、订阅规则、调用额度。
- 兼容性范围，例如 Maclaw 版本、Hub 版本、运行时依赖。

#### 13.9.6 兼容旧 Skill Market 与本地 MCP

为了兼容现有 Skill Market，旧 skill 的 ID 可以迁移为：

```text
legacy_skill_id -> capability_id
capability_type = skill
source = local | clawhub | github | hubcenter
```

旧 API 可以继续接受 `skill_id`，服务端内部映射到 `capability_id`。对外新 API 使用 `capability_id` 和 `capability_type`。

本地 MCP 兼容规则：

- 现有本地 MCP 配置迁移为 `capability_type=mcp`、`source=local`、`managed_by=user`。
- 从市场安装的 MCP 迁移为 `source=hubcenter|enterprise_hub`、`managed_by=market|hub`。
- 用户编辑市场 MCP 时，默认生成 `local_overlay`；只有修改能力本体时才提示“另存为 fork”。

#### 13.9.7 最终结论

- 唯一身份用 `source + publisher + capability_id + version`，不要依赖展示名。
- 企业 Hub 内部永远生成自己的企业 capability 记录，便于授权、审计、发布和下架。
- `origin` / `provenance` 永远保留，保证可追踪、可更新、可撤回。
- Mirror 不改变能力本体，fork 必须生成新企业 ID。
- 搜索可以合并展示，但安装、授权、审计和更新必须使用明确的 `version_key`。
- 旧 Skill Market 和现有 MCP 管理通过字段映射兼容，不需要推倒重来。

### 13.10 首期最小闭环

建议首期不要一次做完全部市场生态。最小闭环可以是：

```text
Hub 策略下发
  → Maclaw 企业态搜索 Hub + HubCenter
  → 免费 Skill 下载成功后沉淀 Hub
  → 收费 Skill/MCP 创建购买审批
  → Hub 管理员批准后在线购买并自动入库
```

MCP 编辑器和在线采购可以作为第二阶段加深。




















## 14. 开发规格：数据模型

首期建议用“能力通用表 + 类型扩展 JSON”的方式落地，先复用现有 Skill Market 商业化基础设施，避免为 MCP 另起一套订单和授权系统。

### 14.1 企业 Hub 数据表

```sql
-- 企业 Hub 内部能力主表
capabilities (
  id TEXT PRIMARY KEY,
  capability_type TEXT NOT NULL,          -- skill | mcp
  publisher TEXT NOT NULL,
  capability_id TEXT NOT NULL,
  display_name TEXT NOT NULL,
  description TEXT,
  source TEXT NOT NULL,                   -- enterprise_hub | hubcenter | clawhub | github | local
  managed_by TEXT NOT NULL,               -- hub | market | user
  status TEXT NOT NULL,                   -- draft | pending_review | approved | published | disabled | revoked
  relation_to_origin TEXT,                -- mirror | policy_overlay | fork | derived | local
  global_key TEXT NOT NULL,
  current_version_key TEXT,
  origin_key TEXT,
  origin_json TEXT,
  provenance_json TEXT,
  metadata_json TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

capability_versions (
  id TEXT PRIMARY KEY,
  capability_ref TEXT NOT NULL,
  version TEXT NOT NULL,
  version_key TEXT NOT NULL,
  package_url TEXT,
  package_checksum TEXT,
  package_signature TEXT,
  manifest_json TEXT NOT NULL,
  type_config_json TEXT,                  -- skill manifest extension or MCP JSON/config
  permissions_json TEXT,
  pricing_json TEXT,
  license_json TEXT,
  hub_customer_id TEXT,
  compatibility_json TEXT,
  status TEXT NOT NULL,                   -- draft | pending_review | approved | published | disabled | revoked
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

capability_acquisition_requests (
  id TEXT PRIMARY KEY,
  requester_user_id TEXT NOT NULL,
  capability_type TEXT NOT NULL,
  source TEXT NOT NULL,
  source_capability_key TEXT NOT NULL,
  source_version_key TEXT,
  request_kind TEXT NOT NULL,             -- import | purchase | license_approval
  status TEXT NOT NULL,                   -- pending_review | approved | rejected | purchasing | purchased | importing | security_review | published | failed | cancelled
  reason TEXT,
  price_json TEXT,
  license_json TEXT,
  hub_customer_id TEXT,
  approval_json TEXT,
  purchase_json TEXT,
  result_capability_id TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

capability_licenses (
  id TEXT PRIMARY KEY,
  capability_ref TEXT NOT NULL,
  source TEXT NOT NULL,
  source_license_id TEXT,
  license_type TEXT NOT NULL,
  scope TEXT NOT NULL,                    -- user | team | department | enterprise
  seats_total INTEGER,
  seats_used INTEGER,
  usage_quota INTEGER,
  expires_at INTEGER,
  status TEXT NOT NULL,                   -- active | expired | cancelled | over_limit
  raw_json TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

managed_capability_deployments (
  id TEXT PRIMARY KEY,
  capability_ref TEXT NOT NULL,
  capability_version_key TEXT,
  scope_json TEXT NOT NULL,
  deployment_policy TEXT NOT NULL,        -- required
  reinstall_if_removed INTEGER NOT NULL DEFAULT 1,
  retry_interval_minutes INTEGER NOT NULL DEFAULT 60,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

recommended_capabilities (
  id TEXT PRIMARY KEY,
  capability_ref TEXT NOT NULL,
  capability_version_key TEXT,
  scope_json TEXT NOT NULL,
  recommendation_reason TEXT,
  allow_user_dismiss INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

mcp_secret_requirements (
  id TEXT PRIMARY KEY,
  capability_ref TEXT NOT NULL,
  version_key TEXT NOT NULL,
  name TEXT NOT NULL,
  label TEXT,
  scope TEXT NOT NULL,                    -- user | org
  storage_policy TEXT NOT NULL,           -- local | hub | local_or_hub | hub_required
  required INTEGER NOT NULL DEFAULT 1,
  help_url TEXT,
  metadata_json TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

mcp_secret_bindings (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  mcp_server_id TEXT NOT NULL,
  requirement_name TEXT NOT NULL,
  storage TEXT NOT NULL,                  -- local | hub
  hub_secret_ref TEXT,
  local_secret_ref TEXT,
  status TEXT NOT NULL,                   -- missing | configured | invalid
  last_verified_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
```

索引建议：

```sql
CREATE UNIQUE INDEX idx_capabilities_global_key ON capabilities(global_key);
CREATE INDEX idx_capabilities_type_status ON capabilities(capability_type, status);
CREATE INDEX idx_capabilities_origin_key ON capabilities(origin_key);
CREATE UNIQUE INDEX idx_capability_versions_key ON capability_versions(version_key);
CREATE INDEX idx_acquisition_status ON capability_acquisition_requests(status);
CREATE INDEX idx_managed_scope ON managed_capability_deployments(enabled);
CREATE INDEX idx_recommended_scope ON recommended_capabilities(enabled);
CREATE UNIQUE INDEX idx_mcp_secret_binding ON mcp_secret_bindings(user_id, mcp_server_id, requirement_name);
```

### 14.2 HubCenter 数据表

HubCenter 可以复用现有 Skill Market 表和购买体系，新增或扩展字段：

```text
capability_type: skill | mcp
capability_id
version_key
type_config_json
mcp_commercial_json
publisher_verification_status
```

如果现有表名仍是 `skillmarket_*`，首期可以不立即改名，只在表内增加 `capability_type`。对外新接口和 UI 统一叫 Capability Market，内部逐步迁移。

## 15. 开发规格：状态机

### 15.1 免费能力沉淀

```text
external_discovered
  → installed
  → verified_successfully
  → promoted_to_hub
  → pending_review
  → published
```

免费 Skill 可以在可信来源下自动发布；免费 MCP 允许直接安装，但进入 Hub 沉淀后仍应记录权限和 health check 结果。

### 15.2 收费能力购买

```text
pending_review
  → approved
  → purchasing
  → purchased
  → importing
  → security_review
  → published
```

失败状态：`rejected`、`purchase_failed`、`import_failed`、`security_blocked`、`cancelled`。

收费 Skill/MCP 共用这条状态机。MCP 不新增商业状态机，只在 import/security 阶段多检查 MCP JSON、secret 声明、远程服务和工具列表。

### 15.3 企业 Hub 能力更新

```text
installed_enterprise_version
  → update_available_from_enterprise_hub
  → update_queued
  → updating
  → updated
```

失败状态：`update_failed`、`license_missing`、`policy_blocked`、`runtime_unhealthy`。

规则：企业 Hub 当前批准版本是企业内权威版本。Maclaw 从企业 Hub 安装的 Skill/MCP 默认自动更新；HubCenter/SkillMarket 免费新版本可自动更新；收费新版本必须先完成收费控制、license 校验、购买/续费或管理员审批。企业 Hub 在 HubCenter 收费体系中只是客户身份，必须遵循 HubCenter 对客户的收费、授权、额度和风控策略。

### 15.4 企业下发安装

```text
assigned
  → install_queued
  → installing
  → installed
  → needs_user_config     # MCP 缺少用户 secret
  → ready
```

失败状态：`install_failed`、`license_missing`、`policy_blocked`、`runtime_unhealthy`。

`needs_user_config` 不算安装失败。用户补齐 API Key/token 后触发 health check，成功后变为 `ready`。

### 15.5 推荐安装

```text
recommended
  → viewed
  → user_installed | dismissed | ignored
```

推荐不自动安装、不强制重装。用户卸载推荐能力后，不进入 required retry。

## 16. 开发规格：DTO

### 16.1 CapabilitySummary

```json
{
  "id": "company.jira-mcp",
  "capability_type": "mcp",
  "display_name": "Jira MCP",
  "description": "Connect Maclaw to Jira tools",
  "source": "enterprise_hub",
  "managed_by": "hub",
  "status": "published",
  "version": "1.2.0-enterprise.1",
  "version_key": "enterprise_hub/company/jira-mcp@1.2.0-enterprise.1",
  "origin_key": "hubcenter/acme/jira-mcp@1.2.0",
  "pricing": { "pricing": "paid", "billing": "seat_based" },
  "enterprise_status": "approved",
  "install_status": "not_installed",
  "recommendation": { "recommended": true, "reason": "推荐用于项目管理" },
  "deployment": { "required": false }
}
```

### 16.2 CapabilityDetail

```json
{
  "summary": {},
  "manifest": {},
  "type_config": {},
  "permissions": {},
  "origin": {},
  "provenance": {},
  "license": {},
  "mcp": {
    "runtime": "stdio",
    "secret_requirements": [],
    "tools_preview": [],
    "health_status": "unknown"
  }
}
```

### 16.3 InstallIntentRequest

```json
{
  "capability_id": "hubcenter/acme/jira-mcp",
  "capability_type": "mcp",
  "version": "1.2.0",
  "source": "hubcenter",
  "user_reason": "需要连接 Jira"
}
```

返回：

```json
{
  "action": "install_from_enterprise_hub | create_acquisition_request | blocked | already_installed",
  "capability_id": "company.jira-mcp",
  "request_id": "req_xxx",
  "message": "组织仅允许从企业 Hub 安装，已创建引入申请"
}
```

### 16.4 MCP Secret Requirement

```json
{
  "server_id": "mcp_local_123",
  "requirements": [
    {
      "name": "JIRA_TOKEN",
      "label": "Jira API Token",
      "scope": "user",
      "storage_policy": "local_or_hub",
      "required": true,
      "status": "missing",
      "help_url": "https://example.com/jira-token-help"
    }
  ]
}
```

## 17. 开发规格：页面与交互

### 17.1 Hub / HubCenter `/marketplace`

统一入口：

```text
GET /marketplace
```

页面模块：

- 全部能力、Skill、MCP 类型筛选。
- 来源筛选：企业 Hub、HubCenter、ClawHub、GitHub。
- 免费/收费筛选。
- 企业状态：已发布、待审核、已购买、需申请、组织禁用。
- 能力详情：版本、来源、权限、license、origin、更新状态。
- 收费能力：申请购买或管理员审批入口。
- 管理员模式：发布、下发、推荐、禁用、版本锁定。

### 17.2 Maclaw 能力市场

- 企业态默认先查企业 Hub。
- `enterprise_only_search=true` 时隐藏外部来源搜索入口。
- `enterprise_only_install=true` 时外部结果仅允许申请引入/购买，不直接安装。
- 推荐能力展示为“推荐安装”，由用户选择。
- 下发能力不展示为普通推荐，进入后台自动安装队列。

### 17.3 Maclaw MCP 管理

在现有 MCP 管理界面增加：

- 搜索/下载 MCP。
- 已安装 MCP：来源、授权、更新、健康状态。
- Secret 配置：API Key/token 填写、选择本地或 Hub 托管、测试连接。
- 缺少 secret 的 MCP 显示“需要配置”。
- 市场托管 MCP 默认只允许编辑 secret 绑定和本地覆盖项。
- 修改 command/args/schema 等能力本体时，提示另存为 fork。

## 18. 开发规格：配置迁移

现有配置：

```go
SkillSourcesAllowed []string
SkillPurchaseMode string
MCPServers []MCPServerEntry
LocalMCPServers []LocalMCPServerEntry
```

迁移目标：

```json
{
  "capability_market_policy": {
    "enterprise_only_install": true,
    "enterprise_only_search": false,
    "source_priority": {
      "enterprise_hub": 100,
      "hubcenter": 80,
      "clawhub": 40,
      "github": 20
    }
  }
}
```

兼容规则：

- 非企业模式继续读取本地 `SkillSourcesAllowed` 和现有 SkillHub 设置。
- 企业模式优先使用 Hub 下发的 `capability_market_policy`。
- 旧 `SkillPurchaseMode=free_only` 可以映射为禁止收费能力 install intent，或只允许创建购买申请。
- 现有本地 MCP 迁移为 `source=local`、`managed_by=user`。
- 市场安装 MCP 写入现有 MCP server registry，同时增加 capability metadata。

## 19. 首期开发任务切片

### Slice 1：模型与配置

- 增加 Capability 类型常量和基础 DTO。
- 增加企业 capability market policy，默认 `enterprise_only_install=true`。
- 保留旧 SkillMarket / SkillHub 配置兼容。

### Slice 2：HubCenter 能力市场兼容层

- 在现有 Skill Market 查询结果中增加 `capability_type`。
- MCP 条目复用现有收费、购买、license 基础设施。
- `/marketplace` 指向能力市场页面。

### Slice 3：企业 Hub 能力市场

- 增加企业能力表和版本表。
- 支持从 HubCenter import/purchase 后生成企业 capability。
- 支持 managed deployment 与 recommendation policy。

### Slice 4：Maclaw 企业态安装策略

- 拉取 Hub policy。
- 支持企业 Hub 优先搜索。
- 支持 `enterprise_only_install`，外部安装转为申请。
- 支持下发能力自动安装、推荐能力展示。

### Slice 5：MCP 管理整合

- MCP 管理增加搜索/下载入口。
- 已安装 MCP 增加 capability metadata。
- 增加 secret requirements / bindings API 和 UI。
- 缺少 secret 时进入 `needs_user_config` 状态。

### Slice 6：审批与购买

- Hub 管理端购买审批列表。
- 审批通过后在线购买 HubCenter 能力。
- 购买成功后导入企业 Hub 并发布。

## 20. 首期验收标准

- Hub 和 HubCenter 的能力市场 Web 入口均为 `/marketplace`。
- Hub 管理员外部搜索范围为 HubCenter/ClawHub/GitHub；HubCenter 管理员外部搜索范围为 ClawHub/GitHub。
- 企业 Hub 默认 `enterprise_only_install=true`。
- Maclaw 企业态不能绕过企业 Hub 直接安装外部能力。
- Hub 上 `capability_market_policy` 等配置变更后，下一次 Maclaw 心跳 ACK 必须返回最新 `hub_config`，客户端收到后更新本地企业策略缓存/配置。
- 外部免费能力在允许策略下可以发现；安装动作按企业策略执行。
- 收费 Skill/MCP 均走同一购买审批和在线购买流程。
- 管理员下发能力后，Maclaw 自动安装；推荐能力只展示，不自动安装。
- 企业 Hub 发布新批准版本后，Maclaw 自动更新企业来源 Skill/MCP。
- HubCenter/SkillMarket 免费能力新版本自动更新；收费能力新版本遵守收费控制和授权策略。
- MCP 安装后如果缺少 API Key/token，状态为“需要配置”，用户可在 MCP 管理界面填写并测试。
- 旧 Skill Market 和现有 MCP 管理功能保持可用。







## 21. 当前实现状态

本轮已落地首期基础设施切片：

- `corelib.AppConfig` 增加 `capability_market_policy`，默认 `enterprise_only_install=true`、`enterprise_only_search=false`。
- `corelib` 增加能力安装决策与更新决策函数，覆盖企业 Hub 优先、外部免费导入申请、外部收费购买申请、HubCenter 免费自动更新、收费遵守收费控制策略。
- Hub SQLite migration 增加企业能力市场基础表，包括能力、版本、购买申请、license、下发、推荐、MCP secret requirements/bindings；MCP secret requirement 已按 `capability_ref + version_key + name` 做幂等 upsert。
- Hub 增加 `/marketplace` Web 入口占位页。
- Hub 增加能力 API 入口：`/api/capabilities`、`/api/capabilities/{id}`、`/api/capabilities/{id}/versions`、`/api/capabilities/{id}/install-intent`、`/api/capabilities/managed-deployments`、`/api/capabilities/recommended`、`/api/admin/billing/customer-account`；其中列表、详情、版本、下发和推荐已接入 Hub SQLite capability service，install-intent 已按企业安装策略创建 import/purchase acquisition request。
- Hub 增加管理员能力管理 API：`POST /api/admin/capabilities` 创建/编辑企业能力和版本，`GET /api/admin/capability-market/acquisition-requests` 与 `GET /api/admin/capability-market/acquisition-requests/{id}` 查看免费导入/收费购买申请，`POST /api/admin/capability-market/acquisition-requests/{id}/approve|reject|complete` 管理审批和购买完成状态，`POST /api/admin/capability-market/managed-deployments` 创建必须下发能力，`POST /api/admin/capability-market/recommendations` 创建推荐能力。
- Hub 增加管理员 MCP 能力创建/编辑 API：`POST/PUT /api/admin/capability-market/mcp` 已在 Router 中接入，可把 MCP JSON、版本、定价信息和 secret requirements 写入企业能力市场。
- Hub 管理员外部搜索已接入 HubCenter Skill/MCP catalog，并补齐 ClawHub/GitHub Skill 搜索：`GET /api/admin/capabilities/external-search?source=hubcenter|clawhub|github&type=skill|mcp&q=...` 会按 Hub 管理员允许来源执行搜索；HubCenter 走 `/api/capability-market/search` 或 `/api/capability-market/mcp`，ClawHub/GitHub Skill 复用 `corelib/skill` 多源搜索客户端并统一映射为 capability search item。
- Hub 管理员从 HubCenter 导入免费 MCP 已打通：`POST /api/admin/capabilities/import-intent` 对免费 HubCenter MCP 会拉取 `/api/capability-market/mcp/{id}`，生成企业 Hub 自己的 mirrored capability/version，保留 origin，写入 MCP JSON 与 secret requirements，并把 acquisition request 标记为 completed。收费能力先创建 purchase acquisition request。
- HubCenter 增加 MCP 在线购买兼容 API：`POST /api/capability-market/mcp/{id}/purchase`，首期返回并持久化 purchase receipt、license 和 capability 信息，并要求收费 MCP 提交 `admin_email`。`GET /api/capability-market/billing/licenses?hub_id=...&admin_email=...` 可统一查询企业 Hub 已购买的 MCP license 与管理员邮箱下的 Skill purchase license。Hub 审批收费 HubCenter MCP acquisition request 后，会以企业 Hub 客户身份调用该接口，再导入 MCP、完成 request，并保存 purchase receipt。HubCenter Skill 也已接入同一审批后购买导入路径：Hub 会调用现有 SkillMarket download/purchase 入口完成购买，然后导入为企业 Hub mirrored Skill capability；purchase receipt 会移除加密包内容，只保留交易元数据。
- Hub 增加 MCP secret requirements 管理和查询 API：`POST /api/admin/capability-market/mcp-secret-requirements`、`GET /api/capabilities/{id}/mcp-secret-requirements`，用于 Maclaw MCP 安装后的 API key/token 配置界面。
- Hub 增加 MCP secret bindings 用户 API：`GET/PUT /api/capabilities/mcp-secret-bindings`。客户端可保存本地 secret 引用或 Hub 托管 secret 引用；API 只保存引用和配置状态，不要求把明文 token 写入能力表。
- Hub 增加首版 MCP Hub 托管 secret 存储 API：`GET/PUT /api/capabilities/mcp-hub-secrets`。写入 secret 后只返回 metadata/digest，不回显 secret value，并自动创建 `storage=hub` 的 MCP secret binding；后续可替换为 KMS/DPAPI/企业密钥服务加密实现。
- Maclaw 增加 Hub 能力市场客户端基础代码，可读取 managed deployments / recommended capabilities，并对企业下发 MCP 执行本地安装到现有 MCP registry；市场安装的 MCP 会记录 capability metadata，缺少必填 secret 时返回 `needs_user_config`，供 UI 打开 API key/token 配置界面。已接入 Hub 连接成功和心跳 ACK 后的后台同步，并提供推荐能力手动安装、能力详情读取、MCP secret requirements 查询、secret binding 保存方法。MCP 管理页已加入能力市场面板，可以同步下发能力并展示推荐能力的名称、来源、类型和安装结果。
- Maclaw 已把 Skill 也接入能力版本追踪：本地安装的企业能力 Skill 会记录 `capability_id/version/source/global_key`，`HubSkillID/HubVersion` 继续保留底层 SkillHub 包信息；后台同步现在会同时检查已安装 Skill 与 MCP 的企业能力版本，并按 `CapabilityUpdateDecision` 自动更新企业 Hub 批准的新版本，HubCenter 免费能力自动更新，收费能力仍进入审批/授权策略。
- Maclaw 管理安装 Skill 时已支持替换旧企业能力版本并保留运行统计、修复历史等本地使用数据；file-backed Skill 在 staging 安全扫描后提交到最终目录时会把 `craft_tool.working_dir` 改写为最终安装目录，避免运行时仍引用临时 staging 路径。
- MCP 管理页已加入 Hub/local 两种 secret 配置编辑入口：用户可以在安装后的 MCP 详情里查看市场声明的 secret requirements，选择托管到 Hub 或保存在本地引用，并通过 Wails binding 调用 Hub API 保存 digest/binding。
- Maclaw MCP 管理页的能力市场面板已从“仅推荐/下发同步”扩展为企业 Hub MCP 搜索/安装视图：前端可调用 `ListHubCapabilities("mcp", query)` 搜索企业 Hub 上的 MCP capability，展示已安装状态，并直接安装到现有 MCP registry；安装后仍复用 secret requirements / Hub-local secret 配置界面。
- Hub 增加能力市场策略管理 API：`GET/PUT /api/admin/capability-market/policy`，写入系统配置 `capability_market_policy`。
- Hub WebSocket 心跳 ACK 增加 `hub_config.capability_market_policy`，Maclaw 客户端在下一次心跳收到后保存到本地配置并触发 `hub-config-options-changed` 事件。
- Hub 管理后台增加首版 `marketplace-tab.js`：统一展示企业安装/搜索策略、HubCenter 购买审批、企业 capability 列表、外部市场搜索导入、HubCenter billing/license 查询，以及管理员 MCP JSON 创建/编辑入口；`/admin` 侧已挂载导航、面板和模块校验。
- HubCenter 增加 `/marketplace` 到现有 SkillMarket UI 的入口映射。
- HubCenter 增加企业客户和 license 查询 API：`/api/capability-market/customer-account`、`/api/capability-market/billing/licenses`；license 查询会合并 MCP purchase 和 SkillMarket purchase。
- Hub 增加管理员账单代理 API：`GET /api/admin/billing/customer-account` 返回 HubCenter customer 绑定状态、管理员邮箱和 hub_id，`GET /api/admin/billing/licenses` 会携带 hub_id/admin_email 代理查询 HubCenter license，供 Hub 管理后台显示续费和授权状态。
- HubCenter 增加能力搜索兼容入口：`/api/capability-market/search` 复用现有 SkillMarket 搜索。
- HubCenter 增加 MCP catalog API：`GET /api/capability-market/mcp`、`GET /api/capability-market/mcp/{id}`、`POST/PUT/DELETE /api/admin/capability-market/mcp`，首期存储在 system settings，便于管理员上传、编辑和发布 MCP JSON。
- HubCenter 管理员外部搜索已接入路由：`GET /api/admin/capabilities/external-search` 只允许 ClawHub 与 GitHub 来源；Skill 搜索复用 `corelib/skill` 的 ClawHub/GitHub 搜索客户端，MCP 搜索先返回空列表等待后续外部 MCP 源适配。

下一步开发应继续完善真实 MCP 扣费/license 持久化、Hub 托管 secret vault、ClawHub/GitHub 外部搜索导入适配，以及企业 Hub 对上游新版本的检测、审批和发布 UI。
