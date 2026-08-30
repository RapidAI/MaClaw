package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) handleImmediateIMCommand(msg IMUserMessage, trimmed string, onProgress tool.ProgressCallback, onToken llm.TokenCallback) (*IMAgentResponse, bool) {
	return h.handleImmediateIMCommandWithLoop(msg, trimmed, nil, onProgress, onToken)
}

// handleImmediateIMCommandWithLoop keeps shortcut commands inside the same
// group boundary as the regular agent loop. A direct command must never gain
// more authority than the tool menu it bypasses.
func (h *IMMessageHandler) handleImmediateIMCommandWithLoop(msg IMUserMessage, trimmed string, providedLoopCtx *LoopContext, onProgress tool.ProgressCallback, onToken llm.TokenCallback) (*IMAgentResponse, bool) {
	commandKind := classifyImmediateIMCommand(trimmed)
	responseLang := h.imCommandResponseLang(msg.Lang)
	if providedLoopCtx != nil && providedLoopCtx.LansengerGroupPermissions != nil && commandKind != imCommandUnknown {
		return &IMAgentResponse{Text: localizedLansengerGroupCommandRestrictedMessage(responseLang)}, true
	}
	if commandKind == imCommandReset {
		resp := h.resetIMSessionForUser(msg.UserID, responseLang)
		if ctx := h.getSessionLoopCtx(msg.UserID); ctx != nil {
			return h.finalizeTraceResult(ctx, resp, resp.Text, ""), true
		}
		if ctx, _, ok := h.legacyLoopSnapshotForUser(msg.UserID); ok {
			return h.finalizeTraceResult(ctx, resp, resp.Text, ""), true
		}
		return resp, true
	}

	hasPendingAskUser := false
	// Load history lazily and at most once: handlers constructed without memory
	// (unit tests) must not touch h.memory, and loads are wasted when no
	// pending state exists.
	var historySnapshot []agent.ConversationEntry
	historyLoaded := false
	loadHistory := func() []agent.ConversationEntry {
		if !historyLoaded {
			historySnapshot = h.memory.Load(msg.UserID)
			historyLoaded = true
		}
		return historySnapshot
	}
	if raw, loaded := h.pendingAskUser.Load(msg.UserID); loaded {
		_, hasPendingAskUser = pendingAskUserForCurrentHistory(raw, loadHistory())
	}
	if !hasPendingAskUser {
		if raw, loaded := h.pendingRecordAudio.Load(msg.UserID); loaded {
			_, hasPendingAskUser = pendingRecordAudioForCurrentHistory(raw, loadHistory())
		}
	}
	if !hasPendingAskUser {
		if raw, loaded := h.pendingPostRecording.Load(msg.UserID); loaded {
			_, hasPendingAskUser = pendingPostRecordingForCurrentHistory(raw, loadHistory())
		}
	}

	if platformKind := normalizeIMMessagePlatformKind(msg.Platform); (platformKind.IsKnown() || msg.Platform != "") && !platformKind.IsDesktop() {
		imConfirmKey := msg.Platform + ":" + msg.UserID
		if v, ok := h.pendingCriticalConfirmIM.LoadAndDelete(imConfirmKey); ok {
			confirmID, _ := v.(string)
			if confirmID != "" {
				confirmed := classifyIMCriticalConfirmReply(trimmed) == imCriticalConfirmReplyApprove
				h.ResolveCriticalConfirm(confirmID, confirmed) //nolint:errcheck // IM path: error logged internally
				return &IMAgentResponse{Text: localizedIMCriticalSkillConfirmMessage(responseLang, confirmed)}, true
			}
		}
	}

	// Short chit-chat only for free-form text — never intercept a classified
	// slash/install command (defense in depth if phrase lists grow later).
	// ACP Mode B: host flushes non-streamed text as agent_message_chunk.
	// Use bare user text when body is workspace-wrapped.
	chitText := trimmed
	if isACPProgrammingMessage(msg) {
		chitText = acpUserFacingText(trimmed)
	}
	if commandKind == imCommandUnknown && !msg.IsBackground && len(msg.Attachments) == 0 && isShortChitChatMessage(chitText) && !hasPendingAskUser && !h.workflowReviewPending(msg.UserID, msg.IsBackground) {
		return &IMAgentResponse{Text: buildShortChitChatResponse(chitText, msg.Lang)}, true
	}
	switch commandKind {
	case imCommandExit:
		return h.handleExitCommand(msg.UserID, responseLang), true
	case imCommandSessions:
		return h.handleSessionsCommandWithLang(responseLang), true
	case imCommandCompress:
		return h.handleCompressCommandWithLang(msg.UserID, responseLang), true
	case imCommandMemory:
		return h.handleMemoryStatusCommandWithLang(responseLang), true
	case imCommandHelp:
		return &IMAgentResponse{Text: localizedIMSlashHelpText(responseLang)}, true
	}
	switch commandKind {
	case imCommandBTW:
		btwQuery := strings.TrimSpace(strings.TrimPrefix(trimmed, "/btw"))
		if btwQuery == "" {
			return &IMAgentResponse{Text: localizedIMBtwUsageText(responseLang)}, true
		}
		if isBtwMainAgentStatusQuery(btwQuery) {
			return h.btwMainAgentStatusResponse(msg.UserID, responseLang), true
		}
		if !h.isMaclawLLMConfigured() {
			return &IMAgentResponse{Error: localizedIMLLMNotConfiguredMessage(responseLang, "/btw")}, true
		}
		return h.handleBtwCommand(msg, btwQuery, onProgress, onToken), true
	case imCommandLoop:
		if !h.isMaclawLLMConfigured() {
			return &IMAgentResponse{Error: localizedIMLLMNotConfiguredMessage(responseLang, "/loop")}, true
		}
		return h.handleLoopCommand(msg, trimmed, onProgress, onToken), true
	case imCommandWorkflow:
		return h.handleWorkflowCommand(msg, trimmed, responseLang)
	case imCommandGoal:
		return h.handleGoalCommand(msg, trimmed), true
	case imCommandBranch:
		return h.handleBranchCommand(msg, trimmed), true
	case imCommandMoA:
		// Normally rewritten in handleIMMessageWithLoop before this switch.
		// Keep as safety net for direct callers.
		return h.handleMoACommand(msg, trimmed, responseLang), true
	case imCommandCodingWorkbench:
		return h.handleCodingWorkbenchIMCommand(msg, trimmed, onProgress, onToken), true
	case imCommandSkill, imCommandMCP, imCommandPlugin, imCommandInstall:
		return h.handleInstallIMCommand(commandKind, trimmed, responseLang), true
	}
	if commandKind == imCommandCancel {
		return h.cancelCurrentTaskForUser(msg.UserID, responseLang), true
	}

	return nil, false
}

