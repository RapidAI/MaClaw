package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func principalTenant(p *auth.MachinePrincipal) string {
	if p == nil {
		return ""
	}
	return p.TenantID
}

func principalUser(p *auth.MachinePrincipal) string {
	if p == nil {
		return ""
	}
	return p.UserID
}

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
	case errors.Is(err, botmgmt.ErrAdminSecretMissing):
		writeError(w, http.StatusConflict, "MACLAWSRV_NOT_CONFIGURED", "请先在 Bot 管理里保存 MaClawSrv 管理密钥")
	case errors.Is(err, botmgmt.ErrNotConfigured):
		writeError(w, http.StatusConflict, "MACLAWSRV_NOT_CONFIGURED", "save the MaClawSrv URL and access token first")
	case errors.Is(err, botmgmt.ErrNotFound):
		writeError(w, http.StatusNotFound, "BOT_NOT_FOUND", "bot not found")
	case errors.Is(err, botmgmt.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", err.Error())
	case errors.Is(err, botmgmt.ErrDisabled):
		writeError(w, http.StatusForbidden, "BOT_DISABLED", botmgmt.DisabledMessage)
	case errors.Is(err, botmgmt.ErrSrv), errors.Is(err, botmgmt.ErrSrvNotFound):
		writeError(w, http.StatusBadGateway, "MACLAWSRV_REQUEST_FAILED", botmgmt.SrvRejectionMessage(err))
	default:
		writeError(w, http.StatusBadGateway, "MACLAWSRV_REQUEST_FAILED", "MaClawSrv rejected the instance request")
	}
}

// writeBotUserError reports a bot failure to an end user. MaClawSrv status
// codes and response bodies are admin diagnostics only, and a user cannot act
// on "save the admin secret", so those collapse into one unavailable message.
// The real cause goes to the hub log instead: without it a 502 here is
// undiagnosable because nothing else records the underlying error.
func writeBotUserError(w http.ResponseWriter, r *http.Request, principal *auth.MachinePrincipal, err error) {
	log.Printf("[hub-bot] user bot request failed method=%s path=%s tenant=%q user=%q err=%v",
		r.Method, r.URL.Path, principalTenant(principal), principalUser(principal), err)
	switch {
	case errors.Is(err, botmgmt.ErrSettingsUnavailable),
		errors.Is(err, botmgmt.ErrNotConfigured),
		errors.Is(err, botmgmt.ErrSrv),
		errors.Is(err, botmgmt.ErrSrvNotFound):
		writeError(w, http.StatusBadGateway, "MACLAWSRV_REQUEST_FAILED", "bot service is unavailable, contact the administrator")
	case errors.Is(err, botmgmt.ErrNotFound):
		writeError(w, http.StatusNotFound, "BOT_NOT_FOUND", "bot not found")
	case errors.Is(err, botmgmt.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
	case errors.Is(err, botmgmt.ErrDisabled):
		writeError(w, http.StatusForbidden, "BOT_DISABLED", botmgmt.DisabledMessage)
	default:
		writeError(w, http.StatusBadGateway, "MACLAWSRV_REQUEST_FAILED", "bot service is unavailable, contact the administrator")
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
func PutBotSettingsAdminHandler(svc *botmgmt.Service, audit store.AdminAuditRepository) http.HandlerFunc {
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
		// This record holds a MaClawSrv bearer token and the root admin
		// secret. Only whether they are set is ever written down.
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "bot.settings.update", map[string]any{
			"base_url":             view.BaseURL,
			"token_set":            view.TokenSet,
			"admin_secret_set":     view.AdminSecretSet,
			"token_changed":        in.AccessToken != nil,
			"admin_secret_changed": in.AdminSecret != nil,
		})
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
func PostBotGrantAdminHandler(svc *botmgmt.Service, audit store.AdminAuditRepository) http.HandlerFunc {
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
		// A grant decides who may use bots, so who got access is part of the
		// record of what this admin did.
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "bot.grant.create", map[string]any{
			"grant_id":  grant.ID,
			"scope":     grant.Scope,
			"target_id": grant.TargetID,
		})
		writeJSON(w, http.StatusCreated, grant)
	}
}

// DeleteBotGrantAdminHandler DELETE /api/admin/bots/grants/{id}
func DeleteBotGrantAdminHandler(svc *botmgmt.Service, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			return
		}
		grantID := r.PathValue("id")
		if err := svc.DeleteGrant(r.Context(), botTenantID(r), grantID); err != nil {
			writeBotError(w, err)
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "bot.grant.delete", map[string]any{
			"grant_id": grantID,
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
