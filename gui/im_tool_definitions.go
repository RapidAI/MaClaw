package main

// Tool definitions: legacy hardcoded tool schema builder (buildToolDefinitions + toolDef helper).

import (
	"fmt"
)

func (h *IMMessageHandler) buildToolDefinitions() []map[string]interface{} {
	defs := []map[string]interface{}{
		toolDef("list_sessions", "列出当前所有远程会话及其状态", nil, nil),
		toolDef("create_session", "创建新的远程编程会话。仅用于明确的代码修改/编程任务，可指定 provider 选择服务商，也可传 resume_session_id 恢复结构化会话。若是服务器运维、SSH 登录、日志排查，请改用 ssh 工具；若需求模糊，先澄清后再创建。创建后建议用 get_session_output 观察启动状态。",
			map[string]interface{}{
				"tool":              map[string]string{"type": "string", "description": "工具名称，如 claude, codex, cursor, gemini, opencode"},
				"project_path":      map[string]string{"type": "string", "description": "项目路径（可选）"},
				"project_id":        map[string]string{"type": "string", "description": "预设项目 ID（可选，与 project_path 二选一）"},
				"provider":          map[string]string{"type": "string", "description": "服务商名称（可选，如 Original, DeepSeek, 百度千帆）。不指定则使用桌面端当前选中的服务商"},
				"resume_session_id": map[string]string{"type": "string", "description": "续接会话 ID（可选）。自动续接时由 get_session_output 返回，传入后使用 --resume 模式恢复完整对话历史"},
			}, []string{"tool"}),
		toolDef("list_providers", "列出指定编程工具的所有可用服务商（已过滤未配置的空服务商）",
			map[string]interface{}{
				"tool": map[string]string{"type": "string", "description": "工具名称，如 claude, codex, gemini"},
			}, []string{"tool"}),
		toolDef("ssh", "SSH 远程服务器管理（connect/exec/exec_background/check_task/list_tasks/kill_task/upload/download/list/close）。适用于服务器登录、远程命令、日志排查、服务重启与文件传输。长命令请优先使用 exec_background。",
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
			}, []string{"action"}),
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
		toolDef("call_mcp_tool", "调用指定 MCP Server 上的工具（server_id 支持 ID 或 Name，重名时请传 ID）",
			map[string]interface{}{
				"server_id": map[string]string{"type": "string", "description": "MCP Server ID 或 Name"},
				"tool_name": map[string]string{"type": "string", "description": "工具名称"},
				"arguments": map[string]string{"type": "object", "description": "工具参数（JSON 对象）"},
			}, []string{"server_id", "tool_name"}),
		toolDef("manage_skill", "Skill 管理（action: list/search/install/run/status/upload/validate）。list 列出本地已注册 Skill（无 Skill 时展示 Hub 推荐）；search 在 SkillHub 搜索可用 Skill；install 从 Hub 安装 Skill 到本地；run 执行指定 Skill；status 查询运行状态（run 返回 run_id 后继续观察进度）；upload 上传本地 Skill 到 SkillMarket；validate 检查 Skill 的跨平台可移植性并可选自动修复。",
			map[string]interface{}{
				"action":       map[string]string{"type": "string", "description": "操作: list/search/install/run/status/upload/validate"},
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
				"auto_fix":     map[string]string{"type": "boolean", "description": "与 action=validate 配合使用，为 true 时自动修复检测到的可移植性问题（可选，默认 false）"},
			}, []string{"action"}),
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
		toolDef("craft_tool", "当现有工具、Skill 或会话式编程都不合适时，生成并执行单脚本来完成一次性自动化任务。更适合本机数据处理、API 调用、文件转换和小型系统自动化；不适合复杂代码库改造或长链路编程任务。",
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
			}, []string{"task"}),
		// --- 本机直接操作工具 ---
		toolDef("bash", "在本机直接执行 shell 命令（如创建目录、移动文件、运行脚本等）。命令在 MaClaw 所在设备上执行，不需要会话。",
			map[string]interface{}{
				"command":     map[string]string{"type": "string", "description": "要执行的 shell 命令"},
				"working_dir": map[string]string{"type": "string", "description": "工作目录（可选，默认为 ~/.maclaw/workspace）"},
				"timeout":     map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
			}, []string{"command"}),
		toolDef("read_file", "读取本机文件内容",
			map[string]interface{}{
				"path":  map[string]string{"type": "string", "description": "文件路径（绝对路径或相对于主目录的路径）"},
				"lines": map[string]string{"type": "integer", "description": "最多读取行数（可选，默认 200）"},
			}, []string{"path"}),
		toolDef("write_file", "写入内容到本机文件（UTF-8 编码，支持覆盖或追加，允许空内容，会创建不存在的目录。大文件请分块写入：先 overwrite 第一部分，再 append 后续部分）",
			map[string]interface{}{
				"path":    map[string]string{"type": "string", "description": "文件路径"},
				"content": map[string]string{"type": "string", "description": "文件内容，可为空字符串"},
				"mode":    map[string]string{"type": "string", "description": "写入模式：overwrite（默认）或 append"},
			}, []string{"path", "content"}),
		toolDef("edit_file", "编辑已有文件内容（按文本替换，支持替换首处或全部匹配）",
			map[string]interface{}{
				"path":        map[string]string{"type": "string", "description": "文件路径"},
				"old_string":  map[string]string{"type": "string", "description": "要查找的原始文本"},
				"new_string":  map[string]string{"type": "string", "description": "替换后的文本，可为空字符串"},
				"replace_all": map[string]string{"type": "boolean", "description": "是否替换全部匹配，默认 false"},
			}, []string{"path", "old_string", "new_string"}),
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
		// --- 结构化提问工具 ---
		toolDef("ask_user", "向用户提出结构化问题并等待回答。适用于需要用户从多个选项中选择、或提供缺失信息的场景。注意：编码工作流的阶段确认（需求/设计/任务确认）不要使用此工具，直接在回复文本中提示用户确认即可。",
			map[string]interface{}{
				"question":   map[string]string{"type": "string", "description": "要问用户的问题"},
				"options":    map[string]interface{}{"type": "array", "description": "可选：预设选项列表，用户可从中选择", "items": map[string]string{"type": "string"}},
				"context":    map[string]string{"type": "string", "description": "可选：问题的背景说明，帮助用户理解为什么需要这个信息"},
				"input_type": map[string]string{"type": "string", "description": "期望的回答类型: choice（从选项中选择）/text（自由文本）/confirm（是/否确认）。默认 text"},
			}, []string{"question"}),
		// --- 任务管理工具 ---
		toolDef("task", "管理任务（action: create/update/complete/fail/list/delegate/delete）。用于跟踪复杂任务的进度、依赖关系和子任务分配。当任务涉及多个步骤时，先用 create 拆分任务，再逐个执行并用 complete/fail 更新状态。",
			map[string]interface{}{
				"action":      map[string]string{"type": "string", "description": "操作: create/update/complete/fail/list/delegate/delete"},
				"task_id":     map[string]string{"type": "string", "description": "任务 ID（update/complete/fail/delegate/delete 时必填）"},
				"title":       map[string]string{"type": "string", "description": "任务标题（create 时必填）"},
				"description": map[string]string{"type": "string", "description": "任务描述（create 时可选）"},
				"depends_on":  map[string]interface{}{"type": "array", "description": "依赖的任务 ID 列表（create 时可选）", "items": map[string]string{"type": "string"}},
				"status":      map[string]string{"type": "string", "description": "新状态（update 时使用）: pending/in_progress/completed/failed/blocked"},
				"status_note": map[string]string{"type": "string", "description": "状态更新说明（update 时可选）"},
				"delegate_to": map[string]string{"type": "string", "description": "委派给哪个会话或 Agent（delegate 时必填）"},
			}, []string{"action"}),
		// --- 子 Agent 委派工具 ---
		toolDef("delegate_task", "将任务委派给专业子 Agent 处理。不传 agent 参数时列出可用的子 Agent。可用子 Agent: coding_workflow（编码工作流：需求→设计→任务拆分）、help（MaClaw 使用帮助）。",
			map[string]interface{}{
				"agent":   map[string]string{"type": "string", "description": "子 Agent 名称: coding_workflow / help。不传则列出所有可用子 Agent"},
				"request": map[string]string{"type": "string", "description": "要委派的任务描述（用户的原始需求）"},
			}, nil),
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
		// --- 合并工具：模板管理 (create/list/launch) ---
		toolDef("manage_template", "会话模板管理（action: create/list/launch）。create 创建模板，list 列出所有模板，launch 使用模板启动会话。",
			map[string]interface{}{
				"action":        map[string]string{"type": "string", "description": "操作: create/list/launch"},
				"name":          map[string]string{"type": "string", "description": "模板名称（create/launch 时必填）"},
				"tool":          map[string]string{"type": "string", "description": "工具名称（create 时必填）"},
				"project_path":  map[string]string{"type": "string", "description": "项目路径（create 时可选）"},
				"model_config":  map[string]string{"type": "string", "description": "模型配置（create 时可选）"},
				"yolo_mode":     map[string]string{"type": "boolean", "description": "是否开启 Yolo 模式（create 时可选）"},
			}, []string{"action"}),
		// --- 合并工具：配置管理 (get/set/batch/schema/export/import) ---
		toolDef("manage_config", "配置管理（action: get/set/batch/schema/export/import）。get 获取配置，set 修改单项，batch 批量修改，schema 列出可配置项，export 导出，import 导入。",
			map[string]interface{}{
				"action":    map[string]string{"type": "string", "description": "操作: get/set/batch/schema/export/import"},
				"section":   map[string]string{"type": "string", "description": "配置区域（get/set 时使用，如 claude/gemini/remote/projects/maclaw_llm/proxy/general）"},
				"key":       map[string]string{"type": "string", "description": "配置项名称（set 时必填）"},
				"value":     map[string]string{"type": "string", "description": "新值（set 时必填）"},
				"changes":   map[string]string{"type": "string", "description": "JSON 数组（batch 时必填），每项含 section/key/value"},
				"json_data": map[string]string{"type": "string", "description": "配置 JSON 字符串（import 时必填）"},
			}, []string{"action"}),
		// --- Agent 自管理工具 ---
		toolDef("set_max_iterations", fmt.Sprintf("调整当前任务的最大推理轮数。仅影响当前推理循环，不会修改设置页中的持久化配置。当你判断任务复杂需要更多轮次时调用此工具扩展上限，任务简单时可缩减。范围 %d-%d。", minAgentIterations, maxAgentIterationsCap),
			map[string]interface{}{
				"max_iterations": map[string]string{"type": "integer", "description": fmt.Sprintf("新的最大轮数（%d-%d）", minAgentIterations, maxAgentIterationsCap)},
				"reason":         map[string]string{"type": "string", "description": "调整原因（用于日志记录）"},
			}, []string{"max_iterations"}),
		// --- 合并工具：定时任务 (create/list/delete/update) ---
		toolDef("manage_schedule", "定时任务管理（action: create/list/delete/update）。create 创建定时任务，list 列出所有任务，delete 删除任务，update 修改任务。day_of_week: -1=每天, 0=周日, 1=周一...6=周六。day_of_month: -1=不限, 1-31。一次性任务请将 start_date 和 end_date 都设为目标日期。",
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
			}, []string{"action"}),
	}

	// ---------- AgentNet tools (dynamic — only when daemon is running) ----------
	if h.app != nil && h.app.agentNetClient != nil && h.app.agentNetClient.IsRunning() {
		defs = append(defs,
			toolDef("agentnet_search", "在智网（AgentNet P2P 知识网络）中搜索知识条目。返回匹配的知识列表，包含标题、内容、作者等。",
				map[string]interface{}{
					"query": map[string]string{"type": "string", "description": "搜索关键词"},
				}, []string{"query"}),
			toolDef("agentnet_publish", "向智网（AgentNet P2P 知识网络）发布一条知识条目。发布后其他节点可以搜索到。",
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
		toolDef("web_fetch", "抓取指定 URL 的网页内容并提取正文文本。支持 HTTP/HTTPS/FTP 协议，自动编码检测（GBK/UTF-8 等）、HTML 正文提取。可选 JS 渲染（需本机安装 Chrome）。也可用 save_path 下载文件到本地。长页面支持续读：当返回 has_more=true 时，请使用 offset=next_offset 继续读取后续内容。",
			map[string]interface{}{
				"url":       map[string]string{"type": "string", "description": "要抓取的 URL（支持 http/https/ftp 协议）"},
				"render_js": map[string]string{"type": "boolean", "description": "是否使用 Chrome 渲染 JS（可选，默认 false。适用于 SPA 等 JS 渲染页面）"},
				"save_path": map[string]string{"type": "string", "description": "保存文件路径（可选。指定后将原始内容保存到文件而非返回文本，适用于下载文件）"},
				"timeout":   map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
				"offset":    map[string]string{"type": "integer", "description": "从第几个字符开始读取（用于长页面续读，默认 0）"},
				"max_chars": map[string]string{"type": "integer", "description": "本次最多返回字符数（可选；不传表示返回全部提取内容）"},
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
