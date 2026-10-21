package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

// isGoalSystemContinuation reports a host-owned goal turn. The text is the
// continuation engine's phase contract (plan, or one execute step), not a
// user request. Callers must not reclassify it as a fresh intent or as a
// coding-workbench file plan.
func isGoalSystemContinuation(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "[系统续接]")
}

// codingLoopIterationExhausted reports the agent loop stopped because its
// iteration budget was spent. A goal continuation treats that as the end of
// one turn. The next turn stays on the same step. The goal itself stays open.
func codingLoopIterationExhausted(errText string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(errText)), "max iterations reached")
}

// goalContinuationPhase is the host phase encoded in the continuation text.
// "plan" must not edit files. "execute" does one step and then reports it.
func goalContinuationPhase(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "[系统续接]") {
		return ""
	}
	if strings.HasPrefix(trimmed, "[系统续接][plan]") || strings.Contains(trimmed, "本轮只做规划") {
		return "plan"
	}
	return "execute"
}

// taskIsGoalPlanPhase reports a host planning turn. It does not write the
// workspace, so coding file audits and the writable-surface gate do not apply.
func taskIsGoalPlanPhase(task *TaskItem, reqCtx string) bool {
	return goalContinuationPhase(goalContinuationSource(task, reqCtx)) == "plan"
}

// goalContinuationTurn is a host phase of an existing goal. Platform is the
// dispatch channel; the marker is the phase contract inside the text.
func goalContinuationTurn(text, platform string) bool {
	return platform == "goal-continuation" || isGoalSystemContinuation(text)
}

// goalPlanPhaseAllowsTool is the plan-phase tool contract for the shared
// loop. Execute and ordinary turns are unchanged. goal is not a read tool,
// but it is the only write the plan turn is allowed to make.
func goalPlanPhaseAllowsTool(userText, name string) bool {
	if goalContinuationPhase(userText) != "plan" {
		return true
	}
	name = strings.TrimSpace(name)
	return name == goalContinuationLifecycleTool || isGoalPlanPhaseTool(name)
}

// goalContinuationPhaseSection is the assignment that outranks a coding
// workbench's generic edit-and-verify prompt. Plan and execute name the
// goal call the host will accept for that phase.
func goalContinuationPhaseSection(text string) string {
	switch goalContinuationPhase(text) {
	case "plan":
		return `## 宿主目标阶段
本轮是已有目标的规划，不是改文件任务。用户消息是宿主写的阶段合同。
只调用 goal(action="plan", understanding, steps, acceptance_criteria)。
不要修改文件，不要运行实现，不要调用 complete。步骤或验收标准为空时不要调用 plan。

`
	case "execute":
		return `## 宿主目标阶段
本轮只执行用户消息里的当前步骤。不要把下一步的工作写进本轮回复。
这一步还没做完时调用 goal(action="step", index, status="progress", result)，下一轮仍是这一步。
交付物完成时调用 status="done"，然后停止。不要把未完成的步骤标成 done。
步骤都完成时调用 goal(action="complete", summary)；无法收尾时调用 goal(action="fail", reason)。

`
	default:
		return ""
	}
}

// goalContinuationSource is the host phase text carried by a coding task.
func goalContinuationSource(task *TaskItem, reqCtx string) string {
	if task != nil {
		if isGoalSystemContinuation(task.Description) {
			return task.Description
		}
		if isGoalSystemContinuation(task.Title) {
			return task.Title
		}
	}
	if isGoalSystemContinuation(reqCtx) {
		return reqCtx
	}
	return ""
}

// goalContinuationLifecycleTool is the registry tool the phase prompt names.
// The closed plan must keep it as host policy, not as a retrieval candidate.
const goalContinuationLifecycleTool = "goal"

func taskCarriesGoalContinuation(task *TaskItem, reqCtx string) bool {
	if isGoalSystemContinuation(reqCtx) {
		return true
	}
	if task == nil {
		return false
	}
	return isGoalSystemContinuation(task.Title) || isGoalSystemContinuation(task.Description)
}

// GoalContinuationEngine manages the auto-continuation loop for active goals.
// It uses a self-message mechanism: after each agent loop completes, if the
// goal is still active and should continue, it schedules a new internal message
// that re-enters the normal message processing pipeline. This ensures:
//   - Proper serialization (chatLoopMu / state.mu)
//   - Full agent loop infrastructure (drift detection, truncation, etc.)
//   - User messages can interleave (normal queue priority)
//   - Frontend sees each continuation as a new round (progress feedback)
type GoalContinuationEngine struct {
	mu              sync.Mutex
	store           *goal.Store
	app             *App
	scheduledTimers map[string]*time.Timer // userID → pending continuation timer
	pendingExpect   map[string]goalContinuationExpect
	// planMiss is set when the one corrective plan turn is scheduled. Dropping
	// that timer before fire refunds the attempt. Once the correction has
	// been dispatched, the next empty plan fails the goal.
	planMiss map[string]goalPlanMiss
	// planCorrection is the goal whose next fire must ask for the missing plan
	// instead of repeating the original planning prompt.
	planCorrection map[string]string
	// scheduleGen invalidates a timer that was already replaced or cancelled.
	// time.AfterFunc can still run after Stop, and that callback must not
	// dispatch or delete the successor entry.
	scheduleGen map[string]uint64
	// stopSuperseded is the cancel fence an explicit continue already re-armed.
	// The dying turn must not pause or fail that goal, and a host continuation
	// of the same fence may enter. A later stop stores a new fence.
	stopSuperseded map[string]cancelledTaskFence
	// expectGen identifies one note of pendingExpect. It only increases.
	// Two notes of an unchanged step must stay distinct, or a late post-loop
	// would retire the successor's note.
	expectGen map[string]uint64
	cooldown  time.Duration // delay between continuations (default 2s)
}

// goalPlanMiss records that this goal already received one plan correction.
type goalPlanMiss struct {
	goalID  string
	retried bool
}

// goalContinuationExpect is the host phase at the moment a continuation is
// fired. The turn is finished only when the goal store records that phase.
// generation belongs to that firing. The step fields alone repeat when the
// next turn starts on the same unwritten step.
type goalContinuationExpect struct {
	goalID     string
	phase      goal.Phase
	stepIndex  int
	stepStatus goal.StepStatus
	stepResult string
	generation uint64
}

// NewGoalContinuationEngine creates a continuation engine.
func NewGoalContinuationEngine(store *goal.Store, app *App) *GoalContinuationEngine {
	return &GoalContinuationEngine{
		store:           store,
		app:             app,
		scheduledTimers: make(map[string]*time.Timer),
		pendingExpect:   make(map[string]goalContinuationExpect),
		planMiss:        make(map[string]goalPlanMiss),
		planCorrection:  make(map[string]string),
		scheduleGen:     make(map[string]uint64),
		expectGen:       make(map[string]uint64),
		cooldown:        2 * time.Second,
	}
}

