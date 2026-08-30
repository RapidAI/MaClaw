package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type proxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f proxyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type lockedResponseRecorder struct {
	mu     sync.Mutex
	header http.Header
	code   int
	body   bytes.Buffer
}

func newLockedResponseRecorder() *lockedResponseRecorder {
	return &lockedResponseRecorder{header: http.Header{}, code: http.StatusOK}
}

func (r *lockedResponseRecorder) Header() http.Header {
	return r.header
}

func (r *lockedResponseRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.code = code
}

func (r *lockedResponseRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(p)
}

func (r *lockedResponseRecorder) Flush() {}

func (r *lockedResponseRecorder) BodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

type recordingUsageRecorder struct {
	records  []*llmpool.UsageRecord
	contexts []usageContextData
}

func (r *recordingUsageRecorder) RecordUsage(ctx context.Context, record *llmpool.UsageRecord) error {
	hubID, tenantID := usageContextValues(ctx)
	r.records = append(r.records, record)
	r.contexts = append(r.contexts, usageContextData{HubID: hubID, TenantID: tenantID})
	return nil
}

// --- mock auth repo ---

type mockAuthRepo struct {
	auths []*TenantAuthorization
}

func (r *mockAuthRepo) Create(_ context.Context, auth *TenantAuthorization) error {
	r.auths = append(r.auths, auth)
	return nil
}
func (r *mockAuthRepo) GetByID(_ context.Context, id string) (*TenantAuthorization, error) {
	for _, a := range r.auths {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, nil
}

func (r *mockAuthRepo) GetByCardOrderID(_ context.Context, orderNo string) (*TenantAuthorization, error) {
	for _, auth := range r.auths {
		if auth != nil && auth.CardOrderID == orderNo {
			return auth, nil
		}
	}
	return nil, nil
}
func (r *mockAuthRepo) ListByHubTenant(_ context.Context, hubID, tenantID string) ([]*TenantAuthorization, error) {
	var result []*TenantAuthorization
	for _, a := range r.auths {
		if a.HubID == hubID && a.TenantID == tenantID {
			result = append(result, a)
		}
	}
	return result, nil
}
func (r *mockAuthRepo) ListAll(_ context.Context) ([]*TenantAuthorization, error) {
	return r.auths, nil
}
func (r *mockAuthRepo) ListByServiceGroup(_ context.Context, serviceGroupID string) ([]*TenantAuthorization, error) {
	var result []*TenantAuthorization
	for _, a := range r.auths {
		if a.ServiceGroupID == serviceGroupID {
			result = append(result, a)
		}
	}
	return result, nil
}
func (r *mockAuthRepo) Update(_ context.Context, auth *TenantAuthorization) error { return nil }
func (r *mockAuthRepo) DeductCredits(_ context.Context, id string, credits float64, _ time.Time) (float64, error) {
	for _, a := range r.auths {
		if a.ID == id {
			available := a.CreditsRemaining()
			if available <= 0 {
				return 0, nil
			}
			actual := credits
			if available < actual {
				actual = available
			}
			a.CreditsUsed += actual
			return actual, nil
		}
	}
	return 0, nil
}

// --- mock system settings ---

type mockSystemSettings struct {
	data map[string]string
}

func (s *mockSystemSettings) Set(_ context.Context, key, val string) error {
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = val
	return nil
}
func (s *mockSystemSettings) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}
func (s *mockSystemSettings) List(_ context.Context) ([]*store.SystemSettingEntry, error) {
	return nil, nil
}

// --- tests ---

func TestHandleProxyRequestRequiresRequestAndBody(t *testing.T) {
	cfg := &ProxyConfig{
		Service:     NewService(&mockSystemSettings{}),
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, nil); err == nil || err.Error() != "proxy request is required" {
		t.Fatalf("nil request error = %v, want proxy request is required", err)
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{}); err == nil || err.Error() != "proxy request body is required" {
		t.Fatalf("nil body error = %v, want proxy request body is required", err)
	}
}

func TestHandleProxyRequestReturnsTenantBoundError(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://127.0.0.1:1"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", AccessPolicy: AccessPolicyFree, Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		CheckBinding: func(context.Context, string, string) (bool, string) {
			return false, "hc-3"
		},
		LookupNodeURL: func(nodeID string) string {
			if nodeID == "hc-3" {
				return "https://hubs2.maclaw.top"
			}
			return ""
		},
	}
	_, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}})
	bound := asTenantBoundError(err)
	if bound == nil {
		t.Fatalf("error = %v, want TenantBoundError", err)
	}
	if bound.NodeID != "hc-3" || bound.RedirectURL != "https://hubs2.maclaw.top" {
		t.Fatalf("bound = %+v", bound)
	}
}

func TestProxyHandlerWritesStructuredTenantRedirect(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://127.0.0.1:1"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", AccessPolicy: AccessPolicyFree, Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		CheckBinding: func(context.Context, string, string) (bool, string) {
			return false, "hc-3"
		},
		LookupNodeURL: func(nodeID string) string {
			if nodeID == "hc-3" {
				return "https://hubs2.maclaw.top"
			}
			return ""
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4"}`))
	req.Header.Set("X-Hub-ID", "hub1")
	req.Header.Set("X-Tenant-ID", "t1")
	rr := httptest.NewRecorder()
	ProxyHandler(cfg).ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get(RedirectNodeHeader); got != "hc-3" {
		t.Fatalf("redirect node = %q", got)
	}
	if got := rr.Header().Get(RedirectURLHeader); got != "https://hubs2.maclaw.top" {
		t.Fatalf("redirect url = %q", got)
	}
	if got := rr.Header().Get("Location"); got != "https://hubs2.maclaw.top" {
		t.Fatalf("Location = %q", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload["code"] != TenantBoundErrorCode || payload["node_id"] != "hc-3" || payload["redirect_url"] != "https://hubs2.maclaw.top" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestClientFacingRedirectURLRejectsUnsafeAddresses(t *testing.T) {
	if got := clientFacingRedirectURL("https://hubs2.maclaw.top"); got != "https://hubs2.maclaw.top" {
		t.Fatalf("got %q", got)
	}
	if got := clientFacingRedirectURL("https://evil:pass@hubs2.maclaw.top"); got != "" {
		t.Fatalf("userinfo should be rejected, got %q", got)
	}
	if got := clientFacingRedirectURL("javascript:alert(1)"); got != "" {
		t.Fatalf("javascript should be rejected, got %q", got)
	}
}

func TestProxyHandlerStreamPreflightReturnsBindingRedirectBeforeSSE(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://127.0.0.1:1"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", AccessPolicy: AccessPolicyFree, Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		CheckBinding: func(context.Context, string, string) (bool, string) {
			return false, "hc-3"
		},
		LookupNodeURL: func(string) string { return "https://hubs2.maclaw.top" },
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4","stream":true}`))
	req.Header.Set("X-Hub-ID", "hub1")
	req.Header.Set("X-Tenant-ID", "t1")
	rr := httptest.NewRecorder()
	ProxyHandler(cfg).ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("Content-Type = %q, want JSON error before SSE starts", rr.Header().Get("Content-Type"))
	}
	if rr.Header().Get(RedirectURLHeader) != "https://hubs2.maclaw.top" {
		t.Fatalf("redirect url = %q", rr.Header().Get(RedirectURLHeader))
	}
}

func TestHandleProxyRequestTrimsModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "  gpt-4  "}}); err != nil {
		t.Fatalf("trimmed body model should match: %v", err)
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Model: "   ", Body: map[string]any{}}); err == nil || err.Error() != "model not specified in request" {
		t.Fatalf("blank explicit model error = %v, want model not specified in request", err)
	}
}

func TestHandleProxyRequestSkipsPausedProvider(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "paused", Name: "Paused", APIURL: "http://paused.invalid", Paused: true},
			{ID: "live", Name: "Live", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "paused"},
				{ProviderID: "live"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "gpt-4"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "live" {
		t.Fatalf("provider = %#v, want live after skipping paused", resp)
	}
}

func TestHandleProxyRequestFailsOverToLiveProviderInSameGroup(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: "http://paused.invalid", Paused: true},
			{ID: "yinyu", Name: "Yinyu", APIURL: upstream.URL, Models: []string{"qwen-plus"}},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "official", Name: "Official",
			Models: []llmpool.ModelConfig{
				{Name: "deepseek-chat", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}},
				{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}},
			},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "deepseek-chat"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want yinyu after pause failover", resp)
	}
	if seen["model"] != "qwen-plus" {
		t.Fatalf("upstream model = %#v, want live provider route qwen-plus", seen["model"])
	}
}

func TestHandleProxyRequestFailsOverAutoToLiveProviderInOtherGroup(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: "http://paused.invalid", Paused: true},
			{ID: "yinyu", Name: "Yinyu", APIURL: upstream.URL, Models: []string{"qwen-plus"}},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID: "official-pool", Name: "Official", AgentID: "maclaw_official",
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}}},
			},
			{
				ID: "qwen-pool", Name: "Qwen", AgentID: "maclaw_official",
				Models: []llmpool.ModelConfig{{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		ServiceGroupID: "official-pool",
		Body:           map[string]any{"model": "auto"},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want yinyu after pausing auto's only provider", resp)
	}
	if seen["model"] != "qwen-plus" {
		t.Fatalf("upstream model = %#v, want live provider route qwen-plus", seen["model"])
	}
}

func TestHandleProxyRequestFailsOverPausedProviderUsingRegistryOnlyBackend(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: "http://paused.invalid", Paused: true},
			{ID: "yinyu", Name: "Yinyu", APIURL: upstream.URL, Models: []string{"qwen-plus"}},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "official-pool", Name: "Official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want registry live provider yinyu", resp)
	}
	if seen["model"] != "qwen-plus" {
		t.Fatalf("upstream model = %#v, want provider default qwen-plus", seen["model"])
	}
}

func TestHandleProxyRequestKeepsHealthyPrimaryAheadOfLowerSequenceFailover(t *testing.T) {
	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "deepseek")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"primary"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer primary.Close()
	failover := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "yinyu")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"failover"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer failover.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "yinyu", Name: "Yinyu", APIURL: failover.URL, Sequence: 1, Models: []string{"qwen-plus"}},
			{ID: "deepseek", Name: "DeepSeek", APIURL: primary.URL, Sequence: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID: "official-pool", Name: "Official", AgentID: "maclaw_official",
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}}},
			},
			{
				ID: "qwen-pool", Name: "Qwen", AgentID: "maclaw_official",
				Models: []llmpool.ModelConfig{{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	resp, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
	}, &ProxyRequest{ServiceGroupID: "official-pool", Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "deepseek" {
		t.Fatalf("provider = %#v, want configured primary deepseek", resp)
	}
	if len(hits) != 1 || hits[0] != "deepseek" {
		t.Fatalf("hits = %#v, want only the healthy primary", hits)
	}
}

func TestHandleProxyRequestFailsOverLivePrimaryErrorToOtherProvider(t *testing.T) {
	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "deepseek")
		http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
	}))
	defer primary.Close()
	failover := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "yinyu")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer failover.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: primary.URL, Sequence: 1},
			{ID: "yinyu", Name: "Yinyu", APIURL: failover.URL, Sequence: 2, Models: []string{"qwen-plus"}},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID: "official-pool", Name: "Official", AgentID: "maclaw_official",
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}}},
			},
			{
				ID: "qwen-pool", Name: "Qwen", AgentID: "maclaw_official",
				Models: []llmpool.ModelConfig{{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	resp, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}, &ProxyRequest{ServiceGroupID: "official-pool", Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want yinyu after live primary error", resp)
	}
	if len(hits) < 2 || hits[0] != "deepseek" || hits[len(hits)-1] != "yinyu" {
		t.Fatalf("hits = %#v, want deepseek then yinyu", hits)
	}
}

func TestHandleProxyRequestSkipsCoolingPrimaryAndUsesFailoverProvider(t *testing.T) {
	var hits []string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "deepseek")
		http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
	}))
	defer primary.Close()
	failover := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "yinyu")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer failover.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: primary.URL, Sequence: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
			{ID: "yinyu", Name: "Yinyu", APIURL: failover.URL, Sequence: 2, Models: []string{"qwen-plus"}},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID: "official-pool", Name: "Official", AgentID: "maclaw_official",
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}}},
			},
			{
				ID: "qwen-pool", Name: "Qwen", AgentID: "maclaw_official",
				Models: []llmpool.ModelConfig{{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		ServiceGroupID: "official-pool", Body: map[string]any{"model": "auto"},
	}); err != nil {
		t.Fatalf("first request: %v", err)
	}
	hits = hits[:0]
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		ServiceGroupID: "official-pool", Body: map[string]any{"model": "auto"},
	})
	if err != nil {
		t.Fatalf("cooldown request: %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want yinyu while primary is cooling down", resp)
	}
	for _, hit := range hits {
		if hit == "deepseek" {
			t.Fatalf("hits = %#v, cooling primary should be skipped", hits)
		}
	}
}

func TestHandleProxyRequestProbeNotStuckWhenProviderIsBusy(t *testing.T) {
	var recovered atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !recovered.Load() {
			http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "yinyu", Name: "Yinyu", APIURL: upstream.URL, MaxConcurrency: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 20},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "official", Name: "Official",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	ctrl := llmpool.NewConcurrencyController()
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
		Concurrency: ctrl,
		Resilience:  llmpool.NewResilienceController(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}}); err == nil {
		t.Fatal("first request: want upstream failure to open the circuit")
	}
	recovered.Store(true)
	time.Sleep(40 * time.Millisecond)
	hold, err := ctrl.TryAcquire("yinyu", 1)
	if err != nil {
		t.Fatalf("hold concurrency: %v", err)
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}}); err == nil {
		t.Fatal("busy probe: want skip while provider is at concurrency limit")
	}
	hold()
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("after releasing probe slot: %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want recovered yinyu", resp)
	}
}

func TestHandleProxyRequestCancelDoesNotOpenCircuit(t *testing.T) {
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(func() {
		upstream.CloseClientConnections()
		upstream.Close()
	})

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "only", Name: "Only", APIURL: upstream.URL, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "only"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	resilience := llmpool.NewResilienceController()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := HandleProxyRequest(ctx, &ProxyConfig{
			Service:     svc,
			AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
			HTTPClient:  upstream.Client(),
			Resilience:  resilience,
		}, &ProxyRequest{Body: map[string]any{"model": "auto"}})
		errCh <- err
	}()
	select {
	case <-started:
	case err := <-errCh:
		t.Fatalf("request returned before upstream started: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not start")
	}
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("canceled request: want error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled request did not return")
	}
	if snap := resilience.Snapshot("only", 1); snap.State != "closed" || snap.ConsecFailures != 0 {
		t.Fatalf("snapshot = %#v, cancel must not open the circuit", snap)
	}
}

func TestShouldAbortResilienceProbe(t *testing.T) {
	if !shouldAbortResilienceProbe(true, true) {
		t.Fatal("fresh probe that never reached upstream must release the slot")
	}
	if shouldAbortResilienceProbe(false, true) {
		t.Fatal("busy later route must keep an in-flight probe for remaining models")
	}
	if !shouldAbortResilienceProbe(false, false) {
		t.Fatal("last route skip must not leave the exclusive probe stuck")
	}
}

