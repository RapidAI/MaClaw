package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	version, err := detectSchemaVersion(ctx, db)
	if err != nil {
		return err
	}
	if version >= knowledgeSchemaVersionV2 {
		if err := createSchemaV2(ctx, db); err != nil {
			return err
		}
		if err := repairStructuredColumnNormalization(ctx, db); err != nil {
			return err
		}
		return repairStructuredCellValues(ctx, db)
	}
	legacy, err := tableExists(ctx, db, "knowledge_sources")
	if err != nil {
		return err
	}
	if err := createTables(db); err != nil {
		return err
	}
	if err := createSchemaV2(ctx, db); err != nil {
		return err
	}
	if version == 0 && legacy {
		if err := migrateV1ToV2(ctx, db); err != nil {
			return err
		}
	}
	if err := repairStructuredColumnNormalization(ctx, db); err != nil {
		return err
	}
	if err := repairStructuredCellValues(ctx, db); err != nil {
		return err
	}
	return setSchemaVersion(ctx, db, knowledgeSchemaVersionV2)
}

func detectSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	exists, err := tableExists(ctx, db, "kb_meta")
	if err != nil || !exists {
		return 0, err
	}
	var raw string
	err = db.QueryRowContext(ctx, `SELECT value FROM kb_meta WHERE key = 'schema_version'`).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("knowledge sqlite detect schema version: %w", err)
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("knowledge sqlite invalid schema version %q: %w", raw, err)
	}
	return version, nil
}

func setSchemaVersion(ctx context.Context, db *sql.DB, version int) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	entries := map[string]string{
		"schema_version": strconv.Itoa(version),
		"migrated_at":    now,
	}
	for key, value := range entries {
		if _, err := db.ExecContext(ctx, `INSERT INTO kb_meta(key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return fmt.Errorf("knowledge sqlite set schema version: %w", err)
		}
	}
	return nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table', 'view') AND name = ?`, name).Scan(&count); err != nil {
		return false, fmt.Errorf("knowledge sqlite inspect table %q: %w", name, err)
	}
	return count > 0, nil
}
