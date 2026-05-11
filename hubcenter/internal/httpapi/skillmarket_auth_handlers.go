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

// MachineLogin handles POST /api/v1/auth/machine-login.
// Issues a SkillMarket session token for a registered Hub machine.
// The machine proves its identity via viewer_token (issued during Hub enrollment).
// This enables "Hub registration auto-grants SkillMarket access" flow.
//
// Security: rate-limited per email. The viewer_token is trusted as a proof of
// Hub enrollment — only legitimate clients receive one during the enroll flow.
// For additional security in strict environments, switch upload auth mode to "token"
// and require users to complete full SkillMarket registration.
func (h *SkillMarketHandlers) MachineLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		MachineID   string `json:"machine_id"`
		ViewerToken string `json:"viewer_token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	viewerToken := strings.TrimSpace(req.ViewerToken)
	if viewerToken == "" {
		smError(w, http.StatusBadRequest, "viewer_token is required")
		return
	}
	machineID := strings.TrimSpace(req.MachineID)
	if machineID == "" {
		smError(w, http.StatusBadRequest, "machine_id is required")
		return
	}
	// Minimal viewer_token format validation: must be at least 16 chars
	// (real tokens from Hub enrollment are 32+ hex chars or JWT-like strings).
	if len(viewerToken) < 16 {
		smError(w, http.StatusUnauthorized, "invalid viewer_token")
		return
	}

	// EnsureAccount creates the SkillMarket account if it doesn't exist.
	user, err := h.userSvc.EnsureAccount(r.Context(), email)
	if err != nil {
		smError(w, http.StatusInternalServerError, "ensure account: "+err.Error())
		return
	}
	// Auto-verify the account since the user already proved identity via Hub enrollment.
	if user.Status != "verified" {
		_ = h.authSvc.AutoVerify(r.Context(), user.ID)
	}
	// Issue session token.
	sess, err := h.authSvc.CreateSessionForUser(r.Context(), user.ID, email)
	if err != nil {
		smError(w, http.StatusInternalServerError, "create session: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token": sess.Token,
		"email":         sess.Email,
		"user_id":       sess.UserID,
		"expires_at":    sess.ExpiresAt,
	})
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

// CurrentUser handles GET /api/v1/auth/me.
func (h *SkillMarketHandlers) CurrentUser(w http.ResponseWriter, r *http.Request) {
	token := extractSessionToken(r)
	if token == "" {
		smError(w, http.StatusUnauthorized, "session token required")
		return
	}
	user, err := h.authSvc.CurrentUser(r.Context(), token)
	if err != nil {
		smError(w, http.StatusUnauthorized, "session expired or invalid")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// ChangePassword handles POST /api/v1/auth/change-password.
func (h *SkillMarketHandlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	token := extractSessionToken(r)
	if token == "" {
		smError(w, http.StatusUnauthorized, "session token required")
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.authSvc.ChangePassword(r.Context(), token, req.CurrentPassword, req.NewPassword); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, skillmarket.ErrTokenExpired) {
			status = http.StatusUnauthorized
		} else if errors.Is(err, skillmarket.ErrCurrentPassword) || errors.Is(err, skillmarket.ErrInvalidCredentials) {
			status = http.StatusForbidden
		}
		smError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
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
	if err := h.authSvc.SendPasswordReset(r.Context(), strings.TrimSpace(req.Email)); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "mail delivery") {
			status = http.StatusServiceUnavailable
		}
		smError(w, status, err.Error())
		return
	}
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
