# 实现计划：MaClaw 能力升级 — 动态工具发现、安全防火墙、差异化调度

## 概述

本实现计划分三个 Phase 交付，每个 Phase 独立可用。Phase 1 是基础，Phase 2 和 Phase 3 可并行开发。

- Phase 1（动态工具发现）：~5 天，改造工具注册和发现机制
- Phase 2（安全防火墙）：~4 天，实现多层安全体系
- Phase 3（差异化调度）：~5 天，实现编排、选择、上下文共享

所有改动在 MaClaw 客户端侧，不涉及 Hub 变更。

## Tasks

### Phase 1: 动态工具发现

- [x] 1. 实现 ToolRegistry 统一工具注册中心
  - [x] 1.1 创建 `tool_registry.go`
    - 定义 `ToolCategory`（builtin/mcp/skill/non_code）、`ToolStatus`（available/degraded/unavailable）枚举
    - 定义 `RegisteredTool` 结构体（Name、Description、Category、Tags、Priority、Status、InputSchema、Required、Source、Handler）
    - 定义 `ToolHandler` 函数类型 `func(args map[string]interface{}) string`
    - 实现 `ToolRegistry` 结构体（tools map、onChange callbacks、sync.RWMutex）
    - 实现 Register、Unregister、Get、List、ListAvailable、ListByCategory、ListByTags、UpdateStatus、OnChange 方法
    - _需求: 1.1, 1.2, 1.4, 1.5_

  - [x] 1.2 将现有内置工具迁移到 ToolRegistry
    - 将 `im_message_handler.go` 中 `buildToolDefinitions()` 的 12 个硬编码工具定义迁移为 ToolRegistry 注册
    - 创建 `registerBuiltinTools(registry *ToolRegistry, handler *IMMessageHandler)` 函数
    - 每个内置工具的 Handler 对应原来 `executeTool` switch-case 中的逻辑
    - 保持工具名和参数 schema 不变，确保向后兼容
    - _需求: 1.1, 1.3_

  - [x] 1.3 改造 IMMessageHandler 使用 ToolRegistry
    - 在 `IMMessageHandler` 中添加 `registry *ToolRegistry` 和 `toolBuilder *DynamicToolBuilder` 字段
    - 改造 `buildToolDefinitions()` → 调用 `toolBuilder.Build(userMessage)`
    - 改造 `executeTool()` → 从 registry.Get(name) 查找并调用 Handler，替代 switch-case
    - 在 `NewIMMessageHandler` 中初始化 registry 并注册内置工具
    - _需求: 1.2, 1.3_

  - [x]* 1.4 编写 ToolRegistry 单元测试
    - 测试注册、注销、查询、状态更新
    - 测试 ListAvailable 只返回可用工具
    - 测试 ListByTags 筛选
    - 测试 OnChange 回调触发
    - _需求: 1.1_

- [x] 2. 实现 DynamicToolBuilder
  - [x] 2.1 创建 `tool_builder.go`
    - 实现 `DynamicToolBuilder` 结构体，持有 `*ToolRegistry` 引用
    - 实现 `BuildAll()` 方法：遍历 registry.ListAvailable()，转换为 LLM tool definition 格式
    - 实现 `Build(userMessage string)` 方法：
      - 如果可用工具 ≤ 20，直接返回 BuildAll()
      - 如果 > 20，启用上下文筛选：始终包含 builtin 类工具，其余按 Tags 与 userMessage 的关键词匹配排序，取 top 15
    - 工具定义格式复用现有 `toolDef()` 函数的输出格式
    - _需求: 3.1, 3.2, 3.3_

  - [x] 2.2 实现工具分组激活
    - 在 `Build()` 中支持用户消息中的分组指令（如 "使用数据库工具" → 激活 tag=database 的所有工具）
    - 定义分组关键词映射（可配置）
    - _需求: 3.4_

  - [x]* 2.3 编写 DynamicToolBuilder 单元测试
    - 测试工具数 ≤ 20 时返回全部
    - 测试工具数 > 20 时的筛选逻辑
    - 测试分组激活
    - _需求: 3.1, 3.2_

