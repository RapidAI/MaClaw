# 需求文档：Maclaw 能力升级改进计划

## 简介

Maclaw 是一个 AI 编程工具调度平台（桌面客户端 + Hub 后端），实际编程由独立编程工具（Claude/Codex/Gemini 等）完成，Maclaw 负责调度与非编程能力。本需求文档定义 Maclaw 在以下五个方向的能力升级：动态工具发现、安全防火墙、经验记忆管理、Skills 备份与恢复、差异化编排能力。

## 术语表

- **Maclaw_Agent**: 运行在桌面客户端上的 AI Agent，通过 LLM 驱动工具调用，处理 IM 消息并执行任务（对应 `IMMessageHandler`）
- **MCP_Registry**: MCP Server 注册表，管理已注册的 MCP Server 及其工具列表（对应 `MCPRegistry`）
- **MCP_Server**: 遵循 MCP 协议的外部工具服务器，通过 JSON-RPC 提供工具调用能力
- **Skill_Executor**: Skill 执行器，管理和执行本地定义的 NL Skill（对应 `SkillExecutor`）
- **NL_Skill**: 自然语言 Skill，由名称、描述、触发条件和步骤序列组成的可复用操作模式（对应 `NLSkillEntry`）
- **Permission_Handler**: 权限处理器，管理工具调用的权限审批（对应 `PermissionHandler`）
- **Tool_Catalog**: 工具目录，维护所有支持的编程工具元数据（对应 `remoteToolCatalog`）
- **Session_Manager**: 远程会话管理器，管理编程工具会话的生命周期（对应 `RemoteSessionManager`）
- **Policy_Engine**: 策略引擎，基于规则评估工具调用的安全策略
- **Audit_Log**: 审计日志，记录所有工具调用的完整链路信息
- **Experience_Extractor**: 经验提取器，从会话历史中提取有价值的操作模式
- **Skill_Backup**: Skill 备份包，包含所有 Skill 定义的 zip 归档文件
- **Tool_Router**: 工具路由器，根据用户意图自动筛选和推荐相关工具
- **Orchestrator**: 编排器，协调多个编程工具会话的并行执行和上下文共享

## 需求

### 需求 1：MCP Server 自动发现

**用户故事：** 作为开发者，我希望 Maclaw 能自动发现局域网内和项目中声明的 MCP Server，以便无需手动注册即可使用新工具。

#### 验收标准

1. WHEN Maclaw_Agent 启动时，THE MCP_Registry SHALL 通过 mDNS/DNS-SD 协议扫描局域网内广播了 `_mcp._tcp` 服务类型的 MCP_Server，并将发现的服务器添加到候选列表
2. WHEN 用户打开一个包含 `.mcp/servers.json` 配置文件的项目目录时，THE MCP_Registry SHALL 解析该配置文件并自动注册其中声明的 MCP_Server
3. WHILE MCP_Registry 包含已注册的 MCP_Server，THE MCP_Registry SHALL 每 60 秒对所有已注册服务器执行健康检查，并更新其健康状态
4. WHEN 一个通过自动发现添加的 MCP_Server 连续 3 次健康检查失败时，THE MCP_Registry SHALL 将该服务器标记为 "unavailable" 状态并从 Maclaw_Agent 的活跃工具列表中移除
5. IF 自动发现的 MCP_Server 与已手动注册的服务器具有相同的 ID，THEN THE MCP_Registry SHALL 保留手动注册的配置并忽略自动发现的条目

### 需求 2：工具能力动态索引

**用户故事：** 作为开发者，我希望 Maclaw Agent 的工具列表能根据已注册 MCP Server 的实际工具动态生成，以便 Agent 能使用所有可用工具而非仅限于硬编码的 12 个。

#### 验收标准

1. WHEN MCP_Registry 中的服务器列表发生变化（注册、注销、健康状态变更）时，THE Maclaw_Agent SHALL 在 5 秒内重新生成工具定义列表，将所有健康 MCP_Server 的工具合并到 Agent 可用工具集中
2. THE Maclaw_Agent SHALL 保留当前 12 个核心工具（会话管理、MCP 调用、Skill 执行）作为内置工具，并将动态发现的 MCP 工具作为扩展工具追加
3. WHEN 动态工具的名称与内置工具名称冲突时，THE Maclaw_Agent SHALL 为动态工具添加 MCP_Server ID 前缀以确保名称唯一性
4. THE Maclaw_Agent SHALL 为每个动态工具生成符合 OpenAI function calling 格式的工具定义，包含名称、描述和参数 schema

