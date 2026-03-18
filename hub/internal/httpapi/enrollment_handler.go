package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
)

type enrollmentResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Status    string `json:"status"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type approveEnrollmentRequest struct {
	ID string `json:"id"`
}

type rejectEnrollmentRequest struct {
	ID string `json:"id"`
}

func ListAllEnrollmentsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := identity.ListAllEnrollments(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		resp := make([]enrollmentResponse, len(items))
		for i, item := range items {
			resp[i] = enrollmentResponse{
				ID:        item.ID,
				Email:     item.Email,
				Status:    item.Status,
				Note:      item.Note,
				CreatedAt: item.CreatedAt.Format(time.RFC3339),
				UpdatedAt: item.UpdatedAt.Format(time.RFC3339),
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"enrollments": resp})
	}
}

func ListPendingEnrollmentsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := identity.ListPendingEnrollments(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		resp := make([]enrollmentResponse, len(items))
		for i, item := range items {
			resp[i] = enrollmentResponse{
				ID:        item.ID,
				Email:     item.Email,
				Status:    item.Status,
				Note:      item.Note,
				CreatedAt: item.CreatedAt.Format(time.RFC3339),
				UpdatedAt: item.UpdatedAt.Format(time.RFC3339),
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"enrollments": resp})
	}
}

func ApproveEnrollmentHandler(identity *auth.IdentityService, feishuNotifier *feishu.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req approveEnrollmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Enrollment ID is required")
			return
		}
		user, enrollment, err := identity.ApproveEnrollment(r.Context(), req.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "APPROVE_FAILED", err.Error())
			return
		}

		// Trigger Feishu auto-enrollment for the newly approved user so they
		// can discover the bot without manual intervention.
		if feishuNotifier != nil {
			if ae := feishuNotifier.AutoEnroller(); ae != nil {
				email := user.Email
				mobile := enrollment.Mobile
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := ae.AddToFeishuOrg(ctx, email, "", mobile); err != nil {
						log.Printf("[enroll/approve] feishu auto-enroll failed for %s: %v", email, err)
					}
				}()
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"user": map[string]any{"email": user.Email, "sn": user.SN},
		})
	}
}

func RejectEnrollmentHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req rejectEnrollmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Enrollment ID is required")
			return
		}
		if err := identity.RejectEnrollment(r.Context(), req.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "REJECT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

type pendingLoginResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Purpose   string `json:"purpose"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

// ListPendingLoginsHandler returns all unconsumed, non-expired login tokens so
// the admin can see which PWA users are waiting for email confirmation.
func ListPendingLoginsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := identity.ListPendingLoginTokens(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		resp := make([]pendingLoginResponse, len(items))
		for i, item := range items {
			resp[i] = pendingLoginResponse{
				ID:        item.ID,
				Email:     item.Email,
				Purpose:   item.Purpose,
				ExpiresAt: item.ExpiresAt.Format(time.RFC3339),
				CreatedAt: item.CreatedAt.Format(time.RFC3339),
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"pending_logins": resp})
	}
}

type adminConfirmLoginRequest struct {
	Email string `json:"email"`
}

// AdminConfirmLoginHandler lets the admin manually confirm a pending email login,
// replacing the need for the user to click the email link.
func AdminConfirmLoginHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req adminConfirmLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}
		user, err := identity.AdminConfirmLoginByEmail(r.Context(), req.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CONFIRM_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"user": map[string]any{"email": user.Email, "sn": user.SN},
		})
	}
}
