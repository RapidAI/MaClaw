package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	excelread "github.com/RapidAI/CodeClaw/corelib/excel"
)

const (
	tableValueTypeString = "string"
	tableValueTypeNumber = "number"
	tableValueTypeBool   = "bool"
	tableValueTypeDate   = "date"
	tableValueTypeMixed  = "mixed"
	tableValueTypeEmpty  = "empty"
)

type normalizedTableCell struct {
	RawValue        string
	NormalizedValue string
	ValueType       string
	NumberValue     *float64
	DateValue       string
	BoolValue       *bool
}

func importSpreadsheetSourceV2(ctx context.Context, tx *sql.Tx, source Source, filePath string, kind string) (Source, error) {
	if !isSpreadsheetKind(kind) {
		return source, nil
	}
	if err := insertKBSource(ctx, tx, source); err != nil {
		return source, err
	}
	sheets, err := excelread.ListSheets(filePath)
	if err != nil {
		return source, err
	}
	now := time.Now().UTC()
	tableCount := 0
	rowCount := 0
	for _, sheetName := range sheets {
		result, err := excelread.ReadFile(filePath, excelread.ReadOptions{SheetName: sheetName})
		if err != nil {
			return source, err
		}
		if result == nil || len(result.Rows) == 0 {
			continue
		}
		tableID := NewID("ktbl")
		headerRow := detectSpreadsheetHeaderRow(result.Rows)
		headers := spreadsheetHeaders(result.Rows, headerRow, result.ColCount)
		valueTypes := inferSpreadsheetColumnTypes(result.Rows, headerRow, len(headers))
		schemaJSON := spreadsheetSchemaJSON(headers, valueTypes)
		tableTitle := fallbackText(result.SheetName, source.Title)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_tables
			(id, source_id, sheet_name, table_title, header_row_index, row_count, column_count, schema_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tableID, source.ID, result.SheetName, tableTitle, headerRow+1, maxInt(0, len(result.Rows)-headerRow-1), len(headers), schemaJSON, formatTime(now), formatTime(now)); err != nil {
			return source, fmt.Errorf("knowledge sqlite insert kb table: %w", err)
		}
		columnIDs := make([]string, len(headers))
		for i, header := range headers {
			columnID := NewID("kcol")
			columnIDs[i] = columnID
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_columns
				(id, table_id, column_index, column_name, normalized_name, value_type, aliases_json, stats_json, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				columnID, tableID, i+1, header, normalizeSpreadsheetColumnName(header), valueTypes[i], "[]", "{}", formatTime(now), formatTime(now)); err != nil {
				return source, fmt.Errorf("knowledge sqlite insert kb column: %w", err)
			}
		}
		for rowIdx := headerRow + 1; rowIdx < len(result.Rows); rowIdx++ {
			row := result.Rows[rowIdx]
			if spreadsheetRowEmpty(row) {
				continue
			}
			rowID := NewID("krow")
			rowText := buildSpreadsheetRowText(headers, row)
			primaryKeyText := buildSpreadsheetPrimaryKey(headers, row)
			rowJSON := spreadsheetRowJSON(headers, row)
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_rows
				(id, table_id, source_id, row_index, primary_key_text, row_text, row_json, embedding, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
				rowID, tableID, source.ID, rowIdx+1, primaryKeyText, rowText, rowJSON, formatTime(now), formatTime(now)); err != nil {
				return source, fmt.Errorf("knowledge sqlite insert kb row: %w", err)
			}
			_, _ = tx.ExecContext(ctx, `DELETE FROM kb_rows_fts WHERE row_id = ?`, rowID)
			_, _ = tx.ExecContext(ctx, `INSERT INTO kb_rows_fts(row_id, primary_key_text, row_text) VALUES (?, ?, ?)`,
				rowID, segmentTextForFTS(primaryKeyText), segmentTextForFTS(rowText))
			rowCells := make([]KnowledgeTableCell, 0, len(headers))
			for colIdx := 0; colIdx < len(headers); colIdx++ {
				cell := cellAt(row, colIdx)
				normalized := normalizeSpreadsheetCell(cell, valueTypes[colIdx])
				if normalized.ValueType == tableValueTypeEmpty {
					continue
				}
				cellID := NewID("kcell")
				cellRecord := KnowledgeTableCell{
					ID:                   cellID,
					RowID:                rowID,
					TableID:              tableID,
					ColumnID:             columnIDs[colIdx],
					ColumnName:           headers[colIdx],
					NormalizedColumnName: normalizeSpreadsheetColumnName(headers[colIdx]),
					RawValue:             normalized.RawValue,
					NormalizedValue:      normalized.NormalizedValue,
					ValueType:            normalized.ValueType,
					NumberValue:          normalized.NumberValue,
					DateValue:            normalized.DateValue,
					BoolValue:            normalized.BoolValue,
					CreatedAt:            now,
					UpdatedAt:            now,
				}
				var boolValue interface{}
				if normalized.BoolValue != nil {
					if *normalized.BoolValue {
						boolValue = 1
					} else {
						boolValue = 0
					}
				}
				var numberValue interface{}
				if normalized.NumberValue != nil {
					numberValue = *normalized.NumberValue
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_cells
					(id, row_id, table_id, column_id, column_name, normalized_column_name, raw_value, normalized_value,
					 value_type, number_value, date_value, bool_value, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					cellID, rowID, tableID, columnIDs[colIdx], headers[colIdx], normalizeSpreadsheetColumnName(headers[colIdx]),
					normalized.RawValue, normalized.NormalizedValue, normalized.ValueType, numberValue, normalized.DateValue, boolValue,
					formatTime(now), formatTime(now)); err != nil {
					return source, fmt.Errorf("knowledge sqlite insert kb cell: %w", err)
				}
				rowCells = append(rowCells, cellRecord)
			}
			if err := insertKBTableRowCardAndFacts(ctx, tx, source, KnowledgeTableRow{
				ID:             rowID,
				TableID:        tableID,
				SourceID:       source.ID,
				RowIndex:       rowIdx + 1,
				PrimaryKeyText: primaryKeyText,
				RowText:        rowText,
				RowJSON:        rowJSON,
				CreatedAt:      now,
				UpdatedAt:      now,
			}, rowCells); err != nil {
				return source, err
			}
			rowCount++
		}
		tableCount++
	}
	source.NodeCount = rowCount
	_ = tableCount
	return source, nil
}

func insertKBSource(ctx context.Context, tx *sql.Tx, source Source) error {
	source = normalizeSource(source)
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kb_sources
		(id, kind, uri, canonical_uri, title, author, site_name, published_at, fetched_at, content_hash,
		 owner_id, tenant_id, project_path, topic_hint, source_trust, batch_id, relative_path, status, error_message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		source.ID, source.Kind, source.URI, source.CanonicalURI, source.Title, source.Author, source.SiteName,
		formatTime(source.PublishedAt), formatTime(source.FetchedAt), source.ContentHash, source.OwnerID, source.TenantID,
		source.ProjectPath, source.TopicHint, source.SourceTrust, source.BatchID, source.RelativePath, source.Status,
		source.ErrorMessage, formatTime(source.CreatedAt), formatTime(source.UpdatedAt))
	if err != nil {
		return fmt.Errorf("knowledge sqlite insert kb source: %w", err)
	}
	return nil
}

func isSpreadsheetKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case SourceKindXLSX, SourceKindXLS, SourceKindCSV:
		return true
	default:
		return false
	}
}

