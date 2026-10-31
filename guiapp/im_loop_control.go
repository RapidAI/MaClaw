package guiapp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// CancelCurrentSession cancels the currently running chat session.
// It signals the loop to stop and waits (up to 10s) for it to exit so that
// a subsequent SendAIAssistantMessage call won't overlap with the old loop.
// Returns the cancelled task's user text (if any) for display purposes.
func (h *IMMessageHandler) CancelCurrentSession() (string, error) {
	ctx, userID, taskText := h.legacyLoopSnapshot()
	if userID != "" {
		return h.CancelSessionForUser(userID)
	}
	if ctx == nil {
		return "", fmt.Errorf("no active session to cancel")
	}
	ctx.MarkUserCancel()
	ctx.Cancel()
	if uid := strings.TrimSpace(ctx.UserID); uid != "" {
		h.markTaskCancelledByUser(uid)
	}
	// Wait for the loop goroutine to finish so the per-session mutex is released
	// before the caller sends a new message.
	select {
	case <-ctx.DoneC:
	case <-time.After(30 * time.Second):
		log.Printf("[CancelCurrentSession] timed out waiting for loop to exit — force-closing DoneC")
		ctx.Done()
	}
	return taskText, nil
}

func (h *IMMessageHandler) CancelSessionForUser(userID string) (string, error) {
	return h.finishUserSessionCancel(userID, true)
}

// RequestCancelSessionForUser starts cancellation without waiting for the
// current goroutine to unwind. IM control commands use this path so a user can
// always stop a tool or model call that is holding the session mutex. The loop
// context's cancellation hooks still revoke any durable coding-runtime work.
func (h *IMMessageHandler) RequestCancelSessionForUser(userID string) (string, error) {
	return h.finishUserSessionCancel(userID, false)
}

// requestCancelSessionForUserExcept is RequestCancelSessionForUser with one
// loop exempted. A host stop tool runs inside the loop it may stop; that loop
// is the one still owing the person the tool's verified answer, so it is not
// work to cancel. Everything else this session owns still stops.
func (h *IMMessageHandler) requestCancelSessionForUserExcept(userID string, except *LoopContext) (string, error) {
	return h.finishUserSessionCancelExcept(userID, false, except)
}

// finishUserSessionCancel stops this session, including a goal continuation
// published, queued, or already admitted under either slash spelling.
// The continuation timer is dropped before a published loop is waited on,
// so it cannot start during that wait. An admitted fire is revoked before
// that wait too: the stop button waits for this spelling's turn to exit,
// and the other spelling's continuation would otherwise enter in that gap.
// A stop that only finds the other spelling pauses the goal first and then
// revokes, matching the goal bar. A stop that finds this spelling leaves
// the pause to that turn.
func (h *IMMessageHandler) finishUserSessionCancel(userID string, wait bool) (string, error) {
	return h.finishUserSessionCancelExcept(userID, wait, nil)
}

func (h *IMMessageHandler) finishUserSessionCancelExcept(userID string, wait bool, except *LoopContext) (string, error) {
	if h == nil {
		return "", fmt.Errorf("missing userID")
	}
	userID = strings.TrimSpace(userID)
	twinText, loopStopped, timerStopped := h.signalSlashTwinGoalSessionExcept(userID, except)
	// A fire can already have left the timer and not yet published a loop.
	// Pause revokes that admit. Stop has to see it too, on every spelling.
	admitted := h.liveContinuationAdmitted(userID)
	ownsPause := !h.sessionCancelTouchesATurn(userID) && (loopStopped || timerStopped || admitted)
	dropTimers := ownsPause || h.sessionCancelTouchesATurn(userID)
	if ownsPause {
		// Status first, then the admit generation. A fire that mints during
		// this stop either loses the goal or loses the ticket. This spelling
		// has no dying turn, so the pause cannot wait for one-id.
		h.pauseSlashTwinGoalNow(userID, loopStopped)
		h.revokeSessionContinuationAdmit(userID)
	} else if dropTimers {
		h.revokeSessionContinuationAdmit(userID)
	}
	if dropTimers && h.app != nil {
		h.app.cancelGoalBarTimers(userID)
	}
	var text string
	var err error
	if wait {
		text, err = h.cancelSessionForUserOne(userID)
	} else {
		text, err = h.requestCancelSessionForUserOneExcept(userID, except)
	}
	if err != nil && ownsPause {
		log.Printf("[CancelSessionForUser] stopped slash-twin continuation user=%s twin_loop=%v twin_timer=%v admitted=%v", userID, loopStopped, timerStopped, admitted)
		return twinText, nil
	}
	if err == nil && !dropTimers {
		// Horizon and session-round stops have no loop to see ahead of time.
		h.revokeSessionContinuationAdmit(userID)
	}
	return text, err
}

// signalSlashTwinGoalSessionExcept cancels a turn that belongs to this session
// but was published under the other slash spelling. It does not wait and it
// does not call CancelSessionForUser: that waits on DoneC, and
// LoopContext.Cancel does not close DoneC. A queued timer is only reported;
// the caller drops it. except is the turn asking to stop, which must survive
// even if another spelling happens to resolve to the same context.
func (h *IMMessageHandler) signalSlashTwinGoalSessionExcept(asked string, except *LoopContext) (taskText string, loopStopped, timerStopped bool) {
	if h == nil {
		return "", false, false
	}
	asked = strings.TrimSpace(asked)
	if asked == "" {
		return "", false, false
	}
	for _, id := range goalBarCandidateIDs(asked) {
		if id == asked {
			continue
		}
		ctx, text, _ := h.loopContextForCancel(id)
		if ctx == nil && strings.TrimSpace(text) == "" {
			continue
		}
		if ctx != nil && ctx != except && !ctx.IsCancelled() {
			ctx.MarkUserCancel()
			ctx.Cancel()
		}
		h.markTaskCancelledByUser(id)
		if taskText == "" {
			taskText = strings.TrimSpace(text)
		}
		loopStopped = true
	}
	if h.app == nil || h.app.goalContinuation == nil {
		return taskText, loopStopped, false
	}
	for _, id := range goalBarCandidateIDs(asked) {
		if id == asked {
			continue
		}
		if h.app.goalContinuation.continuationAlreadyQueued(id) {
			return taskText, loopStopped, true
		}
	}
	return taskText, loopStopped, false
}

// sessionCancelTouchesATurn reports that this exact id already has a turn the
// one-id cancel will stop. Horizon and session-round stops are not visible
// here; they return without waiting.
func (h *IMMessageHandler) sessionCancelTouchesATurn(userID string) bool {
	if h == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	ctx, text, _ := h.loopContextForCancel(userID)
	return ctx != nil || strings.TrimSpace(text) != ""
}

// liveContinuationAdmitted reports a host continuation that any spelling of
// this session has already admitted.
func (h *IMMessageHandler) liveContinuationAdmitted(userID string) bool {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return false
	}
	for _, id := range goalBarCandidateIDs(userID) {
		if h.app.goalContinuation.continuationAdmitLive(id) {
			return true
		}
	}
	return false
}

