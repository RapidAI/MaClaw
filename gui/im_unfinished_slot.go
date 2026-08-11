package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// pendingSlotText stores the user's original task text that was intercepted
// by the unfinished-slot hint. Expires after 10 minutes.
type pendingSlotText struct {
	Text      string
	Timestamp time.Time
}

// pendingCapabilityGapResult stores the outcome of an async capability gap
// resolution that ran in the background after the response was returned.
// If a skill was found and installed, the result is injected into the next
// conversation turn's system prompt so the LLM knows a new capability is
// available.
type pendingCapabilityGapResult struct {
	SkillName string
	Result    string // install/execute result text
	Success   bool   // true if skill was installed and executed successfully
	Timestamp time.Time
}

func hasIncompleteTaskMarker(entries []agent.ConversationEntry) bool {
	for i := len(entries) - 1; i >= 0; i-- {
		text, ok := entries[i].Content.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		if classifyIncompleteTaskMarker(trimmed).IsIncompleteTask() {
			return true
		}
	}
	return false
}

func shouldAutoClearIncompleteTaskContext(newMessage string, entries []agent.ConversationEntry) bool {
	if !hasIncompleteTaskMarker(entries) {
		return false
	}
	return shouldClearHistoryForIncompleteTask(newMessage)
}

// extractOriginalUserTask scans conversation history to find the first
// substantive user message (the original task request). This is used to
// populate the unfinished task slot when max rounds are reached.
func extractOriginalUserTask(history []agent.ConversationEntry) string {
	for _, e := range history {
		if e.Role != "user" {
			continue
		}
		text, ok := e.Content.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || shouldResumeIncompleteTask(trimmed) {
			continue
		}
		// Skip very short messages that are likely confirmations.
		if len([]rune(trimmed)) < 4 {
			continue
		}
		return truncateRunes(trimmed, 300)
	}
	return ""
}

// extractProgressSummary builds a brief summary of what the agent accomplished
// by scanning the last few assistant messages in the conversation history.
func extractProgressSummary(history []agent.ConversationEntry) string {
	var lastAssistantTexts []string
	for i := len(history) - 1; i >= 0 && len(lastAssistantTexts) < 3; i-- {
		if history[i].Role != "assistant" {
			continue
		}
		text, ok := history[i].Content.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		// Skip the max-rounds marker itself.
		if classifyIncompleteTaskMarker(trimmed).IsReasoningRoundMarker() {
			continue
		}
		lastAssistantTexts = append(lastAssistantTexts, truncateRunes(trimmed, 150))
	}
	if len(lastAssistantTexts) == 0 {
		return ""
	}
	// Reverse to chronological order.
	for i, j := 0, len(lastAssistantTexts)-1; i < j; i, j = i+1, j-1 {
		lastAssistantTexts[i], lastAssistantTexts[j] = lastAssistantTexts[j], lastAssistantTexts[i]
	}
	return strings.Join(lastAssistantTexts, " -> ")
}

type explicitTaskSlotDecision struct {
	ResumeSlotID                string
	StartNewTask                bool
	DismissSlotID               string
	ResumeRecoverableSessionID  string
	DismissRecoverableSessionID string
}

func resolveExplicitTaskSlotDecision(msg IMUserMessage, slot *agent.UnfinishedTaskSlot) explicitTaskSlotDecision {
	decision := explicitTaskSlotDecision{
		ResumeSlotID:                strings.TrimSpace(msg.ResumeSlotID),
		StartNewTask:                msg.StartNewTask,
		DismissSlotID:               strings.TrimSpace(msg.DismissSlotID),
		ResumeRecoverableSessionID:  strings.TrimSpace(msg.ResumeRecoverableSessionID),
		DismissRecoverableSessionID: strings.TrimSpace(msg.DismissRecoverableSessionID),
	}
	if decision.ResumeSlotID != "" && (slot == nil || slot.SlotID != decision.ResumeSlotID) {
		decision.ResumeSlotID = ""
	}
	return decision
}

