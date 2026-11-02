package guiapp

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func (h *IMMessageHandler) applyUnifiedTaskContextDecision(msg IMUserMessage, trimmed string, decision explicitTaskSlotDecision, entries []agent.ConversationEntry, unfinishedSlot **agent.UnfinishedTaskSlot, askUserContext string, confirmedResume, freshTask, hasPendingTaskAnswer bool) (string, bool, bool) {
	if h == nil || msg.IsBackground {
		return askUserContext, freshTask, false
	}
	// The continuation prompt repeats the original request, and the max-rounds
	// slot makes the recovery classifier treat that echo as a new task. Clearing
	// here deletes the work the next wave is supposed to resume.
	if sessionRoundTurn(trimmed, msg.Platform) {
		return askUserContext, false, false
	}
	// The bar's continue is the same goal. Leave the fence in place so this
	// host wave is not the user's next message. That message consumes the
	// fence and continues the same conversation.
	if h.goalContinuationAdmittedPastStop(msg) {
		h.bindSessionRestartSlot(msg.UserID, unfinishedSlot)
		return askUserContext, false, false
	}
	if h.consumeCancelledTaskBoundary(msg.UserID) {
		// The fence means the run stopped. Only an explicit new-task command
		// retires the conversation. A fresh-task flag from another gate does
		// not: the follow-up is still this task. Drop the slot that would
		// replay the interrupted side effect, and keep the transcript.
		if decision.StartNewTask {
			if len(entries) >= 2 {
				h.archiveCurrentTask(msg.UserID, entries, agent.ArchivedTaskStatusInterrupted)
			}
			if h.memory != nil {
				h.memory.ClearConversationAndDismissSlot(msg.UserID)
			}
			h.resetConversationResidue(msg.UserID)
			log.Printf("[TaskContext] new task for user %s: stopped run, and this message starts a new task", msg.UserID)
			return "", true, len(entries) > 0
		}
		h.closeStoppedRun(msg.UserID, unfinishedSlot)
		log.Printf("[TaskContext] continue for user %s: previous run was stopped; conversation kept", msg.UserID)
		return askUserContext, false, false
	}
	if confirmedResume || freshTask || decision.ResumeSlotID != "" {
		return askUserContext, freshTask, false
	}
	// ACP Mode B: skip the extra task-context LLM hop (~1–2s). Structural
	// continue/new is enough for an editor programming session.
	if isACPProgrammingMessage(msg) {
		if len(entries) == 0 {
			log.Printf("[TaskContext] acp-mode-b structural new user=%s (no history, skip LLM)", msg.UserID)
			return "", true, false
		}
		log.Printf("[TaskContext] acp-mode-b structural continue user=%s historyLen=%d (skip LLM)", msg.UserID, len(entries))
		h.bindSessionRestartSlot(msg.UserID, unfinishedSlot)
		return askUserContext, freshTask, false
	}
	tcDecision := h.resolveTaskContext(
		msg.CancelCtx, msg.UserID, trimmed, entries,
		hasPendingTaskAnswer, false, false,
	)
	// A recovered in-flight slot may contain an unpaired side effect. Ask the
	// classifier before binding it. "new" does not bind. Continue, including
	// a classifier failure that preserves this session, binds the slot so its
	// resume contract is in the prompt. Binding is not permission to replay
	// the interrupted tool; that contract says to inspect first.
	if shouldConfirmRecoveredSemanticContinuation(tcDecision, unfinishedSlot) {
		tcDecision = h.resolveTaskContextWithClassification(
			msg.CancelCtx, msg.UserID, trimmed, entries,
			hasPendingTaskAnswer, false, false,
		)
	}
	switch tcDecision.Action {
	case agent.TaskNew:
		if len(entries) >= 2 {
			h.archiveCurrentTask(msg.UserID, entries, agent.ArchivedTaskStatusSwitched)
		}
		if h.memory != nil {
			h.memory.ClearConversationAndDismissSlot(msg.UserID)
		}
		h.resetConversationResidue(msg.UserID)
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
		// A restart leaves this session's recovery slot unbound. The transcript
		// can still be on disk, but the resume contract is not in the prompt
		// until the slot is active. Continuing this session binds its own slot.
		// A classifier "new" does not reach this case.
		if isConfirmedSemanticContinuation(tcDecision) || sessionRestartSlotPending(unfinishedSlot) {
			h.bindSemanticContinuationSlot(msg.UserID, unfinishedSlot)
		}
		log.Printf("[TaskContext] continue for user %s: %s", msg.UserID, tcDecision.Reason)
	}
	return askUserContext, freshTask, false
}

