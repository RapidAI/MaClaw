package database

// Regression tests for the review-fix batch. Each test names the review item
// it covers so a future revert of the fix shows up as a focused failure.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/excel"
)

// Item 1 (critical): the postgres driver name must be the one pgx/v5/stdlib
// actually registers. sql.Open does not dial, so openProfile must succeed for
// every network profile type; a regression shows up as "unknown driver".
func TestOpenProfileDriverNames(t *testing.T) {
	profiles := []Profile{
		{ID: "pg", Type: SourcePostgres, Host: "127.0.0.1", Database: "db", Username: "u"},
		{ID: "my", Type: SourceMySQL, Host: "127.0.0.1", Database: "db", Username: "u"},
		{ID: "ms", Type: SourceSQLServer, Host: "127.0.0.1", Database: "db", Username: "u"},
	}
	for _, p := range profiles {
		adapter, err := openProfile(context.Background(), p, nil)
		if err != nil {
			t.Fatalf("openProfile(%s) failed: %v", p.Type, err)
		}
		if adapter == nil {
			t.Fatalf("openProfile(%s) returned a nil adapter", p.Type)
		}
		sqlAd, ok := adapter.(*sqlAdapter)
		if !ok {
			t.Fatalf("openProfile(%s) did not return a sqlAdapter", p.Type)
		}
		// The dialect string drives resetSession/Inspect and must not change
		// with the driver registration name.
		if string(p.Type) != sqlAd.dialect {
			t.Fatalf("openProfile(%s) dialect = %q, want %q", p.Type, sqlAd.dialect, p.Type)
		}
		_ = adapter.Close()
	}
}

// Item 2 (major): a first row that alone exceeds maxResultBytes must fail
// closed with result_too_large instead of being silently accepted.
func TestCollectRowsFirstRowByteLimit(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t (v TEXT)"); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", maxResultBytes)
	if _, err := db.Exec("INSERT INTO t VALUES (?)", big); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("SELECT v FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	page, all, truncated, _, err := collectRows(rows, names, Profile{}, 10)
	if err == nil || !strings.Contains(err.Error(), "result_too_large") {
		t.Fatalf("collectRows err = %v, want result_too_large", err)
	}
	if len(page) != 0 || len(all) != 0 || truncated {
		t.Fatalf("collectRows accepted the oversized first row: page=%d all=%d truncated=%v", len(page), len(all), truncated)
	}
}