// revokeSessionContinuationAdmit drops every continuation this session has
// already admitted. The caller pauses first when this stop owns the goal, so
// a fire that arrives during the bump finds the goal stopped.
func (h *IMMessageHandler) revokeSessionContinuationAdmit(userID string) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	for _, id := range goalBarCandidateIDs(userID) {
		h.app.goalContinuation.revokeContinuationAdmit(id)
	}
}

// pauseSlashTwinGoalNow stamps the goal after a stop that never started a
// turn on the asked spelling. A previous continue mark would swallow that
// pause. A twin loop already cleared the mark when it was fenced. A timer
// or an admitted fire alone must not fence the chat: the next message is
// not a new task.
func (h *IMMessageHandler) pauseSlashTwinGoalNow(userID string, loopStopped bool) {
	if h == nil {
		return
	}
	if !loopStopped && h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.clearContinueRearmed(userID)
	}
	h.pauseStillActiveUserStop(userID)
}

func (h *IMMessageHandler) cancelSessionForUserOne(userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("missing userID")
	}
	// The next session wave is armed only after the previous loop has exited.
	// A stop in that gap finds no LoopContext; it still has to drop the timer.
	stoppedRound := h.cancelSessionRoundContinuation(userID)
	cancelledHorizon := h.cancelHorizonSessionWithReason(userID, "session")
	ctx, taskText, published := h.loopContextForCancel(userID)
	if ctx == nil {
		if taskText != "" {
			// Serialization lock is held but LoopContext is not registered yet
			// (gates / prepare). Fence the next message and let abortIfCancelled
			// stop this turn once it observes the boundary.
			h.markTaskCancelledByUser(userID)
			log.Printf("[CancelSessionForUser] cancelled pending foreground turn user=%s", userID)
			return taskText, nil
		}
		if cancelledHorizon {
			return "horizon", nil
		}
		if stoppedRound {
			return "", nil
		}
		return "", fmt.Errorf("no active session to cancel")
	}
	ctx.MarkUserCancel()
	ctx.Cancel()
	// Close steer acceptance before clearing pending bags. Otherwise a guide can
	// slip in after the clear and be acknowledged against a loop that is already
	// being cancelled.
	h.markTaskCancelledByUser(userID)
	if !published {
		// Pre-loop (UIC / profile classify) has a LoopContext but has not
		// published it or started runAgentLoop, so DoneC will not close until
		// the dying goroutine returns. Do not wait: the next message is fenced
		// by cancelledTaskBoundary, and executePreparedIMEntry aborts on CancelC.
		log.Printf("[CancelSessionForUser] cancelled in-flight pre-loop user=%s", userID)
		return taskText, nil
	}
	select {
	case <-ctx.DoneC:
	case <-time.After(30 * time.Second):
		log.Printf("[CancelSessionForUser] timed out waiting for loop to exit user=%s — force-closing DoneC to unblock waiters", userID)
		// Force-close DoneC so that hasActiveInterruptableLoop and other
		// waiters stop considering this loop as active. The goroutine holding
		// state.mu is stuck on a blocking syscall (e.g. memory Store.mu.RLock
		// waiting for a long-running pipeline write lock) and cannot check
		// IsCancelled. DoneC.close is idempotent — if the loop exits later,
		// its defer ctx.Done() is a no-op.
		ctx.Done()
	}
	return taskText, nil
}

// requestCancelSessionForUserOneExcept is the non-waiting session cancel with
// one loop exempted. The exempted loop is the turn that invoked the stop tool:
// it must finish so the tool's say can reach the person. Cancelling only that
// turn would stop nothing and turn a verified answer into "Task cancelled".
func (h *IMMessageHandler) requestCancelSessionForUserOneExcept(userID string, except *LoopContext) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("missing userID")
	}
	// Same gap as CancelSessionForUser: /stop and the interrupt path arrive
	// here without a live loop while the next session wave is still queued.
	stoppedRound := h.cancelSessionRoundContinuation(userID)
	cancelledHorizon := h.cancelHorizonSessionWithReason(userID, "session")
	ctx, taskText, published := h.loopContextForCancel(userID)
	if ctx != nil && except != nil && ctx == except {
		// The only turn this cancel can reach is the one asking to stop. It is
		// not work to stop; leave it live so it can deliver the tool's answer.
		return "", nil
	}
	if ctx == nil {
		if taskText != "" {
			h.markTaskCancelledByUser(userID)
			log.Printf("[RequestCancelSessionForUser] cancelled pending foreground turn user=%s", userID)
			return taskText, nil
		}
		if cancelledHorizon {
			return "horizon", nil
		}
		if stoppedRound {
			return "", nil
		}
		return "", fmt.Errorf("no active session to cancel")
	}
	ctx.MarkUserCancel()
	ctx.Cancel()
	// Close steering acceptance before clearing pending bags. Otherwise a guide
	// can be acknowledged after the stop command but before the loop observes it.
	h.markTaskCancelledByUser(userID)
	if !published {
		log.Printf("[RequestCancelSessionForUser] cancelled in-flight pre-loop user=%s", userID)
	}
	return taskText, nil
}

// loopContextForCancel resolves the LoopContext an explicit user cancel must
// stop. Prefer the published session loop; if that is empty, cancel the
// in-flight pre-loop turn before falling back to the legacy global pointer.
// A stale last-user leftover must not win over the UIC window or the cancel
// button waits on a DoneC that will never close and never stops the live turn.
func (h *IMMessageHandler) loopContextForCancel(userID string) (ctx *LoopContext, taskText string, published bool) {
	ctx = h.getSessionLoopCtx(userID)
	taskText = h.sessionLoopTaskText(userID)
	if ctx != nil {
		return ctx, taskText, true
	}
	if turn := h.inFlightTurnForUser(userID); turn != nil {
		return turn.ctx, turn.userText, false
	}
	if pending := h.pendingForegroundText(userID); pending != "" {
		return nil, pending, false
	}
	ctx, taskText, _ = h.legacyLoopSnapshotForUser(userID)
	if ctx != nil {
		return ctx, taskText, true
	}
	return nil, "", false
}

func (h *IMMessageHandler) preLoopCancelResponse(msg IMUserMessage, loopCtx *LoopContext, history []agent.ConversationEntry, userText string) *IMAgentResponse {
	if loopCtx == nil {
		return nil
	}
	if !loopCtx.IsCancelled() && !h.hasCancelledTaskBoundary(msg.UserID) {
		return nil
	}
	if !loopCtx.IsCancelled() {
		loopCtx.Cancel()
	}
	log.Printf("[IM] pre-loop cancelled user=%s request_id=%s", msg.UserID, imRequestID(msg))
	return h.cancelledExitResponse(msg.UserID, history, userText)
}

