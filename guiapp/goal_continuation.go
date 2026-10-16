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
本轮只执行用户消息里的当前步骤。
做完调用 goal(action="step", index, status, result)。
步骤都完成时调用 goal(action="complete", summary)；无法收尾时调用 goal(action="fail", reason)。
不要提前做后续步骤。

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
	cooldown        time.Duration // delay between continuations (default 2s)
}

// goalContinuationExpect is the host phase at the moment a continuation is
// fired. The turn is finished only when the goal store records that phase.
type goalContinuationExpect struct {
	goalID     string
	phase      goal.Phase
	stepIndex  int
	stepStatus goal.StepStatus
	stepResult string
}

// NewGoalContinuationEngine creates a continuation engine.
func NewGoalContinuationEngine(store *goal.Store, app *App) *GoalContinuationEngine {
	return &GoalContinuationEngine{
		store:           store,
		app:             app,
		scheduledTimers: make(map[string]*time.Timer),
		pendingExpect:   make(map[string]goalContinuationExpect),
		cooldown:        2 * time.Second,
	}
}

// MaybeScheduleContinuation checks if the goal should auto-continue after the
// current agent loop finishes. Called from schedulePostLoopSideEffects.
// It accounts usage from the just-completed turn, then schedules the next
// continuation if the goal is still continuable.
func (e *GoalContinuationEngine) MaybeScheduleContinuation(userID, goalID string, turnTokens, turnSeconds int, hadToolCalls bool) {
	if e == nil || e.store == nil {
		return
	}

	g := e.store.Get(userID)
	if g == nil || g.GoalID != goalID {
		return // stale or no goal
	}

	// Account this turn's usage
	e.store.AccountUsage(userID, goalID, turnTokens, turnSeconds)

	// Track tool call presence for no-tool suppression
	if hadToolCalls {
		e.store.ResetNoToolCounter(userID, goalID)
	} else {
		e.store.RecordNoToolTurn(userID, goalID)
	}

	// Re-read after accounting (status may have changed to budget_limited)
	g = e.store.Get(userID)
	expect := e.takeContinuationExpect(userID)
	if g != nil && g.Status == goal.StatusActive && g.EffectivePhase() == goal.PhasePlan && len(g.Steps) == 0 {
		e.failGoal(userID, g.GoalID, "未能分解任务", true)
		return
	}
	if reason := continuationMissedReport(expect, g); reason != "" {
		e.failGoal(userID, g.GoalID, reason, true)
		return
	}
	if g == nil || !g.ShouldContinue() {
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

	// Schedule continuation with cooldown delay
	e.scheduleDelayed(userID, goalID)
}

// CancelPending cancels any pending continuation for a user (e.g. on user cancel).
func (e *GoalContinuationEngine) CancelPending(userID string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if timer, ok := e.scheduledTimers[userID]; ok {
		timer.Stop()
		delete(e.scheduledTimers, userID)
		log.Printf("[goal-continuation] cancelled pending continuation: user=%s", userID)
	}
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
	e.CancelPending(userID)

	// User message resets no-tool counter (user interaction = external trigger)
	g := e.store.Get(userID)
	if g != nil && g.Status == goal.StatusActive {
		e.store.ResetNoToolCounter(userID, g.GoalID)
	}
}

// scheduleDelayed schedules a continuation message after the cooldown period.
func (e *GoalContinuationEngine) scheduleDelayed(userID, goalID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Cancel any existing timer for this user
	if existing, ok := e.scheduledTimers[userID]; ok {
		existing.Stop()
	}

	timer := time.AfterFunc(e.cooldown, func() {
		e.mu.Lock()
		delete(e.scheduledTimers, userID)
		e.mu.Unlock()

		e.fireContinuation(userID, goalID)
	})
	e.scheduledTimers[userID] = timer
	log.Printf("[goal-continuation] scheduled next turn in %s: user=%s goal_id=%s", e.cooldown, userID, goalID)
}

// fireContinuation dispatches the self-message that triggers the next agent loop turn.
func (e *GoalContinuationEngine) fireContinuation(userID, goalID string) {
	// Re-check goal is still active (may have been cancelled during cooldown)
	g := e.store.Get(userID)
	if g == nil || g.GoalID != goalID || !g.ShouldContinue() {
		log.Printf("[goal-continuation] skipping fire: goal no longer continuable user=%s", userID)
		return
	}

	prompt := e.buildContinuationPrompt(g)
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

	e.emitForegroundRoundStarted(requestID, userID, prompt, e.buildContinuationDisplayText(g))

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

func (e *GoalContinuationEngine) buildContinuationDisplayText(g *goal.Goal) string {
	if g == nil {
		return "/goal 继续推进目标"
	}
	var b strings.Builder
	if g.EffectivePhase() == goal.PhasePlan {
		b.WriteString(fmt.Sprintf("/goal 分解目标：%s", g.Objective))
	} else if step, ok := g.CurrentStep(); ok {
		b.WriteString(fmt.Sprintf("/goal 执行步骤 %d：%s", step.Index, step.Title))
	} else {
		b.WriteString(fmt.Sprintf("/goal 核对并完成：%s", g.Objective))
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
		b.WriteString("\n已完成:\n")
		for _, step := range done {
			b.WriteString(fmt.Sprintf("  %d. %s — %s\n", step.Index, step.Title, step.Result))
		}
	}
	if len(g.AcceptanceCriteria) > 0 {
		b.WriteString("\n验收标准:\n")
		for _, criterion := range g.AcceptanceCriteria {
			b.WriteString(fmt.Sprintf("  • %s\n", criterion))
		}
	}
	if step, ok := g.CurrentStep(); ok {
		b.WriteString(fmt.Sprintf("\n当前步骤（只做这一步）：\n  %d. %s\n", step.Index, step.Title))
		if strings.TrimSpace(step.Detail) != "" {
			b.WriteString("  " + step.Detail + "\n")
		}
		b.WriteString(fmt.Sprintf("\n做完后调用 goal(action=\"step\", index=%d, status=\"done\" 或 \"failed\", result=\"...\")。不要做后续步骤。", step.Index))
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
	e.pendingExpect[userID] = continuationExpectOf(g)
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

func (e *GoalContinuationEngine) clearContinuationExpect(userID string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	delete(e.pendingExpect, userID)
	e.mu.Unlock()
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

func (e *GoalContinuationEngine) emitGoalNotice(userID, text string, asError bool) {
	if e == nil || e.app == nil || strings.TrimSpace(text) == "" {
		return
	}
	requestID := fmt.Sprintf("goal-notice-%d", time.Now().UnixMilli())
	resp := &IMAgentResponse{SessionKey: userID}
	if asError {
		resp.Error = text
	} else {
		resp.Text = text
	}
	e.emitForegroundRoundStarted(requestID, userID, text, text)
	e.app.emitAIAssistantResponse(requestID, resp)
}

// emitGoalStateChanged notifies the frontend of goal state changes.
func (e *GoalContinuationEngine) emitGoalStateChanged(userID string, g *goal.Goal) {
	if e == nil || e.app == nil || e.app.ctx == nil || g == nil {
		return
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
	})
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
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	engine := h.app.goalContinuation
	g := engine.store.Get(userID)
	if g == nil || g.IsTerminal() || g.Status != goal.StatusActive {
		return
	}
	if h.hasCancelledTaskBoundary(userID) {
		engine.CancelPending(userID)
		log.Printf("[goal-continuation] skipping: task cancelled by user user=%s", userID)
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
	// with the turn error.
	if resp != nil && strings.TrimSpace(resp.Error) != "" && !goalContinuationContractMet(engine.peekContinuationExpect(userID), g) {
		log.Printf("[goal-continuation] failing goal after turn error user=%s", userID)
		engine.failGoal(userID, g.GoalID, resp.Error, false)
		return
	}

	// Keep pure-coding sticky routing armed across multi-turn /goal loops.
	h.ensurePureCodingArmedForGoalContinuation(userID)
	// Remote pure coding with dead SSH: do not schedule into the general agent.
	// A leftover local project path is not an SSH session. Scheduling that
	// turn re-enters the desktop pipeline for a task whose machine is the
	// remote host.
	if h.isPureCodingWorkbenchSession(userID) && !h.remoteCodingSSHEnginePending(userID) {
		mem := h.getStickyCodingWorkbenchMemory(userID)
		if h.codingSessionIsRemote(userID, mem) {
			log.Printf("[goal-continuation] skip schedule: remote pure coding not armed (SSH reconnect required) user=%s", userID)
			return
		}
	}

	if platform == "goal-continuation" {
		// Goal-continuation turn: account usage and schedule next
		turnTokens := 0
		if resp != nil {
			turnTokens = resp.InputTokens + resp.OutputTokens
		}
		hadToolCalls := resp != nil && resp.TraceEventCount > 0
		// Defense-in-depth: pure coding reports often carry activity only via
		// TraceEventCount after finalize; if still zero but response has body,
		// do not auto-pause an otherwise healthy coding workbench loop.
		if !hadToolCalls && resp != nil && strings.TrimSpace(resp.Text) != "" && h.isPureCodingWorkbenchSession(userID) {
			hadToolCalls = true
		}

		// Estimate turn time from token count as proxy
		turnSeconds := 30
		if turnTokens > 0 {
			turnSeconds = turnTokens / 33 // ~1 token per 30ms
			if turnSeconds < 5 {
				turnSeconds = 5
			}
			if turnSeconds > 300 {
				turnSeconds = 300
			}
		}
		engine.MaybeScheduleContinuation(userID, g.GoalID, turnTokens, turnSeconds, hadToolCalls)
	} else {
		// User-initiated turn with active goal: don't account usage (this wasn't
		// a goal turn), but schedule continuation since goal is still active and
		// the user's turn is now complete.
		if g.ShouldContinue() {
			engine.scheduleDelayed(userID, g.GoalID)
		}
	}
}
