package agentservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

type callbackEventExecutor struct{}

func (callbackEventExecutor) Execute(_ context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	if req.OnToken != nil {
		req.OnToken("hello")
	}
	if req.OnToolCall != nil {
		req.OnToolCall("search")
	}
	if req.OnToolResult != nil {
		req.OnToolResult("search", "ok")
	}
	return &ExecuteResult{Content: "done", OutputType: "text/plain"}, nil
}

func TestMemoryRunEventStoreAssignsMonotonicSequenceAndDeduplicates(t *testing.T) {
	store := NewMemoryRunEventStore()
	first, err := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Type: "run.started"})
	if err != nil {
		t.Fatalf("Append first: %v", err)
	}
	second, err := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Type: "run.completed"})
	if err != nil {
		t.Fatalf("Append second: %v", err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("unexpected sequences: %d, %d", first.Sequence, second.Sequence)
	}
	replay, err := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Sequence: 2, Type: "duplicate"})
	if err != nil || replay.Type != second.Type {
		t.Fatalf("duplicate append = %#v, %v; want original event", replay, err)
	}
	events, err := store.ListAfter("t", "u", "r", 1, 10)
	if err != nil || len(events) != 1 || events[0].Type != "run.completed" {
		t.Fatalf("ListAfter = %#v, %v", events, err)
	}
}

func TestMemoryRunEventStoreClonesPayloadWithoutDroppingFields(t *testing.T) {
	store := NewMemoryRunEventStore()
	payload := map[string]any{"message_id": "msg-1", "nested": map[string]any{"ok": true}}
	event, err := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Type: "assistant.message", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if event.Payload["message_id"] != "msg-1" {
		t.Fatalf("payload=%#v; message_id was lost", event.Payload)
	}
	payload["message_id"] = "mutated"
	payload["nested"].(map[string]any)["ok"] = false
	listed, err := store.ListAfter("t", "u", "r", 0, 10)
	if err != nil || len(listed) != 1 || listed[0].Payload["message_id"] != "msg-1" || listed[0].Payload["nested"].(map[string]any)["ok"] != true {
		t.Fatalf("stored payload=%#v err=%v", listed, err)
	}
	if listed[0].SchemaVersion != RunEventSchemaVersion {
		t.Fatalf("schema version=%q; want %q", listed[0].SchemaVersion, RunEventSchemaVersion)
	}
}

func TestMemoryRunEventStoreRejectsUnserializablePayload(t *testing.T) {
	store := NewMemoryRunEventStore()
	if _, err := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Type: "assistant.delta", Payload: map[string]any{"bad": func() {}}}); err == nil {
		t.Fatal("expected unserializable payload to be rejected")
	}
	if events, err := store.ListAfter("t", "u", "r", 0, 10); err != nil || len(events) != 0 {
		t.Fatalf("rejected append changed store: events=%#v err=%v", events, err)
	}
}

func TestMemoryRunEventStoreRejectsInvalidEnvelopeFields(t *testing.T) {
	store := NewMemoryRunEventStore()
	for _, event := range []RunEvent{
		{ID: "evt\nforged", TenantID: "t", UserID: "u", RunID: "r", Type: "assistant.delta"},
		{ID: strings.Repeat("x", agentruntime.MaxEventIDBytes+1), TenantID: "t", UserID: "u", RunID: "r", Type: "assistant.delta"},
		{ID: "evt-invalid-type", TenantID: "t", UserID: "u", RunID: "r", Type: "tool\ncall"},
	} {
		if _, err := store.Append(event); err == nil {
			t.Fatalf("invalid event was accepted: %#v", event)
		}
	}
	if events, err := store.ListAfter("t", "u", "r", 0, 10); err != nil || len(events) != 0 {
		t.Fatalf("invalid append mutated store: events=%#v err=%v", events, err)
	}
}

func TestMemoryRunEventStoreCloseIsIdempotentAndRejectsWrites(t *testing.T) {
	store := NewMemoryRunEventStore()
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Type: "run.started"}); !errors.Is(err, ErrServiceClosed) {
		t.Fatalf("Append after close=%v; want ErrServiceClosed", err)
	}
	if _, err := store.ListAfter("t", "u", "r", 0, 10); !errors.Is(err, ErrServiceClosed) {
		t.Fatalf("ListAfter after close=%v; want ErrServiceClosed", err)
	}
}