// closeStoppedRun ends the interrupted execution without retiring the task.
// The conversation, the task anchor, and the open authorization stay. The
// stopped run's question is dropped earlier, so this message is a new
// instruction in that conversation. Every in-flight slot is dropped, whether
// it is the visible slot or queued behind a reopen slot. The run's checkpoint
// marker is cleared in the same step: a reopen slot hides that marker from
// promotion, and the message after this one would otherwise turn it into a
// new recovery slot. App-exit and session-exit slots stay, so reopening can resume.
func (h *IMMessageHandler) closeStoppedRun(userID string, unfinishedSlot **agent.UnfinishedTaskSlot) {
	if h == nil {
		return
	}
	if h.memory == nil {
		if unfinishedSlot != nil && *unfinishedSlot != nil &&
			!(*unfinishedSlot).Source.IsSessionExit() && !(*unfinishedSlot).Source.IsAppExit() {
			*unfinishedSlot = nil
		}
		return
	}
	// Clear before the nil-slot return. Recovery promotes a marker only when
	// the visible slot is empty, so a marker that survived beside a reopen
	// slot would wait until that slot moved and then own a later message.
	h.memory.ClearInFlightTask(userID)
	if unfinishedSlot == nil || *unfinishedSlot == nil {
		return
	}
	for _, queued := range h.memory.UnfinishedSlots() {
		if queued == nil || queued.UserID != userID {
			continue
		}
		if queued.Source.IsSessionExit() || queued.Source.IsAppExit() {
			continue
		}
		if id := strings.TrimSpace(queued.SlotID); id != "" {
			h.memory.DismissUnfinishedSlot(userID, id)
		}
	}
	next := h.memory.GetUnfinishedSlot(userID)
	if next != nil && (next.Source.IsSessionExit() || next.Source.IsAppExit()) {
		*unfinishedSlot = next
		return
	}
	*unfinishedSlot = nil
}

func shouldConfirmRecoveredSemanticContinuation(decision agent.TaskContextDecision, unfinishedSlot **agent.UnfinishedTaskSlot) bool {
	if decision.Action != agent.TaskContinue || decision.Source != "fallback" || unfinishedSlot == nil || *unfinishedSlot == nil {
		return false
	}
	slot := *unfinishedSlot
	// The classifier runs once, before the slot is bound. A resumed slot is
	// already this task. Asking again adds an LLM call to every later turn
	// and lets that answer clear the transcript.
	if !unfinishedSlotNeedsDecision(slot) || slot.Source.IsSessionExit() {
		return false
	}
	return true
}

func isConfirmedSemanticContinuation(decision agent.TaskContextDecision) bool {
	return decision.Action == agent.TaskContinue &&
		decision.Source == "llm" &&
		decision.ConfirmedContinuation
}

// sessionRestartSlotPending is this session's own interrupted run after the
// process died or its lease expired. It is already stored under this session,
// so binding it does not consult another task. Wording is not the signal.
func sessionRestartSlotPending(unfinishedSlot **agent.UnfinishedTaskSlot) bool {
	if unfinishedSlot == nil || *unfinishedSlot == nil {
		return false
	}
	slot := *unfinishedSlot
	if !slot.Source.IsInFlightRecovery() || !unfinishedSlotNeedsDecision(slot) {
		return false
	}
	return true
}

func (h *IMMessageHandler) bindSessionRestartSlot(userID string, unfinishedSlot **agent.UnfinishedTaskSlot) {
	if !sessionRestartSlotPending(unfinishedSlot) {
		return
	}
	h.bindSemanticContinuationSlot(userID, unfinishedSlot)
}

func (h *IMMessageHandler) bindSemanticContinuationSlot(userID string, unfinishedSlot **agent.UnfinishedTaskSlot) {
	if h == nil || h.memory == nil || unfinishedSlot == nil || *unfinishedSlot == nil {
		return
	}
	if (*unfinishedSlot).Source.IsSessionExit() {
		return
	}
	if !recoveredSlotMatchesSession(userID, *unfinishedSlot, h.effectiveWorkingDirForUser(userID)) {
		log.Printf("[TaskContext] skipped unfinished slot %s for semantic continuation: project mismatch", (*unfinishedSlot).SlotID)
		return
	}
	if unreviewedExternalDeliverySlot(*unfinishedSlot) {
		log.Printf("[TaskContext] skipped unfinished slot %s for semantic continuation: unreviewed external delivery", (*unfinishedSlot).SlotID)
		return
	}
	slotID := (*unfinishedSlot).SlotID
	if h.memory.BindUnfinishedSlot(userID, slotID) {
		*unfinishedSlot = nil
		log.Printf("[TaskContext] bound unfinished slot %s as semantic continuation for user %s", slotID, userID)
	}
}