// cancelAllSessionsForShutdown is a non-blocking lifecycle cancellation used
// when an isolated hardware runtime is removed. Waiting here would make an
// unbind/Hub-disconnect depend on a stuck model or tool request; each loop
// already observes LoopContext cancellation and releases its own resources.
func (h *IMMessageHandler) cancelAllSessionsForShutdown() {
	if h == nil {
		return
	}
	h.cancelAllHorizonSessions("shutdown")
	h.sessionLoops.Range(func(_, value any) bool {
		state, ok := value.(*sessionLoopState)
		if !ok || state == nil {
			return true
		}
		state.stateMu.RLock()
		ctx := state.loopCtx
		state.stateMu.RUnlock()
		if ctx != nil {
			ctx.Cancel()
		}
		return true
	})
	h.inFlightTurns.Range(func(key, value any) bool {
		if turn, ok := value.(*inFlightTurn); ok && turn != nil && turn.ctx != nil {
			turn.ctx.Cancel()
		}
		h.inFlightTurns.CompareAndDelete(key, value)
		return true
	})
	h.activeBtwSubAgents.Range(func(_, value any) bool {
		if subAgent, ok := value.(*BtwSubAgent); ok && subAgent != nil {
			subAgent.Cancel()
		}
		return true
	})
	h.activeLoopCallbacksByOwner.Range(func(_, value any) bool {
		if callbacks, ok := value.(*guiLoopCommandCallbacks); ok && callbacks != nil {
			callbacks.Cancel()
		}
		return true
	})
}

func (h *IMMessageHandler) legacyLoopSnapshot() (*LoopContext, string, string) {
	if h == nil {
		return nil, "", ""
	}
	h.globalLoopMu.RLock()
	defer h.globalLoopMu.RUnlock()
	return h.currentLoopCtx, strings.TrimSpace(h.lastUserID), h.lastUserText
}

func (h *IMMessageHandler) legacyLoopSnapshotForUser(userID string) (*LoopContext, string, bool) {
	userID = strings.TrimSpace(userID)
	if h == nil || userID == "" {
		return nil, "", false
	}
	h.globalLoopMu.RLock()
	defer h.globalLoopMu.RUnlock()
	if strings.TrimSpace(h.lastUserID) != userID {
		return nil, "", false
	}
	return h.currentLoopCtx, h.lastUserText, h.currentLoopCtx != nil
}

func (h *IMMessageHandler) runtimeTaskTextForOwner(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" {
		return h.sessionLoopTaskText(ownerID)
	}
	_, _, taskText := h.legacyLoopSnapshot()
	return taskText
}

func (h *IMMessageHandler) currentRuntimeTaskTextOrLegacy() (string, string) {
	ownerID, explicitRuntime := h.currentRuntimePolicyOwnerState()
	if !explicitRuntime || strings.TrimSpace(ownerID) == "" {
		return "", ""
	}
	return h.runtimeTaskTextForOwner(ownerID), ownerID
}

func (h *IMMessageHandler) sessionLoopTaskText(userID string) string {
	if h == nil || strings.TrimSpace(userID) == "" {
		return ""
	}
	if v, ok := h.sessionLoops.Load(userID); ok {
		state := v.(*sessionLoopState)
		state.stateMu.RLock()
		defer state.stateMu.RUnlock()
		return state.userText
	}
	return ""
}

type cancelledTaskFence struct {
	at  time.Time
	gen uint64
	seq uint64
}

func sameCancelledTaskFence(a, b cancelledTaskFence) bool {
	if a.seq != 0 || b.seq != 0 {
		return a.seq == b.seq
	}
	return a.gen == b.gen && a.at.Equal(b.at)
}

func (h *IMMessageHandler) markTaskCancelledByUser(userID string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	// A continue mark belongs to the previous stop. This stop must be able to
	// pause the goal after the dying turn of that continue has been told to
	// leave the re-armed goal alone.
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.clearContinueRearmed(userID)
	}
	h.cancelledTaskBoundary.Store(userID, cancelledTaskFence{
		at:  time.Now(),
		gen: h.pendingForegroundGen(userID),
		seq: h.cancelFenceSeq.Add(1),
	})
	h.pendingInjection.Delete(userID)
	h.pendingPreLoopGuide.Delete(userID)
	if h.interruptHandler != nil {
		h.interruptHandler.ClearTracker(userID)
	}
}

func (h *IMMessageHandler) hasCancelledTaskBoundary(userID string) bool {
	_, ok := h.currentCancelledTaskFence(userID)
	return ok
}

func (h *IMMessageHandler) currentCancelledTaskFence(userID string) (cancelledTaskFence, bool) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return cancelledTaskFence{}, false
	}
	raw, ok := h.cancelledTaskBoundary.Load(userID)
	if !ok {
		return cancelledTaskFence{}, false
	}
	return cancelledTaskFenceFrom(raw)
}

func cancelledTaskFenceFrom(raw any) (cancelledTaskFence, bool) {
	switch v := raw.(type) {
	case cancelledTaskFence:
		return v, true
	case time.Time:
		return cancelledTaskFence{at: v}, true
	default:
		return cancelledTaskFence{}, false
	}
}

func (h *IMMessageHandler) consumeCancelledTaskBoundary(userID string) bool {
	if h == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	raw, ok := h.cancelledTaskBoundary.Load(userID)
	if !ok {
		return false
	}
	fence, ok := cancelledTaskFenceFrom(raw)
	if !ok {
		h.cancelledTaskBoundary.Delete(userID)
		return true
	}
	// The request that currently holds state.mu set pendingGen before this
	// cancel. Consuming that fence here would treat the cancelled message as
	// a brand-new task and let it run. Leave it for pre-loop abort instead.
	if gen := h.pendingForegroundGen(userID); gen != 0 && gen == fence.gen {
		return false
	}
	return h.cancelledTaskBoundary.CompareAndDelete(userID, raw)
}

// InjectSupplementary stores a supplementary message for the running agent
// loop to consume at the start of its next iteration. Returns true if a loop
// is currently active (injection accepted), false otherwise.
// A user-stop fence refuses the supplement. The text must not ride into the
// dying loop. The user's next message continues the same conversation.
func (h *IMMessageHandler) InjectSupplementary(userID, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || h.hasCancelledTaskBoundary(userID) {
		return false
	}
	return h.acceptLiveLoopSteer(userID, "[用户补充] "+text)
}

// acceptLiveLoopSteer queues text for the loop that is running now.
// True means that loop is guaranteed to observe it. A sealed or cancelled
// loop returns false and stores nothing. The user-stop fence is not checked:
// a goal the user already continued is still this loop, and a rewritten
// command belongs to it. Ordinary chat supplements stay on InjectSupplementary.
func (h *IMMessageHandler) acceptLiveLoopSteer(userID, text string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	text = strings.TrimSpace(text)
	if userID == "" || text == "" {
		return false
	}
	// Queue under the same finalization lock used by TrySealReplans. This keeps
	// the API contract honest: true means the current loop is guaranteed to see
	// the supplement; a final answer that already committed returns false and no
	// orphaned injection is left behind.
	accepted := h.tryInjectActiveReplan(userID, func() {
		h.accumulateInjection(userID, text)
	})
	if !accepted {
		return false
	}
	log.Printf("[inject-supplementary] user=%s text_len=%d", userID, len([]rune(text)))
	return true
}

