package excel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoverSpreadsheetReadsFailClosed(t *testing.T) {
	t.Run("single sheet", func(t *testing.T) {
		result := &ReadResult{SheetName: "partial"}
		var err error
		func() {
			defer recoverSpreadsheetRead(&result, &err)
			panic("malformed workbook")
		}()
		if result != nil || err == nil || !strings.Contains(err.Error(), "spreadsheet parser panicked") {
			t.Fatalf("recovered single-sheet result=%#v err=%v", result, err)
		}
	})
	t.Run("all sheets", func(t *testing.T) {
		results := []*ReadResult{{SheetName: "partial"}}
		var err error
		func() {
			defer recoverAllSpreadsheetReads(&results, &err)
			panic("malformed workbook")
		}()
		if results != nil || err == nil || !strings.Contains(err.Error(), "spreadsheet parser panicked") {
			t.Fatalf("recovered all-sheet results=%#v err=%v", results, err)
		}
	})
	t.Run("sheet names", func(t *testing.T) {
		names := []string{"partial"}
		var err error
		func() {
			defer recoverSpreadsheetSheetNames(&names, &err)
			panic("malformed workbook")
		}()
		if names != nil || err == nil || !strings.Contains(err.Error(), "spreadsheet parser panicked") {
			t.Fatalf("recovered sheet names=%#v err=%v", names, err)
		}
	})
}

func TestReadAllSheetsReadsWorkbookThroughOnePublicResultSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multi-sheet.xlsx")
	if err := WriteFile(path, WriteData{Sheets: []WriteSheet{
		{Name: "Summary", Rows: [][]WriteCell{{{Value: "name"}, {Value: "value"}}, {{Value: "alpha"}, {Value: 1}}}},
		{Name: "Details", Rows: [][]WriteCell{{{Value: "item"}}, {{Value: "beta"}}}},
	}}); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	results, err := ReadAllSheets(path)
	if err != nil {
		t.Fatalf("ReadAllSheets: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("sheet results = %d, want 2", len(results))
	}
	if results[0].SheetName != "Summary" || results[1].SheetName != "Details" {
		t.Fatalf("sheet order/names = %#v", results)
	}
	if got := results[0].Rows[1][0].Value; got != "alpha" {
		t.Fatalf("Summary cell = %#v, want alpha", got)
	}
	if got := results[1].Rows[1][0].Value; got != "beta" {
		t.Fatalf("Details cell = %#v, want beta", got)
	}
}

func TestReadAllSheetsKeepsCSVSingleSheetSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.csv")
	mustWriteFile(t, path, []byte("name,value\nalpha,1\n"))

	results, err := ReadAllSheets(path)
	if err != nil {
		t.Fatalf("ReadAllSheets: %v", err)
	}
	if len(results) != 1 || results[0].SheetName != "Sheet1" || len(results[0].Rows) != 2 {
		t.Fatalf("CSV all-sheet result = %#v", results)
	}
}

func TestReadLegacyXLSUsesPublicStructuredResultModel(t *testing.T) {
	path := legacyXLSTestFixture(t, "small_1_sheet.xls")
	results, err := ReadAllSheets(path)
	if err != nil {
		t.Fatalf("ReadAllSheets legacy XLS: %v", err)
	}
	if len(results) != 1 || results[0] == nil || results[0].SheetName == "" || results[0].RowCount == 0 || results[0].ColCount == 0 {
		t.Fatalf("legacy XLS all-sheet result = %#v", results)
	}
	result, err := ReadFile(path, ReadOptions{SheetName: results[0].SheetName, Range: "A1:A1"})
	if err != nil {
		t.Fatalf("ReadFile legacy XLS range: %v", err)
	}
	if result.RowCount != 1 || result.ColCount != 1 || result.Rows[0][0].Type == CellTypeEmpty {
		t.Fatalf("legacy XLS ranged result = %#v", result)
	}
	exactRange, err := ReadFile(path, ReadOptions{SheetName: results[0].SheetName, Range: "A1:A1", MaxRows: 1})
	if err != nil || exactRange.Truncated {
		t.Fatalf("legacy XLS exact bounded range = %#v, %v", exactRange, err)
	}
	names, err := ListSheets(path)
	if err != nil || len(names) != 1 || names[0] != results[0].SheetName {
		t.Fatalf("legacy XLS sheet names = %#v, %v", names, err)
	}
}

func TestReadCSVMaxRowsStreamsOnlyRequestedPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.csv")
	mustWriteFile(t, path, []byte("id,value\n1,alpha\n2,beta\n3,gamma\n"))

	result, err := ReadFile(path, ReadOptions{MaxRows: 2})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if result.RowCount != 2 || !result.Truncated {
		t.Fatalf("max-row CSV result = %#v", result)
	}
	if got := result.Rows[1][0].Value; got != "1" {
		t.Fatalf("last retained row = %#v, want first data row", got)
	}
}

func TestReadCSVMaxRowsPreservesRangeStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "range.csv")
	mustWriteFile(t, path, []byte("id,value\n1,alpha\n2,beta\n3,gamma\n"))

	result, err := ReadFile(path, ReadOptions{Range: "A3:B4", MaxRows: 1})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if result.RowCount != 1 || !result.Truncated || result.Rows[0][0].Value != "2" {
		t.Fatalf("ranged max-row CSV result = %#v", result)
	}
}

func TestReadCSVExactRangeDoesNotReportMaxRowTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exact-range.csv")
	mustWriteFile(t, path, []byte("id,value\n1,alpha\n2,beta\n3,gamma\n"))

	result, err := ReadFile(path, ReadOptions{Range: "A3:B3", MaxRows: 1})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if result.RowCount != 1 || result.Truncated || result.Rows[0][0].Value != "2" {
		t.Fatalf("exact ranged max-row CSV result = %#v", result)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func legacyXLSTestFixture(t *testing.T, name string) string {
	t.Helper()
	moduleCache := os.Getenv("GOMODCACHE")
	if moduleCache == "" {
		moduleCache = filepath.Join(os.Getenv("USERPROFILE"), "go", "pkg", "mod")
	}
	fixture := filepath.Join(moduleCache, "github.com", "!vantagics", "!legacy!office!reader@v0.0.0-20260621074012-a324c1dbb18b", "testfie", name)
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("legacy XLS module fixture unavailable: %v", err)
	}
	return fixture
}
