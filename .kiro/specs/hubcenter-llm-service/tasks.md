# HubCenter LLM 算力服务接入 — 任务拆分

## T1: corelib/llmpool 共享模块抽取

- **描述**：将 Hub 现有的 LLM 服务组类型定义、Provider 调度逻辑、缓存核心、容量管理（并发控制+熔断）提取到 `corelib/llmpool/` 共享包，Hub 和 HubCenter 复用
- **涉及文件**：
  - 新增 `corelib/llmpool/types.go` — ServiceGroup、ProviderConfig、ModelConfig 等共享类型
  - 新增 `corelib/llmpool/dispatcher.go` — Provider 调度策略（capability matching、priority、resolution tier）
  - 新增 `corelib/llmpool/cache.go` — 从 `hub/internal/llmcache/cache.go` 提取核心逻辑
  - 新增 `corelib/llmpool/ratelimit.go` — 从 `corelib/llm_endpoint_proxy.go` 提取并发控制+熔断
  - 新增 `corelib/llmpool/usage.go` — 用量记录接口定义
  - 修改 `hub/internal/llmcache/cache.go` — 改为委托 `corelib/llmpool/cache.go`
  - 修改 `corelib/llm_endpoint_proxy.go` — 改为委托 `corelib/llmpool/ratelimit.go`
- **依赖**：无
- **优先级**：P0
- **工作量**：核心是代码搬迁+接口抽象，逻辑不变。约 1-2 天

---

## T2: corelib/cardstore 支付流程共享模块抽取

- **描述**：将 Hub Card Store 的支付流程核心（订单状态机、personal_semimanual 手工确认、alipay_direct 回调验签）提取到 `corelib/cardstore/`，定义"充值目标"接口，Hub 和 HubCenter 各自实现
- **涉及文件**：
  - 新增 `corelib/cardstore/order.go` — 订单状态机（pending→paid→activated→cancelled）
  - 新增 `corelib/cardstore/semimanual.go` — 二维码展示 + 手工确认逻辑
  - 新增 `corelib/cardstore/alipay.go` — 支付宝直连对接 + 回调验签
  - 新增 `corelib/cardstore/types.go` — 共享类型（Order、PaymentConfig 等）
  - 修改 `hub/internal/httpapi/card_store_handlers.go` — 改为委托 corelib/cardstore
- **依赖**：无
- **优先级**：P0
- **工作量**：从 Hub 搬代码 + 抽接口。约 1-2 天

---

## T3: Hub 移除 payment_fm 支付方式

- **描述**：从 Hub Card Store 中移除已废弃的 `payment_fm` 支付方式，前后端同步清理
- **涉及文件**：
  - `hub/web/admin/card-store-tab.js` — 移除 payment_fm 选项 + UI
  - `hub/web/admin/index.html` — 移除 payment_fm 相关表单元素
  - `hub/internal/httpapi/card_store_handlers.go` — 移除 payment_fm 逻辑分支
  - `hub/internal/httpapi/card_store_handlers_test.go` — 更新/移除相关测试
- **依赖**：T2（先抽共享层再清理）
- **优先级**：P1
- **工作量**：约 0.5 天

---

## T4: HubCenter LLM Provider + 服务组管理（后端）

- **描述**：HubCenter 新增 LLM Provider 配置和服务组管理，提供 admin API
- **涉及文件**：
  - 新增 `hubcenter/internal/llmservice/registry.go` — 服务组 + Provider 注册表
  - 新增 `hubcenter/internal/llmservice/admin_handlers.go` — CRUD API handlers
  - 新增 `hubcenter/internal/store/llm_tables.go` — DB schema（llm_providers + llm_service_groups）
  - 修改 `hubcenter/internal/httpapi/routes.go` — 注册新路由
- **依赖**：T1（使用 corelib/llmpool 共享类型）
- **优先级**：P1
- **工作量**：约 1.5 天

---

## T5: HubCenter LLM 代理接口

- **描述**：实现 `/api/llm/v1/chat/completions` 代理端点，接收 Hub 请求，内部按服务组策略选 Provider 转发
- **涉及文件**：
  - 新增 `hubcenter/internal/llmservice/proxy.go` — 代理请求处理（认证→授权验证→缓存查找→调度→转发→记录用量）
  - 新增 `hubcenter/internal/llmservice/proxy_handlers.go` — HTTP handler
  - 修改 `hubcenter/internal/httpapi/routes.go` — 注册 LLM 代理路由
