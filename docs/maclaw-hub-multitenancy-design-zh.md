# MaClaw Hub 多租户多用户设计方案

## 1. 背景与目标

本设计只改造 **MaClaw Hub**。Hub Center 仍保持现有 Hub 发现、入口解析和平台治理职责，不在本阶段承担多租户统一身份中心。

当前 Hub 的身份与管理模型偏单管理域：管理员、用户、设备、会话、邀请码、审批、策略和数字员工授权默认共享同一个 Hub 空间。企业版需要让一个 Hub 同时承载多个租户，并让不同租户的数据、授权和管理操作互相隔离。

目标：

- Hub 全局管理员可以创建、停用、恢复租户。
- Hub 全局管理员可以为租户创建租户管理员。
- 租户管理员可以登录 Hub 管理后台，但只能管理自己租户内的用户、设备、邀请、审批、策略和数字员工授权。
- Hub 支持多租户多用户：同一个 Hub 内存在多个租户，每个租户下存在多个用户和管理员。
- 数字员工授权改为面向租户授权：注册状态按租户显示数字员工授权、配额和有效期；修改授权也必须以租户为作用域。
- 老版本单租户 Hub 数据可以迁移到默认租户，保证平滑升级。

## 2. 核心概念

### 2.1 租户

租户是 Hub 内的数据、授权、策略和管理边界。一个租户拥有：

- 租户管理员。
- 普通用户。
- 用户设备、机器 token、会话。
- 邀请码、审批流、邮箱登录 token。
- 企业组织与策略。
- 模型服务、Skill/MCP 能力策略。
- 数字员工授权、配额、有效期。
- 本租户审计日志。

### 2.2 全局管理员

全局管理员属于 Hub，不属于某个租户。全局管理员能力：

- 初始化 Hub。
- 创建和管理租户。
- 创建租户管理员。
- 查看跨租户运行状态。
- 修改租户级数字员工授权。
- 在审计记录完整的前提下进入某个租户做排障。

### 2.3 租户管理员

租户管理员只属于一个租户。租户管理员能力：

- 管理本租户用户。
- 管理本租户设备和会话。
- 管理本租户邀请码、审批、黑名单。
- 查看本租户数字员工授权状态、配额和有效期。
- 按权限使用或申请修改本租户数字员工配置。
- 查看本租户审计。

租户管理员不能看到其它租户，也不能通过 URL、查询参数或接口请求指定其它 `tenant_id`。

## 3. 角色与权限

| 角色 | 作用域 | 能力 |
| --- | --- | --- |
| `global_owner` | Hub 全局 | 创建租户、创建租户管理员、停用租户、修改租户数字员工授权、查看全局审计 |
| `global_operator` | Hub 全局 | 查看租户和运行状态，默认不允许创建租户或修改授权 |
| `tenant_owner` | 单租户 | 管理本租户管理员、用户、设备、策略、数字员工配置 |
| `tenant_operator` | 单租户 | 管理本租户用户、设备、审批、邀请码，查看授权状态 |
| `tenant_viewer` | 单租户 | 只读查看本租户资源和审计 |
| `user` | 单租户 | 普通用户登录 PWA、桌面端注册、使用会话和数字员工能力 |

V1 建议先实现 `global_owner`、`tenant_owner`、`tenant_operator`、`tenant_viewer`。`global_operator` 预留字段和鉴权枚举。

## 4. 租户生命周期

租户状态：

- `active`：正常使用。
- `suspended`：普通用户和机器不能新增登录、注册或创建会话；租户管理员可以登录查看状态和原因。
- `disabled`：租户管理员和普通用户都不能登录；只允许全局管理员恢复。
- `deleted`：软删除，默认不展示，保留恢复窗口和审计。

创建租户流程：

1. 全局管理员提交租户名称、短码、主域名、初始租户管理员。
2. Hub 创建 `tenants` 记录。
3. Hub 创建第一位 `tenant_owner`。
4. Hub 初始化租户级默认设置，包括数字员工授权的默认状态。
5. Hub 写审计：`tenant.created`、`tenant_admin.created`。

## 5. 租户识别

Hub 内所有入口都要能解析租户。

管理员登录：

- 不传 `tenant`：只尝试全局管理员登录。
- 传 `tenant`：只尝试该租户管理员登录。
- 如果同一用户名存在多个租户，必须传租户短码或租户 ID。

普通用户和桌面端注册：

- 优先使用入口 URL 参数，例如 `/app?tenant=acme`。
- 其次使用邀请码绑定的 `tenant_id`。
- 再其次使用邮箱域名映射。
- 多个租户匹配时返回 `TENANT_REQUIRED`，不能静默归属。

机器鉴权：

- `machine_token` 绑定机器。
- 机器绑定用户。
- 用户绑定租户。
- 所有机器 API 自动继承该 `tenant_id`。

## 6. 数据模型

### 6.1 新增租户表

```sql
CREATE TABLE tenants (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  primary_domain TEXT NOT NULL DEFAULT '',
  settings_json TEXT NOT NULL DEFAULT '{}',
  created_by_admin_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);

CREATE TABLE tenant_domains (
  tenant_id TEXT NOT NULL,
  domain TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  verified_at TEXT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, domain)
);
```

### 6.2 管理员表调整

`admin_users` 增加字段：

- `scope`: `global` 或 `tenant`。
- `tenant_id`: 全局管理员为空，租户管理员必填。
- `role`: `global_owner`、`global_operator`、`tenant_owner`、`tenant_operator`、`tenant_viewer`。
- `display_name`。
- `last_login_at`。

唯一性建议：

- 全局管理员用户名全局唯一。
- 租户管理员用户名在 `tenant_id + username` 内唯一。
- 邮箱是否全局唯一可配置；V1 建议全局唯一，减少登录歧义。

### 6.3 用户与业务表调整

以下租户级资源必须增加 `tenant_id`：

- `users`
- `user_enrollments`
- `email_blocklist`
- `invitation_codes`
- `email_invites`
- `machines`
- `sessions`
- `viewer_tokens`
- `login_tokens`
- `audit_logs`
- `admin_audit_logs`
- `failure_event_logs`
- `voiceprints`
- `content_audit_logs`
- `gossip_posts`、`gossip_comments`
- `understanding_sessions`、`workflow_states`
- 数字员工授权记录
- 企业组织、模型服务、Skill/MCP 策略
- 当前已有 `tenant_id` 的 `a2a_group_*` 表统一接入租户上下文

核心索引：

```sql
CREATE UNIQUE INDEX idx_users_tenant_email ON users(tenant_id, email);
CREATE UNIQUE INDEX idx_users_tenant_sn ON users(tenant_id, sn);

CREATE UNIQUE INDEX idx_machines_tenant_user_client
ON machines(tenant_id, user_id, client_id);

CREATE UNIQUE INDEX idx_invitation_codes_tenant_code
ON invitation_codes(tenant_id, code);
```

## 7. 数字员工授权租户化

### 7.1 授权边界

数字员工授权从 Hub 全局授权改为租户授权。一个租户对应一份授权状态：

- 是否启用数字员工。
- 授权配额。
- 已使用数量。
- 授权开始时间。
- 授权有效期。
- 授权到期时间。
- 授权来源。
- 授权状态。
- 最近修改人和修改时间。

租户内用户和机器使用数字员工能力时，只消耗所属租户的配额。

### 7.2 数据表

建议新增 `tenant_digital_employee_authorizations`：

```sql
CREATE TABLE tenant_digital_employee_authorizations (
  tenant_id TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL DEFAULT 0,
  quota INTEGER NOT NULL DEFAULT 0,
  used INTEGER NOT NULL DEFAULT 0,
  valid_from TEXT,
  valid_until TEXT,
  status TEXT NOT NULL DEFAULT 'inactive',
  source TEXT NOT NULL DEFAULT 'manual',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  updated_by_admin_id TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
```

`status` 建议枚举：

- `active`：授权有效。
- `inactive`：未启用。
- `expired`：已过期。
- `quota_exhausted`：配额耗尽。
- `suspended`：租户被暂停或授权被人工暂停。

### 7.3 注册状态展示

现有注册状态接口需要按租户展示数字员工授权。建议响应结构：

```json
{
  "tenant": {
    "id": "tenant_acme",
    "slug": "acme",
    "name": "Acme Corp",
    "status": "active"
  },
  "digital_employee_authorization": {
    "enabled": true,
    "status": "active",
    "quota": 20,
    "used": 7,
    "remaining": 13,
    "valid_from": "2026-05-01T00:00:00Z",
    "valid_until": "2027-05-01T00:00:00Z",
    "source": "global_admin",
    "updated_at": "2026-05-18T10:00:00Z"
  }
}
```

展示规则：

- 全局管理员查看注册状态时，可以选择租户，并看到该租户授权。
- 租户管理员查看注册状态时，只显示自己租户授权。
- 普通用户或机器查询状态时，只返回其所属租户授权。
- 不允许租户管理员通过传 `tenant_id` 查看其它租户授权。

### 7.4 授权修改

全局管理员修改租户数字员工授权：

`PUT /api/admin/tenants/{tenantId}/digital-employee-authorization`

```json
{
  "enabled": true,
  "quota": 20,
  "valid_from": "2026-05-01T00:00:00Z",
  "valid_until": "2027-05-01T00:00:00Z",
  "status": "active"
}
```

租户管理员修改本租户数字员工配置：

`PUT /api/admin/digital-employee/config`

此接口只允许修改租户内配置项，例如默认名称、可见性、审批开关、部门策略等，不允许提升授权配额或延长有效期。是否允许租户管理员申请授权变更可作为 V2。

后端规则：

- 授权配额和有效期只能由 `global_owner` 修改。
- 租户管理员只能修改本租户数字员工运行配置。
- 每次修改都写 `tenant_id` 审计。
- 到期或配额耗尽时，新建数字员工会话必须被拒绝，已有会话按策略继续或终止。

### 7.5 数字员工能力校验

所有数字员工相关入口统一调用：

```go
CheckTenantDigitalEmployeeAuthorization(ctx, tenantID, requestedCount)
```

返回：

- `allowed`
- `reason`
- `status`
- `quota`
- `used`
- `remaining`
- `valid_until`

拒绝原因：

- `TENANT_SUSPENDED`
- `DIGITAL_EMPLOYEE_DISABLED`
- `DIGITAL_EMPLOYEE_EXPIRED`
- `DIGITAL_EMPLOYEE_QUOTA_EXHAUSTED`
- `TENANT_NOT_FOUND`

## 8. 后端架构改造

### 8.1 统一鉴权上下文

新增：

```go
type AuthScope struct {
    AdminID   string
    Username  string
    Scope     string // global | tenant | user | machine
    Role      string
    TenantID  string
    UserID    string
    MachineID string
}
```

中间件负责：

- 管理员 token 解析后写入 `AuthScope`。
- 租户管理员请求自动绑定 `TenantID`。
- 普通用户 token 解析后写入用户所属 `TenantID`。
- 机器 token 解析后写入机器所属 `TenantID`。
- 全局管理员访问租户资源时，从路径解析 `tenantId`，并校验接口是否允许全局访问。

### 8.2 Repository 改造

租户过滤必须下沉到 service/repository，不能依赖前端传参。

示例：

```go
GetUserByEmail(ctx, tenantID, email)
ListUsers(ctx, tenantID, filter)
ListMachinesByUserID(ctx, tenantID, userID)
ListSessions(ctx, tenantID, filter)
GetDigitalEmployeeAuthorization(ctx, tenantID)
UpdateDigitalEmployeeAuthorization(ctx, tenantID, patch)
```

全局跨租户查询必须使用单独方法，例如：

```go
GlobalListTenants(ctx, filter)
GlobalGetTenantSummary(ctx, tenantID)
```

不要用空 `tenantID` 表示全局查询。

### 8.3 IdentityService 改造

优先改造身份链路：

- `StartEnrollment` 增加租户解析结果。
- `RequestEmailLogin` 按租户查用户和登录 token。
- `ManualBind` 在当前租户内创建用户。
- `AuthenticateMachine` 返回 `TenantID`。
- `IssueViewerTokenForMachine` 写入 `tenant_id`。
- 用户创建后按租户授予默认模型服务和默认数字员工可见性。

## 9. API 设计

### 9.1 全局租户管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/tenants` | 租户列表 |
| `POST` | `/api/admin/tenants` | 创建租户和初始租户管理员 |
| `GET` | `/api/admin/tenants/{tenantId}` | 租户详情 |
| `PATCH` | `/api/admin/tenants/{tenantId}` | 修改租户基础信息或状态 |
| `POST` | `/api/admin/tenants/{tenantId}/admins` | 创建租户管理员 |
| `GET` | `/api/admin/tenants/{tenantId}/admins` | 租户管理员列表 |
| `PUT` | `/api/admin/tenants/{tenantId}/digital-employee-authorization` | 修改该租户数字员工授权 |
| `GET` | `/api/admin/tenants/{tenantId}/digital-employee-authorization` | 查看该租户数字员工授权 |

创建租户请求：

```json
{
  "slug": "acme",
  "name": "Acme Corp",
  "primary_domain": "acme.com",
  "admin": {
    "username": "admin",
    "email": "admin@acme.com",
    "password": "initial-password"
  },
  "digital_employee_authorization": {
    "enabled": true,
    "quota": 10,
    "valid_until": "2027-05-01T00:00:00Z"
  }
}
```

### 9.2 租户管理员接口

租户管理员继续使用现有 Hub 管理接口路径，后端根据 token 自动加租户过滤：

