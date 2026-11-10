package guiapp

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
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
	recovered := h.memory.ConsumeInFlightRecovery(userID)
	if strings.TrimSpace(recovered.Task) == "" {
		return nil
	}
	now := time.Now()
	slotID := fmt.Sprintf("interrupted-%d", now.UnixMilli())
	slot := &agent.UnfinishedTaskSlot{
		SlotID:                   slotID,
		UserID:                   userID,
		ProjectPath:              recovered.ProjectPath,
		Status:                   agent.UnfinishedTaskSlotStatusInterrupted,
		LastTask:                 recovered.Task,
		Summary:                  extractProgressSummary(entries),
		ResumePrompt:             "A previous task was interrupted after tool-level progress. Resume from the saved context and continue the original task instead of starting over.",
		Source:                   agent.UnfinishedTaskSlotSourceInFlightRecovery,
		EvidenceScopeKey:         recovered.EvidenceScopeKey,
		LastCheckpointAt:         recovered.SetAt,
		LastToolName:             recovered.LastToolName,
		SideEffectState:          recovered.SideEffectState,
		RecoveryMode:             recovered.RecoveryMode,
		MailboxResultBaseline:    recovered.MailboxResultBaseline,
		MailboxResultBaselineSet: recovered.MailboxResultBaselineSet,
		MailboxUncertainDropped:  recovered.MailboxUncertainDropped,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	if !recovered.MailboxUncertainDropped && agent.MailboxLatchSettled(recovered.LastToolName, entries, recovered.MailboxResultBaseline, recovered.MailboxResultBaselineSet) {
		// The pre-tool latch outlived a host-known non-delivery. Creating a
		// review slot here would refuse the next message before the model can
		// send again. The marker is already consumed.
		log.Printf("[InFlightRecovery] skipped mailbox review slot user=%s tool=%s", userID, recovered.LastToolName)
		return nil
	}
	h.memory.UpsertUnfinishedSlot(userID, slot)
	log.Printf("[InFlightRecovery] recovered interrupted task for user %s: %q (project=%q)", userID, truncateRunes(recovered.Task, 80), recovered.ProjectPath)
	return slot
}

// resumedSlotSettleGrace is how long a just-bound resume stays visible as
// interrupted. Bind happens at the start of the turn, before the loop is
// marked running, and a pause in that gap never reaches turn settlement.
const resumedSlotSettleGrace = 2 * time.Minute

// resumedSlotNeedsReview reports a resumed slot the task list and turn
// settlement must leave alone. A coding recovery still needs its probe, and
// an unreviewed external send is not permission to continue.
func resumedSlotNeedsReview(slot *agent.UnfinishedTaskSlot) bool {
	if slot == nil {
		return false
	}
	if strings.TrimSpace(slot.RuntimeTaskID) != "" {
		return true
	}
	return unreviewedExternalDeliverySlot(slot)
}

// resumedUnfinishedTurnSettled reports a turn that finished on its own.
// A hard exit, an error, or a user stop is the continuation that did not
// finish, and that same continuation has to stay visible as interrupted.
func resumedUnfinishedTurnSettled(loopCtx *LoopContext, resp *IMAgentResponse) bool {
	if resp == nil || strings.TrimSpace(resp.Error) != "" || resp.HardExit || resp.userStopped {
		return false
	}
	// User stop sets UserCancelled. A pre-loop pause and a parent cancel only
	// close the loop. Both are unfinished continuations.
	return loopCtx == nil || (!loopCtx.UserCancelled() && !loopCtx.IsCancelled())
}

// slotResumedOnThisTurn reports a bind that belongs to the loop now ending.
// Bind runs just before the loop is marked started. A resumed slot from an
// earlier turn is older than that, and a later failure must not put it back
// on 已中断.
func slotResumedOnThisTurn(slot *agent.UnfinishedTaskSlot, loopCtx *LoopContext) bool {
	if slot == nil || slot.BoundAt.IsZero() {
		return false
	}
	if loopCtx == nil || loopCtx.StartedAt.IsZero() {
		return time.Since(slot.BoundAt) < resumedSlotSettleGrace
	}
	return !slot.BoundAt.Before(loopCtx.StartedAt.Add(-resumedSlotSettleGrace))
}

