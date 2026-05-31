package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// casConflictMockInstanceStore is an InstanceStore that also implements
// OptimisticInstanceDataUpdater and simulates a cross-process version conflict:
// the FIRST UpdateInstanceDataCAS call observes a concurrent writer that already
// committed `concurrentData` (bumping the row version), so the CAS reports a
// conflict instead of applying. This is the race a per-process mutex CANNOT
// guard (two Hub replicas sharing one database); it forces the executor down the
// re-read-and-retry path so we can assert no vote is lost.
type casConflictMockInstanceStore struct {
	persisted      map[string]interface{}
	version        int64
	concurrentData map[string]interface{} // injected once on the first CAS
	injected       bool
	casCalls       int
	statusUpdate   InstanceStatus

	// concurrentSettlesStatus, when non-empty, models a concurrent writer that
	// not only persisted its vote but already SETTLED the instance (advanced
	// past this node to a terminal/blocked/failed state). After the injected
	// conflict, Get reports this status so the executor's post-conflict
	// running-state guard can decline the late decision. Empty means the
	// instance stays running (the common countersign-not-yet-satisfied case).
	concurrentSettlesStatus InstanceStatus
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(m)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = make(map[string]interface{})
	}
	return out
}

func (m *casConflictMockInstanceStore) Create(_ context.Context, inst *WorkflowInstance) error {
	m.persisted = deepCopyMap(inst.InstanceData)
	return nil
}

func (m *casConflictMockInstanceStore) Get(_ context.Context, _ string) (*WorkflowInstance, error) {
	status := InstanceRunning
	// Once the concurrent writer's conflict has been injected, model it having
	// settled the instance (if configured), so the post-conflict re-read
	// observes a non-running status.
	if m.injected && m.concurrentSettlesStatus != "" {
		status = m.concurrentSettlesStatus
	}
	return &WorkflowInstance{
		ID:            "inst-1",
		VersionID:     "ver-1",
		Status:        status,
		CurrentNodeID: "approval-1",
		InstanceData:  deepCopyMap(m.persisted),
		RowVersion:    m.version,
	}, nil
}

func (m *casConflictMockInstanceStore) UpdateStatus(_ context.Context, _ string, status InstanceStatus) error {
	m.statusUpdate = status
	return nil
}

func (m *casConflictMockInstanceStore) UpdateCurrentNode(_ context.Context, _, _ string) error {
	return nil
}

func (m *casConflictMockInstanceStore) UpdateInstanceData(_ context.Context, _ string, data map[string]interface{}) error {
	m.persisted = deepCopyMap(data)
	return nil
}

func (m *casConflictMockInstanceStore) UpdateInstanceDataCAS(_ context.Context, _ string, expectedVersion int64, data map[string]interface{}) (int64, error) {
	m.casCalls++
	// Simulate a concurrent writer that committed between our read and this
	// write on the first attempt only.
	if !m.injected && m.concurrentData != nil {
		m.injected = true
		m.persisted = deepCopyMap(m.concurrentData)
		m.version++
		return 0, ErrInstanceVersionConflict
	}
	if expectedVersion != m.version {
		return 0, ErrInstanceVersionConflict
	}
	m.persisted = deepCopyMap(data)
	m.version++
	return m.version, nil
}

func (m *casConflictMockInstanceStore) CreateNodeExecution(_ context.Context, _ *NodeExecution) error {
	return nil
}
func (m *casConflictMockInstanceStore) UpdateNodeExecution(_ context.Context, _ string, _ NodeStatus, _ json.RawMessage, _ string) error {
	return nil
}
func (m *casConflictMockInstanceStore) GetPendingApprovals(_ context.Context, _ string) ([]NodeExecution, error) {
	return nil, nil
}
func (m *casConflictMockInstanceStore) QueryMyInitiated(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *casConflictMockInstanceStore) QueryPendingMyAction(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *casConflictMockInstanceStore) QueryPendingMyConfirmation(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}
func (m *casConflictMockInstanceStore) QueryCompleted(_ context.Context, _ string, _ DirectoryFilter) ([]DirectoryItem, int, error) {
	return nil, 0, nil
}

