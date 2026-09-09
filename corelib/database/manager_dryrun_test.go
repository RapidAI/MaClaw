package database

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// newDryRunTestAdapter builds a sqlAdapter backed by an in-memory SQLite
// database. The sqlite dialect needs no session reset statement and binds
// :name parameters to positional ? placeholders. The connection pool is pinned
// to a single connection so the in-memory database survives across queries.
func newDryRunTestAdapter(t *testing.T, profile Profile) *sqlAdapter {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, amount INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO orders (id, amount) VALUES (1, 10), (2, 20)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return &sqlAdapter{db: db, dialect: "sqlite", profile: profile}
}

func orderAmounts(t *testing.T, db *sql.DB) map[int]int {
	t.Helper()
	rows, err := db.Query(`SELECT id, amount FROM orders`)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	defer rows.Close()
	got := map[int]int{}
	for rows.Next() {
		var id, amount int
		if err := rows.Scan(&id, &amount); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[id] = amount
	}
	return got
}

func TestDryRunRolledBackTransaction(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	ctx := context.Background()

	preview, err := a.ExecuteBatch(ctx, BatchExecuteRequest{
		Statements: []BatchStatement{
			{SQL: "UPDATE orders SET amount = :amount WHERE id = :id", Params: map[string]interface{}{"amount": 99, "id": 1}},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !preview.DryRun {
		t.Fatalf("expected dry-run result, got %+v", preview)
	}
	if preview.DryRunGuarantee != "rolled_back_transaction" {
		t.Fatalf("guarantee = %q, want rolled_back_transaction", preview.DryRunGuarantee)
	}
	if preview.AffectedRows != 1 {
		t.Fatalf("affected rows = %d, want 1", preview.AffectedRows)
	}
	if got := orderAmounts(t, a.db); got[1] != 10 {
		t.Fatalf("rollback preview mutated data: %v", got)
	}

	commit, err := a.ExecuteBatch(ctx, BatchExecuteRequest{
		Statements: []BatchStatement{
			{SQL: "UPDATE orders SET amount = :amount WHERE id = :id", Params: map[string]interface{}{"amount": 99, "id": 1}},
		},
		DryRun:        false,
		ApprovalToken: "test-approval",
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if commit.DryRun || commit.CommitID == "" {
		t.Fatalf("expected committed result, got %+v", commit)
	}
	if got := orderAmounts(t, a.db); got[1] != 99 {
		t.Fatalf("commit did not apply: %v", got)
	}
}

func TestDryRunBatchRollbackPreviewsAllStatements(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	preview, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{
			{SQL: "UPDATE orders SET amount = :amount WHERE id = :id", Params: map[string]interface{}{"amount": 11, "id": 1}},
			{SQL: "DELETE FROM orders WHERE id = :id", Params: map[string]interface{}{"id": 2}},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if preview.DryRunGuarantee != "rolled_back_transaction" {
		t.Fatalf("guarantee = %q, want rolled_back_transaction", preview.DryRunGuarantee)
	}
	if preview.AffectedRows != 2 {
		t.Fatalf("affected rows = %d, want 2", preview.AffectedRows)
	}
	if got := orderAmounts(t, a.db); len(got) != 2 || got[1] != 10 || got[2] != 20 {
		t.Fatalf("rollback preview mutated data: %v", got)
	}
}

func TestDryRunDDLFallsBackToPolicyOnly(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true, AllowDDL: true})
	preview, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{
			{SQL: "CREATE TABLE archive (id INTEGER)"},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if preview.DryRunGuarantee != "policy_only" {
		t.Fatalf("guarantee = %q, want policy_only for DDL batches", preview.DryRunGuarantee)
	}
	var name string
	err = a.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='archive'`).Scan(&name)
	if err != sql.ErrNoRows {
		t.Fatalf("DDL must not execute in dry-run, scan err = %v", err)
	}
	if got := orderAmounts(t, a.db); got[1] != 10 {
		t.Fatalf("policy-only dry-run mutated data: %v", got)
	}
}

func TestDryRunDDLRejectedWithoutAllowDDL(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	_, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{{SQL: "CREATE TABLE archive (id INTEGER)"}},
		DryRun:     true,
	})
	if err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected permission error, got %v", err)
	}
}

func TestDryRunEnforcesMaxAffectedRows(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	_, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{
			{SQL: "UPDATE orders SET amount = amount + :delta WHERE id > :min_id", Params: map[string]interface{}{"delta": 1, "min_id": 0}, MaxAffectedRows: 1},
		},
		DryRun: true,
	})
	if err == nil || !strings.Contains(err.Error(), "quota_exceeded") {
		t.Fatalf("expected quota_exceeded, got %v", err)
	}
	if got := orderAmounts(t, a.db); got[1] != 10 || got[2] != 20 {
		t.Fatalf("rejected dry-run mutated data: %v", got)
	}
}

func TestDryRunSurfacesExecutionErrors(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	_, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{
			{SQL: "UPDATE missing_table SET amount = :amount WHERE id = :id", Params: map[string]interface{}{"amount": 1, "id": 1}},
		},
		DryRun: true,
	})
	if err == nil {
		t.Fatal("expected execution error to surface during rollback preview")
	}
}
