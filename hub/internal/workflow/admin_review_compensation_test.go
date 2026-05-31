package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/capability"
)

type failingStatusStore struct {
	versions      map[string]*WorkflowVersion
	workflows     map[string]*WorkflowDefinition
	statusErrByID map[string]error
}

func newFailingStatusStore() *failingStatusStore {
	return &failingStatusStore{
		versions:      map[string]*WorkflowVersion{},
		workflows:     map[string]*WorkflowDefinition{},
		statusErrByID: map[string]error{},
	}
}

func (s *failingStatusStore) CreateWorkflow(_ context.Context, def *WorkflowDefinition) error {
	s.workflows[def.ID] = def
	return nil
}

func (s *failingStatusStore) GetWorkflow(_ context.Context, id string) (*WorkflowDefinition, error) {
	return s.workflows[id], nil
}

func (s *failingStatusStore) ListWorkflows(_ context.Context, _ string) ([]WorkflowDefinition, error) {
	return nil, nil
}

func (s *failingStatusStore) CreateVersion(_ context.Context, ver *WorkflowVersion) error {
	s.versions[ver.ID] = ver
	return nil
}

func (s *failingStatusStore) UpdateVersion(_ context.Context, ver *WorkflowVersion) error {
	existing, ok := s.versions[ver.ID]
	if !ok {
		return nil
	}
	existing.Graph = ver.Graph
	existing.VersionNumber = ver.VersionNumber
	existing.UpdatedAt = ver.UpdatedAt
	return nil
}

func (s *failingStatusStore) GetVersion(_ context.Context, id string) (*WorkflowVersion, error) {
	return s.versions[id], nil
}

func (s *failingStatusStore) GetPublishedVersion(_ context.Context, workflowID string) (*WorkflowVersion, error) {
	for _, ver := range s.versions {
		if ver.WorkflowID == workflowID && ver.Status == VersionPublished {
			return ver, nil
		}
	}
	return nil, nil
}

func (s *failingStatusStore) UpdateVersionStatus(_ context.Context, id string, status VersionStatus, reason string) error {
	if err := s.statusErrByID[id]; err != nil {
		return err
	}
	ver := s.versions[id]
	if ver == nil {
		return errors.New("version not found")
	}
	ver.Status = status
	ver.RejectionReason = reason
	return nil
}

func (s *failingStatusStore) ListVersions(_ context.Context, _ string) ([]WorkflowVersion, error) {
	return nil, nil
}

func (s *failingStatusStore) ListPendingReviews(_ context.Context, _ int, _ int) ([]WorkflowVersion, int, error) {
	return nil, 0, nil
}

func TestAdminReview_ApproveSubmission_PublishFailureDeactivatesNewMarketCapability(t *testing.T) {
	store := newFailingStatusStore()
	db := newAdminReviewCapabilityDB(t)
	svc := NewAdminReviewService(store, capability.NewService(db))
	ctx := context.Background()
	now := time.Now().UTC()

	store.workflows["wf1"] = &WorkflowDefinition{ID: "wf1", OwnerID: "user_alice", Name: "Workflow"}
	store.versions["v1"] = &WorkflowVersion{ID: "v1", WorkflowID: "wf1", VersionNumber: "1.0.0", Status: VersionPendingReview, SubmittedAt: &now, CreatedAt: now, UpdatedAt: now}
	store.statusErrByID["v1"] = errors.New("publish failed")

	if err := svc.ApproveSubmission(ctx, "v1"); err == nil {
		t.Fatal("expected publish error")
	}
	if got := store.versions["v1"].Status; got != VersionPendingReview {
		t.Fatalf("status = %q, want pending_review", got)
	}
	cap, err := capability.NewService(db).Get(ctx, workflowCapabilityID("wf1"))
	if err != nil {
		t.Fatalf("get capability: %v", err)
	}
	if cap.Status != "inactive" {
		t.Fatalf("capability status = %q, want inactive", cap.Status)
	}
}

func TestAdminReview_ApproveSubmission_PublishFailureRestoresPreviousMarketVersion(t *testing.T) {
	store := newFailingStatusStore()
	db := newAdminReviewCapabilityDB(t)
	svc := NewAdminReviewService(store, capability.NewService(db))
	ctx := context.Background()
	now := time.Now().UTC()

	store.workflows["wf1"] = &WorkflowDefinition{ID: "wf1", OwnerID: "user_alice", Name: "Workflow"}
	store.versions["old"] = &WorkflowVersion{ID: "old", WorkflowID: "wf1", VersionNumber: "1.0.0", Status: VersionPublished, PublishedAt: &now, CreatedAt: now, UpdatedAt: now}
	store.versions["new"] = &WorkflowVersion{ID: "new", WorkflowID: "wf1", VersionNumber: "1.1.0", Status: VersionPendingReview, SubmittedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := svc.registerInCapabilityMarket(ctx, store.versions["old"]); err != nil {
		t.Fatalf("seed market: %v", err)
	}
	store.statusErrByID["new"] = errors.New("publish failed")

	if err := svc.ApproveSubmission(ctx, "new"); err == nil {
		t.Fatal("expected publish error")
	}
	if got := store.versions["old"].Status; got != VersionPublished {
		t.Fatalf("old status = %q, want published", got)
	}
	if got := store.versions["new"].Status; got != VersionPendingReview {
		t.Fatalf("new status = %q, want pending_review", got)
	}
	cap, err := capability.NewService(db).Get(ctx, workflowCapabilityID("wf1"))
	if err != nil {
		t.Fatalf("get capability: %v", err)
	}
	if cap.CurrentVersionKey != "approval_workflow:wf1:1.0.0" {
		t.Fatalf("current_version_key = %q", cap.CurrentVersionKey)
	}
	metadata := capabilityMetadata(t, db, workflowCapabilityID("wf1"))
	if metadata["published_at"] != now.Format(time.RFC3339) {
		t.Fatalf("published_at = %q, want previous publish time", metadata["published_at"])
	}
	if metadata["workflow_id"] != "wf1" || metadata["version_id"] != "old" {
		t.Fatalf("unexpected workflow metadata: %#v", metadata)
	}
}

func capabilityMetadata(t *testing.T, db *sql.DB, id string) map[string]any {
	t.Helper()
	var raw string
	if err := db.QueryRow(`SELECT metadata_json FROM capabilities WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatalf("query metadata: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	return metadata
}
