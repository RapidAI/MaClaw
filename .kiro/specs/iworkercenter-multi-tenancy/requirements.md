# iWorkerCenter 多租户支持 — 需求文档

## 背景

- **iWorkerCenter**（目录 `iWorkerCenter/`）：企业私有部署的数字员工中心，管理数字员工、角色、能力包、工作流、记忆等。当前单租户架构，所有数据共享一个 SQLite 数据库（`~/.iworkercenter/center.db`），无企业隔离。
- **iWorkerCloud**（目录 `iWorkerCloud/`）：云端管理平台，管理多个 iWorkerCenter 的注册、审核、License 发放。已有 RSA 公私钥体系（`iWorkerCloud/internal/license/crypto.go`）。
- 两者已有交互：iWorkerCenter 可向 iWorkerCloud 注册（`POST /api/centers/register`），iWorkerCloud 审核后发放 License。

## 术语

| 术语 | 说明 |
|------|------|
| Tenant（租户） | 一家企业，在 iWorkerCenter 中拥有独立的数据空间 |
| iWorkerCenter | 本项目，企业私有部署的数字员工中心（代码目录 `iWorkerCenter/`） |
| iWorkerCloud | 云端管理平台（代码目录 `iWorkerCloud/`） |

---

## 需求 1：多租户数据隔离

### REQ-MT-1.1 tenants 表
- 新增 `tenants` 表，存储企业信息：
  - `id` TEXT PRIMARY KEY
  - `company_name` TEXT NOT NULL（企业名称）
  - `legal_person` TEXT NOT NULL DEFAULT ''（法人代表）
  - `email` TEXT NOT NULL（企业邮箱）
  - `address` TEXT NOT NULL DEFAULT ''（企业地址）
  - `status` TEXT NOT NULL DEFAULT 'active'（active / disabled）
  - `cloud_center_id` TEXT NOT NULL DEFAULT ''（在 iWorkerCloud 注册后返回的 center_id）
  - `created_at` TEXT NOT NULL
  - `updated_at` TEXT NOT NULL
- `company_name` 加 UNIQUE 约束，防止重复开户

### REQ-MT-1.2 现有表增加 tenant_id
以下表增加 `tenant_id TEXT NOT NULL DEFAULT ''` 字段，并建立索引：
- `roles`
- `colleagues`
- `shared_memories`
- `capability_packages`
- `colleague_capability_bindings`
- `collaboration_tasks`
- `collaboration_task_events`
- `workflow_definitions`
- `workflow_step_definitions`
- `workflow_instances`
- `workflow_step_instances`
- `workflow_instance_events`
- `proxy_audit_log`
- `security_policies`
- `security_policy_hit_records`
- `config_bundles`
- `model_endpoints`
- `model_routing_policies`
- `admin_users`
- `security_groups`
- `security_group_members`
- `security_group_policies`
- `diworker_accounts`
- `role_assignment_log`

### REQ-MT-1.3 查询自动过滤
- 所有数据查询必须带上 `tenant_id` 条件
- 不存在无 tenant 的数据（全新系统，无需迁移）

---

## 需求 2：登录时企业选择

### REQ-MT-2.1 单企业自动跳过
- 登录时先查询 `tenants` 表中 status='active' 的企业数量
- 如果只有 1 个企业，自动选中该企业，不显示企业选择框
- 如果有多个企业，登录页面先展示企业列表供选择，再输入账号密码

### REQ-MT-2.2 admin_user 与 tenant 绑定
- `admin_users` 表的 `tenant_id` 字段标识该管理员属于哪个企业
- 一个 admin_user 只属于一个企业，不可跨企业
- 登录验证时需同时校验 tenant_id + username + password

### REQ-MT-2.3 登录 API 变更
- 新增 `GET /api/tenants/list`：返回所有 active 的企业列表（仅 id + company_name），用于登录页企业选择
- 登录接口 `POST /auth/login` 增加 `tenant_id` 参数

---