// Item 3 (major): a symlink/junction inside the workspace pointing outside it
// must be rejected by resolvePath, and therefore by read_table/write_table.
func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside.xlsx")
	if err := os.WriteFile(outside, []byte("not really xlsx"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, nil)
	defer m.Close()
	m.SetWorkspaceRoot(root)

	assertDenied := func(rel string) {
		t.Helper()
		if _, err := m.resolvePath(rel, false); err == nil || !strings.Contains(err.Error(), "path_denied") {
			t.Fatalf("resolvePath(%q) err = %v, want path_denied", rel, err)
		}
		if _, err := readTableAction(context.Background(), m, map[string]interface{}{"file_path": rel}); err == nil || !strings.Contains(err.Error(), "path_denied") {
			t.Fatalf("read_table(%q) err = %v, want path_denied", rel, err)
		}
		if _, err := writeTableActionWithManager(context.Background(), m, map[string]interface{}{"file_path": rel}, false); err == nil || !strings.Contains(err.Error(), "path_denied") {
			t.Fatalf("write_table(%q) err = %v, want path_denied", rel, err)
		}
	}

	checked := false
	if err := os.Symlink(outside, filepath.Join(root, "link.xlsx")); err == nil {
		checked = true
		assertDenied("link.xlsx")
	}
	if runtime.GOOS == "windows" {
		// Directory junctions need no elevation on Windows; they are resolved
		// by EvalSymlinks just like symlinks.
		junction := filepath.Join(root, "jdir")
		if err := exec.Command("cmd", "/c", "mklink", "/J", junction, outsideDir).Run(); err == nil {
			checked = true
			assertDenied(filepath.Join("jdir", "outside.xlsx"))
		}
	}
	if !checked {
		t.Skip("no symlink/junction capability in this environment")
	}

	// Control: a regular file inside the workspace still resolves.
	if err := os.WriteFile(filepath.Join(root, "ok.xlsx"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := m.resolvePath("ok.xlsx", false)
	if err != nil {
		t.Fatalf("resolvePath(regular file) failed: %v", err)
	}
	if !strings.HasPrefix(strings.ToLower(resolved), strings.ToLower(root)) {
		t.Fatalf("resolvePath(regular file) = %q, want under %q", resolved, root)
	}
}

// Item 4 (major): the write_table/export_excel approval fingerprint must bind
// the data.rows fallback content, matching the write-side fallback semantics.
func TestFileActionParamsMapBindsDataRowsFallback(t *testing.T) {
	withData := func(dataRows interface{}) map[string]interface{} {
		return map[string]interface{}{
			"sheet": "S", "range": "A1:B1",
			"rows": []interface{}{},
			"data": map[string]interface{}{"rows": dataRows},
		}
	}
	fpA := paramsFingerprint(fileActionParamsMap(withData([]interface{}{[]interface{}{"a", 1}})))
	fpB := paramsFingerprint(fileActionParamsMap(withData([]interface{}{[]interface{}{"b", 1}})))
	if fpA == fpB {
		t.Fatal("data.rows fallback content is not bound into the approval fingerprint")
	}
	// An explicit non-empty rows array takes precedence over data.rows.
	direct := map[string]interface{}{
		"sheet": "S", "range": "A1:B1",
		"rows": []interface{}{[]interface{}{"a", 1}},
		"data": map[string]interface{}{"rows": []interface{}{[]interface{}{"b", 1}}},
	}
	if got := paramsFingerprint(fileActionParamsMap(direct)); got != fpA {
		t.Fatal("explicit rows must take precedence over data.rows in the fingerprint")
	}
	// Sanity: identical fallback content reproduces the fingerprint.
	fpA2 := paramsFingerprint(fileActionParamsMap(withData([]interface{}{[]interface{}{"a", 1}})))
	if fpA != fpA2 {
		t.Fatal("fingerprint is not deterministic")
	}
}

// Item 5 (major): driver errors must not echo the resolved profile secret.
func TestSQLAdapterSanitizeRedactsSecret(t *testing.T) {
	a := &sqlAdapter{secret: "Sup3rSecret!"}
	err := a.sanitize(errors.New(`password authentication failed for user "u": Sup3rSecret! rejected`))
	if err == nil {
		t.Fatal("sanitize(nil-ish) unexpectedly returned nil")
	}
	if strings.Contains(err.Error(), "Sup3rSecret!") {
		t.Fatalf("secret leaked in error: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("expected redaction marker in: %v", err)
	}
	// Empty secret: the error passes through untouched.
	b := &sqlAdapter{}
	raw := errors.New("boom")
	if got := b.sanitize(raw); got != raw {
		t.Fatalf("sanitize changed an error without a secret: %v", got)
	}
	if got := a.sanitize(nil); got != nil {
		t.Fatalf("sanitize(nil) = %v, want nil", got)
	}
}

// Item 6 (minor): cursor pagination limit above the 5000 ceiling must clamp
// to 5000, not to the 100 default.
func TestReadResultPageClampsLimitToMax(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	rows := make([][]interface{}, 200)
	for i := range rows {
		rows[i] = []interface{}{i}
	}
	token := m.storeResultAt(QueryResult{Columns: []Column{{Name: "n"}}, Rows: rows[:1], allRows: rows}, "", "", 0)
	if token == "" {
		t.Fatal("storeResultAt returned no token")
	}
	page, err := m.readResultPageFor(token, "", "", "", 6000)
	if err != nil {
		t.Fatal(err)
	}
	if page.RowCount != 200 || page.Truncated {
		t.Fatalf("limit clamp regression: RowCount=%d Truncated=%v, want 200 rows in one page", page.RowCount, page.Truncated)
	}
}

// Items 7+8 (performance): invalid SQL is rejected before any import work,
// and repeated queries over the staged in-memory table keep working with a
// pinned single connection.
func TestQueryExcelValidatesBeforeImport(t *testing.T) {
	p := filepath.Join(t.TempDir(), "q.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{
		{{Value: "name"}, {Value: "amount"}},
		{{Value: "a"}, {Value: 3}},
		{{Value: "b"}, {Value: 4}},
	}}}}); err != nil {
		t.Fatal(err)
	}
	prof := Profile{ID: "x", Type: SourceExcel, FilePath: p}

	if _, err := queryExcel(context.Background(), prof, "", QueryRequest{SQL: "DROP TABLE sheet"}); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("queryExcel(DROP) err = %v, want permission rejection", err)
	}
	// The pinned single connection must survive repeated queries (regression:
	// without SetMaxOpenConns(1) a reopened connection loses the staged table).
	for i := 0; i < 2; i++ {
		res, err := queryExcel(context.Background(), prof, "", QueryRequest{SQL: "SELECT name FROM sheet WHERE amount > 3"})
		if err != nil {
			t.Fatalf("queryExcel iteration %d: %v", i, err)
		}
		if res.RowCount != 1 || len(res.Rows) != 1 || res.Rows[0][0] != "b" {
			t.Fatalf("queryExcel iteration %d: rows = %v", i, res.Rows)
		}
	}
}

