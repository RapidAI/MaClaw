package guiapp

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type imMessageSerializationResult struct {
	EntriesBeforeClear     []agent.ConversationEntry
	UnfinishedSlot         *agent.UnfinishedTaskSlot
	Decision               explicitTaskSlotDecision
	Response               *IMAgentResponse
	Handled                bool
	ActiveLoopOwnsNextTurn bool
	Unlock                 func()
	// CodingIngressToken is the desktop Coding credential for this turn when
	// the boundary minted one. The caller must copy it onto the message before
	// the loop starts; the credential is swept when this turn's window closes.
	CodingIngressToken string
}

func (h *IMMessageHandler) enterIMMessageSerializationBoundary(msg IMUserMessage, providedLoopCtx *LoopContext, entries []agent.ConversationEntry, unfinishedSlot *agent.UnfinishedTaskSlot, decision explicitTaskSlotDecision) imMessageSerializationResult {
	result := imMessageSerializationResult{
		EntriesBeforeClear: entries,
		UnfinishedSlot:     unfinishedSlot,
		Decision:           decision,
		Unlock:             func() {},
	}
	if providedLoopCtx != nil || msg.IsBackground {
		return result
	}
	if h.cancelBackgroundLLMForOwner(msg.UserID) {
		log.Printf("[IM serialization] canceled background LLM work for user=%q before foreground loop", msg.UserID)
	}
	// Only attempt interrupt/merge if the active loop belongs to the SAME
	// userID. Project tabs use a different userID ("desktop-user:{path}"),
	// so their messages must NOT be merged into the local tab's loop.
	// A goal continuation is the engine's own next turn. It waits on the
	// session lock, and the admit generation then drops it if pause already
	// won. The continuation text says "不要…". The interrupt scheduler reads
	// that as the person cancelling or merging, which fences chat or leaves
	// the prompt on the next message after pause.
	if h.interruptHandler != nil && msg.Text != "" && msg.Platform != "goal-continuation" && !h.hasCancelledTaskBoundary(msg.UserID) && h.hasActiveLoopForUser(msg.UserID) {
		interrupt := h.interruptHandler.TryInterrupt(msg.UserID, msg.Text)
		if interrupt.PendingConfirm || interrupt.Handled {
			result.Handled = true
			result.ActiveLoopOwnsNextTurn = true
			result.Response = &IMAgentResponse{
				Text:        interrupt.Reply,
				Corrections: interrupt.Corrections,
			}
			return result
		}
	}

	// The owner's exclusive turn window also owns this turn's Coding
	// credential: it is minted once the lock is held and swept before release.
	window, timeoutResponse := h.beginOwnerTurnWindow(&msg)
	if timeoutResponse != nil {
		result.Handled = true
		result.Response = timeoutResponse
		return result
	}
	result.CodingIngressToken = window.Token
	result.Unlock = window.Close
	result.EntriesBeforeClear = h.memory.Load(msg.UserID)
	result.UnfinishedSlot = h.memory.GetUnfinishedSlot(msg.UserID)

	// In-flight marker recovery: now that state.mu is held, we are guaranteed
	// that any previous loop's inFlightLifecycle.Cleanup() has completed (since
	// Cleanup runs inside runAgentLoop which executes under state.mu). This
	// eliminates the TOCTOU race where ConsumeInFlightTask reads a stale marker
	// that the previous loop's Cleanup hasn't cleared yet.
	if shouldRecoverInFlightMarker(msg, result.UnfinishedSlot, h.getSessionLoopCtx(msg.UserID)) {
		if slot := h.recoverInterruptedTaskSlot(msg.UserID, result.EntriesBeforeClear); slot != nil {
			result.UnfinishedSlot = slot
		}
	}

	result.Decision = resolveExplicitTaskSlotDecision(msg, result.UnfinishedSlot)
	return result
}
