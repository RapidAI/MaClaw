package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const feishuConfigKey = "feishu_config"

type FeishuConfigState struct {
	Enabled   bool   `json:"enabled"`
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

func GetFeishuConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		raw, err := system.Get(r.Context(), feishuConfigKey)
		if err != nil || raw == "" {
			writeJSON(w, http.StatusOK, FeishuConfigState{})
			return
		}
		var cfg FeishuConfigState
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeJSON(w, http.StatusOK, FeishuConfigState{})
			return
		}
		// Mask the secret for display.
		if cfg.AppSecret != "" {
			cfg.AppSecret = maskSecret(cfg.AppSecret)
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateFeishuConfigHandler(system store.SystemSettingsRepository, notifier *feishu.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		var cfg FeishuConfigState
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		// If the secret is masked (unchanged from frontend), preserve the old one.
		if isMasked(cfg.AppSecret) {
			old := loadFeishuConfig(r, system)
			cfg.AppSecret = old.AppSecret
		}

		data, _ := json.Marshal(cfg)
		if err := system.Set(r.Context(), feishuConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "FEISHU_CONFIG_SAVE_FAILED", err.Error())
			return
		}

		// Hot-reload: reconfigure the notifier so the new credentials take
		// effect immediately without restarting the hub.
		if notifier != nil {
			if cfg.Enabled {
				notifier.Reconfigure(cfg.AppID, cfg.AppSecret)
			} else {
				notifier.Reconfigure("", "")
			}
		}

		// Return masked version.
		resp := cfg
		if resp.AppSecret != "" {
			resp.AppSecret = maskSecret(resp.AppSecret)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// GetFeishuBindingsHandler returns the current email→open_id bindings with
// server-side pagination and optional search (email or mobile substring).
//
// Query params:
//   - page      (int, default 1)
//   - page_size (int, default 50, max 100)
//   - search    (string, optional email/mobile substring filter)
func GetFeishuBindingsHandler(notifier *feishu.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if notifier == nil {
			writeJSON(w, http.StatusOK, map[string]any{"bindings": []any{}, "total": 0, "page": 1, "page_size": 50})
			return
		}
		m := notifier.GetBindingsMap()

		type binding struct {
			Email  string `json:"email"`
			OpenID string `json:"open_id"`
			Mobile string `json:"mobile"`
		}

		// Collect and optionally filter by search.
		search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
		initCap := len(m)
		if search != "" {
			initCap = 0 // avoid over-allocation when filtering
		}
		all := make([]binding, 0, initCap)
		for email, info := range m {
			if search != "" &&
				!strings.Contains(strings.ToLower(email), search) &&
				!strings.Contains(strings.ToLower(info.Mobile), search) {
				continue
			}
			all = append(all, binding{Email: email, OpenID: info.OpenID, Mobile: info.Mobile})
		}

		// Sort by email for stable pagination.
		sort.Slice(all, func(i, j int) bool { return all[i].Email < all[j].Email })

		total := len(all)

		// Parse pagination params.
		pageSize := 50
		if ps, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && ps > 0 {
			pageSize = ps
		}
		if pageSize > 10000 {
			pageSize = 10000
		}
		page := 1
		if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
			page = p
		}

		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"bindings":  all[start:end],
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		})
	}
}

// DeleteFeishuBindingHandler removes an email→open_id binding.
func DeleteFeishuBindingHandler(notifier *feishu.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if notifier == nil {
			writeError(w, http.StatusServiceUnavailable, "FEISHU_NOT_CONFIGURED", "Feishu notifier is not configured")
			return
		}
		email := r.URL.Query().Get("email")
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		notifier.RemoveOpenID(email)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func loadFeishuConfig(r *http.Request, system store.SystemSettingsRepository) FeishuConfigState {
	raw, err := system.Get(r.Context(), feishuConfigKey)
	if err != nil || raw == "" {
		return FeishuConfigState{}
	}
	var cfg FeishuConfigState
	_ = json.Unmarshal([]byte(raw), &cfg)
	return cfg
}

// ---------------------------------------------------------------------------
// Feishu Auto-Enroll
// ---------------------------------------------------------------------------

// GetFeishuAutoEnrollHandler returns the current auto-enroll setting.
func GetFeishuAutoEnrollHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		cfg := feishu.LoadAutoEnrollSetting(r.Context(), system)
		writeJSON(w, http.StatusOK, cfg)
	}
}

// UpdateFeishuAutoEnrollHandler toggles the auto-enroll setting and
// hot-reloads the AutoEnroller on the notifier.
func UpdateFeishuAutoEnrollHandler(system store.SystemSettingsRepository, notifier *feishu.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		var req feishu.AutoEnrollConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := feishu.SaveAutoEnrollSetting(r.Context(), system, req); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}
		// Hot-reload the auto-enroller.
		if notifier != nil {
			if ae := notifier.AutoEnroller(); ae != nil {
				ae.SetConfig(req)
			}
		}
		writeJSON(w, http.StatusOK, req)
	}
}

func maskSecret(s string) string {
	if len(s) <= 6 {
		return "******"
	}
	return s[:3] + "***" + s[len(s)-3:]
}

func isMasked(s string) bool {
	if len(s) == 0 {
		return false
	}
	if s == "******" {
		return true
	}
	return len(s) > 6 && s[3:6] == "***"
}
