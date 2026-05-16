# 需求文档：Maclaw 智能会话启动与接入

## 简介

Maclaw 当前支持三种方式启动远程编程会话：桌面端直接启动（Desktop PWA）、IM 端通过飞书/QBot 消息触发（Hub WebSocket relay）、移动端 Handoff。本需求文档定义四个方向的改进：（1）让 IM 端的会话启动更智能更流畅，包括自动项目检测、智能工具选择、更好的启动流程；（2）增加新的连接/接入方式，扩展 Maclaw 的可达性；（3）支持通过自然语言对 Maclaw 进行配置修改，提供配置管理工具；（4）构建持久化长期记忆系统，让 Maclaw Agent 能跨会话记住用户偏好、重要事实和项目知识。

## 术语表

- **Maclaw_Agent**: 运行在桌面客户端上的 AI Agent，通过 LLM 驱动工具调用，处理 IM 消息并执行任务（对应 `IMMessageHandler`）
- **Session_Manager**: 远程会话管理器，管理编程工具会话的生命周期（对应 `RemoteSessionManager`）
- **Tool_Catalog**: 工具目录，维护所有支持的编程工具元数据（对应 `remoteToolCatalog`）
- **Launch_Spec**: 会话启动规格，包含工具名、项目路径、模型配置、环境变量等（对应 `LaunchSpec`）
- **IM_Handler**: IM 消息处理器，接收飞书/QBot 消息并驱动 Agent 循环（对应 `IMMessageHandler`）
- **Hub_Client**: Hub 连接客户端，通过 WebSocket 与 Hub 后端通信（对应 `RemoteHubClient`）
- **Startup_Responder**: 启动自动响应器，处理工具启动过程中的交互式提示（对应 `startupAutoResponder`）
- **Project_Context**: 项目上下文信息，包含项目路径、语言、框架、Git 状态等
- **Session_Template**: 会话模板，预定义的工具+项目+配置组合，可一键启动
- **Bridge_Server**: 桥接服务器，为外部客户端提供标准化的 API 接入点（对应 `openclaw-bridge`）
- **CLI_Client**: 命令行客户端，通过终端命令行与 Maclaw 交互
- **Webhook_Endpoint**: Webhook 接入点，接收外部系统的 HTTP 回调触发会话操作
- **Config_Manager**: 配置管理器，提供对 AppConfig 的结构化读写能力，支持 Agent 通过工具调用修改配置（对应 `AppConfig` + `LoadConfig`/`SaveConfig`）
- **Config_Schema**: 配置 schema 描述，定义每个可配置项的名称、类型、取值范围和说明，供 LLM 理解配置结构
- **Memory_Store**: 长期记忆存储，持久化保存用户偏好、重要事实和项目知识，跨会话可召回（对应新增 `MemoryStore`）
- **Memory_Entry**: 记忆条目，包含内容、类别、关联标签和时间戳的结构化记忆单元
- **Memory_Index**: 记忆索引，支持按关键词和语义相关性检索记忆条目

## 需求

### 需求 1：IM 端自动项目检测

**用户故事：** 作为开发者，我希望通过 IM 发起编程会话时 Maclaw 能自动检测当前活跃项目，以便无需手动指定 project_path 即可快速启动。

#### 验收标准

1. WHEN IM_Handler 收到 create_session 工具调用且 project_path 参数为空时，THE Maclaw_Agent SHALL 按以下优先级自动推断项目路径：（a）当前桌面端打开的项目、（b）最近一次使用的项目、（c）用户配置的默认项目
2. WHEN Maclaw_Agent 自动推断出项目路径时，THE Maclaw_Agent SHALL 在创建会话前向用户确认推断的项目路径，并允许用户修改
3. IF Maclaw_Agent 无法推断出任何项目路径，THEN THE Maclaw_Agent SHALL 向用户展示已注册项目列表供选择
4. THE Maclaw_Agent SHALL 在会话创建响应中包含实际使用的项目路径，以便用户确认

