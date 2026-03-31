package main

// Tool definitions: legacy hardcoded tool schema builder (buildToolDefinitions + toolDef helper).

import (
	"fmt"
)

func (h *IMMessageHandler) buildToolDefinitions() []map[string]interface{} {
	defs := []map[string]interface{}{
		toolDef("list_sessions", "列出当前所有远程会话及其状态", nil, nil),
		toolDef("create_session", "创建新的远程会话。可指定 provider 选择服务商。创建后建议用 get_session_output 观察启动状态。",
			map[string]interface{}{
				"tool":         map[string]string{"type": "string", "description": "工具名称，如 claude, codex, cursor, gemini, opencode"},
				"project_path": map[string]string{"type": "string", "description": "项目路径（可选）"},
				"project_id":   map[string]string{"type": "string", "description": "预设项目 ID（可选，与 project_path 二选一）"},
				"provider":            map[string]string{"type": "string", "description": "服务商名称（可选，如 Original, DeepSeek, 百度千帆）。不指定则使用桌面端当前选中的服务商"},
				"resume_session_id": map[string]string{"type": "string", "description": "续接会话 ID（可选）。自动续接时由 get_session_output 返回，传入后使用 --resume 模式恢复完整对话历史"},
			}, []string{"tool"}),
		toolDef("list_providers", "列出指定编程工具的所有可用服务商（已过滤未配置的空服务商）",
			map[string]interface{}{
				"tool": map[string]string{"type": "string", "description": "工具名称，如 claude, codex, gemini"},
			}, []string{"tool"}),
		toolDef("project_manage", "项目管理（创建/列出/删除/切换项目）",
			map[string]interface{}{
				"action": map[string]string{"type": "string", "description": "操作: create/list/delete/switch"},
				"name":   map[string]string{"type": "string", "description": "项目名称（create 必填）"},
				"path":   map[string]string{"type": "string", "description": "项目路径（create 必填）"},
				"target": map[string]string{"type": "string", "description": "项目名称或 ID（delete/switch 必填）"},
			}, []string{"action"}),
		toolDef("send_input", "向指定会话发送文本输入。发送后可用 get_session_output 观察结果。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
				"text":       map[string]string{"type": "string", "description": "要发送的文本"},
			}, []string{"session_id", "text"}),
		toolDef("get_session_output", "获取指定会话的最近输出内容和状态摘要。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
				"lines":      map[string]string{"type": "integer", "description": "返回最近 N 行输出（默认 30，最大 100）"},
			}, []string{"session_id"}),
		toolDef("get_session_events", "获取指定会话的重要事件列表（文件修改、命令执行、错误等）",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("interrupt_session", "中断指定会话（发送 Ctrl+C 信号）",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("kill_session", "终止指定会话",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("screenshot", "截取屏幕截图并发送给用户。仅在以下情况使用：(1) 用户明确要求截屏；(2) 用户通过 IM 远程监督，需要确认操作结果。不要在用户未要求时主动截屏。最小间隔 30 秒。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID（可选，只有一个会话时自动选择）"},
			}, nil),
		toolDef("list_mcp_tools", "列出已注册的 MCP Server 及其工具", nil, nil),
		toolDef("call_mcp_tool", "调用指定 MCP Server 上的工具",
			map[string]interface{}{
				"server_id": map[string]string{"type": "string", "description": "MCP Server ID"},
				"tool_name": map[string]string{"type": "string", "description": "工具名称"},
				"arguments": map[string]string{"type": "object", "description": "工具参数（JSON 对象）"},
			}, []string{"server_id", "tool_name"}),
		toolDef("list_skills", "列出已注册的本地 Skill。如果本地没有 Skill，会同时展示 SkillHub 上的推荐 Skill 供安装。", nil, nil),
		toolDef("search_skill_hub", "在已配置的 SkillHub（如 openclaw、tencent 等）上搜索可用的 Skill",
			map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "搜索关键词（如 'git commit'、'代码审查'、'部署'）"},
			}, []string{"query"}),
		toolDef("install_skill_hub", "从 SkillHub 安装指定的 Skill 到本地。设置 auto_run=true 可安装后立即执行。",
			map[string]interface{}{
				"skill_id": map[string]string{"type": "string", "description": "Skill ID（从 search_skill_hub 结果中获取）"},
				"hub_url":  map[string]string{"type": "string", "description": "来源 Hub URL（从 search_skill_hub 结果中获取）"},
				"auto_run": map[string]string{"type": "boolean", "description": "安装成功后是否立即执行（默认 true）"},
			}, []string{"skill_id", "hub_url"}),
		toolDef("run_skill", "执行指定的 Skill",
			map[string]interface{}{
				"name": map[string]string{"type": "string", "description": "Skill 名称"},
			}, []string{"name"}),
		toolDef("parallel_execute", "并行执行多个编程任务，每个任务在独立会话中运行（最多5个）",
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
			}, []string{"tasks"}),
		toolDef("recommend_tool", "根据任务描述推荐最合适的编程工具",
			map[string]interface{}{
				"task_description": map[string]string{"type": "string", "description": "任务描述"},
			}, []string{"task_description"}),
		toolDef("craft_tool", "当现有工具都无法完成任务时，自动研究问题并生成脚本来解决。会用 LLM 生成代码、执行、并注册为可复用的 Skill。适用于数据处理、API 调用、文件转换、系统管理等需要编程才能完成的任务。",
			map[string]interface{}{
				"task":          map[string]string{"type": "string", "description": "需要完成的任务描述（越详细越好）"},
				"language":      map[string]string{"type": "string", "description": "脚本语言: python/bash/powershell/node（可选，自动检测）"},
				"save_as_skill": map[string]string{"type": "boolean", "description": "执行成功后是否注册为 Skill 供下次复用（默认 true）"},
				"skill_name":    map[string]string{"type": "string", "description": "Skill 名称（可选，自动生成）"},
				"timeout":       map[string]string{"type": "integer", "description": "执行超时秒数（默认 60，最大 300）"},
			}, []string{"task"}),
		// --- 本机直接操作工具 ---
		toolDef("bash", "在本机直接执行 shell 命令（如创建目录、移动文件、运行脚本等）。命令在 MaClaw 所在设备上执行，不需要会话。",
			map[string]interface{}{
				"command":     map[string]string{"type": "string", "description": "要执行的 shell 命令"},
				"working_dir": map[string]string{"type": "string", "description": "工作目录（可选，默认为用户主目录）"},
				"timeout":     map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
			}, []string{"command"}),
		toolDef("read_file", "读取本机文件内容",
			map[string]interface{}{
				"path":  map[string]string{"type": "string", "description": "文件路径（绝对路径或相对于主目录的路径）"},
				"lines": map[string]string{"type": "integer", "description": "最多读取行数（可选，默认 200）"},
			}, []string{"path"}),
		toolDef("write_file", "写入内容到本机文件（会创建不存在的目录）",
			map[string]interface{}{
				"path":    map[string]string{"type": "string", "description": "文件路径"},
				"content": map[string]string{"type": "string", "description": "文件内容"},
			}, []string{"path", "content"}),
		toolDef("list_directory", "列出本机目录内容",
			map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "目录路径（可选，默认为用户主目录）"},
			}, nil),
		toolDef("send_file", "读取本机文件并发送给用户（通过 IM 通道直接发送文件）。设置 forward_to_im=true 可将文件同时转发到用户的飞书/微信/QQ等 IM 平台。",
			map[string]interface{}{
				"path":          map[string]string{"type": "string", "description": "文件的绝对路径或相对于主目录的路径"},
				"file_name":     map[string]string{"type": "string", "description": "发送时显示的文件名（可选，默认使用原文件名）"},
				"forward_to_im": map[string]string{"type": "boolean", "description": "是否同时转发到用户的 IM 平台（飞书/微信/QQ等）。仅在用户明确要求发送到飞书、微信、QQ等 IM 时设为 true，默认 false"},
			}, []string{"path"}),
		toolDef("open", "用操作系统默认程序打开文件或网址。例如：打开 PDF 用默认阅读器、打开 .xlsx 用 Excel、打开 URL 用默认浏览器、打开文件夹用资源管理器。也支持 mailto: 链接。",
			map[string]interface{}{
				"target": map[string]string{"type": "string", "description": "要打开的文件路径、目录路径或 URL（如 C:\\Users\\test\\doc.pdf、https://example.com、mailto:test@example.com）"},
			}, []string{"target"}),
		// --- 长期记忆工具（合并） ---
		toolDef("memory", "管理长期记忆（action: recall/save/list/delete）。recall 按需检索相关记忆，save 保存新记忆。",
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
			}, []string{"action"}),
		// --- 会话模板工具 ---
		toolDef("create_template", "创建会话模板（快捷启动配置）",
			map[string]interface{}{
				"name":         map[string]string{"type": "string", "description": "模板名称"},
				"tool":         map[string]string{"type": "string", "description": "工具名称"},
				"project_path": map[string]string{"type": "string", "description": "项目路径"},
				"model_config": map[string]string{"type": "string", "description": "模型配置"},
				"yolo_mode":    map[string]string{"type": "boolean", "description": "是否开启 Yolo 模式"},
			}, []string{"name", "tool"}),
		toolDef("list_templates", "列出所有会话模板", nil, nil),
		toolDef("launch_template", "使用模板启动会话",
			map[string]interface{}{
				"template_name": map[string]string{"type": "string", "description": "模板名称"},
			}, []string{"template_name"}),
		// --- 配置管理工具 ---
		toolDef("get_config", "获取指定配置区域的当前值",
			map[string]interface{}{
				"section": map[string]string{"type": "string", "description": "配置区域名称（如 claude/gemini/remote/projects/maclaw_llm/proxy/general），为空或 all 返回概览"},
			}, []string{"section"}),
		toolDef("update_config", "修改单个配置项",
			map[string]interface{}{
				"section": map[string]string{"type": "string", "description": "配置区域名称"},
				"key":     map[string]string{"type": "string", "description": "配置项名称"},
				"value":   map[string]string{"type": "string", "description": "新值"},
			}, []string{"section", "key", "value"}),
		toolDef("batch_update_config", "批量修改配置（原子性，任一项失败则全部回滚）",
			map[string]interface{}{
				"changes": map[string]string{"type": "string", "description": "JSON 数组，每项包含 section/key/value，例如 [{\"section\":\"general\",\"key\":\"language\",\"value\":\"en\"}]"},
			}, []string{"changes"}),
		toolDef("list_config_schema", "列出所有可配置项的 schema 信息", nil, nil),
		toolDef("export_config", "导出当前配置（敏感字段已脱敏）", nil, nil),
		toolDef("import_config", "导入配置（JSON 格式，保留本机特有字段）",
			map[string]interface{}{
				"json_data": map[string]string{"type": "string", "description": "要导入的配置 JSON 字符串"},
			}, []string{"json_data"}),
		// --- Agent 自管理工具 ---
		toolDef("set_max_iterations", fmt.Sprintf("调整最大推理轮数。设置后会持久化保存，后续对话也会生效。当你判断任务复杂需要更多轮次时调用此工具扩展上限，任务简单时可缩减。范围 %d-%d。", minAgentIterations, maxAgentIterationsCap),
			map[string]interface{}{
				"max_iterations": map[string]string{"type": "integer", "description": fmt.Sprintf("新的最大轮数（%d-%d）", minAgentIterations, maxAgentIterationsCap)},
				"reason":         map[string]string{"type": "string", "description": "调整原因（用于日志记录）"},
			}, []string{"max_iterations"}),
		// --- 定时任务工具 ---
		toolDef("create_scheduled_task", "创建定时任务。用户说 每天9点做XX、每周一下午3点做YY、从3月1号到15号每天上午10点做ZZ 时，解析出时间参数并调用此工具。用户说 每隔N小时/N分钟做XX 时，使用 interval_minutes 参数（如每4小时=240）。day_of_week: -1=每天, 0=周日, 1=周一...6=周六。day_of_month: -1=不限, 1-31=每月几号。重要：如果用户说的是一次性任务（如'今天中午提醒我'、'明天下午3点做XX'），必须将 start_date 和 end_date 都设为目标日期，确保只执行一次。",
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
			}, []string{"name", "action", "hour"}),
		toolDef("list_scheduled_tasks", "列出所有定时任务及其状态、下次执行时间", nil, nil),
		toolDef("delete_scheduled_task", "删除定时任务（按 ID 或名称）",
			map[string]interface{}{
				"id":   map[string]string{"type": "string", "description": "任务 ID（优先）"},
				"name": map[string]string{"type": "string", "description": "任务名称（ID 为空时按名称匹配）"},
			}, nil),
		toolDef("update_scheduled_task", "修改定时任务的时间或内容",
			map[string]interface{}{
				"id":               map[string]string{"type": "string", "description": "任务 ID（必填）"},
				"name":             map[string]string{"type": "string", "description": "新名称（可选）"},
				"action":           map[string]string{"type": "string", "description": "新的执行内容（可选）"},
				"hour":             map[string]string{"type": "integer", "description": "新的小时（可选）"},
				"minute":           map[string]string{"type": "integer", "description": "新的分钟（可选）"},
				"day_of_week":      map[string]string{"type": "integer", "description": "新的星期几（可选）"},
				"day_of_month":     map[string]string{"type": "integer", "description": "新的每月几号（可选）"},
				"interval_minutes": map[string]string{"type": "integer", "description": "新的重复间隔分钟数（可选，0=关闭间隔模式）"},
				"start_date":       map[string]string{"type": "string", "description": "新的开始日期（可选）"},
				"end_date":         map[string]string{"type": "string", "description": "新的结束日期（可选）"},
			}, []string{"id"}),
	}

	// ---------- ClawNet tools (dynamic — only when daemon is running) ----------
	if h.app != nil && h.app.clawNetClient != nil && h.app.clawNetClient.IsRunning() {
		defs = append(defs,
			toolDef("clawnet_search", "在虾网（ClawNet P2P 知识网络）中搜索知识条目。返回匹配的知识列表，包含标题、内容、作者等。",
				map[string]interface{}{
					"query": map[string]string{"type": "string", "description": "搜索关键词"},
				}, []string{"query"}),
			toolDef("clawnet_publish", "向虾网（ClawNet P2P 知识网络）发布一条知识条目。发布后其他节点可以搜索到。",
				map[string]interface{}{
					"title": map[string]string{"type": "string", "description": "知识标题"},
					"body":  map[string]string{"type": "string", "description": "知识内容（Markdown 格式）"},
				}, []string{"title", "body"}),
		)
	}

	// ---------- Web search & fetch tools ----------
	defs = append(defs,
		toolDef("web_search", "搜索互联网内容。返回搜索结果列表（标题、URL、摘要）。适用于查找资料、技术文档、最新信息等。",
			map[string]interface{}{
				"query":       map[string]string{"type": "string", "description": "搜索关键词"},
				"max_results": map[string]string{"type": "integer", "description": "最大结果数（默认 8，最大 20）"},
			}, []string{"query"}),
		toolDef("web_fetch", "抓取指定 URL 的网页内容并提取正文文本。支持 HTTP/HTTPS/FTP 协议，自动编码检测（GBK/UTF-8 等）、HTML 正文提取。可选 JS 渲染（需本机安装 Chrome）。也可用 save_path 下载文件到本地。",
			map[string]interface{}{
				"url":       map[string]string{"type": "string", "description": "要抓取的 URL（支持 http/https/ftp 协议）"},
				"render_js": map[string]string{"type": "boolean", "description": "是否使用 Chrome 渲染 JS（可选，默认 false。适用于 SPA 等 JS 渲染页面）"},
				"save_path": map[string]string{"type": "string", "description": "保存文件路径（可选。指定后将原始内容保存到文件而非返回文本，适用于下载文件）"},
				"timeout":   map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
			}, []string{"url"}),
	)

	return defs
}

func toolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	params := map[string]interface{}{"type": "object"}
	if props != nil {
		params["properties"] = props
	} else {
		params["properties"] = map[string]interface{}{}
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}

// ---------------------------------------------------------------------------
// Tool Execution
// ---------------------------------------------------------------------------
