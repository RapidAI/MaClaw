package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

const structuredCellValueRepairVersion = 2

func repairStructuredCellValues(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("knowledge sqlite repair structured cell values: db is nil")
	}
	current, err := structuredCellValueRepairMetaVersion(ctx, db)
	if err != nil {
		return err
	}
	if current >= structuredCellValueRepairVersion {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("knowledge sqlite repair structured cell values: begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, row_id, column_id, column_name, raw_value, normalized_value FROM kb_cells WHERE value_type = ? AND COALESCE(raw_value, '') <> ''`, tableValueTypeNumber)
	if err != nil {
		return fmt.Errorf("knowledge sqlite repair structured cell values: list cells: %w", err)
	}
	type cellRepair struct {
		id         string
		rowID      string
		columnID   string
		columnName string
		raw        string
		normalized string
	}
	repairs := make([]cellRepair, 0)
	for rows.Next() {
		var repair cellRepair
		if err := rows.Scan(&repair.id, &repair.rowID, &repair.columnID, &repair.columnName, &repair.raw, &repair.normalized); err != nil {
			_ = rows.Close()
			return fmt.Errorf("knowledge sqlite repair structured cell values: scan cell: %w", err)
		}
		if looksLikeSpreadsheetTextCode(repair.raw) && repair.raw != repair.normalized {
			repairs = append(repairs, repair)
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("knowledge sqlite repair structured cell values: close cells: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("knowledge sqlite repair structured cell values: iterate cells: %w", err)
	}

	repairedColumns := make(map[string]struct{})
	for _, repair := range repairs {
		if _, err := tx.ExecContext(ctx, `UPDATE kb_cells SET value_type = ?, normalized_value = ?, number_value = NULL WHERE id = ?`, tableValueTypeString, repair.raw, repair.id); err != nil {
			return fmt.Errorf("knowledge sqlite repair structured cell values: update cell: %w", err)
		}
		if repair.columnID != "" {
			repairedColumns[repair.columnID] = struct{}{}
		}
		factRows, err := tx.QueryContext(ctx, `SELECT id, subject, predicate FROM kb_facts WHERE row_id = ? AND predicate = ? AND normalized_object = ? AND value_type = ?`, repair.rowID, repair.columnName, repair.normalized, tableValueTypeNumber)
		if err != nil {
			return fmt.Errorf("knowledge sqlite repair structured cell values: list facts: %w", err)
		}
		type factRepair struct {
			id        string
			subject   string
			predicate string
		}
		factRepairs := make([]factRepair, 0)
		for factRows.Next() {
			var fact factRepair
			if err := factRows.Scan(&fact.id, &fact.subject, &fact.predicate); err != nil {
				_ = factRows.Close()
				return fmt.Errorf("knowledge sqlite repair structured cell values: scan fact: %w", err)
			}
			factRepairs = append(factRepairs, fact)
		}
		if err := factRows.Close(); err != nil {
			return fmt.Errorf("knowledge sqlite repair structured cell values: close facts: %w", err)
		}
		if err := factRows.Err(); err != nil {
			return fmt.Errorf("knowledge sqlite repair structured cell values: iterate facts: %w", err)
		}
		for _, fact := range factRepairs {
			if _, err := tx.ExecContext(ctx, `UPDATE kb_facts SET object = ?, normalized_object = ?, value_type = ?, number_value = NULL WHERE id = ?`, repair.raw, repair.raw, tableValueTypeString, fact.id); err != nil {
				return fmt.Errorf("knowledge sqlite repair structured cell values: update fact: %w", err)
			}
			_, _ = tx.ExecContext(ctx, `DELETE FROM kb_facts_fts WHERE fact_id = ?`, fact.id)
			_, _ = tx.ExecContext(ctx, `INSERT INTO kb_facts_fts(fact_id, subject, predicate, object) VALUES (?, ?, ?, ?)`, fact.id, segmentTextForFTS(fact.subject), segmentTextForFTS(fact.predicate), segmentTextForFTS(repair.raw))
		}
	}
	for columnID := range repairedColumns {
		if _, err := tx.ExecContext(ctx, `UPDATE kb_columns SET value_type = ? WHERE id = ? AND value_type = ?`, tableValueTypeMixed, columnID, tableValueTypeNumber); err != nil {
			return fmt.Errorf("knowledge sqlite repair structured cell values: update column: %w", err)
		}
	}
	if current < 2 {
		codeColumnRows, err := tx.QueryContext(ctx, `SELECT DISTINCT column_id, raw_value FROM kb_cells WHERE COALESCE(column_id, '') <> '' AND LTRIM(COALESCE(raw_value, '')) LIKE '0%' AND value_type IN (?, ?)`, tableValueTypeString, tableValueTypeNumber)
		if err != nil {
			return fmt.Errorf("knowledge sqlite repair structured cell values: list code columns: %w", err)
		}
		codeColumns := make(map[string]struct{})
		for codeColumnRows.Next() {
			var columnID, raw string
			if err := codeColumnRows.Scan(&columnID, &raw); err != nil {
				_ = codeColumnRows.Close()
				return fmt.Errorf("knowledge sqlite repair structured cell values: scan code column: %w", err)
			}
			if looksLikeSpreadsheetTextCode(raw) {
				codeColumns[columnID] = struct{}{}
			}
		}
		if err := codeColumnRows.Close(); err != nil {
			return fmt.Errorf("knowledge sqlite repair structured cell values: close code columns: %w", err)
		}
		if err := codeColumnRows.Err(); err != nil {
			return fmt.Errorf("knowledge sqlite repair structured cell values: iterate code columns: %w", err)
		}
		for columnID := range codeColumns {
			if _, err := tx.ExecContext(ctx, `UPDATE kb_columns SET value_type = ? WHERE id = ? AND value_type = ?`, tableValueTypeMixed, columnID, tableValueTypeNumber); err != nil {
				return fmt.Errorf("knowledge sqlite repair structured cell values: update code column: %w", err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"structured_cell_value_repair_version", strconv.Itoa(structuredCellValueRepairVersion)); err != nil {
		return fmt.Errorf("knowledge sqlite repair structured cell values: set version: %w", err)
	}
	return tx.Commit()
}

func structuredCellValueRepairMetaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM kb_meta WHERE key = 'structured_cell_value_repair_version'`).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("knowledge sqlite repair structured cell values: read version: %w", err)
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return 0, nil
	}
	return version, nil
}
