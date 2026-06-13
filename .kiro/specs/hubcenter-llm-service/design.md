# HubCenter LLM 算力服务接入设计

## 1. 需求概览

### 1.1 背景

Hub 上已有完整的 LLM 服务体系（服务组、Provider 调度、用户计费、缓存）。现需将 LLM 服务能力延伸到 HubCenter 层面，使 HubCenter 成为"算力批发商"，Hub 租户可通过 HubCenter 获得 LLM 算力。

### 1.2 核心功能

1. **HubCenter LLM 服务组管理** — 配置 LLM endpoint/provider，按策略调度
2. **Hub 新增"MaClaw 官方"服务商** — 通过 HubCenter 代理 LLM 请求
3. **算力接入授权** — HubCenter 向 Hub 租户授权 credits 额度 + 有效期
4. **Card Store（9 种卡型）** — Hub 租户管理员购买 credits 充值
5. **使用统计** — HubCenter 按 Hub→租户维度统计 token 用量
6. **LLM 缓存** — HubCenter 与 Hub 共用缓存模块，减少成本
7. **接入容量管理** — Hub/HubCenter 共用的限流 + 熔断
8. **HA 防双花** — Hub pin 到单一 HubCenter 节点，节点间轻量 binding 同步

### 1.3 权限模型

| Hub 租户状态 | 可用的 LLM 服务商 |
|---|---|
| 未获得 HubCenter "算力接入"授权 | **只能**用"MaClaw 官方"（走 HubCenter 转发） |
| 已获得 HubCenter "算力接入"授权 | 可以用"MaClaw 官方" **+** 自行接入其他服务商 |

---

## 2. 架构设计

### 2.1 请求链路

```
MaClaw客户端 → Hub → "MaClaw官方" Provider → HubCenter LLM Proxy → 内部Provider调度 → 实际LLM API
```

### 2.2 模块分层

```
corelib/llmpool/          ← 新增：Hub/HubCenter 共享的 LLM 服务组 + Provider 调度 + 缓存 + 限流
  ├── provider.go         ← Provider 定义 + 多 provider 调度策略
  ├── cache.go            ← 请求级缓存（从 hub/internal/llmcache 提升）
  ├── ratelimit.go        ← 容量管理（从 corelib/llm_endpoint_proxy.go 的并发控制提升）
  └── usage.go            ← 用量记录接口

hubcenter/internal/
  ├── llmservice/          ← 新增：HubCenter LLM 服务
  │   ├── registry.go     ← LLM 服务组 + Provider 配置（复用 corelib/llmpool 的类型）
  │   ├── proxy.go        ← LLM 代理端点（接收 Hub 请求，内部调度 provider）
  │   ├── auth.go         ← 租户算力授权验证（credits 额度 + 有效期）
  │   ├── billing.go      ← 租户级用量扣减
  │   └── stats.go        ← 使用统计（按 hub→tenant 维度）
  ├── cardstore/           ← 新增：Credits 点数卡商城
  │   ├── store.go        ← 9 种卡型定义 + 购买/激活逻辑
  │   └── payment.go      ← 支付回调处理
  └── ha/
      └── binding.go       ← 新增：租户节点绑定 + 跨节点同步

hub/internal/
  └── llmservice/
      └── maclaw_provider.go ← 新增："MaClaw 官方" provider 实现（转发到 HubCenter）
```

### 2.3 共享模块设计（corelib/llmpool/）

目标：Hub 的 `llmservice` 和 HubCenter 的 `llmservice` 共用核心逻辑。

