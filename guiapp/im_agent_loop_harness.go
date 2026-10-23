package guiapp

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

type agentLoopHarnessState struct {
	GoalAnchor       *GoalAnchor
	DriftDetector    *DriftDetector
	ProgressTracker  *HarnessProgressTracker
	AdaptiveRetry    *AdaptiveRetry
	PriorReplanCount int
}

type agentLoopRuntimeState struct {
	RequestContext       context.Context
	CancelRequestContext func()
	GoalAnchor           *GoalAnchor
	DriftDetector        *DriftDetector
	ProgressTracker      *HarnessProgressTracker
	AdaptiveRetry        *AdaptiveRetry
	PriorReplanCount     int
	SendProgress         func(string)
	SendToolProgress     func(string)
	StreamDoneCallback   StreamDoneCallback
	IsDebug              func() bool
	MilestoneTracker     *progress.AgentProgressTracker
	Cleanup              func()
}

func (h *IMMessageHandler) prepareAgentLoopHarnessState(userID, userText string) agentLoopHarnessState {
	state := agentLoopHarnessState{}

	// GoalAnchor: always create a fresh instance per loop, keyed to *this*
	// loop's userText. The handler field h.goalAnchor acts only as a config
	// template supplying anchorInterval; we never share the live instance
	// across tabs. This mirrors the DriftDetector pattern.
	anchorInterval := 5
	if h.goalAnchor != nil {
		anchorInterval = h.goalAnchor.AnchorInterval()
	}
	state.GoalAnchor = NewGoalAnchor(userText, anchorInterval)

	if v, ok := h.sessionDriftReplanCount.Load(userID); ok {
		state.PriorReplanCount = v.(int)
	}
	if h.driftDetector != nil {
		state.DriftDetector = NewDriftDetectorWithHistory(h.driftDetector.windowSize, h.driftDetector.similarityThresh, state.PriorReplanCount)
	} else {
		state.DriftDetector = NewDriftDetectorWithHistory(0, 0, state.PriorReplanCount)
	}

	// HarnessProgressTracker: not shared. The handler field would require
	// identical checklist items across all sessions which is meaningless.
	// Production code never calls SetHarnessProgressTracker; nil is correct.
	// Tests that need a tracker construct one directly and pass it via opts.
	state.ProgressTracker = nil

	// AdaptiveRetry: the handler field h.adaptiveRetry is purely a config
	// template (carries maxFailures override). A fresh instance with empty
	// mutable state is created per loop inside prepareAgentLoopRecorderBundle,
	// which has access to the per-loop recorder. We pass the template through
	// agentLoopStartOptions so the bundle can read maxFailures from it.
	// Do NOT assign h.adaptiveRetry to state.AdaptiveRetry here; that would
	// share the template's nil/zero maps across concurrent tabs.
	state.AdaptiveRetry = nil // populated by prepareAgentLoopRecorderBundle via opts

	return state
}

func (h *IMMessageHandler) beginAgentLoopRuntimeState(ctx *LoopContext, userID, userText string, onProgress func(string), onStreamDone StreamDoneCallback, telemetry *agentLoopTelemetry) agentLoopRuntimeState {
	var requestCtx context.Context
	var cancelRequestCtx context.CancelFunc
	if ctx != nil {
		requestCtx, cancelRequestCtx = ctx.Context()
	} else {
		requestCtx, cancelRequestCtx = context.WithCancel(context.Background())
	}
	caller := "agent_loop"
	requestID, loopID := "", ""
	if ctx != nil {
		if strings.TrimSpace(ctx.LLMCaller) != "" {
			caller = strings.TrimSpace(ctx.LLMCaller)
		} else if ctx.Kind == LoopKindBackground {
			caller = "background_agent_loop"
		}
		requestID = ctx.Runtime.RequestID
		loopID = ctx.ID
	}
	requestCtx = llm.WithRequestTrace(requestCtx, llm.RequestTrace{
		Caller:    caller,
		OwnerID:   userID,
		RequestID: requestID,
		LoopID:    loopID,
	})
	harnessState := h.prepareAgentLoopHarnessState(userID, userText)
	uiLang := h.imUILang()
	sendProgress := agentLoopProgressSenderLang(onProgress, uiLang)
	milestoneTracker, cleanupMilestoneTracker := h.startAgentLoopMilestoneTracker(userID, userText, sendProgress, uiLang)
	stopHeartbeat := startAgentLoopHeartbeat(sendProgress)
	cleanup := func() {
		stopHeartbeat()
		cleanupMilestoneTracker()
		cancelRequestCtx()
	}
	return agentLoopRuntimeState{
		RequestContext:       requestCtx,
		CancelRequestContext: cancelRequestCtx,
		GoalAnchor:           harnessState.GoalAnchor,
		DriftDetector:        harnessState.DriftDetector,
		ProgressTracker:      harnessState.ProgressTracker,
		AdaptiveRetry:        harnessState.AdaptiveRetry,
		PriorReplanCount:     harnessState.PriorReplanCount,
		SendProgress:         sendProgress,
		SendToolProgress:     sendProgress,
		StreamDoneCallback:   telemetry.WrapStreamDoneCallback(onStreamDone),
		IsDebug:              h.newAgentLoopDebugFlag(),
		MilestoneTracker:     milestoneTracker,
		Cleanup:              cleanup,
	}
}

