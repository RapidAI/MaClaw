package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
	_ "modernc.org/sqlite"
)

func setupWorkflowStoreTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestWorkflowStoreSQLite_UpdateWorkflow(t *testing.T) {
	db := setupWorkflowStoreTestDB(t)
	store := NewWorkflowStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	def := &workflow.WorkflowDefinition{ID: "wf_update", OwnerID: "owner_a", Name: "Old", Description: "Old desc", CreatedAt: now, UpdatedAt: now}
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

func TestWorkflowStoreSQLite_DeleteWorkflow(t *testing.T) {
	db := setupWorkflowStoreTestDB(t)
	store := NewWorkflowStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	def := &workflow.WorkflowDefinition{ID: "wf_delete", OwnerID: "owner_a", Name: "Delete", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	ver := &workflow.WorkflowVersion{ID: "ver_delete", WorkflowID: def.ID, VersionNumber: "0.1.0", Status: workflow.VersionDraft, Graph: workflow.WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}
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

func TestWorkflowStoreSQLite_DeleteWorkflowRejectsRunningInstance(t *testing.T) {
	db := setupWorkflowStoreTestDB(t)
	store := NewWorkflowStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	def := &workflow.WorkflowDefinition{ID: "wf_running", OwnerID: "owner_a", Name: "Running", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateWorkflow(ctx, def); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	ver := &workflow.WorkflowVersion{ID: "ver_running", WorkflowID: def.ID, VersionNumber: "0.1.0", Status: workflow.VersionPublished, Graph: workflow.WorkflowGraph{}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_instances (id, workflow_id, version_id, status) VALUES (?, ?, ?, ?)`, "inst_running", def.ID, ver.ID, string(workflow.InstanceRunning)); err != nil {
		t.Fatalf("insert instance: %v", err)
	}

	if err := store.DeleteWorkflow(ctx, def.ID); err == nil {
		t.Fatal("expected running instance to block workflow deletion")
	}
}
