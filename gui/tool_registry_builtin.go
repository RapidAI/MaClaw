package main

import "fmt"

// registerBuiltinTools registers all built-in tools into the ToolRegistry.
// Each tool's Handler delegates to the corresponding IMMessageHandler method.
// This replaces the hardcoded buildToolDefinitions() + executeTool() switch-case.
func registerBuiltinTools(registry *ToolRegistry, h *IMMessageHandler) {
	// Helper to build InputSchema from simple property maps.
	props := func(m map[string]interface{}) map[string]interface{} {
		if m == nil {
			return map[string]interface{}{}
		}
		return m
	}

	reg := func(name, desc string, cat ToolCategory, tags []string, schema map[string]interface{}, required []string, handler ToolHandler) {
		registry.Register(RegisteredTool{
			Name:        name,
			Description: desc,
			Category:    cat,
			Tags:        tags,
			Priority:    0,
			Status:      RegToolAvailable,
			InputSchema: props(schema),
			Required:    required,
			Source:      "builtin",
			Handler:     handler,
		})
	}

	regP := func(name, desc string, cat ToolCategory, tags []string, schema map[string]interface{}, required []string, handler ToolHandlerWithProgress) {
		registry.Register(RegisteredTool{
			Name:        name,
			Description: desc,
			Category:    cat,
			Tags:        tags,
			Priority:    0,
			Status:      RegToolAvailable,
			InputSchema: props(schema),
			Required:    required,
			Source:      "builtin",
			HandlerProg: handler,
		})
	}

	// --- Session management tools ---
	reg("list_sessions", "列出当前所有远程会话及其状态",
		ToolCategoryBuiltin, []string{"session", "list"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolListSessions() })

	reg("create_session", "创建远程编程会话。创建后编程工具会等待输入，需用 send_and_observe 发送编程指令。如果用户需求模糊，建议先澄清再创建。",
		ToolCategoryBuiltin, []string{"session", "create", "launch"},
		map[string]interface{}{
			"tool":         map[string]string{"type": "string", "description": "工具名称，如 claude, codex, cursor, gemini, opencode"},
			"project_path": map[string]string{"type": "string", "description": "项目路径（可选）"},
			"project_id":   map[string]string{"type": "string", "description": "预设项目 ID（可选，与 project_path 二选一）"},
			"provider":     map[string]string{"type": "string", "description": "服务商名称（可选，如 Original, DeepSeek, 百度千帆）。不指定则使用桌面端当前选中的服务商"},
		}, []string{"tool"},
		func(args map[string]interface{}) string { return h.toolCreateSession(args) })

	reg("project_manage", "项目管理（创建/列出/删除/切换项目）",
		ToolCategoryBuiltin, []string{"project", "list", "create", "delete", "switch"},
		map[string]interface{}{
			"action": map[string]string{"type": "string", "description": "操作: create/list/delete/switch"},
			"name":   map[string]string{"type": "string", "description": "项目名称（create 必填）"},
			"path":   map[string]string{"type": "string", "description": "项目路径（create 必填）"},
			"target": map[string]string{"type": "string", "description": "项目名称或 ID（delete/switch 必填）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolProjectManage(args) })

	reg("list_providers", "列出指定编程工具的所有可用服务商（已过滤未配置的空服务商）",
		ToolCategoryBuiltin, []string{"provider", "list", "model"},
		map[string]interface{}{
			"tool": map[string]string{"type": "string", "description": "工具名称，如 claude, codex, gemini"},
		}, []string{"tool"},
		func(args map[string]interface{}) string { return h.toolListProviders(args) })

	reg("send_input", "向指定会话发送文本输入。发送后可用 get_session_output 观察结果。",
		ToolCategoryBuiltin, []string{"session", "input", "send"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			"text":       map[string]string{"type": "string", "description": "要发送的文本"},
		}, []string{"session_id", "text"},
		func(args map[string]interface{}) string { return h.toolSendInput(args) })

	reg("get_session_output", "获取指定会话的最近输出内容和状态摘要。",
		ToolCategoryBuiltin, []string{"session", "output", "status"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			"lines":      map[string]string{"type": "integer", "description": "返回最近 N 行输出（默认 30，最大 100）"},
		}, []string{"session_id"},
		func(args map[string]interface{}) string { return h.toolGetSessionOutput(args) })

	reg("get_session_events", "获取指定会话的重要事件列表（文件修改、命令执行、错误等）",
		ToolCategoryBuiltin, []string{"session", "events"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "会话 ID"},
		}, []string{"session_id"},
		func(args map[string]interface{}) string { return h.toolGetSessionEvents(args) })

	reg("interrupt_session", "中断指定会话（发送 Ctrl+C 信号）",
		ToolCategoryBuiltin, []string{"session", "interrupt", "cancel"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "会话 ID"},
		}, []string{"session_id"},
		func(args map[string]interface{}) string { return h.toolInterruptSession(args) })

	reg("kill_session", "终止指定会话",
		ToolCategoryBuiltin, []string{"session", "kill", "stop"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "会话 ID"},
		}, []string{"session_id"},
		func(args map[string]interface{}) string { return h.toolKillSession(args) })

	// --- Merged tools (optimized for fewer LLM round-trips) ---

	reg("send_and_observe", "向会话发送文本并等待返回输出结果（合并了 send_input + get_session_output，推荐优先使用此工具代替分别调用 send_input 和 get_session_output）",
		ToolCategoryBuiltin, []string{"session", "input", "send", "output", "observe"},
		map[string]interface{}{
			"session_id":      map[string]string{"type": "string", "description": "会话 ID"},
			"text":            map[string]string{"type": "string", "description": "要发送的文本"},
			"timeout_seconds": map[string]string{"type": "number", "description": "可选：等待输出的超时秒数（默认约 30 秒，最大 120 秒）。对于复杂编程任务可设置更长时间。"},
		}, []string{"session_id", "text"},
		func(args map[string]interface{}) string { return h.toolSendAndObserve(args) })

	reg("control_session", "控制会话：中断（interrupt）或终止（kill）",
		ToolCategoryBuiltin, []string{"session", "interrupt", "kill", "stop", "cancel", "control"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			"action":     map[string]string{"type": "string", "description": "操作类型: interrupt（发送 Ctrl+C）或 kill（终止会话）"},
		}, []string{"session_id", "action"},
		func(args map[string]interface{}) string { return h.toolControlSession(args) })

	reg("manage_config", "管理 MaClaw 配置。action 可选: get（读取配置）、update（修改单项）、batch_update（批量修改）、list_schema（查看所有可配置项）、export（导出）、import（导入）",
		ToolCategoryBuiltin, []string{"config", "settings", "get", "update", "export", "import"},
		map[string]interface{}{
			"action":  map[string]string{"type": "string", "description": "操作类型: get/update/batch_update/list_schema/export/import"},
			"section": map[string]string{"type": "string", "description": "配置区域（get/update 时使用，如 claude/gemini/remote/maclaw_llm）"},
			"key":     map[string]string{"type": "string", "description": "配置项名称（update 时必填）"},
			"value":   map[string]string{"type": "string", "description": "新值（update 时必填）"},
			"changes": map[string]string{"type": "string", "description": "JSON 数组（batch_update 时必填）"},
			"json_data": map[string]string{"type": "string", "description": "配置 JSON（import 时必填）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolManageConfig(args) })

	reg("screenshot", "截取屏幕截图并发送给用户。仅在以下情况使用：(1) 用户明确要求截屏；(2) 用户通过 IM 远程监督，需要确认操作结果。不要在用户未要求时主动截屏。最小间隔 30 秒。支持 display 参数指定显示器（0=主屏，1=第二屏，不传=所有屏幕拼图）。",
		ToolCategoryBuiltin, []string{"session", "screenshot", "capture"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "会话 ID（可选，只有一个会话时自动选择）"},
			"display":    map[string]string{"type": "integer", "description": "显示器编号（可选，0=主屏，1=第二屏/扩展屏，不传则截取所有屏幕拼图）"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolScreenshot(args) })

	// --- MCP tools ---
	reg("list_mcp_tools", "列出已注册的 MCP Server 及其工具",
		ToolCategoryBuiltin, []string{"mcp", "list", "tools"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolListMCPTools() })

	reg("call_mcp_tool", "调用指定 MCP Server 上的工具",
		ToolCategoryBuiltin, []string{"mcp", "call", "execute"},
		map[string]interface{}{
			"server_id": map[string]string{"type": "string", "description": "MCP Server ID"},
			"tool_name": map[string]string{"type": "string", "description": "工具名称"},
			"arguments": map[string]string{"type": "object", "description": "工具参数（JSON 对象）"},
		}, []string{"server_id", "tool_name"},
		func(args map[string]interface{}) string { return h.toolCallMCPTool(args) })

	// --- Skill tools ---
	reg("list_skills", "列出已注册的本地 Skill。如果本地没有 Skill，会同时展示 SkillHub 上的推荐 Skill 供安装。",
		ToolCategoryBuiltin, []string{"skill", "list"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolListSkills() })

	reg("search_skill_hub", "在已配置的 SkillHub（如 openclaw、tencent 等）上搜索可用的 Skill",
		ToolCategoryBuiltin, []string{"skill", "search", "hub"},
		map[string]interface{}{
			"query": map[string]string{"type": "string", "description": "搜索关键词（如 'git commit'、'代码审查'、'部署'）"},
		}, []string{"query"},
		func(args map[string]interface{}) string { return h.toolSearchSkillHub(args) })

	reg("install_skill_hub", "从 SkillHub 安装指定的 Skill 到本地。设置 auto_run=true 可安装后立即执行。",
		ToolCategoryBuiltin, []string{"skill", "install", "hub"},
		map[string]interface{}{
			"skill_id": map[string]string{"type": "string", "description": "Skill ID（从 search_skill_hub 结果中获取）"},
			"hub_url":  map[string]string{"type": "string", "description": "来源 Hub URL（从 search_skill_hub 结果中获取）"},
			"auto_run": map[string]string{"type": "boolean", "description": "安装成功后是否立即执行（默认 true）"},
		}, []string{"skill_id", "hub_url"},
		func(args map[string]interface{}) string { return h.toolInstallSkillHub(args) })

	reg("run_skill", "执行指定的 Skill",
		ToolCategoryBuiltin, []string{"skill", "run", "execute"},
		map[string]interface{}{
			"name": map[string]string{"type": "string", "description": "Skill 名称"},
		}, []string{"name"},
		func(args map[string]interface{}) string { return h.toolRunSkill(args) })

	// --- Orchestration tools ---
	reg("parallel_execute", "并行执行多个编程任务，每个任务在独立会话中运行（最多5个）",
		ToolCategoryBuiltin, []string{"orchestrate", "parallel", "multi"},
		map[string]interface{}{
			"tasks": map[string]interface{}{
				"type":        "array",
				"description": "任务列表，每个任务包含 tool（工具名）、description（任务描述）、project_path（项目路径）",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"tool":         map[string]string{"type": "string", "description": "工具名称"},
						"description":  map[string]string{"type": "string", "description": "任务描述"},
						"project_path": map[string]string{"type": "string", "description": "项目路径"},
					},
				},
			},
		}, []string{"tasks"},
		func(args map[string]interface{}) string { return h.toolParallelExecute(args) })

	reg("recommend_tool", "根据任务描述推荐最合适的编程工具",
		ToolCategoryBuiltin, []string{"recommend", "select", "tool"},
		map[string]interface{}{
			"task_description": map[string]string{"type": "string", "description": "任务描述"},
		}, []string{"task_description"},
		func(args map[string]interface{}) string { return h.toolRecommendTool(args) })

	reg("discover_tool", "Search for additional tools not in the current tool list. Use when you need a capability that none of the available tools provide.",
		ToolCategoryBuiltin, []string{"discover", "find", "search", "tool"},
		map[string]interface{}{
			"need": map[string]string{"type": "string", "description": "Describe the capability you need, e.g. 'query PostgreSQL database' or 'send Slack notification'"},
		}, []string{"need"},
		func(args map[string]interface{}) string { return h.toolDiscoverTool(args) })

	// --- Craft tool (needs progress callback) ---
	regP("craft_tool", "当现有工具都无法完成任务时，自动研究问题并生成脚本来解决。会用 LLM 生成代码、执行、并注册为可复用的 Skill。适用于数据处理、API 调用、文件转换、系统管理等需要编程才能完成的任务。",
		ToolCategoryBuiltin, []string{"craft", "script", "generate", "code"},
		map[string]interface{}{
			"task":          map[string]string{"type": "string", "description": "需要完成的任务描述（越详细越好）"},
			"language":      map[string]string{"type": "string", "description": "脚本语言: python/bash/powershell/node（可选，自动检测）"},
			"save_as_skill": map[string]string{"type": "boolean", "description": "执行成功后是否注册为 Skill 供下次复用（默认 true）"},
			"skill_name":    map[string]string{"type": "string", "description": "Skill 名称（可选，自动生成）"},
			"timeout":       map[string]string{"type": "integer", "description": "执行超时秒数（默认 60，最大 300）"},
		}, []string{"task"},
		func(args map[string]interface{}, onProgress ProgressCallback) string {
			return h.toolCraftTool(args, onProgress)
		})

	// --- Local machine tools ---
	regP("bash", "在本机直接执行 shell 命令（如创建目录、移动文件、运行脚本等）。命令在 MaClaw 所在设备上执行，不需要会话。",
		ToolCategoryBuiltin, []string{"shell", "bash", "command", "execute"},
		map[string]interface{}{
			"command":     map[string]string{"type": "string", "description": "要执行的 shell 命令"},
			"working_dir": map[string]string{"type": "string", "description": "工作目录（可选，默认为用户主目录）"},
			"timeout":     map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
		}, []string{"command"},
		func(args map[string]interface{}, onProgress ProgressCallback) string {
			return h.toolBash(args, onProgress)
		})

	reg("read_file", "读取本机文件内容",
		ToolCategoryBuiltin, []string{"file", "read"},
		map[string]interface{}{
			"path":  map[string]string{"type": "string", "description": "文件路径（绝对路径或相对于主目录的路径）"},
			"lines": map[string]string{"type": "integer", "description": "最多读取行数（可选，默认 200）"},
		}, []string{"path"},
		func(args map[string]interface{}) string { return h.toolReadFile(args) })

	reg("write_file", "写入内容到本机文件（会创建不存在的目录）",
		ToolCategoryBuiltin, []string{"file", "write"},
		map[string]interface{}{
			"path":    map[string]string{"type": "string", "description": "文件路径"},
			"content": map[string]string{"type": "string", "description": "文件内容"},
		}, []string{"path", "content"},
		func(args map[string]interface{}) string { return h.toolWriteFile(args) })

	reg("list_directory", "列出本机目录内容",
		ToolCategoryBuiltin, []string{"file", "directory", "list"},
		map[string]interface{}{
			"path": map[string]string{"type": "string", "description": "目录路径（可选，默认为用户主目录）"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolListDirectory(args) })

	reg("send_file", "读取本机文件并发送给用户（通过 IM 通道直接发送文件）",
		ToolCategoryBuiltin, []string{"file", "send", "share"},
		map[string]interface{}{
			"path":      map[string]string{"type": "string", "description": "文件的绝对路径或相对于主目录的路径"},
			"file_name": map[string]string{"type": "string", "description": "发送时显示的文件名（可选，默认使用原文件名）"},
		}, []string{"path"},
		func(args map[string]interface{}) string { return h.toolSendFile(args) })

	reg("open", "用操作系统默认程序打开文件或网址。例如：打开 PDF 用默认阅读器、打开 .xlsx 用 Excel、打开 URL 用默认浏览器、打开文件夹用资源管理器。也支持 mailto: 链接。",
		ToolCategoryBuiltin, []string{"open", "launch", "browse"},
		map[string]interface{}{
			"target": map[string]string{"type": "string", "description": "要打开的文件路径、目录路径或 URL"},
		}, []string{"target"},
		func(args map[string]interface{}) string { return h.toolOpen(args) })

	// --- Long-term memory (unified) ---
	reg("memory", "管理长期记忆（action: recall/save/list/delete）。recall 按需检索相关记忆，save 保存新记忆。",
		ToolCategoryBuiltin, []string{"memory", "save", "remember", "list", "search", "delete", "recall"},
		map[string]interface{}{
			"action":   map[string]string{"type": "string", "description": "操作: recall(按需召回)/save(保存)/list(列出或搜索)/delete(删除)"},
			"query":    map[string]string{"type": "string", "description": "检索关键词（recall 时必填，由你提炼的精准检索词，非用户原始消息）"},
			"content":  map[string]string{"type": "string", "description": "记忆内容（save 时必填）"},
			"category": map[string]string{"type": "string", "description": "类别: user_fact/preference/project_knowledge/instruction（save 时必填，recall/list 时可选过滤）"},
			"tags": map[string]interface{}{
				"type":        "array",
				"description": "关联标签（save 时可选）",
				"items":       map[string]string{"type": "string"},
			},
			"keyword": map[string]string{"type": "string", "description": "按关键词搜索（list 时可选）"},
			"id":      map[string]string{"type": "string", "description": "记忆条目 ID（delete 时必填）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolMemory(args) })

	// --- Session template tools ---
	reg("create_template", "创建会话模板（快捷启动配置）",
		ToolCategoryBuiltin, []string{"template", "create"},
		map[string]interface{}{
			"name":         map[string]string{"type": "string", "description": "模板名称"},
			"tool":         map[string]string{"type": "string", "description": "工具名称"},
			"project_path": map[string]string{"type": "string", "description": "项目路径"},
			"model_config": map[string]string{"type": "string", "description": "模型配置"},
			"yolo_mode":    map[string]string{"type": "boolean", "description": "是否开启 Yolo 模式"},
		}, []string{"name", "tool"},
		func(args map[string]interface{}) string { return h.toolCreateTemplate(args) })

	reg("list_templates", "列出所有会话模板",
		ToolCategoryBuiltin, []string{"template", "list"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolListTemplates() })

	reg("launch_template", "使用模板启动会话",
		ToolCategoryBuiltin, []string{"template", "launch"},
		map[string]interface{}{
			"template_name": map[string]string{"type": "string", "description": "模板名称"},
		}, []string{"template_name"},
		func(args map[string]interface{}) string { return h.toolLaunchTemplate(args) })

	// --- Config management tools ---
	reg("get_config", "获取指定配置区域的当前值",
		ToolCategoryBuiltin, []string{"config", "get", "settings"},
		map[string]interface{}{
			"section": map[string]string{"type": "string", "description": "配置区域名称（如 claude/gemini/remote/projects/maclaw_llm/proxy/general），为空或 all 返回概览"},
		}, []string{"section"},
		func(args map[string]interface{}) string { return h.toolGetConfig(args) })

	reg("update_config", "修改单个配置项",
		ToolCategoryBuiltin, []string{"config", "update", "settings"},
		map[string]interface{}{
			"section": map[string]string{"type": "string", "description": "配置区域名称"},
			"key":     map[string]string{"type": "string", "description": "配置项名称"},
			"value":   map[string]string{"type": "string", "description": "新值"},
		}, []string{"section", "key", "value"},
		func(args map[string]interface{}) string { return h.toolUpdateConfig(args) })

	reg("batch_update_config", "批量修改配置（原子性，任一项失败则全部回滚）",
		ToolCategoryBuiltin, []string{"config", "batch", "settings"},
		map[string]interface{}{
			"changes": map[string]string{"type": "string", "description": "JSON 数组，每项包含 section/key/value"},
		}, []string{"changes"},
		func(args map[string]interface{}) string { return h.toolBatchUpdateConfig(args) })

	reg("list_config_schema", "列出所有可配置项的 schema 信息",
		ToolCategoryBuiltin, []string{"config", "schema"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolListConfigSchema() })

	reg("export_config", "导出当前配置（敏感字段已脱敏）",
		ToolCategoryBuiltin, []string{"config", "export"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolExportConfig() })

	reg("import_config", "导入配置（JSON 格式，保留本机特有字段）",
		ToolCategoryBuiltin, []string{"config", "import"},
		map[string]interface{}{
			"json_data": map[string]string{"type": "string", "description": "要导入的配置 JSON 字符串"},
		}, []string{"json_data"},
		func(args map[string]interface{}) string { return h.toolImportConfig(args) })

	// --- LLM provider switch ---
	reg("switch_llm_provider", "换脑子：查看或切换 MaClaw 自身使用的 LLM 服务商。当用户说'换智谱'、'换minimax'、'用智谱想一下'、'换个模型'时切换；当用户问'现在用的什么模型'、'当前脑子是啥'、'你现在用的哪个服务商'时查询。不传 provider 返回当前服务商和可选列表；传入名称则立即切换。",
		ToolCategoryBuiltin, []string{"llm", "provider", "switch", "model", "brain"},
		map[string]interface{}{
			"provider": map[string]string{"type": "string", "description": "服务商名称，如 智谱、MiniMax、Custom1。支持模糊匹配，不区分大小写。不传则列出所有可用服务商。"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolSwitchLLMProvider(args) })

	reg("set_nickname", "设置本机在 Hub 群聊中的昵称。当用户给你起名字（如'你叫安妮'、'以后叫你小明'）时，调用此工具上报新昵称，这样在群聊中 /call 和 @昵称 就能用新名字找到你。",
		ToolCategoryBuiltin, []string{"nickname", "name", "identity", "alias"},
		map[string]interface{}{
			"nickname": map[string]string{"type": "string", "description": "新昵称（如 安妮、小明）"},
		}, []string{"nickname"},
		func(args map[string]interface{}) string { return h.toolSetNickname(args) })

	// --- Agent self-management ---
	reg("set_max_iterations", fmt.Sprintf("调整最大推理轮数。设置后会持久化保存，后续对话也会生效。当你判断任务复杂需要更多轮次时调用此工具扩展上限，任务简单时可缩减。范围 %d-%d。", minAgentIterations, maxAgentIterationsCap),
		ToolCategoryBuiltin, []string{"agent", "iterations", "limit"},
		map[string]interface{}{
			"max_iterations": map[string]string{"type": "integer", "description": fmt.Sprintf("新的最大轮数（%d-%d）", minAgentIterations, maxAgentIterationsCap)},
			"reason":         map[string]string{"type": "string", "description": "调整原因（用于日志记录）"},
		}, []string{"max_iterations"},
		func(args map[string]interface{}) string { return h.toolSetMaxIterations(args) })

	// --- Scheduled task tools ---
	reg("create_scheduled_task", "创建定时任务。用户说 每天9点做XX、每周一下午3点做YY、从3月1号到15号每天上午10点做ZZ 时，解析出时间参数并调用此工具。用户说 每隔N小时/N分钟做XX 时，使用 interval_minutes 参数（如每4小时=240）。day_of_week: -1=每天, 0=周日, 1=周一...6=周六。day_of_month: -1=不限, 1-31=每月几号。重要：如果用户说的是一次性任务（如'今天中午提醒我'、'明天下午3点做XX'），必须将 start_date 和 end_date 都设为目标日期，确保只执行一次。",
		ToolCategoryBuiltin, []string{"schedule", "task", "cron", "timer", "interval"},
		map[string]interface{}{
			"name":             map[string]string{"type": "string", "description": "任务名称（简短描述）"},
			"action":           map[string]string{"type": "string", "description": "到时要执行的操作（自然语言描述，会发送给 agent 执行）"},
			"hour":             map[string]string{"type": "integer", "description": "执行时间-小时（0-23），间隔模式下为首次执行时间"},
			"minute":           map[string]string{"type": "integer", "description": "执行时间-分钟（0-59，默认0），间隔模式下为首次执行时间"},
			"day_of_week":      map[string]string{"type": "integer", "description": "星期几（-1=每天, 0=周日, 1=周一...6=周六，默认-1）"},
			"day_of_month":     map[string]string{"type": "integer", "description": "每月几号（-1=不限, 1-31，默认-1）"},
			"interval_minutes": map[string]string{"type": "integer", "description": "重复间隔（分钟），>0 时启用间隔模式。如每4小时=240，每30分钟=30，每2天=2880"},
			"start_date":       map[string]string{"type": "string", "description": "生效开始日期（格式 2006-01-02，可选）"},
			"end_date":         map[string]string{"type": "string", "description": "生效结束日期（格式 2006-01-02，可选）"},
		}, []string{"name", "action", "hour"},
		func(args map[string]interface{}) string { return h.toolCreateScheduledTask(args) })

	reg("list_scheduled_tasks", "列出所有定时任务及其状态、下次执行时间",
		ToolCategoryBuiltin, []string{"schedule", "task", "list"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolListScheduledTasks() })

	reg("delete_scheduled_task", "删除定时任务（按 ID 或名称）",
		ToolCategoryBuiltin, []string{"schedule", "task", "delete"},
		map[string]interface{}{
			"id":   map[string]string{"type": "string", "description": "任务 ID（优先）"},
			"name": map[string]string{"type": "string", "description": "任务名称（ID 为空时按名称匹配）"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolDeleteScheduledTask(args) })

	reg("update_scheduled_task", "修改定时任务的时间或内容",
		ToolCategoryBuiltin, []string{"schedule", "task", "update"},
		map[string]interface{}{
			"id":           map[string]string{"type": "string", "description": "任务 ID（必填）"},
			"name":         map[string]string{"type": "string", "description": "新名称（可选）"},
			"action":       map[string]string{"type": "string", "description": "新的执行内容（可选）"},
			"hour":         map[string]string{"type": "integer", "description": "新的小时（可选）"},
			"minute":       map[string]string{"type": "integer", "description": "新的分钟（可选）"},
			"day_of_week":  map[string]string{"type": "integer", "description": "新的星期几（可选）"},
			"day_of_month": map[string]string{"type": "integer", "description": "新的每月几号（可选）"},
			"start_date":   map[string]string{"type": "string", "description": "新的开始日期（可选）"},
			"end_date":     map[string]string{"type": "string", "description": "新的结束日期（可选）"},
		}, []string{"id"},
		func(args map[string]interface{}) string { return h.toolUpdateScheduledTask(args) })

	// --- Audit log query tool (Phase 2 upgrade) ---
	reg("query_audit_log", "查询安全审计日志，可按时间范围、工具名、风险等级筛选",
		ToolCategoryBuiltin, []string{"audit", "security", "log", "query"},
		map[string]interface{}{
			"since":      map[string]string{"type": "string", "description": "开始时间（RFC3339 格式，如 2024-01-01T00:00:00Z）"},
			"until":      map[string]string{"type": "string", "description": "结束时间（RFC3339 格式）"},
			"tool_name":  map[string]string{"type": "string", "description": "按工具名筛选"},
			"risk_level": map[string]string{"type": "string", "description": "按风险等级筛选（low/medium/high/critical）"},
			"limit":      map[string]string{"type": "integer", "description": "最多返回条数（默认 20）"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolQueryAuditLog(args) })

	// --- Web search & fetch tools ---
	reg("web_search", "搜索互联网内容。返回搜索结果列表（标题、URL、摘要）。适用于查找资料、技术文档、最新信息等。",
		ToolCategoryBuiltin, []string{"web", "search", "internet", "google", "query", "network"},
		map[string]interface{}{
			"query":       map[string]string{"type": "string", "description": "搜索关键词"},
			"max_results": map[string]string{"type": "integer", "description": "最大结果数（默认 8，最大 20）"},
		}, []string{"query"},
		func(args map[string]interface{}) string { return h.toolWebSearch(args) })

	reg("web_fetch", "抓取指定 URL 的网页内容并提取正文文本。支持自动编码检测（GBK/UTF-8 等）、HTML 正文提取。可选 JS 渲染（需本机安装 Chrome）。也可用 save_path 下载文件到本地。",
		ToolCategoryBuiltin, []string{"web", "fetch", "download", "url", "browse", "network"},
		map[string]interface{}{
			"url":       map[string]string{"type": "string", "description": "要抓取的 URL"},
			"render_js": map[string]string{"type": "boolean", "description": "是否使用 Chrome 渲染 JS（可选，默认 false）"},
			"save_path": map[string]string{"type": "string", "description": "保存文件路径（可选，指定后下载文件而非返回文本）"},
			"timeout":   map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
		}, []string{"url"},
		func(args map[string]interface{}) string { return h.toolWebFetch(args) })

	// --- SSH remote server tools ---
	reg("ssh", "SSH 远程服务器管理（connect/exec/list/close）。连接后会自动注册为后台任务，可在任务后台面板监看。",
		ToolCategoryBuiltin, []string{"ssh", "remote", "server", "connect", "exec"},
		map[string]interface{}{
			"action":          map[string]string{"type": "string", "description": "操作: connect/exec/list/close"},
			"host":            map[string]string{"type": "string", "description": "远程主机地址（connect 时必填）"},
			"user":            map[string]string{"type": "string", "description": "登录用户名（connect 时必填）"},
			"port":            map[string]string{"type": "integer", "description": "SSH 端口（默认 22）"},
			"auth_method":     map[string]string{"type": "string", "description": "认证方式: key/password/agent（默认 key）"},
			"key_path":        map[string]string{"type": "string", "description": "私钥路径（可选）"},
			"password":        map[string]string{"type": "string", "description": "密码（可选）"},
			"label":           map[string]string{"type": "string", "description": "主机标签（可选，如 prod-web-01）"},
			"initial_command": map[string]string{"type": "string", "description": "连接后立即执行的命令（可选）"},
			"session_id":      map[string]string{"type": "string", "description": "SSH 会话 ID（exec/close 时必填）"},
			"command":         map[string]string{"type": "string", "description": "要执行的命令（exec 时必填）"},
			"wait_seconds":    map[string]string{"type": "integer", "description": "等待输出秒数（exec 时可选，默认 5）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolSSH(args) })
}
