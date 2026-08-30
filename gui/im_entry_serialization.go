package main

import (
	"context"
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

	// Acquire state.mu with a timeout. If the previous loop is stuck on a
	// blocking syscall (e.g. memory Store.mu.RLock waiting for pipeline write
	// lock), we must not block the new message indefinitely. After timeout,
	// return a recoverable error so the user can retry or restart.
	const serializationLockTimeout = 60 * time.Second
	acquired := false
	deadline := time.NewTimer(serializationLockTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for !acquired {
		if state.mu.TryLock() {
			acquired = true
			break
		}
		select {
		case <-deadline.C:
			log.Printf("[IM serialization] TIMEOUT acquiring state.mu user=%q after %v — previous loop may be stuck on a blocking lock", msg.UserID, serializationLockTimeout)
			result.Handled = true
			result.Response = &IMAgentResponse{
				Text: "系统正在恢复中（上一个任务因内部锁等待超时未能正常退出），请稍后重试。如持续出现请重启程序。",
			}
			return result
		case <-contextDone(msg.CancelCtx):
			result.Handled = true
			result.Response = &IMAgentResponse{Error: context.Canceled.Error()}
			return result
		case <-ticker.C:
			// Spin with 200ms intervals to check TryLock.
		}
	}
	waited := time.Since(waitStartedAt)
	if waited > 500*time.Millisecond {
		log.Printf("[IM serialization] waited user=%q duration=%v background=%v active_at_wait_start=%v active_loop=%q active_request_id=%q", msg.UserID, waited, msg.IsBackground, activeBeforeLock, activeLoopID, activeRequestID)
	}
	h.setPendingForegroundText(msg.UserID, msg.Text)
	unlockOnce := sync.Once{}
	result.Unlock = func() {
		unlockOnce.Do(func() {
			log.Printf("[IM serialization] release user=%q held=%v background=%v", msg.UserID, time.Since(waitStartedAt).Round(time.Millisecond), msg.IsBackground)
			h.setPendingForegroundText(msg.UserID, "")
			state.mu.Unlock()
		})
	}
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
