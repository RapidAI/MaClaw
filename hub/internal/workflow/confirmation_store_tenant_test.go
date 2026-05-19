package workflow

import (
	"context"
	"database/sql"
	"testing"
	"time"

	storepkg "github.com/RapidAI/CodeClaw/hub/internal/store"
	_ "modernc.org/sqlite"
)

func setupConfirmationStoreTenantDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE confirmations (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			instance_id TEXT NOT NULL,
			recipient_id TEXT NOT NULL,
			type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			notes TEXT DEFAULT '',
			timeout_hours INTEGER NOT NULL DEFAULT 48,
			max_reminders INTEGER NOT NULL DEFAULT 3,
			reminders_sent INTEGER NOT NULL DEFAULT 0,
			reminder_interval_hours INTEGER NOT NULL DEFAULT 24,
			last_reminder_at TEXT,
			confirmed_at TEXT,
			auto_closed_at TEXT,
			auto_close_reason TEXT DEFAULT '',
			created_at TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create confirmations: %v", err)
	}
	return db
}

func TestPgConfirmationStore_TenantIsolation(t *testing.T) {
	db := setupConfirmationStoreTenantDB(t)
	store := NewPgConfirmationStore(db)
	ctxA := storepkg.WithTenant(context.Background(), "tenant_a")
	ctxB := storepkg.WithTenant(context.Background(), "tenant_b")
	created := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)

	confA := &Confirmation{ID: "conf-a", InstanceID: "inst-shared", RecipientID: "user_1", Type: ConfirmTypeExecutor, Status: ConfirmPending, TimeoutHours: 1, MaxReminders: 3, ReminderIntervalHours: 1, CreatedAt: created}
	confB := &Confirmation{ID: "conf-b", InstanceID: "inst-shared", RecipientID: "user_1", Type: ConfirmTypeNotifier, Status: ConfirmPending, TimeoutHours: 1, MaxReminders: 3, ReminderIntervalHours: 1, CreatedAt: created}
	if err := store.Create(ctxA, confA); err != nil {
		t.Fatalf("Create tenant A: %v", err)
	}
	if err := store.Create(ctxB, confB); err != nil {
		t.Fatalf("Create tenant B: %v", err)
	}

	if got, err := store.Get(ctxA, confB.ID); err != nil || got != nil {
		t.Fatalf("tenant A should not read tenant B confirmation: got=%+v err=%v", got, err)
	}
	pendingA, err := store.ListPending(ctxA, "user_1")
	if err != nil {
		t.Fatalf("ListPending tenant A: %v", err)
	}
	if len(pendingA) != 1 || pendingA[0].ID != confA.ID || pendingA[0].TenantID != "tenant_a" {
		t.Fatalf("tenant A pending leaked: %+v", pendingA)
	}
	byInstanceB, err := store.ListByInstance(ctxB, "inst-shared")
	if err != nil {
		t.Fatalf("ListByInstance tenant B: %v", err)
	}
	if len(byInstanceB) != 1 || byInstanceB[0].ID != confB.ID || byInstanceB[0].TenantID != "tenant_b" {
		t.Fatalf("tenant B by-instance leaked: %+v", byInstanceB)
	}

	if err := store.UpdateStatus(ctxA, confB.ID, ConfirmConfirmed, "wrong tenant"); err != nil {
		t.Fatalf("cross-tenant UpdateStatus: %v", err)
	}
	gotB, err := store.Get(ctxB, confB.ID)
	if err != nil {
		t.Fatalf("Get tenant B: %v", err)
	}
	if gotB == nil || gotB.Status != ConfirmPending {
		t.Fatalf("tenant A changed tenant B confirmation: %+v", gotB)
	}

	overdueA, err := store.FindOverdue(ctxA)
	if err != nil {
		t.Fatalf("FindOverdue tenant A: %v", err)
	}
	if len(overdueA) != 1 || overdueA[0].ID != confA.ID {
		t.Fatalf("tenant A overdue leaked: %+v", overdueA)
	}
}
