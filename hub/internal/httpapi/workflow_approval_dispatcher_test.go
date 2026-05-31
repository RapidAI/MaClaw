package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// capturingMachineSender records every SendToMachine call so a test can assert
// real delivery (the post-fix behavior the noop dispatcher could never provide).
type capturingMachineSender struct {
	err   error
	calls []capturedSend
}

type capturedSend struct {
	machineID string
	msg       map[string]any
}

func (s *capturingMachineSender) SendToMachine(machineID string, msg any) error {
	if s.err != nil {
		return s.err
	}
	typed, _ := msg.(map[string]any)
	s.calls = append(s.calls, capturedSend{machineID: machineID, msg: typed})
	return nil
}

func sampleApprovalRequest() *workflow.ApprovalRequest {
	return &workflow.ApprovalRequest{
		ID:           "req-1",
		InstanceID:   "inst-1",
		NodeID:       "approval-1",
		WorkflowName: "Leave Request",
		Title:        "Approve leave",
		Summary:      "3 days annual leave",
		RequesterID:  "u1",
		CreatedAt:    time.Now().UTC(),
	}
}

// decodeApprovalEnvelope extracts the GroupEnvelope from a captured machine
// message, following the same payload.envelope wrapping the dispatcher uses.
func decodeApprovalEnvelope(t *testing.T, msg map[string]any) corea2a.GroupEnvelope {
	t.Helper()
	payload, ok := msg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("message payload is not a map: %#v", msg["payload"])
	}
	raw, err := json.Marshal(payload["envelope"])
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var env corea2a.GroupEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}

func TestHubApprovalDispatcher_Dispatch_DeliversEnvelope(t *testing.T) {
	sender := &capturingMachineSender{}
	d := NewHubApprovalDispatcher(sender)

	if err := d.Dispatch(context.Background(), sampleApprovalRequest(), "ve-1"); err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}

	if len(sender.calls) != 1 {
		t.Fatalf("expected exactly 1 delivery, got %d", len(sender.calls))
	}
	call := sender.calls[0]
	if call.machineID != "ve-1" {
		t.Fatalf("expected delivery to ve-1, got %q", call.machineID)
	}
	if call.msg["type"] != approvalRequestWireType {
		t.Fatalf("expected wire type %q, got %v", approvalRequestWireType, call.msg["type"])
	}

	env := decodeApprovalEnvelope(t, call.msg)
	if env.Type != corea2a.GroupMessageApprovalRequest {
		t.Fatalf("expected approval_request envelope, got %q", env.Type)
	}
	if err := env.ValidateCurrentHub(); err != nil {
		t.Fatalf("delivered envelope is invalid: %v", err)
	}
	if len(env.ToIDs) != 1 || env.ToIDs[0] != "ve-1" {
		t.Fatalf("expected ToIDs=[ve-1], got %#v", env.ToIDs)
	}

	var got workflow.ApprovalRequest
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatalf("unmarshal approval request payload: %v", err)
	}
	if got.ID != "req-1" || got.Title != "Approve leave" {
		t.Fatalf("payload not preserved: %#v", got)
	}
}

func TestHubApprovalDispatcher_DispatchFallback_AnnotatesReason(t *testing.T) {
	sender := &capturingMachineSender{}
	d := NewHubApprovalDispatcher(sender)

	if err := d.DispatchFallback(context.Background(), sampleApprovalRequest(), "fallback-ve", "primary timed out"); err != nil {
		t.Fatalf("DispatchFallback returned error: %v", err)
	}

	if len(sender.calls) != 1 {
		t.Fatalf("expected exactly 1 delivery, got %d", len(sender.calls))
	}
	call := sender.calls[0]
	if call.machineID != "fallback-ve" {
		t.Fatalf("expected delivery to fallback-ve, got %q", call.machineID)
	}
	payload, ok := call.msg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is not a map: %#v", call.msg["payload"])
	}
	if payload["is_fallback"] != true {
		t.Fatalf("expected is_fallback=true, got %v", payload["is_fallback"])
	}
	if reason, _ := payload["fallback_reason"].(string); !strings.Contains(reason, "primary timed out") {
		t.Fatalf("expected fallback reason recorded, got %q", reason)
	}
}

func TestHubApprovalDispatcher_Dispatch_EmptyApproverRejected(t *testing.T) {
	sender := &capturingMachineSender{}
	d := NewHubApprovalDispatcher(sender)

	if err := d.Dispatch(context.Background(), sampleApprovalRequest(), "  "); err == nil {
		t.Fatal("expected error for empty approver id, got nil")
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected no delivery for empty approver, got %d", len(sender.calls))
	}
}

func TestHubApprovalDispatcher_Dispatch_PropagatesSenderError(t *testing.T) {
	sender := &capturingMachineSender{err: errors.New("machine offline")}
	d := NewHubApprovalDispatcher(sender)

	if err := d.Dispatch(context.Background(), sampleApprovalRequest(), "ve-1"); err == nil {
		t.Fatal("expected sender error to propagate, got nil")
	}
}

func TestHubApprovalDispatcher_Dispatch_ValidatesPayload(t *testing.T) {
	sender := &capturingMachineSender{}
	d := NewHubApprovalDispatcher(sender)

	req := sampleApprovalRequest()
	req.Title = strings.Repeat("x", workflow.MaxTitleLength+1) // exceeds limit

	if err := d.Dispatch(context.Background(), req, "ve-1"); err == nil {
		t.Fatal("expected validation error for oversized title, got nil")
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected no delivery when validation fails, got %d", len(sender.calls))
	}
}
