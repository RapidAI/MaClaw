package guiapp

import (
	"context"
	"errors"

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
	projection := &runtimeEventProjectionSink{onToken: onToken, onProgress: onProgress, onNewRound: onNewRound}
	// Both sides are fixed for this turn. An absent primary is dropped once
	// here, so a streamed delta does not repeat the nil-pointer check.
	if agentruntime.EventSinkAbsent(events) {
		return projection
	}
	return &guiRuntimeFanout{primary: events, projection: projection}
}

// guiRuntimeFanout delivers one event to the host sink and the desktop
// callbacks. The primary was already checked when the fanout was built.
type guiRuntimeFanout struct {
	primary    agentruntime.EventSink
	projection *runtimeEventProjectionSink
}

func (f *guiRuntimeFanout) Emit(ctx context.Context, event agentruntime.Event) error {
	if f == nil {
		return nil
	}
	err := f.primary.Emit(ctx, event)
	var perr error
	if f.projection != nil {
		perr = f.projection.Emit(ctx, event)
	}
	if err == nil || perr == nil {
		if err != nil {
			return err
		}
		return perr
	}
	return errors.Join(err, perr)
}

func (s *runtimeEventProjectionSink) Emit(_ context.Context, event agentruntime.Event) error {
	if s == nil {
		return nil
	}
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