```go
// corelib/llmpool/provider.go

// ProviderConfig 是 Hub 和 HubCenter 共享的 LLM Provider 配置
type ProviderConfig struct {
    ID                   string   `json:"id"`
    Name                 string   `json:"name"`
    APIURL               string   `json:"api_url"`
    APIKey               string   `json:"api_key"`
    Protocol             string   `json:"protocol"`          // "openai" / "anthropic"
    Models               []string `json:"models,omitempty"`
    CapabilityTags       []string `json:"capability_tags,omitempty"`
    Priority             int      `json:"priority,omitempty"`
    MaxConcurrency       int      `json:"max_concurrency,omitempty"`
    MaxQueueWaiters      int      `json:"max_queue_waiters,omitempty"`
    QueueTimeoutMS       int      `json:"queue_timeout_ms,omitempty"`
    CircuitBreakerThreshold  int  `json:"circuit_breaker_threshold,omitempty"`
    CircuitBreakerCooldownMS int  `json:"circuit_breaker_cooldown_ms,omitempty"`
}

// ServiceGroup 是 LLM 服务组（Hub/HubCenter 共用结构）
type ServiceGroup struct {
    ID          string           `json:"id"`
    Name        string           `json:"name"`
    Description string           `json:"description,omitempty"`
    Models      []ModelConfig    `json:"models"`
}

type ModelConfig struct {
    Name            string                `json:"name"`
    ProviderConfigs []ModelProviderConfig  `json:"provider_configs"`
    CapabilityTags  []string              `json:"capability_tags,omitempty"`
    CreditMultiplier float64             `json:"credit_multiplier,omitempty"`
}

type ModelProviderConfig struct {
    ProviderID     string   `json:"provider_id"`
    Priority       int      `json:"priority,omitempty"`
    CapabilityTags []string `json:"capability_tags,omitempty"`
}
```

---

## 3. HubCenter LLM 服务详细设计

### 3.1 服务组 + Provider 调度

HubCenter 的 LLM 服务组与 Hub 的 `ModelServiceGroup` 结构对等，但面向不同场景：

- **Hub 的服务组**：面向终端用户，有访问策略（free/grant_required）、用户绑定、组绑定
- **HubCenter 的服务组**：面向内部调度，无用户绑定，只做 provider 选择和负载分配

调度策略复用现有的 `OrderProvidersForRequest()` 逻辑（capability matching + priority + resolution tier）。

### 3.2 LLM 代理接口

```
POST /api/llm/v1/chat/completions
Headers:
  Authorization: Bearer <hub_machine_token>
  X-Hub-ID: <hub_id>
  X-Tenant-ID: <tenant_id>
Body: OpenAI-compatible request
```

处理流程：
1. 验证 `hub_machine_token` + hub_id（复用现有 hub 认证机制）
2. 查找租户授权记录 → 验证 credits 余额 + 有效期
3. 检查节点绑定（HA 防双花）
4. 查 LLM 缓存 → 命中则直接返回
5. 按服务组策略选择 provider → 转发请求
6. 记录 token 用量 → 扣减 credits
7. 写入缓存

### 3.3 授权模型

```go
// hubcenter/internal/llmservice/auth.go

type TenantLLMAuthorization struct {
    ID             string    `json:"id"`
    HubID          string    `json:"hub_id"`
    TenantID       string    `json:"tenant_id"`
    AdminEmail     string    `json:"admin_email"`
    ServiceGroupID string    `json:"service_group_id"`   // 绑定的 LLM 服务组
    CreditsTotal   float64   `json:"credits_total"`
    CreditsUsed    float64   `json:"credits_used"`
    StartsAt       time.Time `json:"starts_at"`
    ExpiresAt      time.Time `json:"expires_at"`
    CreatedAt      time.Time `json:"created_at"`
    Status         string    `json:"status"`          // "active" / "expired" / "exhausted"
    Source         string    `json:"source"`          // "card" / "admin_grant"
    CardOrderID    string    `json:"card_order_id,omitempty"`
    
    // 算力接入授权标志 — 允许租户在 Hub 上自行接入第三方服务商
    AllowExternalProviders bool `json:"allow_external_providers"`
    
    // 绑定的 HubCenter 节点（HA 防双花）
    BoundNodeID    string    `json:"bound_node_id,omitempty"`
    BoundAt        time.Time `json:"bound_at,omitempty"`
}
```

