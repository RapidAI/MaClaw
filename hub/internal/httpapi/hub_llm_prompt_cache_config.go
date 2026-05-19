package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const hubLLMPromptCacheConfigKey = "hub_llm_prompt_cache_config"

type HubLLMPromptCacheConfig struct {
	Enabled                      bool  `json:"enabled"`
	TTLSeconds                   int   `json:"ttl_seconds"`
	MemoryMaxEntries             int   `json:"memory_max_entries"`
	MemoryMaxBytes               int64 `json:"memory_max_bytes"`
	DiskMaxBytes                 int64 `json:"disk_max_bytes"`
	NormalizeDeterministicParams bool  `json:"normalize_deterministic_params"`
	IgnoreModelField             bool  `json:"ignore_model_field"`
	IgnoreUserField              bool  `json:"ignore_user_field"`
	IgnoreMetadataField          bool  `json:"ignore_metadata_field"`
	SingleflightWaitTimeoutMS    int   `json:"singleflight_wait_timeout_ms"`
}
type hubLLMPromptCacheEntriesResponse struct {
	Entries   []hubLLMPromptCacheEntryView `json:"entries"`
	Providers []string                     `json:"providers"`
	Models    []string                     `json:"models"`
	Page      int                          `json:"page"`
	Limit     int                          `json:"limit"`
	Total     int                          `json:"total"`
	HasMore   bool                         `json:"has_more"`
}

type hubLLMPromptCacheEntryView struct {
	CacheKey          string `json:"cache_key"`
	ProviderID        string `json:"provider_id"`
	Model             string `json:"model"`
	Kind              string `json:"kind"`
	PayloadBytes      int64  `json:"payload_bytes"`
	CachedInputTokens int64  `json:"cached_input_tokens"`
	CacheWriteTokens  int64  `json:"cache_write_tokens"`
	HitCount          int64  `json:"hit_count"`
	CreatedAt         string `json:"created_at,omitempty"`
	AccessedAt        string `json:"accessed_at,omitempty"`
	ExpiresAt         string `json:"expires_at,omitempty"`
}

type hubLLMPromptCacheEntryDetailResponse struct {
	CacheKey          string          `json:"cache_key"`
	ProviderID        string          `json:"provider_id"`
	Model             string          `json:"model"`
	Kind              string          `json:"kind"`
	PayloadBytes      int64           `json:"payload_bytes"`
	CachedInputTokens int64           `json:"cached_input_tokens"`
	CacheWriteTokens  int64           `json:"cache_write_tokens"`
	HitCount          int64           `json:"hit_count"`
	CreatedAt         string          `json:"created_at,omitempty"`
	AccessedAt        string          `json:"accessed_at,omitempty"`
	ExpiresAt         string          `json:"expires_at,omitempty"`
	AuthorizedModel   string          `json:"authorized_model,omitempty"`
	RequestedModel    string          `json:"requested_model,omitempty"`
	OrderedProviders  []string        `json:"ordered_providers,omitempty"`
	NormalizedRequest json.RawMessage `json:"normalized_request,omitempty"`
}

func defaultHubLLMPromptCacheConfig() HubLLMPromptCacheConfig {
	return HubLLMPromptCacheConfig{
		Enabled:                      true,
		TTLSeconds:                   1800,
		MemoryMaxEntries:             256,
		MemoryMaxBytes:               8 << 20,
		DiskMaxBytes:                 64 << 20,
		NormalizeDeterministicParams: true,
		IgnoreModelField:             true,
		IgnoreUserField:              true,
		IgnoreMetadataField:          true,
		SingleflightWaitTimeoutMS:    15000,
	}
}

func normalizeHubLLMPromptCacheConfig(cfg HubLLMPromptCacheConfig) HubLLMPromptCacheConfig {
	defaults := defaultHubLLMPromptCacheConfig()
	if cfg.TTLSeconds <= 0 {
		cfg.TTLSeconds = defaults.TTLSeconds
	}
	if cfg.MemoryMaxEntries <= 0 {
		cfg.MemoryMaxEntries = defaults.MemoryMaxEntries
	}
	if cfg.MemoryMaxBytes <= 0 {
		cfg.MemoryMaxBytes = defaults.MemoryMaxBytes
	}
	if cfg.DiskMaxBytes <= 0 {
		cfg.DiskMaxBytes = defaults.DiskMaxBytes
	}
	if cfg.SingleflightWaitTimeoutMS <= 0 {
		cfg.SingleflightWaitTimeoutMS = defaults.SingleflightWaitTimeoutMS
	}
	return cfg
}

