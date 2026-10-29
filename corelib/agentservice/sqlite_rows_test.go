package agentservice

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() { sqliteVerifyWrites = true }

func sqliteTestMemory(t testing.TB, store *SQLiteStore) *MemoryStore {
	t.Helper()
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.(*MemoryStore)
}

// writeLegacyBlobDB builds a pre-migration service.db holding state as the
// old single-row agentservice_state blob.
func writeLegacyBlobDB(t testing.TB, path string, state storeState) []byte {
	t.Helper()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE agentservice_state (id INTEGER PRIMARY KEY CHECK (id = 1), schema_version INTEGER NOT NULL, state_json BLOB NOT NULL, updated_at TEXT NOT NULL, write_seq INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agentservice_state VALUES(1,1,?,?,0)`, payload, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return payload
}

func sampleLegacyState() storeState {
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)
	return storeState{
		Tenants: map[string]Tenant{
			"t1": {ID: "t1", Name: "one", CreatedAt: now},
			"t2": {ID: "t2", Name: "two", CreatedAt: later, Quota: TenantQuota{MaxSessions: 7}},
		},
		Users: map[string]User{
			"t1:u1": {ID: "u1", TenantID: "t1", Name: "first", CreatedAt: now},
			"t2:u2": {ID: "u2", TenantID: "t2", Name: "second", CreatedAt: later},
		},
		Credentials: map[string]Credential{
			"legacy-key": {ID: "c1", TenantID: "t1", UserID: "u1", APIKey: "legacy-key", APISecret: "discard", CreatedAt: now},
		},
		UserConfigs: map[string]UserConfig{
			"t1:u1": {TenantID: "t1", UserID: "u1", UpdatedAt: now},
			"t2:u2": {TenantID: "t2", UserID: "u2", UpdatedAt: later},
		},
		Instances: map[string]Instance{
			"i1": {ID: "i1", TenantID: "t1", UserID: "u1", CreatedAt: now},
			"i2": {ID: "i2", TenantID: "t2", UserID: "u2", CreatedAt: later},
		},
		Sessions: map[string]Session{
			"s1": {ID: "s1", TenantID: "t1", UserID: "u1", InstanceID: "i1", CreatedAt: now, Metadata: map[string]string{"client_session_key": "client-1"}},
			"s2": {ID: "s2", TenantID: "t2", UserID: "u2", InstanceID: "i2", CreatedAt: later},
		},
		Runs: map[string]Run{
			"r1": {ID: "r1", TenantID: "t1", UserID: "u1", InstanceID: "i1", SessionID: "s1", UserMessageID: "m1", StartedAt: now},
			"r2": {ID: "r2", TenantID: "t2", UserID: "u2", InstanceID: "i2", SessionID: "s2", UserMessageID: "m3", StartedAt: later},
		},
		Messages: map[string][]Message{
			"s1": {{ID: "m1", SessionID: "s1", TenantID: "t1", UserID: "u1", InstanceID: "i1", Role: MessageRoleUser, Content: "hi", CreatedAt: now}, {ID: "m2", SessionID: "s1", Role: MessageRoleAssistant, Content: "yo", CreatedAt: later}},
			"s2": {{ID: "m3", SessionID: "s2", TenantID: "t2", UserID: "u2", InstanceID: "i2", Role: MessageRoleUser, Content: "large \x00 text", CreatedAt: later}},
		},
		RunEvents: map[string][]RunEvent{
			runEventScope("t1", "u1", "r1"): {{ID: "e1", TenantID: "t1", UserID: "u1", RunID: "r1", Sequence: 1, Type: "run.started"}, {ID: "e2", TenantID: "t1", UserID: "u1", RunID: "r1", Sequence: 2, Type: "run.completed", Payload: map[string]any{"n": 1.0}}},
			runEventScope("t2", "u2", "r2"): {{ID: "e3", TenantID: "t2", UserID: "u2", RunID: "r2", Sequence: 1, Type: "run.started"}},
		},
		AuditEvents: []AuditEvent{{ID: "a1", TenantID: "t1", UserID: "u1", CreatedAt: now}, {ID: "a2", TenantID: "t2", UserID: "u2", CreatedAt: later}},
	}
}

func TestSQLiteStoreMigratesBlobVerifiesAndPreservesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	payload := writeLegacyBlobDB(t, path, sampleLegacyState())
	want, err := memoryStoreFromJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := canonicalStateJSON(want)
	b, _ := canonicalStateJSON(sqliteTestMemory(t, store))
	if !bytes.Equal(a, b) {
		t.Fatalf("migrated state differs:\n%s\n%s", a, b)
	}
	assertSQLiteMigrationReadsEqual(t, want, store)
	var kept []byte
	if err := store.db.QueryRow(`SELECT state_json FROM agentservice_state_migrated WHERE id=1`).Scan(&kept); err != nil || !bytes.Equal(kept, payload) {
		t.Fatalf("legacy blob backup differs: err=%v", err)
	}
	store.Close()
	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if msgs, err := reopened.ListMessages("s1"); err != nil || len(msgs) != 2 || msgs[1].ID != "m2" {
		t.Fatalf("messages after reopen: %#v err=%v", msgs, err)
	}
}

func TestSQLiteStoreMigrationFailureRollsBackAndUsesLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	st := sampleLegacyState()
	scope := runEventScope("t1", "u1", "r1")
	// Two events with the same sequence cannot be represented as rows.
	st.RunEvents[scope] = append(st.RunEvents[scope], RunEvent{ID: "dup", TenantID: "t1", UserID: "u1", RunID: "r1", Sequence: 2, Type: "x"})
	payload := writeLegacyBlobDB(t, path, st)
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("legacy fallback did not open: %v", err)
	}
	defer store.Close()
	if !store.legacy {
		t.Fatal("unmigratable database did not use legacy fallback")
	}
	events, err := store.ListAfter("t1", "u1", "r1", 0, 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("legacy events after fallback=%#v err=%v", events, err)
	}
	var kept []byte
	if err := store.db.QueryRow(`SELECT state_json FROM agentservice_state WHERE id=1`).Scan(&kept); err != nil || !bytes.Equal(kept, payload) {
		t.Fatalf("blob not preserved after failed migration: err=%v", err)
	}
	var meta int
	if err := store.db.QueryRow(`SELECT count(*) FROM as_meta`).Scan(&meta); err != nil || meta != 0 {
		t.Fatalf("as_meta written despite rollback: %d %v", meta, err)
	}
	for _, table := range []string{"as_tenants", "as_users", "as_messages", "as_runs", "as_run_events"} {
		var n int
		if err := store.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s contains %d rows after rollback (err=%v)", table, n, err)
		}
	}
	if err := store.SaveTenant(Tenant{ID: "fallback-write", Name: "preserved"}); err != nil {
		t.Fatalf("legacy fallback write: %v", err)
	}
	if got, err := store.GetTenant("fallback-write"); err != nil || got.Name != "preserved" {
		t.Fatalf("legacy fallback read after write=%#v err=%v", got, err)
	}
}

func TestSQLiteStoreReadsObserveCommittedRowsAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	a, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := a.SaveTenant(Tenant{ID: "t1", Name: "aaa"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bbb", "ccc", "ddd"} {
		if err := a.SaveTenant(Tenant{ID: "t1", Name: name}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 100; i++ {
			got, err := b.GetTenant("t1")
			if err != nil || got.Name != name {
				t.Fatalf("stale read %q: %#v err=%v", name, got, err)
			}
		}
	}
}

func TestSQLiteStoreSnapshotStableAndLiveReadsObserveExternalWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	live, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	external, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer external.Close()
	if err := external.SaveTenant(Tenant{ID: "t1", Name: "before"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := live.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := snapshot.GetTenant("t1"); err != nil || got.Name != "before" {
		t.Fatalf("initial snapshot = %#v, err=%v", got, err)
	}
	for i := 0; i < 20; i++ {
		if got, err := live.GetTenant("t1"); err != nil || got.Name != "before" {
			t.Fatalf("quiet read = %#v, err=%v", got, err)
		}
	}

	// A second handle has the same committed-write visibility contract as a
	// second process. The old snapshot stays fixed; live reads query latest rows.
	if err := external.SaveTenant(Tenant{ID: "t1", Name: "after"}); err != nil {
		t.Fatal(err)
	}
	if got, err := snapshot.GetTenant("t1"); err != nil || got.Name != "before" {
		t.Fatalf("snapshot changed after external write = %#v, err=%v", got, err)
	}
	if got, err := live.GetTenant("t1"); err != nil || got.Name != "after" {
		t.Fatalf("live read missed external write = %#v, err=%v", got, err)
	}
}

func TestSQLiteStoreFailedWriteLeavesNoTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	boom := fmt.Errorf("boom")
	err = s.write(func(ctx context.Context, q sqlq) error {
		if err := sqliteSaveMapRow(ctx, q, "as_tenants", "ghost", "ghost", "", "", Tenant{ID: "ghost"}); err != nil {
			return err
		}
		return boom
	}, func(m *MemoryStore) error {
		_ = m.SaveTenant(Tenant{ID: "ghost"})
		return boom
	})
	if err != boom {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.GetTenant("ghost"); err == nil {
		t.Fatal("rejected write stayed in rows")
	}
}

// TestSQLiteStoreMigratesRealDatabaseCopy runs the migration on a copy of a
// production service.db (MACLAW_AGENTSERVICE_MIGRATE_COPY=/path/to/copy.db)
// and compares a full export old vs new.
func TestSQLiteStoreMigratesRealDatabaseCopy(t *testing.T) {
	src := os.Getenv("MACLAW_AGENTSERVICE_MIGRATE_COPY")
	if src == "" {
		t.Skip("set MACLAW_AGENTSERVICE_MIGRATE_COPY")
	}
	path := filepath.Join(t.TempDir(), "service.db")
	copyFile(t, src, path)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err := raw.QueryRow(`SELECT state_json FROM agentservice_state WHERE id=1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	old, err := memoryStoreFromJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := canonicalStateJSON(old)
	t0 := time.Now()
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(t0)
	store.Close()
	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	migrated := sqliteTestMemory(t, reopened)
	after, _ := canonicalStateJSON(migrated)
	if !bytes.Equal(before, after) {
		t.Fatalf("export differs: %d vs %d bytes", len(before), len(after))
	}
	fi, _ := os.Stat(path)
	t.Logf("EQUIV OK blob=%dB export=%dB counts[%s] migrate+verify+backup=%s db_after=%dB", len(payload), len(after), rowCountsString(migrated), took, fi.Size())
}

func copyFile(t testing.TB, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	_ = strings.TrimSpace
}