- 一个租户可以有**多条授权记录**（多次购卡，绑定不同服务组）
- 请求到达时按 model → 查找匹配的服务组 → 在该服务组的授权中扣减 credits
- 与 Hub 的 Grant 模型对比：

| | Hub Grant | HubCenter Authorization |
|---|---|---|
| 绑定对象 | 用户 (email) | Hub 租户 (hub_id + tenant_id) |
| 服务组 | ✅ 绑定 | ✅ 绑定 |
| Credits | ✅ | ✅ |
| 有效期 | ✅ | ✅ |
| Period limits (5h/daily/weekly/monthly) | ✅ | ❌ 不需要 |
| Source | card/admin_grant | card/admin_grant |

### 3.4 Card Store（动态卡型）

HubCenter 的卡型不像 Hub 那样写死，而是管理员可以自由创建/管理。

```go
// hubcenter/internal/cardstore/store.go

type CardType struct {
    ID             string  `json:"id"`
    ServiceGroupID string  `json:"service_group_id"`  // 绑定的 LLM 服务组
    Label          string  `json:"label"`             // 卡名称（如"高级模型月卡·10万点"）
    Credits        float64 `json:"credits"`           // 额度（默认选项：10000/100000/1000000，也可自定义）
    Period         string  `json:"period"`            // 有效期："month" / "quarter" / "year"
    PriceRMB       float64 `json:"price_rmb"`         // 价格
    Template       string  `json:"template"`          // 卡片图案模板 ID（预设几种）
    Enabled        bool    `json:"enabled"`           // 是否上架
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}

// 卡片图案模板（预设）
var CardTemplates = []string{
    "gradient_blue",    // 蓝色渐变
    "gradient_purple",  // 紫色渐变
    "gradient_gold",    // 金色渐变
    "dark_tech",        // 深色科技感
    "minimal_white",    // 简约白
}

// 创建卡型时的默认额度选项（UI 快捷选择，也可手工输入任意值）
var DefaultCreditOptions = []float64{10000, 100000, 1000000}
```

#### 卡型管理（HubCenter 管理员操作）

创建卡型只需：
1. 选择绑定的服务组
2. 输入卡名称
3. 选择有效期（月/季/年）
4. 选择或输入额度（默认 1万/10万/100万，也可手工输入）
5. 输入价格
6. 选择卡片图案模板

管理员可以随时：
- 创建新卡型
- 修改已有卡型（价格、名称等）
- 上架/下架卡型（`enabled` 控制是否在商城展示）

