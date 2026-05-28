package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) handleImmediateIMCommand(msg IMUserMessage, trimmed string, onProgress tool.ProgressCallback, onToken llm.TokenCallback) (*IMAgentResponse, bool) {
	commandKind := classifyImmediateIMCommand(trimmed)
	responseLang := h.imCommandResponseLang(msg.Lang)
	if commandKind == imCommandReset {
		h.memory.Clear(msg.UserID)
		h.clearPerUserSessionState(msg.UserID)
		if h.confirmationStore != nil {
			h.confirmationStore.clear(msg.UserID)
		}
		h.flushEvidenceOnSessionEnd(msg.UserID)
		resp := &IMAgentResponse{Text: localizedIMConversationResetMessage(responseLang), ClearUI: true}
		if h.currentLoopCtx != nil {
			return h.finalizeTraceResult(h.currentLoopCtx, resp, resp.Text, ""), true
		}
		return resp, true
	}

	hasPendingAskUser := false
	if raw, loaded := h.pendingAskUser.Load(msg.UserID); loaded {
		_, hasPendingAskUser = pendingAskUserForCurrentHistory(raw, h.memory.Load(msg.UserID))
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

	if !msg.IsBackground && len(msg.Attachments) == 0 && isShortChitChatMessage(trimmed) && !hasPendingAskUser {
		return &IMAgentResponse{Text: buildShortChitChatResponse(trimmed, msg.Lang)}, true
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
		btwQuery := ""
		if len(trimmed) > 5 {
			btwQuery = strings.TrimSpace(trimmed[5:])
		}
		if btwQuery == "" {
			return &IMAgentResponse{Text: localizedIMBtwUsageText(responseLang)}, true
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
	}
	if commandKind == imCommandCancel {
		h.cancelWorkflowForUser(msg.UserID)
		if h.confirmationStore != nil {
			if pending := h.confirmationStore.get(msg.UserID); pending != nil {
				h.confirmationStore.clear(msg.UserID)
				return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "confirmation", "")}, true
			}
		}
		if btw := h.activeBtwSubAgent.Load(); btw != nil {
			btw.Cancel()
			return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "btw", "")}, true
		}
		if loop := h.activeLoopCallbacks.Load(); loop != nil {
			loop.Cancel()
			return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "loop", "")}, true
		}
		ctx := h.getSessionLoopCtx(msg.UserID)
		taskText := h.sessionLoopTaskText(msg.UserID)
		if ctx == nil {
			h.globalLoopMu.RLock()
			if h.lastUserID == msg.UserID {
				ctx = h.currentLoopCtx
				taskText = h.lastUserText
			}
			h.globalLoopMu.RUnlock()
		}
		if ctx == nil {
			return &IMAgentResponse{Text: localizedIMCancelMessage(responseLang, "none", "")}, true
		}
		h.markTaskCancelledByUser(msg.UserID)
		ctx.Cancel()
		cancelMsg := localizedIMCancelMessage(responseLang, "task", truncateRunes(taskText, 30))
		return &IMAgentResponse{Text: cancelMsg}, true
	}

	return nil, false
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
	h.memory.Clear(userID)
	h.clearPerUserSessionState(userID)
	// Flush evidence batch and reset session on exit.
	h.flushEvidenceOnSessionEnd(userID)
	// Reset workflow working directory and suggest_maximize dedup flag.
	if h.getWorkflowEngine() != nil {
		if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
			adapter.ResetWorkingDir()
			adapter.ResetSuggestMaximize(userID)
		}
	}

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
			"/loop <verify_cmd> <goal> - goal-driven verification loop\n" +
			"    e.g. /loop \"go test ./...\" make all tests pass\n" +
			"    options: --max N (iterations), --timeout N (seconds), --dir path\n" +
			"/compress - compress conversation history\n" +
			"/memory - show memory status\n" +
			"/cancel - cancel current task\n" +
			"/exit /quit - stop sessions\n" +
			"/sessions /status - show current sessions\n" +
			"/help - show this help"
	case appLanguageZhHant:
		return "可用命令：\n" +
			"/new /reset /clear - 重置對話\n" +
			"/btw <query> - 臨時旁路查詢\n" +
			"/loop <verify_cmd> <goal> - 目標驅動的驗證循環\n" +
			"    例：/loop \"go test ./...\" 讓所有測試通過\n" +
			"    選項：--max N（迭代次數），--timeout N（秒），--dir 路徑\n" +
			"/compress - 壓縮對話歷史\n" +
			"/memory - 查看記憶狀態\n" +
			"/cancel - 取消當前任務\n" +
			"/exit /quit - 停止會話\n" +
			"/sessions /status - 查看當前會話\n" +
			"/help - 顯示此幫助"
	default:
		return "可用命令：\n" +
			"/new /reset /clear - 重置对话\n" +
			"/btw <query> - 临时旁路查询\n" +
			"/loop <verify_cmd> <goal> - 目标驱动的验证循环\n" +
			"    例：/loop \"go test ./...\" 让所有测试通过\n" +
			"    选项：--max N（迭代次数），--timeout N（秒），--dir 路径\n" +
			"/compress - 压缩对话历史\n" +
			"/memory - 查看记忆状态\n" +
			"/cancel - 取消当前任务\n" +
			"/exit /quit - 停止会话\n" +
			"/sessions /status - 查看当前会话\n" +
			"/help - 显示此帮助"
	}
}

func localizedIMBtwUsageText(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Usage: /btw <query>\n\nExamples:\n  /btw latest Go changes\n  /btw React 19 main changes\n  /btw what framework does this project use"
	case appLanguageZhHant:
		return "用法：/btw <查詢>\n\n示例：\n  /btw 最新 Go 變化\n  /btw React 19 主要變化\n  /btw 這個專案使用什麼框架"
	default:
		return "用法：/btw <查询>\n\n示例：\n  /btw 最新 Go 变化\n  /btw React 19 主要变化\n  /btw 这个项目使用什么框架"
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
// Concurrency: /btw runs before chatLoopMu (by design 鈥?side queries should
// not block on the main loop). Results are NOT appended to the main history
// to avoid racing with a concurrent main loop's Save.
func (h *IMMessageHandler) handleBtwCommand(msg IMUserMessage, query string, onProgress tool.ProgressCallback, onToken llm.TokenCallback) *IMAgentResponse {
	responseLang := h.imCommandResponseLang(msg.Lang)
	cfg := h.getMaclawLLMConfig()
	httpClient := h.client

	btw := NewBtwSubAgent(h, cfg, httpClient)
	btw.SetCallbacks(onToken, func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	})

	// Wire cancellation: store the SubAgent so /cancel can reach it.
	h.activeBtwSubAgent.Store(btw)
	defer h.activeBtwSubAgent.Store((*BtwSubAgent)(nil))

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
