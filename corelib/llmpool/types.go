// Package llmpool provides shared LLM service group management, provider dispatching,
// caching, and rate limiting primitives used by both Hub and HubCenter.
package llmpool

import "time"

// ProviderConfig describes an LLM backend provider endpoint.
// Used by both Hub (endpoint forwarding) and HubCenter (proxy dispatching).
type ProviderConfig struct {
	ID                       string                   `json:"id"`
	Name                     string                   `json:"name"`
	APIURL                   string                   `json:"api_url"`
	APIKey                   string                   `json:"api_key,omitempty"`
	Paused                   bool                     `json:"paused,omitempty"`            // paused providers stay configured but are skipped by dispatch
	Sequence                 int                      `json:"sequence"`                    // admin dispatch order; smaller numbers are tried first. Runtime health does not rewrite this. Always exported so admin cards can show and sort by it.
	Protocol                 string                   `json:"protocol"`                    // "openai" / "anthropic"
	WireAPI                  string                   `json:"wire_api,omitempty"`          // "" / "responses"
	Models                   []string                 `json:"models,omitempty"`            // supported models
	CapabilityTags           []string                 `json:"capability_tags,omitempty"`   // e.g. "tools", "vision", "document"
	Priority                 int                      `json:"priority,omitempty"`          // higher = preferred
	ResolutionTier           int                      `json:"resolution_tier,omitempty"`   // lower = cheaper
	CreditMultiplier         float64                  `json:"credit_multiplier,omitempty"` // default 1.0 when no schedule window matches
	Timezone                 string                   `json:"timezone,omitempty"`          // IANA timezone for schedule windows; default Asia/Shanghai
	CreditMultiplierSchedule []CreditMultiplierWindow `json:"credit_multiplier_schedule,omitempty"`
	// TokenPricing is the provider-wide directional token price. When it has a
	// usable Credits price it is the authoritative settlement price for every
	// route dispatched to this provider. Route pricing exists only as a legacy
	// fallback for providers without a configured price.
	TokenPricing             TokenPricing `json:"token_pricing,omitempty"`
	MaxConcurrency           int          `json:"max_concurrency,omitempty"`             // 0 = unlimited; HubCenter skips to the next provider when this limit is reached
	MaxQueueWaiters          int          `json:"max_queue_waiters,omitempty"`           // max requests waiting in queue
	QueueTimeoutMS           int          `json:"queue_timeout_ms,omitempty"`            // max wait time in queue
	UpstreamTimeoutSec       int          `json:"upstream_timeout_sec,omitempty"`        // HTTP timeout to upstream
	CircuitBreakerThreshold  int          `json:"circuit_breaker_threshold,omitempty"`   // consecutive failures before cooldown; HubCenter treats <=0 as 2
	CircuitBreakerCooldownMS int          `json:"circuit_breaker_cooldown_ms,omitempty"` // base cooldown; HubCenter treats <=0 as 10s
	FailureBackoffBaseMS     int          `json:"failure_backoff_base_ms,omitempty"`
	FailureBackoffMaxMS      int          `json:"failure_backoff_max_ms,omitempty"` // cap for exponential cooldown; HubCenter treats <=0 as 5m
}

// ServiceGroup defines a set of models with associated provider routing.
// Hub uses this for user-facing model groups; HubCenter uses it for
// internal dispatch policy among backend providers.
type ServiceGroup struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	AgentID       string          `json:"agent_id,omitempty"`
	AgentName     string          `json:"agent_name,omitempty"`
	AccessPolicy  string          `json:"access_policy,omitempty"` // "free" / "grant_required"
	Kind          string          `json:"kind,omitempty"`          // "" / "static" / "dynamic"
	QualityFloor  string          `json:"quality_floor,omitempty"`
	ExposedModels []string        `json:"exposed_models,omitempty"`
	Routes        []WorkloadRoute `json:"routes,omitempty"`
	Models        []ModelConfig   `json:"models"`
}

