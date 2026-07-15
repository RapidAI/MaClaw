package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	storepkg "github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
	_ "modernc.org/sqlite"
)

func setupInstanceStoreTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Run migrations to create tables
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestInstanceStore_CreateAndGet(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	store := NewInstanceStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	inst := &workflow.WorkflowInstance{
		ID:            "inst-001",
		WorkflowID:    "wf-001",
		VersionID:     "ver-001",
		Status:        workflow.InstanceRunning,
		CurrentNodeID: "node-trigger",
		InstanceData:  map[string]interface{}{"amount": float64(5000), "dept": "engineering"},
		TriggerData:   `{"source":"api"}`,
		CreatedAt:     now,
	}

	// Create
	if err := store.Create(ctx, inst); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Get
	got, err := store.Get(ctx, "inst-001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}

	if got.ID != inst.ID {
		t.Errorf("ID = %q, want %q", got.ID, inst.ID)
	}
	if got.WorkflowID != inst.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", got.WorkflowID, inst.WorkflowID)
	}
	if got.VersionID != inst.VersionID {
		t.Errorf("VersionID = %q, want %q", got.VersionID, inst.VersionID)
	}
	if got.Status != workflow.InstanceRunning {
		t.Errorf("Status = %q, want %q", got.Status, workflow.InstanceRunning)
	}
	if got.CurrentNodeID != "node-trigger" {
		t.Errorf("CurrentNodeID = %q, want %q", got.CurrentNodeID, "node-trigger")
	}
	if got.TriggerData != `{"source":"api"}` {
		t.Errorf("TriggerData = %q, want %q", got.TriggerData, `{"source":"api"}`)
	}
	if got.CompletedAt != nil {
		t.Errorf("CompletedAt = %v, want nil", got.CompletedAt)
	}

	// Check InstanceData deserialization
	amt, ok := got.InstanceData["amount"]
	if !ok || amt != float64(5000) {
		t.Errorf("InstanceData[amount] = %v, want 5000", amt)
	}
}

func TestInstanceStore_GetNotFound(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	store := NewInstanceStore(db)
	ctx := context.Background()

	got, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent instance, got %+v", got)
	}
}

func TestInstanceStore_UpdateStatus(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	store := NewInstanceStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	inst := &workflow.WorkflowInstance{
		ID:            "inst-002",
		WorkflowID:    "wf-001",
		VersionID:     "ver-001",
		Status:        workflow.InstanceRunning,
		CurrentNodeID: "node-1",
		InstanceData:  map[string]interface{}{},
		CreatedAt:     now,
	}
	if err := store.Create(ctx, inst); err != nil {
		t.Fatal(err)
	}

	// Update to completed
	if err := store.UpdateStatus(ctx, "inst-002", workflow.InstanceCompleted); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := store.Get(ctx, "inst-002")
	if got.Status != workflow.InstanceCompleted {
		t.Errorf("Status = %q, want %q", got.Status, workflow.InstanceCompleted)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should be set when status is completed")
	}
}

func TestInstanceStore_UpdateCurrentNode(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	store := NewInstanceStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	inst := &workflow.WorkflowInstance{
		ID:            "inst-003",
		WorkflowID:    "wf-001",
		VersionID:     "ver-001",
		Status:        workflow.InstanceRunning,
		CurrentNodeID: "node-1",
		InstanceData:  map[string]interface{}{},
		CreatedAt:     now,
	}
	if err := store.Create(ctx, inst); err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateCurrentNode(ctx, "inst-003", "node-approval"); err != nil {
		t.Fatalf("UpdateCurrentNode: %v", err)
	}

	got, _ := store.Get(ctx, "inst-003")
	if got.CurrentNodeID != "node-approval" {
		t.Errorf("CurrentNodeID = %q, want %q", got.CurrentNodeID, "node-approval")
	}
}