// InjectGuideReference stores input-buffer guide-launch text as a live user
// steering directive for the next agent loop iteration. Unlike a normal
// supplementary message, this guides replanning without becoming an independent
// chat turn or causing the current session to finalize by itself.
//
// When a loop is actively running, the guide wrapper is stored in the pending
// injection bag and requests a replan. The next iteration unwraps it into a
// user-role live-steering message. During preflight it is stored separately and
// injected with the same user-role semantics at iteration 0.
func (h *IMMessageHandler) InjectGuideReference(userID, text string) bool {
	return h.InjectGuideReferenceWithID(userID, text, "", "")
}

type guideLaunchAcceptance struct {
	done              chan struct{}
	accepted          bool
	acceptedAt        time.Time
	text              string
	expectedRequestID string
}

const guideLaunchAcceptanceTTL = 10 * time.Minute

func (h *IMMessageHandler) pruneExpiredGuideLaunchAcceptances(now time.Time) {
	h.acceptedGuideLaunchIDs.Range(func(key, value any) bool {
		acceptance, ok := value.(*guideLaunchAcceptance)
		if !ok || acceptance == nil {
			return true
		}
		// accepted/acceptedAt are published-before close(done). Never inspect
		// them while the owning injection is still resolving.
		select {
		case <-acceptance.done:
			if acceptance.accepted && !acceptance.acceptedAt.IsZero() && now.Sub(acceptance.acceptedAt) > guideLaunchAcceptanceTTL {
				h.acceptedGuideLaunchIDs.CompareAndDelete(key, acceptance)
			}
		default:
		}
		return true
	})
}

func (h *IMMessageHandler) clearGuideLaunchAcceptancesForUser(userID string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	prefix := strings.TrimSpace(userID) + "\x00"
	h.acceptedGuideLaunchIDs.Range(func(key, value any) bool {
		launchKey, ok := key.(string)
		if !ok || !strings.HasPrefix(launchKey, prefix) {
			return true
		}
		acceptance, _ := value.(*guideLaunchAcceptance)
		// Never delete an operation that is still resolving: doing so would let a
		// same-ID retry inject concurrently. Completed records are safe to forget
		// when their owning conversation is explicitly reset.
		if acceptance != nil {
			select {
			case <-acceptance.done:
				h.acceptedGuideLaunchIDs.CompareAndDelete(key, acceptance)
			default:
			}
		}
		return true
	})
}

// HasAcceptedGuideReference reports whether an identical durable GUI launch
// was already accepted. It is read-only: callers can resolve a lost RPC reply
// without risking another injection or replan request.
func (h *IMMessageHandler) HasAcceptedGuideReference(userID, text, launchID string, expectedRequestIDs ...string) bool {
	userID = strings.TrimSpace(userID)
	text = strings.TrimSpace(text)
	launchID = strings.TrimSpace(launchID)
	expectedRequestID := ""
	if len(expectedRequestIDs) > 0 {
		expectedRequestID = strings.TrimSpace(expectedRequestIDs[0])
	}
	if userID == "" || text == "" || launchID == "" {
		return false
	}
	h.pruneExpiredGuideLaunchAcceptances(time.Now())
	raw, ok := h.acceptedGuideLaunchIDs.Load(userID + "\x00" + launchID)
	if !ok {
		return false
	}
	acceptance, ok := raw.(*guideLaunchAcceptance)
	if !ok || acceptance == nil || acceptance.text != text || acceptance.expectedRequestID != expectedRequestID {
		return false
	}
	<-acceptance.done
	return acceptance.accepted
}

// InjectGuideReferenceWithID is the idempotent form used by the GUI queue.
// A repeated launchID for the same session reports success without appending a
// second steering directive. IDs are retained only briefly to bound memory.
func (h *IMMessageHandler) InjectGuideReferenceWithID(userID, text, launchID string, expectedRequestIDs ...string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	launchID = strings.TrimSpace(launchID)
	expectedRequestID := ""
	if len(expectedRequestIDs) > 0 {
		expectedRequestID = strings.TrimSpace(expectedRequestIDs[0])
	}
	launchKey := ""
	var launchAcceptance *guideLaunchAcceptance
	if launchID != "" {
		h.pruneExpiredGuideLaunchAcceptances(time.Now())
		launchKey = strings.TrimSpace(userID) + "\x00" + launchID
		launchAcceptance = &guideLaunchAcceptance{done: make(chan struct{}), text: text, expectedRequestID: expectedRequestID}
		actual, loaded := h.acceptedGuideLaunchIDs.LoadOrStore(launchKey, launchAcceptance)
		if loaded {
			previous, ok := actual.(*guideLaunchAcceptance)
			if !ok || previous == nil {
				return false
			}
			// Idempotency only applies to an identical operation. Reusing a key
			// with different text is a client bug or collision; never acknowledge
			// the new instruction as if the old one had been delivered.
			if previous.text != text || previous.expectedRequestID != expectedRequestID {
				return false
			}
			<-previous.done
			return previous.accepted
		}
	}
	finishLaunch := func(accepted bool) {
		if launchAcceptance == nil {
			return
		}
		launchAcceptance.accepted = accepted
		if accepted {
			launchAcceptance.acceptedAt = time.Now()
		}
		close(launchAcceptance.done)
		if !accepted {
			h.acceptedGuideLaunchIDs.CompareAndDelete(launchKey, launchAcceptance)
		}
	}
	// Host commands are classified before any steer. Wrapping one as a guide
	// replans the live call, and the model treats the command as a cue to stop.
	if refuseSteer, executed := h.hostCommandInsteadOfSteer(userID, text); refuseSteer {
		finishLaunch(executed)
		if executed {
			log.Printf("[inject-guide-reference] user=%s text_len=%d mode=goal-command", userID, len([]rune(text)))
			return true
		}
		log.Printf("[inject-guide-reference] user=%s text_len=%d mode=host-command-deferred", userID, len([]rune(text)))
		return false
	}
	injection := buildGuideLaunchInjection(text)
	if injection == "" {
		finishLaunch(false)
		return false
	}
	accepted, preLoop := h.tryInjectGuideReference(userID, injection, expectedRequestID)
	finishLaunch(accepted)
	if !accepted {
		return false
	}
	mode := "replan"
	if preLoop {
		mode = "pre-loop"
	}
	log.Printf("[inject-guide-reference] user=%s text_len=%d mode=%s", userID, len([]rune(text)), mode)
	return true
}

const guideLaunchReferenceMarker = "[\u5f15\u5bfc\u53d1\u5c04\u53c2\u8003]"

// pendingInjectionSeparator preserves the boundary and role of independently
// accepted interruptions that arrive before the next transform. The previous
// newline-only concatenation let one guide wrapper turn adjacent legacy/system
// supplements into user-role content.
const pendingInjectionSeparator = "\n\x1eMACLAW_PENDING_INJECTION\x1e\n"

const guideLaunchReferenceInstructionPrefix = "The following text was fired by the user from the pre-input buffer via the guide-launch button."

