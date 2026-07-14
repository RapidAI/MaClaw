package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Pure-coding slash commands (Codex-style workbench helpers).
// Handled early so they do not enter the full SubAgent loop.

func isCodingWorkbenchSlash(trimmed string) bool {
	lower := strings.ToLower(strings.TrimSpace(trimmed))
	if lower == "" {
		return false
	}
	prefixes := []string{
		"/plan", "/review", "/test", "/commit", "/pr", "/map",
		"/checkpoint", "/agents", "/coding-help", "/cost", "/bg", "/worktree", "/route", "/hooks",
	}
	for _, p := range prefixes {
		if lower == p || strings.HasPrefix(lower, p+" ") {
			return true
		}
	}
	return false
}

// handleCodingWorkbenchIMCommand is the IM entry for pure-coding slash commands.
// /plan approve executes the pending multi-step plan via the coding runner.
func (h *IMMessageHandler) handleCodingWorkbenchIMCommand(
	msg IMUserMessage,
	trimmed string,
	onProgress func(string),
	onToken func(string),
) *IMAgentResponse {
	userID := strings.TrimSpace(msg.UserID)
	mem := stickyCodingWorkbenchMemory{}
	if h != nil {
		mem = h.getStickyCodingWorkbenchMemory(userID)
	}
	projectPath := strings.TrimSpace(mem.ProjectPath)
	if projectPath == "" {
		projectPath = projectPathFromSessionOwnerID(userID)
	}

	lower := strings.ToLower(strings.TrimSpace(trimmed))
	// Approve: run pending plan through local or remote coding template.
	if lower == "/plan approve" || lower == "/plan run" || lower == "/plan go" {
		return h.executeApprovedCodingPlan(userID, projectPath, mem, msg, onProgress, onToken)
	}
	// Skip: single-step execute original pending user text.
	if lower == "/plan skip" {
		return h.executeSkippedCodingPlan(userID, projectPath, mem, msg, onProgress, onToken)
	}
	resp := h.handleCodingWorkbenchSlash(userID, projectPath, trimmed)
	if resp == nil {
		return &IMAgentResponse{Text: codingWorkbenchSlashHelpText()}
	}
	return resp
}

// handleCodingWorkbenchSlash dispatches pure-coding slash commands.
// projectPath is the execution workspace (not necessarily the task folder).
func (h *IMMessageHandler) handleCodingWorkbenchSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	trimmed = strings.TrimSpace(trimmed)
	lower := strings.ToLower(trimmed)
	userID = strings.TrimSpace(userID)
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		// Fall back to sticky project path.
		if h != nil {
			mem := h.getStickyCodingWorkbenchMemory(userID)
			projectPath = strings.TrimSpace(mem.ProjectPath)
		}
	}

	switch {
	case lower == "/coding-help" || lower == "/plan help":
		return &IMAgentResponse{Text: codingWorkbenchSlashHelpText()}

	case lower == "/plan" || strings.HasPrefix(lower, "/plan "):
		return h.handleCodingPlanSlash(userID, projectPath, trimmed)

	case lower == "/review" || strings.HasPrefix(lower, "/review "):
		return h.handleCodingReviewSlash(userID, projectPath, trimmed)

	case lower == "/test" || strings.HasPrefix(lower, "/test "):
		return h.handleCodingTestSlash(userID, projectPath, trimmed)

	case lower == "/commit" || strings.HasPrefix(lower, "/commit "):
		return h.handleCodingCommitSlash(userID, projectPath, trimmed)

	case lower == "/pr" || strings.HasPrefix(lower, "/pr "):
		return h.handleCodingPRSlash(userID, projectPath, trimmed)

	case lower == "/map" || strings.HasPrefix(lower, "/map "):
		return h.handleCodingMapSlash(userID, projectPath, trimmed)

	case lower == "/checkpoint" || strings.HasPrefix(lower, "/checkpoint "):
		return h.handleCodingCheckpointSlash(userID, projectPath, trimmed)

	case lower == "/agents" || strings.HasPrefix(lower, "/agents "):
		return h.handleCodingAgentsSlash(userID, projectPath, trimmed)

	case lower == "/cost" || strings.HasPrefix(lower, "/cost "):
		return h.handleCodingCostSlash(userID, projectPath, trimmed)

	case lower == "/bg" || strings.HasPrefix(lower, "/bg "):
		return h.handleCodingBgSlash(userID, projectPath, trimmed)

	case lower == "/worktree" || strings.HasPrefix(lower, "/worktree "):
		return h.handleCodingWorktreeSlash(userID, projectPath, trimmed)

	case lower == "/route" || strings.HasPrefix(lower, "/route "):
		return h.handleCodingRouteSlash(userID, projectPath, trimmed)

	case lower == "/hooks" || strings.HasPrefix(lower, "/hooks "):
		return h.handleCodingHooksSlash(userID, projectPath, trimmed)
	}
	return nil
}

func (h *IMMessageHandler) executeApprovedCodingPlan(
	userID, projectPath string,
	mem stickyCodingWorkbenchMemory,
	msg IMUserMessage,
	onProgress func(string),
	onToken func(string),
) *IMAgentResponse {
	pending, ok := h.promotePendingToApprovedCodingPlan(userID)
	if !ok {
		return &IMAgentResponse{Text: "当前没有待批准的执行计划。可用 `/plan mode approve` 开启批准模式后再发起复杂任务。"}
	}
	marker := codingPlanApproveExecuteMarker
	mem = h.getStickyCodingWorkbenchMemory(userID)

	loopCtx := NewLoopContext("coding-plan-approve", h.getMaclawAgentMaxIterations(), h.client)
	if loopCtx != nil {
		loopCtx.UserID = userID
		defer func() {
			loopCtx.Cancel()
			loopCtx.Done()
		}()
	}
	if onProgress != nil {
		onProgress(fmt.Sprintf("已批准计划，开始执行 %d 步…", len(pending.Tasks)))
	}
	if strings.EqualFold(mem.Kind, "remote") {
		// Remote: rebuild context from sticky.
		remoteCtx := remoteCodingTemplateContext{
			SessionID:  mem.RemoteSessionID,
			WorkDir:    mem.RemoteWorkDir,
			ProjectDir: mem.RemoteProjectDir,
		}
		if remoteCtx.ProjectDir == "" {
			remoteCtx.ProjectDir = mem.RemoteWorkDir
		}
		if remoteCtx.WorkDir == "" {
			remoteCtx.WorkDir = remoteCtx.ProjectDir
		}
		if remoteCtx.SessionID == "" {
			return &IMAgentResponse{Text: "远程编程无法执行已批准计划：缺少 SSH 会话。请先重连。"}
		}
		return h.runRemoteCodingTemplateSubAgent(userID, marker+" "+pending.UserText, remoteCtx, loopCtx, onProgress, onToken)
	}
	if projectPath == "" {
		projectPath = strings.TrimSpace(pending.UserText) // shouldn't happen
		projectPath = strings.TrimSpace(mem.ProjectPath)
	}
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法执行已批准计划：缺少项目路径。"}
	}
	_ = msg
	return h.runCodingTemplateSubAgent(userID, marker+" "+pending.UserText, projectPath, loopCtx, onProgress, onToken)
}

