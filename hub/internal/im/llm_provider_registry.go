package im

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const LLMProviderRegistryKey = "llm_provider_registry"
const DefaultLLMProviderDownstreamMaxConcurrency = 500
const DefaultLLMProviderUserRateLimitPerMinute = 300
const DefaultLLMProviderUserRateLimitBurst = 80
const DefaultLLMProviderUserRateLimitMaxWaitMS = 30000

// MaxLLMProviderUserRateLimitMaxWaitMS caps how long Hub will block a request
// waiting for a user rate-limit token (guards misconfiguration).
const MaxLLMProviderUserRateLimitMaxWaitMS = 120000
const DefaultLLMProviderCircuitBreakerThreshold = 3
const DefaultLLMProviderCircuitBreakerCooldownMS = 30000
const DefaultLLMProviderFailureBackoffBaseMS = 500
const DefaultLLMProviderFailureBackoffMaxMS = 10000
const DefaultLLMProviderUpstreamTimeoutSec = 900
const DefaultLLMProviderInputPricePerMTokensRMB = corelib.DefaultLLMInputPricePerMTokensRMB
const DefaultLLMProviderOutputPricePerMTokensRMB = corelib.DefaultLLMOutputPricePerMTokensRMB

type LLMProvider struct {
	ID                       string                           `json:"id"`
	Name                     string                           `json:"name"`
	APIURL                   string                           `json:"api_url"`
	APIKey                   string                           `json:"api_key"`
	Model                    string                           `json:"model"`
	Protocol                 string                           `json:"protocol,omitempty"`
	WireAPI                  string                           `json:"wire_api,omitempty"`
	AgentType                string                           `json:"agent_type,omitempty"`
	MaxConcurrency           int                              `json:"max_concurrency,omitempty"`
	MaxQueueWaiters          int                              `json:"max_queue_waiters,omitempty"`
	QueueTimeoutMS           int                              `json:"queue_timeout_ms,omitempty"`
	UpstreamTimeoutSec       int                              `json:"upstream_timeout_sec,omitempty"`
	CircuitBreakerThreshold  int                              `json:"circuit_breaker_threshold,omitempty"`
	CircuitBreakerCooldownMS int                              `json:"circuit_breaker_cooldown_ms,omitempty"`
	FailureBackoffBaseMS     int                              `json:"failure_backoff_base_ms,omitempty"`
	FailureBackoffMaxMS      int                              `json:"failure_backoff_max_ms,omitempty"`
	InputPricePerMTokensRMB  float64                          `json:"input_price_per_m_tokens_rmb,omitempty"`
	OutputPricePerMTokensRMB float64                          `json:"output_price_per_m_tokens_rmb,omitempty"`
	// TokenPricing is the directional Credits price (per 10k tokens) used as
	// the default for service-group routes that do not price themselves.
	TokenPricing             llmpool.TokenPricing             `json:"token_pricing,omitempty"`
	Timezone                 string                           `json:"timezone,omitempty"`
	CreditMultiplier         float64                          `json:"credit_multiplier,omitempty"`
	CreditMultiplierSchedule []llmpool.CreditMultiplierWindow `json:"credit_multiplier_schedule,omitempty"`
}

type LLMProviderRegistry struct {
	Enabled                  bool   `json:"enabled"`
	CurrentProviderID        string `json:"current_provider_id"`
	SmartRouteSingleDevice   bool   `json:"smart_route_single_device"`
	DownstreamMaxConcurrency int    `json:"downstream_max_concurrency,omitempty"`
	UpstreamTimeoutSec       int    `json:"upstream_timeout_sec,omitempty"`
	UserRateLimitPerMinute   int    `json:"user_rate_limit_per_minute,omitempty"`
	UserRateLimitBurst       int    `json:"user_rate_limit_burst,omitempty"`
	// UserRateLimitMaxWaitMS is how long a request may wait for a user rate-limit
	// token before Hub returns 429. 0 means use the default (30s).
	UserRateLimitMaxWaitMS int                                `json:"user_rate_limit_max_wait_ms,omitempty"`
	Providers              []LLMProvider                      `json:"providers"`
	TokenUsage             map[string]*corelib.TokenUsageStat `json:"token_usage,omitempty"`
}

