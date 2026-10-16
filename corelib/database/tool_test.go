package database

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type toolTestAdapter struct {
	queryRequest   QueryRequest
	executeRequest ExecuteRequest
	batchRequest   BatchExecuteRequest
}

func (a *toolTestAdapter) Ping(context.Context) error { return nil }
func (a *toolTestAdapter) Inspect(context.Context, InspectRequest) (SchemaInfo, error) {
	return SchemaInfo{Dialect: "test"}, nil
}
func (a *toolTestAdapter) Query(_ context.Context, req QueryRequest) (QueryResult, error) {
	a.queryRequest = req
	return QueryResult{Columns: []Column{{Name: "ok"}}, Rows: [][]interface{}{{true}}, RowCount: 1}, nil
}
func (a *toolTestAdapter) Execute(_ context.Context, req ExecuteRequest) (MutationResult, error) {
	a.executeRequest = req
	return MutationResult{DryRun: req.DryRun, AffectedRows: 1}, nil
}
func (a *toolTestAdapter) ExecuteBatch(_ context.Context, req BatchExecuteRequest) (MutationResult, error) {
	a.batchRequest = req
	return MutationResult{ContractVersion: ContractVersion, DryRun: req.DryRun, DryRunGuarantee: "policy_only"}, nil
}
func (a *toolTestAdapter) Capabilities() Capabilities { return Capabilities{Read: true, Write: true} }
func (a *toolTestAdapter) Close() error               { return nil }

func TestToolParametersAndDescriptionAreStable(t *testing.T) {
	if !strings.Contains(ToolDescription(), "参数化 query") || !strings.Contains(ToolDescription(), "禁止用 bash") {
		t.Fatalf("ToolDescription lost safety guidance: %q", ToolDescription())
	}
	for _, want := range []string{"查看库", "查看数据库", "schema", "表结构", "Git 仓库"} {
		if !strings.Contains(ToolDescription(), want) {
			t.Fatalf("ToolDescription missing inspect lexicon %q: %q", want, ToolDescription())
		}
	}
	if !strings.Contains(ToolDescriptionReadOnly(), "查看库") || !strings.Contains(ToolDescriptionReadOnly(), "表结构") {
		t.Fatalf("read-only description missing inspect lexicon: %q", ToolDescriptionReadOnly())
	}
	params := ToolParameters()
	properties, ok := params["properties"].(map[string]interface{})
	if !ok || properties["action"] == nil || properties["connection_id"] == nil {
		t.Fatalf("shared database schema missing required properties: %#v", params)
	}
	if !reflect.DeepEqual(params["required"], []string{"action"}) {
		t.Fatalf("required action schema = %#v", params["required"])
	}
	action, _ := params["properties"].(map[string]interface{})
	if _, exposed := action["approval_token"]; exposed {
		t.Fatal("approval_token must not be exposed in the model-facing schema")
	}
	actionSchema, _ := action["action"].(map[string]interface{})
	if len(actionSchema["enum"].([]string)) != 16 {
		t.Fatalf("action enum must cover all contract actions: %#v", actionSchema)
	}
	if _, ok := properties["host"]; !ok {
		t.Fatal("host must be in the shared schema for conversational profile matching")
	}
	if _, ok := properties["async"]; !ok {
		t.Fatal("async must be in the shared schema")
	}
	if _, ok := properties["job_id"]; !ok {
		t.Fatal("job_id must be in the shared schema")
	}
	if _, ok := properties["result_handle"]; !ok {
		t.Fatal("result_handle must be in the shared schema")
	}
	if first, second := ToolSchemaHash(), ToolSchemaHash(); first == "" || first != second {
		t.Fatalf("schema hash is not deterministic: %q %q", first, second)
	}
	if _, ok := properties["parameter_mode"]; !ok {
		t.Fatal("parameter_mode must be in the shared schema")
	}
}

