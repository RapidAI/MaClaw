package main

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/config"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
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
		if isDisabledExternalCodingSessionTool(name) {
			return
		}
		registry.Register(RegisteredTool{
			Name:              name,
			Description:       desc,
			Category:          cat,
			Tags:              tags,
			Priority:          0,
			Status:            RegToolAvailable,
			InputSchema:       props(schema),
			Required:          required,
			Source:            "builtin",
			ExecutionContract: defaultExplicitExecutionContractMetadata(name),
			Handler:           handler,
		})
	}

	regP := func(name, desc string, cat ToolCategory, tags []string, schema map[string]interface{}, required []string, handler ToolHandlerWithProgress) {
		if isDisabledExternalCodingSessionTool(name) {
			return
		}
		registry.Register(RegisteredTool{
			Name:              name,
			Description:       desc,
			Category:          cat,
			Tags:              tags,
			Priority:          0,
			Status:            RegToolAvailable,
			InputSchema:       props(schema),
			Required:          required,
			Source:            "builtin",
			ExecutionContract: defaultExplicitExecutionContractMetadata(name),
			HandlerProg:       handler,
		})
	}

	regCtxP := func(name, desc string, cat ToolCategory, tags []string, schema map[string]interface{}, required []string, handler ToolHandlerWithContext) {
		if isDisabledExternalCodingSessionTool(name) {
			return
		}
		registry.Register(RegisteredTool{
			Name:              name,
			Description:       desc,
			Category:          cat,
			Tags:              tags,
			Priority:          0,
			Status:            RegToolAvailable,
			InputSchema:       props(schema),
			Required:          required,
			Source:            "builtin",
			ExecutionContract: defaultExplicitExecutionContractMetadata(name),
			HandlerCtx:        handler,
		})
	}

	registerCurrentDateTimeTool(registry, ToolCategoryBuiltin, "builtin")

	// --- Session management tools ---
	reg("list_sessions", "列出当前所有远程会话及其状态",
		ToolCategoryBuiltin, []string{"session", "list"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolListSessions() })

	reg("project_manage", "项目管理（创建/列出/删除/切换项目）",
		ToolCategoryBuiltin, []string{"project", "list", "create", "delete", "switch"},
		map[string]interface{}{
			"action": map[string]string{"type": "string", "description": "操作: create/list/delete/switch"},
			"name":   map[string]string{"type": "string", "description": "项目名称（create 必填）"},
			"path":   map[string]string{"type": "string", "description": "项目路径（create 必填）"},
			"target": map[string]string{"type": "string", "description": "项目名称或 ID（delete/switch 必填）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolProjectManage(args) })

	reg("list_providers", "List configured providers for a coding tool.",
		ToolCategoryBuiltin, []string{"provider", "list", "model"},
		map[string]interface{}{
			"tool": map[string]string{"type": "string", "description": "工具名称，如 claude, codex, opencode"},
		}, []string{"tool"},
		func(args map[string]interface{}) string { return h.toolListProviders(args) })

	reg("send_input", "Send text input to a remote coding session.",
		ToolCategoryBuiltin, []string{"session", "input", "send"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "Session ID."},
			"text":       map[string]string{"type": "string", "description": "要发送的文本"},
		}, []string{"session_id", "text"},
		func(args map[string]interface{}) string { return h.toolSendInput(args) })

	reg("get_session_output", "Get recent output and status for a remote coding session.",
		ToolCategoryBuiltin, []string{"session", "output", "status"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "Session ID."},
			"lines":      map[string]string{"type": "integer", "description": "Number of recent output lines to return."},
		}, []string{"session_id"},
		func(args map[string]interface{}) string { return h.toolGetSessionOutput(args) })

	reg("get_session_events", "Get important events for a remote coding session.",
		ToolCategoryBuiltin, []string{"session", "events"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "Session ID."},
		}, []string{"session_id"},
		func(args map[string]interface{}) string { return h.toolGetSessionEvents(args) })

	reg("interrupt_session", "Interrupt a remote coding session with Ctrl+C.",
		ToolCategoryBuiltin, []string{"session", "interrupt", "cancel"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "Session ID."},
		}, []string{"session_id"},
		func(args map[string]interface{}) string { return h.toolInterruptSession(args) })

	reg("kill_session", "终止指定会话",
		ToolCategoryBuiltin, []string{"session", "kill", "stop"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "Session ID."},
		}, []string{"session_id"},
		func(args map[string]interface{}) string { return h.toolKillSession(args) })

	reg("screenshot", "Capture a screenshot and send it to the user.",
		ToolCategoryBuiltin, []string{"session", "screenshot", "capture"},
		map[string]interface{}{
			"session_id": map[string]string{"type": "string", "description": "Session ID."},
			"display":    map[string]string{"type": "integer", "description": "显示器编号（可选，0=主屏，1=第二屏/扩展屏，不传则截取所有屏幕拼图）"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolScreenshot(args) })

	// --- MCP tools ---
	reg("list_mcp_tools", "列出已注册的 MCP Server 及其工具",
		ToolCategoryBuiltin, []string{"mcp", "list", "tools"},
		nil, nil,
		func(args map[string]interface{}) string { return h.toolListMCPTools(args) })

	reg("import_mcp_servers", "通过 JSON 配置导入 MCP Server。支持标准 {\"mcpServers\": {\"name\": {...}}}，也兼容 mcpServers/mcp_servers/mcpservers 缺外层大括号的片段；本地 stdio 使用 command/args/env，远程 HTTP 使用 url 或 endpoint_url/headers。",
		ToolCategoryBuiltin, []string{"mcp", "import", "json", "server"},
		map[string]interface{}{
			"json_config": map[string]string{"type": "string", "description": "MCP JSON 配置文本，例如 {\"mcpServers\":{\"playwright\":{\"command\":\"npx\",\"args\":[\"-y\",\"@playwright/mcp\"]}}}；也可传 mcpServers: {...} 这类缺外层大括号片段。"},
			"target":      map[string]string{"type": "string", "description": "导入目标: auto/local/remote。默认 auto，含 url/endpoint_url 且无 command 时作为远程 MCP，否则作为本地 stdio MCP。"},
		}, []string{"json_config"},
		func(args map[string]interface{}) string { return h.toolImportMCPServers(args) })

	reg("call_mcp_tool", "Call an external tool on a registered MCP server.",
		ToolCategoryBuiltin, []string{"mcp", "call", "execute"},
		map[string]interface{}{
			"server_id": map[string]string{"type": "string", "description": "MCP Server ID 或 Name"},
			"tool_name": map[string]string{"type": "string", "description": "工具名称"},
			"arguments": map[string]string{"type": "object", "description": "Tool arguments as a JSON object."},
		}, []string{"server_id", "tool_name"},
		func(args map[string]interface{}) string { return h.toolCallMCPTool(args) })

	reg("passthrough_task", "管理直通任务注册表。用于帮用户创建、修改、删除、查看可通过 /run 从 AI 助手或 IM 通道直接执行的应急脚本任务；此工具只管理注册表，不执行脚本。保存成功后会返回 /run 运行示例和可发到 IM 的 /runctl save 注册命令。",
		ToolCategoryBuiltin, []string{"passthrough", "run", "emergency", "recovery", "script"},
		map[string]interface{}{
			"action":           map[string]string{"type": "string", "description": "操作: list/status/show/export/preview/save/delete/set_enabled/audit。export 只返回可发到 IM 的 /runctl save 注册命令；preview 只预览最终 argv，不执行脚本；save 会返回 /run 与 /runctl save 示例。"},
			"name":             map[string]string{"type": "string", "description": "任务名，示例 repair-env。允许字母、数字、点、下划线、短横线。"},
			"title":            map[string]string{"type": "string", "description": "显示名称，可选。"},
			"description":      map[string]string{"type": "string", "description": "任务说明，可选。"},
			"command":          map[string]string{"type": "string", "description": "完整命令模板，save 时可替代 script_path + template_args；第一段为脚本/可执行程序，其余段可写固定参数和 ${param} 占位符。"},
			"script_path":      map[string]string{"type": "string", "description": "脚本或可执行文件路径；command 为空时必填。"},
			"template_args":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "命令模板参数，不经过 shell。可写固定参数和 ${param} 占位符；留空时按 --param value 传入所有形参。"},
			"runtime":          map[string]string{"type": "string", "description": "运行时: auto/powershell/pwsh/cmd/bash/python/node/direct。默认 auto。"},
			"cwd":              map[string]string{"type": "string", "description": "工作目录，可选；留空时使用脚本所在目录。"},
			"timeout_seconds":  map[string]string{"type": "integer", "description": "超时秒数，默认 120。"},
			"confirm_required": map[string]string{"type": "boolean", "description": "是否要求远程执行时带 --confirm，默认 true。"},
			"enabled":          map[string]string{"type": "boolean", "description": "是否启用任务，默认 true；set_enabled 时用于启用/禁用。"},
			"params": map[string]interface{}{
				"type":        "array",
				"description": "参数定义数组。脚本会收到 argv 形式的 --name value。",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":     map[string]string{"type": "string", "description": "参数名。"},
						"type":     map[string]string{"type": "string", "description": "text/number/boolean/path。"},
						"required": map[string]string{"type": "boolean", "description": "是否必填。"},
						"default":  map[string]string{"type": "string", "description": "默认值。"},
						"example":  map[string]string{"type": "string", "description": "用于生成 /run 示例的值。"},
					},
				},
			},
			"params_text": map[string]string{"type": "string", "description": "参数定义的简写格式，每行 name:type:required:default:example；可代替 params。"},
			"params_json": map[string]string{"type": "string", "description": "参数定义 JSON 数组字符串，可代替 params。适合生成可发到 IM 的 /runctl save --params-json 内容；Windows 路径需按 JSON 转义为 D:\\\\workprj\\\\aicoder。"},
			"values":      map[string]string{"type": "object", "description": "preview 时使用的参数值，例如 {\"target\":\"D:\\\\workprj\\\\aicoder\"}。未传时使用形参 example/default 或安全样例。"},
			"limit":       map[string]string{"type": "integer", "description": "audit 返回条数，默认 10，最多 50。"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolPassthroughTask(args) })

	// --- Merged skill management tool (progress-aware for run action) ---
	regCtxP("manage_skill", skill.ManageSkillDescription(),
		ToolCategoryBuiltin, append([]string{"skill"}, skill.ManageSkillActionNames()...),
		map[string]interface{}{
			"action":                    map[string]string{"type": "string", "description": "操作: " + skill.ManageSkillActionSlash()},
			"query":                     map[string]string{"type": "string", "description": "Search query for skill discovery."},
			"skill_id":                  map[string]string{"type": "string", "description": "Skill ID（install 时必填，从 search 结果中获取）"},
			"hub_url":                   map[string]string{"type": "string", "description": "来源 Hub URL（install 时必填，从 search 结果中获取）"},
			"auto_run":                  map[string]string{"type": "boolean", "description": "安装成功后是否立即执行（install 时可选，默认 true）"},
			"name":                      map[string]string{"type": "string", "description": "Skill 名称（run/upload 时必填）"},
			"args":                      map[string]string{"type": "object", "description": "Skill 运行参数（run 时按需传入）。Skill 命令中的 {{key}} 占位符会被替换为 args 中对应的值。例如 Skill 命令含 {{city}} 则传 args={\"city\":\"北京\"}，含 {{input}} 则传 args={\"input\":\"文件路径\"}。如果首次调用因缺少参数而失败，错误信息会提示需要哪些 key。"},
			"env":                       map[string]string{"type": "object", "description": "注入到 skill 子进程的环境变量（run 时可选），例如 {\"LIBTV_ACCESS_KEY\": \"xxx\"}"},
			"operation":                 map[string]string{"type": "string", "description": "Optional operation name for api_workflow skills."},
			"input":                     map[string]string{"type": "string", "description": "兼容旧调用的输入参数（run 时可选）"},
			"output":                    map[string]string{"type": "string", "description": "兼容旧调用的输出参数（run 时可选）"},
			"user_prompt":               map[string]string{"type": "string", "description": "用户的原始请求文本（run 或 install+auto_run 时可选，供 craft_tool 类型 Skill 生成脚本时使用）"},
			"wait_seconds":              map[string]string{"type": "number", "description": "Seconds to wait for a status snapshot."},
			"run_id":                    map[string]string{"type": "string", "description": "Run ID returned by a previous run action."},
			"max_actions":               map[string]string{"type": "integer", "description": "maintenance_plan max action count."},
			"stale_after_days":          map[string]string{"type": "integer", "description": "maintenance_plan stale learned/crafted skill threshold in days."},
			"min_failure_runs":          map[string]string{"type": "integer", "description": "maintenance_plan minimum failed runs before review/repair."},
			"duplicate_similarity":      map[string]string{"type": "number", "description": "maintenance_plan duplicate skill similarity threshold."},
			"dry_run":                   map[string]string{"type": "boolean", "description": "execute_maintenance_plan preview mode; defaults true."},
			"confirm":                   map[string]string{"type": "boolean", "description": "Required true when execute_maintenance_plan uses dry_run=false."},
			"approved_actions":          map[string]string{"type": "array", "description": "Approved maintenance action names for execute_maintenance_plan."},
			"approved_draft_ids":        map[string]string{"type": "array", "description": "Reviewed skill governance draft ids to execute. Real runs require confirm=true and either approved_draft_ids or approved_actions."},
			"approved_review_trace_ids": map[string]string{"type": "array", "description": "Completed skill_draft_review trace ids whose stored draft_id should be executed through approved_draft_ids."},
			"allow_duplicate_retire":    map[string]string{"type": "boolean", "description": "Allow execute_maintenance_plan to disable the recommended duplicate skill after merge draft review."},
		}, []string{"action"},
		func(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolManageSkill(ctx, args, onProgress)
		})

	// Legacy backward-compat aliases (handler only, no definition generation)
	reg("list_skills", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}) string { return h.toolListSkills() })
	reg("search_skill_hub", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}) string { return h.toolSearchSkillHub(args) })
	reg("install_skill_hub", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}) string { return h.toolInstallSkillHub(args) })
	regCtxP("run_skill", "", ToolCategoryBuiltin, nil, nil, nil,
		func(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolRunSkill(ctx, args, onProgress)
		})
	reg("get_skill_run", "", ToolCategoryBuiltin, nil, nil, nil,
		func(args map[string]interface{}) string { return h.toolGetSkillRun(args) })

	// --- Orchestration tools ---
	reg("parallel_execute", "按 SubAgent 并发数分批执行多个编程任务（最多5个任务，并发上限4），每个任务在独立会话中运行",
		ToolCategoryBuiltin, []string{"orchestrate", "queue", "multi"},
		map[string]interface{}{
			"tasks": map[string]interface{}{
				"type":        "array",
				"description": "任务列表；按数组顺序和 SubAgent 并发数执行，每个任务包含 tool（工具名）、description（任务描述）、project_path（项目路径）",
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

	reg("discover_tool", "发现更多可用工具。当你需要以下能力但找不到对应工具时调用：配置管理、定时任务、会话模板、MCP 扩展工具、Skill 市场搜索安装、审计日志查询。传入你需要的能力描述，返回匹配的工具定义。",
		ToolCategoryBuiltin, []string{"discover", "find", "search", "tool", "config", "schedule", "template", "mcp", "audit"},
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

	// --- Long-form interactive recording (meeting / discussion capture) ---
	reg("record_audio", "【仅桌面】打开交互式长时录音界面（波形+暂停/停止）并等待用户结束录音。IM 通道不支持会议/长时录音（平台语音过短）。用户当前消息已明确要求开始录音/会议录音时立即调用，不要二次确认、不要搜已有音频文件。仅当意图含糊时再澄清。用户停止后，下一条消息会带上音频路径与时长等摘要，再继续转写/纪要或投递音频文件。若生成会议纪要，必须同时 send_file 投递原始音频（可点击路径，便于用户备份）。",
		ToolCategoryBuiltin, []string{"record", "audio", "meeting", "minutes", "voice", "录音", "会议"},
		map[string]interface{}{
			"title":   map[string]string{"type": "string", "description": "录音会话短标题（如会议名称）"},
			"purpose": map[string]string{"type": "string", "description": "可选：录音用途说明"},
			"hint":    map[string]string{"type": "string", "description": "可选：给用户的额外提示"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolRecordAudio(args) })

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

	// --- Goal management tool (persistent long-running objectives) ---
	reg("goal", "管理持久化长时间运行目标（action: create/complete/fail/get）。创建目标后系统自动持续推进直到达成或预算耗尽。只在用户明确要求时创建目标，不要从普通任务中推断。",
		ToolCategoryBuiltin, []string{"goal", "objective", "autonomous", "long-running"},
		map[string]interface{}{
			"action":              map[string]string{"type": "string", "description": "操作: create/complete/fail/get"},
			"objective":           map[string]string{"type": "string", "description": "目标描述（create 时必填）"},
			"token_budget":        map[string]string{"type": "integer", "description": "Token 预算上限（可选，0=无限制）"},
			"max_turns":           map[string]string{"type": "integer", "description": "最大迭代轮次（可选，默认50）"},
			"acceptance_criteria": map[string]interface{}{"type": "array", "description": "可验证的完成条件列表（可选）", "items": map[string]string{"type": "string"}},
			"project_path":        map[string]string{"type": "string", "description": "项目工作目录（可选）"},
			"summary":             map[string]string{"type": "string", "description": "完成总结（complete 时使用）"},
			"reason":              map[string]string{"type": "string", "description": "失败原因（fail 时使用）"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolGoal(args) })

	// --- Sub-agent delegation tool ---
	reg("delegate_task", "将任务委派给专业子 Agent 处理。coding_workflow 会同步运行内部 CodingSubAgent 完成编码任务，不返回占位激活文本；help 用于使用帮助。",
		ToolCategoryBuiltin, []string{"delegate", "subagent", "workflow", "help"},
		map[string]interface{}{
			"agent":        map[string]string{"type": "string", "description": "子 Agent 名称: coding_workflow / help"},
			"request":      map[string]string{"type": "string", "description": "要委派的任务描述"},
			"project_path": map[string]string{"type": "string", "description": "target project path for coding_workflow (optional)"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolDelegateTask(args) })

	// --- Craft tool (needs progress callback) ---
	regCtxP("craft_tool", "当现有工具、Skill 或会话式编程都不合适时，生成并执行单脚本来完成一次性自动化任务。更适合本机数据处理、API 调用、文件转换和小型系统自动化；不适合复杂代码库改造或长链路编程任务。",
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
		func(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolCraftToolWithContext(ctx, args, onProgress)
		})

	// --- Local machine tools ---
	regCtxP("bash", "在本机直接执行 shell 命令（如创建目录、移动文件、运行脚本等）。命令在 MaClaw 所在设备上执行，不需要会话。",
		ToolCategoryBuiltin, []string{"shell", "bash", "command", "execute"},
		map[string]interface{}{
			"command":     map[string]interface{}{"type": "string", "description": "要执行的 shell 命令", "maxLength": maxAgentLoopInlineBashCommandRunes},
			"working_dir": map[string]string{"type": "string", "description": "工作目录（可选，默认为 ~/.maclaw/workspace）"},
			"timeout":     map[string]string{"type": "integer", "description": "超时秒数（可选，默认 600，范围 240-600）"},
		}, []string{"command"},
		func(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolBash(ctx, args, onProgress)
		})

	reg("read_file", "读取本机文件内容（小文件自动全量返回；大文件自动返回结构摘要+预览，可用 start_line 精准读取特定段落）",
		ToolCategoryBuiltin, []string{"file", "read"},
		map[string]interface{}{
			"path":       map[string]string{"type": "string", "description": "文件路径（绝对路径或相对于主目录的路径）"},
			"lines":      map[string]string{"type": "integer", "description": "最多读取行数（可选，默认 200。指定后跳过自适应策略，按精确行数返回）"},
			"start_line": map[string]string{"type": "integer", "description": "起始行号（从 1 开始，可选。指定后跳过自适应策略，从该行开始精确读取）"},
		}, []string{"path"},
		func(args map[string]interface{}) string { return h.toolReadFile(args) })

	reg("read_tool_result", "分段回读被截断的工具完整输出（[tool_result_handle] 的 id 或 path）。大日志/网页/bash 输出被投影为预览后，用此工具按 offset/limit 读取细节。",
		ToolCategoryBuiltin, []string{"file", "read", "tool_result", "handle"},
		map[string]interface{}{
			"id":          map[string]string{"type": "string", "description": "handle id（来自 [tool_result_handle] 页脚，优先）"},
			"path":        map[string]string{"type": "string", "description": "handle 页脚中的绝对路径（必须位于 tool_results 目录）"},
			"session_key": map[string]string{"type": "string", "description": "可选 session/user key（spill 时使用的会话隔离键）"},
			"offset":      map[string]string{"type": "integer", "description": "从完整结果的字节偏移（默认 0）"},
			"limit":       map[string]string{"type": "integer", "description": "返回最大字节数（默认 6000，最大 32768）"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolReadToolResult(args) })

	reg("write_file", "写入内容到本机文件（UTF-8 编码，支持覆盖或追加，允许空内容，会创建不存在的目录。内容无长度限制，超长内容系统会自动处理。超过约6000字符时建议分块写入：先 overwrite 第一部分，再 append 后续部分）",
		ToolCategoryBuiltin, []string{"file", "write"},
		map[string]interface{}{
			"path":     map[string]string{"type": "string", "description": "文件路径"},
			"content":  map[string]interface{}{"type": "string", "description": "文件内容，可为空字符串。无长度限制，可一次写入完整脚本或文档"},
			"mode":     map[string]string{"type": "string", "description": "写入模式：overwrite（默认）或 append"},
			"phase_id": map[string]string{"type": "string", "description": workflowDocPhaseIDSchemaDescription()},
			"doc_type": map[string]string{"type": "string", "description": workflowDocTypeSchemaDescription()},
		}, []string{"path", "content"},
		func(args map[string]interface{}) string { return h.toolWriteFile(args) })

	reg("edit_file", "修改已有文件的首选工具（搜索替换模式，token 开销极小）。old_string 必须精确匹配文件原文（含缩进），建议包含修改点前后 1-2 行确保唯一匹配。修改已有文件时优先用此工具，不要用 write_file 重写整个文件。",
		ToolCategoryBuiltin, []string{"file", "edit", "replace"},
		map[string]interface{}{
			"path":        map[string]string{"type": "string", "description": "文件路径"},
			"old_string":  map[string]interface{}{"type": "string", "description": "要查找的原始文本"},
			"new_string":  map[string]interface{}{"type": "string", "description": "替换后的文本，可为空字符串"},
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
			"content":    map[string]interface{}{"type": "string", "description": "新内容（replace/insert 时必填，delete 时忽略）"},
		}, []string{"path", "operation", "start_line"},
		func(args map[string]interface{}) string { return h.toolEditLines(args) })

	reg("list_directory", "列出本机目录内容",
		ToolCategoryBuiltin, []string{"file", "directory", "list"},
		map[string]interface{}{
			"path": map[string]string{"type": "string", "description": "目录路径（可选；省略或相对路径时基于当前 Project directory / 工作目录，不要默认用户主目录）"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolListDirectory(args) })

	reg("send_file", "读取本机文件并交付：桌面端默认展示在当前对话；已在微信/飞书通道中则发回本对话即等于发到该通道",
		ToolCategoryBuiltin, []string{"file", "send", "share"},
		map[string]interface{}{
			"path":          map[string]string{"type": "string", "description": "文件的绝对路径或相对于主目录的路径"},
			"file_name":     map[string]string{"type": "string", "description": "发送时显示的文件名（可选，默认使用原文件名）。工作流交付文档请使用稳定 ASCII 文件名，本地化文本放在文档标题或消息正文中。"},
			"phase_id":      map[string]string{"type": "string", "description": workflowDocDeliveryPhaseIDSchemaDescription()},
			"doc_type":      map[string]string{"type": "string", "description": workflowDocDeliveryTypeSchemaDescription()},
			"destination":   map[string]string{"type": "string", "description": "chat/desktop 或 im/wechat/feishu/qq/dingtalk 等；已在 IM 通道中可不设"},
			"forward_to_im": map[string]string{"type": "boolean", "description": "桌面端是否转发到 IM；已在微信/飞书通道中无需设置"},
		}, []string{"path"},
		func(args map[string]interface{}) string { return h.toolSendFile(args) })

	reg("send_to_im", "桌面端把文件发到绑定 IM；已在微信/飞书通道中时用 send_file 发回本对话即可，勿报发送器未配置",
		ToolCategoryBuiltin, []string{"file", "send", "share", "wechat", "im", "feishu"},
		map[string]interface{}{
			"path":        map[string]string{"type": "string", "description": "文件的绝对路径或相对于主目录的路径"},
			"file_name":   map[string]string{"type": "string", "description": "发送时显示的文件名（可选）"},
			"destination": map[string]string{"type": "string", "description": "可选：wechat/feishu/qq/dingtalk/im（默认 im）"},
			"phase_id":    map[string]string{"type": "string", "description": workflowDocDeliveryPhaseIDSchemaDescription()},
			"doc_type":    map[string]string{"type": "string", "description": workflowDocDeliveryTypeSchemaDescription()},
		}, []string{"path"},
		func(args map[string]interface{}) string { return h.toolSendToIM(args) })

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

	// --- TTS voice synthesis ---
	reg("tts", "将文本转换为语音消息发送给用户。IM 通道以语音气泡形式发送，桌面面板播放语音。",
		ToolCategoryBuiltin, []string{"tts", "voice", "speech", "语音"},
		map[string]interface{}{
			"text": map[string]string{"type": "string", "description": "要转换为语音的文本内容（中文，最长 300 字）"},
		}, []string{"text"},
		func(args map[string]interface{}) string { return h.toolTTS(args) })

	// --- ASR speech recognition (local SenseVoice) ---
	reg("asr", audioconv.ASRToolDescription(),
		ToolCategoryBuiltin, []string{"asr", "transcribe", "transcription", "speech", "audio", "voice", "语音", "转写", "转录", "语音识别", "录音"},
		map[string]interface{}{
			"path":           map[string]string{"type": "string", "description": "本地音频文件路径"},
			"format":         map[string]string{"type": "string", "description": "可选格式提示: wav/mp3/ogg/opus/silk（默认按扩展名与文件头自动检测）"},
			"for_minutes":    map[string]string{"type": "boolean", "description": "true=长转写后运行引擎 LLM map-reduce 生成会议纪要草稿（较慢）；默认 false 仅快速 extractive 草稿"},
			"minutes":        map[string]string{"type": "boolean", "description": "for_minutes 的别名"},
			"known_speakers": map[string]string{"type": "integer", "description": "已知/用户确认的说话人数量（1-15）。0 或省略=自动估计。用户确认人数后应传入"},
			"speakers":       map[string]string{"type": "integer", "description": "known_speakers 的别名"},
		}, []string{"path"},
		func(args map[string]interface{}) string { return h.toolASR(args) })

	// --- Long-term memory (unified) ---
	memoryTool := corememory.ToolDefinitionSchema()
	reg("memory", memoryTool.Description,
		ToolCategoryBuiltin, memoryTool.Tags,
		memoryTool.Properties, memoryTool.Required,
		func(args map[string]interface{}) string { return h.toolMemory(args) })

	// --- Experience learning governance ---
	reg("experience_learning", "Inspect and record experience-learning governance without executing changes. Actions include snapshot pointing to governance_summary, governance_summary.memory carrying the memory maintenance, governance_summary.routing_self_evolution carrying the routing_signals and tool_recovery_governance, tool_recovery provider/model/wire_api filters, governance_summary.a2a_discussion carrying read-only A2A trace inspection handoffs, next_actions, queues, follow_up_actions, routing_signals, tool_recovery/inspect_tool_recovery_governance/recovery_governance/tool_recovery_governance handoffs, memory_candidates, trace_details exposing read-only non_executing_boundary, build_*_draft helpers, build_blocked_skill_draft repair/evidence draft, record_followup, record_review, record_draft_review, and record_blocked_skill_draft_review. Responses expose recommended_focus_context, recommended_tool_call, governance_focus_context, and non_executing=true boundaries. This tool must never approve reviews, rewrite memory, change routing, retry execution, change credentials, write files, execute tools, notify users, or install skills by itself.",
		ToolCategoryBuiltin, []string{"experience", "learning", "governance", "memory", "routing", "review", "recovery"},
		map[string]interface{}{
			"action":                  map[string]string{"type": "string", "description": "Action: snapshot/governance_summary/next_actions/queues/follow_up_actions/routing_signals/tool_recovery/recovery_governance/tool_recovery_governance/inspect_tool_recovery_governance/build_routing_adjustment_draft/memory_candidates/build_memory_maintenance_draft/trace_details/build_followup/build_skill_draft/build_blocked_skill_draft/build_rollback_draft/build_escalation_brief/build_conflict_draft/record_followup/record_review/record_draft_review/record_blocked_skill_draft_review"},
			"query":                   map[string]string{"type": "string", "description": "Search or filter query for governance queues, memory candidates, routing signals, or trace details."},
			"q":                       map[string]string{"type": "string", "description": "Alias for query."},
			"trace_id":                map[string]string{"type": "string", "description": "Trace id for exact trace_details filtering, review, or draft-review attachment."},
			"source_trace_id":         map[string]string{"type": "string", "description": "Source trace id that a generated draft or follow-up action is based on."},
			"draft_kind":              map[string]string{"type": "string", "description": "Draft kind: skill_draft, rollback_workflow_draft, escalation_brief, conflict_reconciliation_draft, routing_adjustment_draft, memory_maintenance_draft."},
			"draft_id":                map[string]string{"type": "string", "description": "Exact governance draft id being reviewed, such as skill_draft:mark_needs_review:broken:."},
			"resolution":              map[string]string{"type": "string", "description": "For record_blocked_skill_draft_review: reopen or close."},
			"replacement_draft_id":    map[string]string{"type": "string", "description": "For record_blocked_skill_draft_review resolution=reopen: replacement governance draft id to preview."},
			"draft_markdown":          map[string]string{"type": "string", "description": "Markdown body for a non-executing draft review record."},
			"non_executing_boundary":  map[string]string{"type": "string", "description": "Caller-supplied boundary text confirming the record is audit-only and non-executing."},
			"status":                  map[string]string{"type": "string", "description": "Review or draft review status, such as approved/rejected/recorded/blocked."},
			"note":                    map[string]string{"type": "string", "description": "Reviewer note or audit note."},
			"actor":                   map[string]string{"type": "string", "description": "Human or system actor recording the review evidence."},
			"follow_up_action_kind":   map[string]string{"type": "string", "description": "Filter or record follow-up actions by kind, such as routing_adjustment_draft, skill_draft, rollback_workflow_draft, escalation_brief, conflict_reconciliation_draft, or memory_maintenance_draft."},
			"triggered_rollback_only": map[string]string{"type": "boolean", "description": "When true, only include follow-up actions that triggered rollback-only handling."},
			"tool":                    map[string]string{"type": "string", "description": "Tool name filter for routing_signals or tool_recovery governance."},
			"category":                map[string]string{"type": "string", "description": "Failure category filter for tool_recovery governance."},
			"provider":                map[string]string{"type": "string", "description": "Provider filter for tool_recovery governance."},
			"model":                   map[string]string{"type": "string", "description": "Model filter for tool_recovery governance."},
			"wire_api":                map[string]string{"type": "string", "description": "Wire API filter for tool_recovery governance."},
			"review_only":             map[string]string{"type": "boolean", "description": "When true, only return tool recovery summaries that require review."},
			"limit":                   map[string]string{"type": "integer", "description": "Maximum number of rows or summaries to return."},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolExperienceLearning(args) })

	// --- Active context compression (inspired by GenericAgent's working checkpoint) ---
	reg("compress_context", "主动压缩当前对话上下文。完成子任务后调用，将详细工具调用历史替换为摘要，释放 context 空间。",
		ToolCategoryBuiltin, []string{"context", "compress", "checkpoint", "summary"},
		map[string]interface{}{
			"summary":       map[string]string{"type": "string", "description": "当前工作状态摘要（已完成的工作、文件列表、关键决策、下一步计划）"},
			"preserve_last": map[string]string{"type": "integer", "description": "保留最近 N 条对话条目不压缩（可选，默认 4）"},
		}, []string{"summary"},
		func(args map[string]interface{}) string { return h.toolCompressContext(args) })

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

	// --- Merged tool: manage_schedule (create/list/delete/update/list_targets) ---
	reg("manage_schedule", "定时任务管理。action: create/list/delete/update/list_targets。list_targets 的 channel：lansenger（群/人）、weixin/telegram/qq（self）。create/update 可配 delivery 推送；蓝信 group_name 可解析为 group_id。即时发消息请用 im_message，不要用定时任务硬绕。",
		ToolCategoryBuiltin, []string{"schedule", "task", "cron", "timer", "interval", "create", "list", "delete", "update", "delivery"},
		map[string]interface{}{
			"action":           map[string]string{"type": "string", "description": "操作: create/list/delete/update/list_targets"},
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
			"delivery":         map[string]string{"type": "object", "description": "结果推送 {enabled,channel,targets:[{kind,group_id|group_name|user_id}]}"},
			"channel":          map[string]string{"type": "string", "description": "list_targets 或 delivery 通道"},
			"query":            map[string]string{"type": "string", "description": "list_targets 名称/ID 过滤"},
			"group_name":       map[string]string{"type": "string", "description": "delivery 简写：群名"},
			"group_id":         map[string]string{"type": "string", "description": "delivery 简写：群 ID"},
			"user_id":          map[string]string{"type": "string", "description": "delivery 简写：私聊 ID 或 self"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolManageSchedule(args) })

	// --- Immediate IM message (proactive push, independent of schedule) ---
	reg("im_message", "即时向 IM 通道发文本消息（蓝信群/人、微信/Telegram/QQ 最近会话）。action: list_targets|send（可省略：有 text 则 send，有 query/群名则 list）。用户说「给蓝信某群发…」「推送到微信」时用本工具，不要用 manage_schedule 绕路。send 需 text + group_name/group_id/user_id。",
		ToolCategoryBuiltin, []string{"im", "message", "lansenger", "weixin", "telegram", "qq", "push", "notify", "group"},
		map[string]interface{}{
			"action":           map[string]string{"type": "string", "description": "list_targets 或 send；可省略并自动推断"},
			"text":             map[string]string{"type": "string", "description": "send 时消息正文"},
			"message":          map[string]string{"type": "string", "description": "text 别名"},
			"channel":          map[string]string{"type": "string", "description": "lansenger|weixin|telegram|qq（默认 lansenger）"},
			"query":            map[string]string{"type": "string", "description": "list_targets 时按名称/ID 过滤"},
			"group_name":       map[string]string{"type": "string", "description": "send：群名（自动解析 group_id）"},
			"group_id":         map[string]string{"type": "string", "description": "send：群 ID"},
			"user_id":          map[string]string{"type": "string", "description": "send：私聊 ID；weixin/telegram/qq 可用 self"},
			"mention_user_ids": map[string]string{"type": "string", "description": "群消息可选 @ 用户 ID，逗号分隔"},
			"mention_all":      map[string]string{"type": "boolean", "description": "群消息是否 @所有人"},
			"delivery":         map[string]string{"type": "object", "description": "可选完整投递配置 {channel,targets:[...]}"},
		}, nil,
		func(args map[string]interface{}) string { return h.toolIMMessage(args) })

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

	reg("web_fetch", "抓取指定 URL 的网页内容并提取正文文本。支持自动编码检测（GBK/UTF-8 等）、HTML 正文提取。可选 JS 渲染（需本机安装 Chrome）。下载文件时务必传 save_path（相对路径落在当前工作目录）。通用 HTTP/PDF 下载优先用 download_file 或本工具+save_path，不要安装 ClawHub 的 wget/curl skill。save_path 下载内置反爬升级：遇 Cloudflare/403 拦截会自动换浏览器请求头重试，仍被拦时尝试复用浏览器会话 cookie（需先用 browser 工具打开过该网站）。下载过程日志见 ~/.maclaw/logs/download.log。长页面支持续读：当返回 has_more=true 时，请使用 offset=next_offset 继续读取后续内容。",
		ToolCategoryBuiltin, []string{"web", "fetch", "download", "url", "browse", "network"},
		map[string]interface{}{
			"url":                 map[string]string{"type": "string", "description": "要抓取的 URL"},
			"render_js":           map[string]string{"type": "boolean", "description": "是否使用 Chrome 渲染 JS（可选，默认 false）"},
			"save_path":           map[string]string{"type": "string", "description": "保存文件路径（可选，指定后下载文件而非返回文本；相对路径相对当前工作目录）"},
			"timeout":             map[string]string{"type": "integer", "description": "超时秒数（可选，默认 600，范围 240-600）"},
			"offset":              map[string]string{"type": "integer", "description": "从第几个字符开始读取（用于长页面续读，默认 0）"},
			"max_chars":           map[string]string{"type": "integer", "description": "本次最多返回字符数（可选；不传表示返回全部提取内容）"},
			"headers":             map[string]string{"type": "object", "description": "自定义请求头（可选，如 {\"Referer\": \"...\"})"},
			"cookie":              map[string]string{"type": "string", "description": "Cookie 请求头快捷参数（可选）"},
			"use_browser_cookies": map[string]string{"type": "boolean", "description": "复用浏览器会话的 cookie/UA 发起请求（可选；需先用 browser 工具打开过该网站，适用于 Cloudflare 等验证场景）"},
			"via_browser":         map[string]string{"type": "boolean", "description": "让浏览器亲自下载该 URL（可选，需配合 save_path；用于 HTTP 级下载全部被反爬拦截时）"},
		}, []string{"url"},
		func(args map[string]interface{}) string { return h.toolWebFetch(args) })

	reg("download_file", "将 HTTP/HTTPS URL 下载到当前工作目录（顶栏 working_directory / 项目工作区）。通用文件与 PDF 下载的首选工具；返回绝对路径。不要为简单下载去 Hub 安装 wget/curl/Paper Fetch。内置反爬升级链：被 Cloudflare/403 拦截时自动逐级升级（浏览器请求头 → 模拟 Chrome TLS 指纹 → 复用浏览器会话 cookie）；网络错误与 429/5xx 自动退避重试。终极手段：via_browser=true 让浏览器亲自下载（真实内核 cookie/指纹/JS 环境，可过强反爬与内联 PDF）。下载过程日志见 ~/.maclaw/logs/download.log。",
		ToolCategoryBuiltin, []string{"download", "file", "http", "https", "pdf", "url", "wget", "curl", "fetch"},
		map[string]interface{}{
			"url":                 map[string]string{"type": "string", "description": "要下载的 URL"},
			"save_path":           map[string]string{"type": "string", "description": "保存路径（可选；相对路径相对当前工作目录；省略则用 URL 文件名）"},
			"output":              map[string]string{"type": "string", "description": "save_path 的别名"},
			"timeout":             map[string]string{"type": "integer", "description": "超时秒数（可选）"},
			"headers":             map[string]string{"type": "object", "description": "自定义请求头（可选，如 {\"Referer\": \"...\"})"},
			"cookie":              map[string]string{"type": "string", "description": "Cookie 请求头快捷参数（可选）"},
			"use_browser_cookies": map[string]string{"type": "boolean", "description": "复用浏览器会话的 cookie/UA 下载（可选；需先用 browser 工具打开过该网站，适用于 Cloudflare 等验证场景）"},
			"via_browser":         map[string]string{"type": "boolean", "description": "让浏览器亲自下载该 URL（可选；浏览器内核发起请求，带其 cookie/指纹，内联 PDF 也会强制存盘；用于 HTTP 级下载全部被反爬拦截时）"},
		}, []string{"url"},
		func(args map[string]interface{}) string { return h.toolDownloadFile(args) })

	// --- Unified office document tool ---
	reg("office", "Office 文档操作工具。action 参数：generate_pdf（生成PDF文档）、read_excel（读取XLSX/CSV表格）、write_excel（写入XLSX表格）、read_pptx（读取PPT演示文稿）。Office document tool: generate PDF, read/write Excel (XLSX/CSV), read PowerPoint (PPTX).",
		ToolCategoryBuiltin, []string{"office", "pdf", "excel", "xlsx", "csv", "pptx", "document", "spreadsheet", "presentation"},
		map[string]interface{}{
			"action":    map[string]string{"type": "string", "description": "操作类型: generate_pdf/read_excel/write_excel/read_pptx"},
			"content":   map[string]string{"type": "string", "description": "Markdown 格式的文档内容（generate_pdf 时必填）"},
			"title":     map[string]string{"type": "string", "description": "文档标题，显示在 PDF 封面（generate_pdf 时可选）"},
			"doc_type":  map[string]string{"type": "string", "description": "文档类型（generate_pdf 时可选）: requirements/design/task_plan。影响文件名前缀，不传则使用通用前缀。"},
			"phase_id":  map[string]string{"type": "string", "description": workflowDocGeneratePDFPhaseIDSchemaDescription()},
			"file_path": map[string]string{"type": "string", "description": "文件路径（read_excel/write_excel/read_pptx 时必填）"},
			"sheet":     map[string]string{"type": "string", "description": "工作表名称（read_excel 时可选，默认第一个工作表）"},
			"range":     map[string]string{"type": "string", "description": "A1 表示法的单元格范围，如 A1:D10（read_excel 时可选）"},
			"data":      map[string]string{"type": "object", "description": "写入数据（write_excel 时必填），格式: {\"sheets\": [{\"name\": \"Sheet1\", \"rows\": [[...]]}]}"},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolOffice(args) })

	// --- MIS structured data and AgentView transaction workspace ---
	reg("mis_data", "Structured MIS data tool for semantic business intents, business actions, and local AgentView transaction workspace.",
		ToolCategoryBuiltin, []string{"mis", "data", "business", "agent_view", "transaction", "structured"},
		map[string]interface{}{
			"action":              map[string]string{"type": "string", "description": "Action name. Use list_business_objects and resolve_object_role for MaClaw App object-role binding; use list_agent_transactions to open the local right-side transaction workspace without requiring the MIS service."},
			"query":               map[string]string{"type": "string", "description": "Natural-language business intent query for resolve_intent."},
			"domain":              map[string]string{"type": "string", "description": "Optional MIS domain filter."},
			"business_action_id":  map[string]string{"type": "string", "description": "Business action id for get_business_action, execute_business_action, or transaction filtering."},
			"dataset_id":          map[string]string{"type": "string", "description": "Dataset/business object id for data actions or transaction filtering."},
			"object_role":         map[string]string{"type": "string", "description": "Semantic MaClaw App object role, such as expense_report or employee. Record and approval actions resolve it to dataset_id when dataset_id is omitted."},
			"app_id":              map[string]string{"type": "string", "description": "MaClaw App id used when resolving object_role bindings."},
			"blueprint_id":        map[string]string{"type": "string", "description": "MaClaw App blueprint id used when resolving object_role bindings."},
			"require_initialized": map[string]string{"type": "boolean", "description": "For resolve_object_role, require that the mapped dataset has already been installed/initialized."},
			"record_id":           map[string]string{"type": "string", "description": "Record id for record-level operations."},
			"data":                map[string]string{"type": "object", "description": "Structured payload for writes, validation, imports, queries, or action execution."},
			"limit":               map[string]string{"type": "integer", "description": "Optional result limit."},
			"dry_run":             map[string]string{"type": "boolean", "description": "Validate business action execution without committing."},
		}, []string{"action"},
		func(args map[string]interface{}) string { return h.toolMISData(args) })

	// --- PDF generation tool - backward-compatible alias ---
	reg("generate_pdf", "生成 PDF 文档并发送给用户。将 Markdown 内容渲染为专业排版的 PDF 文件。参数 title 用作 PDF 封面标题，doc_type 可选用于文件名前缀分类。",
		ToolCategoryBuiltin, []string{"pdf", "document", "generate"},
		map[string]interface{}{
			"content":  map[string]string{"type": "string", "description": "Markdown 格式的文档内容"},
			"title":    map[string]string{"type": "string", "description": "文档标题，显示在 PDF 封面（如「竞品分析报告」「需求文档」等）"},
			"doc_type": map[string]string{"type": "string", "description": "文档类型（可选）: requirements/design/task_plan。影响文件名前缀，不传则使用通用前缀。"},
			"phase_id": map[string]string{"type": "string", "description": workflowDocGeneratePDFPhaseIDSchemaDescription()},
		}, []string{"content"},
		func(args map[string]interface{}) string {
			args["action"] = "generate_pdf"
			return h.toolOffice(args)
		})

	// --- SSH remote server tools ---
	reg("ssh", "SSH 远程服务器管理（connect/exec/exec_background/check_task/wait_task/list_tasks/kill_task/upload/download/list/close）。长命令自动转后台模式，支持 SFTP 文件传输。",
		ToolCategoryBuiltin, []string{"ssh", "remote", "server", "connect", "exec", "background", "upload", "download", "sftp"},
		map[string]interface{}{
			"action":          map[string]string{"type": "string", "description": "操作: connect/exec/exec_background/check_task/wait_task/list_tasks/kill_task/upload/download/list/close"},
			"host":            map[string]string{"type": "string", "description": "远程主机地址（connect 时必填）"},
			"user":            map[string]string{"type": "string", "description": "登录用户名（connect 时必填）"},
			"port":            map[string]string{"type": "integer", "description": "SSH 端口（默认 22）"},
			"auth_method":     map[string]string{"type": "string", "description": "认证方式: password/key/agent。当用户提供了密码时必须设为 password"},
			"key_path":        map[string]string{"type": "string", "description": "私钥路径（auth_method=key 时可选）"},
			"password":        map[string]string{"type": "string", "description": "SSH 登录密码。当用户提供了密码时必须传此参数，不要省略"},
			"label":           map[string]string{"type": "string", "description": "主机标签（可选，如 prod-web-01）"},
			"initial_command": map[string]string{"type": "string", "description": "连接后立即执行的命令（可选）"},
			"session_id":      map[string]string{"type": "string", "description": "SSH 会话 ID（exec/exec_background/upload/download/close 时必填）"},
			"command":         map[string]interface{}{"type": "string", "description": "Command to execute (required for exec/exec_background). For long scripts, write/upload a script file first, then execute that file.", "maxLength": maxAgentLoopInlineSSHCommandRunes},
			"wait_seconds":    map[string]string{"type": "integer", "description": "等待输出秒数（exec 时可选，默认 5，最大 600）"},
			"task_id":         map[string]string{"type": "string", "description": "后台任务 ID（check_task/wait_task/kill_task 时必填）"},
			"tail_lines":      map[string]string{"type": "integer", "description": "查看日志尾部行数（check_task/wait_task 时可选，默认 50）"},
			"timeout":         map[string]string{"type": "integer", "description": "wait_task 等待超时秒数（默认 60，最大 600）"},
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