func (h *IMMessageHandler) executeSkippedCodingPlan(
	userID, projectPath string,
	mem stickyCodingWorkbenchMemory,
	msg IMUserMessage,
	onProgress func(string),
	onToken func(string),
) *IMAgentResponse {
	pending, ok := h.loadStickyPendingCodingPlan(userID)
	orig := ""
	if ok {
		orig = pending.UserText
	} else {
		orig = strings.TrimSpace(mem.PendingPlanUserText)
	}
	h.clearStickyPendingCodingPlan(userID)
	if strings.TrimSpace(orig) == "" {
		// No pending — set skip flag for next free-form message.
		m := h.getStickyCodingWorkbenchMemory(userID)
		m.SkipNextPlan = true
		h.storeStickyCodingWorkbenchMemory(userID, m)
		return &IMAgentResponse{Text: "已设置跳过多步规划。请重新发送任务以单步直接执行。"}
	}
	// Force single-task for this original request.
	m := h.getStickyCodingWorkbenchMemory(userID)
	m.SkipNextPlan = true
	m.ApprovedPlanJSON = ""
	h.storeStickyCodingWorkbenchMemory(userID, m)

	loopCtx := NewLoopContext("coding-plan-skip", h.getMaclawAgentMaxIterations(), h.client)
	if loopCtx != nil {
		loopCtx.UserID = userID
		defer func() {
			loopCtx.Cancel()
			loopCtx.Done()
		}()
	}
	if onProgress != nil {
		onProgress("跳过规划，单步直接执行…")
	}
	if strings.EqualFold(mem.Kind, "remote") {
		remoteCtx := remoteCodingTemplateContext{
			SessionID:  mem.RemoteSessionID,
			WorkDir:    mem.RemoteWorkDir,
			ProjectDir: mem.RemoteProjectDir,
		}
		if remoteCtx.ProjectDir == "" {
			remoteCtx.ProjectDir = mem.RemoteWorkDir
		}
		if remoteCtx.SessionID == "" {
			return &IMAgentResponse{Text: "远程编程无法跳过执行：缺少 SSH 会话。"}
		}
		return h.runRemoteCodingTemplateSubAgent(userID, orig, remoteCtx, loopCtx, onProgress, onToken)
	}
	if projectPath == "" {
		projectPath = strings.TrimSpace(mem.ProjectPath)
	}
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法跳过执行：缺少项目路径。"}
	}
	_ = msg
	return h.runCodingTemplateSubAgent(userID, orig, projectPath, loopCtx, onProgress, onToken)
}

// codingPlanApproveExecuteMarker prefixes userText when executing an approved plan.
const codingPlanApproveExecuteMarker = "__coding_plan_approved__"

func codingWorkbenchSlashHelpText() string {
	return strings.TrimSpace("" +
		"## 编程工作台命令\n\n" +
		"| 命令 | 说明 |\n" +
		"|------|------|\n" +
		"| /plan | 查看当前/待批准执行计划 |\n" +
		"| /plan approve | 批准并执行待批计划 |\n" +
		"| /plan skip | 跳过规划，直接按原请求单步执行 |\n" +
		"| /plan reject | 拒绝并清除待批计划 |\n" +
		"| /plan edit <steps> | 改写待批计划（至少 2 步）后再批准 |\n" +
		"| /plan mode auto|approve|off | 规划模式：自动执行 / 需批准 / 关闭多步规划 |\n" +
		"| /review | Git status + diff 摘要（代码审阅入口） |\n" +
		"| /test | 运行项目验证命令 |\n" +
		"| /commit <msg> | git add -A + git commit（不 push） |\n" +
		"| /pr [title] | 生成 PR 描述；有 gh 时尝试创建 |\n" +
		"| /map [query] | 项目结构 / codegraph 定位提示 |\n" +
		"| /checkpoint [label] | 保存会话检查点（含小文件内容快照） |\n" +
		"| /checkpoint list | 列出当前 + 历史检查点 |\n" +
		"| /checkpoint restore [label] | 恢复检查点的会话目标/计划 |\n" +
		"| /checkpoint restore files [label] | 恢复计划并写回文件快照 |\n" +
		"| /checkpoint prune | 清理非 current/history 的侧车文件 |\n" +
		"| /checkpoint usage | 检查点侧车磁盘用量 |\n" +
		"| /agents | 显示已加载的 AGENTS.md / CLAUDE.md |\n" +
		"| /cost | 本会话 token / 估算费用 |\n" +
		"| /bg test | 后台跑项目验证（不阻塞对话） |\n" +
		"| /bg status | 查看后台验证结果 |\n" +
		"| /worktree | 查看 git worktree 状态 |\n" +
		"| /worktree mode auto|always|off | 写改步骤隔离策略 |\n" +
		"| /worktree conflicts | 列出合并失败的隔离树 |\n" +
		"| /worktree adopt <id> | 强制文件合并冲突 worktree |\n" +
		"| /worktree adopt <id> -- <file>… | 仅采纳指定文件 |\n" +
		"| /worktree keep <id> -- <file>… | 主树保留指定文件（不采纳隔离侧） |\n" +
		"| /worktree base <id> -- <file>… | 写回 merge-base 到主树（三路取 base） |\n" +
		"| /worktree resolve <id> adopt|keep|base [-- file…] | 统一批量解决冲突 |\n" +
		"| /worktree log | 冲突解决审计日志 |\n" +
		"| /worktree log clear | 清空冲突解决日志 |\n" +
		"| /worktree log export | 导出冲突日志到 worktree notes |\n" +
		"| /worktree diff <id> | 逐文件 main vs 隔离树预览 |\n" +
		"| /worktree discard <id> | 丢弃冲突隔离树 |\n" +
		"| /worktree discard all | 丢弃全部冲突隔离树 |\n" +
		"| /worktree prune | 清理 maclaw coding worktree |\n" +
		"| /route | 本会话模型路由 |\n" +
		"| /route pref auto|primary|reasoning|vision | 编程选模偏好 |\n" +
		"| /hooks | 查看 .maclaw/hooks.json 生命周期钩子 |\n" +
		"| /coding-help | 本帮助 |\n\n" +
		"计划批准模式：/plan mode approve 后，复杂任务会先展示步骤，点「批准并执行」或发送 /plan approve 再跑。\n" +
		"Worktree：auto 时并行写改步骤在隔离 worktree 中执行并合并回主树。")
}