func TestHandleProxyRequestRetriesAggregatorModelAfter404(t *testing.T) {
	var models []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		model, _ := body["model"].(string)
		models = append(models, model)
		if model != "qwen-plus" {
			http.Error(w, `{"error":{"message":"unknown model"}}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: "http://paused.invalid", Paused: true},
			{ID: "yinyu", Name: "Yinyu", APIURL: upstream.URL, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "official", Name: "Official",
			Models: []llmpool.ModelConfig{
				{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}},
				{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}},
			},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
		Resilience:  llmpool.NewResilienceController(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want yinyu after aggregator 404 retry", resp)
	}
	if len(models) != 2 || models[0] != "auto" || models[1] != "qwen-plus" {
		t.Fatalf("upstream models = %#v, want auto then qwen-plus", models)
	}
}

func TestHandleProxyRequestUsesProviderSequenceOrder(t *testing.T) {
	proxyDispatchWRR.Reset()
	var hits []string
	seq1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "seq1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"one"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer seq1.Close()
	seq2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "seq2")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"two"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer seq2.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "second", Name: "Second", APIURL: seq2.URL, Sequence: 2},
			{ID: "first", Name: "First", APIURL: seq1.URL, Sequence: 1},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "second"},
				{ProviderID: "first"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: &http.Client{}}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "first" {
		t.Fatalf("provider = %#v, want sequence 1 provider", resp)
	}
	if len(hits) != 1 || hits[0] != "seq1" {
		t.Fatalf("hits = %#v, want sequence 1 first", hits)
	}
}

func TestOrderProxyDispatchRoutesStreamDoesNotGiveWRRSlotsToNonStream(t *testing.T) {
	proxyDispatchWRR.Reset()
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "chat-a", Name: "Chat A", Sequence: 1, MaxConcurrency: 10, Protocol: "openai", WireAPI: "chat"},
			{ID: "resp", Name: "Responses", Sequence: 2, MaxConcurrency: 10, Protocol: "openai", WireAPI: "responses"},
			{ID: "chat-b", Name: "Chat B", Sequence: 3, MaxConcurrency: 10, Protocol: "openai", WireAPI: "chat"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "chat-a"},
				{ProviderID: "resp"},
				{ProviderID: "chat-b"},
			}}},
		}},
	}
	group := &reg.ServiceGroups[0]
	scored := []llmpool.ScoredProviderRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "chat-a"}, ResolutionTier: 1},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "resp"}, ResolutionTier: 1},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "chat-b"}, ResolutionTier: 1},
	}
	first := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveStreamProvider, time.Time{}, true)
	second := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveStreamProvider, time.Time{}, true)
	if len(first) < 1 || first[0].ProviderID != "chat-a" {
		t.Fatalf("first pick = %#v, want chat-a", first)
	}
	if len(second) < 1 || second[0].ProviderID != "chat-b" {
		t.Fatalf("second pick = %s, want chat-b (responses must not take a stream WRR slot)", second[0].ProviderID)
	}
}

func TestOrderProxyDispatchRoutesStreamPoolDoesNotResetNonStreamWRR(t *testing.T) {
	proxyDispatchWRR.Reset()
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "chat-a", Name: "Chat A", Sequence: 1, MaxConcurrency: 10, Protocol: "openai", WireAPI: "chat"},
			{ID: "resp", Name: "Responses", Sequence: 2, MaxConcurrency: 10, Protocol: "openai", WireAPI: "responses"},
			{ID: "chat-b", Name: "Chat B", Sequence: 3, MaxConcurrency: 10, Protocol: "openai", WireAPI: "chat"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "chat-a"},
				{ProviderID: "resp"},
				{ProviderID: "chat-b"},
			}}},
		}},
	}
	group := &reg.ServiceGroups[0]
	scored := []llmpool.ScoredProviderRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "chat-a"}, ResolutionTier: 1},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "resp"}, ResolutionTier: 1},
		{Route: llmpool.DispatchProviderRoute{ProviderID: "chat-b"}, ResolutionTier: 1},
	}
	if got := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveProvider, time.Time{}, false); len(got) < 1 || got[0].ProviderID != "chat-a" {
		t.Fatalf("non-stream first = %#v, want chat-a", got)
	}
	if got := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveStreamProvider, time.Time{}, true); len(got) < 1 || got[0].ProviderID != "chat-a" {
		t.Fatalf("stream first = %#v, want chat-a in its own pool", got)
	}
	if got := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveProvider, time.Time{}, false); len(got) < 1 || got[0].ProviderID != "resp" {
		t.Fatalf("non-stream second = %s, want resp (stream traffic must not reset this pool)", got[0].ProviderID)
	}
	if got := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveStreamProvider, time.Time{}, true); len(got) < 1 || got[0].ProviderID != "chat-b" {
		t.Fatalf("stream second = %s, want chat-b", got[0].ProviderID)
	}
}

func TestHandleProxyRequestLoadBalancesSameMultiplier(t *testing.T) {
	proxyDispatchWRR.Reset()
	var hits []string
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "aisi")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"one"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "oc1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"two"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer second.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "aisi", Name: "Aisi", APIURL: first.URL, Sequence: 1, MaxConcurrency: 10},
			{ID: "oc1", Name: "OC1", APIURL: second.URL, Sequence: 2, MaxConcurrency: 10},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "aisi"},
				{ProviderID: "oc1"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: &http.Client{}}
	firstResp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("first request error = %v", err)
	}
	secondResp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("second request error = %v", err)
	}
	if firstResp == nil || firstResp.ProviderID != "aisi" {
		t.Fatalf("first provider = %#v, want aisi", firstResp)
	}
	if secondResp == nil || secondResp.ProviderID != "oc1" {
		t.Fatalf("second provider = %#v, want oc1", secondResp)
	}
	if len(hits) != 2 || hits[0] != "aisi" || hits[1] != "oc1" {
		t.Fatalf("hits = %#v, want aisi then oc1", hits)
	}
}

func TestOrderProxyDispatchRoutesBalancesSameMultiplierExtras(t *testing.T) {
	proxyDispatchWRR.Reset()
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", Sequence: 4, MaxConcurrency: 64, CreditMultiplier: 2, Paused: true},
			{ID: "opencode-1", Name: "OpenCode-1", Sequence: 2, MaxConcurrency: 10, CreditMultiplier: 2},
			{ID: "opencode-2", Name: "OpenCode-2", Sequence: 3, MaxConcurrency: 10, CreditMultiplier: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "redeem", Name: "redeem",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "deepseek"},
			}}},
		}},
	}
	group := &reg.ServiceGroups[0]
	scored := []llmpool.ScoredProviderRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "deepseek"}, ResolutionTier: 1},
	}
	first := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveProvider, time.Time{}, false)
	second := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveProvider, time.Time{}, false)
	if got := joinedProviderIDs(first); got != "deepseek,opencode-1,opencode-2" {
		t.Fatalf("first order = %s, want paused primary then extra WRR winner opencode-1", got)
	}
	if got := joinedProviderIDs(second); got != "deepseek,opencode-2,opencode-1" {
		t.Fatalf("second order = %s, want extras to rotate to opencode-2", got)
	}
}

func TestOrderProxyDispatchRoutesExtraWRRIgnoresBorrowedRoutePolicy(t *testing.T) {
	proxyDispatchWRR.Reset()
	reg := &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", Sequence: 4, MaxConcurrency: 64, CreditMultiplier: 2, Paused: true},
			{ID: "opencode-1", Name: "OpenCode-1", Sequence: 2, MaxConcurrency: 64, CreditMultiplier: 2},
			{ID: "opencode-2", Name: "OpenCode-2", Sequence: 3, MaxConcurrency: 10, CreditMultiplier: 2, Models: []string{"other"}},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID: "redeem", Name: "redeem",
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: "deepseek"},
				}}},
			},
			{
				ID: "other", Name: "other",
				Models: []llmpool.ModelConfig{{Name: "other", ProviderConfigs: []llmpool.ModelProviderConfig{
					{ProviderID: "opencode-2", ResolutionTier: 9, CreditMultiplier: 4},
				}}},
			},
		},
	}
	group := &reg.ServiceGroups[0]
	scored := []llmpool.ScoredProviderRoute{
		{Route: llmpool.DispatchProviderRoute{ProviderID: "deepseek"}, ResolutionTier: 1},
	}
	first := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveProvider, time.Time{}, false)
	second := orderProxyDispatchRoutes(nil, reg, group, "auto", scored, acceptLiveProvider, time.Time{}, false)
	if got := joinedProviderIDsAfter(first, "deepseek"); got != "opencode-1,opencode-2" {
		t.Fatalf("first extras = %s, want equal-weight vendor group (borrowed tier/markup must not split)", got)
	}
	if got := joinedProviderIDsAfter(second, "deepseek"); got != "opencode-2,opencode-1" {
		t.Fatalf("second extras = %s, want opencode-2 after equal-weight rotate", got)
	}
}

func TestHandleProxyRequestLoadBalancesSameMultiplierExtras(t *testing.T) {
	proxyDispatchWRR.Reset()
	var hits []string
	oc1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "opencode-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"one"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer oc1.Close()
	oc2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "opencode-2")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"two"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer oc2.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", Sequence: 4, MaxConcurrency: 64, CreditMultiplier: 2, Paused: true},
			{ID: "opencode-1", Name: "OpenCode-1", APIURL: oc1.URL, Sequence: 2, MaxConcurrency: 10, CreditMultiplier: 2},
			{ID: "opencode-2", Name: "OpenCode-2", APIURL: oc2.URL, Sequence: 3, MaxConcurrency: 10, CreditMultiplier: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "redeem", Name: "redeem",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "deepseek"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: &http.Client{}}
	firstResp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("first request error = %v", err)
	}
	secondResp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("second request error = %v", err)
	}
	if firstResp == nil || firstResp.ProviderID != "opencode-1" {
		t.Fatalf("first provider = %#v, want opencode-1", firstResp)
	}
	if secondResp == nil || secondResp.ProviderID != "opencode-2" {
		t.Fatalf("second provider = %#v, want opencode-2", secondResp)
	}
	if len(hits) != 2 || hits[0] != "opencode-1" || hits[1] != "opencode-2" {
		t.Fatalf("hits = %#v, want opencode-1 then opencode-2", hits)
	}
}

func TestHandleProxyRequestSequenceSkipsPausedAndErrors(t *testing.T) {
	var hits []string
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "bad")
		http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "good")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer good.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "paused", Name: "Paused", APIURL: "http://paused.invalid", Sequence: 1, Paused: true},
			{ID: "bad", Name: "Bad", APIURL: bad.URL, Sequence: 2},
			{ID: "good", Name: "Good", APIURL: good.URL, Sequence: 3},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "paused"},
				{ProviderID: "bad"},
				{ProviderID: "good"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: &http.Client{}}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "good" {
		t.Fatalf("provider = %#v, want sequence 3 after skipping paused and failed", resp)
	}
	if len(hits) == 0 || hits[0] != "bad" || hits[len(hits)-1] != "good" {
		t.Fatalf("hits = %#v, want bad then good", hits)
	}
}

func TestHandleProxyRequestSkipsProviderAtConcurrencyLimit(t *testing.T) {
	var hits []string
	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "busy")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"busy"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer busy.Close()
	free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "free")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer free.Close()

	ctrl := llmpool.NewConcurrencyController()
	hold, err := ctrl.TryAcquire("busy", 1)
	if err != nil {
		t.Fatalf("hold busy slot: %v", err)
	}
	defer hold()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "busy", Name: "Busy", APIURL: busy.URL, Sequence: 1, MaxConcurrency: 1},
			{ID: "free", Name: "Free", APIURL: free.URL, Sequence: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "busy"},
				{ProviderID: "free"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Concurrency: ctrl,
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "free" {
		t.Fatalf("provider = %#v, want free after busy concurrency skip", resp)
	}
	if len(hits) != 1 || hits[0] != "free" {
		t.Fatalf("hits = %#v, want only free", hits)
	}
}

func TestHandleProxyRequestReportsConcurrencyLimitWhenAllBusy(t *testing.T) {
	ctrl := llmpool.NewConcurrencyController()
	hold, err := ctrl.TryAcquire("only", 1)
	if err != nil {
		t.Fatalf("hold slot: %v", err)
	}
	defer hold()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "only", Name: "Only", APIURL: "http://busy.invalid", Sequence: 1, MaxConcurrency: 1}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "only"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	_, err = HandleProxyRequest(context.Background(), &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		Concurrency: ctrl,
	}, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err == nil || !strings.Contains(err.Error(), "concurrency limit") {
		t.Fatalf("error = %v, want concurrency limit", err)
	}
}

func TestHandleProxyRequestSkipsFailedProviderDuringCooldown(t *testing.T) {
	proxyDispatchWRR.Reset()
	var hits []string
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "bad")
		http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "good")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer good.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "bad", Name: "Bad", APIURL: bad.URL, Sequence: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60_000},
			{ID: "good", Name: "Good", APIURL: good.URL, Sequence: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "bad"},
				{ProviderID: "good"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}}); err != nil {
		t.Fatalf("first request: %v", err)
	}
	hits = hits[:0]
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if resp == nil || resp.ProviderID != "good" {
		t.Fatalf("provider = %#v, want good while bad is cooling down", resp)
	}
	for _, hit := range hits {
		if hit == "bad" {
			t.Fatalf("hits = %#v, cooling provider should not be retried on every request", hits)
		}
	}
}

func TestHandleProxyRequestProbesFailedProviderAfterCooldown(t *testing.T) {
	proxyDispatchWRR.Reset()
	var hits []string
	var allowBad atomic.Bool
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "bad")
		if !allowBad.Load() {
			http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, "good")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer good.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "bad", Name: "Bad", APIURL: bad.URL, Sequence: 1, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 20},
			{ID: "good", Name: "Good", APIURL: good.URL, Sequence: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "bad"},
				{ProviderID: "good"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  &http.Client{},
		Resilience:  llmpool.NewResilienceController(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}}); err != nil {
		t.Fatalf("first request: %v", err)
	}
	hits = hits[:0]
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}}); err != nil {
		t.Fatalf("cooldown request: %v", err)
	}
	for _, hit := range hits {
		if hit == "bad" {
			t.Fatalf("hits = %#v, cooling provider should stay out of the WRR pool", hits)
		}
	}
	allowBad.Store(true)
	time.Sleep(40 * time.Millisecond)
	hits = hits[:0]
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("probe request: %v", err)
	}
	if resp == nil || resp.ProviderID != "bad" {
		t.Fatalf("provider = %#v, want recovered sequence-1 provider", resp)
	}
	if len(hits) == 0 || hits[0] != "bad" {
		t.Fatalf("hits = %#v, want probe of sequence-1 provider", hits)
	}
}

func TestHandleProxyRequestProbeRetriesLaterModelAfter404(t *testing.T) {
	var models []string
	var recovered atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		model, _ := body["model"].(string)
		models = append(models, model)
		if !recovered.Load() {
			http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
			return
		}
		if model != "qwen-plus" {
			http.Error(w, `{"error":{"message":"unknown model"}}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: "http://paused.invalid", Paused: true},
			{ID: "yinyu", Name: "Yinyu", APIURL: upstream.URL, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 20},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "official", Name: "Official",
			Models: []llmpool.ModelConfig{
				{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}},
				{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}},
			},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
		Resilience:  llmpool.NewResilienceController(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}}); err == nil {
		t.Fatal("first request: want upstream failure to open the circuit")
	}
	recovered.Store(true)
	time.Sleep(40 * time.Millisecond)
	models = models[:0]
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "auto"}})
	if err != nil {
		t.Fatalf("probe request: %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want yinyu after half-open 404 retry", resp)
	}
	if len(models) < 2 || models[0] != "auto" || models[len(models)-1] != "qwen-plus" {
		t.Fatalf("upstream models = %#v, want auto then qwen-plus on the recovery probe", models)
	}
}

