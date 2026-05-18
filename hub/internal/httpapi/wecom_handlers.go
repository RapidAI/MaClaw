package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/wecom"
)

const wecomConfigKey = "wecom_config"

type WeComConfigState struct {
	Enabled bool   `json:"enabled"`
	BotID   string `json:"bot_id"`
	Secret  string `json:"secret"`
	WSURL   string `json:"ws_url,omitempty"`
}

func GetWeComConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		raw, err := system.Get(r.Context(), wecomConfigKey)
		if err != nil || raw == "" {
			writeJSON(w, http.StatusOK, WeComConfigState{})
			return
		}
		var cfg WeComConfigState
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeJSON(w, http.StatusOK, WeComConfigState{})
			return
		}
		if cfg.Secret != "" {
			cfg.Secret = maskSecret(cfg.Secret)
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateWeComConfigHandler(system store.SystemSettingsRepository, plugin *wecom.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		var cfg WeComConfigState
		if err := json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if isMasked(cfg.Secret) {
			old := loadWeComConfig(r, system)
			cfg.Secret = old.Secret
		}

		data, _ := json.Marshal(cfg)
		if err := system.Set(r.Context(), wecomConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "WECOM_CONFIG_SAVE_FAILED", err.Error())
			return
		}

		// Hot-reload: restart WebSocket gateway
		if plugin != nil {
			_ = plugin.Stop(r.Context())
			if cfg.Enabled {
				_ = plugin.Start(r.Context())
			}
		}

		resp := cfg
		if resp.Secret != "" {
			resp.Secret = maskSecret(resp.Secret)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func GetWeComBindingsHandler(plugin *wecom.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if plugin == nil {
			writeJSON(w, http.StatusOK, map[string]any{"bindings": []any{}})
			return
		}
		m := plugin.GetBindings()
		type binding struct {
			UserID string `json:"userid"`
			Email  string `json:"email"`
		}
		bindings := make([]binding, 0, len(m))
		for uid, email := range m {
			bindings = append(bindings, binding{UserID: uid, Email: email})
		}
		writeJSON(w, http.StatusOK, map[string]any{"bindings": bindings})
	}
}

func DeleteWeComBindingHandler(plugin *wecom.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if plugin == nil {
			writeError(w, http.StatusServiceUnavailable, "WECOM_NOT_CONFIGURED", "WeCom plugin is not configured")
			return
		}
		userID := r.URL.Query().Get("userid")
		if userID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "userid is required")
			return
		}
		plugin.RemoveBinding(userID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func loadWeComConfig(r *http.Request, system store.SystemSettingsRepository) WeComConfigState {
	raw, err := system.Get(r.Context(), wecomConfigKey)
	if err != nil || raw == "" {
		return WeComConfigState{}
	}
	var cfg WeComConfigState
	_ = json.Unmarshal([]byte(raw), &cfg)
	return cfg
}
