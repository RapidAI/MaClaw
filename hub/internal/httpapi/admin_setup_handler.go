package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

type AdminSetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

func AdminStatusHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		initialized, err := admins.IsInitialized(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"initialized": initialized,
		})
	}
}

func SetupAdminHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminSetupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if req.Username == "" || req.Password == "" || req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Username, password, and email are required")
			return
		}

		initialized, err := admins.IsInitialized(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if initialized {
			writeError(w, http.StatusConflict, "ADMIN_ALREADY_INITIALIZED", "Admin has already been initialized")
			return
		}

		if err := admins.SetupInitialAdmin(r.Context(), req.Username, req.Password, req.Email); err != nil {
			writeError(w, http.StatusInternalServerError, "SETUP_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "MaClaw Hub admin initialized",
		})
	}
}
