package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
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

// SpreadsheetRowProgressFunc reports row-level progress during spreadsheet import.
// totalRows is the total data rows across all sheets, processedRows is how many have been inserted so far.
type SpreadsheetRowProgressFunc func(processedRows, totalRows int)

// spreadsheetPreparedRow holds pre-computed data for a single row, produced in parallel.
type spreadsheetPreparedRow struct {
	rowIdx         int
	rowID          string
	rowText        string
	primaryKeyText string
	rowJSON        string
	ftsRowText     string // pre-tokenized for FTS (expensive CJK segmentation)
	ftsPKText      string // pre-tokenized primary key for FTS
	cells          []spreadsheetPreparedCell
	// Pre-computed card/fact FTS data (avoids segmentTextForFTS calls in SQL phase)
	ftsTitle   string // segmentTextForFTS(title)
	ftsSubject string // segmentTextForFTS(subject) — same for all facts in this row
}

// spreadsheetPreparedCell holds pre-computed cell data.
type spreadsheetPreparedCell struct {
	cellID          string
	colIdx          int
	rawValue        string
	normalizedValue string
	valueType       string
	numberValue     *float64
	dateValue       string
	boolValue       *bool
	ftsObject       string // segmentTextForFTS(cleanFactPart(normalizedValue))
}

func importSpreadsheetSourceV2(ctx context.Context, tx *sql.Tx, source Source, filePath string, kind string) (Source, error) {
	return importSpreadsheetSourceV2WithProgress(ctx, tx, source, filePath, kind, nil)
}

