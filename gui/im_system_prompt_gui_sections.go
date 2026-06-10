package main

// im_system_prompt_gui_sections.go contains GUI-only system prompt sections
// that are injected via the hook mechanism in corelib/agent.BuildSystemPrompt.
// These sections are NOT shared with TUI — they depend on GUI-specific features
// (knowledge base, coding sessions, MIS, group discussion, etc.).

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// appendGUIPostCorePrinciples injects GUI-only rules after core principles:
// context management, coding workflow contract, passthrough commands.
// When suppressCodingContract is true (V2 workflow agent loop), the multi-phase
// coding workflow contract is omitted to prevent the LLM from self-confirming
// and re-emitting documents within a single response.
func (h *IMMessageHandler) appendGUIPostCorePrinciples(b *strings.Builder, isProMode bool, trialReflectEnabled bool, suppressCodingContract bool) {
	b.WriteString(`
## 上下文管理（长程任务优化）
- 当你完成一个子任务或阶段性工作后（如完成了文件创建、完成了一轮测试、完成了数据收集），主动调用 compress_context 工具压缩之前的详细工具调用历史为一段摘要。
- 摘要应包含：已完成的工作、创建/修改的文件列表、关键决策和结论、下一步计划。
- 这能释放 context 空间，让后续推理更高效。建议在以下时机调用：
  - 完成一个独立子任务后（如"文件结构已创建完毕"）
  - 连续执行了 10+ 轮工具调用后
  - 切换到不同类型的工作时（如从代码编写切换到测试）
- 不要在每次工具调用后都压缩——只在关键检查点使用。
`)

	if !suppressCodingContract {
		appendCodingWorkflowContract(b)
	}

	b.WriteString(agent.PromptPassthroughCommands)

	if !isProMode {
		b.WriteString(`
## 当前模式
你当前运行在简洁模式，编程会话工具不可用（未配置编程 LLM provider）。
如果用户请求编程任务（写代码、修 bug、重构等），请友好提示：
"当前为简洁模式，编程会话功能未启用。如需使用编程工具，请在设置中切换到专业模式并配置编程 provider。"

你仍然可以使用 bash、read_file、write_file、edit_file、list_directory、craft_tool、web_search、memory、screenshot、send_file、open 等工具帮助用户。
`)
	}

	b.WriteString(`

## Local Coding Tools Boundary
- External programming session tools/providers may be unavailable in simplified mode.
- Local tools such as bash, write_file, edit_file, read_file, list_directory, craft_tool, and send_file remain available when they are present in the current tool list.
- During a workflow implementation phase or any direct coding task, use those local tools to create directories, write files, edit files, build, and test.
- Do not tell the user that bash/write_file/edit_file are unavailable merely because simplified mode is active.
- If a tool is not in the current tool list, choose another available local path or ask for a mode/provider change only for external coding sessions.
`)

	if trialReflectEnabled {
		b.WriteString(`
## 试错并反思模式
- 先提出当前最有可能成立的假设，再决定下一步动作。
- 每一轮只做一个有区分度的尝试，避免同时改很多变量。
- 执行后必须根据工具结果判断：成功、失败、还是证据不足。
- 如果失败，先总结失败原因，再调整下一轮策略；不要机械重复同样的失败动作。
- 如果成功，简要总结这轮什么做法有效，便于后续延续。
- 如果最近一轮已经证明某种做法无效，下一轮优先换方法、换参数或补充证据。
`)
	}
}

