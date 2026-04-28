package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type llmPromptCacheStore interface {
	Get(ctx context.Context, cacheKey string) (*store.LLMPromptCacheEntry, error)
	Put(ctx context.Context, entry *store.LLMPromptCacheEntry) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
	TrimDiskToBytes(ctx context.Context, maxBytes int64) (int64, error)
	Status(ctx context.Context, now time.Time) (*llmcache.Status, error)
}

type cachedAuthorizedModelResponse struct {
	StatusCode        int                    `json:"status_code"`
	Body              json.RawMessage        `json:"body"`
	ProviderID        string                 `json:"provider_id"`
	ServiceGroupIDs   []string               `json:"service_group_ids,omitempty"`
	Usage             corelib.TokenUsageStat `json:"usage"`
	CachedAt          string                 `json:"cached_at,omitempty"`
	AuthorizedModel   string                 `json:"authorized_model,omitempty"`
	RequestedModel    string                 `json:"requested_model,omitempty"`
	OrderedProviders  []string               `json:"ordered_providers,omitempty"`
	NormalizedRequest json.RawMessage        `json:"normalized_request,omitempty"`
}

type llmPromptCacheRuntimeMetrics struct {
	CacheableRequests      int64            `json:"cacheable_requests"`
	BypassDisabled         int64            `json:"bypass_disabled"`
	BypassEmptyBody        int64            `json:"bypass_empty_body"`
	BypassStreaming        int64            `json:"bypass_streaming"`
	BypassMultiChoice      int64            `json:"bypass_multi_choice"`
	BypassTemperature      int64            `json:"bypass_temperature"`
	BypassTopP             int64            `json:"bypass_top_p"`
	BypassPresencePenalty  int64            `json:"bypass_presence_penalty"`
	BypassFrequencyPenalty int64            `json:"bypass_frequency_penalty"`
	SingleflightSharedHits int64            `json:"singleflight_shared_hits"`
	SingleflightSavedCalls int64            `json:"singleflight_saved_calls"`
	BypassReasons          map[string]int64 `json:"bypass_reasons,omitempty"`
}

const llmPromptCacheMaintenanceMinInterval = 15 * time.Second

var globalLLMPromptCacheMaintenance struct {
	running atomic.Bool
	lastRun atomic.Int64
}

var globalLLMPromptCacheMetrics struct {
	cacheableRequests      atomic.Int64
	bypassDisabled         atomic.Int64
	bypassEmptyBody        atomic.Int64
	bypassStreaming        atomic.Int64
	bypassMultiChoice      atomic.Int64
	bypassTemperature      atomic.Int64
	bypassTopP             atomic.Int64
	bypassPresencePenalty  atomic.Int64
	bypassFrequencyPenalty atomic.Int64
	singleflightSharedHits atomic.Int64
	singleflightSavedCalls atomic.Int64
}

func llmPromptCacheable(body map[string]any, cfg HubLLMPromptCacheConfig) bool {
	reason, cacheable := llmPromptCacheDecision(body, cfg)
	recordLLMPromptCacheDecision(reason, cacheable)
	return cacheable
}

func llmPromptCacheDecision(body map[string]any, cfg HubLLMPromptCacheConfig) (string, bool) {
	decision := corelib.LLMPromptCacheable(body, hubPromptCacheOptions(cfg))
	return decision.Reason, decision.Cacheable
}

func recordLLMPromptCacheDecision(reason string, cacheable bool) {
	if cacheable {
		globalLLMPromptCacheMetrics.cacheableRequests.Add(1)
		return
	}
	switch reason {
	case "disabled":
		globalLLMPromptCacheMetrics.bypassDisabled.Add(1)
	case "empty_body":
		globalLLMPromptCacheMetrics.bypassEmptyBody.Add(1)
	case "streaming":
		globalLLMPromptCacheMetrics.bypassStreaming.Add(1)
	case "multi_choice":
		globalLLMPromptCacheMetrics.bypassMultiChoice.Add(1)
	case "temperature":
		globalLLMPromptCacheMetrics.bypassTemperature.Add(1)
	case "top_p":
		globalLLMPromptCacheMetrics.bypassTopP.Add(1)
	case "presence_penalty":
		globalLLMPromptCacheMetrics.bypassPresencePenalty.Add(1)
	case "frequency_penalty":
		globalLLMPromptCacheMetrics.bypassFrequencyPenalty.Add(1)
	}
}

