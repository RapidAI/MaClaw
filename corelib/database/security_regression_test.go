package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// C1: EXPLAIN ANALYZE <DML> executes the statement on PostgreSQL and must not
// pass the read-only gate.
func TestGuardSQLRejectsExplainAnalyze(t *testing.T) {
	for _, sqlText := range []string{
		"EXPLAIN ANALYZE UPDATE t SET a=1",
		"explain (analyze, buffers) delete from t",
		"EXPLAIN (ANALYZE true) DELETE FROM t",
		"EXPLAIN (analyze) INSERT INTO t VALUES (1)",
	} {
		if got, err := GuardSQL(sqlText); err == nil {
			t.Fatalf("GuardSQL(%q) class=%s, want fail-closed rejection", sqlText, got.Class)
		} else if !strings.Contains(err.Error(), "permission") {
			t.Fatalf("GuardSQL(%q) err=%v, want permission class", sqlText, err)
		}
	}
	for _, sqlText := range []string{
		"EXPLAIN SELECT 1",
		"EXPLAIN (BUFFERS) SELECT 1",
		"EXPLAIN (FORMAT JSON) SELECT 1",
	} {
		got, err := GuardSQL(sqlText)
		if err != nil || got.Class != StatementRead {
			t.Fatalf("GuardSQL(%q) class=%s err=%v, want read", sqlText, got.Class, err)
		}
	}
	if err := validateReadSQL("EXPLAIN ANALYZE UPDATE t SET a=1"); err == nil {
		t.Fatal("query path accepted EXPLAIN ANALYZE")
	}
}

// C2: a WITH prefix only introduces the CTE list; the statement that follows
// it must be classified, and DML main statements must be rejected.
func TestGuardSQLWithMainStatement(t *testing.T) {
	for _, sqlText := range []string{
		"WITH x AS (SELECT 1) DELETE FROM t",
		"WITH x AS (SELECT 1) UPDATE t SET a=1",
		"WITH x AS (SELECT 1) INSERT INTO t VALUES (1)",
		"WITH RECURSIVE x AS (SELECT 1) DELETE FROM t",
		"WITH x AS (SELECT 1), y AS (SELECT * FROM x) MERGE INTO t USING y ON t.id=y.id WHEN MATCHED THEN DELETE",
	} {
		if got, err := GuardSQL(sqlText); err == nil {
			t.Fatalf("GuardSQL(%q) class=%s, want fail-closed rejection", sqlText, got.Class)
		} else if !strings.Contains(err.Error(), "permission") {
			t.Fatalf("GuardSQL(%q) err=%v, want permission class", sqlText, err)
		}
		if err := validateReadSQL(sqlText); err == nil {
			t.Fatalf("query path accepted %q", sqlText)
		}
	}
	for _, sqlText := range []string{
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"WITH a AS (SELECT 1), b AS (SELECT * FROM a) SELECT * FROM b",
		"WITH x(n) AS (SELECT (1 + (2))) SELECT n FROM x",
		"WITH x AS (SELECT ') delete' AS s) SELECT s FROM x", // literal must not break paren matching
		"WITH [my cte] AS (SELECT 1) SELECT * FROM [my cte]",
		"WITH RECURSIVE x AS (SELECT 1) SELECT * FROM x",
	} {
		got, err := GuardSQL(sqlText)
		if err != nil || got.Class != StatementRead {
			t.Fatalf("GuardSQL(%q) class=%s err=%v, want read", sqlText, got.Class, err)
		}
	}
	// Unparseable WITH prefixes fail closed.
	for _, sqlText := range []string{"WITH x AS SELECT 1", "WITH (x) AS (SELECT 1) SELECT 1"} {
		if _, err := GuardSQL(sqlText); err == nil {
			t.Fatalf("GuardSQL(%q) accepted an unparseable WITH prefix", sqlText)
		}
	}
}

// M1: a string literal containing ' where ' must not satisfy the mandatory
// WHERE check for UPDATE/DELETE.
func TestValidateMutationWhereLiteralBypass(t *testing.T) {
	for _, sqlText := range []string{
		"UPDATE t SET note=' where '",
		"DELETE FROM t", // baseline
		"UPDATE t SET note='it''s where '",
		"UPDATE t SET note=\" where \"",
	} {
		if err := validateMutationSQL(sqlText); err == nil {
			t.Fatalf("validateMutationSQL(%q) accepted a missing WHERE clause", sqlText)
		}
	}
	for _, sqlText := range []string{
		"DELETE FROM t WHERE note='x'",
		"UPDATE t SET a=1 WHERE(a=1)", // keyword glued to a parenthesis still counts
		"UPDATE t SET a=1 where a=2",  // case-insensitive
	} {
		if err := validateMutationSQL(sqlText); err != nil {
			t.Fatalf("validateMutationSQL(%q) rejected a real WHERE clause: %v", sqlText, err)
		}
	}
}

