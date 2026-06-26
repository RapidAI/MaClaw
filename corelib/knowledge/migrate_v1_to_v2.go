package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	excelread "github.com/RapidAI/CodeClaw/corelib/excel"
)

func migrateV1ToV2(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := migrateV1SourcesToV2(ctx, tx); err != nil {
		return err
	}
	if err := migrateV1SpreadsheetSourcesToV2(ctx, tx); err != nil {
		return err
	}
	if err := migrateV1CardsToV2(ctx, tx); err != nil {
		return err
	}
	if err := migrateV1FactsToV2(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_meta(key, value) VALUES ('migrated_from', 'v1')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		return fmt.Errorf("knowledge sqlite mark v1 migration: %w", err)
	}
	return tx.Commit()
}

func migrateV1SourcesToV2(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO kb_sources
		(id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		 owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at)
		SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		 owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources`)
	if err != nil {
		return fmt.Errorf("knowledge sqlite migrate sources v1->v2: %w", err)
	}
	return nil
}

type legacySpreadsheetNode struct {
	ID         string
	Title      string
	Text       string
	SheetName  string
	RowRange   string
	Offset     int
	Metadata   string
	TokenCount int
}

func migrateV1SpreadsheetSourcesToV2(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at
		FROM knowledge_sources
		WHERE kind IN (?, ?, ?)`, SourceKindXLSX, SourceKindXLS, SourceKindCSV)
	if err != nil {
		return fmt.Errorf("knowledge sqlite list spreadsheet sources v1->v2: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return fmt.Errorf("knowledge sqlite scan spreadsheet source v1->v2: %w", err)
		}
		path := localPathForMigratedSource(source)
		if path == "" {
			if err := migrateV1SpreadsheetNodesDegraded(ctx, tx, source); err != nil {
				return err
			}
			continue
		}
		if _, err := os.Stat(path); err != nil {
			if err := migrateV1SpreadsheetNodesDegraded(ctx, tx, source); err != nil {
				return err
			}
			continue
		}
		if _, err := importSpreadsheetSourceV2(ctx, tx, source, path, source.Kind); err != nil {
			return fmt.Errorf("knowledge sqlite reimport spreadsheet %s v1->v2: %w", source.ID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("knowledge sqlite iterate spreadsheet sources v1->v2: %w", err)
	}
	return nil
}

func migrateV1SpreadsheetNodesDegraded(ctx context.Context, tx *sql.Tx, source Source) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, title, text, sheet_name, row_range, offset, metadata_json, token_count
		FROM document_nodes
		WHERE source_id = ?
		ORDER BY sheet_name, offset, id`, source.ID)
	if err != nil {
		return fmt.Errorf("knowledge sqlite list spreadsheet nodes degraded v1->v2: %w", err)
	}
	defer rows.Close()
	bySheet := make(map[string][]legacySpreadsheetNode)
	order := make([]string, 0)
	for rows.Next() {
		var node legacySpreadsheetNode
		if err := rows.Scan(&node.ID, &node.Title, &node.Text, &node.SheetName, &node.RowRange, &node.Offset, &node.Metadata, &node.TokenCount); err != nil {
			return fmt.Errorf("knowledge sqlite scan spreadsheet node degraded v1->v2: %w", err)
		}
		if strings.TrimSpace(node.Text) == "" {
			continue
		}
		sheetName := fallbackText(node.SheetName, "Sheet1")
		if _, ok := bySheet[sheetName]; !ok {
			order = append(order, sheetName)
		}
		bySheet[sheetName] = append(bySheet[sheetName], node)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("knowledge sqlite iterate spreadsheet nodes degraded v1->v2: %w", err)
	}
	if len(bySheet) == 0 {
		return nil
	}
	now := time.Now().UTC()
	if err := insertKBSource(ctx, tx, source); err != nil {
		return err
	}
	for _, sheetName := range order {
		nodes := bySheet[sheetName]
		tableID := NewID("ktbl")
		headers, valueTypes := inferDegradedSpreadsheetColumns(nodes)
		schemaJSON := degradedSpreadsheetSchemaJSON(headers, valueTypes)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_tables
				(id, source_id, sheet_name, table_title, header_row_index, row_count, column_count, schema_json, created_at, updated_at)
				VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
			tableID, source.ID, sheetName, fallbackText(sheetName, source.Title), len(nodes), len(headers), schemaJSON, formatTime(now), formatTime(now)); err != nil {
			return fmt.Errorf("knowledge sqlite insert degraded kb table: %w", err)
		}
		columnIDs := make([]string, len(headers))
		for i, header := range headers {
			columnID := NewID("kcol")
			columnIDs[i] = columnID
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_columns
					(id, table_id, column_index, column_name, normalized_name, value_type, aliases_json, stats_json, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, '[]', '{"migration_degraded":true}', ?, ?)`,
				columnID, tableID, i+1, header, normalizeSpreadsheetColumnName(header), valueTypes[i], formatTime(now), formatTime(now)); err != nil {
				return fmt.Errorf("knowledge sqlite insert degraded kb column: %w", err)
			}
		}
		for i, node := range nodes {
			rowID := NewID("krow")
			rowIndex := node.Offset
			if rowIndex <= 0 {
				rowIndex = i + 1
			}
			fields := parseLegacySpreadsheetRowFields(node.Text)
			rowText := degradedSpreadsheetRowText(headers, fields, node.Text)
			primaryKey := degradedSpreadsheetPrimaryKey(headers, fields, node, sheetName, rowIndex)
			rowJSONBytes, _ := json.Marshal(degradedSpreadsheetRowJSON(headers, fields, rowText))
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_rows
					(id, table_id, source_id, row_index, primary_key_text, row_text, row_json, embedding, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
				rowID, tableID, source.ID, rowIndex, primaryKey, rowText, string(rowJSONBytes), formatTime(now), formatTime(now)); err != nil {
				return fmt.Errorf("knowledge sqlite insert degraded kb row: %w", err)
			}
			_, _ = tx.ExecContext(ctx, `DELETE FROM kb_rows_fts WHERE row_id = ?`, rowID)
			_, _ = tx.ExecContext(ctx, `INSERT INTO kb_rows_fts(row_id, primary_key_text, row_text) VALUES (?, ?, ?)`,
				rowID, segmentTextForFTS(primaryKey), segmentTextForFTS(rowText))
			rowCells := make([]KnowledgeTableCell, 0, len(headers))
			for colIdx, header := range headers {
				value := fields[header]
				if value == "" && len(fields) == 0 && header == "text" {
					value = rowText
				}
				normalized := normalizeSpreadsheetCell(excelread.CellValue{Value: value}, valueTypes[colIdx])
				if normalized.ValueType == tableValueTypeEmpty {
					continue
				}
				cellID := NewID("kcell")
				cellRecord := KnowledgeTableCell{
					ID:                   cellID,
					RowID:                rowID,
					TableID:              tableID,
					ColumnID:             columnIDs[colIdx],
					ColumnName:           header,
					NormalizedColumnName: normalizeSpreadsheetColumnName(header),
					RawValue:             normalized.RawValue,
					NormalizedValue:      normalized.NormalizedValue,
					ValueType:            normalized.ValueType,
					NumberValue:          normalized.NumberValue,
					DateValue:            normalized.DateValue,
					BoolValue:            normalized.BoolValue,
					CreatedAt:            now,
					UpdatedAt:            now,
				}
				var numberValue interface{}
				if normalized.NumberValue != nil {
					numberValue = *normalized.NumberValue
				}
				var boolValue interface{}
				if normalized.BoolValue != nil {
					if *normalized.BoolValue {
						boolValue = 1
					} else {
						boolValue = 0
					}
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_cells
						(id, row_id, table_id, column_id, column_name, normalized_column_name, raw_value, normalized_value,
						 value_type, number_value, date_value, bool_value, created_at, updated_at)
						VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					cellID, rowID, tableID, columnIDs[colIdx], header, normalizeSpreadsheetColumnName(header),
					normalized.RawValue, normalized.NormalizedValue, normalized.ValueType, numberValue, normalized.DateValue, boolValue,
					formatTime(now), formatTime(now)); err != nil {
					return fmt.Errorf("knowledge sqlite insert degraded kb cell: %w", err)
				}
				rowCells = append(rowCells, cellRecord)
			}
			if err := insertKBTableRowCardAndFacts(ctx, tx, source, KnowledgeTableRow{
				ID:             rowID,
				TableID:        tableID,
				SourceID:       source.ID,
				RowIndex:       rowIndex,
				PrimaryKeyText: primaryKey,
				RowText:        rowText,
				RowJSON:        string(rowJSONBytes),
				CreatedAt:      now,
				UpdatedAt:      now,
			}, rowCells); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseLegacySpreadsheetRowFields(text string) map[string]string {
	fields := make(map[string]string)
	for _, part := range strings.Split(strings.TrimSpace(text), "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sepLen := len(":")
		sep := strings.Index(part, ":")
		if sep < 0 {
			sep = strings.Index(part, "：")
			sepLen = len("：")
		}
		if sep <= 0 {
			continue
		}
		key := strings.TrimSpace(part[:sep])
		value := strings.TrimSpace(part[sep+sepLen:])
		if key != "" && value != "" {
			fields[key] = value
		}
	}
	return fields
}

func inferDegradedSpreadsheetColumns(nodes []legacySpreadsheetNode) ([]string, []string) {
	headers := make([]string, 0)
	seen := make(map[string]struct{})
	countsByHeader := make(map[string]map[string]int)
	for _, node := range nodes {
		for header, value := range parseLegacySpreadsheetRowFields(node.Text) {
			if _, ok := seen[header]; !ok {
				seen[header] = struct{}{}
				headers = append(headers, header)
			}
			if countsByHeader[header] == nil {
				countsByHeader[header] = make(map[string]int)
			}
			typ := inferSpreadsheetCellType(excelread.CellValue{Value: value})
			if typ != tableValueTypeEmpty {
				countsByHeader[header][typ]++
			}
		}
	}
	if len(headers) == 0 {
		headers = []string{"text"}
	}
	valueTypes := make([]string, len(headers))
	for i, header := range headers {
		valueTypes[i] = dominantSpreadsheetType(countsByHeader[header])
	}
	return headers, valueTypes
}

func degradedSpreadsheetSchemaJSON(headers []string, valueTypes []string) string {
	columns := make([]map[string]interface{}, 0, len(headers))
	for i, header := range headers {
		columns = append(columns, map[string]interface{}{
			"index":           i + 1,
			"name":            header,
			"normalized_name": normalizeSpreadsheetColumnName(header),
			"value_type":      valueTypes[i],
		})
	}
	data, _ := json.Marshal(map[string]interface{}{"migration_degraded": true, "columns": columns})
	return string(data)
}

func degradedSpreadsheetRowText(headers []string, fields map[string]string, fallback string) string {
	if len(fields) == 0 {
		return strings.TrimSpace(fallback)
	}
	parts := make([]string, 0, len(headers))
	for _, header := range headers {
		if value := strings.TrimSpace(fields[header]); value != "" {
			parts = append(parts, header+": "+value)
		}
	}
	return strings.Join(parts, " | ")
}

func degradedSpreadsheetPrimaryKey(headers []string, fields map[string]string, node legacySpreadsheetNode, sheetName string, rowIndex int) string {
	preferred := []string{"name", "姓名", "title", "标题", "id", "编号", "code", "编码", "客户", "项目", "案件"}
	for _, needle := range preferred {
		for _, header := range headers {
			if strings.Contains(normalizeSpreadsheetColumnName(header), needle) {
				if value := strings.TrimSpace(fields[header]); value != "" {
					return value
				}
			}
		}
	}
	if title := strings.TrimSpace(node.Title); title != "" {
		return title
	}
	return fmt.Sprintf("%s row %d", sheetName, rowIndex)
}

func degradedSpreadsheetRowJSON(headers []string, fields map[string]string, rowText string) map[string]interface{} {
	values := make(map[string]interface{}, len(headers)+1)
	values["migration_degraded"] = true
	if len(fields) == 0 {
		values["text"] = rowText
		return values
	}
	for _, header := range headers {
		values[header] = fields[header]
	}
	return values
}

func localPathForMigratedSource(source Source) string {
	raw := strings.TrimSpace(source.URI)
	if raw == "" {
		raw = strings.TrimSpace(source.RelativePath)
	}
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "file://") {
		if parsed, err := url.Parse(raw); err == nil {
			if parsed.Path != "" {
				if parsed.Host != "" {
					return `\\` + parsed.Host + filepath.FromSlash(parsed.Path)
				}
				return filepath.FromSlash(parsed.Path)
			}
		}
	}
	return raw
}

