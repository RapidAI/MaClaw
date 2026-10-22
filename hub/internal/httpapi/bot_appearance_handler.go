package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// PutBotAppearanceHandler PUT /api/v1/bots/{id}/appearance
// Body {"color":"#rrggbb","shape":"cloud"}; an omitted field is unchanged,
// an empty string clears it. Answers with the bot.
func PutBotAppearanceHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		var in struct {
			Color *string `json:"color"`
			Shape *string `json:"shape"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		bot, err := svc.SetBotAppearanceForUser(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Color, in.Shape)
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, bot)
	}
}
