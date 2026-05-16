# 实施计划：SkillHub 自主发现与安装

## 概述

基于需求文档和设计文档，将 SkillHub 自主发现与安装功能拆分为增量实施任务。实施顺序：Hub 侧 API → 客户端 SkillHubClient → 能力缺口检测 → 安全集成 → 前端界面。每个阶段有检查点确保增量验证。

## 任务

- [x] 1. Hub 侧 — Skill 数据模型与存储层
  - [x] 1.1 创建 `hub/internal/skill/types.go`，定义 Hub 侧 Skill 数据类型
    - 定义 `HubSkillMeta`（ID、Name、Description、Tags、Version、Author、TrustLevel、Downloads、CreatedAt、UpdatedAt）
    - 定义 `HubSkillFull`（嵌入 HubSkillMeta + Triggers、Steps、Manifest）
    - 定义 `SkillManifest`（MinMaclawVersion、RequiredMCP、Permissions）
    - 定义 `SkillSearchResult`（Skills、Total、Page）
    - _需求: 1.1, 1.2, 1.4_

  - [x] 1.2 创建 `hub/internal/skill/store.go`，实现 SkillStore
    - 实现 JSON 文件存储：每个 Skill 一个 JSON 文件，存放在配置的目录下
    - 实现 `NewSkillStore(dir string)` 构造函数，启动时加载所有 Skill 到内存索引
    - 实现 `Search(query, tags, page)` 方法：对 name、description、tags 进行关键词匹配，支持中文
    - 实现 `Get(id)` 方法：返回完整 Skill 定义
    - 实现 `Publish(skill)` 方法：写入 JSON 文件并更新索引
    - 实现 `RebuildIndex()` 方法：重新扫描目录构建索引
    - 分页：每页 20 条
    - _需求: 1.1, 1.2, 1.3, 1.5, 1.6_

  - [ ]* 1.3 为 SkillStore 编写单元测试
    - 测试搜索（英文、中文关键词）
    - 测试分页逻辑
    - 测试 Publish 和 Get 的 round-trip
    - _需求: 1.1, 1.5, 1.6_

- [x] 2. Hub 侧 — Skill Catalog HTTP API
  - [x] 2.1 创建 `hub/internal/httpapi/skill_handlers.go`，实现 HTTP 端点
    - `GET /api/v1/skills/search?q=xxx&tags=xxx&page=1` — 搜索 Skill
    - `GET /api/v1/skills/{id}` — 获取单个 Skill 完整元数据
    - `GET /api/v1/skills/{id}/download` — 下载 Skill JSON 包
    - `GET /api/v1/skills/popular` — 热门推荐（按 downloads 排序，取 top 20）
    - `POST /api/v1/skills` — 发布 Skill（需认证，设置 trust_level="community"）
    - 所有端点返回 JSON，错误时返回 `{"error": "message"}` 格式
    - _需求: 1.1, 1.2, 1.3, 1.4_

  - [x] 2.2 在 `hub/internal/httpapi/router.go` 中注册 Skill 路由
    - 将 SkillHandlers 注册到现有路由器
    - 在 `hub/internal/app/bootstrap.go` 中初始化 SkillStore 并注入 SkillHandlers
    - _需求: 1.1_

  - [ ]* 2.3 为 Skill HTTP API 编写集成测试
    - 测试搜索、获取、下载端点
    - 测试发布流程
    - _需求: 1.1-1.6_

- [x] 3. 检查点 — Hub 侧 Skill Catalog API
  - 启动 Hub 服务，手动测试 API 端点可用性
  - 发布几个测试 Skill，验证搜索和下载功能

- [x] 4. 客户端 — SkillHubClient 核心
  - [x] 4.1 创建 `skillhub_client.go`，实现 SkillHubClient
    - 定义 `HubSkillMeta`（客户端侧，增加 HubURL 字段标记来源）
    - 定义 `cachedSearchResult` 缓存结构
    - 实现 `NewSkillHubClient(app *App)` 构造函数
    - 实现 `Search(ctx, query)` 方法：
      - 先检查缓存（TTL 5 分钟）
      - 读取 `AppConfig.SkillHubURLs`，并发向所有 Hub 发起 GET 请求
      - 每个 Hub 超时 8 秒
      - 调用 `mergeResults` 去重合并（按 Skill ID，保留延迟最低的来源）
      - 全部不可达时返回空列表
    - 实现 `Install(ctx, skillID, hubURL)` 方法：
      - 从指定 Hub 下载 Skill JSON
      - 解析为 NLSkillEntry，设置 source="hub"、hub_skill_id、hub_version、trust_level
      - 下载失败时自动回退到其他 Hub
    - 实现 `selectBestHub` 和 `mergeResults` 辅助函数
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 8.1, 8.2, 8.3_

  - [x] 4.2 实现 Hub Skill 更新检查
    - 实现 `CheckUpdate(ctx, skillID, currentVersion)` 方法
    - 查询 Hub 上该 Skill 的最新版本，比较版本号
    - _需求: 6.3_

  - [ ]* 4.3 为 SkillHubClient 编写单元测试
    - 使用 httptest.Server 模拟 Hub API
    - 测试并发查询、缓存命中、超时处理、去重合并
    - 测试下载失败回退逻辑
    - _需求: 2.1-2.6, 8.1-8.3_