func hubLLMPromptCacheRuntimeMetricsSnapshot() llmPromptCacheRuntimeMetrics {
	bypassReasons := map[string]int64{}
	addReason := func(name string, value int64) {
		if value > 0 {
			bypassReasons[name] = value
		}
	}
	metrics := llmPromptCacheRuntimeMetrics{
		CacheableRequests:      globalLLMPromptCacheMetrics.cacheableRequests.Load(),
		BypassDisabled:         globalLLMPromptCacheMetrics.bypassDisabled.Load(),
		BypassEmptyBody:        globalLLMPromptCacheMetrics.bypassEmptyBody.Load(),
		BypassStreaming:        globalLLMPromptCacheMetrics.bypassStreaming.Load(),
		BypassMultiChoice:      globalLLMPromptCacheMetrics.bypassMultiChoice.Load(),
		BypassTemperature:      globalLLMPromptCacheMetrics.bypassTemperature.Load(),
		BypassTopP:             globalLLMPromptCacheMetrics.bypassTopP.Load(),
		BypassPresencePenalty:  globalLLMPromptCacheMetrics.bypassPresencePenalty.Load(),
		BypassFrequencyPenalty: globalLLMPromptCacheMetrics.bypassFrequencyPenalty.Load(),
		SingleflightSharedHits: globalLLMPromptCacheMetrics.singleflightSharedHits.Load(),
		SingleflightSavedCalls: globalLLMPromptCacheMetrics.singleflightSavedCalls.Load(),
	}
	addReason("disabled", metrics.BypassDisabled)
	addReason("empty_body", metrics.BypassEmptyBody)
	addReason("streaming", metrics.BypassStreaming)
	addReason("multi_choice", metrics.BypassMultiChoice)
	addReason("temperature", metrics.BypassTemperature)
	addReason("top_p", metrics.BypassTopP)
	addReason("presence_penalty", metrics.BypassPresencePenalty)
	addReason("frequency_penalty", metrics.BypassFrequencyPenalty)
	if len(bypassReasons) > 0 {
		metrics.BypassReasons = bypassReasons
	}
	return metrics
}

func llmPromptCacheKey(model *llmservice.AuthorizedModel, body map[string]any, externalModel string, cfg HubLLMPromptCacheConfig) (string, string, error) {
	if model == nil {
		return "", "", fmt.Errorf("authorized model is required")
	}
	return corelib.LLMPromptCacheKey(model.Name, externalModel, body, hubPromptCacheOptions(cfg))
}

func getCachedAuthorizedModelResponse(ctx context.Context, cache llmPromptCacheStore, model *llmservice.AuthorizedModel, body map[string]any, externalModel string, cfg HubLLMPromptCacheConfig) ([]byte, int, string, []string, corelib.TokenUsageStat, bool, error) {
	if cache == nil || !llmPromptCacheable(body, cfg) {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, nil
	}
	cacheKey, _, err := llmPromptCacheKey(model, body, externalModel, cfg)
	if err != nil {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, err
	}
	entry, err := cache.Get(ctx, cacheKey)
	if err != nil || entry == nil {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, err
	}
	var cached cachedAuthorizedModelResponse
	if err := json.Unmarshal(entry.Payload, &cached); err != nil {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, err
	}
	if cached.StatusCode <= 0 || len(cached.Body) == 0 {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, nil
	}
	respBody := corelib.OverrideOpenAIResponseModel(append([]byte(nil), cached.Body...), strings.TrimSpace(externalModel))
	return respBody, cached.StatusCode, strings.TrimSpace(cached.ProviderID), normalizeCacheServiceGroupIDs(cached.ServiceGroupIDs), cached.Usage, true, nil
}