// WorkloadRoute maps one WorkloadClass (or the balanced fallback) to a logical model.
type WorkloadRoute struct {
	Class   string `json:"class"`
	Model   string `json:"model"`
	Quality string `json:"quality,omitempty"`
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
	ProviderID string `json:"provider_id"`
	Model      string `json:"model,omitempty"`
	// BillingMode is explicit whenever this route is introduced through the
	// token-pricing UI. Empty is retained only for legacy routes.
	BillingMode      string   `json:"billing_mode,omitempty"`
	CapabilityTags   []string `json:"capability_tags,omitempty"`
	Priority         int      `json:"priority,omitempty"`
	ResolutionTier   int      `json:"resolution_tier,omitempty"`
	CreditMultiplier float64  `json:"credit_multiplier,omitempty"`
	// TokenPricing is a legacy per-model fallback input/output base price for
	// providers without provider-level pricing. It is kept separate from
	// CreditMultiplier, which remains a dispatch compatibility field until all
	// routing code uses token prices directly.
	TokenPricing TokenPricing `json:"token_pricing,omitempty"`
}

// CreditMultiplierWindow is one vendor time-of-use billing window.
// Days use Go weekday numbers: 0=Sunday ... 6=Saturday. Empty Days means every day.
// Start and End are local clock times "HH:MM". End is exclusive. If Start > End,
// the window wraps midnight (for example 22:00–08:00).
type CreditMultiplierWindow struct {
	Days       []int   `json:"days,omitempty"`
	Start      string  `json:"start"`
	End        string  `json:"end"`
	Multiplier float64 `json:"multiplier,omitempty"`
}

// ProviderBillingPolicy is the vendor billing rule attached to a provider.
// HubCenter publishes this to Hub so official MaClaw credit deduction can
// follow the same time-of-use rates.
type ProviderBillingPolicy struct {
	ProviderID               string                   `json:"provider_id,omitempty"`
	Timezone                 string                   `json:"timezone,omitempty"`
	CreditMultiplier         float64                  `json:"credit_multiplier,omitempty"`
	CreditMultiplierSchedule []CreditMultiplierWindow `json:"credit_multiplier_schedule,omitempty"`
	Paused                   bool                     `json:"paused,omitempty"`
}

// EffectiveProviderSequence returns the dispatch rank for a provider.
// Unset or non-positive sequences are tried after every numbered provider.
func EffectiveProviderSequence(sequence int) int {
	if sequence <= 0 {
		return int(^uint(0) >> 1)
	}
	return sequence
}

const (
	DefaultCreditMultiplierTimezone = "Asia/Shanghai"
	CreditMultiplierHeader          = "X-MaClaw-Credit-Multiplier"
	ProviderIDHeader                = "X-Provider-ID"
	WorkloadClassHeader             = "X-MaClaw-Workload-Class"
	ResolvedModelHeader             = "X-MaClaw-Resolved-Model"
	WorkflowTypeHeader              = "X-MaClaw-Workflow-Type"
	PhaseKindHeader                 = "X-MaClaw-Phase-Kind"
	TaskTypeHeader                  = "X-MaClaw-Task-Type"
	ServiceGroupIDHeader            = "X-MaClaw-Service-Group-ID"
)

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
	ProviderID     string    `json:"provider_id"`
	Model          string    `json:"model"`
	ServiceGroupID string    `json:"service_group_id,omitempty"`
	WorkloadClass  string    `json:"workload_class,omitempty"`
	ClassSource    string    `json:"class_source,omitempty"`
	Preview        string    `json:"preview,omitempty"`
	InputTokens    int64     `json:"input_tokens"`
	OutputTokens   int64     `json:"output_tokens"`
	Credits        float64   `json:"credits"`
	CacheHit       bool      `json:"cache_hit"`
	AuthID         string    `json:"auth_id,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}