- `GET /api/admin/users`
- `POST /api/admin/users/manual-bind`
- `GET /api/admin/debug/machines`
- `GET /api/admin/sessions/all`
- `GET /api/admin/enrollments/pending`
- `GET /api/admin/invitation-codes`
- `GET /api/ve/status`
- `PUT /api/ve/settings`
- `GET /api/admin/digital-employee/config`
- `PUT /api/admin/digital-employee/config`

响应建议统一补租户信息：

```json
{
  "tenant": {
    "id": "tenant_acme",
    "slug": "acme",
    "name": "Acme Corp"
  },
  "items": []
}
```

## 10. 现有 Hub 功能的租户化改造清单

多租户改造不能只改用户表，需要把当前 Hub 已有功能按“全局配置、租户配置、租户资源、用户资源”重新分层。

| 功能 | 当前语义 | 多租户后语义 | 处理方式 |
| --- | --- | --- | --- |
| 管理员初始化 | Hub 单管理员域 | 初始化全局管理员 | 历史管理员迁移为 `global_owner` |
| 管理员登录 | 用户名密码登录 | 全局登录或租户登录 | 登录请求可带 `tenant`；token 写入 `scope/role/tenant_id` |
| Hub Center 注册 | Hub 级注册 | Hub 仍注册一次，但上报租户路由摘要 | Hub 级配置保留，租户路由随心跳/同步上报 |
| 工作模式/注册模式 | Hub 级 `open/approval/manual` | 租户级注册工作模式 | 全局可设默认值，租户可覆盖；用户接入时按邮箱解析租户后读取该租户模式 |
| 手动绑定 | Hub 级邮箱绑定 SN | 租户内邮箱绑定 SN | 同邮箱可在不同租户绑定；SN 在租户内唯一 |
| 用户列表 | Hub 全量用户 | 当前租户用户 | 租户管理员自动过滤；全局管理员需选择租户 |
| 邮箱登录 | 邮箱直接定位 Hub 用户 | 邮箱先定位租户，再定位租户内用户 | 接入仍按邮箱，但路由/域名/邀请码决定租户 |
| 邀请码 | Hub 级邀请码 | 租户级邀请码 | 邀请码记录带 `tenant_id`，可在不同租户重复 |
| PWA 审批 | Hub 级审批池 | 租户级审批池 | 租户管理员只能审批本租户请求 |
| 黑名单 | Hub 级邮箱黑名单 | 租户级黑名单 + 可选全局黑名单 | 全局黑名单优先，租户黑名单其次 |
| 邮件配置 | Hub 级 SMTP | Hub 默认 SMTP + 租户覆盖模板/发件策略 | V1 可先全局 SMTP，租户级邮件模板和发件名 |
| 机器管理 | Hub 全量机器 | 租户内机器 | 机器鉴权返回 `tenant_id`，所有机器查询带租户过滤 |
| 会话管理 | Hub 全量会话 | 租户内会话 | session、A2A、VE history 查询都带 `tenant_id` |
| 企业组织与策略 | Hub 级组织树 | 租户级组织树 | 每个租户一棵组织树和一套策略继承链 |
| 模型服务 | Hub 级服务组/授权 | 租户级服务组，可继承 Hub 默认 | 服务组、兑换卡、授权诊断都要带 `tenant_id` |
| Skill/MCP 策略 | Hub 级能力下发 | 租户级能力策略 | 能力策略、合规快照、导出带 `tenant_id` |
| IM 插件绑定 | Hub 级绑定 | 租户级绑定 | 飞书/企微/钉钉/openclaw IM 绑定表增加 `tenant_id` |
| Gossip/公告/评论 | Hub 级内容 | 视产品定位决定全局或租户级 | 默认租户级；全局公告另设 `scope=global` |
| 失败日志 | Hub 全局日志 | 全局日志 + 租户日志 | 租户管理员只能看本租户；全局管理员可跨租户筛选 |
| 备份恢复 | Hub 整体备份 | Hub 整体备份，租户可导出 | V1 仍整体备份；V2 做租户级导出/恢复 |
| 模型下载 | Hub 运行时资源 | Hub 全局资源 | 不按租户复制文件，只在授权和可见性上按租户控制 |
| 数字员工授权 | Hub 级授权 | 租户级授权 | 注册状态、修改、配额消耗全部按 `tenant_id` |

### 10.1 系统设置与工作模式

系统设置需要拆成两层：

- Hub 全局设置：监听地址、TLS、数据库、Hub Center 地址、默认 SMTP、模型文件下载、全局安全底线。
- 租户设置：工作模式/注册模式、可见性、邮箱域名、邀请策略、审批策略、邮件模板、组织策略、数字员工运行配置。

因此“系统设置中的工作模式”需要按租户处理。建议字段：

```sql
CREATE TABLE tenant_settings (
  tenant_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value_json TEXT NOT NULL,
  updated_by_admin_id TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, key)
);
```

工作模式读取优先级：

1. 租户设置 `tenant_settings[tenant_id].enrollment_mode`。
2. Hub 全局默认 `system_settings.default_enrollment_mode`。
3. 代码默认 `approval`，多租户场景不建议默认 `open`。

接入流程仍然“按邮箱即可”，但不是只用邮箱查用户，而是：

1. 规范化邮箱。
2. 通过 Hub Center 路由、Hub 本地域名映射或邀请码确定租户。
3. 读取该租户工作模式。
4. 按模式执行自动创建、等待审批或要求手动绑定。

## 11. Hub Center 路由查询的租户化影响

Hub Center 上的路由查询也要考虑租户情况，但它不需要变成完整的租户管理中心。它要解决的是“一个邮箱应该进入哪个 Hub 的哪个租户入口”。

### 11.1 路由粒度

当前 Hub Center 主要按邮箱或域名解析到 Hub。多租户后，路由结果需要从 Hub 级升级为 Hub + tenant 级：

```json
{
  "email": "alice@acme.com",
  "routes": [
    {
      "hub_id": "hub_123",
      "hub_name": "East China Hub",
      "base_url": "https://hub.example.com",
      "tenant_id": "tenant_acme",
      "tenant_slug": "acme",
      "tenant_name": "Acme Corp",
      "entry_url": "https://hub.example.com/app?tenant=acme&email=alice%40acme.com",
      "match_type": "tenant_domain",
      "enrollment_mode": "approval"
    }
  ]
}
```

### 11.2 Hub 向 Hub Center 上报的内容

Hub 注册仍然是 Hub 级注册，但心跳或同步接口需要附带租户路由摘要：

- `tenant_id`
- `tenant_slug`
- `tenant_name`
- `status`
- `domains`
- `public_entry_path`，例如 `/app?tenant=acme`
- `enrollment_mode`
- `accept_public_signup`
- `updated_at`

Hub Center 不保存租户用户明细，只保存路由所需的租户域名和入口信息。

### 11.3 邮箱路由规则

邮箱路由优先级建议：

1. 精确邮箱绑定：`hub_user_links(email, hub_id, tenant_id)`。
2. 租户域名路由：`tenant_domain_routes(domain, hub_id, tenant_id)`。
3. Hub 级公共路由：只在该 Hub 只有一个可接入租户，或请求明确带 `tenant` 时使用。
4. 多匹配时返回候选列表，让用户选择租户，不自动挑选。

### 11.4 Hub Center 表结构调整

可新增或扩展：

```sql
CREATE TABLE hub_tenant_routes (
  hub_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  tenant_slug TEXT NOT NULL,
  tenant_name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  enrollment_mode TEXT NOT NULL DEFAULT 'approval',
  public_entry_path TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (hub_id, tenant_id)
);

CREATE TABLE hub_tenant_domain_routes (
  domain TEXT NOT NULL,
  hub_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (domain, hub_id, tenant_id)
);
```

现有 `hub_user_links` 如果继续用于精确邮箱路由，需要增加 `tenant_id`。如果不改字段，就无法区分同一个 Hub 内多个租户。

### 11.5 Hub Center 管理后台影响

路由查询页需要展示：

- 匹配到的 Hub。
- 匹配到的租户。
- 匹配方式：精确邮箱、租户域名、Hub 默认。
- 最终入口 URL。
- 租户工作模式。

Hub 列表页可增加租户摘要：租户数量、活跃租户数量、最近同步时间。Hub Center 不负责创建 Hub 内租户，创建仍在 Hub 全局管理员后台完成。

## 12. 前端设计

### 10.1 全局管理员首页

全局管理员登录后进入“租户管理”：

- 顶部指标：租户数、活跃租户、暂停租户、总用户数、在线设备数、数字员工授权租户数。
- 租户列表：名称、短码、域名、状态、管理员数、用户数、设备数、数字员工授权状态、有效期。
- 租户详情：基础信息、管理员、数字员工授权、审计、危险操作。
- 创建租户弹窗：租户信息、初始管理员、数字员工初始授权。

### 10.2 租户管理员首页

租户管理员登录后直接进入本租户管理后台：

- 顶部显示当前租户名称、状态、数字员工授权状态和有效期。
- 用户管理只显示本租户用户。
- 设备和会话只显示本租户设备。
- 数字员工页面显示本租户授权：启用状态、配额、已用、剩余、有效期。
- 如果授权过期或配额耗尽，页面给出明确状态，但不能越权修改配额或有效期。

## 13. 审计

所有审计必须包含：

- `tenant_id`
- `actor_scope`
- `actor_role`
- `actor_admin_id` 或 `actor_user_id`
- `action`
- `resource_type`
- `resource_id`
- `request_id`
- `client_ip`
- `created_at`

数字员工授权相关审计：

- `tenant.digital_employee_authorization.created`
- `tenant.digital_employee_authorization.updated`
- `tenant.digital_employee_authorization.expired`
- `tenant.digital_employee_authorization.quota_exhausted`
- `tenant.digital_employee_config.updated`

全局管理员修改租户授权时，审计的 `tenant_id` 是被修改的租户，`actor_scope` 是 `global`。

## 14. 迁移方案

升级步骤：

1. 创建默认租户 `tenant_default`。
2. 所有历史用户、设备、会话、邀请码、审批、策略补 `tenant_id = tenant_default`。
3. 历史管理员升级为 `global_owner`。
4. 创建默认租户的数字员工授权记录。
5. 原有全局数字员工授权迁移为默认租户授权。
6. 所有新 token 增加 `tenant_id`，旧 token 可在兼容期映射到默认租户。

配置开关：

```yaml
multi_tenant:
  enabled: false
  default_tenant_slug: default
  require_tenant_for_user_login: false
```

灰度顺序：

1. 先上线字段和默认租户，行为仍保持单租户。
2. 开启管理员 token 中的 `scope/role/tenant_id`。
3. 开启用户、机器、会话的租户过滤。
4. 开启租户管理员登录。
5. 开启全局租户管理 UI。
6. 开启数字员工授权按租户管理。

## 15. 测试计划

单元测试：

- 全局管理员创建租户和租户管理员。
- 租户管理员登录后 token 带正确 `tenant_id`。
- 同一邮箱在不同租户下注册、登录、审批互不影响。
- 租户管理员访问其它租户用户返回 403 或 404。
- 数字员工授权按租户读取、修改、过期判断。
- 租户 A 配额耗尽不影响租户 B。

接口测试：

- `GET /api/admin/users` 对租户管理员只返回本租户用户。
- `GET /api/ve/status` 对不同租户返回不同授权状态和有效期。
- `PUT /api/admin/tenants/{tenantId}/digital-employee-authorization` 只有全局管理员可调用。
- `PUT /api/admin/digital-employee/config` 租户管理员只能修改本租户配置，不能改授权配额和有效期。
- 暂停租户后，注册、登录和新建数字员工会话被拒绝。

端到端测试：

1. 全局管理员创建租户 A、B。
2. A、B 各创建租户管理员。
3. A、B 各自创建用户和设备。
4. 给 A 授权 10 个数字员工，有效期一年；给 B 授权 1 个数字员工，有效期一天。
5. A 管理员只能看到 A 的授权和用户。
6. B 授权过期后，B 新建数字员工会话失败，A 不受影响。

## 16. V1 实施切片

### 切片 1：租户底座

- 新增 `tenants`、`tenant_domains`。
- 新增默认租户迁移。
- `admin_users` 增加 `scope/role/tenant_id`。
- 管理员 token 增加租户上下文。

### 切片 2：用户和机器隔离

- `users`、`machines`、`sessions`、`viewer_tokens`、`login_tokens` 增加租户过滤。
- 改造注册、邮箱登录、机器鉴权。
- 补隔离测试。

### 切片 3：租户管理 API

- 全局管理员创建、查看、停用、恢复租户。
- 全局管理员创建租户管理员。
- 租户管理员登录。

### 切片 4：数字员工授权租户化

- 新增租户级数字员工授权表。
- 注册状态按租户返回授权、配额和有效期。
- 全局管理员按租户修改授权。
- 租户管理员只修改本租户数字员工运行配置。
- 数字员工能力校验接入 `tenant_id`。

### 切片 5：前端 UI

- 全局租户管理页。
- 创建租户弹窗。
- 租户详情里的数字员工授权面板。
- 租户管理员首页展示本租户授权状态和有效期。

### 切片 6：治理资源补齐

- 邀请码、审批、黑名单、邮件配置、组织策略、模型服务和 Skill/MCP 策略租户化。
- 审计、导出和故障日志补 `tenant_id`。

## 17. 验收标准

- Hub 全局管理员可以创建租户。
- Hub 全局管理员可以创建租户管理员。
- 租户管理员可以登录，只能管理本租户用户。
- 用户、机器、会话、邀请码、审批数据按租户隔离。
- 数字员工授权按租户展示，包含启用状态、配额、剩余量和有效期。
- 数字员工授权修改按租户进行，租户 A 的修改不影响租户 B。
- 老数据迁移到默认租户后可继续使用。
- 审计能回答：谁在什么时候修改了哪个租户的哪个资源。

