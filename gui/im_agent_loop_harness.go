package main

import (
	"context"
	"sync"
	"time"

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
	requestCtx, cancelRequestCtx := ctx.Context()
	caller := "agent_loop"
	if ctx != nil && ctx.Kind == LoopKindBackground {
		caller = "background_agent_loop"
	}
	requestCtx = llm.WithRequestTrace(requestCtx, llm.RequestTrace{
		Caller:    caller,
		OwnerID:   userID,
		RequestID: ctx.Runtime.RequestID,
		LoopID:    ctx.ID,
	})
	harnessState := h.prepareAgentLoopHarnessState(userID, userText)
	sendProgress := agentLoopProgressSender(onProgress)
	milestoneTracker, cleanupMilestoneTracker := h.startAgentLoopMilestoneTracker(userID, userText, sendProgress)
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

func (h *IMMessageHandler) startAgentLoopMilestoneTracker(userID, userText string, sendProgress func(string)) (*progress.AgentProgressTracker, func()) {
	var taskEmbed []float32
	if h.interruptHandler != nil {
		taskEmbed = h.interruptHandler.EmbedText(userText)
	}
	tracker := progress.NewAgentProgressTracker(
		func(text string) { sendProgress(text) },
		userText, "", taskEmbed,
	)
	cleanup := func() {
		tracker.Stop()
		if h.interruptHandler != nil {
			h.interruptHandler.ClearTracker(userID)
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

func agentLoopProgressSender(onProgress func(string)) func(string) {
	return func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}
}