type PurchaseOrder struct {
    ID            string    `json:"id"`
    CardTypeID    string    `json:"card_type_id"`
    AdminEmail    string    `json:"admin_email"`    // Hub 租户管理员邮箱
    HubID         string    `json:"hub_id"`
    TenantID      string    `json:"tenant_id"`
    Status        string    `json:"status"`         // "pending" / "paid" / "activated" / "cancelled"
    PaymentMode   string    `json:"payment_mode"`   // "personal_semimanual" / "alipay_direct"
    PriceRMB      float64   `json:"price_rmb"`
    PaymentRef    string    `json:"payment_ref,omitempty"`
    CreatedAt     time.Time `json:"created_at"`
    PaidAt        *time.Time `json:"paid_at,omitempty"`
    ActivatedAt   *time.Time `json:"activated_at,omitempty"`
    ConfirmedBy   string    `json:"confirmed_by,omitempty"`  // 手工确认时记录操作者
}
```

#### 支付方式（与 Hub Card Store 复用）

| 方式 | ID | 说明 |
|------|-----|------|
| 个人收款码 | `personal_semimanual` | 展示微信/支付宝二维码，管理员手工确认到账后充值 |
| 支付宝直连 | `alipay_direct` | 支付宝官方 API，异步回调自动确认充值 |

~~`payment_fm`~~ 已废弃，Hub 和 HubCenter 均不再使用。

#### 实现策略：直接从 Hub 复用

`personal_semimanual` 支付方式的实现直接从 Hub 搬到 HubCenter，核心代码复用：

| Hub 代码 | HubCenter 复用 | 差异 |
|----------|---------------|------|
| `hub/internal/httpapi/card_store_handlers.go` | 搬过来 | 充值目标从 user grant → tenant credits |
| `hub/web/admin/card-store-tab.js`（二维码配置 UI） | 搬过来 | 产品列表改为 9 种固定卡型 |
| 订单创建/查询/手工确认逻辑 | 搬过来 | 确认后调用 `TenantLLMAuthorization` 增加 credits |
| `alipay_direct` 对接逻辑 | 搬过来 | 回调 URL 改为 HubCenter 的，充值目标同上 |

可考虑将支付流程核心（订单状态机、手工确认机制、支付宝回调验签）提取到 `corelib/cardstore/` 共享，Hub 和 HubCenter 只需实现"充值目标"接口（Hub: 创建 user grant；HubCenter: 增加 tenant credits）。

#### 购买流程

1. Hub 租户管理员选择卡型 → 提交订单（带 admin_email + hub_id + tenant_id）
2. 根据配置的支付方式：
   - `personal_semimanual`：返回收款二维码 URL，订单状态 `pending`
   - `alipay_direct`：返回支付宝支付 URL，订单状态 `pending`
3. 到账确认：
   - `personal_semimanual`：HubCenter 管理员在订单列表中点击"确认到账" → 状态变为 `paid`
   - `alipay_direct`：支付宝异步通知回调 → 状态变为 `paid`
4. 激活（paid → activated）：
   - 找到 admin_email 对应的 Hub+Tenant → 增加 `TenantLLMAuthorization.CreditsTotal` + 延长有效期
   - 如果该租户还没有 `TenantLLMAuthorization` → 自动创建（状态 active，`AllowExternalProviders=false`）

#### 订单管理

- **查询**：按 admin_email / hub_id / tenant_id / status / 时间范围筛选
- **手工确认**：仅 `personal_semimanual` 模式的 `pending` 订单可手工确认
- **取消**：`pending` 状态的订单可取消

### 3.4.1 Hub 侧 payment_fm 废弃

Hub 的 Card Store 同步移除 `payment_fm` 支付方式：
- `hub/web/admin/card-store-tab.js`：移除 payment_fm 选项
- `hub/internal/httpapi/card_store_handlers.go`：移除 payment_fm 相关逻辑
- 保留 `personal_semimanual` + `alipay_direct` 两种方式

### 3.5 使用统计

```go
// hubcenter/internal/llmservice/stats.go

type TenantUsageRecord struct {
    HubID        string    `json:"hub_id"`
    TenantID     string    `json:"tenant_id"`
    Model        string    `json:"model"`
    ProviderID   string    `json:"provider_id"`
    InputTokens  int64     `json:"input_tokens"`
    OutputTokens int64     `json:"output_tokens"`
    Credits      float64   `json:"credits"`
    CacheHit     bool      `json:"cache_hit"`
    Timestamp    time.Time `json:"timestamp"`
}

// 汇总维度：hub→tenant→model→日/周/月
type TenantUsageSummary struct {
    HubID          string  `json:"hub_id"`
    TenantID       string  `json:"tenant_id"`
    Period         string  `json:"period"`     // "daily" / "weekly" / "monthly"
    PeriodStart    string  `json:"period_start"`
    InputTokens    int64   `json:"input_tokens"`
    OutputTokens   int64   `json:"output_tokens"`
    TotalCredits   float64 `json:"total_credits"`
    TotalRequests  int64   `json:"total_requests"`
    CacheHitRate   float64 `json:"cache_hit_rate"`
}
```

---

## 4. Hub 侧改动

### 4.1 内置 MaClaw 官方 Provider + 服务组

Hub 启动时自动注入两个不可删除/不可编辑的内置项：

```go
// hub/internal/llmservice/maclaw_builtin.go