// MaybeScheduleContinuation checks if the goal should auto-continue after the
// current agent loop finishes. Called from schedulePostLoopSideEffects.
// It accounts usage from the just-completed turn, then schedules the next
// continuation if the goal is still continuable.
func (e *GoalContinuationEngine) MaybeScheduleContinuation(userID, goalID string, turnTokens, turnSeconds int, hadToolCalls bool) {
	e.finishContinuationTurn(userID, goalID, turnTokens, turnSeconds, hadToolCalls, goalContinuationExpect{}, false)
}

// finishContinuationTurn accounts this continuation and schedules the next one.
// haveOwned means owned is the note captured when this turn ended. A successor
// that already wrote its own note, or a timer already queued for it, stays.
func (e *GoalContinuationEngine) finishContinuationTurn(userID, goalID string, turnTokens, turnSeconds int, hadToolCalls bool, owned goalContinuationExpect, haveOwned bool) {
	if e == nil || e.store == nil {
		return
	}

	g := e.store.Get(userID)
	if g == nil || g.GoalID != goalID {
		return // stale or no goal
	}

	// Account this turn's usage
	e.store.AccountUsage(userID, goalID, turnTokens, turnSeconds)

	// Re-read after accounting (status may have changed to budget_limited).
	// An empty plan is its own contract. Counting it as a no-tool turn first
	// pauses the goal on the second miss and never reaches the failure.
	g = e.store.Get(userID)
	var expect goalContinuationExpect
	if haveOwned {
		// Miss report stays on the note this turn captured. The map may
		// already hold the next turn's note of the same step.
		expect = owned
	} else {
		expect = e.takeContinuationExpect(userID)
	}
	if g != nil && g.Status == goal.StatusActive && g.EffectivePhase() == goal.PhasePlan && len(g.Steps) == 0 {
		// One missed plan used to fail immediately, and the notice was
		// rendered as the user. Give the model one corrective turn that
		// can only submit goal(action=plan). A second miss still fails.
		// Do not arm that turn when this goal can no longer run: fire would
		// drop it and the active goal would keep an empty plan.
		if haveOwned && e.continuationSuperseded(userID, owned) {
			e.takeContinuationExpectIfCurrent(userID, owned)
			log.Printf("[goal-continuation] successor already queued; this turn does not replace it user=%s goal_id=%s", userID, goalID)
			return
		}
		if !g.ShouldContinue() {
			log.Printf("[goal-continuation] empty plan cannot be corrected user=%s goal_id=%s", userID, g.GoalID)
			e.failGoal(userID, g.GoalID, "未能分解任务", true)
			return
		}
		if e.consumePlanMiss(userID, g.GoalID) {
			e.failGoal(userID, g.GoalID, "未能分解任务", true)
			return
		}
		log.Printf("[goal-continuation] plan missing; scheduling one corrective turn user=%s goal_id=%s", userID, g.GoalID)
		e.schedulePlanCorrection(userID, g.GoalID)
		return
	}

	// Track tool call presence for no-tool suppression
	if hadToolCalls {
		e.store.ResetNoToolCounter(userID, goalID)
	} else {
		e.store.RecordNoToolTurn(userID, goalID)
	}
	g = e.store.Get(userID)
	if g != nil {
		e.clearPlanMiss(userID, g.GoalID)
	}
	// A missing step record is not a failed goal. The same CurrentStep is
	// still the work. ShouldContinue below schedules that normal execute
	// turn. Budget, an explicit fail, and two empty turns stay the stops.
	if reason := continuationMissedReport(expect, g); reason != "" && g != nil && g.ShouldContinue() {
		log.Printf("[goal-continuation] step report still open; next turn stays on this step user=%s goal_id=%s reason=%s", userID, g.GoalID, reason)
	}
	if g == nil || !g.ShouldContinue() {
		if haveOwned {
			e.takeContinuationExpectIfCurrent(userID, owned)
		}
		if g != nil && g.IsTerminal() {
			log.Printf("[goal-continuation] goal reached terminal state: user=%s goal_id=%s status=%s", userID, goalID, g.Status)
			e.emitGoalStateChanged(userID, g)
			e.announceGoalState(userID, g)
		} else if g != nil && g.Status == goal.StatusPaused {
			log.Printf("[goal-continuation] goal paused (no-tool suppression): user=%s goal_id=%s turns=%d", userID, goalID, g.TurnsUsed)
			e.emitGoalStateChanged(userID, g)
			e.announceGoalState(userID, g)
		}
		return
	}

	// Schedule continuation with cooldown delay. A note this turn does not
	// own, or a timer already waiting, belongs to the next turn.
	if haveOwned {
		if !e.scheduleOwnedContinuation(userID, goalID, owned) {
			log.Printf("[goal-continuation] successor already queued; this turn does not replace it user=%s goal_id=%s", userID, goalID)
		}
		return
	}
	e.scheduleDelayed(userID, goalID)
}

// pauseGoalForUnarmedRemote records that this goal cannot run on the desktop.
// The session's machine is a remote host and its SSH engine is down. The goal
// stays paused with the reconnect text so the bar can offer continue.
// A connected or local session returns false and is left unchanged.
func (h *IMMessageHandler) pauseGoalForUnarmedRemote(userID string) (string, bool) {
	if h == nil {
		return "", false
	}
	text, blocked := h.unarmedRemoteCodingDesktopTurn(userID)
	if !blocked {
		return "", false
	}
	if h.app == nil || h.app.goalContinuation == nil || h.app.goalContinuation.store == nil {
		return text, true
	}
	engine := h.app.goalContinuation
	g := engine.store.Get(userID)
	if g == nil || g.Status == goal.StatusComplete {
		return text, true
	}
	engine.store.UpdateStatus(userID, g.GoalID, goal.StatusPaused, text)
	engine.CancelPending(userID)
	if updated := engine.store.Get(userID); updated != nil {
		engine.emitGoalStateChanged(userID, updated)
	}
	return text, true
}

// goalUserStopSummary is the bar reason after the user stops the current turn.
// It must not look like a dead SSH session or a no-tool stall.
const goalUserStopSummary = "用户已停止当前回合。"

// noteGoalContinuePastUserStop records that the bar or /goal resume is
// re-arming this same goal. The stop fence stays, so the next ordinary
// message still starts a new task. Only a host continuation of this fence
// is allowed to enter.
func (h *IMMessageHandler) noteGoalContinuePastUserStop(userID string) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	fence, ok := h.currentCancelledTaskFence(userID)
	if !ok {
		return
	}
	h.app.goalContinuation.noteStopSuperseded(userID, fence)
}

func (e *GoalContinuationEngine) noteStopSuperseded(userID string, fence cancelledTaskFence) {
	if e == nil || strings.TrimSpace(userID) == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopSuperseded == nil {
		e.stopSuperseded = map[string]cancelledTaskFence{}
	}
	e.stopSuperseded[userID] = fence
}

