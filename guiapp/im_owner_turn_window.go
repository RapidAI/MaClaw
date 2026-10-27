package guiapp

import (
	"context"
	"log"
	"sync"
	"time"
)

// ownerTurnWindow is the exclusive window one foreground host turn of an owner
// runs inside. It is also the owner of that turn's desktop Coding credential:
// the credential is minted only after the window's lock is held, and the whole
// chain is swept before the lock is released.
//
// Both halves have to live here. Minting at each dispatch site leaves any
// host-authored turn that nobody instrumented (session round waves, degraded
// retries) with nothing to present. Minting before the window is held lets the
// turn still running sweep a token it never owned, and lets a token outlive
// the work that justified it. And an owner-wide chain sweep is only sound
// while no other turn of that owner can be in flight.
type ownerTurnWindow struct {
	// Token is the Coding ingress minted for this turn ("" when the owner is
	// not a desktop coding workbench session). Callers must put it on the
	// message before any coding step can be dispatched.
	Token string
	close func()
}

// Close sweeps this turn's Coding chain and releases the owner lock.
// It is idempotent.
func (w ownerTurnWindow) Close() {
	if w.close != nil {
		w.close()
	}
}

// ownerTurnLockTimeout bounds how long a turn waits for the previous turn of
// the same owner. A stuck previous loop must surface as a recoverable error
// instead of blocking the new message indefinitely.
const ownerTurnLockTimeout = 60 * time.Second

// beginOwnerTurnWindow acquires the owner's exclusive turn lock and mints the
// turn's Coding credential inside it. A lock timeout returns the recoverable
// response to show the user and no window.
//
// Background-LLM cancellation stays with the message entry (it happens before
// the interrupt decision); this window owns only exclusion and the credential.
func (h *IMMessageHandler) beginOwnerTurnWindow(msg *IMUserMessage) (ownerTurnWindow, *IMAgentResponse) {
	userID := msg.UserID
	// Per-session mutex: different userIDs can run turns concurrently.
	state := h.getSessionLoop(userID)
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
	log.Printf("[IM serialization] acquire user=%q background=%v active=%v active_loop=%q active_request_id=%q", userID, msg.IsBackground, activeBeforeLock, activeLoopID, activeRequestID)

	// Acquire state.mu with a timeout. A previous loop stuck on a blocking
	// syscall (e.g. memory Store.mu.RLock waiting for a pipeline write lock)
	// must not block this message forever.
	deadline := time.NewTimer(ownerTurnLockTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	acquired := false
	for !acquired {
		if state.mu.TryLock() {
			acquired = true
			break
		}
		select {
		case <-deadline.C:
			log.Printf("[IM serialization] TIMEOUT acquiring state.mu user=%q after %v — previous loop may be stuck on a blocking lock", userID, ownerTurnLockTimeout)
			return ownerTurnWindow{}, &IMAgentResponse{
				Text: "系统正在恢复中（上一个任务因内部锁等待超时未能正常退出），请稍后重试。如持续出现请重启程序。",
			}
		case <-contextDone(msg.CancelCtx):
			return ownerTurnWindow{}, &IMAgentResponse{Error: context.Canceled.Error()}
		case <-ticker.C:
			// Spin with 200ms intervals to check TryLock.
		}
	}
	if waited := time.Since(waitStartedAt); waited > 500*time.Millisecond {
		log.Printf("[IM serialization] waited user=%q duration=%v background=%v active_at_wait_start=%v active_loop=%q active_request_id=%q", userID, waited, msg.IsBackground, activeBeforeLock, activeLoopID, activeRequestID)
	}
	h.setPendingForegroundText(userID, msg.Text)
	closeOnce := sync.Once{}
	window := ownerTurnWindow{close: func() {
		closeOnce.Do(func() {
			// The Coding credential lives exactly as long as this window.
			// Sweep the whole chain (request token, unused continuation token,
			// slot and budget) while the owner lock is still held.
			if isDesktopCodingOwner(userID) {
				h.app.endDesktopCodingTaskIngressForOwner(userID)
			}
			log.Printf("[IM serialization] release user=%q held=%v background=%v", userID, time.Since(waitStartedAt).Round(time.Millisecond), msg.IsBackground)
			h.setPendingForegroundText(userID, "")
			// This turn did not publish, or it already took the command.
			// Either way the held note must not ride into the next message.
			h.dropGoalCommandSteer(userID)
			state.mu.Unlock()
		})
	}}
	// Mint only while the window is held. A message keeps no pre-window token:
	// the credential must not exist outside the work it authorizes.
	window.Token = h.app.attachDesktopCodingIngressForTurn(msg)
	return window, nil
}
