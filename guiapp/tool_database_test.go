package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/database"
	"github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

func TestBuiltinDatabaseToolRegistered(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})
	tool, ok := r.Get("database")
	if !ok || tool == nil {
		t.Fatal("database tool not registered")
	}
	if !strings.Contains(tool.Description, "MySQL") || !strings.Contains(tool.Description, "dry-run") {
		t.Fatalf("description=%q", tool.Description)
	}
	if !strings.Contains(tool.Description, "查看库") || !strings.Contains(tool.Description, "表结构") {
		t.Fatalf("description missing inspect lexicon: %q", tool.Description)
	}
	if _, ok := tool.InputSchema["action"]; !ok {
		t.Fatalf("schema missing action: %#v", tool.InputSchema)
	}
	if tool.HandlerCtx == nil {
		t.Fatal("database tool must use the context-aware handler so host approvals reach corelib/database")
	}
}

func TestPublishDatabaseProfileSearchText(t *testing.T) {
	manager := database.NewManager([]database.Profile{{
		ID: "mysql-192-168-1-242", Name: "legacy", Type: database.SourceMySQL,
		Host: "192.168.1.242", Database: "mysql", DefaultSchema: "rapidbi",
	}}, nil)
	router := NewToolRouter(nil)
	publishDatabaseProfileSearchText(router, manager)
	if got := router.SearchTextExtra("database"); !strings.Contains(strings.Join(got, " "), "rapidbi") {
		t.Fatalf("database extra tokens = %#v", got)
	}
	if got := router.SearchTextExtra("database_query"); !strings.Contains(strings.Join(got, " "), "192.168.1.242") {
		t.Fatalf("database_query extra tokens = %#v", got)
	}
}

func TestDatabaseApprovalPanelInjectsContextForSpreadsheetMutation(t *testing.T) {
	root := t.TempDir()
	manager := database.NewManager(nil, nil)
	manager.SetWorkspaceRoot(root)
	h := &IMMessageHandler{registry: NewToolRegistry(), databaseManager: manager}
	registerBuiltinTools(h.registry, h)
	approval := storeRegisteredToolPendingApproval("database", map[string]interface{}{
		"action": "write_table", "file_path": "approved.xlsx", "sheet": "Sheet1",
		"rows": []interface{}{[]interface{}{"name"}, []interface{}{"approved"}}, "dry_run": false,
	}, "sess", "owner", security.RiskAssessment{Level: security.RiskHigh})
	defer deleteRegisteredToolPendingApproval(approval.ID)
	resp := h.handleRegisteredToolApprovalAgentViewSubmit(map[string]interface{}{
		"approved":   true,
		"parameters": map[string]interface{}{registeredToolApprovalIDField: approval.ID},
	})
	if resp == nil || resp.Error != "" {
		t.Fatalf("database approval execution failed: %#v", resp)
	}
	if _, err := os.Stat(filepath.Join(root, "approved.xlsx")); err != nil {
		t.Fatalf("approved spreadsheet was not created: %v", err)
	}
}