### 需求 3：上下文感知的工具路由

**用户故事：** 作为开发者，我希望 Maclaw Agent 能根据我的意图自动筛选相关工具，以便在工具数量较多时 Agent 仍能高效选择正确的工具。

#### 验收标准

1. WHEN 可用工具总数超过 20 个时，THE Tool_Router SHALL 根据用户消息的语义内容筛选出最相关的工具子集（上限 15 个）提供给 LLM
2. THE Tool_Router SHALL 始终包含 12 个内置核心工具，仅对动态 MCP 工具进行筛选
3. WHEN Tool_Router 执行工具筛选时，THE Tool_Router SHALL 基于工具名称和描述与用户意图的语义相似度进行排序
4. IF Tool_Router 筛选后用户所需的工具未被包含，THEN THE Maclaw_Agent SHALL 在下一轮对话中将该工具加入候选列表

### 需求 4：意图级风险评估

**用户故事：** 作为开发者，我希望 Maclaw 的权限系统能分析工具调用参数的语义风险，而非仅根据工具名称判断，以便更精确地控制危险操作。

#### 验收标准

1. WHEN Permission_Handler 收到工具调用权限请求时，THE Permission_Handler SHALL 分析工具名称和调用参数，生成一个风险等级（low/medium/high/critical）
2. THE Permission_Handler SHALL 将包含 `rm -rf`、`DROP TABLE`、`format`、`sudo` 等关键词的参数标记为 "critical" 风险等级
3. THE Permission_Handler SHALL 将文件写入、命令执行类操作标记为至少 "medium" 风险等级
4. THE Permission_Handler SHALL 将只读查询类操作标记为 "low" 风险等级
5. WHILE Permission_Handler 处于 "default" 模式，THE Permission_Handler SHALL 自动批准 "low" 风险操作，对 "medium" 及以上风险操作请求用户确认

### 需求 5：LLM 辅助安全审查

**用户故事：** 作为开发者，我希望对高风险操作使用 LLM 进行安全审查，以便在复杂场景下获得更智能的安全判断。

#### 验收标准

1. WHEN 一个工具调用被评估为 "high" 或 "critical" 风险等级时，THE Permission_Handler SHALL 调用配置的 LLM 对该操作进行安全审查
2. THE Permission_Handler SHALL 向 LLM 提供工具名称、调用参数、当前会话上下文和风险等级，并要求 LLM 返回 "safe"、"risky" 或 "dangerous" 的判断
3. WHEN LLM 安全审查返回 "dangerous" 时，THE Permission_Handler SHALL 拒绝该操作并向用户展示拒绝原因
4. IF LLM 安全审查调用在 5 秒内未返回结果，THEN THE Permission_Handler SHALL 回退到基于规则的风险评估结果进行决策
5. WHILE Maclaw LLM 未配置时，THE Permission_Handler SHALL 跳过 LLM 安全审查，仅使用基于规则的风险评估

### 需求 6：策略引擎

**用户故事：** 作为开发者，我希望能定义灵活的安全策略规则，以便根据项目需求定制权限控制行为。

#### 验收标准

1. THE Policy_Engine SHALL 支持四种策略动作：allow（自动批准）、deny（自动拒绝）、ask（请求用户确认）、audit（批准但记录审计日志）
2. THE Policy_Engine SHALL 支持基于工具名称、参数模式、风险等级和会话上下文的规则匹配
3. WHEN 多条策略规则匹配同一个工具调用时，THE Policy_Engine SHALL 按优先级从高到低评估，使用第一条匹配的规则
4. THE Policy_Engine SHALL 提供默认策略集，对所有 "critical" 风险操作执行 "ask" 动作，对 "low" 风险操作执行 "allow" 动作
5. WHEN 用户通过配置文件修改策略规则时，THE Policy_Engine SHALL 在 Maclaw_Agent 下次处理权限请求时使用更新后的规则