const guideLaunchReferenceInstruction = "The following text was fired by the user from the pre-input buffer via the guide-launch button. Treat it as live user steering for the next agent loop: re-evaluate the current plan, tool choice, and answer direction under this guidance before continuing. If it conflicts with stale reasoning or an in-flight tool decision, the fired guidance wins for the next step. Let the next visible assistant response naturally demonstrate that the user's point affected the work, responding to its substance when useful. Do not emit a canned receipt, mechanically announce that it was received or attached, or force a quotation or acknowledgement when that would sound unnatural. Do not treat it as an independent new chat turn, and do not finalize solely because this directive arrived. \u4e0d\u8981\u628a\u5b83\u5f53\u4f5c\u65b0\u7684\u7528\u6237\u56de\u5408\u3002"

// hostCommandInsteadOfSteer reports whether text is an immediate host command.
// refuseSteer means the caller must not wrap it as model steering. executed
// means a mutating /goal changed the row now, beside the live loop. Help,
// status, and a refusal that did not change the row stay in the GUI queue
// and are sent as the next real turn, so the person sees the reply. Every
// other host command stays in that queue too.
func (h *IMMessageHandler) hostCommandInsteadOfSteer(userID, text string) (refuseSteer, executed bool) {
	userID = strings.TrimSpace(userID)
	text = normalizeImmediateCommandText(text)
	if h == nil || userID == "" || text == "" {
		return false, false
	}
	kind := classifyImmediateIMCommand(text)
	if kind == imCommandUnknown {
		return false, false
	}
	if kind != imCommandGoal || !h.hasActiveLoopForUser(userID) {
		return true, false
	}
	verb, _ := splitGoalCommand(text)
	if !goalSlashMutates(verb) {
		return true, false
	}
	before := h.goalAddressFor(userID).goal
	resp := h.handleGoalCommand(IMUserMessage{UserID: userID, Platform: "desktop", Text: text}, text)
	if resp != nil && strings.TrimSpace(resp.Error) != "" {
		log.Printf("[inject-guide-reference] goal command beside live turn user=%s err=%s", userID, truncateForLog(resp.Error, 120))
	}
	return true, goalSlashSettled(verb, before, h.goalAddressFor(userID).goal)
}

func buildGuideLaunchInjection(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return guideLaunchReferenceMarker + "\n" + guideLaunchReferenceInstruction + "\n" + text
}

func isGuideLaunchReferenceInjection(text string) bool {
	if !strings.Contains(text, guideLaunchReferenceMarker) {
		return false
	}
	lines := strings.Split(text, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if isGuideLaunchReferenceHeader(lines, i) {
			return true
		}
	}
	return false
}

func isGuideLaunchReferenceHeader(lines []string, i int) bool {
	return i+1 < len(lines) &&
		strings.TrimSpace(lines[i]) == guideLaunchReferenceMarker &&
		isGuideLaunchReferenceInstructionLine(lines[i+1])
}

func isGuideLaunchReferenceInstructionLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, guideLaunchReferenceInstructionPrefix)
}

func (h *IMMessageHandler) hasPendingGuideReferenceInjection(userID string) bool {
	if h == nil || userID == "" {
		return false
	}
	pending, ok := h.pendingInjection.Load(userID)
	if !ok {
		return false
	}
	text, _ := pending.(string)
	return isGuideLaunchReferenceInjection(text)
}

func (h *IMMessageHandler) pendingGuideLaunchUserText(userID string) string {
	if h == nil || userID == "" {
		return ""
	}
	pending, ok := h.pendingInjection.Load(userID)
	if !ok {
		return ""
	}
	text, _ := pending.(string)
	var guides []string
	for _, item := range splitPendingInjections(text) {
		if !isGuideLaunchReferenceInjection(item) {
			continue
		}
		if guide := stripInjectionPrefix(item); guide != "" {
			guides = append(guides, guide)
		}
	}
	return strings.Join(guides, "\n")
}

func buildGuideSteeredEntryText(guideText, currentText string) string {
	guideText = strings.TrimSpace(guideText)
	currentText = strings.TrimSpace(currentText)
	if guideText == "" {
		return currentText
	}
	if currentText == "" {
		return guideText
	}
	return "[User-fired guide for this turn]\n" + guideText + "\n\n[Current user message]\n" + currentText
}

func (h *IMMessageHandler) entryTextWithPendingGuideReference(userID, currentText string) string {
	guideText := h.pendingGuideLaunchUserText(userID)
	if guideText == "" {
		return strings.TrimSpace(currentText)
	}
	return buildGuideSteeredEntryText(guideText, currentText)
}

// accumulateInjection appends text to the pending injection for the given
// user. If no pending injection exists, it creates one. If one already
// exists (from a prior injection in the same iteration window), the new
// text is appended with a newline separator.
//
// This is the single write path for pendingInjection; all callers
// (InjectSupplementary, interrupt handler Merge, HandleCorrection Merge)
// must use this method instead of calling pendingInjection.Store directly.
func (h *IMMessageHandler) accumulateInjection(userID, prefixedText string) {
	if strings.TrimSpace(prefixedText) == "" {
		return
	}
	for {
		existing, loaded := h.pendingInjection.Load(userID)
		if !loaded {
			if _, raced := h.pendingInjection.LoadOrStore(userID, prefixedText); !raced {
				return
			}
			continue
		}
		oldText, _ := existing.(string)
		combined := oldText + pendingInjectionSeparator + prefixedText
		if h.pendingInjection.CompareAndSwap(userID, existing, combined) {
			return
		}
	}
}

func splitPendingInjections(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, pendingInjectionSeparator)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

// preLoopGuideEntry holds guide-launch text with a creation timestamp.
// Consumed at iteration 0; discarded if older than preLoopGuideMaxAge.
type preLoopGuideEntry struct {
	Text      string
	CreatedAt time.Time
}

const preLoopGuideMaxAge = 30 * time.Second

// accumulatePreLoopGuide stores guide-launch text that arrived before the
// agent loop started. Multiple fires accumulate with newline separator.
// Consumed at iteration 0 as user-role supplement.
func (h *IMMessageHandler) accumulatePreLoopGuide(userID, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	entry := &preLoopGuideEntry{Text: text, CreatedAt: time.Now()}
	for {
		existing, loaded := h.pendingPreLoopGuide.Load(userID)
		if !loaded {
			if _, raced := h.pendingPreLoopGuide.LoadOrStore(userID, entry); !raced {
				return
			}
			continue
		}
		old := existing.(*preLoopGuideEntry)
		combined := &preLoopGuideEntry{
			Text:      old.Text + "\n" + text,
			CreatedAt: old.CreatedAt, // keep original timestamp
		}
		if h.pendingPreLoopGuide.CompareAndSwap(userID, existing, combined) {
			return
		}
	}
}

// parseSlotKind converts a string slot kind to the SlotKind enum.
// Defaults to SlotKindScheduled for unknown values.
func parseSlotKind(s string) SlotKind {
	return normalizeSlotKind(s)
}