// isBtwMainAgentStatusQuery identifies the compact status forms that should
// not spend an LLM round. Broader natural-language requests still use the
// /btw subagent, which can combine agent_status with other read-only tools.
func isBtwMainAgentStatusQuery(query string) bool {
	query = strings.ToLower(strings.Join(strings.Fields(query), ""))
	switch query {
	case "status", "agentstatus", "mainagentstatus", "mainagent",
		"主agent状态", "主agentstatus", "主agent", "查看主agent状态", "查看主agentstatus",
		"主agent狀態", "查看主agent狀態",
		"主代理状态", "主代理", "查看主代理状态",
		"主代理狀態", "查看主代理狀態",
		"主智能体状态", "主智能体", "查看主智能体状态",
		"主智能體狀態", "主智能體", "查看主智能體狀態",
		"主助手状态", "主助手", "查看主助手状态",
		"主助手狀態", "查看主助手狀態":
		return true
	default:
		return false
	}
}

// btwMainAgentStatusResponse serves /btw status without requiring an LLM and
// uses the caller's owner ID so status remains isolated between IM users.
func (h *IMMessageHandler) btwMainAgentStatusResponse(ownerID, lang string) *IMAgentResponse {
	if strings.TrimSpace(ownerID) == "" {
		return &IMAgentResponse{Error: localizedIMBtwMainAgentStatusOwnerMissing(lang)}
	}
	status := formatMainAgentStatus(h.collectRuntimeStatusForOwner(ownerID), lang)
	return &IMAgentResponse{Text: localizedIMBtwResultPrefix(lang) + status}
}

func localizedIMBtwMainAgentStatusOwnerMissing(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Cannot read main-agent status because the current session identity is missing."
	case appLanguageZhHant:
		return "無法讀取主 Agent 狀態，因為目前會話身分缺失。"
	default:
		return "无法读取主 Agent 状态，因为当前会话身份缺失。"
	}
}

