package knowledge

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchStructuredFiltersRowsByCells(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"部门": "法务"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].ResultType != "table_row" {
		t.Fatalf("result type = %q", results[0].ResultType)
	}
	if results[0].RowIndex != 2 {
		t.Fatalf("row index = %d, want 2", results[0].RowIndex)
	}
	if results[0].Snippet == "" || results[0].SheetName == "" {
		t.Fatalf("missing snippet/sheet: %#v", results[0])
	}
	if results[0].Citation == "" || results[0].Summary == "" {
		t.Fatalf("missing citation/summary: %#v", results[0])
	}

	min := 100000.0
	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		NumberRanges: map[string]NumberRange{"金额": {Min: &min}},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured number range: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("number range results = %#v", results)
	}

	max := 100000.0
	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		NumberRanges: map[string]NumberRange{"金额": {Max: &max}},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured number max range: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 3 {
		t.Fatalf("number max range results = %#v", results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		DateRanges: map[string]DateRange{"入职日期": {Start: "2024-01-01", End: "2024-12-31"}},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("SearchStructured date range: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("date range results = %#v", results)
	}
}

func TestSearchIncludesTableRowFTS(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	results, err := store.Search(t.Context(), SearchOptions{Query: "张三 法务", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, result := range results {
		if result.ResultType == "table_row" && result.RowIndex == 2 {
			return
		}
	}
	t.Fatalf("expected table_row result, got %#v", results)
}

func TestSearchIncludesStructuredCardAndFactFTS(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	cardResults, err := store.Search(t.Context(), SearchOptions{Query: "张三 法务", ResultTypes: []string{"card"}, Limit: 5})
	if err != nil {
		t.Fatalf("Search cards: %v", err)
	}
	if !hasResultTypeWithRow(cardResults, "card", 2) {
		t.Fatalf("expected structured card result, got %#v", cardResults)
	}

	factResults, err := store.Search(t.Context(), SearchOptions{Query: "张三 部门 法务", ResultTypes: []string{"fact"}, Limit: 5})
	if err != nil {
		t.Fatalf("Search facts: %v", err)
	}
	if !hasResultTypeWithRow(factResults, "fact", 2) {
		t.Fatalf("expected structured fact result, got %#v", factResults)
	}
}

func TestSearchSourceIDFilterPreservesCase(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.Search(t.Context(), SearchOptions{Query: "Bob Engineering", SourceID: "Src_CSV_ASCII", Limit: 5})
	if err != nil {
		t.Fatalf("Search exact source id: %v", err)
	}
	if !hasResultTypeWithRow(results, "table_row", 3) {
		t.Fatalf("expected exact source id to match mixed-case source, got %#v", results)
	}

	results, err = store.Search(t.Context(), SearchOptions{Query: "Bob Engineering", SourceID: "src_csv_ascii", Limit: 5})
	if err != nil {
		t.Fatalf("Search lower source id: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("lower-cased source id should not match mixed-case source, got %#v", results)
	}
}
func TestSearchStructuredQueryOnlyUsesFTSWithoutUnfilteredRows(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		Query: "Bob Engineering",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchStructured query only: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected query-only structured search results")
	}
	for _, result := range results {
		if result.ResultType == "table_row" && result.RowIndex == 2 {
			t.Fatalf("query-only structured search returned unfiltered row: %#v", results)
		}
	}
	if !hasResultTypeWithRow(results, "table_row", 3) {
		t.Fatalf("expected matching table row, got %#v", results)
	}
}

func TestSearchStructuredNoSpaceScriptLikeFallback(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()
	if _, err := store.db.Exec(`DELETE FROM kb_rows_fts`); err != nil {
		t.Fatalf("clear structured FTS: %v", err)
	}
	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{Query: "张三", Limit: 10})
	if err != nil {
		t.Fatalf("SearchStructured CJK LIKE fallback: %v", err)
	}
	if !hasResultTypeWithRow(results, "table_row", 2) {
		t.Fatalf("expected CJK row from LIKE fallback, got %#v", results)
	}
}

func TestStructuredRowLikeMatchScoreUsesLiteralTerms(t *testing.T) {
	score, args := structuredRowLikeMatchScore([]string{"张", "_"})
	if strings.Count(score, "CASE WHEN") != 2 {
		t.Fatalf("match score = %q", score)
	}
	if len(args) != 4 || args[0] != "%张%" || args[2] != "%\\_%" {
		t.Fatalf("match score args = %#v", args)
	}
}

func TestSearchStructuredRowFiltersUseNoSpaceScriptLikeFallback(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()
	if _, err := store.db.Exec(`DELETE FROM kb_rows_fts`); err != nil {
		t.Fatalf("clear structured FTS: %v", err)
	}
	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		Query:        "张三",
		ColumnEquals: map[string]string{"部门": "法务"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured CJK fallback with row filter: %v", err)
	}
	if len(results) != 1 || results[0].ResultType != "table_row" || results[0].RowIndex != 2 {
		t.Fatalf("expected filtered CJK row from LIKE fallback, got %#v", results)
	}
}

func TestSearchStructuredCombinesRowFiltersAndQuery(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		Query:        "Bob",
		ColumnEquals: map[string]string{"department": "Legal"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured filtered query mismatch: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("structured query should require both text and column filters, got %#v", results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		Query:        "Bob",
		ColumnEquals: map[string]string{"department": "Engineering"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured filtered query match: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 3 {
		t.Fatalf("structured query filtered results = %#v", results)
	}
}

func TestSearchStructuredSheetNamesAreCaseInsensitive(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		SheetNames: []string{"sheet1"},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("SearchStructured lower sheet name: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("lower sheet name results len = %d, want 2: %#v", len(results), results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		SheetNames: []string{"SHEET1"},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("SearchStructured upper sheet name: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("upper sheet name results len = %d, want 2: %#v", len(results), results)
	}
}

func TestSearchStructuredColumnEqualsNormalizesLookupValues(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"amount": "120,000"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured normalized number equals: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("normalized number equals results = %#v", results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"start_date": "2024/01/05"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured normalized date equals: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("normalized date equals results = %#v", results)
	}
}

func TestSearchStructuredColumnEqualsIsCaseInsensitive(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"department": "engineering"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured case-insensitive equals: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 3 {
		t.Fatalf("case-insensitive equals results = %#v", results)
	}
}

func TestSearchStructuredColumnEqualsPreservesTextCodes(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"employee_code": "00123"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured exact text code: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("exact text code results = %#v", results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"employee_code": "123"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured normalized-looking text code: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("text code should not drop leading zeroes, got %#v", results)
	}
}
func TestSearchStructuredSourceIDFilterPreservesCase(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		SourceID:     "Src_CSV_ASCII",
		ColumnEquals: map[string]string{"department": "Engineering"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured exact source id: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 3 || results[0].Source.ID != "Src_CSV_ASCII" {
		t.Fatalf("exact source id results = %#v", results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		SourceIDs:    []string{"src_csv_ascii"},
		ColumnEquals: map[string]string{"department": "Engineering"},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured lower source id: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("lower-cased source id should not match mixed-case id, got %#v", results)
	}
}
func TestSearchStructuredColumnNamesNormalizePunctuation(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	for _, columnName := range []string{"Start Date", "start-date", "start/date", "START.DATE"} {
		results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
			ColumnEquals: map[string]string{columnName: "2024-01-05"},
			Limit:        10,
		})
		if err != nil {
			t.Fatalf("SearchStructured column %q: %v", columnName, err)
		}
		if len(results) != 1 || results[0].RowIndex != 2 {
			t.Fatalf("column %q results = %#v", columnName, results)
		}
	}
}

func TestSearchStructuredColumnContainsIsCaseInsensitive(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnContains: map[string]string{"department": "eng"},
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchStructured contains lowercase: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 3 {
		t.Fatalf("case-insensitive contains results = %#v", results)
	}
}

func TestSearchStructuredColumnContainsNormalizesLookupValues(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnContains: map[string]string{"amount": "120,000"},
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchStructured contains normalized number: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("contains normalized number results = %#v", results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnContains: map[string]string{"start_date": "2024/01/05"},
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchStructured contains normalized date: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("contains normalized date results = %#v", results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnContains: map[string]string{"employee_code": "001"},
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchStructured contains code prefix: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("contains code prefix should preserve raw text semantics, got %#v", results)
	}
}

func TestSearchStructuredColumnContainsEscapesLikeWildcards(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	for _, needle := range []string{"%", "_", "%_"} {
		results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
			ColumnContains: map[string]string{"note": needle},
			Limit:          10,
		})
		if err != nil {
			t.Fatalf("SearchStructured contains wildcard %q: %v", needle, err)
		}
		if len(results) != 1 || results[0].RowIndex != 2 {
			t.Fatalf("contains wildcard %q should match literal characters only, got %#v", needle, results)
		}
	}
}

func TestSearchStructuredIgnoresEmptyColumnEquals(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"department": " "},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured empty equals: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("empty equals should not scan all rows, got %#v", results)
	}

	results, err = store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnEquals: map[string]string{"department": "Engineering", "employee_code": " "},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured mixed empty equals: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 3 {
		t.Fatalf("mixed empty equals should keep valid filters, got %#v", results)
	}
}
func TestSearchStructuredIgnoresEmptyColumnContains(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		ColumnContains: map[string]string{"department": " "},
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchStructured empty contains: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("empty contains should not scan all rows, got %#v", results)
	}
}

func TestSearchStructuredIgnoresEmptyColumnRangeFilters(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	min := 1.0
	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		NumberRanges: map[string]NumberRange{" ": {Min: &min}},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured empty column range: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("empty column range should not scan or filter rows, got %#v", results)
	}
}