func TestServiceRuntimeMetricsCountsEventReplay(t *testing.T) {
	svc, _, _ := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	event := RunEvent{ID: "stable-event", TenantID: "tenant", UserID: "user", RunID: "run", Type: "checkpoint"}
	// Use the service's actual tenant/user scope so the event store can accept
	// it; the run need not exist for this low-level append contract.
	first, err := svc.AppendRunEvent(event)
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	second, err := svc.AppendRunEvent(event)
	if err != nil || second.ID != first.ID {
		t.Fatalf("replay append=%#v err=%v", second, err)
	}
	snapshot := svc.RuntimeMetrics().Snapshot()
	if snapshot.EventsEmitted != 2 || snapshot.EventsReplayed != 1 {
		t.Fatalf("event replay metrics=%#v", snapshot)
	}
}

func TestMemoryLifecycleTransactionValidatesBeforeMutation(t *testing.T) {
	store := NewMemoryStore()
	message := Message{ID: "msg-invalid", SessionID: "sess-invalid", TenantID: "tenant", UserID: "user", InstanceID: "instance", Role: MessageRoleUser}
	run := Run{ID: "run-invalid", TenantID: message.TenantID, UserID: message.UserID, InstanceID: message.InstanceID, SessionID: message.SessionID, UserMessageID: message.ID, Status: RunStatusRunning}
	err := store.SaveRunAdmissionWithEvents(message, run, []RunEvent{{RunID: run.ID, Payload: map[string]any{"bad": func() {}}}})
	if err == nil {
		t.Fatal("expected invalid event payload error")
	}
	if errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("unexpected idempotency error: %v", err)
	}
	if _, err := store.GetRun(run.TenantID, run.UserID, run.InstanceID, run.ID); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("run was mutated after rejected transaction: %v", err)
	}
	if messages, _ := store.ListMessages(message.SessionID); len(messages) != 0 {
		t.Fatalf("message was mutated after rejected transaction: %#v", messages)
	}
}

func TestListRunEventsForInstanceEnforcesRunScope(t *testing.T) {
	svc, principal, instance := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	_, run, _, err := svc.SendMessage(context.Background(), principal, instance.ID, SendMessageInput{Content: "hello"})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := svc.ListRunEventsForInstance(context.Background(), principal, "missing-instance", run.ID, 0, 10); err == nil {
		t.Fatal("expected wrong instance to be rejected")
	}
	events, err := svc.ListRunEventsForInstance(context.Background(), principal, instance.ID, run.ID, 0, 10)
	if err != nil || len(events) == 0 {
		t.Fatalf("scoped events=%#v err=%v", events, err)
	}
}