func TestToolSchemaHashMatchesCanonical(t *testing.T) {
	got := ToolSchemaHash()
	if CanonicalToolSchemaHash == "sha256:pending-schema-lock" {
		t.Fatalf("set CanonicalToolSchemaHash to %s", got)
	}
	if got != CanonicalToolSchemaHash {
		t.Fatalf("schema hash drifted: got %s want %s", got, CanonicalToolSchemaHash)
	}
}

func TestManagerProfilesAreDeterministicAndRedacted(t *testing.T) {
	manager := NewManager([]Profile{
		{ID: "z", Name: "last", SecretRef: "keyring://z/value", DSN: "server=x;password=secret"},
		{ID: "a", Name: "first", SecretRef: "keyring://a/value"},
	}, nil)
	profiles := manager.Profiles()
	if len(profiles) != 2 || profiles[0].ID != "a" || profiles[1].ID != "z" {
		t.Fatalf("profiles order = %#v", profiles)
	}
	for _, profile := range profiles {
		if profile.SecretRef != "" || profile.DSN != "" {
			t.Fatalf("profile leaked secret material: %#v", profile)
		}
	}
	listed, _ := json.Marshal(manager.ProfileSummaries())
	if strings.Contains(string(listed), "password") || strings.Contains(string(listed), "server=") {
		t.Fatalf("profile summary leaked connection details: %s", listed)
	}
}

func TestUpdateProfilesInvalidatesChangedConnections(t *testing.T) {
	m := NewManager([]Profile{{ID: "p", Type: SourceExcel, FilePath: "one.xlsx"}}, nil)
	m.items["conn"] = &toolTestAdapter{}
	m.profileIDs["conn"] = "p"
	m.lastUsed["conn"] = time.Now()
	m.UpdateProfiles([]Profile{{ID: "p", Type: SourceExcel, FilePath: "two.xlsx"}})
	if _, ok := m.Adapter("conn"); ok {
		t.Fatal("profile update kept a connection created with stale configuration")
	}
	if got := m.ProfileIDForConnection("conn"); got != "" {
		t.Fatalf("stale connection profile binding remained: %q", got)
	}
}

