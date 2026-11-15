package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib/botcapability"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// GetBotCapabilitiesHandler GET /api/v1/bots/capabilities
//
// The MCP servers and skills every bot of this owner can use. Secrets are
// not returned.
func GetBotCapabilitiesHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		view, err := svc.GetOwnerCapabilities(r.Context(), principal.TenantID, principal.UserID)
		if err != nil {
			writeBotCapabilityError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// PutBotCapabilitiesHandler PUT /api/v1/bots/capabilities
//
// Applies one change and pushes it to MaClawSrv. A blank secret keeps the
// stored one. Stdio env is not accepted.
func PutBotCapabilitiesHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var up botcapability.Update
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&up); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_CAPABILITIES", "invalid MCP or skill settings")
			return
		}
		view, err := svc.UpdateOwnerCapabilities(r.Context(), principal.TenantID, principal.UserID, up)
		if err != nil {
			writeBotCapabilityError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func writeBotCapabilityError(w http.ResponseWriter, err error) {
	if errors.Is(err, botmgmt.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, "INVALID_BOT_CAPABILITIES", err.Error())
		return
	}
	if errors.Is(err, botmgmt.ErrSettingsUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
		return
	}
	writeError(w, http.StatusBadGateway, "BOT_CAPABILITIES_FAILED", err.Error())
}
