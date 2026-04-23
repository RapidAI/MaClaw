package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminCanListTenantsAndUsers(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/admin/tenants", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list tenants status = %d body = %s", w.Code, w.Body.String())
	}
	var tenants struct {
		Items []agentservice.Tenant `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tenants); err != nil {
		t.Fatalf("decode tenants: %v", err)
	}
	if len(tenants.Items) != 1 || tenants.Items[0].ID != tenant.ID {
		t.Fatalf("tenants = %#v", tenants.Items)
	}

	req = httptest.NewRequest("GET", "/api/v1/admin/tenants/"+tenant.ID+"/users", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users status = %d body = %s", w.Code, w.Body.String())
	}
	var users struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(users.Items) != 1 || users.Items[0].ID != user.ID {
		t.Fatalf("users = %#v", users.Items)
	}
}

func TestAdminCanListAndRevokeCredentials(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	cred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list credentials status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []agentservice.Credential `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != cred.ID || listed.Items[0].Status != agentservice.CredentialStatusActive {
		t.Fatalf("credentials = %#v", listed.Items)
	}

	req = httptest.NewRequest("DELETE", "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID, bytes.NewReader(nil))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke credential status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key", APISecret: "secret"}); err == nil {
		t.Fatalf("expected revoked credential to reject token issuance")
	}
}

func TestAdminCanListAuditEvents(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/admin/audit-events?tenant_id="+tenant.ID+"&action=user.created", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list audit events status = %d body = %s", w.Code, w.Body.String())
	}
	var events struct {
		Items []agentservice.AuditEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	if len(events.Items) != 1 || events.Items[0].Action != "user.created" || events.Items[0].UserID != user.ID {
		t.Fatalf("audit events = %#v", events.Items)
	}
	if events.Items[0].ActorType != "admin" || events.Items[0].ResourceType != "user" {
		t.Fatalf("unexpected audit event = %#v", events.Items[0])
	}
}

func TestListRunsFiltersByStatus(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	_, err = svc.UpdateUserConfig(context.Background(), principal, testLLMConfig())
	if err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_1", Status: agentservice.RunStatusFailed, StartedAt: now.Add(1 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_2", Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun succeeded: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/runs?status=failed", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list runs status = %d body = %s", w.Code, w.Body.String())
	}
	var runs struct {
		Items []agentservice.Run `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runs.Items) != 1 || runs.Items[0].ID != "run_1" || runs.Items[0].Status != agentservice.RunStatusFailed {
		t.Fatalf("runs = %#v", runs.Items)
	}
}

func TestGetUsageSummary(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	sess := agentservice.Session{ID: "sess_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_user", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_assistant", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage assistant: %v", err)
	}
	completed := now.Add(3 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/usage/summary", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("usage summary status = %d body = %s", w.Code, w.Body.String())
	}
	var summary agentservice.UsageSummary
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Instances != 1 || summary.Sessions != 1 || summary.Messages != 2 || summary.UserMessages != 1 || summary.AssistantMessages != 1 || summary.Runs != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || summary.LastActivityAt == nil {
		t.Fatalf("summary run status/last activity = %#v", summary)
	}
}

func TestPaginateMessagesReturnsNewestWindowChronologically(t *testing.T) {
	base := time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)
	items := []agentservice.Message{
		{ID: "msg_1", CreatedAt: base.Add(1 * time.Minute)},
		{ID: "msg_2", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "msg_3", CreatedAt: base.Add(3 * time.Minute)},
	}

	page, err := parsePageQuery(httptest.NewRequest("GET", "/messages?limit=2", nil))
	if err != nil {
		t.Fatalf("parsePageQuery: %v", err)
	}
	got, meta := paginateMessages(items, page)

	if len(got) != 2 || got[0].ID != "msg_2" || got[1].ID != "msg_3" {
		t.Fatalf("unexpected page: %#v", got)
	}
	if !meta.HasMore || meta.NextBefore != got[0].CreatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected meta: %#v", meta)
	}
}

func TestParsePageQueryCapsLimitAndValidatesBefore(t *testing.T) {
	page, err := parsePageQuery(httptest.NewRequest("GET", "/instances?limit=999", nil))
	if err != nil {
		t.Fatalf("parsePageQuery: %v", err)
	}
	if page.Limit != maxPageLimit {
		t.Fatalf("limit = %d, want %d", page.Limit, maxPageLimit)
	}

	if _, err := parsePageQuery(httptest.NewRequest("GET", "/instances?before=not-time", nil)); err == nil {
		t.Fatalf("expected invalid before error")
	}
}

func testLLMConfig() corelib.AppConfig {
	return corelib.AppConfig{
		MaclawLLMUrl:   "https://llm.example/v1",
		MaclawLLMKey:   "test-key",
		MaclawLLMModel: "test-model",
	}
}

func TestRequestClientIPStripsSourcePort(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/auth/token", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	if got := requestClientIP(req); got != "203.0.113.10" {
		t.Fatalf("requestClientIP = %q", got)
	}
}

func TestTokenRateLimitUsesClientIPNotSourcePort(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	server.authLimiter = newAuthLimiter(1, time.Minute)

	body := []byte(`{"api_key":"missing","api_secret":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.20:10001"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("first token attempt status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.20:10099"
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second token attempt status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestFailedTokenAttemptCreatesAuditEvent(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"missing-key","api_secret":"wrong"}`)))
	req.RemoteAddr = "203.0.113.9:40123"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("token failure status = %d body = %s", w.Code, w.Body.String())
	}

	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "auth.token_failed"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 auth.token_failed event, got %#v", events)
	}
	if events[0].Metadata["remote_ip"] != "203.0.113.9" {
		t.Fatalf("unexpected remote_ip metadata = %#v", events[0].Metadata)
	}
	if events[0].Metadata["api_key_prefix"] != "missin" {
		t.Fatalf("unexpected api_key_prefix metadata = %#v", events[0].Metadata)
	}
}

func TestTokenFailureThresholdTriggersTemporaryLock(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	server.authLimiter = newAuthLimiter(100, time.Minute)

	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"lock-key","api_secret":"wrong"}`)))
		req.RemoteAddr = "198.51.100.50:50000"
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d body = %s", i+1, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"lock-key","api_secret":"wrong"}`)))
	req.RemoteAddr = "198.51.100.50:50001"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("threshold attempt status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Fatalf("expected Retry-After header")
	}
}