// appendGUIPostSSHRules injects GUI-only content after SSH rules:
// skills with usage stats, skill priority, MCP servers, dynamic tools,
// security firewall, device status, sessions, background tasks, MIS, group discussion.
func (h *IMMessageHandler) appendGUIPostSSHRules(b *strings.Builder, isProMode bool, currentNickname string, cfg corelib.AppConfig) {
	// Skills with usage stats
	if h.getSkillExecutor() != nil {
		skills := h.getSkillExecutor().List()
		if len(skills) > 0 {
			b.WriteString("\n## 已注册 Skill\n")
			b.WriteString("调用方式：manage_skill(action=\"run\", name=\"Skill名称\", args={...})\n")
			b.WriteString("⚠️ **Skill 运行规则**：直接调用 manage_skill(action=\"run\") 即可。禁止事先用 bash 检测 Python/Node 等依赖——Skill Runner 内置依赖预检，缺少依赖时会返回明确的安装指引。只有 Runner 报错后才需要根据错误信息处理。\n")
			for _, s := range skills {
				if normalizeSkillEntryStatus(s.Status) == skillEntryStatusActive {
					b.WriteString(fmt.Sprintf("- %s: %s", s.Name, s.Description))
					if s.UsageCount > 0 {
						b.WriteString(fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, s.SuccessRate*100))
					}
					b.WriteString("\n")
				}
			}
		}
	}

	// Skill priority strategy
	if h.app != nil {
		b.WriteString(`
## Skill 优先策略（重要）
当你需要完成一个现有内置工具无法直接处理的任务时，按以下优先级尝试：
1. **本地已安装 Skill**：先检查上面「已注册 Skill」列表，看是否有匹配的 Skill 可以直接用 manage_skill(action="run", name="skill名称") 执行。如果下方有该 Skill 的使用文档，先阅读文档了解工作流程和前置条件再调用 run
2. **搜索并安装 Skill**：只有当前工具列表明确包含 search_and_install_skill，且任务确实需要新能力时，才可调用该工具从 SkillMarket 搜索安装（搜索顺序：SkillMarket → ClawHub 镜像 → GitHub）
3. **craft_tool 自建**：只有在搜索也找不到合适 Skill 时，才用 craft_tool 自己生成脚本

不要跳过第 1、2 步直接 craft_tool——Skill 经过社区验证，质量和安全性更有保障。
`)
	}

	// MCP servers
	if h.getMCPRegistry() != nil {
		servers := h.getMCPRegistry().ListServers()
		if len(servers) > 0 {
			b.WriteString("\n## 已注册 MCP Server\n")
			for _, s := range servers {
				b.WriteString(fmt.Sprintf("- [%s] %s 状态=%s\n", s.ID, s.Name, s.HealthStatus))
			}
		}
	}

	// Dynamic tools info
	if h.registry != nil {
		allTools := h.registry.ListAvailable()
		mcpTools := h.registry.ListByCategory(ToolCategoryMCP)
		nonCodeTools := h.registry.ListByCategory(ToolCategoryNonCode)
		if len(mcpTools) > 0 || len(nonCodeTools) > 0 {
			b.WriteString(fmt.Sprintf("\n## 动态工具（共 %d 个可用）\n", len(allTools)))
			if len(mcpTools) > 0 {
				b.WriteString(fmt.Sprintf("- MCP 工具: %d 个（来自已注册的 MCP Server）\n", len(mcpTools)))
			}
			if len(nonCodeTools) > 0 {
				b.WriteString(fmt.Sprintf("- 非编程工具: %d 个（git_status, git_diff, git_commit, search_files 等）\n", len(nonCodeTools)))
			}
			b.WriteString("- 工具列表根据消息内容动态筛选，可用「使用XX工具」激活特定分组\n")
		}
	}

	// Security firewall
	if h.firewall != nil {
		b.WriteString("\n## 安全防火墙\n")
		b.WriteString("- 所有工具调用经过安全风险评估和策略检查\n")
		b.WriteString("- 高风险操作（删除文件、修改权限、数据库 DROP 等）会按安全级别处理：宽松/开发者记录放行，标准优先确认、无确认通道则记录放行，严格才阻止\n")
		b.WriteString("- Skill 安装会记录安全扫描结果；宽松/开发者/标准默认不因危险关键字直接阻断，严格模式仍会阻止高危安装\n")
		b.WriteString("- 可用 query_audit_log 工具查看安全审计日志\n")
	}

	// MIS Dynamic AgentView + Group Discussion
	b.WriteString(`
## MIS Dynamic AgentView
- When the user asks to submit, edit, continue, validate, approve, query, or store business data such as expenses, reimbursements, purchase requests, leave requests, invoices, customers, contracts, assets, or tickets, prefer the mis_data tool over free-form chat.
- Do not identify business objects by keyword matching. Use semantic business intent handling: call mis_data(action="resolve_intent", query=the user's natural-language request) when the requested business object or action must be inferred.
- If the user wants to continue unfinished business entry or inspect active AgentView business work, call mis_data(action="list_agent_transactions"). This opens the local right-side transaction workspace and does not require the MIS service to be online.
- The right-side AgentView must show directly operable UI such as forms, approvals, progress, or result browsers. Never show schema/source code to the user as the primary UI.
- Standard skills remain immutable. If a skill or tool needs complex input, let the runtime generate an adaptive AgentView form and return validated structured data instead of modifying the skill itself.

## MaClaw Group Discussion
- When group discussion is enabled, you may use group_discussion(action="status") to inspect current-Hub experts, active discussions, and pending invites.
- Group discussion is current Hub only. Never route it through HubCenter, public networks, or cross-Hub discovery.
- Use group discussion only when it materially helps a complex/stuck task, for example architecture tradeoffs, hard debugging, security review, or needing another MaClaw model's experience.
- Before starting a discussion, call group_discussion(action="suggest", topic=..., question=..., context_summary=...) if useful, then ask the human for explicit permission in plain text and stop. Do not call start_authorized in the same turn as the permission question.
- Only call group_discussion(action="start_authorized") after the human has clearly approved, unless local settings explicitly allow same-security-group free discussion and the context is low/medium risk.
- Share the minimum necessary context. Prefer summaries over raw logs, secrets, private files, credentials, personal data, or large source dumps.
- If you receive or auto-accept an invite, use group_discussion(action="process_invites") and contribute concise expertise with send_message or submit_result when you have enough information.
- Use group_discussion(action="readiness") or group_discussion(action="get_detail") to check whether enough expert answers have arrived; use group_discussion(action="summarize_result") to synthesize and optionally submit/inject the result before answering the human.
- Use group_discussion(action="cleanup_stale", dry_run=true) to inspect stale open discussions; cancel stale discussions only when local policy/user intent makes cleanup safe.
- When a useful discussion result is available, incorporate it into your answer as supporting input, not as unquestioned truth.
`)

	// Device status
	b.WriteString("## 当前设备状态\n")
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "MaClaw Desktop"
	}
	b.WriteString(fmt.Sprintf("- 设备名: %s\n", hostname))
	b.WriteString(fmt.Sprintf("- 平台: %s\n", normalizedRemotePlatform()))
	b.WriteString(fmt.Sprintf("- App 版本: %s\n", remoteAppVersion()))
	now := time.Now()
	b.WriteString(fmt.Sprintf("- 当前时间: %s（%s）\n", now.Format("2006-01-02 15:04"), now.Weekday()))
	if currentNickname != "" {
		b.WriteString(fmt.Sprintf("- 当前昵称: %s\n", currentNickname))
	} else {
		b.WriteString("- 当前昵称: （未设置）\n")
	}

	// Background tasks (pro mode)
	if isProMode && h.bgManager != nil {
		bgLoops := h.bgManager.List()
		if len(bgLoops) > 0 {
			b.WriteString("\n## 后台任务\n")
			for _, lctx := range bgLoops {
				b.WriteString(fmt.Sprintf("- [%s] 类型=%s 状态=%s 轮次=%d/%d",
					lctx.ID, lctx.SlotKind.String(), lctx.State(),
					lctx.Iteration(), lctx.MaxIterations()))
				if lctx.Description != "" {
					b.WriteString(fmt.Sprintf(" 描述=%s", lctx.Description))
				}
				b.WriteString("\n")
			}
			b.WriteString("⚠️ 有后台任务正在运行时，如果用户提出新的编程需求，先记录需求，等后台任务完成后再处理。\n")
		}
	}

	// Advanced capabilities (pro mode)
	if isProMode {
		b.WriteString("\n## 高级能力\n")
		b.WriteString("- orchestrate_task: 将复杂任务拆分为多个子任务按队列逐个执行；编程执行走内部 CodingSubAgent\n")
		b.WriteString("- add_context_note: 记录项目上下文备注，跨会话共享\n")
	}

	// Conversation management
	b.WriteString("\n## 对话管理\n")
	if isProMode {
		b.WriteString("- /new /reset /clear 重置对话 | /compress 压缩历史 | /memory 查看记忆状态 | /cancel /取消 取消任务 | /btw 侧查询\n")
		b.WriteString("- /sessions /status 查看状态 | /exit /quit 终止所有会话 | /help 帮助\n")
		b.WriteString("- 用户表达退出意图时，提醒发送 /exit\n")
	} else {
		b.WriteString("- /new /reset /clear 重置对话 | /cancel /取消 取消任务 | /compress 压缩历史 | /memory 查看记忆状态 | /btw 侧查询 | /help 帮助\n")
	}
	b.WriteString("\n请用中文回复，关键技术术语保留英文。回复要简洁实用。")
}