func (e *GoalContinuationEngine) stopSupersededLocked(userID string, fence cancelledTaskFence) bool {
	if e == nil || e.stopSuperseded == nil {
		return false
	}
	noted, ok := e.stopSuperseded[userID]
	return ok && sameCancelledTaskFence(noted, fence)
}

// goalContinuationAdmittedPastStop reports a host continuation that an
// explicit continue already re-armed past the current user-stop fence.
// An ordinary message is never admitted.
func (h *IMMessageHandler) goalContinuationAdmittedPastStop(msg IMUserMessage) bool {
	if h == nil || msg.Platform != "goal-continuation" || h.app == nil || h.app.goalContinuation == nil {
		return false
	}
	fence, ok := h.currentCancelledTaskFence(msg.UserID)
	if !ok {
		return false
	}
	engine := h.app.goalContinuation
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.stopSupersededLocked(msg.UserID, fence)
}

// rejectCancelledTaskTurn stops a turn that is still inside a user cancel.
// An explicit goal continue admits only the host continuation.
func (h *IMMessageHandler) rejectCancelledTaskTurn(msg IMUserMessage) *IMAgentResponse {
	if h == nil || !h.hasCancelledTaskBoundary(msg.UserID) || h.goalContinuationAdmittedPastStop(msg) {
		return nil
	}
	return &IMAgentResponse{Text: cancelledTaskReplyText(msg.Text)}
}

// closeGoalTurnForUserStop pauses the goal when this turn was stopped by the
// user, unless an explicit continue already re-armed that same fence.
// True means the caller must not schedule and must not fail the goal.
// The decision and the pause share the engine lock, so a continue that arms
// the next timer cannot be cancelled by the dying turn.
func (h *IMMessageHandler) closeGoalTurnForUserStop(userID string, resp *IMAgentResponse) bool {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return false
	}
	fence, ok := h.currentCancelledTaskFence(userID)
	if !ok {
		return false
	}
	engine := h.app.goalContinuation
	engine.mu.Lock()
	if engine.stopSupersededLocked(userID, fence) {
		engine.mu.Unlock()
		if resp != nil && resp.userStopped {
			log.Printf("[goal-continuation] skipping user-stop pause: continue already re-armed user=%s", userID)
			return true
		}
		return false
	}
	paused := engine.pauseForUserStopLocked(userID)
	var updated *goal.Goal
	if paused && engine.store != nil {
		updated = engine.store.Get(userID)
	}
	engine.mu.Unlock()
	if updated != nil {
		engine.emitGoalStateChanged(userID, updated)
	}
	if paused {
		log.Printf("[goal-continuation] paused: user stopped the turn user=%s", userID)
	}
	return true
}

// pauseForUserStopLocked records the stop and drops the queued turn.
// Caller holds e.mu. An in-flight loop is not stopped.
func (e *GoalContinuationEngine) pauseForUserStopLocked(userID string) bool {
	if e == nil {
		return false
	}
	e.cancelPendingLocked(userID)
	if e.store == nil {
		return false
	}
	g := e.store.Get(userID)
	if g == nil || g.Status == goal.StatusComplete {
		return false
	}
	e.store.UpdateStatus(userID, g.GoalID, goal.StatusPaused, goalUserStopSummary)
	return true
}

// CancelPending cancels any pending continuation for a user (e.g. on user cancel).
// A corrective plan turn that has not fired is refunded with the timer.
// An in-flight loop is not stopped.
func (e *GoalContinuationEngine) CancelPending(userID string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancelPendingLocked(userID)
}

// cancelPendingLocked retires the timer. Caller holds e.mu.
func (e *GoalContinuationEngine) cancelPendingLocked(userID string) {
	if e == nil {
		return
	}
	if timer, ok := e.scheduledTimers[userID]; ok {
		timer.Stop()
		delete(e.scheduledTimers, userID)
		log.Printf("[goal-continuation] cancelled pending continuation: user=%s", userID)
	}
	delete(e.planCorrection, userID)
	delete(e.planMiss, userID)
	e.bumpScheduleGen(userID)
}

// blocksRunWindowCompletion keeps a scheduled row in progress while this
// session still has another goal turn queued or eligible to run.
func (e *GoalContinuationEngine) blocksRunWindowCompletion(userID string) bool {
	if e == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	e.mu.Lock()
	_, pending := e.scheduledTimers[userID]
	e.mu.Unlock()
	if pending {
		return true
	}
	if e.store == nil {
		return false
	}
	g := e.store.Get(userID)
	return g != nil && g.ShouldContinue()
}

// OnUserMessage is called when a user sends a new message. It cancels any
// pending continuation (user takes priority) and resets no-tool counter.
func (e *GoalContinuationEngine) OnUserMessage(userID string) {
	if e == nil {
		return
	}
	// CancelPending refunds an unsent plan correction. The next empty plan
	// can ask once, because this message interrupted the previous attempt.
	e.CancelPending(userID)

	// User message resets no-tool counter (user interaction = external trigger)
	g := e.store.Get(userID)
	if g != nil && g.Status == goal.StatusActive {
		e.store.ResetNoToolCounter(userID, g.GoalID)
	}
}

type goalCorrectionKind int

const (
	goalCorrectionNone goalCorrectionKind = iota
	goalCorrectionPlan
)

// scheduleDelayed schedules a continuation message after the cooldown period.
func (e *GoalContinuationEngine) scheduleDelayed(userID, goalID string) {
	e.scheduleAfterCooldown(userID, goalID, goalCorrectionNone)
}

// schedulePlanCorrection schedules the single turn that must submit the plan
// the previous continuation left unrecorded.
func (e *GoalContinuationEngine) schedulePlanCorrection(userID, goalID string) {
	e.scheduleAfterCooldown(userID, goalID, goalCorrectionPlan)
}

func (e *GoalContinuationEngine) scheduleAfterCooldown(userID, goalID string, kind goalCorrectionKind) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.scheduleAfterCooldownLocked(userID, goalID, kind)
}

// scheduleAfterCooldownLocked arms the next timer. Caller holds e.mu.
func (e *GoalContinuationEngine) scheduleAfterCooldownLocked(userID, goalID string, kind goalCorrectionKind) {
	// Cancel any existing timer for this user
	if existing, ok := e.scheduledTimers[userID]; ok {
		existing.Stop()
	}
	switch kind {
	case goalCorrectionPlan:
		if e.planCorrection == nil {
			e.planCorrection = map[string]string{}
		}
		e.planCorrection[userID] = goalID
	default:
		// A normal continuation replaces the timer above. If that timer was
		// an unsent plan correction, refund the one-shot. The flag is already
		// gone once fire has dispatched it.
		e.refundUnsentPlanCorrection(userID)
	}

	gen := e.bumpScheduleGen(userID)
	timer := time.AfterFunc(e.cooldown, func() {
		e.runScheduledContinuation(userID, goalID, gen)
	})
	e.scheduledTimers[userID] = timer
	log.Printf("[goal-continuation] scheduled next turn in %s: user=%s goal_id=%s", e.cooldown, userID, goalID)
}

