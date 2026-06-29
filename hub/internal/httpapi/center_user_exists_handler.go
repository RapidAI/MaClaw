package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// CenterUserExistsHandler is called by HubCenter to verify whether a user with
// the given email exists on this Hub. Used for route maintenance — cleaning stale
// routes that point to a Hub where the user no longer exists.
//
// POST /api/center/user-exists
// Body: {"email": "user@example.com", "tenant_id": "(optional)"}
// Response: {"exists": true/false}
//
// Authentication: X-HubCenter-Verify header must contain this Hub's
// hub_secret_hash (the SHA-256 hash of the hub_secret, shared between Hub and
// HubCenter during registration).
func CenterUserExistsHandler(identity *auth.IdentityService, centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Verify the request comes from a legitimate HubCenter.
		verifyToken := strings.TrimSpace(r.Header.Get("X-HubCenter-Verify"))
		if verifyToken == "" || centerSvc == nil || !centerSvc.VerifyHubSecretHash(r.Context(), verifyToken) {
			writeError(w, http.StatusForbidden, "UNAUTHORIZED", "invalid or missing verification token")
			return
		}

		var req struct {
			Email    string `json:"email"`
			TenantID string `json:"tenant_id,omitempty"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}

		tenantID := strings.TrimSpace(req.TenantID)
		if tenantID == "" {
			tenantID = store.DefaultTenantID
		}

		user, err := identity.UsersRepo().GetByTenantEmail(r.Context(), tenantID, email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]bool{
			"exists": user != nil,
		})
	}
}
