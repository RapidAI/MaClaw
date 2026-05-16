package main

import "github.com/RapidAI/CodeClaw/corelib/agent"

type imMessageSerializationResult struct {
	EntriesBeforeClear []agent.ConversationEntry
	UnfinishedSlot     *agent.UnfinishedTaskSlot
	Decision           explicitTaskSlotDecision
	Response           *IMAgentResponse
	Handled            bool
	Unlock             func()
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
	// Only attempt interrupt/merge if the active loop belongs to the SAME
	// userID. Project tabs use a different userID ("desktop-user:{path}"),
	// so their messages must NOT be merged into the local tab's loop.
	if h.interruptHandler != nil && msg.Text != "" && h.hasActiveLoopForUser(msg.UserID) {
		interrupt := h.interruptHandler.TryInterrupt(msg.UserID, msg.Text)
		if interrupt.PendingConfirm || interrupt.Handled {
			result.Handled = true
			result.Response = &IMAgentResponse{
				Text:        interrupt.Reply,
				Corrections: interrupt.Corrections,
			}
			return result
		}
	}

	// Per-session mutex: different userIDs can run agent loops concurrently.
	state := h.getSessionLoop(msg.UserID)
	state.mu.Lock()
	result.Unlock = state.mu.Unlock
	result.EntriesBeforeClear = h.memory.Load(msg.UserID)
	result.UnfinishedSlot = h.memory.GetUnfinishedSlot(msg.UserID)
	result.Decision = resolveExplicitTaskSlotDecision(msg, result.UnfinishedSlot)
	return result
}
