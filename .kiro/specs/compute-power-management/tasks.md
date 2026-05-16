# 实现计划：算力管理模块

## 概述

基于设计文档，将算力管理模块分解为增量实现步骤。先实现 iWorkerCloud 端的基础设施（加密存储、Provider CRUD、权限管理），再实现 iWorkerCenter 端的同步与本地管理，然后实现协议转换层，接着实现双端 Token 用量记录与费用计算，最后实现前端 UI（管理面板 + 用量统计图表）。每个步骤构建在前一步骤之上，确保无孤立代码。

## 任务

- [x] 1. iWorkerCloud 基础设施：加密存储 + Provider 数据层
  - [x] 1.1 创建 `iWorkerCloud/internal/compute/crypto.go`，实现 AES-256-GCM 加密/解密
    - 实现 `EncryptAPIKey(plaintext string, key []byte) (ciphertext, nonce []byte, err error)`
    - 实现 `DecryptAPIKey(ciphertext, nonce, key []byte) (string, error)`
    - 加密密钥从 Hub 配置文件读取或自动生成并持久化
    - _需求: 1.3_

  - [x]* 1.2 编写属性测试：API Key 加密往返
    - **Property 1: API Key 加密往返**
    - 对任意非空字符串，加密后解密应得到原始字符串
    - 测试文件：`iWorkerCloud/internal/compute/crypto_property_test.go`
    - **验证: 需求 1.3**

  - [x] 1.3 创建 `iWorkerCloud/internal/compute/types.go`，定义核心类型
    - 定义 ComputeProvider、TokenUsageRecord、CostSummary、ComputeSyncStatus 结构体
    - 定义 ValidateProvider() 验证函数（base_url HTTPS、protocol 枚举、价格非负）
    - _需求: 1.1, 1.2, 10.1, 10.2_

  - [x]* 1.4 编写属性测试：Provider 输入验证
    - **Property 3: Provider 输入验证**
    - 验证 base_url 仅 HTTPS 通过、protocol 仅支持值通过、价格仅非负通过
    - 测试文件：`iWorkerCloud/internal/compute/validation_property_test.go`
    - **验证: 需求 1.2, 10.2**

  - [x] 1.5 创建 `iWorkerCloud/internal/compute/store.go`，实现 Provider SQLite 存储层
    - 实现 CreateTable() 建表（compute_providers、center_provider_assignments）
    - 实现 CreateProvider / GetProvider / ListProviders / UpdateProvider / DeleteProvider
    - 实现 ToggleProvider（启用/禁用）
    - api_key 使用 crypto.go 加密存储，读取时解密
    - _需求: 1.1, 1.3, 1.4_

  - [x]* 1.6 编写属性测试：Provider CRUD 往返
    - **Property 2: Provider CRUD 往返**
    - 创建后读取应返回所有字段一致的记录
    - 测试文件：`iWorkerCloud/internal/compute/store_property_test.go`
    - **验证: 需求 1.1**

