package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

type captureMachineSender struct {
	messages []map[string]any
	ids      []string
}

func (c *captureMachineSender) SendToMachine(machineID string, msg any) error {
	c.ids = append(c.ids, machineID)
	if m, ok := msg.(map[string]any); ok {
		c.messages = append(c.messages, m)
	}
	return nil
}

type stubInstanceStore struct {
	inst *workflow.WorkflowInstance
}

func (s *stubInstanceStore) Create(context.Context, *workflow.WorkflowInstance) error { return nil }
func (s *stubInstanceStore) Get(context.Context, string) (*workflow.WorkflowInstance, error) {
	return s.inst, nil
}
func (s *stubInstanceStore) UpdateStatus(context.Context, string, workflow.InstanceStatus) error {
	return nil
}
func (s *stubInstanceStore) UpdateCurrentNode(context.Context, string, string) error { return nil }
func (s *stubInstanceStore) UpdateInstanceData(context.Context, string, map[string]interface{}) error {
	return nil
}
func (s *stubInstanceStore) CreateNodeExecution(context.Context, *workflow.NodeExecution) error {
	return nil
}
func (s *stubInstanceStore) UpdateNodeExecution(context.Context, string, workflow.NodeStatus, json.RawMessage, string) error {
	return nil
}
func (s *stubInstanceStore) GetPendingApprovals(context.Context, string) ([]workflow.NodeExecution, error) {
	return nil, nil
}
func (s *stubInstanceStore) QueryMyInitiated(context.Context, string, workflow.DirectoryFilter) ([]workflow.DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *stubInstanceStore) QueryPendingMyAction(context.Context, string, workflow.DirectoryFilter) ([]workflow.DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *stubInstanceStore) QueryPendingMyConfirmation(context.Context, string, workflow.DirectoryFilter) ([]workflow.DirectoryItem, int, error) {
	return nil, 0, nil
}
func (s *stubInstanceStore) QueryCompleted(context.Context, string, workflow.DirectoryFilter) ([]workflow.DirectoryItem, int, error) {
	return nil, 0, nil
}

func TestHubWorkflowParticipantNotifier_NotifyInitiator(t *testing.T) {
	sender := &captureMachineSender{}
	store := &stubInstanceStore{inst: &workflow.WorkflowInstance{
		ID:            "inst-1",
		WorkflowID:    "wf-1",
		Status:        workflow.InstanceBlocked,
		CurrentNodeID: "approval-1",
		InstanceData: map[string]interface{}{
			"requester_machine_id": "machine-alice",
			"workflow_name":        "Leave",
			"blocked_reason":       "timeout",
		},
		CreatedAt: time.Now().UTC(),
	}}
	n := NewHubWorkflowParticipantNotifier(sender, store, nil, nil)
	if err := n.NotifyInitiator(context.Background(), "inst-1", "Approval node blocked: timeout", "no fallback"); err != nil {
		t.Fatal(err)
	}
	if len(sender.ids) != 1 || sender.ids[0] != "machine-alice" {
		t.Fatalf("recipients=%v", sender.ids)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("messages=%d", len(sender.messages))
	}
	if sender.messages[0]["type"] != workflowStatusWireType {
		t.Fatalf("type=%v", sender.messages[0]["type"])
	}
	payload, _ := sender.messages[0]["payload"].(map[string]any)
	// reason contains "timeout" → classified as escalation + overdue urgency.
	if payload["event"] != "escalation" || payload["instance_id"] != "inst-1" || payload["urgency"] != "overdue" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestHubWorkflowParticipantNotifier_IncludesEscalationAttemptsAndExhausted(t *testing.T) {
	sender := &captureMachineSender{}
	store := &stubInstanceStore{inst: &workflow.WorkflowInstance{
		ID:            "inst-esc",
		WorkflowID:    "wf-1",
		Status:        workflow.InstanceRunning,
		CurrentNodeID: "approval-1",
		InstanceData: map[string]interface{}{
			"requester_machine_id": "machine-bob",
			"escalation_pending":   true,
			"escalation_approvers": []string{"ve-a"},
			"escalation_attempts": map[string]interface{}{
				"ve-a": float64(3),
			},
			"escalation_exhausted_approvers": []string{"ve-b"},
		},
	}}
	n := NewHubWorkflowParticipantNotifier(sender, store, nil, nil)
	if err := n.NotifyInitiator(context.Background(), "inst-esc",
		"escalation failed for approver ve-b (approval still satisfiable)", "node=approval-1"); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("messages=%d", len(sender.messages))
	}
	payload, _ := sender.messages[0]["payload"].(map[string]any)
	if payload["escalation_pending"] != true {
		t.Fatalf("pending missing: %#v", payload)
	}
	approvers, _ := payload["escalation_approvers"].([]string)
	if len(approvers) != 1 || approvers[0] != "ve-a" {
		// JSON may round-trip as []any after cast — accept both
		if raw, ok := payload["escalation_approvers"].([]any); ok && len(raw) == 1 && raw[0] == "ve-a" {
			// ok
		} else if len(approvers) != 1 || approvers[0] != "ve-a" {
			t.Fatalf("approvers=%#v", payload["escalation_approvers"])
		}
	}
	exhausted := stringSliceFromPayloadAny(payload["escalation_exhausted_approvers"])
	if len(exhausted) != 1 || exhausted[0] != "ve-b" {
		t.Fatalf("exhausted=%v", exhausted)
	}
	att := escalationAttemptsFromPayloadAny(payload["escalation_attempts"])
	if att["ve-a"] != 3 {
		t.Fatalf("attempts=%v", att)
	}
}