// M3: DDL in a multi-statement batch breaks the atomic commit/rollback
// contract (implicit commits) and is rejected regardless of allow_ddl.
func TestBatchRejectsDDLMixedWithDML(t *testing.T) {
	statements := []BatchStatement{
		{SQL: "UPDATE orders SET amount = :amount WHERE id = :id", Params: map[string]interface{}{"amount": 11, "id": 1}},
		{SQL: "CREATE TABLE archive (id INTEGER)"},
	}
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true, AllowDDL: true})
	if _, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{Statements: statements, DryRun: true}); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("dry-run batch with DDL accepted: %v", err)
	}
	if _, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{Statements: statements, DryRun: false, ApprovalToken: "t"}); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("commit batch with DDL accepted: %v", err)
	}
	if got := orderAmounts(t, a.db); got[1] != 10 || got[2] != 20 {
		t.Fatalf("rejected batch mutated data: %v", got)
	}
}

// M3 counterpart: a single-statement execute keeps the allow_ddl gate.
func TestSingleStatementDDLFollowsAllowDDL(t *testing.T) {
	a := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true, AllowDDL: true})
	preview, err := a.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{{SQL: "CREATE TABLE archive (id INTEGER)"}},
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("single-statement DDL with allow_ddl rejected: %v", err)
	}
	if preview.DryRunGuarantee != "policy_only" {
		t.Fatalf("guarantee = %q, want policy_only", preview.DryRunGuarantee)
	}
	b := newDryRunTestAdapter(t, Profile{ID: "t", WriteEnabled: true})
	if _, err := b.ExecuteBatch(context.Background(), BatchExecuteRequest{
		Statements: []BatchStatement{{SQL: "CREATE TABLE archive (id INTEGER)"}},
		DryRun:     true,
	}); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("single-statement DDL without allow_ddl accepted: %v", err)
	}
}

// srv#1: inline credentials in a profile DSN are rejected; secrets must flow
// through secret_ref.
func TestValidateProfileRejectsDSNInlineCredentials(t *testing.T) {
	base := Profile{ID: "p", Type: SourceAccess, FilePath: "x.accdb"}
	for _, dsn := range []string{
		"Driver={Microsoft Access Driver (*.mdb, *.accdb)};DBQ=x.accdb;PWD=secret;",
		"driver=x;Password=secret",
		"postgres://user:pass@host/db",
		"sqlserver://sa:P%40ss@host:1433?database=x",
	} {
		p := base
		p.DSN = dsn
		if err := ValidateProfile(p); err == nil || !strings.Contains(err.Error(), "permission") {
			t.Fatalf("DSN %q with inline credentials accepted: %v", dsn, err)
		}
	}
	for _, dsn := range []string{
		"Driver={Microsoft Access Driver (*.mdb, *.accdb)};DBQ=x.accdb;",
		"postgres://user@host/db", // userinfo without a password is not a credential
	} {
		p := base
		p.DSN = dsn
		if err := ValidateProfile(p); err != nil {
			t.Fatalf("credential-free DSN %q rejected: %v", dsn, err)
		}
	}
}

// M2: in strict mode the caller-computed operation fingerprint must match the
// fingerprint bound at issuance.
func TestConsumeApprovalBindsOperationFingerprint(t *testing.T) {
	m, _, _ := newPendingTestManager(t)
	defer m.Close()
	fp := sqlFingerprint("update orders set x=1 where id=1")
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: fp})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	if err := m.consumeApproval(context.Background(), approval, sqlFingerprint("delete from orders where id=1")); err == nil {
		t.Fatal("operation fingerprint mismatch accepted")
	}
	// The failed attempt must not have consumed the legitimate approval.
	if err := m.consumeApproval(context.Background(), approval, fp); err != nil {
		t.Fatalf("matching fingerprint rejected: %v", err)
	}
}

