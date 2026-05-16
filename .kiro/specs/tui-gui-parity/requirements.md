# 需求文档：TUI/CLI 与 GUI 功能对齐

## 简介

本功能旨在补全 TUI（Bubble Tea 终端 UI）和 CLI（命令行）相对于 GUI（Wails 桌面应用）缺失的功能。核心原则：所有功能必须复用 `corelib/` 中的共享模块，禁止在 `tui/` 中重复实现业务逻辑。当前 TUI Agent 仅有 6 个工具（bash、read_file、write_file、list_directory、list_sessions、send_input），而 GUI Agent 拥有 40+ 工具。CLI 的 ClawNet、MCP 等子命令也仅覆盖了部分功能。

## 术语表

- **TUI_Agent**：运行在 TUI 交互模式中的 AI 助手 Agent 循环（`tui/agent_handler.go`），负责接收用户消息、调用 LLM、执行工具调用
- **CLI**：`maclaw-tui` 的命令行子命令模式，遵循 `maclaw-tui <command> <subcommand> [flags]` 模式
- **Firewall**：`corelib/security/Firewall`，集成 RiskAnalyzer + PolicyEngine + AuditLog 的统一安全检查组件
- **RiskAnalyzer**：`corelib/security/RiskAnalyzer`，基于正则模式匹配的工具调用风险评估器
- **PolicyEngine**：`corelib/security/PolicyEngine`，安全策略评估引擎
- **AuditLog**：`corelib/security/AuditLog`，安全审计日志记录与查询组件
- **LLMReview**：`corelib/security/LLMReview`，LLM 辅助安全审查组件
- **Router**：`corelib/tool/Router`，基于 TF-IDF 的智能工具路由选择器
- **DefinitionGenerator**：`corelib/tool/DefinitionGenerator`，动态合并 builtin + MCP 工具定义的生成器
- **Selector**：`corelib/tool/Selector`，基于能力画像的编程工具推荐器
- **SessionMonitor**：`corelib/remote/SessionMonitor`，会话状态轮询监控器
- **AutoTaskPicker**：`corelib/clawnet/AutoTaskPicker`，ClawNet 自动任务拾取器
- **NutshellManager**：`corelib/clawnet/NutshellManager`，.nut 包操作管理器
- **ClawNet_Client**：`corelib/clawnet/Client`，ClawNet P2P 网络完整 API 客户端
- **ConfigManager**：`corelib/config/Manager`，配置管理器
- **MemoryStore**：`corelib/memory/Store`，记忆存储
- **MemoryCompressor**：`corelib/memory/Compressor`，记忆压缩器

## 需求

### 需求 1：Agent 会话管理工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能创建、监控和控制编程会话，以便在终端中获得与 GUI 同等的会话管理能力。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求创建编程会话, THE TUI_Agent SHALL 提供 `create_session` 工具，接受 tool、project_path、template_name 参数并返回 session_id
2. WHEN 用户在 TUI_Agent 中请求获取会话输出, THE TUI_Agent SHALL 提供 `get_session_output` 工具，接受 session_id 和可选的 tail_lines 参数并返回会话输出文本
3. WHEN 用户在 TUI_Agent 中请求获取会话事件, THE TUI_Agent SHALL 提供 `get_session_events` 工具，接受 session_id 参数并返回事件列表
4. WHEN 用户在 TUI_Agent 中请求中断会话, THE TUI_Agent SHALL 提供 `interrupt_session` 工具，接受 session_id 参数并向会话发送中断信号
5. WHEN 用户在 TUI_Agent 中请求终止会话, THE TUI_Agent SHALL 提供 `kill_session` 工具，接受 session_id 参数并终止会话进程
6. WHEN 用户在 TUI_Agent 中请求发送输入并观察输出, THE TUI_Agent SHALL 提供 `send_and_observe` 工具，接受 session_id、text 和 wait_seconds 参数，发送输入后等待指定时间并返回新输出
7. WHEN 用户在 TUI_Agent 中请求控制会话, THE TUI_Agent SHALL 提供 `control_session` 工具，接受 session_id 和 action（pause/resume/restart）参数