// TestDatabaseApprovalPanelStrictIssuance exercises the strict path: the
// approval panel mints a fingerprint-bound one-time token through
// Manager.IssueApproval, the commit consumes it, and the pending mutation is
// destroyed.
func TestDatabaseApprovalPanelStrictIssuance(t *testing.T) {
	root := t.TempDir()
	manager := database.NewManager(nil, nil)
	manager.SetWorkspaceRoot(root)
	pendingStore, err := database.NewPendingStore(filepath.Join(root, "database_pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager.SetPendingStore(pendingStore)
	h := &IMMessageHandler{registry: NewToolRegistry(), databaseManager: manager}
	registerBuiltinTools(h.registry, h)
	approval := storeRegisteredToolPendingApproval("database", map[string]interface{}{
		"action": "write_table", "file_path": "strict.xlsx", "sheet": "Sheet1",
		"rows": []interface{}{[]interface{}{"name"}, []interface{}{"strict"}}, "dry_run": false,
	}, "sess", "owner", security.RiskAssessment{Level: security.RiskHigh})
	defer deleteRegisteredToolPendingApproval(approval.ID)
	resp := h.handleRegisteredToolApprovalAgentViewSubmit(map[string]interface{}{
		"approved":   true,
		"parameters": map[string]interface{}{registeredToolApprovalIDField: approval.ID},
	})
	if resp == nil || resp.Error != "" {
		t.Fatalf("strict database approval execution failed: %#v", resp)
	}
	if _, err := os.Stat(filepath.Join(root, "strict.xlsx")); err != nil {
		t.Fatalf("approved spreadsheet was not created: %v", err)
	}
	if pendingStore.Len() != 0 {
		t.Fatal("issued approval was not consumed after commit")
	}
}

func newIsolatedDatabaseToolHandler(t *testing.T) (*App, *IMMessageHandler) {
	t.Helper()
	app := newDatabaseProfileTestApp(t)
	return app, &IMMessageHandler{app: app, databaseManager: database.NewManager(nil, nil)}
}

func TestToolDatabaseProposeProfileEmitsPasswordForm(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	got := h.toolDatabaseWithContext(context.Background(), map[string]interface{}{
		"action": "propose_profile", "host": "192.168.1.242", "username": "root", "name": "lab mysql",
	})
	if !strings.Contains(got, `"needs_secret":true`) {
		t.Fatalf("propose_profile result = %s", got)
	}
	if strings.Contains(got, `"password"`) {
		t.Fatalf("propose_profile leaked a password field: %s", got)
	}
	if _, ok := app.agentViewOpenRecord("tool:run:database"); !ok {
		t.Fatal("propose_profile did not open the task-panel password form")
	}
}

func TestHandleRegisteredToolProposeProfileSubmitIgnoresNilPassword(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	_ = installFakeDatabaseKeyring(t)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), databaseManager: database.NewManager(nil, nil)}
	registerBuiltinTools(h.registry, h)
	resp := h.handleRegisteredToolAgentViewSubmit("database", map[string]interface{}{
		registeredToolAgentViewArgsField: map[string]interface{}{
			"action":   "propose_profile",
			"id":       "mysql-192-168-1-242",
			"type":     "mysql",
			"host":     "192.168.1.242",
			"database": "mysql",
		},
	})
	if resp == nil || resp.Error != "needs_secret" {
		t.Fatalf("missing password must re-open the form, got %#v", resp)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.DatabaseProfiles) != 0 {
		t.Fatalf("nil password saved a profile: %#v", cfg.DatabaseProfiles)
	}
}

func TestHandleRegisteredToolProposeProfileSubmitSavesSecret(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	store := installFakeDatabaseKeyring(t)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), databaseManager: database.NewManager(nil, databaseKeyringSecret)}
	registerBuiltinTools(h.registry, h)
	resp := h.handleRegisteredToolAgentViewSubmit("database", map[string]interface{}{
		registeredToolAgentViewArgsField: map[string]interface{}{
			"action":   "propose_profile",
			"id":       "closed-port",
			"name":     "closed port",
			"type":     "mysql",
			"host":     "127.0.0.1",
			"username": "root",
		},
		"port":     1,
		"database": "mysql",
		"password": "form-secret",
	})
	if resp == nil || resp.Error != "" {
		t.Fatalf("submit failed: %#v", resp)
	}
	if store["maclaw/database/closed-port"] != "form-secret" {
		t.Fatalf("keyring = %#v", store)
	}
	if !strings.Contains(resp.Text, "数据源已保存") && !strings.Contains(resp.Text, "Data source saved") {
		t.Fatalf("save text = %q", resp.Text)
	}
	if strings.Contains(resp.Text, `"needs_secret":true`) {
		t.Fatalf("tcp failure re-asked for a password: %s", resp.Text)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.DatabaseProfiles) != 1 || cfg.DatabaseProfiles[0].Port != 1 {
		t.Fatalf("saved profiles = %#v", cfg.DatabaseProfiles)
	}
}

func TestToolDatabaseConnectMissingProfileEmitsPasswordForm(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	got := h.toolDatabaseWithContext(context.Background(), map[string]interface{}{
		"action": "connect", "host": "192.168.1.242", "port": 3306, "username": "root",
	})
	if !strings.Contains(got, "profile_not_found") {
		t.Fatalf("connect result = %s", got)
	}
	if _, ok := app.agentViewOpenRecord("tool:run:database"); !ok {
		t.Fatal("connect profile_not_found did not open the password form")
	}
}

