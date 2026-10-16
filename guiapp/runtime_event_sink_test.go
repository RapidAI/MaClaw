package guiapp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

type failingRuntimeEventSink struct{ err error }

func (s failingRuntimeEventSink) Emit(context.Context, agentruntime.Event) error { return s.err }

type recordingRuntimeEventSink struct {
	events []agentruntime.Event
}

func (s *recordingRuntimeEventSink) Emit(_ context.Context, event agentruntime.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestSharedAgentLoopCallbacksEmitRuntimeEvents(t *testing.T) {
	sink := &recordingRuntimeEventSink{}
	cb := &sharedAgentLoopCallbacks{
		events:     sink,
		eventScope: agentruntime.Scope{UserID: "user-1", RunID: "run-1"},
	}
	cb.OnToken("hello")
	cb.OnToolCall("read_file")
	cb.OnToolExecuted("read_file", `{}`, "ok", true)

	if len(sink.events) != 3 {
		t.Fatalf("event count = %d, want 3: %#v", len(sink.events), sink.events)
	}
	want := []string{"assistant.delta", "tool.call", "tool.result"}
	for i, event := range sink.events {
		if event.Type != want[i] || event.RunID != "run-1" || event.Scope.UserID != "user-1" {
			t.Fatalf("event[%d] = %#v", i, event)
		}
		if event.EventID == "" || event.Sequence != uint64(i+1) {
			t.Fatalf("event[%d] missing producer id/sequence: %#v", i, event)
		}
		if event.SchemaVersion != agentruntime.ContractVersion {
			t.Fatalf("event[%d] schema = %q", i, event.SchemaVersion)
		}
	}
}

func TestSharedAgentLoopCallbacksRetainEventSinkFailure(t *testing.T) {
	want := errors.New("outbox unavailable")
	cb := &sharedAgentLoopCallbacks{
		events:     failingRuntimeEventSink{err: want},
		eventScope: agentruntime.Scope{UserID: "user-1", RunID: "run-1"},
	}
	cb.OnToken("hello")
	cb.OnToolCall("read_file")
	if got := cb.runtimeEventError(); !errors.Is(got, want) {
		t.Fatalf("runtime event error = %v, want %v", got, want)
	}
	// The first failure is stable even if subsequent callbacks fail differently.
	cb.recordRuntimeEventError(errors.New("later failure"))
	if got := cb.runtimeEventError(); !errors.Is(got, want) {
		t.Fatalf("runtime event error changed after second failure: %v", got)
	}
}

func TestEmitRuntimeEventToSinkReturnsFailure(t *testing.T) {
	want := errors.New("sink down")
	got := emitRuntimeEventToSink(failingRuntimeEventSink{err: want}, agentruntime.Scope{RunID: "run-1"}, "run.started", nil)
	if !errors.Is(got, want) {
		t.Fatalf("emitRuntimeEventToSink error = %v, want %v", got, want)
	}
}

func TestEmitRuntimeEventToSinkAssignsStableSequenceAndID(t *testing.T) {
	sink := &recordingRuntimeEventSink{}
	var sequence atomic.Uint64
	if err := emitRuntimeEventToSink(sink, agentruntime.Scope{RunID: "run-1"}, agentruntime.EventRunStarted, nil, &sequence); err != nil {
		t.Fatal(err)
	}
	if err := emitRuntimeEventToSink(sink, agentruntime.Scope{RunID: "run-1"}, agentruntime.EventRunCompleted, nil, &sequence); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 || sink.events[0].Sequence != 1 || sink.events[1].Sequence != 2 {
		t.Fatalf("event sequences = %#v", sink.events)
	}
	if sink.events[0].EventID != "run-1:run.started:1" || sink.events[1].EventID != "run-1:run.completed:2" {
		t.Fatalf("event ids = %#v", sink.events)
	}
}