func TestHandleProxyStreamRequestProbeRetriesLaterModelAfter404(t *testing.T) {
	var models []string
	var recovered atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		model, _ := body["model"].(string)
		models = append(models, model)
		if !recovered.Load() {
			http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusBadGateway)
			return
		}
		if model != "qwen-plus" {
			http.Error(w, `{"error":{"message":"unknown model"}}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: "http://paused.invalid", Paused: true},
			{ID: "yinyu", Name: "Yinyu", APIURL: upstream.URL, CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 20},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "official", Name: "Official", AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{
				{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}},
				{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}},
			},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
		Resilience:  llmpool.NewResilienceController(),
	}
	if err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		Body: map[string]any{"model": "auto", "stream": true},
	}, newLockedResponseRecorder()); err == nil {
		t.Fatal("first stream: want upstream failure to open the circuit")
	}
	recovered.Store(true)
	time.Sleep(40 * time.Millisecond)
	models = models[:0]
	writer := newLockedResponseRecorder()
	if err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		Body: map[string]any{"model": "auto", "stream": true},
	}, writer); err != nil {
		t.Fatalf("probe stream: %v", err)
	}
	if got := writer.BodyString(); !strings.Contains(got, `"content":"ok"`) {
		t.Fatalf("stream output = %q, want qwen-plus recovery SSE", got)
	}
	if len(models) < 2 || models[0] != "auto" || models[len(models)-1] != "qwen-plus" {
		t.Fatalf("upstream models = %#v, want auto then qwen-plus on the recovery probe", models)
	}
}

func TestHandleProxyRequestPrefersLiveProviderAdvertisedModel(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: "http://paused.invalid", Paused: true},
			{ID: "yinyu", Name: "Yinyu", APIURL: upstream.URL, Models: []string{"deepseek-chat", "qwen-plus"}},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "official", Name: "Official",
			Models: []llmpool.ModelConfig{
				{Name: "deepseek-chat", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-chat"}}},
				{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu", Model: "qwen-plus"}}},
			},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "deepseek-chat"}})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp == nil || resp.ProviderID != "yinyu" {
		t.Fatalf("provider = %#v, want yinyu", resp)
	}
	if seen["model"] != "deepseek-chat" {
		t.Fatalf("upstream model = %#v, want advertised deepseek-chat", seen["model"])
	}
}

func TestHandleProxyStreamRequestFailsOverPausedModelToLiveGroupProvider(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer good.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "deepseek", Name: "DeepSeek", APIURL: "http://paused.invalid", Paused: true},
			{ID: "yinyu", Name: "Yinyu", APIURL: good.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "official", Name: "Official", AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{
				{Name: "deepseek-chat", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek"}}},
				{Name: "qwen-plus", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "yinyu"}}},
			},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{})}
	writer := newLockedResponseRecorder()
	err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		Body: map[string]any{"model": "deepseek-chat", "stream": true},
	}, writer)
	if err != nil {
		t.Fatalf("HandleProxyStreamRequest() error = %v", err)
	}
	if got := writer.BodyString(); !strings.Contains(got, `"content":"ok"`) {
		t.Fatalf("stream output = %q, want live group failover SSE", got)
	}
}

func TestHandleProxyRequestFailsWhenAllProvidersPaused(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://paused.invalid", Paused: true}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{})}
	_, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: map[string]any{"model": "gpt-4"}})
	if err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("error = %v, want paused provider", err)
	}
}

func TestHandleProxyRequestSkipsCacheHitForPausedProvider(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"live"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "paused", Name: "Paused", APIURL: "http://paused.invalid", Paused: true},
			{ID: "live", Name: "Live", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID: "g1", Name: "G1",
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "paused"},
				{ProviderID: "live"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	body := map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	cache := llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 10})
	if err := cache.Put(context.Background(), &llmpool.CacheEntry{
		CacheKey:   buildServiceGroupCacheKey("g1", "gpt-4", body),
		ProviderID: "paused",
		Model:      "gpt-4",
		Payload:    []byte(`{"choices":[{"message":{"content":"cached"}}]}`),
	}); err != nil {
		t.Fatalf("cache put: %v", err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), Cache: cache, HTTPClient: upstream.Client()}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{Body: body})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.CacheHit || resp.ProviderID != "live" {
		t.Fatalf("resp = %#v, want live dispatch after skipping paused cache", resp)
	}
}
func TestHandleProxyRequest_NoModel(t *testing.T) {
	cfg := &ProxyConfig{
		Service:     NewService(&mockSystemSettings{}),
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{}}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil || err.Error() != "model not specified in request" {
		t.Fatalf("expected model error, got: %v", err)
	}
}

func TestBuildTenantAuthorizationStatusIncludesInactiveState(t *testing.T) {
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-expired", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: "g1",
		CreditsTotal: 100, CreditsUsed: 10, StartsAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
		Status: "expired", Source: "admin_grant",
	}}}
	status, err := BuildTenantAuthorizationStatus(context.Background(), NewAuthorizationChecker(authRepo), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = true, want false")
	}
	if len(status.Authorizations) != 1 {
		t.Fatalf("len(Authorizations) = %d, want 1", len(status.Authorizations))
	}
	if status.Authorizations[0].Active {
		t.Fatalf("inactive authorization reported active: %#v", status.Authorizations[0])
	}
	if status.Authorizations[0].Status != "expired" {
		t.Fatalf("Status = %q, want expired", status.Authorizations[0].Status)
	}
}

func TestBuildTenantAuthorizationStatusIncludesInactivePermissionState(t *testing.T) {
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-expired", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
	}}}
	status, err := BuildTenantAuthorizationStatus(context.Background(), NewAuthorizationChecker(authRepo), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = true, want false")
	}
	if len(status.Authorizations) != 1 {
		t.Fatalf("len(Authorizations) = %d, want explicit inactive permission state", len(status.Authorizations))
	}
	if status.Authorizations[0].Active || status.Authorizations[0].ServiceGroupID != ExternalComputePermissionServiceGroupID {
		t.Fatalf("permission state = %#v, want inactive external permission", status.Authorizations[0])
	}
}

func TestBuildTenantAuthorizationStatusLatestRevocationOverridesOlderGrant(t *testing.T) {
	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-active-old", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", Source: "external_provider_permission", AllowExternalProviders: true,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}, {
		ID: "auth_admin_hub1_tenant_acme___external_compute_permission__", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Minute),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
		CreatedAt: now, UpdatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)
	allowed, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("HasExternalProviderAccess() error = %v", err)
	}
	if allowed {
		t.Fatalf("HasExternalProviderAccess() = true, want latest revocation to win")
	}
	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = true, want latest revocation to win")
	}
	if len(status.Authorizations) != 1 || status.Authorizations[0].ID != "auth_admin_hub1_tenant_acme___external_compute_permission__" || status.Authorizations[0].Active {
		t.Fatalf("authorizations = %#v, want only inactive latest revocation", status.Authorizations)
	}
}

func TestBuildTenantAuthorizationStatusNewerGrantOverridesOlderRevocation(t *testing.T) {
	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-revoked-old", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-2 * time.Hour),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	}, {
		ID: "external-active-new", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", Source: "external_provider_permission", AllowExternalProviders: true,
		CreatedAt: now, UpdatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)
	allowed, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("HasExternalProviderAccess() error = %v", err)
	}
	if !allowed {
		t.Fatalf("HasExternalProviderAccess() = false, want newer grant to win")
	}
	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if !status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = false, want newer grant to win")
	}
}

func TestBuildTenantAuthorizationStatusTiebreaksEqualUpdateTimeByCreatedAt(t *testing.T) {
	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-revoked-old-created", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-2 * time.Hour),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	}, {
		ID: "external-active-new-created", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", Source: "external_provider_permission", AllowExternalProviders: true,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)
	allowed, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("HasExternalProviderAccess() error = %v", err)
	}
	if !allowed {
		t.Fatalf("HasExternalProviderAccess() = false, want newer created grant to win equal updated_at")
	}
	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if !status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = false, want newer created grant to win equal updated_at")
	}
}

func TestBuildTenantAuthorizationStatusIgnoresExpiredLegacyRedeemPermissionRowsForAccessState(t *testing.T) {
	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "external-active-canonical", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: ExternalComputePermissionServiceGroupID,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", Source: "external_provider_permission", AllowExternalProviders: true,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
	}, {
		ID: "external-expired-legacy-redeem", HubID: "hub1", TenantID: "tenant_acme", ServiceGroupID: "redeem",
		StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "expired", Source: "external_provider_permission", AllowExternalProviders: false,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)
	allowed, err := checker.HasExternalProviderAccess(context.Background(), "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("HasExternalProviderAccess() error = %v", err)
	}
	if !allowed {
		t.Fatalf("HasExternalProviderAccess() = false, want canonical grant to ignore legacy redeem row")
	}
	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "tenant_acme")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if !status.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = false, want canonical grant to ignore legacy redeem row")
	}
}

func TestHandleProxyRequest_ModelNotInRegistry(t *testing.T) {
	system := &mockSystemSettings{}
	svc := NewService(system)
	// Save a registry with one group but different model
	_ = svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://localhost"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	})

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "nonexistent"}}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestHandleProxyRequest_AuthDenied(t *testing.T) {
	system := &mockSystemSettings{}
	svc := NewService(system)
	_ = svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://localhost"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", AccessPolicy: AccessPolicyGrantRequired, Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	})

	// No authorizations: access denied
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil {
		t.Fatal("expected authorization denied")
	}
	if !contains(err.Error(), "authorization denied") {
		t.Fatalf("expected auth denied error, got: %v", err)
	}
}

func TestHandleProxyRequest_GrantRequiredMatchesTenantIDAlias(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-alias", HubID: "hub1", TenantID: "acme", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "tenant_acme", Body: map[string]any{"model": "gpt-4"}}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("alias tenant grant should authorize proxy request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if authRepo.auths[0].CreditsUsed <= 0 {
		t.Fatalf("expected alias grant credits to be deducted")
	}
}

func TestHandleProxyRequest_GrantRequiredAppliesMinimumCreditCharge(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":60,"completion_tokens":40,"total_tokens":100}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-small", HubID: "hub1", TenantID: "t1", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
		Usage:       &recordingUsageRecorder{},
	}

	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := authRepo.auths[0].CreditsUsed; got != minimumProxyRequestCredits {
		t.Fatalf("CreditsUsed = %.3f, want %.3f minimum charge", got, minimumProxyRequestCredits)
	}
}

func TestHandleProxyRequest_GrantRequiredAppliesMinimumCreditChargeWhenUsageMissing(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-missing-usage", HubID: "hub1", TenantID: "t1", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
		Usage:       &recordingUsageRecorder{},
	}

	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := authRepo.auths[0].CreditsUsed; got != minimumProxyRequestCredits {
		t.Fatalf("CreditsUsed = %.3f, want %.3f minimum charge", got, minimumProxyRequestCredits)
	}
	if !bytes.Contains(resp.Body, []byte(`"usage"`)) || !bytes.Contains(resp.Body, []byte(`"estimated":true`)) {
		t.Fatalf("response body should include estimated usage: %s", resp.Body)
	}
}

func TestHandleProxyRequest_GrantRequiredSpreadsChargeAcrossComputeCards(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10000,"completion_tokens":5000,"total_tokens":15000}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{
		{
			ID: "auth-small", HubID: "hub1", TenantID: "t1", ServiceGroupID: "redeem",
			CreditsTotal: 1, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
			Status: "active", CardOrderID: "HC-SMALL", CreatedAt: now.Add(-2 * time.Hour),
		},
		{
			ID: "auth-large", HubID: "hub1", TenantID: "t1", ServiceGroupID: "redeem",
			CreditsTotal: 1000, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour),
			Status: "active", CardOrderID: "HC-LARGE", CreatedAt: now.Add(-time.Hour),
		},
	}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
		Usage:       &recordingUsageRecorder{},
	}

	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 1 {
		t.Fatalf("small card CreditsUsed = %.3f, want 1", got)
	}
	if got := authRepo.auths[1].CreditsUsed; got != 0.5 {
		t.Fatalf("large card CreditsUsed = %.3f, want 0.5", got)
	}
	usage := cfg.Usage.(*recordingUsageRecorder)
	if len(usage.records) != 1 {
		t.Fatalf("usage records len = %d, want 1", len(usage.records))
	}
	if got := usage.records[0].AuthID; got != "auth-small,auth-large" {
		t.Fatalf("usage AuthID = %q, want charged auth IDs", got)
	}
}

func TestAuthorizationCheckerDeductCreditsForServiceGroupReportsInsufficientAggregateBalance(t *testing.T) {
	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-half", HubID: "hub1", TenantID: "t1", ServiceGroupID: "redeem",
		CreditsTotal: 0.5, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", CreatedAt: now,
	}}}
	checker := NewAuthorizationChecker(authRepo)

	deductions, err := checker.DeductCreditsForServiceGroup(context.Background(), "hub1", "t1", "redeem", 1)
	if err == nil {
		t.Fatal("DeductCreditsForServiceGroup() error = nil, want insufficient credits error")
	}
	var insufficient *InsufficientCreditsError
	if !errors.As(err, &insufficient) {
		t.Fatalf("DeductCreditsForServiceGroup() error = %T %[1]v, want InsufficientCreditsError", err)
	}
	if insufficient.Requested != 1 || insufficient.Deducted != 0.5 || insufficient.Remaining != 0.5 {
		t.Fatalf("insufficient error = %#v, want requested=1 deducted=0.5 remaining=0.5", insufficient)
	}
	if len(deductions) != 1 || deductions[0].AuthID != "auth-half" || deductions[0].Credits != 0.5 {
		t.Fatalf("deductions = %#v, want partial charge against auth-half", deductions)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 0.5 {
		t.Fatalf("CreditsUsed = %.3f, want 0.5", got)
	}
}

func TestHandleProxyRequest_GrantRequiredRecordsOnlyDeductedCreditsWhenBalanceRunsOut(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":5000,"completion_tokens":5000,"total_tokens":10000}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth-half", HubID: "hub1", TenantID: "t1", ServiceGroupID: "redeem",
		CreditsTotal: 0.5, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		Status: "active", CreatedAt: now,
	}}}
	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
		Usage:       usage,
	}

	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 0.5 {
		t.Fatalf("CreditsUsed = %.3f, want 0.5", got)
	}
	if len(usage.records) != 1 {
		t.Fatalf("usage records len = %d, want 1", len(usage.records))
	}
	if got := usage.records[0].Credits; got != 0.5 {
		t.Fatalf("usage Credits = %.3f, want actual deducted credits 0.5", got)
	}
	if got := usage.records[0].AuthID; got != "auth-half" {
		t.Fatalf("usage AuthID = %q, want auth-half", got)
	}
}

func TestAuthorizationCheckerCheckAccessMatchesServiceGroupCaseInsensitively(t *testing.T) {
	now := time.Now().UTC()
	checker := NewAuthorizationChecker(&mockAuthRepo{auths: []*TenantAuthorization{{
		ID:             "auth1",
		HubID:          "hub1",
		TenantID:       "t1",
		ServiceGroupID: "Redeem",
		CreditsTotal:   100,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
	}}})
	auth, err := checker.CheckAccess(context.Background(), "hub1", "t1", "redeem")
	if err != nil {
		t.Fatalf("CheckAccess() error = %v", err)
	}
	if auth == nil || auth.ID != "auth1" {
		t.Fatalf("CheckAccess() auth = %#v, want auth1", auth)
	}
}

func TestBuildTenantAuthorizationStatusPreservesFractionalCredits(t *testing.T) {
	now := time.Now().UTC()
	checker := NewAuthorizationChecker(&mockAuthRepo{auths: []*TenantAuthorization{{
		ID:             "auth-fractional",
		HubID:          "hub1",
		TenantID:       "t1",
		ServiceGroupID: "maclaw-official",
		CreditsTotal:   10,
		CreditsUsed:    1.1,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "card",
	}}})

	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "t1")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if len(status.Authorizations) != 1 {
		t.Fatalf("authorizations len = %d, want 1: %#v", len(status.Authorizations), status)
	}
	got := status.Authorizations[0]
	if got.CreditsUsed != 1.1 || got.CreditsRemaining != 8.9 {
		t.Fatalf("authorization credits = used %.17g remaining %.17g, want 1.1/8.9", got.CreditsUsed, got.CreditsRemaining)
	}
}

func TestBuildTenantAuthorizationStatusIncludesProviderBilling(t *testing.T) {
	SetProviderBillingCatalog(func(context.Context) []llmpool.ProviderBillingPolicy {
		return []llmpool.ProviderBillingPolicy{{
			ProviderID:       "deepseek",
			Timezone:         "Asia/Shanghai",
			CreditMultiplier: 1,
			CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
				Days:       []int{1, 2, 3, 4, 5},
				Start:      "00:30",
				End:        "08:30",
				Multiplier: 0.5,
			}},
		}}
	})
	t.Cleanup(func() { SetProviderBillingCatalog(nil) })

	status, err := BuildTenantAuthorizationStatus(context.Background(), NewAuthorizationChecker(&mockAuthRepo{}), "hub1", "t1")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if len(status.ProviderBilling) != 1 || status.ProviderBilling[0].ProviderID != "deepseek" {
		t.Fatalf("provider_billing = %#v", status.ProviderBilling)
	}
	if status.ProviderBilling[0].CreditMultiplierSchedule[0].Multiplier != 0.5 {
		t.Fatalf("window multiplier = %#v", status.ProviderBilling[0].CreditMultiplierSchedule)
	}
}

func TestBuildTenantAuthorizationStatusRoundsCreditDisplay(t *testing.T) {
	now := time.Now().UTC()
	checker := NewAuthorizationChecker(&mockAuthRepo{auths: []*TenantAuthorization{{
		ID:             "auth-card-display",
		HubID:          "hub1",
		TenantID:       "t1",
		ServiceGroupID: "redeem",
		CreditsTotal:   520000,
		CreditsUsed:    12102.734400000001,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "card",
	}}})

	status, err := BuildTenantAuthorizationStatus(context.Background(), checker, "hub1", "t1")
	if err != nil {
		t.Fatalf("BuildTenantAuthorizationStatus() error = %v", err)
	}
	if len(status.Authorizations) != 1 {
		t.Fatalf("authorizations len = %d, want 1: %#v", len(status.Authorizations), status)
	}
	got := status.Authorizations[0]
	if got.CreditsUsed != 12102.7344 {
		t.Fatalf("credits_used = %.17g, want 12102.7344", got.CreditsUsed)
	}
	if got.CreditsRemaining != 507897.2656 {
		t.Fatalf("credits_remaining = %.17g, want 507897.2656", got.CreditsRemaining)
	}
}

func TestHandleProxyRequest_FreeAccessPolicySkipsAuthorization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":50}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("free group should not require authorization: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(authRepo.auths) != 0 {
		t.Fatalf("free group should not create or deduct authorizations: %#v", authRepo.auths)
	}
}

func TestForwardToProviderUsesSharedCorelibCompatibility(t *testing.T) {
	var seen map[string]any
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "codegen.qianxin-inc.cn" || req.URL.Path != "/api/v1/chat/completions" {
			t.Fatalf("upstream URL = %s, want CodeGen chat completions endpoint", req.URL.String())
		}
		if err := json.NewDecoder(req.Body).Decode(&seen); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"ok","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
			Request:    req,
		}, nil
	})}
	provider := &llmpool.ProviderConfig{
		ID:       "codegen",
		Name:     "CodeGen",
		APIURL:   "https://codegen.qianxin-inc.cn/api/v1",
		APIKey:   "secret",
		Protocol: "openai",
	}
	body := map[string]any{
		"model":           "auto",
		"stream":          true,
		"stream_options":  map[string]any{"include_usage": true},
		"response_format": map[string]any{"type": "json_schema"},
		"messages":        []any{map[string]any{"role": "user", "content": "hello"}},
	}

	resp, err := forwardToProvider(context.Background(), client, provider, body, "auto", "auto")
	if err != nil {
		t.Fatalf("forwardToProvider() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen["model"] != "qax-codegen/Auto" {
		t.Fatalf("model = %#v, want CodeGen auto model", seen["model"])
	}
	for _, key := range []string{"stream_options", "response_format"} {
		if _, ok := seen[key]; ok {
			t.Fatalf("%s leaked upstream: %#v", key, seen)
		}
	}
	if seen["stream"] != false {
		t.Fatalf("stream = %#v, want false for HubCenter non-stream proxy", seen["stream"])
	}
}

func TestProxyHandlerStreamsKeepAliveCompatibleResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var seen map[string]any
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if seen["stream"] != true {
			t.Fatalf("upstream stream = %#v, want true", seen["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-test\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"index\":0}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-test\",\"model\":\"gpt-4\",\"choices\":[{\"delta\":{},\"index\":0,\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	body := bytes.NewBufferString(`{"model":"gpt-4","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", body)
	req.Header.Set("X-Hub-ID", "hub1")
	req.Header.Set("X-Tenant-ID", "t1")
	rr := httptest.NewRecorder()

	ProxyHandler(cfg).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	out := rr.Body.String()
	for _, want := range []string{
		`"delta":{"content":"ok","role":"assistant"}`,
		`"usage":{"completion_tokens":1,"prompt_tokens":1,"total_tokens":2}`,
		"data: [DONE]\n\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stream body missing %q:\n%s", want, out)
		}
	}
}

