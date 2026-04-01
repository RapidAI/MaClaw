package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
)

// ── Auth Handlers ───────────────────────────────────────────────────────

// Register handles POST /api/v1/auth/register.
func (h *SkillMarketHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	user, err := h.authSvc.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, skillmarket.ErrEmailRequired) || errors.Is(err, skillmarket.ErrPasswordRequired) {
			status = http.StatusBadRequest
		}
		smError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "activation_email_sent",
		"user_id": user.ID,
		"email":   user.Email,
	})
}

// Activate handles GET /api/v1/auth/activate?token=xxx.
func (h *SkillMarketHandlers) Activate(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		smError(w, http.StatusBadRequest, "token is required")
		return
	}
	sess, err := h.authSvc.Activate(r.Context(), token)
	if err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "activated",
		"session_token": sess.Token,
		"email":         sess.Email,
		"user_id":       sess.UserID,
		"expires_at":    sess.ExpiresAt,
	})
}

// Login handles POST /api/v1/auth/login.
func (h *SkillMarketHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	sess, err := h.authSvc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, skillmarket.ErrAccountNotActive) {
			status = http.StatusForbidden
		}
		smError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token": sess.Token,
		"email":         sess.Email,
		"user_id":       sess.UserID,
		"expires_at":    sess.ExpiresAt,
	})
}

// Logout handles POST /api/v1/auth/logout.
func (h *SkillMarketHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	token := extractSessionToken(r)
	if token == "" {
		smError(w, http.StatusBadRequest, "session token required")
		return
	}
	_ = h.authSvc.Logout(r.Context(), token)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SendLookupVerification handles POST /api/v1/auth/lookup.
// Sends identity verification email for existing verified accounts.
// Always returns success to prevent email enumeration.
func (h *SkillMarketHandlers) SendLookupVerification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Always return success to prevent email enumeration attacks.
	// Errors are silently ignored — only real verified accounts get emails.
	_ = h.authSvc.SendIdentityVerification(r.Context(), strings.TrimSpace(req.Email))
	writeJSON(w, http.StatusOK, map[string]string{"status": "verification_email_sent"})
}

// VerifyIdentity handles GET /api/v1/auth/verify-identity?token=xxx.
func (h *SkillMarketHandlers) VerifyIdentity(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		smError(w, http.StatusBadRequest, "token is required")
		return
	}
	sess, err := h.authSvc.VerifyIdentity(r.Context(), token)
	if err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token": sess.Token,
		"email":         sess.Email,
		"user_id":       sess.UserID,
		"expires_at":    sess.ExpiresAt,
	})
}

// ValidateSession handles GET /api/v1/auth/session.
func (h *SkillMarketHandlers) ValidateSession(w http.ResponseWriter, r *http.Request) {
	token := extractSessionToken(r)
	if token == "" {
		smError(w, http.StatusUnauthorized, "session token required")
		return
	}
	sess, err := h.authSvc.ValidateSession(r.Context(), token)
	if err != nil {
		smError(w, http.StatusUnauthorized, "session expired or invalid")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email":      sess.Email,
		"user_id":    sess.UserID,
		"expires_at": sess.ExpiresAt,
	})
}

// ResendActivation handles POST /api/v1/auth/resend-activation.
func (h *SkillMarketHandlers) ResendActivation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.authSvc.ResendActivation(r.Context(), req.Email); err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activation_email_sent"})
}

// SendPasswordReset handles POST /api/v1/auth/forgot-password.
func (h *SkillMarketHandlers) SendPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	_ = h.authSvc.SendPasswordReset(r.Context(), strings.TrimSpace(req.Email))
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset_email_sent"})
}

// ResetPassword handles POST /api/v1/auth/reset-password.
func (h *SkillMarketHandlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	sess, err := h.authSvc.ResetPassword(r.Context(), req.Token, req.Password)
	if err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "password_reset",
		"session_token": sess.Token,
		"email":         sess.Email,
		"user_id":       sess.UserID,
		"expires_at":    sess.ExpiresAt,
	})
}

// ── helpers ─────────────────────────────────────────────────────────────

func extractSessionToken(r *http.Request) string {
	// Check Authorization header first
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Fallback to query param
	return r.URL.Query().Get("session_token")
}