func localizedIMBtwResultPrefix(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "**/btw result**\n\n"
	case appLanguageZhHant:
		return "**/btw 查詢結果**\n\n"
	default:
		return "**/btw 查询结果**\n\n"
	}
}

// resetIMSessionForUser cancels the current loop before clearing all state that
// belongs to the conversation. It is shared by direct commands and busy-turn
// interrupts so /new always has identical reset semantics.
func (h *IMMessageHandler) resetIMSessionForUser(userID, responseLang string) *IMAgentResponse {
	// Reset must be an interrupting control action, not a message queued behind
	// a long-running tool call. Apply the full cancellation policy before
	// clearing state so a side-runner (/btw or /loop) cannot outlive the freshly
	// reset conversation.
	_ = h.cancelCurrentTaskForUser(userID, responseLang)
	if h.memory != nil {
		h.memory.Clear(userID)
	}
	h.clearPerUserSessionState(userID)
	h.flushEvidenceOnSessionEnd(userID)
	// Clear active goal on conversation reset — the user is starting fresh.
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.CancelPending(userID)
		h.getGoalStore().Clear(userID)
	}
	return &IMAgentResponse{Text: localizedIMConversationResetMessage(responseLang), ClearUI: true}
}

// cancelCurrentTaskForUser is the single cancellation policy for both normal
// IM command dispatch and gateway busy-turn interrupts. Keeping these paths
// together prevents /stop from cancelling only a LoopContext while leaving an
// active workflow, goal continuation, or side-runner alive.
func (h *IMMessageHandler) cancelCurrentTaskForUser(userID, responseLang string) *IMAgentResponse {
	h.cancelWorkflowForUser(userID)
	confirmationCancelled := false
	btwCancelled := false
	loopCancelled := false
	// Pause active goal on cancel (don't clear — user can resume later).
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.CancelPending(userID)
		if g := h.getGoalStore().Get(userID); g != nil && g.Status == goal.StatusActive {
			h.getGoalStore().Pause(userID, g.GoalID)
			log.Printf("[goal] paused on /cancel: user=%s goal_id=%s", userID, g.GoalID)
		}
	}
	if h.confirmationStore != nil {
		if pending := h.confirmationStore.get(userID); pending != nil {
			h.confirmationStore.clear(userID)
			confirmationCancelled = true
		}
	}
	if btw := h.activeBtwSubAgentForOwner(userID); btw != nil {
		btw.Cancel()
		btwCancelled = true
	}
	if loop := h.activeLoopCallbacksForOwner(userID); loop != nil {
		loop.Cancel()
		loopCancelled = true
	}
	taskText, err := h.RequestCancelSessionForUser(userID)
	if err == nil {
		return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "task", truncateRunes(taskText, 30))}
	}
	if loopCancelled {
		return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "loop", "")}
	}
	if btwCancelled {
		return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "btw", "")}
	}
	if confirmationCancelled {
		return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "confirmation", "")}
	}
	return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "none", "")}
}

func (h *IMMessageHandler) imCommandResponseLang(msgLang string) string {
	if strings.TrimSpace(msgLang) != "" {
		return msgLang
	}
	if h != nil && h.app != nil && strings.TrimSpace(h.app.CurrentLanguage) != "" {
		return h.app.CurrentLanguage
	}
	return "en"
}

// handleExitCommand terminates all active sessions, resets conversation
// memory, and returns the user to normal chat mode.
func (h *IMMessageHandler) handleExitCommand(userID, lang string) *IMAgentResponse {
	// /exit is a destructive session reset, so it must first interrupt every
	// owner-scoped operation. This also lets a busy IM gateway consume /exit
	// immediately instead of queuing it behind a long-running task.
	_ = h.cancelCurrentTaskForUser(userID, lang)
	var killed []string
	var failCount int
	if h.manager != nil {
		for _, s := range h.manager.List() {
			s.mu.RLock()
			active := isActiveRemoteSessionStatus(s.Status)
			sid := s.ID
			tool := s.Tool
			s.mu.RUnlock()
			if active {
				if err := h.manager.Kill(sid); err == nil {
					killed = append(killed, fmt.Sprintf("%s(%s)", sid, tool))
				} else {
					failCount++
				}
			}
		}
	}
	if h.memory != nil {
		h.memory.Clear(userID)
	}
	h.clearPerUserSessionState(userID)
	// Flush evidence batch and reset session on exit.
	h.flushEvidenceOnSessionEnd(userID)
	// Reset workflow working directory and suggest_maximize dedup flag.

	var b strings.Builder
	if len(killed) > 0 {
		b.WriteString(localizedIMExitStoppedMessage(lang, len(killed), strings.Join(killed, ", ")))
	} else {
		b.WriteString(localizedIMExitedCodingModeMessage(lang))
	}
	if failCount > 0 {
		b.WriteString("\n")
		b.WriteString(localizedIMExitFailureMessage(lang, failCount))
	}
	b.WriteString("\n")
	b.WriteString(localizedIMConversationResetFollowupMessage(lang))
	return &IMAgentResponse{Text: b.String(), ClearUI: true}
}

