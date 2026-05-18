package workflow

import (
	"context"
	"encoding/json"
	"testing"
)

// --- Condition Branch Evaluation Tests (Task 8.3) ---

func buildConditionBranchGraph(config ConditionBranchConfig, targetNodes ...WorkflowNode) (WorkflowGraph, *WorkflowNode) {
	configJSON, _ := json.Marshal(config)
	condNode := WorkflowNode{
		ID:     "cond-1",
		Type:   NodeConditionBranch,
		Label:  "Branch",
		Config: configJSON,
	}
	nodes := []WorkflowNode{
		{ID: "trigger-1", Type: NodeTrigger, Label: "Start"},
		condNode,
	}
	nodes = append(nodes, targetNodes...)

	edges := []WorkflowEdge{
		{ID: "e1", SourceID: "trigger-1", TargetID: "cond-1"},
	}

	graph := WorkflowGraph{Nodes: nodes, Edges: edges}
	return graph, &graph.Nodes[1] // return the condition node
}

func newTestExecutor() (*WorkflowExecutor, *mockInstanceStore, *mockAuditStore) {
	instStore := &mockInstanceStore{}
	auditStore := &mockAuditStore{}
	executor := NewWorkflowExecutor(&mockWorkflowStore{}, instStore, auditStore, &mockDispatcher{})
	return executor, instStore, auditStore
}