- [x] 3. 实现 MCP 自动发现
  - [x] 3.1 创建 `mcp_auto_discovery.go`
    - 实现 `MCPAutoDiscovery` 结构体
    - 实现 `ScanProject(projectPath)`: 读取 `{projectPath}/.mcp.json`，解析 servers 列表，逐个注册到 MCPRegistry
    - 实现 `ScanGlobal()`: 读取 `~/.maclaw/mcp-servers.json`，注册全局 MCP Server
    - 注册时自动执行健康检查和工具列表拉取
    - 将发现的 MCP 工具同步注册到 ToolRegistry（Category=mcp，Tags 从声明文件读取）
    - _需求: 2.1, 2.2, 2.4_

  - [x] 3.2 实现项目 MCP 声明文件监听
    - 使用 fsnotify 监听 `{projectPath}/.mcp.json` 变更
    - 文件变更时重新扫描并更新注册
    - 实现 `WatchProject(projectPath)` 和 `Stop()` 方法
    - _需求: 2.3_

  - [x] 3.3 集成到 App 启动流程
    - 在 `App.startup()` 中创建 MCPAutoDiscovery 实例
    - 调用 ScanGlobal() 扫描全局注册表
    - 在会话创建时调用 ScanProject() 扫描项目 MCP
    - 将 MCPRegistry 的工具变更同步到 ToolRegistry
    - _需求: 2.1, 2.2, 2.5_

  - [x]* 3.4 编写 MCP 自动发现单元测试
    - 测试项目级 .mcp.json 解析
    - 测试全局注册表扫描
    - 测试文件变更触发重新扫描
    - _需求: 2.1, 2.3_

- [x] 4. 检查点 — Phase 1 编译和测试
  - 运行 `go build ./...` 确保编译通过
  - 运行 `go test ./...` 确保测试通过
  - 验证：注册新 MCP Server 后，Agent 立即可用其工具
  - 验证：buildToolDefinitions 返回动态工具列表

### Phase 2: 安全防火墙

- [x] 5. 实现 RiskAnalyzer 语义级风险分析器
  - [x] 5.1 创建 `security_risk_analyzer.go`
    - 定义 `RiskLevel` 枚举（low/medium/high/critical）
    - 定义 `RiskAssessment` 结构体（Level、Reason、Patterns、Mitigations）
    - 定义 `RiskPattern` 结构体（Name、Category、ToolMatch、ParamMatch、ParamKey、Level、Description）
    - 实现 `RiskAnalyzer` 结构体，持有 builtinPatterns 和 customPatterns
    - 实现 `NewRiskAnalyzer()` 构造函数，初始化内置风险模式
    - _需求: 4.1, 4.3_

  - [x] 5.2 实现风险评估逻辑
    - 实现 `Assess(toolName string, args map[string]interface{}, context *CallContext) RiskAssessment`
    - 评估流程：
      1. 遍历所有 RiskPattern，用正则匹配 toolName 和参数值
      2. 收集所有匹配的模式，取最高风险等级
      3. 如果有 CallContext.UserMessage 且用户明确要求了该操作，风险降一级
      4. 如果 CallContext.RecentApprovals 包含同类操作，风险降一级
      5. 无匹配模式时默认 RiskLow
    - _需求: 4.2, 4.5_

  - [x] 5.3 定义内置风险模式集
    - 文件删除类：rm -rf、rmdir /s、del /f、shutil.rmtree 等
    - 网络外传类：curl POST、wget --post、nc、scp 到外部地址等
    - 权限变更类：chmod 777、chown、icacls 等
    - 系统命令类：shutdown、reboot、systemctl stop、kill -9 等
    - 环境变量类：export *KEY*、export *SECRET*、export *TOKEN* 等
    - 包管理类：pip install（非 requirements.txt）、npm install -g 等
    - 数据库类：DROP TABLE、DELETE FROM（无 WHERE）、TRUNCATE 等
    - _需求: 4.3_

  - [x] 5.4 实现自定义风险规则
    - 实现 `AddCustomPattern(pattern RiskPattern)` 方法
    - 实现 `LoadCustomPatterns(path string) error` 从 JSON 文件加载
    - 自定义规则优先级高于内置规则
    - _需求: 4.4_

  - [x]* 5.5 编写 RiskAnalyzer 单元测试
    - 测试各类内置风险模式匹配
    - 测试上下文感知的风险降级
    - 测试自定义规则覆盖
    - 测试无匹配时默认 low
    - _需求: 4.1, 4.2, 4.5_