- [x] 5. 客户端 — 推荐与预热
  - [x] 5.1 在 SkillHubClient 中实现推荐功能
    - 实现 `RefreshRecommendations(ctx)` 方法：调用 `/api/v1/skills/popular` 拉取热门 Skill
    - 实现 `GetRecommendations()` 方法：返回本地缓存的推荐列表
    - 推荐索引存储在内存中，不持久化
    - _需求: 7.1, 7.2_

  - [x] 5.2 在 App 启动时触发推荐预热
    - 在 `app.go` 的 `startup()` 或 `ensureRemoteInfra()` 中，异步调用 `RefreshRecommendations`
    - 设置 24 小时定时刷新（使用 `time.Ticker`）
    - _需求: 7.1_

  - [x] 5.3 扩展 ToolRouter，集成推荐 Skill 匹配
    - 在 `Route()` 方法中，额外检查推荐 Skill 索引
    - 如推荐 Skill 与用户意图匹配度高，在工具列表中追加 `search_and_install_skill` 工具提示
    - _需求: 7.3, 7.4_

- [x] 6. 检查点 — SkillHubClient
  - 配置测试 Hub 地址，验证搜索和下载功能
  - 验证多 Hub 并发查询和去重逻辑

- [x] 7. Agent 层 — 能力缺口检测与自动安装
  - [x] 7.1 创建 `capability_gap_detector.go`，实现 CapabilityGapDetector
    - 实现 `NewCapabilityGapDetector(...)` 构造函数
    - 实现 `Detect(llmResponse string) bool` 方法：
      - 调用 LLM 判断响应是否表明能力缺口（不硬编码规则）
      - 如 LLM 未配置，使用简单启发式（检查"无法"、"不支持"等关键词）
    - 实现 `Resolve(ctx, userMessage, history, sendStatus)` 方法：
      - 步骤 1：调用 LLM 提炼能力需求描述作为搜索查询
      - 步骤 2：调用 SkillHubClient.Search
      - 步骤 3：调用 LLM 从候选中选择最匹配的 Skill
      - 步骤 4：下载 Skill
      - 步骤 5：安全审查（调用 RiskAssessor.AssessSkill）
      - 步骤 6：注册到 SkillExecutor
      - 步骤 7：立即执行并返回结果
    - 每个步骤通过 sendStatus 回调向用户发送进度
    - _需求: 3.1, 3.2, 3.3, 3.4, 3.5, 4.1, 4.2, 4.3, 4.4, 4.5_

  - [x] 7.2 扩展 RiskAssessor，新增 Skill 级别风险评估
    - 在 `risk_assessor.go` 中新增 `AssessSkill(skill *NLSkillEntry, trustLevel string) RiskAssessment`
    - 扫描所有 steps，取最高风险等级
    - trust_level 调整：official 降级 medium→low，unknown 升级 low→medium
    - _需求: 5.1, 5.2, 5.3, 5.4_

  - [x] 7.3 集成到 IMMessageHandler
    - 在 `IMMessageHandler` 中添加 `capabilityGapDetector` 字段
    - 在 `runAgentLoop` 的 LLM 推理完成后，调用 `Detect` + `Resolve`
    - 安装成功时向用户发送通知（Skill 名称、描述、来源 Hub）
    - 安装失败时报告原因并回退到正常流程
    - _需求: 3.4, 4.3, 4.4, 4.5_

  - [ ]* 7.4 为 CapabilityGapDetector 编写单元测试
    - 测试 Detect 的判断逻辑
    - 测试 Resolve 的完整流程（使用 mock Hub 和 mock LLM）
    - 测试安全审查拒绝场景
    - _需求: 3.1-3.5, 4.1-4.5, 5.1-5.5_

