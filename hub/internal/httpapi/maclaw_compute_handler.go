package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

// MaClawComputeStatusHandler returns the MaClaw compute authorization status
// for the current tenant. Used by the admin frontend to gate the "add provider" button
// and to construct the compute-store URL (hub_id, center_base_url).
//
// GET /api/admin/llm/maclaw-compute-status
func MaClawComputeStatusHandler(_ interface{}, accessCtrl *llmservice.TenantLLMAccessControl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantIDFromRequest(r)
		if tenantID == "" {
			tenantID = "default"
		}

		result := map[string]any{
			"allow_external_providers": false,
			"authorizations":           []any{},
			"hub_id":                   "",
			"center_base_url":          "",
			"tenant_id":                tenantID,
		}

		if accessCtrl != nil {
			status := accessCtrl.GetAuthorizationStatus(r.Context(), tenantID)
			if status != nil {
				result["allow_external_providers"] = status.AllowExternalProviders
				result["authorizations"] = status.Authorizations
				if status.HubID != "" {
					result["hub_id"] = status.HubID
				}
			}
		}

		// Populate hub_id and center_base_url from the MaClaw provider client config
		// so the frontend can construct the compute-store URL without additional API calls.
		if maclawModule != nil && maclawModule.Client != nil {
			cfg := maclawModule.Client.Config
			if result["hub_id"] == "" && cfg.HubID != "" {
				result["hub_id"] = cfg.HubID
			}
			if cfg.HubCenterURL != "" {
				result["center_base_url"] = cfg.HubCenterURL
			}
		}

		// Include admin email from request context if available
		if adminUser := AdminFromContext(r.Context()); adminUser != nil && adminUser.Email != "" {
			result["admin_email"] = adminUser.Email
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func tenantIDFromRequest(r *http.Request) string {
	// Try query param first
	if tid := r.URL.Query().Get("tenant_id"); tid != "" {
		return tid
	}
	// Try header (set by tenant-scoped middleware)
	if tid := r.Header.Get("X-Tenant-ID"); tid != "" {
		return tid
	}
	return ""
}