// settleResumedUnfinishedSlot retires the continuation the user already
// accepted. A clean finish drops the slot so the task list can show the
// finished outcome, including a resume that finished before this process
// learned to retire it. A turn that stops short puts only the slot it just
// bound back to interrupted. A coding recovery review and an unreviewed
// external send are not this slot.
func (h *IMMessageHandler) settleResumedUnfinishedSlot(userID string, loopCtx *LoopContext, resp *IMAgentResponse) {
	if h == nil || h.memory == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	slot := h.memory.GetUnfinishedSlot(userID)
	// The list predicate also keeps a just-bound resume visible. Settlement
	// still has to retire that slot when the turn ends.
	if slot == nil || slot.Status != agent.UnfinishedTaskSlotStatusResumed || resumedSlotNeedsReview(slot) {
		return
	}
	if resumedUnfinishedTurnSettled(loopCtx, resp) {
		h.memory.CompleteUnfinishedSlot(userID, slot.SlotID)
		log.Printf("[UnfinishedSlot] completed resumed slot after successful turn user=%s slot=%s", userID, slot.SlotID)
		return
	}
	if !slotResumedOnThisTurn(slot, loopCtx) {
		return
	}
	slot.Status = agent.UnfinishedTaskSlotStatusInterrupted
	h.memory.UpsertUnfinishedSlot(userID, slot)
	h.memory.ClearActiveSlot(userID)
	log.Printf("[UnfinishedSlot] reopened resumed slot after unfinished turn user=%s slot=%s", userID, slot.SlotID)
}

// abandonResumedUnfinishedSlot records that this turn stopped before the work
// finished. The sentence shown to the user can be ordinary text, and reading
// that text would retire the slot as a completed continuation.
func (h *IMMessageHandler) abandonResumedUnfinishedSlot(userID string, loopCtx *LoopContext) {
	h.settleResumedUnfinishedSlot(userID, loopCtx, &IMAgentResponse{userStopped: true})
}

// releaseSettledMailboxReviewSlot drops a mailbox latch whose saved results
// are already known. History stays. A confirmation card is not a delivery.
// An uncertain result, a missing result, a dropped transport-uncertain row,
// and im_message / send_file / send_to_im stay latched. resume_context is
// released too, so a known mailbox result does not become the generic
// unfinished card. Completing the current slot promotes the next queued
// slot. That slot stays on this message when it still needs a decision.
// Another settled mailbox slot is released in the same pass.
func (h *IMMessageHandler) releaseSettledMailboxReviewSlot(userID string, slot *agent.UnfinishedTaskSlot, entries []agent.ConversationEntry) *agent.UnfinishedTaskSlot {
	if h == nil || h.memory == nil {
		return slot
	}
	seen := map[string]struct{}{}
	for {
		if slot == nil || slot.Status == agent.UnfinishedTaskSlotStatusCompleted || !mailboxReviewSlotSettled(slot, entries) {
			return slot
		}
		id := strings.TrimSpace(slot.SlotID)
		if _, dup := seen[id]; dup {
			return slot
		}
		seen[id] = struct{}{}
		h.memory.CompleteUnfinishedSlot(userID, slot.SlotID)
		log.Printf("[InFlightRecovery] released settled mailbox review slot user=%s slot=%s tool=%s", userID, slot.SlotID, slot.LastToolName)
		slot = h.memory.GetUnfinishedSlot(userID)
	}
}

func mailboxReviewSlotSettled(slot *agent.UnfinishedTaskSlot, entries []agent.ConversationEntry) bool {
	if slot == nil || slot.Status == agent.UnfinishedTaskSlotStatusCompleted {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(slot.LastToolName)) {
	case "mailbox_send", "mailbox_delete":
	default:
		return false
	}
	if slot.MailboxUncertainDropped {
		return false
	}
	return agent.MailboxLatchSettled(slot.LastToolName, entries, slot.MailboxResultBaseline, slot.MailboxResultBaselineSet)
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
		current := (*agent.UnfinishedTaskSlot)(nil)
		if h.memory != nil {
			current = h.memory.GetUnfinishedSlot(msg.UserID)
		}
		if current != nil && current.SlotID == decision.ResumeSlotID && strings.TrimSpace(current.RuntimeTaskID) != "" {
			review, err := h.prepareCodingRuntimeRecoveryForSlot(current.RuntimeTaskID)
			if err != nil {
				return true, &IMAgentResponse{Error: "coding recovery probe failed: " + err.Error()}, true
			}
			return true, &IMAgentResponse{
				Text:                  "Coding task recovery probe completed. Review the workspace diff and explicitly confirm before a new attempt can be created.",
				CodingRuntimeRecovery: review,
			}, true
		}
		// An unreviewed external send is evidence, not a grant to plan
		// another delivery. Binding would mark the slot resumed and hide
		// the decision, so this turn stops here.
		if current != nil && current.SlotID == decision.ResumeSlotID && unreviewedExternalDeliverySlot(current) {
			return true, h.returnExternalDeliveryReview(*msg, current), true
		}
		if h.memory != nil && h.memory.BindUnfinishedSlot(msg.UserID, decision.ResumeSlotID) {
			*unfinishedSlot = h.memory.ActiveUnfinishedSlot(msg.UserID)
		}
	}
	return false, nil, false
}

