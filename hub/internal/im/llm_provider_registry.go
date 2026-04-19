package im

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const LLMProviderRegistryKey = "llm_provider_registry"

type LLMProvider struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	APIURL         string `json:"api_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	Protocol       string `json:"protocol,omitempty"`
	WireAPI        string `json:"wire_api,omitempty"`
	AgentType      string `json:"agent_type,omitempty"`
	MaxConcurrency int    `json:"max_concurrency,omitempty"`
}

type LLMProviderRegistry struct {
	Enabled                bool                               `json:"enabled"`
	CurrentProviderID      string                             `json:"current_provider_id"`
	SmartRouteSingleDevice bool                               `json:"smart_route_single_device"`
	Providers              []LLMProvider                      `json:"providers"`
	TokenUsage             map[string]*corelib.TokenUsageStat `json:"token_usage,omitempty"`
}

func LoadLLMProviderRegistry(ctx context.Context, system store.SystemSettingsRepository) (*LLMProviderRegistry, error) {
	raw, err := system.Get(ctx, LLMProviderRegistryKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return &LLMProviderRegistry{}, nil
	}
	var reg LLMProviderRegistry
	if err := json.Unmarshal([]byte(raw), &reg); err != nil {
		return nil, err
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
	}
	return &reg, nil
}

func SaveLLMProviderRegistry(ctx context.Context, system store.SystemSettingsRepository, reg *LLMProviderRegistry) error {
	if reg == nil {
		reg = &LLMProviderRegistry{}
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
	}
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
