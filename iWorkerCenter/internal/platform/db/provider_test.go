package db

import (
	"database/sql"
	"errors"
	"testing"
)

func TestOpenMemory(t *testing.T) {
	p, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) error: %v", err)
	}
	defer p.Close()

	if p.Write == nil || p.Read == nil {
		t.Fatal("expected non-nil Write and Read connections")
	}
}

func TestRunInTx_Commit(t *testing.T) {
	p, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	_, _ = p.Write.Exec("CREATE TABLE txtest (v TEXT)")

	if err := p.RunInTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO txtest (v) VALUES ('hello')")
		return err
	}); err != nil {
		t.Fatalf("RunInTx commit: %v", err)
	}

	var v string
	if err := p.Read.QueryRow("SELECT v FROM txtest").Scan(&v); err != nil {
		t.Fatalf("read after commit: %v", err)
	}
	if v != "hello" {
		t.Fatalf("v = %q, want hello", v)
	}
}

func TestRunInTx_Rollback(t *testing.T) {
	p, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	_, _ = p.Write.Exec("CREATE TABLE txtest2 (v TEXT)")

	wantErr := errors.New("forced error")
	if err := p.RunInTx(func(tx *sql.Tx) error {
		_, _ = tx.Exec("INSERT INTO txtest2 (v) VALUES ('should_not_persist')")
		return wantErr
	}); err != wantErr {
		t.Fatalf("RunInTx error = %v, want %v", err, wantErr)
	}

	var count int
	_ = p.Read.QueryRow("SELECT COUNT(*) FROM txtest2").Scan(&count)
	if count != 0 {
		t.Fatalf("count = %d after rollback, want 0", count)
	}
}

func TestMigrate(t *testing.T) {
	p, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if err := Migrate(p.Write); err != nil {
		t.Fatalf("Migrate error: %v", err)
	}

	// verify colleagues table exists
	var name string
	if err := p.Read.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='colleagues'").Scan(&name); err != nil {
		t.Fatalf("colleagues table not found: %v", err)
	}

	// verify roles table exists
	if err := p.Read.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='roles'").Scan(&name); err != nil {
		t.Fatalf("roles table not found: %v", err)
	}

	// verify role_assignment_log table exists
	if err := p.Read.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='role_assignment_log'").Scan(&name); err != nil {
		t.Fatalf("role_assignment_log table not found: %v", err)
	}

	// idempotent
	if err := Migrate(p.Write); err != nil {
		t.Fatalf("second Migrate error: %v", err)
	}
}
