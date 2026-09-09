package guiapp

// Tool definitions: legacy hardcoded tool schema builder (buildToolDefinitions + toolDef helper).

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func (h *IMMessageHandler) buildToolDefinitions() []map[string]interface{} {
	defs := []map[string]interface{}{
		toolDef("list_sessions", "列出当前所有远程会话及其状态", nil, nil),
		toolDefFromCore("ssh", "SSH 远程服务器管理（connect/exec/exec_background/check_task/wait_task/list_tasks/kill_task/upload/download/list/close）。适用于服务器登录、远程命令、日志排查、服务重启与文件传输。长命令请优先使用 exec_background。重要：连接后如果要执行后台任务，请先用 list_tasks 检查是否已有相同任务在运行，避免重复创建。",
			map[string]interface{}{
				"command": map[string]interface{}{"type": "string", "description": "要执行的命令（exec/exec_background 时必填）。长脚本请先写入/上传脚本文件，再执行脚本路径。", "maxLength": maxAgentLoopInlineSSHCommandRunes},
			}),
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
		toolDefFromCore("screenshot", "截取屏幕截图并发送给用户。这是截屏的唯一正确方式，禁止用 bash 编写 PowerShell/Python/scrot 等截屏脚本替代此工具。使用场景：(1) 用户明确要求截屏；(2) 用户通过 IM 远程监督，需要确认操作结果。不要在用户未要求时主动截屏。最小间隔 30 秒。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID（可选，只有一个会话时自动选择）"},
			}),
		toolDefFromCore("list_mcp_tools", "列出已注册的 MCP Server 及其工具（含参数详情）。支持按关键词搜索或按服务器过滤", nil),
		toolDef("call_mcp_tool", "调用指定 MCP Server 上的工具（server_id 支持 ID 或 Name，重名时请传 ID）",
			map[string]interface{}{
				"server_id": map[string]string{"type": "string", "description": "MCP Server ID 或 Name"},
				"tool_name": map[string]string{"type": "string", "description": "工具名称"},
				"arguments": map[string]string{"type": "object", "description": "工具参数（JSON 对象）"},
			}, []string{"server_id", "tool_name"}),
		toolDefFromCore("manage_skill", skill.ManageSkillDescription()+" patch 时如果你使用了某个 Skill 并遇到了它未覆盖的问题，请立即用 patch 修补它。",
			map[string]interface{}{
				"auto_run":     map[string]string{"type": "boolean", "description": "安装成功后是否立即执行（install 时可选，默认 true）"},
				"names":        map[string]interface{}{"type": "array", "description": "Suite 包含的多个本地 Skill 名称（upload_suite 时必填）", "items": map[string]string{"type": "string"}},
				"suite_name":   map[string]string{"type": "string", "description": "Suite 展示名称（upload_suite 可选，默认由 Skill 名称生成）"},
				"env":          map[string]string{"type": "object", "description": "注入到 skill 子进程的环境变量（run 时可选），例如 {\"LIBTV_ACCESS_KEY\": \"xxx\"}"},
				"wait_seconds": map[string]string{"type": "number", "description": "等待状态快照的秒数（install/run/status 时可选，默认 2，最大 30）"},
				"force":        map[string]string{"type": "boolean", "description": "与 action=upload 配合使用，为 true 时跳过可移植性/质量门禁强制上传（可选，默认 false。仅在你已确认并修正问题后使用）"},
			}),
		toolDef("parallel_execute", "按 SubAgent 并发数分批执行多个编程任务（最多5个任务，并发上限4），每个任务在独立会话中运行",
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
			}, []string{"tasks"}),
		toolDef("craft_tool", "当现有工具、Skill 或会话式编程都不合适时，生成并执行单脚本来完成一次性自动化任务。更适合本机数据处理、API 调用、文件转换和小型系统自动化；不适合复杂代码库改造或长链路编程任务。",
			map[string]interface{}{
				"task":               map[string]string{"type": "string", "description": "需要完成的任务描述（越详细越好）"},
				"language":           map[string]string{"type": "string", "description": "脚本语言: python/bash/powershell/node（可选，优先按运行时自动选择）"},
				"working_dir":        map[string]string{"type": "string", "description": "脚本执行工作目录（可选）"},
				"expected_artifacts": map[string]interface{}{"type": "array", "description": "期望生成的文件路径列表（可选，用于验收）", "items": map[string]string{"type": "string"}},
				"verification_mode":  map[string]string{"type": "string", "description": "验收模式（可选，如 artifact_required）"},
				"register_policy":    map[string]string{"type": "string", "description": "注册策略（可选，auto/manual）"},
				"max_attempts":       map[string]string{"type": "integer", "description": "最大自动修复尝试次数（默认 2，最大 3）"},
				"save_as_skill":      map[string]string{"type": "boolean", "description": "执行成功后是否请求注册为 Skill（默认 true，但一次性任务会更保守；实际自动写入仍须在设置中显式启用 Skill 自进化且未触发环境 kill switch）"},
				"skill_name":         map[string]string{"type": "string", "description": "Skill 名称（可选，自动生成）"},
				"timeout":            map[string]string{"type": "integer", "description": "执行超时秒数（默认 60，最大 300）"},
			}, []string{"task"}),
		// --- 本机直接操作工具 ---
		toolDefFromCore("bash", "在本机直接执行 shell 命令（如创建目录、移动文件、运行脚本等）。命令在 MaClaw 所在设备上执行，不需要会话。禁止通过 bash 执行 ssh/scp/rsync 命令——请使用内置 ssh 工具。支持 background=true 后台执行长时间命令（翻译、编译、下载等），返回 task_id 后可用 async_wait 查询状态。",
			map[string]interface{}{
				"command":    map[string]interface{}{"type": "string", "description": "要执行的 shell 命令", "maxLength": maxAgentLoopInlineBashCommandRunes},
				"background": map[string]string{"type": "boolean", "description": "后台执行（可选，默认 false）。设为 true 时命令在后台运行，立即返回 task_id，用 async_wait 查询进度。适用于翻译、编译、下载等长时间任务"},
			}),
		toolDefFromCore("read_file", "读取本机文件内容。支持 offset 参数从指定位置读取（适合监控日志文件的增量内容）", nil),
		toolDefFromCore("read_tool_result", "分段回读被截断的工具完整输出（使用 [tool_result_handle] 的 id）", nil),
		toolDefFromCore("write_file", "写入内容到本机文件（UTF-8 编码，支持覆盖或追加，允许空内容，会创建不存在的目录。内容无长度限制，超长内容系统会自动处理。超过约6000字符时建议分块写入：先 overwrite 第一部分，再 append 后续部分）",
			map[string]interface{}{
				"path":     map[string]string{"type": "string", "description": "文件路径（绝对路径或相对于 Project directory 的路径）"},
				"phase_id": map[string]string{"type": "string", "description": workflowDocPhaseIDSchemaDescription()},
				"doc_type": map[string]string{"type": "string", "description": workflowDocTypeSchemaDescription()},
			}),
		toolDefFromCore("edit_file", "编辑已有文件内容（按文本替换，支持替换首处或全部匹配）", nil),
		toolDefFromCore("list_directory", "列出本机目录内容",
			map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "目录路径（绝对路径或相对于 Project directory 的相对路径）"},
			}),
		toolDefFromCore("Glob", "按 glob 通配符查找本机文件路径，支持 ** 递归。先发现相关文件，再用 read_file 查看内容。", nil),
		toolDefFromCore("send_file", "读取本机文件并交付给用户。在桌面端默认只展示在当前桌面对话；在微信/飞书等 IM 通道中，发到当前对话即等于发到该通道。若语音指定另一个群或用户，先用 im_message(action=list_targets) 把名称解析为 ID，再同时提供 channel 与 group_id/user_id；精确目标失败时禁止广播或改投其他通道。",
			map[string]interface{}{
				"phase_id": map[string]string{"type": "string", "description": workflowDocDeliveryPhaseIDSchemaDescription()},
				"doc_type": map[string]string{"type": "string", "description": workflowDocDeliveryTypeSchemaDescription()},
			}),
		toolDefFromCore("send_to_im", "把本机文件发送到 IM。未指定精确目标时走用户绑定/活跃通道；若语音指定群名或用户名，先用 im_message(action=list_targets) 消歧并取得 ID，再同时提供 channel 与 group_id/user_id。精确目标发送失败时必须报告失败，禁止广播或改投其他通道。",
			map[string]interface{}{
				"phase_id": map[string]string{"type": "string", "description": workflowDocDeliveryPhaseIDSchemaDescription()},
				"doc_type": map[string]string{"type": "string", "description": workflowDocDeliveryTypeSchemaDescription()},
			}),
		toolDefFromCore("open", "用操作系统默认程序打开文件或网址。例如：打开 PDF 用默认阅读器、打开 .xlsx 用 Excel、打开 URL 用默认浏览器、打开文件夹用资源管理器。也支持 mailto: 链接。", nil),
		// --- 后台任务管理工具 ---
		toolDef("async_wait", "管理本机后台任务（与 bash(background=true) 配合使用）。action=check 查询状态，action=wait 阻塞等待完成，action=kill 终止任务，action=list 列出所有任务。",
			map[string]interface{}{
				"action":     map[string]string{"type": "string", "description": "操作: check（查询状态）、wait（等待完成）、kill（终止）、list（列出所有）"},
				"task_id":    map[string]string{"type": "string", "description": "任务 ID（check/wait/kill 必填，由 bash(background=true) 返回）"},
				"timeout":    map[string]string{"type": "integer", "description": "等待超时秒数（仅 wait，默认 60，最大 300）"},
				"tail_lines": map[string]string{"type": "integer", "description": "返回日志尾部行数（可选，默认 50）"},
			}, nil),
		// --- 结构化提问工具 ---
		toolDefFromCore("ask_user", "向用户提出结构化问题并等待回答。适用于需要用户从多个选项中选择、或提供缺失信息的场景。注意：编码工作流的阶段确认（需求/设计/任务确认）不要使用此工具，直接在回复文本中提示用户确认即可。", nil),
		// --- 长时交互录音 ---
		toolDefFromCore("record_audio", "打开交互式长时录音界面（波形+暂停/停止）并等待用户结束录音。用户当前消息已明确要求开始录音/会议录音时，立即调用本工具，不要再二次确认，也不要去目录里找已有音频。仅当意图含糊时再澄清。用户停止后，下一条消息会带上音频路径与时长等摘要，再继续转写/纪要或投递音频文件。若生成会议纪要，必须同时 send_file 投递原始音频（可点击路径，便于用户备份）。", nil),
		// --- 任务管理工具 ---
		toolDefFromCore("task", "管理任务（action: create/update/complete/fail/list/delegate/delete）。用于跟踪复杂任务的进度、依赖关系和子任务分配。当任务涉及多个步骤时，先用 create 拆分任务，再逐个执行并用 complete/fail 更新状态。", nil),
		// --- 子 Agent 委派工具 ---
		toolDefFromCore("delegate_task", "将任务委派给专业子 Agent 处理。不传 agent 参数时列出可用的子 Agent。coding_workflow 会同步运行内部 CodingSubAgent 完成编码任务，不返回占位激活文本；help 用于 MaClaw 使用帮助。",
			map[string]interface{}{
				"agent": map[string]string{"type": "string", "description": "子 Agent 名称: coding_workflow / help。不传则列出所有可用子 Agent"},
			}),
		// --- 语音合成工具 ---
		toolDefFromCore("tts", "将文本转换为语音消息发送给用户。文本会自动清理 Markdown 格式并截断到合适长度（最长 300 字）。桌面面板播放语音，IM 通道（企微/QQ）以语音消息（语音气泡，非文件附件）形式发送。适用于：状态通知、简短回复摘要、任务完成汇报、问候等场景。不适用于长文本音频化。", nil),
		// --- 语音识别工具 ---
		toolDefFromCore("asr", audioconv.ASRToolDescription(),
			map[string]interface{}{
				"known_speakers": map[string]string{"type": "integer", "description": "已知/用户确认的说话人数量（1-15）。0 或省略=自动估计。用户确认人数后应传入以提高说话人分离准确度"},
				"speakers":       map[string]string{"type": "integer", "description": "known_speakers 的别名"},
			}),
		// --- Long-term memory (unified) ---
		toolDefFromCore("memory", "", nil),
		toolDef("compress_context", "主动压缩当前对话上下文，释放 context 空间给后续工作。调用后对话继续，不会中断。适用于长程任务中连续 10+ 轮工具调用后、切换工作方向时、或 context 接近上限时。摘要应包含：已完成的工作、创建/修改的文件、关键决策、下一步计划。",
			map[string]interface{}{
				"summary":       map[string]string{"type": "string", "description": "当前工作状态摘要。应包含：已完成的工作、创建/修改的文件列表、关键决策和结论、下一步计划"},
				"preserve_last": map[string]string{"type": "integer", "description": "保留最近 N 条对话条目不压缩（可选，默认 4，最大 20）"},
			}, []string{"summary"}),
		// --- 合并工具：模板管理 (create/list/launch) ---
		toolDef("manage_template", "会话模板管理（action: create/list/launch）。create 创建模板，list 列出所有模板，launch 使用模板启动会话。",
			map[string]interface{}{
				"action":       map[string]string{"type": "string", "description": "操作: create/list/launch"},
				"name":         map[string]string{"type": "string", "description": "模板名称（create/launch 时必填）"},
				"coding_tool":  map[string]string{"type": "string", "description": "工具名称（create 时必填）"},
				"project_path": map[string]string{"type": "string", "description": "项目路径（create 时可选）"},
				"model_config": map[string]string{"type": "string", "description": "模型配置（create 时可选）"},
				"yolo_mode":    map[string]string{"type": "boolean", "description": "是否开启 Yolo 模式（create 时可选）"},
			}, []string{"action"}),
		toolDef("create_template", "会话模板别名工具：创建模板。",
			map[string]interface{}{
				"name":         map[string]string{"type": "string", "description": "模板名称"},
				"coding_tool":  map[string]string{"type": "string", "description": "工具名称"},
				"project_path": map[string]string{"type": "string", "description": "项目路径"},
				"model_config": map[string]string{"type": "string", "description": "模型配置"},
				"yolo_mode":    map[string]string{"type": "boolean", "description": "是否启用 Yolo 模式"},
			}, []string{"name", "coding_tool"}),
		toolDef("list_templates", "会话模板别名工具：列出模板。", nil, nil),
		toolDef("launch_template", "会话模板别名工具：启动模板。",
			map[string]interface{}{
				"template_name": map[string]string{"type": "string", "description": "模板名称"},
			}, []string{"template_name"}),
		// --- 合并工具：配置管理 (get/set/batch/schema/export/import) ---
		toolDef("manage_config", "配置管理（action: get/set/batch/schema/export/import）。get 获取配置，set 修改单项，batch 批量修改，schema 列出可配置项，export 导出，import 导入。",
			map[string]interface{}{
				"action":    map[string]string{"type": "string", "description": "操作: get/set/batch/schema/export/import"},
				"section":   map[string]string{"type": "string", "description": "配置区域（get/set 时使用，如 claude/codex/remote/projects/maclaw_llm/proxy/general）"},
				"key":       map[string]string{"type": "string", "description": "配置项名称（set 时必填）"},
				"value":     map[string]string{"type": "string", "description": "新值（set 时必填）"},
				"changes":   map[string]string{"type": "string", "description": "JSON 数组（batch 时必填），每项含 section/key/value"},
				"json_data": map[string]string{"type": "string", "description": "配置 JSON 字符串（import 时必填）"},
			}, []string{"action"}),
		// --- Agent 自管理工具 ---
		toolDef("set_max_iterations", fmt.Sprintf("调整当前任务的最大推理轮数。仅影响当前推理循环，不会修改设置页中的持久化配置。当你判断任务复杂需要更多轮次时调用此工具扩展上限，任务简单时可缩减。范围 %d-%d。", config.MinAgentIterations, config.MaxAgentIterationsCap),
			map[string]interface{}{
				"max_iterations": map[string]string{"type": "integer", "description": fmt.Sprintf("新的最大轮数（%d-%d）", config.MinAgentIterations, config.MaxAgentIterationsCap)},
				"reason":         map[string]string{"type": "string", "description": "调整原因（用于日志记录）"},
			}, []string{"max_iterations"}),
		// --- 合并工具：定时任务 (create/list/run/pause/resume/delete/update) ---
		toolDefFromCore("manage_schedule", "定时任务管理（所有本地 IM 通道可用）。仅当用户明确提出定时、计划或提醒需求时才能 action=create；普通文档转换和一次性工作绝不能创建定时任务。action: create/list/run/pause/resume/delete/update/list_targets。run 会立即在后台执行指定任务；pause/resume 暂停或恢复任务。list_targets 的 channel：lansenger（群/人）、weixin/telegram/qq（self=最近会话）。create/update 配 delivery 推送；蓝信 group_name 可解析为 group_id。fail_on_error 默认 false（投递失败只警告）。即时发消息请用 im_message。",
			map[string]interface{}{
				"action":           map[string]string{"type": "string", "description": "create/list/run/pause/resume/delete/update/list_targets（execute/trigger/stop/enable/list_groups 等别名也可）"},
				"mention_user_ids": map[string]string{"type": "string", "description": "群推送时可选 @ 的用户 ID，逗号分隔"},
				"mention_all":      map[string]string{"type": "boolean", "description": "群推送时是否 @所有人"},
			}),
		// --- Immediate IM text push (independent of schedule) ---
		toolDefFromCore("im_message", "即时向 IM 发文本或文件（蓝信群/人、微信/Telegram/QQ）。action: list_targets|send|send_file（可省略：有 text 则 send，有 path 则 send_file）。用户要求「现在发到蓝信某群/微信」时用本工具；周期播报才用 manage_schedule+delivery。send_file 上传本机文件（目前仅蓝信 lansenger 支持），可同时带 text 作为说明文字。", nil),
	}

	// ---------- MIS structured data and AgentView transaction workspace ----------
	defs = append(defs,
		toolDef("mis_data", "Structured MIS data tool for semantic business intents, business actions, and local AgentView transaction workspace.",
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
			}, []string{"action"}),
	)

	// ---------- Web search & fetch tools ----------
	defs = append(defs,
		toolDefFromCore("web_search", "搜索互联网内容，查询天气、新闻、汇率、股价等实时信息。返回搜索结果列表（标题、URL、摘要）。适用于查找资料、技术文档、最新信息等。", nil),
		toolDefFromCore("web_fetch", "抓取指定 URL 的网页内容并提取正文文本。支持 HTTP/HTTPS/FTP 协议，自动编码检测（GBK/UTF-8 等）、HTML 正文提取。可选 JS 渲染（需本机安装 Chrome）。也可用 save_path 下载文件到本地。长页面支持续读：当返回 has_more=true 时，请使用 offset=next_offset 继续读取后续内容。", nil),
	)

	return filterDisabledExternalCodingSessionToolDefs(defs)
}

func toolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	return agent.ToolDef(name, desc, props, required)
}

func overlayCoreToolSchema(name string, extraProps map[string]interface{}) (map[string]interface{}, []string) {
	props, required, ok := agent.OverlayCoreToolSchema(name, extraProps)
	if !ok {
		return extraProps, nil
	}
	return props, required
}

func toolDefFromCore(name, desc string, extraProps map[string]interface{}) map[string]interface{} {
	props, required := overlayCoreToolSchema(name, extraProps)
	if strings.TrimSpace(desc) == "" {
		if entry, ok := agent.LookupCoreTool(name); ok {
			desc = entry.Description
		}
	}
	return toolDef(name, desc, props, required)
}