// drainStatusEvents non-blockingly drains all pending StatusEvents from the
// LoopContext's StatusC channel, injecting each as a system message into the
// conversation and forwarding to the user via sendProgress.
func drainStatusEvents(ctx *LoopContext, conversation *[]interface{}, sendProgress func(string)) {
	for {
		select {
		case evt := <-ctx.StatusC:
			statusMsg := fmt.Sprintf("[Status update] %s", evt.Message)
			*conversation = append(*conversation, map[string]string{
				"role": "system", "content": statusMsg,
			})
			sendProgress(evt.Message)
		default:
			return
		}
	}
}

func (h *IMMessageHandler) clearInFlightTask(userID string) {
	if h == nil || h.memory == nil {
		return
	}
	h.memory.ClearInFlightTask(userID)
}

func (h *IMMessageHandler) prepareIMLoopContext(provided *LoopContext, msg IMUserMessage, httpClient *http.Client, skipNeedsConfirmGate bool, isAskUserResponse bool) *LoopContext {
	loopCtx := provided
	if loopCtx == nil {
		loopCtx = NewLoopContext("chat", h.getMaclawAgentMaxIterations(), httpClient)
	}
	// A supplied LoopContext can be reused by callers. Every new inbound turn
	// must discard its previous ephemeral semantic identity along with the
	// runtime envelope. An explicit different RequestID proves replacement; an
	// omitted RequestID does too, because runtimeContextFromIMMessage will mint
	// a fresh host request ID for this inbound message. Treating an omitted ID
	// as "keep the old one" used to make a reused context retain the prior
	// root/turn identity, platform and trace even though the next message had no
	// verified continuation handle. RequestID is used solely to detect this
	// replacement here; it is never embedded into the semantic root, turn, or
	// session identity.
	incomingRuntime := runtimeContextFromIMMessage(msg)
	incomingFingerprint := imIngressFingerprint(msg, incomingRuntime)
	incomingRequestID := incomingRuntime.RequestID
	priorRequestID := strings.TrimSpace(loopCtx.Runtime.RequestID)
	// A request ID only identifies a transport retry inside its authenticated
	// ingress envelope. It is neither a semantic continuation handle nor a
	// cross-channel/global principal key. Reusing the same string for a
	// different conversation, actor or channel therefore starts a new turn and
	// must not retain the old private route identity or policy owner.
	sameIngress := sameIMRuntimeIngress(loopCtx.Runtime, incomingRuntime)
	// Retrying a request means resending the same authenticated ingress payload,
	// not merely reusing its transport ID. This prevents a changed request from
	// inheriting a prior semantic identity, policy owner, or client-tool binding.
	// An empty prior fingerprint is deliberately non-equivalent for a populated
	// turn: old/reused contexts must rotate once rather than gain a compatibility
	// path that has never attested their payload.
	samePayload := loopCtx.ingressFingerprint != "" && loopCtx.ingressFingerprint == incomingFingerprint
	resetRuntime := priorRequestID == "" || strings.TrimSpace(msg.RequestID) == "" || incomingRequestID != priorRequestID || !sameIngress || !samePayload
	if resetRuntime {
		policyOwnerID := strings.TrimSpace(loopCtx.Runtime.PolicyOwnerID)
		// A supplied runtime without a request ID is an initial host envelope,
		// so it may carry an explicit policy owner. Once it has represented an
		// actual inbound request, retain that owner only for the same trusted
		// ingress envelope; a same-named request from another user/channel has
		// no authority to inherit it.
		// A policy/workflow owner can survive only an authenticated retry of the
		// same ingress payload. Keeping it for a same-channel but changed body
		// would let a caller re-label a new request as an old privileged turn.
		preservePolicyOwner := priorRequestID == "" || (sameIngress && samePayload)
		// A reused LoopContext can receive its replacement before an old provider
		// response has reached the shared-loop callback. Identity rotation alone
		// would make the old scope unreachable from this context while leaving its
		// already-issued grant durable and live. Fence the old generation first;
		// this revokes its request surface/materializations/grants without closing
		// CancelC, which belongs to the reusable loop object rather than a turn.
		loopCtx.ReplaceSemanticTurn()
		loopCtx.Runtime = incomingRuntime
		loopCtx.ingressFingerprint = incomingFingerprint
		// These fields are an ingress envelope, not a continuation capability.
		// They must stay aligned with the fresh Runtime above; retaining a prior
		// platform/user/run could select a stale channel policy or merge audit
		// events from independent requests.
		loopCtx.Platform = msg.Platform
		loopCtx.UserID = msg.UserID
		loopCtx.Lang = msg.Lang
		loopCtx.JobID = ""
		loopCtx.RunID = ""
		if preservePolicyOwner && policyOwnerID != "" {
			loopCtx.Runtime.PolicyOwnerID = policyOwnerID
			loopCtx.Runtime.WorkflowOwnerID = policyOwnerID
		}
	}
	if !resetRuntime {
		// Keep the host-computed value in sync explicitly so a retry can never
		// inherit a zero/stale fingerprint through a manually supplied context.
		loopCtx.ingressFingerprint = incomingFingerprint
	}
	// Leftover flags and leftover-stamped projections are turn-local. A reused
	// Runtime must not keep the previous generate pin, privilege strip, or
	// unknown+chat leftover identity that would look like a new miss.
	resetLoopSemanticLeftoverState(loopCtx)
	resetSemanticTurnLocalState(loopCtx)
	if loopCtx.HTTPClient == nil {
		loopCtx.HTTPClient = httpClient
	}
	// A profile queue may be stopped while a turn is in its pre-loop work. Bind
	// its context before any routing or tool execution so shutdown/reconfigure
	// cancels the active turn as well as the queued ones.
	loopCtx.BindParentContext(msg.CancelCtx)
	if strings.TrimSpace(loopCtx.Platform) == "" {
		loopCtx.Platform = msg.Platform
	}
	if strings.TrimSpace(loopCtx.UserID) == "" {
		loopCtx.UserID = msg.UserID
	}
	// The epoch at this turn's ingress. A retry of the same payload enters
	// here again after its own cancel bumped the epoch, so refresh it on
	// every prepare; only a newer message arriving during the turn leaves
	// it stale, and that turn must not arm a continuation.
	loopCtx.sessionRoundEpochAtStart = h.sessionRoundEpoch(loopCtx.UserID)
	loopCtx.CodingTaskIngressToken = strings.TrimSpace(msg.CodingTaskIngressToken)
	if strings.TrimSpace(loopCtx.Lang) == "" {
		loopCtx.Lang = msg.Lang
	}
	loopCtx.ClientTools = append([]agent.ClientToolDefinition(nil), msg.ClientTools...)
	if msg.ClientToolContext != nil {
		copyContext := *msg.ClientToolContext
		loopCtx.ClientToolContext = &copyContext
	} else {
		loopCtx.ClientToolContext = nil
	}
	loopCtx.DeliveryTarget = bindLoopDeliveryTarget(loopCtx, msg)
	if h.traceService != nil && loopCtx.RunID == "" {
		job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, msg.Text, msg.Platform, msg.UserID, h.traceProjectPath())
		loopCtx.JobID = job.JobID
		loopCtx.RunID = run.RunID
		h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
		h.appendTraceEvent(loopCtx, "request.accepted", "info", "AI request accepted", h.runtimeTraceSummary(loopCtx, msg.Text), "", "")
	}
	if h.bgManager != nil && loopCtx.StatusC == nil {
		loopCtx.StatusC = h.bgManager.statusC
	}
	loopCtx.SkipNeedsConfirmGate = skipNeedsConfirmGate
	loopCtx.IsAskUserResponse = isAskUserResponse
	// A new inbound turn drops the previous turn's discovery binds. The
	// write-confirmation reply restores only the tool that was just
	// approved. A retry of the same payload keeps the binds already on
	// this loop: the stash was taken by the first prepare.
	if resetRuntime {
		loopCtx.clearDiscoveredMCPBinds()
	}
	// Write confirmation ends the loop that discovered the tool. Only the
	// reply that settles that question republishes it. A background prepare
	// in the same message passes isAskUserResponse false and must not
	// delete the stash before the foreground continuation takes it. A later
	// ordinary message drops a leftover so it cannot stick to another ask.
	if isAskUserResponse {
		if bind, ok := h.takeDiscoveredMCPContinuation(msg.UserID); ok {
			loopCtx.rememberDiscoveredMCPBind(bind.ServerID, bind.ToolName)
		}
	} else if !msg.IsBackground {
		h.dropDiscoveredMCPContinuation(msg.UserID)
	}
	return loopCtx
}