func TestHubWorkflowParticipantNotifier_NoRecipientOK(t *testing.T) {
	sender := &captureMachineSender{}
	store := &stubInstanceStore{inst: &workflow.WorkflowInstance{
		ID: "inst-2", InstanceData: map[string]interface{}{},
	}}
	n := NewHubWorkflowParticipantNotifier(sender, store, nil, nil)
	if err := n.NotifyInitiator(context.Background(), "inst-2", "blocked", "x"); err != nil {
		t.Fatal(err)
	}
	if len(sender.ids) != 0 {
		t.Fatalf("expected no delivery, got %v", sender.ids)
	}
}

func TestHubWorkflowParticipantNotifier_BroadcastsMultipleMachines(t *testing.T) {
	sender := &captureMachineSender{}
	store := &stubInstanceStore{inst: &workflow.WorkflowInstance{
		ID: "inst-3",
		InstanceData: map[string]interface{}{
			"requester_machine_id": "machine-a",
			// second id via direct field array is not standard; use two keys
			"initiator_machine_id": "machine-b",
		},
	}}
	n := NewHubWorkflowParticipantNotifier(sender, store, nil, nil)
	if err := n.NotifyInitiator(context.Background(), "inst-3", "blocked", "x"); err != nil {
		t.Fatal(err)
	}
	if len(sender.ids) != 2 {
		t.Fatalf("expected 2 machines, got %v", sender.ids)
	}
	seen := map[string]bool{}
	for _, id := range sender.ids {
		seen[id] = true
	}
	if !seen["machine-a"] || !seen["machine-b"] {
		t.Fatalf("ids=%v", sender.ids)
	}
}

func TestClassifyWorkflowStatusEvent(t *testing.T) {
	event, status, urgency := classifyWorkflowStatusEvent("Approval node blocked: timeout", "fallback timed out", nil)
	if event != "escalation" || status != "blocked" || urgency != "overdue" {
		t.Fatalf("timeout classify: event=%s status=%s urgency=%s", event, status, urgency)
	}
	event, status, urgency = classifyWorkflowStatusEvent("approval node blocked", "no fallback", &workflow.WorkflowInstance{
		InstanceData: map[string]interface{}{"blocked_reason": "unavailable"},
	})
	if event != "blocked" || urgency != "critical" {
		t.Fatalf("unavailable classify: event=%s urgency=%s", event, urgency)
	}
	event, status, urgency = classifyWorkflowStatusEvent("generic", "x", nil)
	if event != "blocked" || status != "blocked" || urgency != "attention" {
		t.Fatalf("default classify: event=%s status=%s urgency=%s", event, status, urgency)
	}
}

func TestHubWorkflowParticipantNotifier_TimeoutClassifiesEscalation(t *testing.T) {
	sender := &captureMachineSender{}
	store := &stubInstanceStore{inst: &workflow.WorkflowInstance{
		ID: "inst-timeout",
		InstanceData: map[string]interface{}{
			"requester_machine_id": "machine-t",
			"blocked_reason":       "timeout",
		},
	}}
	n := NewHubWorkflowParticipantNotifier(sender, store, nil, nil)
	if err := n.NotifyInitiator(context.Background(), "inst-timeout", "Approval node 'mgr' is blocked: timeout", "fallback timed out"); err != nil {
		t.Fatal(err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("messages=%d", len(sender.messages))
	}
	payload, _ := sender.messages[0]["payload"].(map[string]any)
	if payload["event"] != "escalation" || payload["urgency"] != "overdue" {
		t.Fatalf("payload=%#v", payload)
	}
}
