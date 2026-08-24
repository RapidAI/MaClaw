package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
)

// CenterSkillMarketAuthenticateHandler verifies a Hub viewer credential for
// HubCenter before HubCenter mints a SkillMarket session. The HubCenter proof
// is the registered hub-secret hash; the viewer token itself is never trusted
// merely because it has a token-like shape.
func CenterSkillMarketAuthenticateHandler(centerSvc *center.Service, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
			return
		}
		if centerSvc == nil || !centerSvc.VerifyHubSecretHash(r.Context(), strings.TrimSpace(r.Header.Get("X-HubCenter-Verify"))) {
			writeError(w, http.StatusUnauthorized, "CENTER_UNAUTHORIZED", "Hub Center is not authorized")
			return
		}

		var req struct {
			MachineID string `json:"machine_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		machineID := strings.TrimSpace(req.MachineID)
		if machineID == "" || identity == nil || identity.MachinesRepo() == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		principal, err := authenticateViewerRequest(r, identity)
		if err != nil || principal == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		machine, err := identity.MachinesRepo().GetByID(r.Context(), machineID)
		if err != nil || machine == nil || machine.UserID != principal.UserID || machine.TenantID != principal.TenantID {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"tenant_id":  principal.TenantID,
			"user_id":    principal.UserID,
			"email":      principal.Email,
			"machine_id": machine.ID,
		})
	}
}
