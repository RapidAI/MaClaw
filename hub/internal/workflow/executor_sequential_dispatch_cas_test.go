package workflow

import (
	"context"
	"testing"
	"time"
)

// TestResumeInstance_SequentialMode_NoDuplicateDispatchOnCASRetry is the
// regression test for the duplicate-side-effect bug: sequential-mode dispatch to
// the next approver must happen EXACTLY ONCE even when a cross-process
// optimistic-lock CAS conflict forces the decision evaluation
// (processApprovalResponse -> processSequentialMode) to re-run.
//
// Before the fix, processSequentialMode called e.dispatcher.Dispatch inside the
// CAS retry loop. A cross-process conflict (the race a per-process mutex cannot
// guard) re-read state and re-invoked processSequentialMode, dispatching to the
// next approver a SECOND time — delivering a duplicate approval-request envelope.
//
// The fix separates the pure decision EVALUATION (which approver to notify next)
// from the side-effect EXECUTION (the actual Dispatch), which now runs once,
// after the approval state is durably committed.
//
// This test drives a sequential ResumeInstance against a store that implements
// OptimisticInstanceDataUpdater and injects ONE CAS version conflict on the
// first commit, with a spy dispatcher, and asserts the next approver is
// dispatched exactly once despite the retry.
func TestResumeInstance_SequentialMode_NoDuplicateDispatchOnCASRetry(t *testing.T) {
	graph := buildApprovalGraph(ApprovalNodeConfig{
		ApproverIDs:   []string{"ve-1", "ve-2", "ve-3"},
		Mode:          ModeSequential,
		ApproverOrder: []string{"ve-1", "ve-2", "ve-3"},
		TimeoutHours:  24,
	})
	ver := &WorkflowVersion{ID: "ver-1", Graph: graph}
	wfStore := &resumeTestMockWorkflowStore{version: ver}

	// A benign concurrent writer commits an (empty) approval state on the first
	// CAS, forcing exactly one version conflict. The instance stays running
	// (concurrentSettlesStatus empty), so after the re-read ve-1's approval
	// re-applies and the executor still intends to dispatch to ve-2 — the exact
	// condition under which the old code double-dispatched.
	concurrent := map[string]interface{}{
		"_approval_state_approval-1": map[string]interface{}{
			"decisions": map[string]interface{}{},
		},
	}
	instStore := &casConflictMockInstanceStore{
		persisted:      map[string]interface{}{},
		concurrentData: concurrent,
	}
	auditStore := &mockAuditStore{}
	dispatcher := &resumeTestMockDispatcher{}
	executor := NewWorkflowExecutor(wfStore, instStore, auditStore, dispatcher)

	// ve-1 (first in order) approves. Not the last approver, so the executor must
	// dispatch to ve-2. The injected CAS conflict forces the decision evaluation
	// to run twice.
	err := executor.ResumeInstance(context.Background(), "inst-1", "approval-1", ApprovalResponse{
		Decision:   "approve",
		ApproverID: "ve-1",
		DecidedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("ResumeInstance returned error: %v", err)
	}

	// The CAS must have been attempted at least twice (conflict, then success),
	// proving the decision evaluation re-ran — the precondition for the bug.
	if instStore.casCalls < 2 {
		t.Fatalf("expected at least 2 CAS attempts (conflict + retry), got %d", instStore.casCalls)
	}

	// The next approver must be dispatched EXACTLY ONCE despite the retry.
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected exactly 1 dispatch despite CAS retry, got %d (%v)", len(dispatcher.dispatched), dispatcher.dispatched)
	}
	if dispatcher.dispatched[0] != "ve-2" {
		t.Fatalf("expected dispatch to next approver ve-2, got %q", dispatcher.dispatched[0])
	}

	// The instance must remain running (still waiting on ve-2), and ve-1's vote
	// must be persisted.
	if instStore.statusUpdate == InstanceCompleted || instStore.statusUpdate == InstanceFailed {
		t.Fatalf("sequential first-of-three approve must not settle, got status %q", instStore.statusUpdate)
	}
	state, _ := instStore.persisted["_approval_state_approval-1"].(map[string]interface{})
	if state == nil {
		t.Fatalf("expected approval state persisted, got %v", instStore.persisted)
	}
	decisions, _ := state["decisions"].(map[string]interface{})
	if _, ok := decisions["ve-1"]; !ok {
		t.Fatalf("lost vote: expected ve-1 in decisions, got %v", decisions)
	}
}