const (
    MaClawOfficialProviderID       = "maclaw_official"
    MaClawOfficialProviderName     = "MaClaw 官方"
    MaClawOfficialServiceGroupID   = "maclaw_official_group"
    MaClawOfficialServiceGroupName = "MaClaw 官方服务组"
)

// IsBuiltinProvider returns true for items that cannot be deleted/edited.
func IsBuiltinProvider(id string) bool {
    return id == MaClawOfficialProviderID
}

// IsBuiltinServiceGroup returns true for items that cannot be deleted/edited.
func IsBuiltinServiceGroup(id string) bool {
    return id == MaClawOfficialServiceGroupID
}
```

**内置 Provider**：
- ID: `maclaw_official`
- 不可删除/编辑
- 请求转发到 HubCenter LLM 代理接口

**内置服务组**：
- ID: `maclaw_official_group`
- 不可删除/编辑
- 包含 MaClaw 官方 Provider 提供的模型（从 HubCenter 同步可用模型列表）
- 未授权租户的**唯一**可用服务组（自动选中，不需要用户选择）

**权限控制**：

| 租户状态 | 可见的服务组 | 可添加 Provider | 可创建服务组 |
|----------|-------------|-----------------|-------------|
| 未获得"算力接入"授权 | 只有 MaClaw 官方服务组 | 按钮可见但不可用（提示联系 MaClaw 官方获取授权） | 按钮可见但不可用（同上提示） |
| 已获得"算力接入"授权 | MaClaw 官方 + 自建服务组 | ✅ | ✅ |

**未授权时的 UI 行为**：
- "添加 LLM 服务商"按钮始终可见（让管理员知道此能力存在）
- 点击后弹出提示框："需要获得 MaClaw 官方算力模块授权才能添加自定义 LLM 服务。请联系 MaClaw 官方获取授权。"
- "创建服务组"按钮同理

### 4.2 购买算力入口（Hub → HubCenter 算力商店跳转）

Hub 管理后台的"MaClaw 官方"服务商卡片上显示"购买算力"按钮：

```
跳转 URL: {HubCenterURL}/compute-store?hub_id={hubID}&tenant_id={tenantID}&email={adminEmail}
```

参数说明：
- `hub_id`：当前 Hub 实例 ID
- `tenant_id`：当前租户 ID
- `email`：管理员邮箱（用于定位购买者身份 + 充值目标）

算力商店页面收到参数后：
1. 自动填充管理员邮箱、Hub/租户信息（不可编辑）
2. 管理员选卡 → 支付
3. 支付确认后 credits 直接充入该 hub_id + tenant_id 的租户账户

**安全考虑**：
- 跳转 URL 中的参数仅用于预填充，实际充值时 HubCenter 会验证该 email 是否确实是该 Hub+Tenant 的管理员（通过 Hub 注册时的 owner_email 校验）
- 防止伪造 URL 向他人租户充值

### 4.3 MaClaw 官方 Provider 实现

```go
// hub/internal/llmservice/maclaw_provider.go

const MaClawOfficialProviderID = "maclaw_official"
const MaClawOfficialProviderName = "MaClaw 官方"

// MaClawProvider 实现 Hub 的 provider 接口，将请求转发到 HubCenter
type MaClawProvider struct {
    HubCenterURL   string  // 绑定的 HubCenter 节点 URL
    HubID          string
    TenantID       string
    MachineToken   string
}