// scheduleOwnedContinuation schedules the next turn only when owned is still
// this user's note and no later timer is waiting. The successor's note stays.
func (e *GoalContinuationEngine) scheduleOwnedContinuation(userID, goalID string, owned goalContinuationExpect) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	superseded := e.continuationSupersededLocked(userID, owned)
	if e.pendingExpect != nil && owned.goalID != "" && e.pendingExpect[userID] == owned {
		delete(e.pendingExpect, userID)
	}
	if superseded {
		return false
	}
	e.scheduleAfterCooldownLocked(userID, goalID, goalCorrectionNone)
	return true
}

func (e *GoalContinuationEngine) continuationSuperseded(userID string, owned goalContinuationExpect) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.continuationSupersededLocked(userID, owned)
}

// continuationSupersededLocked reports that a later turn already owns the
// next firing. Caller holds e.mu.
func (e *GoalContinuationEngine) continuationSupersededLocked(userID string, owned goalContinuationExpect) bool {
	if _, pending := e.scheduledTimers[userID]; pending {
		return true
	}
	if e.pendingExpect == nil {
		return false
	}
	current := e.pendingExpect[userID]
	return current.goalID != "" && current.generation != owned.generation
}

// bumpScheduleGen retires every timer already handed to AfterFunc.
// Caller holds e.mu.
func (e *GoalContinuationEngine) bumpScheduleGen(userID string) uint64 {
	if e.scheduleGen == nil {
		e.scheduleGen = map[string]uint64{}
	}
	e.scheduleGen[userID]++
	return e.scheduleGen[userID]
}

// claimScheduledContinuation accepts one timer generation. A stale callback
// leaves the current timer and its plan-correction flag untouched.
func (e *GoalContinuationEngine) claimScheduledContinuation(userID, goalID string, gen uint64) (goalCorrectionKind, bool) {
	if e == nil || gen == 0 {
		return goalCorrectionNone, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.scheduleGen[userID] != gen {
		return goalCorrectionNone, false
	}
	delete(e.scheduledTimers, userID)
	return e.takeScheduledCorrectionLocked(userID, goalID), true
}

func (e *GoalContinuationEngine) runScheduledContinuation(userID, goalID string, gen uint64) {
	corrective, ok := e.claimScheduledContinuation(userID, goalID, gen)
	if !ok {
		return
	}
	e.dispatchContinuation(userID, goalID, corrective)
}

// fireContinuation dispatches the self-message that triggers the next agent loop turn.
func (e *GoalContinuationEngine) fireContinuation(userID, goalID string) {
	e.dispatchContinuation(userID, goalID, e.takeScheduledCorrection(userID, goalID))
}

func (e *GoalContinuationEngine) dispatchContinuation(userID, goalID string, kind goalCorrectionKind) {
	// Re-check goal is still active (may have been cancelled during cooldown)
	g := e.store.Get(userID)
	if g == nil || g.GoalID != goalID || !g.ShouldContinue() {
		if kind == goalCorrectionPlan {
			e.abandonUnsentPlanCorrection(userID, goalID, g)
		}
		log.Printf("[goal-continuation] skipping fire: goal no longer continuable user=%s", userID)
		return
	}

	prompt := e.buildContinuationPrompt(g)
	display := buildContinuationDisplayText(g)
	if kind == goalCorrectionPlan && g.EffectivePhase() == goal.PhasePlan && len(g.Steps) == 0 {
		prompt = buildGoalPlanCorrectionPrompt(g)
		display = buildGoalPlanCorrectionDisplay(g)
	}
	e.noteContinuationExpect(userID, g)
	log.Printf("[goal-continuation] firing continuation: user=%s goal_id=%s turn=%d", userID, goalID, g.TurnsUsed)

	// Dispatch via HandleIMMessage with platform="goal-continuation".
	// We cannot use continueAIAssistantWorkflowMessage because it sets
	// Platform="desktop", which would bypass our platform-based accounting
	// and trigger OnUserMessage (cancelling our own continuation).
	if e.app == nil {
		e.failGoal(userID, goalID, "目标失败：助手通道未就绪", false)
		return
	}
	hubClient := e.app.ensureHubClient()
	if hubClient == nil {
		log.Printf("[goal-continuation] fire failed: hubClient nil user=%s", userID)
		e.failGoal(userID, goalID, "目标失败：助手通道未就绪", true)
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		log.Printf("[goal-continuation] fire failed: handler nil user=%s", userID)
		e.failGoal(userID, goalID, "目标失败：助手通道未就绪", true)
		return
	}

	idPrefix := goalID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	requestID := fmt.Sprintf("goal-cont-%s-%d", idPrefix, time.Now().UnixMilli())

	e.emitForegroundRoundStarted(requestID, userID, prompt, display)

	go e.runForegroundContinuation(handler, IMUserMessage{
		RequestID: requestID,
		UserID:    userID,
		Platform:  "goal-continuation",
		Text:      prompt,
	})
}

func (e *GoalContinuationEngine) runForegroundContinuation(handler *IMMessageHandler, msg IMUserMessage) {
	if e == nil || handler == nil {
		return
	}
	finalResponseAttempted := false
	responseEmitted := false
	emitFinalResponse := func(resp *IMAgentResponse) {
		if finalResponseAttempted || e == nil || e.app == nil {
			return
		}
		finalResponseAttempted = true
		if resp == nil {
			resp = &IMAgentResponse{}
		}
		resp.SessionKey = msg.UserID
		responseEmitted = e.app.emitAIAssistantResponse(msg.RequestID, resp)
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[goal-continuation] foreground request %s panicked: %v", msg.RequestID, r)
			emitFinalResponse(&IMAgentResponse{Error: fmt.Sprintf("Goal continuation failed: %v", r)})
			return
		}
		if !finalResponseAttempted {
			log.Printf("[goal-continuation] foreground request %s exited without final response", msg.RequestID)
			emitFinalResponse(&IMAgentResponse{Error: "Goal continuation ended without a final response."})
		} else if !responseEmitted {
			log.Printf("[goal-continuation] foreground request %s final response could not be emitted", msg.RequestID)
		}
	}()
	emitEvent := func(name, value string) {
		e.emitAssistantStreamEvent(name, msg.RequestID, msg.UserID, value)
	}
	onProgress := func(progressText string) {
		if progressText == imHeartbeatMsg {
			emitEvent("ai-assistant-progress", progressText)
			return
		}
		if !isVisibleAIAssistantProgressText(progressText) {
			return
		}
		emitEvent("ai-assistant-progress", progressText)
	}
	streamDeltaNormalizer := &aiAssistantStreamDeltaNormalizer{}
	onToken := func(delta string) {
		delta = streamDeltaNormalizer.Normalize(delta)
		if delta == "" {
			return
		}
		emitEvent("ai-assistant-token", delta)
	}
	onNewRound := func() {
		streamDeltaNormalizer.Reset()
		emitEvent("ai-assistant-new-round", "")
	}
	onStreamDone := func() {
		emitEvent("ai-assistant-stream-done", "")
	}

	// The coding workbench refuses an implementation step that has no desktop
	// ingress token. A continuation is a host request for the same owner, so
	// it mints that token here and drops it when this request returns.
	if endIngress := e.attachCodingIngress(&msg); endIngress != nil {
		defer endIngress()
	}
	resp := handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, onNewRound, onStreamDone)
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	emitFinalResponse(resp)
}