func LoadHubLLMPromptCacheConfig(ctx context.Context, system store.SystemSettingsRepository) HubLLMPromptCacheConfig {
	cfg := defaultHubLLMPromptCacheConfig()
	if system == nil {
		return cfg
	}
	raw, err := system.Get(ctx, hubLLMPromptCacheConfigKey)
	if err != nil || raw == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultHubLLMPromptCacheConfig()
	}
	return normalizeHubLLMPromptCacheConfig(cfg)
}

func SaveHubLLMPromptCacheConfig(ctx context.Context, system store.SystemSettingsRepository, cfg HubLLMPromptCacheConfig) (HubLLMPromptCacheConfig, error) {
	cfg = normalizeHubLLMPromptCacheConfig(cfg)
	data, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	if system == nil {
		return cfg, nil
	}
	if err := system.Set(ctx, hubLLMPromptCacheConfigKey, string(data)); err != nil {
		return cfg, err
	}
	invalidateLLMRuntimeCaches(system)
	return cfg, nil
}

func applyHubLLMPromptCacheRuntimeConfig(cacheSource any, cfg HubLLMPromptCacheConfig) {
	if cache, ok := cacheSource.(*llmcache.Cache); ok && cache != nil {
		cache.UpdateConfig(llmcache.Config{MemoryMaxEntries: cfg.MemoryMaxEntries, MemoryMaxBytes: cfg.MemoryMaxBytes})
	}
}

func GetHubLLMPromptCacheConfigHandler(system store.SystemSettingsRepository, promptCacheSources ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		cfg := loadCachedHubLLMPromptCacheConfig(r.Context(), system)
		if shouldReloadSharedRuntimeForRequest(r) {
			applyHubLLMPromptCacheRuntimeConfig(firstPromptCacheSource(promptCacheSources), cfg)
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateHubLLMPromptCacheConfigHandler(system store.SystemSettingsRepository, promptCacheSources ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		var cfg HubLLMPromptCacheConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		saved, err := SaveHubLLMPromptCacheConfig(r.Context(), system, cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HUB_LLM_PROMPT_CACHE_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		if shouldReloadSharedRuntimeForRequest(r) {
			applyHubLLMPromptCacheRuntimeConfig(firstPromptCacheSource(promptCacheSources), saved)
		}
		writeJSON(w, http.StatusOK, saved)
	}
}

func ClearHubLLMPromptCacheHandler(promptCacheSources ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cache := firstPromptCacheSource(promptCacheSources)
		if cache == nil {
			writeJSON(w, http.StatusOK, map[string]any{"purged": 0})
			return
		}
		if promptCacheTenantScoped(r) {
			purger, ok := cache.(interface {
				PurgeTenant(context.Context, string) (int64, error)
			})
			if !ok {
				writeJSON(w, http.StatusOK, map[string]any{"purged": 0})
				return
			}
			purged, err := purger.PurgeTenant(r.Context(), promptCacheTenantScope(r))
			if err != nil {
				writeError(w, http.StatusInternalServerError, "HUB_LLM_PROMPT_CACHE_CLEAR_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"purged": purged})
			return
		}
		purger, ok := cache.(interface {
			Purge(context.Context) (int64, error)
		})
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"purged": 0})
			return
		}
		purged, err := purger.Purge(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HUB_LLM_PROMPT_CACHE_CLEAR_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"purged": purged})
	}
}

