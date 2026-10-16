package agentruntime

import (
	"context"
	"errors"
	"testing"
)

func TestFanoutEventSinkAttemptsAllSinksAndJoinsErrors(t *testing.T) {
	var calls int
	first := errors.New("durable unavailable")
	second := errors.New("ui unavailable")
	sink := FanoutEventSink{Sinks: []EventSink{
		EventSinkFunc(func(context.Context, Event) error { calls++; return first }),
		EventSinkFunc(func(context.Context, Event) error { calls++; return second }),
	}}
	err := sink.Emit(context.Background(), Event{Type: "run.started"})
	if calls != 2 {
		t.Fatalf("fanout calls = %d, want 2", calls)
	}
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("fanout error = %v, want both causes", err)
	}
}

type nilEventSink struct{}

func (*nilEventSink) Emit(context.Context, Event) error { return nil }

func TestFanoutEventSinkSkipsTypedNilSink(t *testing.T) {
	var typedNil *nilEventSink
	called := false
	sink := FanoutEventSink{Sinks: []EventSink{typedNil, EventSinkFunc(func(context.Context, Event) error { called = true; return nil })}}
	if err := sink.Emit(context.Background(), Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("fanout did not continue after typed nil sink")
	}
}
