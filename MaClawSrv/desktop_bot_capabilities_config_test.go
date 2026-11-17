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

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// Synthetic markers only. These tests must not embed a live credential.
const (
	managedCapabilitySecret = "synthetic-cap-secret"
	managedCapabilityEnv    = "synthetic-env-value"
	managedCapabilityBody   = "synthetic skill text"
	forgedCapabilitySecret  = "forged-cap-secret"
	forgedCapabilityBody    = "forged skill text"
)

func managedCapabilityDocument() json.RawMessage {
	return json.RawMessage(`{"mcp":[{"id":"search","name":"Search","transport":"http","url":"https://mcp.example/mcp","auth_secret":"` + managedCapabilitySecret + `","headers":{"X-Test":"` + managedCapabilitySecret + `"}},{"id":"files","name":"Files","transport":"stdio","command":"npx","args":["-y","pkg"],"env":{"TOKEN":"` + managedCapabilityEnv + `"}}],"skills":[{"name":"draft","description":"save a draft","body":"` + managedCapabilityBody + `"}]}`)
}

func forgedCapabilityDocument() string {
	return `{"mcp":[{"id":"evil","name":"Evil","transport":"http","url":"https://evil.example/mcp","auth_secret":"` + forgedCapabilitySecret + `"}],"skills":[{"name":"forged","body":"` + forgedCapabilityBody + `"}]}`
}