func localizedPreviousTaskDismissedMessage(lang string) string {
	return pickUI(lang, "Previous task dismissed. Tell me the new task.", "已忽略上次未完成任务。请告诉我新的任务。", "已忽略上次未完成任務。請告訴我新的任務。")
}

func (h *IMMessageHandler) maybeReturnUnfinishedSlotHint(msg IMUserMessage, trimmed string, freshTask bool, decision explicitTaskSlotDecision, unfinishedSlot *agent.UnfinishedTaskSlot) (*IMAgentResponse, bool) {
	if unfinishedSlot == nil || !unfinishedSlotNeedsDecision(unfinishedSlot) || unfinishedSlot.Source.IsSessionExit() || unfinishedSlot.Source.IsAppExit() || msg.IsBackground || freshTask || isSlotActionCommand(trimmed) || decision.StartNewTask || decision.ResumeSlotID != "" {
		return nil, false
	}

	// An uncertain external send is not workspace-local. A different working
	// directory must not drop this block and let the turn plan another send.
	if unreviewedExternalDeliverySlot(unfinishedSlot) {
		return h.returnExternalDeliveryReview(msg, unfinishedSlot), true
	}

	// The recorded directory and the directory tools would use can disagree
	// after a crash: an expert task's chosen folder was replaced by the
	// process default, and the checkpoint still names one of them. Dropping
	// the choice here lets the text into the tool loop, and the pending slot
	// then refuses the next checkpoint. Keep the card. A confident reading of
	// the text already resumed or dismissed the slot before this point.
	currentProjectPath := h.effectiveWorkingDirForUser(msg.UserID)
	if !recoveredSlotMatchesSession(msg.UserID, unfinishedSlot, currentProjectPath) {
		log.Printf("[UnfinishedSlot] working dir differs slot project=%q current=%q; keeping the resume choice",
			unfinishedSlot.ProjectPath, currentProjectPath)
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
	// Crash recovery metadata is evidence, not a replay plan. Put the safety
	// boundary in the model context as well as the GUI card: a recovered history
	// may not contain the unpaired tool call that was in flight when the process
	// exited, so relying only on the transcript would hide that uncertainty.
	if toolName := safeRecoveryToolName(slot.LastToolName); toolName != "" {
		b.WriteString(fmt.Sprintf(unfinishedSlotText(lang,
			"- Interrupted tool evidence: `%s` may have started, but its final state is unknown. Do not repeat it merely because this task was recovered.\n",
			"- 中断工具证据：`%s` 可能已开始执行，但最终状态未知。不要因为任务已恢复而直接重复该工具。\n",
			"- 中斷工具證據：`%s` 可能已開始執行，但最終狀態未知。不要因為任務已恢復而直接重複該工具。\n"), toolName))
	}
	if strings.TrimSpace(slot.RecoveryMode) == "requires_review" {
		b.WriteString(unfinishedSlotText(lang,
			"- Recovery safety: prior work may already have changed the workspace or an external system. Inspect the current state before attempting a new mutation or external action.\n",
			"- 恢复安全：此前工作可能已经修改了工作区或外部系统。再次执行本地修改或外部操作前，先检查当前状态。\n",
			"- 恢復安全：此前工作可能已經修改了工作區或外部系統。再次執行本機修改或外部操作前，先檢查目前狀態。\n"))
	}
	b.WriteString(unfinishedSlotText(lang,
		"Continue only this unfinished task; do not mix in other old tasks. Inspect the current files and progress first. Do not redo finished work, and do not replay an interrupted tool just because the task was recovered.\n",
		"这一轮只继续这个未完成任务，不要混入其他旧任务。先看当前文件和已有进度，不要重做已经完成的部分，也不要因为任务被恢复就重放中断时的工具。\n",
		"這一輪只繼續這個未完成任務，不要混入其他舊任務。先看目前檔案和已有進度，不要重做已經完成的部分，也不要因為任務被恢復就重放中斷時的工具。\n"))
	return b.String()
}

// safeRecoveryToolName prevents provider-controlled tool names from becoming
// arbitrary instruction text when recovery evidence is appended to the system
// prompt. Tool names are identifiers, so retain only a small identifier set.
func safeRecoveryToolName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > 80 {
		return ""
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return ""
		}
	}
	return name
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
	return pickUI(lang, "Detected an unfinished task: ", "\u68c0\u6d4b\u5230\u672a\u5b8c\u6210\u4efb\u52a1\uff1a", "偵測到未完成任務：") + title + pickUI(lang,
		". Use the buttons, or type what you want. That message is read as continuing this task or starting a new one.",
		"。可以点按钮，也可以直接输入。系统会根据这句话判断是继续上次任务，还是开始新任务。",
		"。可以點按鈕，也可以直接輸入。系統會根據這句話判斷是繼續上次任務，還是開始新任務。")
}

