package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	storepkg "github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
	_ "modernc.org/sqlite"
)

func setupAuditStoreTestDB(t *testing.T) *sql.DB {
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

func TestAuditStore_TenantIsolation(t *testing.T) {
	db := setupAuditStoreTestDB(t)
	store := NewAuditStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	ctxA := storepkg.WithTenant(context.Background(), "tenant_a")
	ctxB := storepkg.WithTenant(context.Background(), "tenant_b")

	entryA := &workflow.AuditEntry{ID: "audit-tenant-a", InstanceID: "inst-shared", EventType: "decision", ActorID: "approver_1", Decision: "approve", Timestamp: now}
	entryB := &workflow.AuditEntry{ID: "audit-tenant-b", InstanceID: "inst-shared", EventType: "decision", ActorID: "approver_1", Decision: "approve", Timestamp: now.Add(time.Second)}
	if err := store.Append(ctxA, entryA); err != nil {
		t.Fatalf("Append tenant A: %v", err)
	}
	if err := store.Append(ctxB, entryB); err != nil {
		t.Fatalf("Append tenant B: %v", err)
	}

	entries, total, err := store.QueryByInstance(ctxA, "inst-shared", 0, 10)
	if err != nil {
		t.Fatalf("QueryByInstance tenant A: %v", err)
	}
	if total != 1 || len(entries) != 1 || entries[0].ID != entryA.ID || entries[0].TenantID != "tenant_a" {
		t.Fatalf("tenant A instance query leaked entries: total=%d entries=%+v", total, entries)
	}

	entries, total, err = store.QueryByApprover(ctxB, "approver_1", 0, 10)
	if err != nil {
		t.Fatalf("QueryByApprover tenant B: %v", err)
	}
	if total != 1 || len(entries) != 1 || entries[0].ID != entryB.ID || entries[0].TenantID != "tenant_b" {
		t.Fatalf("tenant B approver query leaked entries: total=%d entries=%+v", total, entries)
	}

	entries, total, err = store.QueryByDecision(ctxA, "approve", 0, 10)
	if err != nil {
		t.Fatalf("QueryByDecision tenant A: %v", err)
	}
	if total != 1 || len(entries) != 1 || entries[0].ID != entryA.ID {
		t.Fatalf("tenant A decision query leaked entries: total=%d entries=%+v", total, entries)
	}
}
