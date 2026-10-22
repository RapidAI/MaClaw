package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

// Session goal bar. The chat checklist stays in the transcript. This API only
// drives the bar above the composer: pause, continue after any stop, or
// delete. A completed goal is hidden. Any other remaining goal stays visible
// until the user deletes it or the goal succeeds.

func (a *App) sessionGoalStore() *goal.Store {
	if a == nil || a.goalContinuation == nil {
		return nil
	}
	return a.goalContinuation.store
}

// goalBarCandidateIDs lists the store keys for one chat session.
// The composer asks with desktop-user:F:/bird2. Windows stores that same
// project as desktop-user:F:\bird2, and the drive letter may differ in case.
// Only separator and drive-letter spelling change. Path cleaning is not
// applied: F:/bird2/../other must not address the goal for F:/other.
func goalBarCandidateIDs(sessionKey string) []string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, 4)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(sessionKey)
	flipped := flipProjectSessionSlashes(sessionKey)
	add(flipped)
	add(upperDriveSessionKey(sessionKey))
	add(upperDriveSessionKey(flipped))
	return ids
}

func flipProjectSessionSlashes(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	prefix := desktopUserID + ":"
	if !strings.HasPrefix(sessionKey, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(sessionKey, prefix)
	if strings.HasPrefix(rest, "expert:") || strings.HasPrefix(rest, "acp:") || strings.HasPrefix(rest, "task:") {
		return ""
	}
	switch {
	case strings.Contains(rest, "\\"):
		return prefix + strings.ReplaceAll(rest, "\\", "/")
	case strings.Contains(rest, "/"):
		return prefix + strings.ReplaceAll(rest, "/", "\\")
	default:
		return ""
	}
}

func upperDriveSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	prefix := desktopUserID + ":"
	if !strings.HasPrefix(sessionKey, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(sessionKey, prefix)
	if len(rest) < 2 || rest[1] != ':' || rest[0] < 'a' || rest[0] > 'z' {
		return ""
	}
	return prefix + strings.ToUpper(rest[:1]) + rest[1:]
}

// goalBarHit is the row a composer session addresses. The continuation store
// wins when it has any row. A handler-only row still shows the bar.
type goalBarHit struct {
	id    string
	store *goal.Store
	goal  *goal.Goal
}

func (a *App) goalBarStores() []*goal.Store {
	if a == nil {
		return nil
	}
	var stores []*goal.Store
	if store := a.sessionGoalStore(); store != nil {
		stores = append(stores, store)
	}
	if a.imHandler != nil && a.imHandler.goalStore != nil {
		same := false
		for _, store := range stores {
			if store == a.imHandler.goalStore {
				same = true
				break
			}
		}
		if !same {
			stores = append(stores, a.imHandler.goalStore)
		}
	}
	return stores
}

// goalBarHit resolves one composer session to the row the bar should drive.
// Slash and drive spellings share a session, and one store can hold both.
// An active row wins inside that store, then any other unfinished row, then
// a completed row. The same rank keeps the later update, so pausing the live
// row does not fall through to an older twin. The next store is used only
// when this store has none of those rows.
func (a *App) goalBarHit(sessionKey string) goalBarHit {
	ids := goalBarCandidateIDs(sessionKey)
	for _, store := range a.goalBarStores() {
		var best goalBarHit
		for _, id := range ids {
			g := store.Get(id)
			if g == nil {
				continue
			}
			next := goalBarHit{id: id, store: store, goal: g}
			if goalBarPrefer(next, best) {
				best = next
			}
		}
		if best.goal != nil {
			return best
		}
	}
	return goalBarHit{}
}

// goalBarPrefer reports whether next replaces current. Active outranks a
// paused or failed twin. A finished row loses to both.
func goalBarPrefer(next, current goalBarHit) bool {
	if current.goal == nil {
		return next.goal != nil
	}
	if next.goal == nil {
		return false
	}
	nextRank, currentRank := goalBarLiveRank(next.goal), goalBarLiveRank(current.goal)
	if nextRank != currentRank {
		return nextRank > currentRank
	}
	return next.goal.UpdatedAt.After(current.goal.UpdatedAt)
}

func goalBarLiveRank(g *goal.Goal) int {
	if g == nil {
		return 0
	}
	switch g.Status {
	case goal.StatusActive:
		return 3
	case goal.StatusComplete:
		return 1
	default:
		return 2
	}
}

// cancelGoalBarTimers stops a continuation armed under either slash spelling.
// CancelPending also drops a spent plan-miss. Pause and delete want that.
// Continue does not: it uses cancelOtherGoalBarTimers so the chosen row keeps
// the one correction it has already spent.
func (a *App) cancelGoalBarTimers(ids ...string) {
	if a == nil || a.goalContinuation == nil {
		return
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		for _, candidate := range goalBarCandidateIDs(id) {
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			a.goalContinuation.CancelPending(candidate)
		}
	}
}

// eachSameGoal applies one mutation to every store row with this goal id.
// Slash and drive spellings share a session, but an older twin with a
// different goal id is left alone.
func (a *App) eachSameGoal(hit goalBarHit, fn func(store *goal.Store, userID string, g *goal.Goal)) {
	if a == nil || hit.goal == nil || strings.TrimSpace(hit.goal.GoalID) == "" || fn == nil {
		return
	}
	goalID := hit.goal.GoalID
	type rowKey struct {
		store *goal.Store
		id    string
	}
	seen := map[rowKey]struct{}{}
	for _, store := range a.goalBarStores() {
		if store == nil {
			continue
		}
		for _, id := range goalBarCandidateIDs(hit.id) {
			key := rowKey{store, id}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			row := store.Get(id)
			if row == nil || row.GoalID != goalID {
				continue
			}
			fn(store, id, row)
		}
	}
}

// stopGoalBarLoops stops the turn that is carrying this goal.
// It does not fence the next chat message. Pause and delete use it so the
// current model call does not keep running after the bar has changed state.
func (a *App) stopGoalBarLoops(userID string) {
	if a == nil || a.imHandler == nil {
		return
	}
	for _, id := range goalBarCandidateIDs(userID) {
		a.imHandler.stopGoalBarLoop(id)
	}
}

func (h *IMMessageHandler) stopGoalBarLoop(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.cancelSessionRoundContinuation(userID)
	ctx, taskText, _ := h.loopContextForCancel(userID)
	if ctx != nil && !ctx.IsCancelled() {
		if h.app != nil && h.app.goalContinuation != nil {
			h.app.goalContinuation.noteGoalBarHold(userID)
		}
		ctx.MarkUserCancel()
		ctx.Cancel()
	} else if strings.TrimSpace(taskText) != "" {
		// The turn holds the session but has not published a loop yet.
		// Mark that generation. The turn cancels itself when it builds the
		// loop. The next message is a newer generation and is not fenced.
		h.noteGoalBarStopPending(userID)
	}
	// The generation in flight, or the one that just finished and has not been
	// replaced, must not publish a question after this stop. Note it before
	// dropping the question, so a late commit cannot write the question back.
	// A later message uses a newer generation. This is not a chat fence, and
	// it is not the one-shot unpublished-turn stop.
	h.noteGoalBarNoQuestion(userID)
	// A continuation may already have passed the cooldown check and not yet
	// entered the session. Bump its admit generation so that turn does not
	// start. Continue does not come here, so the next fire can admit again.
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.revokeContinuationAdmit(userID)
	}
	// The stopped turn does not run another iteration. A steer queued for it,
	// including one still waiting for publish, would be read by the next chat
	// message. The goal row already holds the command the next continuation
	// reads. This does not fence that message.
	h.pendingInjection.Delete(userID)
	h.dropGoalCommandSteer(userID)
	// A question must not keep owning the next message after the goal stops.
	h.pendingAskUser.Delete(userID)
}

