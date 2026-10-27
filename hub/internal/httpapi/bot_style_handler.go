package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// BotStyleHandler serves one bot's character, memory and prompt overrides:
//
//	GET  /api/v1/bots/{id}/style           current, history and defaults
//	PUT  /api/v1/bots/{id}/style           {"soul","memory","parts"}; omitted = unchanged
//	POST /api/v1/bots/{id}/style/rollback  {"version":N}
func BotStyleHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		id := r.PathValue("id")
		var (
			view botmgmt.BotStyleView
			err  error
		)
		switch {
		case r.Method == http.MethodGet:
			view, err = svc.GetBotStyleForUser(r.Context(), principal.TenantID, principal.UserID, id)
		case r.Method == http.MethodPut:
			var in botmgmt.BotStyleInput
			if derr := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); derr != nil {
				writeError(w, http.StatusBadRequest, "INVALID_BOT_STYLE", "invalid bot style")
				return
			}
			view, err = svc.SetBotStyleForUser(r.Context(), principal.TenantID, principal.UserID, id, in)
		default:
			var in struct {
				Version int `json:"version"`
			}
			if derr := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); derr != nil || in.Version <= 0 {
				writeError(w, http.StatusBadRequest, "INVALID_BOT_STYLE", "version is required")
				return
			}
			view, err = svc.RollbackBotStyleForUser(r.Context(), principal.TenantID, principal.UserID, id, in.Version)
		}
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// BotAssignmentsHandler GET /api/v1/bots/assignments: the user's
// assignment board (who gave what to whom, state, review).
func BotAssignmentsHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		items, err := svc.ListAssignments(r.Context(), principal.TenantID, principal.UserID)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// BotSaidHandler GET /api/v1/bots/said?room=<id>: what bots told the person
// directly (no room) or the room's conversation.
func BotSaidHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		items, err := svc.ListSaid(r.Context(), principal.TenantID, principal.UserID, r.URL.Query().Get("room"))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