func (p *MaClawProvider) Forward(ctx context.Context, req *http.Request) (*http.Response, error) {
    // 1. 构造转发请求（添加 X-Hub-ID / X-Tenant-ID headers）
    // 2. 使用 corelib/llmpool 的容量管理（限流 + 熔断）
    // 3. 转发到 HubCenterURL/api/llm/v1/chat/completions
    // 4. 透传响应
}
```

### 4.2 服务商列表控制

Hub 查询租户是否有"算力接入"授权：
- 有授权（`AllowExternalProviders=true`）→ 服务商列表 = MaClaw 官方 + 自行配置的第三方
- 无授权 → 服务商列表锁定为只有 MaClaw 官方

授权信息来源：
- Hub 注册到 HubCenter 时，HubCenter 返回该 Hub 各租户的授权状态
- Hub 定期心跳时同步更新授权状态
- 或者 Hub 在需要时主动查询 `GET /api/llm/v1/authorization?hub_id=X&tenant_id=Y`

---

## 5. HA 防双花设计

### 5.1 机制

- **Hub 侧 pin 节点**：Hub 首次请求"MaClaw 官方"时，pin 到响应最快的 HubCenter 节点，持久化 `bound_hubcenter_llm_url`
- **HubCenter 侧绑定记录**：收到请求时记录 `TenantLLMAuthorization.BoundNodeID`
- **跨节点同步**：HubCenter 节点间每 30-60 秒同步 `active_bindings` 表
- **拒绝逻辑**：非 bound 节点收到该租户请求 → 返回 `409 Conflict + redirect_to: bound_node_url`

### 5.2 故障转移

- Hub 连续 3 次请求失败（或 30s 内全部失败）→ 判定节点不可用
- 等待绑定冷却期（5 分钟内不切换，除非连续失败）
- 超过冷却期 → 选新节点 → 新节点检查 lease 是否过期（TTL 10 分钟）→ 接受新绑定
- 旧节点 lease 过期后自动清理

### 5.3 数据结构

```go
// hubcenter/internal/ha/binding.go

type LLMBinding struct {
    HubID      string    `json:"hub_id"`
    TenantID   string    `json:"tenant_id"`
    NodeID     string    `json:"node_id"`
    BoundAt    time.Time `json:"bound_at"`
    LastActive time.Time `json:"last_active"`
    ExpiresAt  time.Time `json:"expires_at"`    // BoundAt + 10min, 每次请求续约
}

