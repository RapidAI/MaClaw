package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/excel"
)

func TestExcelAdapterQuery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "name"}, {Value: "amount"}}, {{Value: "a"}, {Value: 3}}}}}}); err != nil {
		t.Fatal(err)
	}
	m := NewManager([]Profile{{ID: "x", Type: SourceExcel, FilePath: p}}, nil)
	id, _, err := m.Connect(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := m.Adapter(id)
	r, err := a.Query(context.Background(), QueryRequest{SQL: "select name, amount from sheet where amount > :min", Params: map[string]interface{}{"min": 1}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if r.RowCount != 1 || r.Rows[0][0] != "a" {
		t.Fatalf("result=%#v", r)
	}
}

func TestExcelAdapterNormalizesDuplicateHeaders(t *testing.T) {
	p := filepath.Join(t.TempDir(), "duplicate.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "name"}, {Value: "name"}}, {{Value: "a"}, {Value: "b"}}}}}}); err != nil {
		t.Fatal(err)
	}
	a, err := newExcelAdapter(Profile{ID: "x", Type: SourceExcel, FilePath: p})
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Query(context.Background(), QueryRequest{SQL: "select * from sheet", Limit: 10})
	if err != nil || len(result.Columns) != 2 || result.Columns[0].Name == result.Columns[1].Name {
		t.Fatalf("duplicate headers were not normalized: %#v err=%v", result, err)
	}
}

func TestWriteTableActionDryRunAndPathPolicy(t *testing.T) {
	result, err := writeTableAction(map[string]interface{}{
		"file_path": "reports/out.xlsx",
		"sheet":     "Sheet1",
		"rows":      []interface{}{[]interface{}{"name", "value"}, []interface{}{"x", "=1+1"}},
		"dry_run":   true,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]interface{})["dry_run"] != true {
		t.Fatalf("dry-run result=%#v", result)
	}
	if err := validateLocalDataPath("../escape.xlsx"); err == nil {
		t.Fatal("expected path traversal rejection")
	}
}

func TestReadTableHonorsProfileOperationAllowlistForExcel(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "name"}}, {{Value: "a"}}}}}}); err != nil {
		t.Fatal(err)
	}
	m := NewManager([]Profile{{ID: "x", Type: SourceExcel, FilePath: p, AllowedOperations: []string{"inspect"}}}, nil)
	id, _, err := m.Connect(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	got := HandleTool(context.Background(), m, map[string]interface{}{"action": "read_table", "connection_id": id})
	if !strings.Contains(got, "operation is not allowed by profile") {
		t.Fatalf("read_table bypassed profile operation allowlist: %q", got)
	}
}

func TestReadTableReturnsSourceHashForSafeWritePreview(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "name"}}, {{Value: "a"}}}}}}); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, nil)
	m.SetWorkspaceRoot(filepath.Dir(p))
	value, err := readTableAction(context.Background(), m, map[string]interface{}{"file_path": filepath.Base(p), "sheet": "Sheet1"})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(map[string]interface{})
	if !ok || result["source_sha256"] == "" {
		t.Fatalf("read_table preview missing source hash: %#v", value)
	}
}

func TestWriteTableRangePreservesOutsideCells(t *testing.T) {
	p := filepath.Join(t.TempDir(), "range-write.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{
		{Name: "Data", Rows: [][]excel.WriteCell{
			{{Value: "keep-a1"}, {Value: "keep-b1"}, {Value: "keep-c1"}},
			{{Value: "keep-a2"}, {Value: "old-b2"}, {Value: "old-c2"}},
			{{Value: "keep-a3"}, {Value: "old-b3"}, {Value: "old-c3"}},
		}},
		{Name: "Other", Rows: [][]excel.WriteCell{{{Value: "untouched"}}}},
	}}); err != nil {
		t.Fatal(err)
	}

	m := NewManager(nil, nil)
	m.SetWorkspaceRoot(filepath.Dir(p))
	args := map[string]interface{}{
		"file_path": filepath.Base(p),
		"sheet":     "Data",
		"range":     "B2:C3",
		"rows":      []interface{}{[]interface{}{"new-b2", "new-c2"}, []interface{}{"new-b3", "new-c3"}},
	}
	// Range writes into an existing workbook must bind the preview's source
	// hash; fetch it via a dry-run exactly like a real host would.
	preview, err := writeTableActionWithManager(context.Background(), m, map[string]interface{}{
		"file_path": args["file_path"], "sheet": args["sheet"], "range": args["range"], "rows": args["rows"], "dry_run": true,
	}, false)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	args["source_sha256"] = preview.(map[string]interface{})["source_sha256"]
	args["dry_run"] = false
	result, err := writeTableActionWithManager(WithApprovalToken(context.Background(), "test-token"), m, args, false)
	if err != nil {
		t.Fatalf("range write action failed: result=%#v err=%v", result, err)
	}
	got, err := excel.ReadFile(p, excel.ReadOptions{SheetName: "Data", Range: "A1:C3"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0][0].Value != "keep-a1" || got.Rows[0][1].Value != "keep-b1" || got.Rows[1][0].Value != "keep-a2" || got.Rows[1][1].Value != "new-b2" || got.Rows[2][2].Value != "new-c3" {
		t.Fatalf("range write did not preserve/replace expected cells: %#v", got.Rows)
	}
	other, err := excel.ReadFile(p, excel.ReadOptions{SheetName: "Other"})
	if err != nil || other.Rows[0][0].Value != "untouched" {
		t.Fatalf("range write modified unrelated sheet: %#v err=%v", other, err)
	}
}

func TestWriteFileRangeRejectsRowsOutsideRectangle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "range-bounds.xlsx")
	err := excel.WriteFileRange(p, "Data", "A1:B1", [][]excel.WriteCell{{{Value: 1}, {Value: 2}, {Value: 3}}})
	if err == nil || !strings.Contains(err.Error(), "exceed range width") {
		t.Fatalf("expected range width rejection, got %v", err)
	}
}

func TestWriteTableRequiresRangeForExistingWorkbook(t *testing.T) {
	p := filepath.Join(t.TempDir(), "existing.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Data", Rows: [][]excel.WriteCell{{{Value: "keep"}}}}}}); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, nil)
	m.SetWorkspaceRoot(filepath.Dir(p))
	_, err := writeTableActionWithManager(context.Background(), m, map[string]interface{}{
		"file_path": filepath.Base(p),
		"sheet":     "Data",
		"rows":      []interface{}{[]interface{}{"new"}},
		"dry_run":   true,
	}, false)
	if err == nil || !strings.Contains(err.Error(), "range is required") {
		t.Fatalf("expected explicit range requirement, got %v", err)
	}
}

func TestBoundTableRowsFailsClosedWithoutCursor(t *testing.T) {
	rows := [][]interface{}{{"ok"}, {strings.Repeat("x", 32)}, {"after"}}
	bounded, limited, err := boundTableRows(rows, 40)
	if err != nil || !limited || len(bounded) != 1 {
		t.Fatalf("bounded rows=%#v limited=%v err=%v", bounded, limited, err)
	}
	if _, _, err := boundTableRows([][]interface{}{{strings.Repeat("x", 64)}}, 16); err == nil || !strings.Contains(err.Error(), "result_too_large") {
		t.Fatalf("oversized first row should fail closed: %v", err)
	}
}