### 需求 2：智能工具推荐增强

**用户故事：** 作为开发者，我希望通过 IM 发起编程会话时 Maclaw 能根据项目特征和任务描述自动推荐最合适的工具，以便无需记忆各工具的适用场景。

#### 验收标准

1. WHEN IM_Handler 收到 create_session 工具调用且 tool 参数为空时，THE Maclaw_Agent SHALL 根据目标项目的语言、框架和任务描述自动推荐一个编程工具
2. THE Maclaw_Agent SHALL 基于 Tool_Catalog 中各工具的安装状态和健康状态进行推荐，仅推荐状态为 "installed" 且健康的工具
3. WHEN 推荐工具时，THE Maclaw_Agent SHALL 向用户展示推荐的工具名称和推荐理由，用户可以接受推荐或指定其他工具
4. IF 所有已安装工具均不健康，THEN THE Maclaw_Agent SHALL 向用户报告工具状态并建议排查步骤

### 需求 3：会话模板（快捷启动）

**用户故事：** 作为开发者，我希望能保存常用的工具+项目+配置组合为模板，以便通过简短命令一键启动会话。

#### 验收标准

1. THE Session_Manager SHALL 支持创建 Session_Template，每个模板包含以下字段：名称、工具名、项目路径、模型配置、YoloMode 开关和自定义环境变量
2. WHEN 用户通过 IM 发送 "启动 [模板名]" 格式的消息时，THE Maclaw_Agent SHALL 识别该意图并使用对应模板的配置创建会话
3. THE Session_Manager SHALL 将 Session_Template 持久化到本地配置文件，在 Maclaw 重启后仍可使用
4. WHEN 用户请求列出所有模板时，THE Maclaw_Agent SHALL 展示模板名称、关联工具和项目路径
5. IF 模板引用的工具未安装或项目路径不存在，THEN THE Maclaw_Agent SHALL 在启动前向用户报告问题并建议修复方案

### 需求 4：会话启动状态流式反馈

**用户故事：** 作为开发者，我希望通过 IM 启动会话时能实时看到启动进度，以便了解会话是否正常启动而非等待超时。

#### 验收标准

1. WHEN Maclaw_Agent 通过 IM 创建会话时，THE Maclaw_Agent SHALL 在会话创建后立即向用户发送启动中的状态消息
2. WHILE 会话处于启动阶段（状态为 "starting"），THE Maclaw_Agent SHALL 每 3 秒向用户推送一次启动进度更新，包含当前阶段描述（如 "正在初始化工具"、"正在加载项目"、"等待工具就绪"）
3. WHEN 会话状态从 "starting" 变为 "running" 时，THE Maclaw_Agent SHALL 向用户发送启动成功通知，包含会话 ID 和工具名称
4. IF 会话在 60 秒内未进入 "running" 状态，THEN THE Maclaw_Agent SHALL 向用户发送启动超时警告，并提供重试或查看日志的选项

### 需求 5：IM 端会话恢复

**用户故事：** 作为开发者，我希望通过 IM 能快速恢复之前的会话，以便继续未完成的工作而非重新创建。

#### 验收标准

1. WHEN 用户通过 IM 发送包含 "继续" 或 "恢复" 意图的消息时，THE Maclaw_Agent SHALL 列出最近 5 个可恢复的会话（状态为 "running" 或 "paused"）
2. WHEN 用户选择恢复某个会话时，THE Maclaw_Agent SHALL 获取该会话的最近输出摘要并展示给用户，以便用户了解当前上下文
3. THE Maclaw_Agent SHALL 在恢复会话后自动进入该会话的交互模式，后续用户消息直接转发到该会话
4. IF 用户请求恢复的会话已终止（状态为 "completed" 或 "error"），THEN THE Maclaw_Agent SHALL 提示用户该会话已结束，并建议使用相同配置创建新会话

### 需求 6：CLI 命令行接入

