package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func resolveCloudWorkspaceTenant(r *http.Request) string {
	if t := AdminTenantID(r.Context()); t != "" {
		return t
	}
	return store.DefaultTenantID
}

func cloudWorkspaceView(svc *cloudworkspace.Service, r *http.Request, settings cloudworkspace.Settings) cloudworkspace.SettingsView {
	tenantID := resolveCloudWorkspaceTenant(r)
	preview := cloudworkspace.Preview{OverQuotaUsers: []cloudworkspace.OverQuotaUser{}}
	if svc != nil {
		preview = svc.BuildPreview(r.Context(), tenantID, settings)
	}
	if preview.OverQuotaUsers == nil {
		preview.OverQuotaUsers = []cloudworkspace.OverQuotaUser{}
	}
	return cloudworkspace.SettingsView{Settings: settings, Preview: preview}
}

// GetCloudWorkspaceSettingsAdminHandler GET /api/admin/cloud-workspaces/settings
func GetCloudWorkspaceSettingsAdminHandler(svc *cloudworkspace.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "cloud workspace settings store is unavailable")
			return
		}
		tenantID := resolveCloudWorkspaceTenant(r)
		settings := svc.LoadTenantSettings(r.Context(), tenantID)
		writeJSON(w, http.StatusOK, cloudWorkspaceView(svc, r, settings))
	}
}

// PutCloudWorkspaceSettingsAdminHandler PUT /api/admin/cloud-workspaces/settings
func PutCloudWorkspaceSettingsAdminHandler(svc *cloudworkspace.Service, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil || svc.System == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "cloud workspace settings store is unavailable")
			return
		}
		var req cloudworkspace.Settings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return
		}
		tenantID := resolveCloudWorkspaceTenant(r)
		settings, err := svc.SaveTenantSettings(r.Context(), tenantID, req)
		if err != nil {
			if errors.Is(err, cloudworkspace.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
				return
			}
			if errors.Is(err, cloudworkspace.ErrSettingsUnavailable) {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "SETTINGS_SAVE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "cloud_workspace.settings.update", map[string]any{
			"mode":                   settings.Mode,
			"quota":                  settings.Quota,
			"department_ids":         settings.DepartmentIDs,
			"max_workspace_bytes":    settings.MaxWorkspaceBytes,
			"tenant_max_total_bytes": settings.TenantMaxTotalBytes,
		})
		writeJSON(w, http.StatusOK, cloudWorkspaceView(svc, r, settings))
	}
}
