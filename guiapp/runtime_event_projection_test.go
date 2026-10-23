package guiapp

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestShouldProjectRuntimeCallbacksIncludesProgressAndRound(t *testing.T) {
	if !shouldProjectRuntimeCallbacks(nil, func(string) {}, nil) {
		t.Fatal("progress callback should be projected")
	}
	if !shouldProjectRuntimeCallbacks(nil, nil, func() {}) {
		t.Fatal("new-round callback should be projected")
	}
	if shouldProjectRuntimeCallbacks(nil, nil, nil) {
		t.Fatal("empty callbacks should not install projection")
	}
}

func TestRuntimeEventProjectionForwardsAssistantDelta(t *testing.T) {
	var got string
	sink := runtimeEventProjectionSink{onToken: func(delta string) { got += delta }}
	if err := sink.Emit(context.Background(), agentruntime.Event{
		Type:    agentruntime.EventAssistantDelta,
		Payload: map[string]any{"delta": "hello"},
	}); err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("projected delta = %q, want hello", got)
	}
}

func TestProjectGUIRuntimeEventsForwardsRetryProgress(t *testing.T) {
	var got string
	events := projectGUIRuntimeEvents(nil, nil, func(text string) { got = text }, nil)
	if err := events.Emit(context.Background(), agentruntime.Event{
		Type:    agentruntime.EventProgress,
		Payload: map[string]any{"text": "重试 1/5：API 暂时不可用，2 秒后重试"},
	}); err != nil {
		t.Fatal(err)
	}
	if got != "重试 1/5：API 暂时不可用，2 秒后重试" {
		t.Fatalf("chat progress = %q", got)
	}
}

func TestRuntimeEventProjectionForwardsProgressAndRound(t *testing.T) {
	var progress string
	rounds := 0
	sink := runtimeEventProjectionSink{
		onProgress: func(text string) { progress = text },
		onNewRound: func() { rounds++ },
	}
	if err := sink.Emit(context.Background(), agentruntime.Event{Type: agentruntime.EventProgress, Payload: map[string]any{"text": "working"}}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Emit(context.Background(), agentruntime.Event{Type: agentruntime.EventTurnStarted}); err != nil {
		t.Fatal(err)
	}
	if progress != "working" || rounds != 1 {
		t.Fatalf("projection = progress %q rounds %d", progress, rounds)
	}
}
