# 实施计划：Maclaw 能力升级改进计划

## 概述

基于需求文档和设计文档，将 Maclaw 平台的五个能力升级方向拆分为增量实施任务。每个任务在前一个任务基础上构建，最终通过集成任务将所有组件串联。实施语言为 Go，遵循现有代码库的架构风格（Wails 桌面端 + Hub 后端）。

## 任务

- [x] 1. 安全防火墙子系统 — 核心类型与风险评估
  - [x] 1.1 创建 `risk_assessor.go`，实现 RiskAssessor 和相关类型
    - 定义 `RiskLevel`（low/medium/high/critical）、`RiskContext`、`RiskAssessment` 类型
    - 实现 `Assess(ctx RiskContext) RiskAssessment` 方法
    - 规则：参数包含 `rm -rf`/`DROP TABLE`/`format`/`sudo` → critical；文件写入/命令执行 → 至少 medium；只读查询 → low
    - 实现上下文感知：系统目录写操作提升一级、read-only 模式下写操作 → critical、同一工具连续调用 >10 次提升一级
    - _需求: 4.1, 4.2, 4.3, 4.4, 15.1, 15.2, 15.3_

  - [ ]* 1.2 为 RiskAssessor 编写属性测试
    - **属性 1: 风险等级单调性 — 包含危险关键词的参数风险等级 ≥ critical**
    - **验证: 需求 4.2**

  - [ ]* 1.3 为 RiskAssessor 编写单元测试
    - 测试各种参数组合的风险评估结果
    - 测试上下文感知规则（系统目录、read-only 模式、连续调用）
    - _需求: 4.1-4.4, 15.1-15.3_

- [x] 2. 安全防火墙子系统 — 策略引擎与审计日志
  - [x] 2.1 创建 `policy_engine.go`，实现 PolicyEngine
    - 定义 `PolicyAction`（allow/deny/ask/audit）、`PolicyRule` 类型
    - 实现 `Evaluate(toolName string, args map[string]interface{}, risk RiskLevel) PolicyAction`
    - 实现 `LoadRules(path string) error` 从 JSON 配置文件加载规则
    - 实现 `DefaultPolicyRules()` 返回默认策略集
    - 规则按 Priority 排序，数字越小优先级越高，使用第一条匹配规则
    - _需求: 6.1, 6.2, 6.3, 6.4, 6.5_

  - [x] 2.2 创建 `audit_log.go`，实现 AuditLog
    - 定义 `AuditEntry`、`AuditFilter` 类型
    - 实现 `Log(entry AuditEntry) error` 写入 JSONL 格式日志文件
    - 实现按日期分割日志文件，超过 50MB 自动轮转
    - 实现 `Query(filter AuditFilter) ([]AuditEntry, error)` 按条件查询
    - 保留最近 30 天日志
    - _需求: 7.1, 7.2, 7.3, 7.4_

  - [ ]* 2.3 为 PolicyEngine 编写属性测试
    - **属性 2: 策略优先级确定性 — 相同输入始终产生相同的策略决策**
    - **验证: 需求 6.3**

  - [ ]* 2.4 为 AuditLog 编写单元测试
    - 测试日志写入和查询
    - 测试日期分割和文件轮转逻辑
    - _需求: 7.1-7.4_

- [x] 3. 安全防火墙子系统 — LLM 安全审查与 PermissionHandler v2 集成
  - [x] 3.1 创建 `llm_security_review.go`，实现 LLMSecurityReview
    - 定义 `LLMSecurityVerdict`（safe/risky/dangerous）类型
    - 实现 `Review(ctx RiskContext, assessment RiskAssessment) (LLMSecurityVerdict, string, error)`
    - 使用 5 秒超时的 HTTP 客户端调用 LLM
    - 超时未返回则回退到规则评估
    - LLM 未配置时跳过审查
    - _需求: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x] 3.2 扩展 `remote_permission.go`，集成完整安全链路
    - 在 `PermissionHandler` 中添加 `RiskAssessor`、`PolicyEngine`、`LLMSecurityReview`、`AuditLog` 字段
    - 修改 `HandleRequest` 流程：RiskAssessor.Assess → PolicyEngine.Evaluate → [LLMSecurityReview] → AuditLog.Log → Decision
    - default 模式下自动批准 low 风险操作，medium 及以上请求用户确认
    - 保持现有 auto-approve 和 read-only 模式行为不变
    - _需求: 4.5, 5.1-5.5, 6.1-6.5, 7.1_

  - [ ]* 3.3 为 PermissionHandler v2 编写集成测试
    - 测试完整安全链路：风险评估 → 策略引擎 → LLM 审查 → 审计日志
    - 测试 LLM 超时回退行为
    - _需求: 4.5, 5.4_