// 同步消息（节点间广播）
type BindingSyncMessage struct {
    NodeID   string       `json:"node_id"`
    Bindings []LLMBinding `json:"bindings"`
    SentAt   time.Time    `json:"sent_at"`
}
```

---

## 6. 缓存设计

### 6.1 共享缓存模块

从 `hub/internal/llmcache/` 提升到 `corelib/llmpool/cache.go`，Hub 和 HubCenter 复用同一套实现：

- **内存层**：LRU，按 entry 数 + 字节数限制
- **磁盘层**：SQLite，按 TTL 过期
- **缓存 key**：`sha256(model + sorted_messages + temperature + top_p + ...)`（去除不影响结果的参数）

### 6.2 HubCenter 缓存的特殊考虑

- HubCenter 面向多个 Hub 的多个租户，缓存命中率更高（不同租户的相同请求可共享缓存）
- 缓存 key 不包含 hub_id/tenant_id（相同请求 = 相同结果，不区分来源）
- 统计中区分"来自缓存"和"实际请求"，缓存命中不扣减 credits（或按折扣扣减）

---

## 7. 接入容量管理

### 7.1 共享的限流 + 熔断

从 `corelib/llm_endpoint_proxy.go` 提升到 `corelib/llmpool/ratelimit.go`：

- **Per-provider 并发控制**：`MaxConcurrency` + 等待队列
- **Per-provider 熔断器**：连续失败 N 次 → 断路 → 冷却后半开 → 成功则恢复
- **Per-tenant 速率限制**（HubCenter 新增）：防止单个租户占满所有 provider 容量

### 7.2 容量反压

HubCenter → Hub 的反压信号：
- 当 HubCenter 的 provider 全部熔断或队列满时，返回 `503 + Retry-After: N`
- Hub 侧收到 503 后，对终端用户返回"算力繁忙，请稍后重试"

---

## 8. API 接口汇总

### 8.1 HubCenter 新增接口

| Method | Path | 描述 |
|--------|------|------|
| POST | `/api/llm/v1/chat/completions` | LLM 代理（Hub 调用） |
| GET | `/api/llm/v1/authorization` | 查询租户授权状态 |
| POST | `/api/llm/v1/bind` | 租户节点绑定 |
| GET | `/api/llm/v1/usage` | 查询租户用量统计 |
| GET | `/api/admin/llm/providers` | 管理：列出 providers |
| POST | `/api/admin/llm/providers` | 管理：添加 provider |
| PUT | `/api/admin/llm/providers/:id` | 管理：更新 provider |
| DELETE | `/api/admin/llm/providers/:id` | 管理：删除 provider |
| GET | `/api/admin/llm/service-groups` | 管理：列出服务组 |
| POST | `/api/admin/llm/service-groups` | 管理：创建服务组 |
| PUT | `/api/admin/llm/service-groups/:id` | 管理：更新服务组 |
| GET | `/api/admin/llm/authorizations` | 管理：列出所有租户授权 |
| POST | `/api/admin/llm/authorizations` | 管理：创建/更新租户授权 |
| GET | `/api/admin/llm/stats` | 管理：用量统计报表 |
| GET | `/api/cardstore/types` | 卡型列表 |
| POST | `/api/cardstore/purchase` | 购买下单 |
| POST | `/api/cardstore/payment-callback` | 支付回调 |

### 8.2 Hub 侧改动接口

| 改动 | 描述 |
|------|------|
| Provider 列表新增 `maclaw_official` | 始终可见 |
| 服务商管理 UI 受 `AllowExternalProviders` 控制 | 未授权时锁定 |
| 心跳同步授权状态 | 新增字段 |

---

## 9. 数据存储

### 9.1 HubCenter 新增表

```sql
-- LLM Provider 配置
CREATE TABLE llm_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    api_url TEXT NOT NULL,
    api_key_encrypted TEXT NOT NULL,
    protocol TEXT NOT NULL DEFAULT 'openai',
    models TEXT,           -- JSON array
    capability_tags TEXT,  -- JSON array
    priority INTEGER DEFAULT 0,
    max_concurrency INTEGER DEFAULT 10,
    config_json TEXT,      -- 其他配置
    created_at DATETIME,
    updated_at DATETIME
);

-- LLM 服务组
CREATE TABLE llm_service_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    models_json TEXT NOT NULL,   -- JSON: []ModelConfig
    created_at DATETIME,
    updated_at DATETIME
);

-- 租户算力授权（一个租户可有多条，绑定不同服务组）
CREATE TABLE llm_tenant_authorizations (
    id TEXT PRIMARY KEY,
    hub_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    admin_email TEXT NOT NULL,
    service_group_id TEXT NOT NULL,
    credits_total REAL NOT NULL DEFAULT 0,
    credits_used REAL NOT NULL DEFAULT 0,
    starts_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    allow_external_providers INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'card',
    card_order_id TEXT,
    bound_node_id TEXT,
    bound_at DATETIME,
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME,
    updated_at DATETIME
);
CREATE INDEX idx_auth_hub_tenant ON llm_tenant_authorizations(hub_id, tenant_id);
CREATE INDEX idx_auth_status ON llm_tenant_authorizations(status, expires_at);

-- 用量记录
CREATE TABLE llm_usage_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    model TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    credits_deducted REAL NOT NULL,
    cache_hit INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL
);
CREATE INDEX idx_usage_hub_tenant_time ON llm_usage_records(hub_id, tenant_id, created_at);

-- Card Store 卡型定义（管理员动态创建）
CREATE TABLE llm_card_types (
    id TEXT PRIMARY KEY,
    service_group_id TEXT NOT NULL,
    label TEXT NOT NULL,
    credits REAL NOT NULL,
    period TEXT NOT NULL,        -- "month" / "quarter" / "year"
    price_rmb REAL NOT NULL,
    template TEXT NOT NULL DEFAULT 'gradient_blue',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME
);