func detectSpreadsheetHeaderRow(rows [][]excelread.CellValue) int {
	bestIndex := 0
	bestScore := -1
	limit := minInt(len(rows), 10)
	for i := 0; i < limit; i++ {
		score := 0
		seen := map[string]struct{}{}
		for _, cell := range rows[i] {
			text := strings.TrimSpace(fmt.Sprint(cell.Value))
			if text == "" || cell.Value == nil {
				continue
			}
			if _, err := strconv.ParseFloat(text, 64); err == nil {
				score--
				continue
			}
			key := strings.ToLower(text)
			if _, ok := seen[key]; !ok {
				score += 2
				seen[key] = struct{}{}
			}
		}
		if score > bestScore {
			bestScore = score
			bestIndex = i
		}
	}
	return bestIndex
}

func spreadsheetHeaders(rows [][]excelread.CellValue, headerRow int, colCount int) []string {
	if headerRow >= 0 && headerRow < len(rows) && len(rows[headerRow]) > colCount {
		colCount = len(rows[headerRow])
	}
	headers := make([]string, colCount)
	seen := map[string]int{}
	for i := range headers {
		raw := ""
		if headerRow >= 0 && headerRow < len(rows) && i < len(rows[headerRow]) && rows[headerRow][i].Value != nil {
			raw = strings.TrimSpace(fmt.Sprint(rows[headerRow][i].Value))
		}
		headers[i] = normalizeSpreadsheetHeader(raw, i, seen)
	}
	return headers
}

func normalizeSpreadsheetHeader(raw string, index int, seen map[string]int) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = fmt.Sprintf("Column %d", index+1)
	}
	base := name
	key := normalizeSpreadsheetColumnName(base)
	seen[key]++
	if seen[key] > 1 {
		name = fmt.Sprintf("%s %d", base, seen[key])
	}
	return name
}

func normalizeSpreadsheetColumnName(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func inferSpreadsheetColumnTypes(rows [][]excelread.CellValue, headerRow int, colCount int) []string {
	types := make([]string, colCount)
	for col := 0; col < colCount; col++ {
		counts := map[string]int{}
		for rowIdx := headerRow + 1; rowIdx < len(rows); rowIdx++ {
			cell := cellAt(rows[rowIdx], col)
			typ := inferSpreadsheetCellType(cell)
			if typ != tableValueTypeEmpty {
				counts[typ]++
			}
		}
		types[col] = dominantSpreadsheetType(counts)
	}
	return types
}

func inferSpreadsheetCellType(cell excelread.CellValue) string {
	if cell.Value == nil {
		return tableValueTypeEmpty
	}
	switch cell.Type {
	case excelread.CellTypeNumber:
		return tableValueTypeNumber
	case excelread.CellTypeBool:
		return tableValueTypeBool
	}
	text := strings.TrimSpace(fmt.Sprint(cell.Value))
	if text == "" {
		return tableValueTypeEmpty
	}
	if looksLikeSpreadsheetTextCode(text) {
		return tableValueTypeString
	}
	if _, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", ""), 64); err == nil {
		return tableValueTypeNumber
	}
	if _, ok := parseSpreadsheetBool(text); ok {
		return tableValueTypeBool
	}
	if normalizedDate(text) != "" {
		return tableValueTypeDate
	}
	return tableValueTypeString
}

