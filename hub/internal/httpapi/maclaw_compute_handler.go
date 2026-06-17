package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func refreshedAuthsSafe(s *llmservice.TenantAuthorizationStatus) []llmservice.AuthorizationSummary {
	if s == nil {
		return nil
	}
	return s.Authorizations
}

const heartbeatAuthorizationKeyLLMCompute = "llm_compute"

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
		var authStatus *llmservice.TenantAuthorizationStatus
		result := map[string]any{
			"allow_external_providers": false,
			"hub_id":                   "",
			"center_base_url":          "",
			"tenant_id":                tenantID,
		}

		currentAccessCtrl := currentMaClawAccessControl(accessCtrl)
		refreshedAuthorization := false
		if currentAccessCtrl != nil {
			if shouldRefreshMaClawComputeStatus(r) {
				if refreshed, err := currentAccessCtrl.RefreshAuthorizationStatus(r.Context(), tenantID); err == nil {
					authStatus = refreshed
					refreshedAuthorization = true
					log.Printf("[maclaw-compute-status] refresh OK tenant=%s allow=%v auths=%d", tenantID, refreshed != nil && refreshed.AllowExternalProviders, len(refreshedAuthsSafe(refreshed)))
				} else if err != nil {
					result["authorization_error"] = err.Error()
					log.Printf("[maclaw-compute-status] refresh ERROR tenant=%s err=%v", tenantID, err)
				}
			}
			if authStatus == nil {
				authStatus = currentAccessCtrl.GetAuthorizationStatus(r.Context(), tenantID)
				if authStatus != nil {
					log.Printf("[maclaw-compute-status] cache-hit tenant=%s allow=%v auths=%d", tenantID, authStatus.AllowExternalProviders, len(authStatus.Authorizations))
				} else {
					log.Printf("[maclaw-compute-status] cache-miss tenant=%s", tenantID)
				}
			}
		}

		if centerSvc != nil {
			if status, err := centerSvc.Status(r.Context()); err == nil && status != nil {
				// Use top-level AllowExternalProviders from heartbeat as the
				// primary source — same mechanism as digital_employee_authorization.
				// This does NOT depend on LLM module being initialized.
				if status.AllowExternalProviders {
					result["allow_external_providers"] = true
				}
				if heartbeatStatus := llmComputeStatusFromCenterAuthorizationPayload(status, tenantID); heartbeatStatus != nil {
					if currentAccessCtrl != nil && !refreshedAuthorization {
						currentAccessCtrl.UpdateFromHeartbeat(tenantID, heartbeatStatus)
					}
					if !refreshedAuthorization {
						authStatus = heartbeatStatus
					}
				}
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
		if authStatus != nil {
			// authStatus may have its own AllowExternalProviders from LLM module.
			// Use OR logic: allow if either top-level heartbeat OR LLM module says yes.
			if authStatus.AllowExternalProviders {
				result["allow_external_providers"] = true
			}
			result["authorizations"] = authStatus.Authorizations
			result["authorization_tenant_id"] = strings.TrimSpace(authStatus.TenantID)
			result["authorization_lookup_tenant_ids"] = authStatus.LookupTenantIDs
			if authStatus.HubID != "" {
				hubID = authStatus.HubID
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

func llmComputeStatusFromCenterAuthorizationPayload(status *center.RegistrationState, tenantID string) *llmservice.TenantAuthorizationStatus {
	if status == nil || len(status.Authorizations) == 0 {
		return nil
	}
	raw := status.Authorizations[heartbeatAuthorizationKeyLLMCompute]
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Tenants map[string]*llmservice.TenantAuthorizationStatus `json:"tenants"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	tenantID = store.NormalizeTenantID(tenantID)
	for _, candidate := range llmComputeAuthorizationTenantKeys(tenantID) {
		if auth := payload.Tenants[candidate]; auth != nil {
			return auth
		}
	}
	// Tenant not present in heartbeat payload — no credits for this tenant.
	// Return nil so the handler preserves cached AllowExternalProviders.
	// When ?refresh=1 triggers a direct QueryAuthorization call, HubCenter
	// will return the authoritative AllowExternalProviders value.
	return nil
}

func llmComputeAuthorizationTenantKeys(tenantID string) []string {
	tenantID = store.NormalizeTenantID(tenantID)
	seen := map[string]struct{}{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(tenantID)
	if tenantID == store.DefaultTenantID {
		add("")
		add("default")
	}
	if strings.HasPrefix(tenantID, "tenant_") {
		add(strings.TrimPrefix(tenantID, "tenant_"))
	} else {
		add("tenant_" + tenantID)
	}
	return out
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

func shouldRefreshMaClawComputeStatus(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("refresh"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
