package httpapi

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudagent"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	_ "modernc.org/sqlite"
)

// TestCloudAgentLLMAdmitsOwnerThroughResponses drives the shipped loopback
// route into LLMV1ResponsesHandler. The only double is the upstream HTTP
// body. A request without the owner principal still takes the API-key path.
func TestCloudAgentLLMAdmitsOwnerThroughResponses(t *testing.T) {
	db, err := sql.Open("sqlite", "file:cloud_agent_llm?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	const (
		tenant = "tenant-a"
		userID = "user-a"
		email  = "owner@example.com"
		agent  = "cag_0123456789abcdef0123456789abcdef"
		secret = "sec-1"
	)
	settings := streamSettings{
		cloudagent.SrvSettingsKey(tenant): `{"base_url":"http://127.0.0.1:9","admin_secret":"` + secret + `"}`,
	}
	svc := cloudagent.New(db, settings)
	if svc == nil {
		t.Fatal("cloud agent service did not open")
	}
	if _, err := db.Exec(`INSERT INTO cloud_agents (
		id, tenant_id, owner_user_id, name, kind, llm_mode, llm_label, llm_model, status, cloud_workspace_id, created_at, updated_at, deleted_at
	) VALUES (?, ?, ?, '会话', 'session', 'official', '', '', 'idle', '', 't', 't', '')`, agent, tenant, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id TEXT, tenant_id TEXT, email TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, tenant_id, email) VALUES (?, ?, ?)`, userID, tenant, email); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var upstreamBodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		upstreamBodies = append(upstreamBodies, string(raw))
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "test-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "owner-admitted"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
		})
	}))
	t.Cleanup(upstream.Close)

	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	invalidateLLMRuntimeCaches(system)
	tenantSystem := scopedSystemSettingsForTenant(tenant, system)
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: email, ServiceGroupIDs: []string{"coding-basic"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, tenantSystem, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID: "provider-a", APIURL: upstream.URL, Model: "test-model",
	}}}); err != nil {
		t.Fatal(err)
	}
	invalidateLLMRuntimeCaches(system)

	handler := cloudAgentLLMChat(db, svc, nil, system, nil)
	body := `{"model":"auto","input":"cloud-owner-hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/llm/chat", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", secret)
	req.Header.Set("X-MaClaw-Cloud-Agent", agent)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("owner admission rejected: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("responses handler: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Viewer authentication failed") {
		t.Fatalf("fell through to API-key auth: %s", rec.Body.String())
	}
	mu.Lock()
	got := append([]string(nil), upstreamBodies...)
	mu.Unlock()
	if len(got) == 0 || !strings.Contains(got[0], "cloud-owner-hello") {
		t.Fatalf("upstream bodies = %#v", got)
	}

	bare := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", strings.NewReader(body))
	bareRec := httptest.NewRecorder()
	LLMV1ResponsesHandler(nil, system, nil).ServeHTTP(bareRec, bare)
	if bareRec.Code != http.StatusUnauthorized {
		t.Fatalf("missing owner context: %d %s", bareRec.Code, bareRec.Body.String())
	}

	forced := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", strings.NewReader(body))
	forced = forced.WithContext(withCloudAgentOwner(forced.Context(), &auth.ViewerPrincipal{
		TenantID: tenant, UserID: userID, Email: email, Kind: auth.ViewerKindLLMEndpointAPIKey,
	}))
	forcedRec := httptest.NewRecorder()
	LLMV1ResponsesHandler(nil, system, nil).ServeHTTP(forcedRec, forced)
	if forcedRec.Code != http.StatusForbidden {
		t.Fatalf("api-key principal: %d %s", forcedRec.Code, forcedRec.Body.String())
	}

	noHostSettings := streamSettings{}
	noHost := cloudagent.New(db, noHostSettings)
	noHostHandler := cloudAgentLLMChat(db, noHost, nil, system, nil)
	miss := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/llm/chat", strings.NewReader(body))
	miss.RemoteAddr = "127.0.0.1:1"
	miss.Header.Set("X-MaClaw-Admin-Secret", secret)
	miss.Header.Set("X-MaClaw-Cloud-Agent", agent)
	missRec := httptest.NewRecorder()
	noHostHandler.ServeHTTP(missRec, miss)
	if missRec.Code != http.StatusUnauthorized {
		t.Fatalf("unconfigured maclawsrv: %d %s", missRec.Code, missRec.Body.String())
	}
}
