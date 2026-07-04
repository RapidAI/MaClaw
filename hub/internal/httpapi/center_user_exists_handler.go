package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// CenterUserExistsHandler is called by HubCenter to verify whether a user
// identity exists on this Hub. Used for route maintenance that cleans stale
// routes pointing to a Hub where the user no longer exists.
//
// POST /api/center/user-exists
// Body: {"email": "user@example.com|phone:170...", "tenant_id": "(optional)"}
// Response: {"exists": true/false}
//
// Authentication: X-HubCenter-Verify header must contain this Hub's
// hub_secret_hash (the SHA-256 hash of the hub_secret, shared between Hub and
// HubCenter during registration).
func CenterUserExistsHandler(identity *auth.IdentityService, centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		identityType, identityValue := normalizeCenterUserExistsIdentity(req.Email)
		if identityValue == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}

		tenantID := strings.TrimSpace(req.TenantID)
		if tenantID == "" {
			tenantID = store.DefaultTenantID
		}

		var (
			user *store.User
			err  error
		)
		if identityType == "phone" {
			user, err = identity.UsersRepo().GetByTenantIdentity(r.Context(), tenantID, identityType, identityValue)
		} else {
			user, err = identity.UsersRepo().GetByTenantEmail(r.Context(), tenantID, identityValue)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]bool{
			"exists": user != nil,
		})
	}
}

func normalizeCenterUserExistsIdentity(value string) (string, string) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", ""
	}
	if strings.Contains(value, "@") {
		return "email", value
	}

	phone := strings.TrimPrefix(value, "phone:")
	if phone == "" {
		return "", ""
	}
	var digits strings.Builder
	for _, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+' || r == '-' || r == '.' || r == '(' || r == ')' || r == ' ' || r == '\t':
			continue
		default:
			return "", ""
		}
	}
	if digits.Len() < 6 {
		return "", ""
	}
	return "phone", digits.String()
}