func looksLikeSpreadsheetTextCode(value string) bool {
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", "")
	if len(value) < 2 || value[0] != '0' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func dominantSpreadsheetType(counts map[string]int) string {
	if len(counts) == 0 {
		return tableValueTypeString
	}
	bestType := tableValueTypeString
	bestCount := 0
	total := 0
	for typ, count := range counts {
		total += count
		if count > bestCount {
			bestType = typ
			bestCount = count
		}
	}
	if bestCount*2 <= total {
		return tableValueTypeMixed
	}
	return bestType
}

func buildSpreadsheetRowText(headers []string, row []excelread.CellValue) string {
	parts := make([]string, 0, len(headers))
	for i, header := range headers {
		text := spreadsheetCellText(cellAt(row, i))
		if text != "" {
			parts = append(parts, header+": "+text)
		}
	}
	return strings.Join(parts, " | ")
}

func buildSpreadsheetPrimaryKey(headers []string, row []excelread.CellValue) string {
	preferred := []string{"name", "姓名", "title", "标题", "id", "编号", "code", "编码", "客户", "项目", "案件"}
	for _, needle := range preferred {
		for i, header := range headers {
			if strings.Contains(normalizeSpreadsheetColumnName(header), needle) {
				if text := spreadsheetCellText(cellAt(row, i)); text != "" {
					return text
				}
			}
		}
	}
	values := make([]string, 0, 2)
	for i := range headers {
		if text := spreadsheetCellText(cellAt(row, i)); text != "" {
			values = append(values, text)
			if len(values) == 2 {
				break
			}
		}
	}
	return strings.Join(values, " ")
}

func spreadsheetRowJSON(headers []string, row []excelread.CellValue) string {
	values := make(map[string]string, len(headers))
	for i, header := range headers {
		values[header] = spreadsheetCellText(cellAt(row, i))
	}
	data, _ := json.Marshal(values)
	return string(data)
}

func spreadsheetSchemaJSON(headers []string, valueTypes []string) string {
	columns := make([]map[string]interface{}, 0, len(headers))
	for i, header := range headers {
		columns = append(columns, map[string]interface{}{
			"index":           i + 1,
			"name":            header,
			"normalized_name": normalizeSpreadsheetColumnName(header),
			"value_type":      valueTypes[i],
		})
	}
	data, _ := json.Marshal(map[string]interface{}{"columns": columns})
	return string(data)
}

func normalizeSpreadsheetCell(cell excelread.CellValue, inferredType string) normalizedTableCell {
	raw := spreadsheetCellText(cell)
	if raw == "" {
		return normalizedTableCell{ValueType: tableValueTypeEmpty}
	}
	typ := inferredType
	if typ == "" || typ == tableValueTypeMixed {
		typ = inferSpreadsheetCellType(cell)
	}
	normalized := normalizedTableCell{RawValue: raw, NormalizedValue: strings.TrimSpace(raw), ValueType: typ}
	if typ == tableValueTypeNumber {
		if parsed, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64); err == nil {
			normalized.NumberValue = &parsed
			normalized.NormalizedValue = strconv.FormatFloat(parsed, 'f', -1, 64)
		}
	}
	if typ == tableValueTypeBool {
		if parsed, ok := parseSpreadsheetBool(raw); ok {
			normalized.BoolValue = &parsed
			normalized.NormalizedValue = strconv.FormatBool(parsed)
		}
	}
	if typ == tableValueTypeDate {
		if date := normalizedDate(raw); date != "" {
			normalized.DateValue = date
			normalized.NormalizedValue = date
		}
	}
	return normalized
}

func spreadsheetRowEmpty(row []excelread.CellValue) bool {
	for _, cell := range row {
		if spreadsheetCellText(cell) != "" {
			return false
		}
	}
	return true
}

func spreadsheetCellText(cell excelread.CellValue) string {
	if cell.Value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(cell.Value))
}

func cellAt(row []excelread.CellValue, index int) excelread.CellValue {
	if index < 0 || index >= len(row) {
		return excelread.CellValue{Type: excelread.CellTypeEmpty}
	}
	return row[index]
}

func parseSpreadsheetBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "y", "1", "是", "对", "启用":
		return true, true
	case "false", "no", "n", "0", "否", "错", "停用":
		return false, true
	default:
		return false, false
	}
}

func normalizedDate(value string) string {
	value = strings.TrimSpace(value)
	formats := []string{"2006-01-02", "2006/01/02", "2006.01.02", "2006-1-2", "2006/1/2", "2006.1.2", time.RFC3339, time.RFC3339Nano}
	for _, layout := range formats {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
