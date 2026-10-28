package agentservice

import (
	"bytes"
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
	st := storeState{
		Tenants:     map[string]Tenant{"t1": {ID: "t1", Name: "one", CreatedAt: now}},
		Users:       map[string]User{"t1:u1": {ID: "u1", TenantID: "t1"}},
		UserConfigs: map[string]UserConfig{"t1:u1": {TenantID: "t1", UserID: "u1"}},
		Instances:   map[string]Instance{"i1": {ID: "i1", TenantID: "t1", UserID: "u1"}},
		Sessions:    map[string]Session{"s1": {ID: "s1", TenantID: "t1", UserID: "u1", InstanceID: "i1"}},
		Runs:        map[string]Run{"r1": {ID: "r1", TenantID: "t1", UserID: "u1", InstanceID: "i1", SessionID: "s1", StartedAt: now}},
		Messages:    map[string][]Message{"s1": {{ID: "m1", SessionID: "s1", TenantID: "t1", UserID: "u1", InstanceID: "i1", Role: MessageRoleUser, Content: "hi", CreatedAt: now}, {ID: "m2", SessionID: "s1", Role: MessageRoleAssistant, Content: "yo", CreatedAt: now}}},
		RunEvents:   map[string][]RunEvent{runEventScope("t1", "u1", "r1"): {{ID: "e1", TenantID: "t1", UserID: "u1", RunID: "r1", Sequence: 1, Type: "run.started"}, {ID: "e2", TenantID: "t1", UserID: "u1", RunID: "r1", Sequence: 2, Type: "run.completed", Payload: map[string]any{"n": 1.0}}}},
		AuditEvents: []AuditEvent{{ID: "a1", TenantID: "t1", UserID: "u1", CreatedAt: now}, {ID: "a2", TenantID: "t1", UserID: "u1", CreatedAt: now}},
	}
	return st
}

func TestSQLiteStoreMigratesBlobVerifiesAndDropsIt(t *testing.T) {
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
	b, _ := canonicalStateJSON(store.inner)
	if !bytes.Equal(a, b) {
		t.Fatalf("migrated state differs:\n%s\n%s", a, b)
	}
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='agentservice_state'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("legacy blob table still present n=%d err=%v", n, err)
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

func TestSQLiteStoreMigrationFailureAbortsAndKeepsBlob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.db")
	st := sampleLegacyState()
	scope := runEventScope("t1", "u1", "r1")
	// Two events with the same sequence cannot be represented as rows.
	st.RunEvents[scope] = append(st.RunEvents[scope], RunEvent{ID: "dup", TenantID: "t1", UserID: "u1", RunID: "r1", Sequence: 2, Type: "x"})
	payload := writeLegacyBlobDB(t, path, st)
	if _, err := NewSQLiteStore(path); err == nil {
		t.Fatal("startup succeeded despite an unmigratable blob")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var kept []byte
	if err := db.QueryRow(`SELECT state_json FROM agentservice_state WHERE id=1`).Scan(&kept); err != nil || !bytes.Equal(kept, payload) {
		t.Fatalf("blob not preserved after failed migration: err=%v", err)
	}
	var meta int
	if err := db.QueryRow(`SELECT count(*) FROM as_meta`).Scan(&meta); err != nil || meta != 0 {
		t.Fatalf("as_meta written despite failure: %d %v", meta, err)
	}
}

// Quiet reads never touch rows; another handle's write is applied per row
// (no full reload), including a same-length rename.
func TestSQLiteStoreReadsServeCacheAndCatchUpPerRow(t *testing.T) {
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
	start := sqliteFullLoads.Load()
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
	if n := sqliteFullLoads.Load() - start; n != 0 {
		t.Fatalf("full reloads = %d, want 0 (per-row catch-up)", n)
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
	err = s.write(func(m *MemoryStore) error {
		_ = m.SaveTenant(Tenant{ID: "ghost"})
		return boom
	})
	if err != boom {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.GetTenant("ghost"); err == nil {
		t.Fatal("rejected write stayed in cache")
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
	after, _ := canonicalStateJSON(reopened.inner)
	if !bytes.Equal(before, after) {
		t.Fatalf("export differs: %d vs %d bytes", len(before), len(after))
	}
	fi, _ := os.Stat(path)
	t.Logf("EQUIV OK blob=%dB export=%dB counts[%s] migrate+verify+drop+vacuum=%s db_after=%dB", len(payload), len(after), rowCountsString(reopened.inner), took, fi.Size())
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
