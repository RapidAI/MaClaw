package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// maxIncrementalVacuumPages matches dbmaint.MaxIncrementalVacuumPages.
// The store package does not import the scheduler so a prune can release
// pages without taking a dependency on the policy types.
const maxIncrementalVacuumPages = 128

var incrementalVacuumLogOnce sync.Once

// CountHASyncOps returns the number of rows in ha_sync_ops.
func CountHASyncOps(ctx context.Context, db *sql.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("nil sqlite database")
	}
	var n int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM ha_sync_ops`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// AutoVacuumMode reads the persistent PRAGMA auto_vacuum value.
// 0 is NONE, 1 is FULL, 2 is INCREMENTAL.
func AutoVacuumMode(ctx context.Context, db *sql.DB) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("nil sqlite database")
	}
	var mode int
	if err := db.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return 0, err
	}
	return mode, nil
}

// FreelistCount reads PRAGMA freelist_count.
func FreelistCount(ctx context.Context, db *sql.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("nil sqlite database")
	}
	var n int64
	if err := db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// IncrementalVacuum releases up to pages freelist pages back to the OS.
// The pragma emits one row per moved page, and modernc.org/sqlite only
// performs the work while that row set is drained, so this uses Query.
// On a database that is not auto_vacuum=INCREMENTAL the pragma is a no-op.
// pages is clamped to maxIncrementalVacuumPages.
func IncrementalVacuum(ctx context.Context, db *sql.DB, pages int) error {
	if db == nil || pages <= 0 {
		return nil
	}
	if pages > maxIncrementalVacuumPages {
		pages = maxIncrementalVacuumPages
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA incremental_vacuum(%d)", pages))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// ReadMaintenanceState returns the local completion record for key.
// The second result is false when the row does not exist.
func ReadMaintenanceState(ctx context.Context, db *sql.DB, key string) (string, bool, error) {
	if db == nil {
		return "", false, fmt.Errorf("nil sqlite database")
	}
	var value string
	err := db.QueryRowContext(ctx, `SELECT value_json FROM local_maintenance_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// WriteMaintenanceState upserts a node-local maintenance record. Callers
// must not write this through the HA system-settings path.
func WriteMaintenanceState(ctx context.Context, db *sql.DB, key, value string) error {
	if db == nil {
		return fmt.Errorf("nil sqlite database")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO local_maintenance_state (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, key, value, time.Now().UTC().Format(time.RFC3339))
	return err
}

// releaseFreedPages runs a bounded incremental_vacuum after a delete batch.
// A failure is logged once per process and does not fail the prune: the
// rows are already committed, and the next batch or the checkpointer can
// retry. A cancelled context is returned so the prune loop stops.
func (r *haSyncOpRepo) releaseFreedPages(ctx context.Context) error {
	if r == nil || r.incrementalVacuumPages <= 0 || r.db == nil {
		return nil
	}
	err := IncrementalVacuum(ctx, r.db, r.incrementalVacuumPages)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	pages := r.incrementalVacuumPages
	incrementalVacuumLogOnce.Do(func() {
		log.Printf("[hubcenter][db] incremental_vacuum(%d) after history prune: %v", pages, err)
	})
	return nil
}