### 需求 7：审计日志

**用户故事：** 作为开发者，我希望所有工具调用都有完整的审计记录，以便事后追溯操作历史和排查问题。

#### 验收标准

1. THE Audit_Log SHALL 为每次工具调用记录以下信息：时间戳、用户 ID、会话 ID、工具名称、调用参数、风险等级、策略决策、执行结果
2. THE Audit_Log SHALL 将审计记录持久化到本地文件系统，每个日志文件按日期分割
3. WHEN 审计日志文件超过 50MB 时，THE Audit_Log SHALL 自动轮转到新文件并保留最近 30 天的日志
4. THE Audit_Log SHALL 提供按时间范围、工具名称和风险等级查询审计记录的接口

### 需求 8：会话经验提取

**用户故事：** 作为开发者，我希望 Maclaw 能从成功的会话中自动提取有价值的操作模式，以便这些经验不会随会话结束而丢失。

#### 验收标准

1. WHEN 一个编程工具会话正常结束（状态为 "completed"）时，THE Experience_Extractor SHALL 分析该会话的操作历史，识别可复用的操作模式
2. THE Experience_Extractor SHALL 调用配置的 LLM 对会话历史进行分析，提取操作模式的名称、描述、触发条件和步骤序列
3. WHEN Experience_Extractor 提取到一个新的操作模式时，THE Experience_Extractor SHALL 将其转换为 NL_Skill 格式并提交给 Skill_Executor 注册
4. IF 提取的操作模式与已有 NL_Skill 的名称相同，THEN THE Experience_Extractor SHALL 比较两者的步骤序列，仅在新模式包含更多步骤或更详细的参数时更新已有 Skill
5. WHILE Maclaw LLM 未配置时，THE Experience_Extractor SHALL 跳过经验提取流程

### 需求 9：经验跨项目复用

**用户故事：** 作为开发者，我希望在一个项目中学到的经验能在其他项目中使用，以便 Maclaw 的能力持续增强。

#### 验收标准

1. THE Skill_Executor SHALL 将所有 NL_Skill（手动创建和自动提取的）存储在全局配置中，使其在所有项目中可用
2. WHEN Maclaw_Agent 构建系统提示词时，THE Maclaw_Agent SHALL 将所有状态为 "active" 的 NL_Skill 的名称和描述包含在提示词中
3. THE NL_Skill SHALL 包含一个 "source" 字段，标识该 Skill 的来源（"manual" 表示手动创建，"learned" 表示自动提取），以及一个 "source_project" 字段记录提取来源项目
4. WHEN 用户列出 NL_Skill 时，THE Skill_Executor SHALL 在每个 Skill 的信息中展示其来源类型和来源项目

### 需求 10：Skills 备份

**用户故事：** 作为开发者，我希望能将所有 Skills 备份到一个文件中，以便在设备迁移或重装时恢复。

#### 验收标准

1. WHEN 用户触发 Skills 备份操作时，THE Skill_Executor SHALL 将所有 NL_Skill 定义序列化为 JSON 格式，并打包为一个 zip 文件
2. THE Skill_Executor SHALL 在 zip 文件中包含一个 `manifest.json` 文件，记录备份时间、Skill 数量和 Maclaw 版本
3. THE Skill_Executor SHALL 在 zip 文件中为每个 NL_Skill 创建一个独立的 JSON 文件，文件名为 Skill 名称的 kebab-case 格式

### 需求 11：Skills 恢复

**用户故事：** 作为开发者，我希望能从备份文件中恢复 Skills，以便快速恢复之前积累的能力。

#### 验收标准

1. WHEN 用户提供一个 Skills 备份 zip 文件时，THE Skill_Executor SHALL 解析 zip 文件中的 `manifest.json` 和所有 Skill JSON 文件
2. WHEN 恢复过程中发现 zip 文件中的 Skill 与本地已有 Skill 名称相同时，THE Skill_Executor SHALL 跳过该 Skill 并在恢复报告中标记为 "skipped (duplicate)"
3. THE Skill_Executor SHALL 在恢复完成后返回一个报告，包含成功恢复的 Skill 数量、跳过的 Skill 数量和失败的 Skill 数量
4. IF zip 文件格式无效或 `manifest.json` 缺失，THEN THE Skill_Executor SHALL 返回描述性错误信息并中止恢复操作