func TestConditionBranch_RoutesToFirstMatchingBranch(t *testing.T) {
	config := ConditionBranchConfig{
		Branches: []BranchCondition{
			{TargetNodeID: "action-high", Expression: ConditionExpr{Field: "amount", Operator: "greater_than", Value: float64(10000)}, Priority: 1},
			{TargetNodeID: "action-medium", Expression: ConditionExpr{Field: "amount", Operator: "greater_than", Value: float64(1000)}, Priority: 2},
			{TargetNodeID: "action-low", Expression: ConditionExpr{Field: "amount", Operator: "less_than", Value: float64(1000)}, Priority: 3},
		},
	}

	graph, condNode := buildConditionBranchGraph(config,
		WorkflowNode{ID: "action-high", Type: NodeAction, Label: "High"},
		WorkflowNode{ID: "action-medium", Type: NodeAction, Label: "Medium"},
		WorkflowNode{ID: "action-low", Type: NodeAction, Label: "Low"},
	)

	executor, instStore, _ := newTestExecutor()
	inst := &WorkflowInstance{
		ID:           "inst-1",
		InstanceData: map[string]interface{}{"amount": float64(5000)},
	}
	instStore.createdInstance = inst

	err := executor.executeConditionBranchNode(context.Background(), inst, condNode, &graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should route to action-medium (amount 5000 > 1000, first match by priority)
	if inst.CurrentNodeID != "action-medium" {
		t.Errorf("CurrentNodeID = %q, want %q", inst.CurrentNodeID, "action-medium")
	}
}

func TestConditionBranch_PriorityOrder(t *testing.T) {
	// Both branches match, but lower priority number wins
	config := ConditionBranchConfig{
		Branches: []BranchCondition{
			{TargetNodeID: "action-b", Expression: ConditionExpr{Field: "status", Operator: "equals", Value: "active"}, Priority: 10},
			{TargetNodeID: "action-a", Expression: ConditionExpr{Field: "status", Operator: "equals", Value: "active"}, Priority: 1},
		},
	}

	graph, condNode := buildConditionBranchGraph(config,
		WorkflowNode{ID: "action-a", Type: NodeAction, Label: "A"},
		WorkflowNode{ID: "action-b", Type: NodeAction, Label: "B"},
	)

	executor, instStore, _ := newTestExecutor()
	inst := &WorkflowInstance{
		ID:           "inst-1",
		InstanceData: map[string]interface{}{"status": "active"},
	}
	instStore.createdInstance = inst

	err := executor.executeConditionBranchNode(context.Background(), inst, condNode, &graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// action-a has priority 1 (lower = higher priority), should be selected
	if inst.CurrentNodeID != "action-a" {
		t.Errorf("CurrentNodeID = %q, want %q", inst.CurrentNodeID, "action-a")
	}
}

func TestConditionBranch_RoutesToDefaultWhenNoMatch(t *testing.T) {
	config := ConditionBranchConfig{
		Branches: []BranchCondition{
			{TargetNodeID: "action-yes", Expression: ConditionExpr{Field: "approved", Operator: "equals", Value: true}, Priority: 1},
		},
		DefaultBranch: "action-default",
	}

	graph, condNode := buildConditionBranchGraph(config,
		WorkflowNode{ID: "action-yes", Type: NodeAction, Label: "Yes"},
		WorkflowNode{ID: "action-default", Type: NodeAction, Label: "Default"},
	)

	executor, instStore, _ := newTestExecutor()
	inst := &WorkflowInstance{
		ID:           "inst-1",
		InstanceData: map[string]interface{}{"approved": false},
	}
	instStore.createdInstance = inst

	err := executor.executeConditionBranchNode(context.Background(), inst, condNode, &graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inst.CurrentNodeID != "action-default" {
		t.Errorf("CurrentNodeID = %q, want %q", inst.CurrentNodeID, "action-default")
	}
}

func TestConditionBranch_FailsWhenNoMatchAndNoDefault(t *testing.T) {
	config := ConditionBranchConfig{
		Branches: []BranchCondition{
			{TargetNodeID: "action-yes", Expression: ConditionExpr{Field: "approved", Operator: "equals", Value: true}, Priority: 1},
		},
		// No DefaultBranch
	}

	graph, condNode := buildConditionBranchGraph(config,
		WorkflowNode{ID: "action-yes", Type: NodeAction, Label: "Yes"},
	)

	executor, _, auditStore := newTestExecutor()
	inst := &WorkflowInstance{
		ID:           "inst-1",
		InstanceData: map[string]interface{}{"approved": false},
	}

	err := executor.executeConditionBranchNode(context.Background(), inst, condNode, &graph)
	if err == nil {
		t.Fatal("expected error when no branch matches and no default configured")
	}

	expectedMsg := "no condition branch matched and no default branch configured"
	if err.Error() != expectedMsg {
		t.Errorf("error = %q, want %q", err.Error(), expectedMsg)
	}

	// Verify audit trail records the failure
	found := false
	for _, entry := range auditStore.entries {
		if entry.EventType == "node_failed" && entry.NodeID == "cond-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected node_failed audit entry for condition branch node")
	}
}

func TestConditionBranch_MissingFieldTreatedAsNotMatched(t *testing.T) {
	config := ConditionBranchConfig{
		Branches: []BranchCondition{
			{TargetNodeID: "action-match", Expression: ConditionExpr{Field: "nonexistent_field", Operator: "equals", Value: "something"}, Priority: 1},
		},
		DefaultBranch: "action-default",
	}

	graph, condNode := buildConditionBranchGraph(config,
		WorkflowNode{ID: "action-match", Type: NodeAction, Label: "Match"},
		WorkflowNode{ID: "action-default", Type: NodeAction, Label: "Default"},
	)

	executor, instStore, _ := newTestExecutor()
	inst := &WorkflowInstance{
		ID:           "inst-1",
		InstanceData: map[string]interface{}{"other_field": "value"},
	}
	instStore.createdInstance = inst

	err := executor.executeConditionBranchNode(context.Background(), inst, condNode, &graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Missing field → condition not matched → falls through to default
	if inst.CurrentNodeID != "action-default" {
		t.Errorf("CurrentNodeID = %q, want %q", inst.CurrentNodeID, "action-default")
	}
}

func TestConditionBranch_NestedFieldPath(t *testing.T) {
	config := ConditionBranchConfig{
		Branches: []BranchCondition{
			{TargetNodeID: "action-match", Expression: ConditionExpr{Field: "request.details.amount", Operator: "greater_than", Value: float64(500)}, Priority: 1},
		},
		DefaultBranch: "action-default",
	}

	graph, condNode := buildConditionBranchGraph(config,
		WorkflowNode{ID: "action-match", Type: NodeAction, Label: "Match"},
		WorkflowNode{ID: "action-default", Type: NodeAction, Label: "Default"},
	)

	executor, instStore, _ := newTestExecutor()
	inst := &WorkflowInstance{
		ID: "inst-1",
		InstanceData: map[string]interface{}{
			"request": map[string]interface{}{
				"details": map[string]interface{}{
					"amount": float64(1000),
				},
			},
		},
	}
	instStore.createdInstance = inst

	err := executor.executeConditionBranchNode(context.Background(), inst, condNode, &graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inst.CurrentNodeID != "action-match" {
		t.Errorf("CurrentNodeID = %q, want %q", inst.CurrentNodeID, "action-match")
	}
}

// --- evaluateConditionExpr unit tests ---

func TestEvaluateConditionExpr_Equals(t *testing.T) {
	data := map[string]interface{}{"status": "active"}
	expr := ConditionExpr{Field: "status", Operator: "equals", Value: "active"}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected equals to match")
	}

	expr.Value = "inactive"
	if evaluateConditionExpr(expr, data) {
		t.Error("expected equals to not match")
	}
}

func TestEvaluateConditionExpr_NotEquals(t *testing.T) {
	data := map[string]interface{}{"status": "active"}
	expr := ConditionExpr{Field: "status", Operator: "not_equals", Value: "inactive"}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected not_equals to match")
	}
}

func TestEvaluateConditionExpr_GreaterThan(t *testing.T) {
	data := map[string]interface{}{"amount": float64(5000)}
	expr := ConditionExpr{Field: "amount", Operator: "greater_than", Value: float64(1000)}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected greater_than to match")
	}

	expr.Value = float64(10000)
	if evaluateConditionExpr(expr, data) {
		t.Error("expected greater_than to not match")
	}
}

func TestEvaluateConditionExpr_LessThan(t *testing.T) {
	data := map[string]interface{}{"amount": float64(500)}
	expr := ConditionExpr{Field: "amount", Operator: "less_than", Value: float64(1000)}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected less_than to match")
	}
}