// noteGoalBarNoQuestion records the foreground generation a bar stop silences.
// A generation of zero means this session has no turn to silence. The mark
// stays until a newer one replaces it. Continue does not clear it.
func (h *IMMessageHandler) noteGoalBarNoQuestion(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	gen := h.pendingForegroundGen(userID)
	if userID == "" || gen == 0 {
		return
	}
	h.goalBarQuestionMu.Lock()
	h.goalBarNoQuestionGen.Store(userID, gen)
	h.goalBarQuestionMu.Unlock()
}

// goalBarTurnMustNotAsk reports that this foreground generation belongs to
// a turn the bar already stopped. The caller holds goalBarQuestionMu when
// the result decides a write. An unmatched newer generation may ask.
func (h *IMMessageHandler) goalBarTurnMustNotAsk(userID string, gen uint64) bool {
	if h == nil || gen == 0 {
		return false
	}
	raw, ok := h.goalBarNoQuestionGen.Load(strings.TrimSpace(userID))
	if !ok {
		return false
	}
	stopped, _ := raw.(uint64)
	if stopped == 0 {
		return false
	}
	return gen <= stopped
}

// goalBarQuestionBlocked is the locked form of goalBarTurnMustNotAsk.
func (h *IMMessageHandler) goalBarQuestionBlocked(userID string, gen uint64) bool {
	if h == nil {
		return false
	}
	h.goalBarQuestionMu.Lock()
	defer h.goalBarQuestionMu.Unlock()
	return h.goalBarTurnMustNotAsk(userID, gen)
}

