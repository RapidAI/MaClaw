package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// MaClawComputeStatusHandler returns the MaClaw compute authorization status
// for the current tenant. Used by the admin frontend to gate the "add provider" button
// and to construct the compute-store URL (hub_id, center_base_url).
//
// GET /api/admin/llm/maclaw-compute-status
func MaClawComputeStatusHandler(centerSvc *center.Service, accessCtrl *llmservice.TenantLLMAccessControl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := tenantIDFromRequest(r)
		if tenantID == "" {
			tenantID = store.DefaultTenantID
		}

		hubID := ""
		centerBaseURL := ""
		result := map[string]any{
			"allow_external_providers": false,
			"hub_id":                   "",
			"center_base_url":          "",
			"tenant_id":                tenantID,
		}

		currentAccessCtrl := currentMaClawAccessControl(accessCtrl)
		if currentAccessCtrl != nil {
			status := currentAccessCtrl.GetAuthorizationStatus(r.Context(), tenantID)
			if status != nil {
				result["allow_external_providers"] = status.AllowExternalProviders
				if status.HubID != "" {
					hubID = status.HubID
				}
			}
		}

		if centerSvc != nil {
			if status, err := centerSvc.Status(r.Context()); err == nil && status != nil {
				if strings.TrimSpace(hubID) == "" {
					hubID = strings.TrimSpace(status.HubID)
				}
				if strings.TrimSpace(centerBaseURL) == "" {
					centerBaseURL = strings.TrimSpace(status.ActiveBaseURL)
					if centerBaseURL == "" {
						centerBaseURL = strings.TrimSpace(status.BaseURL)
					}
				}
			}
		}

		// Populate hub_id and center_base_url from the MaClaw provider client config
		// so the frontend can construct the compute-store URL without additional API calls.
		if module := GetMaClawModule(); module != nil && module.Client != nil {
			cfg := module.Client.ConfigSnapshot()
			if strings.TrimSpace(hubID) == "" && cfg.HubID != "" {
				hubID = cfg.HubID
			}
			if strings.TrimSpace(centerBaseURL) == "" && cfg.HubCenterURL != "" {
				centerBaseURL = cfg.HubCenterURL
			}
		}
		result["hub_id"] = strings.TrimSpace(hubID)
		result["center_base_url"] = strings.TrimSpace(centerBaseURL)

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
		return store.NormalizeTenantID(tid)
	}
	// Try header (set by tenant-scoped middleware)
	if tid := r.Header.Get("X-Tenant-ID"); tid != "" {
		return store.NormalizeTenantID(tid)
	}
	if tid := RequestTenantID(r); tid != "" {
		return store.NormalizeTenantID(tid)
	}
	return ""
}