func TestGetInstanceCapabilities(t *testing.T) {
	executor := &agentservice.CoreAgentExecutor{}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get capabilities status = %d body = %s", w.Code, w.Body.String())
	}
	var caps agentservice.AgentCapabilities
	if err := json.NewDecoder(w.Body).Decode(&caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if caps.Executor != "core_agent" || !caps.SupportsSSH || !caps.SupportsAskUser {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
	if len(caps.Tools) == 0 {
		t.Fatalf("expected tools in capabilities")
	}
}


func TestMCPRemoteServerCRUDAndTools(t *testing.T) {
	tenantID, userID, token, server := newMCPAuthenticatedServer(t)
	_ = tenantID
	_ = userID

	remoteMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode remote MCP request: %v", err)
		}
		if req["method"] == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{}}}`))
			return
		}
		if req["method"] != "tools/list" {
			t.Fatalf("unexpected remote MCP method: %#v", req["method"])
		}
		if got := r.Header.Get("Authorization"); got != "Bearer remote-secret" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
			t.Fatalf("session id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search_docs","description":"Search docs","inputSchema":{"type":"object"}}]}}`))
	}))
	defer remoteMCP.Close()

	body := fmt.Sprintf(`{"kind":"remote","name":"Docs MCP","endpoint_url":%q,"auth_type":"bearer","auth_secret":"remote-secret"}`, remoteMCP.URL)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create remote MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created MCP server: %v", err)
	}
	if created.Kind != "remote" || created.Name != "Docs MCP" || created.EndpointURL != remoteMCP.URL {
		t.Fatalf("unexpected created remote MCP server: %#v", created)
	}
	if created.HasAuthSecret != true {
		t.Fatalf("expected auth secret marker on created remote MCP server: %#v", created)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/health-check", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("remote MCP health status = %d body = %s", w.Code, w.Body.String())
	}
	var checked agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&checked); err != nil {
		t.Fatalf("decode checked MCP server: %v", err)
	}
	if checked.HealthStatus != "healthy" || !checked.Running || len(checked.Tools) != 1 {
		t.Fatalf("unexpected checked MCP server: %#v", checked)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers/"+created.ID+"/tools", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("remote MCP tools status = %d body = %s", w.Code, w.Body.String())
	}
	var tools struct {
		Items []agentservice.MCPToolView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode remote MCP tools: %v", err)
	}
	if len(tools.Items) != 1 || tools.Items[0].Name != "search_docs" {
		t.Fatalf("unexpected remote MCP tools: %#v", tools.Items)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/mcp/servers/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete remote MCP status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestMCPLocalServerStartAndStop(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	cmd := os.Args[0]
	body := fmt.Sprintf(`{"kind":"local","name":"Local Echo","command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, cmd)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode local MCP server: %v", err)
	}
	if created.Kind != "local" || created.Command != cmd {
		t.Fatalf("unexpected local MCP create result: %#v", created)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/start", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("start local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var started agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&started); err != nil {
		t.Fatalf("decode started local MCP server: %v", err)
	}
	if !started.Running || started.HealthStatus != "running" {
		t.Fatalf("unexpected started local MCP server: %#v", started)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers/"+created.ID+"/tools", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("local MCP tools status = %d body = %s", w.Code, w.Body.String())
	}
	var tools struct {
		Items []agentservice.MCPToolView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode local MCP tools: %v", err)
	}
	if len(tools.Items) != 1 || tools.Items[0].Name != "echo" {
		t.Fatalf("unexpected local MCP tools: %#v", tools.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/stop", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stop local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var stopped agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&stopped); err != nil {
		t.Fatalf("decode stopped local MCP server: %v", err)
	}
	if stopped.Running || stopped.HealthStatus != "stopped" {
		t.Fatalf("unexpected stopped local MCP server: %#v", stopped)
	}
}

func newMCPAuthenticatedServer(t *testing.T) (string, string, string, *HTTPServer) {
	t.Helper()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	return tenant.ID, user.ID, token, NewHTTPServer(svc, "admin-secret")
}

func TestLocalMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_LOCAL_MCP_HELPER") != "1" {
		return
	}
	defer os.Exit(0)
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		if method == "notifications/initialized" {
			continue
		}
		id := int(req["id"].(float64))
		switch method {
		case "initialize":
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{}}}\n", id)
		case "tools/list":
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"tools\":[{\"name\":\"echo\",\"description\":\"Echo text\",\"inputSchema\":{\"type\":\"object\"}}]}}\n", id)
		default:
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{}}\n", id)
		}
	}
}