func (h *IMMessageHandler) handleCodingPlanSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	body := strings.TrimSpace(trimmed)
	if len(body) >= 5 {
		body = strings.TrimSpace(body[5:]) // strip "/plan"
	} else {
		body = ""
	}
	lower := strings.ToLower(body)

	switch {
	case lower == "" || lower == "show" || lower == "status":
		return h.codingPlanStatusResponse(userID)

	case lower == "approve" || lower == "run" || lower == "go":
		// Normally handled by handleCodingWorkbenchIMCommand; keep as fallback.
		pending, ok := h.loadStickyPendingCodingPlan(userID)
		if !ok {
			return &IMAgentResponse{Text: "当前没有待批准的执行计划。可用 `/plan mode approve` 开启批准模式后再发起复杂任务。"}
		}
		return &IMAgentResponse{
			Text:    formatPendingPlanApprovalText(pending.Markdown, len(pending.Tasks)),
			Actions: codingPlanApproveActions(),
		}

	case lower == "skip":
		// Fallback when not routed through IM execute path.
		pending, ok := h.loadStickyPendingCodingPlan(userID)
		if !ok {
			mem := h.getStickyCodingWorkbenchMemory(userID)
			mem.SkipNextPlan = true
			h.storeStickyCodingWorkbenchMemory(userID, mem)
			return &IMAgentResponse{Text: "已设置跳过多步规划。请重新发送任务以单步直接执行。"}
		}
		mem := h.getStickyCodingWorkbenchMemory(userID)
		mem.SkipNextPlan = true
		mem.PendingPlanUserText = pending.UserText
		h.storeStickyCodingWorkbenchMemory(userID, mem)
		return &IMAgentResponse{
			Text: "已标记跳过规划。请再发 `/plan skip`（工作台内会直接单步执行原请求）或重新发送任务。\n\n原请求：\n" + truncateRunesForSubAgent(pending.UserText, 400),
		}

	case lower == "reject" || lower == "cancel" || lower == "clear":
		h.clearStickyPendingCodingPlan(userID)
		h.clearStickyCodingExecutionPlan(userID)
		h.clearStickyCodingStepStatuses(userID)
		return &IMAgentResponse{Text: "已拒绝并清除待批准执行计划。"}

	case strings.HasPrefix(lower, "edit") || strings.HasPrefix(lower, "set") || strings.HasPrefix(lower, "rewrite"):
		// /plan edit <markdown or numbered steps>
		arg := body
		for _, p := range []string{"edit", "set", "rewrite"} {
			if strings.HasPrefix(lower, p) {
				arg = strings.TrimSpace(body[len(p):])
				break
			}
		}
		if arg == "" {
			pending, ok := h.loadStickyPendingCodingPlan(userID)
			if !ok {
				return &IMAgentResponse{Text: "当前没有待批准计划可编辑。先用 `/plan mode approve` 生成计划。"}
			}
			return &IMAgentResponse{Text: "用法：`/plan edit` 后接改写后的步骤（至少 2 步）。\n\n当前待批计划：\n\n" + pending.Markdown}
		}
		updated, err := h.replaceStickyPendingCodingPlanMarkdown(userID, arg)
		if err != nil {
			return &IMAgentResponse{Text: "无法更新待批计划：" + err.Error()}
		}
		return &IMAgentResponse{
			Text:    fmt.Sprintf("已更新待批准计划（**%d** 步）。确认后发送 `/plan approve` 执行。\n\n", len(updated.Tasks)) + updated.Markdown,
			Actions: codingPlanApproveActions(),
		}

	case strings.HasPrefix(lower, "mode"):
		modeArg := strings.TrimSpace(body[len("mode"):])
		if modeArg == "" {
			cur := h.getStickyCodingPlanMode(userID)
			return &IMAgentResponse{Text: fmt.Sprintf("当前规划模式：**%s**\n\n可选：`auto`（规划后立即执行）、`approve`（需批准）、`off`（关闭多步规划）", cur)}
		}
		mode := normalizeCodingPlanMode(modeArg)
		h.setStickyCodingPlanMode(userID, mode)
		return &IMAgentResponse{Text: fmt.Sprintf("规划模式已设为：**%s**", mode)}

	default:
		return &IMAgentResponse{Text: "未知 `/plan` 子命令。\n\n" + codingWorkbenchSlashHelpText()}
	}
}

func (h *IMMessageHandler) codingPlanStatusResponse(userID string) *IMAgentResponse {
	mem := h.getStickyCodingWorkbenchMemory(userID)
	mode := normalizeCodingPlanMode(mem.PlanMode)
	var b strings.Builder
	b.WriteString("## 规划状态\n\n")
	b.WriteString(fmt.Sprintf("- **模式**: `%s`\n", mode))
	if s := strings.TrimSpace(mem.SessionPlan); s != "" {
		b.WriteString("- **会话目标**: ")
		b.WriteString(truncateRunesForSubAgent(s, 200))
		b.WriteString("\n")
	}
	if pending, ok := h.loadStickyPendingCodingPlan(userID); ok {
		b.WriteString(fmt.Sprintf("- **待批准计划**: %d 步\n\n", len(pending.Tasks)))
		b.WriteString(pending.Markdown)
		b.WriteString("\n")
		return &IMAgentResponse{Text: b.String(), Actions: codingPlanApproveActions()}
	}
	if s := strings.TrimSpace(mem.ExecutionPlan); s != "" {
		b.WriteString("\n### 当前执行计划\n\n")
		b.WriteString(s)
		b.WriteString("\n")
	} else {
		b.WriteString("\n当前无活动/待批执行计划。\n")
	}
	if len(mem.StepStatuses) > 0 {
		b.WriteString("\n### 步骤状态\n\n")
		for _, st := range mem.StepStatuses {
			b.WriteString(fmt.Sprintf("- T%d **%s** — `%s`", st.Index, st.Title, st.Status))
			if st.VerifyCmd != "" {
				ok := "?"
				if st.VerifyOK != nil {
					if *st.VerifyOK {
						ok = "pass"
					} else {
						ok = "fail"
					}
				}
				b.WriteString(fmt.Sprintf(" (verify: %s %s)", st.VerifyCmd, ok))
			}
			b.WriteString("\n")
		}
	}
	return &IMAgentResponse{Text: b.String()}
}

func (h *IMMessageHandler) handleCodingReviewSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = trimmed
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法审阅：缺少项目路径。"}
	}
	gs := codingWorkbenchCollectGitSummary(projectPath)
	text := formatCodingWorkbenchGitSummary(gs)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if s := strings.TrimSpace(mem.LastSummary); s != "" {
		text += "\n## Last turn summary\n\n" + truncateRunesForSubAgent(s, 600) + "\n"
	}
	if len(mem.FilesModified)+len(mem.FilesCreated) > 0 {
		text += "\n## Session files\n\n"
		if len(mem.FilesModified) > 0 {
			text += "- Modified: " + strings.Join(uniqueSortedSubAgentStrings(mem.FilesModified), ", ") + "\n"
		}
		if len(mem.FilesCreated) > 0 {
			text += "- Created: " + strings.Join(uniqueSortedSubAgentStrings(mem.FilesCreated), ", ") + "\n"
		}
	}
	text += "\n_提示：深度审阅可直接说「review 本次改动」或 `/goal review changes`。_\n"
	return &IMAgentResponse{Text: text}
}

func (h *IMMessageHandler) handleCodingTestSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = userID
	_ = trimmed
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法运行测试：缺少项目路径。"}
	}
	ok, cmd, output, skipped := runCodingWorkbenchStepVerify(nil, projectPath)
	if skipped {
		return &IMAgentResponse{Text: "未检测到项目验证命令（go.mod / package.json / Cargo.toml / …）。请手动指定测试命令。"}
	}
	status := "通过"
	if !ok {
		status = "失败"
	}
	return &IMAgentResponse{Text: fmt.Sprintf("## 验证结果：**%s**\n\n命令：`%s`\n\n```\n%s\n```\n", status, cmd, output)}
}

