package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const hubLLMConfigKey = "hub_llm_config"

// GetHubLLMConfigHandler returns the current Hub LLM configuration.
// The API key is never sent to the frontend; instead a boolean flag
// indicates whether a key has been configured.
func GetHubLLMConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		raw, err := system.Get(r.Context(), hubLLMConfigKey)
		if err != nil || raw == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"enabled": false, "api_url": "", "api_key": "",
				"model": "", "protocol": "", "smart_route_single_device": false,
				"has_api_key": false,
			})
			return
		}
		var cfg im.HubLLMConfig
		if json.Unmarshal([]byte(raw), &cfg) != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"enabled": false, "api_url": "", "api_key": "",
				"model": "", "protocol": "", "smart_route_single_device": false,
				"has_api_key": false,
			})
			return
		}
		hasKey := cfg.APIKey != ""
		cfg.APIKey = ""
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":                   cfg.Enabled,
			"api_url":                   cfg.APIURL,
			"api_key":                   "",
			"model":                     cfg.Model,
			"protocol":                  cfg.Protocol,
			"smart_route_single_device": cfg.SmartRouteSingleDevice,
			"has_api_key":               hasKey,
		})
	}
}

// UpdateHubLLMConfigHandler saves the Hub LLM configuration.
// If the API key is empty (frontend never receives the real key), the old
// key is preserved so that saving other fields doesn't wipe the key.
func UpdateHubLLMConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		var cfg im.HubLLMConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		// Empty key means the user did not change it; preserve the stored one.
		if cfg.APIKey == "" {
			old := loadHubLLMConfig(r, system)
			if old != nil {
				cfg.APIKey = old.APIKey
			}
		}

		data, _ := json.Marshal(cfg)
		if err := system.Set(r.Context(), hubLLMConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "HUB_LLM_CONFIG_SAVE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":                   cfg.Enabled,
			"api_url":                   cfg.APIURL,
			"api_key":                   "",
			"model":                     cfg.Model,
			"protocol":                  cfg.Protocol,
			"smart_route_single_device": cfg.SmartRouteSingleDevice,
			"has_api_key":               cfg.APIKey != "",
		})
	}
}

// TestHubLLMHandler sends a simple prompt to verify the LLM API is reachable
// and the key is valid. Returns success/failure + latency.
func TestHubLLMHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		cfg := loadHubLLMConfig(r, system)
		if cfg == nil || cfg.APIURL == "" || cfg.APIKey == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"error":   "Hub LLM config requires API URL / Key",
			})
			return
		}

		llmCfg := cfg.ToMaclawLLMConfig()
		messages := []interface{}{
			map[string]string{"role": "user", "content": "Reply with exactly: pong"},
		}

		start := time.Now()
		resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, http.DefaultClient, 10*time.Second)
		elapsed := time.Since(start)

		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      err.Error(),
				"latency_ms": elapsed.Milliseconds(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success":    true,
			"reply":      resp.Content,
			"latency_ms": elapsed.Milliseconds(),
		})
	}
}

type hubLLMCacheStorageStatus struct {
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
}

type hubLLMCacheStatus struct {
	InputTokens       int64                        `json:"input_tokens"`
	CachedInputTokens int64                        `json:"cached_input_tokens"`
	CacheWriteTokens  int64                        `json:"cache_write_tokens"`
	InputCostRMB      float64                      `json:"input_cost_rmb,omitempty"`
	OutputCostRMB     float64                      `json:"output_cost_rmb,omitempty"`
	TotalCostRMB      float64                      `json:"total_cost_rmb,omitempty"`
	Requests          int64                        `json:"requests"`
	CachedRequests    int64                        `json:"cached_requests"`
	CacheRate         float64                      `json:"cache_rate"`
	CacheReuseRate    float64                      `json:"cache_reuse_rate"`
	Config            HubLLMPromptCacheConfig      `json:"config"`
	LocalStorage      hubLLMCacheStorageStatus     `json:"local_storage"`
	Runtime           llmPromptCacheRuntimeMetrics `json:"runtime"`
}

func HubLLMStatusHandler(statusFn func() string, system store.SystemSettingsRepository, promptCacheSources ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		status := "not_configured"
		if statusFn != nil {
			status = statusFn()
		}
		promptCacheSource := firstPromptCacheStatusSource(promptCacheSources)
		cfg := loadCachedHubLLMPromptCacheConfig(r.Context(), system)
		applyHubLLMPromptCacheRuntimeConfig(promptCacheSource, cfg)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       status,
			"prompt_cache": hubLLMPromptCacheStatus(r, system, promptCacheSource, cfg),
		})
	}
}

func hubLLMPromptCacheStatus(r *http.Request, system store.SystemSettingsRepository, promptCacheSource any, cfg HubLLMPromptCacheConfig) hubLLMCacheStatus {
	runtimeMetrics := hubLLMPromptCacheRuntimeMetricsSnapshot()
	if isTenantScopedAdminRequest(r) {
		runtimeMetrics = hubLLMPromptCacheRuntimeMetricsSnapshotForTenant(RequestTenantID(r))
	}
	out := hubLLMCacheStatus{Config: cfg, Runtime: runtimeMetrics}
	if system == nil {
		return out
	}
	reg, err := loadCachedLLMProviderRegistry(r.Context(), system)
	if err != nil || reg == nil {
		return out
	}
	for _, stat := range reg.TokenUsage {
		if stat == nil {
			continue
		}
		out.InputTokens += stat.InputTokens
		out.CachedInputTokens += stat.CachedInputTokens
		out.CacheWriteTokens += stat.CacheWriteTokens
		out.InputCostRMB += stat.InputCostRMB
		out.OutputCostRMB += stat.OutputCostRMB
		out.TotalCostRMB += stat.TotalCostRMB
		out.Requests += stat.Requests
		out.CachedRequests += stat.CachedRequests
	}
	out.CacheRate = hubLLMRate(out.CachedRequests, out.Requests)
	out.CacheReuseRate = hubLLMRate(out.CachedInputTokens, out.InputTokens)
	if stats, err := promptCacheStatusFromSource(r.Context(), promptCacheSource); err == nil && stats != nil {
		out.LocalStorage = hubLLMCacheStorageStatus{
			MemoryEntries:    stats.MemoryEntries,
			MemoryBytes:      stats.MemoryBytes,
			MemoryMaxEntries: stats.MemoryMaxEntries,
			MemoryMaxBytes:   stats.MemoryMaxBytes,
			MemoryHits:       stats.MemoryHits,
			DiskEntries:      stats.DiskEntries,
			DiskBytes:        stats.DiskBytes,
			DiskExpired:      stats.DiskExpired,
			DiskExpiredBytes: stats.DiskExpiredBytes,
			DiskHits:         stats.DiskHits,
		}
	}
	return out
}

func hubLLMRate(hit int64, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round((float64(hit)/float64(total))*1000) / 10
}

func loadHubLLMConfig(r *http.Request, system store.SystemSettingsRepository) *im.HubLLMConfig {
	raw, err := system.Get(r.Context(), hubLLMConfigKey)
	if err != nil || raw == "" {
		return nil
	}
	var cfg im.HubLLMConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return nil
	}
	return &cfg
}

// ConversationStatsProvider returns active context count and total rounds.
type ConversationStatsProvider interface {
	ConvContextStats() (int, int)
}

// ConversationStatsHandler returns conversation context statistics.
func ConversationStatsHandler(provider ConversationStatsProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		contexts, rounds := provider.ConvContextStats()
		writeJSON(w, http.StatusOK, map[string]any{
			"active_contexts": contexts,
			"total_rounds":    rounds,
		})
	}
}
