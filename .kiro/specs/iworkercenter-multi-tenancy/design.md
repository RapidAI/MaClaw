# iWorkerCenter 多租户支持 — 技术设计

#[[file:iWorkerCenter/internal/platform/db/migrate.go]]
#[[file:iWorkerCenter/internal/app/bootstrap.go]]
#[[file:iWorkerCenter/internal/modules/adminauth/handler.go]]
#[[file:iWorkerCloud/internal/license/crypto.go]]
#[[file:iWorkerCloud/internal/httpapi/router.go]]
#[[file:iWorkerCloud/internal/centers/service.go]]

---

## 1. 数据库变更

### 1.1 新增 tenants 表（migration #26）

```sql
CREATE TABLE IF NOT EXISTS tenants (
    id               TEXT PRIMARY KEY,
    company_name     TEXT NOT NULL UNIQUE,
    legal_person     TEXT NOT NULL DEFAULT '',
    email            TEXT NOT NULL,
    address          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'active',
    cloud_center_id  TEXT NOT NULL DEFAULT '',
    cloud_secret     TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
```

### 1.2 现有表增加 tenant_id（migration #27）

由于是全新系统无需迁移数据，采用 DROP + RECREATE 策略太危险。
改用 ALTER TABLE ADD COLUMN，SQLite 支持此操作：

```sql
ALTER TABLE roles ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE colleagues ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE role_assignment_log ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE shared_memories ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE capability_packages ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE colleague_capability_bindings ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE collaboration_tasks ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE collaboration_task_events ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_definitions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_step_definitions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_instances ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_step_instances ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE workflow_instance_events ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE proxy_audit_log ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE security_policies ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE security_policy_hit_records ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE config_bundles ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE model_endpoints ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE model_routing_policies ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE admin_users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE security_groups ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE security_group_members ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE security_group_policies ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE diworker_accounts ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
```

### 1.3 tenant_id 索引（migration #28）

```sql
CREATE INDEX IF NOT EXISTS idx_roles_tenant ON roles(tenant_id);
CREATE INDEX IF NOT EXISTS idx_colleagues_tenant ON colleagues(tenant_id);
CREATE INDEX IF NOT EXISTS idx_shared_memories_tenant ON shared_memories(tenant_id);
CREATE INDEX IF NOT EXISTS idx_capability_packages_tenant ON capability_packages(tenant_id);
CREATE INDEX IF NOT EXISTS idx_collab_tasks_tenant ON collaboration_tasks(tenant_id);
CREATE INDEX IF NOT EXISTS idx_workflow_defs_tenant ON workflow_definitions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_workflow_instances_tenant ON workflow_instances(tenant_id);
CREATE INDEX IF NOT EXISTS idx_security_policies_tenant ON security_policies(tenant_id);
CREATE INDEX IF NOT EXISTS idx_model_endpoints_tenant ON model_endpoints(tenant_id);
CREATE INDEX IF NOT EXISTS idx_admin_users_tenant ON admin_users(tenant_id);
CREATE INDEX IF NOT EXISTS idx_security_groups_tenant ON security_groups(tenant_id);
CREATE INDEX IF NOT EXISTS idx_diworker_accounts_tenant ON diworker_accounts(tenant_id);
CREATE INDEX IF NOT EXISTS idx_config_bundles_tenant ON config_bundles(tenant_id);
CREATE INDEX IF NOT EXISTS idx_proxy_audit_tenant ON proxy_audit_log(tenant_id);
```

### 1.4 nonce 防重放表（migration #29）

```sql
CREATE TABLE IF NOT EXISTS provision_nonces (
    nonce      TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_nonces_expires ON provision_nonces(expires_at);
```

---

## 2. 新增模块：`iWorkerCenter/internal/modules/tenant`

### 2.1 目录结构

```
iWorkerCenter/internal/modules/tenant/
├── repo.go          # TenantRepository — CRUD for tenants table
├── service.go       # TenantService — 业务逻辑
├── handler.go       # HTTP handlers
├── cloud_client.go  # iWorkerCloud 交互（注册、拉取公钥）
├── signature.go     # RSA 签名验证逻辑
└── nonce_repo.go    # nonce 防重放存储
```

### 2.2 TenantRepository（repo.go）

```go
type Tenant struct {
    ID            string
    CompanyName   string
    LegalPerson   string
    Email         string
    Address       string
    Status        string // active, disabled
    CloudCenterID string
    CloudSecret   string
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type TenantRepository interface {
    Create(ctx context.Context, t *Tenant) error
    GetByID(ctx context.Context, id string) (*Tenant, error)
    GetByCompanyName(ctx context.Context, name string) (*Tenant, error)
    ListActive(ctx context.Context) ([]*Tenant, error)
    Count(ctx context.Context) (int, error)
    UpdateStatus(ctx context.Context, id, status string) error
}
```

实现使用 `*sql.DB`（write + read），遵循现有 repo 模式。

### 2.3 TenantService（service.go）

