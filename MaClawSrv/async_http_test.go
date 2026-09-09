package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestPostMessagePreferRespondAsync(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789012345",
	}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Async"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	t.Cleanup(server.Close)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/messages", bytes.NewBufferString(`{"content":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Prefer", "return=representation, RESPOND-ASYNC; wait=0")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Preference-Applied"); got != "respond-async" {
		t.Fatalf("Preference-Applied = %q", got)
	}
	var body struct {
		Async     bool              `json:"async"`
		Run       *agentservice.Run `json:"run"`
		StatusURL string            `json:"status_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Async || body.Run == nil || body.Run.ID == "" {
		t.Fatalf("invalid async response: %#v", body)
	}
	wantURL := "/api/v1/instances/" + inst.ID + "/runs/" + body.Run.ID
	if body.StatusURL != wantURL || !strings.HasPrefix(body.StatusURL, "/api/v1/instances/") {
		t.Fatalf("status_url = %q, want %q", body.StatusURL, wantURL)
	}
	if got := w.Header().Get("Location"); got != wantURL {
		t.Fatalf("Location = %q, want %q", got, wantURL)
	}

	// The convenience endpoint exposes the same async contract while resolving
	// the target session on the service side.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages?async=true", bytes.NewBufferString(`{"session_id":"`+sess.ID+`","content":"hello again"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("convenience async status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestPrefersAsyncResponseParsesRFC7240Tokens(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   bool
	}{
		{name: "single", values: []string{"respond-async"}, want: true},
		{name: "combined", values: []string{"return=representation, RESPOND-ASYNC; wait=0"}, want: true},
		{name: "separate headers", values: []string{"return=representation", "respond-async"}, want: true},
		{name: "other preference", values: []string{"wait=5"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prefersAsyncResponse(tc.values); got != tc.want {
				t.Fatalf("prefersAsyncResponse(%v) = %v, want %v", tc.values, got, tc.want)
			}
		})
	}
	badReq := httptest.NewRequest(http.MethodPost, "/?async=maybe", nil)
	if _, err := wantsAsyncResponse(badReq); err == nil {
		t.Fatal("expected invalid async query to fail")
	}
}
