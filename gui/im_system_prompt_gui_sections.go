package main

// im_system_prompt_gui_sections.go contains GUI-only system prompt sections
// that are injected via the hook mechanism in corelib/agent.BuildSystemPrompt.
// These sections are NOT shared with TUI — they depend on GUI-specific features
// (knowledge base, coding sessions, MIS, group discussion, etc.).

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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
// platform is the originating conversation platform (desktop / weixin / feishu / …).
// managedSemantic skips soup-name routing: host-bound send_file/send_to_im
// grants reject path/channel fields the legacy sections still teach.
func (h *IMMessageHandler) appendGUIPostCorePrinciples(b *strings.Builder, isProMode bool, trialReflectEnabled bool, suppressCodingContract bool, platform string, managedSemantic bool) {
	if !managedSemantic {
		b.WriteString(`
## 上下文管理（长程任务优化）
- 当 context 较大（连续执行了 10+ 轮工具调用）或切换工作方向时，调用 compress_context 压缩之前的详细工具调用历史为一段摘要。
- 调用 compress_context 后你可以继续工作——它不会中断对话，只是释放空间。
- 摘要应包含：已完成的工作、创建/修改的文件列表、关键决策和结论、下一步计划。
- 建议调用时机：
  - 连续执行了 10+ 轮工具调用后（释放空间给后续工作）
  - 完成一个独立子任务准备开始下一个时
  - 切换到不同类型的工作时（如从代码编写切换到测试）
- 不要在每次工具调用后都压缩——只在关键检查点使用。
- 重要：compress_context 和其他工具调用一样，执行后对话继续。如果你还有未完成的工作，压缩后直接继续执行。
`)

		if !suppressCodingContract {
			appendCodingWorkflowContract(b)
		}
	}

	b.WriteString(agent.PromptPassthroughCommands)

	if !managedSemantic && !isProMode {
		b.WriteString(`
## 当前模式
你当前运行在简洁模式，编程会话工具不可用（未配置编程 LLM provider）。
如果用户请求编程任务（写代码、修 bug、重构等），请友好提示：
"当前为简洁模式，编程会话功能未启用。如需使用编程工具，请在设置中切换到专业模式并配置编程 provider。"

你仍然可以使用 bash、read_file、write_file、edit_file、list_directory、craft_tool、web_search、memory、screenshot、send_file、open 等工具帮助用户。
`)
	}

	if !managedSemantic {
		b.WriteString(`

## Local Coding Tools Boundary
- External programming session tools/providers may be unavailable in simplified mode.
- Local tools such as bash, write_file, edit_file, read_file, list_directory, craft_tool, send_file, and send_to_im remain available when they are present in the current tool list.
- During workflow-driven coding execution, use those local tools to create directories, write files, edit files, build, and test.
- Do not tell the user that bash/write_file/edit_file are unavailable merely because simplified mode is active.
- If a tool is not in the current tool list, choose another available local path or ask for a mode/provider change only for external coding sessions.
`)
		appendFileDeliveryChannelRules(b, platform)
	}

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

// appendManagedSemanticSurfaceRules replaces the name-routing guidance with a
// description of the surface a capability-managed turn actually has.
//
// Names are the ordinary host spellings (web_search, generate_pdf). A model
// still has to be told that the list is exhaustive, why a tool disappears
// mid-turn, and what to do when the next step has not unlocked yet.
func appendManagedSemanticSurfaceRules(b *strings.Builder) {
	b.WriteString(`
## 本回合的工具面
- 本回合的工具是按你要做的事预先授权的。**工具列表就是全部可用工具**（仅限此刻列表里出现的名字），名称就是普通工具名（如 web_search、generate_pdf、send_file）。
- 工具列表之外还有**可请愿能力**（如 web_search、web_fetch、download_file、bash、office 等）：它们此刻不在列表里，但都是真实工具。需要时先用 tools_search 查确切名字和状态——标记「可请愿」的名字，把它当普通工具**单独**调用一次（download_file 用 url+save_path，bash 用 command），这表示向主机请求授权：获批后名字会加入列表，按提示重新发起即真正执行；标记「已列入本轮计划/已用尽/不可用」的名字调用必被拒绝。工具结果里点名建议的工具（如反爬提示里的 download_file）按此办理：那是主机指的路，不是缺工具。请愿每轮每类限一次，留给真正需要的能力。
- 不要凭记忆调用 manage_skill、call_mcp_tool、discover_tool、craft_tool、search_and_install_skill、previous_turn_tool，也不要复用历史里的 invoke_* 名称：它们在本回合不存在，调用只会被拒绝，并且浪费一轮。
- 多步任务会按顺序解锁：当前列表可能只有查询；查询成功后，文档生成或投递会出现在**同一次回复**的下一次请求列表里。那不是「没有 PDF 工具」，不要向用户宣布缺工具，也不要用 python、bash、write_file 绕过。工具一旦出现在当前列表（例如 generate_pdf），必须立刻调用；禁止说「请稍候」然后结束，也不要等用户再发一条消息。
- 同一个工具可能有调用次数上限。用完之后它会从列表里消失——这是预算用尽，不是故障，也不代表这件事做不了。下一步授权出现时，再按新列表里的同名工具继续。
- 参数以当前列表的 schema 为准。投递类工具的目的地已由宿主绑定：不要传 path、channel、group_id，也不要套用旧的 send_to_im(path=...) 签名。
- 只有当前列表为空、且下一步也没有新授权出现时，才说明缺什么并停下来。不要声称已经做了。
`)
}

// appendFileDeliveryChannelRules tells the model how send_file / send_to_im map
// to the current conversation platform. On WeChat/Feishu channels, the current
// chat IS the destination — do not claim the sender is unconfigured.
func appendFileDeliveryChannelRules(b *strings.Builder, platform string) {
	if b == nil {
		return
	}
	kind := normalizeIMMessagePlatformKind(platform)
	if kind.IsIMChannel() {
		b.WriteString(`
## 文件发送（当前为 IM 通道）
- 当前对话就是微信/飞书/QQ 通道本身。
- 用户说「发微信」时直接 send_file；工具结果写「已发到当前 IM 通道」即成功。
- 若用户明确说“另一个群/指定群/指定用户”，不要发回当前对话：先用 im_message(action="list_targets", channel=..., query=群名) 消歧，再调用 send_to_im(path=..., channel=..., group_id=... 或 user_id=...)。
- 指定目标必须使用结构化 channel/group_id/group_name/user_id；禁止把群名拼进文件名、caption 或自由文本来猜路由。
- 禁止说「发送器未配置 / 需要客户端登录 / 无法发到微信」；不要让用户手动转发，也不要对同一文件再调 send_to_im。
`)
		return
	}
	b.WriteString(`
## 文件发到微信/飞书（桌面端）
- send_file：默认只在当前桌面对话展示，不会到微信。
- send_to_im：专用「发到 IM」工具，调用即转发到微信/飞书等。用户说「发到微信」「放到飞书」时必须用 send_to_im，不要用 send_file。
- 发送到指定群/用户时，先用 im_message(action="list_targets", channel=..., query=群名) 取得唯一 ID，再调用 send_to_im(path=..., channel=..., group_id=... 或 user_id=...)。如果用户已给出明确 ID，可直接发送。
- 目标有歧义或不存在时应向用户说明并列出候选；不得广播、不得悄悄改发最近会话。
- 不要只在回复文字里写「已发到微信」却未调用 send_to_im；若工具结果提示未转发，立刻用 send_to_im 重试。
`)
}

// appendGUIPostSSHRules injects GUI-only content after SSH rules:
// skills with usage stats, skill priority, MCP servers, dynamic tools,
// security firewall, device status, sessions, background tasks, MIS, group discussion.
//
// managedSemantic marks a capability-managed turn, where the model's tools are
// the planned host names rather than the full registry. Gateway sections that
// say "call manage_skill / discover_tool" are still refused. Sections that
// state facts (device, working directory, background tasks, slash commands)
// still apply and are kept; sections that say "call X" for a gateway are
// replaced by one block describing the surface the model actually has.
func (h *IMMessageHandler) appendGUIPostSSHRules(b *strings.Builder, isProMode bool, currentNickname string, cfg corelib.AppConfig, ownerID string, managedSemantic bool) {
	if managedSemantic {
		appendManagedSemanticSurfaceRules(b)
	}
	// Skills with usage stats
	if !managedSemantic && h.getSkillExecutor() != nil {
		skills := h.getSkillExecutor().List()
		// Expert session: only advertise the expert's bound skills (empty = all).
		skills = h.filterSkillsForExpertUser(ownerID, skills)
		// Self-learned skills stay in the pool they were learned from, so a
		// coding workbench and a general chat do not advertise each other's
		// distilled requests as capabilities.
		skills = filterSkillsForExperienceDomain(h.skillExperienceDomainForOwner(ownerID), skills)
		writeRegisteredSkillsSection(b, skills)
	}

	// Skill priority strategy
	if h.app != nil {
		if !managedSemantic {
			b.WriteString(`
## Skill 优先策略（重要）
当你需要完成一个现有内置工具无法直接处理的任务时，按以下优先级尝试：
1. **内置下载工具（HTTP/PDF）**：通用「下载 URL / 保存 PDF」优先用 download_file（或 web_fetch + save_path）。禁止为简单下载去 Hub 安装 wget/curl/Paper Fetch。
   - download_file 内置反爬升级链与自动重试：被 Cloudflare/403 拦截时会自动逐级升级（浏览器请求头 → 模拟 Chrome TLS 指纹 → 复用浏览器会话 cookie）。若返回"存在反爬验证"的错误，标准解法是：先用 browser 工具打开目标网页完成人机验证，然后重试 download_file（可加 use_browser_cookies=true 直接复用会话）；仍失败则用 download_file(via_browser=true) 让浏览器亲自下载。不要写 Python/CDP 脚本绕道抓 cookie。
   - 下载过程日志在 ~/.maclaw/logs/download.log，排障时可读取。
2. **本地已安装 Skill**：检查上面「已注册 Skill」列表。Agent 工作流 Skill 按文末「Skill 使用文档」用宿主工具执行，不要 discover_tool 搜索同名工具、不要 generate_pdf 替代、不要 manage_skill。领域流水线（如 paper_pdf_translator 论文翻译）用 manage_skill(action="run", name="...")。若列表未出现某 skill，不要假设可用。
3. **搜索并安装 Skill**：只有当前工具列表明确包含 search_and_install_skill，且任务确实需要新能力（非简单下载）时，才可从 SkillMarket/ClawHub/GitHub 搜索安装
4. **craft_tool 自建**：只有在搜索也找不到合适 Skill 时，才用 craft_tool 自己生成脚本

不要跳过内置 download_file 去装社区下载 skill；不要跳过本地可用 Skill 直接 craft_tool。
`)
		}
		// Working directory + download landing zone (owner-aware; matches tool cwd).
		wd := ""
		if h != nil {
			if dir := strings.TrimSpace(h.resolveToolWorkDirForOwner("", ownerID)); dir != "" {
				wd = dir
			}
		}
		if wd == "" {
			if dir := strings.TrimSpace(corelib.EffectiveWorkspaceDir()); dir != "" {
				wd = dir
			}
		}
		if wd != "" {
			skillTemp := filepath.Join(wd, ".maclaw-tmp")
			b.WriteString("\n## 工作目录与下载落点\n")
			b.WriteString(fmt.Sprintf("- 当前有效工作目录：`%s`\n", wd))
			b.WriteString(fmt.Sprintf("- 临时/scratch 目录：`%s`（不是系统 Temp）\n", skillTemp))
			if !managedSemantic {
				b.WriteString("- download_file / web_fetch(save_path=...) 的相对路径相对工作目录。\n")
				b.WriteString("- Skill 运行时 TEMP/TMP 会重定向到上述 `.maclaw-tmp`（勿再到 %TEMP%\\maclaw-arxiv 等系统路径找文件）。\n")
			} else {
				b.WriteString("- 工具参数里的相对路径相对上述工作目录。\n")
			}
			b.WriteString("- 打开路径时使用绝对路径；不要使用未展开的 `~\\...` 字面路径。\n")
		}
	}

	// MCP servers
	if !managedSemantic && h.getMCPRegistry() != nil {
		servers := h.getMCPRegistry().ListServers()
		if len(servers) > 0 {
			b.WriteString("\n## 已注册 MCP Server\n")
			for _, s := range servers {
				b.WriteString(fmt.Sprintf("- [%s] %s 状态=%s\n", s.ID, s.Name, s.HealthStatus))
			}
		}
	}

	// Dynamic tools info
	if !managedSemantic && h.registry != nil {
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
	if !managedSemantic && h.firewall != nil {
		b.WriteString("\n## 安全防火墙\n")
		b.WriteString("- 所有工具调用经过安全风险评估和策略检查\n")
		b.WriteString("- 高风险操作（删除文件、修改权限、数据库 DROP 等）会按安全级别处理：宽松/开发者记录放行，标准优先确认、无确认通道则记录放行，严格才阻止\n")
		b.WriteString("- Skill 安装会记录安全扫描结果；宽松/开发者/标准默认不因危险关键字直接阻断，严格模式仍会阻止高危安装\n")
		b.WriteString("- 可用 query_audit_log 工具查看安全审计日志\n")
	}

	// MIS Dynamic AgentView + Group Discussion
	if !managedSemantic {
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
	}

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
			b.WriteString("有后台任务正在运行时，如果用户提出新的编程需求，先记录需求，等后台任务完成后再处理。\n")
		}
	}

	// Advanced capabilities (pro mode)
	if isProMode && !managedSemantic {
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

func writeRegisteredSkillsSection(b *strings.Builder, skills []NLSkillDefinition) {
	if b == nil || len(skills) == 0 {
		return
	}
	var guided, runnable []NLSkillDefinition
	for _, s := range skills {
		st := normalizeSkillEntryStatus(s.Status)
		if st != skillEntryStatusActive && st != skillEntryStatusUnknown {
			continue
		}
		if strings.TrimSpace(s.Name) == "" {
			continue
		}
		if strings.TrimSpace(s.Description) == "" && len(s.Triggers) == 0 && len(s.Steps) == 0 {
			continue
		}
		if nlSkillIsAgentGuided(s) {
			guided = append(guided, s)
			continue
		}
		runnable = append(runnable, s)
	}
	if len(guided) == 0 && len(runnable) == 0 {
		return
	}
	b.WriteString("\n## 已注册 Skill\n")
	if len(runnable) > 0 {
		b.WriteString("调用方式：manage_skill(action=\"run\", name=\"Skill名称\", args={...})\n")
		b.WriteString("**Skill 运行规则**：直接调用 manage_skill(action=\"run\") 即可。禁止事先用 bash 检测 Python/Node 等依赖——Skill Runner 内置依赖预检，缺少依赖时会返回明确的安装指引。只有 Runner 报错后才需要根据错误信息处理。\n")
		for _, s := range runnable {
			writeRegisteredSkillLine(b, s)
		}
	}
	if len(guided) > 0 {
		b.WriteString("\n### Agent 工作流 Skill（已安装）\n")
		b.WriteString("这些 skill 不是工具名。用户点名时，按文末「Skill 使用文档」用 bash/read_file/write_file/edit_file 执行；禁止 discover_tool 搜索同名工具，禁止 generate_pdf 替代，禁止 manage_skill。\n")
		for _, s := range guided {
			writeRegisteredSkillLine(b, s)
		}
	}
}

func writeRegisteredSkillLine(b *strings.Builder, s NLSkillDefinition) {
	b.WriteString(formatRegisteredSkillLine(s.Name, s.Description))
	if s.UsageCount > 0 {
		b.WriteString(fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, s.SuccessRate*100))
	}
	b.WriteString("\n")
}

func formatRegisteredSkillLine(name, description string) string {
	name = strings.TrimSpace(name)
	desc := strings.TrimSpace(strings.ReplaceAll(description, "\n", " "))
	if strings.ContainsAny(name, ":：") {
		return fmt.Sprintf("- 「%s」: %s", name, desc)
	}
	return fmt.Sprintf("- %s: %s", name, desc)
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
// steering (handled by deps), memory catalog, knowledge skills,
// skill repairs, bundle context.
func (h *IMMessageHandler) appendGUIEpilogue(b *strings.Builder, includeMemoryGuide bool, msg string, eventContext lifecycle.EventContext, userID string, history []agent.ConversationEntry, loopCtx *LoopContext) {
	epilogueStart := time.Now()
	userID = strings.TrimSpace(userID)

	// Computer Use playbook for text-only models (OmniParser local vision).
	cuGateText := msg
	if loopCtx != nil && strings.TrimSpace(loopCtx.ComputerUseRoutingText) != "" {
		cuGateText = loopCtx.ComputerUseRoutingText
	}
	cuActive := false
	cuOwner := ""
	if loopCtx != nil {
		// Owner fallback is LoopContext.UserID, never promptRuntimeUserID / PolicyOwnerID.
		syncComputerUseTurn(h, loopCtx, loopCtx.UserID, msg)
		cuActive = loopCtx.ComputerUseActive
		cuOwner = loopCtx.ComputerUseOwner
	} else {
		cuActive, _ = recordComputerUseGate(h, nil, cuGateText)
	}
	if section := computerUsePlaybookSectionFor(cuActive, cuOwner); section != "" {
		b.WriteString(section)
	}
	if section := browserPlaybookSection(h.shouldActivateBrowser(agent.CompactQueryForEmbedding(semanticUserIntentText(msg)))); section != "" {
		b.WriteString(section)
	}

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

	// Knowledge snippets stay in the warehouse. A new task does not receive
	// auto-recalled cards; the model pulls them through retrieval tools.
	// Group ACL still governs who may call those tools.
	knowledgeStart := time.Now()
	knowledgeElapsed := time.Duration(0)
	if loopCtx != nil && loopCtx.LansengerGroupPermissions != nil && loopCtx.LansengerGroupPermissions.allowsKnowledge() {
		b.WriteString(lansengerGroupKnowledgePriorityPrompt())
	}
	knowledgeElapsed = time.Since(knowledgeStart)

	// Skill docs and repair hints teach manage_skill. ConsumeRepairNotifications
	// is one-shot, so skip the whole family on managed turns and leave the
	// notification for the next unmanaged turn.
	if !loopContextIsSemanticManaged(loopCtx) {
		h.appendKnowledgeSkillSection(b, msg, agentGuidedSkillOwnerID(loopCtx, userID))
		h.appendSkillRepairNotifications(b)
		h.appendMaintenanceExperienceHints(b)
		h.appendBundleContextBanner(b)
	}

	totalElapsed := time.Since(epilogueStart)
	if totalElapsed > 200*time.Millisecond {
		log.Printf("[appendGUIEpilogue] slow: memory=%v knowledge=%v total=%v", memoryElapsed, knowledgeElapsed, totalElapsed)
	}
	log.Printf("[perf] stage=gui_epilogue user=%q elapsed=%s memory=%s knowledge=%s prompt_len=%d msg_len=%d", userID, totalElapsed.Round(time.Millisecond), memoryElapsed.Round(time.Millisecond), knowledgeElapsed.Round(time.Millisecond), b.Len(), len([]rune(msg)))
}