func (h *IMMessageHandler) handleCodingCommitSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = userID
	msg := strings.TrimSpace(trimmed)
	if strings.HasPrefix(strings.ToLower(msg), "/commit") {
		msg = strings.TrimSpace(msg[len("/commit"):])
	}
	// Strip optional quotes.
	msg = strings.Trim(msg, "\"'")
	if msg == "" {
		// Default message from session plan.
		if h != nil {
			mem := h.getStickyCodingWorkbenchMemory(userID)
			if s := strings.TrimSpace(mem.SessionPlan); s != "" {
				msg = truncateRunesForSubAgent(s, 72)
			}
		}
	}
	if msg == "" {
		return &IMAgentResponse{Text: "用法：`/commit <message>`\n例如：`/commit fix: auth token refresh`"}
	}
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法提交：缺少项目路径。"}
	}
	hash, err := codingWorkbenchGitCommit(projectPath, msg)
	if err != nil {
		return &IMAgentResponse{Text: "提交失败：" + err.Error()}
	}
	out := fmt.Sprintf("已提交：`%s`\n\n消息：%s", hash, msg)
	if hash == "" {
		out = "已提交。\n\n消息：" + msg
	}
	return &IMAgentResponse{Text: out}
}

func (h *IMMessageHandler) handleCodingPRSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	title := strings.TrimSpace(trimmed)
	if strings.HasPrefix(strings.ToLower(title), "/pr") {
		title = strings.TrimSpace(title[len("/pr"):])
	}
	title = strings.Trim(title, "\"'")
	mem := stickyCodingWorkbenchMemory{}
	if h != nil {
		mem = h.getStickyCodingWorkbenchMemory(userID)
	}
	if title == "" {
		if s := strings.TrimSpace(mem.SessionPlan); s != "" {
			title = truncateRunesForSubAgent(s, 72)
		} else {
			title = "Coding workbench changes"
		}
	}
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法创建 PR：缺少项目路径。"}
	}
	body := codingWorkbenchSuggestPRBody(projectPath, mem)
	result, err := codingWorkbenchTryOpenPR(projectPath, title, body)
	if err != nil {
		return &IMAgentResponse{Text: "PR 失败：" + err.Error() + "\n\n" + body}
	}
	return &IMAgentResponse{Text: result}
}

func (h *IMMessageHandler) handleCodingMapSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = userID
	query := strings.TrimSpace(trimmed)
	if strings.HasPrefix(strings.ToLower(query), "/map") {
		query = strings.TrimSpace(query[len("/map"):])
	}
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法映射：缺少项目路径。"}
	}
	var b strings.Builder
	b.WriteString("## 项目地图\n\n")
	b.WriteString("**路径**: `")
	b.WriteString(projectPath)
	b.WriteString("`\n\n")

	// List top-level entries.
	entries, err := listProjectTopLevel(projectPath, 40)
	if err != nil {
		b.WriteString("列目录失败：")
		b.WriteString(err.Error())
		b.WriteString("\n")
	} else {
		b.WriteString("### 顶层\n\n")
		for _, e := range entries {
			b.WriteString("- ")
			b.WriteString(e)
			b.WriteString("\n")
		}
	}

	// AGENTS.md presence
	if content, sources := loadCodingWorkbenchProjectInstructions(projectPath); content != "" {
		b.WriteString("\n### 项目指令\n\n来源：")
		b.WriteString(strings.Join(sources, ", "))
		b.WriteString("\n\n")
		b.WriteString(truncateRunesForSubAgent(content, 600))
		b.WriteString("\n")
	}

	if query != "" {
		b.WriteString("\n### 定位建议\n\n")
		b.WriteString("若存在 `.codegraph/`，在编程代理中运行：\n\n```\ncodegraph explore \"")
		b.WriteString(query)
		b.WriteString("\"\n```\n\n否则使用 Glob / ripgrep 搜索关键词。\n")
	} else {
		b.WriteString("\n_提示：`/map <关键词>` 可生成 codegraph explore 提示。_\n")
	}
	return &IMAgentResponse{Text: b.String()}
}

func listProjectTopLevel(projectPath string, limit int) ([]string, error) {
	// Avoid importing os in signature-heavy path — use existing patterns.
	return listDirNamesLimited(projectPath, limit)
}