func TestRuntimeCallbacksFlowThroughSharedEventSink(t *testing.T) {
	svc, principal, instance := setupCaptureAgentService(t, callbackEventExecutor{})
	defer svc.Close()
	_, run, _, err := svc.SendMessage(context.Background(), principal, instance.ID, SendMessageInput{Content: "emit"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	events, err := svc.ListRunEventsForInstance(context.Background(), principal, instance.ID, run.ID, 0, 100)
	if err != nil {
		t.Fatalf("ListRunEventsForInstance: %v", err)
	}
	counts := map[string]int{}
	for _, event := range events {
		if event.SchemaVersion != RunEventSchemaVersion {
			t.Fatalf("event %s schema=%q; want %q", event.Type, event.SchemaVersion, RunEventSchemaVersion)
		}
		counts[event.Type]++
	}
	for _, typ := range []string{"assistant.delta", "tool.call", "tool.result", "run.completed"} {
		if counts[typ] != 1 {
			t.Fatalf("event %s count=%d; events=%#v", typ, counts[typ], events)
		}
	}
	if counts["run.started"] != 1 || counts["assistant.message"] != 1 {
		t.Fatalf("transaction lifecycle events missing or duplicated: %#v", events)
	}
	metrics := svc.RuntimeMetrics().Snapshot()
	if metrics.TurnsAdmitted != 1 || metrics.TurnsSucceeded != 1 || metrics.ActiveRuns != 0 {
		t.Fatalf("runtime lifecycle metrics=%#v", metrics)
	}
	if metrics.TokenDeltas != 1 || metrics.ToolCalls != 1 || metrics.EventsEmitted < 3 || metrics.FirstTokenSamples != 1 {
		t.Fatalf("runtime callback metrics=%#v", metrics)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Sequence <= events[i-1].Sequence {
			t.Fatalf("event sequence is not strictly increasing: %#v", events)
		}
	}
}

func TestRuntimeEventSinkPreservesProducerEventIDAndTimestamp(t *testing.T) {
	store := NewMemoryRunEventStore()
	svc := &Service{runEvents: store, metrics: agentruntime.NewRuntimeMetrics(0)}
	run := Run{TenantID: "tenant", UserID: "user", InstanceID: "instance", SessionID: "session", ID: "run"}
	occurred := time.Date(2026, 9, 1, 1, 2, 3, 456000000, time.FixedZone("test", 8*60*60))
	err := (serviceRuntimeEventSink{service: svc, run: run}).Emit(context.Background(), agentruntime.Event{
		SchemaVersion: agentruntime.ContractVersion,
		EventID:       "evt-producer-1",
		Type:          "assistant.delta",
		Scope:         agentruntime.Scope{TenantID: run.TenantID, UserID: run.UserID, RunID: run.ID},
		OccurredAt:    occurred,
		Payload:       map[string]any{"delta": "hello"},
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	events, err := store.ListAfter(run.TenantID, run.UserID, run.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%#v; want one event", events)
	}
	if events[0].ID != "evt-producer-1" {
		t.Fatalf("event id=%q; want producer id", events[0].ID)
	}
	if !events[0].OccurredAt.Equal(occurred.UTC()) {
		t.Fatalf("occurred_at=%s; want %s", events[0].OccurredAt, occurred.UTC())
	}
	if events[0].Sequence != 1 {
		t.Fatalf("sequence=%d; want store-assigned sequence 1", events[0].Sequence)
	}
}

func TestRuntimeEventSinkRejectsCrossScopeProducerEnvelope(t *testing.T) {
	store := NewMemoryRunEventStore()
	svc := &Service{runEvents: store, metrics: agentruntime.NewRuntimeMetrics(0)}
	run := Run{TenantID: "tenant", UserID: "user", InstanceID: "instance", SessionID: "session", ID: "run"}
	cases := []agentruntime.Event{
		{Type: "assistant.delta", RunID: "other-run", Scope: agentruntime.Scope{RunID: "other-run"}},
		{Type: "assistant.delta", Scope: agentruntime.Scope{TenantID: "other-tenant"}},
		{Type: "assistant.delta", Scope: agentruntime.Scope{UserID: "other-user"}},
		{Type: "assistant.delta", Scope: agentruntime.Scope{InstanceID: "other-instance"}},
		{Type: "assistant.delta", Scope: agentruntime.Scope{SessionID: "other-session"}},
	}
	for _, event := range cases {
		if err := (serviceRuntimeEventSink{service: svc, run: run}).Emit(context.Background(), event); err == nil {
			t.Fatalf("cross-scope event was accepted: %#v", event)
		}
	}
	if events, err := store.ListAfter(run.TenantID, run.UserID, run.ID, 0, 10); err != nil || len(events) != 0 {
		t.Fatalf("cross-scope events mutated outbox: events=%#v err=%v", events, err)
	}
}

func TestRuntimeEventSinkRejectsOversizedProducerEventID(t *testing.T) {
	store := NewMemoryRunEventStore()
	svc := &Service{runEvents: store, metrics: agentruntime.NewRuntimeMetrics(0)}
	run := Run{TenantID: "tenant", UserID: "user", InstanceID: "instance", SessionID: "session", ID: "run"}
	err := (serviceRuntimeEventSink{service: svc, run: run}).Emit(context.Background(), agentruntime.Event{
		EventID: strings.Repeat("x", 257), Type: "assistant.delta",
	})
	if err == nil || !strings.Contains(err.Error(), "event id") {
		t.Fatalf("oversized event id error=%v", err)
	}
	events, listErr := store.ListAfter(run.TenantID, run.UserID, run.ID, 0, 10)
	if listErr != nil {
		t.Fatalf("ListAfter: %v", listErr)
	}
	if len(events) != 0 {
		t.Fatalf("oversized event should not be persisted: %#v", events)
	}
}