**用户故事：** 作为开发者，我希望能通过命令行终端与 Maclaw 交互，以便在 SSH 远程环境或无 GUI 场景下使用 Maclaw 的会话管理能力。

#### 验收标准

1. THE CLI_Client SHALL 提供以下子命令：`session list`（列出会话）、`session start`（创建会话）、`session attach`（接入会话）、`session kill`（终止会话）
2. WHEN 用户执行 `session start` 命令时，THE CLI_Client SHALL 接受 `--tool`、`--project`、`--template` 参数，并通过 Hub API 创建会话
3. WHEN 用户执行 `session attach` 命令时，THE CLI_Client SHALL 建立与指定会话的实时连接，在终端中显示会话输出并接受用户输入
4. THE CLI_Client SHALL 通过 Hub 的 WebSocket API 与 Maclaw 桌面端通信，复用现有的 Hub 认证机制
5. IF CLI_Client 无法连接到 Hub，THEN THE CLI_Client SHALL 显示连接失败原因并建议检查 Hub URL 和认证配置

### 需求 7：Webhook 触发接入

**用户故事：** 作为开发者，我希望能通过 Webhook 从 CI/CD 或其他自动化系统触发 Maclaw 会话，以便将 Maclaw 集成到自动化工作流中。

#### 验收标准

1. THE Webhook_Endpoint SHALL 接受 HTTP POST 请求，请求体包含 `tool`、`project_path`、`prompt`（初始指令）和 `callback_url`（结果回调地址）字段
2. WHEN Webhook_Endpoint 收到有效请求时，THE Webhook_Endpoint SHALL 创建会话、发送初始指令，并返回会话 ID
3. WHEN 通过 Webhook 创建的会话完成时，THE Webhook_Endpoint SHALL 向 callback_url 发送 HTTP POST 请求，包含会话 ID、执行状态和结果摘要
4. THE Webhook_Endpoint SHALL 使用 Bearer Token 认证，拒绝未携带有效 Token 的请求
5. IF Webhook 请求中指定的工具未安装或项目路径无效，THEN THE Webhook_Endpoint SHALL 返回 HTTP 400 错误及描述性错误信息

### 需求 8：多设备会话漫游

**用户故事：** 作为开发者，我希望在一个设备上启动的会话能在另一个设备上无缝接入，以便在桌面和移动设备之间切换工作。

#### 验收标准

1. THE Hub_Client SHALL 将所有活跃会话的元数据（ID、工具、项目、状态）实时同步到 Hub 后端
2. WHEN 用户从另一个设备（IM/移动端/CLI）请求接入一个会话时，THE Hub_Client SHALL 通过 Hub WebSocket 建立到目标会话的中继连接
3. WHILE 多个设备同时接入同一会话时，THE Hub_Client SHALL 将会话输出广播到所有已连接的设备，输入仅接受最近一次发送输入的设备
4. WHEN 一个设备断开与会话的连接时，THE Hub_Client SHALL 保持会话继续运行，其他已连接设备不受影响

### 需求 9：启动前环境预检

**用户故事：** 作为开发者，我希望 Maclaw 在启动会话前自动检查工具和环境是否就绪，以便提前发现问题而非启动后才失败。

#### 验收标准

1. WHEN Maclaw_Agent 准备创建会话时，THE Maclaw_Agent SHALL 在调用 Session_Manager.Create 之前执行以下预检：（a）目标工具二进制文件存在且可执行、（b）项目路径存在且可访问、（c）所需的模型配置已设置
2. WHEN 预检发现工具二进制文件不存在时，THE Maclaw_Agent SHALL 向用户展示该工具的安装指引（对应 Tool_Catalog 中的 ReadinessHint）
3. WHEN 预检发现模型配置缺失时，THE Maclaw_Agent SHALL 向用户提示需要配置的具体项（如 API Key、模型名称）
4. IF 所有预检通过，THEN THE Maclaw_Agent SHALL 在启动确认消息中标注 "环境就绪" 状态
5. THE Maclaw_Agent SHALL 在 3 秒内完成所有预检项，预检超时的项标记为 "未知" 状态而非阻塞启动