func TestProxyHandlerStreamsHeartbeatWhileWaiting(t *testing.T) {
	origInterval := proxyStreamHeartbeatInterval
	proxyStreamHeartbeatInterval = 10 * time.Millisecond
	defer func() { proxyStreamHeartbeatInterval = origInterval }()

	upstreamDone := make(chan struct{})
	var closeUpstreamDone sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-upstreamDone
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	defer closeUpstreamDone.Do(func() { close(upstreamDone) })

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client()}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4","stream":true}`))
	req.Header.Set("X-Hub-ID", "hub1")
	req.Header.Set("X-Tenant-ID", "t1")
	rr := newLockedResponseRecorder()
	done := make(chan struct{})
	go func() {
		ProxyHandler(cfg).ServeHTTP(rr, req)
		close(done)
	}()

	deadline := time.After(250 * time.Millisecond)
	for !strings.Contains(rr.BodyString(), ": ping\n\n") {
		select {
		case <-deadline:
			t.Fatalf("stream body did not receive heartbeat; body=%q", rr.BodyString())
		case <-time.After(5 * time.Millisecond):
		}
	}
	closeUpstreamDone.Do(func() { close(upstreamDone) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not finish after upstream completed")
	}
}

func TestProxyHandlerStreamPreflightReturnsAuthorizationErrorBeforeSSE(t *testing.T) {
	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://127.0.0.1:1"}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{})}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4","stream":true}`))
	req.Header.Set("X-Hub-ID", "hub1")
	req.Header.Set("X-Tenant-ID", "t1")
	rr := httptest.NewRecorder()

	ProxyHandler(cfg).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("Content-Type = %q, want JSON error before SSE starts", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "authorization denied") {
		t.Fatalf("body = %s, want authorization denied", rr.Body.String())
	}
}

func TestHandleProxyStreamRequestFailsOverBeforeStreaming(t *testing.T) {
	proxyDispatchWRR.Reset()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer good.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "bad", Name: "Bad", APIURL: bad.URL},
			{ID: "good", Name: "Good", APIURL: good.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "bad"},
				{ProviderID: "good"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{})}
	writer := newLockedResponseRecorder()
	err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "stream": true},
	}, writer)
	if err != nil {
		t.Fatalf("HandleProxyStreamRequest() error = %v", err)
	}
	if got := writer.BodyString(); !strings.Contains(got, `"content":"ok"`) || !strings.Contains(got, "data: [DONE]") {
		t.Fatalf("stream output = %q, want good provider SSE", got)
	}
}

func TestHandleProxyStreamRequestDoesNotFailOverAfterStreamingStarts(t *testing.T) {
	proxyDispatchWRR.Reset()
	var goodHits int
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "bad.example":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: &errorAfterReader{
					r: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"index\":0}]}\n\n"),
				},
			}, nil
		case "good.example":
			goodHits++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"fallback\"},\"index\":0}]}\n\ndata: [DONE]\n\n")),
			}, nil
		default:
			t.Fatalf("unexpected upstream host: %s", req.URL.Host)
			return nil, nil
		}
	})}

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "bad", Name: "Bad", APIURL: "https://bad.example"},
			{ID: "good", Name: "Good", APIURL: "https://good.example"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "bad"},
				{ProviderID: "good"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: client, Usage: usage}
	writer := newLockedResponseRecorder()
	err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "stream": true},
	}, writer)
	if err == nil {
		t.Fatal("HandleProxyStreamRequest() error = nil, want upstream interruption")
	}
	if goodHits != 0 {
		t.Fatalf("fallback provider hits = %d, want 0 after stream already started", goodHits)
	}
	out := writer.BodyString()
	if !strings.Contains(out, `"content":"partial"`) {
		t.Fatalf("stream output = %q, want partial first provider chunk", out)
	}
	if strings.Contains(out, "fallback") {
		t.Fatalf("stream output contains fallback provider chunk after partial stream: %q", out)
	}
	if len(usage.records) != 1 {
		t.Fatalf("usage records = %d, want 1 for partial streamed output", len(usage.records))
	}
	if usage.records[0].ProviderID != "bad" {
		t.Fatalf("usage provider = %q, want bad", usage.records[0].ProviderID)
	}
	if usage.records[0].OutputTokens <= 0 {
		t.Fatalf("usage output tokens = %d, want > 0", usage.records[0].OutputTokens)
	}
}

func TestHandleProxyStreamRequestCanFailOverAfterHeartbeatOnly(t *testing.T) {
	proxyDispatchWRR.Reset()
	var goodHits int
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "bad.example":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: &errorAfterReader{
					r: strings.NewReader(": keepalive\n\nevent: ping\n\ndata:\n\n"),
				},
			}, nil
		case "good.example":
			goodHits++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"fallback\"},\"index\":0}]}\n\ndata: [DONE]\n\n")),
			}, nil
		default:
			t.Fatalf("unexpected upstream host: %s", req.URL.Host)
			return nil, nil
		}
	})}

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "bad", Name: "Bad", APIURL: "https://bad.example"},
			{ID: "good", Name: "Good", APIURL: "https://good.example"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "bad"},
				{ProviderID: "good"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: client}
	writer := newLockedResponseRecorder()
	err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "stream": true},
	}, writer)
	if err != nil {
		t.Fatalf("HandleProxyStreamRequest() error = %v", err)
	}
	if goodHits != 1 {
		t.Fatalf("fallback provider hits = %d, want 1 after heartbeat-only interruption", goodHits)
	}
	out := writer.BodyString()
	if strings.Contains(out, ": keepalive") || strings.Contains(out, "event: ping") || strings.Contains(out, "data:\n\n") {
		t.Fatalf("stream output leaked upstream heartbeat before fallback: %q", out)
	}
	if !strings.Contains(out, `"content":"fallback"`) {
		t.Fatalf("stream output = %q, want fallback provider chunk", out)
	}
}

func TestHandleProxyStreamRequestCanFailOverAfterUpstreamErrorEventBeforeBusinessStream(t *testing.T) {
	var goodHits int
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "bad.example":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("event: error\ndata: {\"message\":\"provider overloaded\",\"type\":\"server_error\",\"code\":503}\n\n")),
			}, nil
		case "good.example":
			goodHits++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"fallback\"},\"index\":0}]}\n\ndata: [DONE]\n\n")),
			}, nil
		default:
			t.Fatalf("unexpected upstream host: %s", req.URL.Host)
			return nil, nil
		}
	})}

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "bad", Name: "Bad", APIURL: "https://bad.example"},
			{ID: "good", Name: "Good", APIURL: "https://good.example"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "bad"},
				{ProviderID: "good"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: client, Usage: usage}
	writer := newLockedResponseRecorder()
	err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "stream": true},
	}, writer)
	if err != nil {
		t.Fatalf("HandleProxyStreamRequest() error = %v", err)
	}
	if goodHits != 1 {
		t.Fatalf("fallback provider hits = %d, want 1 after upstream error event before business stream", goodHits)
	}
	out := writer.BodyString()
	if strings.Contains(out, "provider overloaded") {
		t.Fatalf("stream output leaked failed provider error before fallback: %q", out)
	}
	if !strings.Contains(out, `"content":"fallback"`) || !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("stream output = %q, want fallback provider chunk", out)
	}
	if strings.Count(out, "data: [DONE]") != 1 {
		t.Fatalf("stream output = %q, want exactly one DONE from fallback provider", out)
	}
	if len(usage.records) != 1 || usage.records[0].ProviderID != "good" {
		t.Fatalf("usage records = %+v, want only good provider usage", usage.records)
	}
}

func TestProxyStreamErrorFromDataRecognizesErrorShapes(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		data      string
		wantErr   bool
		wantText  string
	}{
		{
			name:     "openai error object",
			data:     `{"error":{"message":"provider failed","type":"server_error"}}`,
			wantErr:  true,
			wantText: "provider failed",
		},
		{
			name:      "sse error event message",
			eventType: "error",
			data:      `{"message":"provider overloaded","type":"server_error"}`,
			wantErr:   true,
			wantText:  "provider overloaded",
		},
		{
			name:     "top level code message",
			data:     `{"code":"content_filter","message":"content filtered by upstream"}`,
			wantErr:  true,
			wantText: "content filtered by upstream",
		},
		{
			name:    "normal chat chunk",
			data:    `{"choices":[{"delta":{"content":"ok"},"index":0}]}`,
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := proxyStreamErrorFromData(tc.eventType, []byte(tc.data))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("proxyStreamErrorFromData() error = nil, want error")
				}
				if !strings.Contains(err.Error(), tc.wantText) {
					t.Fatalf("proxyStreamErrorFromData() error = %q, want %q", err.Error(), tc.wantText)
				}
				return
			}
			if err != nil {
				t.Fatalf("proxyStreamErrorFromData() error = %v, want nil", err)
			}
		})
	}
}

func TestProxyProviderSSEMergesMultilineDataEvent(t *testing.T) {
	var dst lockedResponseRecorder
	result := &providerStreamResult{}
	stream := "event: message\n" +
		"data: {\"id\":\"chunk-1\",\"model\":\"upstream\",\"choices\":[\n" +
		"data: {\"index\":0,\"delta\":{\"content\":\"hello\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"

	if err := proxyProviderSSE(strings.NewReader(stream), &dst, "logical-model", result); err != nil {
		t.Fatalf("proxyProviderSSE() error = %v", err)
	}

	out := dst.BodyString()
	if strings.Count(out, "data: ") != 2 {
		t.Fatalf("stream output = %q, want one chunk and one DONE", out)
	}
	if !strings.Contains(out, `"model":"logical-model"`) || !strings.Contains(out, `"content":"hello"`) {
		t.Fatalf("stream output = %q, want patched logical model and content", out)
	}
	if !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("stream output = %q, want DONE", out)
	}
	if !result.wroteBusinessStream || result.outputText != "hello" || result.inputTokens != 3 || result.outputTokens != 2 {
		t.Fatalf("stream result = %+v, want measured multiline business chunk", result)
	}
}
func TestHandleProxyStreamRequestCanFailOverAfterEmptyStreamEnds(t *testing.T) {
	var goodHits int
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "bad.example":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(": keepalive\n\ndata:\n\ndata: [DONE]\n\n")),
			}, nil
		case "good.example":
			goodHits++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"fallback\"},\"index\":0}]}\n\ndata: [DONE]\n\n")),
			}, nil
		default:
			t.Fatalf("unexpected upstream host: %s", req.URL.Host)
			return nil, nil
		}
	})}

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "bad", Name: "Bad", APIURL: "https://bad.example"},
			{ID: "good", Name: "Good", APIURL: "https://good.example"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "bad"},
				{ProviderID: "good"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: client, Usage: usage}
	writer := newLockedResponseRecorder()
	err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "stream": true},
	}, writer)
	if err != nil {
		t.Fatalf("HandleProxyStreamRequest() error = %v", err)
	}
	if goodHits != 1 {
		t.Fatalf("fallback provider hits = %d, want 1 after empty stream", goodHits)
	}
	out := writer.BodyString()
	if strings.Contains(out, ": keepalive") || strings.Contains(out, "data:\n\n") {
		t.Fatalf("stream output leaked empty first provider stream: %q", out)
	}
	if !strings.Contains(out, `"content":"fallback"`) || !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("stream output = %q, want fallback provider chunk", out)
	}
	if strings.Count(out, "data: [DONE]") != 1 {
		t.Fatalf("stream output = %q, want exactly one DONE from fallback provider", out)
	}
	if len(usage.records) != 1 || usage.records[0].ProviderID != "good" {
		t.Fatalf("usage records = %+v, want only good provider usage", usage.records)
	}
}

func TestHandleProxyStreamRequestSetsCodeGenClientNameHeader(t *testing.T) {
	var seenClientName string
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		seenClientName = req.Header.Get(corelib.CodeGenClientNameHeader)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\n\ndata: [DONE]\n\n")),
		}, nil
	})}

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{
			ID:     "codegen",
			Name:   "CodeGen",
			APIURL: "https://codegen.qianxin-inc.cn/api/llm/v1",
		}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "codegen"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: client}
	writer := newLockedResponseRecorder()
	err := HandleProxyStreamRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "gpt-4", "stream": true},
	}, writer)
	if err != nil {
		t.Fatalf("HandleProxyStreamRequest() error = %v", err)
	}
	if seenClientName != corelib.CodeGenClientName {
		t.Fatalf("%s = %q, want %q", corelib.CodeGenClientNameHeader, seenClientName, corelib.CodeGenClientName)
	}
}

type errorAfterReader struct {
	r      *strings.Reader
	failed bool
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	if r.r != nil && r.r.Len() > 0 {
		return r.r.Read(p)
	}
	if !r.failed {
		r.failed = true
		return 0, errors.New("upstream stream interrupted")
	}
	return 0, io.EOF
}

func (r *errorAfterReader) Close() error {
	return nil
}

func TestProxyStreamingHTTPClientClearsTotalTimeout(t *testing.T) {
	base := &http.Client{
		Timeout: 180 * time.Second,
		Transport: &http.Transport{
			ResponseHeaderTimeout: 10 * time.Minute,
		},
	}
	client := proxyStreamingHTTPClient(base, corelib.MaclawLLMConfig{TimeoutSec: 240})
	if client == base {
		t.Fatal("proxyStreamingHTTPClient returned shared base client")
	}
	if client.Timeout != 0 {
		t.Fatalf("Timeout = %s, want no total body timeout for streams", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport == base.Transport {
		t.Fatal("streaming transport should be cloned before mutation")
	}
	if got, want := transport.ResponseHeaderTimeout, 240*time.Second; got != want {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", got, want)
	}
}

func TestProxyStreamingHTTPClientAddsHeaderTimeoutWhenBaseUsesDefaultTransport(t *testing.T) {
	base := &http.Client{Timeout: 180 * time.Second}
	cfg := corelib.MaclawLLMConfig{TimeoutSec: 90}
	client := proxyStreamingHTTPClient(base, cfg)
	if client == base {
		t.Fatal("proxyStreamingHTTPClient returned shared base client")
	}
	if client.Timeout != 0 {
		t.Fatalf("Timeout = %s, want no total body timeout for streams", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("Transport = %T, want cloned *http.Transport", client.Transport)
	}
	if got, want := transport.ResponseHeaderTimeout, time.Duration(cfg.EffectiveTimeoutSec())*time.Second; got != want {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", got, want)
	}
}

func TestHandleProxyRequestUsesProviderConfiguredUpstreamModel(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"qax-codegen/Auto","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "official",
				Model:      "qax-codegen/Auto",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen["model"] != "qax-codegen/Auto" {
		t.Fatalf("upstream model = %#v, want qax-codegen/Auto", seen["model"])
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["model"] != "auto" {
		t.Fatalf("response model = %#v, want logical model auto", payload["model"])
	}
}

func TestHandleProxyRequestUsesRequestedServiceGroupWhenModelsOverlap(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"free-upstream","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "paid", Name: "Paid", APIURL: upstream.URL},
			{ID: "free", Name: "Free", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           "paid-group",
				Name:         "Paid Group",
				AccessPolicy: AccessPolicyGrantRequired,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "paid",
					Model:      "paid-upstream",
				}}}},
			},
			{
				ID:           "system-free",
				Name:         "System Free",
				AccessPolicy: AccessPolicyFree,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "free",
					Model:      "free-upstream",
				}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "system-free",
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ProviderID != "free" {
		t.Fatalf("provider = %q, want free", resp.ProviderID)
	}
	if seen["model"] != "free-upstream" {
		t.Fatalf("upstream model = %#v, want free-upstream", seen["model"])
	}
}

