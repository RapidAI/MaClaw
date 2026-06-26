package knowledge

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestNewSQLiteStoreCreatesSchemaV2(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	for _, table := range []string{
		"kb_meta",
		"kb_sources",
		"kb_tables",
		"kb_columns",
		"kb_rows",
		"kb_cells",
		"kb_cards",
		"kb_facts",
		"kb_rows_fts",
		"kb_cards_fts",
		"kb_facts_fts",
	} {
		if !testTableExists(t, store.db, table) {
			t.Fatalf("expected table %s to exist", table)
		}
	}

	for _, index := range []string{
		"idx_kb_tables_source_sheet_lower",
		"idx_kb_cells_row_col",
		"idx_kb_cells_col_value_row",
		"idx_kb_cells_col_value_lower_row",
		"idx_kb_cells_col_raw_lower_row",
		"idx_kb_cells_col_number_row",
		"idx_kb_cells_col_date_row",
		"idx_kb_cells_col_bool_row",
	} {
		if !testIndexExists(t, store.db, index) {
			t.Fatalf("expected index %s to exist", index)
		}
	}

	var version string
	if err := store.db.QueryRow(`SELECT value FROM kb_meta WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != "2" {
		t.Fatalf("schema version = %q, want 2", version)
	}

	var migratedFrom string
	err = store.db.QueryRow(`SELECT value FROM kb_meta WHERE key = 'migrated_from'`).Scan(&migratedFrom)
	if err != sql.ErrNoRows {
		t.Fatalf("fresh db migrated_from err = %v, value = %q; want no row", err, migratedFrom)
	}
}

func TestNewSQLiteStoreMigratesLegacyV1Metadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := applyPragmas(legacyDB); err != nil {
		t.Fatalf("apply pragmas: %v", err)
	}
	if err := createTables(legacyDB); err != nil {
		t.Fatalf("create legacy tables: %v", err)
	}
	now := time.Now().UTC()
	_, err = legacyDB.Exec(insertSourceSQL,
		"src_1", SourceKindText, "memory://legacy", "", "Legacy Source", "", "",
		formatTime(now), formatTime(now), "hash_1", "owner_1", "tenant_1",
		"", "", 0.8, "", "", StatusDistilled, "", formatTime(now), formatTime(now),
	)
	if err != nil {
		t.Fatalf("insert legacy source: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	var title string
	if err := store.db.QueryRow(`SELECT title FROM kb_sources WHERE id = 'src_1'`).Scan(&title); err != nil {
		t.Fatalf("migrated source: %v", err)
	}
	if title != "Legacy Source" {
		t.Fatalf("migrated source title = %q", title)
	}

	var migratedFrom string
	if err := store.db.QueryRow(`SELECT value FROM kb_meta WHERE key = 'migrated_from'`).Scan(&migratedFrom); err != nil {
		t.Fatalf("migrated_from: %v", err)
	}
	if migratedFrom != "v1" {
		t.Fatalf("migrated_from = %q, want v1", migratedFrom)
	}
}

func TestNewSQLiteStoreBackfillsSchemaV2Indexes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore initial: %v", err)
	}
	for _, index := range []string{
		"idx_kb_tables_source_sheet_lower",
		"idx_kb_cells_col_value_row",
		"idx_kb_cells_col_value_lower_row",
		"idx_kb_cells_col_raw_lower_row",
	} {
		if _, err := store.db.Exec(`DROP INDEX IF EXISTS ` + index); err != nil {
			t.Fatalf("drop test index %s: %v", index, err)
		}
		if testIndexExists(t, store.db, index) {
			t.Fatalf("expected dropped index %s to be absent", index)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore reopen: %v", err)
	}
	defer store.Close()
	for _, index := range []string{
		"idx_kb_tables_source_sheet_lower",
		"idx_kb_cells_col_value_row",
		"idx_kb_cells_col_value_lower_row",
		"idx_kb_cells_col_raw_lower_row",
	} {
		if !testIndexExists(t, store.db, index) {
			t.Fatalf("expected schema ensure to recreate structured search index %s", index)
		}
	}
}

func TestNewSQLiteStoreRepairsStructuredColumnNormalization(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knowledge.db")
	csvPath := filepath.Join(dir, "employees.csv")
	writeTestFile(t, csvPath, "name,start-date\nAlice,2024-01-05\n")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore initial: %v", err)
	}
	now := time.Now().UTC()
	tx, err := store.db.Begin()
	if err != nil {
		store.Close()
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, Source{
		ID:          "src_repair_csv",
		Kind:        SourceKindCSV,
		URI:         csvPath,
		Title:       "employees.csv",
		ContentHash: "hash_repair_csv",
		Status:      StatusParsed,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, csvPath, SourceKindCSV); err != nil {
		_ = tx.Rollback()
		store.Close()
		t.Fatalf("importSpreadsheetSourceV2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		store.Close()
		t.Fatalf("commit: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE kb_columns SET normalized_name = 'start-date' WHERE column_name = 'start-date'`); err != nil {
		store.Close()
		t.Fatalf("simulate old column normalization: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE kb_cells SET normalized_column_name = 'start-date' WHERE column_name = 'start-date'`); err != nil {
		store.Close()
		t.Fatalf("simulate old cell normalization: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO kb_meta(key, value) VALUES ('structured_column_normalization_version', '1')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		store.Close()
		t.Fatalf("simulate old normalization version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore reopen: %v", err)
	}
	defer store.Close()

	var normalizedColumn, normalizedCell string
	if err := store.db.QueryRow(`SELECT normalized_name FROM kb_columns WHERE column_name = 'start-date'`).Scan(&normalizedColumn); err != nil {
		t.Fatalf("repaired column normalization: %v", err)
	}
	if err := store.db.QueryRow(`SELECT normalized_column_name FROM kb_cells WHERE column_name = 'start-date'`).Scan(&normalizedCell); err != nil {
		t.Fatalf("repaired cell normalization: %v", err)
	}
	if normalizedColumn != "start_date" || normalizedCell != "start_date" {
		t.Fatalf("normalization not repaired, column=%q cell=%q", normalizedColumn, normalizedCell)
	}
	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"start/date": "2024-01-05"},
		Limit:        5,
	})
	if err != nil {
		t.Fatalf("SearchStructured repaired column: %v", err)
	}
	if len(results) != 1 || results[0].Source.ID != "src_repair_csv" {
		t.Fatalf("unexpected repaired column search results: %#v", results)
	}
}