```go
type TenantService struct {
    repo        TenantRepository
    nonceRepo   NonceRepository
    cloudClient *CloudClient
    pubKeyCache *PublicKeyCache
}

// CreateTenant — 首次引导或 provision 接口调用
func (s *TenantService) CreateTenant(ctx context.Context, req CreateTenantRequest) (*Tenant, error)

// ListActiveTenants — 登录页获取企业列表
func (s *TenantService) ListActiveTenants(ctx context.Context) ([]*Tenant, error)

// TenantCount — 判断是否需要显示企业选择
func (s *TenantService) TenantCount(ctx context.Context) (int, error)

// ProvisionFromCloud — iWorkerCloud 远程开户（含签名验证）
func (s *TenantService) ProvisionFromCloud(ctx context.Context, req ProvisionRequest) (*Tenant, error)

// RegisterToCloud — 首次引导时向 iWorkerCloud 注册
func (s *TenantService) RegisterToCloud(ctx context.Context, tenantID string) error
```

### 2.4 CloudClient（cloud_client.go）

```go
type CloudConfig struct {
    BaseURL            string // iWorkerCloud 地址
    PublicKeyCacheHours int   // 公钥缓存时间，默认 24
}

type CloudClient struct {
    cfg    CloudConfig
    client *http.Client
}

// FetchPublicKey — GET {base_url}/api/public-key，返回 PEM 格式公钥
func (c *CloudClient) FetchPublicKey(ctx context.Context) ([]byte, error)

// RegisterCenter — POST {base_url}/api/centers/register
func (c *CloudClient) RegisterCenter(ctx context.Context, req RegisterCenterRequest) (*RegisterCenterResponse, error)
```

### 2.5 PublicKeyCache（signature.go）

```go
type PublicKeyCache struct {
    mu        sync.RWMutex
    pubKey    *rsa.PublicKey
    fetchedAt time.Time
    ttl       time.Duration
    fetcher   func(ctx context.Context) ([]byte, error)
}

// Get — 返回缓存的公钥，过期则自动刷新
func (c *PublicKeyCache) Get(ctx context.Context) (*rsa.PublicKey, error)

// VerifyProvisionSignature — 验证 provision 请求签名
// 签名内容 = timestamp + nonce + sha256(body_without_signature)
func VerifyProvisionSignature(pubKey *rsa.PublicKey, timestamp int64, nonce string, bodyHash []byte, signature []byte) error
```

### 2.6 NonceRepository（nonce_repo.go）

```go
type NonceRepository interface {
    // Consume — 如果 nonce 不存在则插入并返回 true，已存在返回 false
    Consume(ctx context.Context, nonce string, expiresAt time.Time) (bool, error)
    // Cleanup — 删除过期 nonce
    Cleanup(ctx context.Context) error
}
```

---

## 3. 登录流程变更

### 3.1 现有 adminauth handler 改造

文件：`iWorkerCenter/internal/modules/adminauth/handler.go`

**handleLogin 变更：**

1. 请求体新增 `tenant_id` 字段
2. 验证时 SQL 改为 `WHERE username = ? AND tenant_id = ?`
3. session token 中携带 tenant_id 信息

**新增 handleTenantList：**

```
GET /auth/tenants → 返回 [{id, company_name}]
```

**新增 handleTenantStatus：**

```
GET /auth/tenant-status → 返回 {count: N, needs_setup: bool}
```

- `count=0` → 需要首次引导
- `count=1` → 自动选中，不显示选择框
- `count>1` → 显示企业选择框

### 3.2 session 中携带 tenant_id

现有 `sessionStore` 的 `sessionEntry` 增加 `tenantID` 字段。
`Authenticate(r *http.Request)` 返回时将 tenant_id 注入 request context。

```go
type tenantContextKey struct{}

func TenantIDFromContext(ctx context.Context) string {
    v, _ := ctx.Value(tenantContextKey{}).(string)
    return v
}
```

所有需要 tenant 过滤的 handler 从 context 中取 tenant_id。

---

## 4. 首次引导流程

### 4.1 API

```
POST /auth/setup-tenant
```

请求体：
```json
{
    "company_name": "xxx公司",
    "legal_person": "张三",
    "email": "admin@xxx.com",
    "address": "北京市xxx",
    "admin_username": "admin",
    "admin_password": "密码"
}
```

### 4.2 处理流程

1. 检查 `tenants` 表是否为空，非空则拒绝（此接口仅首次可用）
2. 创建 tenant 记录（`id = tnt_{timestamp}`）
3. 创建 admin_user 记录（关联 tenant_id，密码用 salt+hash）
4. 初始化该 tenant 的根安全组
5. 如果配置了 `cloud.base_url`，异步调用 iWorkerCloud 注册：
   - `POST {cloud.base_url}/api/centers/register`
   - 成功后更新 tenant 的 `cloud_center_id` 和 `cloud_secret`
   - 失败不阻塞，记录日志，后续可手动重试

---

## 5. Provision 接口（iWorkerCloud 调用）

### 5.1 API