- **依赖**：T4（需要 Provider + 服务组配置）、T1（使用 llmpool 调度+缓存+限流）
- **优先级**：P1
- **工作量**：约 2 天

---

## T6: HubCenter 租户授权管理

- **描述**：实现租户 LLM 算力授权的 CRUD + 验证逻辑（credits 余额检查 + 有效期检查 + 服务组匹配 + 扣减）
- **涉及文件**：
  - 新增 `hubcenter/internal/llmservice/auth.go` — 授权验证 + credits 扣减
  - 新增 `hubcenter/internal/llmservice/auth_handlers.go` — 授权管理 admin API
  - 新增 `hubcenter/internal/store/llm_tables.go` — llm_tenant_authorizations 表
  - 修改 `hubcenter/internal/httpapi/routes.go` — 注册授权管理路由
- **依赖**：T4
- **优先级**：P1
- **工作量**：约 1.5 天

---

## T7: HubCenter Card Store（动态卡型 + 购买流程）

- **描述**：实现 HubCenter 的 Card Store——管理员动态创建卡型（绑定服务组 + 额度 + 有效期 + 价格 + 图案模板），Hub 租户管理员购买后自动充到租户授权
- **涉及文件**：
  - 新增 `hubcenter/internal/cardstore/store.go` — 卡型 CRUD + 订单管理
  - 新增 `hubcenter/internal/cardstore/handlers.go` — API handlers（卡型管理 + 购买 + 订单查询 + 手工确认）
  - 新增 `hubcenter/internal/cardstore/activate.go` — 支付确认后激活逻辑（创建/追加 TenantLLMAuthorization）
  - 新增 `hubcenter/internal/store/llm_tables.go` — llm_card_types + llm_card_orders 表
  - 修改 `hubcenter/internal/httpapi/routes.go` — 注册 cardstore 路由
- **依赖**：T2（复用 corelib/cardstore 支付流程）、T6（激活时写入 TenantLLMAuthorization）
- **优先级**：P2
- **工作量**：约 2 天

---

## T8: Hub 新增"MaClaw 官方" Provider

- **描述**：Hub 的 LLM provider 列表新增 `maclaw_official`，将 LLM 请求转发到 HubCenter 代理接口
- **涉及文件**：
  - 新增 `hub/internal/llmservice/maclaw_provider.go` — "MaClaw 官方" provider 实现
  - 修改 `hub/internal/llmservice/registry.go` — 内置 maclaw_official 到 provider 列表
  - 修改 `hub/internal/llmservice/service.go` — 请求路由支持 maclaw_official
- **依赖**：T5（HubCenter 代理接口已就绪）
- **优先级**：P2
- **工作量**：约 1 天

---

## T9: Hub 服务商列表权限控制（AllowExternalProviders）

- **描述**：Hub 根据 HubCenter 返回的 `AllowExternalProviders` 标志控制租户能否自行接入第三方服务商
- **涉及文件**：
  - 修改 `hub/internal/llmservice/service.go` — 服务商列表过滤逻辑
  - 修改 `hub/internal/httpapi/` — admin API 中"添加服务商"接口增加授权检查
  - 修改 `hub/web/admin/` — 前端未授权时锁定"添加服务商"入口
- **依赖**：T8、T6
- **优先级**：P2
- **工作量**：约 1 天

---

## T10: Hub↔HubCenter 授权状态同步

- **描述**：Hub 通过心跳或主动查询同步租户的授权状态（credits 余额、AllowExternalProviders、有效期）
- **涉及文件**：
  - 修改 Hub 心跳逻辑 — 请求时附带 tenant_id 列表，HubCenter 返回各租户授权状态
  - 或新增 `GET /api/llm/v1/authorization` 查询接口（HubCenter 侧）
  - Hub 本地缓存授权状态（避免每次请求都查 HubCenter）
- **依赖**：T6、T8
- **优先级**：P2
- **工作量**：约 1 天

---

## T11: HubCenter 使用统计