func GetHubLLMPromptCacheEntryHandler(promptCacheSource any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cacheKey := strings.TrimSpace(r.URL.Query().Get("cache_key"))
		if cacheKey == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CACHE_KEY", "cache_key is required")
			return
		}
		repo, ok := promptCacheStatusRepository(promptCacheSource)
		if !ok || repo == nil {
			writeError(w, http.StatusNotFound, "HUB_LLM_PROMPT_CACHE_ENTRY_NOT_FOUND", "cache entry not found")
			return
		}
		scopedKey, ok := promptCacheScopedKeyForRequest(r, cacheKey)
		if !ok {
			writeError(w, http.StatusNotFound, "HUB_LLM_PROMPT_CACHE_ENTRY_NOT_FOUND", "cache entry not found")
			return
		}
		entry, err := repo.Get(r.Context(), scopedKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HUB_LLM_PROMPT_CACHE_ENTRY_FAILED", err.Error())
			return
		}
		if entry == nil {
			writeError(w, http.StatusNotFound, "HUB_LLM_PROMPT_CACHE_ENTRY_NOT_FOUND", "cache entry not found")
			return
		}
		resp := hubLLMPromptCacheEntryDetailResponse{
			CacheKey:          entry.CacheKey,
			ProviderID:        entry.ProviderID,
			Model:             entry.Model,
			Kind:              entry.Kind,
			PayloadBytes:      entry.PayloadBytes,
			CachedInputTokens: entry.CachedInputTokens,
			CacheWriteTokens:  entry.CacheWriteTokens,
			HitCount:          entry.HitCount,
		}
		if !entry.CreatedAt.IsZero() {
			resp.CreatedAt = entry.CreatedAt.UTC().Format(time.RFC3339)
		}
		if !entry.AccessedAt.IsZero() {
			resp.AccessedAt = entry.AccessedAt.UTC().Format(time.RFC3339)
		}
		if entry.ExpiresAt != nil && !entry.ExpiresAt.IsZero() {
			resp.ExpiresAt = entry.ExpiresAt.UTC().Format(time.RFC3339)
		}
		var cached cachedAuthorizedModelResponse
		if err := json.Unmarshal(entry.Payload, &cached); err == nil {
			resp.AuthorizedModel = strings.TrimSpace(cached.AuthorizedModel)
			resp.RequestedModel = strings.TrimSpace(cached.RequestedModel)
			resp.OrderedProviders = append([]string(nil), cached.OrderedProviders...)
			resp.NormalizedRequest = append([]byte(nil), cached.NormalizedRequest...)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func DeleteHubLLMPromptCacheEntryHandler(promptCacheSource any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cacheKey := strings.TrimSpace(r.URL.Query().Get("cache_key"))
		if cacheKey == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CACHE_KEY", "cache_key is required")
			return
		}
		deleter, ok := promptCacheEntryDeleter(promptCacheSource)
		if !ok || deleter == nil {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": false, "cache_key": cacheKey})
			return
		}
		scopedKey, ok := promptCacheScopedKeyForRequest(r, cacheKey)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": false, "cache_key": cacheKey})
			return
		}
		if err := deleter.Delete(r.Context(), scopedKey); err != nil {
			writeError(w, http.StatusInternalServerError, "HUB_LLM_PROMPT_CACHE_DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "cache_key": scopedKey})
	}
}

