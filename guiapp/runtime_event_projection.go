package guiapp

import (
	"context"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// runtimeEventProjectionSink adapts the shared event stream to legacy GUI
// callbacks. Keeping this as an EventSink lets the GUI consume the same
// envelope that headless transports persist, while old callback consumers can
// migrate without changing their public signatures.
type runtimeEventProjectionSink struct {
	onToken    func(string)
	onProgress func(string)
	onNewRound func()
}

func shouldProjectRuntimeCallbacks(onToken func(string), onProgress func(string), onNewRound func()) bool {
	return onToken != nil || onProgress != nil || onNewRound != nil
}

// projectGUIRuntimeEvents fans a turn's runtime events out to the desktop
// chat callbacks. Progress has to be included: the shared loop emits retry
// status only as EventProgress, and a sink built without onProgress drops
// "重试 1/5" before the dialog can show it.
func projectGUIRuntimeEvents(events agentruntime.EventSink, onToken func(string), onProgress func(string), onNewRound func()) agentruntime.EventSink {
	if !shouldProjectRuntimeCallbacks(onToken, onProgress, onNewRound) {
		return events
	}
	return agentruntime.FanoutEventSink{Sinks: []agentruntime.EventSink{
		events,
		runtimeEventProjectionSink{onToken: onToken, onProgress: onProgress, onNewRound: onNewRound},
	}}
}

func (s runtimeEventProjectionSink) Emit(_ context.Context, event agentruntime.Event) error {
	switch event.Type {
	case agentruntime.EventAssistantDelta:
		if s.onToken == nil || event.Payload == nil {
			return nil
		}
		delta, _ := event.Payload["delta"].(string)
		if delta != "" {
			s.onToken(delta)
		}
	case agentruntime.EventProgress:
		if s.onProgress == nil || event.Payload == nil {
			return nil
		}
		progress, _ := event.Payload["text"].(string)
		if progress != "" {
			s.onProgress(progress)
		}
	case agentruntime.EventTurnStarted:
		if s.onNewRound != nil {
			s.onNewRound()
		}
	}
	return nil
}