- [x] 4. 检查点 — 安全防火墙子系统
  - 确保所有测试通过，如有问题请向用户确认。

- [x] 5. 动态工具发现子系统 — MCPRegistry v2 与扫描器
  - [x] 5.1 扩展 `app_nl_mcp.go`，为 MCPServerEntry 添加 Source 字段
    - 定义 `MCPServerSource` 类型（manual/mdns/project）
    - 在 `MCPServerEntry` 中添加 `Source MCPServerSource` 字段
    - 实现 `RegisterAutoDiscovered(entry MCPServerEntry, source MCPServerSource) error`
    - 自动发现的服务器与手动注册冲突时忽略
    - 实现 `StartHealthLoop(ctx context.Context)` 60 秒间隔健康检查
    - 实现 `RemoveUnhealthy()` 移除连续 3 次失败的自动发现服务器
    - _需求: 1.1, 1.3, 1.4, 1.5_

  - [x] 5.2 创建 `mdns_scanner.go`，实现 MDNSScanner
    - 实现 mDNS/DNS-SD 扫描，监听 `_mcp._tcp` 服务类型
    - 发现新服务器时调用 `MCPRegistry.RegisterAutoDiscovered`
    - 实现 `Start()` 和 `Stop()` 生命周期方法
    - _需求: 1.1_

  - [x] 5.3 创建 `project_scanner.go`，实现 ProjectScanner
    - 实现 `ScanProject(projectPath string) ([]MCPServerEntry, error)`
    - 解析 `.mcp/servers.json` 配置文件
    - 将发现的服务器以 source="project" 注册到 MCPRegistry
    - _需求: 1.2_

  - [ ]* 5.4 为 MCPRegistry v2 编写单元测试
    - 测试自动发现注册与手动注册冲突处理
    - 测试健康检查循环和不健康服务器移除
    - _需求: 1.4, 1.5_

- [x] 6. 动态工具发现子系统 — 工具定义生成与路由
  - [x] 6.1 创建 `tool_definition_generator.go`，实现 ToolDefinitionGenerator
    - 保留 12 个内置工具定义作为基础
    - 合并所有健康 MCP Server 的工具为动态工具定义
    - 动态工具名称冲突时添加 server_id 前缀
    - 生成符合 OpenAI function calling 格式的工具定义
    - _需求: 2.1, 2.2, 2.3, 2.4_

  - [x] 6.2 创建 `tool_router.go`，实现 ToolRouter
    - 实现 `Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{}`
    - 工具总数 > 20 时，保留 12 个内置 + 最相关的动态工具（上限 15）
    - 使用关键词匹配 + TF-IDF 相似度排序
    - _需求: 3.1, 3.2, 3.3_

  - [x] 6.3 修改 `im_message_handler.go`，集成动态工具生成和路由
    - 将 `buildToolDefinitions()` 替换为 `ToolDefinitionGenerator.Generate()`
    - 在 `runAgentLoop` 中集成 `ToolRouter` 进行工具筛选
    - MCP_Registry 变化时 5 秒内重新生成工具列表
    - _需求: 2.1, 3.1, 3.4_

  - [ ]* 6.4 为 ToolDefinitionGenerator 和 ToolRouter 编写单元测试
    - 测试工具名称冲突处理
    - 测试工具筛选逻辑和排序
    - _需求: 2.3, 3.1-3.3_