func GetHubLLMPromptCacheEntriesHandler(promptCacheSource any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo, ok := promptCacheStatusRepository(promptCacheSource)
		if !ok || repo == nil {
			writeJSON(w, http.StatusOK, hubLLMPromptCacheEntriesResponse{Entries: []hubLLMPromptCacheEntryView{}, Providers: []string{}, Models: []string{}, Page: 1, Limit: 20, Total: 0, HasMore: false})
			return
		}
		limit := 20
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				page = n
			}
		}
		items, err := repo.ListRecent(r.Context(), 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HUB_LLM_PROMPT_CACHE_ENTRIES_FAILED", err.Error())
			return
		}
		providerFilter := strings.TrimSpace(r.URL.Query().Get("provider"))
		modelFilter := strings.TrimSpace(r.URL.Query().Get("model"))
		providerSet := make(map[string]struct{})
		modelSet := make(map[string]struct{})
		all := make([]hubLLMPromptCacheEntryView, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			if !promptCacheEntryVisibleToRequest(r, item.CacheKey) {
				continue
			}
			if item.ProviderID != "" {
				providerSet[item.ProviderID] = struct{}{}
			}
			providerMatches := providerFilter == "" || item.ProviderID == "" || strings.EqualFold(item.ProviderID, providerFilter)
			if providerMatches {
				if item.Model != "" {
					modelSet[item.Model] = struct{}{}
				}
			}
			if !providerMatches {
				continue
			}
			if modelFilter != "" && !strings.EqualFold(item.Model, modelFilter) {
				continue
			}
			view := hubLLMPromptCacheEntryView{
				CacheKey:          item.CacheKey,
				ProviderID:        item.ProviderID,
				Model:             item.Model,
				Kind:              item.Kind,
				PayloadBytes:      item.PayloadBytes,
				CachedInputTokens: item.CachedInputTokens,
				CacheWriteTokens:  item.CacheWriteTokens,
				HitCount:          item.HitCount,
			}
			if !item.CreatedAt.IsZero() {
				view.CreatedAt = item.CreatedAt.UTC().Format(time.RFC3339)
			}
			if !item.AccessedAt.IsZero() {
				view.AccessedAt = item.AccessedAt.UTC().Format(time.RFC3339)
			}
			if item.ExpiresAt != nil && !item.ExpiresAt.IsZero() {
				view.ExpiresAt = item.ExpiresAt.UTC().Format(time.RFC3339)
			}
			all = append(all, view)
		}
		providers := make([]string, 0, len(providerSet))
		for value := range providerSet {
			providers = append(providers, value)
		}
		sort.Strings(providers)
		models := make([]string, 0, len(modelSet))
		for value := range modelSet {
			models = append(models, value)
		}
		sort.Strings(models)
		start := (page - 1) * limit
		if start > len(all) {
			start = len(all)
		}
		end := start + limit
		if end > len(all) {
			end = len(all)
		}
		hasMore := end < len(all)
		writeJSON(w, http.StatusOK, hubLLMPromptCacheEntriesResponse{Entries: all[start:end], Providers: providers, Models: models, Page: page, Limit: limit, Total: len(all), HasMore: hasMore})
	}
}

func promptCacheTenantScope(r *http.Request) string {
	return strings.TrimSpace(RequestTenantID(r))
}

func promptCacheTenantScoped(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !IsGlobalAdmin(r.Context()) {
		return true
	}
	return strings.TrimSpace(r.URL.Query().Get("tenant_id")) != ""
}

func promptCacheScopedKeyForRequest(r *http.Request, cacheKey string) (string, bool) {
	cacheKey = strings.TrimSpace(cacheKey)
	if cacheKey == "" {
		return "", false
	}
	if !promptCacheTenantScoped(r) {
		return cacheKey, true
	}
	tenantID := promptCacheTenantScope(r)
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	if tenantID == store.DefaultTenantID {
		if strings.HasPrefix(cacheKey, "tenant:") {
			return "", false
		}
		return cacheKey, true
	}
	prefix := "tenant:" + tenantID + ":"
	if strings.HasPrefix(cacheKey, "tenant:") {
		if !strings.HasPrefix(cacheKey, prefix) {
			return "", false
		}
		return cacheKey, true
	}
	return prefix + cacheKey, true
}

func promptCacheEntryVisibleToRequest(r *http.Request, cacheKey string) bool {
	if !promptCacheTenantScoped(r) {
		return true
	}
	tenantID := promptCacheTenantScope(r)
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	if tenantID == store.DefaultTenantID {
		return !strings.HasPrefix(cacheKey, "tenant:")
	}
	return strings.HasPrefix(cacheKey, "tenant:"+tenantID+":")
}

func promptCacheEntryDeleter(source any) (interface {
	Delete(context.Context, string) error
}, bool) {
	switch v := source.(type) {
	case nil:
		return nil, false
	case interface {
		Delete(context.Context, string) error
	}:
		return v, true
	default:
		repo, ok := promptCacheStatusRepository(source)
		return repo, ok && repo != nil
	}
}

func promptCacheStatusRepository(source any) (store.LLMPromptCacheRepository, bool) {
	switch v := source.(type) {
	case nil:
		return nil, false
	case store.LLMPromptCacheRepository:
		return v, true
	case interface {
		Repository() store.LLMPromptCacheRepository
	}:
		repo := v.Repository()
		return repo, repo != nil
	default:
		return nil, false
	}
}
