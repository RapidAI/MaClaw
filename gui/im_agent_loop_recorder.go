package main

type agentLoopRecorderBundle struct {
	Recorder             *TrajectoryRecorder
	AdaptiveRetry        *AdaptiveRetry
	RecordSystemMessages func(int, []interface{})
	RecordToolCall       func(string, string, string)
	RecordToolResult     func(string, interface{})
	Cleanup              func()
}

// prepareAgentLoopRecorderBundle creates a per-loop recorder and an isolated
// AdaptiveRetry instance. The template parameter carries only immutable config
// (maxFailures override); mutable state (failureCounts, disabledTools, etc.)
// is always fresh so concurrent tabs never share retry decisions.
func (h *IMMessageHandler) prepareAgentLoopRecorderBundle(template *AdaptiveRetry) agentLoopRecorderBundle {
	var recorder *TrajectoryRecorder
	if h.trajectoryRecorderFactory != nil {
		recorder = h.trajectoryRecorderFactory()
	}
	cleanup := func() {}
	if recorder != nil {
		cleanup = recorder.Flush
	}
	// Always create a fresh AdaptiveRetry for this loop. The template is only
	// consulted for maxFailures; all mutable tracking state is brand-new.
	// This mirrors how DriftDetector is instantiated: handler field = config
	// template, loop-local instance = live state.
	//
	// Use h.adaptiveRetry as the config template (maxFailures override), but
	// fall back to the caller-supplied template if non-nil (used in tests).
	configTemplate := h.adaptiveRetry
	if configTemplate == nil {
		configTemplate = template
	}
	adaptiveRetry := NewAdaptiveRetryForLoop(configTemplate, recorder)
	adaptiveRetry.SetMemoryStore(h.memoryStore)
	loopRecorder := newAgentLoopRecorder(recorder)
	return agentLoopRecorderBundle{
		Recorder:             recorder,
		AdaptiveRetry:        adaptiveRetry,
		RecordSystemMessages: loopRecorder.RecordSystemMessages,
		RecordToolCall:       loopRecorder.RecordToolCall,
		RecordToolResult:     loopRecorder.RecordToolResult,
		Cleanup:              cleanup,
	}
}

type agentLoopRecorder struct {
	recorder *TrajectoryRecorder
}

func newAgentLoopRecorder(recorder *TrajectoryRecorder) agentLoopRecorder {
	return agentLoopRecorder{recorder: recorder}
}

func (r agentLoopRecorder) RecordSystemMessages(start int, items []interface{}) {
	if r.recorder == nil {
		return
	}
	for i := start; i < len(items); i++ {
		msg, ok := items[i].(map[string]string)
		if !ok {
			continue
		}
		if msg["role"] != "system" {
			continue
		}
		r.recorder.Record("system", msg["content"], nil, "", "")
	}
}

func (r agentLoopRecorder) RecordToolCall(id, name, args string) {
	if r.recorder == nil {
		return
	}
	r.recorder.Record("tool", map[string]interface{}{
		"name":      name,
		"arguments": args,
	}, nil, id, "")
}

func (r agentLoopRecorder) RecordToolResult(id string, content interface{}) {
	if r.recorder == nil {
		return
	}
	r.recorder.Record("tool_result", content, nil, id, "")
}
