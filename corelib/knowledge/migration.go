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
		if err := scrubLegacyImageAssetPaths(ctx, db); err != nil {
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
	if err := scrubLegacyImageAssetPaths(ctx, db); err != nil {
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

// scrubLegacyImageAssetPaths removes the historical metadata fields that do
// not satisfy the managed-image contract. It is intentionally idempotent and
// runs on every open so existing databases become safe without a schema bump:
// image_asset_path can disclose a host path, and image_asset_id must be a
// canonical opaque token just like IDs accepted on new writes.
func scrubLegacyImageAssetPaths(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.ExecContext(ctx, `UPDATE document_nodes
		SET metadata_json = json_remove(COALESCE(metadata_json, '{}'), '$.image_asset_path')
		WHERE json_type(COALESCE(metadata_json, '{}'), '$.image_asset_path') IS NOT NULL`); err != nil {
		return fmt.Errorf("knowledge sqlite scrub legacy image asset paths: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE document_nodes
		SET metadata_json = json_remove(COALESCE(metadata_json, '{}'), '$.image_asset_id')
		WHERE json_type(COALESCE(metadata_json, '{}'), '$.image_asset_id') != 'text'
		   OR length(json_extract(COALESCE(metadata_json, '{}'), '$.image_asset_id')) NOT BETWEEN 1 AND 200
		   OR json_extract(COALESCE(metadata_json, '{}'), '$.image_asset_id') GLOB '*[^A-Za-z0-9_-]*'`); err != nil {
		return fmt.Errorf("knowledge sqlite scrub invalid image asset IDs: %w", err)
	}
	return nil
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
