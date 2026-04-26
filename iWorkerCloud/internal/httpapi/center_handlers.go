package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/centers"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

func RegisterCenterHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req centers.RegisterRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		result, err := svc.Register(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "REGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func ListCentersHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := svc.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

func CenterManagementHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report, err := svc.Management(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANAGEMENT_REPORT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

func ConfirmCenterTrialHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := svc.ConfirmWithTrial(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "CONFIRM_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func ConfirmCenterManualHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			Modules []string `json:"modules"`
			Days    int      `json:"days"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if len(req.Modules) == 0 {
			req.Modules = []string{"compute"}
		}
		if err := svc.ConfirmManual(r.Context(), id, req.Modules, req.Days); err != nil {
			writeError(w, http.StatusBadRequest, "CONFIRM_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func UpdateCenterIntegrationHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			BaseURL             string `json:"base_url"`
			SupportsMultiTenant bool   `json:"supports_multi_tenant"`
			TenantCount         int    `json:"tenant_count"`
			CloudControlMode    string `json:"cloud_control_mode"`
			LastSyncStatus      string `json:"last_sync_status"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		center, err := svc.UpdateIntegration(r.Context(), id, store.Center{
			BaseURL:             req.BaseURL,
			SupportsMultiTenant: req.SupportsMultiTenant,
			TenantCount:         req.TenantCount,
			CloudControlMode:    req.CloudControlMode,
			LastSyncStatus:      req.LastSyncStatus,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "UPDATE_INTEGRATION_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, center)
	}
}

func ProbeCenterHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		result, center, err := svc.Probe(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusBadRequest, "PROBE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"probe":  result,
			"center": center,
		})
	}
}

func DisableCenterHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := svc.Disable(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "DISABLE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func EnableCenterHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := svc.Enable(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "ENABLE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func DeleteCenterHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := svc.Delete(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func HeartbeatHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			Secret string `json:"secret"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if err := svc.Heartbeat(r.Context(), id, req.Secret); err != nil {
			writeError(w, http.StatusUnauthorized, "HEARTBEAT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ProvisionTenantHandler lets the admin remotely create a tenant on an iWorkerCenter.
func ProvisionTenantHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		center, err := svc.Get(r.Context(), centerID)
		if err != nil || center == nil {
			writeError(w, http.StatusNotFound, "CENTER_NOT_FOUND", "center not found")
			return
		}

		var req struct {
			CompanyName   string `json:"company_name"`
			LegalPerson   string `json:"legal_person"`
			Email         string `json:"email"`
			Address       string `json:"address"`
			AdminUsername string `json:"admin_username"`
			AdminPassword string `json:"admin_password"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		baseURL := strings.TrimSpace(center.BaseURL)
		if override := strings.TrimSpace(r.URL.Query().Get("base_url")); override != "" {
			baseURL = override
		}
		if baseURL == "" {
			writeError(w, http.StatusBadRequest, "MISSING_BASE_URL", "center base_url is not configured")
			return
		}

		result, err := svc.ProvisionRemote(r.Context(), baseURL,
			req.CompanyName, req.LegalPerson, req.Email, req.Address,
			req.AdminUsername, req.AdminPassword)
		if err != nil {
			writeError(w, http.StatusBadGateway, "PROVISION_FAILED", err.Error())
			return
		}

		_, _ = svc.UpdateIntegration(r.Context(), centerID, store.Center{
			BaseURL:             center.BaseURL,
			SupportsMultiTenant: center.SupportsMultiTenant,
			TenantCount:         center.TenantCount + 1,
			CloudControlMode:    center.CloudControlMode,
			LastSyncStatus:      "tenant_provisioned",
		})
		writeJSON(w, http.StatusOK, result)
	}
}
