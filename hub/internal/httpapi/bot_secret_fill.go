package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// PostBotSecretFillHandler POST /api/v1/bots/{id}/secret-fill
// The value is forwarded to MaClawSrv and is not written to the log.
func PostBotSecretFillHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		code, err := svc.FillBotSecret(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Name, in.Value)
		if err != nil {
			writeBotUserError(w, r, principal, err)
			return
		}
		if code != "" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": code, "message": code})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"filled": strings.TrimSpace(in.Name)})
	}
}