// attachCodingIngress mints the same desktop coding credential a user send
// gets at the Wails boundary. startNewTask is false: this phase continues
// the owner's current relation and must not fence it. The returned function
// closes that request's ingress, including any step token chained from it.
func (e *GoalContinuationEngine) attachCodingIngress(msg *IMUserMessage) func() {
	if e == nil || e.app == nil || msg == nil {
		return nil
	}
	projectPath := projectPathFromSessionOwnerID(msg.UserID)
	if !e.app.isDesktopCodingWorkbenchSession(msg.UserID, projectPath) {
		return nil
	}
	workspaceDir := ""
	if projectPath != "" && e.app.projectPathIsLocalCodingWorkbench(projectPath) {
		workspaceDir = e.app.codingWorkbenchLocalExecDirOrDesktop(projectPath)
	}
	token := e.app.beginDesktopCodingTaskIngressForRequestWithWorkspace(msg.UserID, workspaceDir, false)
	if token == "" {
		return nil
	}
	msg.CodingTaskIngressToken = token
	app := e.app
	ownerID := msg.UserID
	return func() { app.endDesktopCodingTaskIngressForOwner(ownerID) }
}

func (e *GoalContinuationEngine) emitForegroundRoundStarted(requestID, userID, prompt, displayText string) {
	e.emitAssistantStreamEventWithDisplayText("ai-assistant-foreground-round-started", requestID, userID, prompt, displayText)
}

func (e *GoalContinuationEngine) emitAssistantStreamEvent(name, requestID, userID, text string) {
	e.emitAssistantStreamEventWithDisplayText(name, requestID, userID, text, "")
}

func (e *GoalContinuationEngine) emitAssistantStreamEventWithDisplayText(name, requestID, userID, text, displayText string) {
	if e == nil || e.app == nil || e.app.ctx == nil {
		return
	}
	payload, err := json.Marshal(AIAssistantStreamEvent{RequestID: requestID, Text: text, SessionKey: userID, DisplayText: displayText})
	if err != nil {
		log.Printf("[goal-continuation] marshal %s event failed: %v", name, err)
		return
	}
	e.app.emitEvent(name, string(payload))
}

func buildContinuationDisplayText(g *goal.Goal) string {
	if g == nil {
		return "目标清单\n正在继续当前步骤"
	}
	var b strings.Builder
	b.WriteString("目标清单")
	if g.EffectivePhase() == goal.PhasePlan || len(g.Steps) == 0 {
		objective := strings.TrimSpace(g.Objective)
		if objective == "" {
			objective = "当前目标"
		}
		b.WriteString("\n正在分解：" + objective)
	} else {
		current, hasCurrent := g.CurrentStep()
		for _, step := range g.Steps {
			mark := "○"
			switch step.Status {
			case goal.StepDone:
				mark = "✓"
			case goal.StepFailed:
				mark = "×"
			case goal.StepProgress:
				mark = "→"
			default:
				if hasCurrent && step.Index == current.Index {
					mark = "→"
				}
			}
			b.WriteString(fmt.Sprintf("\n%s %d. %s", mark, step.Index, step.Title))
		}
		if !hasCurrent {
			b.WriteString("\n正在核对验收")
		}
	}
	if g.MaxTurns > 0 {
		b.WriteString(fmt.Sprintf("\n进度：第 %d/%d 轮", g.TurnsUsed+1, g.MaxTurns))
	}
	if g.TokenBudget > 0 {
		b.WriteString(fmt.Sprintf(" | Token: %d/%d", g.TokensUsed, g.TokenBudget))
	}
	return b.String()
}

// buildContinuationPrompt constructs the message that drives the next iteration.
func (e *GoalContinuationEngine) buildContinuationPrompt(g *goal.Goal) string {
	if g == nil {
		return "[系统续接][plan] 当前没有可推进的目标。"
	}
	if g.EffectivePhase() == goal.PhasePlan {
		return buildGoalPlanPrompt(g)
	}
	return buildGoalExecutePrompt(g)
}

func buildGoalPlanCorrectionPrompt(g *goal.Goal) string {
	objective := ""
	if g != nil {
		objective = g.Objective
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[系统续接][plan] 上一轮没有把分解写入目标。本轮只补交规划，不要实现，不要问用户。\n\n目标：%s\n\n", objective))
	b.WriteString("立刻调用 goal(action=\"plan\", understanding=\"...\", steps=[{\"title\":\"...\",\"detail\":\"...\"}], acceptance_criteria=[\"...\"]).\n")
	b.WriteString("步骤和验收标准都必须非空。不要修改文件，不要运行实现，不要调用 complete。")
	return b.String()
}

func buildGoalPlanCorrectionDisplay(g *goal.Goal) string {
	if g == nil {
		return "目标清单\n正在补交分解"
	}
	var b strings.Builder
	b.WriteString("目标清单\n正在补交分解")
	if objective := strings.TrimSpace(g.Objective); objective != "" {
		b.WriteString("：" + objective)
	}
	if g.MaxTurns > 0 {
		b.WriteString(fmt.Sprintf("\n进度：第 %d/%d 轮", g.TurnsUsed+1, g.MaxTurns))
	}
	if g.TokenBudget > 0 {
		b.WriteString(fmt.Sprintf(" | Token: %d/%d", g.TokensUsed, g.TokenBudget))
	}
	return b.String()
}

func buildGoalPlanPrompt(g *goal.Goal) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[系统续接][plan] 先理解并分解目标，不要实现。\n\n目标：%s\n\n", g.Objective))
	b.WriteString("本轮只做规划：\n")
	b.WriteString("1. 用你自己的话复述任务，确认你理解要交付的结果。\n")
	b.WriteString("2. 把任务分解成有序、可独立执行的步骤。\n")
	b.WriteString("3. 写出可以核对的验收标准。\n")
	b.WriteString("4. 调用 goal(action=\"plan\", understanding=\"...\", steps=[{\"title\":\"...\",\"detail\":\"...\"}], acceptance_criteria=[\"...\"]).\n\n")
	b.WriteString("禁止修改文件、运行实现或调用 complete。步骤或验收标准为空时不要调用 plan。")
	return b.String()
}