func (h *IMMessageHandler) recoverInterruptedTaskSlot(userID string, entries []agent.ConversationEntry) *agent.UnfinishedTaskSlot {
	if h == nil || h.memory == nil {
		return nil
	}
	interruptedTask, interruptedProjectPath := h.memory.ConsumeInFlightTask(userID)
	if interruptedTask == "" {
		return nil
	}
	slotID := fmt.Sprintf("interrupted-%d", time.Now().UnixMilli())
	slot := &agent.UnfinishedTaskSlot{
		SlotID:       slotID,
		UserID:       userID,
		ProjectPath:  interruptedProjectPath,
		Status:       agent.UnfinishedTaskSlotStatusInterrupted,
		LastTask:     interruptedTask,
		Summary:      extractProgressSummary(entries),
		ResumePrompt: "A previous task was interrupted after tool-level progress. Resume from the saved context and continue the original task instead of starting over.",
		Source:       agent.UnfinishedTaskSlotSourceInFlightRecovery,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	h.memory.UpsertUnfinishedSlot(userID, slot)
	log.Printf("[InFlightRecovery] recovered interrupted task for user %s: %q (project=%q)", userID, truncateRunes(interruptedTask, 80), interruptedProjectPath)
	return slot
}

func (h *IMMessageHandler) handleRecoverableSessionDecision(decision *explicitTaskSlotDecision, lang string) (*IMAgentResponse, bool, bool) {
	if h == nil || decision == nil || h.manager == nil {
		return nil, false, false
	}
	if decision.DismissRecoverableSessionID != "" {
		h.manager.SuppressResumeForSession(decision.DismissRecoverableSessionID)
		decision.ResumeRecoverableSessionID = ""
		return &IMAgentResponse{Text: localizedRecoverableSessionDismissedMessage(lang)}, true, true
	}
	if decision.ResumeRecoverableSessionID == "" {
		return nil, false, false
	}
	session, ok := h.manager.Get(decision.ResumeRecoverableSessionID)
	if ok && session != nil {
		session.mu.RLock()
		hasResumeContext := session.ResumeContext != nil && strings.TrimSpace(session.ResumeContext.ResumeSessionID) != ""
		session.mu.RUnlock()
		if hasResumeContext {
			h.manager.SuppressResumeForSession(decision.ResumeRecoverableSessionID)
			return &IMAgentResponse{Text: localizedRecoverableSessionResumeDisabledMessage(lang)}, true, false
		}
	}
	return &IMAgentResponse{Error: localizedRecoverableSessionUnavailableMessage(lang)}, true, false
}

func localizedRecoverableSessionDismissedMessage(lang string) string {
	return unfinishedSlotText(lang,
		"Recoverable session dismissed.",
		"已忽略可恢复会话。",
		"已忽略可恢復會話。")
}

func localizedRecoverableSessionResumeDisabledMessage(lang string) string {
	return unfinishedSlotText(lang,
		"Recoverable external coding session resume is disabled. Coding work now runs through the internal CodingSubAgent; start the task again to continue with agent-managed coding.",
		"已禁用外部编码会话恢复。编码任务现在通过内部 CodingSubAgent 执行；请重新发起任务以继续由智能体管理的编码流程。",
		"已停用外部編碼會話恢復。編碼任務現在透過內部 CodingSubAgent 執行；請重新發起任務以繼續由智能體管理的編碼流程。")
}

func localizedRecoverableSessionUnavailableMessage(lang string) string {
	return unfinishedSlotText(lang,
		"There is no recoverable session available, or the session does not support resume.",
		"没有可恢复的会话，或该会话不支持恢复。",
		"沒有可恢復的會話，或該會話不支援恢復。")
}

func (h *IMMessageHandler) applyExplicitTaskSlotAction(msg *IMUserMessage, trimmed *string, decision explicitTaskSlotDecision, entries *[]agent.ConversationEntry, unfinishedSlot **agent.UnfinishedTaskSlot) (bool, *IMAgentResponse, bool) {
	if h == nil || msg == nil || trimmed == nil || entries == nil || unfinishedSlot == nil {
		return false, nil, false
	}
	if decision.StartNewTask {
		var savedPendingText *pendingSlotText
		if msg.UIAction {
			if savedText, ok := h.pendingSlotUserText.LoadAndDelete(msg.UserID); ok {
				if pending, ok := savedText.(*pendingSlotText); ok && pending != nil {
					savedPendingText = pending
				}
			}
		}
		if len(*entries) >= 2 {
			h.archiveCurrentTask(msg.UserID, *entries, agent.ArchivedTaskStatusAbandoned)
		}
		h.memory.ClearConversationAndDismissSlot(msg.UserID)
		h.clearPerUserSessionState(msg.UserID)
		*entries = nil
		*unfinishedSlot = nil

		if msg.UIAction {
			if savedPendingText != nil {
				pending := savedPendingText
				if time.Since(pending.Timestamp) < 10*time.Minute {
					msg.Text = pending.Text
					msg.UIAction = false
					*trimmed = strings.TrimSpace(msg.Text)
					log.Printf("[TaskSlot] UI action for user %s: dismiss+replay original task %q", msg.UserID, truncateRunes(*trimmed, 80))
				} else {
					log.Printf("[TaskSlot] UI action for user %s: saved text expired (age=%v)", msg.UserID, time.Since(pending.Timestamp))
					savedPendingText = nil
				}
			}
			if savedPendingText == nil {
				log.Printf("[TaskSlot] UI action for user %s: dismiss_slot:%s", msg.UserID, decision.DismissSlotID)
				return true, &IMAgentResponse{Text: localizedPreviousTaskDismissedMessage(msg.Lang)}, true
			}
		}
		return true, nil, false
	}
	if decision.ResumeSlotID != "" {
		// Runtime-backed recovery may have uncertain side effects. Probe before
		// binding the slot so no generic continuation machinery can treat the old
		// conversation as permission to replay it.
		if current := h.memory.GetUnfinishedSlot(msg.UserID); current != nil && current.SlotID == decision.ResumeSlotID && strings.TrimSpace(current.RuntimeTaskID) != "" {
			review, err := h.prepareCodingRuntimeRecoveryForSlot(current.RuntimeTaskID)
			if err != nil {
				return true, &IMAgentResponse{Error: "coding recovery probe failed: " + err.Error()}, true
			}
			return true, &IMAgentResponse{
				Text:                  "Coding task recovery probe completed. Review the workspace diff and explicitly confirm before a new attempt can be created.",
				CodingRuntimeRecovery: review,
			}, true
		}
		if h.memory.BindUnfinishedSlot(msg.UserID, decision.ResumeSlotID) {
			*unfinishedSlot = h.memory.ActiveUnfinishedSlot(msg.UserID)
		}
	}
	return false, nil, false
}

func localizedPreviousTaskDismissedMessage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Previous task dismissed. Tell me the new task."
	case appLanguageZhHant:
		return "已忽略上次未完成任務。請告訴我新的任務。"
	default:
		return "已忽略上次未完成任务。请告诉我新的任务。"
	}
}

