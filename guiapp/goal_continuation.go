package guiapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
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

// goalTurnErrorIsRetryable reports a turn error the agent loop already treats
// as a transport failure. A task failure such as a compile error still closes
// the goal. The string is not a typed error, so this uses the same classifier
// the loop uses once the text is all that remains. A copied "unexpected EOF"
// stays non-retryable, matching IsUnexpectedStreamEOF.
func goalTurnErrorIsRetryable(errText string) bool {
	text := strings.TrimSpace(errText)
	if text == "" {
		return false
	}
	return agentruntime.IsRetryableLLMError(errors.New(text))
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
todo_write 只是清单，不能代替本轮记录。index 用用户消息里的步骤号，不要用清单编号。
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
	// queuedTurn is the goal id closed over by the cooldown timer. A side
	// command reads it before cancelPendingLocked deletes the timer.
	queuedTurn map[string]queuedGoalTurn
	// scheduleGen invalidates a timer that was already replaced or cancelled.
	// time.AfterFunc can still run after Stop, and that callback must not
	// dispatch or delete the successor entry.
	scheduleGen map[string]uint64
	// stopSuperseded is the cancel fence an explicit continue already re-armed.
	// The dying turn must not pause or fail that goal, and a host continuation
	// of the same fence may enter. A later stop stores a new fence.
	stopSuperseded map[string]cancelledTaskFence
	// barHold marks a turn the goal bar stopped. The dying post-loop consumes
	// it and must not fail or re-arm the goal, even if continue already ran.
	barHold map[string]struct{}
	// continueRearmed is set before a continue flips the goal back to active.
	// The dying turn reads it under the same lock. A queued timer is not the
	// signal: it is armed after that flip and disappears once it fires.
	// The next chat stop or goal-bar pause clears it, so that stop still pauses.
	continueRearmed map[string]struct{}
	// continuationAdmit is the generation that may still enter. A bar pause
	// or delete bumps it. A continuation that already passed the cooldown
	// check carries the old generation and must not start. Continue does not
	// bump it; the next fire mints a newer one.
	continuationAdmit map[string]uint64
	// continuationTicket is one dispatched request. The admit map is what
	// makes that request stale. Keyed by request ID.
	continuationTicket map[string]continuationAdmitTicket
	// expectGen identifies one note of pendingExpect. It only increases.
	// Two notes of an unchanged step must stay distinct, or a late post-loop
	// would retire the successor's note.
	expectGen map[string]uint64
	// restartCandidates is the goal id loaded from disk for each session.
	// Only those rows are resumed. A goal created later in this process
	// already has its own timer, and a bar poll must not arm a second turn.
	restartCandidates map[string]string
	// transientTurnMiss counts consecutive transport failures for one session.
	// A completed turn clears it. The count does not stop the goal.
	transientTurnMiss map[string]int
	cooldown          time.Duration // delay between continuations (default 2s)
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
	e := &GoalContinuationEngine{
		store:             store,
		app:               app,
		scheduledTimers:   make(map[string]*time.Timer),
		pendingExpect:     make(map[string]goalContinuationExpect),
		planMiss:          make(map[string]goalPlanMiss),
		planCorrection:    make(map[string]string),
		scheduleGen:       make(map[string]uint64),
		expectGen:         make(map[string]uint64),
		restartCandidates: map[string]string{},
		cooldown:          2 * time.Second,
	}
	for _, g := range store.Snapshot() {
		if g == nil || !g.ShouldContinue() {
			continue
		}
		userID := strings.TrimSpace(g.UserID)
		goalID := strings.TrimSpace(g.GoalID)
		if userID == "" || goalID == "" {
			continue
		}
		e.restartCandidates[userID] = goalID
	}
	return e
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

// continuationGoalKey is the continuation-store row this chat addresses.
// The composer asks with desktop-user:F:/bird2 while Windows stores
// desktop-user:F:\bird2. The bar and ReleaseForContinue already use that
// row. Sticky coding, ask-user, cancel fences, and continuation notes stay
// on the chat id. A miss keeps the chat id so a session with no row still
// addresses its own timer.
func (h *IMMessageHandler) continuationGoalKey(chatUserID string) (string, *goal.Goal) {
	chatUserID = strings.TrimSpace(chatUserID)
	if h == nil || h.app == nil || h.app.goalContinuation == nil || h.app.goalContinuation.store == nil {
		return chatUserID, nil
	}
	engine := h.app.goalContinuation
	hit := h.app.goalBarHit(chatUserID)
	if hit.store == engine.store && hit.goal != nil && strings.TrimSpace(hit.id) != "" {
		return hit.id, hit.goal
	}
	return chatUserID, engine.store.Get(chatUserID)
}

// pauseGoalForUnarmedRemote records that this goal cannot run on the desktop.
// The session's machine is a remote host and its SSH engine is down. The goal
// stays paused with the reconnect text so the bar can offer continue.
// The session check uses the chat id. The row may be the slash-flipped twin.
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
	goalKey, g := h.continuationGoalKey(userID)
	if g == nil || g.Status == goal.StatusComplete {
		return text, true
	}
	engine.store.UpdateStatus(goalKey, g.GoalID, goal.StatusPaused, text)
	// Drop a timer armed under either spelling. The bar reads this row.
	h.app.cancelGoalBarTimers(goalKey)
	if updated := engine.store.Get(goalKey); updated != nil {
		engine.emitGoalStateChanged(goalKey, updated)
	}
	return text, true
}

// goalUserStopSummary is the bar reason after the user stops the current turn.
// It must not look like a dead SSH session or a no-tool stall.
const goalUserStopSummary = "用户已停止当前回合。"

// stoppedGoalUserTurn is what this user turn is already doing. The goal bar
// follows that, not the wording. Chinese, English, and Mongolian therefore
// take the same path.
type stoppedGoalUserTurn struct {
	// DocPhase is a workflow document confirmation. That phase is not the
	// goal's work, even when the button label is a continue word.
	DocPhase bool
	// FreshTask means the task boundary already classified this message as a
	// new task. The stopped goal stays stopped.
	FreshTask bool
}

