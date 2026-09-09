package agentservice

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteRunEventStorePersistsAcrossOpen(t *testing.T) {
	path := t.TempDir() + "/events.db"
	store, err := NewSQLiteRunEventStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Type: "run.started"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := NewSQLiteRunEventStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	events, err := reopened.ListAfter("t", "u", "r", 0, 10)
	if err != nil || len(events) != 1 || events[0].Type != "run.started" {
		t.Fatalf("events=%#v err=%v", events, err)
	}
}

func TestSQLiteRunEventStoreReplayReturnsExistingEvent(t *testing.T) {
	path := t.TempDir() + "/events.db"
	store, err := NewSQLiteRunEventStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	first, err := store.Append(RunEvent{ID: "evt-fixed", TenantID: "t", UserID: "u", RunID: "r", Sequence: 7, Type: "run.started", Payload: map[string]any{"n": 1}})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	replay, err := store.Append(RunEvent{ID: "evt-retry", TenantID: "t", UserID: "u", RunID: "r", Sequence: 7, Type: "tampered", Payload: map[string]any{"n": 2}})
	if err != nil {
		t.Fatalf("append replay: %v", err)
	}
	if replay.ID != first.ID || replay.Type != first.Type || replay.Payload["n"] != float64(1) {
		t.Fatalf("replay=%#v; want original=%#v", replay, first)
	}
	byID, err := store.Append(RunEvent{ID: "evt-fixed", TenantID: "t", UserID: "u", RunID: "r", Sequence: 9, Type: "tampered"})
	if err != nil {
		t.Fatalf("append id replay: %v", err)
	}
	if byID.Sequence != first.Sequence || byID.Type != first.Type {
		t.Fatalf("id replay=%#v; want original=%#v", byID, first)
	}
	seq, found, err := store.SequenceForID("t", "u", "r", "evt-fixed")
	if err != nil || !found || seq != 7 {
		t.Fatalf("SequenceForID = %d, %v, %v; want 7,true,nil", seq, found, err)
	}
}

func TestSQLiteRunEventStoreScopesProducerEventIDsPerRun(t *testing.T) {
	store, err := NewSQLiteRunEventStore(t.TempDir() + "/events.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	first, err := store.Append(RunEvent{ID: "evt-deterministic", TenantID: "t", UserID: "u", RunID: "run-a", Type: "assistant.delta"})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	second, err := store.Append(RunEvent{ID: "evt-deterministic", TenantID: "t", UserID: "u", RunID: "run-b", Type: "assistant.delta"})
	if err != nil {
		t.Fatalf("same producer id in another run: %v", err)
	}
	if first.ID != second.ID || first.RunID == second.RunID {
		t.Fatalf("events were not kept in independent scopes: first=%#v second=%#v", first, second)
	}
	if _, err := store.Append(RunEvent{ID: "evt-deterministic", TenantID: "t", UserID: "u", RunID: "run-a", Sequence: 99, Type: "tampered"}); err != nil {
		t.Fatalf("same-scope replay: %v", err)
	} else if events, listErr := store.ListAfter("t", "u", "run-a", 0, 10); listErr != nil || len(events) != 1 || events[0].Type != "assistant.delta" {
		t.Fatalf("same-scope replay changed canonical event: events=%#v err=%v", events, listErr)
	}
}

func TestSQLiteRunEventStoreMigratesGlobalEventIDSchema(t *testing.T) {
	path := t.TempDir() + "/legacy-events.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE run_events (
		event_id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		instance_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		run_id TEXT NOT NULL,
		sequence INTEGER NOT NULL,
		type TEXT NOT NULL,
		schema_version TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		occurred_at TEXT NOT NULL,
		UNIQUE(tenant_id, user_id, run_id, sequence)
	);
	CREATE INDEX idx_run_events_scope ON run_events(tenant_id, user_id, run_id, sequence);
	INSERT INTO run_events(event_id, tenant_id, user_id, instance_id, session_id, run_id, sequence, type, schema_version, payload_json, occurred_at)
	VALUES('evt-legacy', 't', 'u', 'i', 's', 'run-a', 1, 'run.started', 'maclaw.run-event/v1', '{}', ?);`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		db.Close()
		t.Fatalf("seed legacy db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
	store, err := NewSQLiteRunEventStore(path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	defer store.Close()
	if _, err := store.Append(RunEvent{ID: "evt-legacy", TenantID: "t", UserID: "u", RunID: "run-b", Type: "run.started"}); err != nil {
		t.Fatalf("append duplicate producer id after migration: %v", err)
	}
	for _, runID := range []string{"run-a", "run-b"} {
		events, err := store.ListAfter("t", "u", runID, 0, 10)
		if err != nil || len(events) != 1 || events[0].ID != "evt-legacy" {
			t.Fatalf("run %s events=%#v err=%v", runID, events, err)
		}
	}
}

func TestSQLiteRunEventStoreConcurrentAppendsAssignUniqueSequences(t *testing.T) {
	store, err := NewSQLiteRunEventStore(t.TempDir() + "/events.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	const writers = 24
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, appendErr := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Type: "assistant.delta"})
			if appendErr != nil {
				errs <- appendErr
			}
		}()
	}
	wg.Wait()
	close(errs)
	for appendErr := range errs {
		t.Fatalf("append: %v", appendErr)
	}
	events, err := store.ListAfter("t", "u", "r", 0, writers)
	if err != nil || len(events) != writers {
		t.Fatalf("events=%d err=%v; want %d", len(events), err, writers)
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("event[%d] sequence=%d; want %d", i, event.Sequence, i+1)
		}
	}
}

func TestSQLiteRunEventStoreCloseIsIdempotentAndRejectsAccess(t *testing.T) {
	store, err := NewSQLiteRunEventStore(t.TempDir() + "/events.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := store.Append(RunEvent{TenantID: "t", UserID: "u", RunID: "r", Type: "run.started"}); !errors.Is(err, ErrServiceClosed) {
		t.Fatalf("Append after close=%v; want ErrServiceClosed", err)
	}
	if _, err := store.ListAfter("t", "u", "r", 0, 10); !errors.Is(err, ErrServiceClosed) {
		t.Fatalf("ListAfter after close=%v; want ErrServiceClosed", err)
	}
}
