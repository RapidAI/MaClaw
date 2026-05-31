package sqlite

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
	_ "modernc.org/sqlite"
)

// concNoopDispatcher is a no-op ApprovalDispatcher for the concurrency test.
type concNoopDispatcher struct{}

func (concNoopDispatcher) Dispatch(_ context.Context, _ *workflow.ApprovalRequest, _ string) error {
	return nil
}
func (concNoopDispatcher) DispatchFallback(_ context.Context, _ *workflow.ApprovalRequest, _ string, _ string) error {
	return nil
}

// TestResumeInstance_ConcurrentDecisions_SameNode_NoLostVote is a focused
// end-to-end test for task 3.6 (Requirement 2.6 / Finding 1.6).
//
// Two approvers of a countersign node respond near-simultaneously. The original
// code did a read-modify-write of InstanceData followed by a full
// UpdateInstanceData overwrite with no optimistic locking, so the second
// writer's overwrite could silently discard the first writer's vote. The fix
// serializes the read-modify-write-persist cycle per instance AND guards the
// persist with the row's optimistic-lock version (conditional UPDATE ... WHERE
// row_version = expectedVersion, retrying on conflict), so neither vote is lost.
//
// This test uses the REAL production sqlite instanceStore (which implements
// workflow.OptimisticInstanceDataUpdater), so it exercises the actual CAS path,
// not a mock.
func TestResumeInstance_ConcurrentDecisions_SameNode_NoLostVote(t *testing.T) {
	db := setupInstanceStoreTestDB(t)
	ctx := context.Background()

	instStore := NewInstanceStore(db)
	wfStore := NewWorkflowStore(db)
	auditStore := NewAuditStore(db)

	// --- Build a published version with a 3-approver countersign node. ---
	approvalCfg, _ := json.Marshal(workflow.ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1", "ve-2", "ve-3"},
		Mode:         workflow.ModeCountersign,
		TimeoutHours: 24,
	})
	graph := workflow.WorkflowGraph{
		Nodes: []workflow.WorkflowNode{
			{ID: "trigger-1", Type: workflow.NodeTrigger, Label: "Start"},
			{ID: "approval-1", Type: workflow.NodeApproval, Label: "Review", Config: approvalCfg},
			{ID: "action-1", Type: workflow.NodeAction, Label: "Done"},
		},
		Edges: []workflow.WorkflowEdge{
			{ID: "e1", SourceID: "trigger-1", TargetID: "approval-1"},
			{ID: "e2", SourceID: "approval-1", TargetID: "action-1"},
		},
	}

	now := time.Now().UTC()
	def := &workflow.WorkflowDefinition{ID: "wf-1", OwnerID: "owner-1", Name: "Countersign", CreatedAt: now, UpdatedAt: now}
	if err := wfStore.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	ver := &workflow.WorkflowVersion{
		ID: "ver-1", WorkflowID: "wf-1", VersionNumber: "1.0.0",
		Status: workflow.VersionPublished, Graph: graph, CreatedAt: now, UpdatedAt: now,
	}
	if err := wfStore.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	// --- Create a running instance blocked at the countersign approval node. ---
	inst := &workflow.WorkflowInstance{
		ID: "inst-1", WorkflowID: "wf-1", VersionID: "ver-1",
		Status: workflow.InstanceRunning, CurrentNodeID: "approval-1",
		InstanceData: map[string]interface{}{"requester_id": "owner-1"},
		CreatedAt:    now,
	}
	if err := instStore.Create(ctx, inst); err != nil {
		t.Fatalf("Create instance: %v", err)
	}

	executor := workflow.NewWorkflowExecutor(wfStore, instStore, auditStore, concNoopDispatcher{})

	// --- Two near-simultaneous approvals on the same countersign node. ---
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	for i, approver := range []string{"ve-1", "ve-2"} {
		go func(idx int, id string) {
			defer wg.Done()
			errs[idx] = executor.ResumeInstance(ctx, "inst-1", "approval-1", workflow.ApprovalResponse{
				Decision:   "approve",
				ApproverID: id,
				DecidedAt:  time.Now().UTC(),
			})
		}(i, approver)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent ResumeInstance[%d] returned error: %v", i, err)
		}
	}

	// --- Both votes must be present in the persisted approval state. ---
	got, err := instStore.Get(ctx, "inst-1")
	if err != nil {
		t.Fatalf("Get instance: %v", err)
	}
	if got == nil {
		t.Fatal("instance disappeared")
	}
	state, _ := got.InstanceData["_approval_state_approval-1"].(map[string]interface{})
	if state == nil {
		t.Fatalf("expected approval state to be persisted, got instance_data=%v", got.InstanceData)
	}
	decisions, _ := state["decisions"].(map[string]interface{})
	recorded := 0
	for _, id := range []string{"ve-1", "ve-2"} {
		if _, ok := decisions[id]; ok {
			recorded++
		}
	}
	if recorded != 2 {
		t.Fatalf("lost vote on concurrent countersign decisions: expected 2 recorded approvals, got %d (decisions=%v)", recorded, decisions)
	}

	// Instance must still be running (third approver ve-3 has not yet approved,
	// so the countersign node is not satisfied).
	if got.Status != workflow.InstanceRunning {
		t.Fatalf("countersign node should still be waiting for ve-3, got status %q", got.Status)
	}

	// The row version must reflect that exactly two CAS writes committed, proving
	// both persists were applied serially under the optimistic-lock guard.
	if got.RowVersion != 2 {
		t.Fatalf("expected row_version=2 after two committed decisions, got %d", got.RowVersion)
	}
}
