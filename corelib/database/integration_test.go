//go:build dbintegration

package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// Integration tests against real MySQL/PostgreSQL/SQL Server instances.
// DSNs come from the environment; a database whose variable is unset is
// skipped so the suite still compiles and runs (as pure skips) on machines
// without any server:
//
//	MACLAW_DBTEST_MYSQL_DSN  e.g. root:test@tcp(127.0.0.1:3306)/dbtest
//	MACLAW_DBTEST_PG_DSN     e.g. postgres://postgres:postgres@127.0.0.1:5432/dbtest?sslmode=disable
//	MACLAW_DBTEST_MSSQL_DSN  e.g. sqlserver://sa:Test!Passw0rd123@127.0.0.1:1433?database=dbtest
//
// Every subtest drives the real Manager + Profile path (Connect/Inspect/
// Query/Execute, approval issuance, metadata-only audit) instead of talking
// to the bare driver. The bare driver is used only to create and drop the
// throwaway t_dbtest_<rand> fixture table.

type integrationTarget struct {
	name   string
	source SourceType
	driver string
	dsnEnv string
}

func integrationTargets() []integrationTarget {
	return []integrationTarget{
		{name: "mysql", source: SourceMySQL, driver: "mysql", dsnEnv: "MACLAW_DBTEST_MYSQL_DSN"},
		{name: "postgres", source: SourcePostgres, driver: "pgx", dsnEnv: "MACLAW_DBTEST_PG_DSN"},
		{name: "sqlserver", source: SourceSQLServer, driver: "sqlserver", dsnEnv: "MACLAW_DBTEST_MSSQL_DSN"},
	}
}

func TestIntegrationSQLDatabases(t *testing.T) {
	for _, target := range integrationTargets() {
		target := target
		t.Run(target.name, func(t *testing.T) {
			dsn := strings.TrimSpace(os.Getenv(target.dsnEnv))
			if dsn == "" {
				t.Skipf("%s is not set; skipping %s integration test", target.dsnEnv, target.name)
			}
			runSQLDatabaseIntegration(t, target, dsn)
		})
	}
}

// profileFromIntegrationDSN splits a driver DSN into the host-side Profile
// projection (no embedded password) plus the secret the resolver returns.
// This mirrors production, where the model never sees credentials.
func profileFromIntegrationDSN(t *testing.T, target integrationTarget, dsn, table string) (Profile, SecretResolver) {
	t.Helper()
	p := Profile{
		ID:            "it-" + target.name,
		Name:          "integration " + target.name,
		SchemaVersion: 1,
		Type:          target.source,
		WriteEnabled:  true,
		AllowedTables: []string{table},
		SecretRef:     "keyring://maclaw/database/it-" + target.name,
	}
	secret := ""
	switch target.source {
	case SourceMySQL:
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			t.Fatalf("parse mysql DSN: %v", err)
		}
		addr := strings.TrimSuffix(strings.TrimPrefix(cfg.Addr, "tcp("), ")")
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("parse mysql address %q: %v", cfg.Addr, err)
		}
		p.Host, p.Database, p.Username = host, cfg.DBName, cfg.User
		p.Port, _ = strconv.Atoi(port)
		secret = cfg.Passwd
	case SourcePostgres, SourceSQLServer:
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("parse %s DSN: %v", target.name, err)
		}
		p.Host = u.Hostname()
		if port := u.Port(); port != "" {
			p.Port, _ = strconv.Atoi(port)
		}
		p.Username = u.User.Username()
		secret, _ = u.User.Password()
		if target.source == SourceSQLServer {
			p.Database = u.Query().Get("database")
			if p.Database == "" {
				p.Database = strings.TrimPrefix(u.Path, "/")
			}
		} else {
			p.Database = strings.TrimPrefix(u.Path, "/")
		}
	}
	if p.Host == "" || p.Database == "" {
		t.Fatalf("DSN for %s must carry host and database, got host=%q database=%q", target.name, p.Host, p.Database)
	}
	resolver := func(context.Context, string) (string, error) { return secret, nil }
	return p, resolver
}

func setupIntegrationTable(t *testing.T, driver, dsn, table string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	// CI services and local servers can still be warming up; give them a
	// bounded grace period before declaring the target unavailable.
	deadline := time.Now().Add(60 * time.Second)
	for {
		err = db.Ping()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = db.Close()
			t.Fatalf("ping %s: %v", driver, err)
		}
		time.Sleep(time.Second)
	}
	// Drop first so a crashed previous run cannot leave a stale fixture.
	_, _ = db.Exec("DROP TABLE " + table)
	if _, err := db.Exec("CREATE TABLE " + table + " (id INT PRIMARY KEY, name VARCHAR(64), amount INT)"); err != nil {
		_ = db.Close()
		t.Fatalf("create fixture table: %v", err)
	}
	if _, err := db.Exec("INSERT INTO " + table + " (id, name, amount) VALUES (1,'alpha',10),(2,'beta',20),(3,'gamma',30),(4,'delta',40),(5,'epsilon',50)"); err != nil {
		_ = db.Close()
		t.Fatalf("seed fixture table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE " + table)
		_ = db.Close()
	})
	return db
}