- [x] 7. 检查点 — 动态工具发现子系统
  - 确保所有测试通过，如有问题请向用户确认。

- [x] 8. 经验记忆子系统 — NLSkillEntry v2 与经验提取
  - [x] 8.1 扩展 `app_nl_skills.go`，为 NLSkillEntry 添加 Source 和 SourceProject 字段
    - 在 `NLSkillEntry` 中添加 `Source string` 和 `SourceProject string` 字段
    - 更新 `NLSkillDefinition` 视图类型，包含来源信息
    - 更新 `List()` 方法展示来源类型和来源项目
    - _需求: 9.1, 9.3, 9.4_

  - [x] 8.2 创建 `experience_extractor.go`，实现 ExperienceExtractor
    - 实现 `Extract(session *RemoteSession) error`
    - 调用 LLM 分析会话历史，提取操作模式的名称、描述、触发条件和步骤序列
    - 将提取的模式转换为 NL_Skill 格式并注册
    - 与已有同名 Skill 比较，仅在新模式更详细时更新
    - LLM 未配置时跳过提取
    - _需求: 8.1, 8.2, 8.3, 8.4, 8.5_

  - [x] 8.3 修改 `im_message_handler.go` 的 `buildSystemPrompt()`，将所有 active Skill 的名称和描述包含在系统提示词中
    - _需求: 9.2_

  - [ ]* 8.4 为 ExperienceExtractor 编写单元测试
    - 测试会话历史分析和 Skill 提取逻辑
    - 测试重复 Skill 的更新判断
    - _需求: 8.3, 8.4_

- [x] 9. Skills 备份与恢复子系统
  - [x] 9.1 创建 `skill_backup.go`，实现 BackupSkills 和 RestoreSkills
    - 实现 `BackupSkills(outputPath string) error`：序列化所有 Skill 为 JSON，打包为 zip
    - zip 中包含 `manifest.json`（备份时间、Skill 数量、Maclaw 版本）
    - 每个 Skill 一个独立 JSON 文件，文件名为 kebab-case 格式
    - 实现 `RestoreSkills(zipPath string) (*RestoreReport, error)`：解析 zip 并恢复
    - 同名 Skill 跳过并标记为 "skipped (duplicate)"
    - 返回恢复报告（restored/skipped/failed 数量）
    - 无效 zip 或缺少 manifest.json 时返回描述性错误
    - _需求: 10.1, 10.2, 10.3, 11.1, 11.2, 11.3, 11.4_

  - [x] 9.2 实现 Skill 序列化与反序列化函数
    - 实现 `SerializeSkill(skill NLSkillEntry) ([]byte, error)` 和 `DeserializeSkill(data []byte) (NLSkillEntry, error)`
    - 包含所有字段：name、description、triggers、steps、status、created_at、source、source_project
    - 缺少必填字段（name 或 steps）时返回描述性错误
    - _需求: 16.1, 16.2, 16.4_

  - [ ]* 9.3 为 Skill 序列化编写属性测试
    - **属性 3: Round-trip 一致性 — 任意有效 NLSkillEntry 序列化后反序列化应与原始对象等价**
    - **验证: 需求 16.3**

  - [ ]* 9.4 为 BackupSkills/RestoreSkills 编写单元测试
    - 测试完整备份恢复流程
    - 测试重复 Skill 跳过逻辑
    - 测试无效 zip 文件处理
    - _需求: 10.1-10.3, 11.1-11.4_

- [x] 10. 检查点 — 经验记忆与备份恢复子系统
  - 确保所有测试通过，如有问题请向用户确认。

