package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

const structuredColumnNormalizationVersion = 2

func repairStructuredColumnNormalization(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("knowledge sqlite repair column normalization: db is nil")
	}
	current, err := structuredColumnNormalizationMetaVersion(ctx, db)
	if err != nil {
		return err
	}
	if current >= structuredColumnNormalizationVersion {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("knowledge sqlite repair column normalization: begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, column_name, normalized_name FROM kb_columns`)
	if err != nil {
		return fmt.Errorf("knowledge sqlite repair column normalization: list columns: %w", err)
	}
	type columnRepair struct {
		id         string
		normalized string
	}
	repairs := make([]columnRepair, 0)
	for rows.Next() {
		var id, name, normalized string
		if err := rows.Scan(&id, &name, &normalized); err != nil {
			_ = rows.Close()
			return fmt.Errorf("knowledge sqlite repair column normalization: scan column: %w", err)
		}
		next := normalizeSpreadsheetColumnName(name)
		if next != "" && next != normalized {
			repairs = append(repairs, columnRepair{id: id, normalized: next})
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("knowledge sqlite repair column normalization: close columns: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("knowledge sqlite repair column normalization: iterate columns: %w", err)
	}

	for _, repair := range repairs {
		if _, err := tx.ExecContext(ctx, `UPDATE kb_columns SET normalized_name = ? WHERE id = ?`, repair.normalized, repair.id); err != nil {
			return fmt.Errorf("knowledge sqlite repair column normalization: update column: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE kb_cells SET normalized_column_name = ? WHERE column_id = ?`, repair.normalized, repair.id); err != nil {
			return fmt.Errorf("knowledge sqlite repair column normalization: update cells: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"structured_column_normalization_version", strconv.Itoa(structuredColumnNormalizationVersion)); err != nil {
		return fmt.Errorf("knowledge sqlite repair column normalization: set version: %w", err)
	}
	return tx.Commit()
}

func structuredColumnNormalizationMetaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM kb_meta WHERE key = 'structured_column_normalization_version'`).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("knowledge sqlite repair column normalization: read version: %w", err)
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return 0, nil
	}
	return version, nil
}
