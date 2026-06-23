package workflow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateWorkflowGraphDetailed_EmptyGraph(t *testing.T) {
	graph := WorkflowGraph{}
	errs := ValidateWorkflowGraphDetailed(graph)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs[0].Message != "workflow graph has no nodes" {
		t.Errorf("unexpected message: %s", errs[0].Message)
	}
}

func TestValidateWorkflowGraphDetailed_NoTriggerNode(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}, Config: approvalConfigRaw("role:function:finance:finance_approver")},
		},
	}
	errs := ValidateWorkflowGraphDetailed(graph)
	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}
	found := false
	for _, e := range errs {
		if e.Message == "workflow must have exactly one Trigger_Node as the entry point" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'exactly one Trigger_Node' error not found")
	}
}

func TestValidateWorkflowGraphDetailed_MultipleTriggerNodes(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Trigger 1", Position: Position{X: 0, Y: 0}},
			{ID: "t2", Type: NodeTrigger, Label: "Trigger 2", Position: Position{X: 200, Y: 0}},
			{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "n1"},
			{ID: "e2", SourceID: "t2", TargetID: "n1"},
		},
	}
	errs := ValidateWorkflowGraphDetailed(graph)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for multiple triggers")
	}
	found := false
	for _, e := range errs {
		if e.NodeID == "t2" && e.NodeLabel == "Trigger 2" {
			found = true
			if e.Position == nil || e.Position.X != 200 {
				t.Errorf("expected position X=200, got %v", e.Position)
			}
		}
	}
	if !found {
		t.Error("expected error for second trigger node 't2' not found")
	}
}

func TestValidateWorkflowGraphDetailed_TriggerNodeHasIncomingEdge(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start", Position: Position{X: 0, Y: 0}},
			{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "n1"},
			{ID: "e2", SourceID: "n1", TargetID: "t1"}, // Invalid: edge targeting trigger
		},
	}
	errs := ValidateWorkflowGraphDetailed(graph)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for trigger with incoming edge")
	}
	found := false
	for _, e := range errs {
		if e.NodeID == "t1" && e.Message == "Trigger_Node cannot have incoming edges; it can only be a start node" {
			found = true
			if e.Position == nil || e.Position.X != 0 {
				t.Errorf("expected position X=0, got %v", e.Position)
			}
		}
	}
	if !found {
		t.Error("expected 'Trigger_Node cannot have incoming edges' error not found")
	}
}

func TestValidateWorkflowGraphDetailed_DisconnectedNodes(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start", Position: Position{X: 0, Y: 0}},
			{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}},
			{ID: "n2", Type: NodeAction, Label: "Orphan Action", Position: Position{X: 300, Y: 300}},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "n1"},
			// n2 has no incoming edge from the connected graph
		},
	}
	errs := ValidateWorkflowGraphDetailed(graph)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for disconnected node")
	}
	found := false
	for _, e := range errs {
		if e.NodeID == "n2" && e.NodeLabel == "Orphan Action" {
			found = true
			if e.Position == nil || e.Position.X != 300 {
				t.Errorf("expected position X=300, got %v", e.Position)
			}
		}
	}
	if !found {
		t.Error("expected disconnected node error for 'n2' not found")
	}
}

func TestValidateWorkflowGraphDetailed_ValidGraph(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start", Position: Position{X: 0, Y: 0}},
			{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}, Config: approvalConfigRaw("role:function:finance:finance_approver")},
			{ID: "n2", Type: NodeAction, Label: "Complete", Position: Position{X: 200, Y: 200}},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "n1"},
			{ID: "e2", SourceID: "n1", TargetID: "n2"},
		},
	}
	errs := ValidateWorkflowGraphDetailed(graph)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid graph, got %d: %v", len(errs), errs)
	}
}

func TestValidateWorkflowGraphDetailed_ApprovalWithoutApprover(t *testing.T) {
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start", Position: Position{X: 0, Y: 0}},
			{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}, Config: approvalConfigRaw()},
		},
		Edges: []WorkflowEdge{{ID: "e1", SourceID: "t1", TargetID: "n1"}},
	}
	errs := ValidateWorkflowGraphDetailed(graph)
	if len(errs) == 0 {
		t.Fatal("expected validation error for approval without approver")
	}
	if errs[0].NodeID != "n1" || errs[0].Message != "Approval node must have at least one approver or approval role" {
		t.Fatalf("unexpected error: %+v", errs[0])
	}
}

func TestValidateWorkflowGraphDetailed_MultipleErrors(t *testing.T) {
	// Graph with multiple issues: two triggers, an incoming edge to a trigger, and a disconnected node
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Trigger A", Position: Position{X: 0, Y: 0}},
			{ID: "t2", Type: NodeTrigger, Label: "Trigger B", Position: Position{X: 200, Y: 0}},
			{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}},
			{ID: "n2", Type: NodeAction, Label: "Orphan", Position: Position{X: 400, Y: 400}},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "t1", TargetID: "n1"},
			{ID: "e2", SourceID: "n1", TargetID: "t1"}, // Invalid: targets trigger
		},
	}
	errs := ValidateWorkflowGraphDetailed(graph)
	// Should have: multiple triggers error, incoming edge to trigger error
	// (disconnected node check is skipped when there's not exactly 1 trigger)
	if len(errs) < 2 {
		t.Errorf("expected at least 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateWorkflowGraphDetailed_SingleNodeTrigger(t *testing.T) {
	// A graph with only a trigger node — valid (no disconnected nodes since all are reachable)
	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "t1", Type: NodeTrigger, Label: "Start", Position: Position{X: 0, Y: 0}},
		},
	}
	errs := ValidateWorkflowGraphDetailed(graph)
	if len(errs) != 0 {
		t.Errorf("expected no errors for single trigger node, got %d: %v", len(errs), errs)
	}
}