func buildGoalExecutePrompt(g *goal.Goal) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[系统续接][execute] 按步骤执行目标：%s\n\n", g.Objective))
	if strings.TrimSpace(g.Understanding) != "" {
		b.WriteString("理解：" + g.Understanding + "\n")
	}
	b.WriteString(fmt.Sprintf("进度：第 %d/%d 轮", g.TurnsUsed, g.MaxTurns))
	if g.TokenBudget > 0 {
		pct := float64(g.TokensUsed) / float64(g.TokenBudget) * 100
		b.WriteString(fmt.Sprintf(" | Token: %d/%d (%.0f%%)", g.TokensUsed, g.TokenBudget, pct))
	}
	b.WriteString(fmt.Sprintf(" | 耗时: %ds\n", g.TimeUsedSeconds))

	var done []goal.Step
	for _, step := range g.Steps {
		if step.Status == goal.StepDone {
			done = append(done, step)
		}
	}
	if len(done) > 0 {
		b.WriteString("\n已完成（这些步骤已结束，本轮不要重做，也不要把它们没写完的内容并进当前步骤）:\n")
		for _, step := range done {
			b.WriteString(fmt.Sprintf("  %d. %s\n", step.Index, step.Title))
		}
	}
	if len(g.AcceptanceCriteria) > 0 {
		b.WriteString("\n验收标准:\n")
		for _, criterion := range g.AcceptanceCriteria {
			b.WriteString(fmt.Sprintf("  • %s\n", criterion))
		}
	}
	if step, ok := g.CurrentStep(); ok {
		b.WriteString(fmt.Sprintf("\n当前步骤（本轮只做这一步，不要开始后续步骤）：\n  %d. %s\n", step.Index, step.Title))
		if strings.TrimSpace(step.Detail) != "" {
			b.WriteString("  " + step.Detail + "\n")
		}
		if step.Status == goal.StepProgress && strings.TrimSpace(step.Result) != "" {
			b.WriteString("  已记录的进度：" + strings.TrimSpace(step.Result) + "\n")
		}
		b.WriteString(fmt.Sprintf("\n这一步的交付物还没完成时，调用 goal(action=\"step\", index=%d, status=\"progress\", result=\"这一步已经做成什么样，还剩什么\"）。下一轮仍是这一步。", step.Index))
		b.WriteString(fmt.Sprintf("\n交付物完成后再调用 goal(action=\"step\", index=%d, status=\"done\", result=\"...\")，然后停止。不要把未完成的步骤标成 done，也不要在本轮里做步骤 %d 之后的工作。", step.Index, step.Index))
		b.WriteString("\n如果这一步无法完成，status 用 failed，并把原因写进 result。")
		return b.String()
	}
	b.WriteString("\n所有步骤已完成。请核对验收标准后调用 goal(action=\"complete\", summary=\"...\")。")
	b.WriteString("总结必须逐条包含上面的验收标准原文。没有总结不能结束。")
	b.WriteString("\n如果确定无法收尾，调用 goal(action=\"fail\", reason=\"...\")。")
	return b.String()
}

func continuationExpectOf(g *goal.Goal) goalContinuationExpect {
	if g == nil {
		return goalContinuationExpect{}
	}
	exp := goalContinuationExpect{goalID: g.GoalID, phase: g.EffectivePhase()}
	if step, ok := g.CurrentStep(); ok {
		exp.stepIndex = step.Index
		exp.stepStatus = step.Status
		exp.stepResult = step.Result
	}
	return exp
}

func (e *GoalContinuationEngine) noteContinuationExpect(userID string, g *goal.Goal) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pendingExpect == nil {
		e.pendingExpect = make(map[string]goalContinuationExpect)
	}
	if e.expectGen == nil {
		e.expectGen = map[string]uint64{}
	}
	e.expectGen[userID]++
	exp := continuationExpectOf(g)
	exp.generation = e.expectGen[userID]
	e.pendingExpect[userID] = exp
}

