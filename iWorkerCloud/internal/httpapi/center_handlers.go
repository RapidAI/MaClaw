package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/centers"
)

func RegisterCenterHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req centers.RegisterRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "请求格式错误")
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
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "请求格式错误")
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
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "请求格式错误")
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
			writeError(w, http.StatusNotFound, "CENTER_NOT_FOUND", "找不到该中心")
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
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "请求格式错误")
			return
		}

		// The center's BaseURL is stored as AdminEmail for now — we need the actual base_url.
		// For the provision flow, the admin must provide the center's base URL or we derive it.
		// Since centers table doesn't have base_url yet, we'll use a header or query param.
		baseURL := r.URL.Query().Get("base_url")
		if baseURL == "" {
			writeError(w, http.StatusBadRequest, "MISSING_BASE_URL", "请提供 base_url 参数")
			return
		}

		result, err := svc.ProvisionRemote(r.Context(), baseURL,
			req.CompanyName, req.LegalPerson, req.Email, req.Address,
			req.AdminUsername, req.AdminPassword)
		if err != nil {
			writeError(w, http.StatusBadGateway, "PROVISION_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