- [x] 11. 编排子系统 — SharedContextStore 与 ToolSelector
  - [x] 11.1 创建 `shared_context.go`，实现 SharedContextStore
    - 定义 `ContextEntry` 类型（Key、Value、SessionID、CreatedAt）
    - 实现 `Put(entry ContextEntry)` 写入条目，超出 100KB 时 FIFO 淘汰
    - 实现 `GetForSession(sessionID string) []ContextEntry` 获取相关上下文
    - 使用 `sync.RWMutex` 保护并发访问
    - _需求: 13.1, 13.4_

  - [x] 11.2 创建 `tool_selector.go`，实现 ToolSelector
    - 定义 `ToolProfile` 类型（Name、Languages、Frameworks、TaskTypes、Score）
    - 实现 `Recommend(taskDescription string, installed []string) (string, string)`
    - 基于任务描述与工具能力画像的匹配度推荐工具
    - 优先选择已安装且健康的工具
    - _需求: 14.1, 14.2, 14.3, 14.4_

  - [ ]* 11.3 为 SharedContextStore 编写属性测试
    - **属性 4: FIFO 淘汰正确性 — 存储总大小始终 ≤ 100KB**
    - **验证: 需求 13.4**

  - [ ]* 11.4 为 ToolSelector 编写单元测试
    - 测试工具推荐逻辑和已安装工具优先
    - _需求: 14.1-14.4_

- [x] 12. 编排子系统 — Orchestrator 核心
  - [x] 12.1 创建 `orchestrator.go`，实现 Orchestrator
    - 定义 `OrchestratorTask`、`TaskRequest`、`OrchestratorResult` 类型
    - 实现 `ExecuteParallel(tasks []TaskRequest) (*OrchestratorResult, error)`
    - 最多 5 个并行会话，使用 `sync.WaitGroup` 和 goroutine 并行执行
    - 跟踪各会话执行状态，汇总结果
    - 部分失败时标记失败会话，其他继续执行
    - _需求: 12.1, 12.2, 12.3, 12.4_

  - [x] 12.2 在 Orchestrator 中集成 SharedContextStore
    - 会话产生重要事件时写入共享上下文
    - 向会话发送输入时附加相关共享上下文
    - _需求: 13.2, 13.3_

  - [ ]* 12.3 为 Orchestrator 编写单元测试
    - 测试并行执行和结果汇总
    - 测试部分失败场景
    - 测试最大并行数限制
    - _需求: 12.1-12.4_

- [x] 13. 全局集成 — 将所有子系统接入 App 和 IMMessageHandler
  - [x] 13.1 修改 `app.go`，在 App 结构体中添加新组件字段并初始化
    - 添加 `riskAssessor`、`policyEngine`、`auditLog`、`llmSecurityReview` 字段
    - 添加 `mdnsScanner`、`projectScanner`、`toolDefGenerator`、`toolRouter` 字段
    - 添加 `experienceExtractor`、`orchestrator`、`sharedContext`、`toolSelector` 字段
    - 在 App 启动时初始化所有组件，在关闭时清理资源
    - _需求: 全部_

  - [x] 13.2 修改 `im_message_handler.go`，集成编排工具
    - 在 `buildToolDefinitions()` 中添加编排相关工具定义（parallel_execute、recommend_tool）
    - 在 `executeTool()` 中添加编排工具的执行分支
    - _需求: 12.1-12.4, 14.1-14.4_

  - [x] 13.3 添加 Wails 绑定函数
    - 在 `app.go` 或相关文件中添加 `BackupSkills`、`RestoreSkills` 的 Wails 绑定
    - 添加 `QueryAuditLog` 的 Wails 绑定
    - 添加 `RecommendTool` 的 Wails 绑定
    - _需求: 10.1, 11.1, 7.4, 14.4_

  - [ ]* 13.4 为全局集成编写集成测试
    - 测试完整消息处理流程：用户消息 → 工具路由 → 权限检查 → 工具执行 → 结果返回
    - _需求: 全部_

- [x] 14. 最终检查点 — 确保所有测试通过
  - 确保所有测试通过，如有问题请向用户确认。

## 备注

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号，确保需求可追溯
- 检查点任务确保增量验证，及时发现问题
- 属性测试验证核心正确性属性，单元测试覆盖边界情况
- 所有新文件遵循现有代码库的 Go 惯用法：`sync.RWMutex` 并发保护、`interface` 多态、`context.Context` 超时控制
