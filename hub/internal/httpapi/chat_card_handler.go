package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// PostBotInteractionDecisionHandler POST /api/v1/bots/{id}/runs/{runID}/interactions/{requestID}/decision
// Forwards the signed-in person's choice. The body cannot name another user.
func PostBotInteractionDecisionHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		body := map[string]any{}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, botSupplementBodyLimit)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		out, err := svc.DecideChatCard(r.Context(), principal.TenantID, principal.UserID, strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.PathValue("runID")), strings.TrimSpace(r.PathValue("requestID")), botmgmt.ChatCardBody(body))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// PostBotInteractionDesktopHandler POST /api/v1/bots/{id}/runs/{runID}/interactions/{requestID}/desktop
// Returns the desktop lease for this card. It does not wake a computer.
func PostBotInteractionDesktopHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		body := map[string]any{}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, botSupplementBodyLimit)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		out, err := svc.OpenChatCardDesktop(r.Context(), principal.TenantID, principal.UserID, strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.PathValue("runID")), strings.TrimSpace(r.PathValue("requestID")), botmgmt.ChatCardBody(body))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// GetBotInteractionShotHandler GET /api/v1/bots/{id}/runs/{runID}/interactions/{requestID}/shot
func GetBotInteractionShotHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		out, err := svc.GetChatCardShot(r.Context(), principal.TenantID, principal.UserID, strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.PathValue("runID")), strings.TrimSpace(r.PathValue("requestID")))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}