func TestSearchStructuredNumberRangeCombinesBounds(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	min := 97000.0
	max := 100000.0
	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		NumberRanges: map[string]NumberRange{"amount": {Min: &min, Max: &max}},
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchStructured combined number range: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 3 {
		t.Fatalf("combined number range results = %#v", results)
	}
}
func TestSearchStructuredDateRangeNormalizesLookupValues(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	results, err := store.SearchStructured(t.Context(), StructuredSearchOptions{
		DateRanges: map[string]DateRange{"start_date": {Start: "2024/1/1", End: "2024/1/31"}},
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("SearchStructured normalized date range: %v", err)
	}
	if len(results) != 1 || results[0].RowIndex != 2 {
		t.Fatalf("normalized date range results = %#v", results)
	}
}

func hasResultTypeWithRow(results []SearchResult, typ string, rowIndex int) bool {
	for _, result := range results {
		if result.ResultType == typ && result.RowIndex == rowIndex {
			return true
		}
	}
	return false
}

func newStoreWithStructuredCSV(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "employees.csv")
	writeTestFile(t, csvPath, "姓名,部门,入职日期,金额\n张三,法务,2024-01-05,120000\n李四,销售,2023-12-01,98000\n")
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
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
		store.Close()
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, source, csvPath, SourceKindCSV); err != nil {
		_ = tx.Rollback()
		store.Close()
		t.Fatalf("importSpreadsheetSourceV2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		store.Close()
		t.Fatalf("commit: %v", err)
	}
	return store
}

func newStoreWithASCIIStructuredCSV(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "employees-ascii.csv")
	writeTestFile(t, csvPath, "name,department,start_date,amount,employee_code,note\nAlice,Legal,2024-01-05,120000,00123,100%_ready\nBob,Engineering,2023-12-01,98000,21999,plain ready\n")
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	now := time.Now().UTC()
	source := Source{
		ID:          "Src_CSV_ASCII",
		Kind:        SourceKindCSV,
		URI:         csvPath,
		Title:       "employees-ascii.csv",
		ContentHash: "hash_csv_ascii",
		Status:      StatusParsed,
		FetchedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	tx, err := store.db.Begin()
	if err != nil {
		store.Close()
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := importSpreadsheetSourceV2(t.Context(), tx, source, csvPath, SourceKindCSV); err != nil {
		_ = tx.Rollback()
		store.Close()
		t.Fatalf("importSpreadsheetSourceV2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		store.Close()
		t.Fatalf("commit: %v", err)
	}
	return store
}
