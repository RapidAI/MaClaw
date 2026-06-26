package knowledge

import "testing"

func TestStructuredCatalogListsSpreadsheetTablesAndColumns(t *testing.T) {
	store := newStoreWithStructuredCSV(t)
	defer store.Close()

	result, err := store.StructuredCatalog(t.Context(), StructuredCatalogOptions{Limit: 10})
	if err != nil {
		t.Fatalf("StructuredCatalog: %v", err)
	}
	if result.Count != 1 || len(result.Tables) != 1 {
		t.Fatalf("catalog count = %#v, want one table", result)
	}
	table := result.Tables[0]
	if table.SourceID != "src_csv" || table.RowCount != 2 || table.ColumnCount != 4 {
		t.Fatalf("unexpected table metadata: %#v", table)
	}
	if len(table.Columns) != 4 {
		t.Fatalf("columns len = %d, want 4: %#v", len(table.Columns), table.Columns)
	}
	names := make(map[string]struct{}, len(table.Columns))
	for _, column := range table.Columns {
		names[column.ColumnName] = struct{}{}
	}
	for _, want := range []string{"姓名", "部门", "入职日期", "金额"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("columns = %#v, want %q", names, want)
		}
	}
}

func TestStructuredCatalogSheetNamesAreCaseInsensitive(t *testing.T) {
	store := newStoreWithASCIIStructuredCSV(t)
	defer store.Close()

	result, err := store.StructuredCatalog(t.Context(), StructuredCatalogOptions{SheetNames: []string{"sheet1"}, Limit: 10})
	if err != nil {
		t.Fatalf("StructuredCatalog lower sheet name: %v", err)
	}
	if result.Count != 1 || len(result.Tables) != 1 || result.Tables[0].SheetName != "Sheet1" {
		t.Fatalf("lower sheet catalog = %#v, want Sheet1", result)
	}

	result, err = store.StructuredCatalog(t.Context(), StructuredCatalogOptions{SheetNames: []string{"SHEET1"}, Limit: 10})
	if err != nil {
		t.Fatalf("StructuredCatalog upper sheet name: %v", err)
	}
	if result.Count != 1 || len(result.Tables) != 1 || result.Tables[0].SheetName != "Sheet1" {
		t.Fatalf("upper sheet catalog = %#v, want Sheet1", result)
	}
}
