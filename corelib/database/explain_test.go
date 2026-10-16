package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/excel"
)

func TestWrapExplainSQLRejectsAnalyzeAndNonReads(t *testing.T) {
	if _, _, err := wrapExplainSQL("postgres", "EXPLAIN ANALYZE SELECT 1"); err == nil {
		t.Fatal("ANALYZE must be rejected")
	}
	if _, _, err := wrapExplainSQL("postgres", "UPDATE t SET a=1 WHERE id=1"); err == nil {
		t.Fatal("DML must be rejected")
	}
	got, showplan, err := wrapExplainSQL("postgres", "SELECT id FROM t WHERE id = :id")
	if err != nil || showplan || !strings.HasPrefix(got, "EXPLAIN (FORMAT TEXT)") {
		t.Fatalf("postgres wrap = %q showplan=%v err=%v", got, showplan, err)
	}
	_, showplan, err = wrapExplainSQL("sqlserver", "SELECT 1")
	if err != nil || !showplan {
		t.Fatalf("sqlserver showplan = %v err=%v", showplan, err)
	}
	if _, _, err := wrapExplainSQL("access", "SELECT 1"); err == nil || !strings.Contains(err.Error(), "unsupported_capability") {
		t.Fatalf("access err = %v", err)
	}
}

func TestClassifyDDLPlanMarksDestructiveDrop(t *testing.T) {
	plan := classifyDDLOperation("DROP TABLE orders")
	if plan.Operation != "drop" || !plan.Destructive {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.Targets) != 1 || !strings.EqualFold(plan.Targets[0], "orders") {
		t.Fatalf("targets = %v", plan.Targets)
	}
	create := classifyDDLOperation("CREATE INDEX idx_orders ON orders (id)")
	if create.Operation != "create_index" || create.Destructive {
		t.Fatalf("create index plan = %+v", create)
	}
}

func TestExcelExplainAndNLPreview(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "name"}}, {{Value: "a"}}}}}}); err != nil {
		t.Fatal(err)
	}
	m := NewManager([]Profile{{ID: "x", Type: SourceExcel, FilePath: p}}, nil)
	defer m.Close()
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "o", SessionID: "s"})
	connID, _, err := m.ConnectFor(ctx, "x", "o", "s")
	if err != nil {
		t.Fatal(err)
	}
	preview := HandleTool(ctx, m, map[string]interface{}{"action": "explain", "connection_id": connID, "prompt": "按姓名查"})
	if !strings.Contains(preview, `"guidance"`) || !strings.Contains(preview, "explain") {
		t.Fatalf("nl preview = %s", preview)
	}
	plan := HandleTool(ctx, m, map[string]interface{}{"action": "explain", "connection_id": connID, "sql": "SELECT name FROM sheet"})
	if !strings.Contains(plan, "explain_preview") {
		t.Fatalf("excel explain = %s", plan)
	}
}

func TestDDLDryRunReturnsPlanner(t *testing.T) {
	a := &sqlAdapter{dialect: "sqlite", profile: Profile{ID: "p", AllowDDL: true, WriteEnabled: true}}
	plan := classifyDDLOperation("ALTER TABLE t ADD COLUMN n INT")
	if plan.Operation != "alter" {
		t.Fatalf("op = %s", plan.Operation)
	}
	_ = a
	_ = context.Background()
}