// userTurnExecutesSessionGoal reports that this turn is about to run the work
// the session goal belongs to. A coding workbench and a workflow
// implementation phase are that work. A shared chat reply is not: the agent
// loop also answers ordinary messages, and those must not wake a paused goal.
// A document confirmation is not that work either.
func (h *IMMessageHandler) userTurnExecutesSessionGoal(userID string, workflowAgentLoop, workflowDocPhase bool) bool {
	if h == nil || workflowDocPhase {
		return false
	}
	if workflowAgentLoop {
		return true
	}
	return h.isPureCodingWorkbenchSession(userID)
}

// releaseStoppedGoalForUserContinue puts a paused, failed, or budget-stopped
// goal back to active when this user turn is about to run that session's work.
// Callers are the behavior: the coding workbench or a workflow implementation
// has already been chosen. No user phrase list is consulted, so an unlisted
// wording still re-arms. Reopening the app does not resume those rows, and
// the coding turn still runs, so the bar would keep the old stop while the
// agent is already working. This message is the turn: do not schedule a
// second one beside it. The post-loop arm runs after this turn, once the
// goal is active again. A completed goal stays finished.
//
// A new task leaves the stopped goal alone. A workflow document confirmation
// is not this goal's work. Host platforms, and the session-round and
// goal-continuation markers, are not the person resuming.
//
// A dead remote SSH session is left on its current status. This path must
// not pause the row: the caller is about to run, and pauseGoalForUnarmedRemote
// would rewrite a failure into an SSH pause while that work still starts.
// The unarmed check reads the pending SSH engine, so callers have to release
// before they consume those flags. The ingress that refuses the turn
// (finishUnarmedRemoteCodingTurn, ContinueSessionGoal) still pauses.
func (h *IMMessageHandler) releaseStoppedGoalForUserContinue(msg IMUserMessage, turn stoppedGoalUserTurn) {
	if turn.DocPhase || turn.FreshTask || h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	switch strings.TrimSpace(msg.Platform) {
	case "goal-continuation", unattendedWorkflowPlatform, sessionRoundContinuationPlatform:
		return
	}
	if isSessionRoundContinuation(msg.Text) || isGoalSystemContinuation(msg.Text) {
		return
	}
	hit := h.app.goalBarHit(msg.UserID)
	if hit.store == nil || hit.goal == nil || strings.TrimSpace(hit.id) == "" {
		return
	}
	switch hit.goal.Status {
	case goal.StatusPaused, goal.StatusFailed, goal.StatusBudgetLimit:
	default:
		return
	}
	// Pending flags and the cancel fence live on the chat owner. The goal row
	// may be the slash-flipped twin.
	owner := strings.TrimSpace(msg.UserID)
	if owner == "" {
		owner = hit.id
	}
	h.ensurePureCodingArmedForGoalContinuation(owner)
	if _, blocked := h.unarmedRemoteCodingDesktopTurn(owner); blocked {
		return
	}
	// Record the continue before the status flip. A dying stop from the
	// previous process lifetime must not pause this turn again.
	h.noteGoalContinuePastUserStop(owner)
	goalID := hit.goal.GoalID
	from := hit.goal.Status
	if !hit.store.ReleaseForContinue(hit.id, goalID) {
		h.app.goalContinuation.clearContinueRearmed(owner)
		return
	}
	h.app.eachSameGoal(hit, func(other *goal.Store, id string, row *goal.Goal) {
		if other == hit.store && id == hit.id {
			return
		}
		if row.Status == goal.StatusComplete {
			return
		}
		other.ReleaseForContinue(id, row.GoalID)
	})
	updated := hit.store.Get(hit.id)
	if updated != nil {
		h.app.goalContinuation.emitGoalStateChanged(hit.id, updated)
	}
	log.Printf("[goal-continuation] user continue re-armed stopped goal user=%s goal_id=%s from=%s", hit.id, goalID, from)
}

// noteGoalContinuePastUserStop records that the bar or /goal resume is
// re-arming this same goal. The stop fence stays, so the next ordinary
// message still starts a new task. Only a host continuation of this fence
// is allowed to enter. The composer and the stored row spell one session
// with different slashes. The dying turn looks the mark up under its own id,
// so every spelling has to carry it or that turn pauses the goal again.
func (h *IMMessageHandler) noteGoalContinuePastUserStop(userID string) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	ids := goalBarCandidateIDs(userID)
	if len(ids) == 0 {
		return
	}
	fences := make(map[string]cancelledTaskFence, len(ids))
	for _, id := range ids {
		fence, ok := h.currentCancelledTaskFence(id)
		if !ok {
			continue
		}
		fences[id] = fence
	}
	// Record the continue before ReleaseForContinue makes the goal active.
	// The dying turn and this mark share the engine lock.
	h.app.goalContinuation.noteContinuePastSessionStop(ids, fences)
}

// noteContinueRearmed records that continue is re-arming this session.
// Caller must invoke it before the goal status becomes active.
func (e *GoalContinuationEngine) noteContinueRearmed(userID string) {
	if e == nil {
		return
	}
	e.noteContinuePastSessionStop([]string{userID}, nil)
}

// noteContinuePastSessionStop records one continue on every spelling of the
// session. fences is the cancel fence already stored under that spelling.
// Caller must invoke it before the goal status becomes active.
func (e *GoalContinuationEngine) noteContinuePastSessionStop(ids []string, fences map[string]cancelledTaskFence) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.continueRearmed == nil {
		e.continueRearmed = map[string]struct{}{}
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		e.continueRearmed[id] = struct{}{}
		fence, ok := fences[id]
		if !ok {
			continue
		}
		if e.stopSuperseded == nil {
			e.stopSuperseded = map[string]cancelledTaskFence{}
		}
		e.stopSuperseded[id] = fence
	}
}

// continueRearmedLocked reports a continue that has not been superseded by
// a later user stop. Caller holds e.mu.
func (e *GoalContinuationEngine) continueRearmedLocked(userID string) bool {
	if e == nil || e.continueRearmed == nil {
		return false
	}
	_, ok := e.continueRearmed[userID]
	return ok
}