// sameIMRuntimeIngress compares only host-authenticated envelope fields. It
// deliberately excludes RequestID (a retry key) and PolicyOwnerID (a host
// execution binding which may legitimately differ from the conversation
// owner). A semantic continuation needs its own verified handle; matching this
// envelope merely allows a transport retry to retain its in-flight context.
func sameIMRuntimeIngress(previous, incoming RuntimeContext) bool {
	return previous.Source == incoming.Source &&
		previous.Actor == incoming.Actor &&
		previous.Conversation == incoming.Conversation
}

// imIngressFingerprint binds RequestID retry semantics to the entire
// request-local surface. It intentionally includes the body, attachments and
// client tool/target declarations, but excludes host-managed cancellation,
// trace IDs and CodingTaskIngressToken. The latter is an independently
// one-shot capability and must never become a retry identity input.
func imIngressFingerprint(msg IMUserMessage, runtime RuntimeContext) string {
	type fingerprintInput struct {
		Source             RuntimeSourceRef
		Actor              RuntimeActorRef
		Conversation       RuntimeConversationRef
		Text               string
		MessageType        string
		Lang               string
		Attachments        []MessageAttachment
		ClientTools        []agent.ClientToolDefinition
		ClientToolContext  *agent.ClientToolContext
		DeliveryTarget     *agent.DeliveryTarget
		AssistantBinding   *agent.AssistantBinding
		ClientCapabilities *agent.ClientCapabilities
		StartNewTask       bool
		ResumeSlotID       string
		DismissSlotID      string
		ResumeSessionID    string
		DismissSessionID   string
		UIAction           bool
		SlashCommand       string
	}
	payload, err := json.Marshal(fingerprintInput{
		Source: runtime.Source, Actor: runtime.Actor, Conversation: runtime.Conversation,
		Text: msg.Text, MessageType: msg.MessageType, Lang: msg.Lang,
		Attachments: msg.Attachments, ClientTools: msg.ClientTools,
		ClientToolContext: msg.ClientToolContext, DeliveryTarget: msg.DeliveryTarget,
		AssistantBinding: msg.AssistantBinding, ClientCapabilities: msg.ClientCapabilities,
		StartNewTask: msg.StartNewTask, ResumeSlotID: msg.ResumeSlotID, DismissSlotID: msg.DismissSlotID,
		ResumeSessionID: msg.ResumeRecoverableSessionID, DismissSessionID: msg.DismissRecoverableSessionID,
		UIAction: msg.UIAction, SlashCommand: msg.SlashCommand,
	})
	if err != nil {
		// JSON marshalling only fails for malformed dynamic schemas. Make that a
		// guaranteed fresh value instead of retaining a prior turn on failure.
		return "invalid:" + generateID()
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum[:])
}

// bindLoopDeliveryTarget copies a complete inbound target when it matches this
// turn's channel. Empty or cross-channel pointers are treated as missing so a
// desktop chat can still bind its host-owned current-channel destination.
// Weixin/Lansenger stay fail-closed unless the gateway supplied a real inbound
// destination; this helper must not invent one for them.
func bindLoopDeliveryTarget(loopCtx *LoopContext, msg IMUserMessage) *agent.DeliveryTarget {
	platform := strings.TrimSpace(msg.Platform)
	userID := strings.TrimSpace(msg.UserID)
	if loopCtx != nil {
		if platform == "" {
			platform = strings.TrimSpace(loopCtx.Platform)
		}
		if userID == "" {
			userID = strings.TrimSpace(loopCtx.UserID)
		}
	}
	if target := copyInboundDeliveryTarget(msg.DeliveryTarget, platform); target != nil {
		return target
	}
	return hostOwnedCurrentChannelDeliveryTarget(platform, userID)
}

func copyInboundDeliveryTarget(target *agent.DeliveryTarget, platform string) *agent.DeliveryTarget {
	if target == nil {
		return nil
	}
	destination := strings.TrimSpace(target.DestinationID)
	if !semanticTrustedDispatchDestination(destination) {
		return nil
	}
	scope := strings.TrimSpace(target.ChannelScope)
	want := normalizeIMMessagePlatformKind(platform).ChannelScope()
	if want == "" {
		return nil
	}
	if !strings.EqualFold(scope, want) && !strings.EqualFold(scope, strings.TrimSpace(platform)) {
		return nil
	}
	return &agent.DeliveryTarget{ChannelScope: want, DestinationID: destination}
}

func trustedLoopDeliveryTarget(loop *LoopContext) *agent.DeliveryTarget {
	if loop == nil || loop.DeliveryTarget == nil {
		return nil
	}
	rawScope := strings.TrimSpace(loop.DeliveryTarget.ChannelScope)
	destination := strings.TrimSpace(loop.DeliveryTarget.DestinationID)
	if !semanticTrustedDispatchDestination(destination) {
		return nil
	}
	scope := normalizeIMMessagePlatformKind(rawScope).ChannelScope()
	if scope == "" {
		return nil
	}
	return &agent.DeliveryTarget{ChannelScope: scope, DestinationID: destination}
}

// hostOwnedCurrentChannelDeliveryTarget returns the trusted current-channel
// destination for local chat UIs that never attach an inbound IM target.
func hostOwnedCurrentChannelDeliveryTarget(platform, userID string) *agent.DeliveryTarget {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	kind := normalizeIMMessagePlatformKind(platform)
	switch kind {
	case imMessagePlatformDesktop, imMessagePlatformTUI:
		destination := "user:" + userID
		if !semanticTrustedDispatchDestination(destination) {
			return nil
		}
		return &agent.DeliveryTarget{
			ChannelScope:  kind.ChannelScope(),
			DestinationID: destination,
		}
	default:
		return nil
	}
}

