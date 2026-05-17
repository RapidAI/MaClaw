package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminCredentialDetailUpdateRotateAndDeleteUserTenant(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
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

	body = bytes.NewBufferString(`{"api_key":"key-http-rotated"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID+"/rotate-key", body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate credential key status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key-http", APISecret: "secret-new"}); err == nil {
		t.Fatalf("old credential key should be rejected after rotate")
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key-http-rotated", APISecret: "secret-new"}); err != nil {
		t.Fatalf("rotated credential key should work: %v", err)
	}

	body = bytes.NewBufferString(`{}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID+"/rotate-secret", body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("generated rotate credential secret status = %d body = %s", w.Code, w.Body.String())
	}
	var generatedSecret agentservice.Credential
	if err := json.NewDecoder(w.Body).Decode(&generatedSecret); err != nil {
		t.Fatalf("decode generated secret rotation: %v", err)
	}
	if generatedSecret.APISecret == "" || generatedSecret.APIKeyHash != "" || generatedSecret.SecretDigest != "" {
		t.Fatalf("expected one-time generated secret response: %#v", generatedSecret)
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key-http-rotated", APISecret: "secret-new"}); err == nil {
		t.Fatalf("previous rotated secret should be rejected after generated rotate")
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key-http-rotated", APISecret: generatedSecret.APISecret}); err != nil {
		t.Fatalf("generated rotated secret should work: %v", err)
	}

	body = bytes.NewBufferString(`{}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID+"/rotate-key", body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("generated rotate credential key status = %d body = %s", w.Code, w.Body.String())
	}
	var generatedKey agentservice.Credential
	if err := json.NewDecoder(w.Body).Decode(&generatedKey); err != nil {
		t.Fatalf("decode generated key rotation: %v", err)
	}
	if generatedKey.APIKey == "" || generatedKey.APIKey == "key-http-rotated" || generatedKey.APIKeyHash != "" || generatedKey.SecretDigest != "" {
		t.Fatalf("expected one-time generated key response: %#v", generatedKey)
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key-http-rotated", APISecret: generatedSecret.APISecret}); err == nil {
		t.Fatalf("previous rotated key should be rejected after generated rotate")
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: generatedKey.APIKey, APISecret: generatedSecret.APISecret}); err != nil {
		t.Fatalf("generated rotated key should work: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"?confirm=true", nil)
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

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tenants/"+tenant.ID+"?confirm=true", nil)
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
	server := NewHTTPServer(svc, "admin-secret", nil)
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
	server := NewHTTPServer(svc, "admin-secret", nil)
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
	if w.Code != http.StatusBadRequest {
		t.Fatalf("export with secrets without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID+"&include_secrets=true&confirm=true&include_messages=false&include_runs=false&include_audit=false", nil)
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
	exportEvents, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.service_state_exported"})
	if err != nil {
		t.Fatalf("ListAuditEvents export: %v", err)
	}
	foundSecretExportAudit := false
	for _, event := range exportEvents {
		if event.Metadata["include_secrets"] == "true" {
			foundSecretExportAudit = true
		}
	}
	if !foundSecretExportAudit {
		t.Fatalf("expected export audit event, got %#v", exportEvents)
	}
}

func TestAdminExportServiceStateRequiresTenantForUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
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
	sourceServer := NewHTTPServer(sourceSvc, "admin-secret", nil)
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

	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID+"&include_secrets=true&confirm=true", nil)
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
	targetServer := NewHTTPServer(targetSvc, "admin-secret", nil)
	missingConfirmReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/import", bytes.NewReader(exportBody))
	missingConfirmReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	missingConfirmReq.Header.Set("Content-Type", "application/json")
	missingConfirmRec := httptest.NewRecorder()
	targetServer.Handler().ServeHTTP(missingConfirmRec, missingConfirmReq)
	if missingConfirmRec.Code != http.StatusBadRequest {
		t.Fatalf("import without confirm status = %d body = %s", missingConfirmRec.Code, missingConfirmRec.Body.String())
	}

	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/import?confirm=true", bytes.NewReader(exportBody))
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
	importEvents, err := targetSvc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "admin.service_state_imported"})
	if err != nil {
		t.Fatalf("ListAuditEvents import: %v", err)
	}
	if len(importEvents) == 0 || importEvents[0].Metadata["dry_run"] != "false" {
		t.Fatalf("expected import audit event, got %#v", importEvents)
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
	sourceServer := NewHTTPServer(sourceSvc, "admin-secret", nil)
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
	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID+"&include_secrets=true&confirm=true", nil)
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
	targetServer := NewHTTPServer(targetSvc, "admin-secret", nil)
	for i, path := range []string{"/api/v1/admin/import?confirm=true", "/api/v1/admin/import?confirm=true", "/api/v1/admin/import?overwrite=true&confirm=true"} {
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
	sourceServer := NewHTTPServer(sourceSvc, "admin-secret", nil)
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
	exportReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/export?tenant_id="+tenant.ID+"&user_id="+user.ID+"&include_secrets=true&confirm=true", nil)
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
	targetServer := NewHTTPServer(targetSvc, "admin-secret", nil)
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

func TestAdminServiceSnapshots(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Snapshot Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Snapshot User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "snapshot-key", APISecret: "snapshot-secret"}); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	body := bytes.NewBufferString(`{"name":"tenant backup","tenant_id":"` + tenant.ID + `","include_messages":true,"include_runs":true,"include_audit":true,"include_secrets":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots", body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create secret snapshot without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	body = bytes.NewBufferString(`{"name":"tenant backup","tenant_id":"` + tenant.ID + `","include_messages":true,"include_runs":true,"include_audit":true,"include_secrets":true}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots?confirm=true", body)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create snapshot status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.ServiceSnapshotEnvelope
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created snapshot: %v", err)
	}
	if created.Snapshot.ID == "" || created.Snapshot.Scope != "tenant" || created.Snapshot.TenantID != tenant.ID || created.Snapshot.SizeBytes <= 0 {
		t.Fatalf("unexpected created snapshot: %#v", created.Snapshot)
	}
	if len(created.Data.Tenants) != 1 || len(created.Data.Users) != 1 {
		t.Fatalf("unexpected snapshot export payload: %#v", created.Data)
	}
	createdEvents, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.snapshot_created"})
	if err != nil {
		t.Fatalf("ListAuditEvents snapshot created: %v", err)
	}
	if len(createdEvents) == 0 || createdEvents[0].Metadata["include_secrets"] != "true" {
		t.Fatalf("expected snapshot create audit event, got %#v", createdEvents)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/snapshots?tenant_id="+tenant.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list snapshots status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []agentservice.ServiceSnapshot `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list snapshots: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != created.Snapshot.ID {
		t.Fatalf("unexpected listed snapshots: %#v", listed.Items)
	}
	since := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	until := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/snapshots?scope=tenant&name=BACKUP&since="+since+"&until="+until, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("filtered list snapshots status = %d body = %s", w.Code, w.Body.String())
	}
	listed.Items = nil
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode filtered list snapshots: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != created.Snapshot.ID {
		t.Fatalf("unexpected filtered snapshots: %#v", listed.Items)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/snapshots?scope=project", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid snapshot scope status = %d body = %s", w.Code, w.Body.String())
	}
	overview, err := svc.GetAdminOverview(context.Background())
	if err != nil {
		t.Fatalf("GetAdminOverview: %v", err)
	}
	if overview.Snapshots != 1 || overview.SnapshotBytes <= 0 {
		t.Fatalf("expected snapshot counters in overview: %#v", overview)
	}
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics status = %d body = %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("maclaw_snapshots_total 1")) || !bytes.Contains(w.Body.Bytes(), []byte("maclaw_snapshot_bytes_total ")) {
		t.Fatalf("expected snapshot metrics, got %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/snapshots/"+created.Snapshot.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get snapshot status = %d body = %s", w.Code, w.Body.String())
	}
	var got agentservice.ServiceSnapshotEnvelope
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode got snapshot: %v", err)
	}
	if got.Snapshot.ID != created.Snapshot.ID || got.Data.Scope != "tenant" {
		t.Fatalf("unexpected got snapshot: %#v", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots/"+created.Snapshot.ID+"/restore", bytes.NewBufferString(`{"dry_run":true}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dry run restore snapshot status = %d body = %s", w.Code, w.Body.String())
	}
	var dryRun agentservice.RestoreServiceSnapshotOutput
	if err := json.NewDecoder(w.Body).Decode(&dryRun); err != nil {
		t.Fatalf("decode dry run restore: %v", err)
	}
	if !dryRun.Import.DryRun || len(dryRun.Import.Conflicts) == 0 {
		t.Fatalf("expected dry run conflicts for existing resources: %#v", dryRun.Import)
	}

	if err := svc.DeleteUser(context.Background(), tenant.ID, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := svc.GetUser(context.Background(), tenant.ID, user.ID); err == nil {
		t.Fatalf("expected user to be deleted before restore")
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots/"+created.Snapshot.ID+"/restore?overwrite=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("restore snapshot without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots/"+created.Snapshot.ID+"/restore?overwrite=true&confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("restore snapshot status = %d body = %s", w.Code, w.Body.String())
	}
	var restored agentservice.RestoreServiceSnapshotOutput
	if err := json.NewDecoder(w.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore snapshot: %v", err)
	}
	if restored.Snapshot.ID != created.Snapshot.ID || restored.Import.Users != 1 || restored.Import.Credentials != 1 {
		t.Fatalf("unexpected restore output: %#v", restored)
	}
	restoreEvents, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.snapshot_restored"})
	if err != nil {
		t.Fatalf("ListAuditEvents restore: %v", err)
	}
	foundRestoreAudit := false
	for _, event := range restoreEvents {
		if event.Metadata["dry_run"] == "false" {
			foundRestoreAudit = true
		}
	}
	if !foundRestoreAudit {
		t.Fatalf("expected restore audit event, got %#v", restoreEvents)
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "snapshot-key", APISecret: "snapshot-secret"}); err != nil {
		t.Fatalf("restored credential should authenticate: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/snapshots/"+created.Snapshot.ID+"?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete snapshot status = %d body = %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/snapshots/"+created.Snapshot.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get deleted snapshot status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminPruneServiceSnapshots(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Prune Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Prune User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for i := 0; i < 3; i++ {
		body := bytes.NewBufferString(`{"name":"snapshot ` + string(rune('a'+i)) + `","tenant_id":"` + tenant.ID + `","user_id":"` + user.ID + `"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots", body)
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create snapshot %d status = %d body = %s", i, w.Code, w.Body.String())
		}
	}
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots/prune?tenant_id="+tenant.ID+"&user_id="+user.ID+"&older_than="+future+"&keep_latest=1&dry_run=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dry run prune status = %d body = %s", w.Code, w.Body.String())
	}
	var dryRun agentservice.PruneServiceSnapshotsOutput
	if err := json.NewDecoder(w.Body).Decode(&dryRun); err != nil {
		t.Fatalf("decode dry run prune: %v", err)
	}
	if !dryRun.DryRun || dryRun.Deleted != 0 || len(dryRun.Snapshots) != 2 || len(dryRun.KeptSnapshots) != 1 {
		t.Fatalf("unexpected dry run prune output: %#v", dryRun)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots/prune?tenant_id="+tenant.ID+"&user_id="+user.ID+"&older_than="+future+"&keep_latest=1", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("prune status = %d body = %s", w.Code, w.Body.String())
	}
	var pruned agentservice.PruneServiceSnapshotsOutput
	if err := json.NewDecoder(w.Body).Decode(&pruned); err != nil {
		t.Fatalf("decode prune: %v", err)
	}
	if pruned.Deleted != 2 || len(pruned.Snapshots) != 2 || pruned.FreedBytes <= 0 {
		t.Fatalf("unexpected prune output: %#v", pruned)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/snapshots?tenant_id="+tenant.ID+"&user_id="+user.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list after prune status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []agentservice.ServiceSnapshot `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list after prune: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != dryRun.KeptSnapshots[0].ID {
		t.Fatalf("unexpected snapshots after prune: %#v kept=%#v", listed.Items, dryRun.KeptSnapshots)
	}
}
