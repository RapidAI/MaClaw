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

// ownerTurnLockTimeout bounds how long the owner's lock may stay held while
// NO live turn claims it. A turn claims the lock by marking its window,
// publishing its loop, or registering as in-flight — so queueing behind a
// claimed lock is normal and far longer-bounded (foreground turns routinely
// hold it for many minutes). Only a held lock with no claimant means the
// previous window leaked; that is the case that must surface a recoverable
// error quickly. A var so tests can shrink the orphan grace.
var ownerTurnLockTimeout = 60 * time.Second

// ownerTurnLiveWaitCeiling bounds a claimed wait as well. Liveness cannot
// tell a healthy 30-minute turn from a holder that is claimed-but-wedged
// (or a same-goroutine re-entry, which can never make progress), and an
// unbounded wait would turn that pathology into a silent permanent hang —
// strictly worse than the recoverable error this used to surface at 60s.
// The ceiling is deliberately far above observed hold times (~18min), so it
// fires only when the wait itself has become the anomaly.
var ownerTurnLiveWaitCeiling = 30 * time.Minute

// beginOwnerTurnWindow acquires the owner's exclusive turn lock and mints the
// turn's Coding credential inside it. A lock timeout returns the recoverable
// response to show the user and no window.
//
// steerWhileQueued permits one interrupt-scheduler verdict once the
// queued-behind loop publishes its context (the same shot the message would
// have had at entry). Commands that must execute deterministically after the
// window — e.g. /plan approve — pass false so they always wait.
//
// Background-LLM cancellation stays with the message entry (it happens before
// the interrupt decision); this window owns only exclusion and the credential.
func (h *IMMessageHandler) beginOwnerTurnWindow(msg *IMUserMessage, steerWhileQueued bool) (ownerTurnWindow, *IMAgentResponse) {
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

	// Acquire state.mu, bounded by owner liveness rather than wall-clock.
	// While a turn claims the lock this wait is ordinary queueing and must
	// outlast the turn; a fixed deadline cannot tell that apart from a stuck
	// holder and produced false "系统正在恢复中" replies mid-turn.
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	orphanSince := time.Now()
	steerTried := false
	lastQueuedLog := time.Now()
	for {
		if state.mu.TryLock() {
			break
		}
		if h.ownerTurnClaimed(state, userID) {
			orphanSince = time.Now()
			// Even a claimed wait cannot outrun the ceiling: past it the
			// holder is indistinguishable from wedged (or self-locked), and
			// queueing forever is worse than admitting defeat.
			if time.Since(waitStartedAt) >= ownerTurnLiveWaitCeiling {
				log.Printf("[IM serialization] TIMEOUT acquiring state.mu user=%q after %v — claimed wait exceeded live ceiling (holder wedged?)", userID, ownerTurnLiveWaitCeiling)
				return ownerTurnWindow{}, &IMAgentResponse{
					Text: "系统正在恢复中（上一个任务因内部锁等待超时未能正常退出），请稍后重试。如持续出现请重启程序。",
				}
			}
			// A claimed wait may legitimately last the rest of a long turn.
			// Keep a heartbeat in the log so a truly wedged holder is at
			// least diagnosable instead of a silent hang.
			if since := time.Since(lastQueuedLog); since >= time.Minute {
				lastQueuedLog = time.Now()
				log.Printf("[IM serialization] still queued behind live turn user=%q waited=%v", userID, time.Since(waitStartedAt).Round(time.Second))
			}
			// A turn admitted before its LoopContext was published is
			// invisible to the entry interrupt check. Once the loop becomes
			// visible mid-wait, give the scheduler the same shot it would
			// have had at entry: merge, status and confirm replies resolve
			// right away instead of queueing behind the whole turn.
			if !steerTried && steerWhileQueued && h.ownerTurnSteerEligible(msg) && h.hasActiveLoopForUser(userID) {
				steerTried = true
				if interrupt := h.interruptHandler.TryInterrupt(userID, msg.Text); interrupt.PendingConfirm || interrupt.Handled {
					log.Printf("[IM serialization] queued steer accepted user=%q action=%v", userID, interrupt.Action)
					return ownerTurnWindow{}, &IMAgentResponse{
						Text:        interrupt.Reply,
						Corrections: interrupt.Corrections,
					}
				}
			}
		} else if time.Since(orphanSince) >= ownerTurnLockTimeout {
			log.Printf("[IM serialization] TIMEOUT acquiring state.mu user=%q after %v — lock held with no owning turn (leaked window)", userID, ownerTurnLockTimeout)
			return ownerTurnWindow{}, &IMAgentResponse{
				Text: "系统正在恢复中（上一个任务因内部锁等待超时未能正常退出），请稍后重试。如持续出现请重启程序。",
			}
		}
		// Cancellation outranks the ticker's re-check: a cancelled message
		// must not win the lock on the next spin and briefly hold a window
		// for a turn that will never run.
		select {
		case <-contextDone(msg.CancelCtx):
			return ownerTurnWindow{}, &IMAgentResponse{Error: context.Canceled.Error()}
		default:
		}
		select {
		case <-contextDone(msg.CancelCtx):
			return ownerTurnWindow{}, &IMAgentResponse{Error: context.Canceled.Error()}
		case <-ticker.C:
			// Spin with 200ms intervals to re-check TryLock and liveness.
		}
	}
	if waited := time.Since(waitStartedAt); waited > 500*time.Millisecond {
		log.Printf("[IM serialization] waited user=%q duration=%v background=%v active_at_wait_start=%v active_loop=%q active_request_id=%q", userID, waited, msg.IsBackground, activeBeforeLock, activeLoopID, activeRequestID)
	}
	// Claim the lock for the whole window so queued waiters see a live owner
	// even while this turn is in the pre-loop phase (no loopCtx yet) or runs
	// with an empty message text.
	state.stateMu.Lock()
	state.windowHeld = true
	state.stateMu.Unlock()
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
			state.stateMu.Lock()
			state.windowHeld = false
			state.stateMu.Unlock()
			state.mu.Unlock()
		})
	}}
	// Mint only while the window is held. A message keeps no pre-window token:
	// the credential must not exist outside the work it authorizes.
	window.Token = h.app.attachDesktopCodingIngressForTurn(msg)
	return window, nil
}