- [x] 2. iWorkerCloud Provider CRUD API + 连通性测试
  - [x] 2.1 创建 `iWorkerCloud/internal/httpapi/compute_handler.go`，实现 Provider CRUD HTTP 端点
    - POST /api/admin/compute/providers — 创建
    - GET /api/admin/compute/providers — 列表（api_key 替换为 has_api_key）
    - GET /api/admin/compute/providers/{id} — 详情
    - PUT /api/admin/compute/providers/{id} — 更新
    - DELETE /api/admin/compute/providers/{id} — 删除
    - POST /api/admin/compute/providers/{id}/toggle — 启用/禁用
    - _需求: 1.1, 1.2, 1.4_

  - [x]* 2.2 编写属性测试：Admin API Key 遮蔽
    - **Property 4: Admin API Key 遮蔽**
    - Admin API 返回时 api_key 为空，has_api_key 正确反映是否有 key
    - 测试文件：`iWorkerCloud/internal/compute/api_masking_property_test.go`
    - **验证: 需求 1.4**

  - [x] 2.3 实现连通性测试端点
    - POST /api/admin/compute/providers/{id}/test
    - 创建 `iWorkerCloud/internal/compute/tester.go`，发送简单 prompt 到 Provider，返回 success/failure + latency
    - 支持 OpenAI / Anthropic / Gemini 三种协议的测试请求构造
    - _需求: 1.5_

  - [x] 2.4 在 `iWorkerCloud/internal/httpapi/router.go` 注册算力管理路由
    - 注册所有 /api/admin/compute/* 路由
    - 在 `iWorkerCloud/internal/app/bootstrap.go` 中初始化 compute store 并注入 handler
    - _需求: 1.1_

- [x] 3. iWorkerCloud 算力分发 + 权限管理
  - [x] 3.1 实现 Center 算力分发 API
    - GET /api/centers/{id}/compute-providers — 返回分配的 enabled Provider 列表（含完整 api_key）
    - 复用现有 Center 认证机制（secret 校验）
    - 禁用的 Center 返回 403 CENTER_DISABLED
    - 无特定分配时返回所有 enabled Provider
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [x]* 3.2 编写属性测试：Center 认证 + 分配过滤
    - **Property 5: Center API Key 完整返回**
    - **Property 6: Center 认证**
    - **Property 7: Provider 分配过滤**
    - 测试文件：`iWorkerCloud/internal/compute/auth_property_test.go`
    - **验证: 需求 2.2, 2.3, 2.5**

  - [x] 3.3 实现权限管理 API
    - PUT /api/admin/centers/{id}/compute-permission — 授予/撤销算力自管理权限
    - 权限存储在 system_settings 中（key: compute_permission_{center_id}）
    - 撤销权限时设置 force_sync 标志
    - compute-providers API 响应中包含 compute_permission 和 force_sync 字段
    - _需求: 3.1, 3.2, 3.3, 3.5_

  - [x] 3.4 实现 Provider 分配管理
    - POST /api/admin/centers/{id}/compute-providers — 分配 Provider 给 Center
    - DELETE /api/admin/centers/{id}/compute-providers/{provider_id} — 取消分配
    - 在 center_provider_assignments 表中维护分配关系
    - _需求: 2.5_

- [x] 4. 检查点 - iWorkerCloud 算力管理 API 完整性
  - 运行 `go test ./iWorkerCloud/internal/compute/...` 和 `go test ./iWorkerCloud/internal/httpapi/...` 确保所有测试通过，如有问题请询问用户。

- [ ] 5. 协议转换层
  - [x] 5.1 创建 `iWorkerCloud/internal/compute/adapter.go`，定义 ProtocolAdapter 接口
    - 定义 ProtocolAdapter 接口：ConvertRequest、ConvertResponse、ExtractUsage
    - 实现 OpenAI 适配器（passthrough，仅设置 User-Agent）
    - _需求: 6.1, 6.2, 6.5_

  - [x] 5.2 实现 Anthropic 协议适配器
    - 创建 `iWorkerCloud/internal/compute/adapter_anthropic.go`
    - ConvertRequest：提取 system 消息到 Anthropic system 参数，设置 anthropic-version 头，设置 x-api-key 和 Authorization: Bearer 头
    - ConvertResponse：将 Anthropic content 数组转换为 OpenAI choices 格式
    - ExtractUsage：从 Anthropic usage 对象提取 input_tokens / output_tokens
    - _需求: 6.1, 6.2, 6.3_

  - [x] 5.3 实现 Gemini 协议适配器
    - 创建 `iWorkerCloud/internal/compute/adapter_gemini.go`
    - ConvertRequest：messages 转换为 contents 数组，system 映射为 systemInstruction，API Key 作为查询参数
    - ConvertResponse：将 Gemini candidates 转换为 OpenAI choices 格式
    - ExtractUsage：从 Gemini usageMetadata 提取 promptTokenCount / candidatesTokenCount
    - _需求: 6.1, 6.2, 6.4_

  - [x] 5.4 实现错误响应转换和 Token 估算回退
    - 非 200 状态码转换为 OpenAI 错误格式（error.message + error.type），保持原始 HTTP 状态码
    - Token 用量缺失时基于字符数估算，标记 estimated=true
    - _需求: 6.6, 9.3, 9.4_

  - [x]* 5.5 编写属性测试：协议转换
    - **Property 8: 协议转换往返**
    - **Property 9: Anthropic 系统消息提取**
    - **Property 10: Gemini 格式转换**
    - **Property 11: 协议错误转换**
    - **Property 12: 多协议 Token 用量提取**
    - **Property 13: Token 估算回退**
    - 测试文件：`iWorkerCloud/internal/compute/adapter_property_test.go`
    - **验证: 需求 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 9.3, 9.4**

- [x] 6. 检查点 - 协议转换层完整性
  - 运行 `go test ./iWorkerCloud/internal/compute/...` 确保所有测试通过，如有问题请询问用户。

- [ ] 7. iWorkerCenter 算力同步与本地管理
  - [x] 7.1 创建 `iWorkerCenter/internal/compute/sync.go`，实现配置同步管理器
    - 实现 SyncManager：5 分钟轮询从 Cloud 拉取 Provider 配置
    - 处理 compute_permission 和 force_sync 字段
    - Cloud 不可达时保留上次成功同步的配置，下次重试
    - 记录 ComputeSyncStatus（last_sync_at、status、error、provider_count）
    - _需求: 4.1, 4.2, 4.6, 4.7_

  - [x] 7.2 创建 `iWorkerCenter/internal/compute/source.go`，实现算力来源切换
    - 维护 Compute_Source 设置（cloud / local），默认 cloud
    - cloud 模式：使用同步的 Provider 列表，禁用本地编辑
    - local 模式：使用本地 Provider 列表，停止同步
    - 收到 force_sync 时切换回 cloud 模式，丢弃本地覆盖
    - 收到 compute_permission: true 时允许切换到 local
    - _需求: 4.1, 4.3, 4.4, 4.5, 4.6_

  - [x] 7.3 创建 `iWorkerCenter/internal/compute/local_store.go`，实现本地 Provider 管理
    - CRUD 接口：SaveLocalComputeProvider / DeleteLocalComputeProvider / ListLocalProviders
    - 存储在 settings.json 的 providers 数组中，扩展 user_agent、compute_type 字段
    - 实现 TestComputeProvider 连通性测试
    - _需求: 5.1, 5.2, 5.3_

  - [x] 7.4 在 hubcenter HTTP API 中注册算力管理端点
    - 在 `iWorkerCenter/internal/httpapi/router.go` 注册内部 API 路由
    - GET /api/compute/source — 获取当前算力来源
    - PUT /api/compute/source — 切换算力来源
    - GET /api/compute/providers — 获取当前生效的 Provider 列表
    - POST /api/compute/sync — 手动触发同步
    - CRUD /api/compute/local-providers — 本地 Provider 管理（local 模式）
    - POST /api/compute/test — 测试 Provider 连通性
    - 在 bootstrap 中初始化 SyncManager 并启动后台同步
    - _需求: 4.1, 5.1, 7.3, 7.4_

- [x] 8. 检查点 - iWorkerCenter 算力管理完整性
  - 运行 `go test ./iWorkerCenter/internal/compute/...` 确保所有测试通过，如有问题请询问用户。

- [ ] 9. Token 用量记录（双端）
  - [x] 9.1 创建 `iWorkerCloud/internal/compute/usage_store.go`，实现 Cloud 端用量存储
    - 建表：token_usage_records（含 center_id、diworker_id 索引）
    - 实现 RecordUsage(record TokenUsageRecord) error
    - 实现 QueryUsage(filter UsageFilter) ([]TokenUsageRecord, error)
    - _需求: 9.1, 9.5_

  - [x] 9.2 在 iWorkerCloud 转发层集成用量记录
    - 在 Cloud 转发 LLM 请求并收到响应后，调用 ExtractUsage 提取 token 用量
    - 创建 TokenUsageRecord 并写入 usage_store
    - 用量缺失时使用字符估算，标记 estimated=true
    - _需求: 9.1, 9.3, 9.4_

  - [x] 9.3 创建 `iWorkerCenter/internal/compute/usage_store.go`，实现 Center 端用量存储
    - 建表：center_token_usage（含 diworker_id 索引）
    - 实现 RecordUsage / QueryUsage
    - _需求: 9.2, 9.6_

  - [x] 9.4 在 iWorkerCenter 转发层集成用量记录
    - DiWorker 请求经 Center 转发后，记录本地 TokenUsageRecord
    - 无论 cloud 模式（经 Cloud 转发）还是 local 模式（直连），都在 Center 本地记录
    - _需求: 9.2, 9.7_

  - [x]* 9.5 编写属性测试：Token 用量记录
    - **Property 14: Token 用量记录完整性**
    - 验证记录包含所有必填字段，total_tokens = input_tokens + output_tokens
    - 测试文件：`iWorkerCloud/internal/compute/usage_property_test.go`
    - **验证: 需求 9.1, 9.2**

- [ ] 10. 费用计算引擎
  - [x] 10.1 创建 `iWorkerCloud/internal/compute/cost_engine.go`，实现 Cloud 端费用计算
    - 实现费用公式：input_cost = input_tokens × input_price_per_mtoken / 1,000,000
    - 实现 GenerateDailySummary / GenerateMonthlySummary
    - 建表：cost_summaries，记录 input_price_used / output_price_used 快照
    - _需求: 11.1, 11.2, 11.5_

  - [x]* 10.2 编写属性测试：费用计算
    - **Property 15: 费用计算公式**
    - **Property 16: 费用聚合一致性**
    - **Property 17: 历史价格不可变性**
    - 测试文件：`iWorkerCloud/internal/compute/cost_property_test.go`
    - **验证: 需求 11.1, 11.2, 10.5**

  - [x] 10.3 实现 Cloud 端定时汇总任务
    - 每日 00:05 UTC 生成前一天的 daily 汇总
    - 每月 1 日生成上月的 monthly 汇总
    - 在 bootstrap.go 中启动定时任务
    - _需求: 11.5_

  - [x] 10.4 实现 Cloud 端费用统计查询 API
    - GET /api/stats/center-costs?center_id={id}&period=daily|monthly&start={date}&end={date}
    - 不指定 center_id 时返回所有 Center 的汇总 + 分 Center 明细
    - GET /api/centers/{id}/monthly-usage?month={YYYY-MM} — 供 Center 对账拉取
    - _需求: 11.3, 11.4, 12.6_

  - [x] 10.5 创建 `iWorkerCenter/internal/compute/cost_engine.go`，实现 Center 端费用计算
    - 复用相同费用公式，按 diworker_id 维度聚合
    - 建表：center_cost_summaries
    - 实现 GenerateDailySummary / GenerateMonthlySummary
    - 每日 00:05 本地时间生成 daily 汇总，每月 1 日生成 monthly 汇总
    - _需求: 12.1, 12.2, 12.4_

  - [x] 10.6 实现 Center 端月度对账
    - 月度汇总时从 Cloud 拉取 GET /api/centers/{id}/monthly-usage?month={YYYY-MM}
    - 对比本地月度 token 总量与 Cloud 月度 token 总量，计算差异
    - 拉取失败时显示"对账数据不可用"，不影响本地统计
    - _需求: 12.6_

  - [x]* 10.7 编写属性测试：月度对账差异
    - **Property 18: 月度对账差异计算**
    - 差异值 = |本地总量 - Cloud 总量|
    - 测试文件：`iWorkerCenter/internal/compute/reconciliation_property_test.go`
    - **验证: 需求 12.6**

  - [x] 10.8 在 hubcenter HTTP API 中注册费用统计端点
    - GET /api/compute/cost/diworkers — DiWorker 费用列表
    - GET /api/compute/cost/diworkers/{id} — 单个 DiWorker 费用明细（含 per-provider 分解）
    - 支持 period=daily|monthly、start、end 查询参数
    - _需求: 12.1, 12.2, 12.3, 12.5_

- [x] 11. 检查点 - 双端用量记录与费用计算完整性
  - 运行 `go test ./iWorkerCloud/internal/compute/...` 和 `go test ./iWorkerCenter/internal/compute/...` 确保所有测试通过，如有问题请询问用户。

- [x] 12. iWorkerCloud 管理面板 - 算力管理页面
  - [x] 12.1 修改 `iWorkerCloud/web/admin/index.html`，新增"算力管理"导航入口
    - 在侧边栏 nav 中添加"算力管理"按钮
    - 创建算力管理页面容器，包含 Provider 管理和 Center 权限两个区域
    - _需求: 8.1_

  - [x] 12.2 实现 Provider 管理 UI
    - Provider 列表表格：name、protocol、compute_type、user_agent、enabled 状态、操作按钮（编辑、删除、测试、启用/禁用）
    - 添加/编辑表单：name、base_url、api_key（密码输入）、protocol（下拉）、user_agent（文本 + 预设 openclaw / claude-code/2.0.0）、compute_type（下拉）、model、priority、description、input_price_per_mtoken、output_price_per_mtoken、enabled 开关
    - 测试连通性按钮：调用 test API，显示结果（成功/失败、延迟、错误信息）
    - _需求: 8.2, 8.4, 8.5, 10.4_

  - [x] 12.3 实现 Center 权限管理 UI
    - Center 列表：显示所有注册的 iWorkerCenter，当前 Compute_Permission 状态
    - 每个 Center 行提供权限开关（toggle）
    - _需求: 8.3, 3.4_

- [x] 13. iWorkerCloud 管理面板 - 用量统计子页
  - [x] 13.1 在算力管理页面新增"用量统计"子标签
    - 日期范围选择器 + 周期选择器（daily / monthly）
    - 顶部汇总行：选定周期内所有 Center 的总费用
    - _需求: 13.1, 13.2, 13.3_

  - [x] 13.2 实现 Center 费用统计表格
    - 列：center_name、total_input_tokens、total_output_tokens、total_tokens、input_cost、output_cost、total_cost
    - 点击某行展开 per-provider 明细
    - _需求: 13.1, 13.7_

  - [x] 13.3 实现趋势图表（使用内联 JS 图表或轻量图表库）
    - daily 模式：折线图显示每日 token 用量和费用趋势，支持按 Center 切换
    - monthly 模式：柱状图显示月度 token 用量和费用，支持月度对比
    - 悬停 tooltip 显示精确数值（日期、token 数、费用）
    - _需求: 13.8, 13.9, 13.12_

- [x] 14. iWorkerCenter 管理面板 - 算力管理标签页
  - [x] 14.1 修改 `iWorkerCenter/web/admin/index.html`，新增"算力管理"导航标签
    - 在导航栏中添加"算力管理"标签，位于"模型调度"之后
    - 显示当前 Compute_Source 模式（cloud / local）指示器
    - _需求: 7.1, 7.2_

  - [x] 14.2 实现 cloud 模式 UI
    - 只读 Provider 列表，标注"来自 iWorkerCloud"
    - 显示最后同步时间、同步状态（success/failure/pending）
    - "立即同步"按钮
    - 无权限时切换 local 模式显示提示："需要 iWorkerCloud 管理员授予算力自管理权限"
    - _需求: 7.3, 7.5, 5.4_

  - [x] 14.3 实现 local 模式 UI
    - 可编辑 Provider 列表：添加、编辑、删除、启用/禁用、测试连通性
    - 每个 Provider 显示 protocol、compute_type、user_agent、enabled 状态、最后测试结果
    - _需求: 7.4, 7.6_

- [x] 15. iWorkerCenter 管理面板 - 用量统计子页
  - [x] 15.1 在算力管理标签页新增"用量统计"子标签
    - 日期范围选择器 + 周期选择器（daily / monthly）
    - _需求: 13.4, 13.5_

  - [x] 15.2 实现 DiWorker 费用统计表格
    - 列：diworker_name、total_input_tokens、total_output_tokens、total_tokens、input_cost、output_cost、total_cost、request_count
    - 点击某行展开 per-provider 明细
    - 月度对账指示器（本地 vs Cloud token 差异）
    - _需求: 13.4, 13.6, 12.3, 12.5, 12.6_

  - [x] 15.3 实现趋势图表
    - daily 模式：折线图显示每日 token 用量和费用趋势，支持按 DiWorker 切换
    - monthly 模式：柱状图显示月度 token 用量和费用
    - 悬停 tooltip 显示精确数值
    - _需求: 13.10, 13.11, 13.12_

- [x] 16. 最终检查点 - 全模块集成验证
  - 运行 `go test ./iWorkerCloud/internal/compute/...` 和 `go test ./iWorkerCenter/internal/compute/...` 确保所有测试通过
  - 确保所有 API 路由已注册，前端页面可正常加载
  - 如有问题请询问用户。

## 备注

- 标记 `*` 的子任务为可选测试任务，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号，确保需求全覆盖
- 属性测试验证设计文档中定义的 18 个正确性属性
- iWorkerCloud 代码位于 `iWorkerCloud/` 目录，iWorkerCenter 代码位于 `iWorkerCenter/` 目录
- 前端使用内嵌 HTML（iWorkerCloud/web/admin/index.html 和 iWorkerCenter/web/admin/index.html），与现有项目风格一致
