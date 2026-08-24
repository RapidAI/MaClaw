package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestDynamicSkillCapabilityPublicationRequiresAdminOwnerConfirmAndObservedBinding(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(dataRoot, "tenants", tenant.ID, "users", user.ID, "skills", "lookup")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("id: acme.lookup\nname: lookup\nversion: v1\ndescription: untrusted description\nsteps:\n  - action: message\n    content: ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	defer server.Close()
	path := "/api/v1/admin/tenants/" + tenant.ID + "/users/" + user.ID + "/dynamic-capabilities/skills/acme.lookup"
	body := map[string]any{
		"provisions": []any{map[string]any{"capability": "information.lookup", "qualifiers": map[string]string{"scope": "reference"}, "quality": 1}},
		"effects":    []string{"read_only"},
	}
	request := dynamicCapabilityPublicationHTTPRequest(t, path, body, "")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", response.Code, response.Body.String())
	}

	request = dynamicCapabilityPublicationHTTPRequest(t, path, body, "admin-secret")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed status=%d body=%s", response.Code, response.Body.String())
	}

	body["confirm"] = true
	request = dynamicCapabilityPublicationHTTPRequest(t, path, body, "admin-secret")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("publication status=%d body=%s", response.Code, response.Body.String())
	}
	var published map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published["contract_digest"] == "" || published["observed_binding_digest"] == "" {
		t.Fatalf("publication exposed missing digest fields: %#v", published)
	}
	p := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	contract, ok := svc.DynamicCapabilityContracts().ResolveSkillDynamicContract(context.Background(), p, "acme.lookup")
	if !ok || contract.ObservedBindingDigest == "" {
		t.Fatalf("published contract=%#v found=%v", contract, ok)
	}
	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{TenantID: tenant.ID, UserID: user.ID, Action: "admin.dynamic_capability.skill_published"})
	if err != nil || len(events) != 1 {
		t.Fatalf("audit events=%#v err=%v", events, err)
	}
	for _, key := range []string{"registry_version", "contract_digest", "observed_binding_digest"} {
		if events[0].Metadata[key] == "" {
			t.Fatalf("audit missing %s: %#v", key, events[0].Metadata)
		}
	}
}

func TestDynamicCapabilityPublicationFailsClosedForMissingObservedBinding(t *testing.T) {
	_, _, _, server := newMCPAuthenticatedServer(t)
	defer server.Close()
	request := dynamicCapabilityPublicationHTTPRequest(t, "/api/v1/admin/tenants/tenant/users/user/dynamic-capabilities/skills/missing", map[string]any{
		"confirm":    true,
		"provisions": []any{map[string]any{"capability": "information.lookup", "qualifiers": map[string]string{"scope": "reference"}, "quality": 1}},
		"effects":    []string{"read_only"},
	}, "admin-secret")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code == http.StatusCreated {
		t.Fatalf("missing observed binding was published: %s", response.Body.String())
	}
}

func dynamicCapabilityPublicationHTTPRequest(t *testing.T, path string, payload map[string]any, adminSecret string) *http.Request {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	if adminSecret != "" {
		request.Header.Set("X-MaClaw-Admin-Secret", adminSecret)
	}
	return request
}