func importSpreadsheetSourceV2WithProgress(ctx context.Context, tx *sql.Tx, source Source, filePath string, kind string, onRowProgress SpreadsheetRowProgressFunc) (Source, error) {
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

	// Read all sheets into memory once (avoids double-read for pre-count + processing)
	type sheetData struct {
		result    *excelread.ReadResult
		headerRow int
	}
	sheetResults := make([]sheetData, 0, len(sheets))
	totalDataRows := 0
	for _, sheetName := range sheets {
		result, err := excelread.ReadFile(filePath, excelread.ReadOptions{SheetName: sheetName})
		if err != nil {
			return source, err
		}
		if result == nil || len(result.Rows) == 0 {
			continue
		}
		headerRow := detectSpreadsheetHeaderRow(result.Rows)
		dataRows := 0
		for rowIdx := headerRow + 1; rowIdx < len(result.Rows); rowIdx++ {
			if !spreadsheetRowEmpty(result.Rows[rowIdx]) {
				dataRows++
			}
		}
		totalDataRows += dataRows
		sheetResults = append(sheetResults, sheetData{result: result, headerRow: headerRow})
	}
	processedDataRows := 0

	now := time.Now().UTC()
	tableCount := 0
	rowCount := 0
	for _, sd := range sheetResults {
		result := sd.result
		headerRow := sd.headerRow
		tableID := NewID("ktbl")
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
		// --- Batched parallel row processing ---
		// Phase 1: Collect non-empty row indices
		dataRowIndices := make([]int, 0, len(result.Rows)-headerRow-1)
		for rowIdx := headerRow + 1; rowIdx < len(result.Rows); rowIdx++ {
			if !spreadsheetRowEmpty(result.Rows[rowIdx]) {
				dataRowIndices = append(dataRowIndices, rowIdx)
			}
		}

		if len(dataRowIndices) == 0 {
			tableCount++
			continue
		}

		// Pre-compute per-column invariants (same for all rows in this sheet)
		normalizedColNames := make([]string, len(headers))
		for i, h := range headers {
			normalizedColNames[i] = normalizeSpreadsheetColumnName(h)
		}
		// Pre-compute FTS-tokenized predicate for each column (predicate = cleanFactPart(columnName))
		ftsPredicates := make([]string, len(headers))
		for i, h := range headers {
			ftsPredicates[i] = segmentTextForFTS(cleanFactPart(h))
		}
		// Pre-compute primary key column index for fast lookup in goroutines
		primaryKeyColIdx := findPrimaryKeyColumnIdx(headers, normalizedColNames)
		// Pre-compute per-source invariants for card/fact insertion
		tagsJSON, _ := json.Marshal([]string{source.Kind, "table_row", "structured"})
		topicsJSON, _ := json.Marshal(topicsForSource(source))
		tagsJSONStr := string(tagsJSON)
		topicsJSONStr := string(topicsJSON)

		// Prepare statements once per sheet (reused across all batches)
		stmtRow, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO kb_rows
			(id, table_id, source_id, row_index, primary_key_text, row_text, row_json, embedding, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`)
		if err != nil {
			return source, fmt.Errorf("knowledge sqlite prepare kb_rows: %w", err)
		}
		stmtRowFTSIns, err := tx.PrepareContext(ctx, `INSERT INTO kb_rows_fts(row_id, primary_key_text, row_text) VALUES (?, ?, ?)`)
		if err != nil {
			stmtRow.Close()
			return source, fmt.Errorf("knowledge sqlite prepare kb_rows_fts ins: %w", err)
		}
		stmtCell, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO kb_cells
			(id, row_id, table_id, column_id, column_name, normalized_column_name, raw_value, normalized_value,
			 value_type, number_value, date_value, bool_value, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			stmtRow.Close()
			stmtRowFTSIns.Close()
			return source, fmt.Errorf("knowledge sqlite prepare kb_cells: %w", err)
		}
		cardFactStmts, err := prepareBatchCardFactStmts(ctx, tx)
		if err != nil {
			stmtRow.Close()
			stmtRowFTSIns.Close()
			stmtCell.Close()
			return source, fmt.Errorf("knowledge sqlite prepare card/fact stmts: %w", err)
		}
		closeAllStmts := func() {
			stmtRow.Close()
			stmtRowFTSIns.Close()
			stmtCell.Close()
			cardFactStmts.Close()
		}

		// Phase 2+3: Parallel CPU preparation + sequential SQL insert, in batches
		const prepBatchSize = 200
		nowStr := formatTime(now)
		for batchStart := 0; batchStart < len(dataRowIndices); batchStart += prepBatchSize {
			if ctx.Err() != nil {
				closeAllStmts()
				return source, ctx.Err()
			}
			batchEnd := batchStart + prepBatchSize
			if batchEnd > len(dataRowIndices) {
				batchEnd = len(dataRowIndices)
			}
			batchIndices := dataRowIndices[batchStart:batchEnd]

			// Phase 2: Parallel CPU-bound preparation
			prepared := make([]spreadsheetPreparedRow, len(batchIndices))
			workers := runtime.NumCPU()
			if workers > 8 {
				workers = 8
			}
			if workers > len(batchIndices) {
				workers = len(batchIndices)
			}

			var wg sync.WaitGroup
			chunkSize := (len(batchIndices) + workers - 1) / workers
			for w := 0; w < workers; w++ {
				wStart := w * chunkSize
				wEnd := wStart + chunkSize
				if wEnd > len(batchIndices) {
					wEnd = len(batchIndices)
				}
				if wStart >= wEnd {
					break
				}
				wg.Add(1)
				go func(start, end int) {
					defer wg.Done()
					for i := start; i < end; i++ {
						rowIdx := batchIndices[i]
						row := result.Rows[rowIdx]
						rowID := NewID("krow")
						rowText := buildSpreadsheetRowText(headers, row)
						primaryKeyText := buildSpreadsheetPrimaryKeyFast(headers, row, primaryKeyColIdx)
						rowJSON := spreadsheetRowJSON(headers, row)
						// Pre-compute FTS tokenization (expensive for CJK)
						ftsRowText := segmentTextForFTS(rowText)
						ftsPKText := segmentTextForFTS(primaryKeyText)

						// Pre-compute cells
						cells := make([]spreadsheetPreparedCell, 0, len(headers))
						for colIdx := 0; colIdx < len(headers); colIdx++ {
							cell := cellAt(row, colIdx)
							normalized := normalizeSpreadsheetCell(cell, valueTypes[colIdx])
							if normalized.ValueType == tableValueTypeEmpty {
								continue
							}
							cellID := NewID("kcell")
							// Pre-compute fact object FTS
							ftsObj := ""
							if obj := cleanFactPart(normalized.NormalizedValue); obj != "" {
								ftsObj = segmentTextForFTS(obj)
							}
							cells = append(cells, spreadsheetPreparedCell{
								cellID:          cellID,
								colIdx:          colIdx,
								rawValue:        normalized.RawValue,
								normalizedValue: normalized.NormalizedValue,
								valueType:       normalized.ValueType,
								numberValue:     normalized.NumberValue,
								dateValue:       normalized.DateValue,
								boolValue:       normalized.BoolValue,
								ftsObject:       ftsObj,
							})
						}

						// Pre-compute card title and subject FTS
						// Must match chooseTableRowSubject logic exactly
						subject := cleanFactPart(primaryKeyText)
						if subject == "" {
							// fallback: first "name"/"title"/"id" cell
							for _, pc := range cells {
								colName := normalizedColNames[pc.colIdx]
								if strings.Contains(colName, "name") || strings.Contains(colName, "姓名") || strings.Contains(colName, "title") || strings.Contains(colName, "标题") || strings.Contains(colName, "id") || strings.Contains(colName, "编号") {
									if v := cleanFactPart(pc.normalizedValue); v != "" {
										subject = v
										break
									}
								}
							}
						}
						if subject == "" && source.Title != "" && (rowIdx+1) > 0 {
							subject = fmt.Sprintf("%s row %d", source.Title, rowIdx+1)
						}
						if subject == "" {
							subject = source.ID
						}
						title := tableRowCardTitle(source, KnowledgeTableRow{RowIndex: rowIdx + 1}, subject)
						ftsTitle := segmentTextForFTS(title)
						ftsSubject := segmentTextForFTS(subject)

						prepared[i] = spreadsheetPreparedRow{
							rowIdx:         rowIdx,
							rowID:          rowID,
							rowText:        rowText,
							primaryKeyText: primaryKeyText,
							rowJSON:        rowJSON,
							ftsRowText:     ftsRowText,
							ftsPKText:      ftsPKText,
							cells:          cells,
							ftsTitle:       ftsTitle,
							ftsSubject:     ftsSubject,
						}
					}
				}(wStart, wEnd)
			}
			wg.Wait()

			// Phase 3: Sequential SQL inserts using pre-prepared statements
			// Note: Since all row/card/fact IDs are freshly generated (NewID),
			// FTS DELETE is unnecessary (no prior entries exist). Skip them.
			for _, pr := range prepared {
				if ctx.Err() != nil {
					closeAllStmts()
					return source, ctx.Err()
				}
				// Insert row
				if _, err := stmtRow.ExecContext(ctx, pr.rowID, tableID, source.ID, pr.rowIdx+1,
					pr.primaryKeyText, pr.rowText, pr.rowJSON, nowStr, nowStr); err != nil {
					closeAllStmts()
					return source, fmt.Errorf("knowledge sqlite insert kb row: %w", err)
				}
				// FTS index (skip DELETE — IDs are brand new, no prior FTS entry exists)
				_, _ = stmtRowFTSIns.ExecContext(ctx, pr.rowID, pr.ftsPKText, pr.ftsRowText)
				// Insert cells
				for _, pc := range pr.cells {
					var boolValue interface{}
					if pc.boolValue != nil {
						if *pc.boolValue {
							boolValue = 1
						} else {
							boolValue = 0
						}
					}
					var numberValue interface{}
					if pc.numberValue != nil {
						numberValue = *pc.numberValue
					}
					if _, err := stmtCell.ExecContext(ctx, pc.cellID, pr.rowID, tableID, columnIDs[pc.colIdx],
						headers[pc.colIdx], normalizedColNames[pc.colIdx], pc.rawValue, pc.normalizedValue,
						pc.valueType, numberValue, pc.dateValue, boolValue, nowStr, nowStr); err != nil {
						closeAllStmts()
						return source, fmt.Errorf("knowledge sqlite insert kb cell: %w", err)
					}
				}
				// Build KnowledgeTableCell slice for card/fact insertion
				rowCells := make([]KnowledgeTableCell, len(pr.cells))
				for i, pc := range pr.cells {
					rowCells[i] = KnowledgeTableCell{
						ID:                   pc.cellID,
						RowID:                pr.rowID,
						TableID:              tableID,
						ColumnID:             columnIDs[pc.colIdx],
						ColumnName:           headers[pc.colIdx],
						NormalizedColumnName: normalizedColNames[pc.colIdx],
						RawValue:             pc.rawValue,
						NormalizedValue:      pc.normalizedValue,
						ValueType:            pc.valueType,
						NumberValue:          pc.numberValue,
						DateValue:            pc.dateValue,
						BoolValue:            pc.boolValue,
						CreatedAt:            now,
						UpdatedAt:            now,
					}
				}
				if err := insertKBTableRowCardAndFactsBatch(ctx, cardFactStmts, source, KnowledgeTableRow{
					ID:             pr.rowID,
					TableID:        tableID,
					SourceID:       source.ID,
					RowIndex:       pr.rowIdx + 1,
					PrimaryKeyText: pr.primaryKeyText,
					RowText:        pr.rowText,
					RowJSON:        pr.rowJSON,
					CreatedAt:      now,
					UpdatedAt:      now,
				}, rowCells, &rowCardFTSData{
					ftsRowText:    pr.ftsRowText,
					ftsTitle:      pr.ftsTitle,
					ftsSubject:    pr.ftsSubject,
					ftsPredicates: buildCellFTSPredicates(pr.cells, ftsPredicates),
					ftsObjects:    buildCellFTSObjects(pr.cells),
				}, nowStr, tagsJSONStr, topicsJSONStr, true); err != nil { // skipFTSDelete=true: fresh IDs, no prior entries
					closeAllStmts()
					return source, err
				}
				rowCount++
				processedDataRows++
			}

			// Progress reporting per batch
			if onRowProgress != nil && totalDataRows > 0 {
				onRowProgress(processedDataRows, totalDataRows)
			}
		}
		closeAllStmts()
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

// findPrimaryKeyColumnIdx pre-computes the primary key column index for a sheet.
// Returns -1 if no preferred column is found (fallback to first 2 non-empty cells).
func findPrimaryKeyColumnIdx(headers []string, normalizedColNames []string) int {
	preferred := []string{"name", "姓名", "title", "标题", "id", "编号", "code", "编码", "客户", "项目", "案件"}
	for _, needle := range preferred {
		for i, norm := range normalizedColNames {
			if strings.Contains(norm, needle) {
				_ = headers[i] // bounds check elision
				return i
			}
		}
	}
	return -1
}

// buildSpreadsheetPrimaryKeyFast uses pre-computed primaryKeyColIdx to avoid
// calling normalizeSpreadsheetColumnName per row.
func buildSpreadsheetPrimaryKeyFast(headers []string, row []excelread.CellValue, pkColIdx int) string {
	if pkColIdx >= 0 {
		if text := spreadsheetCellText(cellAt(row, pkColIdx)); text != "" {
			return text
		}
	}
	// Fallback: first 2 non-empty cells
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

// buildCellFTSPredicates maps each prepared cell to its pre-computed FTS predicate
// from the sheet-level ftsPredicates array (indexed by column).
func buildCellFTSPredicates(cells []spreadsheetPreparedCell, sheetFTSPredicates []string) []string {
	preds := make([]string, len(cells))
	for i, pc := range cells {
		if pc.colIdx < len(sheetFTSPredicates) {
			preds[i] = sheetFTSPredicates[pc.colIdx]
		}
	}
	return preds
}

// buildCellFTSObjects extracts pre-computed FTS object tokens from prepared cells.
func buildCellFTSObjects(cells []spreadsheetPreparedCell) []string {
	objs := make([]string, len(cells))
	for i, pc := range cells {
		objs[i] = pc.ftsObject
	}
	return objs
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
