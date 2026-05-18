package workflow

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTriggerFromMarket_Success(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger_1", Type: NodeTrigger, Label: "Start", Config: json.RawMessage(`{}`)},
		},
		Edges: []WorkflowEdge{},
	}

	store := &mockWorkflowStore{
		publishedVersion: &WorkflowVersion{
			ID:            "ver_1",
			WorkflowID:    "wf_1",
			VersionNumber: "1.0.0",
			Status:        VersionPublished,
			Graph:         graph,
		},
	}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(store, instStore, auditStore, dispatcher)

	inst, err := executor.TriggerFromMarket(context.Background(), "wf_1", "user_42", `{"amount":500}`)
	if err != nil {
		t.Fatalf("TriggerFromMarket failed: %v", err)
	}
	if inst == nil {
		t.Fatal("expected non-nil instance")
	}
	if inst.WorkflowID != "wf_1" {
		t.Errorf("expected WorkflowID=wf_1, got %s", inst.WorkflowID)
	}
	// With only a trigger node and no outgoing edges, the instance completes immediately.
	if inst.Status != InstanceCompleted {
		t.Errorf("expected status=completed (trigger-only workflow completes immediately), got %s", inst.Status)
	}

	// Verify requester_id was injected into instance data.
	requesterID, ok := inst.InstanceData["requester_id"].(string)
	if !ok || requesterID != "user_42" {
		t.Errorf("expected requester_id=user_42 in instance data, got %v", inst.InstanceData["requester_id"])
	}

	// Verify original trigger data was preserved.
	amount, ok := inst.InstanceData["amount"].(float64)
	if !ok || amount != 500 {
		t.Errorf("expected amount=500 in instance data, got %v", inst.InstanceData["amount"])
	}
}

func TestTriggerFromMarket_NotPublished(t *testing.T) {
	store := &mockWorkflowStore{
		publishedVersion: nil, // no published version
	}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(store, instStore, auditStore, dispatcher)

	_, err := executor.TriggerFromMarket(context.Background(), "wf_1", "user_42", `{}`)
	if err != ErrWorkflowNotPublished {
		t.Errorf("expected ErrWorkflowNotPublished, got %v", err)
	}
}

func TestTriggerFromMarket_MissingUserID(t *testing.T) {
	store := &mockWorkflowStore{}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(store, instStore, auditStore, dispatcher)

	_, err := executor.TriggerFromMarket(context.Background(), "wf_1", "", `{}`)
	if err != ErrMissingUserID {
		t.Errorf("expected ErrMissingUserID, got %v", err)
	}
}

func TestTriggerFromMarket_UserIsolation(t *testing.T) {
	// Verify that different users triggering the same workflow get separate instances
	// with their own requester_id.
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "trigger_1", Type: NodeTrigger, Label: "Start", Config: json.RawMessage(`{}`)},
		},
		Edges: []WorkflowEdge{},
	}

	store := &mockWorkflowStore{
		publishedVersion: &WorkflowVersion{
			ID:            "ver_1",
			WorkflowID:    "wf_1",
			VersionNumber: "1.0.0",
			Status:        VersionPublished,
			Graph:         graph,
		},
	}
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	dispatcher := &mockDispatcher{}

	executor := NewWorkflowExecutor(store, instStore, auditStore, dispatcher)

	// User A triggers the workflow.
	instA, err := executor.TriggerFromMarket(context.Background(), "wf_1", "user_A", `{}`)
	if err != nil {
		t.Fatalf("TriggerFromMarket for user_A failed: %v", err)
	}

	// User B triggers the same workflow.
	instB, err := executor.TriggerFromMarket(context.Background(), "wf_1", "user_B", `{}`)
	if err != nil {
		t.Fatalf("TriggerFromMarket for user_B failed: %v", err)
	}

	// Each instance should have its own requester_id.
	reqA, _ := instA.InstanceData["requester_id"].(string)
	reqB, _ := instB.InstanceData["requester_id"].(string)

	if reqA != "user_A" {
		t.Errorf("expected instance A requester_id=user_A, got %s", reqA)
	}
	if reqB != "user_B" {
		t.Errorf("expected instance B requester_id=user_B, got %s", reqB)
	}

	// Instances should have different IDs.
	if instA.ID == instB.ID {
		t.Error("expected different instance IDs for different users")
	}
}

func TestEnrichTriggerDataWithUser(t *testing.T) {
	tests := []struct {
		name        string
		triggerData string
		userID      string
		wantKey     string
		wantValue   string
	}{
		{
			name:        "empty trigger data",
			triggerData: "",
			userID:      "user_1",
			wantKey:     "requester_id",
			wantValue:   "user_1",
		},
		{
			name:        "empty JSON object",
			triggerData: "{}",
			userID:      "user_2",
			wantKey:     "requester_id",
			wantValue:   "user_2",
		},
		{
			name:        "existing JSON object",
			triggerData: `{"amount":1000,"department":"engineering"}`,
			userID:      "user_3",
			wantKey:     "requester_id",
			wantValue:   "user_3",
		},
		{
			name:        "non-object JSON",
			triggerData: `"just a string"`,
			userID:      "user_4",
			wantKey:     "requester_id",
			wantValue:   "user_4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := enrichTriggerDataWithUser(tt.triggerData, tt.userID)

			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
			}

			val, ok := parsed[tt.wantKey].(string)
			if !ok || val != tt.wantValue {
				t.Errorf("expected %s=%s, got %v", tt.wantKey, tt.wantValue, parsed[tt.wantKey])
			}
		})
	}
}

func TestEnrichTriggerDataWithUser_PreservesExistingFields(t *testing.T) {
	triggerData := `{"amount":1000,"department":"engineering","priority":"high"}`
	result := enrichTriggerDataWithUser(triggerData, "user_5")

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\nresult: %s", err, result)
	}

	// Check requester_id was added.
	if parsed["requester_id"] != "user_5" {
		t.Errorf("expected requester_id=user_5, got %v", parsed["requester_id"])
	}

	// Check existing fields are preserved.
	if parsed["amount"] != float64(1000) {
		t.Errorf("expected amount=1000, got %v", parsed["amount"])
	}
	if parsed["department"] != "engineering" {
		t.Errorf("expected department=engineering, got %v", parsed["department"])
	}
	if parsed["priority"] != "high" {
		t.Errorf("expected priority=high, got %v", parsed["priority"])
	}
}