// M4: a pending mutation bound to an owner/session can only be consumed by a
// request carrying the same scope.
func TestConsumeApprovalEnforcesOwnerBinding(t *testing.T) {
	m, _, _ := newPendingTestManager(t)
	defer m.Close()
	fp := sqlFingerprint("update orders set x=1 where id=1")
	approval, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: fp, OwnerID: "owner-a", SessionID: "session-a"})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	crossCtx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner-b", SessionID: "session-b"})
	if err := m.consumeApproval(crossCtx, approval, fp); err == nil || !strings.Contains(err.Error(), "another session") {
		t.Fatalf("cross-owner consume accepted: %v", err)
	}
	// A scope-less request must not consume an owner-bound approval either.
	approval2, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: fp, OwnerID: "owner-a", SessionID: "session-a"})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	if err := m.consumeApproval(context.Background(), approval2, fp); err == nil {
		t.Fatal("scope-less request consumed an owner-bound approval")
	}
	approval3, err := m.IssueApproval(ApprovalRequest{ProfileID: "p", SQLFingerprint: fp, OwnerID: "owner-a", SessionID: "session-a"})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	sameCtx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner-a", SessionID: "session-a"})
	if err := m.consumeApproval(sameCtx, approval3, fp); err != nil {
		t.Fatalf("same-owner consume rejected: %v", err)
	}
}

// guiapp#3: stores constructed over the same path share one in-memory state
// and lock, so concurrent handlers cannot flush over each other.
func TestPendingStoreIsSingletonPerPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database_pending.json")
	a, err := NewPendingStore(path)
	if err != nil {
		t.Fatalf("NewPendingStore: %v", err)
	}
	b, err := NewPendingStore(path)
	if err != nil {
		t.Fatalf("NewPendingStore: %v", err)
	}
	item := PendingMutation{ID: "m1", TokenHash: "hash", SQLFingerprint: "fp", ExpiresAt: time.Now().Add(time.Minute)}
	if err := a.Put(item); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if b.Len() != 1 {
		t.Fatalf("second instance does not observe the first instance's state, len=%d", b.Len())
	}
	if _, err := b.Consume("m1", "hash", "fp", "", "", 0); err != nil {
		t.Fatalf("Consume through second instance: %v", err)
	}
	if _, err := a.Consume("m1", "hash", "fp", "", "", 0); err == nil {
		t.Fatal("consumed entry resurrected through the first instance")
	}
}

// M2: file-action approvals bind the exact content. Committing rows that
// differ from the previewed fingerprint is rejected; a matching fingerprint
// commits.
func TestFileActionApprovalBindsContent(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	m.SetWorkspaceRoot(t.TempDir())
	store, err := NewPendingStore(filepath.Join(t.TempDir(), "database_pending.json"))
	if err != nil {
		t.Fatalf("NewPendingStore: %v", err)
	}
	m.SetPendingStore(store)

	args := map[string]interface{}{
		"action": "write_table", "file_path": "out.xlsx", "sheet": "Sheet1",
		"rows": []interface{}{[]interface{}{"a", "b"}}, "dry_run": false,
	}
	sqlFP, paramsFP, err := MutationFingerprints(args)
	if err != nil {
		t.Fatalf("MutationFingerprints: %v", err)
	}
	approval, err := m.IssueApproval(ApprovalRequest{SQLFingerprint: sqlFP, ParamsFingerprint: paramsFP})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	if got := HandleTool(WithApprovalContext(context.Background(), approval), m, args); !strings.Contains(got, `"ok":true`) {
		t.Fatalf("commit with matching fingerprint failed: %q", got)
	}

	// Issue against the original rows, then commit different rows: the params
	// fingerprint no longer matches and the commit is rejected before any
	// write.
	approval2, err := m.IssueApproval(ApprovalRequest{SQLFingerprint: sqlFP, ParamsFingerprint: paramsFP})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	tampered := map[string]interface{}{
		"action": "write_table", "file_path": "out.xlsx", "sheet": "Sheet1",
		"rows": []interface{}{[]interface{}{"tampered"}}, "dry_run": false,
	}
	got := HandleTool(WithApprovalContext(context.Background(), approval2), m, tampered)
	if !strings.Contains(got, "parameter fingerprint") {
		t.Fatalf("rows swapped after approval committed: %q", got)
	}
}

