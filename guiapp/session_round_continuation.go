package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	// sessionRoundContinuationPlatform is a host wave of the same session
	// task. It is not a user message and must not cancel the wave it starts.
	sessionRoundContinuationPlatform = "session-round-continuation"
	sessionRoundContinuationSource   = "session_round_continuation"
	// sessionRoundContinuationMaxWaves matches the goal turn bound. One wave
	// already uses the configured iteration budget. The next wave continues
	// the same task instead of asking the user to send another message.
	sessionRoundContinuationMaxWaves = 50
	sessionRoundManualContinueText   = "(Maximum reasoning rounds reached. Send another message to continue the task.)"
)

// sessionRoundState is the host scheduler for a session-agent task whose
// iteration budget ended before the work did.
type sessionRoundState struct {
	mu       sync.Mutex
	timers   map[string]*time.Timer
	gens     map[string]uint64
	epochs   map[string]uint64
	waves    map[string]int
	cooldown time.Duration
}

func (s *sessionRoundState) initLocked() {
	if s.timers == nil {
		s.timers = map[string]*time.Timer{}
	}
	if s.gens == nil {
		s.gens = map[string]uint64{}
	}
	if s.epochs == nil {
		s.epochs = map[string]uint64{}
	}
	if s.waves == nil {
		s.waves = map[string]int{}
	}
}

func (s *sessionRoundState) cooldownOrDefault() time.Duration {
	if s.cooldown > 0 {
		return s.cooldown
	}
	return 2 * time.Second
}

// beginSessionRoundContinuation ends this wave and queues the next one when
// the session agent still has the task open. A worker loops inside its own
// task. A foreground turn arms a short timer. A light lookup, a goal phase,
// and a turn with no tool progress stay on the manual stop. A repeated-tool
// dead loop already returns its own stop, so one recovered replan does not.
func (h *IMMessageHandler) beginSessionRoundContinuation(ctx *LoopContext, userID, userText string, history []agent.ConversationEntry, toolCalls int) (*IMAgentResponse, bool) {
	task := extractOriginalUserTask(history)
	if strings.TrimSpace(task) == "" {
		task = strings.TrimSpace(userText)
	}
	if !sessionRoundEligible(ctx, task, userText, history, toolCalls) {
		return nil, false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" && ctx != nil {
		userID = strings.TrimSpace(ctx.UserID)
	}
	if userID == "" {
		return nil, false
	}
	// ACP Mode B streams one request through the bridge callbacks and closes
	// its durable attempt when that call returns. A timer wave would keep
	// editing after the editor saw the result, outside that attempt and with
	// no stream to the client. The client owns the retry.
	if isACPAssistantSessionUserID(userID) {
		log.Printf("[session-round] ACP turn does not arm a desktop wave user=%s", userID)
		return nil, false
	}
	// A newer user message arrived while this turn ran. Resuming the old
	// work would compete with the request the user just made, so the newer
	// turn owns the next round. An unprepared context (0) keeps the old
	// behavior for workers and tests.
	if ctx.sessionRoundEpochAtStart != 0 && h.sessionRoundEpoch(userID) != ctx.sessionRoundEpochAtStart {
		log.Printf("[session-round] newer user turn superseded this one; not arming user=%s", userID)
		return nil, false
	}
	if h != nil && h.memory != nil {
		h.createMaxRoundsUnfinishedSlot(userID, history)
	}
	resp := &IMAgentResponse{
		Text:                 sessionRoundContinuationNote(task),
		ResponseSource:       sessionRoundContinuationSource,
		sessionRoundContinue: true,
	}
	if chatTaskWorkerLoop(ctx) {
		return resp, true
	}
	if !h.armSessionRoundContinuation(userID, task) {
		return nil, false
	}
	return resp, true
}

func sessionRoundEligible(ctx *LoopContext, task, userText string, history []agent.ConversationEntry, toolCalls int) bool {
	if ctx == nil || ctx.IsCancelled() || toolCalls <= 0 {
		return false
	}
	if ctx.Kind != LoopKindChat || ctx.ForegroundChatBudget > 0 || ctx.Runtime.Execution.IsLight() {
		return false
	}
	// The wave is delivered through the desktop assistant event stream. A
	// channel turn (Lansenger, WeChat, Hub, group executor, ...) already
	// returned its response, so a wave there would keep working with no
	// delivery path. Leave the retry to the channel user. Empty is the
	// internal/test context and stays on the desktop behavior.
	platform := strings.TrimSpace(ctx.Platform)
	if platform != "" && platform != desktopPlatform && platform != sessionRoundContinuationPlatform {
		log.Printf("[session-round] channel turn does not arm a desktop wave platform=%q user=%q", platform, ctx.UserID)
		return false
	}
	// A workflow phase owns its own retry and resume. A session wave bypasses
	// the workflow router and would leave the phase waiting. WorkflowAgentLoop
	// alone is not the signal: an armed pure-coding session also carries it
	// while the workbench, not a phase, runs.
	if ctx.WorkflowDocPhase || strings.TrimSpace(ctx.WorkflowPhaseID) != "" {
		return false
	}
	task = strings.TrimSpace(task)
	if task == "" || isGoalSystemContinuation(task) || isGoalSystemContinuation(userText) {
		return false
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		text, _ := history[i].Content.(string)
		return !isGoalSystemContinuation(text)
	}
	return true
}