func localizedIMConversationResetMessage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Conversation reset."
	case appLanguageZhHant:
		return "對話已重置。"
	default:
		return "对话已重置。"
	}
}

func localizedIMConversationResetFollowupMessage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Conversation reset. Future messages will continue normally."
	case appLanguageZhHant:
		return "對話已重置。後續消息將正常繼續。"
	default:
		return "对话已重置。后续消息将正常继续。"
	}
}

func localizedIMExitedCodingModeMessage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Exited coding mode."
	case appLanguageZhHant:
		return "已退出編程模式。"
	default:
		return "已退出编程模式。"
	}
}

func localizedIMExitStoppedMessage(lang string, count int, sessions string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Exited coding mode. Stopped %d session(s): %s", count, sessions)
	case appLanguageZhHant:
		return fmt.Sprintf("已退出編程模式。已停止 %d 個會話：%s", count, sessions)
	default:
		return fmt.Sprintf("已退出编程模式。已停止 %d 个会话：%s", count, sessions)
	}
}

func localizedIMExitFailureMessage(lang string, count int) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("%d session(s) failed to stop and may need manual handling.", count)
	case appLanguageZhHant:
		return fmt.Sprintf("%d 個會話停止失敗，可能需要手動處理。", count)
	default:
		return fmt.Sprintf("%d 个会话停止失败，可能需要手动处理。", count)
	}
}

func localizedIMSlashHelpText(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Available commands:\n" +
			"/new /reset /clear - reset conversation\n" +
			"/btw <query> - side query\n" +
			"/moa [@preset] <prompt> - multi-model council (one-shot MoA)\n" +
			"/loop <verify_cmd> <goal> - goal-driven verification loop\n" +
			"    e.g. /loop \"go test ./...\" make all tests pass\n" +
			"    options: --max N (iterations), --timeout N (seconds), --dir path\n" +
			"/goal <objective> - persistent long-running autonomous goal\n" +
			"    e.g. /goal implement user login with JWT auth\n" +
			"    sub-commands: status, pause, resume, cancel\n" +
			"/workflow [type] - list or force-start a workflow\n" +
			installSlashHelpBlock(lang) +
			"/summary - (Lansenger group) summarize new group chat since last summary\n" +
			"/compress - compress conversation history\n" +
			"/memory - show memory status\n" +
			"/cancel /stop - cancel current task\n" +
			"/exit /quit - stop sessions\n" +
			"/sessions /status - show current sessions\n" +
			"/help - show this help"
	case appLanguageZhHant:
		return "可用命令：\n" +
			"/new /reset /clear - 重置對話\n" +
			"/btw <query> - 臨時旁路查詢\n" +
			"/moa [@方案] <提示> - 多模型會診（單次 MoA）\n" +
			"/loop <verify_cmd> <goal> - 目標驅動的驗證循環\n" +
			"    例：/loop \"go test ./...\" 讓所有測試通過\n" +
			"    選項：--max N（迭代次數），--timeout N（秒），--dir 路徑\n" +
			"/goal <目標> - 持久化長時間自主目標\n" +
			"    例：/goal 實現用戶登錄功能，包含JWT認證\n" +
			"    子命令：status, pause, resume, cancel\n" +
			"/workflow [類型] - 列出或強制啟動工作流\n" +
			installSlashHelpBlock(lang) +
			"/summary - （藍信群）摘要自上次以來的新群聊討論\n" +
			"/compress - 壓縮對話歷史\n" +
			"/memory - 查看記憶狀態\n" +
			"/cancel /stop - 取消當前任務\n" +
			"/exit /quit - 停止會話\n" +
			"/sessions /status - 查看當前會話\n" +
			"/help - 顯示此幫助"
	default:
		return "可用命令：\n" +
			"/new /reset /clear - 重置对话\n" +
			"/btw <query> - 临时旁路查询\n" +
			"/moa [@方案] <提示> - 多模型会诊（单次 MoA）\n" +
			"/loop <verify_cmd> <goal> - 目标驱动的验证循环\n" +
			"    例：/loop \"go test ./...\" 让所有测试通过\n" +
			"    选项：--max N（迭代次数），--timeout N（秒），--dir 路径\n" +
			"/goal <目标> - 持久化长时间自主目标\n" +
			"    例：/goal 实现用户登录功能，包含JWT认证\n" +
			"    子命令：status, pause, resume, cancel\n" +
			"/workflow [类型] - 列出或强制启动工作流\n" +
			installSlashHelpBlock(lang) +
			"/summary - （蓝信群）摘要自上次以来的新群聊讨论\n" +
			"/compress - 压缩对话历史\n" +
			"/memory - 查看记忆状态\n" +
			"/cancel /stop - 取消当前任务\n" +
			"/exit /quit - 停止会话\n" +
			"/sessions /status - 查看当前会话\n" +
			"/help - 显示此帮助"
	}
}

