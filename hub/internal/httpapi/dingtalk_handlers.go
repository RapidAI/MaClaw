package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/dingtalk"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const dingtalkConfigKey = "dingtalk_config"

type DingTalkConfigState struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func GetDingTalkConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		raw, err := system.Get(r.Context(), dingtalkConfigKey)
		if err != nil || raw == "" {
			writeJSON(w, http.StatusOK, DingTalkConfigState{})
			return
		}
		var cfg DingTalkConfigState
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeJSON(w, http.StatusOK, DingTalkConfigState{})
			return
		}
		if cfg.ClientSecret != "" {
			cfg.ClientSecret = maskSecret(cfg.ClientSecret)
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateDingTalkConfigHandler(system store.SystemSettingsRepository, plugin *dingtalk.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		var cfg DingTalkConfigState
		if err := json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		// Preserve masked secret
		if isMasked(cfg.ClientSecret) {
			old := loadDingTalkConfig(r, system)
			cfg.ClientSecret = old.ClientSecret
		}

		data, _ := json.Marshal(cfg)
		if err := system.Set(r.Context(), dingtalkConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "DINGTALK_CONFIG_SAVE_FAILED", err.Error())
			return
		}

		// Hot-reload only for the Hub-level singleton gateway. Tenant-scoped
		// settings are consumed per tenant and must not rewire the shared process.
		if plugin != nil && shouldReloadSharedRuntimeForRequest(r) {
			_ = plugin.Stop(r.Context())
			if cfg.Enabled {
				_ = plugin.Start(r.Context())
			}
		}

		resp := cfg
		if resp.ClientSecret != "" {
			resp.ClientSecret = maskSecret(resp.ClientSecret)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// GetDingTalkBindingsHandler returns the current staffId→email bindings.
func GetDingTalkBindingsHandler(plugin *dingtalk.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if plugin == nil {
			writeJSON(w, http.StatusOK, map[string]any{"bindings": []any{}})
			return
		}
		tenantID := RequestTenantID(r)
		m := plugin.GetTenantBindings(tenantID)
		type binding struct {
			StaffID  string `json:"staff_id"`
			Email    string `json:"email"`
			TenantID string `json:"tenant_id"`
		}
		bindings := make([]binding, 0, len(m))
		for sid, email := range m {
			bindings = append(bindings, binding{StaffID: sid, Email: email, TenantID: tenantID})
		}
		writeJSON(w, http.StatusOK, map[string]any{"bindings": bindings})
	}
}

// DeleteDingTalkBindingHandler removes a staffId→email binding.
func DeleteDingTalkBindingHandler(plugin *dingtalk.Plugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if plugin == nil {
			writeError(w, http.StatusServiceUnavailable, "DINGTALK_NOT_CONFIGURED", "DingTalk plugin is not configured")
			return
		}
		staffID := r.URL.Query().Get("staff_id")
		if staffID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "staff_id is required")
			return
		}
		plugin.RemoveTenantBinding(staffID, RequestTenantID(r))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func loadDingTalkConfig(r *http.Request, system store.SystemSettingsRepository) DingTalkConfigState {
	raw, err := system.Get(r.Context(), dingtalkConfigKey)
	if err != nil || raw == "" {
		return DingTalkConfigState{}
	}
	var cfg DingTalkConfigState
	_ = json.Unmarshal([]byte(raw), &cfg)
	return cfg
}
