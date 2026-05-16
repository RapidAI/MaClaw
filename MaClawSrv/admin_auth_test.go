package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