### 需求 10：IM 端自然语言启动

**用户故事：** 作为开发者，我希望通过自然语言描述任务即可启动会话，以便无需了解 create_session 的参数格式。

#### 验收标准

1. WHEN 用户通过 IM 发送包含编程任务描述的消息（如 "帮我用 Claude 修复 myproject 的 bug"）时，THE Maclaw_Agent SHALL 从消息中提取工具名称、项目标识和任务描述
2. WHEN Maclaw_Agent 成功提取启动参数时，THE Maclaw_Agent SHALL 自动调用 create_session 创建会话，并将任务描述作为初始指令发送到会话
3. IF Maclaw_Agent 无法从消息中确定工具名称，THEN THE Maclaw_Agent SHALL 使用智能工具推荐（需求 2）自动选择工具
4. IF Maclaw_Agent 无法从消息中确定项目路径，THEN THE Maclaw_Agent SHALL 使用自动项目检测（需求 1）推断项目
5. THE Maclaw_Agent SHALL 在创建会话前向用户确认解析出的参数（工具、项目、任务），用户确认后执行

### 需求 11：Launch_Spec 序列化与反序列化

**用户故事：** 作为开发者，我希望会话模板和启动配置能可靠地序列化和反序列化，以便在存储、传输和恢复时不丢失信息。

#### 验收标准

1. THE Session_Manager SHALL 将 Session_Template 序列化为 JSON 格式，包含所有字段（名称、工具名、项目路径、模型配置、YoloMode、环境变量）
2. THE Session_Manager SHALL 将 JSON 格式的 Session_Template 反序列化为内存结构体
3. FOR ALL 有效的 Session_Template 对象，序列化后再反序列化 SHALL 产生与原始对象等价的结果（round-trip 属性）
4. IF 反序列化的 JSON 缺少必填字段（名称或工具名），THEN THE Session_Manager SHALL 返回描述性错误信息

### 需求 12：自然语言配置查询

**用户故事：** 作为开发者，我希望通过 IM 用自然语言查询 Maclaw 的当前配置状态，以便无需打开桌面端 UI 即可了解配置情况。

#### 验收标准

1. WHEN 用户通过 IM 发送配置查询意图的消息（如 "当前 Claude 用的什么模型"、"远程模式开了吗"、"项目列表"）时，THE Maclaw_Agent SHALL 调用 `get_config` 工具读取对应的配置项并以可读格式返回
2. THE Config_Manager SHALL 提供 `get_config` 工具，接受 `section` 参数（如 "claude"、"remote"、"projects"、"maclaw_llm"、"mcp_servers"），返回该 section 的当前配置值
3. WHEN `section` 参数为 "all" 或为空时，THE Config_Manager SHALL 返回配置概览，包含各工具的当前模型、项目数量、远程模式状态、Maclaw LLM 状态等摘要信息
4. THE Config_Manager SHALL 对敏感字段（API Key、Token）进行脱敏处理，仅展示前 4 位和后 4 位字符

### 需求 13：自然语言配置修改

**用户故事：** 作为开发者，我希望通过 IM 用自然语言修改 Maclaw 的配置，以便无需打开桌面端 UI 即可完成常用配置变更。

#### 验收标准

1. WHEN 用户通过 IM 发送配置修改意图的消息（如 "把 Claude 的模型切换到 DeepSeek"、"开启远程模式"、"添加一个新项目 /home/user/myapp"）时，THE Maclaw_Agent SHALL 解析意图并调用 `update_config` 工具执行配置变更
2. THE Config_Manager SHALL 提供 `update_config` 工具，接受 `section`（配置区域）、`key`（配置项）和 `value`（新值）参数，执行配置修改并调用 `SaveConfig` 持久化
3. WHEN Config_Manager 执行配置修改前，THE Config_Manager SHALL 向用户展示变更预览（旧值 → 新值），用户确认后才执行实际修改
4. IF 用户提供的 `value` 不在合法取值范围内（如指定了不存在的模型名称），THEN THE Config_Manager SHALL 返回错误信息并列出合法取值
5. THE Config_Manager SHALL 在配置修改成功后触发 `OnConfigChanged` 回调，确保桌面端 UI 实时同步更新

