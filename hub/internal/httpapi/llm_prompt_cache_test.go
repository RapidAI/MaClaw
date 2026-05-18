package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type promptCacheMaintenanceStub struct {
	deleteExpiredCalls atomic.Int32
	trimCalls          atomic.Int32
}

func (s *promptCacheMaintenanceStub) Get(context.Context, string) (*store.LLMPromptCacheEntry, error) {
	return nil, nil
}

func (s *promptCacheMaintenanceStub) Put(context.Context, *store.LLMPromptCacheEntry) error {
	return nil
}

func (s *promptCacheMaintenanceStub) DeleteExpired(context.Context, time.Time) (int64, error) {
	s.deleteExpiredCalls.Add(1)
	return 0, nil
}

func (s *promptCacheMaintenanceStub) TrimDiskToBytes(context.Context, int64) (int64, error) {
	s.trimCalls.Add(1)
	return 0, nil
}

func (s *promptCacheMaintenanceStub) Status(context.Context, time.Time) (*llmcache.Status, error) {
	return &llmcache.Status{}, nil
}

func TestLLMPromptCacheableRequiresDeterministicRequestShape(t *testing.T) {
	resetLLMPromptCacheMetricsForTest()
	cfg := defaultHubLLMPromptCacheConfig()
	if !llmPromptCacheable(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}, "temperature": 0, "n": 1, "top_p": 1}, cfg) {
		t.Fatal("expected deterministic request to be cacheable")
	}
	if llmPromptCacheable(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}, "temperature": 0.7}, cfg) {
		t.Fatal("expected sampled request to bypass cache")
	}
	if llmPromptCacheable(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}, "n": 2}, cfg) {
		t.Fatal("expected multi-choice request to bypass cache")
	}
	if !llmPromptCacheable(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}, "stream": true}, cfg) {
		t.Fatal("expected streaming request to be cacheable because Hub forwards upstream as non-streaming JSON")
	}
}

func TestLLMPromptCacheRuntimeMetricsAreTenantScoped(t *testing.T) {
	resetLLMPromptCacheMetricsForTest()
	cfg := defaultHubLLMPromptCacheConfig()
	tenantA := withLLMPromptCacheTenant(context.Background(), "tenant_a")
	tenantB := withLLMPromptCacheTenant(context.Background(), "tenant_b")

	if !llmPromptCacheableForTenant(tenantA, map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}}, cfg) {
		t.Fatal("tenant_a request should be cacheable")
	}
	if llmPromptCacheableForTenant(tenantB, map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}, "temperature": 0.8}, cfg) {
		t.Fatal("tenant_b sampled request should bypass cache")
	}
	recordLLMPromptCacheSingleflightShared(tenantA)

	global := hubLLMPromptCacheRuntimeMetricsSnapshot()
	if global.CacheableRequests != 1 || global.BypassTemperature != 1 || global.SingleflightSharedHits != 1 || global.SingleflightSavedCalls != 1 {
		t.Fatalf("unexpected global metrics: %#v", global)
	}
	a := hubLLMPromptCacheRuntimeMetricsSnapshotForTenant("tenant_a")
	if a.CacheableRequests != 1 || a.BypassTemperature != 0 || a.SingleflightSharedHits != 1 || a.SingleflightSavedCalls != 1 {
		t.Fatalf("unexpected tenant_a metrics: %#v", a)
	}
	b := hubLLMPromptCacheRuntimeMetricsSnapshotForTenant("tenant_b")
	if b.CacheableRequests != 0 || b.BypassTemperature != 1 || b.SingleflightSharedHits != 0 || b.BypassReasons["temperature"] != 1 {
		t.Fatalf("unexpected tenant_b metrics: %#v", b)
	}
}

func TestLLMPromptCacheKeyNormalizesEquivalentRequestFields(t *testing.T) {
	model := &llmservice.AuthorizedModel{Name: "AUTO", ProviderIDs: []string{"provider-a", "provider-b"}}
	bodyA := map[string]any{
		"model":       "AUTO",
		"messages":    []any{map[string]any{"role": "user", "content": "repeat this"}},
		"temperature": 0,
		"top_p":       1,
		"n":           1,
		"stream":      false,
		"user":        "user-123",
		"metadata":    map[string]any{"trace_id": "abc"},
		"tool_choice": "auto",
	}
	bodyB := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "repeat this"}},
	}
	keyA, hashA, err := llmPromptCacheKey(model, bodyA, "Auto", defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("keyA error: %v", err)
	}
	keyB, hashB, err := llmPromptCacheKey(model, bodyB, "auto", defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("keyB error: %v", err)
	}
	if keyA != keyB || hashA != hashB {
		t.Fatalf("expected equivalent bodies to share cache key: %q/%q vs %q/%q", keyA, hashA, keyB, hashB)
	}
}