// --- API endpoint tests ---

func TestValidateVersionEndpoint_ValidGraph(t *testing.T) {
	_, mux, store := setupTestAPI()

	// Create a workflow and version with a valid graph
	store.workflows["wf-1"] = &WorkflowDefinition{ID: "wf-1", OwnerID: "user-001", Name: "Test"}
	store.versions["ver-1"] = &WorkflowVersion{
		ID:         "ver-1",
		WorkflowID: "wf-1",
		Status:     VersionDraft,
		Graph: WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "t1", Type: NodeTrigger, Label: "Start", Position: Position{X: 0, Y: 0}},
				{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}, Config: approvalConfigRaw("role:function:finance:finance_approver")},
			},
			Edges: []WorkflowEdge{
				{ID: "e1", SourceID: "t1", TargetID: "n1"},
			},
		},
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-1/versions/ver-1/validate", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["valid"] != true {
		t.Errorf("expected valid=true, got %v", resp["valid"])
	}
	errs, ok := resp["errors"].([]any)
	if !ok || len(errs) != 0 {
		t.Errorf("expected empty errors, got %v", resp["errors"])
	}
}

func TestValidateVersionEndpoint_InvalidGraph(t *testing.T) {
	_, mux, store := setupTestAPI()

	// Create a workflow and version with an invalid graph (no trigger, disconnected node)
	store.workflows["wf-2"] = &WorkflowDefinition{ID: "wf-2", OwnerID: "user-001", Name: "Bad"}
	store.versions["ver-2"] = &WorkflowVersion{
		ID:         "ver-2",
		WorkflowID: "wf-2",
		Status:     VersionDraft,
		Graph: WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}},
				{ID: "n2", Type: NodeAction, Label: "Action", Position: Position{X: 200, Y: 200}},
			},
		},
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-2/versions/ver-2/validate", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["valid"] != false {
		t.Errorf("expected valid=false, got %v", resp["valid"])
	}
	errs, ok := resp["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Errorf("expected non-empty errors, got %v", resp["errors"])
	}
}

func TestValidateVersionEndpoint_OwnerIsolation(t *testing.T) {
	_, mux, store := setupTestAPI()

	// Create a workflow owned by a different user
	store.workflows["wf-3"] = &WorkflowDefinition{ID: "wf-3", OwnerID: "other-user", Name: "Other"}
	store.versions["ver-3"] = &WorkflowVersion{
		ID:         "ver-3",
		WorkflowID: "wf-3",
		Status:     VersionDraft,
		Graph:      WorkflowGraph{},
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-3/versions/ver-3/validate", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for owner isolation, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestValidateVersionEndpoint_SemanticEdgeErrors(t *testing.T) {
	_, mux, store := setupTestAPI()

	// Graph with trigger node having incoming edge
	store.workflows["wf-4"] = &WorkflowDefinition{ID: "wf-4", OwnerID: "user-001", Name: "Semantic"}
	store.versions["ver-4"] = &WorkflowVersion{
		ID:         "ver-4",
		WorkflowID: "wf-4",
		Status:     VersionDraft,
		Graph: WorkflowGraph{
			Nodes: []WorkflowNode{
				{ID: "t1", Type: NodeTrigger, Label: "Start", Position: Position{X: 0, Y: 0}},
				{ID: "n1", Type: NodeApproval, Label: "Approve", Position: Position{X: 100, Y: 100}},
			},
			Edges: []WorkflowEdge{
				{ID: "e1", SourceID: "t1", TargetID: "n1"},
				{ID: "e2", SourceID: "n1", TargetID: "t1"}, // Invalid
			},
		},
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-4/versions/ver-4/validate", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["valid"] != false {
		t.Errorf("expected valid=false, got %v", resp["valid"])
	}

	errs, ok := resp["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatal("expected non-empty errors")
	}

	// Check that the error includes node label and position
	firstErr, ok := errs[0].(map[string]any)
	if !ok {
		t.Fatal("expected error to be a map")
	}
	if firstErr["node_id"] != "t1" {
		t.Errorf("expected node_id=t1, got %v", firstErr["node_id"])
	}
	if firstErr["node_label"] != "Start" {
		t.Errorf("expected node_label=Start, got %v", firstErr["node_label"])
	}
	pos, ok := firstErr["position"].(map[string]any)
	if !ok {
		t.Fatal("expected position to be a map")
	}
	if pos["x"] != float64(0) || pos["y"] != float64(0) {
		t.Errorf("expected position {0,0}, got %v", pos)
	}
}

func TestValidateVersionEndpoint_NotFound(t *testing.T) {
	_, mux, _ := setupTestAPI()

	req := httptest.NewRequest("POST", "/api/v1/workflows/nonexistent/versions/v1/validate", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestValidateVersionEndpoint_VersionNotBelongToWorkflow(t *testing.T) {
	_, mux, store := setupTestAPI()

	store.workflows["wf-5"] = &WorkflowDefinition{ID: "wf-5", OwnerID: "user-001", Name: "WF5"}
	store.versions["ver-5"] = &WorkflowVersion{
		ID:         "ver-5",
		WorkflowID: "wf-other", // Belongs to a different workflow
		Status:     VersionDraft,
		Graph:      WorkflowGraph{},
	}

	req := httptest.NewRequest("POST", "/api/v1/workflows/wf-5/versions/ver-5/validate", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}