func TestHandleProxyRequestSystemFreeFallsBackToFreeAutoGroup(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"deepseek-v4-flash","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem Free",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "deepseek",
				Model:      "deepseek-v4-flash",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "system-free",
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ProviderID != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", resp.ProviderID)
	}
	if seen["model"] != "deepseek-v4-flash" {
		t.Fatalf("upstream model = %#v, want deepseek-v4-flash", seen["model"])
	}
}

func TestHandleProxyRequestSystemFreeFallsBackToGrantBackedAutoGroup(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"deepseek-v4-flash","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyGrantRequired,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "deepseek",
				Model:      "deepseek-v4-flash",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID:             "auth1",
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "redeem",
		CreditsTotal:   100,
		CreditsUsed:    0,
		StartsAt:       time.Now().UTC().Add(-time.Hour),
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		Status:         "active",
	}}}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "system-free",
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ProviderID != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", resp.ProviderID)
	}
	if seen["model"] != "deepseek-v4-flash" {
		t.Fatalf("upstream model = %#v, want deepseek-v4-flash", seen["model"])
	}
}

func TestHandleProxyRequestSystemFreePrefersRedeemOverOtherGrantBackedAutoGroups(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"redeem-upstream","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "other", Name: "Other", APIURL: upstream.URL},
			{ID: "redeem-provider", Name: "Redeem", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           "premium-auto",
				Name:         "Premium Auto",
				AccessPolicy: AccessPolicyGrantRequired,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "other",
					Model:      "premium-upstream",
				}}}},
			},
			{
				ID:           "redeem",
				Name:         "Redeem",
				AccessPolicy: AccessPolicyGrantRequired,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "redeem-provider",
					Model:      "redeem-upstream",
				}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{
		{
			ID:             "auth-premium",
			HubID:          "hub1",
			TenantID:       "tenant1",
			ServiceGroupID: "premium-auto",
			CreditsTotal:   100,
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(time.Hour),
			Status:         "active",
		},
		{
			ID:             "auth-redeem",
			HubID:          "hub1",
			TenantID:       "tenant1",
			ServiceGroupID: "redeem",
			CreditsTotal:   100,
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(time.Hour),
			Status:         "active",
		},
	}}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "system-free",
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.ProviderID != "redeem-provider" {
		t.Fatalf("provider = %q, want redeem-provider", resp.ProviderID)
	}
	if seen["model"] != "redeem-upstream" {
		t.Fatalf("upstream model = %#v, want redeem-upstream", seen["model"])
	}
}

func TestMatchProxyServiceGroupModelOfficialAndAliasRules(t *testing.T) {
	autoGroup := func(id, policy string) llmpool.ServiceGroup {
		return llmpool.ServiceGroup{
			ID:           id,
			Name:         id,
			AccessPolicy: policy,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "p1",
				Model:      id + "-up",
			}}}},
		}
	}
	reg := func(groups ...llmpool.ServiceGroup) *Registry {
		return &Registry{
			Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://127.0.0.1"}},
			ServiceGroups: groups,
		}
	}

	tests := []struct {
		name      string
		requestID string
		reg       *Registry
		wantID    string
	}{
		{
			name:      "official uses grant redeem",
			requestID: llmpool.HubOfficialServiceGroupID,
			reg:       reg(autoGroup("redeem", AccessPolicyGrantRequired)),
			wantID:    "redeem",
		},
		{
			name:      "official prefers redeem over later grant group",
			requestID: llmpool.HubOfficialServiceGroupID,
			reg:       reg(autoGroup("premium-auto", AccessPolicyGrantRequired), autoGroup("redeem", AccessPolicyGrantRequired)),
			wantID:    "redeem",
		},
		{
			name:      "official ignores local same-id group",
			requestID: llmpool.HubOfficialServiceGroupID,
			reg:       reg(autoGroup(llmpool.HubOfficialServiceGroupID, AccessPolicyGrantRequired), autoGroup("redeem", AccessPolicyGrantRequired)),
			wantID:    "redeem",
		},
		{
			name:      "official does not use local same-id grant as compute",
			requestID: llmpool.HubOfficialServiceGroupID,
			reg:       reg(autoGroup(llmpool.HubOfficialServiceGroupID, AccessPolicyGrantRequired)),
			wantID:    "",
		},
		{
			name:      "official skips free redeem",
			requestID: llmpool.HubOfficialServiceGroupID,
			reg:       reg(autoGroup("redeem", AccessPolicyFree), autoGroup("premium-auto", AccessPolicyGrantRequired)),
			wantID:    "premium-auto",
		},
		{
			name:      "official skips free-only catalog",
			requestID: llmpool.HubOfficialServiceGroupID,
			reg:       reg(autoGroup("free-auto", AccessPolicyFree)),
			wantID:    "",
		},
		{
			name:      "system-free prefers free over redeem",
			requestID: "system-free",
			reg:       reg(autoGroup("redeem", AccessPolicyGrantRequired), autoGroup("free-auto", AccessPolicyFree)),
			wantID:    "free-auto",
		},
		{
			name:      "unknown paid group does not fallback",
			requestID: "missing-paid-group",
			reg:       reg(autoGroup("redeem", AccessPolicyGrantRequired)),
			wantID:    "",
		},
		{
			name:      "missing card group uses configured default",
			requestID: "deleted-card-group",
			reg: func() *Registry {
				r := reg(autoGroup("redeem", AccessPolicyGrantRequired), autoGroup("coding-auto", AccessPolicyGrantRequired))
				r.DefaultServiceGroupID = "redeem"
				return r
			}(),
			wantID: "redeem",
		},
		{
			name:      "official prefers configured default grant group",
			requestID: llmpool.HubOfficialServiceGroupID,
			reg: func() *Registry {
				r := reg(autoGroup("premium-auto", AccessPolicyGrantRequired), autoGroup("redeem", AccessPolicyGrantRequired))
				r.DefaultServiceGroupID = "premium-auto"
				return r
			}(),
			wantID: "premium-auto",
		},
		{
			name:      "empty id skips local official entry",
			requestID: "",
			reg:       reg(autoGroup(llmpool.HubOfficialServiceGroupID, AccessPolicyFree), autoGroup("redeem", AccessPolicyGrantRequired)),
			wantID:    "redeem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dispatch := matchProxyServiceGroupModel(tt.reg, tt.requestID, "auto")
			if tt.wantID == "" {
				if got != nil {
					t.Fatalf("matched %q, want none", got.ID)
				}
				return
			}
			if got == nil || dispatch == nil {
				t.Fatalf("matched nil, want %q", tt.wantID)
			}
			if got.ID != tt.wantID {
				t.Fatalf("matched %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestHandleProxyRequestOfficialHubEntryUsesTenantComputeGroup(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"deepseek-v4-flash","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyGrantRequired,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "deepseek",
				Model:      "deepseek-v4-flash",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID:             "auth1",
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "redeem",
		CreditsTotal:   100,
		CreditsUsed:    0,
		StartsAt:       time.Now().UTC().Add(-time.Hour),
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		Status:         "active",
	}}}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: llmpool.HubOfficialServiceGroupID,
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.ProviderID != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", resp.ProviderID)
	}
	if seen["model"] != "deepseek-v4-flash" {
		t.Fatalf("upstream model = %#v, want deepseek-v4-flash", seen["model"])
	}
}

func TestHandleProxyRequestOfficialHubEntryIgnoresLocalGroupWithSameID(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"redeem-upstream","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "local", Name: "Local", APIURL: upstream.URL},
			{ID: "redeem-provider", Name: "Redeem", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           llmpool.HubOfficialServiceGroupID,
				Name:         "Misplaced Hub Entry",
				AccessPolicy: AccessPolicyFree,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "local",
					Model:      "local-upstream",
				}}}},
			},
			{
				ID:           "redeem",
				Name:         "Redeem",
				AccessPolicy: AccessPolicyGrantRequired,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "redeem-provider",
					Model:      "redeem-upstream",
				}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	now := time.Now().UTC()
	cfg := &ProxyConfig{
		Service: svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{auths: []*TenantAuthorization{{
			ID: "auth1", HubID: "hub1", TenantID: "tenant1", ServiceGroupID: "redeem",
			CreditsTotal: 100, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: "active",
		}}}),
		HTTPClient: upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: llmpool.HubOfficialServiceGroupID,
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.ProviderID != "redeem-provider" {
		t.Fatalf("provider = %q, want redeem-provider", resp.ProviderID)
	}
	if seen["model"] != "redeem-upstream" {
		t.Fatalf("upstream model = %#v, want redeem-upstream", seen["model"])
	}
}

func TestHandleProxyRequestOfficialHubEntryPrefersRedeemOverFree(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"redeem-upstream","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "free", Name: "Free", APIURL: upstream.URL},
			{ID: "redeem-provider", Name: "Redeem", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           "free-auto",
				Name:         "Free Auto",
				AccessPolicy: AccessPolicyFree,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "free",
					Model:      "free-upstream",
				}}}},
			},
			{
				ID:           "redeem",
				Name:         "Redeem",
				AccessPolicy: AccessPolicyGrantRequired,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "redeem-provider",
					Model:      "redeem-upstream",
				}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	now := time.Now().UTC()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID:             "auth1",
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "redeem",
		CreditsTotal:   100,
		CreditsUsed:    0,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
	}}}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: llmpool.HubOfficialServiceGroupID,
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.ProviderID != "redeem-provider" {
		t.Fatalf("provider = %q, want redeem-provider", resp.ProviderID)
	}
	if seen["model"] != "redeem-upstream" {
		t.Fatalf("upstream model = %#v, want redeem-upstream", seen["model"])
	}
}

func TestHandleProxyRequestOfficialHubEntrySkipsFreePolicyRedeem(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: "http://127.0.0.1"}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem Free",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-v4-flash"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	_, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  http.DefaultClient,
	}, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: llmpool.HubOfficialServiceGroupID,
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err == nil || !strings.Contains(err.Error(), `model "auto" not available on this HubCenter`) {
		t.Fatalf("HandleProxyRequest() error = %v, want official entry to skip free redeem", err)
	}
}

func TestHandleProxyRequestOfficialHubEntryDoesNotUseFreeOnlyCatalog(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "free", Name: "Free", APIURL: "http://127.0.0.1"}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "free-auto",
			Name:         "Free Auto",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "free", Model: "free-upstream"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	_, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  http.DefaultClient,
	}, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: llmpool.HubOfficialServiceGroupID,
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err == nil || !strings.Contains(err.Error(), `model "auto" not available on this HubCenter`) {
		t.Fatalf("HandleProxyRequest() error = %v, want official entry to require compute", err)
	}
}

func TestHandleProxyRequestUnknownExplicitServiceGroupDoesNotFallback(t *testing.T) {
	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: "http://127.0.0.1"}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem Free",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek", Model: "deepseek-v4-flash"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  http.DefaultClient,
	}
	_, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "missing-paid-group",
		Body:           map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err == nil || !strings.Contains(err.Error(), `model "auto" not available on this HubCenter`) {
		t.Fatalf("HandleProxyRequest() error = %v, want model unavailable", err)
	}
}

func TestHandleProxyRequestEstimatesAndInjectsUsageWhenUpstreamOmitsUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"qax-codegen/Auto","choices":[{"message":{"content":"estimated answer"}}]}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "official",
				Model:      "qax-codegen/Auto",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
		Usage:       usage,
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello official usage"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if len(usage.records) != 1 {
		t.Fatalf("usage records = %d, want 1", len(usage.records))
	}
	if len(usage.contexts) != 1 || usage.contexts[0].HubID != "hub1" || usage.contexts[0].TenantID != "t1" {
		t.Fatalf("usage context = %+v, want hub1/t1", usage.contexts)
	}
	if usage.records[0].InputTokens <= 0 || usage.records[0].OutputTokens <= 0 {
		t.Fatalf("usage record = %+v, want estimated input/output tokens", usage.records[0])
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	usagePayload, _ := payload["usage"].(map[string]any)
	if usagePayload == nil || usagePayload["estimated"] != true {
		t.Fatalf("response usage not injected as estimated: %#v", payload["usage"])
	}
	if usagePayload["total_tokens"].(float64) <= 0 {
		t.Fatalf("total_tokens = %#v, want positive", usagePayload["total_tokens"])
	}
}

func TestHandleProxyRequestCompletesPartialUsageFromEstimate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"qax-codegen/Auto","choices":[{"message":{"content":"partial usage answer"}}],"usage":{"prompt_tokens":13}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "official", Model: "qax-codegen/Auto"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: upstream.Client(), Usage: usage}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if len(usage.records) != 1 || usage.records[0].InputTokens != 13 || usage.records[0].OutputTokens <= 0 {
		t.Fatalf("usage record = %+v, want input=13 and estimated output", usage.records)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	usagePayload, _ := payload["usage"].(map[string]any)
	if usagePayload["prompt_tokens"].(float64) != 13 || usagePayload["completion_tokens"].(float64) <= 0 || usagePayload["estimated"] != true {
		t.Fatalf("usage payload = %#v, want completed estimated usage", usagePayload)
	}
}

func TestHandleProxyRequestPreservesInputOutputUsageShape(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"qax-codegen/Auto","choices":[{"message":{"content":"ok"}}],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "official", Model: "qax-codegen/Auto"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	usage := &recordingUsageRecorder{}
	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
		Usage:       usage,
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if len(usage.records) != 1 {
		t.Fatalf("usage records = %d, want 1", len(usage.records))
	}
	if usage.records[0].InputTokens != 11 || usage.records[0].OutputTokens != 7 {
		t.Fatalf("usage record = %+v, want input=11 output=7", usage.records[0])
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	usagePayload, _ := payload["usage"].(map[string]any)
	if usagePayload["estimated"] == true {
		t.Fatalf("existing input/output usage should not be marked estimated: %#v", usagePayload)
	}
	if usagePayload["input_tokens"].(float64) != 11 || usagePayload["output_tokens"].(float64) != 7 {
		t.Fatalf("usage payload = %#v, want original input/output fields", usagePayload)
	}
}

func TestProxyResponseUsageWithFallbackPatchesMissingUsageShapes(t *testing.T) {
	reqBody := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}}

	input, output, patched := proxyResponseUsageWithFallback(reqBody, []byte(`{"choices":[{"message":{"content":"fallback answer"}}]}`))
	if input <= 0 || output <= 0 || !bytes.Contains(patched, []byte(`"estimated":true`)) {
		t.Fatalf("missing usage fallback = %d/%d %s, want estimated usage", input, output, patched)
	}

	input, output, patched = proxyResponseUsageWithFallback(reqBody, []byte(`{"choices":[{"message":{"content":"fallback answer"}}],"usage":{"prompt_tokens":9}}`))
	if input != 9 || output <= 0 || !bytes.Contains(patched, []byte(`"completion_tokens"`)) {
		t.Fatalf("partial usage fallback = %d/%d %s, want completed output usage", input, output, patched)
	}

	body := []byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	input, output, patched = proxyResponseUsageWithFallback(reqBody, body)
	if input != 3 || output != 2 || string(patched) != string(body) {
		t.Fatalf("complete usage fallback = %d/%d %s, want original body", input, output, patched)
	}
}

