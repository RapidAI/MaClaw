package im

import (
	"context"
	"testing"
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