func TestHandleToolUsesSharedDefaultsAndRejectsCredentials(t *testing.T) {
	manager := NewManager(nil, nil)
	adapter := &toolTestAdapter{}
	manager.items["conn"] = adapter

	if got := HandleTool(nil, manager, map[string]interface{}{"action": "list_connections"}); !strings.Contains(got, `"items":[]`) && !strings.Contains(got, `"count":0`) {
		t.Fatalf("list_connections = %q", got)
	}
	query := HandleTool(context.Background(), manager, map[string]interface{}{
		"action": "query", "connection_id": "conn", "sql": "select 1",
	})
	if !strings.Contains(query, `"row_count":1`) {
		t.Fatalf("query result = %q", query)
	}
	if adapter.queryRequest.Limit != 100 || adapter.queryRequest.Timeout != 30 {
		t.Fatalf("query defaults = %#v", adapter.queryRequest)
	}
	execute := HandleTool(context.Background(), manager, map[string]interface{}{
		"action": "execute", "connection_id": "conn", "sql": "update t set x=:x", "dry_run": true,
	})
	if !strings.Contains(execute, `"dry_run":true`) || !adapter.executeRequest.DryRun {
		t.Fatalf("execute result/request = %q %#v", execute, adapter.executeRequest)
	}
	batch := HandleTool(context.Background(), manager, map[string]interface{}{
		"action": "batch_execute", "connection_id": "conn", "statements": []interface{}{
			map[string]interface{}{"sql": "update t set x=:x", "params": map[string]interface{}{"x": 1}},
			map[string]interface{}{"sql": "delete from t where id=:id", "params": map[string]interface{}{"id": 2}},
		},
	})
	if !strings.Contains(batch, `"dry_run":true`) || len(adapter.batchRequest.Statements) != 2 || !adapter.batchRequest.DryRun {
		t.Fatalf("batch result/request = %q %#v", batch, adapter.batchRequest)
	}
	if got := HandleTool(context.Background(), manager, map[string]interface{}{
		"action": "execute", "connection_id": "conn", "sql": "update t set x=1", "dry_run": false, "approval_token": "model-forged",
	}); !strings.Contains(got, "approval_token must be supplied by the trusted host approval context") {
		t.Fatalf("model-supplied approval token was accepted: %q", got)
	}
	if got := HandleTool(context.Background(), manager, map[string]interface{}{
		"action": "execute", "connection_id": "conn", "sql": "update t set x=1", "dry_run": false, "approval_token": 123,
	}); !strings.Contains(got, "approval_token must be supplied by the trusted host approval context") {
		t.Fatalf("non-string model-supplied approval token was accepted: %q", got)
	}
	if got := HandleTool(context.Background(), manager, map[string]interface{}{
		"action": "execute", "connection_id": "conn", "sql": "update t set x=1", "dry_run": false, "Approval_Token": "model-forged",
	}); !strings.Contains(got, "approval_token must be supplied by the trusted host approval context") {
		t.Fatalf("case-variant model-supplied approval token was accepted: %q", got)
	}
	approved := WithApprovalContext(context.Background(), ApprovalContext{Token: "trusted-token", ID: "approval-42"})
	committed := HandleTool(approved, manager, map[string]interface{}{
		"action": "execute", "connection_id": "conn", "sql": "update t set x=1", "dry_run": false,
	})
	if !strings.Contains(committed, `"dry_run":false`) || adapter.executeRequest.ApprovalToken != "trusted-token" {
		t.Fatalf("trusted approval was not injected: result=%q request=%#v", committed, adapter.executeRequest)
	}
	if got := HandleTool(context.Background(), manager, map[string]interface{}{
		"action": "connect", "profile_id": "p", "password": "secret",
	}); !strings.HasPrefix(got, "Error: database arguments must not include credentials") {
		t.Fatalf("credential rejection = %q", got)
	}
}

func TestExecuteExpectedAffectedRowsIsWarningOnly(t *testing.T) {
	m := NewManager(nil, nil)
	m.items["conn"] = &toolTestAdapter{}
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "execute", "connection_id": "conn", "sql": "update t set x=1 where id=1",
		"dry_run": true, "expected_affected_rows": 99,
	})
	if !strings.Contains(got, "expected_affected_rows_mismatch") {
		t.Fatalf("expected mismatch warning, got %q", got)
	}
}

func TestMutationAuditContainsApprovalIDButNeverToken(t *testing.T) {
	manager := NewManager(nil, nil)
	adapter := &toolTestAdapter{}
	manager.items["conn"] = adapter
	var events []AuditEvent
	manager.SetAuditSink(func(_ context.Context, event AuditEvent) { events = append(events, event) })
	ctx := WithApprovalContext(context.Background(), ApprovalContext{Token: "trusted-token", ID: "approval-42"})
	if got := HandleTool(ctx, manager, map[string]interface{}{
		"action": "execute", "connection_id": "conn", "sql": "update t set x=:x", "params": map[string]interface{}{"x": 1}, "dry_run": false,
	}); !strings.Contains(got, `"dry_run":false`) || !strings.Contains(got, `"receipt_id":"db-receipt-`) {
		t.Fatalf("execute failed: %q", got)
	}
	batchCtx := WithApprovalContext(context.Background(), ApprovalContext{Token: "trusted-token-2", ID: "approval-43"})
	if got := HandleTool(batchCtx, manager, map[string]interface{}{
		"action": "batch_execute", "connection_id": "conn", "statements": []interface{}{
			map[string]interface{}{"sql": "update t set x=:x", "params": map[string]interface{}{"x": 1}},
			map[string]interface{}{"sql": "delete from t where id=:id", "params": map[string]interface{}{"id": 2}},
		}, "dry_run": false,
	}); !strings.Contains(got, `"dry_run":false`) {
		t.Fatalf("batch_execute failed: %q", got)
	}
	if len(events) != 2 {
		t.Fatalf("audit event count = %d, want 2", len(events))
	}
	for _, event := range events {
		if event.ApprovalID != "approval-42" && event.ApprovalID != "approval-43" {
			t.Fatalf("approval id missing from audit event: %#v", event)
		}
		if event.SQLFingerprint == "" || event.ParameterCount == 0 {
			t.Fatalf("mutation audit lacks metadata: %#v", event)
		}
		if event.ReceiptID == "" {
			t.Fatalf("mutation audit lacks receipt id: %#v", event)
		}
		payload, _ := json.Marshal(event)
		if strings.Contains(string(payload), "trusted-token") {
			t.Fatalf("approval token leaked into audit event: %s", payload)
		}
	}
}

