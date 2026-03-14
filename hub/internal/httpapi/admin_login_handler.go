package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

type AdminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AdminChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type AdminUpdateProfileRequest struct {
	Email string `json:"email"`
}

func AdminLoginHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Username and password are required")
			return
		}

		token, admin, err := admins.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "LOGIN_FAILED", "Invalid username or password")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"expires_in":   7200,
			"admin": map[string]any{
				"username": admin.Username,
				"email":    admin.Email,
			},
		})
	}
}

func AdminChangePasswordHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		var req AdminChangePasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.CurrentPassword == "" || req.NewPassword == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Current password and new password are required")
			return
		}

		token, updatedAdmin, err := admins.ChangePassword(r.Context(), admin.Username, req.CurrentPassword, req.NewPassword)
		if err != nil {
			if err == auth.ErrInvalidAdminPassword {
				writeError(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Current password is incorrect")
				return
			}
			writeError(w, http.StatusInternalServerError, "CHANGE_PASSWORD_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"access_token": token,
			"admin": map[string]any{
				"username": updatedAdmin.Username,
				"email":    updatedAdmin.Email,
			},
		})
	}
}

func AdminUpdateProfileHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		var req AdminUpdateProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		token, updatedAdmin, err := admins.UpdateEmail(r.Context(), admin.Username, req.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_PROFILE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"access_token": token,
			"admin": map[string]any{
				"username": updatedAdmin.Username,
				"email":    updatedAdmin.Email,
			},
		})
	}
}
