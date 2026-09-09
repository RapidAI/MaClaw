package im

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

type testSystemSettingsRepo struct {
	values map[string]string
}

func (r *testSystemSettingsRepo) Set(_ context.Context, key, valueJSON string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = valueJSON
	return nil
}

func (r *testSystemSettingsRepo) Get(_ context.Context, key string) (string, error) {
	if r.values == nil {
		return "", nil
	}
	return r.values[key], nil
}

func TestLLMProviderRegistryRoundTripNormalizesAgentTypeAndWireAPI(t *testing.T) {
	repo := &testSystemSettingsRepo{}
	ctx := context.Background()
	reg := &LLMProviderRegistry{
		Enabled:                true,
		CurrentProviderID:      "provider-a",
		SmartRouteSingleDevice: true,
		Providers: []LLMProvider{{
			ID:                 "provider-a",
			Name:               "Provider A",
			APIURL:             "https://example.com",
			APIKey:             "secret",
			Model:              "claude-3-7-sonnet",
			Protocol:           "Anthropic",
			WireAPI:            "Responses-WS",
			AgentType:          "  claude-code/2.0.0  ",
			MaxConcurrency:     -3,
			MaxQueueWaiters:    -2,
			QueueTimeoutMS:     -100,
			UpstreamTimeoutSec: -100,
		}},
	}
	if err := SaveLLMProviderRegistry(ctx, repo, reg); err != nil {
		t.Fatalf("SaveLLMProviderRegistry() error = %v", err)
	}
	loaded, err := LoadLLMProviderRegistry(ctx, repo)
	if err != nil {
		t.Fatalf("LoadLLMProviderRegistry() error = %v", err)
	}
	if len(loaded.Providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(loaded.Providers))
	}
	provider := loaded.Providers[0]
	if provider.Protocol != "anthropic" {
		t.Fatalf("protocol = %q, want anthropic", provider.Protocol)
	}
	if provider.WireAPI != "responses-ws" {
		t.Fatalf("wire_api = %q, want responses-ws", provider.WireAPI)
	}
	if provider.AgentType != "claude-code/2.0.0" {
		t.Fatalf("agent_type = %q, want claude-code/2.0.0", provider.AgentType)
	}
	if provider.MaxConcurrency != 0 {
		t.Fatalf("max_concurrency = %d, want 0", provider.MaxConcurrency)
	}
	if provider.MaxQueueWaiters != 0 {
		t.Fatalf("max_queue_waiters = %d, want 0", provider.MaxQueueWaiters)
	}
	if provider.QueueTimeoutMS != 0 {
		t.Fatalf("queue_timeout_ms = %d, want 0", provider.QueueTimeoutMS)
	}
	if provider.UpstreamTimeoutSec != DefaultLLMProviderUpstreamTimeoutSec {
		t.Fatalf("upstream_timeout_sec = %d, want %d", provider.UpstreamTimeoutSec, DefaultLLMProviderUpstreamTimeoutSec)
	}
	if provider.InputPricePerMTokensRMB != 0 {
		t.Fatalf("input price = %.4f, want 0", provider.InputPricePerMTokensRMB)
	}
	if provider.OutputPricePerMTokensRMB != 0 {
		t.Fatalf("output price = %.4f, want 0", provider.OutputPricePerMTokensRMB)
	}
	cfg := loaded.ToHubLLMConfig()
	if cfg == nil {
		t.Fatal("ToHubLLMConfig() returned nil")
	}
	if cfg.AgentType != "claude-code/2.0.0" {
		t.Fatalf("cfg.AgentType = %q, want claude-code/2.0.0", cfg.AgentType)
	}
	if cfg.WireAPI != "responses-ws" {
		t.Fatalf("cfg.WireAPI = %q, want responses-ws", cfg.WireAPI)
	}
	if cfg.Protocol != "anthropic" {
		t.Fatalf("cfg.Protocol = %q, want anthropic", cfg.Protocol)
	}
}

