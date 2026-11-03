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

func TestEmitRuntimeEventToSinkSkipsTypedNil(t *testing.T) {
	var typedNil *recordingRuntimeEventSink
	if err := emitRuntimeEventToSink(typedNil, agentruntime.Scope{RunID: "run-1"}, agentruntime.EventRunStarted, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTypedNilRuntimeSinkUsesLegacyCallbacks(t *testing.T) {
	var typedNil *recordingRuntimeEventSink
	var rounds, progress int
	cb := &sharedAgentLoopCallbacks{
		events:     typedNil,
		onNewRound: func() { rounds++ },
		onProgress: func(string) { progress++ },
	}
	cb.OnToken("delta")
	cb.OnLLMNewRound()
	cb.OnProgress("step")
	if rounds != 1 || progress != 1 {
		t.Fatalf("legacy callbacks rounds=%d progress=%d", rounds, progress)
	}
}

func TestLiveRuntimeSinkStaysOnTheEventPath(t *testing.T) {
	sink := &recordingRuntimeEventSink{}
	var rounds int
	cb := &sharedAgentLoopCallbacks{
		events:     sink,
		eventScope: agentruntime.Scope{RunID: "run-1"},
		onNewRound: func() { rounds++ },
	}
	cb.OnLLMNewRound()
	cb.OnToken("delta")
	if rounds != 0 {
		t.Fatalf("legacy new-round ran alongside a live sink: %d", rounds)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want turn start and delta", len(sink.events))
	}
}

func TestOnTokenProjectsDirectlyWhenThereIsNoHostSink(t *testing.T) {
	var got string
	cb := &sharedAgentLoopCallbacks{
		events: projectGUIRuntimeEvents(nil, func(delta string) { got += delta }, nil, nil),
	}
	cb.OnToken("ab")
	cb.OnToken("")
	if got != "ab" {
		t.Fatalf("delta = %q", got)
	}
}

func TestOnTokenStillEmitsWhenAHostSinkIsPresent(t *testing.T) {
	primary := &recordingRuntimeEventSink{}
	var got string
	cb := &sharedAgentLoopCallbacks{
		events:     projectGUIRuntimeEvents(primary, func(delta string) { got += delta }, nil, nil),
		eventScope: agentruntime.Scope{RunID: "run-1"},
	}
	cb.OnToken("ab")
	if got != "ab" {
		t.Fatalf("projected = %q", got)
	}
	if len(primary.events) != 1 || primary.events[0].Type != agentruntime.EventAssistantDelta {
		t.Fatalf("host events = %#v", primary.events)
	}
	payload, _ := primary.events[0].Payload["delta"].(string)
	if payload != "ab" {
		t.Fatalf("host delta = %q", payload)
	}
}
