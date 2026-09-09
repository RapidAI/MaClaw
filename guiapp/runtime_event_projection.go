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
