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
	h.memory.BindUnfinishedSlot(userID, slotID)
	log.Printf("[InFlightRecovery] recovered interrupted task for user %s: %q (project=%q)", userID, truncateRunes(interruptedTask, 80), interruptedProjectPath)
	return slot
}

func (h *IMMessageHandler) handleRecoverableSessionDecision(decision *explicitTaskSlotDecision) (*IMAgentResponse, bool, bool) {
	if h == nil || decision == nil || h.manager == nil {
		return nil, false, false
	}
	if decision.DismissRecoverableSessionID != "" {
		h.manager.SuppressResumeForSession(decision.DismissRecoverableSessionID)
		decision.ResumeRecoverableSessionID = ""
		return &IMAgentResponse{Text: "Recoverable session dismissed."}, true, true
	}
	if decision.ResumeRecoverableSessionID == "" {
		return nil, false, false
	}
	session, ok := h.manager.Get(decision.ResumeRecoverableSessionID)
	if ok && session != nil {
		var resumeSessionID, projectPath, tool string
		session.mu.RLock()
		if session.ResumeContext != nil {
			resumeSessionID = strings.TrimSpace(session.ResumeContext.ResumeSessionID)
			projectPath = strings.TrimSpace(firstNonEmptyTraceText(session.ProjectPath, session.ResumeContext.ProjectPath))
			tool = strings.TrimSpace(firstNonEmptyTraceText(session.Tool, session.ResumeContext.Tool))
		}
		session.mu.RUnlock()
		if resumeSessionID != "" && h.app != nil {
			_, err := h.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
				Tool:               tool,
				ProjectPath:        projectPath,
				LaunchSource:       RemoteLaunchSourceAI,
				ResumeSessionID:    resumeSessionID,
				InjectResumePrompt: false,
			})
			if err != nil {
				return &IMAgentResponse{Error: fmt.Sprintf("Recoverable session resume failed: %v", err)}, true, false
			}
			h.manager.SuppressResumeForSession(decision.ResumeRecoverableSessionID)
			return &IMAgentResponse{Text: "Recoverable session started. Check the remote session list for execution status."}, true, false
		}
	}
	return &IMAgentResponse{Error: "There is no recoverable session available, or the session does not support resume."}, true, false
}

func (h *IMMessageHandler) applyExplicitTaskSlotAction(msg *IMUserMessage, trimmed *string, decision explicitTaskSlotDecision, entries *[]agent.ConversationEntry, unfinishedSlot **agent.UnfinishedTaskSlot) (bool, *IMAgentResponse, bool) {
	if h == nil || msg == nil || trimmed == nil || entries == nil || unfinishedSlot == nil {
		return false, nil, false
	}
	if decision.StartNewTask {
		if len(*entries) >= 2 {
			h.archiveCurrentTask(msg.UserID, *entries, agent.ArchivedTaskStatusAbandoned)
		}
		h.memory.ClearConversationAndDismissSlot(msg.UserID)
		h.clearPerUserSessionState(msg.UserID)
		*entries = nil
		*unfinishedSlot = nil

		if msg.UIAction {
			savedText, hasSavedText := h.pendingSlotUserText.LoadAndDelete(msg.UserID)
			if hasSavedText {
				pending := savedText.(*pendingSlotText)
				if time.Since(pending.Timestamp) < 10*time.Minute {
					msg.Text = pending.Text
					msg.UIAction = false
					*trimmed = strings.TrimSpace(msg.Text)
					log.Printf("[TaskSlot] UI action for user %s: dismiss+replay original task %q", msg.UserID, truncateRunes(*trimmed, 80))
				} else {
					log.Printf("[TaskSlot] UI action for user %s: saved text expired (age=%v)", msg.UserID, time.Since(pending.Timestamp))
					hasSavedText = false
				}
			}
			if !hasSavedText {
				log.Printf("[TaskSlot] UI action for user %s: dismiss_slot:%s", msg.UserID, decision.DismissSlotID)
				return true, &IMAgentResponse{Text: "Previous task dismissed. Tell me the new task."}, true
			}
		}
		return true, nil, false
	}
	if decision.ResumeSlotID != "" {
		if h.memory.BindUnfinishedSlot(msg.UserID, decision.ResumeSlotID) {
			*unfinishedSlot = h.memory.ActiveUnfinishedSlot(msg.UserID)
		}
	}
	return false, nil, false
}