- [x] 6. 实现 PolicyEngine 策略引擎
  - [x] 6.1 创建 `security_policy_engine.go`
    - 定义 `PolicyAction` 枚举（allow/deny/ask/audit）
    - 定义 `PolicyRule` 结构体（Name、ToolMatch、RiskLevel、Action、Priority）
    - 定义 `PolicyDecision` 结构体（Action、Rule、Risk、Timestamp）
    - 实现 `PolicyEngine` 结构体（globalRules、projectRules、sessionRules）
    - _需求: 5.1_

  - [x] 6.2 实现策略决策逻辑
    - 实现 `Decide(toolName string, risk RiskAssessment, sessionID string) PolicyDecision`
    - 决策流程：
      1. 检查会话级规则（最高优先级）
      2. 检查项目级规则
      3. 检查全局自定义规则
      4. 使用默认策略（low→allow, medium→audit, high→ask, critical→deny）
    - 规则匹配：ToolMatch 用正则匹配工具名，RiskLevel 匹配风险等级
    - _需求: 5.1, 5.2_

  - [x] 6.3 实现策略加载和会话级审批
    - 实现 `LoadProjectPolicy(projectPath string) error`：读取 `.maclaw/security-policy.json`
    - 实现 `ApproveForSession(sessionID, toolPattern, riskLevel)`：将用户审批记录为会话级规则
    - _需求: 5.3, 5.4_

  - [x]* 6.4 编写 PolicyEngine 单元测试
    - 测试默认策略
    - 测试自定义规则覆盖
    - 测试会话级审批继承
    - 测试项目级策略加载
    - _需求: 5.1, 5.3_

- [x] 7. 实现 AuditLog 审计日志
  - [x] 7.1 创建 `security_audit_log.go`
    - 定义 `AuditEntry` 结构体（Timestamp、UserID、SessionID、ToolName、Args、Risk、Decision、Result、Duration）
    - 定义 `AuditFilter` 结构体（Since、Until、UserID、ToolName、RiskLevel、Limit）
    - 实现 `AuditLog` 结构体，日志目录为 `~/.maclaw/audit/`
    - _需求: 6.1, 6.2_

  - [x] 7.2 实现日志写入和查询
    - 实现 `Record(entry AuditEntry) error`：追加写入当日 JSONL 文件（`YYYY-MM-DD.jsonl`）
    - 实现 `Query(filter AuditFilter) ([]AuditEntry, error)`：按条件筛选日志
    - 实现日志文件自动轮转（按日期）
    - _需求: 6.2, 6.3_

  - [x] 7.3 注册 query_audit_log 为 Agent 工具
    - 在 ToolRegistry 中注册 `query_audit_log` 工具
    - 参数：since、until、tool_name、risk_level、limit
    - 返回格式化的审计记录摘要
    - _需求: 6.4_

- [x] 8. 实现 SecurityFirewall 集成
  - [x] 8.1 创建 `security_firewall.go`
    - 实现 `SecurityFirewall` 结构体，组合 RiskAnalyzer + PolicyEngine + AuditLog
    - 实现 `Check(toolName, args, ctx) (bool, string)` 方法：
      1. RiskAnalyzer.Assess() → 风险评估
      2. PolicyEngine.Decide() → 策略决策
      3. AuditLog.Record() → 记录审计
      4. 如果 action=ask，通过 onAsk 回调请求用户确认
      5. 返回最终决策
    - _需求: 4.1, 5.1, 6.1_

  - [x] 8.2 集成到 IMMessageHandler
    - 在 `IMMessageHandler` 中添加 `firewall *SecurityFirewall` 字段
    - 在 `executeTool()` 中，工具执行前调用 `firewall.Check()`
    - 被拒绝时返回友好的中文提示（包含风险原因和建议）
    - _需求: 4.1, 5.5_

  - [x] 8.3 集成到 App 启动流程
    - 在 `App.startup()` 中创建 SecurityFirewall 实例
    - 加载全局安全策略
    - 注入到 IMMessageHandler
    - _需求: 5.4_

  - [x] 8.4 添加前端安全策略配置
    - 在 RemoteSettingsPanel 中添加安全策略配置区域
    - 支持选择全局策略模式（宽松/标准/严格）
    - 支持查看审计日志摘要
    - _需求: 5.2, 6.3_

- [x] 9. 检查点 — Phase 2 编译和测试
  - 运行 `go build ./...` 确保编译通过
  - 运行 `go test ./...` 确保测试通过
  - 验证：高风险操作被拦截并提示用户
  - 验证：审计日志正确记录
  - 验证：会话级审批继承生效

### Phase 3: 差异化调度能力