-- Card Store 订单
CREATE TABLE llm_card_orders (
    id TEXT PRIMARY KEY,
    card_type_id TEXT NOT NULL,
    admin_email TEXT NOT NULL,
    hub_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    credits REAL NOT NULL,
    service_group_id TEXT NOT NULL,
    period TEXT NOT NULL,
    price_rmb REAL NOT NULL,
    payment_mode TEXT NOT NULL,   -- "personal_semimanual" / "alipay_direct"
    status TEXT NOT NULL DEFAULT 'pending',
    payment_ref TEXT,
    confirmed_by TEXT,
    created_at DATETIME,
    paid_at DATETIME,
    activated_at DATETIME
);

-- 节点绑定（HA）
CREATE TABLE llm_node_bindings (
    hub_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    bound_at DATETIME NOT NULL,
    last_active DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    PRIMARY KEY (hub_id, tenant_id)
);
```

---

## 10. 实现优先级

| Phase | 内容 | 依赖 |
|-------|------|------|
| P0 | corelib/llmpool 共享模块抽取 | 无 |
| P1 | HubCenter LLM provider + 服务组管理（admin API） | P0 |
| P2 | HubCenter LLM 代理接口 + 租户授权验证 | P1 |
| P3 | Hub "MaClaw 官方" provider + 授权同步 | P2 |
| P4 | HubCenter 使用统计 + 计费扣减 | P2 |
| P5 | HA 防双花（节点绑定 + 同步） | P2 |
| P6 | Card Store（卡型 + 购买 + 支付回调） | P4 |
| P7 | LLM 缓存提升到 corelib + HubCenter 缓存集成 | P0 |
| P8 | 接入容量管理增强（per-tenant 限流） | P2 |


---

## 11. 安全与健壮性改进（Review 阶段应用）

### 11.1 安全

- **API Key 不回传**：Admin API 列出 Provider 时，响应中不包含 API Key 原文，只返回 `has_api_key` 标志
- **算力商店身份验证**：`CreateOrder` 调用 `verifyTenant` 回调验证 hub_id + tenant_id + email 合法性，防止伪造 URL 给他人充值
- **审计日志**：订单确认等关键操作通过 `auditLog` 回调记录审计轨迹

### 11.2 数据一致性

- **Credits 扣减防雪崩**：DeductCredits SQL 添加 `WHERE (total - used) >= credits` 条件，超支后标记 exhausted，不无限扣减
- **订单激活重试**：`paid` 状态下再次确认会重试 `activateOrder`，不会因一次激活失败永久卡住
- **Registry 缓存 TTL**：30 秒后自动失效重新从 DB 加载，HA 多节点间配置变更在 30s 内生效

### 11.3 可用性

- **Per-request 超时**：forwardToProvider 使用 provider 的 `UpstreamTimeoutSec`（默认 120s）做 context 超时，防止上游挂起阻塞 goroutine
- **强制非流式**：Proxy 入口移除 `stream` 参数，保证完整响应后才返回（Hub 侧自行处理客户端 SSE）
- **HA 409 处理**：节点绑定冲突返回 HTTP 409 + redirect 信息，Hub 侧对 409 计入 failover 失败计数

### 11.4 运维

- **Credits 扣减失败日志**：扣减失败时记录 WARN 日志，便于对账
- **Provider 未找到诊断**：调度时 provider ID 在 registry 中不存在时记录具体错误信息
- **缓存 key 规范化**：temperature=0 等价于不传（不纳入 hash），提高缓存命中率
- **API URL 拼接防重复**：检测 URL 是否已以 `/v1` 结尾，避免 `/v1/v1/chat/completions`
- **AccessControl 缓存清理**：`CleanupStale()` 清理 1 小时未刷新的租户条目，防止内存泄漏