### 需求 2：Agent 配置管理工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能读取和修改配置，以便无需离开聊天界面即可调整系统设置。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求读取配置, THE TUI_Agent SHALL 提供 `get_config` 工具，接受 section 和可选的 key 参数，复用 ConfigManager 返回配置值
2. WHEN 用户在 TUI_Agent 中请求更新配置, THE TUI_Agent SHALL 提供 `update_config` 工具，接受 section、key、value 参数，复用 ConfigManager 更新配置并返回旧值
3. WHEN 用户在 TUI_Agent 中请求批量更新配置, THE TUI_Agent SHALL 提供 `batch_update_config` 工具，接受 updates 数组参数（每项含 section、key、value），复用 ConfigManager 批量更新
4. WHEN 用户在 TUI_Agent 中请求查看配置模式, THE TUI_Agent SHALL 提供 `list_config_schema` 工具，复用 ConfigManager 返回所有配置节和键的描述
5. WHEN 用户在 TUI_Agent 中请求导出配置, THE TUI_Agent SHALL 提供 `export_config` 工具，复用 ConfigManager 返回完整配置 JSON
6. WHEN 用户在 TUI_Agent 中请求导入配置, THE TUI_Agent SHALL 提供 `import_config` 工具，接受 JSON 字符串参数，复用 ConfigManager 导入并返回应用/跳过计数

### 需求 3：Agent 模板管理工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能管理会话模板，以便快速创建预配置的编程会话。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求创建模板, THE TUI_Agent SHALL 提供 `create_template` 工具，接受 name、tool、project_path、description 参数并持久化模板
2. WHEN 用户在 TUI_Agent 中请求列出模板, THE TUI_Agent SHALL 提供 `list_templates` 工具，返回所有已保存模板的列表
3. WHEN 用户在 TUI_Agent 中请求从模板启动会话, THE TUI_Agent SHALL 提供 `launch_template` 工具，接受 template_name 参数并创建对应会话

### 需求 4：Agent 定时任务工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能管理定时任务，以便自动化重复性编程操作。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求创建定时任务, THE TUI_Agent SHALL 提供 `create_scheduled_task` 工具，接受 name、action、hour、minute 等参数，复用 scheduler.Manager 创建任务
2. WHEN 用户在 TUI_Agent 中请求列出定时任务, THE TUI_Agent SHALL 提供 `list_scheduled_tasks` 工具，复用 scheduler.Manager 返回任务列表
3. WHEN 用户在 TUI_Agent 中请求删除定时任务, THE TUI_Agent SHALL 提供 `delete_scheduled_task` 工具，接受 task_id 参数，复用 scheduler.Manager 删除任务
4. WHEN 用户在 TUI_Agent 中请求更新定时任务, THE TUI_Agent SHALL 提供 `update_scheduled_task` 工具，接受 task_id 和可修改字段参数，复用 scheduler.Manager 更新任务

### 需求 5：Agent 记忆管理工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能保存和检索记忆，以便在对话中积累和利用上下文知识。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求记忆操作, THE TUI_Agent SHALL 提供 `memory` 工具，接受 action（save/list/search/delete）和对应参数，复用 MemoryStore 执行操作
2. WHEN action 为 save, THE TUI_Agent SHALL 接受 content、category、tags 参数并调用 MemoryStore.Save()
3. WHEN action 为 search, THE TUI_Agent SHALL 接受 keyword、category、limit 参数并调用 MemoryStore.Search() 返回匹配条目
4. WHEN action 为 delete, THE TUI_Agent SHALL 接受 id 参数并调用 MemoryStore.Delete()

### 需求 6：Agent MCP 工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能列出和调用 MCP 服务器提供的工具，以便利用外部工具扩展能力。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求列出 MCP 工具, THE TUI_Agent SHALL 提供 `list_mcp_tools` 工具，返回所有已配置 MCP 服务器及其工具列表
2. WHEN 用户在 TUI_Agent 中请求调用 MCP 工具, THE TUI_Agent SHALL 提供 `call_mcp_tool` 工具，接受 server_id、tool_name、arguments 参数并转发调用到对应 MCP 服务器