func putCachedAuthorizedModelResponse(ctx context.Context, cache llmPromptCacheStore, model *llmservice.AuthorizedModel, body map[string]any, externalModel string, respBody []byte, statusCode int, providerID string, serviceGroupIDs []string, usage corelib.TokenUsageStat, cfg HubLLMPromptCacheConfig) error {
	if cache == nil || !llmPromptCacheable(body, cfg) || statusCode < 200 || statusCode >= 400 || len(respBody) == 0 {
		return nil
	}
	cacheKey, inputHash, err := llmPromptCacheKey(model, body, externalModel, cfg)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	orderedProviders := []string(nil)
	if model != nil {
		orderedProviders = llmservice.OrderProvidersForRequest(normalizePromptCacheBody(body, cfg), model)
	}
	serviceGroupIDs = normalizeCacheServiceGroupIDs(serviceGroupIDs)
	normalizedBody := normalizePromptCacheBody(body, cfg)
	normalizedPayload, err := json.Marshal(normalizedBody)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(cachedAuthorizedModelResponse{
		StatusCode:        statusCode,
		Body:              append([]byte(nil), respBody...),
		ProviderID:        strings.TrimSpace(providerID),
		ServiceGroupIDs:   append([]string(nil), serviceGroupIDs...),
		Usage:             usage,
		CachedAt:          now.Format(time.RFC3339),
		AuthorizedModel:   strings.TrimSpace(model.Name),
		RequestedModel:    strings.TrimSpace(externalModel),
		OrderedProviders:  orderedProviders,
		NormalizedRequest: json.RawMessage(normalizedPayload),
	})
	if err != nil {
		return err
	}
	expiresAt := now.Add(time.Duration(cfg.TTLSeconds) * time.Second)
	entry := &store.LLMPromptCacheEntry{
		CacheKey:          cacheKey,
		ProviderID:        strings.TrimSpace(providerID),
		Model:             strings.TrimSpace(model.Name),
		Kind:              "chat_completion_response",
		InputHash:         inputHash,
		Payload:           payload,
		PayloadBytes:      int64(len(payload)),
		CachedInputTokens: usage.CachedInputTokens,
		CacheWriteTokens:  usage.CacheWriteTokens,
		CreatedAt:         now,
		AccessedAt:        now,
		ExpiresAt:         &expiresAt,
	}
	if err := cache.Put(ctx, entry); err != nil {
		return err
	}
	scheduleHubLLMPromptCacheMaintenance(cache, cfg)
	return nil
}

func scheduleHubLLMPromptCacheMaintenance(cache llmPromptCacheStore, cfg HubLLMPromptCacheConfig) {
	if cache == nil {
		return
	}
	now := time.Now().UnixNano()
	lastRun := globalLLMPromptCacheMaintenance.lastRun.Load()
	if now-lastRun < int64(llmPromptCacheMaintenanceMinInterval) {
		return
	}
	if !globalLLMPromptCacheMaintenance.running.CompareAndSwap(false, true) {
		return
	}
	if !globalLLMPromptCacheMaintenance.lastRun.CompareAndSwap(lastRun, now) {
		globalLLMPromptCacheMaintenance.running.Store(false)
		return
	}
	go func() {
		defer globalLLMPromptCacheMaintenance.running.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		runHubLLMPromptCacheMaintenance(ctx, cache, cfg)
	}()
}

func runHubLLMPromptCacheMaintenance(ctx context.Context, cache llmPromptCacheStore, cfg HubLLMPromptCacheConfig) {
	if cache == nil {
		return
	}
	now := time.Now().UTC()
	_, _ = cache.DeleteExpired(ctx, now)
	_, _ = cache.TrimDiskToBytes(ctx, cfg.DiskMaxBytes)
}

func normalizePromptCacheBody(body map[string]any, cfg HubLLMPromptCacheConfig) map[string]any {
	return corelib.NormalizeLLMPromptCacheBody(body, hubPromptCacheOptions(cfg))
}

func hubPromptCacheOptions(cfg HubLLMPromptCacheConfig) corelib.LLMPromptCacheOptions {
	return corelib.LLMPromptCacheOptions{
		Enabled:                      cfg.Enabled,
		NormalizeDeterministicParams: cfg.NormalizeDeterministicParams,
		IgnoreModelField:             cfg.IgnoreModelField,
		IgnoreUserField:              cfg.IgnoreUserField,
		IgnoreMetadataField:          cfg.IgnoreMetadataField,
	}
}

func promptCacheStatusFromSource(ctx context.Context, cacheSource any) (*llmcache.Status, error) {
	switch source := cacheSource.(type) {
	case nil:
		return nil, nil
	case llmPromptCacheStore:
		return source.Status(ctx, time.Now().UTC())
	case store.LLMPromptCacheRepository:
		stats, err := source.Stats(ctx, time.Now().UTC())
		if err != nil || stats == nil {
			return nil, err
		}
		return &llmcache.Status{
			DiskEntries:      stats.Entries,
			DiskBytes:        stats.TotalBytes,
			DiskExpired:      stats.ExpiredEntries,
			DiskExpiredBytes: stats.ExpiredBytes,
			DiskHits:         stats.TotalHits,
		}, nil
	default:
		return nil, nil
	}
}

func firstPromptCacheStatusSource(sources []any) any {
	for _, source := range sources {
		if source != nil {
			return source
		}
	}
	return nil
}

func normalizeCacheServiceGroupIDs(serviceGroupIDs []string) []string {
	if len(serviceGroupIDs) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(serviceGroupIDs))
	for _, id := range serviceGroupIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
