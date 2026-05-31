package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
)

// TestVersionManager_Approve_RegistersInCapabilityMarket is the focused test
// for task 3.8 (single authoritative publish path / Requirement 2.8).
//
// It exercises the bug-condition branch isBugCondition(X) where
// X.kind = Publish AND X.viaVersionManagerApprove: a workflow published
// through VersionManager.Approve must appear in the capability market, just
// like one published through AdminReviewService.ApproveSubmission.
func TestVersionManager_Approve_RegistersInCapabilityMarket(t *testing.T) {
	store := newMockStoreForAdmin()
	db := newAdminReviewCapabilityDB(t)
	capSvc := capability.NewService(db)

	// Wire the capability market into the VersionManager so Approve converges
	// on the single authoritative publish path.
	vm := NewVersionManager(store).WithCapabilityService(capSvc)
	ctx := context.Background()

	store.workflows["wf1"] = &WorkflowDefinition{
		ID:          "wf1",
		OwnerID:     "user_alice",
		Name:        "采购审批",
		Description: "Publish me via VersionManager.Approve",
	}

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:            "v1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPendingReview,
		SubmittedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Graph:         WorkflowGraph{Nodes: []WorkflowNode{{ID: "n1", Type: NodeTrigger}}},
	}

	if err := vm.Approve(ctx, "v1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// The version transitioned to published.
	if store.versions["v1"].Status != VersionPublished {
		t.Fatalf("version status = %q, want published", store.versions["v1"].Status)
	}

	// The workflow now appears in the capability market with an active status
	// and the correct current version — same invariant as ApproveSubmission.
	cap, err := capSvc.Get(ctx, workflowCapabilityID("wf1"))
	if err != nil {
		t.Fatalf("capability not registered in market: %v", err)
	}
	if cap.Status != "active" {
		t.Errorf("capability status = %q, want active", cap.Status)
	}
	if cap.CapabilityType != "approval_workflow" {
		t.Errorf("capability type = %q, want approval_workflow", cap.CapabilityType)
	}
	if cap.CurrentVersionKey != "approval_workflow:wf1:1.0.0" {
		t.Errorf("current_version_key = %q, want approval_workflow:wf1:1.0.0", cap.CurrentVersionKey)
	}
}

// TestVersionManager_Approve_MarketFailureKeepsPending verifies the rollback
// invariant: when capability-market registration fails, Approve does not
// transition the version (it remains pending_review for retry), mirroring
// AdminReviewService.ApproveSubmission.
func TestVersionManager_Approve_MarketFailureKeepsPending(t *testing.T) {
	store := newMockStoreForAdmin()
	// A zero-value *capability.Service has no DB and fails UpsertCapability,
	// simulating a market-registration failure.
	vm := NewVersionManager(store).WithCapabilityService(&capability.Service{})
	ctx := context.Background()

	store.workflows["wf1"] = &WorkflowDefinition{
		ID:      "wf1",
		OwnerID: "user_alice",
		Name:    "采购审批",
	}

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:            "v1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPendingReview,
		SubmittedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Graph:         WorkflowGraph{Nodes: []WorkflowNode{{ID: "n1", Type: NodeTrigger}}},
	}

	if err := vm.Approve(ctx, "v1"); err == nil {
		t.Fatal("expected market registration error, got nil")
	}
	if store.versions["v1"].Status != VersionPendingReview {
		t.Fatalf("version status = %q, want pending_review (no mutation on market failure)", store.versions["v1"].Status)
	}
	if len(store.statusLog) != 0 {
		t.Fatalf("status log = %+v, want no status mutation", store.statusLog)
	}
}

// TestVersionManager_Approve_NoCapabilityServicePreservesOriginalBehavior
// confirms the preservation guarantee: when no capability market is
// configured, Approve retains its original publish-and-supersede-only
// behavior (status transition succeeds without touching a market).
func TestVersionManager_Approve_NoCapabilityServicePreservesOriginalBehavior(t *testing.T) {
	store := newMockStoreForAdmin()
	vm := NewVersionManager(store) // no capability service
	ctx := context.Background()

	store.workflows["wf1"] = &WorkflowDefinition{
		ID:      "wf1",
		OwnerID: "user_alice",
		Name:    "采购审批",
	}

	now := time.Now().UTC()
	store.versions["v1"] = &WorkflowVersion{
		ID:            "v1",
		WorkflowID:    "wf1",
		VersionNumber: "1.0.0",
		Status:        VersionPendingReview,
		SubmittedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Graph:         WorkflowGraph{Nodes: []WorkflowNode{{ID: "n1", Type: NodeTrigger}}},
	}

	if err := vm.Approve(ctx, "v1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if store.versions["v1"].Status != VersionPublished {
		t.Fatalf("version status = %q, want published", store.versions["v1"].Status)
	}
}