- [x] 10. 实现 ToolSelector 智能工具选择
  - [x] 10.1 创建 `tool_selector.go`
    - 定义 `ToolProfile` 结构体（Tool、Strengths、Languages、SuccessRate、AvgDuration、UserPreference）
    - 实现 `ToolSelector` 结构体，持有 catalog、profiles、history
    - 初始化默认工具画像：
      - Claude: 擅长前端/React/TypeScript/代码审查/重构
      - Codex: 擅长 Python/数据处理/脚本编写
      - Gemini: 擅长 Go/Java/系统编程
      - Cursor: 擅长快速原型/全栈开发
      - OpenCode: 通用编程/多语言支持
    - _需求: 8.3_

  - [x] 10.2 实现智能选择逻辑
    - 实现 `Select(taskDescription, projectPath) (toolName, reason)`
    - 选择流程：
      1. 检测项目语言/框架（通过文件扩展名和配置文件）
      2. 分析任务描述关键词（重构/测试/修复/新功能等）
      3. 匹配工具画像的 Strengths 和 Languages
      4. 考虑工具可用性（已安装且健康）
      5. 考虑历史成功率和用户偏好
      6. 返回最佳匹配及选择理由
    - _需求: 8.1, 8.2_

  - [x] 10.3 实现选择结果记录和学习
    - 实现 `RecordResult(tool, taskType, success, duration)`
    - 持久化到 `~/.maclaw/tool-profiles.json`
    - 定期更新 SuccessRate 和 AvgDuration
    - _需求: 8.4_

  - [x] 10.4 集成到 create_session 工具
    - 修改 `toolCreateSession`：当 tool="auto" 时，调用 ToolSelector.Select()
    - 在回复中说明选择了哪个工具及原因
    - _需求: 8.1, 8.5_

  - [x]* 10.5 编写 ToolSelector 单元测试
    - 测试项目语言检测
    - 测试任务类型匹配
    - 测试历史数据影响选择
    - _需求: 8.2_

- [x] 11. 实现 ContextBridge 跨工具上下文共享
  - [x] 11.1 创建 `context_bridge.go`
    - 定义 `ProjectContext` 结构体（ProjectPath、FileChanges、Decisions、Notes、LastUpdated）
    - 定义 `FileChangeRecord`（File、Action、Timestamp、SessionID）
    - 定义 `DecisionRecord`（Description、Timestamp、SessionID）
    - 实现 `ContextBridge` 结构体，持有 contexts map
    - _需求: 9.1_

  - [x] 11.2 实现事件提取
    - 实现 `ExtractFromEvents(projectPath, events []ImportantEvent)`
    - 从 OutputPipeline 的 ImportantEvent 中提取：
      - file.change → FileChangeRecord
      - command.execute → 如果是关键决策，记录为 DecisionRecord
    - 限制每个项目最多保留最近 100 条记录
    - _需求: 9.3_

  - [x] 11.3 实现上下文注入
    - 实现 `BuildContextPrompt(projectPath) string`
    - 生成简洁的上下文摘要，包含：
      - 最近修改的文件列表
      - 关键决策记录
      - 用户手动添加的注释
    - 控制在 2000 token 以内
    - _需求: 9.2_

  - [x] 11.4 集成到会话创建流程
    - 在 `RemoteSessionManager.Create()` 中，调用 ContextBridge.BuildContextPrompt()
    - 将上下文注入到会话的初始 prompt 中（通过 LaunchSpec.SystemPrompt 或 append-system-prompt）
    - _需求: 9.2_

  - [x] 11.5 实现手动注释功能
    - 实现 `AddNote(projectPath, note)` 方法
    - 在 Agent 工具中注册 `add_context_note` 工具
    - 用户可通过 IM 说 "记住：xxx" 来添加项目上下文注释
    - _需求: 9.4_

  - [x] 11.6 集成到 OutputPipeline
    - 在 `RemoteSessionManager` 的输出处理循环中，将事件传递给 ContextBridge.ExtractFromEvents()
    - _需求: 9.3_

  - [x]* 11.7 编写 ContextBridge 单元测试
    - 测试事件提取
    - 测试上下文 prompt 生成
    - 测试记录数量限制
    - _需求: 9.1, 9.3_