// appendGUIPostCodingWorkflow injects the full 9-step coding workflow (pro mode).
// This is the detailed GUI version with session management, PDF generation, etc.
func (h *IMMessageHandler) appendGUIPostCodingWorkflow(b *strings.Builder, cfg corelib.AppConfig) {
	if h == nil || h.manager == nil {
		return
	}

	// External coding sessions are legacy-only. Do not expose provider creation
	// guidance to the agent; only summarize existing sessions so the agent can
	// avoid disrupting active work.
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("\n## Legacy Coding Sessions (%d active)\n", len(sessions)))
	b.WriteString("- External coding sessions are disabled for new agent work; route coding tasks through internal CodingSubAgent.\n")
	for _, s := range sessions {
		s.mu.RLock()
		status := s.Status
		task := s.Summary.CurrentTask
		lastResult := s.Summary.LastResult
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("- [%s] tool=%s title=%s status=%s", s.ID, s.Tool, s.Title, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" current_task=%s", task))
		}
		if lastResult != "" {
			b.WriteString(fmt.Sprintf(" last_result=%s", lastResult))
		}
		b.WriteString("\n")
	}
}

// appendGUIEpilogue injects final GUI-only sections:
// steering (handled by deps), memory, knowledge auto-recall, knowledge skills,
// skill repairs, bundle context.
func (h *IMMessageHandler) appendGUIEpilogue(b *strings.Builder, includeMemoryGuide bool, msg string, eventContext lifecycle.EventContext, userID string) {
	epilogueStart := time.Now()
	userID = strings.TrimSpace(userID)

	// OpenHuman-inspired: inject situation report (active tasks, SSH sessions, etc.)
	if report := h.buildSituationReport(userID); report != "" {
		b.WriteString("\n\n")
		b.WriteString(report)
		b.WriteString("\n")
	}

	// Memory section (frozen snapshot + proactive recall)
	if userID != "" {
		h.appendMemorySection(b, includeMemoryGuide, userID, eventContext, msg)
	}
	memoryElapsed := time.Since(epilogueStart)

	// Knowledge base auto-recall
	knowledgeStart := time.Now()
	h.appendKnowledgeAutoRecall(b, msg)
	knowledgeElapsed := time.Since(knowledgeStart)

	// Knowledge skill section
	h.appendKnowledgeSkillSection(b, msg)

	// Skill repair notifications
	h.appendSkillRepairNotifications(b)

	// Bundle context banner
	h.appendBundleContextBanner(b)

	totalElapsed := time.Since(epilogueStart)
	if totalElapsed > 200*time.Millisecond {
		log.Printf("[appendGUIEpilogue] slow: memory=%v knowledge=%v total=%v", memoryElapsed, knowledgeElapsed, totalElapsed)
	}
	log.Printf("[perf] stage=gui_epilogue user=%q elapsed=%s memory=%s knowledge=%s prompt_len=%d msg_len=%d", userID, totalElapsed.Round(time.Millisecond), memoryElapsed.Round(time.Millisecond), knowledgeElapsed.Round(time.Millisecond), b.Len(), len([]rune(msg)))
}