func (h *IMMessageHandler) handleCodingCheckpointSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = projectPath
	body := strings.TrimSpace(trimmed)
	if strings.HasPrefix(strings.ToLower(body), "/checkpoint") {
		body = strings.TrimSpace(body[len("/checkpoint"):])
	}
	lower := strings.ToLower(body)
	// list | ls | history — show current + prior checkpoints
	if lower == "list" || lower == "ls" || lower == "history" {
		entries := h.listStickyCodingCheckpoints(userID)
		if len(entries) == 0 {
			return &IMAgentResponse{Text: "尚无检查点。用 `/checkpoint [label]` 保存。"}
		}
		var b strings.Builder
		b.WriteString("## 检查点列表\n\n")
		for i, e := range entries {
			cur := ""
			if e.Current {
				cur = " **(current)**"
			}
			b.WriteString(fmt.Sprintf("%d. `%s`%s", i+1, e.Label, cur))
			if e.CreatedAt > 0 {
				b.WriteString(fmt.Sprintf(" · %s", time.Unix(e.CreatedAt, 0).Format("01-02 15:04")))
			}
			if e.SnapshotCount > 0 {
				b.WriteString(fmt.Sprintf(" · %d snaps", e.SnapshotCount))
			} else if e.FileCount > 0 {
				b.WriteString(fmt.Sprintf(" · %d files", e.FileCount))
			}
			b.WriteString("\n")
			if s := strings.TrimSpace(e.Summary); s != "" {
				b.WriteString("   " + truncateRunesForSubAgent(s, 100) + "\n")
			}
		}
		b.WriteString("\n`/checkpoint restore [label]` 还原计划；`/checkpoint restore files [label]` 含文件。\n")
		return &IMAgentResponse{Text: b.String()}
	}
	// restore [files|all|plan] [label]  — default plan-only current; optional history label
	if strings.HasPrefix(lower, "restore") || strings.HasPrefix(lower, "load") {
		arg := body
		for _, p := range []string{"restore", "load"} {
			if strings.HasPrefix(lower, p) {
				arg = strings.TrimSpace(body[len(p):])
				break
			}
		}
		// Parse: "files mid" | "files" | "mid" | "all before-refactor"
		parts := strings.Fields(arg)
		wantFiles := false
		label := ""
		for _, p := range parts {
			pl := strings.ToLower(p)
			if pl == "files" || pl == "file" || pl == "all" || pl == "--files" || pl == "--all" || pl == "plan" {
				if pl != "plan" {
					wantFiles = true
				}
				continue
			}
			if label == "" {
				label = p
			} else {
				label = label + " " + p
			}
		}
		// Always restore plan first; apply files once when requested.
		cp, ok := h.restoreStickyCodingCheckpointByLabel(userID, label, false)
		if !ok {
			if label != "" {
				return &IMAgentResponse{Text: fmt.Sprintf("未找到检查点 `%s`。用 `/checkpoint list` 查看。", label)}
			}
			return &IMAgentResponse{Text: "没有可恢复的检查点。先用 `/checkpoint [label]` 保存。"}
		}
		msg := fmt.Sprintf("已恢复检查点 **%s**（会话目标与执行计划已还原）。", cp.Label)
		if wantFiles {
			restored, skipped, err := h.applyCodingCheckpointFileSnapshots(userID, cp, nil)
			if err != nil {
				msg += "\n文件还原：" + err.Error()
			} else {
				msg += fmt.Sprintf("\n已写回 **%d** 个文件快照", restored)
				if skipped > 0 {
					msg += fmt.Sprintf("（跳过 %d）", skipped)
				}
				msg += "。"
			}
		} else {
			msg += "\n\n仅还原计划。还原磁盘文件请用 `/checkpoint restore files` 或 `/checkpoint restore files <label>`。"
		}
		if s := strings.TrimSpace(cp.Summary); s != "" {
			msg += "\n\n摘要：" + truncateRunesForSubAgent(s, 400)
		}
		return &IMAgentResponse{Text: msg}
	}
	if lower == "prune" || lower == "gc" || lower == "clean" {
		userN, orphanN := h.pruneStickyCodingCheckpointSidecars(userID)
		keep := ""
		if cp, ok := h.loadStickyCodingCheckpoint(userID); ok {
			keep = cp.Label
		}
		st := collectCodingCheckpointSidecarStats(userID, keep)
		return &IMAgentResponse{Text: fmt.Sprintf(
			"已清理检查点侧车：用户目录移除 **%d** 项，全局孤儿 **%d** 项。\n当前检查点 label 的文件快照会保留。\n\n%s",
			userN, orphanN, formatCodingCheckpointSidecarStatsLine(st),
		)}
	}
	if lower == "usage" || lower == "stats" || lower == "disk" {
		keep := ""
		if cp, ok := h.loadStickyCodingCheckpoint(userID); ok {
			keep = cp.Label
		}
		st := collectCodingCheckpointSidecarStats(userID, keep)
		var b strings.Builder
		b.WriteString("## 检查点侧车用量\n\n")
		b.WriteString(fmt.Sprintf("- **Total**: %.2f MB / %.0f MB (%.0f%%)\n",
			float64(st.TotalBytes)/(1024*1024), float64(st.MaxBytes)/(1024*1024), st.UsageRatio*100))
		b.WriteString(fmt.Sprintf("- **Labels**: %d\n", st.DirCount))
		if st.UserKey != "" {
			b.WriteString(fmt.Sprintf("- **This session**: %.2f MB · %d labels\n",
				float64(st.UserBytes)/(1024*1024), st.UserDirCount))
		}
		if st.KeepLabel != "" {
			b.WriteString(fmt.Sprintf("- **Keep label**: `%s`\n", st.KeepLabel))
		}
		b.WriteString("\n`/checkpoint prune` 清理非当前 label 并驱逐超额目录。\n")
		return &IMAgentResponse{Text: b.String()}
	}
	if lower == "show" || lower == "status" || strings.HasPrefix(lower, "show ") {
		showLabel := ""
		if strings.HasPrefix(lower, "show ") {
			showLabel = strings.TrimSpace(body[len("show"):])
		}
		cp, ok := h.loadStickyCodingCheckpointByLabel(userID, showLabel)
		if !ok {
			if showLabel != "" {
				return &IMAgentResponse{Text: fmt.Sprintf("未找到检查点 `%s`。", showLabel)}
			}
			return &IMAgentResponse{Text: "尚无检查点。"}
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("## 检查点\n\n- **Label**: %s\n- **At**: %d\n- **Summary**: %s\n", cp.Label, cp.CreatedAt, truncateRunesForSubAgent(cp.Summary, 400)))
		if s := strings.TrimSpace(cp.SessionPlan); s != "" {
			b.WriteString(fmt.Sprintf("- **Session plan**: %s\n", truncateRunesForSubAgent(s, 200)))
		}
		if len(cp.Files) > 0 {
			b.WriteString(fmt.Sprintf("- **Files** (%d): ", len(cp.Files)))
			shown := cp.Files
			if len(shown) > 20 {
				shown = shown[:20]
			}
			b.WriteString(strings.Join(shown, ", "))
			if len(cp.Files) > 20 {
				b.WriteString(fmt.Sprintf(" …(+%d)", len(cp.Files)-20))
			}
			b.WriteString("\n")
		}
		snapOK, snapSkip, snapSide := 0, 0, 0
		for _, s := range cp.FileSnapshots {
			if s.Content != "" {
				snapOK++
			} else if strings.TrimSpace(s.Sidecar) != "" {
				snapSide++
				snapOK++
			} else {
				snapSkip++
			}
		}
		if snapOK > 0 || snapSkip > 0 {
			b.WriteString(fmt.Sprintf("- **File snapshots**: %d restorable", snapOK))
			if snapSide > 0 {
				b.WriteString(fmt.Sprintf(" (%d sidecar)", snapSide))
			}
			if snapSkip > 0 {
				b.WriteString(fmt.Sprintf(", %d skipped (large/binary/missing)", snapSkip))
			}
			b.WriteString("\n")
		}
		histN := len(h.loadStickyCodingCheckpointHistory(userID))
		if histN > 0 {
			b.WriteString(fmt.Sprintf("- **History slots**: %d（`/checkpoint list`）\n", histN))
		}
		b.WriteString("\n`/checkpoint restore [label]` 还原计划；`/checkpoint restore files [label]` 同时写回文件快照。\n")
		return &IMAgentResponse{Text: b.String()}
	}
	label := body
	cp := h.saveStickyCodingCheckpoint(userID, label)
	snapN := 0
	for _, s := range cp.FileSnapshots {
		if s.Content != "" || strings.TrimSpace(s.Sidecar) != "" {
			snapN++
		}
	}
	msg := fmt.Sprintf("已保存检查点 **%s**。", cp.Label)
	if snapN > 0 {
		msg += fmt.Sprintf(" 含 %d 个文件内容快照。", snapN)
	}
	msg += "\n\n`/checkpoint restore` 还原计划；`/checkpoint restore files` 还原计划+文件。"
	return &IMAgentResponse{Text: msg}
}

