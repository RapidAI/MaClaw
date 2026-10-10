package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type countingOwnerLLM struct {
	mu    sync.Mutex
	calls int
	url   string
}

func (c *countingOwnerLLM) IssueViewerTokenForUser(_ context.Context, userID string) (string, error) {
	if userID != "alice" {
		return "", fmt.Errorf("unexpected user %s", userID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return fmt.Sprintf("viewer-token-%d", c.calls), nil
}

func (c *countingOwnerLLM) callsN() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *countingOwnerLLM) PublicLLMBaseURL(context.Context) string { return c.url }

func TestBotMessageUsesOwnerViewerTokenOnSystemFree(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("MACLAW_BOT_LOG_DIR", logDir)
	const endpoint = "https://hub.example/api/llm/v1"
	var (
		mu        sync.Mutex
		patches   []string
		configs   []map[string]any
		adminSeen string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			patches = append(patches, string(raw))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users/user_alice/config":
			_, _ = w.Write([]byte(`{"app_config":{"language":"zh","maclaw_llm_key":"old-key"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users/user_alice/config":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				AppConfig map[string]any `json:"app_config"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("config body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			configs = append(configs, body.AppConfig)
			adminSeen = r.Header.Get("X-MaClaw-Admin-Secret")
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	issuer := &countingOwnerLLM{url: endpoint}
	now := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.Now = func() time.Time { return now }
	svc.OwnerLLM = issuer
	ctx := context.Background()

	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "again"); err != nil {
		t.Fatal(err)
	}
	if issuer.callsN() != 1 {
		t.Fatalf("issuer calls = %d", issuer.callsN())
	}
	mu.Lock()
	putsAfterReuse := len(configs)
	mu.Unlock()
	if putsAfterReuse != 1 {
		t.Fatalf("reused token rewrote config %d times", putsAfterReuse)
	}
	const moved = "https://hub-moved.example/api/llm/v1"
	issuer.url = moved
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "moved"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(21 * 24 * time.Hour)
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "later"); err != nil {
		t.Fatal(err)
	}
	if issuer.callsN() != 2 {
		t.Fatalf("issuer calls after expiry = %d", issuer.callsN())
	}

	mu.Lock()
	defer mu.Unlock()
	if adminSeen != "admin-secret" {
		t.Fatalf("admin secret header = %q", adminSeen)
	}
	if len(patches) != 4 || len(configs) != 3 {
		t.Fatalf("patches=%d configs=%d", len(patches), len(configs))
	}
	for _, patch := range patches {
		if !strings.Contains(patch, `"llm_service_group_id":"system-free"`) || strings.Contains(patch, "group-1") {
			t.Fatalf("patch = %s", patch)
		}
	}
	want := []struct{ token, url string }{
		{"viewer-token-1", endpoint},
		{"viewer-token-1", moved},
		{"viewer-token-2", moved},
	}
	for i, cfg := range configs {
		if cfg["language"] != "zh" || cfg["maclaw_llm_model"] != "auto" || cfg["maclaw_llm_url"] != want[i].url || cfg["maclaw_llm_key"] != want[i].token || cfg["maclaw_llm_current_provider"] != "hub-llm" {
			t.Fatalf("config %d = %#v", i, cfg)
		}
		providers, _ := cfg["maclaw_llm_providers"].([]any)
		if len(providers) != 1 {
			t.Fatalf("providers %d = %#v", i, cfg["maclaw_llm_providers"])
		}
		item, _ := providers[0].(map[string]any)
		if item["name"] != "hub-llm" || item["model"] != "auto" || item["key"] != want[i].token || item["url"] != want[i].url || item["is_hub_service"] != true {
			t.Fatalf("provider %d = %#v", i, item)
		}
		if strings.HasPrefix(want[i].token, "sk-llmg-") {
			t.Fatal("owner token must not be a service-group api key")
		}
	}

	stored, err := settings.Get(context.Background(), storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	var saved record
	if err := json.Unmarshal([]byte(stored), &saved); err != nil {
		t.Fatal(err)
	}
	principal := findPrincipal(saved.Principals, "alice")
	if principal == nil || principal.LLMViewerToken != "viewer-token-2" || principal.LLMViewerIssuedAt != now.UTC().Format(time.RFC3339) || principal.LLMEndpoint != moved {
		t.Fatalf("principal = %#v", principal)
	}

	logged, err := os.ReadFile(filepath.Join(logDir, "bot_alice.log"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(logged)
	if !strings.Contains(text, "stage=hub.owner_llm") || !strings.Contains(text, "group=system-free") {
		t.Fatalf("bot log missing owner llm\n%s", text)
	}
	if strings.Contains(text, "viewer-token-") || strings.Contains(text, "admin-secret") || strings.Contains(text, "key-alice") || strings.Contains(text, "secret-token") {
		t.Fatalf("bot log leaked a credential\n%s", text)
	}
}

func TestBotMessageWithoutPublicURLDoesNotCallTheModel(t *testing.T) {
	var messages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			messages++
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	issuer := &countingOwnerLLM{}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.OwnerLLM = issuer
	_, err = svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "hello")
	if !errors.Is(err, ErrSrv) || !strings.Contains(err.Error(), "hub public url is not configured") {
		t.Fatalf("err = %v", err)
	}
	if messages != 0 || issuer.callsN() != 0 {
		t.Fatalf("messages=%d issuer=%d", messages, issuer.callsN())
	}
}

func TestOwnerLLMTokenStaysOnProvisionedPrincipal(t *testing.T) {
	const endpoint = "https://hub.example/api/llm/v1"
	var (
		mu      sync.Mutex
		configs int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users/user_alice/config":
			_, _ = w.Write([]byte(`{"app_config":{"language":"zh"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users/user_alice/config":
			mu.Lock()
			configs++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{
			{HubUserID: "alice"},
			{HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice"},
		},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	issuer := &countingOwnerLLM{url: endpoint}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.Now = func() time.Time { return time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC) }
	svc.OwnerLLM = issuer
	ctx := context.Background()
	for _, text := range []string{"hello", "again"} {
		if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", text); err != nil {
			t.Fatal(err)
		}
	}
	if issuer.callsN() != 1 {
		t.Fatalf("issuer calls = %d", issuer.callsN())
	}
	mu.Lock()
	puts := configs
	mu.Unlock()
	if puts != 1 {
		t.Fatalf("config writes = %d", puts)
	}
	stored, err := settings.Get(context.Background(), storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	var saved record
	if err := json.Unmarshal([]byte(stored), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Principals) != 2 || saved.Principals[0].LLMViewerToken != "" || saved.Principals[0].LLMEndpoint != "" {
		t.Fatalf("stub principal = %#v", saved.Principals)
	}
	principal := findPrincipal(saved.Principals, "alice")
	if principal == nil || principal.UserID != "user_alice" || principal.LLMViewerToken != "viewer-token-1" || principal.LLMEndpoint != endpoint {
		t.Fatalf("provisioned principal = %#v", principal)
	}
}

func TestRepeatedMessageSkipsUnchangedInstanceMetadata(t *testing.T) {
	var (
		mu       sync.Mutex
		patches  int
		messages int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			_, _ = w.Write([]byte(`{"id":"inst_alice","metadata":{"hub_bot":"1","hub_user_id":"alice","hub_tenant_id":"tenant-a","llm_service_group_id":"system-free","keep":"yes"}}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			mu.Lock()
			patches++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			messages++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	ctx := context.Background()
	for _, text := range []string{"hello", "again"} {
		if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", text); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if patches != 0 || messages != 2 {
		t.Fatalf("patches=%d messages=%d", patches, messages)
	}
}

func TestOwnerBearerIsReusedUntilItExpires(t *testing.T) {
	var (
		mu        sync.Mutex
		authCalls int
	)
	now := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			mu.Lock()
			authCalls++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","expires_at":"` + now.Add(time.Hour).Format(time.RFC3339) + `","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.Now = func() time.Time { return now }
	ctx := context.Background()
	for _, text := range []string{"hello", "again"} {
		if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", text); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	reused := authCalls
	mu.Unlock()
	if reused != 1 {
		t.Fatalf("auth calls while token is valid = %d", reused)
	}
	now = now.Add(2 * time.Hour)
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "later"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if authCalls != 2 {
		t.Fatalf("auth calls after expiry = %d", authCalls)
	}
}

func TestRejectedOwnerBearerIsRetriedOnce(t *testing.T) {
	var (
		mu           sync.Mutex
		authCalls    int
		gets         int
		messages     int
		retryBearer  string
		postedBearer string
	)
	now := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			mu.Lock()
			authCalls++
			n := authCalls
			mu.Unlock()
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"access_token":"bearer-%d","expires_at":"%s","principal":{"user_id":"user_alice"}}`,
				n, now.Add(time.Hour).Format(time.RFC3339),
			)))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			mu.Lock()
			gets++
			n := gets
			if n > 1 {
				retryBearer = r.Header.Get("Authorization")
			}
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			messages++
			postedBearer = r.Header.Get("Authorization")
			mu.Unlock()
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newOwnerBearerService(t, srv)
	svc.Now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "hello"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if authCalls != 2 || gets != 2 || messages != 1 || retryBearer != "Bearer bearer-2" || postedBearer != "Bearer bearer-2" {
		t.Fatalf("after retry auth=%d gets=%d messages=%d retry=%q posted=%q", authCalls, gets, messages, retryBearer, postedBearer)
	}
	mu.Unlock()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "again"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if authCalls != 2 || messages != 2 {
		t.Fatalf("retried bearer was not reused auth=%d messages=%d", authCalls, messages)
	}
}

func TestRejectedOwnerBearerStopsAfterOneRetry(t *testing.T) {
	var (
		mu        sync.Mutex
		authCalls int
		gets      int
		messages  int
	)
	now := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			mu.Lock()
			authCalls++
			n := authCalls
			mu.Unlock()
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"access_token":"bearer-%d","expires_at":"%s","principal":{"user_id":"user_alice"}}`,
				n, now.Add(time.Hour).Format(time.RFC3339),
			)))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			mu.Lock()
			gets++
			mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			messages++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newOwnerBearerService(t, srv)
	svc.Now = func() time.Time { return now }
	_, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "hello")
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if authCalls != 2 || gets != 2 || messages != 0 {
		t.Fatalf("auth=%d gets=%d messages=%d", authCalls, gets, messages)
	}
}

func newOwnerBearerService(t *testing.T, srv *httptest.Server) *Service {
	t.Helper()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	return svc
}

func TestWipedOwnerLLMConfigIsPushedAgain(t *testing.T) {
	var (
		mu    sync.Mutex
		puts  int
		posts int
		mode  = "ok"
	)
	now := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	incomplete := `{"error":"instance is not ready: user LLM configuration is incomplete"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","expires_at":"` + now.Add(time.Hour).Format(time.RFC3339) + `","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/") && !strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"id":"inst_alice","metadata":{"hub_bot":"1","hub_user_id":"alice","hub_tenant_id":"tenant-a","llm_service_group_id":"system-free"}}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/config"):
			_, _ = w.Write([]byte(`{"app_config":{"language":"zh","maclaw_llm_key":"old-key"}}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/config"):
			mu.Lock()
			puts++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			mu.Lock()
			posts++
			current := mode
			mu.Unlock()
			if current == "once" {
				mu.Lock()
				mode = "ok"
				mu.Unlock()
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(incomplete))
				return
			}
			if current == "always" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(incomplete))
				return
			}
			if current == "mention" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"model said user LLM configuration is incomplete"}`))
				return
			}
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newOwnerBearerService(t, srv)
	svc.Now = func() time.Time { return now }
	svc.OwnerLLM = &countingOwnerLLM{url: "https://hub.example/api/llm/v1"}
	ctx := context.Background()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "again"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if puts != 1 {
		t.Fatalf("puts after reuse = %d", puts)
	}
	mode = "once"
	mu.Unlock()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "wiped"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if puts != 2 || posts != 4 {
		t.Fatalf("after repair puts=%d posts=%d", puts, posts)
	}
	mu.Unlock()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "later"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if puts != 2 {
		t.Fatalf("repair did not stick, puts=%d", puts)
	}
	mode = "always"
	postsBefore := posts
	mu.Unlock()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "stuck"); err == nil || !strings.Contains(err.Error(), "user LLM configuration is incomplete") {
		t.Fatalf("stuck error = %v", err)
	}
	mu.Lock()
	if puts != 3 || posts != postsBefore+2 {
		t.Fatalf("failed repair puts=%d posts=%d before=%d", puts, posts, postsBefore)
	}
	postsBefore = posts
	mu.Unlock()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "stuck-again"); err == nil {
		t.Fatal("expected the incomplete config to keep failing")
	}
	mu.Lock()
	if puts != 3 || posts != postsBefore+1 {
		putsNow, postsNow := puts, posts
		mu.Unlock()
		t.Fatalf("repeated failure rewrote config puts=%d posts=%d before=%d", putsNow, postsNow, postsBefore)
	}
	mode = "ok"
	postsBefore = posts
	mu.Unlock()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "healed"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if puts != 3 || posts != postsBefore+1 {
		t.Fatalf("healed message rewrote config puts=%d posts=%d before=%d", puts, posts, postsBefore)
	}
	mode = "once"
	mu.Unlock()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "wiped-again"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if puts != 4 {
		t.Fatalf("second wipe was not repaired, puts=%d", puts)
	}
	mode = "mention"
	postsBefore = posts
	mu.Unlock()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "mentioned"); err == nil || !strings.Contains(err.Error(), "user LLM configuration is incomplete") {
		t.Fatalf("mention error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if puts != 4 || posts != postsBefore+1 {
		t.Fatalf("non-readiness error retried puts=%d posts=%d before=%d", puts, posts, postsBefore)
	}
}

func TestBotMessagePushesSelectedAnthropicProvider(t *testing.T) {
	const endpoint = "https://hub.example/api/llm/v1"
	var (
		mu      sync.Mutex
		configs []map[string]any
		patches []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			patches = append(patches, string(raw))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users/user_alice/config":
			_, _ = w.Write([]byte(`{"app_config":{"language":"zh","maclaw_llm_protocol":"anthropic"}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users/user_alice/config":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				AppConfig map[string]any `json:"app_config"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("config body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			configs = append(configs, body.AppConfig)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
		LLM: llmSettings{
			Current: "lp_claude0000000001",
			Providers: []llmProvider{{
				ID: "lp_claude0000000001", Name: "Claude", Protocol: "anthropic",
				URL: "https://api.anthropic.com", Key: "sk-ant-secret", Model: "claude-sonnet-4-5",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.OwnerLLM = &countingOwnerLLM{url: endpoint}
	ctx := context.Background()
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "again"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(configs) != 1 {
		t.Fatalf("reused provider rewrote config %d times", len(configs))
	}
	cfg := configs[0]
	mu.Unlock()
	if cfg["language"] != "zh" || cfg["maclaw_llm_current_provider"] != "Claude" || cfg["maclaw_llm_protocol"] != "anthropic" || cfg["maclaw_llm_model"] != "claude-sonnet-4-5" || cfg["maclaw_llm_url"] != "https://api.anthropic.com" || cfg["maclaw_llm_key"] != "sk-ant-secret" {
		t.Fatalf("config = %#v", cfg)
	}
	providers, _ := cfg["maclaw_llm_providers"].([]any)
	if len(providers) != 2 {
		t.Fatalf("providers = %#v", cfg["maclaw_llm_providers"])
	}
	hub, _ := providers[0].(map[string]any)
	extra, _ := providers[1].(map[string]any)
	if hub["name"] != "hub-llm" || hub["model"] != "auto" || hub["url"] != endpoint || hub["is_hub_service"] != true {
		t.Fatalf("hub provider = %#v", hub)
	}
	if extra["name"] != "Claude" || extra["protocol"] != "anthropic" || extra["key"] != "sk-ant-secret" || extra["model"] != "claude-sonnet-4-5" {
		t.Fatalf("extra provider = %#v", extra)
	}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
		ID: "lp_claude0000000001", Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "back"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(configs) != 2 {
		t.Fatalf("configs = %d", len(configs))
	}
	back := configs[1]
	if _, ok := back["maclaw_llm_protocol"]; ok || back["maclaw_llm_current_provider"] != "hub-llm" || back["maclaw_llm_model"] != "auto" || back["maclaw_llm_url"] != endpoint {
		t.Fatalf("system-free config = %#v", back)
	}
	backProviders, _ := back["maclaw_llm_providers"].([]any)
	if len(backProviders) != 2 {
		t.Fatalf("system-free providers = %#v", back["maclaw_llm_providers"])
	}
	if len(patches) != 3 {
		t.Fatalf("patches = %d", len(patches))
	}
	for _, patch := range patches {
		if !strings.Contains(patch, `"llm_service_group_id":"system-free"`) {
			t.Fatalf("patch = %s", patch)
		}
	}
}