func TestNewSQLiteStoreRepairsStructuredLeadingZeroTextCodes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knowledge.db")
	csvPath := filepath.Join(dir, "employees-ascii.csv")
	writeTestFile(t, csvPath, "name,employee_code\nAlice,00123\nBob,21999\n")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore initial: %v", err)
	}
	now := time.Now().UTC()
	tx, err := store.db.Begin()
	if err != nil {
		store.Close()
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, Source{
		ID:          "src_repair_codes",
		Kind:        SourceKindCSV,
		URI:         csvPath,
		Title:       "employees-ascii.csv",
		ContentHash: "hash_repair_codes",
		Status:      StatusParsed,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, csvPath, SourceKindCSV); err != nil {
		_ = tx.Rollback()
		store.Close()
		t.Fatalf("importSpreadsheetSourceV2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		store.Close()
		t.Fatalf("commit: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE kb_cells SET value_type = ?, normalized_value = '123', number_value = 123 WHERE column_name = 'employee_code' AND raw_value = '00123'`, tableValueTypeNumber); err != nil {
		store.Close()
		t.Fatalf("simulate old leading-zero cell normalization: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE kb_facts SET object = '123', normalized_object = '123', value_type = ?, number_value = 123 WHERE predicate = 'employee_code' AND object = '00123'`, tableValueTypeNumber); err != nil {
		store.Close()
		t.Fatalf("simulate old leading-zero fact normalization: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO kb_meta(key, value) VALUES ('structured_cell_value_repair_version', '0')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		store.Close()
		t.Fatalf("simulate old cell value repair version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore reopen: %v", err)
	}
	defer store.Close()

	var normalized, valueType string
	var numberValue sql.NullFloat64
	if err := store.db.QueryRow(`SELECT normalized_value, value_type, number_value FROM kb_cells WHERE column_name = 'employee_code' AND raw_value = '00123'`).Scan(&normalized, &valueType, &numberValue); err != nil {
		t.Fatalf("repaired leading-zero cell: %v", err)
	}
	if normalized != "00123" || valueType != tableValueTypeString || numberValue.Valid {
		t.Fatalf("leading-zero cell not repaired, normalized=%q type=%q numberValid=%v", normalized, valueType, numberValue.Valid)
	}
	var columnType string
	if err := store.db.QueryRow(`SELECT value_type FROM kb_columns WHERE column_name = 'employee_code'`).Scan(&columnType); err != nil {
		t.Fatalf("repaired leading-zero column type: %v", err)
	}
	if columnType != tableValueTypeMixed {
		t.Fatalf("leading-zero column type = %q, want %q", columnType, tableValueTypeMixed)
	}

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"employee_code": "00123"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured repaired code: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("repaired code results = %#v", results)
	}
	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"employee_code": "123"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured old normalized code: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("old normalized code should not match repaired text code, got %#v", results)
	}
}
func TestNewSQLiteStoreUpgradesStructuredLeadingZeroRepairV1ColumnType(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knowledge.db")
	csvPath := filepath.Join(dir, "employees-ascii.csv")
	writeTestFile(t, csvPath, "name,employee_code\nAlice,00123\nBob,21999\n")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore initial: %v", err)
	}
	now := time.Now().UTC()
	tx, err := store.db.Begin()
	if err != nil {
		store.Close()
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, Source{
		ID:          "src_repair_codes_v1",
		Kind:        SourceKindCSV,
		URI:         csvPath,
		Title:       "employees-ascii.csv",
		ContentHash: "hash_repair_codes_v1",
		Status:      StatusParsed,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, csvPath, SourceKindCSV); err != nil {
		_ = tx.Rollback()
		store.Close()
		t.Fatalf("importSpreadsheetSourceV2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		store.Close()
		t.Fatalf("commit: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE kb_cells SET value_type = ?, normalized_value = raw_value, number_value = NULL WHERE column_name = 'employee_code' AND raw_value = '00123'`, tableValueTypeString); err != nil {
		store.Close()
		t.Fatalf("simulate v1 repaired leading-zero cell: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE kb_facts SET object = '00123', normalized_object = '00123', value_type = ?, number_value = NULL WHERE predicate = 'employee_code' AND object = '00123'`, tableValueTypeString); err != nil {
		store.Close()
		t.Fatalf("simulate v1 repaired leading-zero fact: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE kb_columns SET value_type = ? WHERE column_name = 'employee_code'`, tableValueTypeNumber); err != nil {
		store.Close()
		t.Fatalf("simulate v1 missing column type repair: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO kb_meta(key, value) VALUES ('structured_cell_value_repair_version', '1')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		store.Close()
		t.Fatalf("simulate v1 cell value repair version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore reopen: %v", err)
	}
	defer store.Close()

	var columnType string
	if err := store.db.QueryRow(`SELECT value_type FROM kb_columns WHERE column_name = 'employee_code'`).Scan(&columnType); err != nil {
		t.Fatalf("upgraded leading-zero column type: %v", err)
	}
	if columnType != tableValueTypeMixed {
		t.Fatalf("leading-zero column type after v1 upgrade = %q, want %q", columnType, tableValueTypeMixed)
	}
	var repairVersion string
	if err := store.db.QueryRow(`SELECT value FROM kb_meta WHERE key = 'structured_cell_value_repair_version'`).Scan(&repairVersion); err != nil {
		t.Fatalf("structured cell value repair version: %v", err)
	}
	if repairVersion != "2" {
		t.Fatalf("structured cell value repair version = %q, want 2", repairVersion)
	}

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"employee_code": "00123"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured upgraded code: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("upgraded code results = %#v", results)
	}
	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"employee_code": "123"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured old normalized upgraded code: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("old normalized code should not match v1-upgraded text code, got %#v", results)
	}
}
func TestNewSQLiteStoreMigratesLegacySpreadsheetSource(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "legacy-employees.csv")
	writeTestFile(t, csvPath, "姓名,部门\n张三,法务\n")
	dbPath := filepath.Join(dir, "knowledge.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := applyPragmas(legacyDB); err != nil {
		t.Fatalf("apply pragmas: %v", err)
	}
	if err := createTables(legacyDB); err != nil {
		t.Fatalf("create legacy tables: %v", err)
	}
	now := time.Now().UTC()
	_, err = legacyDB.Exec(insertSourceSQL,
		"src_legacy_csv", SourceKindCSV, csvPath, "", "Legacy CSV", "", "",
		formatTime(now), formatTime(now), "hash_legacy_csv", "owner_1", "tenant_1",
		"", "", 0.9, "", "", StatusParsed, "", formatTime(now), formatTime(now),
	)
	if err != nil {
		t.Fatalf("insert legacy csv source: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	assertCount(t, store.db, "kb_tables", 1)
	assertCount(t, store.db, "kb_columns", 2)
	assertCount(t, store.db, "kb_rows", 1)
	assertCount(t, store.db, "kb_cells", 2)

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"部门": "法务"},
		Limit:        5,
	})
	if err != nil {
		t.Fatalf("SearchStructured migrated csv: %v", err)
	}
	if len(results) != 1 || results[0].Source.ID != "src_legacy_csv" {
		t.Fatalf("unexpected migrated csv search results: %#v", results)
	}
}

func TestNewSQLiteStoreMigratesLegacySpreadsheetNodesDegraded(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "knowledge.db")
	missingCSVPath := filepath.Join(dir, "missing.csv")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := applyPragmas(legacyDB); err != nil {
		t.Fatalf("apply pragmas: %v", err)
	}
	if err := createTables(legacyDB); err != nil {
		t.Fatalf("create legacy tables: %v", err)
	}
	now := time.Now().UTC()
	_, err = legacyDB.Exec(insertSourceSQL,
		"src_missing_csv", SourceKindCSV, missingCSVPath, "", "Missing CSV", "", "",
		formatTime(now), formatTime(now), "hash_missing_csv", "owner_1", "tenant_1",
		"", "", 0.9, "", "", StatusParsed, "", formatTime(now), formatTime(now),
	)
	if err != nil {
		t.Fatalf("insert legacy missing csv source: %v", err)
	}
	tx, err := legacyDB.Begin()
	if err != nil {
		t.Fatalf("begin legacy tx: %v", err)
	}
	if err := insertDocumentNode(t.Context(), tx, DocumentNode{
		ID:        "node_missing_csv_1",
		SourceID:  "src_missing_csv",
		Type:      "sheet",
		Title:     "Sheet1 rows 2-2",
		Text:      "姓名: 张三 | 部门: 法务",
		SheetName: "Sheet1",
		RowRange:  "2:2",
		Offset:    2,
		Metadata:  map[string]string{"migration_test": "true"},
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert legacy node: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit legacy node: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	assertCount(t, store.db, "kb_tables", 1)
	assertCount(t, store.db, "kb_rows", 1)
	assertCount(t, store.db, "kb_cells", 2)
	assertCount(t, store.db, "kb_cards", 1)
	assertCount(t, store.db, "kb_facts", 2)

	var schemaJSON string
	if err := store.db.QueryRow(`SELECT schema_json FROM kb_tables WHERE source_id = 'src_missing_csv'`).Scan(&schemaJSON); err != nil {
		t.Fatalf("degraded schema: %v", err)
	}
	if !strings.Contains(schemaJSON, `"migration_degraded":true`) {
		t.Fatalf("schema_json = %q, want migration_degraded", schemaJSON)
	}

	structuredResults, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"部门": "法务"},
		Limit:        5,
	})
	if err != nil {
		t.Fatalf("SearchStructured degraded row: %v", err)
	}
	if len(structuredResults) != 1 || structuredResults[0].ResultType != "table_row" || structuredResults[0].Source.ID != "src_missing_csv" {
		t.Fatalf("unexpected degraded structured search results: %#v", structuredResults)
	}

	results, err := store.Search(t.Context(), SearchOptions{Query: "张三 法务", ResultTypes: []string{"table_row"}, Limit: 5})
	if err != nil {
		t.Fatalf("Search degraded row: %v", err)
	}
	foundTableRow := false
	for _, result := range results {
		if result.ResultType == "table_row" && result.Source.ID == "src_missing_csv" {
			foundTableRow = true
			break
		}
	}
	if !foundTableRow {
		t.Fatalf("unexpected degraded row search results: %#v", results)
	}
}

func testTableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table', 'view') AND name = ?`, name).Scan(&count); err != nil {
		t.Fatalf("inspect table %s: %v", name, err)
	}
	return count > 0
}

func testIndexExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&count); err != nil {
		t.Fatalf("inspect index %s: %v", name, err)
	}
	return count > 0
}

func writeTestFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("count %s = %d, want %d", table, got, want)
	}
}