func TestLLMPromptCacheKeyCollapsesStreamingAndModelAliases(t *testing.T) {
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}}
	bodyA := map[string]any{
		"model":             "gpt-5-codex",
		"messages":          []any{map[string]any{"role": "user", "content": "repeat this"}},
		"stream":            true,
		"stream_options":    map[string]any{"include_usage": true},
		"prompt_cache_key":  "client-generated-key",
		"service_tier":      "auto",
		"safety_identifier": "user-123",
		"store":             false,
		"temperature":       0,
	}
	bodyB := map[string]any{
		"model":       "auto",
		"messages":    []any{map[string]any{"role": "user", "content": "repeat this"}},
		"temperature": 0,
	}
	keyA, _, err := llmPromptCacheKey(model, bodyA, "gpt-5-codex", defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("keyA error: %v", err)
	}
	keyB, _, err := llmPromptCacheKey(model, bodyB, "auto", defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("keyB error: %v", err)
	}
	if keyA != keyB {
		t.Fatalf("expected streaming/model alias requests to share cache key: %q vs %q", keyA, keyB)
	}
}

func TestLLMPromptCacheKeyCollapsesOpenAIDefaultNoise(t *testing.T) {
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}}
	bodyA := map[string]any{
		"messages":            []any{map[string]any{"role": "user", "content": "repeat this"}},
		"tools":               []any{},
		"tool_choice":         "auto",
		"parallel_tool_calls": true,
		"logprobs":            false,
		"top_logprobs":        0,
		"response_format":     map[string]any{"type": "text"},
		"modalities":          []any{"text"},
	}
	bodyB := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "repeat this"}},
	}
	keyA, _, err := llmPromptCacheKey(model, bodyA, "auto", defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("keyA error: %v", err)
	}
	keyB, _, err := llmPromptCacheKey(model, bodyB, "auto", defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("keyB error: %v", err)
	}
	if keyA != keyB {
		t.Fatalf("expected default OpenAI noise fields to share cache key: %q vs %q", keyA, keyB)
	}
}

func TestLLMPromptCacheKeyPreservesOutputChangingFields(t *testing.T) {
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}}
	base := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "repeat this"}}}
	jsonFormat := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "repeat this"}}, "response_format": map[string]any{"type": "json_object"}}
	keyA, _, err := llmPromptCacheKey(model, base, "auto", defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("base key error: %v", err)
	}
	keyB, _, err := llmPromptCacheKey(model, jsonFormat, "auto", defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("json key error: %v", err)
	}
	if keyA == keyB {
		t.Fatalf("expected response_format json_object to affect cache key: %q", keyA)
	}
}
func TestLLMPromptCacheKeyRespectsConfigurableIgnoreFlags(t *testing.T) {
	cfgA := defaultHubLLMPromptCacheConfig()
	cfgB := defaultHubLLMPromptCacheConfig()
	cfgB.IgnoreUserField = false
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}}
	bodyA := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "repeat this"}}, "user": "u1"}
	bodyB := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "repeat this"}}, "user": "u2"}
	keyA, _, err := llmPromptCacheKey(model, bodyA, "auto", cfgA)
	if err != nil {
		t.Fatalf("cfgA keyA error: %v", err)
	}
	keyB, _, err := llmPromptCacheKey(model, bodyB, "auto", cfgA)
	if err != nil {
		t.Fatalf("cfgA keyB error: %v", err)
	}
	if keyA != keyB {
		t.Fatalf("expected ignore-user config to collapse keys: %q vs %q", keyA, keyB)
	}
	keyC, _, err := llmPromptCacheKey(model, bodyA, "auto", cfgB)
	if err != nil {
		t.Fatalf("cfgB keyA error: %v", err)
	}
	keyD, _, err := llmPromptCacheKey(model, bodyB, "auto", cfgB)
	if err != nil {
		t.Fatalf("cfgB keyB error: %v", err)
	}
	if keyC == keyD {
		t.Fatalf("expected user field to affect key when ignore flag is disabled: %q", keyC)
	}
}