func (h *IMMessageHandler) newAgentLoopDebugFlag() func() bool {
	// Read once at loop construction time. Debug mode doesn't need live-toggling
	// mid-loop, and this eliminates per-iteration configMu lock acquisition.
	c, err := h.loadConfig()
	debugEnabled := err == nil && c.MaclawDebugToolCalls
	return func() bool { return debugEnabled }
}

func (h *IMMessageHandler) startAgentLoopMilestoneTracker(userID, userText string, sendProgress func(string), uiLang string) (*progress.AgentProgressTracker, func()) {
	var taskEmbed []float32
	if h.interruptHandler != nil {
		taskEmbed = h.interruptHandler.EmbedText(userText)
	}
	if strings.TrimSpace(uiLang) == "" {
		uiLang = h.imUILang()
	}
	tracker := progress.NewAgentProgressTrackerLang(
		func(text string) { sendProgress(text) },
		userText, "", taskEmbed, uiLang,
	)
	cleanup := func() {
		tracker.Stop()
		if h.interruptHandler != nil {
			h.interruptHandler.ClearTrackerIfCurrent(userID, tracker)
		}
	}
	if h.interruptHandler != nil {
		h.interruptHandler.SetTracker(userID, tracker)
	}
	return tracker, cleanup
}

// startHeartbeatTicker starts a periodic heartbeat that sends imHeartbeatMsg
// through the given progress callback. Returns a stop function.
// If sendProgress is nil, returns a no-op stop function.
func startHeartbeatTicker(interval time.Duration, sendProgress func(string)) func() {
	if sendProgress == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendProgress(imHeartbeatMsg)
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
	}
}

func startAgentLoopHeartbeat(sendProgress func(string)) func() {
	return startHeartbeatTicker(60*time.Second, sendProgress)
}

// startRequestLevelHeartbeat covers the entire request processing lifecycle,
// including pre-loop phases (IUM LLM calls, proactive_recall, system prompt
// construction) that happen before the agent loop's own heartbeat starts.
// Interval is 30s, safely below the frontend's configurable activity timeout.
func startRequestLevelHeartbeat(onProgress func(string)) func() {
	return startHeartbeatTicker(30*time.Second, onProgress)
}

// llmRetryActivityNoteInterval is how often a live retry row refreshes the
// milestone silence clock. The silence heartbeat fires after 90s without a
// milestone. One note at the announcement does not cover the following HTTP
// call or a period-quota sleep, both of which can run longer than that.
var llmRetryActivityNoteInterval = 30 * time.Second

type llmRetryActivity int

const (
	llmRetryActivityNone llmRetryActivity = iota
	llmRetryActivityArm
	llmRetryActivityDisarm
)

