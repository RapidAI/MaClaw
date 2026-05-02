package httpapi

import (
	"errors"
	"net/http"

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
			BaseURL          string `json:"base_url"`
			CloudControlMode string `json:"cloud_control_mode"`
			LastSyncStatus   string `json:"last_sync_status"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		center, err := svc.UpdateIntegration(r.Context(), id, store.Center{
			BaseURL:          req.BaseURL,
			CloudControlMode: req.CloudControlMode,
			LastSyncStatus:   req.LastSyncStatus,
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

func RuntimeSnapshotHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := svc.RuntimeSnapshot(r.Context(), r.PathValue("id"))
		if err != nil {
			if errors.Is(err, centers.ErrNotFound) {
				writeError(w, http.StatusNotFound, "CENTER_NOT_FOUND", "center not found")
				return
			}
			writeError(w, http.StatusBadRequest, "RUNTIME_SNAPSHOT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
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
		var req centers.HeartbeatRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if err := svc.Heartbeat(r.Context(), id, req); err != nil {
			writeHeartbeatError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func ServiceReadinessHandler(svc *centers.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		readiness, err := svc.ServiceReadiness(r.Context(), r.PathValue("id"))
		if err != nil {
			writeServiceReadinessError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, readiness)
	}
}

func writeServiceReadinessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, centers.ErrNotFound):
		writeError(w, http.StatusNotFound, "CENTER_NOT_FOUND", "center not found")
	case errors.Is(err, centers.ErrServiceManagementNotAllowed):
		writeError(w, http.StatusConflict, "CENTER_SERVICE_NOT_READY", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "CENTER_SERVICE_CHECK_FAILED", err.Error())
	}
}
func writeHeartbeatError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, centers.ErrInvalidServiceIdentity):
		writeError(w, http.StatusBadRequest, "HEARTBEAT_IDENTITY_FAILED", err.Error())
	case errors.Is(err, centers.ErrDisabled):
		writeError(w, http.StatusForbidden, "CENTER_DISABLED", err.Error())
	case errors.Is(err, centers.ErrNotFound):
		writeError(w, http.StatusNotFound, "CENTER_NOT_FOUND", err.Error())
	case errors.Is(err, centers.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "HEARTBEAT_UNAUTHORIZED", err.Error())
	default:
		writeError(w, http.StatusUnauthorized, "HEARTBEAT_FAILED", err.Error())
	}
}