func TestDesktopBotCapabilitiesUserConfigPreservesManagedDocument(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenantA, userA := createConfigTenantUser(t, svc, "Tenant A", "User A")
	tenantB, userB := createConfigTenantUser(t, svc, "Tenant B", "User B")
	principalA := agentservice.Principal{TenantID: tenantA.ID, UserID: userA.ID}
	principalB := agentservice.Principal{TenantID: tenantB.ID, UserID: userB.ID}
	doc := managedCapabilityDocument()
	other := json.RawMessage(`{"skills":[{"name":"other","body":"other synthetic skill text"}]}`)
	seedManagedCapabilities(t, svc, principalA, doc, 3)
	seedManagedCapabilities(t, svc, principalB, other, 1)
	tokenA := issueConfigUserToken(t, principalA)
	server := NewHTTPServer(svc, "admin-secret", nil)

	t.Run("user get and schema hide the document", func(t *testing.T) {
		body := configHTTP(t, server, http.MethodGet, "/api/v1/config", tokenA, "", http.StatusOK)
		assertNoManagedCapabilityLeak(t, body)
		if !strings.Contains(body, `"memory_max_backups":3`) {
			t.Fatal("user config response dropped an ordinary setting")
		}
		schema := configHTTP(t, server, http.MethodGet, "/api/v1/config/schema", tokenA, "", http.StatusOK)
		if strings.Contains(schema, "desktop_bot_capabilities") || strings.Contains(schema, managedCapabilitySecret) || strings.Contains(schema, managedCapabilityBody) {
			t.Fatal("user schema exposed the managed capability field")
		}
		if !strings.Contains(schema, `"key":"memory_max_backups"`) && !strings.Contains(schema, `"key": "memory_max_backups"`) {
			t.Fatal("user schema dropped an ordinary setting")
		}
	})

	t.Run("user put omission forgery and null keep the document", func(t *testing.T) {
		for _, body := range []string{
			`{"app_config":{"memory_max_backups":8}}`,
			`{"app_config":{"memory_max_backups":6,"desktop_bot_capabilities":null}}`,
			`{"app_config":{"memory_max_backups":9,"desktop_bot_capabilities":{}}}`,
			`{"app_config":{"memory_max_backups":7,"desktop_bot_capabilities":` + forgedCapabilityDocument() + `}}`,
		} {
			response := configHTTP(t, server, http.MethodPut, "/api/v1/config", tokenA, body, http.StatusOK)
			assertNoManagedCapabilityLeak(t, response)
		}
		raw := rawCapabilityDocument(t, svc, principalA)
		if !bytes.Equal(raw, doc) {
			t.Fatalf("user config write changed the managed document (%d bytes, want %d)", len(raw), len(doc))
		}
		if got := rawMemoryBackups(t, svc, principalA); got != 7 {
			t.Fatalf("last ordinary setting was not saved: memory_max_backups=%d", got)
		}
		if !bytes.Equal(rawCapabilityDocument(t, svc, principalB), other) {
			t.Fatal("user A config write changed tenant B")
		}
	})

	t.Run("user without a document cannot inject one", func(t *testing.T) {
		_, fresh := createConfigTenantUser(t, svc, "Tenant Fresh", "User Fresh")
		principal := agentservice.Principal{TenantID: fresh.TenantID, UserID: fresh.ID}
		token := issueConfigUserToken(t, principal)
		response := configHTTP(t, server, http.MethodPut, "/api/v1/config", token, `{"app_config":{"memory_max_backups":4,"desktop_bot_capabilities":`+forgedCapabilityDocument()+`}}`, http.StatusOK)
		assertNoManagedCapabilityLeak(t, response)
		if raw := rawCapabilityDocument(t, svc, principal); len(bytes.TrimSpace(raw)) != 0 {
			t.Fatalf("first user save stored a forged document (%d bytes)", len(raw))
		}
		if got := rawMemoryBackups(t, svc, principal); got != 4 {
			t.Fatalf("first user save lost the ordinary setting: memory_max_backups=%d", got)
		}
	})

	t.Run("default client config cannot replace the document", func(t *testing.T) {
		configHTTP(t, server, http.MethodPut, "/api/v1/admin/client-config/default", "", `{"app_config":{"language":"zh-CN","desktop_bot_capabilities":`+forgedCapabilityDocument()+`}}`, http.StatusOK)
		shared, err := svc.GetDefaultClientConfig(context.Background())
		if err != nil {
			t.Fatalf("GetDefaultClientConfig: %v", err)
		}
		if len(bytes.TrimSpace(shared.AppConfig.DesktopBotCapabilities)) != 0 {
			t.Fatal("default client config stored managed bot capabilities")
		}
		visible, err := svc.GetUserConfig(context.Background(), principalA)
		if err != nil {
			t.Fatalf("GetUserConfig: %v", err)
		}
		if !bytes.Equal(visible.AppConfig.DesktopBotCapabilities, doc) {
			t.Fatal("shared-client overlay replaced the managed document")
		}
		response := configHTTP(t, server, http.MethodPut, "/api/v1/config", tokenA, `{"app_config":{"memory_max_backups":11}}`, http.StatusOK)
		assertNoManagedCapabilityLeak(t, response)
		if !bytes.Equal(rawCapabilityDocument(t, svc, principalA), doc) {
			t.Fatal("user save after a default-client update changed the managed document")
		}
		if got := rawMemoryBackups(t, svc, principalA); got != 11 {
			t.Fatalf("ordinary setting after default-client update = %d", got)
		}
	})

	t.Run("admin update is scoped and can replace or clear", func(t *testing.T) {
		pathA := "/api/v1/admin/tenants/" + tenantA.ID + "/users/" + userA.ID + "/config"
		adminBody := configHTTP(t, server, http.MethodGet, pathA, "", "", http.StatusOK)
		if !strings.Contains(adminBody, managedCapabilitySecret) || !strings.Contains(adminBody, managedCapabilityBody) || !strings.Contains(adminBody, managedCapabilityEnv) {
			t.Fatal("admin config read dropped the managed document Hub synchronizes")
		}
		schema := configHTTP(t, server, http.MethodGet, pathA+"/schema", "", "", http.StatusOK)
		if !strings.Contains(schema, "desktop_bot_capabilities") {
			t.Fatal("admin schema hid the field Hub writes")
		}
		replaced := `{"mcp":[{"id":"search","name":"Search","transport":"http","url":"https://mcp.example/mcp","auth_secret":"synthetic-cap-secret-2"}],"skills":[{"name":"draft","body":"synthetic skill text two"}]}`
		configHTTP(t, server, http.MethodPut, pathA, "", `{"app_config":{"memory_max_backups":12,"maclaw_llm_url":"https://llm.example/v1","maclaw_llm_key":"synthetic-llm-key","maclaw_llm_model":"synthetic-model","desktop_bot_capabilities":`+replaced+`}}`, http.StatusOK)
		raw := rawCapabilityDocument(t, svc, principalA)
		if !bytes.Contains(raw, []byte("synthetic-cap-secret-2")) || bytes.Contains(raw, []byte(forgedCapabilitySecret)) {
			t.Fatal("admin update did not replace the managed document")
		}
		if got := rawMemoryBackups(t, svc, principalA); got != 12 {
			t.Fatalf("admin update lost the ordinary setting: memory_max_backups=%d", got)
		}
		if key := rawLLMKey(t, svc, principalA); key != "synthetic-llm-key" {
			t.Fatal("admin update did not keep the supplied LLM credential")
		}
		configHTTP(t, server, http.MethodPut, "/api/v1/admin/tenants/"+tenantA.ID+"/users/"+userB.ID+"/config", "", `{"app_config":{"memory_max_backups":99,"desktop_bot_capabilities":`+forgedCapabilityDocument()+`}}`, http.StatusNotFound)
		if !bytes.Equal(rawCapabilityDocument(t, svc, principalB), other) || rawMemoryBackups(t, svc, principalB) != 1 {
			t.Fatal("admin path for another tenant's user id changed tenant B")
		}
		configHTTP(t, server, http.MethodPut, "/api/v1/admin/tenants/"+tenantB.ID+"/users/"+userB.ID+"/config", "", `{"app_config":{"memory_max_backups":2}}`, http.StatusOK)
		if len(bytes.TrimSpace(rawCapabilityDocument(t, svc, principalB))) != 0 {
			t.Fatal("admin replace that omits the field did not clear it")
		}
		if !bytes.Contains(rawCapabilityDocument(t, svc, principalA), []byte("synthetic-cap-secret-2")) {
			t.Fatal("clearing tenant B cleared tenant A")
		}
		if got := rawMemoryBackups(t, svc, principalB); got != 2 {
			t.Fatalf("admin clear lost the ordinary setting: memory_max_backups=%d", got)
		}
	})
}

