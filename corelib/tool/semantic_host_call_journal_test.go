package tool

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func hostCallTestIdentity() HostCallIdentity {
	return HostCallIdentity{Protocol: "openai", ConnectionID: "request-1", CallID: "call-1"}
}

func exerciseHostCallJournal(t *testing.T, journal HostCallJournal) {
	t.Helper()
	now := time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC)
	identity := hostCallTestIdentity()
	record, action, err := journal.Acquire(identity, "grant-a", "request-a", now)
	if err != nil || action != HostCallAcquireAdmit || record.State != HostCallReceived {
		t.Fatalf("first acquire record=%+v action=%q err=%v", record, action, err)
	}
	if _, err := journal.MarkAdmitted(identity, "grant-a", "request-a", now.Add(time.Second)); err != nil {
		t.Fatalf("mark admitted: %v", err)
	}
	if _, err := journal.Complete(identity, "grant-a", "request-a", "safe result", now.Add(2*time.Second)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	record, action, err = journal.Acquire(identity, "grant-a", "request-a", now.Add(3*time.Second))
	if err != nil || action != HostCallAcquireReplay || record.Result != "safe result" || record.ResultDigest == "" {
		t.Fatalf("replay record=%+v action=%q err=%v", record, action, err)
	}
	if _, action, err = journal.Acquire(identity, "grant-b", "request-a", now); err != nil || action != HostCallAcquireConflict {
		t.Fatalf("changed grant action=%q err=%v", action, err)
	}
	if _, action, err = journal.Acquire(identity, "grant-a", "request-b", now); err != nil || action != HostCallAcquireConflict {
		t.Fatalf("changed request action=%q err=%v", action, err)
	}
}

func TestMemoryHostCallJournalReplaysTerminalAndRejectsConflicts(t *testing.T) {
	exerciseHostCallJournal(t, NewMemoryHostCallJournal())
}

func TestSQLiteHostCallJournalPersistsTerminalReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host-calls.db")
	journal, err := NewSQLiteHostCallJournal(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	exerciseHostCallJournal(t, journal)
	if err := journal.Close(); err != nil {
		t.Fatalf("close journal: %v", err)
	}
	restarted, err := NewSQLiteHostCallJournal(path)
	if err != nil {
		t.Fatalf("reopen journal: %v", err)
	}
	defer restarted.Close()
	record, action, err := restarted.Acquire(hostCallTestIdentity(), "grant-a", "request-a", time.Now().UTC())
	if err != nil || action != HostCallAcquireReplay || record.Result != "safe result" {
		t.Fatalf("restart replay record=%+v action=%q err=%v", record, action, err)
	}
}

func TestHostCallJournalReconcilesInterruptedCallsToUnknown(t *testing.T) {
	journal := NewMemoryHostCallJournal()
	now := time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC)
	identity := hostCallTestIdentity()
	if _, action, err := journal.Acquire(identity, "grant-a", "request-a", now); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("acquire action=%q err=%v", action, err)
	}
	if changed, err := journal.ReconcileStale(now.Add(2*time.Minute), time.Minute); err != nil || changed != 1 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	if _, action, err := journal.Acquire(identity, "grant-a", "request-a", now.Add(3*time.Minute)); err != nil || action != HostCallAcquireUnknown {
		t.Fatalf("recovered call action=%q err=%v", action, err)
	}
}

func TestResolveHostCallAcquireActionReplaysMatchingDigestAfterGrantMismatch(t *testing.T) {
	completed := HostCallRecord{RequestDigest: "digest-a", State: HostCallCompleted, Result: "done"}
	if got := ResolveHostCallAcquireAction(HostCallAcquireConflict, completed, "digest-a"); got != HostCallAcquireReplay {
		t.Fatalf("same digest after grant mismatch = %q, want replay", got)
	}
	if got := ResolveHostCallAcquireAction(HostCallAcquireConflict, completed, "digest-b"); got != HostCallAcquireConflict {
		t.Fatalf("different digest = %q, want conflict", got)
	}
	if got := ResolveHostCallAcquireAction(HostCallAcquireAdmit, completed, "digest-a"); got != HostCallAcquireAdmit {
		t.Fatalf("non-conflict action was rewritten: %q", got)
	}
}

func TestHostCallJournalConcurrentAcquireHasOneAdmitter(t *testing.T) {
	journal := NewMemoryHostCallJournal()
	identity := hostCallTestIdentity()
	var wg sync.WaitGroup
	actions := make(chan HostCallAcquireAction, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, action, err := journal.Acquire(identity, "grant-a", "request-a", time.Now().UTC())
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			actions <- action
		}()
	}
	wg.Wait()
	close(actions)
	var admitted, pending int
	for action := range actions {
		if action == HostCallAcquireAdmit {
			admitted++
		}
		if action == HostCallAcquireInProgress {
			pending++
		}
	}
	if admitted != 1 || pending != 1 {
		t.Fatalf("actions admitted=%d pending=%d", admitted, pending)
	}
}

func TestHostCallJournalSeparatesIdenticalCallsBySurfaceEpoch(t *testing.T) {
	journal := NewMemoryHostCallJournal()
	base := hostCallTestIdentity()
	first := base
	first.SurfaceEpoch = "surface-a"
	second := base
	second.SurfaceEpoch = "surface-b"
	if _, action, err := journal.Acquire(first, "grant-a", "request-a", time.Now().UTC()); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("first action=%q err=%v", action, err)
	}
	if _, action, err := journal.Acquire(second, "grant-b", "request-b", time.Now().UTC()); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("second epoch action=%q err=%v", action, err)
	}
}

func TestSQLiteHostCallJournalMigratesSurfaceEpochColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-host-calls.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE semantic_host_calls (
		call_key TEXT PRIMARY KEY, protocol TEXT NOT NULL, connection_id TEXT NOT NULL, call_id TEXT NOT NULL,
		grant_fingerprint TEXT NOT NULL, request_digest TEXT NOT NULL,
		state TEXT NOT NULL, result TEXT NOT NULL DEFAULT '', result_digest TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`)
	if closeErr := legacy.Close(); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	} else if closeErr != nil {
		t.Fatalf("close legacy db: %v", closeErr)
	}
	journal, err := NewSQLiteHostCallJournal(path)
	if err != nil {
		t.Fatalf("migrate journal: %v", err)
	}
	defer journal.Close()
	identity := hostCallTestIdentity()
	identity.SurfaceEpoch = "surface-a"
	if _, action, err := journal.Acquire(identity, "grant-a", "request-a", time.Now().UTC()); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("post-migration acquire action=%q err=%v", action, err)
	}
}
