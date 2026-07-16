package main

type agentLoopRecorderBundle struct {
	Recorder             *TrajectoryRecorder
	AdaptiveRetry        *AdaptiveRetry
	RecordSystemMessages func(int, []interface{})
	RecordToolCall       func(string, string, string)
	// RecordToolResult records a tool result. toolName/outcome may be empty when unknown.
	RecordToolResult func(id string, content interface{}, toolName, outcome string)
	Cleanup          func()
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

// newTrajectoryRecorderIfEnabled returns a recorder when the host factory is set
// and trajectory logging is on. Used by SubAgents that bypass the main agent loop.
func (h *IMMessageHandler) newTrajectoryRecorderIfEnabled() *TrajectoryRecorder {
	if h == nil || h.trajectoryRecorderFactory == nil {
		return nil
	}
	return h.trajectoryRecorderFactory()
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
	if start < 0 {
		start = 0
	}
	for i := start; i < len(items); i++ {
		role, content, ok := trajectoryMessageRoleContent(items[i])
		if !ok || role != "system" {
			continue
		}
		r.recorder.Record("system", content, nil, "", "")
	}
}

func trajectoryMessageRoleContent(item interface{}) (role string, content interface{}, ok bool) {
	switch msg := item.(type) {
	case map[string]string:
		role = msg["role"]
		content = msg["content"]
		return role, content, role != ""
	case map[string]interface{}:
		role, _ = msg["role"].(string)
		content = msg["content"]
		return role, content, role != ""
	default:
		return "", nil, false
	}
}

func (r agentLoopRecorder) RecordToolCall(id, name, args string) {
	if r.recorder == nil {
		return
	}
	r.recorder.RecordEntry(TrajectoryEntry{
		Role:       "tool",
		Content:    map[string]interface{}{"name": name, "arguments": args},
		ToolCallID: id,
		ToolName:   name,
	})
}

func (r agentLoopRecorder) RecordToolResult(id string, content interface{}, toolName, outcome string) {
	if r.recorder == nil {
		return
	}
	r.recorder.RecordEntry(TrajectoryEntry{
		Role:        "tool_result",
		Content:     content,
		ToolCallID:  id,
		ToolName:    toolName,
		ToolOutcome: outcome,
	})
}