// moaPromptFromText strips the /moa prefix for one-shot prompts (shared parser).
// Empty for bare /moa, stats, sticky, or usage errors.
func moaPromptFromText(trimmed string) string {
	cmd := moa.ParseSlash(trimmed)
	if cmd.Kind == moa.SlashOneShot {
		return cmd.Prompt
	}
	return ""
}

func (h *IMMessageHandler) handleMoACommand(msg IMUserMessage, trimmed, lang string) *IMAgentResponse {
	cmd := moa.ParseSlash(trimmed)
	switch cmd.Kind {
	case moa.SlashHelp:
		return &IMAgentResponse{Text: localizedIMMoAUsageText(lang)}
	case moa.SlashUsage:
		return &IMAgentResponse{Text: localizedIMMoAAtPresetUsage(lang, cmd.Hint)}
	case moa.SlashStats:
		line := moa.FormatStatsLine()
		if line == "" {
			line = localizedIMMoAStatsEmpty(lang)
		}
		return &IMAgentResponse{Text: line}
	case moa.SlashSticky:
		return &IMAgentResponse{Text: localizedIMMoAStickyHint(lang)}
	case moa.SlashOneShot:
		if strings.TrimSpace(cmd.Prompt) == "" {
			return &IMAgentResponse{Text: localizedIMMoAUsageText(lang)}
		}
		if errText := h.tryArmMoAOneShotPreset(msg.UserID, lang, cmd.Preset); errText != "" {
			return &IMAgentResponse{Text: errText}
		}
		return &IMAgentResponse{Text: localizedIMMoAArmedHint(lang, cmd.Prompt)}
	default:
		return &IMAgentResponse{Text: localizedIMMoAUsageText(lang)}
	}
}

func localizedIMMoAArmedHint(lang, prompt string) string {
	display := prompt
	if r := []rune(display); len(r) > 120 {
		display = string(r[:120]) + "…"
	}
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "MoA is ready. Please send your question without the `/moa` prefix (or use + → multi-model council):\n" + display
	case appLanguageZhHant:
		return "多模型會診已就緒。請直接發送問題（不要帶 `/moa` 前綴），或使用輸入框 +「多模型會診」：\n" + display
	default:
		return "多模型会诊已就绪。请直接发送问题（不要带 `/moa` 前缀），或使用输入框 +「多模型会诊」：\n" + display
	}
}