// recoveredSlotMatchesSession reports whether a pending recovery slot belongs
// to the conversation that just received a message. An expert session is one
// conversation, and its checkpoint may still name the process default from
// before the task directory was attached. A project-tab session is also one
// task: after a restart its live directory can fall back to the process
// default while the slot still names the task directory, or the reverse.
// The task directory and its workspace sandbox are the same task. A slot
// for a different task directory is not. An expert session, and a desktop
// session with no project, still refuse two chosen directories.
func recoveredSlotMatchesSession(userID string, slot *agent.UnfinishedTaskSlot, currentProjectPath string) bool {
	if unfinishedSlotProjectMatchesCurrent(slot, currentProjectPath) {
		return true
	}
	if slot == nil {
		return false
	}
	if expertIDFromUserID(userID) != "" && (isProcessDefaultWorkspacePath(slot.ProjectPath) || isProcessDefaultWorkspacePath(currentProjectPath)) {
		return true
	}
	sessionProject := projectPathFromSessionOwnerID(userID)
	if sessionProject == "" {
		return false
	}
	slotPath := normalizeProjectSessionPath(slot.ProjectPath)
	current := normalizeProjectSessionPath(currentProjectPath)
	// The slot is this session when it names the task directory or that
	// directory's workspace sandbox. The live directory may be either of
	// those, or the process default after a restart. A tagged work root
	// can sit outside both; the slot still belongs to this session.
	if sameNormalizedPath(slotPath, sessionProject) || sameTaskWorkspace(slotPath, sessionProject) {
		return true
	}
	if isProcessDefaultWorkspacePath(slotPath) && (current == "" || sameNormalizedPath(current, sessionProject) || sameTaskWorkspace(current, sessionProject) || isProcessDefaultWorkspacePath(current)) {
		return true
	}
	if isProcessDefaultWorkspacePath(current) && (slotPath == "" || sameNormalizedPath(slotPath, sessionProject) || sameTaskWorkspace(slotPath, sessionProject)) {
		return true
	}
	// A linked checkout of the repository this slot names is the same task.
	// So is the private copy this process already detached for that
	// occupation. A sandbox does not adopt an unrelated directory.
	if sameTaskWorkRoot(userID, slotPath, current) {
		return true
	}
	return false
}

// sameTaskWorkspace reports that one path is the other's managed-task
// workspace sandbox. The process default workspace is not that sandbox.
func sameTaskWorkspace(left, right string) bool {
	left = normalizeProjectSessionPath(left)
	right = normalizeProjectSessionPath(right)
	if left == "" || right == "" || sameNormalizedPath(left, right) {
		return false
	}
	if isProcessDefaultWorkspacePath(left) || isProcessDefaultWorkspacePath(right) {
		return false
	}
	return sameNormalizedPath(left, taskWorkspaceSandbox(right)) || sameNormalizedPath(right, taskWorkspaceSandbox(left))
}

func taskWorkspaceSandbox(dir string) string {
	dir = normalizeProjectSessionPath(dir)
	if dir == "" || isProcessDefaultWorkspacePath(dir) || strings.EqualFold(filepath.Base(dir), "workspace") {
		return ""
	}
	return normalizeProjectSessionPath(filepath.Join(dir, "workspace"))
}