func TestProxyResponseUsageWithFallbackPreservesExplicitZeroUsage(t *testing.T) {
	reqBody := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "this must not be estimated"}}}
	body := []byte(`{"choices":[{"message":{"content":"cached"}}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`)
	input, output, patched := proxyResponseUsageWithFallback(reqBody, body)
	if input != 0 || output != 0 || string(patched) != string(body) {
		t.Fatalf("explicit zero usage = %d/%d %s, want original zero usage", input, output, patched)
	}

	body = []byte(`{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":0,"completion_tokens":3,"total_tokens":3}}`)
	input, output, patched = proxyResponseUsageWithFallback(reqBody, body)
	if input != 0 || output != 3 || string(patched) != string(body) {
		t.Fatalf("directional zero usage = %d/%d %s, want original 0/3 usage", input, output, patched)
	}
}

func TestExtractTokenUsageAcceptsStringNumbers(t *testing.T) {
	input, output := extractTokenUsage([]byte(`{"usage":{"prompt_tokens":"12.0","completion_tokens":"8","total_tokens":"20"}}`))
	if input != 12 || output != 8 {
		t.Fatalf("usage = %d/%d, want 12/8", input, output)
	}
}

func TestExtractTokenUsageInfersMissingSideFromTotal(t *testing.T) {
	input, output := extractTokenUsage([]byte(`{"usage":{"completion_tokens":8,"total_tokens":20}}`))
	if input != 12 || output != 8 {
		t.Fatalf("usage = %d/%d, want 12/8", input, output)
	}
	input, output = extractTokenUsage([]byte(`{"usage":{"prompt_tokens":12,"total_tokens":20}}`))
	if input != 12 || output != 8 {
		t.Fatalf("usage = %d/%d, want 12/8", input, output)
	}
}

func TestExtractTokenUsageDoesNotEstimateWhenOnlyTotalIsPresent(t *testing.T) {
	reqBody := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "this must not inflate the total"}}}
	input, output, _ := proxyResponseUsageWithFallback(reqBody, []byte(`{"choices":[{"message":{"content":"answer"}}],"usage":{"total_tokens":20}}`))
	if input != 20 || output != 0 {
		t.Fatalf("total-only usage = %d/%d, want 20/0 without estimated output", input, output)
	}
}

func TestExtractTokenUsageFoldsSeparateReasoningTokensIntoOutput(t *testing.T) {
	input, output := extractTokenUsage([]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":550,"completion_tokens_details":{"reasoning_tokens":400}}}`))
	if input != 100 || output != 450 {
		t.Fatalf("separate reasoning usage = %d/%d, want 100/450", input, output)
	}
	input, output = extractTokenUsage([]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":500,"total_tokens":600,"completion_tokens_details":{"reasoning_tokens":400}}}`))
	if input != 100 || output != 500 {
		t.Fatalf("included reasoning usage = %d/%d, want 100/500", input, output)
	}
}

func TestEstimateProxyResponseTokensIncludesStructuredContentAndToolCalls(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":[{"type":"text","text":"structured answer"}],"tool_calls":[{"function":{"name":"lookup","arguments":"{\"id\":\"T-1\"}"}}],"function_call":{"name":"legacy_lookup","arguments":"{\"id\":\"T-2\"}"}}},{"text":"legacy completion text"}]}`)
	if got := estimateProxyResponseTokens(body); got <= 0 {
		t.Fatalf("estimateProxyResponseTokens() = %d, want positive", got)
	}
}

func TestEstimateProxyTokenUsageIncludesResponsesInputAndOutput(t *testing.T) {
	input, output := estimateProxyTokenUsage(
		map[string]any{"input": "hello responses", "instructions": "be concise"},
		[]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"responses answer"}]}]}`),
	)
	if input <= 0 || output <= 0 {
		t.Fatalf("usage = %d/%d, want positive input/output", input, output)
	}
}

func TestEstimateProxyTokenUsageCountsToolSchemas(t *testing.T) {
	input, _ := estimateProxyTokenUsage(
		map[string]any{
			"messages":        []any{map[string]any{"role": "user", "content": "use tool"}},
			"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "ticket", "schema": map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}}}}},
			"tool_choice":     map[string]any{"type": "function", "function": map[string]any{"name": "lookup_ticket"}},
			"tools": []any{map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup_ticket",
					"description": "look up a ticket by id",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string", "description": "ticket id"}}},
				},
			}},
		},
		[]byte(`{"choices":[{"message":{"content":"ok"}}]}`),
	)
	withoutTools, _ := estimateProxyTokenUsage(
		map[string]any{"messages": []any{map[string]any{"role": "user", "content": "use tool"}}},
		[]byte(`{"choices":[{"message":{"content":"ok"}}]}`),
	)
	if input <= withoutTools {
		t.Fatalf("input with tools = %d, without tools = %d, want tool schema counted", input, withoutTools)
	}
}

func TestHandleProxyRequestFallsBackToSingleProviderModel(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "official", Name: "MaClaw Official", APIURL: upstream.URL, Models: []string{"qax-codegen/Auto"}}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "maclaw-official",
			Name:         "MaClaw Official",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "official"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{}},
	}); err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if seen["model"] != "qax-codegen/Auto" {
		t.Fatalf("upstream model = %#v, want provider model fallback", seen["model"])
	}
}

func TestHandleProxyRequestSupportsLegacyProviderIDs(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "legacy", Name: "Legacy", APIURL: upstream.URL, Models: []string{"legacy-auto"}}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "legacy-group",
			Name:         "Legacy Group",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "auto", ProviderIDs: []string{"legacy"}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{}},
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen["model"] != "legacy-auto" {
		t.Fatalf("upstream model = %#v, want legacy provider single-model fallback", seen["model"])
	}
}

func TestHandleProxyRequestAllowsSameProviderWithDifferentModelRoutes(t *testing.T) {
	var seen map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "deepseek", Model: "deepseek-v4-flash", Priority: 10, ResolutionTier: 1, CreditMultiplier: 1},
				{ProviderID: "deepseek", Model: "deepseek-v4-pro", Priority: 50, ResolutionTier: 2, CreditMultiplier: 2},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	if _, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "t1",
		Body:     map[string]any{"model": "auto", "messages": []any{}},
	}); err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if seen["model"] != "deepseek-v4-flash" {
		t.Fatalf("upstream model = %#v, want cheaper x1 flash before x2 pro failover", seen["model"])
	}
}

func TestProxyCreditMultiplierForRouteHandlesNilModel(t *testing.T) {
	got := proxyCreditMultiplierForRoute(nil, llmpool.DispatchProviderRoute{ProviderID: "p1"})
	if got != 1 {
		t.Fatalf("multiplier = %v, want 1", got)
	}
}

func TestProxyCreditMultiplierForRouteDoesNotFallBackToProviderMapInRouteMode(t *testing.T) {
	model := &llmpool.DispatchModel{
		CreditMultiplier: 1.5,
		ProviderRoutes: []llmpool.DispatchProviderRoute{
			{ProviderID: "deepseek", Model: "deepseek-v4-flash", CreditMultiplier: 0},
			{ProviderID: "deepseek", Model: "deepseek-v4-pro", CreditMultiplier: 5},
		},
		ProviderCreditMultipliers: map[string]float64{"deepseek": 5},
	}

	got := proxyCreditMultiplierForRoute(model, model.ProviderRoutes[0])
	if got != 1.5 {
		t.Fatalf("multiplier = %v, want model fallback 1.5 without provider-map cross-talk", got)
	}
}

func TestProxyEffectiveCreditMultiplierUsesRequestStartVendorRate(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	provider := &llmpool.ProviderConfig{
		ID:               "deepseek",
		Timezone:         "Asia/Shanghai",
		CreditMultiplier: 1,
		CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
			Days:       []int{1, 2, 3, 4, 5},
			Start:      "00:30",
			End:        "08:30",
			Multiplier: 0.5,
		}},
	}
	offPeak := time.Date(2026, 8, 17, 2, 0, 0, 0, loc)
	if got := proxyEffectiveCreditMultiplier(provider, nil, llmpool.DispatchProviderRoute{}, offPeak); got != 0.5 {
		t.Fatalf("off-peak vendor rate = %v, want 0.5", got)
	}
	if got := proxyEffectiveCreditMultiplier(provider, nil, llmpool.DispatchProviderRoute{CreditMultiplier: 2}, offPeak); got != 1 {
		t.Fatalf("off-peak vendor 0.5 * route 2 = %v, want 1", got)
	}
	peak := time.Date(2026, 8, 17, 12, 0, 0, 0, loc)
	if got := proxyEffectiveCreditMultiplier(provider, nil, llmpool.DispatchProviderRoute{}, peak); got != 1 {
		t.Fatalf("peak vendor rate = %v, want 1", got)
	}
}

func TestProxySharedCreditMultiplierOmitsHeaderWhenRatesDiffer(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	offPeak := time.Date(2026, 8, 17, 2, 0, 0, 0, loc)
	cheap := &llmpool.ProviderConfig{
		ID: "deepseek", Timezone: "Asia/Shanghai", CreditMultiplier: 1,
		CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
			Days: []int{1, 2, 3, 4, 5}, Start: "00:30", End: "08:30", Multiplier: 0.5,
		}},
	}
	full := &llmpool.ProviderConfig{ID: "openai", Timezone: "Asia/Shanghai", CreditMultiplier: 1}
	if _, ok := proxySharedCreditMultiplier(nil, []*proxyDispatch{
		{provider: cheap},
		{provider: full},
	}, offPeak); ok {
		t.Fatal("expected mixed vendor rates to omit a shared multiplier header")
	}
	got, ok := proxySharedCreditMultiplier(nil, []*proxyDispatch{{provider: cheap}, {provider: cheap}}, offPeak)
	if !ok || got != 0.5 {
		t.Fatalf("shared off-peak = %v ok=%v, want 0.5 true", got, ok)
	}
}

func TestProxyHandlerStreamTrailersReportWinningProviderRate(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer good.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "cheap", Name: "Cheap", APIURL: bad.URL, CreditMultiplier: 0.5},
			{ID: "full", Name: "Full", APIURL: good.URL, CreditMultiplier: 2},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "cheap"},
				{ProviderID: "full"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: http.DefaultClient}
	center := httptest.NewServer(ProxyHandler(cfg))
	defer center.Close()

	req, err := http.NewRequest(http.MethodPost, center.URL+"/api/llm/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hub-ID", "hub1")
	req.Header.Set("X-Tenant-ID", "t1")
	resp, err := center.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"content":"ok"`) {
		t.Fatalf("stream body = %s", body)
	}
	if got := resp.Header.Get(llmpool.CreditMultiplierHeader); got != "" {
		t.Fatalf("mixed-rate stream should omit initial multiplier header, got %q", got)
	}
	if got := resp.Trailer.Get(llmpool.CreditMultiplierHeader); got != "2" {
		t.Fatalf("trailer multiplier = %q, want 2", got)
	}
	if got := resp.Trailer.Get(llmpool.ProviderIDHeader); got != "full" {
		t.Fatalf("trailer provider = %q, want full", got)
	}
}

func TestHandleProxyRequest_GrantRequiredBillsVendorRateAtRequestStart(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":4000,"completion_tokens":6000,"total_tokens":10000}}`))
	}))
	defer upstream.Close()

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{
			ID:               "deepseek",
			Name:             "DeepSeek",
			APIURL:           upstream.URL,
			Timezone:         "Asia/Shanghai",
			CreditMultiplier: 1,
			CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
				Days:       []int{1, 2, 3, 4, 5},
				Start:      "00:30",
				End:        "08:30",
				Multiplier: 0.5,
			}},
		}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []llmpool.ModelConfig{{Name: "deepseek-chat", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	run := func(startedAt time.Time) float64 {
		authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
			ID: "auth-tou", HubID: "hub1", TenantID: "t1", ServiceGroupID: "g1",
			CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
			Status: "active",
		}}}
		cfg := &ProxyConfig{
			Service:     svc,
			AuthChecker: NewAuthorizationChecker(authRepo),
			HTTPClient:  upstream.Client(),
			Usage:       &recordingUsageRecorder{},
		}
		resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
			HubID:     "hub1",
			TenantID:  "t1",
			StartedAt: startedAt,
			Body:      map[string]any{"model": "deepseek-chat", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		})
		if err != nil {
			t.Fatalf("HandleProxyRequest() error = %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		return authRepo.auths[0].CreditsUsed
	}

	offPeak := run(time.Date(2026, 8, 17, 2, 0, 0, 0, loc))
	if offPeak != 0.5 {
		t.Fatalf("off-peak credits = %.3f, want 0.5", offPeak)
	}
	peak := run(time.Date(2026, 8, 17, 12, 0, 0, 0, loc))
	if peak != 1 {
		t.Fatalf("peak credits = %.3f, want 1", peak)
	}
}

func TestHandleProxyRequestRetriesProviderAuthorizationFailure(t *testing.T) {
	var firstHits int
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits++
		http.Error(w, `{"error":{"message":"upstream key forbidden"}}`, http.StatusForbidden)
	}))
	defer first.Close()
	var secondHits int
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer second.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "p1", Name: "P1", APIURL: first.URL},
			{ID: "p2", Name: "P2", APIURL: second.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{
				{ProviderID: "p1"},
				{ProviderID: "p2"},
			}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  second.Client(),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.ProviderID != "p2" {
		t.Fatalf("status/provider = %d/%q, want 200/p2", resp.StatusCode, resp.ProviderID)
	}
	if firstHits != 1 || secondHits != 1 {
		t.Fatalf("provider hits = %d/%d, want 1/1", firstHits, secondHits)
	}
}

