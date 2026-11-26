package codingruntime

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startSemanticAnchorAttempt(t *testing.T, store Store) (*Task, *Attempt) {
	t.Helper()
	task, err := store.CreateTask(Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: "work"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, PolicySnapshot{ProjectRoot: "project", Mode: "local", ReadOnly: true}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return task, attempt
}

func semanticAnchor(task *Task, attempt *Attempt, tenant, principal, session, root, turn string) SemanticTaskAnchor {
	return SemanticTaskAnchor{
		RuntimeTaskID: task.TaskID, RuntimeAttemptID: attempt.AttemptID,
		TenantID: tenant, PrincipalID: principal, SessionID: session, RootTaskID: root, TurnID: turn,
	}
}

func TestSQLiteStoreKeepsOneConnection(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if got := store.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1", got)
	}
	var foreignKeys int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestSQLiteStoreUsesImmediateTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(50)&_txlock=immediate")
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	defer other.Close()
	other.SetMaxOpenConns(1)
	conflicting, err := other.Begin()
	if err == nil {
		_ = conflicting.Rollback()
		_ = tx.Rollback()
		t.Fatal("a second immediate transaction started while the store transaction was open; store transactions are not immediate")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	conflicting, err = other.Begin()
	if err != nil {
		t.Fatalf("immediate transaction after store rollback: %v", err)
	}
	if err := conflicting.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestMemorySemanticTaskAnchorRejectsMissingAndConflictingLineage(t *testing.T) {
	store := NewMemoryStore()
	task, attempt := startSemanticAnchorAttempt(t, store)
	if _, err := store.RegisterSemanticTaskAnchor(semanticAnchor(task, attempt, "", "principal", "session", "root", "turn")); !errors.Is(err, ErrSemanticAnchorNotFound) {
		t.Fatalf("missing tenant error = %v, want ErrSemanticAnchorNotFound", err)
	}
	anchor := semanticAnchor(task, attempt, "tenant", "principal", "session", "root", "turn")
	if _, err := store.RegisterSemanticTaskAnchor(anchor); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterSemanticTaskAnchor(anchor); err != nil {
		t.Fatalf("idempotent registration: %v", err)
	}
	anchor.RootTaskID = "other-root"
	if _, err := store.RegisterSemanticTaskAnchor(anchor); !errors.Is(err, ErrSemanticAnchorConflict) {
		t.Fatalf("conflicting anchor error = %v, want ErrSemanticAnchorConflict", err)
	}
	if _, err := store.ResolveSemanticTaskAnchor(task.TaskID, "unknown-attempt"); !errors.Is(err, ErrSemanticAnchorNotFound) {
		t.Fatalf("unknown attempt error = %v, want ErrSemanticAnchorNotFound", err)
	}
}

func TestSQLiteSemanticTaskAnchorPersistsAndScopesAttempts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	task, attempt := startSemanticAnchorAttempt(t, store)
	anchor := semanticAnchor(task, attempt, "tenant-a", "principal", "session-a", "semantic-root", "turn-a")
	if _, err := store.RegisterSemanticTaskAnchor(anchor); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.RegisterSemanticTaskAnchor(anchor); err != nil {
		t.Fatalf("same attempt registration must be idempotent across reopen: %v", err)
	}
	got, err := reopened.ResolveSemanticTaskAnchor(task.TaskID, attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant-a" || got.PrincipalID != "principal" || got.SessionID != "session-a" || got.RootTaskID != "semantic-root" || got.TurnID != "turn-a" {
		t.Fatalf("reopened anchor = %#v", got)
	}
	if _, err := reopened.ResolveSemanticTaskAnchor(task.TaskID, "another-attempt"); !errors.Is(err, ErrSemanticAnchorNotFound) {
		t.Fatalf("cross-attempt lookup error = %v, want ErrSemanticAnchorNotFound", err)
	}
}

type semanticAnchorTestStore interface {
	Store
	SemanticTaskAnchorStore
}

func testSemanticAnchorTurnBindsOnlyOneAttempt(t *testing.T, store semanticAnchorTestStore) {
	t.Helper()
	firstTask, firstAttempt := startSemanticAnchorAttempt(t, store)
	anchor := semanticAnchor(firstTask, firstAttempt, "tenant-a", "principal-a", "session-a", "root-a", "turn-a")
	if _, err := store.RegisterSemanticTaskAnchor(anchor); err != nil {
		t.Fatal(err)
	}
	secondTask, secondAttempt := startSemanticAnchorAttempt(t, store)
	replayed := semanticAnchor(secondTask, secondAttempt, "tenant-a", "principal-a", "session-a", "root-a", "turn-a")
	if _, err := store.RegisterSemanticTaskAnchor(replayed); !errors.Is(err, ErrSemanticAnchorConflict) {
		t.Fatalf("second attempt for one semantic turn error = %v, want ErrSemanticAnchorConflict", err)
	}
}

func TestMemorySemanticTaskAnchorTurnBindsOnlyOneAttempt(t *testing.T) {
	testSemanticAnchorTurnBindsOnlyOneAttempt(t, NewMemoryStore())
}

func TestSQLiteSemanticTaskAnchorTurnBindsOnlyOneAttempt(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	testSemanticAnchorTurnBindsOnlyOneAttempt(t, store)
}

func TestMemorySemanticTaskAnchorRegistrationClockIsNotPartOfIdentity(t *testing.T) {
	store := NewMemoryStore()
	task, attempt := startSemanticAnchorAttempt(t, store)
	anchor := semanticAnchor(task, attempt, "tenant", "principal", "session", "root", "turn")
	if _, err := store.RegisterSemanticTaskAnchor(anchor); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := store.RegisterSemanticTaskAnchor(anchor); err != nil {
		t.Fatalf("same attempt registration must not depend on the registration clock: %v", err)
	}
}

func testSemanticAnchorRejectsTerminalAttempt(t *testing.T, store semanticAnchorTestStore) {
	t.Helper()
	unboundTask, unboundAttempt := startSemanticAnchorAttempt(t, store)
	boundTask, boundAttempt := startSemanticAnchorAttempt(t, store)
	boundAnchor := semanticAnchor(boundTask, boundAttempt, "tenant-a", "principal-a", "session-a", "root-b", "turn-b")
	if _, err := store.RegisterSemanticTaskAnchor(boundAnchor); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []*Attempt{unboundAttempt, boundAttempt} {
		if _, err := store.FinishAttempt(attempt.AttemptID, "owner", FinishInput{Status: TaskInterrupted, SideEffectState: SideEffectUncertain}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	unboundAnchor := semanticAnchor(unboundTask, unboundAttempt, "tenant-a", "principal-a", "session-a", "root-a", "turn-a")
	if _, err := store.RegisterSemanticTaskAnchor(unboundAnchor); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal unbound attempt anchor error = %v, want ErrInvalidTransition", err)
	}
	if _, err := store.RegisterSemanticTaskAnchor(boundAnchor); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal anchored attempt re-registration error = %v, want ErrInvalidTransition", err)
	}
}

func TestMemorySemanticTaskAnchorRejectsTerminalAttempt(t *testing.T) {
	testSemanticAnchorRejectsTerminalAttempt(t, NewMemoryStore())
}

func TestSQLiteSemanticTaskAnchorRejectsTerminalAttempt(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	testSemanticAnchorRejectsTerminalAttempt(t, store)
}

func TestSQLiteSemanticAnchorTurnMigrationRejectsNonUniqueIndexShadow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE coding_runtime_semantic_anchors (
 runtime_attempt_id TEXT PRIMARY KEY, runtime_task_id TEXT NOT NULL,
 tenant_id TEXT NOT NULL, principal_id TEXT NOT NULL, session_id TEXT NOT NULL,
 root_task_id TEXT NOT NULL, turn_id TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE INDEX coding_runtime_semantic_anchor_turn ON coding_runtime_semantic_anchors(tenant_id,principal_id,session_id,root_task_id,turn_id)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(path)
	if err == nil {
		_ = store.Close()
		t.Fatal("non-unique semantic anchor turn index shadow did not fail closed")
	}
	if !strings.Contains(err.Error(), "not a unique index") {
		t.Fatalf("migration error = %v, want non-unique index diagnostic", err)
	}
}

func TestSQLiteSemanticAnchorTurnMigrationRejectsWrongUniqueIndexShadow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE coding_runtime_semantic_anchors (
 runtime_attempt_id TEXT PRIMARY KEY, runtime_task_id TEXT NOT NULL,
 tenant_id TEXT NOT NULL, principal_id TEXT NOT NULL, session_id TEXT NOT NULL,
 root_task_id TEXT NOT NULL, turn_id TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX coding_runtime_semantic_anchor_turn ON coding_runtime_semantic_anchors(tenant_id,principal_id,session_id,root_task_id)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(path)
	if err == nil {
		_ = store.Close()
		t.Fatal("wrong-column semantic anchor turn index shadow did not fail closed")
	}
	if !strings.Contains(err.Error(), "semantic turn identity columns") && !strings.Contains(err.Error(), "want \"turn_id\"") {
		t.Fatalf("migration error = %v, want wrong-column index diagnostic", err)
	}
}

func TestSQLiteSemanticAnchorTurnMigrationDeduplicatesLegacyRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE coding_runtime_semantic_anchors (
 runtime_attempt_id TEXT PRIMARY KEY, runtime_task_id TEXT NOT NULL,
 tenant_id TEXT NOT NULL, principal_id TEXT NOT NULL, session_id TEXT NOT NULL,
 root_task_id TEXT NOT NULL, turn_id TEXT NOT NULL, created_at INTEGER NOT NULL
)`)
	if err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO coding_runtime_semantic_anchors(runtime_attempt_id,runtime_task_id,tenant_id,principal_id,session_id,root_task_id,turn_id,created_at) VALUES(?,?,?,?,?,?,?,?)`
	for _, row := range [][]any{
		{"attempt-first", "task-a", "tenant", "principal", "session", "root", "turn-dup", 20},
		{"attempt-duplicate", "task-b", "tenant", "principal", "session", "root", "turn-dup", 10},
		{"attempt-distinct", "task-c", "tenant", "principal", "session", "root", "turn-distinct", 30},
	} {
		if _, err := legacy.Exec(insert, row...); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("legacy duplicate anchors must not brick store startup: %v", err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(1) FROM coding_runtime_semantic_anchors`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("migrated anchor count = %d, want 2", count)
	}
	var kept string
	if err := store.db.QueryRow(`SELECT runtime_attempt_id FROM coding_runtime_semantic_anchors WHERE turn_id='turn-dup'`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != "attempt-first" {
		t.Fatalf("kept duplicate-turn anchor %q, want first inserted attempt-first", kept)
	}
	var quarantined int
	if err := store.db.QueryRow(`SELECT COUNT(1) FROM coding_runtime_semantic_anchor_turn_conflicts WHERE runtime_attempt_id='attempt-duplicate'`).Scan(&quarantined); err != nil {
		t.Fatal(err)
	}
	if quarantined != 1 {
		t.Fatalf("duplicate anchor quarantine rows = %d, want 1", quarantined)
	}
}