func (h *IMMessageHandler) armSessionRoundContinuation(userID, task string) bool {
	if h == nil || h.app == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	task = strings.TrimSpace(task)
	if userID == "" || task == "" {
		return false
	}
	h.sessionRounds.mu.Lock()
	defer h.sessionRounds.mu.Unlock()
	h.sessionRounds.initLocked()
	if h.sessionRounds.waves[userID] >= sessionRoundContinuationMaxWaves {
		return false
	}
	h.sessionRounds.waves[userID]++
	if existing := h.sessionRounds.timers[userID]; existing != nil {
		existing.Stop()
	}
	gen := h.sessionRounds.gens[userID] + 1
	h.sessionRounds.gens[userID] = gen
	epoch := h.sessionRounds.epochs[userID]
	timer := time.AfterFunc(h.sessionRounds.cooldownOrDefault(), func() {
		h.fireSessionRoundContinuation(userID, task, gen, epoch)
	})
	h.sessionRounds.timers[userID] = timer
	log.Printf("[session-round] scheduled next wave in %s: user=%s wave=%d", h.sessionRounds.cooldownOrDefault(), userID, h.sessionRounds.waves[userID])
	return true
}

// cancelSessionRoundContinuation stops a queued wave and invalidates one that
// has already been fired. pending is true when a timer or a wave count was
// still open, so an explicit stop can report that it cancelled work. A later
// user message uses the same function and ignores pending: that message is
// the next turn, not a request to fence the conversation.
func (h *IMMessageHandler) cancelSessionRoundContinuation(userID string) (pending bool) {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	h.sessionRounds.mu.Lock()
	defer h.sessionRounds.mu.Unlock()
	h.sessionRounds.initLocked()
	if existing := h.sessionRounds.timers[userID]; existing != nil {
		existing.Stop()
		delete(h.sessionRounds.timers, userID)
		pending = true
	}
	if h.sessionRounds.waves[userID] > 0 {
		pending = true
	}
	h.sessionRounds.gens[userID]++
	h.sessionRounds.epochs[userID]++
	delete(h.sessionRounds.waves, userID)
	return pending
}

func (h *IMMessageHandler) resetSessionRoundWaves(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.sessionRounds.mu.Lock()
	defer h.sessionRounds.mu.Unlock()
	if h.sessionRounds.waves == nil {
		return
	}
	delete(h.sessionRounds.waves, userID)
}

func (h *IMMessageHandler) fireSessionRoundContinuation(userID, task string, gen, epoch uint64) {
	if h == nil {
		return
	}
	h.sessionRounds.mu.Lock()
	h.sessionRounds.initLocked()
	if h.sessionRounds.gens[userID] != gen || h.sessionRounds.epochs[userID] != epoch {
		h.sessionRounds.mu.Unlock()
		return
	}
	if existing := h.sessionRounds.timers[userID]; existing != nil {
		existing.Stop()
		delete(h.sessionRounds.timers, userID)
	}
	h.sessionRounds.mu.Unlock()
	if h.app == nil {
		log.Printf("[session-round] fire skipped: app nil user=%s", userID)
		return
	}
	go h.runSessionRoundContinuation(IMUserMessage{
		RequestID: fmt.Sprintf("session-round-%d", time.Now().UnixMilli()),
		UserID:    userID,
		Platform:  sessionRoundContinuationPlatform,
		Text:      sessionRoundContinuationPrompt(task),
	}, task, gen, epoch)
}

func (h *IMMessageHandler) sessionRoundEpoch(userID string) uint64 {
	if h == nil {
		return 0
	}
	userID = strings.TrimSpace(userID)
	h.sessionRounds.mu.Lock()
	defer h.sessionRounds.mu.Unlock()
	h.sessionRounds.initLocked()
	return h.sessionRounds.epochs[userID]
}

func (h *IMMessageHandler) sessionRoundEpochCurrent(userID string, epoch uint64) bool {
	return h != nil && h.sessionRoundEpoch(userID) == epoch
}

func (h *IMMessageHandler) sessionRoundWaveCurrent(userID string, gen, epoch uint64) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	h.sessionRounds.mu.Lock()
	defer h.sessionRounds.mu.Unlock()
	h.sessionRounds.initLocked()
	return h.sessionRounds.gens[userID] == gen && h.sessionRounds.epochs[userID] == epoch
}