func TestHandleProxyRequestDoesNotReturnProviderAuthorizationAsTenantDenial(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"upstream key forbidden"}}`, http.StatusForbidden)
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "g1",
			Name:         "G1",
			AccessPolicy: AccessPolicyFree,
			Models:       []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: map[string]any{"model": "gpt-4"}}
	_, err := HandleProxyRequest(context.Background(), cfg, req)
	if err == nil {
		t.Fatal("HandleProxyRequest() error = nil, want provider failure")
	}
	if !contains(err.Error(), "all providers failed") || contains(err.Error(), "authorization denied") {
		t.Fatalf("error = %v, want provider failure without tenant authorization denial", err)
	}
}

func TestHandleProxyRequestProviderFailureIncludesRoutingDetails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(550)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream timeout"}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{ID: "deepseek", Name: "DeepSeek", APIURL: upstream.URL}},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyFree,
			Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "deepseek",
				Model:      "deepseek-v4-flash",
			}}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	_, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		HTTPClient:  upstream.Client(),
	}, &ProxyRequest{
		HubID:    "hub1",
		TenantID: "tenant1",
		Body:     map[string]any{"model": "auto", "messages": []any{}},
	})
	if err == nil {
		t.Fatal("HandleProxyRequest() error = nil, want provider failure")
	}
	for _, want := range []string{"all providers failed", "deepseek", "logical model auto", "upstream model deepseek-v4-flash", "550", "upstream timeout"} {
		if !contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}

func TestProxyProviderErrorSnippetPreservesUTF8(t *testing.T) {
	snippet := proxyProviderErrorSnippet(bytes.Repeat([]byte("审"), 510))
	if !utf8.ValidString(snippet) {
		t.Fatalf("snippet is invalid UTF-8: %q", snippet)
	}
	if len([]rune(snippet)) != 502 {
		t.Fatalf("snippet = %q", snippet)
	}
	if !bytes.HasSuffix([]byte(snippet), []byte("...")) {
		t.Fatalf("snippet should be truncated with ellipsis: %q", snippet)
	}

	snippet = proxyProviderErrorSnippet([]byte{'o', 'k', 0xff})
	if !utf8.ValidString(snippet) {
		t.Fatalf("invalid-byte snippet is invalid UTF-8: %q", snippet)
	}
	if snippet != ": ok\ufffd" {
		t.Fatalf("invalid-byte snippet = %q", snippet)
	}
}

func TestProxyProviderErrorSnippetRedactsSecrets(t *testing.T) {
	snippet := proxyProviderErrorSnippet([]byte(`Authorization: Bearer sk-live {"api_key":"abc123","password":"secret","openai_api_key":"provider-secret","x-api-key":"proxy-secret","accessToken":"access-secret"}`))
	for _, leaked := range []string{"sk-live", "abc123", "secret", "provider-secret", "proxy-secret", "access-secret"} {
		if strings.Contains(snippet, leaked) {
			t.Fatalf("snippet leaked %q: %q", leaked, snippet)
		}
	}
	if strings.Count(snippet, "[redacted]") != 6 {
		t.Fatalf("snippet = %q, want six redactions", snippet)
	}
}

func TestHandleProxyRequest_CacheHit(t *testing.T) {
	system := &mockSystemSettings{}
	svc := NewService(system)
	_ = svc.SaveRegistry(context.Background(), &Registry{
		Providers:     []llmpool.ProviderConfig{{ID: "p1", Name: "P1", APIURL: "http://localhost"}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	})

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth1", HubID: "hub1", TenantID: "t1", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}

	cache := llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 10})
	// Pre-populate cache
	body := map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	cacheKey := buildServiceGroupCacheKey("g1", "gpt-4", body)
	_ = cache.Put(context.Background(), &llmpool.CacheEntry{
		CacheKey:   cacheKey,
		ProviderID: "p1",
		Model:      "gpt-4",
		Payload:    []byte(`{"choices":[{"message":{"content":"cached"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`),
	})

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		Cache:       cache,
	}
	req := &ProxyRequest{HubID: "hub1", TenantID: "t1", Body: body}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.CacheHit {
		t.Fatal("expected cache hit")
	}
	if resp.ProviderID != "p1" {
		t.Fatalf("expected provider p1, got %s", resp.ProviderID)
	}
	if bytes.Contains(resp.Body, []byte(`"usage"`)) {
		t.Fatalf("cache hit response should not include billable usage: %s", resp.Body)
	}
	if resp.CreditMultiplier != 1 {
		t.Fatalf("cache hit default multiplier = %v, want 1", resp.CreditMultiplier)
	}
}

func TestHandleProxyRequest_CacheHitReportsCurrentVendorRate(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	system := &mockSystemSettings{}
	svc := NewService(system)
	_ = svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{{
			ID:               "p1",
			Name:             "P1",
			APIURL:           "http://localhost",
			Timezone:         "Asia/Shanghai",
			CreditMultiplier: 1,
			CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
				Days:       []int{1, 2, 3, 4, 5},
				Start:      "00:30",
				End:        "08:30",
				Multiplier: 0.5,
			}},
		}},
		ServiceGroups: []llmpool.ServiceGroup{{ID: "g1", Name: "G1", Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}}}}},
	})

	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth1", HubID: "hub1", TenantID: "t1", ServiceGroupID: "g1",
		CreditsTotal: 1000, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Status: "active",
	}}}

	cache := llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 10})
	body := map[string]any{"model": "gpt-4", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	cacheKey := buildServiceGroupCacheKey("g1", "gpt-4", body)
	_ = cache.Put(context.Background(), &llmpool.CacheEntry{
		CacheKey:   cacheKey,
		ProviderID: "p1",
		Model:      "gpt-4",
		Payload:    []byte(`{"choices":[{"message":{"content":"cached"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`),
	})

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(authRepo),
		Cache:       cache,
	}
	req := &ProxyRequest{
		HubID:     "hub1",
		TenantID:  "t1",
		Body:      body,
		StartedAt: time.Date(2026, 8, 17, 2, 0, 0, 0, loc),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.CacheHit {
		t.Fatal("expected cache hit")
	}
	if resp.CreditMultiplier != 0.5 {
		t.Fatalf("cache hit off-peak multiplier = %v, want 0.5", resp.CreditMultiplier)
	}
}

func TestHandleProxyRequestCacheIsScopedByServiceGroup(t *testing.T) {
	var providerCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","model":"free-upstream","choices":[{"message":{"content":"free-live"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer upstream.Close()

	system := &mockSystemSettings{}
	svc := NewService(system)
	if err := svc.SaveRegistry(context.Background(), &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "paid", Name: "Paid", APIURL: upstream.URL},
			{ID: "free", Name: "Free", APIURL: upstream.URL},
		},
		ServiceGroups: []llmpool.ServiceGroup{
			{
				ID:           "paid-group",
				Name:         "Paid Group",
				AccessPolicy: AccessPolicyFree,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "paid",
					Model:      "paid-upstream",
				}}}},
			},
			{
				ID:           "system-free",
				Name:         "System Free",
				AccessPolicy: AccessPolicyFree,
				Models: []llmpool.ModelConfig{{Name: "auto", ProviderConfigs: []llmpool.ModelProviderConfig{{
					ProviderID: "free",
					Model:      "free-upstream",
				}}}},
			},
		},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	cache := llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 10})
	_ = cache.Put(context.Background(), &llmpool.CacheEntry{
		CacheKey:   buildServiceGroupCacheKey("paid-group", "auto", body),
		ProviderID: "paid",
		Model:      "auto",
		Payload:    []byte(`{"choices":[{"message":{"content":"paid-cached"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`),
	})

	cfg := &ProxyConfig{
		Service:     svc,
		AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}),
		Cache:       cache,
		HTTPClient:  upstream.Client(),
	}
	resp, err := HandleProxyRequest(context.Background(), cfg, &ProxyRequest{
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "system-free",
		Body:           body,
	})
	if err != nil {
		t.Fatalf("HandleProxyRequest() error = %v", err)
	}
	if resp.CacheHit {
		t.Fatalf("system-free request must not hit paid-group cache")
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want 1", providerCalls)
	}
	if resp.ProviderID != "free" {
		t.Fatalf("provider = %q, want free", resp.ProviderID)
	}
	if !bytes.Contains(resp.Body, []byte("free-live")) {
		t.Fatalf("response body = %s, want live free response", resp.Body)
	}
}

func joinedProviderIDs(routes []llmpool.DispatchProviderRoute) string {
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		ids = append(ids, route.ProviderID)
	}
	return strings.Join(ids, ",")
}

func joinedProviderIDsAfter(routes []llmpool.DispatchProviderRoute, skip string) string {
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		if route.ProviderID == skip {
			continue
		}
		ids = append(ids, route.ProviderID)
	}
	return strings.Join(ids, ",")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestProxyQuotePinsProviderAndDirectionalPrice(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"first"}}],"usage":{"prompt_tokens":2,"completion_tokens":3}}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"second"}}],"usage":{"prompt_tokens":2,"completion_tokens":3}}`))
	}))
	defer second.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "first", APIURL: first.URL, CreditMultiplier: 0.5}, {ID: "second", APIURL: second.URL},
	}, ServiceGroups: []llmpool.ServiceGroup{{ID: "g", AccessPolicy: AccessPolicyFree, Models: []llmpool.ModelConfig{{Name: "m", ProviderConfigs: []llmpool.ModelProviderConfig{
		{ProviderID: "first", Model: "m", BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
		{ProviderID: "second", Model: "m", BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 9, OutputCreditsPer10K: 10}},
	}}}}}}); err != nil {
		t.Fatal(err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: http.DefaultClient, Quotes: NewProxyQuoteStore()}
	quoteSrv := httptest.NewServer(ProxyQuoteHandler(cfg))
	defer quoteSrv.Close()
	body := []byte(`{"model":"m"}`)
	quoteReq, _ := http.NewRequest(http.MethodPost, quoteSrv.URL, bytes.NewReader(body))
	quoteReq.Header.Set("X-Hub-ID", "hub")
	quoteReq.Header.Set("X-Tenant-ID", "tenant")
	quoteReq.Header.Set("X-MaClaw-Request-ID", "request")
	quoteResp, err := quoteSrv.Client().Do(quoteReq)
	if err != nil {
		t.Fatal(err)
	}
	defer quoteResp.Body.Close()
	var payload struct {
		Quote ProxyQuote `json:"quote"`
		Token string     `json:"token"`
	}
	if err := json.NewDecoder(quoteResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Quote.ProviderID != "first" || payload.Quote.Pricing.InputCreditsPer10K != 1 || payload.Quote.ProviderMultiplier != 0.5 {
		t.Fatalf("quote = %#v", payload.Quote)
	}

	proxySrv := httptest.NewServer(ProxyHandler(cfg))
	defer proxySrv.Close()
	proxyReq, _ := http.NewRequest(http.MethodPost, proxySrv.URL, bytes.NewReader(body))
	proxyReq.Header.Set("X-Hub-ID", "hub")
	proxyReq.Header.Set("X-Tenant-ID", "tenant")
	proxyReq.Header.Set("X-MaClaw-Request-ID", "request")
	proxyReq.Header.Set(llmpool.PricingQuoteHeader, payload.Token)
	proxyResp, err := proxySrv.Client().Do(proxyReq)
	if err != nil {
		t.Fatal(err)
	}
	defer proxyResp.Body.Close()
	if proxyResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", proxyResp.StatusCode)
	}
	snapshot, ok := llmpool.DecodeTokenPricingSnapshot(proxyResp.Header.Get(llmpool.TokenPricingSnapshotHeader))
	if !ok || snapshot.ProviderID != "first" || snapshot.Pricing.InputCreditsPer10K != 1 || snapshot.ProviderMultiplier != 0.5 {
		t.Fatalf("pricing snapshot = %#v", snapshot)
	}
}

func TestProxyQuoteForStreamPinsStreamCapableRouteAndRecordsAttempt(t *testing.T) {
	nonStream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("non-stream route must not receive a quoted stream request")
	}))
	defer nonStream.Close()
	stream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer stream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "responses-only", APIURL: nonStream.URL, WireAPI: "responses"},
		{ID: "streaming", APIURL: stream.URL, WireAPI: "chat"},
	}, ServiceGroups: []llmpool.ServiceGroup{{ID: "g", AccessPolicy: AccessPolicyFree, Models: []llmpool.ModelConfig{{Name: "m", ProviderConfigs: []llmpool.ModelProviderConfig{
		{ProviderID: "responses-only", Model: "m", BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 9, OutputCreditsPer10K: 10}},
		{ProviderID: "streaming", Model: "m", BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
	}}}}}}); err != nil {
		t.Fatal(err)
	}
	attempts := NewProxyBillingAttemptStore()
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: http.DefaultClient, Quotes: NewProxyQuoteStore(), Attempts: attempts}
	quoteSrv := httptest.NewServer(ProxyQuoteHandler(cfg))
	defer quoteSrv.Close()
	body := []byte(`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	quoteReq, _ := http.NewRequest(http.MethodPost, quoteSrv.URL, bytes.NewReader(body))
	quoteReq.Header.Set("X-Hub-ID", "hub")
	quoteReq.Header.Set("X-Tenant-ID", "tenant")
	quoteReq.Header.Set("X-MaClaw-Request-ID", "request")
	quoteResp, err := quoteSrv.Client().Do(quoteReq)
	if err != nil {
		t.Fatal(err)
	}
	defer quoteResp.Body.Close()
	var quotePayload struct {
		Quote ProxyQuote `json:"quote"`
		Token string     `json:"token"`
	}
	if err := json.NewDecoder(quoteResp.Body).Decode(&quotePayload); err != nil {
		t.Fatal(err)
	}
	if quoteResp.StatusCode != http.StatusOK || quotePayload.Quote.ProviderID != "streaming" {
		t.Fatalf("quoted stream route = %#v, status=%d; want streaming route", quotePayload.Quote, quoteResp.StatusCode)
	}

	proxySrv := httptest.NewServer(ProxyHandler(cfg))
	defer proxySrv.Close()
	proxyReq, _ := http.NewRequest(http.MethodPost, proxySrv.URL, bytes.NewReader(body))
	proxyReq.Header.Set("X-Hub-ID", "hub")
	proxyReq.Header.Set("X-Tenant-ID", "tenant")
	proxyReq.Header.Set("X-MaClaw-Request-ID", "request")
	proxyReq.Header.Set(llmpool.PricingQuoteHeader, quotePayload.Token)
	proxyResp, err := proxySrv.Client().Do(proxyReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(proxyResp.Body)
	_ = proxyResp.Body.Close()
	if proxyResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", proxyResp.StatusCode)
	}
	attempt, ok := attempts.Get("hub", "tenant", "request")
	if !ok {
		t.Fatal("quoted stream did not record a billing attempt")
	}
	if attempt.ProviderID != "streaming" || attempt.PricingSnapshot.InputTokens != 2 || attempt.PricingSnapshot.OutputTokens != 3 || attempt.PricingSnapshot.Pricing.OutputCreditsPer10K != 2 {
		t.Fatalf("billing attempt = %#v", attempt)
	}
}

func TestHandleProxyRequestChargesDirectionalPriceWithGroupMultiplierOnce(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10000,"completion_tokens":1000}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{Providers: []llmpool.ProviderConfig{{
		ID: "p", APIURL: upstream.URL, CreditMultiplier: 1.5,
		TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 2, OutputCreditsPer10K: 8},
	}}, ServiceGroups: []llmpool.ServiceGroup{{
		ID: "g", AccessPolicy: AccessPolicyGrantRequired,
		Models: []llmpool.ModelConfig{{Name: "m", CreditMultiplier: 2, ProviderConfigs: []llmpool.ModelProviderConfig{{
			ProviderID: "p", Model: "m", BillingMode: llmpool.BillingModePaid,
			// This stale route price must not replace the selected provider's
			// configured HubCenter input/output price.
			TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4},
		}}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth", HubID: "hub", TenantID: "tenant", ServiceGroupID: "g", CreditsTotal: 100,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: "active",
	}}}
	usage := &recordingUsageRecorder{}
	resp, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service: svc, AuthChecker: NewAuthorizationChecker(authRepo), HTTPClient: upstream.Client(), Usage: usage,
	}, &ProxyRequest{HubID: "hub", TenantID: "tenant", StartedAt: now, Body: map[string]any{"model": "m"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 8.4 {
		t.Fatalf("credits used = %v, want 8.4 (provider directional price x provider x group multiplier)", got)
	}
	if resp.CreditMultiplier != 3 {
		t.Fatalf("response multiplier = %v, want combined provider and service-group multiplier 3", resp.CreditMultiplier)
	}
	if resp.PricingSnapshot == nil || resp.PricingSnapshot.Pricing.InputCreditsPer10K != 2 || resp.PricingSnapshot.Pricing.OutputCreditsPer10K != 8 || resp.PricingSnapshot.ProviderMultiplier != 1.5 {
		t.Fatalf("pricing snapshot = %#v, want provider price 2/8 and multiplier 1.5", resp.PricingSnapshot)
	}
	if len(usage.records) != 1 || usage.records[0].Credits != 8.4 {
		t.Fatalf("usage records = %#v, want directional 8.4-credit debit", usage.records)
	}
}

func TestHandleProxyRequestDoesNotBillExplicitFreeRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10000,"completion_tokens":10000}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{Providers: []llmpool.ProviderConfig{{
		ID: "p", APIURL: upstream.URL, CreditMultiplier: 9,
	}}, ServiceGroups: []llmpool.ServiceGroup{{
		ID: "g", AccessPolicy: AccessPolicyGrantRequired,
		Models: []llmpool.ModelConfig{{Name: "m", CreditMultiplier: 7, ProviderConfigs: []llmpool.ModelProviderConfig{{
			ProviderID: "p", Model: "m", BillingMode: llmpool.BillingModeFree,
		}}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth", HubID: "hub", TenantID: "tenant", ServiceGroupID: "g", CreditsTotal: 100,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: "active",
	}}}
	usage := &recordingUsageRecorder{}
	resp, err := HandleProxyRequest(context.Background(), &ProxyConfig{
		Service: svc, AuthChecker: NewAuthorizationChecker(authRepo), HTTPClient: upstream.Client(), Usage: usage,
	}, &ProxyRequest{HubID: "hub", TenantID: "tenant", StartedAt: now, Body: map[string]any{"model": "m"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 0 {
		t.Fatalf("credits used = %v, want 0 for explicit free route", got)
	}
	if resp.CreditMultiplier != 1 {
		t.Fatalf("response multiplier = %v, want 1 for explicit free route", resp.CreditMultiplier)
	}
	if len(usage.records) != 1 || usage.records[0].Credits != 0 {
		t.Fatalf("usage records = %#v, want zero-credit free record", usage.records)
	}
}

func TestHandleProxyStreamRequestDoesNotBillExplicitFreeRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}],\"usage\":{\"prompt_tokens\":10000,\"completion_tokens\":10000}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{Providers: []llmpool.ProviderConfig{{
		ID: "p", APIURL: upstream.URL, CreditMultiplier: 9,
	}}, ServiceGroups: []llmpool.ServiceGroup{{
		ID: "g", AccessPolicy: AccessPolicyGrantRequired,
		Models: []llmpool.ModelConfig{{Name: "m", CreditMultiplier: 7, ProviderConfigs: []llmpool.ModelProviderConfig{{
			ProviderID: "p", Model: "m", BillingMode: llmpool.BillingModeFree,
		}}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth", HubID: "hub", TenantID: "tenant", ServiceGroupID: "g", CreditsTotal: 100,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: "active",
	}}}
	usage := &recordingUsageRecorder{}
	writer := newLockedResponseRecorder()
	err := HandleProxyStreamRequest(context.Background(), &ProxyConfig{
		Service: svc, AuthChecker: NewAuthorizationChecker(authRepo), HTTPClient: upstream.Client(), Usage: usage,
	}, &ProxyRequest{HubID: "hub", TenantID: "tenant", StartedAt: now, Body: map[string]any{"model": "m", "stream": true}}, writer)
	if err != nil {
		t.Fatal(err)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 0 {
		t.Fatalf("credits used = %v, want 0 for explicit free stream route", got)
	}
	if len(usage.records) != 1 || usage.records[0].Credits != 0 {
		t.Fatalf("usage records = %#v, want zero-credit free stream record", usage.records)
	}
}

func TestHandleProxyStreamRequestFreezesDirectionalPriceAtStart(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}],\"usage\":{\"prompt_tokens\":10000,\"completion_tokens\":1000}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	peakInput := 3.0
	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{Providers: []llmpool.ProviderConfig{{
		ID: "p", APIURL: upstream.URL, CreditMultiplier: 2,
		TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4, Timezone: "Asia/Shanghai", PriceSchedule: []llmpool.TokenPriceWindow{{
			ID: "peak", Days: []int{1}, Start: "12:00", End: "13:00", InputCreditsPer10K: &peakInput,
		}}},
	}}, ServiceGroups: []llmpool.ServiceGroup{{
		ID: "g", AccessPolicy: AccessPolicyGrantRequired,
		Models: []llmpool.ModelConfig{{Name: "m", CreditMultiplier: 2, ProviderConfigs: []llmpool.ModelProviderConfig{{
			ProviderID: "p", Model: "m", BillingMode: llmpool.BillingModePaid,
			// A legacy route value may remain, but the provider-owned schedule
			// is authoritative for this request.
			TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 4, Timezone: "Asia/Shanghai", PriceSchedule: []llmpool.TokenPriceWindow{{
				ID: "peak", Days: []int{1}, Start: "12:00", End: "13:00", InputCreditsPer10K: &peakInput,
			}}},
		}}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 8, 24, 12, 30, 0, 0, loc)
	now := time.Now()
	authRepo := &mockAuthRepo{auths: []*TenantAuthorization{{
		ID: "auth", HubID: "hub", TenantID: "tenant", ServiceGroupID: "g", CreditsTotal: 100,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: "active",
	}}}
	usage := &recordingUsageRecorder{}
	writer := newLockedResponseRecorder()
	err = HandleProxyStreamRequest(context.Background(), &ProxyConfig{
		Service: svc, AuthChecker: NewAuthorizationChecker(authRepo), HTTPClient: upstream.Client(), Usage: usage,
	}, &ProxyRequest{HubID: "hub", TenantID: "tenant", StartedAt: startedAt, Body: map[string]any{"model": "m", "stream": true}}, writer)
	if err != nil {
		t.Fatal(err)
	}
	if got := authRepo.auths[0].CreditsUsed; got != 13.6 {
		t.Fatalf("credits used = %v, want 13.6 from frozen provider peak price and both multipliers", got)
	}
	if len(usage.records) != 1 || usage.records[0].Credits != 13.6 {
		t.Fatalf("usage records = %#v, want frozen directional 13.6-credit debit", usage.records)
	}
}

func TestProxyStreamBillingPreservesExplicitZeroUsage(t *testing.T) {
	result := &providerStreamResult{}
	if _, err := proxyStreamPatchAndMeasureData([]byte(`{"choices":[{"delta":{"content":"cached"}}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`), "m", result); err != nil {
		t.Fatalf("measure stream usage: %v", err)
	}
	if !result.inputTokensObserved || !result.outputTokensObserved || result.inputTokens != 0 || result.outputTokens != 0 {
		t.Fatalf("stream usage = %#v, want observed explicit zeroes", result)
	}

	attempts := NewProxyBillingAttemptStore()
	dispatch := &proxyDispatch{
		model:        "m",
		matchedGroup: &llmpool.ServiceGroup{ID: "g"},
		provider:     &llmpool.ProviderConfig{ID: "p"},
		pricing:      &llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
	}
	recordProxyStreamUsage(context.Background(), &ProxyConfig{Attempts: attempts}, &ProxyRequest{
		HubID: "hub", TenantID: "tenant", RequestID: "request",
		Body: map[string]any{"messages": []any{map[string]any{"role": "user", "content": "this must not be estimated"}}},
	}, dispatch, "p", result)
	attempt, ok := attempts.Get("hub", "tenant", "request")
	if !ok || attempt.PricingSnapshot.InputTokens != 0 || attempt.PricingSnapshot.OutputTokens != 0 {
		t.Fatalf("billing attempt = %#v ok=%v, want explicit zero usage", attempt, ok)
	}
}

func TestExtractProxyTokenUsagePreservesExplicitDirectionalZero(t *testing.T) {
	input, output, inputObserved, outputObserved := extractTokenUsageFromMapWithPresence(map[string]any{
		"prompt_tokens":     float64(0),
		"completion_tokens": float64(3),
		"total_tokens":      float64(3),
	})
	if !inputObserved || !outputObserved || input != 0 || output != 3 {
		t.Fatalf("usage = input=%d output=%d observed=%v/%v, want explicit 0/3", input, output, inputObserved, outputObserved)
	}
}

func TestExtractProxyTokenUsageTotalZeroCompletesMissingDirection(t *testing.T) {
	input, output, inputObserved, outputObserved := extractTokenUsageFromMapWithPresence(map[string]any{
		"prompt_tokens": float64(0),
		"total_tokens":  float64(0),
	})
	if !inputObserved || !outputObserved || input != 0 || output != 0 {
		t.Fatalf("usage = input=%d output=%d observed=%v/%v, want explicit total zero", input, output, inputObserved, outputObserved)
	}
}

func TestProxyQuoteRejectsMismatchedRequest(t *testing.T) {
	store := NewProxyQuoteStore()
	quote, err := store.Put(ProxyQuote{RequestDigest: proxyRequestDigest([]byte(`{"model":"m"}`)), HubID: "hub", TenantID: "tenant", RequestID: "request", ServiceGroupID: "g", LogicalModel: "m", ProviderID: "p", ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ProxyConfig{Quotes: store}
	if _, ok := proxyQuoteFromRequest(cfg, quote.Token, &ProxyRequest{HubID: "hub", TenantID: "tenant", RequestID: "other", RawBody: []byte(`{"model":"m"}`)}); ok {
		t.Fatal("mismatched request accepted quote")
	}
	if _, ok := proxyQuoteFromRequest(cfg, quote.Token, &ProxyRequest{HubID: "hub", TenantID: "tenant", RequestID: "request", RawBody: []byte(`{"model":"m"}`)}); !ok {
		t.Fatal("mismatched request consumed quote")
	}
}

func TestProxyQuoteRejectsDifferentPayloadWithoutConsumingIt(t *testing.T) {
	store := NewProxyQuoteStore()
	payload := []byte(`{"model":"m","messages":[{"role":"user","content":"priced"}]}`)
	quote, err := store.Put(ProxyQuote{RequestDigest: proxyRequestDigest(payload), HubID: "hub", TenantID: "tenant", RequestID: "request", ServiceGroupID: "g", LogicalModel: "m", ProviderID: "p", ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ProxyConfig{Quotes: store}
	if _, ok := proxyQuoteFromRequest(cfg, quote.Token, &ProxyRequest{HubID: "hub", TenantID: "tenant", RequestID: "request", RawBody: []byte(`{"model":"m","messages":[{"role":"user","content":"substituted"}]}`)}); ok {
		t.Fatal("different payload accepted quote")
	}
	if _, ok := proxyQuoteFromRequest(cfg, quote.Token, &ProxyRequest{HubID: "hub", TenantID: "tenant", RequestID: "request", RawBody: payload}); !ok {
		t.Fatal("different payload consumed quote")
	}
}

func TestClaimedProxyQuoteRemainsUsableAfterAdmissionTTL(t *testing.T) {
	store := NewProxyQuoteStore()
	body := []byte(`{"model":"m"}`)
	quote, err := store.Put(ProxyQuote{
		RequestDigest:  proxyRequestDigest(body),
		HubID:          "hub",
		TenantID:       "tenant",
		RequestID:      "request",
		ServiceGroupID: "g",
		LogicalModel:   "m",
		ProviderID:     "provider",
		UpstreamModel:  "m",
		ExpiresAt:      time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok := store.Claim(quote.Token, "hub", "tenant", "request", proxyRequestDigest(body))
	if !ok || !claimed.Claimed {
		t.Fatalf("claim = %#v ok=%v", claimed, ok)
	}
	claimed.ExpiresAt = time.Now().Add(-time.Second)
	routes, err := proxyOrderedRoutesForRequest(nil, &Registry{Providers: []llmpool.ProviderConfig{{ID: "provider"}}}, &ProxyRequest{HubID: "hub", TenantID: "tenant", RequestID: "request", Body: map[string]any{"model": "m"}, Quote: &claimed}, &llmpool.ServiceGroup{ID: "g"}, &llmpool.DispatchModel{ProviderRoutes: []llmpool.DispatchProviderRoute{{ProviderID: "provider", Model: "m"}}}, "m", acceptLiveProvider, time.Now(), false)
	if err != nil || len(routes) != 1 || routes[0].ProviderID != "provider" {
		t.Fatalf("claimed expired quote routes=%#v err=%v", routes, err)
	}
	claimed.Claimed = false
	if _, err := proxyOrderedRoutesForRequest(nil, &Registry{Providers: []llmpool.ProviderConfig{{ID: "provider"}}}, &ProxyRequest{HubID: "hub", TenantID: "tenant", RequestID: "request", Body: map[string]any{"model": "m"}, Quote: &claimed}, &llmpool.ServiceGroup{ID: "g"}, &llmpool.DispatchModel{ProviderRoutes: []llmpool.DispatchProviderRoute{{ProviderID: "provider", Model: "m"}}}, "m", acceptLiveProvider, time.Now(), false); err == nil {
		t.Fatal("unclaimed expired quote was accepted")
	}
}

func TestProxyQuoteCanOnlyBeUsedOnce(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":3}}`))
	}))
	defer upstream.Close()

	svc := NewService(&mockSystemSettings{})
	if err := svc.SaveRegistry(context.Background(), &Registry{Providers: []llmpool.ProviderConfig{{ID: "provider", APIURL: upstream.URL}}, ServiceGroups: []llmpool.ServiceGroup{{ID: "g", AccessPolicy: AccessPolicyFree, Models: []llmpool.ModelConfig{{Name: "m", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "provider", Model: "m", BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}}}}}}}}); err != nil {
		t.Fatal(err)
	}
	cfg := &ProxyConfig{Service: svc, AuthChecker: NewAuthorizationChecker(&mockAuthRepo{}), HTTPClient: http.DefaultClient, Quotes: NewProxyQuoteStore()}
	quoteSrv := httptest.NewServer(ProxyQuoteHandler(cfg))
	defer quoteSrv.Close()
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	quoteReq, _ := http.NewRequest(http.MethodPost, quoteSrv.URL, bytes.NewReader(body))
	quoteReq.Header.Set("X-Hub-ID", "hub")
	quoteReq.Header.Set("X-Tenant-ID", "tenant")
	quoteReq.Header.Set("X-MaClaw-Request-ID", "request")
	quoteResp, err := quoteSrv.Client().Do(quoteReq)
	if err != nil {
		t.Fatal(err)
	}
	defer quoteResp.Body.Close()
	var quotePayload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(quoteResp.Body).Decode(&quotePayload); err != nil {
		t.Fatal(err)
	}

	proxySrv := httptest.NewServer(ProxyHandler(cfg))
	defer proxySrv.Close()
	for attempt := 0; attempt < 2; attempt++ {
		proxyReq, _ := http.NewRequest(http.MethodPost, proxySrv.URL, bytes.NewReader(body))
		proxyReq.Header.Set("X-Hub-ID", "hub")
		proxyReq.Header.Set("X-Tenant-ID", "tenant")
		proxyReq.Header.Set("X-MaClaw-Request-ID", "request")
		proxyReq.Header.Set(llmpool.PricingQuoteHeader, quotePayload.Token)
		proxyResp, err := proxySrv.Client().Do(proxyReq)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(proxyResp.Body)
		_ = proxyResp.Body.Close()
		wantStatus := http.StatusOK
		if attempt == 1 {
			wantStatus = http.StatusConflict
		}
		if proxyResp.StatusCode != wantStatus {
			t.Fatalf("attempt %d status=%d, want %d", attempt, proxyResp.StatusCode, wantStatus)
		}
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls=%d, want exactly one", got)
	}
}