func (h *IMMessageHandler) maybeReturnUnfinishedSlotHint(msg IMUserMessage, trimmed string, freshTask bool, decision explicitTaskSlotDecision, unfinishedSlot *agent.UnfinishedTaskSlot) (*IMAgentResponse, bool) {
	if unfinishedSlot == nil || !unfinishedSlotNeedsDecision(unfinishedSlot) || unfinishedSlot.Source.IsSessionExit() || unfinishedSlot.Source.IsAppExit() || msg.IsBackground || freshTask || isSlotActionCommand(trimmed) || decision.StartNewTask || decision.ResumeSlotID != "" {
		return nil, false
	}

	// Match against the same working directory used when creating slots
	// (effectiveWorkingDirForUser), not the Projects-list CurrentProject.
	currentProjectPath := h.effectiveWorkingDirForUser(msg.UserID)
	if !unfinishedSlotProjectMatchesCurrent(unfinishedSlot, currentProjectPath) {
		log.Printf("[UnfinishedSlot] suppressed: slot project=%q != current working dir=%q",
			unfinishedSlot.ProjectPath, currentProjectPath)
		return nil, false
	}

	hint := buildUnfinishedSlotHintWithLang(unfinishedSlot, msg.Lang)
	if hint == "" {
		return nil, false
	}

	// Save the user's original task text so it can be replayed after the user
	// clicks dismiss/start-new. Without this, the original task is silently
	// dropped and the user must re-type it.
	if trimmed != "" {
		h.pendingSlotUserText.Store(msg.UserID, &pendingSlotText{
			Text:      trimmed,
			Timestamp: time.Now(),
		})
	}

	unfinishedTask := buildUnfinishedTaskPayloadWithLang(unfinishedSlot, msg.Lang)
	recoverableSession := (*IMResponseRecoverableSession)(nil)
	if h.manager != nil {
		for _, session := range h.manager.List() {
			if strings.TrimSpace(session.ProjectPath) != strings.TrimSpace(unfinishedSlot.ProjectPath) {
				continue
			}
			recoverableSession = buildRecoverableSessionPayloadWithLang(session, msg.Lang)
			if recoverableSession != nil {
				break
			}
		}
	}

	return &IMAgentResponse{
		Text:               hint,
		UnfinishedTask:     unfinishedTask,
		UnfinishedSlot:     unfinishedTask,
		RecoverableSession: recoverableSession,
	}, true
}

