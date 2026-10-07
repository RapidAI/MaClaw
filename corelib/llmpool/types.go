// Package llmpool provides shared LLM service group management, provider dispatching,
// caching, and rate limiting primitives used by both Hub and HubCenter.
package llmpool

import "time"

// ProviderConfig describes an LLM backend provider endpoint.
// Used by both Hub (endpoint forwarding) and HubCenter (proxy dispatching).
type ProviderConfig struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	APIURL         string   `json:"api_url"`
	APIKey         string   `json:"api_key,omitempty"`
	Paused         bool     `json:"paused,omitempty"`          // paused providers stay configured but are skipped by dispatch
	Sequence       int      `json:"sequence"`                  // admin dispatch order; smaller numbers are tried first. Runtime health does not rewrite this. Always exported so admin cards can show and sort by it.
	Protocol       string   `json:"protocol"`                  // "openai" / "anthropic"
	WireAPI        string   `json:"wire_api,omitempty"`        // "" / "responses"
	Models         []string `json:"models,omitempty"`          // supported models
	CapabilityTags []string `json:"capability_tags,omitempty"` // e.g. "tools", "vision", "document"
	Priority       int      `json:"priority,omitempty"`        // higher = preferred
	// DispatchWeight is this member's share of traffic inside its provider
	// array. 0 and 1 are an equal share. Larger values are chosen more often.
	DispatchWeight int `json:"dispatch_weight,omitempty"`
	// RequestsPerMinute and RequestsPerDay cap upstream calls counted by this
	// process. 0 means unlimited. A member that has used its cap is skipped
	// until the window resets.
	RequestsPerMinute int `json:"requests_per_minute,omitempty"`
	RequestsPerDay    int `json:"requests_per_day,omitempty"`
	// RateLimitCooldownSec is how long to skip this member after HTTP 429.
	// 0 keeps the default 60 second pause.
	RateLimitCooldownSec int `json:"rate_limit_cooldown_sec,omitempty"`
	// MaxInputTokensPerRequest and MaxOutputTokensPerRequest cap a single
	// Token Bank call. Zero means unlimited. The proxy enforces them only for
	// token-bank member ids.
	MaxInputTokensPerRequest  int64 `json:"max_input_tokens_per_request,omitempty"`
	MaxOutputTokensPerRequest int64 `json:"max_output_tokens_per_request,omitempty"`
	// Token Bank snapshot. Published with the member so a call that is already
	// in flight can still be settled after the share row is deleted. A zero
	// multiplier means this provider is not a bank member.
	TokenBankOwnerUserID      string  `json:"token_bank_owner_user_id,omitempty"`
	TokenBankTier             string  `json:"token_bank_tier,omitempty"`
	TokenBankTierMultiplier   float64 `json:"token_bank_tier_multiplier,omitempty"`
	TokenBankShareDisplayName string  `json:"token_bank_share_display_name,omitempty"`
	// TokenBankVisibility is "public" or "private". Empty means public.
	// A private member is dispatched only to TokenBankAudiences.
	TokenBankVisibility string `json:"token_bank_visibility,omitempty"`
	// TokenBankAudiences is who may consume a private share. A row matches
	// when each non-empty id equals the request. An empty list matches nobody.
	TokenBankAudiences []TokenBankAudience `json:"token_bank_audiences,omitempty"`
	// TokenBankCanaryUntil is RFC3339. Until that time the member takes about
	// 5% of traffic when siblings exist. Empty means the member is at full share.
	TokenBankCanaryUntil string `json:"token_bank_canary_until,omitempty"`
	// TokenBankExtraKeys are additional upstream keys rotated with APIKey so
	// one share can spread a provider's per-key rate limit.
	TokenBankExtraKeys []string `json:"token_bank_extra_keys,omitempty"`
	// TokenBankShareWindow is when this member may be dialed. The zero value
	// means always. It is not a billing schedule: CreditMultiplierSchedule
	// stays empty so a cheap-hour window cannot change the consumer bill.
	TokenBankShareWindow TokenBankShareWindow `json:"token_bank_share_window,omitzero"`
	// ModelMap rewrites a public model name to this member's upstream model id.
	// Example: {"free-llama-70b": "meta-llama/Llama-3.3-70B-Instruct"}.
	ModelMap                 map[string]string        `json:"model_map,omitempty"`
	ResolutionTier           int                      `json:"resolution_tier,omitempty"`   // lower = cheaper
	CreditMultiplier         float64                  `json:"credit_multiplier,omitempty"` // default 1.0 when no schedule window matches
	Timezone                 string                   `json:"timezone,omitempty"`          // IANA timezone for schedule windows; default Asia/Shanghai
	CreditMultiplierSchedule []CreditMultiplierWindow `json:"credit_multiplier_schedule,omitempty"`
	// ServeWindows is when this provider may answer requests. Multiple windows
	// are a union: one match opens the provider, and an empty list means
	// always available. It is a dial gate, not a billing schedule, so it never
	// changes what the consumer is charged.
	ServeWindows []TokenBankShareWindow `json:"serve_windows,omitempty"`
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
	// AllowedNodeIDs is the HubCenter node allowlist that may egress this
	// provider. Empty means every cluster node may call upstream (default).
	AllowedNodeIDs []string `json:"allowed_node_ids,omitempty"`
	// AllowedNodes is a write-only comma-separated form of AllowedNodeIDs,
	// for example "hc-1, hc-2, hc-3". It is folded into AllowedNodeIDs on save
	// and is not stored.
	AllowedNodes string `json:"allowed_nodes,omitempty"`
	// ArrayID is the logical provider array this upstream belongs to.
	// Empty is normalized to this provider's own ID, so each existing
	// provider starts as an independent one-member array.
	ArrayID string `json:"array_id,omitempty"`
	// ArrayName renames that array when set on create or update. It is applied
	// to the array record and then cleared; it is not provider configuration.
	ArrayName string `json:"array_name,omitempty"`
	// ArrayIndependent detaches this provider into its own array. Empty array_id
	// on update otherwise keeps the current array.
	ArrayIndependent bool `json:"array_independent,omitempty"`
	// AuthKind selects how HubCenter obtains upstream credentials.
	// Empty and "api_key" mean a static API key. "workbuddy" means a
	// WorkBuddy or CodeBuddy account login.
	AuthKind string `json:"auth_kind,omitempty"`
	// WorkBuddyEdition is "china" or "global" when AuthKind is workbuddy.
	WorkBuddyEdition string `json:"workbuddy_edition,omitempty"`
	// WorkBuddyRefreshToken renews APIKey. It is persisted and redacted
	// from admin reads, the same way as APIKey.
	WorkBuddyRefreshToken string `json:"workbuddy_refresh_token,omitempty"`
	WorkBuddyExpiresAt    int64  `json:"workbuddy_expires_at,omitempty"`
	WorkBuddyUserID       string `json:"workbuddy_user_id,omitempty"`
	WorkBuddyEnterpriseID string `json:"workbuddy_enterprise_id,omitempty"`
	WorkBuddyDomain       string `json:"workbuddy_domain,omitempty"`
	// WorkBuddySessionID is accepted on create or update and then discarded.
	// It names an in-memory login whose credential and model catalog fill
	// the fields above. It is never stored.
	WorkBuddySessionID string `json:"workbuddy_session_id,omitempty"`
}