// TestResumeInstance_OptimisticLock_RetriesOnConflictNoLostVote verifies the
// cross-process serialization mechanism (Requirement 2.6 / Finding 1.6): when
// the optimistic-lock CAS reports that a concurrent decision committed first,
// ResumeInstance re-reads the fresh state and re-applies its own decision, so
// BOTH votes survive — exactly the guarantee a per-process mutex cannot provide
// for a multi-replica Hub.
func TestResumeInstance_OptimisticLock_RetriesOnConflictNoLostVote(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1", "ve-2", "ve-3"},
		Mode:         ModeCountersign,
		TimeoutHours: 24,
	})
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}

	// Concurrent writer ve-2's committed approval state — injected on the first
	// CAS attempt to force a conflict.
	concurrent := map[string]interface{}{
		"_approval_state_approval-1": map[string]interface{}{
			"decisions": map[string]interface{}{"ve-2": "approve"},
		},
	}
	instStore := &casConflictMockInstanceStore{
		persisted:      map[string]interface{}{},
		concurrentData: concurrent,
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// This goroutine's decision is ve-1's approval. It will hit a CAS conflict
	// (ve-2 won the race), then re-read and re-apply.
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-1", DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("ResumeInstance returned error: %v", err)
	}

	// The CAS must have been attempted at least twice (conflict, then success).
	if instStore.casCalls < 2 {
		t.Fatalf("expected at least 2 CAS attempts (conflict + retry), got %d", instStore.casCalls)
	}

	// Both ve-1's (this decision) and ve-2's (concurrent) votes must persist.
	state, _ := instStore.persisted["_approval_state_approval-1"].(map[string]interface{})
	if state == nil {
		t.Fatalf("expected approval state persisted, got %v", instStore.persisted)
	}
	decisions, _ := state["decisions"].(map[string]interface{})
	for _, id := range []string{"ve-1", "ve-2"} {
		if _, ok := decisions[id]; !ok {
			t.Fatalf("lost vote: expected %q in decisions, got %v", id, decisions)
		}
	}

	// Two approvals of three: countersign node not yet satisfied, instance stays
	// running (no settle).
	if instStore.statusUpdate == InstanceCompleted || instStore.statusUpdate == InstanceFailed {
		t.Fatalf("countersign with 2/3 approvals must not settle, got status %q", instStore.statusUpdate)
	}
}

// TestResumeInstance_OptimisticLock_DeclinesLateDecisionAfterConcurrentSettle
// verifies the cross-process settle guard: when a concurrent decision wins the
// CAS race AND settles the instance (advances past this node so the instance
// reaches a terminal state), a late decision that conflicts must NOT re-apply
// and re-advance — it must observe the settled status on the conflict re-read
// and decline, exactly as the single-process mutex path does via the top-level
// running-state guard in ResumeInstance.
//
// Without the guard, the cross-process CAS path would re-apply the late vote on
// top of the settled state and return shouldAdvance again, re-executing
// downstream nodes / re-completing the instance (double execution). This is the
// race the optimistic lock exists to close, so a per-process mutex cannot help.
func TestResumeInstance_OptimisticLock_DeclinesLateDecisionAfterConcurrentSettle(t *testing.T) {
	// any-N-of-M with N=1: a single approval settles (advances) the node, so the
	// winning concurrent writer completes the instance.
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve-1", "ve-2", "ve-3"},
		Mode:         ModeAnyNofM,
		MinApprovals: 1,
		TimeoutHours: 24,
	})
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}

	// The concurrent writer ve-2 approved (reaching N=1) and SETTLED the
	// instance to completed.
	concurrent := map[string]interface{}{
		"_approval_state_approval-1": map[string]interface{}{
			"decisions": map[string]interface{}{"ve-2": "approve"},
		},
	}
	instStore := &casConflictMockInstanceStore{
		persisted:               map[string]interface{}{},
		concurrentData:          concurrent,
		concurrentSettlesStatus: InstanceCompleted,
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// ve-1's late approval conflicts with ve-2's settling decision.
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision: "approve", ApproverID: "ve-1", DecidedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected late decision after concurrent settle to be declined, got nil error")
	}
	if !strings.Contains(err.Error(), "is not running") {
		t.Fatalf("expected a not-running guard error, got: %v", err)
	}

	// The late decision must NOT have re-settled the instance: no
	// UpdateStatus(completed/failed) issued by this goroutine after the
	// concurrent writer already completed it.
	if instStore.statusUpdate == InstanceCompleted || instStore.statusUpdate == InstanceFailed {
		t.Fatalf("late decision re-settled the instance to %q (double execution); want no settle from this goroutine", instStore.statusUpdate)
	}
}
