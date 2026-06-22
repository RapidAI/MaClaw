package workflow

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// passthroughOwnerAuth is a test auth middleware that mirrors the production
// workflowUserAuth contract: it copies a test-supplied caller identity into the
// X-Owner-ID header that the handler reads. The caller identity is provided via
// the X-Test-Owner header so individual requests can vary the authenticated
// user without re-registering routes.
func passthroughOwnerAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if owner := r.Header.Get("X-Test-Owner"); owner != "" {
			r.Header.Set("X-Owner-ID", owner)
		}
		h(w, r)
	}
}

// newDecisionTestServer wires a DecisionAPI over an approval graph in single
// mode with the given approver and an instance parked at the approval node.
func newDecisionTestServer(t *testing.T, approverID string) (*http.ServeMux, *resumeTestMockInstanceStore) {
	t.Helper()

	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{approverID},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  map[string]interface{}{"requester_id": "initiator-1"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	decisionAPI := NewDecisionAPI(executor, instStore, wfStore)
	mux := http.NewServeMux()
	decisionAPI.RegisterRoutes(mux, passthroughOwnerAuth)
	return mux, instStore
}

// TestDecisionAPI_AuthorizedApproverAdvancesInstance verifies that a configured
// approver's decision routes into WorkflowExecutor.ResumeInstance and advances
// the instance (Requirement 2.1 — the decision entry point).
func TestDecisionAPI_AuthorizedApproverAdvancesInstance(t *testing.T) {
	mux, instStore := newDecisionTestServer(t, "ve-1")

	body, _ := json.Marshal(map[string]string{
		"decision":  "approve",
		"rationale": "looks good",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst-1/nodes/approval-1/decision", bytes.NewReader(body))
	req.Header.Set("X-Test-Owner", "ve-1") // authenticated as the configured approver
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Single-mode approve advances past the approval node to completion.
	if instStore.statusUpdate != InstanceCompleted {
		t.Errorf("expected instance to advance to %q, got %q", InstanceCompleted, instStore.statusUpdate)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["instance_id"] != "inst-1" || resp["node_id"] != "approval-1" {
		t.Errorf("unexpected response payload: %v", resp)
	}
}

// TestDecisionAPI_ResolvedApprovalRoleApproverAdvancesInstance verifies that
// the HTTP authorization boundary uses the same Hub approval-role resolution as
// the executor runtime path.
func TestDecisionAPI_ResolvedApprovalRoleApproverAdvancesInstance(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"role:function:finance:finance_approver"},
		Mode:         ModeSingle,
		TimeoutHours: 24,
	})
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}
	instStore := &resumeTestMockInstanceStore{
		instance: &WorkflowInstance{
			ID:            "inst-1",
			VersionID:     "ver-1",
			Status:        InstanceRunning,
			CurrentNodeID: "approval-1",
			InstanceData:  map[string]interface{}{"requester_id": "initiator-1"},
		},
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher, WithApprovalApproverResolver(mockApproverResolver{values: map[string][]string{
		"role:function:finance:finance_approver": {"machine-finance-1"},
	}}))

	decisionAPI := NewDecisionAPI(executor, instStore, wfStore)
	mux := http.NewServeMux()
	decisionAPI.RegisterRoutes(mux, passthroughOwnerAuth)

	body, _ := json.Marshal(map[string]string{"decision": "approve"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst-1/nodes/approval-1/decision", bytes.NewReader(body))
	req.Header.Set("X-Test-Owner", "machine-finance-1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", w.Code, w.Body.String())
	}
	if instStore.statusUpdate != InstanceCompleted {
		t.Errorf("expected instance to advance to %q, got %q", InstanceCompleted, instStore.statusUpdate)
	}
}

// TestDecisionAPI_NonApproverForbidden verifies that a caller who is not a
// configured approver for the node receives 403 and the decision is not routed
// into ResumeInstance (Requirement 2.1 — approver authorization).
func TestDecisionAPI_NonApproverForbidden(t *testing.T) {
	mux, instStore := newDecisionTestServer(t, "ve-1")

	body, _ := json.Marshal(map[string]string{"decision": "approve"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst-1/nodes/approval-1/decision", bytes.NewReader(body))
	req.Header.Set("X-Test-Owner", "ve-outsider") // not a configured approver
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d (body: %s)", w.Code, w.Body.String())
	}

	// The instance must not have advanced — no decision was routed.
	if instStore.statusUpdate != "" {
		t.Errorf("expected no status update for non-approver, got %q", instStore.statusUpdate)
	}
}

// TestDecisionAPI_InvalidDecisionRejected verifies that a malformed decision
// value (not approve/reject/escalate) is rejected at the HTTP boundary with 400
// rather than surfacing as a 500 from deep inside ResumeInstance's per-mode
// logic, and that the instance is not advanced.
func TestDecisionAPI_InvalidDecisionRejected(t *testing.T) {
	mux, instStore := newDecisionTestServer(t, "ve-1")

	body, _ := json.Marshal(map[string]string{"decision": "maybe"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst-1/nodes/approval-1/decision", bytes.NewReader(body))
	req.Header.Set("X-Test-Owner", "ve-1") // authenticated as the configured approver
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid decision, got %d (body: %s)", w.Code, w.Body.String())
	}

	// The instance must not have advanced — no decision was routed.
	if instStore.statusUpdate != "" {
		t.Errorf("expected no status update for invalid decision, got %q", instStore.statusUpdate)
	}
}
