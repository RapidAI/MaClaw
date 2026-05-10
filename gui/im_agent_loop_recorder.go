package main

type agentLoopRecorderBundle struct {
	Recorder             *TrajectoryRecorder
	AdaptiveRetry        *AdaptiveRetry
	RecordSystemMessages func(int, []interface{})
	RecordToolCall       func(string, string, string)
	RecordToolResult     func(string, interface{})
	Cleanup              func()
}

func (h *IMMessageHandler) prepareAgentLoopRecorderBundle(adaptiveRetry *AdaptiveRetry) agentLoopRecorderBundle {
	var recorder *TrajectoryRecorder
	if h.trajectoryRecorderFactory != nil {
		recorder = h.trajectoryRecorderFactory()
	}
	cleanup := func() {}
	if recorder != nil {
		cleanup = recorder.Flush
		if adaptiveRetry == nil {
			adaptiveRetry = NewAdaptiveRetry(recorder)
		}
	}
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