// clearContinueRearmed drops the mark so the next explicit stop can pause.
// Every slash spelling is one session. Clearing only the row that was paused
// leaves the composer's mark, and the next stop then leaves the goal active.
func (e *GoalContinuationEngine) clearContinueRearmed(userID string) {
	if e == nil {
		return
	}
	ids := goalBarCandidateIDs(userID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(ids) == 0 {
		delete(e.continueRearmed, strings.TrimSpace(userID))
		return
	}
	for _, id := range ids {
		delete(e.continueRearmed, id)
	}
}

// noteGoalBarHold records that this session's live turn was stopped from the
// goal bar. The matching post-loop consumes it.
func (e *GoalContinuationEngine) noteGoalBarHold(userID string) {
	if e == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.barHold == nil {
		e.barHold = map[string]struct{}{}
	}
	e.barHold[userID] = struct{}{}
}

// consumeGoalBarHold reports and clears a bar stop for this session.
func (e *GoalContinuationEngine) consumeGoalBarHold(userID string) bool {
	if e == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.barHold == nil {
		return false
	}
	if _, ok := e.barHold[userID]; !ok {
		return false
	}
	delete(e.barHold, userID)
	return true
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
	// Resolve the row before taking the engine lock. The fence check below
	// stays on the chat id.
	goalKey := h.queuedGoalTimerKey(userID)
	engine.mu.Lock()
	if engine.stopSupersededLocked(userID, fence) {
		engine.mu.Unlock()
		if resp != nil && resp.userStopped {
			log.Printf("[goal-continuation] skipping user-stop pause: continue already re-armed user=%s", userID)
			return true
		}
		return false
	}
	paused := engine.pauseForUserStopLocked(goalKey)
	if goalKey != userID {
		engine.cancelPendingLocked(userID)
	}
	var updated *goal.Goal
	if paused && engine.store != nil {
		updated = engine.store.Get(goalKey)
	}
	engine.mu.Unlock()
	if updated != nil {
		engine.emitGoalStateChanged(goalKey, updated)
	}
	if paused {
		log.Printf("[goal-continuation] paused: user stopped the turn user=%s", userID)
	}
	return true
}

// finishCancelledGoalTurn closes a turn that returns before post-loop.
// Shutdown, timeout, and turn replacement cancel the loop without a user
// stop, and those leave the goal alone. A fenced user stop pauses and stamps
// the clock. A goal-bar hold means that pause already happened. A continue
// recorded before the goal became active again is left armed.
func (h *IMMessageHandler) finishCancelledGoalTurn(userID string, resp *IMAgentResponse, loopCtx *LoopContext) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	_, fenced := h.currentCancelledTaskFence(userID)
	userStopped := loopCtx != nil && loopCtx.UserCancelled()
	if !fenced && !userStopped {
		return
	}
	stopped := IMAgentResponse{}
	if resp != nil {
		stopped = *resp
	}
	stopped.userStopped = true
	if fenced {
		h.closeGoalTurnForUserStop(userID, &stopped)
		if h.app != nil && h.app.goalContinuation != nil {
			h.app.goalContinuation.consumeGoalBarHold(userID)
		}
		return
	}
	if h.app != nil && h.app.goalContinuation != nil && h.app.goalContinuation.consumeGoalBarHold(userID) {
		return
	}
	h.pauseStillActiveUserStop(userID)
}

// pauseStillActiveUserStop stamps the clock when the user stopped this loop
// and the goal is still active. A continue that already re-armed is left
// alone, including after its timer has fired. An already paused or finished
// goal keeps the stamp it has. A normal queued turn is not a continue: this
// stop cancels that timer.
func (h *IMMessageHandler) pauseStillActiveUserStop(userID string) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	engine := h.app.goalContinuation
	goalKey, _ := h.continuationGoalKey(userID)
	engine.mu.Lock()
	if engine.continueRearmedLocked(userID) {
		engine.mu.Unlock()
		return
	}
	if engine.store == nil {
		engine.mu.Unlock()
		return
	}
	g := engine.store.Get(goalKey)
	if g == nil || g.Status != goal.StatusActive {
		engine.mu.Unlock()
		return
	}
	paused := engine.pauseForUserStopLocked(goalKey)
	if goalKey != userID {
		engine.cancelPendingLocked(userID)
	}
	var updated *goal.Goal
	if paused {
		updated = engine.store.Get(goalKey)
	}
	engine.mu.Unlock()
	if updated != nil {
		engine.emitGoalStateChanged(goalKey, updated)
	}
	if paused {
		log.Printf("[goal-continuation] paused: user stopped the turn user=%s", userID)
	}
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
	delete(e.queuedTurn, userID)
	delete(e.planCorrection, userID)
	delete(e.planMiss, userID)
	e.bumpScheduleGen(userID)
}

// queuedGoalTurn is the continuation waiting out the cooldown.
type queuedGoalTurn struct {
	goalID         string
	planCorrection bool
}

// suspendedGoalTurn is that continuation parked for one side command.
// gen is the schedule generation after the park, so a later cancel or a
// newer timer is not overwritten when the command returns.
type suspendedGoalTurn struct {
	goalID         string
	planCorrection bool
	planMiss       goalPlanMiss
	hadPlanMiss    bool
	gen            uint64
}