func normalizeLLMProviderRegistry(reg *LLMProviderRegistry) *LLMProviderRegistry {
	if reg == nil {
		reg = &LLMProviderRegistry{}
	}
	if reg.DownstreamMaxConcurrency <= 0 {
		reg.DownstreamMaxConcurrency = DefaultLLMProviderDownstreamMaxConcurrency
	}
	if reg.UpstreamTimeoutSec <= 0 {
		reg.UpstreamTimeoutSec = DefaultLLMProviderUpstreamTimeoutSec
	}
	if reg.UserRateLimitPerMinute <= 0 {
		reg.UserRateLimitPerMinute = DefaultLLMProviderUserRateLimitPerMinute
	}
	if reg.UserRateLimitBurst <= 0 {
		reg.UserRateLimitBurst = DefaultLLMProviderUserRateLimitBurst
	}
	if reg.UserRateLimitMaxWaitMS <= 0 {
		reg.UserRateLimitMaxWaitMS = DefaultLLMProviderUserRateLimitMaxWaitMS
	}
	if reg.UserRateLimitMaxWaitMS > MaxLLMProviderUserRateLimitMaxWaitMS {
		reg.UserRateLimitMaxWaitMS = MaxLLMProviderUserRateLimitMaxWaitMS
	}
	if reg.TokenUsage == nil {
		reg.TokenUsage = map[string]*corelib.TokenUsageStat{}
	}
	for i := range reg.Providers {
		reg.Providers[i].Protocol = normalizeStoredProviderProtocol(reg.Providers[i].Protocol)
		reg.Providers[i].WireAPI = normalizeStoredProviderWireAPI(reg.Providers[i].WireAPI)
		reg.Providers[i].AgentType = normalizeStoredProviderAgentType(reg.Providers[i].AgentType)
		if reg.Providers[i].MaxConcurrency < 0 {
			reg.Providers[i].MaxConcurrency = 0
		}
		if reg.Providers[i].MaxQueueWaiters < 0 {
			reg.Providers[i].MaxQueueWaiters = 0
		}
		if reg.Providers[i].QueueTimeoutMS < 0 {
			reg.Providers[i].QueueTimeoutMS = 0
		}
		if reg.Providers[i].UpstreamTimeoutSec <= 0 {
			reg.Providers[i].UpstreamTimeoutSec = DefaultLLMProviderUpstreamTimeoutSec
		}
		if reg.Providers[i].UpstreamTimeoutSec < 300 {
			reg.Providers[i].UpstreamTimeoutSec = 300
		}
		if reg.Providers[i].CircuitBreakerThreshold <= 0 {
			reg.Providers[i].CircuitBreakerThreshold = DefaultLLMProviderCircuitBreakerThreshold
		}
		if reg.Providers[i].CircuitBreakerCooldownMS <= 0 {
			reg.Providers[i].CircuitBreakerCooldownMS = DefaultLLMProviderCircuitBreakerCooldownMS
		}
		if reg.Providers[i].FailureBackoffBaseMS <= 0 {
			reg.Providers[i].FailureBackoffBaseMS = DefaultLLMProviderFailureBackoffBaseMS
		}
		if reg.Providers[i].FailureBackoffMaxMS <= 0 {
			reg.Providers[i].FailureBackoffMaxMS = DefaultLLMProviderFailureBackoffMaxMS
		}
		if reg.Providers[i].FailureBackoffMaxMS < reg.Providers[i].FailureBackoffBaseMS {
			reg.Providers[i].FailureBackoffMaxMS = reg.Providers[i].FailureBackoffBaseMS
		}
		reg.Providers[i].InputPricePerMTokensRMB = corelib.NormalizeLLMTokenPricePerMTokensRMB(reg.Providers[i].InputPricePerMTokensRMB, DefaultLLMProviderInputPricePerMTokensRMB)
		reg.Providers[i].OutputPricePerMTokensRMB = corelib.NormalizeLLMTokenPricePerMTokensRMB(reg.Providers[i].OutputPricePerMTokensRMB, DefaultLLMProviderOutputPricePerMTokensRMB)
		if llmpool.ValidateRouteBilling(llmpool.BillingModeFree, reg.Providers[i].TokenPricing) != nil {
			// Drop invalid pricing shapes (negative/NaN or malformed schedule)
			// so a bad save can never poison billing resolution.
			reg.Providers[i].TokenPricing = llmpool.TokenPricing{}
		}
		policy := llmpool.NormalizeProviderBillingPolicy(llmpool.ProviderBillingPolicy{
			ProviderID:               reg.Providers[i].ID,
			Timezone:                 reg.Providers[i].Timezone,
			CreditMultiplier:         reg.Providers[i].CreditMultiplier,
			CreditMultiplierSchedule: reg.Providers[i].CreditMultiplierSchedule,
		})
		reg.Providers[i].Timezone = policy.Timezone
		reg.Providers[i].CreditMultiplier = policy.CreditMultiplier
		reg.Providers[i].CreditMultiplierSchedule = policy.CreditMultiplierSchedule
	}
	return reg
}