func (h *IMMessageHandler) maybeReturnUnfinishedSlotHint(msg IMUserMessage, trimmed string, freshTask bool, decision explicitTaskSlotDecision, unfinishedSlot *agent.UnfinishedTaskSlot) (*IMAgentResponse, bool) {
	if unfinishedSlot == nil || unfinishedSlot.Source.IsSessionExit() || msg.IsBackground || freshTask || isSlotActionCommand(trimmed) || decision.StartNewTask || decision.ResumeSlotID != "" {
		return nil, false
	}

	// Project path check: don't show an unfinished slot from a different
	// project. The slot is preserved in memory; switching back to the original
	// project will surface it again.
	currentProjectPath := h.getCurrentProjectPath()
	if unfinishedSlot.ProjectPath != "" && currentProjectPath != "" {
		if !strings.EqualFold(filepath.Clean(unfinishedSlot.ProjectPath), filepath.Clean(currentProjectPath)) {
			log.Printf("[UnfinishedSlot] suppressed: slot project=%q != current project=%q",
				unfinishedSlot.ProjectPath, currentProjectPath)
			return nil, false
		}
	}

	hint := buildUnfinishedSlotHint(unfinishedSlot)
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

	unfinishedTask := buildUnfinishedTaskPayload(unfinishedSlot)
	recoverableSession := (*IMResponseRecoverableSession)(nil)
	if h.manager != nil {
		for _, session := range h.manager.List() {
			if strings.TrimSpace(session.ProjectPath) != strings.TrimSpace(unfinishedSlot.ProjectPath) {
				continue
			}
			recoverableSession = buildRecoverableSessionPayload(session)
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

func buildUnfinishedSlotResumeContext(slot *agent.UnfinishedTaskSlot) string {
	if slot == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## 显式恢复未完成任务\n")
	if slot.LastTask != "" {
		b.WriteString("- 任务: ")
		b.WriteString(slot.LastTask)
		b.WriteString("\n")
	}
	if slot.Summary != "" {
		b.WriteString("- 当前进度: ")
		b.WriteString(slot.Summary)
		b.WriteString("\n")
	}
	if slot.ResumePrompt != "" {
		b.WriteString(slot.ResumePrompt)
		if !strings.HasSuffix(slot.ResumePrompt, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("用户已显式选择继续这个未完成任务。请仅围绕该任务继续，不要混入其他旧任务。\n")
	return b.String()
}

func buildResumeSlotActions(slot *agent.UnfinishedTaskSlot) []IMResponseAction {
	if slot == nil || strings.TrimSpace(slot.SlotID) == "" {
		return nil
	}
	return []IMResponseAction{
		{Label: "继续上次任务", Command: "__resume_unfinished__ " + slot.SlotID, Style: "default"},
		{Label: "Start new task", Command: "__dismiss_unfinished__ " + slot.SlotID, Style: "primary"},
	}
}

func buildUnfinishedSlotHint(slot *agent.UnfinishedTaskSlot) string {
	if slot == nil {
		return ""
	}
	title := strings.TrimSpace(firstNonEmptyTraceText(slot.LastTask, slot.Summary, slot.ProjectPath))
	if title == "" {
		title = "Previous unfinished task"
	}
	return "Detected an unfinished task: " + truncateRunes(title, 60) + ". Choose resume to continue it."
}

func isSlotActionCommand(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "__resume_unfinished__ ") || trimmed == "__start_new_task__" || strings.HasPrefix(trimmed, "__dismiss_unfinished__ ")
}