func (h *IMMessageHandler) handleCodingWorktreeSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	body := strings.TrimSpace(trimmed)
	if strings.HasPrefix(strings.ToLower(body), "/worktree") {
		body = strings.TrimSpace(body[len("/worktree"):])
	}
	lower := strings.ToLower(body)
	switch {
	case lower == "" || lower == "status" || lower == "list" || lower == "show":
		mode := h.getStickyCodingWorktreeMode(userID)
		list, err := listCodingWorkbenchWorktrees(projectPath)
		if err != nil {
			list = "list error: " + err.Error()
		}
		mem := h.getStickyCodingWorkbenchMemory(userID)
		var b strings.Builder
		b.WriteString(fmt.Sprintf("## Worktree 模式: `%s`\n\n", mode))
		b.WriteString("可选：`auto`（并行写改隔离）· `always`（所有写步骤隔离）· `off`\n\n")
		b.WriteString(list)
		if len(mem.WorktreeConflicts) > 0 {
			b.WriteString("\n")
			b.WriteString(formatCodingConflictsMarkdown(mem.WorktreeConflicts))
		}
		if len(mem.WorktreeNotes) > 0 {
			b.WriteString("\n### 本会话记录\n\n")
			for i := len(mem.WorktreeNotes) - 1; i >= 0; i-- {
				b.WriteString("- ")
				b.WriteString(mem.WorktreeNotes[i])
				b.WriteString("\n")
			}
		}
		return &IMAgentResponse{Text: b.String()}

	case lower == "conflicts" || lower == "conflict":
		return &IMAgentResponse{Text: formatCodingConflictsMarkdown(h.listStickyCodingConflicts(userID))}

	case strings.HasPrefix(lower, "log") || strings.HasPrefix(lower, "audit") || strings.HasPrefix(lower, "history"):
		arg := ""
		for _, p := range []string{"log", "audit", "history"} {
			if strings.HasPrefix(lower, p) {
				arg = strings.TrimSpace(body[len(p):])
				break
			}
		}
		argLower := strings.ToLower(arg)
		if argLower == "clear" || argLower == "reset" || argLower == "wipe" {
			h.clearStickyCodingConflictLog(userID)
			return &IMAgentResponse{Text: "冲突解决日志已清空。"}
		}
		if argLower == "export" || argLower == "dump" || argLower == "save" {
			md, n := h.exportStickyCodingConflictLog(userID)
			if n == 0 {
				return &IMAgentResponse{Text: "尚无冲突解决记录可导出。"}
			}
			return &IMAgentResponse{Text: md + "\n\n_已写入 worktree notes 摘要，可用 `/worktree` 查看。_"}
		}
		mem := h.getStickyCodingWorkbenchMemory(userID)
		if len(mem.ConflictLog) == 0 {
			return &IMAgentResponse{Text: "尚无冲突解决记录。采纳 / 保留主树 / base / 丢弃后会写入日志。\n\n`/worktree log clear` 清空 · `/worktree log export` 导出。"}
		}
		var b strings.Builder
		b.WriteString("## 冲突解决日志\n\n")
		// Newest last in sticky; show newest first.
		for i := len(mem.ConflictLog) - 1; i >= 0; i-- {
			b.WriteString("- ")
			b.WriteString(mem.ConflictLog[i])
			b.WriteString("\n")
		}
		b.WriteString("\n`/worktree log clear` 清空 · `/worktree log export` 导出到 notes。\n")
		return &IMAgentResponse{Text: b.String()}

	case strings.HasPrefix(lower, "diff"):
		arg := strings.TrimSpace(body[len("diff"):])
		if arg == "" {
			conflicts := h.listStickyCodingConflicts(userID)
			if len(conflicts) == 1 {
				arg = conflicts[0].ID
			} else {
				return &IMAgentResponse{Text: "用法：`/worktree diff <id>`\n\n" + formatCodingConflictsMarkdown(conflicts)}
			}
		}
		diffs, c, err := h.getCodingConflictFileDiffs(userID, arg, 20)
		if err != nil {
			return &IMAgentResponse{Text: "diff 失败：" + err.Error()}
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("## 冲突文件预览 `%s`\n\n", c.ID))
		if len(diffs) == 0 {
			b.WriteString("（无文件）\n")
		}
		for _, d := range diffs {
			b.WriteString(fmt.Sprintf("### `%s` · %s\n\n", d.Path, d.Status))
			if d.ThreeWay != "" {
				b.WriteString("```text\n")
				b.WriteString(d.ThreeWay)
				b.WriteString("\n```\n\n")
			} else if d.Unified != "" {
				b.WriteString("```diff\n")
				b.WriteString(d.Unified)
				b.WriteString("\n```\n\n")
			}
			if d.BaseHead != "" && d.ThreeWay == "" {
				b.WriteString("_base (merge-base) available — use adopt for theirs, or edit main manually._\n\n")
			}
		}
		b.WriteString("采纳：`/worktree adopt " + c.ID + "` 或 `/worktree adopt " + c.ID + " -- path/to/file`\n")
		return &IMAgentResponse{Text: b.String()}

	case strings.HasPrefix(lower, "adopt"):
		arg := strings.TrimSpace(body[len("adopt"):])
		if arg == "" {
			conflicts := h.listStickyCodingConflicts(userID)
			if len(conflicts) == 1 {
				arg = conflicts[0].ID
			} else {
				return &IMAgentResponse{Text: "用法：`/worktree adopt <id> [-- file…]`\n\n" + formatCodingConflictsMarkdown(conflicts)}
			}
		}
		// Parse: <id> [-- file1 file2]  or  <id> file1 file2
		id, files := parseAdoptArgs(arg)
		var out string
		var err error
		if len(files) > 0 {
			out, err = h.adoptCodingWorkbenchConflictFiles(userID, id, files)
		} else {
			out, err = h.adoptCodingWorkbenchConflict(userID, id)
		}
		if err != nil {
			return &IMAgentResponse{Text: "adopt 失败：" + err.Error()}
		}
		return &IMAgentResponse{Text: out}

	case strings.HasPrefix(lower, "discard") || strings.HasPrefix(lower, "drop") || strings.HasPrefix(lower, "delete"):
		var arg string
		for _, p := range []string{"discard", "drop", "delete"} {
			if strings.HasPrefix(lower, p) {
				arg = strings.TrimSpace(body[len(p):])
				break
			}
		}
		if arg == "" {
			return &IMAgentResponse{Text: "用法：`/worktree discard <id|all>`\n\n" + formatCodingConflictsMarkdown(h.listStickyCodingConflicts(userID))}
		}
		argLower := strings.ToLower(strings.TrimSpace(arg))
		if argLower == "all" || argLower == "--all" || argLower == "*" {
			out, err := h.discardAllStickyCodingConflicts(userID)
			if err != nil {
				return &IMAgentResponse{Text: "discard all 失败：" + err.Error()}
			}
			return &IMAgentResponse{Text: out}
		}
		out, err := h.discardCodingWorkbenchConflict(userID, arg)
		if err != nil {
			return &IMAgentResponse{Text: "discard 失败：" + err.Error()}
		}
		return &IMAgentResponse{Text: out}

	case strings.HasPrefix(lower, "keep") || strings.HasPrefix(lower, "reject"):
		// keep main / reject theirs for selected files
		var arg string
		for _, p := range []string{"keep", "reject"} {
			if strings.HasPrefix(lower, p) {
				arg = strings.TrimSpace(body[len(p):])
				break
			}
		}
		if arg == "" {
			return &IMAgentResponse{Text: "用法：`/worktree keep <id> [-- file…]`（主树保留，不采纳隔离侧）\n\n" + formatCodingConflictsMarkdown(h.listStickyCodingConflicts(userID))}
		}
		id, files := parseAdoptArgs(arg)
		out, err := h.keepMainCodingConflictFiles(userID, id, files)
		if err != nil {
			return &IMAgentResponse{Text: "keep 失败：" + err.Error()}
		}
		return &IMAgentResponse{Text: out}

	case strings.HasPrefix(lower, "base") || strings.HasPrefix(lower, "adopt-base") || strings.HasPrefix(lower, "take-base"):
		var arg string
		for _, p := range []string{"adopt-base", "take-base", "base"} {
			if strings.HasPrefix(lower, p) {
				arg = strings.TrimSpace(body[len(p):])
				break
			}
		}
		if arg == "" {
			return &IMAgentResponse{Text: "用法：`/worktree base <id> [-- file…]`（写回 merge-base 到主树）\n\n" + formatCodingConflictsMarkdown(h.listStickyCodingConflicts(userID))}
		}
		id, files := parseAdoptArgs(arg)
		out, err := h.adoptBaseCodingConflictFiles(userID, id, files)
		if err != nil {
			return &IMAgentResponse{Text: "base 失败：" + err.Error()}
		}
		return &IMAgentResponse{Text: out}

	case strings.HasPrefix(lower, "resolve"):
		// /worktree resolve <id> <adopt|keep|base> [-- file…]
		arg := strings.TrimSpace(body[len("resolve"):])
		if arg == "" {
			return &IMAgentResponse{Text: "用法：`/worktree resolve <id> adopt|keep|base [-- file…]`\n\n" + formatCodingConflictsMarkdown(h.listStickyCodingConflicts(userID))}
		}
		id, rest, action, files := parseWorktreeResolveArgs(arg)
		if id == "" || action == "" {
			return &IMAgentResponse{Text: "用法：`/worktree resolve <id> adopt|keep|base [-- file…]`\n\n" + formatCodingConflictsMarkdown(h.listStickyCodingConflicts(userID))}
		}
		_ = rest
		var out string
		var err error
		switch action {
		case "adopt", "theirs":
			if len(files) == 0 {
				out, err = h.adoptCodingWorkbenchConflict(userID, id)
			} else {
				out, err = h.adoptCodingWorkbenchConflictFiles(userID, id, files)
			}
		case "keep", "main", "ours":
			out, err = h.keepMainCodingConflictFiles(userID, id, files)
		case "base", "adopt-base", "take-base":
			out, err = h.adoptBaseCodingConflictFiles(userID, id, files)
		default:
			return &IMAgentResponse{Text: "未知 resolve 动作，请用 adopt / keep / base。"}
		}
		if err != nil {
			return &IMAgentResponse{Text: "resolve 失败：" + err.Error()}
		}
		return &IMAgentResponse{Text: out}

	case strings.HasPrefix(lower, "mode"):
		arg := strings.TrimSpace(body[len("mode"):])
		if arg == "" {
			return &IMAgentResponse{Text: fmt.Sprintf("当前 worktree 模式：**%s**\n\n可选 auto / always / off", h.getStickyCodingWorktreeMode(userID))}
		}
		mode := normalizeCodingWorktreeMode(arg)
		h.setStickyCodingWorktreeMode(userID, mode)
		return &IMAgentResponse{Text: fmt.Sprintf("Worktree 模式已设为：**%s**", mode)}

	case lower == "prune" || lower == "clean":
		out, err := pruneCodingWorkbenchWorktrees(projectPath)
		if err != nil {
			return &IMAgentResponse{Text: "prune 失败：" + err.Error()}
		}
		return &IMAgentResponse{Text: out}

	default:
		return &IMAgentResponse{Text: "未知 `/worktree` 子命令。\n\n用法：`/worktree` · `mode` · `conflicts` · `diff` · `adopt` · `keep` · `base` · `resolve` · `log` · `discard` · `discard all` · `prune`"}
	}
}