// forgetPendingUserReply drops a prose question unless this generation was
// stopped. A stopped turn must not erase a question the goal engine kept.
func (h *IMMessageHandler) forgetPendingUserReply(userID string, turnGen uint64) {
	if h == nil {
		return
	}
	h.goalBarQuestionMu.Lock()
	defer h.goalBarQuestionMu.Unlock()
	if h.goalBarTurnMustNotAsk(userID, turnGen) {
		return
	}
	h.pendingUserReply.Delete(userID)
}

// rememberPendingUserReply stores a prose question unless this generation
// was stopped. The mark and the store share one lock.
func (h *IMMessageHandler) rememberPendingUserReply(userID string, turnGen uint64, state *pendingUserReplyState) bool {
	if h == nil || state == nil {
		return false
	}
	h.goalBarQuestionMu.Lock()
	defer h.goalBarQuestionMu.Unlock()
	if h.goalBarTurnMustNotAsk(userID, turnGen) {
		return false
	}
	h.pendingUserReply.Store(userID, state)
	return true
}

// noteGoalBarStopPending marks the foreground generation that is waiting to
// publish a loop. A generation of zero means there is nothing to stop.
func (h *IMMessageHandler) noteGoalBarStopPending(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	gen := h.pendingForegroundGen(userID)
	if userID == "" || gen == 0 {
		return
	}
	h.goalBarStopGen.Store(userID, gen)
}

// takeGoalBarStop reports that this turn's generation was stopped from the
// goal bar. The mark is one shot. A later message has a newer generation and
// is not stopped. ctx is cancelled when present.
func (h *IMMessageHandler) takeGoalBarStop(userID string, ctx *LoopContext) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	raw, ok := h.goalBarStopGen.Load(userID)
	if !ok {
		return false
	}
	stopped, _ := raw.(uint64)
	gen := h.pendingForegroundGen(userID)
	if gen == 0 || stopped != gen {
		if gen != stopped {
			h.goalBarStopGen.Delete(userID)
		}
		return false
	}
	h.goalBarStopGen.Delete(userID)
	if ctx != nil && !ctx.IsCancelled() {
		ctx.MarkUserCancel()
		ctx.Cancel()
	}
	return true
}

