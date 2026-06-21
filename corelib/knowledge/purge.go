package knowledge

import (
	"context"
	"fmt"
)

// PurgeAll deletes all data from the knowledge base and reclaims disk space
// via VACUUM. Unlike deleting the DB file, this works while the database
// connection is open (no file-lock conflicts on Windows).
func (s *SQLiteStore) PurgeAll(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge store not initialized")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete all data from all tables (reuses the snapshot import clear logic).
	if err := s.clearSnapshotImportTargetTx(ctx, tx); err != nil {
		return fmt.Errorf("clear tables: %w", err)
	}

	// Note: we intentionally do NOT clear knowledge_meta. It only contains
	// the FTS segmentation version marker. After purge the FTS tables are
	// empty (correct state), and keeping the marker prevents a useless FTS
	// rebuild on the next auto-recall store open.

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// VACUUM must run outside a transaction — it reclaims disk space.
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	return nil
}