// M2: a range write into an existing workbook requires the preview
// source_sha256 at commit time.
func TestFileActionRangeWriteRequiresSourceHash(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	m.SetWorkspaceRoot(t.TempDir())
	store, err := NewPendingStore(filepath.Join(t.TempDir(), "database_pending.json"))
	if err != nil {
		t.Fatalf("NewPendingStore: %v", err)
	}
	m.SetPendingStore(store)

	// Create the workbook (new file, no range: no source hash needed).
	createArgs := map[string]interface{}{
		"action": "write_table", "file_path": "out.xlsx", "sheet": "Sheet1",
		"rows": []interface{}{[]interface{}{"a"}}, "dry_run": false,
	}
	sqlFP, paramsFP, err := MutationFingerprints(createArgs)
	if err != nil {
		t.Fatalf("MutationFingerprints: %v", err)
	}
	approval, err := m.IssueApproval(ApprovalRequest{SQLFingerprint: sqlFP, ParamsFingerprint: paramsFP})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	if got := HandleTool(WithApprovalContext(context.Background(), approval), m, createArgs); !strings.Contains(got, `"ok":true`) {
		t.Fatalf("create commit failed: %q", got)
	}

	// Range write into the now-existing workbook without source_sha256.
	rangeArgs := map[string]interface{}{
		"action": "write_table", "file_path": "out.xlsx", "sheet": "Sheet1", "range": "A1",
		"rows": []interface{}{[]interface{}{"z"}}, "dry_run": false,
	}
	rangeFP, rangeParamsFP, err := MutationFingerprints(rangeArgs)
	if err != nil {
		t.Fatalf("MutationFingerprints: %v", err)
	}
	approval2, err := m.IssueApproval(ApprovalRequest{SQLFingerprint: rangeFP, ParamsFingerprint: rangeParamsFP})
	if err != nil {
		t.Fatalf("IssueApproval: %v", err)
	}
	if got := HandleTool(WithApprovalContext(context.Background(), approval2), m, rangeArgs); !strings.Contains(got, "source_sha256") {
		t.Fatalf("range write without source_sha256 committed: %q", got)
	}
}

// Review C-1: '[' is an array/jsonb subscript operator in PostgreSQL, not an
// identifier quote; treating it as one desynchronized the scanners and let a
// top-level INTO hide inside what looked like a quoted region.
func TestGuardSQLDialectPostgresBracketSubscript(t *testing.T) {
	poc := "SELECT j['a]b'] INTO backup FROM t WHERE note = $$'$$"
	got, err := GuardSQLDialect(poc, "postgres")
	if err != nil {
		t.Fatalf("GuardSQLDialect(postgres PoC) unexpected error: %v", err)
	}
	if got.Class != StatementExternal {
		t.Fatalf("GuardSQLDialect(postgres PoC) class=%s, want external (top-level INTO must be detected)", got.Class)
	}
	// The unknown-dialect wrapper keeps the historical conservative bracket
	// behavior; passing the real dialect is the caller's responsibility.
	if got, err := GuardSQL(poc); err != nil || got.Class != StatementRead {
		t.Fatalf("GuardSQL(unknown-dialect PoC) class=%s err=%v, want read (documented legacy behavior)", got.Class, err)
	}
	// Ordinary jsonb subscript queries still classify as read.
	for _, sqlText := range []string{
		"SELECT j['a'] FROM t",
		"SELECT j['a'] FROM t WHERE j['b'] = ']'",
	} {
		got, err := GuardSQLDialect(sqlText, "postgres")
		if err != nil || got.Class != StatementRead {
			t.Fatalf("GuardSQLDialect(%q, postgres) class=%s err=%v, want read", sqlText, got.Class, err)
		}
	}
	// Bracket identifier quoting is unchanged for sqlserver.
	for _, sqlText := range []string{
		"SELECT [name] FROM [dbo].[orders]",
		"SELECT [a;b] FROM [t]",
	} {
		got, err := GuardSQLDialect(sqlText, "sqlserver")
		if err != nil || got.Class != StatementRead {
			t.Fatalf("GuardSQLDialect(%q, sqlserver) class=%s err=%v, want read", sqlText, got.Class, err)
		}
	}
	// Dollar-quoted literals must not parse as comments, statement separators
	// or keywords.
	for _, sqlText := range []string{
		"SELECT $$a;b INTO backup$$ FROM t",
		"SELECT $tag$into$tag$ FROM t",
		"SELECT $$--not a comment$$ FROM t",
		"SELECT $$/*nor this*/$$ FROM t",
		"SELECT a $1, b FROM t", // $1 is a parameter placeholder, not a quote
	} {
		got, err := GuardSQLDialect(sqlText, "postgres")
		if err != nil || got.Class != StatementRead {
			t.Fatalf("GuardSQLDialect(%q, postgres) class=%s err=%v, want read", sqlText, got.Class, err)
		}
	}
	// A real INTO after a dollar-quoted literal is still detected.
	got2, err := GuardSQLDialect("SELECT $$x$$ INTO backup FROM t", "postgres")
	if err != nil || got2.Class != StatementExternal {
		t.Fatalf("dollar-quoted literal hid a following INTO: class=%s err=%v", got2.Class, err)
	}
	// Unterminated dollar quotes fail closed.
	if _, err := GuardSQLDialect("SELECT $$x FROM t", "postgres"); err == nil {
		t.Fatal("unterminated dollar-quoted literal accepted")
	}
}

