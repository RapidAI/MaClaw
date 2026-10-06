package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

func botMachine(w http.ResponseWriter, r *http.Request, identity veMachineAuthenticator) (*auth.MachinePrincipal, bool) {
	principal, ok := authenticateVEMachine(w, r, identity)
	if !ok {
		return nil, false
	}
	if strings.TrimSpace(principal.UserID) == "" || strings.TrimSpace(principal.TenantID) == "" {
		writeError(w, http.StatusForbidden, "BOT_DISABLED", botmgmt.DisabledMessage)
		return nil, false
	}
	return principal, true
}

// GetBotAccessHandler GET /api/v1/bots/access
func GetBotAccessHandler(svc *botmgmt.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		enabled := false
		if svc != nil {
			on, err := svc.Enabled(r.Context(), principal.TenantID, principal.UserID)
			if err == nil {
				enabled = on
			}
		}
		out := map[string]any{"enabled": enabled}
		if !enabled {
			out["message"] = botmgmt.DisabledMessage
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// ListBotsHandler GET /api/v1/bots
func ListBotsHandler(svc *botmgmt.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		bots, err := svc.BotsForUser(r.Context(), principal.TenantID, principal.UserID)
		if err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": bots})
	}
}

// PostBotUserHandler POST /api/v1/bots
func PostBotUserHandler(svc *botmgmt.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		bot, err := svc.CreateBotForUser(r.Context(), principal.TenantID, principal.UserID, in.Name, in.Description)
		if err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, bot)
	}
}

// PatchBotUserHandler PATCH /api/v1/bots/{id}
func PatchBotUserHandler(svc *botmgmt.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		bot, err := svc.UpdateBotForUser(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Name, in.Description)
		if err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, bot)
	}
}

// DeleteBotUserHandler DELETE /api/v1/bots/{id}
func DeleteBotUserHandler(svc *botmgmt.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		if err := svc.DeleteBotForUser(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id")); err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// PostBotMessageHandler POST /api/v1/bots/{id}/messages
func PostBotMessageHandler(svc *botmgmt.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		reply, err := svc.PostMessage(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Content)
		if err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, reply)
	}
}

// GetBotDesktopHandler GET /api/v1/bots/{id}/desktop
// Returns the Hub noVNC path while this user's desktop is running.
func GetBotDesktopHandler(svc *botmgmt.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		novnc, userControl, err := svc.DesktopWatch(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"))
		if err != nil {
			writeBotError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"novnc_url": novnc, "user_control": userControl})
	}
}

// GetDesktopHandoffHandler proxies noVNC through Hub. The token in the path
// is the credential; the iframe cannot send the machine bearer.
func GetDesktopHandoffHandler(svc *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Frame-Options", "ALLOWALL")
		w.Header().Set("Content-Security-Policy", "frame-ancestors *")
		svc.ProxyDesktopHandoff(w, r, r.PathValue("token"), r.PathValue("rest"))
	}
}