func TestLLMProviderRegistryRoundTripPreservesCachePricing(t *testing.T) {
	repo := &testSystemSettingsRepo{}
	pricing := llmpool.TokenPricing{InputCreditsPer10K: 4, OutputCreditsPer10K: 8, CacheReadCreditsPer10K: cachePrice(0.4), CacheWriteCreditsPer10K: cachePrice(4), InputRMBPer10K: 0.02, OutputRMBPer10K: 0.08, CacheReadRMBPer10K: cachePrice(0.002), CacheWriteRMBPer10K: cachePrice(0.02)}
	if err := SaveLLMProviderRegistry(context.Background(), repo, &LLMProviderRegistry{Providers: []LLMProvider{{ID: "p", TokenPricing: pricing}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadLLMProviderRegistry(context.Background(), repo)
	if err != nil || len(loaded.Providers) != 1 {
		t.Fatalf("load: err=%v providers=%d", err, len(loaded.Providers))
	}
	got := loaded.Providers[0].TokenPricing
	if llmpool.OptionalTokenPriceValue(got.CacheReadCreditsPer10K) != 0.4 || llmpool.OptionalTokenPriceValue(got.CacheWriteCreditsPer10K) != 4 ||
		llmpool.OptionalTokenPriceValue(got.CacheReadRMBPer10K) != 0.002 || llmpool.OptionalTokenPriceValue(got.CacheWriteRMBPer10K) != 0.02 {
		t.Fatalf("cache pricing lost: got=%+v want=%+v", got, pricing)
	}
}

func cachePrice(value float64) *float64 { return &value }

// TestLLMProviderRegistryKeepsCachePricingPresence pins the presence contract:
// an unset cache price must remain unset through a save/load round trip (the
// documented defaults are derived only at billing resolution time), while an
// explicitly configured zero must survive as an explicit zero.
func TestLLMProviderRegistryKeepsCachePricingPresence(t *testing.T) {
	repo := &testSystemSettingsRepo{}
	if err := SaveLLMProviderRegistry(context.Background(), repo, &LLMProviderRegistry{Providers: []LLMProvider{{ID: "p", TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 4, OutputCreditsPer10K: 8}}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadLLMProviderRegistry(context.Background(), repo)
	if err != nil || len(loaded.Providers) != 1 {
		t.Fatalf("load: err=%v providers=%d", err, len(loaded.Providers))
	}
	p := loaded.Providers[0].TokenPricing
	if p.CacheReadCreditsPer10K != nil || p.CacheWriteCreditsPer10K != nil || p.CacheReadRMBPer10K != nil || p.CacheWriteRMBPer10K != nil {
		t.Fatalf("unset cache prices were materialized into storage: %+v", p)
	}
	// The billing path still sees the documented defaults for unset fields.
	resolved := p.WithCachePricingDefaults()
	if llmpool.OptionalTokenPriceValue(resolved.CacheReadCreditsPer10K) != 0.4 || llmpool.OptionalTokenPriceValue(resolved.CacheWriteCreditsPer10K) != 4 {
		t.Fatalf("resolution-time defaults missing: %+v", resolved)
	}

	explicitZero := llmpool.TokenPricing{InputCreditsPer10K: 4, OutputCreditsPer10K: 8, CacheReadCreditsPer10K: cachePrice(0), CacheWriteRMBPer10K: cachePrice(0)}
	if err := SaveLLMProviderRegistry(context.Background(), repo, &LLMProviderRegistry{Providers: []LLMProvider{{ID: "p", TokenPricing: explicitZero}}}); err != nil {
		t.Fatalf("save explicit zero: %v", err)
	}
	loaded, err = LoadLLMProviderRegistry(context.Background(), repo)
	if err != nil || len(loaded.Providers) != 1 {
		t.Fatalf("load explicit zero: err=%v providers=%d", err, len(loaded.Providers))
	}
	p = loaded.Providers[0].TokenPricing
	if p.CacheReadCreditsPer10K == nil || *p.CacheReadCreditsPer10K != 0 || p.CacheWriteRMBPer10K == nil || *p.CacheWriteRMBPer10K != 0 {
		t.Fatalf("explicit zero cache pricing was rewritten: %+v", p)
	}
	if p.CacheWriteCreditsPer10K != nil || p.CacheReadRMBPer10K != nil {
		t.Fatalf("unrelated unset cache prices were materialized: %+v", p)
	}
	resolved = p.WithCachePricingDefaults()
	if got := llmpool.OptionalTokenPriceValue(resolved.CacheReadCreditsPer10K); got != 0 {
		t.Fatalf("explicit zero cache read overwritten by default: %v", got)
	}
	if got := llmpool.OptionalTokenPriceValue(resolved.CacheWriteCreditsPer10K); got != 4 {
		t.Fatalf("unset cache write did not fall back to input price: %v", got)
	}
}

func TestLLMProviderRegistryDefaultsDownstreamMaxConcurrency(t *testing.T) {
	repo := &testSystemSettingsRepo{}
	ctx := context.Background()
	reg := &LLMProviderRegistry{}
	if err := SaveLLMProviderRegistry(ctx, repo, reg); err != nil {
		t.Fatalf("SaveLLMProviderRegistry() error = %v", err)
	}
	loaded, err := LoadLLMProviderRegistry(ctx, repo)
	if err != nil {
		t.Fatalf("LoadLLMProviderRegistry() error = %v", err)
	}
	if loaded.DownstreamMaxConcurrency != DefaultLLMProviderDownstreamMaxConcurrency {
		t.Fatalf("downstream_max_concurrency = %d, want %d", loaded.DownstreamMaxConcurrency, DefaultLLMProviderDownstreamMaxConcurrency)
	}

	reg.DownstreamMaxConcurrency = -5
	if err := SaveLLMProviderRegistry(ctx, repo, reg); err != nil {
		t.Fatalf("SaveLLMProviderRegistry() with negative value error = %v", err)
	}
	loaded, err = LoadLLMProviderRegistry(ctx, repo)
	if err != nil {
		t.Fatalf("LoadLLMProviderRegistry() after negative value error = %v", err)
	}
	if loaded.DownstreamMaxConcurrency != DefaultLLMProviderDownstreamMaxConcurrency {
		t.Fatalf("normalized downstream_max_concurrency = %d, want %d", loaded.DownstreamMaxConcurrency, DefaultLLMProviderDownstreamMaxConcurrency)
	}
}

func TestLLMProviderRegistryDefaultsUserLimitsAndResilience(t *testing.T) {
	repo := &testSystemSettingsRepo{}
	ctx := context.Background()
	reg := &LLMProviderRegistry{
		Providers: []LLMProvider{{
			ID:                       "provider-a",
			Name:                     "Provider A",
			APIURL:                   "https://example.com",
			Model:                    "gpt-4.1",
			CircuitBreakerThreshold:  -1,
			CircuitBreakerCooldownMS: -2,
			FailureBackoffBaseMS:     -3,
			FailureBackoffMaxMS:      -4,
			UpstreamTimeoutSec:       -5,
		}},
	}
	if err := SaveLLMProviderRegistry(ctx, repo, reg); err != nil {
		t.Fatalf("SaveLLMProviderRegistry() error = %v", err)
	}
	loaded, err := LoadLLMProviderRegistry(ctx, repo)
	if err != nil {
		t.Fatalf("LoadLLMProviderRegistry() error = %v", err)
	}
	if loaded.UserRateLimitPerMinute != DefaultLLMProviderUserRateLimitPerMinute {
		t.Fatalf("user_rate_limit_per_minute = %d, want %d", loaded.UserRateLimitPerMinute, DefaultLLMProviderUserRateLimitPerMinute)
	}
	if loaded.UserRateLimitBurst != DefaultLLMProviderUserRateLimitBurst {
		t.Fatalf("user_rate_limit_burst = %d, want %d", loaded.UserRateLimitBurst, DefaultLLMProviderUserRateLimitBurst)
	}
	if loaded.UserRateLimitMaxWaitMS != DefaultLLMProviderUserRateLimitMaxWaitMS {
		t.Fatalf("user_rate_limit_max_wait_ms = %d, want %d", loaded.UserRateLimitMaxWaitMS, DefaultLLMProviderUserRateLimitMaxWaitMS)
	}
	// Oversized max_wait must be clamped on normalize/save.
	reg.UserRateLimitMaxWaitMS = MaxLLMProviderUserRateLimitMaxWaitMS + 5000
	if err := SaveLLMProviderRegistry(ctx, repo, reg); err != nil {
		t.Fatalf("SaveLLMProviderRegistry() oversized wait error = %v", err)
	}
	loaded, err = LoadLLMProviderRegistry(ctx, repo)
	if err != nil {
		t.Fatalf("LoadLLMProviderRegistry() after oversized wait error = %v", err)
	}
	if loaded.UserRateLimitMaxWaitMS != MaxLLMProviderUserRateLimitMaxWaitMS {
		t.Fatalf("clamped user_rate_limit_max_wait_ms = %d, want %d", loaded.UserRateLimitMaxWaitMS, MaxLLMProviderUserRateLimitMaxWaitMS)
	}
	provider := loaded.Providers[0]
	if provider.UpstreamTimeoutSec != DefaultLLMProviderUpstreamTimeoutSec {
		t.Fatalf("upstream_timeout_sec = %d, want %d", provider.UpstreamTimeoutSec, DefaultLLMProviderUpstreamTimeoutSec)
	}
	if provider.CircuitBreakerThreshold != DefaultLLMProviderCircuitBreakerThreshold {
		t.Fatalf("circuit_breaker_threshold = %d, want %d", provider.CircuitBreakerThreshold, DefaultLLMProviderCircuitBreakerThreshold)
	}
	if provider.CircuitBreakerCooldownMS != DefaultLLMProviderCircuitBreakerCooldownMS {
		t.Fatalf("circuit_breaker_cooldown_ms = %d, want %d", provider.CircuitBreakerCooldownMS, DefaultLLMProviderCircuitBreakerCooldownMS)
	}
	if provider.FailureBackoffBaseMS != DefaultLLMProviderFailureBackoffBaseMS {
		t.Fatalf("failure_backoff_base_ms = %d, want %d", provider.FailureBackoffBaseMS, DefaultLLMProviderFailureBackoffBaseMS)
	}
	if provider.FailureBackoffMaxMS != DefaultLLMProviderFailureBackoffMaxMS {
		t.Fatalf("failure_backoff_max_ms = %d, want %d", provider.FailureBackoffMaxMS, DefaultLLMProviderFailureBackoffMaxMS)
	}
}

func TestLLMProviderRegistryNormalizesVendorBillingSchedule(t *testing.T) {
	repo := &testSystemSettingsRepo{}
	ctx := context.Background()
	reg := &LLMProviderRegistry{
		Providers: []LLMProvider{{
			ID:     "deepseek",
			Name:   "DeepSeek",
			APIURL: "https://api.deepseek.com",
			Model:  "deepseek-chat",
			CreditMultiplierSchedule: []llmpool.CreditMultiplierWindow{{
				Days:       []int{1, 2, 3, 4, 5},
				Start:      "0:30",
				End:        "8:30",
				Multiplier: 0.5,
			}},
		}},
	}
	if err := SaveLLMProviderRegistry(ctx, repo, reg); err != nil {
		t.Fatalf("SaveLLMProviderRegistry() error = %v", err)
	}
	loaded, err := LoadLLMProviderRegistry(ctx, repo)
	if err != nil {
		t.Fatalf("LoadLLMProviderRegistry() error = %v", err)
	}
	provider := loaded.Providers[0]
	if provider.Timezone != llmpool.DefaultCreditMultiplierTimezone {
		t.Fatalf("timezone = %q, want %q", provider.Timezone, llmpool.DefaultCreditMultiplierTimezone)
	}
	if provider.CreditMultiplier != 1 {
		t.Fatalf("credit_multiplier = %v, want 1", provider.CreditMultiplier)
	}
	if len(provider.CreditMultiplierSchedule) != 1 || provider.CreditMultiplierSchedule[0].Start != "00:30" {
		t.Fatalf("schedule = %#v", provider.CreditMultiplierSchedule)
	}
}
