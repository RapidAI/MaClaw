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
			ID:             "provider-a",
			Name:           "Provider A",
			APIURL:         "https://example.com",
			APIKey:         "secret",
			Model:          "claude-3-7-sonnet",
			Protocol:       "Anthropic",
			WireAPI:        "Responses-WS",
			AgentType:      "  claude-code/2.0.0  ",
			MaxConcurrency: -3,
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
