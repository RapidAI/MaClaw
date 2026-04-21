package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	StatusCode       int                    `json:"status_code"`
	Body             json.RawMessage        `json:"body"`
	ProviderID       string                 `json:"provider_id"`
	ServiceGroupIDs  []string               `json:"service_group_ids,omitempty"`
	Usage            corelib.TokenUsageStat `json:"usage"`
	CachedAt         string                 `json:"cached_at,omitempty"`
	AuthorizedModel  string                 `json:"authorized_model,omitempty"`
	RequestedModel   string                 `json:"requested_model,omitempty"`
	OrderedProviders []string               `json:"ordered_providers,omitempty"`
}

func llmPromptCacheable(body map[string]any, cfg HubLLMPromptCacheConfig) bool {
	if !cfg.Enabled || len(body) == 0 {
		return false
	}
	stream, _ := body["stream"].(bool)
	return !stream
}

func llmPromptCacheKey(model *llmservice.AuthorizedModel, body map[string]any, externalModel string) (string, string, error) {
	if model == nil {
		return "", "", fmt.Errorf("authorized model is required")
	}
	canonicalBody, err := json.Marshal(body)
	if err != nil {
		return "", "", err
	}
	orderedProviders := llmservice.OrderProvidersForRequest(body, model)
	fingerprint := map[string]any{
		"authorized_model":  strings.TrimSpace(model.Name),
		"requested_model":   strings.TrimSpace(externalModel),
		"ordered_providers": orderedProviders,
		"body":              json.RawMessage(canonicalBody),
	}
	payload, err := json.Marshal(fingerprint)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	return "llm_resp_" + hash, hash, nil
}

func getCachedAuthorizedModelResponse(ctx context.Context, cache llmPromptCacheStore, model *llmservice.AuthorizedModel, body map[string]any, externalModel string, cfg HubLLMPromptCacheConfig) ([]byte, int, string, []string, corelib.TokenUsageStat, bool, error) {
	if cache == nil || !llmPromptCacheable(body, cfg) {
		return nil, 0, "", nil, corelib.TokenUsageStat{}, false, nil
	}
	cacheKey, _, err := llmPromptCacheKey(model, body, externalModel)
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
	return append([]byte(nil), cached.Body...), cached.StatusCode, strings.TrimSpace(cached.ProviderID), append([]string(nil), cached.ServiceGroupIDs...), cached.Usage, true, nil
}

func putCachedAuthorizedModelResponse(ctx context.Context, cache llmPromptCacheStore, model *llmservice.AuthorizedModel, body map[string]any, externalModel string, respBody []byte, statusCode int, providerID string, serviceGroupIDs []string, usage corelib.TokenUsageStat, cfg HubLLMPromptCacheConfig) error {
	if cache == nil || !llmPromptCacheable(body, cfg) || statusCode < 200 || statusCode >= 400 || len(respBody) == 0 {
		return nil
	}
	cacheKey, inputHash, err := llmPromptCacheKey(model, body, externalModel)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	orderedProviders := []string(nil)
	if model != nil {
		orderedProviders = llmservice.OrderProvidersForRequest(body, model)
	}
	payload, err := json.Marshal(cachedAuthorizedModelResponse{
		StatusCode:       statusCode,
		Body:             append([]byte(nil), respBody...),
		ProviderID:       strings.TrimSpace(providerID),
		ServiceGroupIDs:  append([]string(nil), serviceGroupIDs...),
		Usage:            usage,
		CachedAt:         now.Format(time.RFC3339),
		AuthorizedModel:  strings.TrimSpace(model.Name),
		RequestedModel:   strings.TrimSpace(externalModel),
		OrderedProviders: orderedProviders,
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
	_, _ = cache.DeleteExpired(ctx, now)
	_, _ = cache.TrimDiskToBytes(ctx, cfg.DiskMaxBytes)
	return nil
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
