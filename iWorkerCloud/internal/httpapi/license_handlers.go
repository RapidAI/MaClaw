package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
)

func ListLicensesHandler(svc *license.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.URL.Query().Get("center_id")
		if centerID != "" {
			list, err := svc.ListByCenter(r.Context(), centerID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, list)
			return
		}
		list, err := svc.ListAll(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

func IssueLicenseHandler(svc *license.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CenterID string   `json:"center_id"`
			Modules  []string `json:"modules"`
			Days     int      `json:"days"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if req.CenterID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center_id is required")
			return
		}
		if len(req.Modules) == 0 {
			req.Modules = []string{"compute"}
		}
		lic, err := svc.IssueManual(r.Context(), req.CenterID, req.Modules, req.Days)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ISSUE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, lic)
	}
}

func RevokeLicenseHandler(svc *license.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := svc.Revoke(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "REVOKE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func GetPublicKeyHandler(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		pem, err := license.LoadPublicKeyPEM(dataDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "KEY_ERROR", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Write(pem)
	}
}

func GetActiveLicenseHandler(svc *license.Service, centerAuth centerAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}
		if _, ok := authenticateCenterRequest(w, r, centerAuth, centerID); !ok {
			return
		}
		lic, err := svc.GetActive(r.Context(), centerID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no active license")
			return
		}
		writeJSON(w, http.StatusOK, lic)
	}
}
