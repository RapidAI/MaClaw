package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestWebAccessTokenRefreshExtendsExpiry(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789012345",
		TokenTTL:    2 * time.Second,
	}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
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
	credExp := time.Now().UTC().Add(15 * time.Minute)
	cred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{
		TenantID:  tenant.ID,
		UserID:    user.ID,
		Name:      "web launch",
		ExpiresAt: &credExp,
	})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	issued, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: cred.APIKey, APISecret: cred.APISecret})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	originalCredentialExpiry := *cred.ExpiresAt
	time.Sleep(50 * time.Millisecond)
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/web/refresh", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+issued.AccessToken)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	if out.AccessToken == "" || out.AccessToken == issued.AccessToken {
		t.Fatalf("refresh token = %#v original=%q", out, issued.AccessToken)
	}
	if !out.ExpiresAt.After(issued.ExpiresAt) {
		t.Fatalf("refresh expiry %s should be after %s", out.ExpiresAt, issued.ExpiresAt)
	}
	refreshedCred, err := svc.GetCredential(context.Background(), tenant.ID, user.ID, cred.ID)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if refreshedCred.ExpiresAt == nil || !refreshedCred.ExpiresAt.After(originalCredentialExpiry) {
		t.Fatalf("credential expiry was not extended: before=%s after=%v", originalCredentialExpiry, refreshedCred.ExpiresAt)
	}
	principal, err := svc.Authenticate(out.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate refreshed token: %v", err)
	}
	if principal.TenantID != tenant.ID || principal.UserID != user.ID {
		t.Fatalf("principal mismatch: %#v", principal)
	}
}