// parseWorktreeResolveArgs parses "id adopt|keep|base [-- file…]".
// Returns id, leftover rest (unused), action, files.
func parseWorktreeResolveArgs(arg string) (id, rest, action string, files []string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", "", "", nil
	}
	// Prefer "--" file separator: "c1 keep -- a.go b.go"
	if i := strings.Index(arg, " -- "); i >= 0 {
		head := strings.Fields(strings.TrimSpace(arg[:i]))
		tail := strings.Fields(strings.TrimSpace(arg[i+4:]))
		if len(head) >= 1 {
			id = head[0]
		}
		if len(head) >= 2 {
			action = strings.ToLower(head[1])
		}
		// allow "id action" only in head; extra head tokens ignored
		files = tail
		return id, strings.TrimSpace(arg[i+4:]), action, files
	}
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return "", "", "", nil
	}
	id = fields[0]
	if len(fields) == 1 {
		return id, "", "", nil
	}
	action = strings.ToLower(fields[1])
	if len(fields) > 2 {
		files = fields[2:]
	}
	return id, strings.Join(fields[2:], " "), action, files
}

// parseAdoptArgs splits "id -- a.go b.go" or "id a.go b.go".
func parseAdoptArgs(arg string) (id string, files []string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", nil
	}
	if i := strings.Index(arg, " -- "); i >= 0 {
		id = strings.TrimSpace(arg[:i])
		rest := strings.Fields(strings.TrimSpace(arg[i+4:]))
		return id, rest
	}
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return "", nil
	}
	id = fields[0]
	if len(fields) > 1 {
		files = fields[1:]
	}
	return id, files
}

func (h *IMMessageHandler) handleCodingRouteSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = projectPath
	body := strings.TrimSpace(trimmed)
	if strings.HasPrefix(strings.ToLower(body), "/route") {
		body = strings.TrimSpace(body[len("/route"):])
	}
	lower := strings.ToLower(body)
	if strings.HasPrefix(lower, "pref") || strings.HasPrefix(lower, "mode") || strings.HasPrefix(lower, "set") {
		arg := body
		for _, p := range []string{"pref", "mode", "set"} {
			if strings.HasPrefix(lower, p) {
				arg = strings.TrimSpace(body[len(p):])
				break
			}
		}
		if arg == "" {
			return &IMAgentResponse{Text: fmt.Sprintf("当前选模偏好：**%s**\n\n可选：`auto` · `primary` · `reasoning` · `vision`", h.getStickyCodingRoutePref(userID))}
		}
		pref := normalizeCodingRoutePref(arg)
		h.setStickyCodingRoutePref(userID, pref)
		return &IMAgentResponse{Text: fmt.Sprintf("编程选模偏好已设为：**%s**", pref)}
	}

	mem := h.getStickyCodingWorkbenchMemory(userID)
	var b strings.Builder
	b.WriteString("## 模型路由\n\n")
	b.WriteString(fmt.Sprintf("- **Preference**: `%s`\n", normalizeCodingRoutePref(mem.RoutePref)))
	if m := strings.TrimSpace(mem.LastRouteModel); m != "" {
		b.WriteString(fmt.Sprintf("- **Last Model**: `%s`\n", m))
		if s := strings.TrimSpace(mem.LastRouteSource); s != "" {
			b.WriteString(fmt.Sprintf("- **Source**: `%s`\n", s))
		}
		if t := strings.TrimSpace(mem.LastRouteTask); t != "" {
			b.WriteString(fmt.Sprintf("- **Task**: `%s`\n", t))
		}
		if r := strings.TrimSpace(mem.LastRouteReason); r != "" {
			b.WriteString(fmt.Sprintf("- **Reason**: %s\n", r))
		}
	} else {
		b.WriteString("- **Last Model**: （尚无记录）\n")
	}
	b.WriteString("\n")
	b.WriteString(h.formatCodingRouteCapabilitiesMarkdown())
	cost := formatCodingSessionCostLine(mem)
	if cost != "" {
		b.WriteString("\n")
		b.WriteString(cost)
		b.WriteString("\n")
	}
	b.WriteString("\n设置偏好：`/route pref auto|primary|reasoning|vision`\n")
	b.WriteString("_auto：有图走 vision，否则 reasoning；primary：强制主模型。_\n")
	b.WriteString("_ModelRoutes 在 **设置 → LLM 缓存 → 模型路由** 中配置（热更新，无需重启）；未配置时 reasoning/vision 回退主模型。_\n")
	return &IMAgentResponse{Text: b.String()}
}