// Review C-2: the data-modifying CTE check used substring matching with a
// mandatory space after AS, so AS( with zero space slipped through. The check
// is now folded into the WITH parse pass, which reads the first keyword of
// each CTE body directly.
func TestGuardSQLDataModifyingCTEZeroSpace(t *testing.T) {
	for _, sqlText := range []string{
		"WITH x AS(DELETE FROM t RETURNING *) SELECT * FROM x",
		"WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x",
		"WITH x AS(UPDATE t SET a=1 RETURNING *) SELECT * FROM x",
		"WITH x AS(INSERT INTO t VALUES (1) RETURNING *) SELECT * FROM x",
		"WITH x(a) AS(DELETE FROM t RETURNING *) SELECT * FROM x",       // column list, zero space
		"WITH x(a,b) AS (UPDATE t SET a=1 RETURNING *) SELECT * FROM x", // column list, spaced
		"WITH a AS (SELECT 1), b AS(DELETE FROM t RETURNING *) SELECT * FROM b",
		"WITH a AS(INSERT INTO t VALUES (1) RETURNING *), b AS (SELECT 1) SELECT * FROM b",
		"WITH x AS(MERGE INTO t USING s ON t.id=s.id WHEN MATCHED THEN DELETE) SELECT * FROM x",
		"WITH x AS(delete from t returning *) select * from x", // case-insensitive
	} {
		if got, err := GuardSQL(sqlText); err == nil {
			t.Fatalf("GuardSQL(%q) class=%s, want fail-closed rejection", sqlText, got.Class)
		} else if !strings.Contains(err.Error(), "permission") {
			t.Fatalf("GuardSQL(%q) err=%v, want permission class", sqlText, err)
		}
		if err := validateReadSQL(sqlText); err == nil {
			t.Fatalf("query path accepted %q", sqlText)
		}
	}
	for _, sqlText := range []string{
		"WITH x AS(SELECT 1) SELECT * FROM x",
		"WITH x(a) AS(SELECT 1) SELECT a FROM x",
		"WITH a AS(SELECT 1), b AS(SELECT * FROM a) SELECT * FROM b",
		"WITH x AS (VALUES (1)) SELECT * FROM x",
		"WITH x AS (SELECT 'as(update' AS s) SELECT s FROM x", // literal must not trip the check
	} {
		got, err := GuardSQL(sqlText)
		if err != nil || got.Class != StatementRead {
			t.Fatalf("GuardSQL(%q) class=%s err=%v, want read", sqlText, got.Class, err)
		}
	}
}

// Review M-1: HasTopLevelKeyword strips comments and dialect-aware literals
// before the depth-0 word-boundary match, so WHERE inside a subquery or a
// literal no longer satisfies a top-level keyword check.
func TestHasTopLevelKeywordWhere(t *testing.T) {
	noTopLevelWhere := []struct{ sql, dialect string }{
		{"UPDATE t SET a=(SELECT 1 FROM u WHERE u.id=1)", ""},      // WHERE only in subquery
		{"UPDATE t SET a=$$where$$", "postgres"},                   // dollar-quoted literal
		{"UPDATE t SET a='P\\' WHERE 1=1 AND 1=', b='3'", "mysql"}, // backslash escape keeps the literal open
		{"UPDATE t SET note=' where '", ""},                        // plain literal
	}
	for _, tc := range noTopLevelWhere {
		if HasTopLevelKeyword(tc.sql, tc.dialect, "where") {
			t.Fatalf("HasTopLevelKeyword(%q, %q) reported a top-level WHERE", tc.sql, tc.dialect)
		}
	}
	topLevelWhere := []struct{ sql, dialect string }{
		{"UPDATE t SET a=1 WHERE id=2", ""},
		{"UPDATE t SET a=1 where(a=1)", "mysql"},                     // keyword glued to a parenthesis still counts
		{"DELETE FROM t WHERE note='x' /* trailing */", "postgres"},  // comments stripped first
		{"UPDATE t SET a=$tag$where$tag$ WHERE x=$$y$$", "postgres"}, // literals stripped, real WHERE remains
	}
	for _, tc := range topLevelWhere {
		if !HasTopLevelKeyword(tc.sql, tc.dialect, "where") {
			t.Fatalf("HasTopLevelKeyword(%q, %q) missed a real top-level WHERE", tc.sql, tc.dialect)
		}
	}
	// Unterminated input fails closed for a "must contain keyword" check.
	if HasTopLevelKeyword("UPDATE t SET a='x WHERE id=1", "", "where") {
		t.Fatal("unterminated literal reported a top-level WHERE")
	}
}