// beginPureCodingRuntime registers a pure local/remote coding SubAgent turn as
// the active session loop so guide-launch inject, cancel, and cancel-wait work
// the same way as the main agent loop. Returns a no-op when ctx is nil.
//
// Any pre-loop guide accumulated while the previous turn was ending (or during
// plan-approve wait) is promoted into pendingInjection so it is not lost to the
// short pre-loop TTL before the SubAgent's first TransformConversation drain.
func (h *IMMessageHandler) beginPureCodingRuntime(ctx *LoopContext, userID, userText string) func() {
	if h == nil || ctx == nil {
		return func() {}
	}
	platform := strings.TrimSpace(ctx.Platform)
	if platform == "" {
		platform = "desktop"
	}
	cleanup := h.beginAgentLoopRuntime(ctx, userID, userText, platform)
	return cleanup
}

// promotePreLoopGuideToPendingInjection moves a pending pre-loop guide into the
// mid-loop injection bag (guide-launch format) so pure-coding SubAgents can
// consume it on the next LLM round without the 30s pre-loop expiry race.
func (h *IMMessageHandler) promotePreLoopGuideToPendingInjection(userID string) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return
	}
	raw, ok := h.pendingPreLoopGuide.LoadAndDelete(userID)
	if !ok {
		return
	}
	entry, isEntry := raw.(*preLoopGuideEntry)
	if !isEntry || entry == nil {
		return
	}
	text := strings.TrimSpace(entry.Text)
	if text == "" {
		return
	}
	// Keep a generous window so plan-approve waits still promote; drop only
	// clearly abandoned guides (far beyond the active-session accept window).
	if !entry.CreatedAt.IsZero() && time.Since(entry.CreatedAt) > 2*time.Minute {
		log.Printf("[injection] user=%s dropped stale pre-loop guide on pure-coding start (age=%v)", userID, time.Since(entry.CreatedAt).Round(time.Second))
		return
	}
	injection := buildGuideLaunchInjection(text)
	if injection == "" {
		return
	}
	h.accumulateInjection(userID, injection)
	log.Printf("[injection] user=%s promoted pre-loop guide to pendingInjection for pure coding", userID)
}

func (h *IMMessageHandler) beginAgentLoopRuntime(ctx *LoopContext, userID, userText, platform string) func() {
	requestID := ""
	loopID := ""
	if ctx != nil {
		requestID = ctx.Runtime.RequestID
		loopID = ctx.ID
	}
	cleanupForegroundQoS := func() {}
	if h != nil && h.app != nil && isForegroundAgentLoopRuntime(ctx) {
		cleanupForegroundQoS = h.app.beginForegroundAgentLoop(userID, requestID, loopID)
	} else if ctx != nil && ctx.Kind == LoopKindBackground {
		log.Printf("[agent-qos] foreground_skip_background owner=%q request_id=%q loop=%q slot=%s", userID, requestID, loopID, ctx.SlotKind.String())
	}
	// Tag the loop before it is visible. A pause that lands in this window
	// would otherwise read an empty platform and treat a desktop turn as the
	// goal's own continuation.
	if ctx != nil {
		ctx.Platform = platform
		ctx.UserID = userID
	}
	// Write to per-session state (primary, race-free).
	state := h.getSessionLoop(userID)
	state.stateMu.Lock()
	state.loopCtx = ctx
	state.userText = userText
	state.endedAt = time.Time{}
	state.stateMu.Unlock()
	// The edit may have arrived before this loop was visible. Take it now,
	// before the first iteration, and before a later cleanup can miss it.
	h.adoptGoalCommandSteer(userID)
	h.endInFlightTurn(userID, ctx)
	// A guide can be accepted while the session mutex is held during preflight,
	// before loopCtx is visible. Drain that pre-loop handoff only after publishing
	// this exact consumer so accepted=true always leads to a reachable iteration.
	h.promotePreLoopGuideToPendingInjection(userID)

	// Write to legacy global fields (deprecated, kept for tool functions that
	// don't have access to userID). Under concurrency these may be overwritten
	// by another session — tools should migrate to LoopContext parameter passing.
	h.globalLoopMu.Lock()
	h.currentLoopCtx = ctx
	h.lastUserText = userText
	h.lastUserID = userID
	h.globalLoopMu.Unlock()

	if h.traceService != nil && ctx.RunID != "" {
		h.traceService.SetRunLoopID(ctx.RunID, ctx.ID)
		h.appendTraceEvent(ctx, "loop.started", "info", "Agent loop started", h.runtimeTraceSummary(ctx, userText), "", "")
	}
	return func() {
		cleanupForegroundQoS()
		// Clear per-session state only if this is still the active loop. A
		// cancellation may time out and deliberately unblock a replacement
		// request before the old goroutine actually returns. In that case the
		// old cleanup must never clear the replacement loop's context, task
		// text, or newly arrived steering injection.
		state.stateMu.Lock()
		isCurrentLoop := state.loopCtx == ctx
		if isCurrentLoop {
			state.loopCtx = nil
			state.userText = ""
			state.endedAt = time.Now()
			// Keep cleanup inside the same publication lock. Otherwise a replacement
			// loop can publish itself after loopCtx is cleared but before this call,
			// and the old cleanup may delete the replacement's freshly accepted
			// supplementary input. Guide entries happened to survive the filter, but
			// ordinary live speech did not.
			h.clearNonGuidePendingInjection(userID)
		}
		state.stateMu.Unlock()
		// Clear legacy global fields only if they still point to THIS loop.
		// Under concurrency another loop may have overwritten them — don't
		// clobber the other loop's state.
		h.globalLoopMu.Lock()
		if h.currentLoopCtx == ctx {
			h.currentLoopCtx = nil
			h.lastUserText = ""
			h.lastUserID = ""
		}
		h.globalLoopMu.Unlock()
		ctx.Done()
		if isCurrentLoop && h.app != nil {
			h.app.noteRunWindowSessionSettled(userID)
		}
	}
}

func isForegroundAgentLoopRuntime(ctx *LoopContext) bool {
	return ctx == nil || ctx.Kind != LoopKindBackground
}

func (h *IMMessageHandler) clearNonGuidePendingInjection(userID string) {
	for {
		pending, ok := h.pendingInjection.Load(userID)
		if !ok {
			return
		}
		text, _ := pending.(string)
		if guideOnly := trimToGuideLaunchReferenceInjection(text); guideOnly != "" {
			if guideOnly == text || h.pendingInjection.CompareAndSwap(userID, pending, guideOnly) {
				return
			}
			continue
		}
		if h.pendingInjection.CompareAndDelete(userID, pending) {
			return
		}
	}
}

func trimToGuideLaunchReferenceInjection(text string) string {
	var guides []string
	for _, pending := range splitPendingInjections(text) {
		if isGuideLaunchReferenceInjection(pending) {
			guides = append(guides, pending)
		}
	}
	return strings.Join(guides, pendingInjectionSeparator)
}

func isLegacyInjectionPrefixLine(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") || strings.HasPrefix(line, guideLaunchReferenceMarker) {
		return false
	}
	idx := strings.Index(line, "] ")
	return idx > 0 && idx < 80
}
