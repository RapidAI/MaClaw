package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const qqbotConfigKey = "qqbot_config"

type QQBotConfigState struct {
	Enabled   bool   `json:"enabled"`
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

func GetQQBotConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := system.Get(r.Context(), qqbotConfigKey)
		if err != nil || raw == "" {
			writeJSON(w, http.StatusOK, QQBotConfigState{})
			return
		}
		var cfg QQBotConfigState
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeJSON(w, http.StatusOK, QQBotConfigState{})
			return
		}
		if cfg.AppSecret != "" {
			cfg.AppSecret = maskSecret(cfg.AppSecret)
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateQQBotConfigHandler(system store.SystemSettingsRepository, plugin *qqbot.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg QQBotConfigState
		if err := json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		// Preserve masked secret
		if isMasked(cfg.AppSecret) {
			old := loadQQBotConfig(r, system)
			cfg.AppSecret = old.AppSecret
		}

		data, _ := json.Marshal(cfg)
		if err := system.Set(r.Context(), qqbotConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "QQBOT_CONFIG_SAVE_FAILED", err.Error())
			return
		}

		// Hot-reload: restart WebSocket gateway if plugin is available
		if plugin != nil {
			_ = plugin.Stop(r.Context())
			if cfg.Enabled {
				_ = plugin.Start(r.Context())
			}
		}

		resp := cfg
		if resp.AppSecret != "" {
			resp.AppSecret = maskSecret(resp.AppSecret)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// GetQQBotBindingsHandler returns the current openid→email bindings.
func GetQQBotBindingsHandler(plugin *qqbot.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if plugin == nil {
			writeJSON(w, http.StatusOK, map[string]any{"bindings": []any{}})
			return
		}
		m := plugin.GetBindings()
		type binding struct {
			OpenID string `json:"open_id"`
			Email  string `json:"email"`
		}
		bindings := make([]binding, 0, len(m))
		for oid, email := range m {
			bindings = append(bindings, binding{OpenID: oid, Email: email})
		}
		writeJSON(w, http.StatusOK, map[string]any{"bindings": bindings})
	}
}

// DeleteQQBotBindingHandler removes an openid→email binding.
func DeleteQQBotBindingHandler(plugin *qqbot.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if plugin == nil {
			writeError(w, http.StatusServiceUnavailable, "QQBOT_NOT_CONFIGURED", "QQBot plugin is not configured")
			return
		}
		openID := r.URL.Query().Get("open_id")
		if openID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "open_id is required")
			return
		}
		plugin.RemoveBinding(openID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// QQBotWebhookHandler handles incoming QQ Bot webhook events.
// This is the alternative to WebSocket for servers with public IP.
func QQBotWebhookHandler(plugin *qqbot.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if plugin == nil {
			http.Error(w, "qqbot not configured", http.StatusServiceUnavailable)
			return
		}
		body, status := plugin.HandleWebhook(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(body)
	}
}

func loadQQBotConfig(r *http.Request, system store.SystemSettingsRepository) QQBotConfigState {
	raw, err := system.Get(r.Context(), qqbotConfigKey)
	if err != nil || raw == "" {
		return QQBotConfigState{}
	}
	var cfg QQBotConfigState
	_ = json.Unmarshal([]byte(raw), &cfg)
	return cfg
}