func localizedIMMoAUsageText(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Usage:\n" +
			"  /moa <prompt>              one-shot multi-model (default preset)\n" +
			"  /moa @preset <prompt>      one-shot with a named preset\n" +
			"  /moa stats                 runtime counters\n\n" +
			"Examples:\n" +
			"  /moa review this migration plan for risks\n" +
			"  /moa @review compare approach A vs B for auth\n\n" +
			"Tip: + menu → multi-model, or sidebar sticky for this session."
	case appLanguageZhHant:
		return "用法：\n" +
			"  /moa <提示>                單次多模型會診（預設方案）\n" +
			"  /moa @方案名 <提示>        指定方案的單次會診\n" +
			"  /moa stats                 運行計數\n\n" +
			"示例：\n" +
			"  /moa 評估這份遷移方案的風險\n" +
			"  /moa @review 對比認證方案 A 與 B\n\n" +
			"提示：輸入框 + →「多模型會診」，或側欄開啟本會話常開。"
	default:
		return "用法：\n" +
			"  /moa <提示>                单次多模型会诊（默认方案）\n" +
			"  /moa @方案名 <提示>        指定方案的单次会诊\n" +
			"  /moa stats                 运行计数\n\n" +
			"示例：\n" +
			"  /moa 评估这份迁移方案的风险\n" +
			"  /moa @review 对比认证方案 A 与 B\n\n" +
			"提示：输入框 + →「多模型会诊」，或侧栏开启本会话常开。"
	}
}

func localizedIMMoAAtPresetUsage(lang, hint string) string {
	hint = strings.TrimSpace(hint)
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		msg := "Usage: /moa @preset <prompt>\nExample: /moa @review compare two auth designs"
		if hint != "" {
			msg = hint + "\n\n" + msg
		}
		return msg
	case appLanguageZhHant:
		msg := "用法：/moa @方案名 <提示>\n示例：/moa @review 對比兩種認證設計"
		if hint != "" {
			msg = hint + "\n\n" + msg
		}
		return msg
	default:
		msg := "用法：/moa @方案名 <提示>\n示例：/moa @review 对比两种认证设计"
		if hint != "" {
			msg = hint + "\n\n" + msg
		}
		return msg
	}
}

func localizedIMMoAStatsEmpty(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "No multi-model fan-outs recorded yet today."
	case appLanguageZhHant:
		return "今日尚無多模型會診計數。"
	default:
		return "今日尚无多模型会诊计数。"
	}
}

func localizedIMMoAStickyHint(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Session sticky multi-model: use the sidebar provider menu or LLM settings toggle.\n" +
			"One-shot: /moa <prompt> or /moa @preset <prompt>."
	case appLanguageZhHant:
		return "本會話常開多模型：請用側欄服務商選單或 LLM 設定中的開關。\n" +
			"單次會診：/moa <提示> 或 /moa @方案名 <提示>。"
	default:
		return "本会话常开多模型：请用侧栏服务商菜单或 LLM 设置中的开关。\n" +
			"单次会诊：/moa <提示> 或 /moa @方案名 <提示>。"
	}
}

func localizedIMMoANotReadyText(lang, prompt string) string {
	// Truncate long prompts in the status reply to keep the bubble readable.
	display := prompt
	if r := []rune(display); len(r) > 200 {
		display = string(r[:200]) + "…"
	}
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "MoA multi-model council entry is ready (composer / `/moa`), " +
			"but the MoA engine is not enabled yet (coming with the MoA PR plan).\n\n" +
			"Your prompt was:\n" + display + "\n\n" +
			"Resend without `/moa` to use the normal single-model agent for now."
	case appLanguageZhHant:
		return "多模型會診入口已就緒（輸入框 + 選單 / `/moa`），" +
			"但 MoA 引擎尚未接入（見設計文檔 PR 計劃）。\n\n" +
			"你提交的問題：\n" + display + "\n\n" +
			"目前可去掉 `/moa` 前綴，用普通單模型助手繼續。"
	default:
		return "多模型会诊入口已就绪（输入框 + 菜单 / `/moa`），" +
			"但 MoA 引擎尚未接入（见设计文档 PR 计划）。\n\n" +
			"你提交的问题：\n" + display + "\n\n" +
			"目前可去掉 `/moa` 前缀，用普通单模型助手继续。"
	}
}

func localizedIMBtwUsageText(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Usage: /btw <query>\n\nExamples:\n  /btw status\n  /btw latest Go changes\n  /btw React 19 main changes\n  /btw what framework does this project use"
	case appLanguageZhHant:
		return "用法：/btw <查詢>\n\n示例：\n  /btw status（查看主 Agent 狀態）\n  /btw 最新 Go 變化\n  /btw React 19 主要變化\n  /btw 這個專案使用什麼框架"
	default:
		return "用法：/btw <查询>\n\n示例：\n  /btw status（查看主 Agent 状态）\n  /btw 最新 Go 变化\n  /btw React 19 主要变化\n  /btw 这个项目使用什么框架"
	}
}