func TestBatchApprovalFingerprintUsesStableCanonicalForm(t *testing.T) {
	manager := NewManager(nil, nil)
	adapter := &toolTestAdapter{}
	manager.items["conn"] = adapter
	statements := []BatchStatement{
		{SQL: "update t set x=:x", Params: map[string]interface{}{"x": 1}},
		{SQL: "delete from t where id=:id", Params: map[string]interface{}{"id": 2}},
	}
	args := map[string]interface{}{
		"action": "batch_execute", "connection_id": "conn", "dry_run": false,
		"statements": []interface{}{
			map[string]interface{}{"sql": statements[0].SQL, "params": statements[0].Params},
			map[string]interface{}{"sql": statements[1].SQL, "params": statements[1].Params},
		},
	}
	ctx := WithApprovalContext(context.Background(), ApprovalContext{
		Token:          "trusted-token",
		SQLFingerprint: batchSQLFingerprint(statements),
	})
	if got := HandleTool(ctx, manager, args); !strings.Contains(got, `"dry_run":false`) {
		t.Fatalf("batch approval fingerprint rejected: %q", got)
	}
	mismatch := WithApprovalContext(context.Background(), ApprovalContext{Token: "trusted-token", SQLFingerprint: sqlFingerprint("different")})
	if got := HandleTool(mismatch, manager, args); !strings.Contains(got, "does not match SQL fingerprint") {
		t.Fatalf("mismatched batch approval was accepted: %q", got)
	}
}

func TestRequestScopeOverridesModelIdentityFields(t *testing.T) {
	manager := NewManager(nil, nil)
	adapter := &toolTestAdapter{}
	manager.items["conn"] = adapter
	manager.bindings["conn"] = connectionBinding{ownerID: "trusted-owner", sessionID: "trusted-session"}
	manager.lastUsed["conn"] = time.Now()
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "trusted-owner", SessionID: "trusted-session"})
	got := HandleTool(ctx, manager, map[string]interface{}{
		"action": "query", "connection_id": "conn", "sql": "select 1",
		"_owner_id": "model-owner", "_session_id": "model-session",
	})
	if !strings.Contains(got, `"row_count":1`) {
		t.Fatalf("model identity fields overrode trusted request scope: %q", got)
	}
}

func TestHandleToolRejectsModelIdentityWithoutTrustedScope(t *testing.T) {
	m := NewManager(nil, nil)
	m.items["conn"] = &toolTestAdapter{}
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "query", "connection_id": "conn", "sql": "select 1", "_owner_id": "forged",
	})
	if !strings.Contains(got, "_owner_id must be supplied by the trusted host request scope") {
		t.Fatalf("model identity was accepted without trusted scope: %q", got)
	}
}

func TestHandleToolPreservesStableErrors(t *testing.T) {
	if got := HandleTool(context.Background(), nil, nil); got != "数据库连接工具未初始化。请先配置数据源 profile。" {
		t.Fatalf("nil manager = %q", got)
	}
	manager := NewManager(nil, nil)
	manager.items["conn"] = &errorToolAdapter{}
	got := HandleTool(context.Background(), manager, map[string]interface{}{"action": "query", "connection_id": "conn", "sql": "select 1"})
	if !strings.Contains(got, "database query failed: unavailable") {
		t.Fatalf("adapter error = %q", got)
	}
}