func TestToolDatabaseListConnectionsWithHostEmitsPasswordForm(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	got := h.toolDatabaseWithContext(context.Background(), map[string]interface{}{
		"action": "list_connections", "host": "192.168.1.242",
	})
	if !strings.Contains(got, `"items"`) {
		t.Fatalf("list_connections result = %s", got)
	}
	if _, ok := app.agentViewOpenRecord("tool:run:database"); !ok {
		t.Fatal("empty host-filtered list_connections did not open the password form")
	}
}

func TestToolDatabaseConnectAuthFailureEmitsPasswordForm(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	if err := app.DatabaseProfileSave(map[string]interface{}{
		"id":         "mysql-192-168-1-242",
		"name":       "192.168.1.242",
		"type":       "mysql",
		"host":       "192.168.1.242",
		"port":       3306,
		"database":   "mysql",
		"username":   "root",
		"secret_ref": "keyring://maclaw/database/mysql-192-168-1-242",
	}); err != nil {
		t.Fatal(err)
	}
	got := h.toolDatabaseWithContext(context.Background(), map[string]interface{}{
		"action": "connect", "profile_id": "mysql-192-168-1-242",
	})
	if !strings.Contains(got, `"needs_secret":true`) {
		t.Fatalf("auth failure = %s", got)
	}
	if _, ok := app.agentViewOpenRecord("tool:run:database"); !ok {
		t.Fatal("authentication failure did not open the password form")
	}
}

func TestToolDatabaseConnectAmbiguousDoesNotEmitPasswordForm(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	for _, payload := range []map[string]interface{}{
		{"id": "a", "type": "mysql", "host": "192.168.1.242", "database": "a", "username": "root"},
		{"id": "b", "type": "mysql", "host": "192.168.1.242", "database": "b", "username": "root"},
	} {
		if err := app.DatabaseProfileSave(payload); err != nil {
			t.Fatal(err)
		}
	}
	got := h.toolDatabaseWithContext(context.Background(), map[string]interface{}{
		"action": "connect", "host": "192.168.1.242",
	})
	if !strings.Contains(got, "ambiguous") {
		t.Fatalf("connect result = %s", got)
	}
	if _, ok := app.agentViewOpenRecord("tool:run:database"); ok {
		t.Fatal("ambiguous host must not open the password form")
	}
}

func TestToolDatabaseListConnectionsDoesNotEmitPasswordForm(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	got := h.toolDatabaseWithContext(context.Background(), map[string]interface{}{
		"action": "list_connections",
	})
	if !strings.Contains(got, `"items"`) {
		t.Fatalf("list_connections result = %s", got)
	}
	if _, ok := app.agentViewOpenRecord("tool:run:database"); ok {
		t.Fatal("list_connections opened the propose_profile password form")
	}
}

func TestToolDatabaseExecuteDryRunFalseEmitsApprovalPanel(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	ctx := database.WithRequestScope(context.Background(), database.RequestScope{OwnerID: "owner", SessionID: "gui:owner"})
	got := h.toolDatabaseWithContext(ctx, map[string]interface{}{
		"action": "execute", "connection_id": "c1",
		"sql": "DELETE FROM rapidbi.customers WHERE customerid = 'ALFKI'", "dry_run": false,
	})
	if !strings.Contains(got, "approval panel") {
		t.Fatalf("execute without approval context = %s", got)
	}
	if strings.Contains(got, "approval context is required") {
		t.Fatalf("host must open the panel instead of fail-closing: %s", got)
	}
	if _, ok := app.agentViewOpenRecord("tool:approval"); !ok {
		t.Fatal("database mutation did not open the approval panel")
	}
}

func TestToolDatabaseExecuteDryRunDoesNotEmitApprovalPanel(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	got := h.toolDatabaseWithContext(context.Background(), map[string]interface{}{
		"action": "execute", "connection_id": "c1",
		"sql": "DELETE FROM t WHERE id = 1",
	})
	if _, ok := app.agentViewOpenRecord("tool:approval"); ok {
		t.Fatalf("dry-run execute opened approval: %s", got)
	}
}