- [x] 8. 安全与审计集成
  - [x] 8.1 在 AuditLog 中记录 Hub Skill 安装操作
    - 新增 `AuditAction` 类型："hub_skill_install"、"hub_skill_update"、"hub_skill_reject"
    - 记录 Skill 名称、来源 Hub、trust_level、安全审查结果
    - _需求: 5.6_

  - [x] 8.2 实现 critical 风险 Skill 的用户确认流程
    - 当 AssessSkill 返回 critical 时，通过 IM 向用户发送风险详情
    - 等待用户确认（"确认安装" / "取消"）
    - 用户确认后继续安装，取消则中止
    - _需求: 5.5_

- [x] 9. 检查点 — 能力缺口检测与自动安装
  - 端到端测试：发送一个现有工具无法处理的请求，验证 Maclaw 自动搜索 Hub、安装 Skill 并执行
  - 测试安全审查拦截场景

- [x] 10. NLSkillEntry 扩展与生命周期管理
  - [x] 10.1 扩展 NLSkillEntry，添加 Hub 相关字段
    - 添加 `HubSkillID string`、`HubVersion string`、`TrustLevel string` 字段
    - 更新 `NLSkillDefinition` 视图类型，包含新字段
    - 更新 `List()` 方法，对 source="hub" 的 Skill 额外展示 Hub URL 和 trust_level
    - _需求: 4.2, 6.1, 6.4_

  - [x] 10.2 实现 Hub Skill 更新功能
    - 在 SkillExecutor 中新增 `UpdateFromHub(name string)` 方法
    - 调用 SkillHubClient.CheckUpdate 检查新版本
    - 有更新时下载新版本并替换本地 Skill（保留 source 和 hub_skill_id）
    - _需求: 6.3_

  - [x] 10.3 添加 Wails 绑定函数
    - `SearchSkillHub(query string) []HubSkillMeta`
    - `InstallHubSkill(skillID, hubURL string) error`
    - `CheckHubSkillUpdates() []HubSkillUpdateInfo`
    - `UpdateHubSkill(skillName string) error`
    - _需求: 6.2, 6.3, 9.4_

- [x] 11. 前端 — Hub 市场界面
  - [x] 11.1 在 SkillsManagementPanel 中新增 "Hub 市场" Tab
    - 新增 Tab 切换：本地 Skills / Hub 市场
    - Hub 市场 Tab 包含搜索框和结果列表区域
    - _需求: 9.1_

  - [x] 11.2 实现搜索和结果展示
    - 搜索框输入关键词，调用 `SearchSkillHub` Wails 绑定
    - 结果卡片展示：名称、描述、标签、trust_level 徽章（official=绿色、community=蓝色、unknown=灰色）、下载量
    - 每个卡片包含"安装"按钮
    - _需求: 9.2, 9.3_

  - [x] 11.3 实现安装和更新交互
    - 点击"安装"按钮调用 `InstallHubSkill`，安装完成后刷新本地 Skill 列表
    - 已安装的 Hub Skill 显示"已安装"状态
    - 有新版本的 Skill 显示"更新"按钮，点击调用 `UpdateHubSkill`
    - 安装/更新过程中显示 loading 状态
    - _需求: 9.4, 9.5_

- [x] 12. 全局集成
  - [x] 12.1 修改 `app.go`，初始化 SkillHubClient 和 CapabilityGapDetector
    - 在 `ensureRemoteInfra()` 中创建 SkillHubClient
    - 在 `ensureRemoteInfra()` 中创建 CapabilityGapDetector，注入依赖
    - 启动推荐预热定时任务
    - 在 `shutdown()` 中清理资源
    - _需求: 全部_

  - [x] 12.2 修改 `im_message_handler.go`，注入 CapabilityGapDetector
    - 在 IMMessageHandler 构造时传入 CapabilityGapDetector
    - 在 runAgentLoop 中集成能力缺口检测分支
    - _需求: 3.1, 4.3_

  - [x] 12.3 在 Hub 侧 bootstrap 中初始化 SkillStore
    - 在 `hub/internal/app/bootstrap.go` 中创建 SkillStore
    - 注册 SkillHandlers 路由
    - 创建默认 Skill 存储目录
    - _需求: 1.1_

- [x] 13. 最终检查点
  - 端到端验证完整流程：Hub 发布 Skill → 客户端搜索 → 自动安装 → 执行
  - 验证多 Hub 容错和镜像回退
  - 验证安全审查和审计日志
  - 验证前端 Hub 市场界面

## 备注

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP 交付
- MVP 优先级：任务 1-4（Hub API + Client）→ 任务 7（能力缺口检测）→ 任务 10-12（集成）
- Hub 侧存储 MVP 使用 JSON 文件，后续可迁移到 SQLite 或其他数据库
- 搜索 MVP 使用关键词匹配，后续可接入向量搜索实现真正的语义搜索
- 所有新文件遵循现有代码库的 Go 惯用法
- 前端任务可与后端任务并行开发
