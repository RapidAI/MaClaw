package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestWithdrawReview_WithOwnershipCheck(t *testing.T) {
	store := newMockVersionStore()
	auditStore := &mockAuditStore{}
	vm := NewVersionManager(store, auditStore)
	ctx := context.Background()

	// Create workflow owned by user_a.
	store.CreateWorkflow(ctx, &WorkflowDefinition{
		ID: "wf_own", OwnerID: "user_a", Name: "Test Workflow",
	})

	// Create and submit a version.
	ver, _ := vm.SaveDraft(ctx, "wf_own", validGraph())
	_ = vm.SubmitForReview(ctx, ver.ID)

	// Withdraw as the owner — should succeed.
	err := vm.WithdrawReview(ctx, ver.ID, "user_a")
	if err != nil {
		t.Fatalf("WithdrawReview as owner failed: %v", err)
	}

	// Verify status changed to draft.
	if store.versions[ver.ID].Status != VersionDraft {
		t.Fatalf("expected status draft after withdrawal, got %s", store.versions[ver.ID].Status)
	}

	// Verify audit trail entry was recorded.
	if len(auditStore.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditStore.entries))
	}
	entry := auditStore.entries[0]
	if entry.EventType != "version_withdrawn" {
		t.Fatalf("expected event_type version_withdrawn, got %s", entry.EventType)
	}
	if entry.ActorID != "user_a" {
		t.Fatalf("expected actor_id user_a, got %s", entry.ActorID)
	}
	if entry.InstanceID != "wf_own" {
		t.Fatalf("expected instance_id wf_own, got %s", entry.InstanceID)
	}
}

func TestWithdrawReview_NotOwner(t *testing.T) {
	store := newMockVersionStore()
	auditStore := &mockAuditStore{}
	vm := NewVersionManager(store, auditStore)
	ctx := context.Background()

	// Create workflow owned by user_a.
	store.CreateWorkflow(ctx, &WorkflowDefinition{
		ID: "wf_notowner", OwnerID: "user_a", Name: "Test Workflow",
	})

	// Create and submit a version.
	ver, _ := vm.SaveDraft(ctx, "wf_notowner", validGraph())
	_ = vm.SubmitForReview(ctx, ver.ID)

	// Attempt withdrawal as a different user — should fail.
	err := vm.WithdrawReview(ctx, ver.ID, "user_b")
	if err == nil {
		t.Fatal("expected error when non-owner attempts withdrawal")
	}
	if !errors.Is(err, ErrNotWorkflowOwner) {
		t.Fatalf("expected ErrNotWorkflowOwner, got %v", err)
	}

	// Verify status unchanged.
	if store.versions[ver.ID].Status != VersionPendingReview {
		t.Fatalf("expected status unchanged (pending_review), got %s", store.versions[ver.ID].Status)
	}

	// Verify no audit entry was recorded.
	if len(auditStore.entries) != 0 {
		t.Fatalf("expected 0 audit entries after failed withdrawal, got %d", len(auditStore.entries))
	}
}

func TestWithdrawReview_WrongStatus_WithOwnership(t *testing.T) {
	store := newMockVersionStore()
	auditStore := &mockAuditStore{}
	vm := NewVersionManager(store, auditStore)
	ctx := context.Background()

	// Create workflow owned by user_a.
	store.CreateWorkflow(ctx, &WorkflowDefinition{
		ID: "wf_wrongst", OwnerID: "user_a", Name: "Test Workflow",
	})

	// Create a draft version (not submitted).
	ver, _ := vm.SaveDraft(ctx, "wf_wrongst", validGraph())

	// Attempt withdrawal from draft status — should fail.
	err := vm.WithdrawReview(ctx, ver.ID, "user_a")
	if err == nil {
		t.Fatal("expected error when withdrawing from draft status")
	}
	if !errors.Is(err, ErrVersionNotPendingReview) {
		t.Fatalf("expected ErrVersionNotPendingReview, got %v", err)
	}
}

func TestWithdrawReview_NoAuditStore(t *testing.T) {
	store := newMockVersionStore()
	// Create VersionManager without audit store.
	vm := NewVersionManager(store)
	ctx := context.Background()

	// Create workflow owned by user_a.
	store.CreateWorkflow(ctx, &WorkflowDefinition{
		ID: "wf_noaudit", OwnerID: "user_a", Name: "Test Workflow",
	})

	// Create and submit a version.
	ver, _ := vm.SaveDraft(ctx, "wf_noaudit", validGraph())
	_ = vm.SubmitForReview(ctx, ver.ID)

	// Withdraw with ownership check but no audit store — should succeed.
	err := vm.WithdrawReview(ctx, ver.ID, "user_a")
	if err != nil {
		t.Fatalf("WithdrawReview without audit store failed: %v", err)
	}

	// Verify status changed to draft.
	if store.versions[ver.ID].Status != VersionDraft {
		t.Fatalf("expected status draft, got %s", store.versions[ver.ID].Status)
	}
}

func TestWithdrawReview_BackwardCompatible_NoUserID(t *testing.T) {
	store := newMockVersionStore()
	vm := NewVersionManager(store)
	ctx := context.Background()

	// Create workflow.
	store.CreateWorkflow(ctx, &WorkflowDefinition{
		ID: "wf_compat", OwnerID: "user_a", Name: "Test Workflow",
	})

	// Create and submit a version.
	ver, _ := vm.SaveDraft(ctx, "wf_compat", validGraph())
	_ = vm.SubmitForReview(ctx, ver.ID)

	// Withdraw without userID (backward compatible) — should succeed without ownership check.
	err := vm.WithdrawReview(ctx, ver.ID)
	if err != nil {
		t.Fatalf("WithdrawReview without userID failed: %v", err)
	}

	if store.versions[ver.ID].Status != VersionDraft {
		t.Fatalf("expected status draft, got %s", store.versions[ver.ID].Status)
	}
}