## 18. 暂不纳入 V1

- Hub Center 平台级多租户统一目录。
- 租户自助注册和在线付费。
- 租户级独立数据库或物理隔离。
- 租户管理员 SSO/OIDC/SCIM。
- 跨租户用户合并和统一身份图谱。
## 19. 当前功能逐项租户化明细

这一节按当前 Hub/Hub Center 已有功能拆解，作为后续研发任务清单使用。

### 19.1 Hub：身份、登录、接入

| 模块 | 必改点 | 说明 |
| --- | --- | --- |
| 管理员初始化 | 初始化全局管理员 | 首次 setup 创建 `global_owner`，不创建租户管理员 |
| 管理员登录 | 支持全局登录和租户登录 | 请求可带 `tenant`；租户管理员 token 必须包含 `tenant_id` |
| Admin middleware | 输出统一 `AuthScope` | 后续所有 handler 从上下文取租户，不信任前端传参 |
| 用户邮箱登录 | 邮箱先解析租户 | 接入体验仍是填邮箱；后端用域名、精确路由、邀请码确定租户 |
| 桌面端 enroll | 创建租户内用户和机器 | `users/machines/viewer_tokens/login_tokens` 都带 `tenant_id` |
| SN 体系 | SN 租户内唯一 | 同一 SN 是否允许跨租户重复可配置，V1 建议租户内唯一即可 |
| pending login | 登录 token 带租户 | 防止同邮箱在多个租户下确认错账号 |

接入时“按邮箱即可”的含义是用户不用手动理解租户模型，但系统必须在后台把邮箱解析为租户。若邮箱域名匹配多个租户，返回候选项让用户选择，不能自动选第一个。

### 19.2 Hub：系统设置

| 设置项 | 租户化策略 |
| --- | --- |
| Hub Center 地址、公网 URL、TLS、数据库 | Hub 全局设置，不按租户拆分 |
| 工作模式/注册模式 `open/approval/manual` | 租户级设置，Hub 全局只提供默认值 |
| 可见性 `private/shared` | 租户级路由可见性；Hub 级可见性作为默认和上限 |
| 邮件 SMTP | V1 保持 Hub 全局 SMTP；租户可覆盖发件名称、模板、审批通知策略 |
| 管理员密码 | 按管理员账号处理；租户管理员只改自己的密码 |
| 模型文件下载 | Hub 全局运行时资源，不复制到租户 |
| OpenClaw IM bridge 安装路径 | Hub 全局资源；绑定关系和可用范围按租户 |

结论：系统设置里的“工作模式”必须按租户处理，否则同一个 Hub 内无法同时支持 A 租户开放注册、B 租户审批注册、C 租户仅手动绑定。

### 19.3 Hub：用户治理

| 功能 | 租户化策略 |
| --- | --- |
| 手动绑定 | 租户管理员只给本租户邮箱绑定 SN；全局管理员必须先选择租户 |
| 用户列表 | 默认只查当前租户；全局后台跨租户查询走单独 API |
| 用户删除 | 删除本租户用户，同时删除本租户机器、邀请码绑定、IM 绑定 |
| 黑名单 | 支持全局黑名单 + 租户黑名单；全局优先 |
| 邀请 | `email_invites` 带 `tenant_id`；邀请链接携带租户短码或租户签名 |
| 邀请码 | `invitation_codes` 带 `tenant_id`；邀请码可跨租户重复，但解析时必须明确租户 |
| PWA 审批 | 审批池按租户隔离；租户管理员只能审批本租户 |
| 失败日志 | 登录、注册、审批失败日志带 `tenant_id`，未解析租户时记录 `tenant_resolution_failed` |

### 19.4 Hub：机器、会话、远程控制

| 功能 | 租户化策略 |
| --- | --- |
| 机器列表 | 按机器所属用户的 `tenant_id` 过滤 |
| 机器重命名/删除 | 校验机器属于当前租户 |
| 清理离线机器 | 租户管理员只清理本租户；全局管理员可按租户或全局清理 |
| 会话列表 | `sessions` 增加 `tenant_id`，查询按租户过滤 |
| session detail/output | 通过 session 的 `tenant_id` 鉴权 |
| webhook session | token 或配置要绑定租户；创建会话时写 `tenant_id` |
| 文件/附件 | 元数据带 `tenant_id`；下载前校验会话和租户 |

### 19.5 Hub：数字员工和 A2A

| 功能 | 租户化策略 |
| --- | --- |
| 数字员工注册状态 | 按租户返回授权状态、配额、已用、剩余、有效期 |
| 数字员工授权修改 | 全局管理员按租户修改；租户管理员不能提升配额或延长期限 |
| 数字员工配置 | 租户管理员可改本租户运行配置，如审批、可见性、默认策略 |
| 数字员工发起 | 校验租户状态、授权有效期、配额剩余 |
| VE history/search/detail | 查询按 `tenant_id` 过滤 |
| A2A group profiles/sessions/invites | 当前已有 `tenant_id`，改为从统一 `AuthScope` 注入，避免 header 伪造 |
| VE 文件中继 | 文件归属会话租户，上传下载都校验租户 |

### 19.6 Hub：企业管理、安全策略、能力包

| 功能 | 租户化策略 |
| --- | --- |
| 企业组织树 | 每个租户一棵树，默认组也是租户级 |
| 组成员 | 用户邮箱在租户内解析，不能跨租户加入组 |
| 生效策略 | 按租户内全局、部门、用户三层继承 |
| 安全设置 | 租户级配置，Hub 全局保留最低安全底线 |
| Skill/MCP 能力包 | 策略、合规检查、导出快照都带 `tenant_id` |
| 能力外部搜索 | 搜索源可以全局，安装/下发策略必须租户级 |
| MCP marketplace | MCP 定义可全局复用，租户启用、密钥要求和可见性租户化 |
| 审计导出 | 文件内包含 `tenant_id/tenant_slug/exported_by` |

### 19.7 Hub：模型服务、用量、计费相关

| 功能 | 租户化策略 |
| --- | --- |
| LLM providers | Provider 可 Hub 全局保存；租户可启用子集或覆盖密钥策略 |
| LLM service groups | 服务组绑定租户；用户/部门绑定只能在租户内生效 |
| Service cards | 兑换卡需要绑定租户，兑换后只给本租户授权 |
| 用量统计 | 统计维度增加 `tenant_id`；租户管理员只能看本租户 |
| Prompt cache | 共享缓存可保留全局，但访问日志和计费归属必须带租户 |
| Billing/licenses | 若来自 Hub Center 或外部平台，落地为租户级 entitlement |

### 19.8 Hub：IM、插件、聊天与通知

| 功能 | 租户化策略 |
| --- | --- |
| 飞书/企微/钉钉/QQBot 配置 | 应区分连接配置和用户绑定；连接可全局或租户级，绑定必须租户级 |
| IM 用户绑定 | 绑定表增加 `tenant_id`，按租户解析邮箱 |
| OpenClaw IM webhook | webhook channel 绑定租户或携带租户签名 |
| Chat/channel/message | 群、消息、文件、已读、presence 都要带 `tenant_id` |
| 通知 | 按租户模板、租户管理员收件人发送 |

### 19.9 Hub：工作流、审批流、内容审核

| 功能 | 租户化策略 |
| --- | --- |
| workflow templates | 模板可全局发布，租户启用和版本选择租户化 |
| workflow instances | 实例带 `tenant_id`，审批人只能来自本租户 |
| admin review | 租户管理员只审核本租户提交；全局管理员可跨租户筛选 |
| audit logs | workflow audit 增加 `tenant_id` |
| content audit | 内容审核日志带租户；策略阈值可租户级覆盖 |

### 19.10 Hub：市场、公告、公共内容

| 功能 | 租户化策略 |
| --- | --- |
| capability market policy | 租户级采购/安装策略，Hub 全局只给默认策略 |
| acquisition requests | 请求带 `tenant_id`；租户管理员审批本租户请求 |
| managed deployments | 部署范围是租户、部门、用户 |
| recommendations | 推荐可全局，租户可隐藏或强制推荐 |
| gossip/news | 默认作为 Hub 全局公共内容；若企业内部使用，需要增加 `scope=global/tenant` |
| model download public status | Hub 全局公开资源，不按租户拆分 |

## 20. Hub Center 对应改造明细

Hub Center 不创建 Hub 内租户，也不管理租户用户。它只维护“邮箱/域名到 Hub + 租户入口”的路由索引，并让用户能够按邮箱找到正确入口。

### 20.1 Hub 注册与心跳

Hub 仍向 Hub Center 注册一次，保留 `hub_id/hub_secret/base_url/visibility`。多租户后，心跳或同步 payload 增加租户摘要：

```json
{
  "hub_id": "hub_123",
  "hub_secret": "***",
  "tenants": [
    {
      "tenant_id": "tenant_acme",
      "tenant_slug": "acme",
      "tenant_name": "Acme Corp",
      "status": "active",
      "domains": ["acme.com"],
      "visibility": "private",
      "enrollment_mode": "approval",
      "entry_path": "/app?tenant=acme",
      "updated_at": "2026-05-18T10:00:00Z"
    }
  ]
}
```

Hub Center 保存租户路由摘要，不保存租户授权详情、租户用户列表或租户策略正文。

### 20.2 路由查询

现有路由查询必须从“邮箱到 Hub”升级成“邮箱到 Hub + tenant”。返回结果包含：

- `hub_id`
- `hub_name`
- `base_url`
- `tenant_id`
- `tenant_slug`
- `tenant_name`
- `entry_url`
- `match_type`
- `enrollment_mode`
- `tenant_status`

查询优先级：

1. 精确邮箱路由：适合管理员手动迁移或用户已有绑定。
2. 租户域名路由：`@acme.com` 匹配 `tenant_acme`。
3. 明确租户参数：客户端传 `tenant=acme` 时只查对应租户。
4. Hub 默认路由：仅当该 Hub 只有一个 active 可接入租户时使用。
5. 多候选：返回候选列表，前端让用户选择。

### 20.3 Hub Center 数据表

建议新增：

```sql
CREATE TABLE hub_tenant_routes (
  hub_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  tenant_slug TEXT NOT NULL,
  tenant_name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  visibility TEXT NOT NULL DEFAULT 'private',
  enrollment_mode TEXT NOT NULL DEFAULT 'approval',
  entry_path TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (hub_id, tenant_id)
);

CREATE TABLE hub_tenant_domain_routes (
  domain TEXT NOT NULL,
  hub_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (domain, hub_id, tenant_id)
);
```

现有 `hub_user_links` 需要增加 `tenant_id`：

```sql
ALTER TABLE hub_user_links ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
```

如果不加 `tenant_id`，同一个 Hub 内多租户时，Hub Center 只能知道用户属于这个 Hub，不能生成正确的租户入口。

### 20.4 Hub Center 管理后台

| 页面 | 改造点 |
| --- | --- |
| 路由查询 | 展示匹配租户、租户状态、工作模式和最终入口 URL |
| Hubs 列表 | 增加租户数、活跃租户数、最近租户同步时间 |
| Hub 详情 | 增加租户路由摘要和域名列表，只读展示 |
| Policies | 邮箱/IP 全局封禁仍是 Hub Center 全局策略；可选支持按 Hub/tenant 限定 |
| Failure logs | 路由失败日志记录邮箱、候选租户数、失败原因 |
| Console | 输出同步到的租户路由数量 |

### 20.5 Hub Center API 建议

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/entry/resolve` | 邮箱入口解析，返回 Hub + tenant routes |
| `POST` | `/api/hubs/{hubId}/tenant-routes/sync` | Hub 同步租户路由摘要 |
| `GET` | `/api/admin/hubs/{hubId}/tenant-routes` | 管理员查看某 Hub 租户路由 |
| `GET` | `/api/admin/route-query?email=...` | 管理后台路由查询，展示多候选 |

### 20.6 Hub Center 不做的事

- 不创建 Hub 内租户。
- 不修改租户数字员工授权。
- 不保存租户用户完整列表。
- 不接管租户管理员登录。
- 不判断租户内组织策略、模型服务、Skill/MCP 策略。

这些仍然属于 Hub。
## 21. 多租户管理边界再定义

这里的管理模式要严格区分“系统级管理”和“租户内管理”。全局管理员是 Hub 系统管理员，不是所有租户的超级业务管理员。全局管理员负责把系统跑起来、创建租户、分配租户管理员、维护系统级默认值和安全底线；租户内的用户、能力市场、安全管理、组织策略、审批和日常运营由租户管理员负责。

### 21.1 全局管理员负责什么

全局管理员只负责系统级对象：

- Hub 初始化、升级、备份、恢复、健康检查。
- Hub Center 注册、公网 URL、TLS、数据库、模型文件等运行时配置。
- 创建、停用、恢复租户。
- 创建租户管理员，重置租户管理员账号状态。
- 设置 Hub 全局默认值和不可突破的安全底线。
- 发放或调整租户级数字员工授权额度、有效期等 entitlement。
- 查看跨租户汇总指标和系统级审计。

全局管理员不负责租户下具体业务管理：

- 不直接新增、删除、审批租户用户。
- 不直接管理租户的能力市场采购、安装、推荐、部署策略。
- 不直接管理租户的安全组织树、部门、成员、用户策略。
- 不直接处理租户内工作流审批。
- 不直接查看或修改租户内会话、聊天、文件等业务明细，除非后续设计单独的合规授权流程。

### 21.2 租户管理员负责什么

租户管理员登录后进入自己的租户后台，管理本租户业务对象：

- 本租户用户、设备、会话、邀请码、审批、黑名单。
- 本租户安全管理：组织树、部门、成员、安全策略、生效策略、导出审计。
- 本租户能力市场：采购策略、安装策略、托管部署、推荐、MCP 配置、合规检查。
- 本租户模型服务：服务组、用户/部门绑定、用量统计。
- 本租户数字员工运行配置：可见性、审批开关、默认策略、会话规则。
- 本租户 IM 绑定、工作流、内容审核和通知模板。

租户管理员看到的所有接口和列表都必须由后端用 token 中的 `tenant_id` 自动过滤。

### 21.3 能力市场按租户管理

能力市场在 Hub 多租户模式下属于租户内管理能力。处理规则：

- `capabilities` 可作为 Hub 全局能力目录缓存，避免同一个 Skill/MCP 重复导入。
- 租户是否启用、安装、推荐、禁止、托管部署某个能力，必须存到租户级策略表。
- acquisition request 必须带 `tenant_id`，由租户管理员审批。
- managed deployment 的 scope 必须是 `tenant/group/user`，且都在同一租户内。
- MCP endpoint 可全局缓存元数据，但密钥、启用状态、secret requirement、可见范围按租户保存。
- 能力合规检查、用户 inventory、effective policies、CSV/JSON 导出全部按租户过滤。
- 全局管理员只维护全局能力源、默认策略模板和安全底线，不进入某租户替它安装或审批能力。

建议补充表：

```sql
CREATE TABLE tenant_capability_policies (
  tenant_id TEXT NOT NULL,
  capability_id TEXT NOT NULL,
  policy TEXT NOT NULL, -- required | recommended | blocked | hidden
  required_version TEXT NOT NULL DEFAULT '',
  config_json TEXT NOT NULL DEFAULT '{}',
  updated_by_admin_id TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, capability_id)
);