func localizedIMLLMNotConfiguredMessage(lang, command string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("LLM is not configured, so %s cannot run.", command)
	case appLanguageZhHant:
		return fmt.Sprintf("尚未配置 LLM，無法執行 %s。", command)
	default:
		return fmt.Sprintf("尚未配置 LLM，无法执行 %s。", command)
	}
}

func localizedIMCriticalSkillConfirmMessage(lang string, confirmed bool) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		if confirmed {
			return "Critical-risk Skill installation confirmed."
		}
		return "Critical-risk Skill installation rejected."
	case appLanguageZhHant:
		if confirmed {
			return "已確認安裝高風險 Skill。"
		}
		return "已拒絕安裝高風險 Skill。"
	default:
		if confirmed {
			return "已确认安装高风险 Skill。"
		}
		return "已拒绝安装高风险 Skill。"
	}
}

func localizedIMCancelMessage(lang, kind, preview string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		switch kind {
		case "confirmation":
			return "Pending confirmation canceled."
		case "btw":
			return "/btw side query canceled."
		case "loop":
			return "/loop command canceled."
		case "none":
			return "There is no active task to cancel."
		default:
			if preview != "" {
				return fmt.Sprintf("Task canceled: %s", preview)
			}
			return "Task canceled."
		}
	case appLanguageZhHant:
		switch kind {
		case "confirmation":
			return "已取消待確認操作。"
		case "btw":
			return "已取消 /btw 旁路查詢。"
		case "loop":
			return "已取消 /loop 命令。"
		case "none":
			return "目前沒有可取消的任務。"
		default:
			if preview != "" {
				return fmt.Sprintf("任務已取消：%s", preview)
			}
			return "任務已取消。"
		}
	default:
		switch kind {
		case "confirmation":
			return "已取消待确认操作。"
		case "btw":
			return "已取消 /btw 旁路查询。"
		case "loop":
			return "已取消 /loop 命令。"
		case "none":
			return "当前没有可取消的任务。"
		default:
			if preview != "" {
				return fmt.Sprintf("任务已取消：%s", preview)
			}
			return "任务已取消。"
		}
	}
}

// handleBtwCommand runs a /btw side query in an independent agent loop.
// The query runs with a minimal tool set (web_search, web_fetch, read_file,
// memory) and does not pollute the main conversation with intermediate steps.
//
// Concurrency: /btw runs outside the per-session loop mutex (by design — side queries should
// not block on the main loop). Results are NOT appended to the main history
// to avoid racing with a concurrent main loop's Save.
func (h *IMMessageHandler) handleBtwCommand(msg IMUserMessage, query string, onProgress tool.ProgressCallback, onToken llm.TokenCallback) *IMAgentResponse {
	if isBtwMainAgentStatusQuery(query) {
		return h.btwMainAgentStatusResponse(msg.UserID, h.imCommandResponseLang(msg.Lang))
	}
	responseLang := h.imCommandResponseLang(msg.Lang)
	cfg := h.getMaclawLLMConfig()
	httpClient := h.client

	btw := NewBtwSubAgent(h, cfg, httpClient, msg.UserID)
	btw.SetCallbacks(onToken, func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	})

	// Wire cancellation: store the SubAgent so /cancel can reach it.
	defer h.storeActiveBtwSubAgent(msg.UserID, btw)()

	result := btw.Execute(query)

	if result.Error != "" && result.Text == "" {
		return &IMAgentResponse{Error: localizedIMBtwFailedMessage(responseLang, result.Error)}
	}

	// NOTE: We intentionally do NOT append /btw results to the main
	// conversation history. Reasons:
	//
	// 1. If a main agent loop is running concurrently, its final Save()
	//    does a full replacement of the history 鈥?any Append we do here
	//    would be silently overwritten. Appending gives a false sense of
	//    persistence.
	//
	// 2. The desktop frontend manages its own message list (setMessages)
	//    and already displays the /btw result. The backend history is not
	//    the source of truth for the desktop panel's UI.
	//
	// 3. For IM channels, the /btw result is returned as IMAgentResponse.Text
	//    and delivered to the user. The next user message will trigger a
	//    fresh Load() that doesn't include the /btw exchange 鈥?this is
	//    acceptable because /btw is a side query, not part of the main task.
	//
	// If future requirements need /btw context in the main conversation,
	// the correct approach is to inject it as a system message at the start
	// of the next agent loop (similar to askUserContext), not to race with
	// the concurrent Save.

	log.Printf("[btw] completed query=%q iterations=%d tools=%d", truncateRunes(query, 50), result.Iterations, result.ToolCalls)

	return &IMAgentResponse{Text: result.Text}
}