func TestProxyBillingAttemptStoreScopesLookupAndExpires(t *testing.T) {
	store := NewProxyBillingAttemptStore()
	now := time.Now().UTC()
	store.Record(ProxyBillingAttempt{
		HubID: "hub-a", TenantID: "tenant-a", RequestID: "request-a", StatusCode: http.StatusOK, ProviderID: "provider-a",
		PricingSnapshot: llmpool.TokenPricingSnapshot{ProviderID: "provider-a", InputTokens: 12, OutputTokens: 3}, CompletedAt: now,
	})
	if _, ok := store.Get("hub-b", "tenant-a", "request-a"); ok {
		t.Fatal("cross-hub billing attempt lookup succeeded")
	}
	attempt, ok := store.Get("hub-a", "tenant-a", "request-a")
	if !ok || attempt.PricingSnapshot.InputTokens != 12 {
		t.Fatalf("attempt=%#v ok=%v", attempt, ok)
	}
	store.Record(ProxyBillingAttempt{
		HubID: "hub-a", TenantID: "tenant-a", RequestID: "request-a", StatusCode: http.StatusOK, ProviderID: "provider-b",
		PricingSnapshot: llmpool.TokenPricingSnapshot{ProviderID: "provider-b", InputTokens: 99, OutputTokens: 99}, CompletedAt: now.Add(time.Second),
	})
	attempt, ok = store.Get("hub-a", "tenant-a", "request-a")
	if !ok || attempt.ProviderID != "provider-a" || attempt.PricingSnapshot.InputTokens != 12 {
		t.Fatalf("billing attempt was overwritten: %#v ok=%v", attempt, ok)
	}
	store.Record(ProxyBillingAttempt{HubID: "hub-a", TenantID: "tenant-a", RequestID: "expired", StatusCode: http.StatusOK, ProviderID: "provider-a", CompletedAt: now.Add(-proxyBillingAttemptTTL - time.Second)})
	if _, ok := store.Get("hub-a", "tenant-a", "expired"); ok {
		t.Fatal("expired billing attempt lookup succeeded")
	}
}

// The hub stream path must translate a client-side reasoning request into the
// upstream provider's native spelling: Agnes only honors reasoning_effort and
// silently ignores the DeepSeek-style thinking object, which used to make the
// desktop thinking panel stay empty despite thinking being enabled.
func TestStreamProviderToWriterRetargetsReasoningControlsForAgnes(t *testing.T) {
	newSSEClient := func(seen *map[string]any) *http.Client {
		return &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(req.Body).Decode(seen); err != nil {
				t.Fatalf("decode upstream request: %v", err)
			}
			stream := "data: {\"id\":\"c1\",\"model\":\"agnes-2.5-flash\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n" +
				"data: {\"id\":\"c2\",\"model\":\"agnes-2.5-flash\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2}}\n\n" +
				"data: [DONE]\n\n"
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(stream)),
			}, nil
		})}
	}
	provider := &llmpool.ProviderConfig{ID: "agnes", Name: "Agnes", APIURL: "https://api.agnes-ai.cn/v1"}

	t.Run("thinking object becomes reasoning effort", func(t *testing.T) {
		var seen map[string]any
		dst := newLockedResponseRecorder()
		if _, err := streamProviderToWriter(context.Background(), newSSEClient(&seen), provider, map[string]any{
			"model":    "auto",
			"thinking": map[string]any{"type": "enabled"},
		}, "agnes-2.5-flash", "auto", dst); err != nil {
			t.Fatalf("streamProviderToWriter() error = %v", err)
		}
		if got := seen["reasoning_effort"]; got != "medium" {
			t.Fatalf("upstream reasoning_effort = %#v, want medium", got)
		}
		if _, exists := seen["thinking"]; exists {
			t.Fatalf("upstream request must not keep the thinking object: %#v", seen)
		}
		// reasoning_content must survive the proxy so the client can render it.
		if out := dst.BodyString(); !strings.Contains(out, `"reasoning_content":"think"`) {
			t.Fatalf("client stream = %q, want reasoning_content passthrough", out)
		}
	})

	t.Run("auto body stays untouched", func(t *testing.T) {
		var seen map[string]any
		dst := newLockedResponseRecorder()
		if _, err := streamProviderToWriter(context.Background(), newSSEClient(&seen), provider, map[string]any{
			"model": "auto",
		}, "agnes-2.5-flash", "auto", dst); err != nil {
			t.Fatalf("streamProviderToWriter() error = %v", err)
		}
		for _, key := range []string{"thinking", "reasoning", "reasoning_effort", "enable_thinking"} {
			if _, exists := seen[key]; exists {
				t.Fatalf("auto upstream request gained %q: %#v", key, seen)
			}
		}
	})
}
