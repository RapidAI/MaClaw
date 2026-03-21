package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const hubLLMConfigKey = "hub_llm_config"

// GetHubLLMConfigHandler returns the current Hub LLM configuration with
// the API key masked.
func GetHubLLMConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := system.Get(r.Context(), hubLLMConfigKey)
		if err != nil || raw == "" {
			writeJSON(w, http.StatusOK, &im.HubLLMConfig{})
			return
		}
		var cfg im.HubLLMConfig
		if json.Unmarshal([]byte(raw), &cfg) != nil {
			writeJSON(w, http.StatusOK, &im.HubLLMConfig{})
			return
		}
		cfg.APIKey = cfg.MaskAPIKey()
		writeJSON(w, http.StatusOK, cfg)
	}
}

// UpdateHubLLMConfigHandler saves the Hub LLM configuration.
// If the API key looks masked (unchanged from frontend), the old key is preserved.
func UpdateHubLLMConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg im.HubLLMConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		// Preserve old key if the submitted one is masked.
		if isMasked(cfg.APIKey) || cfg.APIKey == "********" {
			old := loadHubLLMConfig(r, system)
			if old != nil {
				cfg.APIKey = old.APIKey
			}
		}

		data, _ := json.Marshal(cfg)
		if err := system.Set(r.Context(), hubLLMConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "HUB_LLM_CONFIG_SAVE_FAILED", err.Error())
			return
		}

		resp := cfg
		resp.APIKey = resp.MaskAPIKey()
		writeJSON(w, http.StatusOK, resp)
	}
}

// TestHubLLMHandler sends a simple prompt to verify the LLM API is reachable
// and the key is valid. Returns success/failure + latency.
func TestHubLLMHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := loadHubLLMConfig(r, system)
		if cfg == nil || cfg.APIURL == "" || cfg.APIKey == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"error":   "Hub LLM 未配置或缺少 API URL / Key",
			})
			return
		}

		llmCfg := cfg.ToMaclawLLMConfig()
		messages := []interface{}{
			map[string]string{"role": "user", "content": "Reply with exactly: pong"},
		}

		start := time.Now()
		resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, http.DefaultClient, 10*time.Second)
		elapsed := time.Since(start)

		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"success":    false,
				"error":      err.Error(),
				"latency_ms": elapsed.Milliseconds(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success":    true,
			"reply":      resp.Content,
			"latency_ms": elapsed.Milliseconds(),
		})
	}
}

// HubLLMStatusHandler returns the current LLM health status.
// Requires a StatusProvider to be wired (the Coordinator).
func HubLLMStatusHandler(statusFn func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := "not_configured"
		if statusFn != nil {
			status = statusFn()
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
	}
}

// loadHubLLMConfig reads the current Hub LLM config from system_settings.
func loadHubLLMConfig(r *http.Request, system store.SystemSettingsRepository) *im.HubLLMConfig {
	raw, err := system.Get(r.Context(), hubLLMConfigKey)
	if err != nil || raw == "" {
		return nil
	}
	var cfg im.HubLLMConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return nil
	}
	return &cfg
}