func (h *IMMessageHandler) storeActiveBtwSubAgent(userID string, btw *BtwSubAgent) func() {
	if h == nil || btw == nil {
		return func() {}
	}
	ownerID := strings.TrimSpace(userID)
	if ownerID != "" {
		h.activeBtwSubAgents.Store(ownerID, btw)
	}
	h.activeBtwSubAgent.Store(btw)
	return func() {
		if ownerID != "" {
			if current, ok := h.activeBtwSubAgents.Load(ownerID); ok && current == btw {
				h.activeBtwSubAgents.Delete(ownerID)
			}
		}
		h.activeBtwSubAgent.CompareAndSwap(btw, nil)
	}
}

func (h *IMMessageHandler) activeBtwSubAgentForOwner(userID string) *BtwSubAgent {
	if h == nil {
		return nil
	}
	ownerID := strings.TrimSpace(userID)
	if ownerID != "" {
		if v, ok := h.activeBtwSubAgents.Load(ownerID); ok {
			if btw, _ := v.(*BtwSubAgent); btw != nil {
				return btw
			}
		}
	}
	if btw := h.activeBtwSubAgent.Load(); btw != nil && btw.OwnerID() == ownerID {
		return btw
	}
	return nil
}

// handleSessionsCommand returns a quick status summary of active sessions.
func (h *IMMessageHandler) handleSessionsCommand() *IMAgentResponse {
	return h.handleSessionsCommandWithLang("")
}

func (h *IMMessageHandler) handleSessionsCommandWithLang(lang string) *IMAgentResponse {
	if h.manager == nil {
		return &IMAgentResponse{Text: localizedIMSessionsManagerMissingMessage(lang)}
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return &IMAgentResponse{
			Text: localizedIMNoSessionsMessage(lang),
		}
	}
	var b strings.Builder
	b.WriteString(localizedIMCurrentSessionsHeader(lang, len(sessions)))
	for _, s := range sessions {
		s.mu.RLock()
		status := s.Status
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("- [%s] %s - %s", s.ID, s.Tool, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" | %s", task))
		}
		if waiting {
			b.WriteString(localizedIMWaitingForInputSuffix(lang))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(localizedIMExitSessionsHint(lang))
	return &IMAgentResponse{Text: b.String()}
}

func localizedIMBtwFailedMessage(lang, errText string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("/btw query failed: %s", errText)
	case appLanguageZhHant:
		return fmt.Sprintf("/btw 查詢失敗：%s", errText)
	default:
		return fmt.Sprintf("/btw 查询失败：%s", errText)
	}
}

func localizedIMSessionsManagerMissingMessage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Session manager is not initialized."
	case appLanguageZhHant:
		return "會話管理器尚未初始化。"
	default:
		return "会话管理器尚未初始化。"
	}
}

func localizedIMNoSessionsMessage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "There are no active sessions.\n\nTip: send /exit to leave coding mode and return to normal chat."
	case appLanguageZhHant:
		return "目前沒有活動會話。\n\n提示：發送 /exit 可退出編程模式並返回普通對話。"
	default:
		return "当前没有活动会话。\n\n提示：发送 /exit 可退出编程模式并返回普通对话。"
	}
}

func localizedIMCurrentSessionsHeader(lang string, count int) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Current sessions: %d\n", count)
	case appLanguageZhHant:
		return fmt.Sprintf("當前會話：%d\n", count)
	default:
		return fmt.Sprintf("当前会话：%d\n", count)
	}
}

func localizedIMWaitingForInputSuffix(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return " waiting for input"
	case appLanguageZhHant:
		return "，正在等待輸入"
	default:
		return "，正在等待输入"
	}
}

func localizedIMExitSessionsHint(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Send /exit to stop all sessions and leave coding mode."
	case appLanguageZhHant:
		return "發送 /exit 可停止全部會話並退出編程模式。"
	default:
		return "发送 /exit 可停止全部会话并退出编程模式。"
	}
}
