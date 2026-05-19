package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

type hubPromptCacheRepoStub struct{ stats store.LLMPromptCacheStats }

func (s hubPromptCacheRepoStub) Get(_ context.Context, _ string) (*store.LLMPromptCacheEntry, error) {
	return nil, nil
}
func (s hubPromptCacheRepoStub) Put(_ context.Context, _ *store.LLMPromptCacheEntry) error {
	return nil
}
func (s hubPromptCacheRepoStub) Delete(_ context.Context, _ string) error {
	return nil
}
func (s hubPromptCacheRepoStub) Purge(_ context.Context) (int64, error) {
	return 0, nil
}
func (s hubPromptCacheRepoStub) DeleteExpired(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (s hubPromptCacheRepoStub) TrimToBytes(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}
func (s hubPromptCacheRepoStub) ListRecent(_ context.Context, _ int) ([]*store.LLMPromptCacheEntry, error) {
	return nil, nil
}
func (s hubPromptCacheRepoStub) Stats(_ context.Context, _ time.Time) (*store.LLMPromptCacheStats, error) {
	copyStats := s.stats
	return &copyStats, nil
}

func TestHubLLMStatusIncludesPromptCacheRate(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	reg := &im.LLMProviderRegistry{
		TokenUsage: map[string]*corelib.TokenUsageStat{
			"provider-a": {
				InputTokens:       100,
				CachedInputTokens: 40,
				CacheWriteTokens:  10,
				Requests:          4,
				CachedRequests:    1,
			},
			"provider-b": {
				InputTokens:       300,
				CachedInputTokens: 160,
				CacheWriteTokens:  20,
				Requests:          6,
				CachedRequests:    3,
			},
		},
	}
	if err := im.SaveLLMProviderRegistry(nil, settings, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_status", nil)
	rr := httptest.NewRecorder()
	HubLLMStatusHandler(func(context.Context) string { return "healthy" }, settings, hubPromptCacheRepoStub{stats: store.LLMPromptCacheStats{Entries: 2, TotalBytes: 4096, ExpiredEntries: 1, ExpiredBytes: 1024, TotalHits: 7}}).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Status      string `json:"status"`
		PromptCache struct {
			InputTokens       int64   `json:"input_tokens"`
			CachedInputTokens int64   `json:"cached_input_tokens"`
			CacheWriteTokens  int64   `json:"cache_write_tokens"`
			Requests          int64   `json:"requests"`
			CachedRequests    int64   `json:"cached_requests"`
			CacheRate         float64 `json:"cache_rate"`
			CacheReuseRate    float64 `json:"cache_reuse_rate"`
			LocalStorage      struct {
				DiskEntries        int64 `json:"disk_entries"`
				DiskBytes          int64 `json:"disk_bytes"`
				DiskExpiredEntries int64 `json:"disk_expired_entries"`
				DiskExpiredBytes   int64 `json:"disk_expired_bytes"`
				DiskHits           int64 `json:"disk_hits"`
			} `json:"local_storage"`
		} `json:"prompt_cache"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "healthy" {
		t.Fatalf("status = %q", body.Status)
	}
	if body.PromptCache.CachedRequests != 4 || body.PromptCache.Requests != 10 {
		t.Fatalf("cache requests = %d/%d", body.PromptCache.CachedRequests, body.PromptCache.Requests)
	}
	if body.PromptCache.CacheRate != 40 {
		t.Fatalf("cache rate = %v, want 40", body.PromptCache.CacheRate)
	}
	if body.PromptCache.CachedInputTokens != 200 || body.PromptCache.InputTokens != 400 {
		t.Fatalf("cache tokens = %d/%d", body.PromptCache.CachedInputTokens, body.PromptCache.InputTokens)
	}
	if body.PromptCache.CacheReuseRate != 50 {
		t.Fatalf("cache reuse rate = %v, want 50", body.PromptCache.CacheReuseRate)
	}
	if body.PromptCache.CacheWriteTokens != 30 {
		t.Fatalf("cache write tokens = %d, want 30", body.PromptCache.CacheWriteTokens)
	}
	if body.PromptCache.LocalStorage.DiskEntries != 2 || body.PromptCache.LocalStorage.DiskBytes != 4096 {
		t.Fatalf("unexpected local storage stats: %#v", body.PromptCache.LocalStorage)
	}
	if body.PromptCache.LocalStorage.DiskExpiredEntries != 1 || body.PromptCache.LocalStorage.DiskHits != 7 {
		t.Fatalf("unexpected local storage detail: %#v", body.PromptCache.LocalStorage)
	}
}

func TestHubLLMStatusIncludesMemoryAndDiskCacheStatusFromRuntimeCache(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	if err := im.SaveLLMProviderRegistry(nil, settings, &im.LLMProviderRegistry{}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	if _, err := SaveHubLLMPromptCacheConfig(context.Background(), settings, HubLLMPromptCacheConfig{Enabled: true, TTLSeconds: 1800, MemoryMaxEntries: 4, MemoryMaxBytes: 1024, DiskMaxBytes: 4096}); err != nil {
		t.Fatalf("save cache config: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	cache := llmcache.New(hubPromptCacheRepoStub{stats: store.LLMPromptCacheStats{Entries: 3, TotalBytes: 300, ExpiredEntries: 1, ExpiredBytes: 100, TotalHits: 11}}, llmcache.Config{MemoryMaxEntries: 4, MemoryMaxBytes: 1024})
	entry := &store.LLMPromptCacheEntry{
		CacheKey:     "mem-key",
		ProviderID:   "provider-a",
		Model:        "auto",
		Kind:         "chat_completion_response",
		InputHash:    "hash",
		Payload:      []byte("pong"),
		PayloadBytes: 4,
		CreatedAt:    now,
		AccessedAt:   now,
	}
	if err := cache.Put(context.Background(), entry); err != nil {
		t.Fatalf("cache put: %v", err)
	}
	if _, err := cache.Get(context.Background(), entry.CacheKey); err != nil {
		t.Fatalf("cache get: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_status", nil)
	rr := httptest.NewRecorder()
	HubLLMStatusHandler(func(context.Context) string { return "healthy" }, settings, cache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		PromptCache struct {
			LocalStorage struct {
				MemoryEntries    int   `json:"memory_entries"`
				MemoryBytes      int64 `json:"memory_bytes"`
				MemoryMaxEntries int   `json:"memory_max_entries"`
				MemoryMaxBytes   int64 `json:"memory_max_bytes"`
				MemoryHits       int64 `json:"memory_hits"`
				DiskEntries      int64 `json:"disk_entries"`
				DiskBytes        int64 `json:"disk_bytes"`
				DiskExpired      int64 `json:"disk_expired_entries"`
				DiskExpiredBytes int64 `json:"disk_expired_bytes"`
				DiskHits         int64 `json:"disk_hits"`
			} `json:"local_storage"`
		} `json:"prompt_cache"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.PromptCache.LocalStorage.MemoryEntries != 1 || body.PromptCache.LocalStorage.MemoryBytes != 4 {
		t.Fatalf("unexpected memory status: %#v", body.PromptCache.LocalStorage)
	}
	if body.PromptCache.LocalStorage.MemoryMaxEntries != 4 || body.PromptCache.LocalStorage.MemoryMaxBytes != 1024 {
		t.Fatalf("unexpected memory limits: %#v", body.PromptCache.LocalStorage)
	}
	if body.PromptCache.LocalStorage.MemoryHits != 1 {
		t.Fatalf("unexpected memory hits: %#v", body.PromptCache.LocalStorage)
	}
	if body.PromptCache.LocalStorage.DiskEntries != 3 || body.PromptCache.LocalStorage.DiskBytes != 300 {
		t.Fatalf("unexpected disk status: %#v", body.PromptCache.LocalStorage)
	}
	if body.PromptCache.LocalStorage.DiskExpired != 1 || body.PromptCache.LocalStorage.DiskExpiredBytes != 100 || body.PromptCache.LocalStorage.DiskHits != 11 {
		t.Fatalf("unexpected disk detail: %#v", body.PromptCache.LocalStorage)
	}
}

func TestHubLLMPromptCacheStatusIncludesRuntimeMetrics(t *testing.T) {
	resetLLMPromptCacheMetricsForTest()
	settings := &testSystemSettingsRepo{}
	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1024})
	globalLLMPromptCacheMetrics.cacheableRequests.Store(7)
	globalLLMPromptCacheMetrics.bypassStreaming.Store(3)
	globalLLMPromptCacheMetrics.singleflightSharedHits.Store(2)
	globalLLMPromptCacheMetrics.singleflightSavedCalls.Store(2)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_status", nil)
	rr := httptest.NewRecorder()
	HubLLMStatusHandler(func(context.Context) string { return "healthy" }, settings, cache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		PromptCache struct {
			Runtime struct {
				CacheableRequests      int64            `json:"cacheable_requests"`
				BypassStreaming        int64            `json:"bypass_streaming"`
				SingleflightSharedHits int64            `json:"singleflight_shared_hits"`
				SingleflightSavedCalls int64            `json:"singleflight_saved_calls"`
				BypassReasons          map[string]int64 `json:"bypass_reasons"`
			} `json:"runtime"`
		} `json:"prompt_cache"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.PromptCache.Runtime.CacheableRequests != 7 {
		t.Fatalf("cacheable_requests = %d, want 7", body.PromptCache.Runtime.CacheableRequests)
	}
	if body.PromptCache.Runtime.BypassStreaming != 3 {
		t.Fatalf("bypass_streaming = %d, want 3", body.PromptCache.Runtime.BypassStreaming)
	}
	if body.PromptCache.Runtime.SingleflightSharedHits != 2 || body.PromptCache.Runtime.SingleflightSavedCalls != 2 {
		t.Fatalf("unexpected singleflight metrics: %+v", body.PromptCache.Runtime)
	}
	if body.PromptCache.Runtime.BypassReasons["streaming"] != 3 {
		t.Fatalf("bypass_reasons = %#v", body.PromptCache.Runtime.BypassReasons)
	}
}

func TestHubLLMPromptCacheStatusUsesTenantRuntimeMetrics(t *testing.T) {
	resetLLMPromptCacheMetricsForTest()
	settings := &testSystemSettingsRepo{}
	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1024})
	cfg := defaultHubLLMPromptCacheConfig()
	_ = llmPromptCacheableForTenant(withLLMPromptCacheTenant(context.Background(), "tenant_a"), map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}}, cfg)
	_ = llmPromptCacheableForTenant(withLLMPromptCacheTenant(context.Background(), "tenant_b"), map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}, "temperature": 0.8}, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_status", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	rr := httptest.NewRecorder()
	HubLLMStatusHandler(func(context.Context) string { return "healthy" }, settings, cache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		PromptCache struct {
			Runtime struct {
				CacheableRequests int64            `json:"cacheable_requests"`
				BypassTemperature int64            `json:"bypass_temperature"`
				BypassReasons     map[string]int64 `json:"bypass_reasons"`
			} `json:"runtime"`
		} `json:"prompt_cache"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.PromptCache.Runtime.CacheableRequests != 1 || body.PromptCache.Runtime.BypassTemperature != 0 || len(body.PromptCache.Runtime.BypassReasons) != 0 {
		t.Fatalf("unexpected tenant runtime metrics: %#v", body.PromptCache.Runtime)
	}
}

func TestHubLLMPromptCacheConfigHandlersRoundTrip(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1024})

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_config", nil)
	getRR := httptest.NewRecorder()
	GetHubLLMPromptCacheConfigHandler(settings, cache).ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("default config status = %d body=%s", getRR.Code, getRR.Body.String())
	}
	var defaults HubLLMPromptCacheConfig
	if err := json.Unmarshal(getRR.Body.Bytes(), &defaults); err != nil {
		t.Fatalf("decode defaults: %v", err)
	}
	if !defaults.Enabled || defaults.TTLSeconds != 1800 || defaults.MemoryMaxEntries != 256 || defaults.DiskMaxBytes != 64<<20 || !defaults.NormalizeDeterministicParams || !defaults.IgnoreModelField || !defaults.IgnoreUserField || !defaults.IgnoreMetadataField || defaults.SingleflightWaitTimeoutMS != 15000 {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}

	body := strings.NewReader(`{"enabled":false,"ttl_seconds":90,"memory_max_entries":12,"memory_max_bytes":2048,"disk_max_bytes":4096,"normalize_deterministic_params":false,"ignore_model_field":false,"ignore_user_field":false,"ignore_metadata_field":false,"singleflight_wait_timeout_ms":2200}`)
	putReq := httptest.NewRequest(http.MethodPut, "/api/admin/hub_llm_prompt_cache_config", body)
	putRR := httptest.NewRecorder()
	UpdateHubLLMPromptCacheConfigHandler(settings, cache).ServeHTTP(putRR, putReq)
	if putRR.Code != http.StatusOK {
		t.Fatalf("update config status = %d body=%s", putRR.Code, putRR.Body.String())
	}
	var saved HubLLMPromptCacheConfig
	if err := json.Unmarshal(putRR.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved: %v", err)
	}
	if saved.Enabled || saved.TTLSeconds != 90 || saved.MemoryMaxEntries != 12 || saved.MemoryMaxBytes != 2048 || saved.DiskMaxBytes != 4096 || saved.NormalizeDeterministicParams || saved.IgnoreModelField || saved.IgnoreUserField || saved.IgnoreMetadataField || saved.SingleflightWaitTimeoutMS != 2200 {
		t.Fatalf("unexpected saved config: %#v", saved)
	}

	status, err := cache.Status(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("cache status: %v", err)
	}
	if status.MemoryMaxEntries != 12 || status.MemoryMaxBytes != 2048 {
		t.Fatalf("runtime cache config not applied: %#v", status)
	}
}