### 需求 7：Agent 技能工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能管理和执行技能，以便复用预定义的自动化流程。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求列出技能, THE TUI_Agent SHALL 提供 `list_skills` 工具，返回本地已安装技能列表
2. WHEN 用户在 TUI_Agent 中请求搜索 SkillHub, THE TUI_Agent SHALL 提供 `search_skill_hub` 工具，接受 query 参数并返回匹配技能
3. WHEN 用户在 TUI_Agent 中请求安装 SkillHub 技能, THE TUI_Agent SHALL 提供 `install_skill_hub` 工具，接受 skill_id 参数并安装到本地
4. WHEN 用户在 TUI_Agent 中请求执行技能, THE TUI_Agent SHALL 提供 `run_skill` 工具，接受 skill_name 和 parameters 参数并执行技能步骤

### 需求 8：Agent ClawNet 工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能搜索和发布 ClawNet 知识，以便参与 P2P 知识网络。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求搜索 ClawNet 知识, THE TUI_Agent SHALL 提供 `clawnet_search` 工具，接受 query 参数，复用 ClawNet_Client.SearchKnowledge() 返回结果
2. WHEN 用户在 TUI_Agent 中请求发布 ClawNet 知识, THE TUI_Agent SHALL 提供 `clawnet_publish` 工具，接受 title 和 body 参数，复用 ClawNet_Client.PublishKnowledge() 发布

### 需求 9：Agent 审计查询工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手能查询审计日志，以便了解工具调用的安全记录。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求查询审计日志, THE TUI_Agent SHALL 提供 `query_audit_log` 工具，接受 tool_name、risk_level、start_date、end_date 等过滤参数，复用 AuditLog.Query() 返回审计条目

### 需求 10：Agent 实用工具扩展

**用户故事：** 作为 TUI 用户，我希望 AI 助手拥有文件发送、并行执行、LLM 管理、工具推荐和截图等实用工具，以便获得与 GUI 同等的操作能力。

#### 验收标准

1. WHEN 用户在 TUI_Agent 中请求发送文件, THE TUI_Agent SHALL 提供 `send_file` 工具，接受 session_id 和 file_path 参数，将文件内容发送到指定会话
2. WHEN 用户在 TUI_Agent 中请求并行执行多个命令, THE TUI_Agent SHALL 提供 `parallel_execute` 工具，接受 commands 数组参数，并发执行并返回各命令结果
3. WHEN 用户在 TUI_Agent 中请求切换 LLM 提供商, THE TUI_Agent SHALL 提供 `switch_llm_provider` 工具，接受 provider_name 参数，复用本地配置切换 LLM
4. WHEN 用户在 TUI_Agent 中请求设置最大迭代次数, THE TUI_Agent SHALL 提供 `set_max_iterations` 工具，接受 max_iterations 参数并更新 Agent 循环上限
5. WHEN 用户在 TUI_Agent 中请求工具推荐, THE TUI_Agent SHALL 提供 `recommend_tool` 工具，接受 task_description 参数，复用 Selector.Recommend() 返回推荐工具和理由
6. WHEN 用户在 TUI_Agent 中请求截图, THE TUI_Agent SHALL 提供 `screenshot` 工具，复用 `corelib/remote/screenshot.go` 截取屏幕并返回图片路径


### 需求 11：安全防火墙集成

**用户故事：** 作为 TUI 用户，我希望 Agent 工具调用受到安全防火墙保护，以便防止高风险操作在未经确认的情况下执行。

#### 验收标准

1. THE TUI_Agent SHALL 在每次工具调用执行前调用 Firewall.Check(toolName, args, ctx) 进行安全检查
2. WHEN Firewall.Check() 返回拒绝（false）, THE TUI_Agent SHALL 跳过该工具调用并将拒绝原因作为工具结果返回给 LLM
3. WHEN PolicyEngine 评估结果为 "ask", THE TUI_Agent SHALL 通过终端提示用户确认（实现 onAsk 回调），等待用户输入 y/n
4. THE TUI_Agent SHALL 将所有工具调用记录到 AuditLog，包含工具名、参数、风险等级和策略动作
5. IF Firewall 初始化失败, THEN THE TUI_Agent SHALL 以无防火墙模式继续运行并记录警告日志
6. WHERE LLMReview 已配置, THE TUI_Agent SHALL 对高风险工具调用额外执行 LLM 安全审查

### 需求 12：CLI ClawNet 任务管理完整功能

**用户故事：** 作为 CLI 用户，我希望通过命令行完整管理 ClawNet 任务生命周期，以便在终端中高效参与任务竞标和交付。

