package im

import "github.com/RapidAI/CodeClaw/corelib"

// HubLLMConfig is the Hub-side LLM configuration stored in system_settings.
type HubLLMConfig struct {
	Enabled                bool   `json:"enabled"`
	APIURL                 string `json:"api_url"`
	APIKey                 string `json:"api_key"`
	Model                  string `json:"model"`
	Protocol               string `json:"protocol"`                  // "openai" (default) or "anthropic"
	SmartRouteSingleDevice bool   `json:"smart_route_single_device"` // default false
	MaxConcurrent          int    `json:"max_concurrent"`            // max concurrent LLM calls; 0 = default (5)
}

// DefaultMaxConcurrent is the default limit for concurrent LLM API calls.
const DefaultMaxConcurrent = 5

// EffectiveMaxConcurrent returns the configured max concurrent value,
// falling back to DefaultMaxConcurrent when unset or invalid.
func (c *HubLLMConfig) EffectiveMaxConcurrent() int {
	if c.MaxConcurrent > 0 {
		return c.MaxConcurrent
	}
	return DefaultMaxConcurrent
}

// ToMaclawLLMConfig converts to the corelib LLM config format used by
// agent.DoSimpleLLMRequest.
func (c *HubLLMConfig) ToMaclawLLMConfig() corelib.MaclawLLMConfig {
	proto := c.Protocol
	if proto == "" {
		proto = "openai"
	}
	return corelib.MaclawLLMConfig{
		URL:      c.APIURL,
		Key:      c.APIKey,
		Model:    c.Model,
		Protocol: proto,
	}
}


