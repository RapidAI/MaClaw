// Package llmpool provides shared LLM service group management, provider dispatching,
// caching, and rate limiting primitives used by both Hub and HubCenter.
package llmpool

import "time"

// ProviderConfig describes an LLM backend provider endpoint.
// Used by both Hub (endpoint forwarding) and HubCenter (proxy dispatching).
type ProviderConfig struct {
	ID                       string   `json:"id"`
	Name                     string   `json:"name"`
	APIURL                   string   `json:"api_url"`
	APIKey                   string   `json:"api_key,omitempty"`
	Protocol                 string   `json:"protocol"`                       // "openai" / "anthropic"
	WireAPI                  string   `json:"wire_api,omitempty"`             // "" / "responses"
	Models                   []string `json:"models,omitempty"`               // supported models
	CapabilityTags           []string `json:"capability_tags,omitempty"`      // e.g. "tools", "vision", "document"
	Priority                 int      `json:"priority,omitempty"`             // higher = preferred
	ResolutionTier           int      `json:"resolution_tier,omitempty"`      // lower = cheaper
	CreditMultiplier         float64  `json:"credit_multiplier,omitempty"`    // default 1.0
	MaxConcurrency           int      `json:"max_concurrency,omitempty"`      // per-provider concurrency limit
	MaxQueueWaiters          int      `json:"max_queue_waiters,omitempty"`    // max requests waiting in queue
	QueueTimeoutMS           int      `json:"queue_timeout_ms,omitempty"`     // max wait time in queue
	UpstreamTimeoutSec       int      `json:"upstream_timeout_sec,omitempty"` // HTTP timeout to upstream
	CircuitBreakerThreshold  int      `json:"circuit_breaker_threshold,omitempty"`
	CircuitBreakerCooldownMS int      `json:"circuit_breaker_cooldown_ms,omitempty"`
	FailureBackoffBaseMS     int      `json:"failure_backoff_base_ms,omitempty"`
	FailureBackoffMaxMS      int      `json:"failure_backoff_max_ms,omitempty"`
}

// ServiceGroup defines a set of models with associated provider routing.
// Hub uses this for user-facing model groups; HubCenter uses it for
// internal dispatch policy among backend providers.
type ServiceGroup struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	AgentID      string        `json:"agent_id,omitempty"`
	AgentName    string        `json:"agent_name,omitempty"`
	AccessPolicy string        `json:"access_policy,omitempty"` // "free" / "grant_required"
	Models       []ModelConfig `json:"models"`
}

// ModelConfig maps a logical model name to one or more provider backends.
type ModelConfig struct {
	Name             string                `json:"name"`
	ProviderIDs      []string              `json:"provider_ids,omitempty"`
	ProviderConfigs  []ModelProviderConfig `json:"provider_configs"`
	CapabilityTags   []string              `json:"capability_tags,omitempty"`
	Priority         int                   `json:"priority,omitempty"`
	ResolutionTier   int                   `json:"resolution_tier,omitempty"`
	CreditMultiplier float64               `json:"credit_multiplier,omitempty"`
}

// ModelProviderConfig holds per-provider overrides for a specific model.
type ModelProviderConfig struct {
	ProviderID       string   `json:"provider_id"`
	Model            string   `json:"model,omitempty"`
	CapabilityTags   []string `json:"capability_tags,omitempty"`
	Priority         int      `json:"priority,omitempty"`
	ResolutionTier   int      `json:"resolution_tier,omitempty"`
	CreditMultiplier float64  `json:"credit_multiplier,omitempty"`
}

// CacheEntry represents a cached LLM response. This is the shared type used
// by both Hub and HubCenter cache layers. Storage backends (SQLite, etc.)
// map to/from this type.
type CacheEntry struct {
	CacheKey          string     `json:"cache_key"`
	ProviderID        string     `json:"provider_id"`
	Model             string     `json:"model"`
	Kind              string     `json:"kind"` // "metadata" / "full"
	InputHash         string     `json:"input_hash"`
	Payload           []byte     `json:"payload"`
	PayloadBytes      int64      `json:"payload_bytes"`
	CachedInputTokens int64      `json:"cached_input_tokens"`
	CacheWriteTokens  int64      `json:"cache_write_tokens"`
	HitCount          int64      `json:"hit_count"`
	CreatedAt         time.Time  `json:"created_at"`
	AccessedAt        time.Time  `json:"accessed_at"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
}

// CacheStats aggregates cache metrics.
type CacheStats struct {
	Entries        int64 `json:"entries"`
	TotalBytes     int64 `json:"total_bytes"`
	ExpiredEntries int64 `json:"expired_entries"`
	ExpiredBytes   int64 `json:"expired_bytes"`
	TotalHits      int64 `json:"total_hits"`
}

// UsageRecord represents a single LLM request's usage for billing/statistics.
type UsageRecord struct {
	ProviderID   string    `json:"provider_id"`
	Model        string    `json:"model"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	Credits      float64   `json:"credits"`
	CacheHit     bool      `json:"cache_hit"`
	Timestamp    time.Time `json:"timestamp"`
}
