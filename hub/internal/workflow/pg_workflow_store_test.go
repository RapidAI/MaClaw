package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	storepkg "github.com/RapidAI/CodeClaw/hub/internal/store"

	_ "modernc.org/sqlite"
)

// newTestPGStore creates a PGWorkflowStore backed by an in-memory SQLite DB
// with PostgreSQL-compatible schema. SQLite supports $N placeholders when using
// modernc.org/sqlite, so we can test the PG store logic without a real PostgreSQL.
func newTestPGStore(t *testing.T) *PGWorkflowStore {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Create tables matching the PostgreSQL schema
	schema := `
		CREATE TABLE IF NOT EXISTS workflow_definitions (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL DEFAULT 'tenant_default',
			owner_id    TEXT NOT NULL,
			name        TEXT NOT NULL,
			description TEXT DEFAULT '',
			created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_workflow_def_owner ON workflow_definitions(owner_id);
		CREATE INDEX IF NOT EXISTS idx_workflow_def_tenant_owner ON workflow_definitions(tenant_id, owner_id);

		CREATE TABLE IF NOT EXISTS workflow_versions (
			id               TEXT PRIMARY KEY,
			workflow_id      TEXT NOT NULL REFERENCES workflow_definitions(id),
			version_number   TEXT NOT NULL,
			status           TEXT NOT NULL DEFAULT 'draft',
			graph_json       TEXT NOT NULL,
			submitted_at     TIMESTAMP,
			published_at     TIMESTAMP,
			rejection_reason TEXT DEFAULT '',
			created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_wf_ver_workflow ON workflow_versions(workflow_id);
		CREATE INDEX IF NOT EXISTS idx_wf_ver_status ON workflow_versions(status);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_wf_ver_published ON workflow_versions(workflow_id)
			WHERE status = 'published';

		CREATE TABLE IF NOT EXISTS workflow_instances (
			id              TEXT PRIMARY KEY,
			tenant_id       TEXT NOT NULL DEFAULT 'tenant_default',
			workflow_id     TEXT NOT NULL,
			version_id      TEXT NOT NULL REFERENCES workflow_versions(id),
			status          TEXT NOT NULL DEFAULT 'running',
			current_node_id TEXT DEFAULT '',
			instance_data   TEXT DEFAULT '{}',
			trigger_data    TEXT DEFAULT '',
			row_version     INTEGER NOT NULL DEFAULT 0,
			created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at    TIMESTAMP
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return NewPGWorkflowStore(db)
}

// ---------------------------------------------------------------------------
// Workflow Definition Tests
// ---------------------------------------------------------------------------

func TestPGWorkflowStore_CreateAndGetWorkflow(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	def := &WorkflowDefinition{
		ID:          "wf_001",
		OwnerID:     "user_abc",
		Name:        "Purchase Approval",
		Description: "Purchase approval workflow",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	got, err := store.GetWorkflow(ctx, "wf_001")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got == nil {
		t.Fatal("GetWorkflow returned nil")
	}
	if got.ID != def.ID || got.OwnerID != def.OwnerID || got.Name != def.Name || got.Description != def.Description {
		t.Fatalf("GetWorkflow mismatch: got %+v", got)
	}
}

func TestPGWorkflowStore_GetWorkflow_NotFound(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	got, err := store.GetWorkflow(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for nonexistent workflow, got %+v", got)
	}
}

func TestPGWorkflowStore_ListWorkflows(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create workflows for two different owners
	for i, id := range []string{"wf_1", "wf_2", "wf_3"} {
		owner := "user_a"
		if i == 2 {
			owner = "user_b"
		}
		def := &WorkflowDefinition{
			ID:        id,
			OwnerID:   owner,
			Name:      "Workflow " + id,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if err := store.CreateWorkflow(ctx, def); err != nil {
			t.Fatalf("CreateWorkflow %s: %v", id, err)
		}
	}

	// List for user_a -should get 2
	defs, err := store.ListWorkflows(ctx, "user_a")
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 workflows for user_a, got %d", len(defs))
	}

	// List for user_b -should get 1
	defs, err = store.ListWorkflows(ctx, "user_b")
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 workflow for user_b, got %d", len(defs))
	}
}

func TestPGWorkflowStore_UpdateWorkflow(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	def := &WorkflowDefinition{ID: "wf_update", OwnerID: "user_a", Name: "Old", Description: "Old desc", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	def.Name = "New"
	def.Description = "New desc"
	def.UpdatedAt = now.Add(time.Hour)
	if err := store.UpdateWorkflow(ctx, def); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}

	got, err := store.GetWorkflow(ctx, def.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.Name != "New" || got.Description != "New desc" {
		t.Fatalf("workflow not updated: %+v", got)
	}
}

func TestPGWorkflowStore_DeleteWorkflow(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	def := &WorkflowDefinition{ID: "wf_delete", OwnerID: "user_a", Name: "Delete", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	ver := &WorkflowVersion{ID: "ver_delete", WorkflowID: def.ID, VersionNumber: "0.1.0", Status: VersionDraft, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	if err := store.DeleteWorkflow(ctx, def.ID); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	got, err := store.GetWorkflow(ctx, def.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got != nil {
		t.Fatalf("expected workflow deleted, got %+v", got)
	}
}

func TestPGWorkflowStore_DeleteWorkflowRejectsRunningInstance(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	def := &WorkflowDefinition{ID: "wf_running", OwnerID: "user_a", Name: "Running", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	ver := &WorkflowVersion{ID: "ver_running", WorkflowID: def.ID, VersionNumber: "0.1.0", Status: VersionPublished, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO workflow_instances (id, workflow_id, version_id, status) VALUES ($1, $2, $3, $4)`, "inst_running", def.ID, ver.ID, string(InstanceRunning)); err != nil {
		t.Fatalf("insert instance: %v", err)
	}

	if err := store.DeleteWorkflow(ctx, def.ID); err == nil {
		t.Fatal("expected running instance to block workflow deletion")
	}
}

