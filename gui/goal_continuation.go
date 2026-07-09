package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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
	cooldown        time.Duration          // delay between continuations (default 2s)
}

// NewGoalContinuationEngine creates a continuation engine.
func NewGoalContinuationEngine(store *goal.Store, app *App) *GoalContinuationEngine {
	return &GoalContinuationEngine{
		store:           store,
		app:             app,
		scheduledTimers: make(map[string]*time.Timer),
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
	if g == nil || !g.ShouldContinue() {
		if g != nil && g.IsTerminal() {
			log.Printf("[goal-continuation] goal reached terminal state: user=%s goal_id=%s status=%s", userID, goalID, g.Status)
			e.emitGoalStateChanged(userID, g)
		} else if g != nil && g.Status == goal.StatusPaused {
			log.Printf("[goal-continuation] goal paused (no-tool suppression): user=%s goal_id=%s turns=%d", userID, goalID, g.TurnsUsed)
			e.emitGoalStateChanged(userID, g)
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
	log.Printf("[goal-continuation] firing continuation: user=%s goal_id=%s turn=%d", userID, goalID, g.TurnsUsed)

	// Dispatch via HandleIMMessage with platform="goal-continuation".
	// We cannot use continueAIAssistantWorkflowMessage because it sets
	// Platform="desktop", which would bypass our platform-based accounting
	// and trigger OnUserMessage (cancelling our own continuation).
	hubClient := e.app.ensureHubClient()
	if hubClient == nil {
		log.Printf("[goal-continuation] fire failed: hubClient nil user=%s", userID)
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		log.Printf("[goal-continuation] fire failed: handler nil user=%s", userID)
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
	runtime.EventsEmit(e.app.ctx, name, string(payload))
}

func (e *GoalContinuationEngine) buildContinuationDisplayText(g *goal.Goal) string {
	if g == nil {
		return "/goal 继续推进目标"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("/goal 继续推进目标：%s", g.Objective))
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
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[系统续接] 继续推进目标：%s\n\n", g.Objective))

	// Progress report
	b.WriteString(fmt.Sprintf("📊 进度：第 %d/%d 轮", g.TurnsUsed, g.MaxTurns))
	if g.TokenBudget > 0 {
		pct := float64(g.TokensUsed) / float64(g.TokenBudget) * 100
		b.WriteString(fmt.Sprintf(" | Token: %d/%d (%.0f%%)", g.TokensUsed, g.TokenBudget, pct))
	}
	b.WriteString(fmt.Sprintf(" | 耗时: %ds\n", g.TimeUsedSeconds))

	// Acceptance criteria reminder
	if len(g.AcceptanceCriteria) > 0 {
		b.WriteString("\n验收标准:\n")
		for _, c := range g.AcceptanceCriteria {
			b.WriteString(fmt.Sprintf("  • %s\n", c))
		}
	}

	b.WriteString("\n请继续工作。如果目标已达成，调用 goal(action=\"complete\", summary=\"...\")。")
	b.WriteString("如果确定无法继续，调用 goal(action=\"fail\", reason=\"...\")。")
	return b.String()
}

// emitGoalStateChanged notifies the frontend of goal state changes.
func (e *GoalContinuationEngine) emitGoalStateChanged(userID string, g *goal.Goal) {
	if e.app == nil || e.app.ctx == nil {
		return
	}
	runtime.EventsEmit(e.app.ctx, "goal-state-changed", map[string]interface{}{
		"user_id":      userID,
		"goal_id":      g.GoalID,
		"objective":    g.Objective,
		"status":       string(g.Status),
		"turns_used":   g.TurnsUsed,
		"max_turns":    g.MaxTurns,
		"tokens_used":  g.TokensUsed,
		"token_budget": g.TokenBudget,
		"summary":      g.Summary,
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

	// Skip continuation if last loop returned error
	if resp != nil && resp.Error != "" {
		log.Printf("[goal-continuation] skipping: last loop returned error user=%s", userID)
		return
	}

	if platform == "goal-continuation" {
		// Goal-continuation turn: account usage and schedule next
		turnTokens := 0
		if resp != nil {
			turnTokens = resp.InputTokens + resp.OutputTokens
		}
		hadToolCalls := resp != nil && resp.TraceEventCount > 0

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