### 需求 14：配置 Schema 自描述

**用户故事：** 作为开发者，我希望 Maclaw Agent 能理解所有可配置项的含义和约束，以便在自然语言交互中给出准确的配置建议。

#### 验收标准

1. THE Config_Schema SHALL 为每个可配置 section 定义以下元数据：section 名称、中文描述、包含的 key 列表
2. THE Config_Schema SHALL 为每个可配置 key 定义以下元数据：key 名称、中文描述、数据类型（string/bool/int/enum/list）、默认值、取值范围（enum 类型列出所有合法值）
3. THE Config_Manager SHALL 提供 `list_config_schema` 工具，返回所有可配置项的 schema 信息，供 Maclaw_Agent 在系统提示词中使用
4. THE Config_Schema SHALL 覆盖以下配置区域：工具模型选择（claude/gemini/codex/opencode/iflow/kilo/cursor）、项目管理（projects）、远程设置（remote_*）、代理设置（default_proxy_*）、Maclaw LLM 配置（maclaw_llm_*）、MCP Server 管理（mcp_servers）、通用设置（language/power_optimization/active_tool 等）

### 需求 15：批量配置操作

**用户故事：** 作为开发者，我希望能通过一条消息完成多项配置变更，以便高效地初始化或迁移配置。

#### 验收标准

1. THE Config_Manager SHALL 提供 `batch_update_config` 工具，接受一个配置变更列表，每项包含 `section`、`key` 和 `value`，在单次操作中应用所有变更
2. WHEN 批量变更中任一项校验失败时，THE Config_Manager SHALL 中止整个批量操作，返回失败项的错误信息，不执行任何变更（原子性）
3. THE Config_Manager SHALL 在批量操作成功后仅触发一次 `SaveConfig` 和 `OnConfigChanged`，避免多次写入和 UI 刷新

### 需求 16：配置导出与导入

**用户故事：** 作为开发者，我希望能通过 IM 导出和导入 Maclaw 配置，以便在多台设备间同步配置或备份恢复。

#### 验收标准

1. WHEN 用户通过 IM 请求导出配置时，THE Config_Manager SHALL 将当前 AppConfig 序列化为 JSON 并返回（敏感字段脱敏）
2. WHEN 用户通过 IM 提供一个配置 JSON 并请求导入时，THE Config_Manager SHALL 校验 JSON 格式和字段合法性，展示变更差异预览，用户确认后应用
3. IF 导入的配置 JSON 包含未知字段，THEN THE Config_Manager SHALL 忽略未知字段并在导入报告中标注
4. THE Config_Manager SHALL 在导入时保留本机特有的字段（如 remote_machine_id、remote_machine_token），不被导入内容覆盖

### 需求 17：长期记忆存储与持久化

**用户故事：** 作为开发者，我希望 Maclaw 能记住我告诉它的重要信息（如我的名字、偏好、项目约定），以便下次对话时不需要重复说明。

#### 验收标准

1. THE Memory_Store SHALL 将 Memory_Entry 持久化到本地文件系统（`~/.maclaw/memories.json`），确保 Maclaw 重启或对话过期后记忆不丢失
2. THE Memory_Entry SHALL 包含以下字段：`id`（唯一标识）、`content`（记忆内容）、`category`（类别：user_fact/preference/project_knowledge/instruction）、`tags`（关联标签列表）、`created_at`（创建时间）、`updated_at`（更新时间）、`access_count`（访问次数）
3. THE Memory_Store SHALL 支持最多 500 条 Memory_Entry，超出时按 LRU（最近最少访问）策略淘汰最旧且访问次数最低的条目
4. THE Memory_Store SHALL 在每次写入操作后 5 秒内将变更持久化到磁盘，使用写入合并（debounce）避免频繁 IO