#### 验收标准

1. WHEN 用户执行 `clawnet tasks` 子命令, THE CLI SHALL 支持 bid、assign、claim、submit、approve、reject、cancel 操作，复用 ClawNet_Client 对应方法
2. WHEN 用户执行 `clawnet tasks bid --id <task_id> --message <msg>`, THE CLI SHALL 调用 ClawNet_Client.BidOnTask() 提交竞标
3. WHEN 用户执行 `clawnet tasks submit --id <task_id> --result <text>`, THE CLI SHALL 调用 ClawNet_Client.SubmitTaskResult() 提交结果
4. WHEN 用户执行 `clawnet tasks board`, THE CLI SHALL 调用 ClawNet_Client.GetTaskBoard() 显示任务看板
5. WHEN 用户执行 `clawnet tasks submissions --id <task_id>`, THE CLI SHALL 调用 ClawNet_Client.GetTaskSubmissions() 显示提交列表
6. WHEN 用户执行 `clawnet tasks pick-winner --id <task_id> --winner <peer_id>`, THE CLI SHALL 调用 ClawNet_Client.PickTaskWinner() 选择获胜者

### 需求 13：CLI ClawNet 身份与排行榜功能

**用户故事：** 作为 CLI 用户，我希望管理 ClawNet 身份和查看排行榜，以便维护网络声誉和了解社区排名。

#### 验收标准

1. WHEN 用户执行 `clawnet identity` 子命令, THE CLI SHALL 支持 has-identity、export-identity、import-identity、backup-key、restore-key 操作
2. WHEN 用户执行 `clawnet leaderboard`, THE CLI SHALL 调用 ClawNet_Client.GetLeaderboard() 显示排行榜
3. WHEN 用户执行 `clawnet transactions`, THE CLI SHALL 调用 ClawNet_Client.GetCreditsTransactions() 显示交易记录
4. WHEN 用户执行 `clawnet credits-audit`, THE CLI SHALL 调用 ClawNet_Client.GetCreditsAudit() 显示积分审计

### 需求 14：CLI ClawNet 自动任务拾取功能

**用户故事：** 作为 CLI 用户，我希望配置和控制自动任务拾取器，以便让 Agent 自动接取和完成 ClawNet 任务赚取积分。

#### 验收标准

1. WHEN 用户执行 `clawnet auto-picker status`, THE CLI SHALL 复用 AutoTaskPicker.GetStatus() 显示拾取器状态
2. WHEN 用户执行 `clawnet auto-picker configure --enabled --poll-minutes <n> --min-reward <n> --tags <t1,t2>`, THE CLI SHALL 复用 AutoTaskPicker.Configure() 更新配置
3. WHEN 用户执行 `clawnet auto-picker trigger --task <id>`, THE CLI SHALL 复用 AutoTaskPicker.PickAndExecuteTask() 手动触发任务执行

### 需求 15：CLI ClawNet 守护进程与二进制管理

**用户故事：** 作为 CLI 用户，我希望管理 ClawNet 守护进程和二进制文件，以便控制 P2P 节点的运行状态。

#### 验收标准

1. WHEN 用户执行 `clawnet daemon ensure`, THE CLI SHALL 调用 ClawNet_Client.EnsureDaemon() 确保守护进程运行
2. WHEN 用户执行 `clawnet daemon stop`, THE CLI SHALL 调用 ClawNet_Client.StopDaemon() 停止守护进程
3. WHEN 用户执行 `clawnet daemon info`, THE CLI SHALL 显示守护进程 PID 和运行状态
4. WHEN 用户执行 `clawnet binary install`, THE CLI SHALL 复用 clawnet.Download() 下载安装 ClawNet 二进制
5. WHEN 用户执行 `clawnet binary update`, THE CLI SHALL 调用 ClawNet_Client.SelfUpdate() 更新二进制
6. WHEN 用户执行 `clawnet binary path`, THE CLI SHALL 显示 ClawNet 二进制文件路径

### 需求 16：CLI ClawNet 画像管理功能

**用户故事：** 作为 CLI 用户，我希望管理 ClawNet 个人画像，以便展示身份和设置个性签名。

#### 验收标准