- [x] 12. 实现 TaskOrchestrator 多会话编排
  - [x] 12.1 创建 `task_orchestrator.go`
    - 定义 `TaskPlan` 结构体（ID、Description、SubTasks、Status）
    - 定义 `SubTask` 结构体（ID、Description、Tool、SessionID、DependsOn、Status、Result）
    - 实现 `TaskOrchestrator` 结构体，持有 manager、toolSelector、contextBridge、plans
    - _需求: 7.1_

  - [x] 12.2 实现任务计划创建和执行
    - 实现 `CreatePlan(description, subTasks) (*TaskPlan, error)`
    - 实现 `Execute(planID) error`：
      1. 拓扑排序子任务（按 DependsOn）
      2. 并行启动无依赖的子任务（每个子任务创建一个会话）
      3. 子任务完成后检查依赖，启动后续任务
      4. 汇总所有子任务结果
    - 实现 `GetStatus(planID)` 和 `Cancel(planID)`
    - _需求: 7.2, 7.3_

  - [x] 12.3 注册 orchestrate_task 为 Agent 工具
    - 在 ToolRegistry 中注册 `orchestrate_task` 工具
    - 参数：description（任务描述）、sub_tasks（子任务列表，每个包含 description 和 tool）
    - 返回 plan ID 和执行状态
    - 注册 `get_plan_status` 工具用于查询计划状态
    - _需求: 7.4_

  - [x] 12.4 实现子任务失败处理
    - 子任务失败时暂停计划执行
    - 通过 Agent 回复通知用户，提供重试/跳过/取消选项
    - _需求: 7.5_

  - [x]* 12.5 编写 TaskOrchestrator 单元测试
    - 测试并行执行无依赖子任务
    - 测试依赖关系的拓扑排序
    - 测试子任务失败暂停
    - _需求: 7.1, 7.3, 7.5_

- [x] 13. 实现非编程工具集
  - [x] 13.1 创建 `tools_non_code.go`
    - 实现 `registerNonCodeTools(registry *ToolRegistry, app *App)` 函数
    - 实现 Git 工具：
      - `git_status`: 执行 `git status --porcelain` 并格式化输出
      - `git_diff`: 执行 `git diff` 并返回摘要（限制输出长度）
      - `git_commit`: 执行 `git add -A && git commit -m "message"`（经过 SecurityFirewall）
      - `git_push`: 执行 `git push`（经过 SecurityFirewall）
    - _需求: 10.1_

  - [x] 13.2 实现文件操作工具
    - `read_file`: 读取文件内容（限制大小，大文件返回前 N 行）
    - `list_dir`: 列出目录内容（支持递归深度限制）
    - `search_files`: 使用 ripgrep 或 strings.Contains 搜索文件内容
    - _需求: 10.3_

  - [x] 13.3 实现环境管理工具
    - `list_env`: 列出关键环境变量（脱敏处理 KEY/SECRET/TOKEN）
    - `install_deps`: 根据项目类型执行依赖安装（npm install / pip install -r / go mod tidy）
    - `check_health`: 检查项目健康状态（编译是否通过、测试是否通过）
    - _需求: 10.2_

  - [x] 13.4 集成到 App 启动流程
    - 在 ToolRegistry 初始化后调用 `registerNonCodeTools()`
    - 所有写操作类工具标记 Tags 包含 "write"，确保经过 SecurityFirewall
    - _需求: 10.4, 10.5_

  - [x]* 13.5 编写非编程工具单元测试
    - 测试 Git 工具输出格式
    - 测试文件读取大小限制
    - 测试环境变量脱敏
    - _需求: 10.1, 10.3_

- [x] 14. 检查点 — Phase 3 编译和测试
  - 运行 `go build ./...` 确保编译通过
  - 运行 `go test ./...` 确保测试通过
  - 验证：tool=auto 模式正确选择工具
  - 验证：跨会话上下文注入生效
  - 验证：多任务编排并行执行
  - 验证：非编程工具可通过 IM 调用

- [x] 15. 最终集成和文档
  - [x] 15.1 更新 system prompt
    - 在 `buildSystemPrompt()` 中添加新能力说明
    - 说明 tool=auto 模式、安全策略、任务编排等新功能
    - 添加使用示例
  - [x] 15.2 更新前端面板
    - 在 RemoteSettingsPanel 中添加安全策略配置
    - 在 RemoteSettingsPanel 中添加工具选择偏好设置
    - 添加审计日志查看入口
  - [x] 15.3 最终编译和全量测试
    - 运行 `go build ./...`
    - 运行 `go test ./...`
    - 端到端验证：通过 IM 发送消息，验证动态工具发现、安全拦截、智能选择全链路

## 备注

- 标记 `*` 的任务为可选测试任务，可跳过以加速交付
- Phase 1 是基础，必须先完成；Phase 2 和 Phase 3 可并行开发
- 核心路径：Task 1 → 2 → 3 → 5 → 6 → 8 是最小安全可用路径
- TaskOrchestrator（Task 12）是最复杂的模块，可作为最后实现
- 所有改动在 MaClaw 客户端侧（根目录 .go 文件），不涉及 Hub 变更
- 后端使用 Go，前端使用 React/TypeScript
- 新文件命名规范：`tool_*.go`（工具相关）、`security_*.go`（安全相关）、`task_*.go`（编排相关）、`context_*.go`（上下文相关）