## 需求 3：首次登录企业信息填写

### REQ-MT-3.1 首次引导
- 系统启动后，如果 `tenants` 表为空，进入"企业注册引导"流程
- 引导页面收集：企业名称、法人代表名称、邮箱、企业地址
- 同时收集初始管理员的 username 和 password

### REQ-MT-3.2 向 iWorkerCloud 注册
- 填写完成后，iWorkerCenter 调用 iWorkerCloud 的 `POST /api/centers/register` 接口提交企业信息
- 注册成功后，将返回的 `center_id` 和 `secret` 保存到本地 `tenants` 表的 `cloud_center_id` 字段
- `secret` 存入 `system_settings`（或 tenants 表新增字段），用于后续心跳认证

### REQ-MT-3.3 本地创建
- 在 `tenants` 表创建企业记录
- 在 `admin_users` 表创建初始管理员，关联该 tenant_id
- 初始化该 tenant 的根安全组（`security_groups`）

---

## 需求 4：多租户开户接口（由 iWorkerCloud 调用）

### REQ-MT-4.1 开户 API
- iWorkerCenter 暴露 `POST /api/tenants/provision` 接口
- 请求参数：
  ```json
  {
    "company_name": "xxx公司",
    "legal_person": "张三",
    "email": "admin@xxx.com",
    "address": "北京市xxx",
    "admin_username": "admin",
    "admin_password": "初始密码",
    "timestamp": 1234567890,
    "nonce": "随机字符串",
    "signature": "RSA-SHA256签名"
  }
  ```
- 返回：
  ```json
  {
    "tenant_id": "tnt_xxx",
    "status": "active",
    "message": "开户成功"
  }
  ```

### REQ-MT-4.2 公私钥身份验证
- iWorkerCloud 用私钥对请求签名（签名内容：`timestamp + nonce + JSON(body不含signature)`）
- iWorkerCenter 用 iWorkerCloud 的公钥验证签名
- 公钥获取方式：iWorkerCenter 配置 iWorkerCloud 地址，启动时自动从 `GET /api/public-key` 拉取并缓存
- 验证失败返回 401

### REQ-MT-4.3 防重放
- 校验 `timestamp` 与服务器时间差不超过 5 分钟
- 校验 `nonce` 未被使用过（短期内缓存已用 nonce）

### REQ-MT-4.4 幂等性
- 如果 `company_name` 已存在，返回错误（409 Conflict），不做幂等处理

### REQ-MT-4.5 开户后初始化
- 创建 tenant 记录
- 创建初始 admin_user（关联 tenant_id）
- 初始化该 tenant 的根安全组

---

## 需求 5：iWorkerCenter 配置变更

### REQ-MT-5.1 新增配置项
- iWorkerCenter 需要新增配置（可在现有配置体系中扩展，或新增配置文件）：
  ```yaml
  cloud:
    base_url: "http://cloud.example.com:9366"  # iWorkerCloud 地址
    public_key_cache_hours: 24                   # 公钥缓存时间（小时）
  ```

### REQ-MT-5.2 公钥自动拉取
- iWorkerCenter 启动时从 `{cloud.base_url}/api/public-key` 拉取 iWorkerCloud 的 RSA 公钥
- 缓存到内存，按 `public_key_cache_hours` 定期刷新
- 拉取失败时使用上次缓存的公钥（如有），并记录告警日志

---

## 非功能需求

### REQ-MT-NF-1 向后兼容
- 全新系统，无需数据迁移
- 数据库 migration 追加到现有 `migrations` 列表末尾

### REQ-MT-NF-2 安全
- 开户接口必须验证 iWorkerCloud 签名，拒绝未签名请求
- admin 密码使用 bcrypt 或现有的 salt+hash 方式存储
- 公钥传输走 HTTPS（生产环境）

### REQ-MT-NF-3 性能
- tenant_id 字段建立索引，确保多租户查询不影响性能
- 公钥缓存避免每次请求都访问 iWorkerCloud