CREATE TABLE tenant_capability_deployments (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  capability_id TEXT NOT NULL,
  scope_type TEXT NOT NULL, -- tenant | group | user
  scope_id TEXT NOT NULL,
  status TEXT NOT NULL,
  config_json TEXT NOT NULL DEFAULT '{}',
  created_by_admin_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

### 21.4 安全管理按租户管理

安全管理也属于租户内管理能力。处理规则：

- 每个租户有独立组织树、默认部门、用户成员关系。
- 安全策略继承链只在租户内计算：租户默认策略 -> 部门/组策略 -> 用户例外。
- 租户管理员只能查看和修改本租户安全设置。
- 全局管理员只配置 Hub 全局最低安全底线，例如禁止关闭审计、最大外发风险等级、全局封禁能力源等。
- 当租户策略低于全局底线时，生效策略以更严格者为准，并在 UI 标明来源为 `global_guardrail`。
- 安全审计、合规导出、对象快照全部带 `tenant_id`。

建议补充表字段或表：

```sql
CREATE TABLE tenant_security_settings (
  tenant_id TEXT PRIMARY KEY,
  settings_json TEXT NOT NULL DEFAULT '{}',
  updated_by_admin_id TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

已有安全组、成员、策略表需要增加 `tenant_id`，并把唯一键改为租户内唯一。

### 21.5 接口权限边界

现有 `/api/admin/*` 接口要拆权限，不再因为路径叫 admin 就默认全局管理员可操作全部业务。

| 接口类别 | global_owner | tenant_owner/operator |
| --- | --- | --- |
| `/api/admin/tenants*` | 可管理 | 不可访问 |
| `/api/admin/system*`、TLS、备份、Hub Center 注册 | 可管理 | 只读或不可访问 |
| `/api/admin/users*` | 只看汇总或不可访问明细 | 管理本租户 |
| `/api/admin/security*` | 只管理全局底线 | 管理本租户安全策略 |
| `/api/admin/capability-market*` | 只管理全局源和默认模板 | 管理本租户能力市场策略 |
| `/api/admin/llm/services*` | 只管理全局 provider/default | 管理本租户服务组和绑定 |
| `/api/ve/config`、`/api/ve/list`、`/api/ve/history*` | 只看授权汇总或系统诊断 | 管理本租户数字员工 |
| `/api/v1/admin/reviews*` | 只看异常汇总 | 审核本租户提交 |

实现上建议提供两个中间件：

```go
RequireGlobalAdmin(action)
RequireTenantAdmin(action)
```

`RequireTenantAdmin` 必须从 token 取 `tenant_id` 并注入上下文；`RequireGlobalAdmin` 只能访问系统级 action，不能绕过租户过滤直接调用业务 repository。
## 22. 数字员工及运行态资源的租户隔离

数字员工、A2A、多智能体协作、会话历史、文件中继、审批记录都必须按租户隔离。这里的隔离包括数据隔离、授权隔离、配置隔离、审计隔离和运行时查询隔离，不只是 UI 上按租户筛选。

### 22.1 数字员工隔离范围

数字员工相关对象全部归属租户：

- 数字员工授权额度、有效期、启用状态。
- 数字员工注册记录。
- 数字员工可见性、审批规则、默认策略。
- 数字员工列表。
- 数字员工发起、加入、邀请、拒绝、禁用等操作。
- 数字员工会话历史。
- 数字员工附件和文件中继。
- 数字员工搜索、详情、导出和审计。

租户 A 的数字员工不能被租户 B 发现、邀请、发起会话或查看历史。即使 machine_id、agent_id、display_name 相同，也必须通过 `tenant_id` 区分。

### 22.2 运行时校验规则

所有数字员工入口都必须从鉴权主体解析租户：

- 租户管理员 token -> `tenant_id`。
- 普通用户 viewer token -> 用户所属 `tenant_id`。
- 机器 token -> 机器所属用户的 `tenant_id`。
- A2A group session -> session 记录中的 `tenant_id`。

禁止从前端 header 或 query 直接信任 `tenant_id`。如果当前已有 `X-Tenant-ID`，后续只能作为兼容输入，必须与 token 解析出的租户一致，否则返回 403。

### 22.3 数据表和索引要求

数字员工相关表的主键或唯一键必须包含 `tenant_id`：

```sql
-- 示例：数字员工注册
CREATE UNIQUE INDEX idx_ve_profiles_tenant_agent
ON ve_profiles(tenant_id, agent_id);

-- 示例：数字员工会话
CREATE INDEX idx_ve_sessions_tenant_updated
ON ve_sessions(tenant_id, updated_at DESC);

-- 当前 a2a_group_profiles 已有 tenant_id，后续要统一由 AuthScope 注入
-- PRIMARY KEY (tenant_id, agent_id)
```

对外暴露 ID 时可以继续使用原 ID，但后端查询必须始终带 `tenant_id`：

```go
GetDigitalEmployee(ctx, tenantID, agentID)
ListDigitalEmployees(ctx, tenantID, filter)
CreateDigitalEmployeeSession(ctx, tenantID, req)
SearchDigitalEmployeeHistory(ctx, tenantID, query)
GetDigitalEmployeeAttachment(ctx, tenantID, sessionID, fileID)
```

### 22.4 配额和有效期隔离

数字员工授权按租户独立消耗：

- 租户 A 配额耗尽，不影响租户 B。
- 租户 A 授权过期，不影响租户 B。
- 租户 A 暂停后，只阻断 A 的数字员工能力。
- 全局管理员调整 A 的授权，只写 A 的授权记录和审计。

配额统计必须按 `tenant_id` 聚合，不能用 Hub 全局的数字员工总数判断某个租户是否超额。

### 22.5 UI 隔离

租户管理员登录后：

- 数字员工页面只显示本租户数字员工。
- 状态卡只显示本租户授权、配额、剩余量、有效期。
- 历史搜索只搜索本租户会话。
- 审批列表只显示本租户数字员工审批。
- 文件下载只允许下载本租户会话附件。

全局管理员页面只显示租户级授权汇总，例如每个租户的启用状态、配额、已用、有效期、风险状态；不进入租户内数字员工列表和会话明细管理。

### 22.6 验收补充

- 两个租户创建同名数字员工，互不覆盖。
- 租户 A 管理员无法通过 agent_id 查询租户 B 的数字员工。
- 租户 A 用户无法邀请租户 B 的数字员工加入会话。
- 租户 A 的 VE history/search/detail 不返回租户 B 数据。
- 租户 A 附件 URL 不能被租户 B 管理员或用户下载。
- A2A `X-Tenant-ID` 与 token 租户不一致时返回 403。
## 23. 现有全局数据迁移到租户

当前 Hub 的数据默认是全局作用域。多租户上线时必须先把历史全局数据收敛到一个明确租户中，后续所有新数据再按租户写入。迁移原则是：默认不拆散历史数据，先全部迁到默认租户，确保升级后行为与旧版本一致；如果企业已经存在明确部门或客户边界，再提供二次拆租户工具。

### 23.1 默认迁移策略

V1 采用“默认租户迁移”：

- 创建默认租户：`tenant_default` / `default` / `Default Tenant`。
- 所有历史用户、机器、会话、邀请码、审批、策略、能力市场策略、数字员工记录等补 `tenant_id = tenant_default`。
- 历史管理员升级为 `global_owner`。
- 可选创建一个默认租户管理员，用于继续管理旧数据。
- 原全局数字员工授权迁移为默认租户授权。
- 原全局工作模式迁移为默认租户工作模式，同时保留为 Hub 全局默认值。

这样升级后旧用户仍能登录、旧机器仍能连接、旧会话仍能查询，行为不发生突然变化。

### 23.2 迁移前备份和版本标记

迁移前必须做 SQLite 一致性备份，并写迁移版本：

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL,
  notes TEXT NOT NULL DEFAULT ''
);
```

迁移流程要求幂等：重复执行不会重复创建租户，不会覆盖已有 `tenant_id`，不会重复改管理员角色。

### 23.3 字段补齐顺序

推荐迁移顺序：

1. 新增 `tenants`、`tenant_domains`、`tenant_settings`。
2. 插入默认租户。
3. 给核心身份表增加 `tenant_id`：`users`、`machines`、`viewer_tokens`、`login_tokens`、`admin_users`。
4. 给运行态表增加 `tenant_id`：`sessions`、`audit_logs`、`failure_event_logs`、`voiceprints`、`content_audit_logs`。
5. 给治理表增加 `tenant_id`：`user_enrollments`、`email_blocklist`、`invitation_codes`、`email_invites`。
6. 给企业管理和能力市场表增加 `tenant_id` 或租户策略表。
7. 给数字员工、A2A、工作流、IM 绑定等表补 `tenant_id`。
8. 重建需要从全局唯一改为租户内唯一的索引。
9. 写入迁移版本。

### 23.4 核心 SQL 示例

```sql
INSERT OR IGNORE INTO tenants (
  id, slug, name, status, primary_domain, settings_json,
  created_by_admin_id, created_at, updated_at
) VALUES (
  'tenant_default', 'default', 'Default Tenant', 'active', '', '{}',
  'migration', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
);

ALTER TABLE users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE machines ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE sessions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE user_enrollments ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE invitation_codes ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE email_invites ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE email_blocklist ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE viewer_tokens ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE login_tokens ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE admin_audit_logs ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
```

SQLite 对已有唯一约束无法简单 drop 时，需要重建表。例如 `users.email UNIQUE` 要改成 `(tenant_id, email)`：

1. 创建 `users_new`。
2. 拷贝旧数据并填 `tenant_id`。
3. 删除旧表。
4. 重命名新表。
5. 创建新复合唯一索引。

### 23.5 管理员迁移

历史 `admin_users` 没有租户概念，迁移为系统全局管理员：

```sql
ALTER TABLE admin_users ADD COLUMN scope TEXT NOT NULL DEFAULT 'global';
ALTER TABLE admin_users ADD COLUMN role TEXT NOT NULL DEFAULT 'global_owner';
ALTER TABLE admin_users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
```

升级后建议引导 `global_owner` 创建默认租户管理员：

- `tenant_id = tenant_default`
- `role = tenant_owner`
- 可复用同邮箱，但用户名建议不同，例如 `tenant-admin`

如果为了兼容旧体验，也可以自动创建默认租户管理员，但要避免与全局管理员同名登录产生歧义。

### 23.6 数字员工授权迁移

原来的数字员工授权如果存在于系统设置或全局配置中，迁移到默认租户：

```sql
INSERT OR IGNORE INTO tenant_digital_employee_authorizations (
  tenant_id, enabled, quota, used, valid_from, valid_until,
  status, source, metadata_json, updated_by_admin_id, updated_at, created_at
) VALUES (
  'tenant_default', 0, 0, 0, NULL, NULL,
  'inactive', 'migration', '{}', 'migration', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
);
```

如果旧配置里已有 quota、enabled、expires_at，则转换规则：

- `enabled=true` 且未过期：`status=active`。
- `enabled=false`：`status=inactive`。
- 已过期：`status=expired`。
- `used >= quota`：`status=quota_exhausted`。

`used` 不建议凭空估算。如果旧系统没有准确用量，迁移时设为 0，并在第一次运行后通过数字员工注册记录或会话记录重算。

### 23.7 工作模式迁移

原 Hub 全局工作模式迁移为两份：

- `system_settings.default_enrollment_mode`：作为新租户默认值。
- `tenant_settings(tenant_default, enrollment_mode)`：保证旧用户接入行为不变。

示例：

```sql
INSERT OR REPLACE INTO tenant_settings (tenant_id, key, value_json, updated_by_admin_id, updated_at)
VALUES ('tenant_default', 'enrollment_mode', '{"value":"approval"}', 'migration', CURRENT_TIMESTAMP);
```

### 23.8 能力市场和安全管理迁移

能力市场：

- 全局能力目录 `capabilities` 可以保留为 Hub 全局缓存，不强制复制。
- 原来的安装、推荐、禁止、托管部署、采购请求等管理策略迁移到 `tenant_default`。
- 如果旧表没有策略作用域，统一视为默认租户策略。

安全管理：

- 原组织树、默认部门、成员、用户策略迁移到默认租户。
- 原安全设置迁移到 `tenant_security_settings(tenant_default)`。
- Hub 全局安全底线单独初始化，不直接等于旧租户策略；默认可从旧设置中提取最严格项。

### 23.9 Hub Center 路由迁移

Hub Center 侧现有路由只有 Hub 概念时：

- 为每个已注册 Hub 创建一条默认租户路由：`tenant_id=tenant_default`、`tenant_slug=default`。
- 原 `hub_user_links` 增加 `tenant_id`，历史记录填 `tenant_default`。
- 原域名路由如果只指向 Hub，也填到该 Hub 的默认租户。
- Hub 升级并开始上报真实租户路由后，Hub Center 用新路由覆盖默认摘要。

### 23.10 二次拆租户工具

如果客户希望把旧全局数据拆成多个租户，不建议在自动迁移中完成，应提供显式工具：

```powershell
go run .\cmd\hub tenant migrate-users --from tenant_default --to tenant_acme --email-domain acme.com --dry-run
```

二次拆分需要一起迁移：

- users
- machines
- sessions
- viewer/login tokens
- invitation codes and enrollments
- voiceprints
- digital employee registrations and sessions
- security group membership
- model service grants
- workflow instances
- IM bindings

拆分前必须 dry-run 输出影响数量和冲突项，例如邮箱重复、SN 重复、机器 client_id 冲突、未找到目标租户等。

### 23.11 兼容期策略

兼容期内：

- 旧 token 没有 `tenant_id` 时，服务端按用户/机器反查；仍查不到则落到 `tenant_default`。
- 旧 API 未传租户时，管理员 token 如果是租户管理员则使用 token 租户；如果是全局管理员则只允许系统级接口。
- 旧 Hub Center 路由返回 Hub 入口时，Hub 端如果只有默认租户，则自动进入默认租户。

兼容期结束后，所有 token、用户、机器和会话都必须有明确 `tenant_id`。

### 23.12 迁移验收

- 旧管理员能以 `global_owner` 登录。
- 默认租户存在，且旧用户数量等于迁移前用户数量。
- 旧机器能继续心跳并反查到 `tenant_default`。
- 旧会话历史仍可在默认租户管理员下查看。
- 原工作模式在默认租户下保持不变。
- 原数字员工授权能在默认租户注册状态中显示。
- Hub Center 对旧邮箱路由能返回默认租户入口。
- 所有新建数据都写入明确租户，不再产生空 `tenant_id`。
## 24. 多租户设计仍需补齐的关键点

前面的章节已经覆盖租户、用户、能力市场、安全管理、数字员工和 Hub Center 路由。真正落地时还需要补齐下面这些横切问题，否则容易出现“主流程隔离了，边角功能串租户”的风险。

### 24.1 租户解析必须统一

不能让每个 handler 自己解析租户。需要一个统一的 `TenantResolver`：

```go
type TenantResolver interface {
  ResolveForAdmin(ctx context.Context, token string, requestedTenant string) (AuthScope, error)
  ResolveForViewer(ctx context.Context, token string) (AuthScope, error)
  ResolveForMachine(ctx context.Context, machineID, token string) (AuthScope, error)
  ResolveForEmail(ctx context.Context, email, tenantHint, inviteCode string) (TenantResolution, error)
}
```

统一解析规则：

- 管理员请求以 admin token 为准。
- 普通用户请求以 viewer token 为准。
- 机器请求以 machine token 为准。
- 邮箱接入阶段才允许使用邮箱、域名、邀请码、Hub Center 路由解析租户。
- 一旦登录成功，后续不能再靠前端传 `tenant_id` 决定权限。

### 24.2 权限矩阵要显式配置

多租户后不能只靠 `RequireAdmin`。需要把接口权限做成矩阵：

| 权限动作 | global_owner | tenant_owner | tenant_operator | tenant_viewer |
| --- | --- | --- | --- | --- |
| `tenant.create` | allow | deny | deny | deny |
| `tenant.admin.create` | allow | allow-self-tenant | deny | deny |
| `user.manage` | deny | allow-own-tenant | allow-own-tenant | read-own-tenant |
| `security.manage` | global-guardrail-only | allow-own-tenant | limited | read-own-tenant |
| `capability.manage` | global-template-only | allow-own-tenant | limited | read-own-tenant |
| `ve.authorization.grant` | allow | deny | deny | deny |
| `ve.config.manage` | deny | allow-own-tenant | limited | read-own-tenant |
| `workflow.review` | summary-only | allow-own-tenant | allow-own-tenant | read-own-tenant |

`global_owner` 对租户业务接口默认不是 allow，而是 deny 或 summary-only。这样才能落实“系统全局管理员只负责系统级管理，不负责租户下具体管理”。

### 24.3 缓存 Key 必须带租户

所有缓存、内存索引和本地快照都要检查 key 是否包含 `tenant_id`：

- 用户缓存：`tenant_id + email`。
- 机器缓存：`tenant_id + machine_id` 或 machine_id 全局唯一后反查租户。
- LLM service entitlement cache：`tenant_id + user_id/service_id`。
- 安全策略 effective policy cache：`tenant_id + user/group`。
- 能力合规快照：`tenant_id + snapshot_id`。
- 数字员工 discoverable cache：`tenant_id + agent_id`。
- Hub Center route snapshot：`hub_id + tenant_id + domain/email`。

如果缓存 key 只用 email、user_id、group_id、agent_id，后续很容易串租户。

### 24.4 后台任务和定时任务要有租户作用域

后台任务不能默认扫全库后直接执行租户业务动作。需要每个任务声明作用域：

- `system`：系统级任务，例如清理全局临时文件、模型下载状态检查。
- `tenant`：逐租户执行，例如数字员工授权过期扫描、租户策略合规快照、邀请过期、审批超时。
- `tenant-user`：按租户用户执行，例如用量结算、服务授权到期。

任务记录建议包含：

```sql
CREATE TABLE background_jobs (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL, -- system | tenant | tenant_user
  tenant_id TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  job_type TEXT NOT NULL,
  status TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

### 24.5 外部回调和 Webhook 要绑定租户

所有外部入口都要有租户绑定，不能只靠 URL token：

- OpenClaw IM webhook。
- 飞书/企微/钉钉/QQBot 回调。
- workflow webhook trigger。
- A2A 外部桥接。
- 邮件确认链接。
- Hub Center route resolve callback。

回调 token、签名 secret 或 channel 配置必须带 `tenant_id`。收到回调后先解析租户，再处理用户、机器、会话或审批。

### 24.6 Secret 和凭证要按租户隔离

租户级密钥不能混在全局设置里：

- MCP API Key。
- 租户覆盖的 LLM provider key。
- IM 应用 secret 或 webhook secret。
- 邮件模板中的租户发件身份。
- 外部系统 webhook token。

建议新增统一 secret store：

```sql
CREATE TABLE tenant_secrets (
  tenant_id TEXT NOT NULL,
  secret_key TEXT NOT NULL,
  encrypted_value BLOB NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  updated_by_admin_id TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, secret_key)
);
```

Hub 全局管理员可以管理系统级 secret；租户管理员只能管理自己租户 secret。审计日志不能记录明文。

### 24.7 ID 策略要统一

可以有两种策略：

- 全局唯一 ID：`user_id/machine_id/session_id` 全局唯一，查询时仍校验对象归属租户。
- 租户内唯一 ID：业务自然键如 email、SN、邀请码、agent_id、group_id 在租户内唯一。

建议：

- 技术主键全局唯一，便于日志和排障。
- 业务唯一键租户内唯一，便于客户迁移和复用。
- 所有通过 ID 查询的接口仍必须带租户过滤，不能因为 ID 全局唯一就跳过权限校验。

### 24.8 导入导出要带租户元数据

所有导出文件必须包含：

- `tenant_id`
- `tenant_slug`
- `tenant_name`
- `exported_by`
- `actor_scope`
- `exported_at`
- `filter`
- `checksum`

所有导入文件必须校验：

- 文件声明的租户是否等于当前租户。
- 如果导入到不同租户，必须走显式 remap 流程。
- 导入前 dry-run，输出冲突项。

能力策略、安全策略、数字员工配置、工作流模板、用户清单、审计快照都适用这条规则。

### 24.9 备份恢复要定义租户粒度

V1 可以只支持 Hub 整体备份恢复，但设计上要预留租户级导出：

- 整体备份：系统管理员使用，包含所有租户。
- 租户导出：租户管理员或系统管理员发起，只包含单租户业务数据。
- 租户恢复：只能恢复到同租户或新租户，必须 remap 技术 ID 和外部 secret。

租户级恢复特别要处理：

- 用户和机器 ID 冲突。
- 邀请码、SN、邮箱冲突。
- 数字员工授权不能从导入文件直接提升，需要重新发放 entitlement。
- 外部密钥默认不导出明文。

### 24.10 限流和配额要按租户维度

多租户后限流维度至少包括：

- 登录失败：`tenant_id + username/email + ip`。
- 邮件发送：`tenant_id + email`。
- 邀请码生成：`tenant_id + admin_id`。
- 数字员工会话：`tenant_id + user_id`。
- LLM 请求：`tenant_id + service_id + user_id`。
- 能力市场导入/安装：`tenant_id + admin_id`。

不能只做 Hub 全局限流，否则一个租户的异常会影响其它租户；也不能只做用户限流，否则租户级成本不可控。

### 24.11 观测指标要有租户标签

日志、metrics、trace 都要带租户标签，但要避免泄露敏感信息：

- `tenant_id`
- `tenant_slug`
- `actor_scope`
- `role`
- `request_id`
- `operation`
- `resource_type`

跨租户汇总指标可以给全局管理员看；租户明细指标只给租户管理员看。

### 24.12 前端路由和状态管理要防串租户

前端也要防止状态残留：

- 登录后保存当前 `tenant_id/tenant_slug/tenant_name`。
- 切换账号或退出时清空本地缓存。
- 租户管理员没有租户切换器。
- 全局管理员只有租户总览，不进入租户业务管理页。
- 所有 API 错误如果返回 `TENANT_MISMATCH`，前端应强制刷新登录态。
- 本地导出登记簿、快照 registry、最近筛选条件都要带租户维度。

### 24.13 灰度和回滚

迁移到多租户要支持灰度：

1. 字段和默认租户上线，但逻辑仍单租户。
2. token 增加租户声明。
3. 核心查询双写/双读校验。
4. 租户管理员入口灰度开放。
5. 能力市场、安全管理、数字员工逐项切换租户过滤。
6. Hub Center 路由切换到 Hub + tenant。

回滚要求：

- 数据库迁移前有备份。
- 默认租户模式下可临时关闭多租户登录入口。
- 新增租户数据不能回滚到旧版本继续使用，除非明确执行导出归档。

### 24.14 错误码要标准化

建议新增统一错误码：

| 错误码 | 含义 |
| --- | --- |
| `TENANT_REQUIRED` | 邮箱匹配多个租户，需要选择租户 |
| `TENANT_NOT_FOUND` | 租户不存在 |
| `TENANT_DISABLED` | 租户不可用 |
| `TENANT_SUSPENDED` | 租户暂停 |
| `TENANT_MISMATCH` | 请求租户与 token 租户不一致 |
| `TENANT_FORBIDDEN` | 当前角色无权访问该租户资源 |
| `GLOBAL_ADMIN_BUSINESS_DENIED` | 全局管理员不能直接管理租户业务对象 |
| `TENANT_QUOTA_EXCEEDED` | 租户配额超限 |
| `TENANT_ROUTE_AMBIGUOUS` | Hub Center 或本地路由匹配多个租户 |

### 24.15 必补测试类型

除功能测试外，还需要专门做“反串租户测试”：

- 把租户 A 的 token 配租户 B 的 path/query/header，必须失败。
- 用租户 A 的管理员访问租户 B 的 user_id、machine_id、session_id、agent_id，必须失败。
- 缓存预热后再访问另一个租户，不能返回旧租户缓存。
- 后台任务只处理目标租户数据。
- Hub Center 多候选路由不会自动选错租户。
- 导入租户 A 的策略到租户 B 时必须要求 remap 确认。
## 25. Hub LLM 服务多租户设计

Hub 上的 LLM 服务必须按租户隔离。这里的 LLM 服务包括 provider 配置、模型服务组、兑换卡、用户/部门绑定、默认服务、用量统计、并发限制、prompt cache 归属、访问日志和诊断接口。

### 25.1 分层原则

LLM 服务分三层：

| 层级 | 说明 | 管理者 |
| --- | --- | --- |
| Hub 全局 Provider 目录 | 系统级可用 provider 类型、公共 endpoint 模板、模型下载状态、全局最低安全底线 | 全局管理员 |
| 租户 LLM 服务组 | 租户启用哪些 provider、服务组、默认模型、额度、并发、有效期、部门/用户绑定 | 租户管理员 |
| 用户实际授权 | 用户最终可用模型、默认模型、额度、过期状态、用量 | 服务端按租户策略计算 |

全局管理员不直接给某个租户用户绑定模型服务。租户管理员登录后，只管理自己租户内的 LLM 服务组和绑定。

### 25.2 数据模型

建议新增租户级 LLM 表，或在现有系统设置 JSON 之外拆成结构化表：

```sql
CREATE TABLE tenant_llm_providers (
  tenant_id TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  global_provider_ref TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL,
  endpoint_url TEXT NOT NULL,
  auth_secret_ref TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  models_json TEXT NOT NULL DEFAULT '[]',
  limits_json TEXT NOT NULL DEFAULT '{}',
  created_by_admin_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, provider_id)
);

CREATE TABLE tenant_llm_service_groups (
  tenant_id TEXT NOT NULL,
  service_id TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  default_model TEXT NOT NULL DEFAULT '',
  provider_ids_json TEXT NOT NULL DEFAULT '[]',
  quota_json TEXT NOT NULL DEFAULT '{}',
  concurrency_json TEXT NOT NULL DEFAULT '{}',
  valid_from TEXT,
  valid_until TEXT,
  created_by_admin_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, service_id)
);

CREATE TABLE tenant_llm_service_bindings (
  tenant_id TEXT NOT NULL,
  binding_id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL,
  scope_type TEXT NOT NULL, -- tenant | group | user
  scope_id TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  created_by_admin_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

如果继续使用 `system_settings` 保存 LLM JSON，也必须把 key 改成租户级，例如：

- `tenant:{tenant_id}:llm_providers`
- `tenant:{tenant_id}:llm_services`
- `tenant:{tenant_id}:llm_default_service`

不允许所有租户共用同一个 `hub_llm_config` 作为业务授权配置。

### 25.3 Provider 与密钥隔离

Provider 可以有全局模板，但密钥和启用状态必须租户隔离：

- 全局模板：OpenAI-compatible、Anthropic-compatible、本地模型等 provider 类型和默认字段。
- 租户 provider：具体 endpoint、模型白名单、密钥引用、状态、限流。
- 租户密钥：保存到 `tenant_secrets`，例如 `llm.provider.openai.api_key`。

全局管理员可以维护 provider 类型模板和全局禁用列表；租户管理员配置本租户的 endpoint 和密钥。若企业要求统一密钥，也可以由全局管理员创建“系统托管 provider”，租户只引用，不可查看密钥明文。

### 25.4 模型服务组按租户生效

模型服务组绑定规则必须在租户内计算：

1. 用户直接绑定的服务组。
2. 用户所在部门/安全组绑定的服务组。
3. 租户默认服务组。
4. Hub 全局默认模板仅用于新租户初始化，不作为运行时跨租户授权。

返回给客户端的有效授权必须包含租户：

```json
{
  "tenant_id": "tenant_acme",
  "user_id": "usr_123",
  "service_id": "svc_research",
  "source": "group",
  "default_model": "gpt-4.1-mini",
  "models": ["gpt-4.1-mini", "gpt-4.1"],
  "quota": { "monthly_tokens": 1000000, "used_tokens": 12000 },
  "valid_until": "2027-05-01T00:00:00Z"
}
```

### 25.5 LLM 访问路径隔离

所有 LLM 代理、统一 OpenAI v1 endpoint、测试连接、模型列表、用量统计都要绑定租户：

- 普通用户调用 LLM：从 viewer token 解析 `tenant_id`。
- 桌面机器调用 LLM：从 machine token 解析 `tenant_id`。
- 租户管理员测试 provider：从 admin token 解析 `tenant_id`。
- 服务账号调用：服务账号必须绑定 `tenant_id`。

即使请求 body 里传了 `service_id/model/provider_id`，服务端也只能在当前租户范围内查找。

### 25.6 用量、并发和限流

LLM 用量统计必须至少有这些维度：

- `tenant_id`
- `service_id`
- `provider_id`
- `model`
- `user_id`
- `machine_id`
- `request_id`
- `input_tokens`
- `output_tokens`
- `cached_tokens`
- `cost_estimate`
- `created_at`

并发限制分层：

- Hub 全局硬上限：保护进程和机器资源。
- 租户上限：防止单租户打满 Hub。
- 服务组上限：区分不同服务套餐。
- 用户上限：防止单用户滥用。

限流 key 示例：

```text
tenant:{tenant_id}:llm
tenant:{tenant_id}:service:{service_id}:llm
 tenant:{tenant_id}:user:{user_id}:llm
```

### 25.7 Prompt Cache 多租户处理

Prompt cache 可以共享物理存储，但归属和可见性必须租户化：

- cache key 应包含 `tenant_id`，避免租户间命中包含敏感上下文的缓存。
- 如果要跨租户共享纯模型元数据缓存，必须单独标记 `cache_scope=global_metadata`，且不能包含用户 prompt。
- prompt cache entries 管理接口租户管理员只能查看和清理本租户缓存。
- 全局管理员只能清理全局 metadata cache 或按系统维护动作清理全部缓存，不查看租户 prompt 内容。

### 25.8 Service Cards 与兑换

服务兑换卡也要租户化：

- 全局管理员可以创建租户授权额度包或系统级兑换批次。
- 租户管理员可以在本租户内发放用户服务卡。
- 兑换记录必须带 `tenant_id`。
- 兑换后只增加本租户服务组或用户授权。
- 同一兑换码是否跨租户唯一由系统策略决定；V1 建议全局唯一，降低误兑风险。

### 25.9 API 改造

现有 LLM 管理接口保留路径，但权限和数据按租户：

| 接口 | 多租户语义 |
| --- | --- |
| `GET /api/admin/llm/providers` | 租户管理员返回本租户 provider；全局管理员返回全局模板或系统 provider 目录 |
| `PUT /api/admin/llm/providers` | 租户管理员修改本租户 provider；全局管理员修改系统模板 |
| `POST /api/admin/llm/providers/test` | 测试当前租户 provider，不允许测试其它租户密钥 |
| `GET /api/admin/llm/services` | 返回本租户服务组 |
| `PUT /api/admin/llm/services` | 修改本租户服务组、默认服务、绑定 |
| `POST /api/admin/llm/service-cards` | 租户内发卡或全局授权批次，按角色分流 |
| `GET /api/admin/llm/services/diagnose` | 诊断当前租户授权链路 |
| `GET /api/llm/v1/models` | 根据当前用户/机器租户返回可用模型 |
| OpenAI-compatible proxy | 根据 token 租户和服务组转发，不接受跨租户 provider_id |

### 25.10 Hub Center 关系

Hub Center 不管理 Hub 内 LLM 服务组。它最多记录租户是否有可用 LLM 服务的摘要，用于路由或运维展示：

- `tenant_id`
- `llm_enabled`
- `default_service_status`
- `last_llm_status_at`

LLM provider 密钥、模型服务组、用户绑定、用量明细都留在 Hub 内按租户管理。

### 25.11 迁移策略

旧 Hub 的 LLM 配置迁移到默认租户：

- 原 `hub_llm_config` -> `tenant_default` 的 provider/service 配置。
- 原默认模型服务组 -> `tenant_default` 默认服务组。
- 原用户授权 -> `tenant_default` 用户绑定。
- 原用量统计如果没有租户字段 -> 补 `tenant_default`。
- 原 prompt cache 如果包含用户 prompt -> 补 `tenant_default`；纯模型 metadata 可标为 global metadata。

### 25.12 验收标准

- 租户 A 和 B 可以配置不同 LLM provider、模型列表、默认服务组。
- 租户 A 管理员看不到租户 B 的 provider、服务组、密钥、用量。
- 租户 A 用户调用 `/api/llm/v1/models` 只返回 A 授权模型。
- 租户 A 的 prompt cache 不命中租户 B 的用户 prompt cache。
- 租户 A 配额耗尽不影响租户 B。
- 全局管理员不能直接修改租户内某用户的模型服务绑定，只能管理系统模板或租户授权边界。
## 26. 旧客户端兼容与邮箱自动确认租户

多租户上线后，旧版 MaClaw 客户端可能不会传 `tenant_id`、`tenant_slug` 或租户选择参数。为了避免旧客户端无法注册、登录或连接 Hub，需要提供兼容期策略：旧客户端仍然只提交邮箱，服务端通过邮箱自动确认租户；只有无法唯一确认时，才要求升级客户端或走人工绑定。

### 26.1 兼容目标

- 旧客户端只传邮箱，也能在单租户或明确邮箱域名场景下继续接入。
- 旧客户端不需要理解租户概念，但服务端必须给它绑定明确 `tenant_id`。
- 一旦服务端确认租户，返回的 token、用户、机器、会话都写入该租户。
- 不能为了兼容而把无法确认租户的请求落到错误租户。

### 26.2 邮箱自动确认租户规则

旧客户端接入时，服务端按以下顺序解析租户：

1. 邀请码或激活码绑定的 `tenant_id`。
2. 精确邮箱路由：Hub 本地或 Hub Center 中已有 `email -> tenant_id` 绑定。
3. 租户邮箱域名：邮箱后缀唯一匹配某个 active 租户。
4. Hub 只有一个 active 租户：自动使用该租户。
5. 默认租户兼容：多租户开关未完全开启或处于迁移兼容期时，落到 `tenant_default`。
6. 多个候选或无候选：返回 `TENANT_REQUIRED` 或 `TENANT_ROUTE_AMBIGUOUS`。

关键点：只有“唯一匹配”才能自动确认租户。不能在多个租户都可能匹配时静默选择默认租户。

### 26.3 邮件确认链路

旧客户端发起邮箱登录：

1. 客户端提交邮箱。
2. Hub 解析租户。
3. Hub 创建带 `tenant_id` 的 login token。
4. 邮件确认链接带租户签名参数。
5. 用户点击邮件后，Hub 校验 token、邮箱和租户一致。
6. Hub 返回 viewer token，token 内包含 `tenant_id`。

邮件链接示例：

```text
https://hub.example.com/app/confirm?token=xxx&email=alice%40acme.com&tenant=acme&sig=...
```

`tenant` 用于前端展示和路由，真正安全校验以服务端 login token 中的 `tenant_id` 为准。

### 26.4 桌面端 enroll 兼容

旧桌面端 `POST /api/enroll/start` 如果只传邮箱：

- Hub 自动解析租户。
- 如果成功，响应里新增租户字段；旧客户端忽略新增字段也不影响。
- 新客户端可以保存这些字段，用于展示和后续诊断。

响应兼容示例：

```json
{
  "email": "alice@acme.com",
  "sn": "SN-001",
  "user_id": "usr_123",
  "machine_id": "mac_123",
  "machine_token": "...",
  "tenant_id": "tenant_acme",
  "tenant_slug": "acme",
  "tenant_name": "Acme Corp"
}
```

旧客户端不传租户，但后续机器请求携带 `machine_id + machine_token`，服务端可通过机器记录反查租户。

### 26.5 旧 token 兼容

旧 token 没有 `tenant_id` 时：

- viewer token：通过 `user_id` 查用户所属租户。
- machine token：通过 `machine_id` 查机器，再查用户所属租户。
- admin token：历史管理员按 `global_owner` 处理；不允许直接访问租户业务接口。
- 查不到归属时，兼容期可落到 `tenant_default`，并写告警日志。

兼容期结束后，没有租户归属的 token 应返回 `TENANT_REQUIRED`，要求重新登录或升级客户端。

### 26.6 自动确认失败时的用户体验

旧客户端无法展示租户选择器时，服务端应返回可理解的错误：

```json
{
  "error": "TENANT_REQUIRED",
  "message": "This email matches multiple tenants. Please upgrade MaClaw or ask your administrator for an invitation link.",
  "candidates": [
    { "tenant_slug": "acme", "tenant_name": "Acme Corp" },
    { "tenant_slug": "acme-lab", "tenant_name": "Acme Lab" }
  ],
  "upgrade_required": true
}
```

旧客户端可以展示错误文本；新客户端展示租户选择器。

### 26.7 Hub Center 路由兼容

旧客户端如果先问 Hub Center 路由，Hub Center 也按邮箱自动确认租户：

- 单一匹配：返回带 `tenant` 参数的入口 URL。
- 多匹配：返回候选列表。
- 旧客户端不支持候选列表时，Hub Center 返回 `TENANT_ROUTE_AMBIGUOUS` 和升级提示。

Hub Center 不应在多候选时只返回第一个 Hub 或第一个租户。

### 26.8 管理后台兼容开关

建议提供兼容期配置：

```yaml
multi_tenant:
  legacy_client_compat:
    enabled: true
    allow_default_tenant_fallback: true
    default_tenant_fallback_until: "2026-12-31T23:59:59Z"
    require_invite_when_ambiguous: true
```

含义：

- `enabled`：允许旧客户端只按邮箱接入。
- `allow_default_tenant_fallback`：无明确租户但处于默认租户兼容期时允许进入默认租户。
- `default_tenant_fallback_until`：默认租户兜底截止时间。
- `require_invite_when_ambiguous`：邮箱多候选时必须使用带租户的邀请码或升级客户端选择租户。

### 26.9 审计与告警

旧客户端兼容路径要写审计和告警：

- `tenant.auto_resolved_by_invite`
- `tenant.auto_resolved_by_email_route`
- `tenant.auto_resolved_by_domain`
- `tenant.auto_resolved_by_single_active_tenant`
- `tenant.fallback_to_default_for_legacy_client`
- `tenant.route_ambiguous_for_legacy_client`

这样后续可以统计还有多少旧客户端依赖默认租户兜底，并决定何时关闭兼容。

### 26.10 验收标准

- 旧客户端只传邮箱，在唯一域名匹配时能注册并拿到机器 token。
- 旧客户端只传邮箱，在 Hub 只有一个 active 租户时能接入。
- 同一邮箱域名匹配多个租户时，服务端不自动选租户，返回明确错误。
- 邮件确认 token 绑定租户，不能用 A 租户邮件链接确认 B 租户登录。
- 旧 machine token 请求能通过 machine 记录反查租户。
- 兼容期默认租户兜底会写审计，便于后续清理。
## 27. 邀请码、服务卡与兑换类资源租户化

邀请码、服务卡、兑换码、授权卡、试用资格等“可转移凭证”是多租户设计里最容易串租户的部分。它们必须按租户发放、按租户兑换、按租户审计，不能只是全局生成一批码。

### 27.1 凭证分类

| 类型 | 用途 | 作用域 |
| --- | --- | --- |
| 邀请码 `invitation_code` | 用户注册、桌面端 enroll、PWA 接入 | 租户级 |
| 邮箱邀请 `email_invite` | 邀请指定邮箱加入租户 | 租户级 |
| 服务卡 `service_card` | 发放 LLM 服务组、额度、有效期 | 租户级 |
| 数字员工授权卡 `ve_authorization_card` | 发放或追加租户数字员工额度 | 系统发给租户，兑换后落租户 |
| 能力市场兑换码 `capability_card` | 发放能力包、托管部署或采购额度 | 租户级 |
| 试用资格 `trial_grant` | 新租户或新用户试用 | 租户级，受系统级上限约束 |

### 27.2 邀请码按租户发放

邀请码必须包含 `tenant_id`：

```sql
ALTER TABLE invitation_codes ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default';
ALTER TABLE invitation_codes ADD COLUMN created_by_admin_id TEXT NOT NULL DEFAULT '';
ALTER TABLE invitation_codes ADD COLUMN expires_at TEXT;
ALTER TABLE invitation_codes ADD COLUMN max_uses INTEGER NOT NULL DEFAULT 1;
ALTER TABLE invitation_codes ADD COLUMN used_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE invitation_codes ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '{}';
```

唯一性建议：

- V1 为了避免用户输错租户仍能兑换，邀请码 `code` 建议全局唯一。
- 如果未来允许跨租户重复码，兑换时必须同时有 `tenant_id` 或邀请链接签名。

邀请码发放规则：

- 租户管理员只能发放本租户邀请码。
- 全局管理员不直接给租户用户发邀请码；如需系统批量初始化，只创建租户管理员或租户授权边界。
- 邀请码链接必须带租户短码或签名，例如 `/app/invite?tenant=acme&code=xxx&sig=...`。
- 兑换时校验 code、tenant_id、邮箱限制、有效期、使用次数、租户状态。

### 27.3 邮箱邀请按租户发放

`email_invites` 必须按 `tenant_id + email` 管理：

```sql
CREATE UNIQUE INDEX idx_email_invites_tenant_email_status
ON email_invites(tenant_id, email, status);
```

处理规则：

- 同一邮箱可被不同租户邀请，但登录时必须明确选择或通过邀请链接确认租户。
- 邮箱邀请生成的邮件确认链接绑定 `tenant_id`。
- 租户管理员只能查看、撤销、重发本租户邀请。
- Hub Center 精确邮箱路由可以由已接受的邮箱邀请生成，但必须带 `tenant_id`。

### 27.4 服务卡按租户发放

服务卡用于给租户内用户、部门或整个租户发放 LLM 服务能力。必须带租户：

```sql
CREATE TABLE tenant_service_cards (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  code_hash TEXT NOT NULL UNIQUE,
  service_id TEXT NOT NULL,
  grant_scope TEXT NOT NULL, -- tenant | group | user
  grant_target TEXT NOT NULL DEFAULT '',
  quota_json TEXT NOT NULL DEFAULT '{}',
  valid_days INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'unused',
  created_by_admin_id TEXT NOT NULL,
  used_by_user_id TEXT NOT NULL DEFAULT '',
  used_at TEXT,
  expires_at TEXT,
  created_at TEXT NOT NULL
);
```

服务卡规则：

- 租户管理员发的服务卡只能兑换到本租户。
- 服务卡兑换时，兑换人必须属于该租户。
- 服务卡不能把用户绑定到其它租户的 LLM 服务组。
- 租户 A 的服务卡不能在租户 B 兑换。
- 全局管理员如果发放“租户授权额度包”，兑换对象是租户，不是租户内用户。

### 27.5 数字员工授权卡

数字员工授权卡如果存在，应分成两类：

- 系统级租户授权卡：全局管理员发给某个租户，用于增加该租户数字员工额度或延长有效期。
- 租户内使用卡：租户管理员发给本租户用户或部门，用于申请或启用数字员工运行权限。

系统级授权卡兑换后只能修改 `tenant_digital_employee_authorizations` 的目标租户记录。租户内使用卡不能提升租户总额度，只能在租户额度内分配使用权。

### 27.6 能力市场兑换码

能力市场相关兑换也必须租户化：

- 采购额度归属租户。
- 能力包兑换归属租户。
- MCP 托管部署兑换归属租户。
- 兑换后生成 `tenant_capability_policies` 或 `tenant_capability_deployments`。
- 租户管理员审批和查看本租户兑换记录。

全局能力市场可以保存公共商品目录，但兑换结果必须落到租户。

### 27.7 兑换审计

所有凭证兑换必须记录：

- `tenant_id`
- `credential_type`
- `credential_id`
- `code_hash`
- `issued_by_admin_id`
- `redeemed_by_user_id/admin_id`
- `redeemed_email`
- `redeemed_ip`
- `result`
- `failure_reason`
- `created_at`

失败也要记录，特别是跨租户兑换尝试：

- `credential.tenant_mismatch`
- `credential.expired`
- `credential.already_used`
- `credential.email_not_allowed`
- `credential.tenant_suspended`

### 27.8 迁移规则

旧邀请码、旧服务卡没有租户字段时：

- 全部迁移到 `tenant_default`。
- 如果旧服务卡本质是 Hub 全局模型服务兑换卡，迁移为默认租户服务卡。
- 如果旧数字员工授权卡本质是 Hub 级授权，迁移为默认租户数字员工授权额度。
- 迁移后新增兑换必须写明确 `tenant_id`。

### 27.9 验收标准

- 租户 A 邀请码只能创建租户 A 用户。
- 租户 A 邮箱邀请不会让用户进入租户 B。
- 租户 A 服务卡只能兑换租户 A 的 LLM 服务组。
- 租户 A 能力市场兑换码只能生成租户 A 的能力策略或部署。
- 数字员工授权卡不会跨租户增加额度。
- 所有兑换失败都写带 `tenant_id` 的审计。

## 28. 还需要继续关注的租户化边界

除邀请码和服务卡外，后续实现时还应逐项确认这些对象的作用域：

| 对象 | 建议作用域 | 说明 |
| --- | --- | --- |
| 邮件模板 | 租户级 | 发给用户的品牌、入口链接、审批文案都应属于租户 |
| 通知收件人 | 租户级 | 审批、告警、授权到期通知发给本租户管理员 |
| 试用策略 | 系统默认 + 租户实例 | 全局定义默认试用，实际试用记录落租户 |
| 账单客户号 | 租户级 | 若后续收费，一个租户对应一个 billing account |
| webhook service account | 租户级 | 外部系统触发工作流或会话必须绑定租户 |
| API token | 租户级 | 租户管理员创建的 API token 只访问本租户 |
| 数据保留策略 | 租户级，可受全局底线约束 | 会话、日志、文件保留期可能不同 |
| 合规模板 | 全局模板 + 租户启用 | 模板可复用，启用和参数按租户 |
| 品牌配置 | 租户级 | PWA 标题、Logo、邮件署名、入口页文案 |
| 域名/子路径入口 | 租户级 | `tenant.example.com` 或 `/app?tenant=slug` 都映射租户 |

这些对象如果暂时不实现独立配置，也要明确“继承 Hub 默认值，但运行时归属租户”，避免以后补功能时破坏隔离模型。
## 29. Hub Center 视角：租户即虚拟 Hub

从 Hub Center 的视角看，Hub 上的租户可以建模为“虚拟 Hub”。物理 Hub 负责承载进程、存储、运行时和真实网络入口；租户负责承载独立的用户空间、路由入口、工作模式和业务配置。这样可以最大化复用 Hub Center 现有“按邮箱路由到 Hub”的模型，只是把路由目标从 `hub_id` 扩展为 `hub_id + virtual_hub_id(tenant_id)`。

### 29.1 概念映射

| 旧模型 | 多租户模型 |
| --- | --- |
| Hub Center 路由到 Hub | Hub Center 路由到虚拟 Hub，即某物理 Hub 下的租户 |
| `hub_id` | 物理 Hub ID |
| `base_url` | 物理 Hub 入口 |
| `visibility` | 可作为物理 Hub 默认值，也可被虚拟 Hub 覆盖 |
| `enrollment_mode` | 虚拟 Hub/租户级工作模式 |
| `hub_user_links` | 邮箱到虚拟 Hub 的精确路由 |
| `hub_domain_routes` | 域名到虚拟 Hub 的路由 |
| Hub 管理员 | 物理 Hub 全局管理员 |
| Hub 内用户 | 虚拟 Hub/租户内用户 |

因此，Hub Center 不需要理解 Hub 内所有业务表，只需要知道每个物理 Hub 暴露了哪些虚拟 Hub，以及这些虚拟 Hub 的路由属性。

### 29.2 虚拟 Hub 标识

建议 Hub Center 内部使用明确字段：

- `physical_hub_id`：现有 `hub_id`。
- `virtual_hub_id`：租户 ID，可直接等于 Hub 内 `tenant_id`。
- `virtual_hub_slug`：租户短码。
- `virtual_hub_name`：租户名称。
- `entry_url`：最终入口 URL，例如 `https://hub.example.com/app?tenant=acme`。

对外 API 可以继续叫 `tenant_id`，但 Hub Center 内部实现可以把它当作虚拟 Hub 路由目标。这样现有 route snapshot、route query、hub visible list 可以较小改动地扩展。

### 29.3 路由目标结构

Hub Center 路由结果建议统一成：

```json
{
  "route_target_type": "virtual_hub",
  "physical_hub_id": "hub_123",
  "virtual_hub_id": "tenant_acme",
  "virtual_hub_slug": "acme",
  "virtual_hub_name": "Acme Corp",
  "base_url": "https://hub.example.com",
  "entry_url": "https://hub.example.com/app?tenant=acme",
  "match_type": "domain",
  "enrollment_mode": "approval",
  "status": "active"
}
```

如果某个 Hub 仍是单租户旧模式，Hub Center 可以自动把默认租户暴露为一个虚拟 Hub：

```text
physical_hub_id = hub_123
virtual_hub_id = tenant_default
virtual_hub_slug = default
```

### 29.4 对 Hub Center 现有功能的影响

| Hub Center 功能 | 虚拟 Hub 改造方式 |
| --- | --- |
| 路由查询 | 从返回 Hub 列表改为返回虚拟 Hub 列表 |
| Hubs 列表 | 仍展示物理 Hub；增加虚拟 Hub 数、active 虚拟 Hub 数 |
| Hub 详情 | 展示该物理 Hub 下的虚拟 Hub 路由表 |
| 邮箱精确路由 | `email -> physical_hub_id + virtual_hub_id` |
| 域名路由 | `domain -> physical_hub_id + virtual_hub_id` |
| public/shared 可见性 | 以虚拟 Hub 可见性为准，物理 Hub 可设置上限 |
| blocklist | 可保留全局封禁，也可扩展到 physical hub 或 virtual hub 级别 |
| 失败日志 | 记录物理 Hub、虚拟 Hub、匹配候选数和失败原因 |
| route snapshot | snapshot key 从 `hub_id` 扩展为 `hub_id + virtual_hub_id` |

### 29.5 数据表命名建议

如果希望更贴合 Hub Center 的旧模型，可以不用 `tenant_routes` 命名，而用 `virtual_hubs`：

```sql
CREATE TABLE virtual_hubs (
  physical_hub_id TEXT NOT NULL,
  virtual_hub_id TEXT NOT NULL,
  slug TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  visibility TEXT NOT NULL DEFAULT 'private',
  enrollment_mode TEXT NOT NULL DEFAULT 'approval',
  entry_path TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (physical_hub_id, virtual_hub_id)
);

CREATE TABLE virtual_hub_domain_routes (
  domain TEXT NOT NULL,
  physical_hub_id TEXT NOT NULL,
  virtual_hub_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (domain, physical_hub_id, virtual_hub_id)
);
```

如果沿用前文 `hub_tenant_routes` 也可以，本质相同。建议在代码注释中明确：Hub Center treats Hub tenants as virtual hubs.

### 29.6 Hub 上报虚拟 Hub

物理 Hub 心跳或同步时上报虚拟 Hub 列表：

```json
{
  "hub_id": "hub_123",
  "hub_secret": "***",
  "virtual_hubs": [
    {
      "virtual_hub_id": "tenant_acme",
      "slug": "acme",
      "name": "Acme Corp",
      "status": "active",
      "visibility": "private",
      "enrollment_mode": "approval",
      "domains": ["acme.com"],
      "entry_path": "/app?tenant=acme",
      "updated_at": "2026-05-18T10:00:00Z"
    }
  ]
}
```

Hub Center 只验证这是已注册物理 Hub 用 `hub_secret` 上报的数据，不干预 Hub 内租户创建和业务管理。

### 29.7 客户端体验

对用户来说，Hub Center 查询邮箱后得到的仍是“你可以进入哪些工作区/Hub”：

- 单个匹配：直接跳转到虚拟 Hub 入口。
- 多个匹配：展示多个工作区，每个工作区背后可能是不同物理 Hub，也可能是同一物理 Hub 的不同租户。
- 旧客户端：如果只支持单 URL，只有单个匹配时返回 URL；多个虚拟 Hub 匹配时返回升级提示。

### 29.8 好处

- Hub Center 不需要成为租户管理中心。
- 复用现有 Hub 路由、可见性、邮箱解析模型。
- 支持一个物理 Hub 暴露多个逻辑入口。
- 未来如果某个租户迁移到独立物理 Hub，Hub Center 只需要把虚拟 Hub 路由指向新 `physical_hub_id`，用户入口模型不变。

### 29.9 验收标准

- Hub Center 能展示一个物理 Hub 下多个虚拟 Hub。
- 邮箱路由能返回 `physical_hub_id + virtual_hub_id`。
- 同一物理 Hub 下两个租户可配置不同域名、工作模式和入口 URL。
- 多候选时 Hub Center 不自动选择。
- 单租户旧 Hub 自动映射为默认虚拟 Hub，旧路由仍可用。
## 30. Hub Center 注册边界：物理 Hub 由全局管理员注册，租户只用唯一域名路由

需要进一步明确：Hub 注册到 Hub Center 是物理 Hub 的系统级动作，只能由 Hub 全局管理员处理。租户管理员不注册 Hub，也不维护 Hub Center 连接。Hub Center 视角里租户像“虚拟 Hub”，但这些虚拟 Hub 不是单独注册主体，而是由已注册的物理 Hub 同步出来的路由项。

### 30.1 注册主体

| 操作 | 主体 | 管理者 |
| --- | --- | --- |
| 物理 Hub 注册到 Hub Center | Hub | Hub 全局管理员 |
| Hub Center 地址、公网 URL、hub_secret | Hub 系统级配置 | Hub 全局管理员 |
| 租户创建 | Hub 内租户记录 | Hub 全局管理员 |
| 租户域名绑定 | 租户路由配置 | 仅 Hub 全局管理员配置和变更 |
| 租户同步到 Hub Center | 物理 Hub 上报虚拟 Hub/租户路由 | Hub 后台任务，用 hub_secret 鉴权 |
| 租户用户接入 | 邮箱域名解析到租户 | Hub/Hub Center 路由服务 |

租户管理员不应该看到“注册到 Hub Center”的按钮，也不能修改物理 Hub 的 Hub Center 地址和 hub_secret。

### 30.2 租户注册/接入只使用唯一域名

租户对 Hub Center 暴露的路由应以唯一域名为核心。也就是说，用户注册和路由查询时只需要邮箱，Hub Center/Hub 通过邮箱域名唯一定位租户：

```text
alice@acme.com -> domain acme.com -> physical_hub_id hub_123 + tenant_id tenant_acme
```

设计约束：

- 每个租户至少绑定一个唯一邮箱域名，建议 `primary_domain` 必填。
- 同一个邮箱域名在 Hub Center 全局只能绑定到一个 active 虚拟 Hub/租户。
- 不建议依赖用户手动选择租户作为主路径；选择器只用于兼容历史数据或异常场景。
- 邀请链接可以携带租户，但最终也应校验邮箱域名是否属于该租户，除非管理员显式允许外部邮箱。
- 公共邮箱域名如 `gmail.com`、`qq.com`、`163.com` 默认不能作为租户唯一域名，除非走邀请码或精确邮箱绑定。

### 30.3 Hub Center 路由规则收敛

在“唯一域名注册”的约束下，Hub Center 路由优先级可以简化为：

1. 全局封禁检查：邮箱/IP 是否被 Hub Center 封禁。
2. 精确邮箱路由：仅用于公共邮箱、外包账号、历史兼容或管理员手工绑定。
3. 唯一域名路由：邮箱域名匹配到唯一租户。
4. 无匹配：返回未注册或要求联系管理员。
5. 多匹配：这应被视为配置错误，Hub Center 后台告警，不自动选择。

也就是说，正常注册路径不需要用户输入租户 ID，也不需要旧客户端理解租户。邮箱域名就是租户路由键。

### 30.4 Hub 同步给 Hub Center 的租户域名

物理 Hub 同步虚拟 Hub/租户时，必须上报唯一域名：

```json
{
  "hub_id": "hub_123",
  "hub_secret": "***",
  "virtual_hubs": [
    {
      "virtual_hub_id": "tenant_acme",
      "slug": "acme",
      "name": "Acme Corp",
      "status": "active",
      "primary_domain": "acme.com",
      "domains": ["acme.com", "corp.acme.com"],
      "enrollment_mode": "approval",
      "entry_path": "/app?tenant=acme"
    }
  ]
}
```

Hub Center 接收同步时要校验：

- domain 格式合法。
- domain 不在公共邮箱域名黑名单中。
- domain 未被其它 active 虚拟 Hub 占用。
- 若 domain 冲突，本次同步该租户路由失败，并写 failure log，不影响同一 Hub 的其它租户路由。

### 30.5 Hub 管理后台职责划分

全局管理员后台：

- 配置 Hub Center 地址。
- 注册/重新注册物理 Hub。
- 查看物理 Hub 注册状态。
- 创建租户并设置唯一域名。
- 查看租户路由同步状态和冲突原因。`r`n- 配置、修改、删除租户唯一域名。

租户管理员后台：

- 查看本租户域名和接入状态。
- 查看用户应使用哪个邮箱域名接入。
- 不能提交或执行域名变更；域名绑定和路由变更统一由 Hub 全局管理员处理。

### 30.6 迁移影响

旧单租户 Hub 迁移时：

- 物理 Hub 注册状态不变。
- 默认租户需要补 `primary_domain`。
- 如果旧 Hub 没有企业域名，只能继续使用默认租户兼容或精确邮箱路由。
- 当全局管理员为默认租户配置唯一域名后，Hub Center 路由切到域名路由。

多租户启用前建议做域名冲突检查：

```text
acme.com -> hub_123 / tenant_acme
acme.com -> hub_456 / tenant_other   // conflict, must resolve before activation
```

### 30.7 验收标准

- 只有 Hub 全局管理员可以注册 Hub 到 Hub Center。
- 租户管理员看不到 Hub Center 注册密钥和注册按钮。
- 租户用唯一邮箱域名暴露给 Hub Center。
- Hub Center 不允许同一 active 域名绑定到两个租户。
- 用户只输入邮箱即可路由到正确物理 Hub 和租户。
- 公共邮箱域名默认不允许作为租户唯一域名。
### 30.8 域名变更权限修正

租户域名属于 Hub 到 Hub Center 的系统级路由配置，不属于租户内业务配置。因此：

- 租户管理员不能申请域名变更。
- 租户管理员不能新增、删除、验证、修改租户域名。
- 租户管理员只能只读查看当前租户接入域名、入口 URL 和路由状态。
- Hub 全局管理员负责配置租户唯一域名，并处理 Hub Center 路由冲突。
- 域名变更必须写系统级审计：`tenant.domain.created`、`tenant.domain.updated`、`tenant.domain.deleted`、`tenant.domain.sync_failed`。

这样边界更清楚：租户管理员管租户内用户、能力市场、安全策略、数字员工、LLM 服务等业务；Hub 全局管理员管系统级租户入口和 Hub Center 路由。
## 31. 最后补充检查项

经过前面设计，主干已经覆盖 Hub 多租户、Hub Center 虚拟 Hub 路由、能力市场、安全管理、数字员工、LLM 服务、邀请码、服务卡和旧客户端兼容。落地前还需要补充以下检查项，避免实现时留下安全或运维缺口。

### 31.1 租户 slug 与域名生命周期

- `tenant_id` 一旦创建不可变。
- `tenant_slug` 建议创建后不可随意修改；如果必须修改，要保留旧 slug 到新 slug 的短期跳转映射。
- 租户域名必须由 Hub 全局管理员配置。
- 域名需要验证状态：`pending_verification`、`active`、`conflict`、`disabled`。
- 只有 `active` 域名才能同步到 Hub Center 用于路由。
- 域名删除或停用后，Hub Center 必须收到同步删除，避免旧路由继续生效。

### 31.2 租户暂停、禁用、删除的行为

租户状态变化要影响所有入口：

| 租户状态 | 管理员登录 | 用户登录 | 机器心跳 | 新会话 | 后台任务 | Hub Center 路由 |
| --- | --- | --- | --- | --- | --- | --- |
| `active` | 允许 | 允许 | 允许 | 允许 | 正常 | 返回入口 |
| `suspended` | 租户管理员只读 | 拒绝新增登录 | 可返回受限状态 | 拒绝 | 只跑清理/到期类 | 返回暂停提示或不返回入口 |
| `disabled` | 拒绝租户管理员 | 拒绝 | 拒绝敏感操作 | 拒绝 | 只跑系统维护 | 不返回入口 |
| `deleted` | 拒绝 | 拒绝 | 拒绝 | 拒绝 | 等待清理窗口 | 删除路由 |

删除必须是软删除优先。物理清理需要单独的 retention 策略和二次确认。

### 31.3 租户数据保留与清理

每个租户应有数据保留策略，可继承 Hub 默认：

- 会话保留期。
- 聊天和附件保留期。
- 审计日志保留期。
- 失败日志保留期。
- 工作流实例保留期。
- Prompt cache 保留期。

全局管理员设置系统最低或最高边界，租户管理员只能在允许范围内配置本租户保留策略。清理任务必须逐租户执行并写审计。

### 31.4 服务账号和 API Token

如果 Hub 提供 API token、service account、webhook token，必须租户化：

```sql
CREATE TABLE tenant_api_tokens (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  scopes_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'active',
  created_by_admin_id TEXT NOT NULL,
  last_used_at TEXT,
  expires_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

规则：

- 租户管理员创建的 API token 只能访问本租户。
- token scopes 必须是租户内权限，例如 `users.read`、`workflow.trigger`、`llm.invoke`。
- 全局管理员创建的是系统 API token，不可直接调用租户业务接口，除非是明确的系统同步接口。

### 31.5 前端本地存储与多账号切换

客户端和管理后台本地存储都要带租户维度：

- 当前租户信息。
- 最近使用的 Hub URL。
- 管理后台筛选条件。
- 快照导出登记簿。
- PWA session cache。
- 数字员工最近会话。
- LLM 服务选择。

退出登录、切换账号、租户不匹配时必须清理旧租户缓存，防止 UI 显示旧租户数据。

### 31.6 数据库索引和性能

所有高频租户查询都要建复合索引，`tenant_id` 应在索引前缀：

```sql
CREATE INDEX idx_sessions_tenant_updated ON sessions(tenant_id, updated_at DESC);
CREATE INDEX idx_machines_tenant_status ON machines(tenant_id, status, updated_at DESC);
CREATE INDEX idx_audit_tenant_created ON audit_logs(tenant_id, created_at DESC);
CREATE INDEX idx_workflow_tenant_status ON workflow_states(tenant_id, state, updated_at DESC);
CREATE INDEX idx_llm_usage_tenant_created ON llm_usage_logs(tenant_id, created_at DESC);
```

不要上线后才靠全表扫描过滤租户，否则多租户数据量上来后管理后台会明显变慢。

### 31.7 跨租户汇总只能读聚合

全局管理员需要看系统健康，但不应直接看租户明细。建议提供聚合表或聚合视图：

- 租户用户数。
- 在线机器数。
- 数字员工授权状态。
- LLM 用量总量。
- 能力市场风险数量。
- 安全策略违规数量。
- 最近错误数。

聚合结果可以包含租户 ID 和租户名，但不包含用户 prompt、聊天内容、附件、用户隐私明细。

### 31.8 Hub Center 同步一致性

Hub 到 Hub Center 的虚拟 Hub 同步要有版本号或更新时间：

- 每次同步带 `route_revision`。
- Hub Center 只接受新 revision，避免旧心跳覆盖新配置。
- 单个租户域名冲突不影响其它租户同步。
- Hub Center 要记录每个虚拟 Hub 的 `last_sync_at` 和 `sync_error`。
- Hub 全局管理员后台展示同步状态。

### 31.9 公共邮箱域名例外策略

如果租户没有企业域名，使用 `gmail.com/qq.com/163.com` 这类公共邮箱时，不能靠域名路由。可选方案：

- 精确邮箱绑定到租户。
- 带租户签名的邀请码链接。
- 新客户端租户选择器。

公共邮箱域名默认禁止作为租户唯一域名，除非系统级配置明确允许，并接受路由歧义风险。

### 31.10 审计不可被租户管理员删除

租户管理员可以查看本租户审计，但不能删除或篡改审计。审计清理只能由系统保留策略驱动，或由全局管理员在合规流程下执行。租户级导出的审计快照必须带 checksum。

### 31.11 租户级 feature flag

多租户上线后，不同租户可能逐步开放功能。建议支持租户级 feature flag：

```sql
CREATE TABLE tenant_feature_flags (
  tenant_id TEXT NOT NULL,
  flag TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  config_json TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (tenant_id, flag)
);
```

适用功能：数字员工、能力市场、LLM 服务、工作流、IM 插件、租户 API token、服务卡等。

### 31.12 最终实现红线

实现时必须守住这些红线：

- 任何租户业务表的新数据都不能出现空 `tenant_id`。
- 任何租户管理员接口都不能接受前端传入的 `tenant_id` 作为授权依据。
- 全局管理员不能直接复用租户业务 repository 绕过过滤。
- 邮箱域名多匹配时不能自动选择租户。
- Hub Center 只路由到虚拟 Hub，不管理租户业务。
- 租户 A 的 token、ID、缓存、导出文件、服务卡、邀请码不能访问或影响租户 B。