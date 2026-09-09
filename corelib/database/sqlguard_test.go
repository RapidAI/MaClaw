package database

import "testing"

func TestGuardSQLClassifiesAndFailsClosed(t *testing.T) {
	got, err := GuardSQL("SELECT ';' AS value")
	if err != nil || got.Class != StatementRead {
		t.Fatalf("read classification=%#v err=%v", got, err)
	}
	for _, sqlText := range []string{"select 1; delete from t", "wat nonsense"} {
		if _, err := GuardSQL(sqlText); err == nil {
			t.Fatalf("expected fail-closed rejection for %q", sqlText)
		}
	}
	if got, err := GuardSQL("grant all on t to x"); err != nil || got.Class != StatementDDL {
		t.Fatalf("DDL classification=%#v err=%v", got, err)
	}
	if err := validateReadSQL("select 1 -- comment"); err == nil {
		t.Fatal("expected query comment rejection")
	}
	if _, err := GuardSQL("WITH x AS (UPDATE t SET a=1) SELECT * FROM x"); err == nil {
		t.Fatal("expected data-modifying CTE rejection")
	}
	if _, err := GuardSQL("WITH x AS (\n UPDATE t SET a=1\n) SELECT * FROM x"); err == nil {
		t.Fatal("expected whitespace-separated data-modifying CTE rejection")
	}
}

func TestValidateMutationRequiresWhereForUpdateDelete(t *testing.T) {
	for _, sqlText := range []string{"UPDATE t SET a=:a", "DELETE FROM t"} {
		if err := validateMutationSQL(sqlText); err == nil {
			t.Fatalf("expected WHERE requirement for %q", sqlText)
		}
	}
	if err := validateMutationSQL("UPDATE t SET a=:a WHERE id=:id"); err != nil {
		t.Fatalf("valid bounded update rejected: %v", err)
	}
}

func TestTopLevelOrderByWarningDetectorSkipsLiterals(t *testing.T) {
	if !hasTopLevelKeywordPair("SELECT id FROM t ORDER BY id", "order", "by") {
		t.Fatal("ORDER BY was not detected")
	}
	if hasTopLevelKeywordPair("SELECT 'order by' AS note FROM t", "order", "by") {
		t.Fatal("literal text was treated as ORDER BY")
	}
}

func TestGuardSQLSelectIntoIsExternal(t *testing.T) {
	for _, sqlText := range []string{
		"SELECT * INTO backup FROM orders",
		"select id into t2 from t1",
		"SELECT * INTO OUTFILE '/tmp/x' FROM t",
		"SELECT a, b FROM t INTO OUTFILE '/tmp/x'",
		"WITH x AS (SELECT 1 AS a) SELECT a INTO dst FROM x",
		"SELECT\u3000INTO t FROM x", // full-width space must not bypass
		"SELECT\u00a0INTO t FROM x", // NBSP must not bypass
	} {
		got, err := GuardSQL(sqlText)
		if err != nil {
			t.Fatalf("GuardSQL(%q) unexpected error: %v", sqlText, err)
		}
		if got.Class != StatementExternal {
			t.Fatalf("GuardSQL(%q) class=%s, want external", sqlText, got.Class)
		}
	}
	// Both the query and execute paths must refuse SELECT INTO.
	if err := validateReadSQL("SELECT * INTO backup FROM orders"); err == nil {
		t.Fatal("query path accepted SELECT INTO")
	}
	if err := validateMutationSQL("SELECT * INTO backup FROM orders"); err == nil {
		t.Fatal("execute path accepted SELECT INTO")
	}
	// Non-top-level or non-keyword 'into' must not be flagged.
	for _, sqlText := range []string{
		"SELECT * FROM t WHERE note = 'into'",
		"SELECT * FROM t WHERE note = 'x''into''y'",
		"SELECT [into] FROM t",
		"SELECT `into` FROM t",
		"SELECT \"into\" FROM t",
		"SELECT into_col FROM t",
		"SELECT x FROM (SELECT a INTO b FROM t) AS sub", // nested in parens: not top-level
	} {
		got, err := GuardSQL(sqlText)
		if err != nil {
			t.Fatalf("GuardSQL(%q) unexpected error: %v", sqlText, err)
		}
		if got.Class != StatementRead {
			t.Fatalf("GuardSQL(%q) class=%s, want read", sqlText, got.Class)
		}
	}
	// INSERT INTO is DML and must not be touched by the INTO detector.
	if got, err := GuardSQL("INSERT INTO t VALUES (1)"); err != nil || got.Class != StatementDML {
		t.Fatalf("INSERT INTO classification=%#v err=%v", got, err)
	}
}

func TestGuardSQLDialectFixtures(t *testing.T) {
	readCases := map[string]string{
		"mysql backtick semicolon":    "SELECT `a;b` FROM t",
		"mysql backtick dashes":       "SELECT `a--b` FROM t",
		"mysql backtick blockcomment": "SELECT `a/*b*/c` FROM t",
		"postgres cast":               "SELECT amount::numeric FROM t",
		"postgres json arrow":         "SELECT data->>'k' FROM t",
		"postgres json exists":        "SELECT data->>'k' FROM t WHERE data ? 'x'",
		"sqlserver brackets":          "SELECT [name] FROM [dbo].[orders]",
		"sqlserver bracket semicolon": "SELECT [a;b] FROM [t]",
		"access date literal":         "SELECT * FROM t WHERE d = #2026-01-01#",
		"semicolon in nested literal": "SELECT * FROM t WHERE x IN (SELECT ';' FROM y)",
		"unicode fullwidth spaces":    "SELECT\u3000*\u3000FROM\u3000t",
		"unicode nbsp":                "SELECT\u00a01",
	}
	for name, sqlText := range readCases {
		got, err := GuardSQL(sqlText)
		if err != nil {
			t.Fatalf("%s: GuardSQL(%q) unexpected error: %v", name, sqlText, err)
		}
		if got.Class != StatementRead {
			t.Fatalf("%s: GuardSQL(%q) class=%s, want read", name, sqlText, got.Class)
		}
	}

	rejectCases := map[string]string{
		"cte update":            "WITH x AS (UPDATE t SET a=1 RETURNING *) SELECT * FROM x",
		"cte delete":            "WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x",
		"cte insert":            "WITH x AS (INSERT INTO t VALUES (1) RETURNING *) SELECT * FROM x",
		"cte merge":             "WITH x AS (MERGE INTO t USING s ON t.id=s.id WHEN MATCHED THEN UPDATE SET a=1) SELECT * FROM x",
		"unknown first word":    "VACUUM t",
		"unknown unicode space": "VACUUM\u3000t",
		"unterminated bracket":  "SELECT [a FROM t; DELETE FROM t",
		"unterminated literal":  "SELECT 'x FROM t",
	}
	for name, sqlText := range rejectCases {
		if got, err := GuardSQL(sqlText); err == nil {
			t.Fatalf("%s: GuardSQL(%q) class=%s, want fail-closed rejection", name, sqlText, got.Class)
		}
	}
}