func (e *GoalContinuationEngine) peekContinuationExpect(userID string) goalContinuationExpect {
	if e == nil {
		return goalContinuationExpect{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pendingExpect == nil {
		return goalContinuationExpect{}
	}
	return e.pendingExpect[userID]
}

func (e *GoalContinuationEngine) takeContinuationExpect(userID string) goalContinuationExpect {
	if e == nil {
		return goalContinuationExpect{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	exp := e.pendingExpect[userID]
	delete(e.pendingExpect, userID)
	return exp
}

// takeContinuationExpectIfCurrent retires the note recorded for this turn.
// Equality includes generation, so a later note of the same step stays.
func (e *GoalContinuationEngine) takeContinuationExpectIfCurrent(userID string, owned goalContinuationExpect) goalContinuationExpect {
	if e == nil || owned.goalID == "" {
		return goalContinuationExpect{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	exp := e.pendingExpect[userID]
	if exp != owned {
		return goalContinuationExpect{}
	}
	delete(e.pendingExpect, userID)
	return exp
}

// consumePlanMiss reports whether this goal already spent its one corrective
// plan turn. The first miss records the attempt and returns false.
func (e *GoalContinuationEngine) consumePlanMiss(userID, goalID string) bool {
	if e == nil || goalID == "" {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.planMiss == nil {
		e.planMiss = map[string]goalPlanMiss{}
	}
	state := e.planMiss[userID]
	if state.goalID == goalID && state.retried {
		delete(e.planMiss, userID)
		return true
	}
	e.planMiss[userID] = goalPlanMiss{goalID: goalID, retried: true}
	return false
}

// refundUnsentPlanCorrection drops a correction that has not fired and
// returns its one-shot. Caller holds e.mu.
func (e *GoalContinuationEngine) refundUnsentPlanCorrection(userID string) {
	prev := e.planCorrection[userID]
	delete(e.planCorrection, userID)
	if miss, ok := e.planMiss[userID]; ok && prev != "" && miss.goalID == prev {
		delete(e.planMiss, userID)
	}
}

func (e *GoalContinuationEngine) clearPlanMiss(userID, goalID string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	state, ok := e.planMiss[userID]
	if !ok {
		return
	}
	if goalID != "" && state.goalID != goalID {
		return
	}
	delete(e.planMiss, userID)
}

// abandonUnsentPlanCorrection runs when a scheduled /goal 提交分解 did not
// start. A paused, cleared, or replaced goal keeps its status and can still
// receive that one correction later. An active goal that still has no plan
// cannot run the turn, so leaving it active would burn the retry and stall.
func (e *GoalContinuationEngine) abandonUnsentPlanCorrection(userID, goalID string, g *goal.Goal) {
	if g != nil && g.GoalID == goalID && g.Status == goal.StatusActive &&
		g.EffectivePhase() == goal.PhasePlan && len(g.Steps) == 0 {
		log.Printf("[goal-continuation] corrective turn cannot run; failing unplanned goal user=%s goal_id=%s", userID, goalID)
		e.failGoal(userID, goalID, "未能分解任务", true)
		return
	}
	e.clearPlanMiss(userID, goalID)
}

func (e *GoalContinuationEngine) takeScheduledCorrection(userID, goalID string) goalCorrectionKind {
	if e == nil {
		return goalCorrectionNone
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.takeScheduledCorrectionLocked(userID, goalID)
}

// takeScheduledCorrectionLocked removes the pending correction flag.
// Caller holds e.mu.
func (e *GoalContinuationEngine) takeScheduledCorrectionLocked(userID, goalID string) goalCorrectionKind {
	if id, ok := e.planCorrection[userID]; ok {
		delete(e.planCorrection, userID)
		if id == goalID {
			return goalCorrectionPlan
		}
		return goalCorrectionNone
	}
	return goalCorrectionNone
}

func (e *GoalContinuationEngine) clearContinuationExpect(userID string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	delete(e.pendingExpect, userID)
	e.mu.Unlock()
}

// openGoalContinuationContract reports the phase this turn was fired to
// record, when that record is still missing. An empty expect, a different
// goal, or a terminal goal is not an open contract.
func (h *IMMessageHandler) openGoalContinuationContract(userID, userText, platform string) (goalContinuationExpect, bool) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil || !goalContinuationTurn(userText, platform) {
		return goalContinuationExpect{}, false
	}
	engine := h.app.goalContinuation
	if engine.store == nil {
		return goalContinuationExpect{}, false
	}
	g := engine.store.Get(userID)
	exp := engine.peekContinuationExpect(userID)
	if exp.goalID == "" || g == nil || g.GoalID != exp.goalID || g.IsTerminal() {
		return goalContinuationExpect{}, false
	}
	if goalContinuationContractMet(exp, g) {
		return exp, false
	}
	return exp, true
}

// goalContinuationContractHoldPrompt restates the phase contract inside the
// same turn. The model keeps doing that phase and writes it before stopping.
func goalContinuationContractHoldPrompt(exp goalContinuationExpect) string {
	marker := "[系统续接][execute]"
	if exp.phase == goal.PhasePlan {
		marker = "[系统续接][plan]"
	}
	section := strings.TrimSpace(goalContinuationPhaseSection(marker))
	if section == "" {
		return ""
	}
	return "[system] 本轮还没结束。这一阶段还没有写入目标。继续做完这一阶段，写入后再停止。\n" + section
}

// goalContinuationContractMet is true when the turn already stored the record
// this phase required. An empty expect is not a met contract.
func goalContinuationContractMet(exp goalContinuationExpect, g *goal.Goal) bool {
	if exp.goalID == "" || g == nil || g.GoalID != exp.goalID || g.IsTerminal() {
		return false
	}
	switch exp.phase {
	case goal.PhasePlan:
		return len(g.Steps) > 0
	case goal.PhaseExecute:
		return continuationMissedReport(exp, g) == ""
	default:
		return false
	}
}

// continuationMissedReport is set when an execute continuation ended without
// recording the step that was current when the turn started. A later step
// that is still pending is the next turn, not a missed report.
func continuationMissedReport(exp goalContinuationExpect, g *goal.Goal) string {
	if exp.goalID == "" || g == nil || g.GoalID != exp.goalID || g.IsTerminal() || exp.phase != goal.PhaseExecute {
		return ""
	}
	if exp.stepIndex > 0 {
		for _, step := range g.Steps {
			if step.Index != exp.stepIndex {
				continue
			}
			if step.Status == exp.stepStatus && step.Result == exp.stepResult {
				return "未提交当前步骤"
			}
			return ""
		}
		return "未提交当前步骤"
	}
	if g.ReadyToComplete() && g.Status == goal.StatusActive {
		return "未提交完成"
	}
	return ""
}

// failGoal marks the active goal failed and optionally tells the user.
func (e *GoalContinuationEngine) failGoal(userID, goalID, reason string, announce bool) {
	if e == nil || e.store == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "目标失败"
	}
	g := e.store.Get(userID)
	if g == nil {
		return
	}
	if goalID == "" {
		goalID = g.GoalID
	}
	if g.GoalID != goalID || g.IsTerminal() {
		return
	}
	e.store.UpdateStatus(userID, goalID, goal.StatusFailed, reason)
	e.CancelPending(userID)
	e.clearContinuationExpect(userID)
	updated := e.store.Get(userID)
	e.emitGoalStateChanged(userID, updated)
	if announce {
		e.emitGoalNotice(userID, reason, true)
	}
}

func (e *GoalContinuationEngine) announceGoalState(userID string, g *goal.Goal) {
	text, asError := goalTerminalNotice(g)
	if text == "" {
		return
	}
	e.emitGoalNotice(userID, text, asError)
}

func goalTerminalNotice(g *goal.Goal) (text string, asError bool) {
	if g == nil {
		return "", false
	}
	switch g.Status {
	case goal.StatusBudgetLimit:
		reason := strings.TrimSpace(g.Summary)
		if reason == "" {
			reason = "预算已耗尽"
		}
		return "目标已停止：" + reason, true
	case goal.StatusPaused:
		if strings.Contains(g.Summary, "no tool") || strings.Contains(g.Summary, "Auto-paused") {
			return "目标已暂停：连续两轮没有工具调用。发送 /goal resume 继续。", false
		}
		return "", false
	case goal.StatusFailed:
		reason := strings.TrimSpace(g.Summary)
		if reason == "" {
			reason = "目标失败"
		}
		return reason, true
	default:
		return "", false
	}
}

func newGoalNoticeEvent(userID, text string, asError bool) AIContinuationEvent {
	return AIContinuationEvent{
		RequestID:  fmt.Sprintf("goal-notice-%d", time.Now().UnixMilli()),
		SessionKey: userID,
		Kind:       "notice",
		Text:       text,
		AsError:    asError,
	}
}

// emitGoalNotice tells the user a host outcome. It must not open a foreground
// round: that event is rendered as a user message, which made "未能分解任务"
// look like something the user sent.
func (e *GoalContinuationEngine) emitGoalNotice(userID, text string, asError bool) {
	if e == nil || e.app == nil || strings.TrimSpace(text) == "" {
		return
	}
	e.app.emitAIContinuationEvent(newGoalNoticeEvent(userID, text, asError))
}

// emitGoalStateChanged notifies the frontend of goal state changes.
func (e *GoalContinuationEngine) emitGoalStateChanged(userID string, g *goal.Goal) {
	if e == nil || e.app == nil || e.app.ctx == nil || g == nil {
		return
	}
	awaiting := false
	if e.app.imHandler != nil {
		awaiting = e.app.imHandler.freshPendingAskUser(userID)
	}
	e.app.emitEvent("goal-state-changed", map[string]interface{}{
		"user_id":       userID,
		"goal_id":       g.GoalID,
		"objective":     g.Objective,
		"status":        string(g.Status),
		"phase":         string(g.EffectivePhase()),
		"understanding": g.Understanding,
		"steps":         g.Steps,
		"turns_used":    g.TurnsUsed,
		"max_turns":     g.MaxTurns,
		"tokens_used":   g.TokensUsed,
		"token_budget":  g.TokenBudget,
		"summary":       g.Summary,
		"awaiting_user": awaiting,
		"visible":       g.Status != goal.StatusComplete,
		"created_at":    goalCreatedUnix(g),
	})
}

func goalCreatedUnix(g *goal.Goal) int64 {
	if g == nil || g.CreatedAt.IsZero() {
		return 0
	}
	return g.CreatedAt.Unix()
}

// emitGoalCleared tells the chat bar this session no longer has a goal.
// The transcript checklist is left as it was: an empty record does not
// replace that card.
func (e *GoalContinuationEngine) emitGoalCleared(userID string) {
	if e == nil || e.app == nil {
		return
	}
	e.app.emitEvent("goal-state-changed", map[string]interface{}{
		"user_id":       userID,
		"goal_id":       "",
		"objective":     "",
		"status":        "cleared",
		"phase":         "",
		"steps":         nil,
		"turns_used":    0,
		"max_turns":     0,
		"summary":       "",
		"awaiting_user": false,
		"visible":       false,
	})
}

func turnWaitsForAskUser(resp *IMAgentResponse) bool {
	return resp != nil && resp.ResponseSource == imResponseSourceAskUser.String()
}

func goalContinuationTurnCost(resp *IMAgentResponse) (tokens, seconds int) {
	seconds = 30
	if resp != nil {
		tokens = resp.InputTokens + resp.OutputTokens
	}
	if tokens <= 0 {
		return tokens, seconds
	}
	seconds = tokens / 33 // ~1 token per 30ms
	if seconds < 5 {
		seconds = 5
	}
	if seconds > 300 {
		seconds = 300
	}
	return tokens, seconds
}

// maybeScheduleGoalContinuation is called from the post-loop side effects
// goroutine. It checks if the user has an active goal and schedules the
// next continuation turn via the continuation engine.
//
// Two modes:
//   - goal-continuation platform: accounts usage + schedules next turn
//   - other platforms (user message): only schedules next turn if goal is active
//     (no accounting — user's own agent loop is not a "goal turn")
func (h *IMMessageHandler) maybeScheduleGoalContinuation(userID string, resp *IMAgentResponse, platform string) {
	h.maybeScheduleGoalContinuationOwned(userID, resp, platform, goalContinuationExpect{}, false)
}

func (h *IMMessageHandler) maybeScheduleGoalContinuationOwned(userID string, resp *IMAgentResponse, platform string, ownedExpect goalContinuationExpect, haveOwnedExpect bool) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	engine := h.app.goalContinuation
	g := engine.store.Get(userID)
	if g == nil || g.IsTerminal() || g.Status != goal.StatusActive {
		return
	}
	// A user stop pauses the goal. The bar can continue it. The dying turn
	// must not fail the goal, and must not cancel a continue that already
	// armed the next timer. The fence itself stays for the next ordinary message.
	if h.closeGoalTurnForUserStop(userID, resp) {
		return
	}
	// The session task is still running. A second host turn would take the
	// same session. A session-round failure is not a failed goal step.
	if resp != nil && resp.sessionRoundContinue {
		return
	}
	if strings.TrimSpace(platform) == sessionRoundContinuationPlatform && resp != nil && strings.TrimSpace(resp.Error) != "" {
		return
	}
	if resp != nil && resp.runWindowPaused {
		engine.CancelPending(userID)
		log.Printf("[goal-continuation] skipping: run window paused user=%s", userID)
		return
	}

	// A failed turn closes the goal only when this phase's contract is still
	// open. A plan or step already stored is the host record; a later audit
	// error must not erase it. A continuation with no such record still fails
	// with the turn error. ownedExpect is the note captured when this turn
	// ended. The map may already hold the next step, and that open note is
	// not a reason to fail this turn.
	contractExpect := engine.peekContinuationExpect(userID)
	if haveOwnedExpect {
		contractExpect = ownedExpect
	}
	if resp != nil && strings.TrimSpace(resp.Error) != "" && !goalContinuationContractMet(contractExpect, g) {
		log.Printf("[goal-continuation] failing goal after turn error user=%s", userID)
		engine.failGoal(userID, g.GoalID, resp.Error, false)
		return
	}

	// Keep pure-coding sticky routing armed across multi-turn /goal loops.
	h.ensurePureCodingArmedForGoalContinuation(userID)
	// Dead SSH must not leave the goal looking active. The bar stays up with
	// the reconnect reason, and continue retries this same goal later.
	if text, blocked := h.pauseGoalForUnarmedRemote(userID); blocked {
		log.Printf("[goal-continuation] paused: remote SSH is not connected user=%s reason=%s", userID, text)
		return
	}

	// ask_user owns the next message. A continuation must not run in its place.
	// The person's answer is an ordinary turn, and that turn schedules again.
	// Post-loop work runs before this check, so the answer may already have
	// cleared the pending map. The response snapshot is this turn's own pause
	// and must still not queue a second continuation beside that answer.
	// ownedExpect was captured when this turn ended. A successor may have
	// noted a new phase while this goroutine was still in side effects.
	if turnWaitsForAskUser(resp) || h.freshPendingAskUser(userID) {
		log.Printf("[goal-continuation] waiting for ask_user before the next turn user=%s goal_id=%s", userID, g.GoalID)
		if platform == "goal-continuation" {
			tokens, seconds := goalContinuationTurnCost(resp)
			if engine.store != nil {
				engine.store.AccountUsage(userID, g.GoalID, tokens, seconds)
				engine.store.ResetNoToolCounter(userID, g.GoalID)
			}
			if haveOwnedExpect {
				engine.takeContinuationExpectIfCurrent(userID, ownedExpect)
			} else {
				engine.takeContinuationExpect(userID)
			}
		}
		if updated := engine.store.Get(userID); updated != nil {
			engine.emitGoalStateChanged(userID, updated)
		}
		return
	}

	if platform == "goal-continuation" {
		// Goal-continuation turn: account usage and schedule next
		turnTokens, turnSeconds := goalContinuationTurnCost(resp)
		hadToolCalls := resp != nil && resp.TraceEventCount > 0
		// Defense-in-depth: pure coding reports often carry activity only via
		// TraceEventCount after finalize; if still zero but response has body,
		// do not auto-pause an otherwise healthy coding workbench loop.
		if !hadToolCalls && resp != nil && strings.TrimSpace(resp.Text) != "" && h.isPureCodingWorkbenchSession(userID) {
			hadToolCalls = true
		}

		engine.finishContinuationTurn(userID, g.GoalID, turnTokens, turnSeconds, hadToolCalls, ownedExpect, haveOwnedExpect)
	} else {
		// User-initiated turn with active goal: don't account usage (this wasn't
		// a goal turn), but schedule continuation since goal is still active and
		// the user's turn is now complete.
		if g.ShouldContinue() {
			engine.scheduleDelayed(userID, g.GoalID)
		}
	}
}
