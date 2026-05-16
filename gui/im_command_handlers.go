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
	if commandKind == imCommandReset {
		h.memory.Clear(msg.UserID)
		h.clearPerUserSessionState(msg.UserID)
		if h.confirmationStore != nil {
			h.confirmationStore.clear(msg.UserID)
		}
		h.flushEvidenceOnSessionEnd(msg.UserID)
		resp := &IMAgentResponse{Text: "Conversation reset.", ClearUI: true}
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
				if confirmed {
					return &IMAgentResponse{Text: "Critical-risk Skill installation confirmed."}, true
				}
				return &IMAgentResponse{Text: "Critical-risk Skill installation rejected."}, true
			}
		}
	}

	if !msg.IsBackground && len(msg.Attachments) == 0 && isShortChitChatMessage(trimmed) && !hasPendingAskUser {
		return &IMAgentResponse{Text: buildShortChitChatResponse(trimmed, msg.Lang)}, true
	}
	switch commandKind {
	case imCommandExit:
		return h.handleExitCommand(msg.UserID), true
	case imCommandSessions:
		return h.handleSessionsCommand(), true
	case imCommandCompress:
		return h.handleCompressCommand(msg.UserID), true
	case imCommandMemory:
		return h.handleMemoryStatusCommand(), true
	case imCommandHelp:
		return &IMAgentResponse{Text: "Available commands:\n" +
			"/new /reset /clear - reset conversation\n" +
			"/btw <query> - side query\n" +
			"/loop <verify_cmd> <goal> - goal-driven verification loop\n" +
			"    e.g. /loop \"go test ./...\" 让所有测试通过\n" +
			"    options: --max N (iterations), --timeout N (seconds), --dir path\n" +
			"/compress - compress conversation history\n" +
			"/memory - show memory status\n" +
			"/cancel - cancel current task\n" +
			"/exit /quit - stop sessions\n" +
			"/sessions /status - show current sessions\n" +
			"/help - show this help"}, true
	}
	switch commandKind {
	case imCommandBTW:
		btwQuery := ""
		if len(trimmed) > 5 {
			btwQuery = strings.TrimSpace(trimmed[5:])
		}
		if btwQuery == "" {
			return &IMAgentResponse{Text: "Usage: /btw <query>\n\nExamples:\n  /btw latest Go changes\n  /btw React 19 main changes\n  /btw what framework does this project use"}, true
		}
		if !h.isMaclawLLMConfigured() {
			return &IMAgentResponse{Error: "LLM is not configured, so /btw cannot run."}, true
		}
		return h.handleBtwCommand(msg, btwQuery, onProgress, onToken), true
	case imCommandLoop:
		if !h.isMaclawLLMConfigured() {
			return &IMAgentResponse{Error: "LLM is not configured. Cannot run /loop."}, true
		}
		return h.handleLoopCommand(msg, trimmed, onProgress, onToken), true
	}
	if commandKind == imCommandCancel {
		h.cancelWorkflowForUser(msg.UserID)
		if h.confirmationStore != nil {
			if pending := h.confirmationStore.get(msg.UserID); pending != nil {
				h.confirmationStore.clear(msg.UserID)
				return &IMAgentResponse{Text: "Pending confirmation canceled."}, true
			}
		}
		if btw := h.activeBtwSubAgent.Load(); btw != nil {
			btw.Cancel()
			return &IMAgentResponse{Text: "/btw side query canceled."}, true
		}
		if loop := h.activeLoopCallbacks.Load(); loop != nil {
			loop.Cancel()
			return &IMAgentResponse{Text: "/loop command canceled."}, true
		}
		ctx := h.currentLoopCtx
		if ctx == nil {
			return &IMAgentResponse{Text: "There is no active task to cancel."}, true
		}
		taskText := h.lastUserText
		ctx.Cancel()
		cancelMsg := "Task canceled."
		if preview := truncateRunes(taskText, 30); preview != "" {
			cancelMsg = fmt.Sprintf("Task canceled: %s", preview)
		}
		return &IMAgentResponse{Text: cancelMsg}, true
	}

	return nil, false
}

// handleExitCommand terminates all active sessions, resets conversation
// memory, and returns the user to normal chat mode.
func (h *IMMessageHandler) handleExitCommand(userID string) *IMAgentResponse {
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
		b.WriteString(fmt.Sprintf("Exited coding mode. Stopped %d session(s): %s", len(killed), strings.Join(killed, ", ")))
	} else {
		b.WriteString("Exited coding mode.")
	}
	if failCount > 0 {
		b.WriteString(fmt.Sprintf("\n%d session(s) failed to stop and may need manual handling.", failCount))
	}
	b.WriteString("\nConversation reset. Future messages will continue normally.")
	return &IMAgentResponse{Text: b.String(), ClearUI: true}
}

// handleBtwCommand runs a /btw side query in an independent agent loop.
// The query runs with a minimal tool set (web_search, web_fetch, read_file,
// memory) and does not pollute the main conversation with intermediate steps.
//
// Concurrency: /btw runs before chatLoopMu (by design 鈥?side queries should
// not block on the main loop). Results are NOT appended to the main history
// to avoid racing with a concurrent main loop's Save.
func (h *IMMessageHandler) handleBtwCommand(msg IMUserMessage, query string, onProgress tool.ProgressCallback, onToken llm.TokenCallback) *IMAgentResponse {
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
		return &IMAgentResponse{Error: fmt.Sprintf("/btw 鏌ヨ澶辫触: %s", result.Error)}
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
	if h.manager == nil {
		return &IMAgentResponse{Text: "Session manager is not initialized."}
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return &IMAgentResponse{
			Text: "There are no active sessions.\n\nTip: send /exit to leave coding mode and return to normal chat.",
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Current sessions: %d\n", len(sessions)))
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
			b.WriteString(" waiting for input")
		}
		b.WriteString("\n")
	}
	b.WriteString("\nSend /exit to stop all sessions and leave coding mode.")
	return &IMAgentResponse{Text: b.String()}
}