// Item 10 (minor): equivalent path spellings share one PendingStore.
func TestPendingStoreKeyNormalizes(t *testing.T) {
	resetPendingStoresForTest()
	defer resetPendingStoresForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "database_pending.json")
	s1, err := NewPendingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewPendingStore(filepath.Join(dir, ".", "database_pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatal("lexically equivalent paths produced two PendingStore instances")
	}
	if runtime.GOOS == "windows" {
		// Case variants of the same file must also share the singleton.
		variant := filepath.Join(dir, "Database_Pending.JSON")
		s3, err := NewPendingStore(variant)
		if err != nil {
			t.Fatal(err)
		}
		if s3 != s1 {
			t.Fatal("case-variant path produced a second PendingStore on Windows")
		}
	}
}

// TestQueryPathRejectsPostgresBracketDesyncPoC verifies the dialect-aware
// guard is wired into the adapter query path: the jsonb-subscript PoC that
// desyncs a dialect-blind scanner must be rejected before reaching the driver.
func TestQueryPathRejectsPostgresBracketDesyncPoC(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	a.dialect = "postgres"
	poc := "SELECT j['a]b'] INTO backup FROM orders WHERE id = $$1$$"
	if _, err := a.Query(context.Background(), QueryRequest{SQL: poc}); err == nil {
		t.Fatal("postgres bracket-desync PoC reached the driver")
	} else if !strings.Contains(err.Error(), "read-only") && !strings.Contains(err.Error(), "permission") && !strings.Contains(err.Error(), "syntax") {
		t.Fatalf("unexpected error classification: %v", err)
	}
	// A legitimate jsonb subscript read must still pass validation (it will
	// fail later against sqlite, but not at the guard).
	if err := validateReadSQLDialect("SELECT j['a'] FROM orders", "postgres"); err != nil {
		t.Fatalf("legitimate jsonb subscript rejected: %v", err)
	}
}

// TestMutationPathUsesDialectWhereScan wires the dialect-aware top-level WHERE
// detection into the mutation gate: subquery/dollar-quoted/escaped fakes must
// not count as a real clause.
func TestMutationPathUsesDialectWhereScan(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	_, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{{SQL: "UPDATE orders SET amount = (SELECT 1 FROM orders WHERE id = 1)"}},
		DryRun:     true,
	})
	if err == nil || !strings.Contains(err.Error(), "WHERE") {
		t.Fatalf("subquery WHERE accepted as the statement clause: %v", err)
	}
}