func (h *IMMessageHandler) handleCodingCostSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = projectPath
	_ = trimmed
	mem := h.getStickyCodingWorkbenchMemory(userID)
	line := formatCodingSessionCostLine(mem)
	if line == "" {
		return &IMAgentResponse{Text: "本编程会话尚无 token/费用记录。完成一轮编码任务后会自动累计。"}
	}
	return &IMAgentResponse{Text: "## 费用与用量\n\n" + line + "\n"}
}

func (h *IMMessageHandler) handleCodingBgSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	body := strings.TrimSpace(trimmed)
	if strings.HasPrefix(strings.ToLower(body), "/bg") {
		body = strings.TrimSpace(body[len("/bg"):])
	}
	lower := strings.ToLower(body)
	if lower == "" || lower == "help" {
		return &IMAgentResponse{Text: "用法：`/bg test` 后台验证；`/bg status` 查看结果。"}
	}
	if lower == "status" || lower == "result" {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		if s := strings.TrimSpace(mem.BackgroundVerifySummary); s != "" {
			return &IMAgentResponse{Text: "## 后台验证结果\n\n" + s}
		}
		return &IMAgentResponse{Text: "尚无后台验证结果。先运行 `/bg test`。"}
	}
	if lower == "test" || lower == "verify" {
		msg, err := h.startCodingWorkbenchBackgroundVerify(userID, projectPath)
		if err != nil {
			return &IMAgentResponse{Text: err.Error()}
		}
		return &IMAgentResponse{Text: msg}
	}
	return &IMAgentResponse{Text: "未知 `/bg` 子命令。用法：`/bg test` · `/bg status`"}
}

// startCodingWorkbenchBackgroundVerify kicks off async project verification and
// stores the result in sticky BackgroundVerifySummary. Safe for slash + Wails.
func (h *IMMessageHandler) startCodingWorkbenchBackgroundVerify(userID, projectPath string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("handler unavailable")
	}
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		projectPath = strings.TrimSpace(mem.ProjectPath)
	}
	if projectPath == "" {
		return "", fmt.Errorf("无法后台验证：缺少项目路径")
	}
	mem := h.getStickyCodingWorkbenchMemory(userID)
	mem.BackgroundVerifySummary = "后台验证运行中…"
	mem.BackgroundVerifyAtUnix = time.Now().Unix()
	h.storeStickyCodingWorkbenchMemory(userID, mem)

	if strings.EqualFold(mem.Kind, "remote") && strings.TrimSpace(mem.RemoteSessionID) != "" {
		sid := mem.RemoteSessionID
		pdir := mem.RemoteProjectDir
		if pdir == "" {
			pdir = mem.RemoteWorkDir
		}
		go func() {
			ok, cmd, out, skipped := runCodingWorkbenchRemoteStepVerify(h, sid, pdir)
			sum := codingWorkbenchStepGateSummary(ok, cmd, out, skipped)
			if skipped {
				sum = "后台远程验证：未检测到验证命令"
			}
			m := h.getStickyCodingWorkbenchMemory(userID)
			m.BackgroundVerifySummary = sum
			m.BackgroundVerifyAtUnix = time.Now().Unix()
			h.storeStickyCodingWorkbenchMemory(userID, m)
		}()
		return "已在后台启动**远程**项目验证。稍后用 `/bg status` 或状态栏查看结果。", nil
	}
	go func() {
		ok, cmd, out, skipped := runCodingWorkbenchStepVerify(nil, projectPath)
		sum := codingWorkbenchStepGateSummary(ok, cmd, out, skipped)
		if skipped {
			sum = "后台验证：未检测到验证命令"
		}
		m := h.getStickyCodingWorkbenchMemory(userID)
		m.BackgroundVerifySummary = sum
		m.BackgroundVerifyAtUnix = time.Now().Unix()
		h.storeStickyCodingWorkbenchMemory(userID, m)
	}()
	return "已在后台启动项目验证。稍后用 `/bg status` 或状态栏查看结果。", nil
}

func (h *IMMessageHandler) handleCodingHooksSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = userID
	_ = trimmed
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法加载 hooks：缺少项目路径。"}
	}
	hooks := loadCodingWorkbenchHooks(projectPath)
	var b strings.Builder
	b.WriteString("## Coding hooks (`.maclaw/hooks.json`)\n\n")
	if hooks.FailOnError {
		b.WriteString("_fail_on_error: **true**（pre_step / pre_verify 失败会中止该步）_\n\n")
	} else {
		b.WriteString("_fail_on_error: false（钩子失败只记日志）_\n\n")
	}
	writeHookPhase := func(name string, cmds []string) {
		b.WriteString(fmt.Sprintf("### `%s` (%d)\n", name, len(cmds)))
		if len(cmds) == 0 {
			b.WriteString("_（无）_\n\n")
			return
		}
		for i, c := range cmds {
			b.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, truncateRunesForSubAgent(c, 120)))
		}
		b.WriteString("\n")
	}
	writeHookPhase("pre_plan", hooks.PrePlan)
	writeHookPhase("pre_step", hooks.PreStep)
	writeHookPhase("post_step", hooks.PostStep)
	writeHookPhase("pre_verify", hooks.PreVerify)
	writeHookPhase("post_verify", hooks.PostVerify)
	writeHookPhase("post_turn", hooks.PostTurn)
	writeHookPhase("pre_checkpoint", hooks.PreCheckpoint)
	writeHookPhase("post_checkpoint", hooks.PostCheckpoint)
	writeHookPhase("on_conflict", hooks.OnConflict)
	b.WriteString("示例：\n```json\n{\n  \"pre_step\": [\"echo pre\"],\n  \"post_step\": [],\n  \"pre_plan\": [],\n  \"pre_verify\": [],\n  \"post_verify\": [],\n  \"post_turn\": [],\n  \"pre_checkpoint\": [],\n  \"post_checkpoint\": [],\n  \"on_conflict\": [],\n  \"fail_on_error\": false\n}\n```\n")
	return &IMAgentResponse{Text: b.String()}
}

func (h *IMMessageHandler) handleCodingAgentsSlash(userID, projectPath, trimmed string) *IMAgentResponse {
	_ = trimmed
	if projectPath == "" {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		projectPath = strings.TrimSpace(mem.ProjectPath)
	}
	if projectPath == "" {
		return &IMAgentResponse{Text: "无法加载项目指令：缺少项目路径。"}
	}
	content, sources := loadCodingWorkbenchProjectInstructions(projectPath)
	// Refresh sticky.
	if h != nil {
		h.ensureStickyProjectInstructions(userID, projectPath)
	}
	if content == "" {
		return &IMAgentResponse{Text: "未找到 AGENTS.md / CLAUDE.md / .maclaw 指令文件。\n\n可在项目根目录添加 `AGENTS.md`。"}
	}
	var b strings.Builder
	b.WriteString("## 项目指令\n\n")
	b.WriteString("来源：")
	b.WriteString(strings.Join(sources, ", "))
	b.WriteString("\n\n")
	// Full content already capped by loader.
	if utf8.RuneCountInString(content) > 3500 {
		b.WriteString(truncateRunesForSubAgent(content, 3500))
	} else {
		b.WriteString(content)
	}
	return &IMAgentResponse{Text: b.String()}
}
