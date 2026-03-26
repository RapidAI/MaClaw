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
	MaxConcurrentGUI       int    `json:"max_concurrent_gui"`        // max concurrent LLM calls from GUI; 0 = default (3)
	MaxConcurrentIM        int    `json:"max_concurrent_im"`         // max concurrent LLM calls from IM; 0 = default (3)
}

// DefaultMaxConcurrent is the default limit for concurrent LLM API calls
// (used as the global/fallback limit).
const DefaultMaxConcurrent = 5

// DefaultMaxConcurrentGUI is the default limit for GUI-originated LLM calls.
const DefaultMaxConcurrentGUI = 3

// DefaultMaxConcurrentIM is the default limit for IM-originated LLM calls.
const DefaultMaxConcurrentIM = 3

// EffectiveMaxConcurrent returns the configured max concurrent value,
// falling back to DefaultMaxConcurrent when unset or invalid.
func (c *HubLLMConfig) EffectiveMaxConcurrent() int {
	if c.MaxConcurrent > 0 {
		return c.MaxConcurrent
	}
	return DefaultMaxConcurrent
}

// EffectiveMaxConcurrentGUI returns the GUI-specific concurrency limit.
func (c *HubLLMConfig) EffectiveMaxConcurrentGUI() int {
	if c.MaxConcurrentGUI > 0 {
		return c.MaxConcurrentGUI
	}
	return DefaultMaxConcurrentGUI
}

// EffectiveMaxConcurrentIM returns the IM-specific concurrency limit.
func (c *HubLLMConfig) EffectiveMaxConcurrentIM() int {
	if c.MaxConcurrentIM > 0 {
		return c.MaxConcurrentIM
	}
	return DefaultMaxConcurrentIM
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


