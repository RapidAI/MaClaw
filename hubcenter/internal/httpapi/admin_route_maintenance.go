package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
)

// AdminDeleteEmailRouteHandler removes all user-link routes for a specific email
// from the HubCenter routing table. This is used when a user has been deleted from
// the target Hub (e.g. via direct DB operation) but the route record was not cleaned
// up, causing subsequent enrollment/invitation-code attempts to be mis-routed.
//
// POST /api/admin/routing/delete-email-route
// Body: {"email": "user@example.com", "hub_id": "(optional) only delete route to this hub"}
func AdminDeleteEmailRouteHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
			HubID string `json:"hub_id,omitempty"`
		}
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		hubID := strings.TrimSpace(req.HubID)

		deleted, err := service.AdminDeleteEmailRoutes(r.Context(), email, hubID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_ROUTE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"email":         email,
			"deleted_count": deleted,
			"message":       "Route entries removed. Route snapshot will refresh on next rebuild.",
		})
	}
}

// AdminRestoreEmailRouteHandler creates a user-link route for a specific email to
// a specific Hub+Tenant. This is used when a user exists on a Hub but the HubCenter
// routing table has no record (e.g. after a HubCenter database restore or manual
// cleanup that went too far), causing the user to be routed to the wrong Hub.
//
// POST /api/admin/routing/restore-email-route
// Body: {"email": "user@example.com", "hub_id": "hub_xxx", "tenant_id": "(optional)", "is_default": true}
func AdminRestoreEmailRouteHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email     string `json:"email"`
			HubID     string `json:"hub_id"`
			TenantID  string `json:"tenant_id,omitempty"`
			IsDefault bool   `json:"is_default"`
		}
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		hubID := strings.TrimSpace(req.HubID)
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "hub_id is required")
			return
		}
		tenantID := strings.TrimSpace(req.TenantID)
		if tenantID == "" {
			tenantID = "tenant_default"
		}

		link, err := service.AdminRestoreEmailRoute(r.Context(), email, hubID, tenantID, req.IsDefault)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RESTORE_ROUTE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"email":      email,
			"hub_id":     hubID,
			"tenant_id":  tenantID,
			"is_default": req.IsDefault,
			"link_id":    link.ID,
			"message":    "Route entry created. Route snapshot will refresh on next rebuild.",
		})
	}
}

// AdminVerifyEmailRouteHandler checks whether the routed Hub still has the user
// registered. If the Hub is reachable and confirms the user does NOT exist, the
// stale route is automatically cleaned up.
//
// POST /api/admin/routing/verify-email-route
// Body: {"email": "user@example.com"}
func AdminVerifyEmailRouteHandler(service *hubs.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}

		result, err := service.AdminVerifyEmailRoute(r.Context(), email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VERIFY_ROUTE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