- **描述**：实现按 Hub→租户→模型 维度的 token 用量统计 + 日/周/月汇总 + 缓存命中率
- **涉及文件**：
  - 新增 `hubcenter/internal/llmservice/stats.go` — 用量记录写入 + 汇总查询
  - 新增 `hubcenter/internal/llmservice/stats_handlers.go` — 统计查询 API
  - 新增 `hubcenter/internal/store/llm_tables.go` — llm_usage_records 表 + 索引
  - 修改 `hubcenter/internal/llmservice/proxy.go` — 每次请求完成后写入用量记录
- **依赖**：T5
- **优先级**：P3
- **工作量**：约 1.5 天

---

## T12: HA 防双花（节点绑定 + 同步）

- **描述**：实现租户级 HubCenter 节点绑定机制——Hub pin 到单一节点，节点间轻量同步 binding 表，故障转移时冷却+重绑
- **涉及文件**：
  - 新增 `hubcenter/internal/ha/llm_binding.go` — 绑定记录管理 + 续约 + 过期清理
  - 新增 `hubcenter/internal/ha/llm_binding_sync.go` — 节点间 30-60s 周期同步
  - 新增 `hubcenter/internal/store/llm_tables.go` — llm_node_bindings 表
  - 修改 `hubcenter/internal/llmservice/proxy.go` — 请求处理前检查绑定 + 拒绝非 bound 节点请求
  - 修改 Hub 的 maclaw_provider.go — pin 节点持久化 + 故障检测 + 冷却重选
- **依赖**：T5、T8
- **优先级**：P3
- **工作量**：约 2 天

---

## T13: HubCenter LLM 缓存集成

- **描述**：在 HubCenter LLM 代理中启用请求级缓存（复用 corelib/llmpool/cache），缓存命中不扣/折扣扣 credits
- **涉及文件**：
  - 修改 `hubcenter/internal/llmservice/proxy.go` — 请求前查缓存、请求后写缓存
  - 新增缓存配置（内存上限、磁盘 TTL）
  - 修改 credits 扣减逻辑 — 缓存命中时不扣减
- **依赖**：T1（corelib/llmpool/cache）、T5
- **优先级**：P3
- **工作量**：约 0.5 天

---

## T14: 接入容量管理增强（per-tenant 限流）

- **描述**：在 HubCenter 代理层新增 per-tenant 速率限制（防止单个租户占满所有 provider 容量），复用 corelib/llmpool/ratelimit + 反压 503
- **涉及文件**：
  - 修改 `hubcenter/internal/llmservice/proxy.go` — per-tenant 并发/QPS 检查
  - 修改 `corelib/llmpool/ratelimit.go` — 新增 tenant-scoped limiter
  - Hub maclaw_provider.go — 处理 503 + Retry-After 反压
- **依赖**：T1、T5
- **优先级**：P3
- **工作量**：约 1 天

---

## T15: HubCenter Card Store 前端 + 管理后台

- **描述**：HubCenter admin 面板新增卡型管理页面 + 购买商城页面（可从 Hub 的 card-store-tab.js 移植简改）
- **涉及文件**：
  - 新增 `hubcenter/web/admin/cardstore-tab.js`（或对应前端框架文件）
  - 新增卡面图案模板 SVG/CSS
  - 新增订单管理页面（查询 + 手工确认按钮）
- **依赖**：T7（后端 API 就绪）
- **优先级**：P3
- **工作量**：约 2 天

---

## 依赖关系图

```
T1 (corelib/llmpool) ──┬──→ T4 (HubCenter Provider+服务组) ──→ T5 (LLM代理) ──┬──→ T8 (Hub MaClaw官方)
                       │                                                        │     ↓
T2 (corelib/cardstore)─┤──→ T6 (租户授权) ──→ T7 (Card Store) ──→ T15 (前端)    ├──→ T9 (服务商权限控制)
                       │                                                        ├──→ T10 (授权同步)
                       └──→ T3 (移除payment_fm)                                 ├──→ T11 (使用统计)
                                                                                ├──→ T12 (HA防双花)
                                                                                ├──→ T13 (缓存集成)
                                                                                └──→ T14 (容量管理)
```

## 总工作量估算

| Phase | 任务 | 估算 |
|-------|------|------|
| P0 | T1 + T2 | 2-4 天 |
| P1 | T3 + T4 + T5 + T6 | 5-6 天 |
| P2 | T7 + T8 + T9 + T10 | 5 天 |
| P3 | T11 + T12 + T13 + T14 + T15 | 7 天 |
| **总计** | | **19-22 天** |
