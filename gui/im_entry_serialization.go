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
	if h.interruptHandler != nil && msg.Text != "" && h.currentLoopCtx != nil {
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

	h.chatLoopMu.Lock()
	// Cancel any running background LLM tasks from the previous agent loop.
	// This frees API bandwidth for the new agent loop's main LLM calls.
	if h.backgroundLLMCancel != nil {
		h.backgroundLLMCancel()
		h.backgroundLLMCancel = nil
	}
	result.Unlock = h.chatLoopMu.Unlock
	result.EntriesBeforeClear = h.memory.Load(msg.UserID)
	result.UnfinishedSlot = h.memory.GetUnfinishedSlot(msg.UserID)
	result.Decision = resolveExplicitTaskSlotDecision(msg, result.UnfinishedSlot)
	return result
}
