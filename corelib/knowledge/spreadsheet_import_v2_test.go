package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestImportSpreadsheetSourceV2CreatesRowsAndCells(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "employees.csv")
	writeTestFile(t, csvPath, "姓名,部门,入职日期,金额\n张三,法务,2024-01-05,120000\n李四,销售,2023-12-01,98000\n")

	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	source := Source{
		ID:          "src_csv",
		Kind:        SourceKindCSV,
		URI:         csvPath,
		Title:       "employees.csv",
		ContentHash: "hash_csv",
		Status:      StatusParsed,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, source, csvPath, SourceKindCSV); err != nil {
		_ = tx.Rollback()
		t.Fatalf("importSpreadsheetSourceV2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	assertCount(t, store.db, "kb_tables", 1)
	assertCount(t, store.db, "kb_columns", 4)
	assertCount(t, store.db, "kb_rows", 2)
	assertCount(t, store.db, "kb_cells", 8)
	assertCount(t, store.db, "kb_cards", 2)
	assertCount(t, store.db, "kb_facts", 8)

	var rowText string
	if err := store.db.QueryRow(`SELECT row_text FROM kb_rows WHERE row_index = 2`).Scan(&rowText); err != nil {
		t.Fatalf("row text: %v", err)
	}
	if rowText != "姓名: 张三 | 部门: 法务 | 入职日期: 2024-01-05 | 金额: 120000" {
		t.Fatalf("row_text = %q", rowText)
	}

	var numberValue float64
	if err := store.db.QueryRow(`SELECT number_value FROM kb_cells WHERE column_name = '金额' AND raw_value = '120000'`).Scan(&numberValue); err != nil {
		t.Fatalf("number value: %v", err)
	}
	if numberValue != 120000 {
		t.Fatalf("number_value = %v", numberValue)
	}

	var factObject string
	if err := store.db.QueryRow(`SELECT object FROM kb_facts WHERE subject = '张三' AND predicate = '部门'`).Scan(&factObject); err != nil {
		t.Fatalf("row fact: %v", err)
	}
	if factObject != "法务" {
		t.Fatalf("fact object = %q", factObject)
	}
}

func TestImportSpreadsheetSourceV2CreatesStructuredRowsForLegacyXLS(t *testing.T) {
	dir := t.TempDir()
	path := copyLegacyXLSTestFixture(t, dir, "small_1_sheet.xls")

	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "src_legacy_xls", Kind: SourceKindXLS, URI: path, Title: "legacy.xls", Status: StatusParsed}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, source, path, SourceKindXLS); err != nil {
		_ = tx.Rollback()
		t.Fatalf("import legacy XLS structured rows: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertCount(t, store.db, "kb_tables", 1)
	var rowCount, cellCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM kb_rows WHERE source_id = ?`, source.ID).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM kb_cells`).Scan(&cellCount); err != nil {
		t.Fatal(err)
	}
	if rowCount == 0 || cellCount == 0 {
		t.Fatalf("legacy XLS was not indexed as structured rows/cells: rows=%d cells=%d", rowCount, cellCount)
	}
}

func TestKnowledgeImportFilesIndexesLegacyXLSAsStructuredRows(t *testing.T) {
	dir := t.TempDir()
	path := copyLegacyXLSTestFixture(t, dir, "small_1_sheet.xls")
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	result, err := store.ImportFiles(t.Context(), DirectoryImportRequest{
		RootPath: dir, IncludeExts: []string{".xls"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil || result.ImportedFiles != 1 || result.FailedFiles != 0 || len(result.Items) != 1 || result.Items[0].SourceID == "" {
		t.Fatalf("ImportFiles legacy XLS = %#v, %v", result, err)
	}
	assertCount(t, store.db, "kb_tables", 1)
	var rowCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM kb_rows WHERE source_id = ?`, result.Items[0].SourceID).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount == 0 {
		t.Fatalf("ImportFiles did not create structured legacy XLS rows for source %q", result.Items[0].SourceID)
	}
}

func TestImportSpreadsheetSourceV2RejectsOversizedCSVBeforeSourceWrite(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "oversized.csv")
	file, err := os.Create(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(agent.MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "src_oversized_csv", Kind: SourceKindCSV, URI: csvPath, Title: "Oversized"}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, source, csvPath, SourceKindCSV); !errors.Is(err, agent.ErrOfficeReadInputTooLarge) {
		_ = tx.Rollback()
		t.Fatalf("oversized direct spreadsheet import err = %v, want shared input limit", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertCount(t, store.db, "knowledge_sources", 0)
}

func TestImportSpreadsheetSourceV2RejectsDocumentContainerDisguisedAsCSV(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "disguised.csv")
	writeOfficeReadDOCX(t, csvPath, "must not reach table import", nil)
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "src_disguised_csv", Kind: SourceKindCSV, URI: csvPath, Title: "Disguised"}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, source, csvPath, SourceKindCSV); !errors.Is(err, agent.ErrOfficeReadFormatMismatch) {
		_ = tx.Rollback()
		t.Fatalf("disguised CSV import err = %v, want format mismatch", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertCount(t, store.db, "knowledge_sources", 0)
}

func TestImportSpreadsheetSourceV2ParsesPrivateSnapshotAcrossReplacement(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "employees.csv")
	writeTestFile(t, csvPath, "id,value\n1,validated\n")
	previousSnapshot := spreadsheetImportSnapshot
	spreadsheetImportSnapshot = func(path, kind string) (string, func(), error) {
		snapshot, cleanup, err := snapshotSpreadsheetImportInput(path, kind)
		if err != nil {
			return "", nil, err
		}
		writeTestFile(t, csvPath, "id,value\n1,replacement\n")
		return snapshot, cleanup, nil
	}
	defer func() { spreadsheetImportSnapshot = previousSnapshot }()

	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "src_snapshot_csv", Kind: SourceKindCSV, URI: csvPath, Title: "employees.csv"}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, source, csvPath, SourceKindCSV); err != nil {
		_ = tx.Rollback()
		t.Fatalf("import snapshot: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var rowText string
	if err := store.db.QueryRow(`SELECT row_text FROM kb_rows WHERE source_id = ?`, source.ID).Scan(&rowText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rowText, "validated") || strings.Contains(rowText, "replacement") {
		t.Fatalf("structured import parsed replacement instead of snapshot: %q", rowText)
	}
}

func TestNormalizeSpreadsheetColumnNameTreatsPunctuationAsSeparators(t *testing.T) {
	cases := map[string]string{
		" Start Date ": "start_date",
		"start-date":   "start_date",
		"start/date":   "start_date",
		"START.DATE":   "start_date",
		"姓名":           "姓名",
		"客户 ID":        "客户_id",
	}
	for input, want := range cases {
		if got := normalizeSpreadsheetColumnName(input); got != want {
			t.Fatalf("normalizeSpreadsheetColumnName(%q) = %q, want %q", input, got, want)
		}
	}
}

func copyLegacyXLSTestFixture(t *testing.T, destinationDir, name string) string {
	t.Helper()
	moduleCache := os.Getenv("GOMODCACHE")
	if moduleCache == "" {
		moduleCache = filepath.Join(os.Getenv("USERPROFILE"), "go", "pkg", "mod")
	}
	fixture := filepath.Join(moduleCache, "github.com", "!vantagics", "!legacy!office!reader@v0.0.0-20260621074012-a324c1dbb18b", "testfie", name)
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Skipf("legacy XLS module fixture unavailable: %v", err)
	}
	destination := filepath.Join(destinationDir, name)
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return destination
}
