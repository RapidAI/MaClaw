# 需求文档：MaClaw 能力升级 — 动态工具发现、安全防火墙、差异化调度

## 简介

本改进计划针对 MaClaw 的三个核心能力短板进行升级：

1. **动态工具发现** — 当前 MCP Server 和 Skills 均为手动注册，Agent 工具集硬编码在 `buildToolDefinitions()` 中。升级为自动发现 + 动态注册 + 上下文感知的工具选择机制。
2. **安全防火墙** — 当前 `PermissionHandler` 仅支持模式切换（default/auto-approve/read-only）和工具名匹配。升级为语义级风险评估 + 策略引擎 + 审计链的多层安全体系。
3. **差异化调度能力** — 作为 "调度与非编程能力提供工具"，MaClaw 需要在多工具编排、跨工具上下文共享、智能工具选择、非编程能力矩阵等方面建立与 OpenClaw 协议层的差异化。

改进原则：增量式演进，每个阶段独立可交付，不破坏现有 Agent Passthrough 架构。

## 术语表

- **ToolRegistry** — 统一工具注册中心，合并管理内置工具、MCP 工具、Skill 工具的元数据和生命周期
- **DynamicToolBuilder** — 动态工具定义构建器，根据当前注册的工具和上下文动态生成 LLM tool definitions
- **MCPAutoDiscovery** — MCP Server 自动发现模块，支持项目级声明、全局注册表、网络发现
- **SecurityFirewall** — 安全防火墙，在工具调用前进行多层风险评估和策略决策
- **RiskAnalyzer** — 风险分析器，对工具名 + 参数进行语义级风险评估
- **PolicyEngine** — 策略引擎，基于规则和上下文做出 allow/deny/ask/audit 决策
- **AuditLog** — 审计日志，记录所有工具调用的完整链路
- **TaskOrchestrator** — 任务编排器，支持多会话并行编排和任务分解
- **ContextBridge** — 上下文桥接器，在不同编程工具会话间共享项目上下文
- **ToolSelector** — 智能工具选择器，根据任务特征自动选择最合适的编程工具

## 需求

### 需求 1：统一工具注册中心（ToolRegistry）

**用户故事：** 作为开发者，我希望新注册的 MCP Server 或 Skill 能立即被 Agent 使用，而不需要重启或手动刷新。

#### 验收标准

1. THE ToolRegistry SHALL 统一管理三类工具的元数据：内置工具（会话管理等）、MCP 工具、Skill 工具
2. WHEN 新的 MCP Server 注册或 Skill 创建时, THE ToolRegistry SHALL 自动更新可用工具列表，无需重启
3. THE DynamicToolBuilder SHALL 在每次 Agent 调用时，从 ToolRegistry 动态生成 LLM tool definitions
4. THE ToolRegistry SHALL 为每个工具维护状态（available/degraded/unavailable），不可用的工具不出现在 LLM tool definitions 中
5. THE ToolRegistry SHALL 支持工具优先级，当多个工具提供相同能力时，优先使用高优先级工具

### 需求 2：MCP 自动发现

**用户故事：** 作为开发者，我希望打开一个项目时，项目中声明的 MCP Server 能自动被发现和注册。

#### 验收标准

1. THE MCPAutoDiscovery SHALL 支持项目级声明文件（`.mcp.json` 或 `.mcp/servers.json`），项目打开时自动注册
2. THE MCPAutoDiscovery SHALL 支持全局注册表（`~/.maclaw/mcp-servers.json`），跨项目共享
3. WHEN 项目级 MCP 声明文件变更时, THE MCPAutoDiscovery SHALL 自动检测并更新注册
4. THE MCPAutoDiscovery SHALL 在注册时自动执行健康检查和工具列表拉取
5. IF MCP Server 声明了认证要求, THEN THE MCPAutoDiscovery SHALL 提示用户配置认证信息

### 需求 3：上下文感知的工具选择

**用户故事：** 作为开发者，当我注册了大量 MCP 工具时，我希望 Agent 只看到与当前任务相关的工具，而不是被几十个工具定义淹没。

#### 验收标准

1. WHEN 注册工具总数超过 20 个时, THE DynamicToolBuilder SHALL 启用上下文感知筛选
2. THE DynamicToolBuilder SHALL 根据用户消息的语义，从 ToolRegistry 中筛选最相关的工具子集（最多 15 个）
3. THE DynamicToolBuilder SHALL 始终包含核心工具（list_sessions、create_session、send_input 等）
4. THE DynamicToolBuilder SHALL 支持工具分组（tag/category），用户可通过自然语言激活特定分组

### 需求 4：语义级风险评估

**用户故事：** 作为系统管理员，我希望安全防火墙不仅看工具名，还能分析工具参数的实际风险。

#### 验收标准

