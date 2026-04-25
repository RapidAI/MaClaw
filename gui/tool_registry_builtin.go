package main

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

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

	reg("create_session", "创建远程编程会话。仅用于明确的代码修改/编程任务。服务器运维、SSH 登录、日志排查请使用 ssh；如果用户需求模糊，建议先澄清再创建。创建后编程工具会等待输入，需用 send_and_observe 发送编程指令。",
		ToolCategoryBuiltin, []string{"session", "create", "launch"},
		map[string]interface{}{
			"tool":              map[string]string{"type": "string", "description": "工具名称，如 claude, codex, cursor, gemini, opencode"},
			"project_path":      map[string]string{"type": "string", "description": "项目路径（可选）"},
			"project_id":        map[string]string{"type": "string", "description": "预设项目 ID（可选，与 project_path 二选一）"},
			"provider":          map[string]string{"type": "string", "description": "服务商名称（可选，如 Original, DeepSeek, 百度千帆）。不指定则使用桌面端当前选中的服务商"},
			"resume_session_id": map[string]string{"type": "string", "description": "续接会话 ID（可选）。用于恢复之前的结构化编程会话；Claude 会优先映射到 --resume 以续接完整对话历史"},
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

	reg("screenshot", "截取屏幕截图并发送给用户。这是截屏的唯一正确方式，禁止用 bash 编写 PowerShell/Python/scrot 等截屏脚本替代此工具。使用场景：(1) 用户明确要求截屏；(2) 用户通过 IM 远程监督，需要确认操作结果。不要在用户未要求时主动截屏。最小间隔 30 秒。支持 display 参数指定显示器（0=主屏，1=第二屏，不传=所有屏幕拼图）。",
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
		func(args map[string]interface{}) string { return h.toolListMCPTools(args) })

	reg("call_mcp_tool", "调用 MCP Server 上的外部工具。仅用于 MCP 扩展工具（通过 list_mcp_tools 查看），不要用于内置工具（ssh、bash、write_file 等内置工具直接以函数名调用）。server_id 支持 ID 或 Name，重名时请传 ID。",
		ToolCategoryBuiltin, []string{"mcp", "call", "execute"},
		map[string]interface{}{
			"server_id": map[string]string{"type": "string", "description": "MCP Server ID 或 Name"},
			"tool_name": map[string]string{"type": "string", "description": "工具名称"},
			"arguments": map[string]string{"type": "object", "description": "工具参数（JSON 对象）"},
		}, []string{"server_id", "tool_name"},
		func(args map[string]interface{}) string { return h.toolCallMCPTool(args) })

	// --- Merged skill management tool (progress-aware for run action) ---
	regP("manage_skill", skill.ManageSkillDescription(),
		ToolCategoryBuiltin, append([]string{"skill"}, skill.ManageSkillActionNames()...),
		map[string]interface{}{
			"action":       map[string]string{"type": "string", "description": "操作: " + skill.ManageSkillActionSlash()},
			"query":        map[string]string{"type": "string", "description": "搜索关键词（search 时必填，如 'git commit'、'代码审查'、'部署'）"},
			"skill_id":     map[string]string{"type": "string", "description": "Skill ID（install 时必填，从 search 结果中获取）"},
			"hub_url":      map[string]string{"type": "string", "description": "来源 Hub URL（install 时必填，从 search 结果中获取）"},
			"auto_run":     map[string]string{"type": "boolean", "description": "安装成功后是否立即执行（install 时可选，默认 true）"},
			"name":         map[string]string{"type": "string", "description": "Skill 名称（run/upload 时必填）"},
			"args":         map[string]string{"type": "object", "description": "Skill 运行参数（run 时按需传入）。Skill 命令中的 {{key}} 占位符会被替换为 args 中对应的值。例如 Skill 命令含 {{city}} 则传 args={\"city\":\"北京\"}，含 {{input}} 则传 args={\"input\":\"文件路径\"}。如果首次调用因缺少参数而失败，错误信息会提示需要哪些 key。"},
			"env":          map[string]string{"type": "object", "description": "注入到 skill 子进程的环境变量（run 时可选），例如 {\"LIBTV_ACCESS_KEY\": \"xxx\"}"},
			"operation":    map[string]string{"type": "string", "description": "执行指定的 operation（run 时可选，api_workflow 模式 Skill 的操作名称，如 generate/query）"},
			"input":        map[string]string{"type": "string", "description": "兼容旧调用的输入参数（run 时可选）"},
			"output":       map[string]string{"type": "string", "description": "兼容旧调用的输出参数（run 时可选）"},
			"user_prompt":  map[string]string{"type": "string", "description": "用户的原始请求文本（run 时可选，供 craft_tool 类型 Skill 生成脚本时使用）"},
			"wait_seconds": map[string]string{"type": "number", "description": "等待状态快照的秒数（install/run/status 时可选，默认 2，最大 30）"},
			"run_id":       map[string]string{"type": "string", "description": "运行 ID（status 时必填，从 run 返回值中获取）"},
		}, []string{"action"},
		func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolManageSkill(args, onProgress)
		})

	// Legacy backward-compat aliases (handler only, no definition generation)
	reg("list_skills", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}) string { return h.toolListSkills() })
	reg("search_skill_hub", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}) string { return h.toolSearchSkillHub(args) })
	reg("install_skill_hub", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}) string { return h.toolInstallSkillHub(args) })
	regP("run_skill", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolRunSkill(args, onProgress)
		})
	reg("get_skill_run", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}) string { return h.toolGetSkillRun(args) })

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

	reg("discover_tool", "发现更多可用工具。当你需要以下能力但找不到对应工具时调用：配置管理、定时任务、会话模板、AgentNet 知识网络、MCP 扩展工具、Skill 市场搜索安装、审计日志查询。传入你需要的能力描述，返回匹配的工具定义。",
		ToolCategoryBuiltin, []string{"discover", "find", "search", "tool", "config", "schedule", "template", "agentnet", "mcp", "audit"},
		map[string]interface{}{
			"need": map[string]string{"type": "string", "description": "描述你需要的能力（如'修改配置'、'定时执行'、'搜索知识网络'、'查询审计日志'）"},
		}, []string{"need"},
		func(args map[string]interface{}) string { return h.toolDiscoverTool(args) })

	// --- Structured question tool ---
	reg("ask_user", "向用户提出结构化问题并等待回答。适用于需要用户从多个选项中选择、或提供缺失信息的场景。编码工作流的阶段确认不要使用此工具。",
		ToolCategoryBuiltin, []string{"ask", "question", "confirm", "clarify", "input"},
		map[string]interface{}{
			"question":   map[string]string{"type": "string", "description": "要问用户的问题"},
			"options":    map[string]interface{}{"type": "array", "description": "可选：预设选项列表", "items": map[string]string{"type": "string"}},
			"context":    map[string]string{"type": "string", "description": "可选：问题的背景说明"},
			"input_type": map[string]string{"type": "string", "description": "期望的回答类型: choice/text/confirm（默认 text）"},
		}, []string{"question"},
		func(args map[string]interface{}) string { return h.toolAskUser(args) })

	// --- Task management tool ---
	reg("task", "管理任务（action: create/update/complete/fail/list/delegate/delete）。用于跟踪复杂任务的进度、依赖关系和子任务分配。",
		ToolCategoryBuiltin, []string{"task", "todo", "plan", "track", "progress"},
		map[string]interface{}{
			"action":      map[string]string{"type": "string", "description": "操作: create/update/complete/fail/list/delegate/delete"},
			"task_id":     map[string]string{"type": "string", "description": "任务 ID"},
			"title":       map[string]string{"type": "string", "description": "任务标题（create 时必填）"},
			"description": map[string]string{"type": "string", "description": "任务描述"},
			"depends_on":  map[string]interface{}{"type": "array", "description": "依赖的任务 ID 列表", "items": map[string]string{"type": "string"}},
			"status":      map[string]string{"type": "string", "description": "新状态: pending/in_progress/completed/failed/blocked"},
			"status_note": map[string]string{"type": "string", "description": "状态更新说明"},
			"delegate_to": map[string]string{"type": "string", "description": "委派目标"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolTask(args) })

	// --- Sub-agent delegation tool ---
	reg("delegate_task", "将任务委派给专业子 Agent 处理。可用: coding_workflow（编码工作流）、help（使用帮助）。",
		ToolCategoryBuiltin, []string{"delegate", "subagent", "workflow", "help"},
		map[string]interface{}{
			"agent":   map[string]string{"type": "string", "description": "子 Agent 名称: coding_workflow / help"},
			"request": map[string]string{"type": "string", "description": "要委派的任务描述"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolDelegateTask(args) })

	// --- Craft tool (needs progress callback) ---
	regP("craft_tool", "当现有工具、Skill 或会话式编程都不合适时，生成并执行单脚本来完成一次性自动化任务。更适合本机数据处理、API 调用、文件转换和小型系统自动化；不适合复杂代码库改造或长链路编程任务。",
		ToolCategoryBuiltin, []string{"craft", "script", "generate", "code"},
		map[string]interface{}{
			"task":               map[string]string{"type": "string", "description": "需要完成的任务描述（越详细越好）"},
			"language":           map[string]string{"type": "string", "description": "脚本语言: python/bash/powershell/node（可选，优先按运行时自动选择）"},
			"working_dir":        map[string]string{"type": "string", "description": "脚本执行工作目录（可选）"},
			"expected_artifacts": map[string]interface{}{"type": "array", "description": "期望生成的文件路径列表（可选，用于验收）", "items": map[string]string{"type": "string"}},
			"verification_mode":  map[string]string{"type": "string", "description": "验收模式（可选，如 artifact_required）"},
			"register_policy":    map[string]string{"type": "string", "description": "注册策略（可选，auto/manual）"},
			"max_attempts":       map[string]string{"type": "integer", "description": "最大自动修复尝试次数（默认 2，最大 3）"},
			"save_as_skill":      map[string]string{"type": "boolean", "description": "执行成功后是否尝试注册为 Skill（默认 true，但一次性任务会更保守）"},
			"skill_name":         map[string]string{"type": "string", "description": "Skill 名称（可选，自动生成）"},
			"timeout":            map[string]string{"type": "integer", "description": "执行超时秒数（默认 60，最大 300）"},
		}, []string{"task"},
		func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolCraftTool(args, onProgress)
		})

	// --- Local machine tools ---
	regP("bash", "在本机直接执行 shell 命令（如创建目录、移动文件、运行脚本等）。命令在 MaClaw 所在设备上执行，不需要会话。",
		ToolCategoryBuiltin, []string{"shell", "bash", "command", "execute"},
		map[string]interface{}{
			"command":     map[string]string{"type": "string", "description": "要执行的 shell 命令"},
			"working_dir": map[string]string{"type": "string", "description": "工作目录（可选，默认为 ~/.maclaw/workspace）"},
			"timeout":     map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
		}, []string{"command"},
		func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolBash(args, onProgress)
		})

	reg("read_file", "读取本机文件内容（小文件自动全量返回；大文件自动返回结构摘要+预览，可用 start_line 精准读取特定段落）",
		ToolCategoryBuiltin, []string{"file", "read"},
		map[string]interface{}{
			"path":       map[string]string{"type": "string", "description": "文件路径（绝对路径或相对于主目录的路径）"},
			"lines":      map[string]string{"type": "integer", "description": "最多读取行数（可选，默认 200。指定后跳过自适应策略，按精确行数返回）"},
			"start_line": map[string]string{"type": "integer", "description": "起始行号（从 1 开始，可选。指定后跳过自适应策略，从该行开始精确读取）"},
		}, []string{"path"},
		func(args map[string]interface{}) string { return h.toolReadFile(args) })

	reg("write_file", "写入内容到本机文件（UTF-8 编码，支持覆盖或追加，允许空内容，会创建不存在的目录。大文件请分块写入：先 overwrite 第一部分，再 append 后续部分）",
		ToolCategoryBuiltin, []string{"file", "write"},
		map[string]interface{}{
			"path":    map[string]string{"type": "string", "description": "文件路径"},
			"content": map[string]string{"type": "string", "description": "文件内容，可为空字符串"},
			"mode":    map[string]string{"type": "string", "description": "写入模式：overwrite（默认）或 append"},
		}, []string{"path", "content"},
		func(args map[string]interface{}) string { return h.toolWriteFile(args) })

	reg("edit_file", "修改已有文件的首选工具（搜索替换模式，token 开销极小）。old_string 必须精确匹配文件原文（含缩进），建议包含修改点前后 1-2 行确保唯一匹配。修改已有文件时优先用此工具，不要用 write_file 重写整个文件。",
		ToolCategoryBuiltin, []string{"file", "edit", "replace"},
		map[string]interface{}{
			"path":        map[string]string{"type": "string", "description": "文件路径"},
			"old_string":  map[string]string{"type": "string", "description": "要查找的原始文本"},
			"new_string":  map[string]string{"type": "string", "description": "替换后的文本，可为空字符串"},
			"replace_all": map[string]string{"type": "boolean", "description": "是否替换全部匹配，默认 false"},
		}, []string{"path", "old_string", "new_string"},
		func(args map[string]interface{}) string { return h.toolEditFile(args) })

	reg("edit_lines", "按行号精确编辑文件（替换/插入/删除指定行）。比 edit_file 更精确——用行号定位，不怕重复内容。先用 read_file 查看行号，再用此工具编辑。",
		ToolCategoryBuiltin, []string{"file", "edit", "line", "patch"},
		map[string]interface{}{
			"path":       map[string]string{"type": "string", "description": "文件路径"},
			"operation":  map[string]string{"type": "string", "description": "操作类型: replace（替换行）、insert（插入行）、delete（删除行）"},
			"start_line": map[string]string{"type": "integer", "description": "起始行号（1-indexed）。insert 时 0 表示插入到文件开头"},
			"end_line":   map[string]string{"type": "integer", "description": "结束行号（含，replace/delete 时必填）"},
			"content":    map[string]string{"type": "string", "description": "新内容（replace/insert 时必填，delete 时忽略）"},
		}, []string{"path", "operation", "start_line"},
		func(args map[string]interface{}) string { return h.toolEditLines(args) })

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

	// --- 后台任务管理工具 ---
	regP("async_wait", "管理本机后台任务（与 bash(background=true) 配合使用）",
		ToolCategoryBuiltin, []string{"wait", "async", "background", "task", "check"},
		map[string]interface{}{
			"action":     map[string]string{"type": "string", "description": "操作: check/wait/kill/list"},
			"task_id":    map[string]string{"type": "string", "description": "任务 ID"},
			"timeout":    map[string]string{"type": "integer", "description": "等待超时秒数（仅 wait）"},
			"tail_lines": map[string]string{"type": "integer", "description": "日志尾部行数"},
		}, nil,
		func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolAsyncWait(args, onProgress)
		})

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

	// --- Merged tool: manage_template (create/list/launch) ---
	reg("manage_template", "会话模板管理（action: create/list/launch）。create 创建模板，list 列出所有模板，launch 使用模板启动会话。",
		ToolCategoryBuiltin, []string{"template", "create", "list", "launch"},
		map[string]interface{}{
			"action":       map[string]string{"type": "string", "description": "操作: create/list/launch"},
			"name":         map[string]string{"type": "string", "description": "模板名称（create/launch 时必填）"},
			"tool":         map[string]string{"type": "string", "description": "工具名称（create 时必填）"},
			"project_path": map[string]string{"type": "string", "description": "项目路径（create 时可选）"},
			"model_config": map[string]string{"type": "string", "description": "模型配置（create 时可选）"},
			"yolo_mode":    map[string]string{"type": "boolean", "description": "是否开启 Yolo 模式（create 时可选）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolManageTemplate(args) })

	// --- Merged tool: manage_config (get/set/batch/schema/export/import) ---
	reg("manage_config", "配置管理（action: get/set/batch/schema/export/import）。get 获取配置，set 修改单项，batch 批量修改，schema 列出可配置项，export 导出，import 导入。",
		ToolCategoryBuiltin, []string{"config", "get", "set", "settings", "batch", "schema", "export", "import"},
		map[string]interface{}{
			"action":    map[string]string{"type": "string", "description": "操作: get/set/batch/schema/export/import"},
			"section":   map[string]string{"type": "string", "description": "配置区域（get/set 时使用）"},
			"key":       map[string]string{"type": "string", "description": "配置项名称（set 时必填）"},
			"value":     map[string]string{"type": "string", "description": "新值（set 时必填）"},
			"changes":   map[string]string{"type": "string", "description": "JSON 数组（batch 时必填），每项含 section/key/value"},
			"json_data": map[string]string{"type": "string", "description": "配置 JSON 字符串（import 时必填）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolManageConfig(args) })

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
	reg("set_max_iterations", fmt.Sprintf("调整最大推理轮数。设置后会持久化保存，后续对话也会生效。当你判断任务复杂需要更多轮次时调用此工具扩展上限，任务简单时可缩减。范围 %d-%d。", config.MinAgentIterations, config.MaxAgentIterationsCap),
		ToolCategoryBuiltin, []string{"agent", "iterations", "limit"},
		map[string]interface{}{
			"max_iterations": map[string]string{"type": "integer", "description": fmt.Sprintf("新的最大轮数（%d-%d）", config.MinAgentIterations, config.MaxAgentIterationsCap)},
			"reason":         map[string]string{"type": "string", "description": "调整原因（用于日志记录）"},
		}, []string{"max_iterations"},
		func(args map[string]interface{}) string { return h.toolSetMaxIterations(args) })

	// --- Merged tool: manage_schedule (create/list/delete/update) ---
	reg("manage_schedule", "定时任务管理（action: create/list/delete/update）。day_of_week: -1=每天, 0=周日, 1=周一...6=周六。day_of_month: -1=不限, 1-31。一次性任务请将 start_date 和 end_date 都设为目标日期。",
		ToolCategoryBuiltin, []string{"schedule", "task", "cron", "timer", "interval", "create", "list", "delete", "update"},
		map[string]interface{}{
			"action":           map[string]string{"type": "string", "description": "操作: create/list/delete/update"},
			"id":               map[string]string{"type": "string", "description": "任务 ID（delete/update 时必填）"},
			"name":             map[string]string{"type": "string", "description": "任务名称（create 时必填，update/delete 时可选）"},
			"task_action":      map[string]string{"type": "string", "description": "到时要执行的操作（自然语言描述，create/update 时使用）"},
			"hour":             map[string]string{"type": "integer", "description": "执行时间-小时（0-23）"},
			"minute":           map[string]string{"type": "integer", "description": "执行时间-分钟（0-59，默认0）"},
			"day_of_week":      map[string]string{"type": "integer", "description": "星期几（-1=每天, 0=周日...6=周六，默认-1）"},
			"day_of_month":     map[string]string{"type": "integer", "description": "每月几号（-1=不限, 1-31，默认-1）"},
			"interval_minutes": map[string]string{"type": "integer", "description": "重复间隔分钟数（>0 启用间隔模式）"},
			"start_date":       map[string]string{"type": "string", "description": "生效开始日期（格式 2006-01-02）"},
			"end_date":         map[string]string{"type": "string", "description": "生效结束日期（格式 2006-01-02）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolManageSchedule(args) })

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

	// --- Session search tool (cross-session FTS5 full-text search) ---
	reg("session_search", "搜索历史对话记录。在所有已保存的会话中进行全文搜索，返回匹配的会话片段、时间戳、主题和平台信息。",
		ToolCategoryBuiltin, []string{"session", "search", "history", "recall", "conversation"},
		map[string]interface{}{
			"query":       map[string]string{"type": "string", "description": "搜索关键词"},
			"max_results": map[string]string{"type": "integer", "description": "最大结果数（默认 10）"},
		}, []string{"query"},
		func(args map[string]interface{}) string { return h.toolSessionSearch(args) })

	// --- User model management tool (dialectic user modeling) ---
	reg("manage_user_model", "管理用户画像。查看、修正或重置用户偏好维度。",
		ToolCategoryBuiltin, []string{"user", "profile", "preference", "model"},
		map[string]interface{}{
			"action":    map[string]string{"type": "string", "description": "操作类型: view（查看画像）、correct（修正维度）、reset（重置维度）"},
			"dimension": map[string]string{"type": "string", "description": "维度名称（correct/reset 时必填）: communication_style, technical_level, preferred_languages, domain_expertise, work_patterns, tool_preferences"},
			"value":     map[string]string{"type": "string", "description": "新值（correct 时必填）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolManageUserModel(args) })

	// --- Web search & fetch tools ---
	reg("web_search", "搜索互联网内容。返回搜索结果列表（标题、URL、摘要）。适用于查找资料、技术文档、最新信息等。",
		ToolCategoryBuiltin, []string{"web", "search", "internet", "google", "query", "network"},
		map[string]interface{}{
			"query":       map[string]string{"type": "string", "description": "搜索关键词"},
			"max_results": map[string]string{"type": "integer", "description": "最大结果数（默认 8，最大 20）"},
		}, []string{"query"},
		func(args map[string]interface{}) string { return h.toolWebSearch(args) })

	reg("web_fetch", "抓取指定 URL 的网页内容并提取正文文本。支持自动编码检测（GBK/UTF-8 等）、HTML 正文提取。可选 JS 渲染（需本机安装 Chrome）。也可用 save_path 下载文件到本地。长页面支持续读：当返回 has_more=true 时，请使用 offset=next_offset 继续读取后续内容。",
		ToolCategoryBuiltin, []string{"web", "fetch", "download", "url", "browse", "network"},
		map[string]interface{}{
			"url":       map[string]string{"type": "string", "description": "要抓取的 URL"},
			"render_js": map[string]string{"type": "boolean", "description": "是否使用 Chrome 渲染 JS（可选，默认 false）"},
			"save_path": map[string]string{"type": "string", "description": "保存文件路径（可选，指定后下载文件而非返回文本）"},
			"timeout":   map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
			"offset":    map[string]string{"type": "integer", "description": "从第几个字符开始读取（用于长页面续读，默认 0）"},
			"max_chars": map[string]string{"type": "integer", "description": "本次最多返回字符数（可选；不传表示返回全部提取内容）"},
		}, []string{"url"},
		func(args map[string]interface{}) string { return h.toolWebFetch(args) })

	// --- Unified office document tool ---
	reg("office", "Office 文档操作工具。action 参数：generate_pdf（生成PDF文档）、read_excel（读取XLSX/CSV表格）、write_excel（写入XLSX表格）、read_pptx（读取PPT演示文稿）。Office document tool: generate PDF, read/write Excel (XLSX/CSV), read PowerPoint (PPTX).",
		ToolCategoryBuiltin, []string{"office", "pdf", "excel", "xlsx", "csv", "pptx", "document", "spreadsheet", "presentation"},
		map[string]interface{}{
			"action":    map[string]string{"type": "string", "description": "操作类型: generate_pdf/read_excel/write_excel/read_pptx"},
			"content":   map[string]string{"type": "string", "description": "Markdown 格式的文档内容（generate_pdf 时必填）"},
			"title":     map[string]string{"type": "string", "description": "文档标题（generate_pdf 时可选）"},
			"doc_type":  map[string]string{"type": "string", "description": "文档类型: requirements/design/task_plan（generate_pdf 时可选）"},
			"file_path": map[string]string{"type": "string", "description": "文件路径（read_excel/write_excel/read_pptx 时必填）"},
			"sheet":     map[string]string{"type": "string", "description": "工作表名称（read_excel 时可选，默认第一个工作表）"},
			"range":     map[string]string{"type": "string", "description": "A1 表示法的单元格范围，如 A1:D10（read_excel 时可选）"},
			"data":      map[string]string{"type": "object", "description": "写入数据（write_excel 时必填），格式: {\"sheets\": [{\"name\": \"Sheet1\", \"rows\": [[...]]}]}"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolOffice(args) })

	// --- PDF generation tool (coding workflow only) - backward-compatible alias ---
	reg("generate_pdf", "生成 PDF 文档并发送给用户。仅用于编程流程的需求文档、技术设计文档、任务拆分文档。严禁用于资料收集、翻译、内容整理等非编程流程任务的 Markdown 转 PDF。参数 doc_type 可选: requirements（需求文档）、design（设计文档）、task_plan（任务计划）。",
		ToolCategoryBuiltin, []string{"pdf", "document", "generate"},
		map[string]interface{}{
			"content":  map[string]string{"type": "string", "description": "Markdown 格式的文档内容"},
			"title":    map[string]string{"type": "string", "description": "项目名称或文档标题"},
			"doc_type": map[string]string{"type": "string", "description": "文档类型: requirements（需求文档）、design（设计文档）、task_plan（任务计划）。不传则为通用文档"},
		}, []string{"content"},
		func(args map[string]interface{}) string {
			args["action"] = "generate_pdf"
			return h.toolOffice(args)
		})

	// --- SSH remote server tools ---
	reg("ssh", "SSH 远程服务器管理（connect/exec/exec_background/check_task/list_tasks/kill_task/upload/download/list/close）。长命令自动转后台模式，支持 SFTP 文件传输。",
		ToolCategoryBuiltin, []string{"ssh", "remote", "server", "connect", "exec", "background", "upload", "download", "sftp"},
		map[string]interface{}{
			"action":          map[string]string{"type": "string", "description": "操作: connect/exec/exec_background/check_task/list_tasks/kill_task/upload/download/list/close"},
			"host":            map[string]string{"type": "string", "description": "远程主机地址（connect 时必填）"},
			"user":            map[string]string{"type": "string", "description": "登录用户名（connect 时必填）"},
			"port":            map[string]string{"type": "integer", "description": "SSH 端口（默认 22）"},
			"auth_method":     map[string]string{"type": "string", "description": "认证方式: password/key/agent。当用户提供了密码时必须设为 password"},
			"key_path":        map[string]string{"type": "string", "description": "私钥路径（auth_method=key 时可选）"},
			"password":        map[string]string{"type": "string", "description": "SSH 登录密码。当用户提供了密码时必须传此参数，不要省略"},
			"label":           map[string]string{"type": "string", "description": "主机标签（可选，如 prod-web-01）"},
			"initial_command": map[string]string{"type": "string", "description": "连接后立即执行的命令（可选）"},
			"session_id":      map[string]string{"type": "string", "description": "SSH 会话 ID（exec/exec_background/upload/download/close 时必填）"},
			"command":         map[string]string{"type": "string", "description": "要执行的命令（exec/exec_background 时必填）"},
			"wait_seconds":    map[string]string{"type": "integer", "description": "等待输出秒数（exec 时可选，默认 5，最大 600）"},
			"task_id":         map[string]string{"type": "string", "description": "后台任务 ID（check_task/kill_task 时必填）"},
			"tail_lines":      map[string]string{"type": "integer", "description": "查看日志尾部行数（check_task 时可选，默认 50）"},
			"local_path":      map[string]string{"type": "string", "description": "本地文件/目录路径（upload/download 时必填）"},
			"remote_path":     map[string]string{"type": "string", "description": "远程文件/目录路径（upload/download 时必填）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolSSH(args) })

	// --- Backward compatibility aliases for merged tools ---
	// These allow old tool names to still work if the LLM uses them.
	alias := func(oldName string, handler ToolHandler) {
		registry.Register(RegisteredTool{
			Name:    oldName,
			Status:  RegToolAvailable,
			Source:  "builtin-alias",
			Handler: handler,
		})
	}
	// Config aliases
	alias("get_config", func(args map[string]interface{}) string { return h.toolGetConfig(args) })
	alias("update_config", func(args map[string]interface{}) string { return h.toolUpdateConfig(args) })
	alias("batch_update_config", func(args map[string]interface{}) string { return h.toolBatchUpdateConfig(args) })
	alias("list_config_schema", func(args map[string]interface{}) string { return h.toolListConfigSchema() })
	alias("export_config", func(args map[string]interface{}) string { return h.toolExportConfig() })
	alias("import_config", func(args map[string]interface{}) string { return h.toolImportConfig(args) })
	// Template aliases
	alias("create_template", func(args map[string]interface{}) string { return h.toolCreateTemplate(args) })
	alias("list_templates", func(args map[string]interface{}) string { return h.toolListTemplates() })
	alias("launch_template", func(args map[string]interface{}) string { return h.toolLaunchTemplate(args) })
	// Schedule aliases
	alias("create_scheduled_task", func(args map[string]interface{}) string { return h.toolCreateScheduledTask(args) })
	alias("list_scheduled_tasks", func(args map[string]interface{}) string { return h.toolListScheduledTasks() })
	alias("delete_scheduled_task", func(args map[string]interface{}) string { return h.toolDeleteScheduledTask(args) })
	alias("update_scheduled_task", func(args map[string]interface{}) string { return h.toolUpdateScheduledTask(args) })
}
