# iWorkerCenter 多租户支持 — 任务拆解

#[[file:.kiro/specs/iworkercenter-multi-tenancy/requirements.md]]
#[[file:.kiro/specs/iworkercenter-multi-tenancy/design.md]]

---

## 阶段一：数据库与基础设施

### Task 1: 数据库 migration — tenants 表 + tenant_id 列 + 索引 + nonce 表
- [x] 在 `iWorkerCenter/internal/platform/db/migrate.go` 的 `migrations` 列表末尾追加 4 条 migration（#27~#30）
  - #27: CREATE TABLE tenants
  - #28: ALTER TABLE ... ADD COLUMN tenant_id（24 张表）
  - #29: CREATE INDEX tenant_id 索引（14 条）
  - #30: CREATE TABLE provision_nonces
- [x] 运行 `TestMigrate` 确认无报错

### Task 2: tenant 模块 — repo 层
- [x] 新建 `iWorkerCenter/internal/modules/tenant/repo.go`
  - Tenant struct
  - TenantRepo（Create, GetByID, GetByCompanyName, ListActive, Count, UpdateStatus）
- [x] 新建 `iWorkerCenter/internal/modules/tenant/nonce_repo.go`
  - NonceRepo（Consume, Cleanup）

### Task 3: tenant 模块 — cloud client + 公钥缓存
- [x] 新建 `iWorkerCenter/internal/modules/tenant/cloud_client.go`
  - CloudClient struct（BaseURL, http.Client）
  - FetchPublicKey() — GET /api/public-key
  - RegisterCenter() — POST /api/centers/register
- [x] 新建 `iWorkerCenter/internal/modules/tenant/signature.go`
  - PublicKeyCache（带 TTL 的公钥缓存，自动刷新）
  - VerifyProvisionSignature() — RSA-SHA256 签名验证
  - BuildSignPayload() — 构造签名内容

---

## 阶段二：核心业务逻辑

### Task 4: tenant 模块 — service 层
- [ ] 新建 `iWorkerCenter/internal/modules/tenant/service.go`
  - TenantService struct
  - CreateTenant() — 创建 tenant + admin_user + 根安全组
  - ListActiveTenants()
  - TenantCount()
  - ProvisionFromCloud() — 验签 + 防重放 + 创建 tenant
  - RegisterToCloud() — 调用 cloud client 注册

### Task 5: tenant 模块 — HTTP handler
- [ ] 新建 `iWorkerCenter/internal/modules/tenant/handler.go`
  - GET /auth/tenant-status — 返回 {count, needs_setup}
  - GET /auth/tenants — 返回 active 企业列表
  - POST /auth/setup-tenant — 首次引导
  - POST /api/tenants/provision — iWorkerCloud 远程开户

---

## 阶段三：登录流程改造

### Task 6: adminauth 模块改造
- [x] 修改 `iWorkerCenter/internal/modules/adminauth/handler.go`
  - handleLogin: 请求体增加 tenant_id，SQL 查询增加 tenant_id 条件
  - sessionEntry 增加 tenantID 字段
  - Authenticate() 将 tenant_id 注入 request context
  - ensureDefaultAdmin() 移除或改为 no-op（不再自动创建默认 admin，由 setup-tenant 创建）
- [ ] 新建 `iWorkerCenter/internal/modules/tenant/context.go`
  - tenantContextKey type
  - TenantIDFromContext(ctx) string
  - WithTenantID(ctx, id) context.Context

---

## 阶段四：现有模块适配 tenant_id

### Task 7: roles 模块适配
- [x] `iWorkerCenter/internal/modules/roles/repo/` — 所有 SQL 增加 tenant_id 条件

### Task 8: colleagues 模块适配
- [x] `iWorkerCenter/internal/modules/colleagues/repo/` — 同上

### Task 9: memories 模块适配
- [x] `iWorkerCenter/internal/modules/memories/` — handler 内部的 SQL 增加 tenant_id

### Task 10: capabilities 模块适配
- [x] `iWorkerCenter/internal/modules/capabilities/` — 同上

### Task 11: collaboration 模块适配
- [x] `iWorkerCenter/internal/modules/collaboration/` — repo + service + handler 增加 tenant_id

### Task 12: workflow 模块适配
- [x] `iWorkerCenter/internal/modules/workflow/` — repo + service + handler 增加 tenant_id

### Task 13: audit 模块适配
- [x] `iWorkerCenter/internal/modules/audit/` — repo + handler 增加 tenant_id

### Task 14: delivery 模块适配
- [x] `iWorkerCenter/internal/modules/delivery/` — handler 增加 tenant_id

### Task 15: security 模块适配
- [x] `iWorkerCenter/internal/modules/security/` — repo + checker + handler 增加 tenant_id

### Task 16: modelrouting 模块适配
- [x] `iWorkerCenter/internal/modules/modelrouting/` — handler 增加 tenant_id

### Task 17: recommend + imconfig + diworkerauth + experience 模块适配
- [x] `iWorkerCenter/internal/modules/diworkerauth/` — handler 增加 tenant_id
- [x] `iWorkerCenter/internal/modules/experience/` — extractor 增加 tenant_id

---

## 阶段五：Bootstrap 与配置

### Task 18: bootstrap 改造
- [ ] 修改 `iWorkerCenter/internal/app/bootstrap.go`
  - 新增 CloudConfig 读取（从 config.yaml 或环境变量）
  - 初始化 TenantRepo, NonceRepo, CloudClient, PublicKeyCache, TenantService
  - 注册 tenant handler 路由
  - 启动 nonce 清理 goroutine（每小时清理过期 nonce）
  - 启动公钥预热（如果配置了 cloud.base_url）
  - Center struct 增加 TenantService 字段

### Task 19: 配置文件支持
- [ ] 新建或扩展 `~/.iworkercenter/config.yaml` 读取逻辑
  - cloud.base_url
  - cloud.public_key_cache_hours
- [ ] bootstrap 中加载配置

---

## 阶段六：iWorkerCloud 侧变更

### Task 20: iWorkerCloud provision 签名能力
- [ ] 修改 `iWorkerCloud/internal/centers/service.go`
  - 新增 ProvisionRemote() 方法：构造签名请求，POST 到 center 的 /api/tenants/provision
- [ ] 修改 `iWorkerCloud/internal/httpapi/center_handlers.go`
  - 新增 ProvisionTenantHandler：POST /api/admin/centers/{id}/provision-tenant
- [ ] 修改 `iWorkerCloud/internal/httpapi/router.go`
  - 注册新路由

---

## 阶段七：前端

### Task 21: 登录页改造
- [ ] 修改 `iWorkerCenter/frontend/src/` 登录页面
  - 加载时调用 GET /auth/tenant-status
  - count=0 → 跳转首次引导
  - count=1 → 隐藏企业选择
  - count>1 → 显示企业下拉框
  - 登录提交增加 tenant_id

### Task 22: 首次引导页
- [ ] 新建首次引导页面组件
  - 表单：企业名称、法人代表、邮箱、地址、管理员用户名、密码
  - 提交到 POST /auth/setup-tenant
  - 成功后跳转登录页

---

## 阶段八：集成与测试

### Task 23: 集成测试
- [ ] 测试首次引导流程：setup-tenant → 登录
- [ ] 测试多租户登录：创建 2 个 tenant，验证数据隔离
- [ ] 测试 provision 接口：模拟 iWorkerCloud 签名请求
- [ ] 测试签名验证失败、重复 nonce、过期 timestamp 等异常场景
- [ ] 测试重复 company_name 返回 409
