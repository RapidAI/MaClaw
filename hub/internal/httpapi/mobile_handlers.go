package httpapi

import (
	"net/http"
	"sort"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// MobileBootstrapHandler returns the small, cheap payload the mobile app needs
// immediately after restoring a viewer token. Expensive service details stay on
// their existing dedicated endpoints.
func MobileBootstrapHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"user": map[string]any{
				"user_id":   principal.UserID,
				"email":     principal.Email,
				"tenant_id": principal.TenantID,
			},
			"features": map[string]any{
				"search":             true,
				"documents":          true,
				"local_ssh":          true,
				"digital_employees":  true,
				"push_notifications": false,
			},
			"services": map[string]any{
				"hub_status":             "online",
				"llm_status_path":        "/api/llm/service/status",
				"models_path":            "/api/llm/v1/models",
				"search_path":            "/api/mobile/search",
				"documents_path":         "/api/mobile/documents",
				"digital_employees_path": "/api/mobile/digital-employees",
			},
			"limits": map[string]any{
				"max_upload_bytes": 25 * 1024 * 1024,
				"max_export_jobs":  3,
			},
			"server_time": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// MobileDigitalEmployeesHandler lists digital employees a mobile viewer may use
// as remote capability entry points. It intentionally uses viewer auth instead
// of the desktop machine token required by /api/ve/discoverable.
func MobileDigitalEmployeesHandler(identity *auth.IdentityService, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		tenantSystem := scopedSystemSettingsForTenant(principal.TenantID, system)
		authz := loadVEDigitalEmployeeAuthorization(r.Context(), tenantSystem)
		if !veAuthorizationActive(authz) {
			writeJSON(w, http.StatusOK, map[string]any{
				"employees":     []digitalEmployeeEntry{},
				"authorization": authz,
			})
			return
		}

		baseSystem := globalSystemSettings(system)
		runtimePresence := emptyMacLawSrvRuntimePresence()
		registry := loadVERegistry(r.Context(), tenantSystem)
		if veRegistryHasMacLawSrvRuntimeEmployees(registry, true) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), baseSystem, principal.TenantID)
		}

		employees := make([]digitalEmployeeEntry, 0, len(registry.Employees))
		for _, entry := range registry.Employees {
			if entry.Status != veStatusActive {
				continue
			}
			if !veVisibleToRequester(entry, nil, false) {
				continue
			}
			if !veAccessAllowed(entry, principal.UserID) {
				continue
			}
			entry = applyVEDiscoverablePresence(r.Context(), entry, nil, runtimePresence)
			employees = append(employees, entry)
		}
		sort.SliceStable(employees, func(i, j int) bool {
			if employees[i].OnlineStatus != employees[j].OnlineStatus {
				return employees[i].OnlineStatus == veOnlineStatusOnline
			}
			return employees[i].Name < employees[j].Name
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"employees": employees,
		})
	}
}