// steerGoalCommand tells the turn that owns the session about a rewritten
// command. No such turn returns false. The next continuation reads the stored
// command either way.
func (h *IMMessageHandler) steerGoalCommand(userID, objective string) bool {
	if h == nil {
		return false
	}
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return false
	}
	note := "[用户补充] 目标命令已改为：" + objective + "\n请按这个新命令继续。已经完成的步骤不要重做。"
	// The stop fence blocks ordinary supplements. A continued goal is still
	// the live loop, so the rewritten command uses the loop path directly.
	// A turn that holds the session before publish keeps the same note and
	// reads it on the first iteration. An idle session does not: the next
	// continuation reads the stored command, and a chat message must not.
	for _, id := range goalBarCandidateIDs(userID) {
		if h.deliverGoalCommandSteer(id, note) {
			return true
		}
	}
	return false
}

// deliverGoalCommandSteer hands the rewritten command to the turn that owns
// this session. A published loop takes it now. A turn that has not published
// yet keeps it until publish. No such turn returns false.
func (h *IMMessageHandler) deliverGoalCommandSteer(userID, note string) bool {
	if h.acceptLiveLoopSteer(userID, note) {
		return true
	}
	return h.holdGoalCommandSteerForPendingTurn(userID, note)
}

// holdGoalCommandSteerForPendingTurn keeps the command for a turn that has
// the session lock and has not published a loop. A stop mark or a chat fence
// means that turn will not start, so the note is not kept for whoever runs next.
func (h *IMMessageHandler) holdGoalCommandSteerForPendingTurn(userID, note string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	note = strings.TrimSpace(note)
	if userID == "" || note == "" {
		return false
	}
	v, ok := h.sessionLoops.Load(userID)
	if !ok {
		return false
	}
	state := v.(*sessionLoopState)
	state.stateMu.Lock()
	if state.loopCtx != nil {
		state.stateMu.Unlock()
		// Published between the first attempt and this lock.
		return h.acceptLiveLoopSteer(userID, note)
	}
	pending := strings.TrimSpace(state.pendingText)
	gen := state.pendingGen
	stopPending := h.goalBarStopMatches(userID, gen)
	fenced := h.hasCancelledTaskBoundary(userID)
	if pending == "" || stopPending || fenced {
		state.stateMu.Unlock()
		return false
	}
	h.goalBarSteer.Store(userID, note)
	state.stateMu.Unlock()
	return true
}

func (h *IMMessageHandler) goalBarStopMatches(userID string, gen uint64) bool {
	if h == nil || gen == 0 {
		return false
	}
	raw, ok := h.goalBarStopGen.Load(strings.TrimSpace(userID))
	if !ok {
		return false
	}
	stopped, _ := raw.(uint64)
	return stopped == gen
}

// adoptGoalCommandSteer moves a command held for the unpublished turn into
// the injection the first loop iteration reads. Caller publishes the loop
// before this, so a later edit takes the live-loop path instead.
func (h *IMMessageHandler) adoptGoalCommandSteer(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	raw, ok := h.goalBarSteer.LoadAndDelete(userID)
	if !ok {
		return
	}
	text, _ := raw.(string)
	if strings.TrimSpace(text) == "" {
		return
	}
	h.accumulateInjection(userID, text)
}

func (h *IMMessageHandler) dropGoalCommandSteer(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.goalBarSteer.Delete(userID)
}

// cancelOtherGoalBarTimers stops a timer armed under the other spelling.
// The chosen id is left alone. scheduleDelayed already replaces that timer,
// refunds an unsent plan correction, and keeps a correction that already ran.
func (a *App) cancelOtherGoalBarTimers(userID string) {
	if a == nil || a.goalContinuation == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	for _, candidate := range goalBarCandidateIDs(userID) {
		if candidate == "" || candidate == userID {
			continue
		}
		a.goalContinuation.CancelPending(candidate)
	}
}

func goalBarHidden(userID string) map[string]interface{} {
	return map[string]interface{}{
		"visible":            false,
		"user_id":            userID,
		"goal_id":            "",
		"objective":          "",
		"status":             "",
		"phase":              "",
		"turns_used":         0,
		"max_turns":          0,
		"summary":            "",
		"awaiting_user":      false,
		"created_at":         int64(0),
		"stopped_at":         int64(0),
		"clock_hold_seconds": int64(0),
	}
}

