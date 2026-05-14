package main

import (
	"context"
	"sync"
	"time"

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
	if h.goalAnchor != nil {
		state.GoalAnchor = h.goalAnchor
	} else {
		state.GoalAnchor = NewGoalAnchor(userText, 5)
	}

	if v, ok := h.sessionDriftReplanCount.Load(userID); ok {
		state.PriorReplanCount = v.(int)
	}
	if h.driftDetector != nil {
		state.DriftDetector = NewDriftDetectorWithHistory(h.driftDetector.windowSize, h.driftDetector.similarityThresh, state.PriorReplanCount)
	} else {
		state.DriftDetector = NewDriftDetectorWithHistory(0, 0, state.PriorReplanCount)
	}

	if h.harnessProgressTracker != nil {
		state.ProgressTracker = h.harnessProgressTracker
	}
	if h.adaptiveRetry != nil {
		state.AdaptiveRetry = h.adaptiveRetry
	}
	return state
}

func (h *IMMessageHandler) beginAgentLoopRuntimeState(ctx *LoopContext, userID, userText string, onProgress func(string), onStreamDone StreamDoneCallback, telemetry *agentLoopTelemetry) agentLoopRuntimeState {
	requestCtx, cancelRequestCtx := ctx.Context()
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
	var cached bool
	var cachedAt time.Time
	return func() bool {
		if now := time.Now(); now.Sub(cachedAt) > 2*time.Second {
			c, err := h.loadConfig()
			if err != nil {
				cached = false
			} else {
				cached = c.MaclawDebugToolCalls
			}
			cachedAt = now
		}
		return cached
	}
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
// Interval is 30s — half of the frontend's 120s activity timeout.
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