func TestToolDatabaseExecuteEmptySQLDoesNotEmitApprovalPanel(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	got := h.toolDatabaseWithContext(context.Background(), map[string]interface{}{
		"action": "execute", "connection_id": "c1", "sql": "", "dry_run": false,
	})
	if _, ok := app.agentViewOpenRecord("tool:approval"); ok {
		t.Fatalf("empty SQL opened approval: %s", got)
	}
}

func TestEmitRegisteredToolApprovalAlwaysAsksForDatabaseMutation(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	firewall := NewSecurityFirewall(NewSecurityRiskAnalyzer(), NewPolicyEngine(), nil)
	firewall.ApproveForSession("gui:owner", "database")
	h := &IMMessageHandler{app: app, firewall: firewall}
	args := map[string]interface{}{"action": "execute", "sql": "DELETE FROM t WHERE id=1", "dry_run": false}
	if !h.emitRegisteredToolApprovalAgentViewIfNeeded("database", args, &SecurityCallContext{SessionID: "gui:owner"}, "owner", context.Background()) {
		t.Fatal("relaxed/session-approved policy must still open the database approval panel")
	}
	if _, ok := app.agentViewOpenRecord("tool:approval"); !ok {
		t.Fatal("missing approval panel")
	}
}

func TestRegisteredToolExecutionResultTreatsDatabaseApprovalWaitAsUncertain(t *testing.T) {
	result := registeredToolExecutionResultForContext(databaseMutationApprovalWaitText, context.Background())
	if result.Outcome != toolOutcomeUncertain || result.FailureKind != toolFailureApprovalRequired {
		t.Fatalf("wait text classified as %#v", result)
	}
}

func TestClassifySharedLoopToolTextDoesNotMarkApprovalWaitAsOK(t *testing.T) {
	got := classifySharedLoopToolText(databaseMutationApprovalWaitText)
	if got.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("shared loop classified approval wait as %q", got.Outcome)
	}
	if got.Result != databaseMutationApprovalWaitText {
		t.Fatalf("wait text rewritten: %q", got.Result)
	}
	ok := classifySharedLoopToolText(`{"ok":true,"affected_rows":1}`)
	if ok.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("successful JSON classified as %q", ok.Outcome)
	}
}

func TestToolDatabaseExecuteWithApprovalContextDoesNotReemitPanel(t *testing.T) {
	app, h := newIsolatedDatabaseToolHandler(t)
	ctx := database.WithApprovalToken(context.Background(), "host-token")
	got := h.toolDatabaseWithContext(ctx, map[string]interface{}{
		"action": "execute", "connection_id": "c1",
		"sql": "DELETE FROM t WHERE id = 1", "dry_run": false,
	})
	if _, ok := app.agentViewOpenRecord("tool:approval"); ok {
		t.Fatalf("approved execute re-opened panel: %s", got)
	}
	if strings.Contains(got, "approval panel") {
		t.Fatalf("approved execute returned wait text: %s", got)
	}
}

func TestDatabaseReadTableUsesSharedProfilePolicy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.xlsx")
	if err := excel.WriteFile(path, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "name"}}, {{Value: "a"}}}}}}); err != nil {
		t.Fatal(err)
	}
	manager := database.NewManager([]database.Profile{{
		ID: "restricted", Type: database.SourceExcel, FilePath: path,
		AllowedOperations: []string{"inspect"},
	}}, nil)
	manager.SetWorkspaceRoot(root)
	connectionID, _, err := manager.ConnectFor(context.Background(), "restricted", "owner", "session")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	h := &IMMessageHandler{databaseManager: manager}
	ctx := database.WithRequestScope(context.Background(), database.RequestScope{OwnerID: "owner", SessionID: "session"})
	got := h.toolDatabaseWithContext(ctx, map[string]interface{}{
		"action": "read_table", "connection_id": connectionID,
	})
	if !strings.Contains(got, "operation is not allowed by profile") {
		t.Fatalf("GUI read_table bypassed shared profile policy: %q", got)
	}
}
