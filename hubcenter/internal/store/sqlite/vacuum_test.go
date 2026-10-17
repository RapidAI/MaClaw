package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

	// incremental_vacuum is a no-op on auto_vacuum=NONE and must not fail
	// the prune path that calls it before the one-time conversion.
	if err := IncrementalVacuum(context.Background(), provider.Write, 8); err != nil {
		t.Fatalf("IncrementalVacuum() on NONE database error = %v", err)
	}

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

func TestIncrementalVacuumPageCountIsBounded(t *testing.T) {
	provider := newVacuumTestProvider(t, "incremental")
	churnHASyncOps(t, provider.Write, 800)
	before := pragmaInt(t, provider.Write, "PRAGMA freelist_count")
	if before <= int64(maxIncrementalVacuumPages) {
		t.Fatalf("freelist_count = %d, want more than the cap %d", before, maxIncrementalVacuumPages)
	}
	if err := IncrementalVacuum(context.Background(), provider.Write, 1_000_000); err != nil {
		t.Fatalf("IncrementalVacuum() error = %v", err)
	}
	after := pragmaInt(t, provider.Write, "PRAGMA freelist_count")
	released := before - after
	if released <= 0 || released > int64(maxIncrementalVacuumPages) {
		t.Fatalf("released %d pages (before %d after %d), want 1..%d", released, before, after, maxIncrementalVacuumPages)
	}
}

func TestPruneHistoryReleasesFreelistAfterEachBatch(t *testing.T) {
	withPages := newVacuumTestProvider(t, "incremental")
	without := newVacuumTestProvider(t, "incremental")
	seedSupersededOps(t, withPages.Write)
	seedSupersededOps(t, without.Write)
	ctx := context.Background()
	releasing := &haSyncOpRepo{db: withPages.Write, readDB: withPages.Read, incrementalVacuumPages: maxIncrementalVacuumPages}
	holding := &haSyncOpRepo{db: without.Write, readDB: without.Read}
	cutoff := time.Now().UTC()
	if _, err := releasing.PruneHistory(ctx, cutoff, 0, 5); err != nil {
		t.Fatalf("PruneHistory() with incremental vacuum error = %v", err)
	}
	if _, err := holding.PruneHistory(ctx, cutoff, 0, 5); err != nil {
		t.Fatalf("PruneHistory() without incremental vacuum error = %v", err)
	}
	freed := pragmaInt(t, withPages.Write, "PRAGMA freelist_count")
	kept := pragmaInt(t, without.Write, "PRAGMA freelist_count")
	if kept == 0 {
		t.Fatal("control prune left no freelist pages; the fixture did not delete anything")
	}
	if freed >= kept {
		t.Fatalf("freelist with incremental vacuum = %d, control = %d; prune batches did not release pages", freed, kept)
	}
}

func TestMaintenanceStateStaysOutOfSystemSettings(t *testing.T) {
	provider := newVacuumTestProvider(t, "incremental")
	ctx := context.Background()
	value, ok, err := ReadMaintenanceState(ctx, provider.Write, "auto_vacuum_incremental")
	if err != nil || ok || value != "" {
		t.Fatalf("initial state value=%q ok=%v err=%v", value, ok, err)
	}
	if err := WriteMaintenanceState(ctx, provider.Write, "auto_vacuum_incremental", `{"vacuumed":true}`); err != nil {
		t.Fatalf("WriteMaintenanceState() error = %v", err)
	}
	value, ok, err = ReadMaintenanceState(ctx, provider.Write, "auto_vacuum_incremental")
	if err != nil || !ok || value != `{"vacuumed":true}` {
		t.Fatalf("stored state value=%q ok=%v err=%v", value, ok, err)
	}
	var settings int
	if err := provider.Write.QueryRowContext(ctx, `SELECT COUNT(1) FROM system_settings`).Scan(&settings); err != nil || settings != 0 {
		t.Fatalf("system_settings count=%d err=%v", settings, err)
	}
}

func seedSupersededOps(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	payload := strings.Repeat("y", 4096)
	old := "2020-01-01T00:00:00Z"
	newer := "2024-01-01T00:00:00Z"
	for i := 0; i < 30; i++ {
		entityID := "ent-" + strconv.Itoa(i)
		for _, item := range []struct {
			opID string
			at   string
		}{
			{opID: "old-" + strconv.Itoa(i), at: old},
			{opID: "new-" + strconv.Itoa(i), at: newer},
		} {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO ha_sync_ops (
					op_id, source_node_id, entity_type, entity_id, op_type,
					entity_version, occurred_at, payload_json, payload_hash
				) VALUES (?, 'node-a', 'news_article', ?, 'upsert', 1, ?, ?, 'hash')
			`, item.opID, entityID, item.at, payload); err != nil {
				t.Fatalf("insert %s: %v", item.opID, err)
			}
		}
	}
}