func (a *App) goalBarSnapshotIn(store *goal.Store, userID string, g *goal.Goal) map[string]interface{} {
	userID = strings.TrimSpace(userID)
	if store == nil || userID == "" {
		return goalBarHidden(userID)
	}
	if g == nil {
		g = store.Get(userID)
	}
	if g == nil || g.Status == goal.StatusComplete {
		return goalBarHidden(userID)
	}
	awaiting := false
	if a.imHandler != nil {
		awaiting = a.imHandler.freshPendingAskUser(userID)
	}
	return map[string]interface{}{
		"visible":            true,
		"user_id":            userID,
		"goal_id":            g.GoalID,
		"objective":          g.Objective,
		"status":             string(g.Status),
		"phase":              string(g.EffectivePhase()),
		"turns_used":         g.TurnsUsed,
		"max_turns":          g.MaxTurns,
		"summary":            g.Summary,
		"awaiting_user":      awaiting,
		"created_at":         goalCreatedUnix(g),
		"stopped_at":         goalStoppedUnix(g),
		"clock_hold_seconds": goalClockHoldSeconds(g),
	}
}

// UpdateSessionGoal rewrites the goal command. Steps and status stay.
// An empty command is ignored. Deleting the goal is CancelSessionGoal.
func (a *App) UpdateSessionGoal(sessionKey, objective string) map[string]interface{} {
	hit := a.goalBarHit(sessionKey)
	userID, store, g := hit.id, hit.store, hit.goal
	objective = strings.TrimSpace(objective)
	if store == nil || userID == "" || g == nil || objective == "" || g.Status == goal.StatusComplete {
		if store == nil || userID == "" || g == nil {
			return goalBarHidden(strings.TrimSpace(sessionKey))
		}
		return a.goalBarSnapshotIn(store, userID, g)
	}
	previous := strings.TrimSpace(g.Objective)
	if !store.UpdateObjective(userID, g.GoalID, objective) {
		return a.goalBarSnapshotIn(store, userID, g)
	}
	a.eachSameGoal(hit, func(other *goal.Store, id string, row *goal.Goal) {
		if other == store && id == userID {
			return
		}
		other.UpdateObjective(id, row.GoalID, objective)
	})
	updated := store.Get(userID)
	// A decomposed goal already has its checklist and session plan. Rewriting
	// those from the new command would drop the steps. Only a goal that is
	// still a single line mirrors the command into the session plan.
	if updated != nil && a.imHandler != nil &&
		strings.TrimSpace(updated.Understanding) == "" && len(updated.Steps) == 0 {
		a.imHandler.syncCodingWorkbenchSessionPlanFromGoal(userID, objective)
	}
	if previous != objective && a.imHandler != nil {
		a.imHandler.steerGoalCommand(userID, objective)
	}
	if a.goalContinuation != nil && updated != nil {
		a.goalContinuation.emitGoalStateChanged(userID, updated)
	}
	return a.goalBarSnapshotIn(store, userID, updated)
}

// GetSessionGoalBar returns the goal bar for one assistant session.
// An empty snapshot means the bar stays hidden.
// The first read of a goal that was still active when this process started
// arms its continuation. The bar is painted from the persisted row, so it
// already says the goal is running; this is what makes the agent start.
func (a *App) GetSessionGoalBar(sessionKey string) map[string]interface{} {
	a.resumeVisibleGoalAfterRestart(sessionKey)
	hit := a.goalBarHit(sessionKey)
	if hit.goal == nil || hit.goal.Status == goal.StatusComplete {
		userID := strings.TrimSpace(sessionKey)
		if hit.goal != nil {
			userID = hit.id
		}
		return goalBarHidden(userID)
	}
	return a.goalBarSnapshotIn(hit.store, hit.id, hit.goal)
}