### 需求 12：多工具编排

**用户故事：** 作为开发者，我希望 Maclaw 能同时操作多个编程工具会话，以便并行处理不同的任务。

#### 验收标准

1. THE Orchestrator SHALL 支持同时管理最多 5 个活跃的编程工具会话
2. WHEN 用户请求并行执行多个任务时，THE Orchestrator SHALL 为每个任务创建独立的编程工具会话，并跟踪各会话的执行状态
3. WHEN 所有并行任务完成时，THE Orchestrator SHALL 汇总各会话的执行结果并向用户返回统一的结果报告
4. IF 任一并行会话执行失败，THEN THE Orchestrator SHALL 在结果报告中标记失败的会话及其错误信息，其他会话继续执行

### 需求 13：跨工具上下文共享

**用户故事：** 作为开发者，我希望不同编程工具会话之间能共享上下文信息，以便协同完成复杂任务。

#### 验收标准

1. THE Orchestrator SHALL 维护一个共享上下文存储，允许不同会话之间传递键值对形式的上下文数据
2. WHEN 一个会话产生重要事件（文件修改、命令执行结果）时，THE Orchestrator SHALL 将该事件摘要写入共享上下文存储
3. WHEN 向一个会话发送输入时，THE Orchestrator SHALL 将共享上下文中与该会话相关的信息附加到输入提示中
4. THE Orchestrator SHALL 限制共享上下文存储的总大小为 100KB，超出时按 FIFO 策略淘汰最早的条目

### 需求 14：智能工具选择

**用户故事：** 作为开发者，我希望 Maclaw 能根据任务特征自动选择最合适的编程工具，以便获得最佳的执行效果。

#### 验收标准

1. WHEN 用户提交一个编程任务但未指定工具时，THE Orchestrator SHALL 根据任务描述和已安装工具的能力特征推荐一个编程工具
2. THE Orchestrator SHALL 维护每个编程工具的能力画像，包含擅长的语言、框架和任务类型
3. WHEN 推荐工具时，THE Orchestrator SHALL 优先选择已安装且健康状态为 "installed" 的工具
4. THE Orchestrator SHALL 向用户展示推荐的工具及推荐理由，用户可以接受推荐或手动选择其他工具

### 需求 15：上下文感知的风险评估

**用户故事：** 作为开发者，我希望权限系统能考虑操作的上下文环境，以便同样的操作在不同场景下获得不同的风险评估。

#### 验收标准

1. WHEN Permission_Handler 评估风险等级时，THE Permission_Handler SHALL 将当前项目路径纳入评估因素，对系统目录（如 `/etc`、`/usr`、`C:\Windows`）下的写操作提升一个风险等级
2. WHEN Permission_Handler 评估风险等级时，THE Permission_Handler SHALL 将当前会话的权限模式纳入评估因素，在 "read-only" 模式下将所有写操作标记为 "critical"
3. WHEN 同一工具在同一会话中被连续调用超过 10 次时，THE Permission_Handler SHALL 将后续调用的风险等级提升一级，以防止自动化循环中的意外操作

### 需求 16：Skill 序列化与反序列化

**用户故事：** 作为开发者，我希望 Skill 的导入导出格式是稳定且可验证的，以便在不同版本的 Maclaw 之间安全迁移。

#### 验收标准

1. THE Skill_Executor SHALL 将 NL_Skill 序列化为 JSON 格式，包含所有字段（name、description、triggers、steps、status、created_at、source、source_project）
2. THE Skill_Executor SHALL 将 JSON 格式的 NL_Skill 反序列化为 NLSkillEntry 结构体
3. FOR ALL 有效的 NL_Skill 对象，序列化后再反序列化 SHALL 产生与原始对象等价的结果（round-trip 属性）
4. IF 反序列化的 JSON 缺少必填字段（name 或 steps），THEN THE Skill_Executor SHALL 返回描述性错误信息