func isProcessDefaultWorkspacePath(dir string) bool {
	dir = normalizeProjectSessionPath(dir)
	if dir == "" {
		return false
	}
	if sameNormalizedPath(dir, corelib.EffectiveWorkspaceDir()) {
		return true
	}
	return sameNormalizedPath(dir, corelib.WorkspaceDir())
}

func sameNormalizedPath(left, right string) bool {
	left = normalizeProjectSessionPath(left)
	right = normalizeProjectSessionPath(right)
	return left != "" && strings.EqualFold(left, right)
}

func unfinishedSlotText(lang, en, zhHans, zhHant string) string {
	return pickUI(lang, en, zhHans, zhHant)
}

func isSlotActionCommand(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "__resume_unfinished__ ") || trimmed == "__start_new_task__" || strings.HasPrefix(trimmed, "__dismiss_unfinished__ ")
}

// unreviewedExternalDeliverySlot is an external send whose result is still
// uncertain. Recovery metadata blocks a second send. It does not replay the
// tool, and it does not cover local writes, search, or document generation.
func unreviewedExternalDeliverySlot(slot *agent.UnfinishedTaskSlot) bool {
	if slot == nil || slot.Status == agent.UnfinishedTaskSlotStatusCompleted {
		return false
	}
	if strings.TrimSpace(slot.RecoveryMode) != "requires_review" || strings.TrimSpace(slot.RuntimeTaskID) != "" {
		return false
	}
	return agent.ExternalSendCheckpointEvidence(slot.LastToolName, slot.SideEffectState)
}

// returnExternalDeliveryReview stops the turn. It also drops any utterance the
// unfinished-slot hint saved, so the dismiss button cannot replay that send.
func (h *IMMessageHandler) returnExternalDeliveryReview(msg IMUserMessage, slot *agent.UnfinishedTaskSlot) *IMAgentResponse {
	if h != nil {
		h.pendingSlotUserText.Delete(msg.UserID)
	}
	return externalDeliveryReviewResponse(msg, slot)
}

func externalDeliveryReviewResponse(msg IMUserMessage, slot *agent.UnfinishedTaskSlot) *IMAgentResponse {
	scope := ""
	if slot != nil {
		if dest, ok := agentruntime.ResolveHostFileDestination(slot.LastTask); ok &&
			!agentruntime.SemanticCrossChannelFileDeliveryPublished(string(normalizeIMMessagePlatformKind(msg.Platform)), dest.String()) {
			scope = dest.ChannelScope()
		}
	}
	text := unfinishedSlotText(msg.Lang,
		"The previous external send has an uncertain result. This turn will not deliver again and will not replay that send.",
		"上一次外部发送的结果还不能确定。这一轮不会再次投递，也不会重放那次发送。",
		"上一次外部發送的結果還不能確定。這一輪不會再次投遞，也不會重放那次發送。",
	)
	if scope != "" {
		format := unfinishedSlotText(msg.Lang,
			"The previous send to %s has an uncertain result, and this channel cannot deliver a file there. This turn will not deliver again, will not generate a new document, and will not hand a path to im_message.",
			"上一次发到 %s 的结果还不能确定，而且当前通道不能把文件投递到 %s。这一轮不会再次投递，不会生成新文档，也不会把文件路径交给 im_message。",
			"上一次發到 %s 的結果還不能確定，而且目前通道不能把檔案投遞到 %s。這一輪不會再次投遞，不會生成新文件，也不會把路徑交給 im_message。",
		)
		if strings.Count(format, "%s") == 2 {
			text = fmt.Sprintf(format, scope, scope)
		} else {
			text = fmt.Sprintf(format, scope)
		}
	}
	return &IMAgentResponse{
		Text:           text,
		Error:          "semantic_external_delivery_review_required",
		ResponseSource: "semantic_host_reject",
		// Dismiss starts a new task. There is no resume command and no slot
		// card: a resumed slot would render as already continued.
		Actions: externalDeliveryDismissAction(msg, slot),
	}
}

func externalDeliveryDismissAction(msg IMUserMessage, slot *agent.UnfinishedTaskSlot) []IMResponseAction {
	if slot == nil {
		return nil
	}
	slotID := strings.TrimSpace(slot.SlotID)
	if slotID == "" {
		return nil
	}
	return []IMResponseAction{{
		Label:   unfinishedSlotText(msg.Lang, "Start new task", "开始新任务", "開始新任務"),
		Command: "__dismiss_unfinished__ " + slotID,
		Style:   "primary",
	}}
}
