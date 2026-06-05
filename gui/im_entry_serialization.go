package main

import (
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

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
	if h.cancelBackgroundLLMForOwner(msg.UserID) {
		log.Printf("[IM serialization] canceled background LLM work for user=%q before foreground loop", msg.UserID)
	}
	// Only attempt interrupt/merge if the active loop belongs to the SAME
	// userID. Project tabs use a different userID ("desktop-user:{path}"),
	// so their messages must NOT be merged into the local tab's loop.
	if h.interruptHandler != nil && msg.Text != "" && !h.hasCancelledTaskBoundary(msg.UserID) && h.hasActiveLoopForUser(msg.UserID) {
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
	state.stateMu.RLock()
	activeBeforeLock := state.loopCtx != nil
	activeLoopID := ""
	activeRequestID := ""
	if state.loopCtx != nil {
		activeLoopID = state.loopCtx.ID
		activeRequestID = state.loopCtx.Runtime.RequestID
	}
	state.stateMu.RUnlock()
	waitStartedAt := time.Now()
	log.Printf("[IM serialization] acquire user=%q background=%v active=%v active_loop=%q active_request_id=%q", msg.UserID, msg.IsBackground, activeBeforeLock, activeLoopID, activeRequestID)
	state.mu.Lock()
	waited := time.Since(waitStartedAt)
	if waited > 500*time.Millisecond {
		log.Printf("[IM serialization] waited user=%q duration=%v background=%v active_at_wait_start=%v active_loop=%q active_request_id=%q", msg.UserID, waited, msg.IsBackground, activeBeforeLock, activeLoopID, activeRequestID)
	}
	unlockOnce := sync.Once{}
	result.Unlock = func() {
		unlockOnce.Do(func() {
			log.Printf("[IM serialization] release user=%q held=%v background=%v", msg.UserID, time.Since(waitStartedAt).Round(time.Millisecond), msg.IsBackground)
			state.mu.Unlock()
		})
	}
	result.EntriesBeforeClear = h.memory.Load(msg.UserID)
	result.UnfinishedSlot = h.memory.GetUnfinishedSlot(msg.UserID)
	result.Decision = resolveExplicitTaskSlotDecision(msg, result.UnfinishedSlot)
	return result
}