// TokenBankAudience is one hub and/or tenant allowed to call a private share.
type TokenBankAudience struct {
	HubID    string `json:"hub_id,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

const (
	// ProviderAuthAPIKey is the explicit static-key choice from the admin form.
	// Stored providers leave AuthKind empty for this case.
	ProviderAuthAPIKey = "api_key"
	// ProviderAuthWorkBuddy is a WorkBuddy domestic or international account.
	ProviderAuthWorkBuddy = "workbuddy"
)

// ProviderArray is one logical provider. A model service group routes to the
// array, not to each member. Members share the array's multiplier and token
// price, are tried round-robin, and fail over to the next member on 429 or 5xx.
type ProviderArray struct {
	ID                       string                   `json:"id"`
	Name                     string                   `json:"name"`
	MemberIDs                []string                 `json:"member_ids"`
	Timezone                 string                   `json:"timezone,omitempty"`
	CreditMultiplier         float64                  `json:"credit_multiplier,omitempty"`
	CreditMultiplierSchedule []CreditMultiplierWindow `json:"credit_multiplier_schedule,omitempty"`
	TokenPricing             TokenPricing             `json:"token_pricing,omitempty"`
	// Manual is set when an operator creates the array itself. An empty manual
	// array is kept so its rate can be set before the first provider joins.
	// A derived array disappears once its last provider leaves.
	Manual bool `json:"manual,omitempty"`
	// System is set on arrays the platform owns, such as the three Token Bank
	// tier arrays. A system array is never empty-dropped and never deletable;
	// it can only gain and lose members.
	System bool `json:"system,omitempty"`
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
	// BillingMultiplier is the user-facing fee coefficient for this capability
	// (auto / official-low / official-mid / official-high). Zero uses the band
	// default: auto 1, low 0.5, mid 1, high 2. It is separate from
	// CreditMultiplier, which remains a dispatch field.
	BillingMultiplier float64 `json:"billing_multiplier,omitempty"`
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
	// TokenPricingOverride explicitly opts this service-group route into its
	// own commercial price. When false, provider/model pricing remains the
	// source of truth and TokenPricing is only a legacy fallback.
	TokenPricingOverride bool `json:"token_pricing_override,omitempty"`
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
	// RequestID is the Hub-generated idempotency/correlation key.  It lets the
	// upstream usage ledger be reconciled to Hub's settled debit without using
	// a lossy time/token heuristic.
	RequestID         string    `json:"request_id,omitempty"`
	ProviderID        string    `json:"provider_id"`
	Model             string    `json:"model"`
	ServiceGroupID    string    `json:"service_group_id,omitempty"`
	WorkloadClass     string    `json:"workload_class,omitempty"`
	ClassSource       string    `json:"class_source,omitempty"`
	Preview           string    `json:"preview,omitempty"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	CachedInputTokens int64     `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64     `json:"cache_write_tokens,omitempty"`
	CacheUsageSource  string    `json:"cache_usage_source,omitempty"`
	UsageAnomaly      string    `json:"usage_anomaly,omitempty"`
	PricingSource     string    `json:"pricing_source,omitempty"`
	Credits           float64   `json:"credits"`
	CacheHit          bool      `json:"cache_hit"`
	AuthID            string    `json:"auth_id,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
}
