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

func goalBarHidden(userID string) map[string]interface{} {
	return map[string]interface{}{
		"visible":       false,
		"user_id":       userID,
		"goal_id":       "",
		"objective":     "",
		"status":        "",
		"phase":         "",
		"turns_used":    0,
		"max_turns":     0,
		"summary":       "",
		"awaiting_user": false,
		"created_at":    int64(0),
	}
}

func (a *App) goalBarSnapshot(userID string) map[string]interface{} {
	userID = strings.TrimSpace(userID)
	store := a.sessionGoalStore()
	if store == nil || userID == "" {
		return goalBarHidden(userID)
	}
	g := store.Get(userID)
	if g == nil || g.Status == goal.StatusComplete {
		return goalBarHidden(userID)
	}
	awaiting := false
	if a.imHandler != nil {
		awaiting = a.imHandler.freshPendingAskUser(userID)
	}
	return map[string]interface{}{
		"visible":       true,
		"user_id":       userID,
		"goal_id":       g.GoalID,
		"objective":     g.Objective,
		"status":        string(g.Status),
		"phase":         string(g.EffectivePhase()),
		"turns_used":    g.TurnsUsed,
		"max_turns":     g.MaxTurns,
		"summary":       g.Summary,
		"awaiting_user": awaiting,
		"created_at":    goalCreatedUnix(g),
	}
}

// UpdateSessionGoal rewrites the goal command. Steps and status stay.
// An empty command is ignored. Deleting the goal is CancelSessionGoal.
func (a *App) UpdateSessionGoal(sessionKey, objective string) map[string]interface{} {
	userID := strings.TrimSpace(sessionKey)
	objective = strings.TrimSpace(objective)
	store := a.sessionGoalStore()
	if store == nil || userID == "" || objective == "" {
		return a.goalBarSnapshot(userID)
	}
	g := store.Get(userID)
	if g == nil || g.Status == goal.StatusComplete {
		return a.goalBarSnapshot(userID)
	}
	if !store.UpdateObjective(userID, g.GoalID, objective) {
		return a.goalBarSnapshot(userID)
	}
	// A decomposed goal already has its checklist and session plan. Rewriting
	// those from the new command would drop the steps. Only a goal that is
	// still a single line mirrors the command into the session plan.
	if updated := store.Get(userID); updated != nil && a.imHandler != nil &&
		strings.TrimSpace(updated.Understanding) == "" && len(updated.Steps) == 0 {
		a.imHandler.syncCodingWorkbenchSessionPlanFromGoal(userID, objective)
	}
	if a.goalContinuation != nil {
		if updated := store.Get(userID); updated != nil {
			a.goalContinuation.emitGoalStateChanged(userID, updated)
		}
	}
	return a.goalBarSnapshot(userID)
}

// GetSessionGoalBar returns the goal bar for one assistant session.
// An empty snapshot means the bar stays hidden.
func (a *App) GetSessionGoalBar(sessionKey string) map[string]interface{} {
	return a.goalBarSnapshot(strings.TrimSpace(sessionKey))
}

// PauseSessionGoal stops automatic continuation. The goal stays on the bar.
func (a *App) PauseSessionGoal(sessionKey string) map[string]interface{} {
	userID := strings.TrimSpace(sessionKey)
	store := a.sessionGoalStore()
	if store == nil || userID == "" {
		return goalBarHidden(userID)
	}
	g := store.Get(userID)
	if g == nil || g.Status != goal.StatusActive {
		return a.goalBarSnapshot(userID)
	}
	store.Pause(userID, g.GoalID)
	if a.goalContinuation != nil {
		a.goalContinuation.CancelPending(userID)
		if updated := store.Get(userID); updated != nil {
			a.goalContinuation.emitGoalStateChanged(userID, updated)
		}
	}
	return a.goalBarSnapshot(userID)
}

// ContinueSessionGoal starts the same goal again after any stop other than
// success or an explicit delete. A dead remote SSH session is not run on
// this desktop: the goal stays paused and the bar keeps the reason.
func (a *App) ContinueSessionGoal(sessionKey string) map[string]interface{} {
	userID := strings.TrimSpace(sessionKey)
	store := a.sessionGoalStore()
	if store == nil || userID == "" {
		return goalBarHidden(userID)
	}
	g := store.Get(userID)
	if g == nil || g.Status == goal.StatusComplete {
		return a.goalBarSnapshot(userID)
	}
	if h := a.imHandler; h != nil {
		h.ensurePureCodingArmedForGoalContinuation(userID)
		if _, blocked := h.pauseGoalForUnarmedRemote(userID); blocked {
			return a.goalBarSnapshot(userID)
		}
		h.pendingAskUser.Delete(userID)
		// Record the stop fence before the goal becomes active again.
		// The dying turn then leaves this timer alone.
		h.noteGoalContinuePastUserStop(userID)
	}
	if !store.ReleaseForContinue(userID, g.GoalID) {
		return a.goalBarSnapshot(userID)
	}
	if a.goalContinuation != nil {
		a.goalContinuation.scheduleDelayed(userID, g.GoalID)
		if updated := store.Get(userID); updated != nil {
			a.goalContinuation.emitGoalStateChanged(userID, updated)
		}
	}
	return a.goalBarSnapshot(userID)
}

// CancelSessionGoal deletes the goal. The bar hides. The checklist card
// already in the transcript is left in place.
func (a *App) CancelSessionGoal(sessionKey string) map[string]interface{} {
	userID := strings.TrimSpace(sessionKey)
	if a.goalContinuation != nil {
		a.goalContinuation.CancelPending(userID)
	}
	store := a.sessionGoalStore()
	if store != nil && userID != "" {
		store.Clear(userID)
	}
	if h := a.imHandler; h != nil && h.goalStore != nil && h.goalStore != store {
		h.goalStore.Clear(userID)
	}
	if a.goalContinuation != nil {
		a.goalContinuation.emitGoalCleared(userID)
	}
	return goalBarHidden(userID)
}
