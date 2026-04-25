package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminCredentialDetailUpdateRotateAndDeleteUserTenant(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	cred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key-http", APISecret: "secret-old"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get credential status = %d body = %s", w.Code, w.Body.String())
	}
	var got agentservice.Credential
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	if got.ID != cred.ID || got.APIKey == "key-http" || got.APIKey == "" {
		t.Fatalf("unexpected credential payload: %#v", got)
	}

	body := bytes.NewBufferString(`{"name":"Renamed API"}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID, body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update credential status = %d body = %s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"api_secret":"secret-new"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID+"/rotate-secret", body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate credential secret status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key-http", APISecret: "secret-old"}); err == nil {
		t.Fatalf("old credential secret should be rejected after rotate")
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key-http", APISecret: "secret-new"}); err != nil {
		t.Fatalf("new credential secret should work: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete user status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(context.Background(), tenant.ID, user.ID); err == nil {
		t.Fatalf("expected deleted user to be gone")
	}

	otherUser, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Other"})
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	if _, err := svc.UpdateUserConfig(context.Background(), agentservice.Principal{TenantID: tenant.ID, UserID: otherUser.ID}, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig other: %v", err)
	}
	if _, err := svc.CreateInstance(context.Background(), agentservice.Principal{TenantID: tenant.ID, UserID: otherUser.ID}, agentservice.CreateInstanceInput{Name: "Instance"}); err != nil {
		t.Fatalf("CreateInstance other: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete tenant status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetTenant(context.Background(), tenant.ID); err == nil {
		t.Fatalf("expected deleted tenant to be gone")
	}
}

func TestSystemOpsEndpoints(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	cases := []struct {
		path string
		want int
	}{
		{path: "/livez", want: http.StatusOK},
		{path: "/readyz", want: http.StatusOK},
		{path: "/version", want: http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s status = %d body = %s", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestAdminExportServiceState(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	ctx := context.Background()
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User", Email: "user@example.com"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	cfg, err := svc.UpdateUserConfig(ctx, principal, testLLMConfig())
	if err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMKey == "" {
		t.Fatalf("expected test config to carry api key")
	}
	cred, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "export-key", APISecret: "export-secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	inst, err := svc.CreateInstance(ctx, principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(ctx, principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, _, err := svc.PostMessage(ctx, principal, inst.ID, sess.ID, agentservice.PostMessageInput{Content: "hello export"}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export status = %d body = %s", w.Code, w.Body.String())
	}
	var out agentservice.ExportServiceStateOutput
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if out.Scope != "user" || out.TenantID != tenant.ID || out.UserID != user.ID {
		t.Fatalf("unexpected export scope: %#v", out)
	}
	if len(out.Users) != 1 || out.Users[0].User.ID != user.ID {
		t.Fatalf("unexpected export users: %#v", out.Users)
	}
	if out.Users[0].Config == nil || out.Users[0].Config.AppConfig.MaclawLLMKey == "" || out.Users[0].Config.AppConfig.MaclawLLMKey == "test-key" {
		t.Fatalf("expected sanitized config in export: %#v", out.Users[0].Config)
	}
	if len(out.Users[0].Credentials) != 1 || out.Users[0].Credentials[0].ID != cred.ID || out.Users[0].Credentials[0].SecretDigest != "" || out.Users[0].Credentials[0].APIKeyHash != "" {
		t.Fatalf("expected sanitized credentials in export: %#v", out.Users[0].Credentials)
	}
	if len(out.Users[0].Instances) != 1 || len(out.Users[0].Instances[0].Sessions) != 1 || len(out.Users[0].Instances[0].Sessions[0].Messages) == 0 || len(out.Users[0].Instances[0].Runs) == 0 {
		t.Fatalf("expected nested instance/session/message/run export: %#v", out.Users[0].Instances)
	}
	if len(out.AuditEvents) == 0 {
		t.Fatalf("expected audit events in export")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID+"&include_secrets=true&include_messages=false&include_runs=false&include_audit=false", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export with secrets status = %d body = %s", w.Code, w.Body.String())
	}
	out = agentservice.ExportServiceStateOutput{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode export with secrets: %v", err)
	}
	if out.Users[0].Config == nil || out.Users[0].Config.AppConfig.MaclawLLMKey == "" {
		t.Fatalf("expected full config when include_secrets=true: %#v", out.Users[0].Config)
	}
	if len(out.Users[0].Credentials) != 1 || out.Users[0].Credentials[0].APIKeyHash == "" || out.Users[0].Credentials[0].APIKeyPrefix == "" {
		t.Fatalf("expected internal credential state when include_secrets=true: %#v", out.Users[0].Credentials)
	}
	if len(out.Users[0].Instances[0].Sessions[0].Messages) != 0 || len(out.Users[0].Instances[0].Runs) != 0 || len(out.AuditEvents) != 0 {
		t.Fatalf("expected include flags to omit nested data: %#v %#v", out.Users[0].Instances[0], out.AuditEvents)
	}
}

func TestAdminExportServiceStateRequiresTenantForUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?user_id=user_x", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("export status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminImportServiceStateRoundTrip(t *testing.T) {
	sourceSvc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService source: %v", err)
	}
	sourceServer := NewHTTPServer(sourceSvc, "admin-secret")
	ctx := context.Background()
	tenant, err := sourceSvc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := sourceSvc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User", Email: "user@example.com"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := sourceSvc.UpdateUserConfig(ctx, principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, err := sourceSvc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "roundtrip-key", APISecret: "roundtrip-secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	inst, err := sourceSvc.CreateInstance(ctx, principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := sourceSvc.CreateSession(ctx, principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, _, err := sourceSvc.PostMessage(ctx, principal, inst.ID, sess.ID, agentservice.PostMessageInput{Content: "roundtrip"}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID+"&include_secrets=true", nil)
	exportReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	exportRec := httptest.NewRecorder()
	sourceServer.Handler().ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d body = %s", exportRec.Code, exportRec.Body.String())
	}
	exportBody := append([]byte(nil), exportRec.Body.Bytes()...)
	var exported agentservice.ExportServiceStateOutput
	if err := json.Unmarshal(exportBody, &exported); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if len(exported.Users) != 1 || len(exported.Users[0].Credentials) != 1 || exported.Users[0].Credentials[0].SecretDigest == "" {
		t.Fatalf("expected export to carry restorable credential secrets: %#v", exported.Users)
	}

	targetRoot := t.TempDir()
	targetStore := agentservice.NewMemoryStore()
	targetSvc, err := agentservice.NewService(agentservice.Config{DataRoot: targetRoot, TokenSecret: "test-token-secret-0123456789012345"}, targetStore, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService target: %v", err)
	}
	targetServer := NewHTTPServer(targetSvc, "admin-secret")
	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/import", bytes.NewReader(exportBody))
	importReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	importReq.Header.Set("Content-Type", "application/json")
	importRec := httptest.NewRecorder()
	targetServer.Handler().ServeHTTP(importRec, importReq)
	if importRec.Code != http.StatusOK {
		t.Fatalf("import status = %d body = %s", importRec.Code, importRec.Body.String())
	}
	var imported agentservice.ImportServiceStateOutput
	if err := json.NewDecoder(importRec.Body).Decode(&imported); err != nil {
		t.Fatalf("decode import result: %v", err)
	}
	if imported.Users != 1 || imported.Credentials != 1 || imported.Instances != 1 || imported.Sessions != 1 || imported.Messages == 0 || imported.Runs == 0 {
		t.Fatalf("unexpected import counts: %#v", imported)
	}
	cfg, err := targetStore.GetUserConfig(tenant.ID, user.ID)
	if err != nil {
		t.Fatalf("GetUserConfig target: %v", err)
	}
	if cfg.AppConfig.MaclawLLMKey != "test-key" {
		t.Fatalf("expected restored config secret, got %#v", cfg.AppConfig)
	}
	if _, err := targetSvc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "roundtrip-key", APISecret: "roundtrip-secret"}); err != nil {
		t.Fatalf("expected restored credential auth to work: %v", err)
	}
	restoredInst, err := targetSvc.GetInstance(ctx, principal, inst.ID)
	if err != nil {
		t.Fatalf("GetInstance target: %v", err)
	}
	if restoredInst.DataDir == inst.DataDir || restoredInst.RuntimeDir == inst.RuntimeDir {
		t.Fatalf("expected imported instance paths remapped to target root: %#v", restoredInst)
	}
	if restoredInst.DataDir == "" || restoredInst.RuntimeDir == "" || restoredInst.Workspace == "" {
		t.Fatalf("expected populated runtime paths: %#v", restoredInst)
	}
}

func TestAdminImportServiceStateConflictAndOverwrite(t *testing.T) {
	sourceSvc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService source: %v", err)
	}
	sourceServer := NewHTTPServer(sourceSvc, "admin-secret")
	ctx := context.Background()
	tenant, err := sourceSvc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := sourceSvc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := sourceSvc.UpdateUserConfig(ctx, agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, err := sourceSvc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "overwrite-key", APISecret: "overwrite-secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID+"&include_secrets=true", nil)
	exportReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	exportRec := httptest.NewRecorder()
	sourceServer.Handler().ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d body = %s", exportRec.Code, exportRec.Body.String())
	}
	payload := append([]byte(nil), exportRec.Body.Bytes()...)

	targetStore := agentservice.NewMemoryStore()
	targetSvc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, targetStore, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService target: %v", err)
	}
	targetServer := NewHTTPServer(targetSvc, "admin-secret")
	for i, path := range []string{"/api/v1/admin/import", "/api/v1/admin/import", "/api/v1/admin/import?overwrite=true"} {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		targetServer.Handler().ServeHTTP(w, req)
		want := http.StatusOK
		if i == 1 {
			want = http.StatusConflict
		}
		if w.Code != want {
			t.Fatalf("request %d status = %d want %d body = %s", i, w.Code, want, w.Body.String())
		}
	}
}

func TestAdminImportServiceStateDryRun(t *testing.T) {
	sourceSvc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService source: %v", err)
	}
	sourceServer := NewHTTPServer(sourceSvc, "admin-secret")
	ctx := context.Background()
	tenant, err := sourceSvc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := sourceSvc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := sourceSvc.UpdateUserConfig(ctx, agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, err := sourceSvc.CreateCredential(ctx, agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "dryrun-key", APISecret: "dryrun-secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID+"&include_secrets=true", nil)
	exportReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	exportRec := httptest.NewRecorder()
	sourceServer.Handler().ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d body = %s", exportRec.Code, exportRec.Body.String())
	}
	payload := append([]byte(nil), exportRec.Body.Bytes()...)

	targetStore := agentservice.NewMemoryStore()
	targetSvc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, targetStore, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService target: %v", err)
	}
	targetServer := NewHTTPServer(targetSvc, "admin-secret")
	if err := targetStore.SaveTenant(*tenant); err != nil {
		t.Fatalf("SaveTenant target: %v", err)
	}
	if err := targetStore.SaveUser(*user); err != nil {
		t.Fatalf("SaveUser target: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/import?dry_run=true", bytes.NewReader(payload))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	targetServer.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dry run import status = %d body = %s", w.Code, w.Body.String())
	}
	var out agentservice.ImportServiceStateOutput
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode dry run result: %v", err)
	}
	if !out.DryRun || len(out.Conflicts) == 0 || len(out.Plan) == 0 || out.Users != 1 || out.Credentials != 1 {
		t.Fatalf("unexpected dry run output: %#v", out)
	}
	foundOverwrite := false
	for _, item := range out.Plan {
		if item.ResourceType == "user" && item.Action == "overwrite" {
			foundOverwrite = true
			break
		}
	}
	if !foundOverwrite {
		t.Fatalf("expected overwrite plan item in dry run: %#v", out.Plan)
	}
	if _, err := targetSvc.IssueToken(ctx, agentservice.IssueTokenInput{APIKey: "dryrun-key", APISecret: "dryrun-secret"}); err == nil {
		t.Fatalf("dry run should not mutate target state")
	}
}