// unfinishedSlotNeedsDecision distinguishes a recovery candidate from an
// already active continuation. Only the former should be offered as a choice.
func unfinishedSlotNeedsDecision(slot *agent.UnfinishedTaskSlot) bool {
	if slot == nil {
		return false
	}
	switch slot.Status {
	case agent.UnfinishedTaskSlotStatusResumed, agent.UnfinishedTaskSlotStatusCompleted:
		return false
	default:
		return true
	}
}

func buildUnfinishedSlotResumeContext(slot *agent.UnfinishedTaskSlot) string {
	lang, _ := agentViewCurrentLang.Load().(string)
	return buildUnfinishedSlotResumeContextWithLang(slot, lang)
}

func unfinishedSlotProjectMatchesCurrent(slot *agent.UnfinishedTaskSlot, currentProjectPath string) bool {
	if slot == nil {
		return false
	}
	slotProjectPath := strings.TrimSpace(slot.ProjectPath)
	currentProjectPath = strings.TrimSpace(currentProjectPath)
	if slotProjectPath == "" || currentProjectPath == "" {
		return true
	}
	return strings.EqualFold(filepath.Clean(slotProjectPath), filepath.Clean(currentProjectPath))
}

func buildUnfinishedSlotResumeContextWithLang(slot *agent.UnfinishedTaskSlot, lang string) string {
	if slot == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(unfinishedSlotText(lang,
		"\n## Explicit unfinished task resume\n",
		"\n## \u663e\u5f0f\u6062\u590d\u672a\u5b8c\u6210\u4efb\u52a1\n",
		"\n## \u986f\u5f0f\u6062\u5fa9\u672a\u5b8c\u6210\u4efb\u52d9\n"))
	if slot.LastTask != "" {
		b.WriteString(unfinishedSlotText(lang, "- Task: ", "- \u4efb\u52a1: ", "- \u4efb\u52d9: "))
		b.WriteString(slot.LastTask)
		b.WriteString("\n")
	}
	if slot.Summary != "" {
		b.WriteString(unfinishedSlotText(lang, "- Current progress: ", "- \u5f53\u524d\u8fdb\u5ea6: ", "- \u76ee\u524d\u9032\u5ea6: "))
		b.WriteString(localizedUnfinishedSlotSummary(slot.Summary, lang))
		b.WriteString("\n")
	}
	if slot.ResumePrompt != "" {
		b.WriteString(slot.ResumePrompt)
		if !strings.HasSuffix(slot.ResumePrompt, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString(unfinishedSlotText(lang,
		"User explicitly chose to continue this unfinished task. Continue only that task; do not mix in other old tasks.\n",
		"\u7528\u6237\u5df2\u663e\u5f0f\u9009\u62e9\u7ee7\u7eed\u8fd9\u4e2a\u672a\u5b8c\u6210\u4efb\u52a1\u3002\u8bf7\u4ec5\u56f4\u7ed5\u8be5\u4efb\u52a1\u7ee7\u7eed\uff0c\u4e0d\u8981\u6df7\u5165\u5176\u4ed6\u65e7\u4efb\u52a1\u3002\n",
		"\u4f7f\u7528\u8005\u5df2\u986f\u5f0f\u9078\u64c7\u7e7c\u7e8c\u9019\u500b\u672a\u5b8c\u6210\u4efb\u52d9\u3002\u8acb\u50c5\u570d\u7e5e\u8a72\u4efb\u52d9\u7e7c\u7e8c\uff0c\u4e0d\u8981\u6df7\u5165\u5176\u4ed6\u820a\u4efb\u52d9\u3002\n"))
	return b.String()
}

func buildResumeSlotActions(slot *agent.UnfinishedTaskSlot) []IMResponseAction {
	lang, _ := agentViewCurrentLang.Load().(string)
	return buildResumeSlotActionsWithLang(slot, lang)
}

func buildResumeSlotActionsWithLang(slot *agent.UnfinishedTaskSlot, lang string) []IMResponseAction {
	if slot == nil || strings.TrimSpace(slot.SlotID) == "" {
		return nil
	}
	return []IMResponseAction{
		{Label: unfinishedSlotText(lang, "Resume previous task", "\u7ee7\u7eed\u4e0a\u6b21\u4efb\u52a1", "\u7e7c\u7e8c\u4e0a\u6b21\u4efb\u52d9"), Command: "__resume_unfinished__ " + slot.SlotID, Style: "default"},
		{Label: unfinishedSlotText(lang, "Start new task", "\u5f00\u59cb\u65b0\u4efb\u52a1", "\u958b\u59cb\u65b0\u4efb\u52d9"), Command: "__dismiss_unfinished__ " + slot.SlotID, Style: "primary"},
	}
}

func buildUnfinishedSlotHint(slot *agent.UnfinishedTaskSlot) string {
	lang, _ := agentViewCurrentLang.Load().(string)
	return buildUnfinishedSlotHintWithLang(slot, lang)
}

func buildUnfinishedSlotHintWithLang(slot *agent.UnfinishedTaskSlot, lang string) string {
	if slot == nil {
		return ""
	}
	title := strings.TrimSpace(firstNonEmptyTraceText(slot.LastTask, slot.Summary, slot.ProjectPath))
	if title == "" {
		title = unfinishedSlotText(lang, "Previous unfinished task", "\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1", "\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52d9")
	}
	title = localizedUnfinishedSlotSummary(title, lang)
	title = truncateRunes(title, 60)
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Detected an unfinished task: " + title + ". Choose resume to continue it."
	case appLanguageZhHant:
		return "偵測到未完成任務：" + title + "。選擇「繼續上次任務」可繼續。"
	default:
		return "\u68c0\u6d4b\u5230\u672a\u5b8c\u6210\u4efb\u52a1\uff1a" + title + "\u3002\u9009\u62e9\u201c\u7ee7\u7eed\u4e0a\u6b21\u4efb\u52a1\u201d\u53ef\u7ee7\u7eed\u3002"
	}
}

func unfinishedSlotText(lang, en, zhHans, zhHant string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return en
	case appLanguageZhHant:
		return zhHant
	default:
		return zhHans
	}
}

func isSlotActionCommand(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "__resume_unfinished__ ") || trimmed == "__start_new_task__" || strings.HasPrefix(trimmed, "__dismiss_unfinished__ ")
}