func TestProfileOperationAllowlistCoversExcelMutations(t *testing.T) {
	m := NewManager([]Profile{{ID: "p", WriteEnabled: true, AllowedOperations: []string{"inspect"}}}, nil)
	if m.operationAllowed("p", "write_table") || m.operationAllowed("p", "export_excel") {
		t.Fatal("Excel mutation actions bypassed the profile operation allowlist")
	}
	m = NewManager([]Profile{{ID: "p", AllowedOperations: []string{"write_table", "export_excel"}}}, nil)
	if m.operationAllowed("p", "write_table") || m.operationAllowed("p", "export_excel") {
		t.Fatal("operation allowlist must not override read-only/write opt-in")
	}
}

func TestHandleToolEnforcesExcelProfileAllowlistForWrite(t *testing.T) {
	m := NewManager([]Profile{{ID: "p", Type: SourceExcel, WriteEnabled: true, AllowedOperations: []string{"inspect"}}}, nil)
	m.items["excel-conn"] = &toolTestAdapter{}
	m.profileIDs["excel-conn"] = "p"
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "write_table", "connection_id": "excel-conn", "file_path": "out.xlsx",
		"sheet": "Sheet1", "range": "A1", "rows": []interface{}{[]interface{}{"x"}}, "dry_run": true,
	})
	if !strings.Contains(got, "operation is not allowed by profile") {
		t.Fatalf("Excel write bypassed profile allowlist: %q", got)
	}
}

func TestValidateMutationSQL(t *testing.T) {
	for _, tc := range []struct {
		sql     string
		wantErr bool
	}{
		{"update orders set state=:state where id=:id", false},
		{"select 1", true},
		{"update t set x=1; delete from t", true},
		{"exec dangerous_proc", true},
	} {
		if err := validateMutationSQL(tc.sql); (err != nil) != tc.wantErr {
			t.Fatalf("sql=%q err=%v", tc.sql, err)
		}
	}
}

func TestValidateReadSQLFailsClosed(t *testing.T) {
	for _, sqlText := range []string{"update t set x=1", "select 1; select 2", "select 1 -- comment"} {
		if err := validateReadSQL(sqlText); err == nil {
			t.Fatalf("expected read SQL rejection for %q", sqlText)
		}
	}
	if err := validateReadSQL("WITH c AS (SELECT 1) SELECT * FROM c"); err != nil {
		t.Fatalf("expected CTE read SQL to pass: %v", err)
	}
}

func TestManagerExpiresIdleConnections(t *testing.T) {
	m := NewManager(nil, nil)
	m.SetIdleTTL(time.Millisecond)
	a := &toolTestAdapter{}
	m.items["idle"] = a
	m.lastUsed["idle"] = time.Now().Add(-time.Second)
	if _, ok := m.Adapter("idle"); ok {
		t.Fatal("expected idle connection to expire")
	}
}

func TestManagerConnectionBindingRejectsCrossSessionAccess(t *testing.T) {
	m := NewManager(nil, nil)
	a := &toolTestAdapter{}
	id := "bound"
	m.items[id] = a
	m.lastUsed[id] = time.Now()
	m.bindings[id] = connectionBinding{ownerID: "owner-a", sessionID: "session-a"}
	if _, ok := m.AdapterFor(id, "owner-b", "session-b"); ok {
		t.Fatal("expected cross-session connection access to be rejected")
	}
	if _, ok := m.AdapterFor(id, "owner-a", "session-a"); !ok {
		t.Fatal("expected owner/session-bound access to succeed")
	}
}

func TestProfileOperationAllowlist(t *testing.T) {
	m := NewManager([]Profile{{ID: "p", AllowedOperations: []string{"inspect", "query"}}}, nil)
	if !m.operationAllowed("p", "query") || m.operationAllowed("p", "execute") {
		t.Fatal("profile operation allowlist not enforced")
	}
}

