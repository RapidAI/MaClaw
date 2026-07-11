package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MobileAgentKnowledgeStatusHandler reports knowledge store readiness and
// coarse stats for the mobile account settings surface.
//
//	GET /api/mobile/agent/knowledge/status
func MobileAgentKnowledgeStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		if _, err := authenticateViewerRequest(r, identity); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		// Ensure runtime (and knowledge store) is up.
		_, _, err := mobileEnsureCoreAgent()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"available": false,
				"mode":      "unavailable",
				"message":   "mobile agent runtime is unavailable",
				"sources":   0,
				"cards":     0,
				"facts":     0,
			})
			return
		}
		if mobileKnowledgeStore == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"available": false,
				"mode":      "unavailable",
				"message":   "knowledge store is not initialized",
				"sources":   0,
				"cards":     0,
				"facts":     0,
			})
			return
		}
		mode, msg := mobileKnowledgeModeMessage()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		stats, err := mobileKnowledgeStore.Stats(ctx)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"available": true,
				"mode":      mode,
				"message":   msg + " (stats unavailable)",
				"sources":   0,
				"cards":     0,
				"facts":     0,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"available": true,
			"mode":      mode,
			"message":   msg,
			"sources":   stats.Sources,
			"cards":     stats.Cards,
			"facts":     stats.Facts,
		})
	}
}