// ownerTurnClaimed reports whether the session lock is held by a turn that
// claims it: its window marker, a live published loop, or an in-flight
// pre-loop entry. A held lock with none of these leaked and must not be
// queued behind.
func (h *IMMessageHandler) ownerTurnClaimed(state *sessionLoopState, userID string) bool {
	state.stateMu.RLock()
	// A done-but-still-published loopCtx is stale: if the lock also leaked
	// (windowHeld cleared without unlock), it must not stand in as a live
	// owner. The unwind gap between DoneC closing and unlock while the lock
	// is genuinely held is covered by the orphan grace, not by this flag.
	claimed := state.windowHeld || (state.loopCtx != nil && !state.loopCtx.IsDone())
	state.stateMu.RUnlock()
	if claimed {
		return true
	}
	// An in-flight turn whose loop already exited cannot be the holder — a
	// stale entry must not keep waiters queued behind a phantom owner.
	if turn := h.inFlightTurnForUser(userID); turn != nil && turn.ctx != nil {
		return !turn.ctx.IsDone()
	}
	return false
}

// ownerTurnSteerEligible mirrors the entry interrupt check in
// enterIMMessageSerializationBoundary, minus loop visibility — the caller
// runs it once the queued-behind loop has published its context.
func (h *IMMessageHandler) ownerTurnSteerEligible(msg *IMUserMessage) bool {
	return h.interruptHandler != nil &&
		!msg.IsBackground &&
		msg.Text != "" &&
		!imHostAuthoredTurnPlatform(msg.Platform) &&
		!h.hasCancelledTaskBoundary(msg.UserID)
}

// imHostAuthoredTurnPlatform reports whether the platform belongs to an
// engine-authored continuation turn. Their "不要…" bodies are instructions
// to the engine, not user speech the interrupt scheduler may read as a
// cancel or a merge.
func imHostAuthoredTurnPlatform(platform string) bool {
	switch platform {
	case "goal-continuation", unattendedWorkflowPlatform, sessionRoundContinuationPlatform:
		return true
	}
	return false
}