```
POST /api/tenants/provision
```

### 5.2 请求签名协议

iWorkerCloud 构造请求时：

1. 构造 body JSON（不含 `signature` 字段）
2. 计算 `payload = fmt.Sprintf("%d:%s:%s", timestamp, nonce, sha256hex(body))`
3. 用 RSA 私钥对 `sha256(payload)` 签名
4. 将签名 base64 编码后放入 `signature` 字段

iWorkerCenter 验证时：

1. 从请求中提取 `timestamp`、`nonce`、`signature`
2. 检查 timestamp 与当前时间差 ≤ 5 分钟
3. 检查 nonce 未被使用（`provision_nonces` 表）
4. 从 body 中去掉 `signature` 字段，计算 sha256
5. 重建 payload，用缓存的 iWorkerCloud 公钥验证签名
6. 验证通过后消费 nonce

### 5.3 处理流程

1. 验证签名（失败返回 401）
2. 检查 `company_name` 是否已存在（存在返回 409）
3. 创建 tenant 记录
4. 创建初始 admin_user（关联 tenant_id）
5. 初始化根安全组
6. 返回 `{tenant_id, status: "active"}`

---

## 6. iWorkerCloud 侧变更

### 6.1 新增 provision 签名能力

文件：`iWorkerCloud/internal/centers/service.go`

新增方法：
```go
// ProvisionRemote — 向 iWorkerCenter 发送签名的 provision 请求
func (s *Service) ProvisionRemote(ctx context.Context, centerBaseURL string, req ProvisionRequest) (*ProvisionResult, error)
```

使用已有的 `license.SignData(privKey, data)` 进行签名。

### 6.2 新增 admin API

文件：`iWorkerCloud/internal/httpapi/center_handlers.go`

```
POST /api/admin/centers/{id}/provision-tenant
```

请求体：
```json
{
    "company_name": "xxx公司",
    "legal_person": "张三",
    "email": "admin@xxx.com",
    "address": "北京市xxx",
    "admin_username": "admin",
    "admin_password": "初始密码"
}
```

iWorkerCloud admin 通过此接口向指定 center 远程开户。
center 的 `base_url` 从 centers 表中获取（注册时已保存）。

---

## 7. 配置变更

### 7.1 iWorkerCenter 配置

在 `iWorkerCenter/internal/app/bootstrap.go` 中读取环境变量或配置文件：

```go
type CloudConfig struct {
    BaseURL            string `yaml:"base_url"`    // 如 http://cloud.example.com:9366
    PublicKeyCacheHours int    `yaml:"public_key_cache_hours"` // 默认 24
}
```

配置文件路径：`~/.iworkercenter/config.yaml`（如果不存在则用默认值）

```yaml
cloud:
  base_url: ""
  public_key_cache_hours: 24
```

`base_url` 为空时，provision 接口仍可工作（只是不验证签名？不，应该拒绝）。
实际上：`base_url` 为空时，provision 接口返回 503（未配置 cloud 连接）。

---

## 8. 现有模块适配

### 8.1 所有 repo 层增加 tenant_id 参数

以 roles 模块为例（`iWorkerCenter/internal/modules/roles/repo/`）：

现有：
```go
func (r *Repo) List() ([]*Role, error) {
    rows, err := r.read.Query("SELECT ... FROM roles WHERE status='active'")
}
```

改为：
```go
func (r *Repo) List(tenantID string) ([]*Role, error) {
    rows, err := r.read.Query("SELECT ... FROM roles WHERE tenant_id=? AND status='active'", tenantID)
}
```

所有 CRUD 方法增加 tenantID 参数。Create 时写入 tenant_id。

### 8.2 所有 service 层传递 tenant_id

从 handler 的 request context 中取 tenant_id，传给 service → repo。

### 8.3 所有 handler 层从 context 取 tenant_id

```go
tenantID := tenant.TenantIDFromContext(r.Context())
```

---

## 9. 前端变更（概要）

文件：`iWorkerCenter/frontend/`

### 9.1 登录页

1. 页面加载时调用 `GET /auth/tenant-status`
2. `count=0` → 跳转到首次引导页
3. `count=1` → 隐藏企业选择，直接显示登录表单
4. `count>1` → 显示企业下拉框（数据来自 `GET /auth/tenants`），选择后显示登录表单
5. 登录时提交 `{tenant_id, username, password, captcha_id, captcha_answer}`

### 9.2 首次引导页

表单字段：企业名称、法人代表、邮箱、地址、管理员用户名、管理员密码
提交到 `POST /auth/setup-tenant`

---

## 10. 安全考虑

- provision 接口必须验证 RSA 签名，无签名或验证失败返回 401
- timestamp 窗口 5 分钟，防重放
- nonce 一次性使用，过期后定期清理（bootstrap 时启动 goroutine，每小时清理一次）
- admin 密码使用现有的 salt + sha256 方式（与 adminauth 一致）
- 公钥缓存失败时使用上次成功的缓存，记录 WARN 日志