func (p LLMProvider) BillingPolicy() llmpool.ProviderBillingPolicy {
	return llmpool.ProviderBillingPolicy{
		ProviderID:               strings.TrimSpace(p.ID),
		Timezone:                 p.Timezone,
		CreditMultiplier:         p.CreditMultiplier,
		CreditMultiplierSchedule: append([]llmpool.CreditMultiplierWindow(nil), p.CreditMultiplierSchedule...),
	}
}

func LoadLLMProviderRegistry(ctx context.Context, system store.SystemSettingsRepository) (*LLMProviderRegistry, error) {
	raw, err := system.Get(ctx, LLMProviderRegistryKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return normalizeLLMProviderRegistry(&LLMProviderRegistry{}), nil
	}
	var reg LLMProviderRegistry
	if err := json.Unmarshal([]byte(raw), &reg); err != nil {
		return nil, err
	}
	return normalizeLLMProviderRegistry(&reg), nil
}

func SaveLLMProviderRegistry(ctx context.Context, system store.SystemSettingsRepository, reg *LLMProviderRegistry) error {
	reg = normalizeLLMProviderRegistry(reg)
	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	return system.Set(ctx, LLMProviderRegistryKey, string(data))
}

func (r *LLMProviderRegistry) FindProvider(id string) *LLMProvider {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for i := range r.Providers {
		if strings.EqualFold(strings.TrimSpace(r.Providers[i].ID), id) {
			return &r.Providers[i]
		}
	}
	return nil
}

func (r *LLMProviderRegistry) CurrentProvider() *LLMProvider {
	if r == nil {
		return nil
	}
	if p := r.FindProvider(r.CurrentProviderID); p != nil {
		return p
	}
	if len(r.Providers) > 0 {
		return &r.Providers[0]
	}
	return nil
}

func (r *LLMProviderRegistry) ToHubLLMConfig() *HubLLMConfig {
	if r == nil {
		return nil
	}
	p := r.CurrentProvider()
	if p == nil {
		return nil
	}
	return &HubLLMConfig{
		Enabled:                r.Enabled,
		APIURL:                 p.APIURL,
		APIKey:                 p.APIKey,
		Model:                  p.Model,
		Protocol:               normalizeStoredProviderProtocol(p.Protocol),
		WireAPI:                normalizeStoredProviderWireAPI(p.WireAPI),
		AgentType:              normalizeStoredProviderAgentType(p.AgentType),
		SmartRouteSingleDevice: r.SmartRouteSingleDevice,
	}
}

func normalizeStoredProviderProtocol(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "anthropic") {
		return "anthropic"
	}
	return "openai"
}

func normalizeStoredProviderWireAPI(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "responses", "responses-ws":
		return v
	default:
		return "chat"
	}
}

func normalizeStoredProviderAgentType(v string) string {
	return strings.TrimSpace(v)
}