func TestPGWorkflowStore_DeleteWorkflowRejectsPublishedVersion(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	def := &WorkflowDefinition{ID: "wf_published_delete", OwnerID: "user_a", Name: "Published", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	ver := &WorkflowVersion{ID: "ver_published_delete", WorkflowID: def.ID, VersionNumber: "1.0.0", Status: VersionPublished, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	if err := store.DeleteWorkflow(ctx, def.ID); err == nil {
		t.Fatal("expected published version to block workflow deletion")
	}
	got, err := store.GetWorkflow(ctx, def.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got == nil {
		t.Fatal("published workflow should not have been deleted")
	}
}

func TestPGWorkflowStore_DeleteWorkflowRejectsPreviouslyPublishedVersion(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for _, status := range []VersionStatus{VersionSuperseded, VersionUnpublished} {
		def := &WorkflowDefinition{ID: "wf_" + string(status) + "_delete", OwnerID: "user_a", Name: "Published History", CreatedAt: now, UpdatedAt: now}
		if err := store.CreateWorkflow(ctx, def); err != nil {
			t.Fatalf("CreateWorkflow %s: %v", status, err)
		}
		ver := &WorkflowVersion{ID: "ver_" + string(status) + "_delete", WorkflowID: def.ID, VersionNumber: "1.0.0", Status: status, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}
		if err := store.CreateVersion(ctx, ver); err != nil {
			t.Fatalf("CreateVersion %s: %v", status, err)
		}

		if err := store.DeleteWorkflow(ctx, def.ID); err == nil {
			t.Fatalf("expected %s version to block workflow deletion", status)
		}
		got, err := store.GetWorkflow(ctx, def.ID)
		if err != nil {
			t.Fatalf("GetWorkflow %s: %v", status, err)
		}
		if got == nil {
			t.Fatalf("%s workflow should not have been deleted", status)
		}
	}
}

// ---------------------------------------------------------------------------
// Workflow Version Tests
// ---------------------------------------------------------------------------

func TestPGWorkflowStore_CreateAndGetVersion(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	// Create parent workflow
	def := &WorkflowDefinition{ID: "wf_v", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "n1", Type: NodeTrigger, Label: "Start", Position: Position{X: 100, Y: 200}},
		},
		Edges: []WorkflowEdge{},
	}

	ver := &WorkflowVersion{
		ID:            "ver_001",
		WorkflowID:    "wf_v",
		VersionNumber: "1.0.0",
		Status:        VersionDraft,
		Graph:         graph,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := store.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	got, err := store.GetVersion(ctx, "ver_001")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got == nil {
		t.Fatal("GetVersion returned nil")
	}
	if got.ID != ver.ID || got.WorkflowID != ver.WorkflowID || got.VersionNumber != ver.VersionNumber {
		t.Fatalf("GetVersion mismatch: got %+v", got)
	}
	if got.Status != VersionDraft {
		t.Fatalf("expected status draft, got %s", got.Status)
	}
	if len(got.Graph.Nodes) != 1 || got.Graph.Nodes[0].ID != "n1" {
		t.Fatalf("graph mismatch: got %+v", got.Graph)
	}
}

func TestPGWorkflowStore_GetVersion_NotFound(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	got, err := store.GetVersion(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for nonexistent version, got %+v", got)
	}
}

func TestPGWorkflowStore_GetPublishedVersion(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_pub", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Create a draft version
	draftVer := &WorkflowVersion{
		ID: "ver_draft", WorkflowID: "wf_pub", VersionNumber: "1.0.0",
		Status: VersionDraft, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateVersion(ctx, draftVer); err != nil {
		t.Fatalf("CreateVersion draft: %v", err)
	}

	// No published version yet
	got, err := store.GetPublishedVersion(ctx, "wf_pub")
	if err != nil {
		t.Fatalf("GetPublishedVersion: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when no published version, got %+v", got)
	}

	// Create a published version
	pubTime := now.Add(time.Hour)
	pubVer := &WorkflowVersion{
		ID: "ver_pub", WorkflowID: "wf_pub", VersionNumber: "1.1.0",
		Status: VersionPublished, Graph: WorkflowGraph{}, PublishedAt: &pubTime,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateVersion(ctx, pubVer); err != nil {
		t.Fatalf("CreateVersion published: %v", err)
	}

	got, err = store.GetPublishedVersion(ctx, "wf_pub")
	if err != nil {
		t.Fatalf("GetPublishedVersion: %v", err)
	}
	if got == nil {
		t.Fatal("GetPublishedVersion returned nil")
	}
	if got.ID != "ver_pub" || got.Status != VersionPublished {
		t.Fatalf("GetPublishedVersion mismatch: got %+v", got)
	}
}

func TestPGWorkflowStore_UpdateVersionStatus(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_st", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	ver := &WorkflowVersion{
		ID: "ver_st", WorkflowID: "wf_st", VersionNumber: "1.0.0",
		Status: VersionDraft, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	// Transition to pending_review
	if err := store.UpdateVersionStatus(ctx, "ver_st", VersionPendingReview, ""); err != nil {
		t.Fatalf("UpdateVersionStatus to pending_review: %v", err)
	}
	got, _ := store.GetVersion(ctx, "ver_st")
	if got.Status != VersionPendingReview {
		t.Fatalf("expected pending_review, got %s", got.Status)
	}
	if got.SubmittedAt == nil {
		t.Fatal("expected SubmittedAt to be set")
	}

	// Transition to published
	if err := store.UpdateVersionStatus(ctx, "ver_st", VersionPublished, ""); err != nil {
		t.Fatalf("UpdateVersionStatus to published: %v", err)
	}
	got, _ = store.GetVersion(ctx, "ver_st")
	if got.Status != VersionPublished {
		t.Fatalf("expected published, got %s", got.Status)
	}
	if got.PublishedAt == nil {
		t.Fatal("expected PublishedAt to be set")
	}
}

func TestPGWorkflowStore_UpdateVersionStatus_Rejected(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_rej", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	ver := &WorkflowVersion{
		ID: "ver_rej", WorkflowID: "wf_rej", VersionNumber: "1.0.0",
		Status: VersionPendingReview, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	reason := "Missing approval node configuration"
	if err := store.UpdateVersionStatus(ctx, "ver_rej", VersionRejected, reason); err != nil {
		t.Fatalf("UpdateVersionStatus to rejected: %v", err)
	}

	got, _ := store.GetVersion(ctx, "ver_rej")
	if got.Status != VersionRejected {
		t.Fatalf("expected rejected, got %s", got.Status)
	}
	if got.RejectionReason != reason {
		t.Fatalf("expected rejection reason %q, got %q", reason, got.RejectionReason)
	}
}

func TestPGWorkflowStore_ListVersions(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_lv", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Create 3 versions
	for i, id := range []string{"v1", "v2", "v3"} {
		ver := &WorkflowVersion{
			ID: id, WorkflowID: "wf_lv", VersionNumber: "1." + string(rune('0'+i)) + ".0",
			Status: VersionDraft, Graph: WorkflowGraph{},
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if err := store.CreateVersion(ctx, ver); err != nil {
			t.Fatalf("CreateVersion %s: %v", id, err)
		}
	}

	versions, err := store.ListVersions(ctx, "wf_lv")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	// Should be ordered by created_at DESC
	if versions[0].ID != "v3" {
		t.Fatalf("expected first version to be v3 (newest), got %s", versions[0].ID)
	}
}

// ---------------------------------------------------------------------------
// ListPendingReviews Tests
// ---------------------------------------------------------------------------

func TestPGWorkflowStore_ListPendingReviews(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_pr", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Create versions with different statuses
	statuses := []VersionStatus{VersionPendingReview, VersionDraft, VersionPendingReview, VersionPublished, VersionPendingReview}
	for i, status := range statuses {
		subAt := now.Add(time.Duration(i) * time.Minute)
		ver := &WorkflowVersion{
			ID: "vpr_" + string(rune('a'+i)), WorkflowID: "wf_pr",
			VersionNumber: "1.0." + string(rune('0'+i)),
			Status:        status, Graph: WorkflowGraph{},
			SubmittedAt: &subAt,
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		if err := store.CreateVersion(ctx, ver); err != nil {
			t.Fatalf("CreateVersion %d: %v", i, err)
		}
	}

	// Should return 3 pending_review versions
	versions, total, err := store.ListPendingReviews(ctx, 1, 50)
	if err != nil {
		t.Fatalf("ListPendingReviews: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	// Should be sorted by submitted_at ASC (oldest first)
	if versions[0].ID != "vpr_a" {
		t.Fatalf("expected first to be vpr_a (oldest), got %s", versions[0].ID)
	}
}

func TestPGWorkflowStore_ListPendingReviews_Pagination(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_pag", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Create 5 pending_review versions
	for i := 0; i < 5; i++ {
		subAt := now.Add(time.Duration(i) * time.Minute)
		ver := &WorkflowVersion{
			ID: "vpag_" + string(rune('a'+i)), WorkflowID: "wf_pag",
			VersionNumber: "1.0." + string(rune('0'+i)),
			Status:        VersionPendingReview, Graph: WorkflowGraph{},
			SubmittedAt: &subAt,
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		if err := store.CreateVersion(ctx, ver); err != nil {
			t.Fatalf("CreateVersion %d: %v", i, err)
		}
	}

	// Page 1, size 2
	versions, total, err := store.ListPendingReviews(ctx, 1, 2)
	if err != nil {
		t.Fatalf("ListPendingReviews page 1: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected total=5, got %d", total)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions on page 1, got %d", len(versions))
	}

	// Page 2, size 2
	versions, total, err = store.ListPendingReviews(ctx, 2, 2)
	if err != nil {
		t.Fatalf("ListPendingReviews page 2: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected total=5, got %d", total)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions on page 2, got %d", len(versions))
	}

	// Page 3, size 2 -should get 1
	versions, total, err = store.ListPendingReviews(ctx, 3, 2)
	if err != nil {
		t.Fatalf("ListPendingReviews page 3: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected total=5, got %d", total)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version on page 3, got %d", len(versions))
	}
}

func TestPGWorkflowStore_ListPendingReviews_DefaultPageSize(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	// With no pending reviews, should return empty
	versions, total, err := store.ListPendingReviews(ctx, 1, 0)
	if err != nil {
		t.Fatalf("ListPendingReviews: %v", err)
	}
	if total != 0 || versions != nil {
		t.Fatalf("expected empty result, got total=%d versions=%v", total, versions)
	}
}

func TestPGWorkflowStore_VersionGraphSerialization(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_gs", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Create a complex graph
	approvalConfig, _ := json.Marshal(ApprovalNodeConfig{
		ApproverIDs:  []string{"ve_1", "ve_2"},
		Mode:         ModeCountersign,
		TimeoutHours: 24,
	})

	graph := WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "n1", Type: NodeTrigger, Label: "Start", Position: Position{X: 0, Y: 0}},
			{ID: "n2", Type: NodeApproval, Label: "Manager Approval", Position: Position{X: 200, Y: 100}, Config: approvalConfig},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", SourceID: "n1", TargetID: "n2", Label: "submit"},
		},
	}

	ver := &WorkflowVersion{
		ID: "ver_gs", WorkflowID: "wf_gs", VersionNumber: "1.0.0",
		Status: VersionDraft, Graph: graph, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	got, err := store.GetVersion(ctx, "ver_gs")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got == nil {
		t.Fatal("GetVersion returned nil")
	}

	// Verify graph round-trip
	if len(got.Graph.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(got.Graph.Nodes))
	}
	if len(got.Graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(got.Graph.Edges))
	}
	if got.Graph.Nodes[1].Type != NodeApproval {
		t.Fatalf("expected node 2 type approval, got %s", got.Graph.Nodes[1].Type)
	}
	if got.Graph.Edges[0].SourceID != "n1" || got.Graph.Edges[0].TargetID != "n2" {
		t.Fatalf("edge mismatch: %+v", got.Graph.Edges[0])
	}

	// Verify config round-trip
	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(got.Graph.Nodes[1].Config, &cfg); err != nil {
		t.Fatalf("unmarshal approval config: %v", err)
	}
	if cfg.Mode != ModeCountersign || cfg.TimeoutHours != 24 || len(cfg.ApproverIDs) != 2 {
		t.Fatalf("approval config mismatch: %+v", cfg)
	}
}

func TestPGWorkflowStore_PublishedUniqueConstraint(t *testing.T) {
	store := newTestPGStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_uc", OwnerID: "user_1", Name: "Test", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// First published version -should succeed
	ver1 := &WorkflowVersion{
		ID: "ver_uc1", WorkflowID: "wf_uc", VersionNumber: "1.0.0",
		Status: VersionPublished, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateVersion(ctx, ver1); err != nil {
		t.Fatalf("CreateVersion first published: %v", err)
	}

	// Second published version for same workflow -should fail (unique partial index)
	ver2 := &WorkflowVersion{
		ID: "ver_uc2", WorkflowID: "wf_uc", VersionNumber: "2.0.0",
		Status: VersionPublished, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now,
	}
	err := store.CreateVersion(ctx, ver2)
	if err == nil {
		t.Fatal("expected unique constraint violation for second published version")
	}

	// Draft version for same workflow -should succeed
	ver3 := &WorkflowVersion{
		ID: "ver_uc3", WorkflowID: "wf_uc", VersionNumber: "2.0.0",
		Status: VersionDraft, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateVersion(ctx, ver3); err != nil {
		t.Fatalf("CreateVersion draft should succeed: %v", err)
	}
}

func TestPGWorkflowStore_TenantIsolation(t *testing.T) {
	wfStore := newTestPGStore(t)
	ctxA := storepkg.WithTenant(context.Background(), "tenant_a")
	ctxB := storepkg.WithTenant(context.Background(), "tenant_b")
	now := time.Now().UTC().Truncate(time.Second)

	if err := wfStore.CreateWorkflow(ctxA, &WorkflowDefinition{ID: "wf_a", OwnerID: "owner_1", Name: "Tenant A", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant A workflow: %v", err)
	}
	if err := wfStore.CreateWorkflow(ctxB, &WorkflowDefinition{ID: "wf_b", OwnerID: "owner_1", Name: "Tenant B", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant B workflow: %v", err)
	}

	defsA, err := wfStore.ListWorkflows(ctxA, "owner_1")
	if err != nil {
		t.Fatalf("list tenant A workflows: %v", err)
	}
	if len(defsA) != 1 || defsA[0].ID != "wf_a" || defsA[0].TenantID != "tenant_a" {
		t.Fatalf("tenant A list = %+v, want only wf_a", defsA)
	}
	if got, err := wfStore.GetWorkflow(ctxA, "wf_b"); err != nil || got != nil {
		t.Fatalf("tenant A got tenant B workflow: got=%+v err=%v", got, err)
	}

	if err := wfStore.CreateVersion(ctxA, &WorkflowVersion{ID: "ver_a", WorkflowID: "wf_a", VersionNumber: "1.0.0", Status: VersionPendingReview, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant A version: %v", err)
	}
	if err := wfStore.CreateVersion(ctxB, &WorkflowVersion{ID: "ver_b", WorkflowID: "wf_b", VersionNumber: "1.0.0", Status: VersionPendingReview, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant B version: %v", err)
	}
	pendingA, totalA, err := wfStore.ListPendingReviews(ctxA, 1, 50)
	if err != nil {
		t.Fatalf("list tenant A reviews: %v", err)
	}
	if totalA != 1 || len(pendingA) != 1 || pendingA[0].ID != "ver_a" {
		t.Fatalf("tenant A pending = total %d items %+v, want ver_a", totalA, pendingA)
	}
	if got, err := wfStore.GetVersion(ctxA, "ver_b"); err != nil || got != nil {
		t.Fatalf("tenant A got tenant B version: got=%+v err=%v", got, err)
	}
}

func TestPGWorkflowStore_DeleteWorkflowIgnoresOtherTenantRunningInstance(t *testing.T) {
	wfStore := newTestPGStore(t)
	ctxA := storepkg.WithTenant(context.Background(), "tenant_a")
	now := time.Now().UTC().Truncate(time.Second)

	def := &WorkflowDefinition{ID: "wf_cross_running", OwnerID: "owner_a", Name: "Cross", CreatedAt: now, UpdatedAt: now}
	if err := wfStore.CreateWorkflow(ctxA, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	ver := &WorkflowVersion{ID: "ver_cross_running", WorkflowID: def.ID, VersionNumber: "0.1.0", Status: VersionDraft, Graph: WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}
	if err := wfStore.CreateVersion(ctxA, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if _, err := wfStore.db.ExecContext(context.Background(), `INSERT INTO workflow_instances (id, tenant_id, workflow_id, version_id, status) VALUES ($1, $2, $3, $4, $5)`, "inst_other_tenant", "tenant_b", def.ID, ver.ID, string(InstanceRunning)); err != nil {
		t.Fatalf("insert other tenant instance: %v", err)
	}

	if err := wfStore.DeleteWorkflow(ctxA, def.ID); err != nil {
		t.Fatalf("DeleteWorkflow should ignore other tenant running instance: %v", err)
	}
	got, err := wfStore.GetWorkflow(ctxA, def.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got != nil {
		t.Fatalf("expected workflow deleted, got %+v", got)
	}
}
