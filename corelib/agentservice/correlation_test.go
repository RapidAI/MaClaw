package agentservice

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestPostMessagePropagatesCorrelationAcrossRunEventsAndAudit(t *testing.T) {
	svc, principal, inst := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "correlation"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := agentruntime.WithCorrelationID(context.Background(), "req-correlation-1")
	ctx = agentruntime.WithTraceID(ctx, "4bf92f3577b34da6a3ce929d0e0e4736")
	ctx = agentruntime.WithSpanID(ctx, "00f067aa0ba902b7")
	run, _, err := svc.PostMessage(ctx, principal, inst.ID, sess.ID, PostMessageInput{Content: "hello"})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if run == nil || run.Metadata["request_id"] != "req-correlation-1" || run.Metadata["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || run.Metadata["span_id"] != "00f067aa0ba902b7" {
		t.Fatalf("run correlation metadata = %#v", run)
	}
	if run.Metadata["parent_span_id"] != "00f067aa0ba902b7" || run.Metadata["run_span_id"] == "" || run.Metadata["run_span_id"] == run.Metadata["span_id"] {
		t.Fatalf("run child span linkage = %#v", run.Metadata)
	}
	events, err := svc.ListRunEventsForInstance(context.Background(), principal, inst.ID, run.ID, 0, 100)
	if err != nil {
		t.Fatalf("ListRunEventsForInstance: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected lifecycle events, got %#v", events)
	}
	for _, event := range events {
		if got, _ := event.Payload["request_id"].(string); got != "req-correlation-1" || event.Payload["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || event.Payload["span_id"] != "00f067aa0ba902b7" {
			t.Fatalf("event %s correlation = %#v, want request id", event.Type, event.Payload)
		}
		if event.Payload["parent_span_id"] != "00f067aa0ba902b7" || event.Payload["run_span_id"] != run.Metadata["run_span_id"] {
			t.Fatalf("event %s child span linkage = %#v", event.Type, event.Payload)
		}
	}
	audits, err := svc.ListAuditEvents(context.Background(), ListAuditEventsInput{TenantID: principal.TenantID, UserID: principal.UserID, ResourceID: run.ID})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(audits) < 2 {
		t.Fatalf("expected run audit events, got %#v", audits)
	}
	for _, event := range audits {
		if strings.HasPrefix(event.Action, "run.") && (event.Metadata["request_id"] != "req-correlation-1" || event.Metadata["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || event.Metadata["span_id"] != "00f067aa0ba902b7") {
			t.Fatalf("audit %s correlation = %#v", event.Action, event.Metadata)
		}
		if strings.HasPrefix(event.Action, "run.") && event.Metadata["run_span_id"] != run.Metadata["run_span_id"] {
			t.Fatalf("audit %s child span linkage = %#v", event.Action, event.Metadata)
		}
	}
}

func TestCorrelationIDRejectsUnsafeValues(t *testing.T) {
	ctx := agentruntime.WithCorrelationID(context.Background(), "bad\nvalue")
	if got := agentruntime.CorrelationID(ctx); got != "" {
		t.Fatalf("unsafe correlation id accepted: %q", got)
	}
	ctx = agentruntime.WithCorrelationID(context.Background(), strings.Repeat("x", 129))
	if got := agentruntime.CorrelationID(ctx); got != "" {
		t.Fatalf("oversized correlation id accepted: %q", got)
	}
}