func TestInstanceStore_NodeExecution_CreateAndUpdate(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	store := NewInstanceStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create parent instance first (foreign key)
	inst := &workflow.WorkflowInstance{
		ID:           "inst-004",
		WorkflowID:   "wf-001",
		VersionID:    "ver-001",
		Status:       workflow.InstanceRunning,
		InstanceData: map[string]interface{}{},
		CreatedAt:    now,
	}
	if err := store.Create(ctx, inst); err != nil {
		t.Fatal(err)
	}

	// Create node execution
	exec := &workflow.NodeExecution{
		ID:         "exec-001",
		InstanceID: "inst-004",
		NodeID:     "approval-node-1",
		Status:     workflow.NodePending,
		StartedAt:  now,
	}
	if err := store.CreateNodeExecution(ctx, exec); err != nil {
		t.Fatalf("CreateNodeExecution: %v", err)
	}

	// Update node execution
	result := json.RawMessage(`{"decision":"approve","rationale":"amount under threshold"}`)
	if err := store.UpdateNodeExecution(ctx, "exec-001", workflow.NodeCompleted, result, ""); err != nil {
		t.Fatalf("UpdateNodeExecution: %v", err)
	}

	// Verify via GetPendingApprovals (should not return completed ones)
	pending, err := store.GetPendingApprovals(ctx, "approver-1")
	if err != nil {
		t.Fatalf("GetPendingApprovals: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending approvals after completion, got %d", len(pending))
	}
}

func TestInstanceStore_GetPendingApprovals(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	store := NewInstanceStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create parent instance
	inst := &workflow.WorkflowInstance{
		ID:           "inst-005",
		WorkflowID:   "wf-001",
		VersionID:    "ver-001",
		Status:       workflow.InstanceRunning,
		InstanceData: map[string]interface{}{},
		CreatedAt:    now,
	}
	if err := store.Create(ctx, inst); err != nil {
		t.Fatal(err)
	}

	// Create multiple node executions with different statuses and node types.
	executions := []*workflow.NodeExecution{
		{ID: "exec-p1", InstanceID: "inst-005", NodeID: "node-a", NodeType: workflow.NodeApproval, Status: workflow.NodeRunning, StartedAt: now},
		{ID: "exec-p2", InstanceID: "inst-005", NodeID: "node-b", NodeType: workflow.NodeApproval, Status: workflow.NodeRunning, StartedAt: now.Add(time.Second)},
		{ID: "exec-r1", InstanceID: "inst-005", NodeID: "node-c", NodeType: workflow.NodeAction, Status: workflow.NodeRunning, StartedAt: now},
		{ID: "exec-c1", InstanceID: "inst-005", NodeID: "node-d", NodeType: workflow.NodeApproval, Status: workflow.NodeCompleted, StartedAt: now},
	}
	for _, e := range executions {
		if err := store.CreateNodeExecution(ctx, e); err != nil {
			t.Fatalf("CreateNodeExecution(%s): %v", e.ID, err)
		}
	}

	// GetPendingApprovals should return only running approval nodes.
	pending, err := store.GetPendingApprovals(ctx, "any-approver")
	if err != nil {
		t.Fatalf("GetPendingApprovals: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending approvals, got %d", len(pending))
	}

	// Verify ordering by started_at ASC
	if pending[0].ID != "exec-p1" {
		t.Errorf("first pending ID = %q, want %q", pending[0].ID, "exec-p1")
	}
	if pending[1].ID != "exec-p2" {
		t.Errorf("second pending ID = %q, want %q", pending[1].ID, "exec-p2")
	}
}

func TestInstanceStore_NodeExecution_WithResult(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	store := NewInstanceStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create parent instance
	inst := &workflow.WorkflowInstance{
		ID:           "inst-006",
		WorkflowID:   "wf-001",
		VersionID:    "ver-001",
		Status:       workflow.InstanceRunning,
		InstanceData: map[string]interface{}{},
		CreatedAt:    now,
	}
	if err := store.Create(ctx, inst); err != nil {
		t.Fatal(err)
	}

	// Create node execution with initial result
	initialResult := json.RawMessage(`{"step":"started"}`)
	exec := &workflow.NodeExecution{
		ID:         "exec-res-1",
		InstanceID: "inst-006",
		NodeID:     "action-node",
		Status:     workflow.NodeRunning,
		StartedAt:  now,
		Result:     initialResult,
	}
	if err := store.CreateNodeExecution(ctx, exec); err != nil {
		t.Fatalf("CreateNodeExecution: %v", err)
	}

	// Update with failure
	if err := store.UpdateNodeExecution(ctx, "exec-res-1", workflow.NodeFailed, nil, "timeout exceeded"); err != nil {
		t.Fatalf("UpdateNodeExecution: %v", err)
	}

	// Verify it's not in pending list
	pending, _ := store.GetPendingApprovals(ctx, "")
	for _, p := range pending {
		if p.ID == "exec-res-1" {
			t.Error("failed node execution should not appear in pending approvals")
		}
	}
}

