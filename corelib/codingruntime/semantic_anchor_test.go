package codingruntime

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func startSemanticAnchorAttempt(t *testing.T, store Store) (*Task, *Attempt) {
	t.Helper()
	task, err := store.CreateTask(Task{OwnerID: "owner", ProjectRef: "project", Mode: "local", RequestedWork: "work"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "owner", time.Minute, PolicySnapshot{ProjectRoot: "project", Mode: "local"}, time.Now().UTC())
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
