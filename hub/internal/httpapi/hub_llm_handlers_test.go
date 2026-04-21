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
	HubLLMStatusHandler(func() string { return "healthy" }, settings, hubPromptCacheRepoStub{stats: store.LLMPromptCacheStats{Entries: 2, TotalBytes: 4096, ExpiredEntries: 1, ExpiredBytes: 1024, TotalHits: 7}}).ServeHTTP(rr, req)
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
	HubLLMStatusHandler(func() string { return "healthy" }, settings, cache).ServeHTTP(rr, req)
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
	if !defaults.Enabled || defaults.TTLSeconds != 1800 || defaults.MemoryMaxEntries != 256 || defaults.DiskMaxBytes != 64<<20 {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}

	body := strings.NewReader(`{"enabled":false,"ttl_seconds":90,"memory_max_entries":12,"memory_max_bytes":2048,"disk_max_bytes":4096}`)
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
	if saved.Enabled || saved.TTLSeconds != 90 || saved.MemoryMaxEntries != 12 || saved.MemoryMaxBytes != 2048 || saved.DiskMaxBytes != 4096 {
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
	if len(body.Entries) != 1 || body.Entries[0].CacheKey != "c" || body.Entries[0].HitCount != 3 || body.Entries[0].Model != "gpt-4.1" || body.Page != 1 || body.Limit != 1 || body.Total != 3 || !body.HasMore || len(body.Providers) != 2 || len(body.Models) != 2 {
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
	if len(filteredBody.Entries) != 2 {
		t.Fatalf("filtered entries len = %d, want 2", len(filteredBody.Entries))
	}
	if len(filteredBody.Providers) != 2 || len(filteredBody.Models) != 1 || filteredBody.Models[0] != "auto" {
		t.Fatalf("unexpected filter metadata: %#v", filteredBody)
	}
	for _, entry := range filteredBody.Entries {
		if entry.ProviderID != "provider-a" || entry.Model != "auto" {
			t.Fatalf("unexpected filtered entry: %#v", filteredBody.Entries)
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
	if len(pagedBody.Entries) != 1 || pagedBody.Entries[0].CacheKey != "a" || pagedBody.Page != 2 || pagedBody.Limit != 1 || pagedBody.Total != 2 || pagedBody.HasMore || len(pagedBody.Providers) != 2 || len(pagedBody.Models) != 1 {
		t.Fatalf("unexpected paged entries: %#v %#v", pagedBody.Entries, pagedBody)
	}
}