func TestHubLLMPromptCacheConfigHandlersScopeTenantAdmin(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1024})
	globalCfg := HubLLMPromptCacheConfig{Enabled: true, TTLSeconds: 1800, MemoryMaxEntries: 8, MemoryMaxBytes: 1024, DiskMaxBytes: 4096}
	if _, err := SaveHubLLMPromptCacheConfig(context.Background(), settings, globalCfg); err != nil {
		t.Fatalf("save global config: %v", err)
	}
	tenantReq := httptest.NewRequest(http.MethodPut, "/api/admin/hub_llm_prompt_cache_config", strings.NewReader(`{"enabled":false,"ttl_seconds":60,"memory_max_entries":3,"memory_max_bytes":256,"disk_max_bytes":512}`))
	tenantReq = tenantReq.WithContext(context.WithValue(tenantReq.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	tenantRR := httptest.NewRecorder()
	UpdateHubLLMPromptCacheConfigHandler(settings, cache).ServeHTTP(tenantRR, tenantReq)
	if tenantRR.Code != http.StatusOK {
		t.Fatalf("tenant update status = %d body=%s", tenantRR.Code, tenantRR.Body.String())
	}
	status, err := cache.Status(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("cache status: %v", err)
	}
	if status.MemoryMaxEntries != 8 || status.MemoryMaxBytes != 1024 {
		t.Fatalf("tenant config should not rewrite shared runtime cache limits: %#v", status)
	}

	globalReq := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_config", nil)
	globalRR := httptest.NewRecorder()
	GetHubLLMPromptCacheConfigHandler(settings).ServeHTTP(globalRR, globalReq)
	if globalRR.Code != http.StatusOK {
		t.Fatalf("global get status = %d body=%s", globalRR.Code, globalRR.Body.String())
	}
	var gotGlobal HubLLMPromptCacheConfig
	if err := json.Unmarshal(globalRR.Body.Bytes(), &gotGlobal); err != nil {
		t.Fatalf("decode global: %v", err)
	}
	if gotGlobal.TTLSeconds != 1800 || gotGlobal.MemoryMaxEntries != 8 || !gotGlobal.Enabled {
		t.Fatalf("global config leaked tenant update: %#v", gotGlobal)
	}

	tenantGet := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_config", nil)
	tenantGet = tenantGet.WithContext(context.WithValue(tenantGet.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	tenantGetRR := httptest.NewRecorder()
	GetHubLLMPromptCacheConfigHandler(settings, cache).ServeHTTP(tenantGetRR, tenantGet)
	if tenantGetRR.Code != http.StatusOK {
		t.Fatalf("tenant get status = %d body=%s", tenantGetRR.Code, tenantGetRR.Body.String())
	}
	var gotTenant HubLLMPromptCacheConfig
	if err := json.Unmarshal(tenantGetRR.Body.Bytes(), &gotTenant); err != nil {
		t.Fatalf("decode tenant: %v", err)
	}
	if gotTenant.Enabled || gotTenant.TTLSeconds != 60 || gotTenant.MemoryMaxEntries != 3 {
		t.Fatalf("unexpected tenant config: %#v", gotTenant)
	}
}

func TestHubLLMConfigHandlersScopeTenantAdmin(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	globalCfg := im.HubLLMConfig{Enabled: true, APIURL: "https://global.example/v1", APIKey: "global-key", Model: "global-model"}
	data, _ := json.Marshal(globalCfg)
	if err := settings.Set(context.Background(), hubLLMConfigKey, string(data)); err != nil {
		t.Fatalf("save global llm config: %v", err)
	}
	tenantReq := httptest.NewRequest(http.MethodPut, "/api/admin/hub_llm_config", strings.NewReader(`{"enabled":true,"api_url":"https://tenant-a.example/v1","api_key":"tenant-key","model":"tenant-model"}`))
	tenantReq = tenantReq.WithContext(context.WithValue(tenantReq.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	tenantRR := httptest.NewRecorder()
	UpdateHubLLMConfigHandler(settings).ServeHTTP(tenantRR, tenantReq)
	if tenantRR.Code != http.StatusOK {
		t.Fatalf("tenant update status = %d body=%s", tenantRR.Code, tenantRR.Body.String())
	}

	globalReq := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_config", nil)
	globalRR := httptest.NewRecorder()
	GetHubLLMConfigHandler(settings).ServeHTTP(globalRR, globalReq)
	if globalRR.Code != http.StatusOK {
		t.Fatalf("global get status = %d body=%s", globalRR.Code, globalRR.Body.String())
	}
	var gotGlobal map[string]any
	if err := json.Unmarshal(globalRR.Body.Bytes(), &gotGlobal); err != nil {
		t.Fatalf("decode global: %v", err)
	}
	if gotGlobal["api_url"] != "https://global.example/v1" || gotGlobal["model"] != "global-model" {
		t.Fatalf("global llm config leaked tenant update: %#v", gotGlobal)
	}

	tenantGet := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_config", nil)
	tenantGet = tenantGet.WithContext(context.WithValue(tenantGet.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	tenantGetRR := httptest.NewRecorder()
	GetHubLLMConfigHandler(settings).ServeHTTP(tenantGetRR, tenantGet)
	if tenantGetRR.Code != http.StatusOK {
		t.Fatalf("tenant get status = %d body=%s", tenantGetRR.Code, tenantGetRR.Body.String())
	}
	var gotTenant map[string]any
	if err := json.Unmarshal(tenantGetRR.Body.Bytes(), &gotTenant); err != nil {
		t.Fatalf("decode tenant: %v", err)
	}
	if gotTenant["api_url"] != "https://tenant-a.example/v1" || gotTenant["model"] != "tenant-model" || gotTenant["has_api_key"] != true {
		t.Fatalf("unexpected tenant llm config: %#v", gotTenant)
	}
}

func TestClearHubLLMPromptCacheHandlerPurgesRuntimeCache(t *testing.T) {
	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1024})
	now := time.Now().UTC()
	if err := cache.Put(context.Background(), &store.LLMPromptCacheEntry{CacheKey: "hit", Payload: []byte("pong"), PayloadBytes: 4, CreatedAt: now, AccessedAt: now}); err != nil {
		t.Fatalf("put cache: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/hub_llm_prompt_cache_clear", nil)
	rr := httptest.NewRecorder()
	ClearHubLLMPromptCacheHandler(cache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Purged int64 `json:"purged"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Purged != 1 {
		t.Fatalf("purged = %d, want memory purge count when no disk repo is attached", body.Purged)
	}
	status, err := cache.Status(context.Background(), now)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryEntries != 0 || status.MemoryBytes != 0 {
		t.Fatalf("cache not purged: %#v", status)
	}
}

func TestClearHubLLMPromptCacheHandlerTenantAdminPurgesOwnEntries(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "cache-clear-tenant.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	cache := llmcache.New(st.LLMPromptCache, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 4096})
	now := time.Now().UTC().Truncate(time.Second)
	for _, key := range []string{"tenant:tenant_a:hit-a", "tenant:tenant_b:hit-b", "legacy-default"} {
		if err := cache.Put(context.Background(), &store.LLMPromptCacheEntry{CacheKey: key, Payload: []byte("pong"), PayloadBytes: 4, CreatedAt: now, AccessedAt: now}); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/hub_llm_prompt_cache_clear", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	rr := httptest.NewRecorder()
	ClearHubLLMPromptCacheHandler(cache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if got, err := st.LLMPromptCache.Get(context.Background(), "tenant:tenant_a:hit-a"); err != nil || got != nil {
		t.Fatalf("tenant_a entry should be purged, got=%#v err=%v", got, err)
	}
	for _, key := range []string{"tenant:tenant_b:hit-b", "legacy-default"} {
		got, err := st.LLMPromptCache.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		if got == nil {
			t.Fatalf("%s should remain", key)
		}
	}
}

func TestGetHubLLMPromptCacheEntryHandlerReturnsStoredDetails(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "cache-entry-detail.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	cache := llmcache.New(st.LLMPromptCache, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 4096})
	cfg := defaultHubLLMPromptCacheConfig()
	body := map[string]any{
		"model": "gpt-4.1",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"temperature": 0,
		"user":        "alice@example.com",
		"metadata":    map[string]any{"trace_id": "abc"},
	}
	model := &llmservice.AuthorizedModel{Name: "writer-auto", ProviderIDs: []string{"provider-a", "provider-b"}}
	usage := corelib.TokenUsageStat{CachedInputTokens: 12, CacheWriteTokens: 6}
	if err := putCachedAuthorizedModelResponse(context.Background(), cache, model, body, "gpt-4.1", []byte(`{"id":"resp_1"}`), http.StatusOK, "provider-a", []string{"sg-1", "sg-1"}, usage, cfg); err != nil {
		t.Fatalf("put cached response: %v", err)
	}
	cacheKey, _, err := llmPromptCacheKey(model, body, "gpt-4.1", cfg)
	if err != nil {
		t.Fatalf("cache key: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_entry?cache_key="+cacheKey, nil)
	rr := httptest.NewRecorder()
	GetHubLLMPromptCacheEntryHandler(cache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		CacheKey          string         `json:"cache_key"`
		AuthorizedModel   string         `json:"authorized_model"`
		RequestedModel    string         `json:"requested_model"`
		OrderedProviders  []string       `json:"ordered_providers"`
		NormalizedRequest map[string]any `json:"normalized_request"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.CacheKey != cacheKey {
		t.Fatalf("cache_key = %q, want %q", resp.CacheKey, cacheKey)
	}
	if resp.AuthorizedModel != "writer-auto" || resp.RequestedModel != "gpt-4.1" {
		t.Fatalf("unexpected model fields: %#v", resp)
	}
	if len(resp.OrderedProviders) != 2 || resp.OrderedProviders[0] != "provider-a" || resp.OrderedProviders[1] != "provider-b" {
		t.Fatalf("unexpected ordered providers: %#v", resp.OrderedProviders)
	}
	if _, ok := resp.NormalizedRequest["model"]; ok {
		t.Fatalf("normalized request should omit model: %#v", resp.NormalizedRequest)
	}
	if _, ok := resp.NormalizedRequest["user"]; ok {
		t.Fatalf("normalized request should omit user: %#v", resp.NormalizedRequest)
	}
	if _, ok := resp.NormalizedRequest["metadata"]; ok {
		t.Fatalf("normalized request should omit metadata: %#v", resp.NormalizedRequest)
	}
	messages, ok := resp.NormalizedRequest["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("normalized request messages = %#v", resp.NormalizedRequest["messages"])
	}
}

func TestHubLLMPromptCacheEntryHandlersScopeTenantAdmin(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "cache-entry-tenant.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	now := time.Now().UTC().Truncate(time.Second)
	for _, key := range []string{"tenant:tenant_a:key-a", "tenant:tenant_b:key-b", "legacy-default"} {
		if err := st.LLMPromptCache.Put(context.Background(), &store.LLMPromptCacheEntry{CacheKey: key, ProviderID: "provider-a", Model: "auto", Kind: "chat_completion_response", InputHash: key, Payload: []byte("{}"), PayloadBytes: 2, CreatedAt: now, AccessedAt: now}); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	adminCtx := context.WithValue(context.Background(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"})

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_entry?cache_key=key-a", nil).WithContext(adminCtx)
	getRR := httptest.NewRecorder()
	GetHubLLMPromptCacheEntryHandler(st.LLMPromptCache).ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get own status = %d body=%s", getRR.Code, getRR.Body.String())
	}
	var getBody struct {
		CacheKey string `json:"cache_key"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if getBody.CacheKey != "tenant:tenant_a:key-a" {
		t.Fatalf("cache_key = %q", getBody.CacheKey)
	}

	otherReq := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_entry?cache_key=tenant:tenant_b:key-b", nil).WithContext(adminCtx)
	otherRR := httptest.NewRecorder()
	GetHubLLMPromptCacheEntryHandler(st.LLMPromptCache).ServeHTTP(otherRR, otherReq)
	if otherRR.Code != http.StatusNotFound {
		t.Fatalf("get other status = %d body=%s", otherRR.Code, otherRR.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/hub_llm_prompt_cache_entry?cache_key=key-a", nil).WithContext(adminCtx)
	deleteRR := httptest.NewRecorder()
	DeleteHubLLMPromptCacheEntryHandler(st.LLMPromptCache).ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("delete own status = %d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
	if got, err := st.LLMPromptCache.Get(context.Background(), "tenant:tenant_a:key-a"); err != nil || got != nil {
		t.Fatalf("tenant_a entry should be deleted, got=%#v err=%v", got, err)
	}
	for _, key := range []string{"tenant:tenant_b:key-b", "legacy-default"} {
		got, err := st.LLMPromptCache.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		if got == nil {
			t.Fatalf("%s should remain", key)
		}
	}
}

func TestDeleteHubLLMPromptCacheEntryHandlerDeletesEntry(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "cache-delete.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	now := time.Now().UTC().Truncate(time.Second)
	entry := &store.LLMPromptCacheEntry{CacheKey: "delete-me", ProviderID: "provider-a", Model: "auto", Kind: "chat_completion_response", InputHash: "delete-me", Payload: []byte("{}"), PayloadBytes: 2, HitCount: 1, CreatedAt: now, AccessedAt: now}
	if err := st.LLMPromptCache.Put(context.Background(), entry); err != nil {
		t.Fatalf("put entry: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/hub_llm_prompt_cache_entry?cache_key=delete-me", nil)
	rr := httptest.NewRecorder()
	DeleteHubLLMPromptCacheEntryHandler(st.LLMPromptCache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rr.Code, rr.Body.String())
	}
	got, err := st.LLMPromptCache.Get(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if got != nil {
		t.Fatalf("expected deleted entry, got %#v", got)
	}
}

func TestGetHubLLMPromptCacheEntriesHandlerFiltersTenantAdmin(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "cache-entries-tenant.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	now := time.Now().UTC().Truncate(time.Second)
	entries := []*store.LLMPromptCacheEntry{
		{CacheKey: "tenant:tenant_a:a", ProviderID: "provider-a", Model: "auto", Kind: "chat_completion_response", InputHash: "a", Payload: []byte("{}"), PayloadBytes: 2, CreatedAt: now, AccessedAt: now},
		{CacheKey: "tenant:tenant_b:b", ProviderID: "provider-b", Model: "gpt-4.1", Kind: "chat_completion_response", InputHash: "b", Payload: []byte("{}"), PayloadBytes: 2, CreatedAt: now.Add(time.Minute), AccessedAt: now.Add(time.Minute)},
		{CacheKey: "legacy-default", ProviderID: "provider-default", Model: "auto", Kind: "chat_completion_response", InputHash: "legacy", Payload: []byte("{}"), PayloadBytes: 2, CreatedAt: now.Add(2 * time.Minute), AccessedAt: now.Add(2 * time.Minute)},
	}
	for _, entry := range entries {
		if err := st.LLMPromptCache.Put(context.Background(), entry); err != nil {
			t.Fatalf("put %s: %v", entry.CacheKey, err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_entries?limit=10", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	rr := httptest.NewRecorder()
	GetHubLLMPromptCacheEntriesHandler(st.LLMPromptCache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Entries []struct {
			CacheKey string `json:"cache_key"`
		} `json:"entries"`
		Providers []string `json:"providers"`
		Models    []string `json:"models"`
		Total     int      `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 1 || len(body.Entries) != 1 || body.Entries[0].CacheKey != "tenant:tenant_a:a" {
		t.Fatalf("unexpected entries: %#v", body)
	}
	if len(body.Providers) != 1 || body.Providers[0] != "provider-a" || len(body.Models) != 1 || body.Models[0] != "auto" {
		t.Fatalf("unexpected metadata: %#v", body)
	}
}

func TestGetHubLLMPromptCacheEntriesHandlerReturnsRecentEntries(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "cache-entries.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	now := time.Now().UTC().Truncate(time.Second)
	entries := []*store.LLMPromptCacheEntry{
		{CacheKey: "a", ProviderID: "provider-a", Model: "auto", Kind: "chat_completion_response", InputHash: "a", Payload: []byte("{}"), PayloadBytes: 2, HitCount: 1, CreatedAt: now, AccessedAt: now},
		{CacheKey: "b", ProviderID: "provider-a", Model: "auto", Kind: "chat_completion_response", InputHash: "b", Payload: []byte("{}"), PayloadBytes: 2, HitCount: 2, CreatedAt: now.Add(1 * time.Minute), AccessedAt: now.Add(1 * time.Minute)},
		{CacheKey: "legacy", ProviderID: "", Model: "auto", Kind: "chat_completion_response", InputHash: "legacy", Payload: []byte("{}"), PayloadBytes: 2, HitCount: 4, CreatedAt: now.Add(90 * time.Second), AccessedAt: now.Add(90 * time.Second)},
		{CacheKey: "c", ProviderID: "provider-b", Model: "gpt-4.1", Kind: "chat_completion_response", InputHash: "c", Payload: []byte("{}"), PayloadBytes: 2, HitCount: 3, CreatedAt: now.Add(2 * time.Minute), AccessedAt: now.Add(2 * time.Minute)},
	}
	for _, entry := range entries {
		if err := st.LLMPromptCache.Put(context.Background(), entry); err != nil {
			t.Fatalf("put %s: %v", entry.CacheKey, err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_entries?limit=1", nil)
	rr := httptest.NewRecorder()
	GetHubLLMPromptCacheEntriesHandler(st.LLMPromptCache).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Entries []struct {
			CacheKey string `json:"cache_key"`
			HitCount int64  `json:"hit_count"`
			Model    string `json:"model"`
		} `json:"entries"`
		Providers []string `json:"providers"`
		Models    []string `json:"models"`
		Page      int      `json:"page"`
		Limit     int      `json:"limit"`
		Total     int      `json:"total"`
		HasMore   bool     `json:"has_more"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) != 1 || body.Entries[0].CacheKey != "c" || body.Entries[0].HitCount != 3 || body.Entries[0].Model != "gpt-4.1" || body.Page != 1 || body.Limit != 1 || body.Total != 4 || !body.HasMore || len(body.Providers) != 2 || len(body.Models) != 2 {
		t.Fatalf("unexpected entries: %#v %#v", body.Entries, body)
	}

	filteredReq := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_entries?limit=10&provider=provider-a&model=auto", nil)
	filteredRR := httptest.NewRecorder()
	GetHubLLMPromptCacheEntriesHandler(st.LLMPromptCache).ServeHTTP(filteredRR, filteredReq)
	if filteredRR.Code != http.StatusOK {
		t.Fatalf("filtered status = %d body=%s", filteredRR.Code, filteredRR.Body.String())
	}
	var filteredBody struct {
		Entries []struct {
			CacheKey   string `json:"cache_key"`
			ProviderID string `json:"provider_id"`
			Model      string `json:"model"`
		} `json:"entries"`
		Providers []string `json:"providers"`
		Models    []string `json:"models"`
	}
	if err := json.Unmarshal(filteredRR.Body.Bytes(), &filteredBody); err != nil {
		t.Fatalf("decode filtered: %v", err)
	}
	if len(filteredBody.Entries) != 3 {
		t.Fatalf("filtered entries len = %d, want 3", len(filteredBody.Entries))
	}
	if len(filteredBody.Providers) != 2 || len(filteredBody.Models) != 1 || filteredBody.Models[0] != "auto" {
		t.Fatalf("unexpected filter metadata: %#v", filteredBody)
	}
	for _, entry := range filteredBody.Entries {
		if entry.ProviderID != "" && entry.ProviderID != "provider-a" {
			t.Fatalf("unexpected filtered entry provider: %#v", filteredBody.Entries)
		}
		if entry.Model != "auto" {
			t.Fatalf("unexpected filtered entry model: %#v", filteredBody.Entries)
		}
	}

	pagedReq := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_prompt_cache_entries?limit=1&page=2&provider=provider-a&model=auto", nil)
	pagedRR := httptest.NewRecorder()
	GetHubLLMPromptCacheEntriesHandler(st.LLMPromptCache).ServeHTTP(pagedRR, pagedReq)
	if pagedRR.Code != http.StatusOK {
		t.Fatalf("paged status = %d body=%s", pagedRR.Code, pagedRR.Body.String())
	}
	var pagedBody struct {
		Entries []struct {
			CacheKey string `json:"cache_key"`
		} `json:"entries"`
		Providers []string `json:"providers"`
		Models    []string `json:"models"`
		Page      int      `json:"page"`
		Limit     int      `json:"limit"`
		Total     int      `json:"total"`
		HasMore   bool     `json:"has_more"`
	}
	if err := json.Unmarshal(pagedRR.Body.Bytes(), &pagedBody); err != nil {
		t.Fatalf("decode paged: %v", err)
	}
	if len(pagedBody.Entries) != 1 || pagedBody.Entries[0].CacheKey != "b" || pagedBody.Page != 2 || pagedBody.Limit != 1 || pagedBody.Total != 3 || !pagedBody.HasMore || len(pagedBody.Providers) != 2 || len(pagedBody.Models) != 1 {
		t.Fatalf("unexpected paged entries: %#v %#v", pagedBody.Entries, pagedBody)
	}
}
