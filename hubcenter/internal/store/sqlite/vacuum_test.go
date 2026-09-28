package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func newVacuumTestProvider(t *testing.T, autoVacuum string) *Provider {
	t.Helper()
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "vacuum-test.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		AutoVacuum:        autoVacuum,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	return provider
}

func pragmaInt(t *testing.T, db *sql.DB, pragma string) int64 {
	t.Helper()
	var value int64
	if err := db.QueryRow(pragma).Scan(&value); err != nil {
		t.Fatalf("%s: %v", pragma, err)
	}
	return value
}

func churnHASyncOps(t *testing.T, db *sql.DB, rows int) {
	t.Helper()
	ctx := context.Background()
	payload := strings.Repeat("x", 4096)
	for i := 0; i < rows; i++ {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO ha_sync_ops (
				op_id, source_node_id, entity_type, entity_id, op_type,
				entity_version, occurred_at, payload_json, payload_hash
			) VALUES (?, 'node-a', 'vacuum_test_entity', ?, 'upsert', 1, '2026-09-27T00:00:00Z', ?, 'hash')
		`, "op-"+strconv.Itoa(i), "ent-"+strconv.Itoa(i), payload); err != nil {
			t.Fatalf("insert op %d: %v", i, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM ha_sync_ops`); err != nil {
		t.Fatalf("delete ops: %v", err)
	}
}

// runIncrementalVacuum drains the pragma's row set: the driver only performs
// the page-moving work while the returned rows are being consumed, so
// db.Exec/QueryRow leave it unfinished.
func runIncrementalVacuum(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`PRAGMA incremental_vacuum(1024)`)
	if err != nil {
		t.Fatalf("PRAGMA incremental_vacuum(1024): %v", err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("drain incremental_vacuum rows: %v", err)
	}
}

// TestAutoVacuumIncrementalReleasesFreelistWithoutExclusiveLock covers the
// runtime half of the fix: with auto_vacuum=INCREMENTAL applied at open time,
// mass DELETEs land on a freelist and PRAGMA incremental_vacuum hands those
// pages back to the OS — no VACUUM, no exclusive lock.
func TestAutoVacuumIncrementalReleasesFreelistWithoutExclusiveLock(t *testing.T) {
	provider := newVacuumTestProvider(t, "incremental")
	if got := pragmaInt(t, provider.Write, "PRAGMA auto_vacuum"); got != 2 {
		t.Fatalf("PRAGMA auto_vacuum = %d, want 2 (INCREMENTAL) on a fresh database", got)
	}

	churnHASyncOps(t, provider.Write, 600)

	if got := pragmaInt(t, provider.Write, "PRAGMA freelist_count"); got == 0 {
		t.Fatal("PRAGMA freelist_count = 0 after mass delete, want freed pages retained on the freelist")
	}
	runIncrementalVacuum(t, provider.Write)
	if got := pragmaInt(t, provider.Write, "PRAGMA freelist_count"); got != 0 {
		t.Fatalf("PRAGMA freelist_count = %d after incremental_vacuum, want 0", got)
	}
}

// TestVacuumPersistsIncrementalAutoVacuumOnLegacyDatabase covers the one-time
// production migration path: a legacy database opened without an AutoVacuum
// config (mode NONE) becomes auto_vacuum=INCREMENTAL after sqlite.Vacuum's
// rebuild, and the rebuild leaves no free pages behind.
func TestVacuumPersistsIncrementalAutoVacuumOnLegacyDatabase(t *testing.T) {
	provider := newVacuumTestProvider(t, "")
	if got := pragmaInt(t, provider.Write, "PRAGMA auto_vacuum"); got != 0 {
		t.Fatalf("PRAGMA auto_vacuum = %d, want 0 (NONE) with empty AutoVacuum config", got)
	}

	churnHASyncOps(t, provider.Write, 300)

	if err := Vacuum(context.Background(), provider.Write); err != nil {
		t.Fatalf("Vacuum() error = %v", err)
	}
	if got := pragmaInt(t, provider.Write, "PRAGMA auto_vacuum"); got != 2 {
		t.Fatalf("PRAGMA auto_vacuum = %d after Vacuum, want 2 (INCREMENTAL persisted by the rebuild)", got)
	}
	if got := pragmaInt(t, provider.Write, "PRAGMA freelist_count"); got != 0 {
		t.Fatalf("PRAGMA freelist_count = %d after Vacuum, want 0", got)
	}
}
