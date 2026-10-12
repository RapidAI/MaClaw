package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// GetBotLLMHandler GET /api/v1/bots/llm
//
// Reports the model settings that apply to every bot of the calling owner:
// either their own provider, or the default configured for them.
func GetBotLLMHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		view, err := svc.GetPrincipalLLM(r.Context(), principal.TenantID, principal.UserID)
		if err != nil {
			writeBotLLMError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// PutBotLLMHandler PUT /api/v1/bots/llm
//
// A body of {"mode":"default"} drops the owner's provider and restores the
// configured default, whichever one that resolves to now.
func PutBotLLMHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in botmgmt.PrincipalLLMInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_LLM", "invalid model settings")
			return
		}
		view, err := svc.SavePrincipalLLM(r.Context(), principal.TenantID, principal.UserID, in)
		if err != nil {
			writeBotLLMError(w, r, principal, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// writeBotLLMError behaves like writeBotUserError except for rejected input.
// Every rejected value here comes from the caller, and the generic text leaves
// an owner unable to tell a reserved provider name from a malformed URL, which
// is exactly the distinction they have to act on.
func writeBotLLMError(w http.ResponseWriter, r *http.Request, principal *auth.MachinePrincipal, err error) {
	if errors.Is(err, botmgmt.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, "INVALID_BOT_LLM", err.Error())
		return
	}
	writeBotUserError(w, r, principal, err, "")
}