func TestPutCachedAuthorizedModelResponseSchedulesBackgroundMaintenance(t *testing.T) {
	globalLLMPromptCacheMaintenance.lastRun.Store(0)
	globalLLMPromptCacheMaintenance.running.Store(false)
	stub := &promptCacheMaintenanceStub{}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}}
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "repeat this"}}}
	resp := []byte(`{"id":"cached","model":"auto","choices":[{"index":0,"message":{"role":"assistant","content":"cached"},"finish_reason":"stop"}]}`)
	if err := putCachedAuthorizedModelResponse(context.Background(), stub, model, body, "auto", resp, http.StatusOK, "provider-a", []string{"group-a"}, corelib.TokenUsageStat{}, defaultHubLLMPromptCacheConfig()); err != nil {
		t.Fatalf("putCachedAuthorizedModelResponse() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stub.deleteExpiredCalls.Load() > 0 && stub.trimCalls.Load() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected background maintenance to run, deleteExpired=%d trim=%d", stub.deleteExpiredCalls.Load(), stub.trimCalls.Load())
}

func TestPromptCacheLookupIsTenantScoped(t *testing.T) {
	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1 << 20})
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}}
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "repeat this"}}}
	cfg := defaultHubLLMPromptCacheConfig()
	ctxA := withLLMPromptCacheTenant(context.Background(), "tenant_a")
	ctxB := withLLMPromptCacheTenant(context.Background(), "tenant_b")

	respA := []byte(`{"id":"cached-a","model":"auto","choices":[{"index":0,"message":{"role":"assistant","content":"tenant-a"},"finish_reason":"stop"}]}`)
	if err := putCachedAuthorizedModelResponse(ctxA, cache, model, body, "auto", respA, http.StatusOK, "provider-a", []string{"group-a"}, corelib.TokenUsageStat{}, cfg); err != nil {
		t.Fatalf("put tenant a cache: %v", err)
	}

	if resp, _, _, _, _, ok, err := getCachedAuthorizedModelResponse(ctxB, cache, model, body, "auto", cfg); err != nil || ok || len(resp) != 0 {
		t.Fatalf("tenant b unexpectedly hit tenant a cache, ok=%v err=%v resp=%s", ok, err, string(resp))
	}
	resp, _, _, _, _, ok, err := getCachedAuthorizedModelResponse(ctxA, cache, model, body, "auto", cfg)
	if err != nil {
		t.Fatalf("tenant a cache lookup: %v", err)
	}
	if !ok || !strings.Contains(string(resp), "tenant-a") {
		t.Fatalf("expected tenant a cache hit, ok=%v resp=%s", ok, string(resp))
	}
}

func TestForwardAuthorizedModelRequestWithCacheCoalescesConcurrentMisses(t *testing.T) {
	globalAuthorizedModelRequestFlights = authorizedModelRequestFlightGroup{}
	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		time.Sleep(100 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "shared"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12},
		})
	}))
	defer server.Close()

	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}}
	body := map[string]any{"model": "gpt-5-codex", "messages": []any{map[string]any{"role": "user", "content": "repeat this"}}, "stream": true, "stream_options": map[string]any{"include_usage": true}, "prompt_cache_key": "codex-cache-key", "service_tier": "auto", "store": false}
	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1 << 20})

	type result struct {
		statusCode int
		providerID string
		cacheHit   bool
		err        error
	}
	results := make(chan result, 2)
	call := func() {
		req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
		_, statusCode, providerID, _, _, cacheHit, err := forwardAuthorizedModelRequestWithCache(req, reg, model, body, "gpt-5-codex", cache, defaultHubLLMPromptCacheConfig())
		results <- result{statusCode: statusCode, providerID: providerID, cacheHit: cacheHit, err: err}
	}
	go call()
	go call()
	for i := 0; i < 2; i++ {
		res := <-results
		if res.err != nil {
			t.Fatalf("call %d error = %v", i+1, res.err)
		}
		if res.statusCode != http.StatusOK || res.providerID != "provider-a" {
			t.Fatalf("call %d unexpected result: %+v", i+1, res)
		}
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected one upstream call for concurrent misses, hits=%d", upstreamHits.Load())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	aliasBody := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "repeat this"}}}
	respBody, _, _, _, _, cacheHit, err := forwardAuthorizedModelRequestWithCache(req, reg, model, aliasBody, "auto", cache, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("follow-up call error = %v", err)
	}
	if !cacheHit {
		t.Fatal("expected follow-up call to be served from cache")
	}
	if !strings.Contains(string(respBody), `"model":"auto"`) {
		t.Fatalf("expected cached response model to be rewritten for current request, body=%s", string(respBody))
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected no extra upstream calls after cache warmup, hits=%d", upstreamHits.Load())
	}
}
