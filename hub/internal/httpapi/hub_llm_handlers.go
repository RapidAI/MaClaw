package httpapi

import (
	"context"
	"math"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

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

func HubLLMStatusHandler(statusFn func(context.Context) string, system store.SystemSettingsRepository, promptCacheSources ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// "status" is retained for older API callers; the standalone Hub LLM
		// admin UI has been removed.
		status := "not_configured"
		if statusFn != nil {
			status = statusFn(r.Context())
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
	for providerID, stat := range reg.TokenUsage {
		if isRemoteCodingToolUsageProviderID(providerID) {
			continue
		}
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