func (h *IMMessageHandler) bindPendingMaxRoundsSlot(userID string) {
	if h == nil || h.memory == nil {
		return
	}
	slot := h.memory.GetUnfinishedSlot(userID)
	if slot == nil || slot.Status != agent.UnfinishedTaskSlotStatusMaxRoundsReached {
		return
	}
	if !h.memory.BindUnfinishedSlot(userID, slot.SlotID) {
		log.Printf("[session-round] pending slot was not bound user=%s slot=%s", userID, slot.SlotID)
	}
}

func (h *IMMessageHandler) runSessionRoundContinuation(msg IMUserMessage, task string, gen, epoch uint64) {
	if h == nil {
		return
	}
	if !h.sessionRoundWaveCurrent(msg.UserID, gen, epoch) {
		log.Printf("[session-round] wave superseded before start user=%s", msg.UserID)
		return
	}
	h.bindPendingMaxRoundsSlot(msg.UserID)
	// Stop can land while the slot is being bound. Recheck before the round
	// is announced, and before the completion defer can emit an error pill.
	if !h.sessionRoundWaveCurrent(msg.UserID, gen, epoch) {
		log.Printf("[session-round] wave superseded before start user=%s", msg.UserID)
		return
	}
	h.emitSessionRoundStarted(msg.RequestID, msg.UserID, msg.Text, sessionRoundContinuationDisplay(task))
	finalResponseAttempted := false
	emitFinal := func(resp *IMAgentResponse) {
		if finalResponseAttempted || h.app == nil {
			return
		}
		finalResponseAttempted = true
		if resp == nil {
			resp = &IMAgentResponse{}
		}
		resp.SessionKey = msg.UserID
		h.app.emitAIAssistantResponse(msg.RequestID, resp)
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[session-round] request %s panicked: %v", msg.RequestID, r)
			emitFinal(&IMAgentResponse{Error: fmt.Sprintf("Session continuation failed: %v", r)})
			return
		}
		if !finalResponseAttempted {
			emitFinal(&IMAgentResponse{Error: "Session continuation ended without a final response."})
		}
	}()
	emit := func(name, value string) {
		h.emitSessionRoundEvent(name, msg.RequestID, msg.UserID, value, "")
	}
	resp := h.HandleIMMessageWithProgressAndStream(msg, func(progressText string) {
		if progressText == "" {
			return
		}
		emit("ai-assistant-progress", progressText)
	}, func(delta string) {
		if delta == "" {
			return
		}
		emit("ai-assistant-token", delta)
	}, func() {
		emit("ai-assistant-new-round", "")
	}, func() {
		emit("ai-assistant-stream-done", "")
	})
	emitFinal(resp)
}

func (h *IMMessageHandler) emitSessionRoundStarted(requestID, userID, prompt, displayText string) {
	h.emitSessionRoundEvent("ai-assistant-foreground-round-started", requestID, userID, prompt, displayText)
}

func (h *IMMessageHandler) emitSessionRoundEvent(name, requestID, userID, text, displayText string) {
	if h == nil || h.app == nil || h.app.ctx == nil {
		return
	}
	payload, err := json.Marshal(AIAssistantStreamEvent{
		RequestID:   requestID,
		Text:        text,
		SessionKey:  userID,
		DisplayText: displayText,
	})
	if err != nil {
		log.Printf("[session-round] marshal %s failed: %v", name, err)
		return
	}
	h.app.emitEvent(name, string(payload))
}

func isSessionRoundContinuation(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "[系统续轮]")
}

// sessionRoundTurn is a host wave of the same session task. It is stored as
// a user row, but it is not the person starting a new task, answering a
// question, or confirming a plan.
func sessionRoundTurn(text, platform string) bool {
	return strings.TrimSpace(platform) == sessionRoundContinuationPlatform || isSessionRoundContinuation(text)
}

func sessionRoundContinuationPrompt(task string) string {
	task = truncateRunes(strings.TrimSpace(task), 1200)
	return "[系统续轮] 继续完成同一任务。从对话记录里已经完成的部分接着做，不要重做已经成功的步骤，不要要求用户再发一条消息。\n\n任务：\n" + task
}

func sessionRoundContinuationNote(task string) string {
	minimal := "Round used up; continuing."
	if sessionRoundTextHasHan(task) {
		minimal = "这轮用完了，接着做。"
	}
	return botNotice("session-round-note", "notice", "This round's reasoning budget is used up; the same task continues automatically in a new round.", truncateRunes(task, 200), botNoticeLang(task), minimal, 3*time.Second)
}

func sessionRoundContinuationDisplay(task string) string {
	minimal := "Continuing"
	if sessionRoundTextHasHan(task) {
		minimal = "接着做"
	}
	return botNotice("session-round-display", "notice", "A short status label: still continuing the same task.", truncateRunes(task, 200), botNoticeLang(task), minimal, 2*time.Second)
}

func sessionRoundTextHasHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
