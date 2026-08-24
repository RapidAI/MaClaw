package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
)

const skillMarketAuthJSONBodyLimit = 4096

// ── Auth Handlers ───────────────────────────────────────────────────────

// Register handles POST /api/v1/auth/register.
func (h *SkillMarketHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
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
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
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
// Issues a SkillMarket session token for an enrolled Hub machine. HubCenter
// asks the registered Hub to authenticate the viewer token and verify that the
// requested machine belongs to its user. Client-provided IDs are never trusted.
func (h *SkillMarketHandlers) MachineLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account     string `json:"account"`
		UserID      string `json:"user_id"`
		Email       string `json:"email"`
		HubID       string `json:"hub_id"`
		MachineID   string `json:"machine_id"`
		ViewerToken string `json:"viewer_token"`
	}
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
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
	hubID := strings.TrimSpace(req.HubID)
	if hubID == "" {
		smError(w, http.StatusBadRequest, "hub_id is required")
		return
	}
	if h.hubVerifier == nil {
		smError(w, http.StatusServiceUnavailable, "Hub identity verification is unavailable")
		return
	}
	principal, err := h.hubVerifier.AuthenticateViewerMachine(r.Context(), hubID, viewerToken, machineID)
	if err != nil || principal == nil || strings.TrimSpace(principal.UserID) == "" {
		smError(w, http.StatusUnauthorized, "viewer authentication failed")
		return
	}
	userID := strings.TrimSpace(principal.UserID)
	if requestedUserID := strings.TrimSpace(req.UserID); requestedUserID != "" && requestedUserID != userID {
		smError(w, http.StatusUnauthorized, "user_id does not match authenticated viewer")
		return
	}
	contact := strings.TrimSpace(req.Account)
	if contact == "" {
		contact = strings.TrimSpace(req.Email)
	}
	if contact == "" {
		contact = strings.TrimSpace(principal.Email)
	}
	if contact == "" {
		contact = userID
	}

	// The Hub user ID is the durable market principal. The contact is retained
	// only for moderation/audit information, so a bound phone and email always
	// reach the same purchases and submissions.
	user, err := h.userSvc.EnsureAccountWithID(r.Context(), userID, contact)
	if err != nil {
		smError(w, http.StatusInternalServerError, "ensure account: "+err.Error())
		return
	}
	// Auto-verify the account since the user already proved identity via Hub enrollment.
	if user.Status != "verified" {
		_ = h.authSvc.AutoVerify(r.Context(), user.ID)
	}
	// Issue session token.
	sess, err := h.authSvc.CreateSessionForUser(r.Context(), user.ID, user.Email)
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
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
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
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
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
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
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
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
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
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
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

func decodeSkillMarketJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	if err := decodeLimitedJSON(w, r, dst, limit); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			smError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		smError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

func extractSessionToken(r *http.Request) string {
	// Bearer header only. The ?session_token= query fallback was removed
	// because no client uses it anymore (web and desktop both send the
	// Authorization header) and query strings end up in access logs.
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
