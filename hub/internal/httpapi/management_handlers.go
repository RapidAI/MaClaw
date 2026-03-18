package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
)

type ManualBindRequest struct {
	Email string `json:"email"`
}

type LookupUserRequest struct {
	Email string `json:"email"`
}

type BlockEmailRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type CenterConfigRequest struct {
	BaseURL        string `json:"base_url"`
	PublicBaseURL  string `json:"public_base_url"`
	Visibility     string `json:"visibility"`
	EnrollmentMode string `json:"enrollment_mode"`
}

type BoundUserView struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	SN               string `json:"sn"`
	Status           string `json:"status"`
	EnrollmentStatus string `json:"enrollment_status"`
}

func ManualBindHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ManualBindRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		user, err := identity.ManualBind(r.Context(), req.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANUAL_BIND_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"user": map[string]any{
				"id":    user.ID,
				"email": user.Email,
				"sn":    user.SN,
			},
		})
	}
}

func ListBlockedEmailsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := identity.ListBlockedEmails(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_BLOCKED_EMAILS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocked_emails": items})
	}
}

func AddBlockedEmailHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BlockEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		if err := identity.AddBlockedEmail(r.Context(), req.Email, req.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, "ADD_BLOCKED_EMAIL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func RemoveBlockedEmailHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := r.PathValue("email")
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		if err := identity.RemoveBlockedEmail(r.Context(), email); err != nil {
			writeError(w, http.StatusInternalServerError, "REMOVE_BLOCKED_EMAIL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func LookupUserHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")
		if email == "" {
			var req LookupUserRequest
			if r.Method != http.MethodGet {
				if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
					email = req.Email
				}
			}
		}
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		user, err := identity.LookupUserByEmail(r.Context(), email)
		if err != nil {
			if err == auth.ErrInvalidEmail {
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "LOOKUP_USER_FAILED", err.Error())
			return
		}
		if user == nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"user": map[string]any{
				"id":     user.ID,
				"email":  user.Email,
				"sn":     user.SN,
				"status": user.Status,
			},
		})
	}
}

func ListUsersHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := identity.ListUsers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_USERS_FAILED", err.Error())
			return
		}
		out := make([]BoundUserView, 0, len(items))
		for _, user := range items {
			if user == nil {
				continue
			}
			out = append(out, BoundUserView{
				ID:               user.ID,
				Email:            user.Email,
				SN:               user.SN,
				Status:           user.Status,
				EnrollmentStatus: user.EnrollmentStatus,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": out})
	}
}

func GetCenterStatusHandler(centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := centerSvc.Status(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CENTER_STATUS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func UpdateCenterConfigHandler(centerSvc *center.Service, identity *auth.IdentityService, onPublicBaseURLChanged ...func(string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CenterConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.BaseURL == "" && req.PublicBaseURL == "" && req.Visibility == "" && req.EnrollmentMode == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Base URL, public base URL, visibility, or enrollment mode is required")
			return
		}
		var (
			status *center.RegistrationState
			err    error
		)
		if req.BaseURL != "" {
			status, err = centerSvc.SetBaseURL(r.Context(), req.BaseURL)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.PublicBaseURL != "" {
			status, err = centerSvc.SetPublicBaseURL(r.Context(), req.PublicBaseURL)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
			// Notify IM plugins so temp-file download URLs use the new domain.
			for _, fn := range onPublicBaseURLChanged {
				fn(status.PublicBaseURL)
			}
		}
		if req.Visibility != "" {
			status, err = centerSvc.SetVisibility(r.Context(), req.Visibility)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		if req.EnrollmentMode != "" {
			status, err = centerSvc.SetEnrollmentMode(r.Context(), req.EnrollmentMode)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
			if err := identity.SetEnrollmentMode(r.Context(), req.EnrollmentMode); err != nil {
				writeError(w, http.StatusInternalServerError, "CENTER_CONFIG_FAILED", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func RegisterCenterHandler(centerSvc *center.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		status, err := centerSvc.Register(r.Context(), admin.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CENTER_REGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}