// llmRetryProgressActivity decides whether this progress line keeps the
// model-wait row alive. The row stays up through the sleep and the request.
// A later tray milestone means that wait is over. Heartbeats, thinking-panel
// status, and the generic acknowledgement do not enter the tray, so they
// must not end the wait either.
func llmRetryProgressActivity(text string) llmRetryActivity {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || trimmed == imHeartbeatMsg || strings.Contains(trimmed, "命令仍在执行中") {
		return llmRetryActivityNone
	}
	if _, ok := agent.LLMRetryLiveStatusText(trimmed); ok {
		return llmRetryActivityArm
	}
	if strings.HasPrefix(trimmed, "[Status]") || progressTextIsAcknowledgement(trimmed) {
		return llmRetryActivityNone
	}
	return llmRetryActivityDisarm
}

func progressTextIsAcknowledgement(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) >= len("[Status]") && strings.EqualFold(trimmed[:len("[Status]")], "[Status]") {
		trimmed = strings.TrimSpace(trimmed[len("[Status]"):])
	}
	for _, prefix := range []string{"〔进度〕", "[进度]", "【进度】", "〔Progress〕", "[Progress]", "【Progress】"} {
		if strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(prefix)) {
			trimmed = strings.TrimSpace(trimmed[len(prefix):])
			break
		}
	}
	return trimmed == "收到，正在处理"
}

// startRetryActivityNotes records activity immediately and again on each
// interval until stop. The caller stops it when the wait ends or the turn ends.
// The immediate note runs on the caller so the silence clock moves before the
// next wait. Callers must not hold a lock that the note, or a progress
// callback reached from the pusher, also needs.
func startRetryActivityNotes(note func(), interval time.Duration) func() {
	if note == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = llmRetryActivityNoteInterval
	}
	note()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// A ready ticker stays ready after cancel. Leaving on the first
				// extra tick lets the goroutine exit instead of noting forever.
				if ctx.Err() != nil {
					return
				}
				note()
			}
		}
	}()
	return cancel
}

func (ih *imInterruptHandler) startRetryActivity(userID string) func() {
	if ih == nil {
		return func() {}
	}
	return startRetryActivityNotes(func() {
		ih.NoteTrackerActivity(userID)
	}, llmRetryActivityNoteInterval)
}

// observeLLMRetryProgress keeps the milestone silence clock fresh while a
// live retry row is the current phase. One note at the announcement does not
// cover the following HTTP call or a period-quota sleep. The ticker keeps
// noting until a tray milestone replaces that row, or until the turn ends.
// The shared loop emits the row as an event, and the legacy loop calls
// onProgress directly. Neither path records a milestone.
//
// The silence heartbeat takes the pusher lock and then delivers through this
// same callback. Noting activity while this callback's lock is held inverts
// those two locks, so the note always happens after the lock is released.
func observeLLMRetryProgress(h *IMMessageHandler, userID string, onProgress func(string)) (func(string), func()) {
	if h == nil || onProgress == nil {
		return onProgress, func() {}
	}
	var mu sync.Mutex
	var stop func()
	starting := false
	release := func() {
		mu.Lock()
		current := stop
		stop = nil
		starting = false
		mu.Unlock()
		if current != nil {
			current()
		}
	}
	return func(text string) {
		switch llmRetryProgressActivity(text) {
		case llmRetryActivityArm:
			if h.interruptHandler != nil {
				mu.Lock()
				already := stop != nil || starting
				if !already {
					starting = true
				}
				mu.Unlock()
				if !already {
					started := h.interruptHandler.startRetryActivity(userID)
					mu.Lock()
					if starting {
						stop = started
						starting = false
						mu.Unlock()
					} else {
						// Released while the ticker was being created.
						mu.Unlock()
						started()
					}
				} else {
					h.interruptHandler.NoteTrackerActivity(userID)
				}
			}
		case llmRetryActivityDisarm:
			release()
		}
		onProgress(text)
	}, release
}

func agentLoopProgressSender(onProgress func(string)) func(string) {
	return agentLoopProgressSenderLang(onProgress, "")
}

// agentLoopProgressSenderLang wraps progress delivery so intermediate status
// text is visually distinct from final assistant replies on IM channels.
// Tool activity items (工具 · /Tool · ) and heartbeats are left unchanged.
func agentLoopProgressSenderLang(onProgress func(string), lang string) func(string) {
	return func(text string) {
		if onProgress == nil {
			return
		}
		onProgress(styleIMIntermediateProgress(lang, text))
	}
}