func TestProfileTableAllowlistAcceptsDialectQuotedIdentifiers(t *testing.T) {
	p := Profile{AllowedTables: []string{"orders"}, AllowedSchemas: []string{"dbo"}}
	if err := p.validateSQLTables("SELECT * FROM [dbo].[orders]"); err != nil {
		t.Fatalf("SQL Server quoted table rejected: %v", err)
	}
	if err := p.validateSQLTables("SELECT * FROM [dbo].[other]"); err == nil {
		t.Fatal("disallowed quoted table accepted")
	}
	if err := p.validateSQLTables("SELECT * FROM orders"); err == nil {
		t.Fatal("unqualified table bypassed schema allowlist")
	}
	p.DefaultSchema = "dbo"
	if err := p.validateSQLTables("SELECT * FROM orders"); err != nil {
		t.Fatalf("default-schema unqualified table rejected: %v", err)
	}
	if err := (Profile{AllowedTables: []string{"orders"}}).validateSQLTables("WITH recent AS (SELECT * FROM orders) SELECT * FROM recent"); err != nil {
		t.Fatalf("CTE alias should not be treated as an external table: %v", err)
	}
	if err := (Profile{AllowedTables: []string{"orders"}}).validateSQLTables("SELECT * FROM orders, secrets"); err == nil {
		t.Fatal("comma-separated disallowed table accepted")
	}
	if got := normalizeTableReference("[Order Details]"); got != "Order Details" || !safeTableName(got) {
		t.Fatalf("Access quoted table name was not preserved: %q", got)
	}
	if err := (Profile{AllowedTables: []string{"Order Details"}}).validateSQLTables(`SELECT * FROM "Order Details"`); err != nil {
		t.Fatalf("quoted Access table rejected by allowlist: %v", err)
	}
}

func TestValidateProfileRequiresSafeConnectionFields(t *testing.T) {
	if err := ValidateProfile(Profile{ID: "p", Type: SourcePostgres}); err == nil {
		t.Fatal("expected missing host/database rejection")
	}
	if err := ValidateProfile(Profile{ID: "p", Type: SourceExcel, FilePath: "x.xlsx"}); err != nil {
		t.Fatalf("valid Excel profile rejected: %v", err)
	}
	if got := (Profile{ID: "p", Type: SourcePostgres, Host: "db", Database: "x"}).WriteEnabled; got {
		t.Fatal("write must be explicit opt-in")
	}
}

func TestHandleToolEmitsMetadataOnlyAudit(t *testing.T) {
	m := NewManager(nil, nil)
	a := &toolTestAdapter{}
	m.items["conn"] = a
	m.lastUsed["conn"] = time.Now()
	var got AuditEvent
	m.SetAuditSink(func(_ context.Context, event AuditEvent) { got = event })
	result := HandleTool(WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"}), m, map[string]interface{}{
		"action": "query", "connection_id": "conn", "sql": "select :secret", "params": map[string]interface{}{"secret": "do-not-log"},
		"_owner_id": "owner", "_session_id": "session",
	})
	if !strings.Contains(result, "row_count") || got.Action != "query" || got.ParameterCount != 1 || got.SQLFingerprint == "" {
		t.Fatalf("result=%q audit=%#v", result, got)
	}
	if got.OwnerID != "owner" || got.SessionID != "session" {
		t.Fatalf("audit binding=%#v", got)
	}
}

func TestProfileColumnMasking(t *testing.T) {
	p := Profile{DeniedColumns: []string{"secret"}, MaskedColumns: []string{"email"}}
	if !p.columnDenied("SECRET") || !p.columnMasked("Email") {
		t.Fatal("column policies should be case-insensitive")
	}
	if got := maskValue("alice@example.com"); got != "***.com" {
		t.Fatalf("masked value=%v", got)
	}
}