### 需求 18：记忆写入工具

**用户故事：** 作为开发者，我希望 Maclaw Agent 能在对话中自动识别值得记住的信息并主动存储，也能响应我的明确指令保存记忆。

#### 验收标准

1. THE Maclaw_Agent SHALL 拥有 `save_memory` 工具，接受 `content`（记忆内容）、`category`（类别）和 `tags`（标签列表）参数，将信息存入 Memory_Store
2. WHEN 用户明确要求记住某信息时（如 "记住我叫马二"、"以后都用 Claude"），THE Maclaw_Agent SHALL 调用 `save_memory` 工具存储该信息
3. WHEN Maclaw_Agent 在对话中识别到用户偏好或重要事实（如用户反复使用某个工具、提到项目特定约定）时，THE Maclaw_Agent SHOULD 主动调用 `save_memory` 存储，无需用户明确指示
4. IF Memory_Store 中已存在语义相同的记忆条目，THEN THE Memory_Store SHALL 更新该条目的 `updated_at` 和 `access_count`，而非创建重复条目

### 需求 19：记忆召回与注入

**用户故事：** 作为开发者，我希望 Maclaw 在每次对话开始时自动加载相关记忆，以便它能像一个了解我的助手一样工作。

#### 验收标准

1. WHEN Maclaw_Agent 构建系统提示词时，THE Maclaw_Agent SHALL 从 Memory_Store 中检索与当前上下文相关的记忆条目，并注入到系统提示词的 "用户记忆" 区域
2. THE Memory_Index SHALL 支持两种检索方式：（a）按 category 过滤（如始终加载所有 user_fact 类记忆）、（b）按 tags 和关键词与用户消息的相关性排序
3. THE Maclaw_Agent SHALL 在每次对话中注入最多 20 条最相关的记忆条目，总 token 数不超过 2000
4. WHEN 记忆被成功召回并使用时，THE Memory_Store SHALL 更新对应条目的 `access_count`，使高频使用的记忆在 LRU 淘汰中获得更高优先级

### 需求 20：记忆管理工具

**用户故事：** 作为开发者，我希望能查看、搜索和删除 Maclaw 的记忆，以便管理和纠正不准确的记忆。

#### 验收标准

1. THE Maclaw_Agent SHALL 拥有 `list_memories` 工具，接受可选的 `category` 和 `keyword` 参数，返回匹配的记忆条目列表（包含 id、content、category、tags、created_at）
2. THE Maclaw_Agent SHALL 拥有 `delete_memory` 工具，接受 `id` 参数，删除指定的记忆条目
3. WHEN 用户通过 IM 请求查看记忆（如 "你记得什么"、"你知道我叫什么"）时，THE Maclaw_Agent SHALL 调用 `list_memories` 检索相关记忆并展示
4. WHEN 用户要求忘记某信息（如 "忘掉我的名字"、"删除关于 XX 的记忆"）时，THE Maclaw_Agent SHALL 调用 `delete_memory` 删除对应条目并确认

### 需求 21：对话摘要自动归档

**用户故事：** 作为开发者，我希望 Maclaw 能在长对话结束时自动提取关键信息存入记忆，以便重要的对话结论不会随对话过期而丢失。

#### 验收标准

1. WHEN 对话记忆因 TTL 过期（当前 2 小时）即将被清除时，THE Maclaw_Agent SHALL 调用 LLM 对即将过期的对话历史生成摘要，提取其中的用户偏好、决策结论和重要事实
2. THE Maclaw_Agent SHALL 将提取的摘要信息作为 category="conversation_summary" 的 Memory_Entry 存入 Memory_Store
3. IF 对话中未产生有价值的信息（如仅包含简单问答），THEN THE Maclaw_Agent SHALL 跳过摘要归档
4. WHILE Maclaw LLM 未配置时，THE Maclaw_Agent SHALL 跳过对话摘要归档流程
