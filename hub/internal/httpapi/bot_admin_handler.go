package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func botTenantID(r *http.Request) string {
	if t := AdminTenantID(r.Context()); t != "" {
		return t
	}
	return store.DefaultTenantID
}

func writeBotError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, botmgmt.ErrSettingsUnavailable):
		writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
	case errors.Is(err, botmgmt.ErrNotConfigured):
		message := "save the MaClawSrv URL and access token first"
		if strings.Contains(err.Error(), "管理密钥") {
			message = "请先在 Bot 管理里保存 MaClawSrv 管理密钥"
		}
		writeError(w, http.StatusConflict, "MACLAWSRV_NOT_CONFIGURED", message)
	case errors.Is(err, botmgmt.ErrNotFound):
		writeError(w, http.StatusNotFound, "BOT_NOT_FOUND", "bot not found")
	case errors.Is(err, botmgmt.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", err.Error())
	case errors.Is(err, botmgmt.ErrDisabled):
		writeError(w, http.StatusForbidden, "BOT_DISABLED", botmgmt.DisabledMessage)
	case errors.Is(err, botmgmt.ErrSrv):
		writeError(w, http.StatusBadGateway, "MACLAWSRV_REQUEST_FAILED", "MaClawSrv rejected the instance request")
	default:
		writeError(w, http.StatusBadGateway, "MACLAWSRV_REQUEST_FAILED", "MaClawSrv rejected the instance request")
	}
}

// GetBotSettingsAdminHandler GET /api/admin/bots/settings
func GetBotSettingsAdminHandler(svc *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			return
		}
		view, err := svc.View(r.Context(), botTenantID(r))
		if err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// PutBotSettingsAdminHandler PUT /api/admin/bots/settings
func PutBotSettingsAdminHandler(svc *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			return
		}
		var in struct {
			BaseURL     string  `json:"base_url"`
			AccessToken *string `json:"access_token"`
			AdminSecret *string `json:"admin_secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		token := ""
		if in.AccessToken != nil {
			token = *in.AccessToken
		}
		view, err := svc.SaveConnection(r.Context(), botTenantID(r), in.BaseURL, token, in.AccessToken != nil)
		if err != nil {
			writeBotError(w, err)
			return
		}
		if in.AdminSecret != nil {
			view, err = svc.SaveAdminSecret(r.Context(), botTenantID(r), *in.AdminSecret)
			if err != nil {
				writeBotError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// TestBotConnectionAdminHandler POST /api/admin/bots/connection/test
func TestBotConnectionAdminHandler(svc *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			return
		}
		count, err := svc.TestConnection(r.Context(), botTenantID(r))
		if err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "instance_count": count})
	}
}

// PostBotGrantAdminHandler POST /api/admin/bots/grants
func PostBotGrantAdminHandler(svc *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			return
		}
		var in botmgmt.Grant
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		grant, err := svc.CreateGrant(r.Context(), botTenantID(r), in)
		if err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, grant)
	}
}

// DeleteBotGrantAdminHandler DELETE /api/admin/bots/grants/{id}
func DeleteBotGrantAdminHandler(svc *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			return
		}
		if err := svc.DeleteGrant(r.Context(), botTenantID(r), r.PathValue("id")); err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