func TestApprovalContextBindsFingerprintAndExpiry(t *testing.T) {
	sqlText := "update t set a=:a where id=:id"
	ctx := ApprovalContext{Token: "t", SQLFingerprint: sqlFingerprint(sqlText), ProfileID: "p", SchemaVersion: 2, ExpiresAt: time.Now().Add(time.Minute)}
	if err := validateApprovalContext(ctx, sqlText, "p", 2); err != nil {
		t.Fatalf("valid approval rejected: %v", err)
	}
	if err := validateApprovalContext(ctx, sqlText, "p", 3); err == nil {
		t.Fatal("profile schema version mismatch accepted")
	}
	ctx.ExpiresAt = time.Now().Add(-time.Second)
	if err := validateApprovalContext(ctx, sqlText, "p"); err == nil {
		t.Fatal("expired approval accepted")
	}
	ctx.ExpiresAt = time.Time{}
	ctx.SQLFingerprint = "different"
	if err := validateApprovalContext(ctx, sqlText, "p"); err == nil {
		t.Fatal("fingerprint mismatch accepted")
	}
	payload, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "{}" {
		t.Fatalf("approval context leaked into JSON: %s", payload)
	}
	ctx.ParamsFingerprint = paramsFingerprint(map[string]interface{}{"id": 1})
	if err := validateApprovalContextWithParams(ctx, sqlText, "p", map[string]interface{}{"id": 2}, 0); err == nil {
		t.Fatal("parameter fingerprint mismatch accepted")
	}
}

func TestManagerWorkspaceRootResolvesOnlyInsideRoot(t *testing.T) {
	m := NewManager(nil, nil)
	m.SetWorkspaceRoot(t.TempDir())
	if _, err := m.resolvePath("../escape.xlsx", false); err == nil {
		t.Fatal("workspace escape was accepted")
	}
	if got, err := m.resolvePath("reports/out.xlsx", false); err != nil || !strings.Contains(got, "reports") {
		t.Fatalf("workspace relative path failed: %q %v", got, err)
	}
}

func TestResultCursorIsOwnerBoundAndExpires(t *testing.T) {
	m := NewManager(nil, nil)
	r := QueryResult{ContractVersion: ContractVersion, Columns: []Column{{Name: "n"}}, Rows: [][]interface{}{{1}}, allRows: [][]interface{}{{1}, {2}, {3}}}
	token := m.storeResult(r, "owner", "session")
	page, err := m.readResultPage(token, "owner", "session", 1)
	if err != nil || page.RowCount != 1 || page.NextCursor == "" {
		t.Fatalf("first page=%#v err=%v", page, err)
	}
	if _, err := m.readResultPage(token, "other", "session", 1); err == nil {
		t.Fatal("cross-owner cursor access accepted")
	}
	page, err = m.readResultPage(token, "owner", "session", 5)
	if err != nil || page.RowCount != 2 || page.NextCursor != "" {
		t.Fatalf("second page=%#v err=%v", page, err)
	}
	if _, err := m.readResultPage(token, "owner", "session", 1); err == nil {
		t.Fatal("consumed cursor remained usable")
	}
}

func TestResultCursorIsInvalidatedWhenConnectionCloses(t *testing.T) {
	m := NewManager(nil, nil)
	a := &toolTestAdapter{}
	m.items["conn"] = a
	m.bindings["conn"] = connectionBinding{ownerID: "owner", sessionID: "session"}
	m.profileIDs["conn"] = "profile"
	r := QueryResult{ProfileID: "profile", Columns: []Column{{Name: "n"}}, Rows: [][]interface{}{{1}}, allRows: [][]interface{}{{1}, {2}}}
	token := m.storeResultForConnection(r, "owner", "session", "conn", 1)
	if token == "" {
		t.Fatal("expected result cursor")
	}
	if err := m.DisconnectFor("conn", "owner", "session"); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := m.readResultPageFor(token, "owner", "session", "conn", 1); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("closed connection cursor remained usable: %v", err)
	}
}