func migrateV1CardsToV2(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO kb_cards
		(id, source_id, row_id, origin_type, title, claim, summary, entities_json, topics_json, tags_json,
		 project_path, owner_id, tenant_id, valid_at, invalid_at, confidence, importance, source_trust, embedding, created_at, updated_at)
		SELECT id, source_id, NULL, 'document', title, claim, summary, entities_json, topics_json, tags_json,
		 project_path, owner_id, tenant_id, valid_at, invalid_at, confidence, importance, source_trust, embedding, created_at, updated_at
		FROM knowledge_cards`)
	if err != nil {
		return fmt.Errorf("knowledge sqlite migrate cards v1->v2: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO kb_cards_fts(card_id, title, claim, summary)
		SELECT c.id, c.title, c.claim, c.summary FROM kb_cards c
		WHERE NOT EXISTS (SELECT 1 FROM kb_cards_fts WHERE card_id = c.id)`)
	if err != nil {
		return fmt.Errorf("knowledge sqlite rebuild card fts v2: %w", err)
	}
	return nil
}

func migrateV1FactsToV2(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO kb_facts
		(id, card_id, source_id, row_id, subject, predicate, object, normalized_object, value_type,
		 number_value, date_value, bool_value, negated, valid_at, invalid_at, confidence, created_at)
		SELECT id, card_id, source_id, NULL, subject, predicate, object, object, NULL,
		 NULL, NULL, NULL, negated, valid_at, invalid_at, confidence, ''
		FROM knowledge_facts`)
	if err != nil {
		return fmt.Errorf("knowledge sqlite migrate facts v1->v2: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO kb_facts_fts(fact_id, subject, predicate, object)
		SELECT f.id, f.subject, f.predicate, f.object FROM kb_facts f
		WHERE NOT EXISTS (SELECT 1 FROM kb_facts_fts WHERE fact_id = f.id)`)
	if err != nil {
		return fmt.Errorf("knowledge sqlite rebuild fact fts v2: %w", err)
	}
	return nil
}
