package knowledge

import (
	"path/filepath"
	"testing"
	"time"
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