func createConfigTenantUser(t *testing.T, svc *agentservice.Service, tenantName, userName string) (*agentservice.Tenant, *agentservice.User) {
	t.Helper()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: tenantName})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: userName})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return tenant, user
}

func seedManagedCapabilities(t *testing.T, svc *agentservice.Service, p agentservice.Principal, doc json.RawMessage, backups int) {
	t.Helper()
	if _, err := svc.UpdateUserConfig(context.Background(), p, corelib.AppConfig{
		MaclawLLMUrl:           "https://llm.example/v1",
		MaclawLLMKey:           "synthetic-llm-key",
		MaclawLLMModel:         "synthetic-model",
		MemoryMaxBackups:       backups,
		DesktopBotCapabilities: append(json.RawMessage(nil), doc...),
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func issueConfigUserToken(t *testing.T, p agentservice.Principal) string {
	t.Helper()
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(p)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	return token
}

func configHTTP(t *testing.T, server *HTTPServer, method, path, bearer, body string, want int) string {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	} else if strings.Contains(path, "/admin/") {
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("%s %s status=%d want=%d", method, path, w.Code, want)
	}
	return w.Body.String()
}

func assertNoManagedCapabilityLeak(t *testing.T, body string) {
	t.Helper()
	for _, marker := range []string{
		"desktop_bot_capabilities",
		managedCapabilitySecret,
		managedCapabilityEnv,
		managedCapabilityBody,
		forgedCapabilitySecret,
		forgedCapabilityBody,
		"synthetic-cap-secret-2",
		"synthetic skill text two",
	} {
		if strings.Contains(body, marker) {
			t.Fatalf("ordinary config response exposed %q", marker)
		}
	}
}

func rawCapabilityDocument(t *testing.T, svc *agentservice.Service, p agentservice.Principal) json.RawMessage {
	t.Helper()
	raw, err := svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	return append(json.RawMessage(nil), raw.AppConfig.DesktopBotCapabilities...)
}

func rawMemoryBackups(t *testing.T, svc *agentservice.Service, p agentservice.Principal) int {
	t.Helper()
	raw, err := svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	return raw.AppConfig.MemoryMaxBackups
}

func rawLLMKey(t *testing.T, svc *agentservice.Service, p agentservice.Principal) string {
	t.Helper()
	raw, err := svc.GetRawUserConfig(context.Background(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	return raw.AppConfig.MaclawLLMKey
}