// PauseSessionGoal stops automatic continuation. The goal stays on the bar.
func (a *App) PauseSessionGoal(sessionKey string) map[string]interface{} {
	hit := a.goalBarHit(sessionKey)
	userID, store, g := hit.id, hit.store, hit.goal
	if store == nil || userID == "" || g == nil {
		return goalBarHidden(strings.TrimSpace(sessionKey))
	}
	if g.Status != goal.StatusActive {
		return a.goalBarSnapshotIn(store, userID, g)
	}
	a.eachSameGoal(hit, func(rowStore *goal.Store, id string, row *goal.Goal) {
		if row.Status != goal.StatusActive {
			return
		}
		rowStore.UpdateStatus(id, row.GoalID, goal.StatusPaused, "用户已暂停")
	})
	a.cancelGoalBarTimers(userID)
	a.stopGoalBarLoops(userID)
	updated := store.Get(userID)
	if a.goalContinuation != nil && updated != nil {
		a.goalContinuation.emitGoalStateChanged(userID, updated)
	}
	return a.goalBarSnapshotIn(store, userID, updated)
}

// ContinueSessionGoal starts the same goal again after any stop other than
// success or an explicit delete. A dead remote SSH session is not run on
// this desktop: the goal stays paused and the bar keeps the reason.
func (a *App) ContinueSessionGoal(sessionKey string) map[string]interface{} {
	hit := a.goalBarHit(sessionKey)
	userID, store, g := hit.id, hit.store, hit.goal
	if store == nil || userID == "" || g == nil {
		return goalBarHidden(strings.TrimSpace(sessionKey))
	}
	if g.Status == goal.StatusComplete {
		return a.goalBarSnapshotIn(store, userID, g)
	}
	if h := a.imHandler; h != nil {
		h.ensurePureCodingArmedForGoalContinuation(userID)
		if _, blocked := h.pauseGoalForUnarmedRemote(userID); blocked {
			return a.goalBarSnapshotIn(store, userID, nil)
		}
		h.pendingAskUser.Delete(userID)
		// Record the stop fence before the goal becomes active again.
		// The dying turn then leaves this timer alone.
		h.noteGoalContinuePastUserStop(userID)
	}
	if !store.ReleaseForContinue(userID, g.GoalID) {
		return a.goalBarSnapshotIn(store, userID, nil)
	}
	a.eachSameGoal(hit, func(other *goal.Store, id string, row *goal.Goal) {
		if (other == store && id == userID) || row.Status == goal.StatusComplete {
			return
		}
		other.ReleaseForContinue(id, row.GoalID)
	})
	// The other slash spelling can still hold a timer. Drop that one only.
	// scheduleDelayed replaces the chosen id's timer and keeps a plan
	// correction that has already been sent.
	a.cancelOtherGoalBarTimers(userID)
	if a.goalContinuation != nil {
		a.goalContinuation.scheduleDelayed(userID, g.GoalID)
	}
	updated := store.Get(userID)
	if a.goalContinuation != nil && updated != nil {
		a.goalContinuation.emitGoalStateChanged(userID, updated)
	}
	return a.goalBarSnapshotIn(store, userID, updated)
}

// CancelSessionGoal deletes the goal. The bar hides. The checklist card
// already in the transcript is left in place.
func (a *App) CancelSessionGoal(sessionKey string) map[string]interface{} {
	hit := a.goalBarHit(sessionKey)
	userID := hit.id
	if userID == "" {
		userID = strings.TrimSpace(sessionKey)
	}
	ids := goalBarCandidateIDs(userID)
	a.stopGoalBarLoops(userID)
	a.cancelGoalBarTimers(ids...)
	for _, store := range a.goalBarStores() {
		for _, id := range ids {
			store.Clear(id)
		}
	}
	// The dying turn can arm a timer after the first cancel. Drop that one too.
	a.cancelGoalBarTimers(ids...)
	if a.goalContinuation != nil {
		a.goalContinuation.emitGoalCleared(userID)
	}
	return goalBarHidden(userID)
}