func TestEvaluateConditionExpr_Contains(t *testing.T) {
	data := map[string]interface{}{"department": "Engineering Team"}
	expr := ConditionExpr{Field: "department", Operator: "contains", Value: "Engineering"}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected contains to match")
	}

	expr.Value = "Marketing"
	if evaluateConditionExpr(expr, data) {
		t.Error("expected contains to not match")
	}
}

func TestEvaluateConditionExpr_ContainsSlice(t *testing.T) {
	data := map[string]interface{}{"tags": []interface{}{"urgent", "finance", "review"}}
	expr := ConditionExpr{Field: "tags", Operator: "contains", Value: "urgent"}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected contains in slice to match")
	}

	expr.Value = "marketing"
	if evaluateConditionExpr(expr, data) {
		t.Error("expected contains in slice to not match")
	}
}

func TestEvaluateConditionExpr_InList(t *testing.T) {
	data := map[string]interface{}{"priority": "high"}
	expr := ConditionExpr{Field: "priority", Operator: "in_list", Value: []interface{}{"high", "critical"}}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected in_list to match")
	}

	expr.Value = []interface{}{"low", "medium"}
	if evaluateConditionExpr(expr, data) {
		t.Error("expected in_list to not match")
	}
}

func TestEvaluateConditionExpr_NotInList(t *testing.T) {
	data := map[string]interface{}{"priority": "low"}
	expr := ConditionExpr{Field: "priority", Operator: "not_in_list", Value: []interface{}{"high", "critical"}}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected not_in_list to match")
	}
}

func TestEvaluateConditionExpr_IsEmpty(t *testing.T) {
	// Missing field → empty
	data := map[string]interface{}{"other": "value"}
	expr := ConditionExpr{Field: "missing", Operator: "is_empty", Value: nil}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected is_empty to match for missing field")
	}

	// Empty string → empty
	data = map[string]interface{}{"name": ""}
	expr = ConditionExpr{Field: "name", Operator: "is_empty", Value: nil}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected is_empty to match for empty string")
	}

	// Non-empty string → not empty
	data = map[string]interface{}{"name": "Alice"}
	if evaluateConditionExpr(expr, data) {
		t.Error("expected is_empty to not match for non-empty string")
	}
}

func TestEvaluateConditionExpr_IsNotEmpty(t *testing.T) {
	data := map[string]interface{}{"name": "Alice"}
	expr := ConditionExpr{Field: "name", Operator: "is_not_empty", Value: nil}
	if !evaluateConditionExpr(expr, data) {
		t.Error("expected is_not_empty to match for non-empty field")
	}

	// Missing field → not "not empty"
	expr.Field = "missing"
	if evaluateConditionExpr(expr, data) {
		t.Error("expected is_not_empty to not match for missing field")
	}
}

func TestEvaluateConditionExpr_NilData(t *testing.T) {
	expr := ConditionExpr{Field: "anything", Operator: "equals", Value: "test"}
	if evaluateConditionExpr(expr, nil) {
		t.Error("expected false for nil data")
	}

	// is_empty should return true for nil data (field is missing)
	expr.Operator = "is_empty"
	if !evaluateConditionExpr(expr, nil) {
		t.Error("expected is_empty to return true for nil data")
	}
}

func TestEvaluateConditionExpr_UnknownOperator(t *testing.T) {
	data := map[string]interface{}{"field": "value"}
	expr := ConditionExpr{Field: "field", Operator: "unknown_op", Value: "value"}
	if evaluateConditionExpr(expr, data) {
		t.Error("expected false for unknown operator")
	}
}

func TestResolveFieldPath_MaxDepth(t *testing.T) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": map[string]interface{}{
					"d": "too deep",
				},
			},
		},
	}

	// Depth 3 should work
	val, found := resolveFieldPath(data, "a.b.c")
	if !found {
		t.Error("expected depth 3 to resolve")
	}
	if _, ok := val.(map[string]interface{}); !ok {
		t.Error("expected map at depth 3")
	}

	// Depth 4 should fail
	_, found = resolveFieldPath(data, "a.b.c.d")
	if found {
		t.Error("expected depth 4 to fail (max depth is 3)")
	}
}

func TestResolveFieldPath_NullValue(t *testing.T) {
	data := map[string]interface{}{"field": nil}
	_, found := resolveFieldPath(data, "field")
	if found {
		t.Error("expected nil value to return not found")
	}
}