func TestInstanceStore_TenantIsolation(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	store := NewInstanceStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	ctxA := storepkg.WithTenant(context.Background(), "tenant_a")
	ctxB := storepkg.WithTenant(context.Background(), "tenant_b")

	instA := &workflow.WorkflowInstance{
		ID:           "inst-tenant-a",
		WorkflowID:   "wf-tenant",
		VersionID:    "ver-tenant",
		Status:       workflow.InstanceRunning,
		InstanceData: map[string]interface{}{"initiator_id": "user_1", "approver_id": "user_1", "workflow_name": "Tenant A"},
		CreatedAt:    now,
	}
	instB := &workflow.WorkflowInstance{
		ID:           "inst-tenant-b",
		WorkflowID:   "wf-tenant",
		VersionID:    "ver-tenant",
		Status:       workflow.InstanceRunning,
		InstanceData: map[string]interface{}{"initiator_id": "user_1", "approver_id": "user_1", "workflow_name": "Tenant B"},
		CreatedAt:    now.Add(time.Second),
	}
	if err := store.Create(ctxA, instA); err != nil {
		t.Fatalf("Create tenant A: %v", err)
	}
	if err := store.Create(ctxB, instB); err != nil {
		t.Fatalf("Create tenant B: %v", err)
	}

	if got, err := store.Get(ctxA, instB.ID); err != nil || got != nil {
		t.Fatalf("tenant A should not read tenant B instance, got=%+v err=%v", got, err)
	}
	items, total, err := store.QueryMyInitiated(ctxA, "user_1", workflow.DirectoryFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("QueryMyInitiated tenant A: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].InstanceID != instA.ID {
		t.Fatalf("tenant A query leaked instances: total=%d items=%+v", total, items)
	}

	execA := &workflow.NodeExecution{ID: "exec-tenant-a", InstanceID: instA.ID, NodeID: "approval", NodeType: workflow.NodeApproval, Status: workflow.NodeRunning, StartedAt: now}
	execB := &workflow.NodeExecution{ID: "exec-tenant-b", InstanceID: instB.ID, NodeID: "approval", NodeType: workflow.NodeApproval, Status: workflow.NodeRunning, StartedAt: now}
	if err := store.CreateNodeExecution(ctxA, execA); err != nil {
		t.Fatalf("CreateNodeExecution tenant A: %v", err)
	}
	if err := store.CreateNodeExecution(ctxB, execB); err != nil {
		t.Fatalf("CreateNodeExecution tenant B: %v", err)
	}
	if err := store.UpdateNodeExecution(ctxA, execB.ID, workflow.NodeCompleted, nil, ""); err != nil {
		t.Fatalf("UpdateNodeExecution cross tenant: %v", err)
	}
	pendingA, err := store.GetPendingApprovals(ctxA, "user_1")
	if err != nil {
		t.Fatalf("GetPendingApprovals tenant A: %v", err)
	}
	if len(pendingA) != 1 || pendingA[0].ID != execA.ID {
		t.Fatalf("tenant A pending approvals leaked: %+v", pendingA)
	}
}

func TestInstanceStore_GetPendingApprovalsWithoutTenantListsAllTenants(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	instanceStore := NewInstanceStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	ctxA := storepkg.WithTenant(context.Background(), "tenant_a")
	ctxB := storepkg.WithTenant(context.Background(), "tenant_b")

	for _, item := range []struct {
		ctx                     context.Context
		instanceID, executionID string
	}{
		{ctxA, "inst-a", "exec-a"},
		{ctxB, "inst-b", "exec-b"},
	} {
		if err := instanceStore.Create(item.ctx, &workflow.WorkflowInstance{ID: item.instanceID, WorkflowID: "wf", VersionID: "ver", Status: workflow.InstanceRunning, InstanceData: map[string]interface{}{}, CreatedAt: now}); err != nil {
			t.Fatalf("create instance %s: %v", item.instanceID, err)
		}
		if err := instanceStore.CreateNodeExecution(item.ctx, &workflow.NodeExecution{ID: item.executionID, InstanceID: item.instanceID, NodeID: "approval", NodeType: workflow.NodeApproval, Status: workflow.NodeRunning, StartedAt: now}); err != nil {
			t.Fatalf("create node execution %s: %v", item.executionID, err)
		}
	}

	pending, err := instanceStore.GetPendingApprovals(context.Background(), "")
	if err != nil {
		t.Fatalf("GetPendingApprovals without tenant: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("global pending count = %d, want 2", len(pending))
	}
	if pending[0].TenantID != "tenant_a" || pending[1].TenantID != "tenant_b" {
		t.Fatalf("global pending tenant ids = %q, %q", pending[0].TenantID, pending[1].TenantID)
	}
}
