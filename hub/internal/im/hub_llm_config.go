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

// MaskAPIKey returns a masked version of the API key for display.
// Shows first 4 and last 4 characters with **** in between.
func (c *HubLLMConfig) MaskAPIKey() string {
	k := c.APIKey
	if len(k) <= 8 {
		return "********"
	}
	return k[:4] + "****" + k[len(k)-4:]
}