func TestResultCursorIsInvalidatedWhenProfileSchemaChanges(t *testing.T) {
	m := NewManager([]Profile{{ID: "profile", SchemaVersion: 1}}, nil)
	a := &toolTestAdapter{}
	m.items["conn"] = a
	m.bindings["conn"] = connectionBinding{ownerID: "owner", sessionID: "session"}
	m.profileIDs["conn"] = "profile"
	r := QueryResult{ProfileID: "profile", Columns: []Column{{Name: "n"}}, Rows: [][]interface{}{{1}}, allRows: [][]interface{}{{1}, {2}}}
	token := m.storeResultForConnection(r, "owner", "session", "conn", 1)
	m.profiles["profile"] = Profile{ID: "profile", SchemaVersion: 2}
	if _, err := m.readResultPageFor(token, "owner", "session", "conn", 1); err == nil || !strings.Contains(err.Error(), "schema change") {
		t.Fatalf("schema change cursor remained usable: %v", err)
	}
}

func TestQueryCursorDoesNotRequireLiveConnection(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	m.SetResultStoreDir(t.TempDir())
	token := m.storeResultAtWithConnection(QueryResult{
		Columns: []Column{{Name: "n"}},
		allRows: [][]interface{}{{1}, {2}, {3}},
	}, "owner", "session", "conn-gone", 0)
	if token == "" {
		t.Fatal("missing handle")
	}
	m.mu.Lock()
	delete(m.results, token)
	m.mu.Unlock()
	got := HandleTool(WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"}), m, map[string]interface{}{
		"action": "query", "cursor": token, "limit": 2,
	})
	if strings.Contains(got, "缺少 connection_id") {
		t.Fatalf("cursor-only paging = %s", got)
	}
	page := mustToolResult[QueryResult](t, got)
	if page.RowCount != 2 || page.NextCursor == "" {
		t.Fatalf("cursor-only page = %+v", page)
	}
}

func TestCursorHonorsProfileOperationAllowlist(t *testing.T) {
	m := NewManager([]Profile{{ID: "profile", AllowedOperations: []string{"inspect"}}}, nil)
	m.items["conn"] = &toolTestAdapter{}
	m.bindings["conn"] = connectionBinding{ownerID: "owner", sessionID: "session"}
	m.profileIDs["conn"] = "profile"
	r := QueryResult{ProfileID: "profile", Columns: []Column{{Name: "n"}}, Rows: [][]interface{}{{1}}, allRows: [][]interface{}{{1}, {2}}}
	token := m.storeResultForConnection(r, "owner", "session", "conn", 1)
	got := HandleTool(WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"}), m, map[string]interface{}{
		"action": "query", "connection_id": "conn", "cursor": token,
	})
	if !strings.Contains(got, "operation is not allowed by profile") {
		t.Fatalf("cursor bypassed operation allowlist: %q", got)
	}
}

func TestManagerCloseIsTerminalAndFailClosed(t *testing.T) {
	m := NewManager(nil, nil)
	adapter := &toolTestAdapter{}
	m.items["conn"] = adapter
	m.lastUsed["conn"] = time.Now()
	m.Close()
	// Close is idempotent and establishes a terminal ingress boundary. A
	// second close must not panic or attempt to close already detached adapters.
	m.Close()
	if !m.IsClosed() {
		t.Fatal("manager did not enter closed state")
	}
	if _, ok := m.Adapter("conn"); ok {
		t.Fatal("closed manager exposed a detached connection")
	}
	if _, err := m.resolvePath("file.xlsx", false); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("resolvePath after close = %v, want ErrManagerClosed", err)
	}
	if got := HandleTool(context.Background(), m, map[string]interface{}{"action": "list_connections"}); !strings.Contains(got, "已关闭") {
		t.Fatalf("closed manager tool response = %q", got)
	}
}

type errorToolAdapter struct{ toolTestAdapter }

func (*errorToolAdapter) Query(context.Context, QueryRequest) (QueryResult, error) {
	return QueryResult{}, errors.New("unavailable")
}
