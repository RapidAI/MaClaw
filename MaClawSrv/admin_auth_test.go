package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminBootstrapInitializeLoginMeLogout(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv(adminBootstrapSetupTokenEnv, "setup-token-123456")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/bootstrap/status", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d body = %s", w.Code, w.Body.String())
	}
	var status map[string]any
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status["initialized"] != false || status["setup_required"] != true || status["setup_token_required"] != true {
		t.Fatalf("unexpected bootstrap status: %#v", status)
	}

	initBody := `{"setup_token":"setup-token-123456","username":"Admin.Owner","password":"strong-password-123","display_name":"Owner","locale":"en-US"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/bootstrap/initialize", bytes.NewBufferString(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.20:4444"
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap initialize = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "state", "admin_bootstrap.json")); err != nil {
		t.Fatalf("expected bootstrap state: %v", err)
	}

	loginBody := `{"username":"admin.owner","password":"strong-password-123"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(loginBody))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.20:5555"
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusOK {
		t.Fatalf("admin login = %d body = %s", w.Code, w.Body.String())
	}
	var login struct {
		Token string          `json:"token"`
		Admin adminUserPublic `json:"admin"`
	}
	if err := json.NewDecoder(w.Body).Decode(&login); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if login.Token == "" || login.Admin.Username != "admin.owner" || login.Admin.Role != "owner" {
		t.Fatalf("unexpected login payload: %#v", login)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/me", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("admin_session")) {
		t.Fatalf("admin me = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-config/effective", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("session token admin endpoint = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/logout", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin logout = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-config/effective", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminBootstrapRejectsBadSetupTokenAndShortPassword(t *testing.T) {
	t.Setenv(adminBootstrapSetupTokenEnv, "setup-token-expected")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	body := `{"setup_token":"wrong","username":"admin","password":"strong-password-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bootstrap/initialize", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad setup token status = %d body = %s", w.Code, w.Body.String())
	}

	body = `{"setup_token":"setup-token-expected","username":"admin","password":"short"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/bootstrap/initialize", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short password status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminChangePasswordRequiresSessionAndRevokesOtherSessions(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv(adminBootstrapSetupTokenEnv, "setup-token-change")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	initBody := `{"setup_token":"setup-token-change","username":"admin","password":"old-password-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bootstrap/initialize", bytes.NewBufferString(initBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap initialize = %d body = %s", w.Code, w.Body.String())
	}

	login := func(password string) (string, int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"admin","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			return "", w.Code, w.Body.String()
		}
		var out struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode login: %v", err)
		}
		return out.Token, w.Code, w.Body.String()
	}
	token1, code, body := login("old-password-123")
	if code != http.StatusOK || token1 == "" {
		t.Fatalf("login1 code=%d body=%s", code, body)
	}
	token2, code, body := login("old-password-123")
	if code != http.StatusOK || token2 == "" {
		t.Fatalf("login2 code=%d body=%s", code, body)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/change-password", bytes.NewBufferString(`{"old_password":"old-password-123","new_password":"new-password-456"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", token1)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("change password = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/me", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", token2)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked second session = %d body = %s", w.Code, w.Body.String())
	}

	_, code, _ = login("old-password-123")
	if code != http.StatusUnauthorized {
		t.Fatalf("old password login code = %d", code)
	}
	newToken, code, body := login("new-password-456")
	if code != http.StatusOK || newToken == "" {
		t.Fatalf("new password login code=%d body=%s", code, body)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/change-password", bytes.NewBufferString(`{"old_password":"new-password-456","new_password":"another-password-789"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("root secret change password should require session, got %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminOwnerCanListAndRevokeAdminSessions(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv(adminBootstrapSetupTokenEnv, "setup-token-sessions")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bootstrap/initialize", bytes.NewBufferString(`{"setup_token":"setup-token-sessions","username":"admin","password":"session-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap initialize = %d body = %s", w.Code, w.Body.String())
	}

	login := func() (string, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"admin","password":"session-password-123"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login = %d body = %s", w.Code, w.Body.String())
		}
		var out struct {
			Token   string             `json:"token"`
			Session adminSessionPublic `json:"session"`
		}
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode login: %v", err)
		}
		if out.Token == "" || out.Session.ID == "" {
			t.Fatalf("unexpected login payload: %#v", out)
		}
		return out.Token, out.Session.ID
	}
	token1, _ := login()
	token2, token2SessionID := login()

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/sessions", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", token1)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list sessions = %d body = %s", w.Code, w.Body.String())
	}
	var sessions struct {
		Items []adminSessionAdminView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessions.Items) != 2 {
		t.Fatalf("expected 2 sessions, got %#v", sessions.Items)
	}
	revokeID := token2SessionID

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/auth/sessions/"+revokeID+"?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", token1)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke session = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/me", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", token2)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token2 status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminOwnerSuspendRequiresUnsafeConfirmationAndRevokesSessions(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv(adminBootstrapSetupTokenEnv, "setup-token-suspend")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bootstrap/initialize", bytes.NewBufferString(`{"setup_token":"setup-token-suspend","username":"admin","password":"suspend-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap initialize = %d body = %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"admin","password":"suspend-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d body = %s", w.Code, w.Body.String())
	}
	var login struct {
		Token string          `json:"token"`
		Admin adminUserPublic `json:"admin"`
	}
	if err := json.NewDecoder(w.Body).Decode(&login); err != nil {
		t.Fatalf("decode login: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/users", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("admin")) {
		t.Fatalf("list users = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/auth/users/"+login.Admin.ID, bytes.NewBufferString(`{"status":"suspended"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("suspend without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/auth/users/"+login.Admin.ID, bytes.NewBufferString(`{"status":"suspended","confirm_unsafe":true}`))
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("suspended")) {
		t.Fatalf("suspend with confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/me", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", login.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("suspended session status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"admin","password":"suspend-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("suspended login status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminOwnerCanCreateOperatorAndOperatorCannotManageAdmins(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv(adminBootstrapSetupTokenEnv, "setup-token-create-admin")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bootstrap/initialize", bytes.NewBufferString(`{"setup_token":"setup-token-create-admin","username":"owner","password":"owner-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap initialize = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"owner","password":"owner-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner login = %d body = %s", w.Code, w.Body.String())
	}
	var ownerLogin struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(w.Body).Decode(&ownerLogin); err != nil {
		t.Fatalf("decode owner login: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/users", bytes.NewBufferString(`{"username":"ops","password":"operator-password-123","display_name":"Ops","role":"operator","locale":"en_US"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", ownerLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create operator = %d body = %s", w.Code, w.Body.String())
	}
	var created struct {
		Admin adminUserPublic `json:"admin"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Admin.Username != "ops" || created.Admin.Role != "operator" || created.Admin.Locale != "en-US" {
		t.Fatalf("unexpected created admin: %#v", created.Admin)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/auth/users/"+created.Admin.ID, bytes.NewBufferString(`{"locale":"zh-Hans"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", ownerLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"locale":"zh-CN"`) {
		t.Fatalf("normalize operator locale = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"ops","password":"operator-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("operator login = %d body = %s", w.Code, w.Body.String())
	}
	var operatorLogin struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(w.Body).Decode(&operatorLogin); err != nil {
		t.Fatalf("decode operator login: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/status", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", operatorLogin.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("operator runtime access = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/users", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", operatorLogin.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("operator admin-user access should be forbidden, got %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants", bytes.NewBufferString(`{"name":"operator tenant"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", operatorLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("operator tenant create should be forbidden, got %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/switch", bytes.NewBufferString(`{"mode":"bwrap"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", operatorLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("operator sandbox switch should be forbidden, got %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants", bytes.NewBufferString(`{"name":"owner tenant"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", ownerLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner tenant create = %d body = %s", w.Code, w.Body.String())
	}
	var ownerTenant agentservice.Tenant
	if err := json.NewDecoder(w.Body).Decode(&ownerTenant); err != nil {
		t.Fatalf("decode owner tenant: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants/"+ownerTenant.ID+"/users", bytes.NewBufferString(`{"name":"Owner User","email":"owner-user@example.test"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", ownerLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner user create = %d body = %s", w.Code, w.Body.String())
	}
	var ownerUser agentservice.User
	if err := json.NewDecoder(w.Body).Decode(&ownerUser); err != nil {
		t.Fatalf("decode owner user: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/snapshots?confirm=true", bytes.NewBufferString(`{"name":"secret snapshot","include_secrets":true}`))
	req.Header.Set("X-MaClaw-Admin-Secret", ownerLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner secret snapshot create = %d body = %s", w.Code, w.Body.String())
	}
	var secretSnapshot struct {
		Snapshot agentservice.ServiceSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(w.Body).Decode(&secretSnapshot); err != nil {
		t.Fatalf("decode secret snapshot: %v", err)
	}
	if secretSnapshot.Snapshot.ID == "" || !secretSnapshot.Snapshot.IncludeSecrets {
		t.Fatalf("unexpected secret snapshot: %#v", secretSnapshot.Snapshot)
	}

	ownerOnlyChecks := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPatch, "/api/v1/admin/service-config/draft", `{"values":{"sandbox_mode":"auto"}}`},
		{http.MethodDelete, "/api/v1/admin/service-config/draft?confirm=true", ``},
		{http.MethodPost, "/api/v1/admin/service-config/export-plan", `{}`},
		{http.MethodPut, "/api/v1/admin/sandbox/config", `{"mode":"bwrap"}`},
		{http.MethodPost, "/api/v1/admin/sandbox/rollback", `{}`},
		{http.MethodPut, "/api/v1/admin/sandbox/profiles/default", `{"backend":"bwrap"}`},
		{http.MethodDelete, "/api/v1/admin/sandbox/profiles/default?confirm=true", ``},
		{http.MethodGet, "/api/v1/admin/export?include_secrets=true&confirm=true", ``},
		{http.MethodGet, "/api/v1/admin/snapshots/" + secretSnapshot.Snapshot.ID, ``},
		{http.MethodPost, "/api/v1/admin/import?dry_run=true", `{}`},
		{http.MethodPost, "/api/v1/admin/snapshots", `{"name":"operator snapshot"}`},
		{http.MethodPost, "/api/v1/admin/snapshots/prune?dry_run=true", ``},
		{http.MethodPost, "/api/v1/admin/snapshots/missing/restore?dry_run=true", `{}`},
		{http.MethodDelete, "/api/v1/admin/snapshots/missing?confirm=true", ``},
		{http.MethodPost, "/api/v1/admin/runtime/gc", ``},
		{http.MethodPost, "/api/v1/admin/jobs/missing/cancel", ``},
		{http.MethodPost, "/api/v1/admin/logs/service/rotate?confirm=true", ``},
		{http.MethodDelete, "/api/v1/admin/sandbox/reports/missing?confirm=true", ``},
		{http.MethodPut, "/api/v1/admin/knowledge-access/cross-tenant", `{"enabled":true}`},
		{http.MethodPut, "/api/v1/admin/knowledge-access/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID, `{"scopes":[]}`},
		{http.MethodDelete, "/api/v1/admin/knowledge-access/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID, ``},
		{http.MethodGet, "/api/v1/admin/tenants/" + ownerTenant.ID + "/retire-plan?include_secrets=true&confirm=true", ``},
		{http.MethodGet, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "/retire-plan?include_secrets=true&confirm=true", ``},
		{http.MethodDelete, "/api/v1/admin/tenants/" + ownerTenant.ID + "/knowledge?confirm=true", ``},
		{http.MethodPut, "/api/v1/admin/skill-sources/global", `{"enabled":true,"allowed_sources":["github"]}`},
		{http.MethodPut, "/api/v1/admin/skill-sources/tenant/" + ownerTenant.ID, `{"enabled":true,"allowed_sources":["github"]}`},
		{http.MethodDelete, "/api/v1/admin/skill-sources/tenant/" + ownerTenant.ID, ``},
		{http.MethodPut, "/api/v1/admin/skill-sources/user/operator@example.test", `{"enabled":true,"allowed_sources":["github"]}`},
		{http.MethodDelete, "/api/v1/admin/skill-sources/user/operator@example.test", ``},
		{http.MethodPatch, "/api/v1/admin/tenants/" + ownerTenant.ID, `{"name":"operator update"}`},
		{http.MethodPost, "/api/v1/admin/tenants/" + ownerTenant.ID + "/pause", ``},
		{http.MethodPost, "/api/v1/admin/tenants/" + ownerTenant.ID + "/resume", ``},
		{http.MethodDelete, "/api/v1/admin/tenants/" + ownerTenant.ID + "?confirm=true", ``},
		{http.MethodPost, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "/credentials", `{}`},
		{http.MethodPatch, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID, `{"name":"operator update"}`},
		{http.MethodPost, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "/pause", ``},
		{http.MethodPost, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "/resume", ``},
		{http.MethodDelete, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "?confirm=true", ``},
		{http.MethodPatch, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "/credentials/missing", `{"status":"suspended"}`},
		{http.MethodPost, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "/credentials/missing/rotate-secret", `{}`},
		{http.MethodPost, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "/credentials/missing/rotate-key", `{}`},
		{http.MethodDelete, "/api/v1/admin/tenants/" + ownerTenant.ID + "/users/" + ownerUser.ID + "/credentials/missing", ``},
	}
	for _, tc := range ownerOnlyChecks {
		req = httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("X-MaClaw-Admin-Secret", operatorLogin.Token)
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("operator %s %s should be forbidden, got %d body = %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	deniedEvents, err := svc.ListAuditEvents(req.Context(), agentservice.ListAuditEventsInput{Action: "admin.owner_required_failed", ActorType: "admin"})
	if err != nil {
		t.Fatalf("ListAuditEvents owner denied: %v", err)
	}
	if len(deniedEvents) != len(ownerOnlyChecks)+3 {
		t.Fatalf("owner denied audit events = %d, want %d", len(deniedEvents), len(ownerOnlyChecks)+3)
	}
	lastDenied := deniedEvents[0]
	if lastDenied.ActorUser != created.Admin.ID || lastDenied.ResourceType != "admin_authorization" || lastDenied.Metadata["admin_role"] != "operator" || lastDenied.Metadata["method"] == "" || lastDenied.Metadata["auth_type"] != "admin_session" {
		t.Fatalf("unexpected owner denied audit metadata: %#v", lastDenied)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?kind=admin_authorization_denied", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", ownerLogin.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner denied risk events status = %d body = %s", w.Code, w.Body.String())
	}
	var deniedRisks struct {
		Items []adminRiskEvent `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&deniedRisks); err != nil {
		t.Fatalf("decode owner denied risk events: %v", err)
	}
	if deniedRisks.Total == 0 || len(deniedRisks.Items) == 0 || deniedRisks.Items[0].Kind != "admin_authorization_denied" {
		t.Fatalf("expected owner denied risk events, got %#v", deniedRisks)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/auth/users/"+created.Admin.ID, bytes.NewBufferString(`{"new_password":"operator-password-456"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", ownerLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reset operator password = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/status", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", operatorLogin.Token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old operator session after reset = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"ops","password":"operator-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old operator password status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", bytes.NewBufferString(`{"username":"ops","password":"operator-password-456"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("new operator password login = %d body = %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/users", bytes.NewBufferString(`{"username":"ops","password":"operator-password-456"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", ownerLogin.Token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate admin username status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestInvalidAdminSecretRecordsAuditAndRisk(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/status", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "bad-admin-secret")
	req.RemoteAddr = "203.0.113.45:4567"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid admin secret status = %d body = %s", w.Code, w.Body.String())
	}

	events, err := svc.ListAuditEvents(req.Context(), agentservice.ListAuditEventsInput{Action: "admin.auth_failed", ResourceType: "admin_auth"})
	if err != nil {
		t.Fatalf("ListAuditEvents admin auth failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("admin auth failed audit events = %d, want 1: %#v", len(events), events)
	}
	if events[0].ResourceID != "/api/v1/admin/runtime/status" || events[0].Metadata["method"] != http.MethodGet || events[0].Metadata["remote_ip"] != "203.0.113.45" {
		t.Fatalf("unexpected admin auth failed audit event: %#v", events[0])
	}
	if _, ok := events[0].Metadata["admin_secret"]; ok {
		t.Fatalf("admin auth failed audit must not store presented secret: %#v", events[0].Metadata)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?kind=auth_failed", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("auth failed risk events status = %d body = %s", w.Code, w.Body.String())
	}
	var risks struct {
		Items []adminRiskEvent `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&risks); err != nil {
		t.Fatalf("decode auth failed risks: %v", err)
	}
	if risks.Total != 1 || len(risks.Items) != 1 || risks.Items[0].Action != "admin.auth_failed" || risks.Items[0].Kind != "auth_failed" {
		t.Fatalf("expected admin auth failed risk, got %#v", risks)
	}
}

func TestRootAdminSecretAuditIdentity(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants", bytes.NewBufferString(`{"name":"Root Secret Tenant"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("root secret create tenant status = %d body = %s", w.Code, w.Body.String())
	}

	events, err := svc.ListAuditEvents(req.Context(), agentservice.ListAuditEventsInput{Action: "admin.tenant_created", ActorType: "admin"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("tenant create audit events = %d, want 1: %#v", len(events), events)
	}
	got := events[0]
	if got.ActorUser != "" {
		t.Fatalf("root admin secret audit should not have admin actor user, got %q", got.ActorUser)
	}
	if got.Metadata["auth_type"] != "admin_secret" {
		t.Fatalf("root admin secret audit auth_type = %q metadata=%#v", got.Metadata["auth_type"], got.Metadata)
	}
	if got.Metadata["admin_user_id"] != "" || got.Metadata["admin_username"] != "" || got.Metadata["admin_role"] != "" {
		t.Fatalf("root admin secret audit should not include session admin identity: %#v", got.Metadata)
	}
}