func runSQLDatabaseIntegration(t *testing.T, target integrationTarget, dsn string) {
	table := "t_dbtest_" + randomID()[:12]
	setupIntegrationTable(t, target.driver, dsn, table)

	profile, resolver := profileFromIntegrationDSN(t, target, dsn, table)
	m := NewManager([]Profile{profile}, resolver)
	defer m.Close()
	store, err := NewPendingStore(filepath.Join(t.TempDir(), "database_pending.json"))
	if err != nil {
		t.Fatalf("pending store: %v", err)
	}
	m.SetPendingStore(store)

	var auditEvents []AuditEvent
	m.SetAuditSink(func(_ context.Context, event AuditEvent) { auditEvents = append(auditEvents, event) })

	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner-it", SessionID: "session-it"})

	// connect + ping (ConnectFor pings before registering the connection).
	connID, caps, err := m.ConnectFor(ctx, profile.ID, "owner-it", "session-it")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !caps.Read || !caps.Write || !caps.Transactions {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}
	defer func() { _ = m.DisconnectFor(connID, "owner-it", "session-it") }()

	adapter, ok := m.AdapterFor(connID, "owner-it", "session-it")
	if !ok {
		t.Fatal("adapter not bound to owner/session")
	}

	// inspect the fixture table.
	info, err := adapter.Inspect(ctx, InspectRequest{Table: table})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(info.Tables) != 1 || !strings.EqualFold(info.Tables[0].Name, table) {
		t.Fatalf("inspect returned %+v, want fixture table %s", info.Tables, table)
	}
	columns := map[string]bool{}
	for _, c := range info.Tables[0].Columns {
		columns[strings.ToLower(c.Name)] = true
	}
	for _, want := range []string{"id", "name", "amount"} {
		if !columns[want] {
			t.Fatalf("inspect missing column %q in %+v", want, info.Tables[0].Columns)
		}
	}

	query := func(sqlText string, params map[string]interface{}, limit int) QueryResult {
		t.Helper()
		raw := HandleTool(ctx, m, map[string]interface{}{
			"action": "query", "connection_id": connID,
			"sql": sqlText, "params": params, "limit": limit,
		})
		return mustToolResult[QueryResult](t, raw)
	}

	// parameterized query with :name binding.
	page := query("SELECT id, name, amount FROM "+table+" WHERE name = :name", map[string]interface{}{"name": "alpha"}, 100)
	if page.RowCount != 1 || fmt.Sprint(page.Rows[0][1]) != "alpha" {
		t.Fatalf("parameterized query returned %+v", page)
	}

	// limit truncation issues next_cursor and the cursor pages consume the
	// full result exactly once.
	first := query("SELECT id, name, amount FROM "+table+" ORDER BY id", nil, 2)
	if !first.Truncated || first.NextCursor == "" || first.RowCount != 2 {
		t.Fatalf("expected truncated first page with cursor, got %+v", first)
	}
	total := first.RowCount
	cursor := first.NextCursor
	for cursor != "" {
		raw := HandleTool(ctx, m, map[string]interface{}{
			"action": "query", "connection_id": connID, "cursor": cursor, "limit": 2,
		})
		next := mustToolResult[QueryResult](t, raw)
		total += next.RowCount
		cursor = next.NextCursor
	}
	if total != 5 {
		t.Fatalf("paged through %d rows, want 5", total)
	}

	// dry-run preview rolls its transaction back.
	updateSQL := "UPDATE " + table + " SET amount = :amount WHERE id = :id"
	updateParams := map[string]interface{}{"amount": 500, "id": 1}
	rawPreview := HandleTool(ctx, m, map[string]interface{}{
		"action": "execute", "connection_id": connID,
		"sql": updateSQL, "params": updateParams, "dry_run": true,
	})
	preview := mustToolResult[MutationResult](t, rawPreview)
	if !preview.DryRun || preview.DryRunGuarantee != "rolled_back_transaction" || preview.AffectedRows != 1 {
		t.Fatalf("unexpected dry-run result: %+v", preview)
	}
	if got := query("SELECT amount FROM "+table+" WHERE id = :id", map[string]interface{}{"id": 1}, 1); fmt.Sprint(got.Rows[0][0]) != "10" {
		t.Fatalf("dry-run mutated data: %+v", got.Rows)
	}

	// commit requires a host-issued, one-time approval context.
	rawDenied := HandleTool(ctx, m, map[string]interface{}{
		"action": "execute", "connection_id": connID,
		"sql": updateSQL, "params": updateParams, "dry_run": false,
	})
	if !strings.Contains(rawDenied, "approval context") {
		t.Fatalf("commit without approval must fail closed, got: %s", rawDenied)
	}
	commitArgs := map[string]interface{}{
		"action": "execute", "connection_id": connID,
		"sql": updateSQL, "params": updateParams, "dry_run": false,
	}
	sqlFP, paramsFP, err := MutationFingerprints(commitArgs)
	if err != nil {
		t.Fatalf("fingerprints: %v", err)
	}
	approval, err := m.IssueApproval(ApprovalRequest{
		ProfileID: profile.ID, SQLFingerprint: sqlFP, ParamsFingerprint: paramsFP,
		OwnerID: "owner-it", SessionID: "session-it",
	})
	if err != nil {
		t.Fatalf("issue approval: %v", err)
	}
	commit := mustToolResult[MutationResult](t, HandleTool(WithApprovalContext(ctx, approval), m, commitArgs))
	if commit.DryRun || commit.AffectedRows != 1 || commit.ReceiptID == "" {
		t.Fatalf("unexpected commit result: %+v", commit)
	}
	if got := query("SELECT amount FROM "+table+" WHERE id = :id", map[string]interface{}{"id": 1}, 1); fmt.Sprint(got.Rows[0][0]) != "500" {
		t.Fatalf("commit did not apply: %+v", got.Rows)
	}

	// max_affected_rows over the limit rolls the transaction back. Use the
	// commit path so rollback (not just preview) is exercised.
	bumpSQL := "UPDATE " + table + " SET amount = amount + :delta WHERE id > :min_id"
	bumpParams := map[string]interface{}{"delta": 1, "min_id": 0}
	bumpArgs := map[string]interface{}{
		"action": "execute", "connection_id": connID,
		"sql": bumpSQL, "params": bumpParams, "dry_run": false, "max_affected_rows": 1,
	}
	bumpSQLFP, bumpParamsFP, err := MutationFingerprints(bumpArgs)
	if err != nil {
		t.Fatalf("fingerprints: %v", err)
	}
	bumpApproval, err := m.IssueApproval(ApprovalRequest{
		ProfileID: profile.ID, SQLFingerprint: bumpSQLFP, ParamsFingerprint: bumpParamsFP,
		OwnerID: "owner-it", SessionID: "session-it",
	})
	if err != nil {
		t.Fatalf("issue approval: %v", err)
	}
	rawBump := HandleTool(WithApprovalContext(ctx, bumpApproval), m, bumpArgs)
	if !strings.Contains(rawBump, "quota_exceeded") {
		t.Fatalf("expected quota_exceeded, got: %s", rawBump)
	}
	sum := query("SELECT SUM(amount) FROM "+table, nil, 1)
	if fmt.Sprint(sum.Rows[0][0]) != "640" { // 500+20+30+40+50
		t.Fatalf("over-limit mutation was not rolled back: %+v", sum.Rows)
	}

	// Guard rejections: SELECT INTO, multi-statement, WHERE-less UPDATE.
	for _, tc := range []struct{ action, sqlText string }{
		{"query", "SELECT id INTO " + table + "_copy FROM " + table},
		{"query", "SELECT id FROM " + table + "; SELECT 1"},
		{"execute", "UPDATE " + table + " SET amount = 0"},
	} {
		raw := HandleTool(ctx, m, map[string]interface{}{
			"action": tc.action, "connection_id": connID, "sql": tc.sqlText, "dry_run": true,
		})
		if strings.HasPrefix(strings.TrimSpace(raw), "{") {
			t.Fatalf("%s %q must be rejected, got: %s", tc.action, tc.sqlText, raw)
		}
	}

	// The committed write must have produced a metadata-only audit event:
	// approval/receipt identifiers and a fingerprint, never the SQL text.
	foundCommit := false
	for _, event := range auditEvents {
		encoded, _ := json.Marshal(event)
		if strings.Contains(string(encoded), table) || strings.Contains(string(encoded), "amount =") {
			t.Fatalf("audit event leaks SQL text: %s", encoded)
		}
		if event.Action == "execute" && event.ResultClass == "ok" && event.ReceiptID == commit.ReceiptID {
			foundCommit = true
			if event.ApprovalID == "" || event.SQLFingerprint == "" || event.AffectedRows != 1 {
				t.Fatalf("commit audit event incomplete: %+v", event)
			}
		}
	}
	if !foundCommit {
		t.Fatalf("no audit event recorded for committed execute; events: %+v", auditEvents)
	}
}