1. THE RiskAnalyzer SHALL 对每次工具调用进行风险评估，输出风险等级（low/medium/high/critical）
2. THE RiskAnalyzer SHALL 分析工具参数的语义风险，而非仅匹配工具名。例如 Bash 工具中 `ls` 为 low，`rm -rf` 为 critical
3. THE RiskAnalyzer SHALL 维护一组内置风险模式（文件删除、权限变更、网络外传、系统命令、环境变量修改等）
4. THE RiskAnalyzer SHALL 支持用户自定义风险规则
5. THE RiskAnalyzer SHALL 考虑上下文：如果用户明确要求了某操作，该操作的风险等级可降低一级

### 需求 5：策略引擎

**用户故事：** 作为系统管理员，我希望能配置灵活的安全策略，而不是只有 "全部允许" 或 "全部拒绝"。

#### 验收标准

1. THE PolicyEngine SHALL 支持基于风险等级的默认策略：low→auto-approve, medium→audit, high→ask, critical→deny
2. THE PolicyEngine SHALL 支持用户自定义策略规则（JSON 格式），覆盖默认策略
3. THE PolicyEngine SHALL 支持会话级策略继承：用户在会话中批准某类操作后，同类操作自动批准
4. THE PolicyEngine SHALL 支持项目级策略文件（`.maclaw/security-policy.json`），不同项目可有不同安全策略
5. WHEN 策略决策为 "ask" 时, THE SecurityFirewall SHALL 通过 IM 向用户发送审批请求，等待用户确认

### 需求 6：审计日志

**用户故事：** 作为系统管理员，我希望能查看所有工具调用的历史记录，用于安全审查。

#### 验收标准

1. THE AuditLog SHALL 记录每次工具调用的完整信息：时间、用户、会话、工具名、参数、风险等级、策略决策、执行结果
2. THE AuditLog SHALL 持久化到本地文件（`~/.maclaw/audit/`），支持按日期轮转
3. THE AuditLog SHALL 提供查询接口，支持按时间范围、用户、工具名、风险等级筛选
4. THE Agent SHALL 提供 `query_audit_log` 工具，允许用户通过自然语言查询审计记录

### 需求 7：多会话并行编排

**用户故事：** 作为开发者，我希望能同时启动多个编程工具会话，让它们并行处理不同的子任务。

#### 验收标准

1. THE TaskOrchestrator SHALL 支持将用户任务分解为多个子任务，分配给不同的编程工具会话
2. THE TaskOrchestrator SHALL 支持并行执行多个子任务，并汇总结果
3. THE TaskOrchestrator SHALL 支持子任务间的依赖关系（A 完成后再启动 B）
4. THE Agent SHALL 提供 `orchestrate_task` 工具，接受任务描述和分解策略
5. WHEN 某个子任务失败时, THE TaskOrchestrator SHALL 通知用户并提供重试或跳过选项

### 需求 8：智能工具选择

**用户故事：** 作为开发者，我希望创建会话时不需要指定具体工具，Agent 能根据任务自动选择最合适的。

#### 验收标准

1. THE ToolSelector SHALL 支持 `create_session` 的 `tool=auto` 模式
2. THE ToolSelector SHALL 根据以下因素选择工具：项目语言/框架、任务类型、工具可用性、历史成功率
3. THE ToolSelector SHALL 维护工具能力画像（每个工具擅长什么类型的任务）
4. THE ToolSelector SHALL 记录每次选择的结果和用户反馈，持续优化选择策略
5. IF 自动选择的工具不满足用户需求, THEN 用户可随时手动指定工具覆盖

### 需求 9：跨工具上下文共享

**用户故事：** 作为开发者，当我从 Claude 会话切换到 Codex 会话时，我希望新会话能了解之前的工作进展。

#### 验收标准

1. THE ContextBridge SHALL 维护项目级的共享上下文（文件变更历史、决策记录、架构理解）
2. WHEN 创建新会话时, THE ContextBridge SHALL 自动将相关上下文注入到新会话的初始 prompt 中
3. THE ContextBridge SHALL 从会话输出中自动提取关键信息（修改了哪些文件、做了什么决策、遇到了什么问题）
4. THE ContextBridge SHALL 支持手动添加上下文注释（用户通过 IM 说 "记住：这个项目用 monorepo 结构"）

### 需求 10：非编程能力工具集

**用户故事：** 作为开发者，我希望 MaClaw Agent 除了管理编程会话外，还能帮我做 Git 操作、环境管理等非编程任务。

#### 验收标准

1. THE Agent SHALL 提供 Git 操作工具：`git_status`、`git_diff`、`git_commit`、`git_push`
2. THE Agent SHALL 提供环境管理工具：`list_env`、`install_deps`、`check_health`
3. THE Agent SHALL 提供文件操作工具：`read_file`、`list_dir`、`search_files`
4. 这些工具 SHALL 作为内置工具注册到 ToolRegistry，不依赖外部 MCP Server
5. THE Agent SHALL 在执行写操作类非编程工具时，经过 SecurityFirewall 的风险评估

</content>
</invoke>