package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MobileAgentSkillsHandler lists skills installed for the mobile user
// (including Hub-seeded packages). Optional reseed=1 re-runs seed if marker removed.
//
//	GET /api/mobile/agent/skills
//	POST /api/mobile/agent/skills/reseed
func MobileAgentSkillsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		_, svc, err := mobileEnsureCoreAgent()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "AGENT_UNAVAILABLE", "mobile agent runtime is unavailable")
			return
		}
		p := mobileAgentPrincipal(principal)
		if err := svc.EnsurePrincipal(r.Context(), p, principal.Email, p.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, "AGENT_PRINCIPAL", "failed to ensure agent principal")
			return
		}

		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reseed"):
			// Force reseed: drop marker then seed again.
			root := svc.UserSkillsRoot(p.TenantID, p.UserID)
			_ = removeMobileSkillSeedMarker(root)
			mobileSeedUserSkills(svc, p)
			// fall through to list
		case r.Method == http.MethodGet:
			// list only; seed once on first ensure via mobileRunCoreAgent, also seed here if empty
			mobileSeedUserSkills(svc, p)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST .../reseed")
			return
		}

		items, err := svc.ListSkills(r.Context(), p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SKILLS_LIST_FAILED", "failed to list skills")
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			status := strings.TrimSpace(item.Status)
			if status == "" {
				status = "active"
			}
			kind := strings.TrimSpace(item.Type)
			if kind == "" {
				kind = "executable"
			}
			out = append(out, map[string]any{
				"name":        item.Name,
				"description": item.Description,
				"type":        kind,
				"status":      status,
				"version":     item.Version,
				"source":      item.Source,
				"skill_id":    item.SkillID,
				"step_count":  len(item.Steps),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"skills": out,
			"count":  len(out),
		})
	}
}

func removeMobileSkillSeedMarker(skillsRoot string) error {
	return removeFileIfExists(skillsRoot, mobileSkillSeedMarker)
}