1. WHEN 用户执行 `clawnet profile get`, THE CLI SHALL 调用 ClawNet_Client.GetProfile() 显示个人画像
2. WHEN 用户执行 `clawnet profile update --name <name> --bio <bio>`, THE CLI SHALL 调用 ClawNet_Client.UpdateProfile() 更新画像
3. WHEN 用户执行 `clawnet profile set-motto --motto <text>`, THE CLI SHALL 调用 ClawNet_Client.SetMotto() 设置个性签名

### 需求 17：CLI MCP 完整功能

**用户故事：** 作为 CLI 用户，我希望对 MCP 服务器进行健康检查和工具调用，以便在终端中完整管理 MCP 生态。

#### 验收标准

1. WHEN 用户执行 `mcp health-check`, THE CLI SHALL 检查所有已配置 MCP 服务器的健康状态并显示结果
2. WHEN 用户执行 `mcp tools`, THE CLI SHALL 列出所有 MCP 服务器提供的工具及其参数描述
3. WHEN 用户执行 `mcp call-tool --server <id> --tool <name> --args <json>`, THE CLI SHALL 调用指定 MCP 服务器的工具并显示结果

### 需求 18：CLI NL 技能执行

**用户故事：** 作为 CLI 用户，我希望直接执行 NL 技能，以便在终端中运行自动化流程。

#### 验收标准

1. WHEN 用户执行 `nlskill execute <name>`, THE CLI SHALL 查找匹配的 NL 技能并按步骤执行其 Steps
2. IF 指定的 NL 技能不存在或状态为 disabled, THEN THE CLI SHALL 返回描述性错误信息
3. WHEN 技能步骤执行中某步失败且 on_error 为 "stop", THE CLI SHALL 停止执行并报告失败步骤

### 需求 19：CLI SkillHub 更新检查

**用户故事：** 作为 CLI 用户，我希望检查已安装技能的更新，以便保持技能版本最新。

#### 验收标准

1. WHEN 用户执行 `skillhub check-updates`, THE CLI SHALL 对比本地已安装的 Hub 技能版本与远程最新版本，列出可更新项
2. WHEN 用户执行 `skillhub update <name>`, THE CLI SHALL 从 SkillHub 下载最新版本并替换本地技能

### 需求 20：CLI LLM 迭代次数管理

**用户故事：** 作为 CLI 用户，我希望查看和设置 Agent 最大迭代次数，以便控制 AI 助手的推理深度。

#### 验收标准

1. WHEN 用户执行 `llm set-max-iterations <n>`, THE CLI SHALL 将最大迭代次数保存到本地配置
2. WHEN 用户执行 `llm get-max-iterations`, THE CLI SHALL 从本地配置读取并显示当前最大迭代次数
3. THE TUI_Agent SHALL 在启动时从配置读取 max_iterations 值，若未配置则使用默认值 20

### 需求 21：Agent 智能工具路由集成

**用户故事：** 作为 TUI 用户，我希望 Agent 能根据用户消息智能选择最相关的工具子集，以便在工具数量增多后仍保持 LLM 调用效率。

#### 验收标准

1. THE TUI_Agent SHALL 在构建工具定义时复用 DefinitionGenerator.Generate() 动态合并 builtin 和 MCP 工具定义
2. WHEN 工具总数超过 Router.MaxToolBudget（28）, THE TUI_Agent SHALL 复用 Router.Route(userMessage, allTools) 选择最相关的工具子集发送给 LLM
3. WHILE 工具总数不超过 MaxToolBudget, THE TUI_Agent SHALL 将所有工具定义直接发送给 LLM

### 需求 22：TUI 会话监控集成

**用户故事：** 作为 TUI 用户，我希望在终端 UI 中看到编程会话的实时状态变化，以便及时了解会话完成或出错。

#### 验收标准

1. THE TUI SHALL 在会话创建后调用 SessionMonitor.StartWatching() 开始监控会话状态
2. WHEN SessionMonitor 检测到会话状态从 busy 变为 waiting_input 或 exited, THE TUI SHALL 在状态栏显示通知消息
3. WHEN 会话被终止或关闭, THE TUI SHALL 调用 SessionMonitor.StopWatching() 停止监控
4. THE TUI SHALL 在退出时调用 SessionMonitor.Close() 释放所有监控资源
