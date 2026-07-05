package main

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func (h *IMMessageHandler) applyUnifiedTaskContextDecision(msg IMUserMessage, trimmed string, decision explicitTaskSlotDecision, entries []agent.ConversationEntry, unfinishedSlot **agent.UnfinishedTaskSlot, askUserContext string, confirmedResume, freshTask, hasPendingTaskAnswer bool) (string, bool, bool) {
	if h == nil || msg.IsBackground {
		return askUserContext, freshTask, false
	}
	if h.consumeCancelledTaskBoundary(msg.UserID) {
		if len(entries) >= 2 {
			h.archiveCurrentTask(msg.UserID, entries, agent.ArchivedTaskStatusInterrupted)
		}
		if h.memory != nil {
			h.memory.ClearConversationAndDismissSlot(msg.UserID)
		}
		h.clearPerUserSessionState(msg.UserID)
		log.Printf("[TaskContext] new task for user %s: previous task was explicitly cancelled", msg.UserID)
		return "", true, len(entries) > 0
	}
	if confirmedResume || freshTask || decision.ResumeSlotID != "" {
		return askUserContext, freshTask, false
	}
	tcDecision := h.resolveTaskContext(
		msg.CancelCtx, msg.UserID, trimmed, entries,
		hasPendingTaskAnswer, false, false,
	)
	switch tcDecision.Action {
	case agent.TaskNew:
		if len(entries) >= 2 {
			h.archiveCurrentTask(msg.UserID, entries, agent.ArchivedTaskStatusSwitched)
		}
		if h.memory != nil {
			h.memory.ClearConversationAndDismissSlot(msg.UserID)
		}
		h.clearPerUserSessionState(msg.UserID)
		log.Printf("[TaskContext] new task for user %s: %s", msg.UserID, tcDecision.Reason)
		return "", true, len(entries) > 0
	case agent.TaskRecall:
		if h.restoreRecalledTask(msg.UserID, tcDecision.RecallTaskID) {
			if len(entries) >= 2 {
				h.archiveCurrentTask(msg.UserID, entries, agent.ArchivedTaskStatusSwitched)
			}
			if unfinishedSlot != nil {
				*unfinishedSlot = nil
			}
			log.Printf("[TaskContext] recalled task %s for user %s", tcDecision.RecallTaskID, msg.UserID)
			return "", false, true
		}
		log.Printf("[TaskContext] recall failed for user %s, preserving current task context", msg.UserID)
		return askUserContext, freshTask, false
	case agent.TaskContinue:
		if isConfirmedSemanticContinuation(tcDecision) {
			h.bindSemanticContinuationSlot(msg.UserID, unfinishedSlot)
		}
		log.Printf("[TaskContext] continue for user %s: %s", msg.UserID, tcDecision.Reason)
	}
	return askUserContext, freshTask, false
}

func isConfirmedSemanticContinuation(decision agent.TaskContextDecision) bool {
	return decision.Action == agent.TaskContinue &&
		decision.Source == "llm" &&
		decision.ConfirmedContinuation
}

func (h *IMMessageHandler) bindSemanticContinuationSlot(userID string, unfinishedSlot **agent.UnfinishedTaskSlot) {
	if h == nil || h.memory == nil || unfinishedSlot == nil || *unfinishedSlot == nil {
		return
	}
	if (*unfinishedSlot).Source.IsSessionExit() {
		return
	}
	if !unfinishedSlotProjectMatchesCurrent(*unfinishedSlot, h.getCurrentProjectPath()) {
		log.Printf("[TaskContext] skipped unfinished slot %s for semantic continuation: project mismatch", (*unfinishedSlot).SlotID)
		return
	}
	slotID := (*unfinishedSlot).SlotID
	if h.memory.BindUnfinishedSlot(userID, slotID) {
		*unfinishedSlot = nil
		log.Printf("[TaskContext] bound unfinished slot %s as semantic continuation for user %s", slotID, userID)
	}
}