// suspendQueuedTurn stops the cooldown timer and remembers which goal it
// belonged to. CancelPending deletes that record, so the caller keeps the
// returned copy. No queued timer returns false and changes nothing.
func (e *GoalContinuationEngine) suspendQueuedTurn(userID string) (suspendedGoalTurn, bool) {
	if e == nil {
		return suspendedGoalTurn{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	turn, ok := e.queuedTurn[userID]
	if !ok || turn.goalID == "" {
		return suspendedGoalTurn{}, false
	}
	if _, pending := e.scheduledTimers[userID]; !pending {
		delete(e.queuedTurn, userID)
		return suspendedGoalTurn{}, false
	}
	suspended := suspendedGoalTurn{
		goalID:         turn.goalID,
		planCorrection: e.planCorrection[userID] == turn.goalID,
	}
	if miss, exists := e.planMiss[userID]; exists && miss.goalID == turn.goalID {
		suspended.planMiss = miss
		suspended.hadPlanMiss = true
	}
	e.cancelPendingLocked(userID)
	suspended.gen = e.scheduleGen[userID]
	return suspended, true
}

// restoreSuspendedGoalTurn re-arms the parked turn. A pause, cancel, reset,
// or a timer scheduled while the command ran moves the generation or leaves
// the goal unable to continue, and this leaves that newer state alone.
func (e *GoalContinuationEngine) restoreSuspendedGoalTurn(userID string, turn suspendedGoalTurn) {
	if e == nil || turn.goalID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.scheduleGen[userID] != turn.gen {
		return
	}
	if _, pending := e.scheduledTimers[userID]; pending {
		return
	}
	if e.store == nil {
		return
	}
	g := e.store.Get(userID)
	if g == nil || g.GoalID != turn.goalID || !g.ShouldContinue() {
		return
	}
	if turn.planCorrection && turn.hadPlanMiss {
		if e.planMiss == nil {
			e.planMiss = map[string]goalPlanMiss{}
		}
		e.planMiss[userID] = turn.planMiss
	}
	kind := goalCorrectionNone
	if turn.planCorrection {
		kind = goalCorrectionPlan
	}
	e.scheduleAfterCooldownLocked(userID, turn.goalID, kind)
}

// blocksRunWindowCompletion keeps a scheduled row in progress while this
// session still has another goal turn queued or eligible to run.
func (e *GoalContinuationEngine) blocksRunWindowCompletion(userID string) bool {
	if e == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	ids := goalBarCandidateIDs(userID)
	if len(ids) == 0 {
		ids = []string{userID}
	}
	e.mu.Lock()
	pending := false
	for _, id := range ids {
		if _, ok := e.scheduledTimers[id]; ok {
			pending = true
			break
		}
	}
	e.mu.Unlock()
	if pending {
		return true
	}
	if e.store == nil {
		return false
	}
	if e.app != nil {
		hit := e.app.goalBarHit(userID)
		if hit.store == e.store && hit.goal != nil {
			return hit.goal.ShouldContinue()
		}
	}
	g := e.store.Get(userID)
	return g != nil && g.ShouldContinue()
}

// OnUserMessage is called when a user message is about to take the session.
// It cancels any pending continuation (user takes priority) and resets the
// no-tool counter. An immediate reply does not come here, so a rejected
// /goal, /help, or a short greeting leaves the queued turn in place.
// Pause and cancel stop the timer themselves.
func (e *GoalContinuationEngine) OnUserMessage(userID string) {
	if e == nil {
		return
	}
	// CancelPending refunds an unsent plan correction. The next empty plan
	// can ask once, because this message interrupted the previous attempt.
	e.CancelPending(userID)
	e.resetActiveGoalNoToolCounter(userID)
}

// resetActiveGoalNoToolCounter records that the user touched an active goal
// without preempting its next turn.
func (e *GoalContinuationEngine) resetActiveGoalNoToolCounter(userID string) {
	if e == nil || e.store == nil {
		return
	}
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

// continuationHandler is the assistant handler that can re-arm a coding
// session and refuse a dead remote host. A nil handler means this process
// cannot start a turn yet.
func (e *GoalContinuationEngine) continuationHandler() *IMMessageHandler {
	if e == nil || e.app == nil {
		return nil
	}
	if client := e.app.hubClient(); client != nil {
		if handler := client.ensureIMHandler(); handler != nil {
			return handler
		}
	}
	return e.app.imHandler
}

// resumeVisibleGoalAfterRestart arms the goal on the session the bar is
// showing. Startup itself does not arm: the coding tab binds its directory
// as the window opens, and a timer started before that runs in the wrong
// place. The bar read is the moment that session is on screen. Pause and
// continue work without this because continue uses the same live-turn wait.
// A missing handler leaves the session unclaimed so a later read can resume.
func (a *App) resumeVisibleGoalAfterRestart(sessionKey string) {
	if a == nil || a.goalContinuation == nil || a.goalContinuation.continuationHandler() == nil {
		return
	}
	hit := a.goalBarHit(sessionKey)
	if hit.goal == nil || strings.TrimSpace(hit.id) == "" || !hit.goal.ShouldContinue() {
		return
	}
	if !a.goalContinuation.takeRestartCandidate(hit.id, hit.goal.GoalID) {
		return
	}
	a.goalContinuation.resumePersistedGoal(hit.id, hit.goal.GoalID)
}

// takeRestartCandidate accepts the disk row captured when this process
// started. A later poll, or a goal created after startup, gets nothing.
func (e *GoalContinuationEngine) takeRestartCandidate(userID, goalID string) bool {
	if e == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	goalID = strings.TrimSpace(goalID)
	if userID == "" || goalID == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.restartCandidates[userID] != goalID {
		return false
	}
	delete(e.restartCandidates, userID)
	return true
}

// resumePersistedGoal schedules the next turn for one goal that is still
// allowed to run. Paused, finished, and exhausted goals stay as they are.
// A remote coding session whose SSH engine is down is paused, matching the
// continue button, so the bar does not keep saying the agent is working.
func (e *GoalContinuationEngine) resumePersistedGoal(userID, goalID string) {
	if e == nil || e.store == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	goalID = strings.TrimSpace(goalID)
	if userID == "" || goalID == "" || e.continuationAlreadyQueued(userID) {
		return
	}
	g := e.store.Get(userID)
	if g == nil || g.GoalID != goalID || !g.ShouldContinue() {
		return
	}
	if handler := e.continuationHandler(); handler != nil {
		// Do not arm the coding route here. The bar read is on the UI call.
		// The continuation turn arms from the project index, the same way
		// opening the task does. A dead remote is paused now so the bar
		// stops saying the agent is working and no model turn is spent.
		if _, blocked := handler.pauseGoalForUnarmedRemote(userID); blocked {
			log.Printf("[goal-continuation] restart left goal paused; remote coding session is down user=%s goal_id=%s", userID, goalID)
			return
		}
		// Same rule as /goal resume. The bar can paint while a coding turn
		// still holds the session, and that wait is capped at 60s.
		// The live turn schedules this goal when it ends.
		log.Printf("[goal-continuation] resuming goal after restart user=%s goal_id=%s", userID, goalID)
		handler.scheduleGoalContinuationUnlessLive(userID, goalID)
		return
	}
	log.Printf("[goal-continuation] resuming goal after restart user=%s goal_id=%s", userID, goalID)
	e.scheduleDelayed(userID, goalID)
}

// continuationAlreadyQueued reports that this session already has a timer.
// Restart resume must not replace a turn the user just armed.
func (e *GoalContinuationEngine) continuationAlreadyQueued(userID string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, queued := e.scheduledTimers[userID]
	return queued
}

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

// goalStillContinuable reports whether this goal may arm another turn.
// A nil store keeps the old arming behavior for callers that have no row.
// Caller may hold e.mu. Store lock is taken and released inside Get.
func (e *GoalContinuationEngine) goalStillContinuable(userID, goalID string) bool {
	if e == nil || e.store == nil {
		return true
	}
	g := e.store.Get(userID)
	if g == nil || strings.TrimSpace(goalID) == "" || g.GoalID != goalID {
		return false
	}
	return g.ShouldContinue()
}

// scheduleAfterCooldownLocked arms the next timer. Caller holds e.mu.
// A pause, delete, or failed goal that lands before this lock refuses the arm,
// so a dying turn cannot put the timer back.
func (e *GoalContinuationEngine) scheduleAfterCooldownLocked(userID, goalID string, kind goalCorrectionKind) {
	if !e.goalStillContinuable(userID, goalID) {
		e.cancelPendingLocked(userID)
		log.Printf("[goal-continuation] not scheduling; goal is not continuable user=%s goal_id=%s", userID, goalID)
		return
	}
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
	if e.queuedTurn == nil {
		e.queuedTurn = map[string]queuedGoalTurn{}
	}
	e.queuedTurn[userID] = queuedGoalTurn{
		goalID:         goalID,
		planCorrection: kind == goalCorrectionPlan,
	}
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
	delete(e.queuedTurn, userID)
	return e.takeScheduledCorrectionLocked(userID, goalID), true
}

func (e *GoalContinuationEngine) runScheduledContinuation(userID, goalID string, gen uint64) {
	corrective, ok := e.claimScheduledContinuation(userID, goalID, gen)
	if !ok {
		return
	}
	e.dispatchContinuation(userID, goalID, corrective)
}

// continuationAdmitTicket is the generation a dispatched continuation must
// still match when it enters. Pause bumps the session generation, so this
// ticket no longer matches and the turn does not start.
type continuationAdmitTicket struct {
	userID string
	gen    uint64
}

// continuationAdmitDecision is why a fire may or may not enter.
type continuationAdmitDecision int

const (
	continuationAdmitYes continuationAdmitDecision = iota
	continuationAdmitIdle
	continuationAdmitStopped
)

// admitContinuationIfContinuable records one continuation that may enter.
// The continuable check and the mint share the engine lock, so a pause that
// bumps the generation cannot land between them. A false result means this
// fire must not start a turn.
func (e *GoalContinuationEngine) admitContinuationIfContinuable(userID, goalID, requestID string) bool {
	return e.decideContinuationAdmit(userID, goalID, requestID) == continuationAdmitYes
}

// decideContinuationAdmit distinguishes a goal that can no longer run from a
// user stop that still leaves the goal active. The stop waits for one
// spelling's turn to exit. Failing an unplanned goal in that window turns a
// stop into "未能分解任务".
func (e *GoalContinuationEngine) decideContinuationAdmit(userID, goalID, requestID string) continuationAdmitDecision {
	if e == nil {
		return continuationAdmitIdle
	}
	userID = strings.TrimSpace(userID)
	requestID = strings.TrimSpace(requestID)
	if userID == "" || requestID == "" {
		return continuationAdmitIdle
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.goalStillContinuable(userID, goalID) {
		return continuationAdmitIdle
	}
	// The stop button fences one spelling and then waits for that turn to
	// exit. The continuation is dispatched on the stored spelling, which has
	// its own lock, so the fence has to block the mint here. Continue records
	// the fence it already passed; a later stop has a new fence and blocks.
	if e.sessionStopBlocksContinuationLocked(userID) {
		return continuationAdmitStopped
	}
	if e.continuationAdmit == nil {
		e.continuationAdmit = map[string]uint64{}
	}
	if e.continuationTicket == nil {
		e.continuationTicket = map[string]continuationAdmitTicket{}
	}
	e.continuationAdmit[userID]++
	e.continuationTicket[requestID] = continuationAdmitTicket{
		userID: userID,
		gen:    e.continuationAdmit[userID],
	}
	return continuationAdmitYes
}

// sessionStopBlocksContinuation reports that a user stop still owns this
// session. The fence may sit on the other slash spelling. A continue that
// already recorded that fence is not blocked.
func (e *GoalContinuationEngine) sessionStopBlocksContinuation(userID string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessionStopBlocksContinuationLocked(userID)
}

// sessionStopBlocksContinuationLocked is sessionStopBlocksContinuation.
// Caller holds e.mu.
func (e *GoalContinuationEngine) sessionStopBlocksContinuationLocked(userID string) bool {
	if e == nil || e.app == nil || e.app.imHandler == nil {
		return false
	}
	h := e.app.imHandler
	for _, id := range goalBarCandidateIDs(userID) {
		fence, ok := h.currentCancelledTaskFence(id)
		if !ok {
			continue
		}
		if !e.stopSupersededLocked(id, fence) {
			return true
		}
	}
	return false
}

// continuationTurnBlocked reports that this dispatched continuation must not
// enter. The admit ticket and a user-stop fence on either slash spelling
// are the same stop. An unknown request with no fence is left alone.
func (e *GoalContinuationEngine) continuationTurnBlocked(msg IMUserMessage) bool {
	if e == nil || msg.Platform != "goal-continuation" {
		return false
	}
	if e.continuationAdmitRevoked(msg.RequestID) {
		return true
	}
	return e.sessionStopBlocksContinuation(msg.UserID)
}

// continuationAdmitLive reports a fire that has already been admitted and
// has not entered or finished. Pause and stop both have to see it: the
// timer is already gone, and the loop is not published yet.
func (e *GoalContinuationEngine) continuationAdmitLive(userID string) bool {
	if e == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.continuationAdmit[userID]
	if current == 0 || e.continuationTicket == nil {
		return false
	}
	for _, ticket := range e.continuationTicket {
		if ticket.userID == userID && ticket.gen == current {
			return true
		}
	}
	return false
}

// revokeContinuationAdmit invalidates every continuation already admitted for
// this session. The next fire mints a newer generation.
func (e *GoalContinuationEngine) revokeContinuationAdmit(userID string) {
	if e == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.continuationAdmit == nil {
		e.continuationAdmit = map[string]uint64{}
	}
	e.continuationAdmit[userID]++
}

// continuationAdmitRevoked reports that this dispatched request must not
// enter. An unknown request is not a goal continuation and is left alone.
func (e *GoalContinuationEngine) continuationAdmitRevoked(requestID string) bool {
	if e == nil {
		return false
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	ticket, ok := e.continuationTicket[requestID]
	if !ok || ticket.gen == 0 {
		return false
	}
	return e.continuationAdmit[ticket.userID] != ticket.gen
}

func (e *GoalContinuationEngine) finishContinuationAdmit(requestID string) {
	if e == nil {
		return
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	e.mu.Lock()
	delete(e.continuationTicket, requestID)
	e.mu.Unlock()
}

// goalContinuationRequestSeq keeps admit tickets unique. The map fail-opens
// for an unknown request, so two fires that shared a millisecond would drop
// each other's ticket and the paused one could still enter.
var goalContinuationRequestSeq atomic.Uint64

// admittedContinuationGoal is the row a fire may still address after admit
// has released the engine lock. Delete and pause both land in that gap.
// A removed or stopped row retires the ticket. Continue arms a new timer
// and a new admit; this fire must not stay around to run beside it.
func (e *GoalContinuationEngine) admittedContinuationGoal(userID, goalID, requestID string) *goal.Goal {
	if e == nil {
		return nil
	}
	var g *goal.Goal
	if e.store != nil {
		g = e.store.Get(userID)
	}
	if g == nil || g.GoalID != goalID || !g.ShouldContinue() {
		e.finishContinuationAdmit(requestID)
		return nil
	}
	return g
}

// fireContinuation dispatches the self-message that triggers the next agent loop turn.
func (e *GoalContinuationEngine) fireContinuation(userID, goalID string) {
	e.dispatchContinuation(userID, goalID, e.takeScheduledCorrection(userID, goalID))
}

func (e *GoalContinuationEngine) dispatchContinuation(userID, goalID string, kind goalCorrectionKind) {
	// Re-check goal is still active (may have been cancelled during cooldown).
	// Admitting under the same lock closes the gap where pause lands after
	// this check and before the turn enters the session.
	idPrefix := goalID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	requestID := fmt.Sprintf("goal-cont-%s-%d-%d", idPrefix, time.Now().UnixMilli(), goalContinuationRequestSeq.Add(1))
	switch e.decideContinuationAdmit(userID, goalID, requestID) {
	case continuationAdmitStopped:
		// The goal is still active. The dying turn pauses it. This fire did
		// not run, so a plan correction keeps its one retry.
		if kind == goalCorrectionPlan {
			e.clearPlanMiss(userID, goalID)
		}
		log.Printf("[goal-continuation] skipping fire: user stop owns the session user=%s", userID)
		return
	case continuationAdmitIdle:
		g := e.store.Get(userID)
		if kind == goalCorrectionPlan {
			e.abandonUnsentPlanCorrection(userID, goalID, g)
		}
		log.Printf("[goal-continuation] skipping fire: goal no longer continuable user=%s", userID)
		return
	}

	g := e.admittedContinuationGoal(userID, goalID, requestID)
	if g == nil {
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
	// A fire that never starts its goroutine retires the ticket. Leaving it
	// would make a later check of this request fail open once the entry is gone.
	if e.app == nil {
		e.finishContinuationAdmit(requestID)
		e.failGoal(userID, goalID, "目标失败：助手通道未就绪", false)
		return
	}
	hubClient := e.app.ensureHubClient()
	if hubClient == nil {
		log.Printf("[goal-continuation] fire failed: hubClient nil user=%s", userID)
		e.finishContinuationAdmit(requestID)
		e.failGoal(userID, goalID, "目标失败：助手通道未就绪", true)
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		log.Printf("[goal-continuation] fire failed: handler nil user=%s", userID)
		e.finishContinuationAdmit(requestID)
		e.failGoal(userID, goalID, "目标失败：助手通道未就绪", true)
		return
	}

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
		e.finishContinuationAdmit(msg.RequestID)
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
	// Pause may have landed after dispatch admitted this turn. Do not enter
	// the session. An empty final response closes the round the dispatch
	// already opened and does not fail the goal or fence chat.
	if e.continuationTurnBlocked(msg) {
		log.Printf("[goal-continuation] dropped dispatched turn after the goal stopped user=%s request_id=%s", msg.UserID, msg.RequestID)
		emitFinalResponse(&IMAgentResponse{})
		return
	}
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

	// The turn itself carries the desktop Coding credential: the handler's
	// per-owner turn window mints it and closes the chain, so this host
	// continuation needs no credential code of its own.
	resp := handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, onNewRound, onStreamDone)
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	emitFinalResponse(resp)
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

// capGoalPromptRunes shortens prompt background. The current step stays
// outside this cap so a long understanding cannot push it out of the window.
func capGoalPromptRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if max <= 0 || len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// capGoalPromptTail keeps the latest part of a progress note. The conclusion
// is at the end, and the opening is already known.
func capGoalPromptTail(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if max <= 0 || len(runes) <= max {
		return s
	}
	return "…" + string(runes[len(runes)-max:])
}

func buildGoalExecutePrompt(g *goal.Goal) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[系统续接][execute] 按步骤执行目标：%s\n\n", g.Objective))
	step, hasStep := g.CurrentStep()
	if hasStep {
		b.WriteString(fmt.Sprintf("本轮只能提交步骤 %d：%s。已完成的步骤不要再调用 goal。\n调用 goal(action=\"step\", index=%d, status=\"progress\" 或 \"done\", result=\"...\")。\n\n", step.Index, step.Title, step.Index))
		b.WriteString(fmt.Sprintf("当前步骤（本轮只做这一步，不要开始后续步骤）：\n  %d. %s\n", step.Index, step.Title))
		if detail := capGoalPromptRunes(step.Detail, 480); detail != "" {
			b.WriteString("  " + detail + "\n")
		}
		if step.Status == goal.StepProgress {
			if progress := capGoalPromptTail(step.Result, 240); progress != "" {
				b.WriteString("  已记录的进度：" + progress + "\n")
			}
		}
		b.WriteString(fmt.Sprintf("\n这一步的交付物还没完成时，调用 goal(action=\"step\", index=%d, status=\"progress\", result=\"这一步已经做成什么样，还剩什么\"）。下一轮仍是这一步。", step.Index))
		b.WriteString(fmt.Sprintf("\n交付物完成后再调用 goal(action=\"step\", index=%d, status=\"done\", result=\"...\")，然后停止。不要把未完成的步骤标成 done，也不要在本轮里做步骤 %d 之后的工作。", step.Index, step.Index))
		b.WriteString("\n如果这一步无法完成，status 用 failed，并把原因写进 result。\n")
	}
	if g.MaxTurns > 0 {
		b.WriteString(fmt.Sprintf("进度：第 %d/%d 轮", g.TurnsUsed, g.MaxTurns))
	} else {
		b.WriteString(fmt.Sprintf("进度：第 %d 轮，轮次不限", g.TurnsUsed))
	}
	if g.TokenBudget > 0 {
		pct := float64(g.TokensUsed) / float64(g.TokenBudget) * 100
		b.WriteString(fmt.Sprintf(" | Token: %d/%d (%.0f%%)", g.TokensUsed, g.TokenBudget, pct))
	}
	b.WriteString(fmt.Sprintf(" | 耗时: %ds\n", g.TimeUsedSeconds))

	var done []goal.Step
	for _, item := range g.Steps {
		if item.Status == goal.StepDone {
			done = append(done, item)
		}
	}
	if len(done) > 0 {
		b.WriteString("\n已完成（这些步骤已结束，本轮不要重做，也不要把它们没写完的内容并进当前步骤）:\n")
		for _, item := range done {
			b.WriteString(fmt.Sprintf("  %d. %s\n", item.Index, item.Title))
		}
	}
	// The plan essay describes the whole goal, and its opening is usually the
	// step that is already done. While a step is current, that essay pulls
	// the model back onto the closed step. The complete turn still needs it.
	if !hasStep {
		if understanding := strings.TrimSpace(g.Understanding); understanding != "" {
			b.WriteString("\n理解：" + understanding + "\n")
		}
	}
	if len(g.AcceptanceCriteria) > 0 {
		b.WriteString("\n验收标准:\n")
		limit := len(g.AcceptanceCriteria)
		if hasStep && limit > 6 {
			limit = 6
		}
		for _, criterion := range g.AcceptanceCriteria[:limit] {
			shown := criterion
			if hasStep {
				shown = capGoalPromptRunes(criterion, 80)
			}
			b.WriteString(fmt.Sprintf("  • %s\n", shown))
		}
		if hasStep && len(g.AcceptanceCriteria) > limit {
			b.WriteString(fmt.Sprintf("  • …其余 %d 条留到完成时再逐条引用\n", len(g.AcceptanceCriteria)-limit))
		}
	}
	if hasStep {
		b.WriteString(fmt.Sprintf("\n再次确认：本轮只调用 goal(action=\"step\", index=%d, status=\"progress\" 或 \"done\", result=\"...\")。\n", step.Index))
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

// retryTransientGoalTurn reschedules a transport failure. There is no attempt
// cap: a timeout does not fail the goal. A successor note or a queued timer
// is left alone and is not counted. A late dying turn must not replace that
// timer or close the goal. Returns false only when this engine cannot schedule.
func (e *GoalContinuationEngine) retryTransientGoalTurn(userID, goalID string, owned goalContinuationExpect, haveOwned bool) bool {
	if e == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	goalID = strings.TrimSpace(goalID)
	if userID == "" || goalID == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if haveOwned && e.continuationSupersededLocked(userID, owned) {
		if e.pendingExpect != nil && owned.goalID != "" && e.pendingExpect[userID] == owned {
			delete(e.pendingExpect, userID)
		}
		log.Printf("[goal-continuation] transient error on a superseded turn; successor stays user=%s", userID)
		return true
	}
	if e.transientTurnMiss == nil {
		e.transientTurnMiss = map[string]int{}
	}
	e.transientTurnMiss[userID]++
	if e.pendingExpect != nil && owned.goalID != "" && e.pendingExpect[userID] == owned {
		delete(e.pendingExpect, userID)
	}
	log.Printf("[goal-continuation] retrying after transient turn error user=%s goal_id=%s misses=%d", userID, goalID, e.transientTurnMiss[userID])
	e.scheduleAfterCooldownLocked(userID, goalID, goalCorrectionNone)
	return true
}

// clearTransientGoalRetry forgets the transport-failure streak after a turn
// that was not a retryable transport error.
func (e *GoalContinuationEngine) clearTransientGoalRetry(userID string) {
	if e == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.transientTurnMiss, userID)
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
	// Pause and delete own the row. A dying turn's error must not replace them
	// with a failure. Budget and complete are already terminal.
	if g.GoalID != goalID || g.Status != goal.StatusActive {
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
		return botNotice("goal-stopped", "notice", "The goal stopped. Reason: "+reason, "", "zh-Hans", "目标停了："+reason, 5*time.Second), true
	case goal.StatusPaused:
		if strings.Contains(g.Summary, "no tool") || strings.Contains(g.Summary, "Auto-paused") {
			line := botNotice("goal-paused", "notice", "The goal paused: two rounds in a row made no tool call. They can send /goal resume to continue; include /goal resume literally.", "", "zh-Hans", "目标暂停了。发 /goal resume 继续。", 5*time.Second)
			if !strings.Contains(line, "/goal resume") {
				line += " /goal resume"
			}
			return line, false
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
	if e == nil || e.app == nil || g == nil {
		return
	}
	// A completed goal no longer owns the live step checklist. Clear it before
	// the state event so the panel and the goal bar retire together. If the
	// closing turn writes one last todo, the post-turn terminal emit clears it
	// again.
	if g.Status == goal.StatusComplete && e.app.imHandler != nil {
		e.app.imHandler.clearGoalFinishedCodingChecklist(userID)
	}
	if e.app.ctx == nil {
		return
	}
	awaiting := false
	if e.app.imHandler != nil {
		awaiting = e.app.imHandler.freshPendingAskUser(userID)
	}
	e.app.emitEvent("goal-state-changed", map[string]interface{}{
		"user_id":            userID,
		"goal_id":            g.GoalID,
		"objective":          g.Objective,
		"status":             string(g.Status),
		"phase":              string(g.EffectivePhase()),
		"understanding":      g.Understanding,
		"steps":              g.Steps,
		"turns_used":         g.TurnsUsed,
		"max_turns":          g.MaxTurns,
		"tokens_used":        g.TokensUsed,
		"token_budget":       g.TokenBudget,
		"summary":            g.Summary,
		"awaiting_user":      awaiting,
		"visible":            g.Status != goal.StatusComplete,
		"created_at":         goalCreatedUnix(g),
		"stopped_at":         goalStoppedUnix(g),
		"clock_hold_seconds": goalClockHoldSeconds(g),
	})
}

func goalCreatedUnix(g *goal.Goal) int64 {
	if g == nil || g.CreatedAt.IsZero() {
		return 0
	}
	return g.CreatedAt.Unix()
}

// goalStoppedUnix is the instant the composer clock stops. Zero means the
// clock is still running. Pause, failure, and a filled budget all stop it.
// A stopped goal that predates stopped_at freezes at its last update.
func goalStoppedUnix(g *goal.Goal) int64 {
	if g == nil || !g.ComposerClockStopped() {
		return 0
	}
	if !g.StoppedAt.IsZero() {
		return g.StoppedAt.Unix()
	}
	if !g.UpdatedAt.IsZero() {
		return g.UpdatedAt.Unix()
	}
	return goalCreatedUnix(g)
}

func goalClockHoldSeconds(g *goal.Goal) int64 {
	if g == nil || g.ClockHoldSeconds < 0 {
		return 0
	}
	return g.ClockHoldSeconds
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
	// The chat id owns session maps. The goal row and its timer follow the
	// store key the bar already drives, including the slash-flipped twin.
	goalKey, g := h.continuationGoalKey(userID)
	clearRetry := func() {
		engine.clearTransientGoalRetry(userID)
		if goalKey != userID {
			engine.clearTransientGoalRetry(goalKey)
		}
	}
	// A bar pause or delete stopped this turn. Continue may already have made
	// the goal active again. Leave that timer alone, and do not fail the row.
	if resp != nil && resp.userStopped && engine.consumeGoalBarHold(userID) {
		log.Printf("[goal-continuation] dying turn ignored after goal bar stop user=%s", userID)
		return
	}
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
		h.app.cancelGoalBarTimers(goalKey)
		log.Printf("[goal-continuation] skipping: run window paused user=%s", userID)
		return
	}

	// A failed goal-continuation turn closes the goal only when this phase's
	// contract is still open. A plan or step already stored is the host
	// record; a later audit error must not erase it. A continuation with no
	// such record still fails with the turn error. A desktop or coding turn
	// is not that continuation: its error belongs to that turn, and the goal
	// stays active so the post-loop path below can arm the next one.
	// ownedExpect is the note captured when this turn ended. The map may
	// already hold the next step, and that open note is not a reason to fail
	// this turn.
	contractExpect := engine.peekContinuationExpect(userID)
	if haveOwnedExpect {
		contractExpect = ownedExpect
	}
	turnErr := ""
	if resp != nil {
		turnErr = strings.TrimSpace(resp.Error)
	}
	// An exhausted iteration budget ends this turn. The next continuation
	// stays on the same step. It is not a failed goal and not a transport retry.
	if codingLoopIterationExhausted(turnErr) {
		turnErr = ""
	}
	if strings.TrimSpace(platform) == "goal-continuation" && turnErr != "" && !goalContinuationContractMet(contractExpect, g) {
		if goalTurnErrorIsRetryable(turnErr) && g.ShouldContinue() && engine.retryTransientGoalTurn(goalKey, g.GoalID, ownedExpect, haveOwnedExpect) {
			h.app.cancelOtherGoalBarTimers(goalKey)
			return
		}
		log.Printf("[goal-continuation] failing goal after turn error user=%s", userID)
		clearRetry()
		// The bar status alone does not say why the turn stopped. The notice
		// is the error row the user sees after a failed goal turn.
		engine.failGoal(goalKey, g.GoalID, turnErr, true)
		if goalKey != userID {
			engine.clearContinuationExpect(userID)
		}
		h.app.cancelOtherGoalBarTimers(goalKey)
		return
	}
	clearRetry()

	// Keep pure-coding sticky routing armed across multi-turn /goal loops.
	// Sticky state lives on the chat id, not the store key.
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
				engine.store.AccountUsage(goalKey, g.GoalID, tokens, seconds)
				engine.store.ResetNoToolCounter(goalKey, g.GoalID)
			}
			if haveOwnedExpect {
				engine.takeContinuationExpectIfCurrent(userID, ownedExpect)
			} else {
				engine.takeContinuationExpect(userID)
			}
		}
		if updated := engine.store.Get(goalKey); updated != nil {
			engine.emitGoalStateChanged(goalKey, updated)
		}
		return
	}

	if platform == "goal-continuation" {
		// Goal-continuation turn: account usage and schedule next.
		// The note stays on the chat id. Accounting and the next timer use
		// the store key, matching ContinueSessionGoal.
		turnTokens, turnSeconds := goalContinuationTurnCost(resp)
		hadToolCalls := resp != nil && resp.TraceEventCount > 0
		// Defense-in-depth: pure coding reports often carry activity only via
		// TraceEventCount after finalize; if still zero but response has body,
		// do not auto-pause an otherwise healthy coding workbench loop.
		if !hadToolCalls && resp != nil && strings.TrimSpace(resp.Text) != "" && h.isPureCodingWorkbenchSession(userID) {
			hadToolCalls = true
		}
		owned := ownedExpect
		haveOwned := haveOwnedExpect
		if goalKey != userID {
			if !haveOwned {
				owned = engine.takeContinuationExpect(userID)
				haveOwned = true
			} else {
				engine.takeContinuationExpectIfCurrent(userID, ownedExpect)
			}
		}
		engine.finishContinuationTurn(goalKey, g.GoalID, turnTokens, turnSeconds, hadToolCalls, owned, haveOwned)
		h.app.cancelOtherGoalBarTimers(goalKey)
	} else {
		// User-initiated turn with active goal: don't account usage (this wasn't
		// a goal turn), but schedule continuation since goal is still active and
		// the user's turn is now complete. The other slash spelling must not
		// keep a second timer.
		if g.ShouldContinue() {
			h.app.cancelOtherGoalBarTimers(goalKey)
			engine.scheduleDelayed(goalKey, g.GoalID)
		} else {
			// An active row can still be out of turns or budget. Drop a timer
			// armed under either spelling. scheduleDelayed used to do this for
			// the chat id on the way out.
			h.app.cancelGoalBarTimers(goalKey)
		}
	}
}
