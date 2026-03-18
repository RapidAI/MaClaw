package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
)

// ──────────────────────────────────────────────────────────────────────────────
// In-memory verification code store (email → code+expiry). Codes expire after
// 5 minutes and are single-use.
// ──────────────────────────────────────────────────────────────────────────────

type verifyEntry struct {
	Code      string
	ExpiresAt time.Time
}

var (
	verifyCodes   = map[string]*verifyEntry{}
	verifyMu      sync.Mutex
	verifyCodeTTL = 5 * time.Minute
)

func generateVerifyCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

func storeVerifyCode(email, code string) {
	verifyMu.Lock()
	defer verifyMu.Unlock()
	verifyCodes[email] = &verifyEntry{Code: code, ExpiresAt: time.Now().Add(verifyCodeTTL)}
}

func consumeVerifyCode(email, code string) bool {
	verifyMu.Lock()
	defer verifyMu.Unlock()
	entry, ok := verifyCodes[email]
	if !ok || entry.Code != code || time.Now().After(entry.ExpiresAt) {
		return false
	}
	delete(verifyCodes, email)
	return true
}

// ──────────────────────────────────────────────────────────────────────────────
// Public (no-auth) binding page API handlers
// ──────────────────────────────────────────────────────────────────────────────

// BindQueryHandler returns binding info for an email.
// POST /api/bind/query  { "email": "..." }
func BindQueryHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}
		user, err := identity.LookupUserByEmail(r.Context(), email)
		if err != nil {
			writeError(w, http.StatusBadRequest, "LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"bound": false,
				"email": email,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"bound":             true,
			"email":             user.Email,
			"sn":                user.SN,
			"status":            user.Status,
			"enrollment_status": user.EnrollmentStatus,
			"created_at":        user.CreatedAt.Format(time.RFC3339),
		})
	}
}

// BindSendCodeHandler sends a 6-digit verification code via email and/or feishu.
// POST /api/bind/send-code  { "email": "...", "channel": "email"|"feishu"|"both" }
func BindSendCodeHandler(identity *auth.IdentityService, mailer *mail.Service, feishuNotifier *feishu.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email   string `json:"email"`
			Channel string `json:"channel"` // "email", "feishu", "both"
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}
		channel := strings.TrimSpace(strings.ToLower(req.Channel))
		if channel == "" {
			channel = "email"
		}

		// Must be a bound user to send unbind code
		user, err := identity.LookupUserByEmail(r.Context(), email)
		if err != nil || user == nil {
			writeError(w, http.StatusBadRequest, "NOT_BOUND", "This email is not bound")
			return
		}

		code := generateVerifyCode()
		storeVerifyCode(email, code)

		sentVia := []string{}
		subject := "MaClaw 解绑验证码"
		body := fmt.Sprintf("您的解绑验证码是: %s\r\n\r\n验证码 %d 分钟内有效。如非本人操作，请忽略此消息。", code, int(verifyCodeTTL.Minutes()))

		if channel == "email" || channel == "both" {
			if mailer != nil {
				if err := mailer.Send(r.Context(), []string{email}, subject, body); err != nil {
					log.Printf("[bind] send email verify code failed for %s: %v", email, err)
				} else {
					sentVia = append(sentVia, "email")
				}
			}
		}

		if channel == "feishu" || channel == "both" {
			if feishuNotifier != nil {
				openID := feishuNotifier.ResolveOpenIDByEmail(email)
				if openID != "" {
					feishuNotifier.SendTextToOpenID(openID, fmt.Sprintf("MaClaw 解绑验证码: %s（%d分钟内有效）", code, int(verifyCodeTTL.Minutes())))
					sentVia = append(sentVia, "feishu")
				}
			}
		}

		if len(sentVia) == 0 {
			writeError(w, http.StatusInternalServerError, "SEND_FAILED", "Failed to send verification code via any channel")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"sent_via": sentVia,
		})
	}
}

// BindUnbindHandler verifies code and deletes all machines for the email.
// POST /api/bind/unbind  { "email": "...", "code": "..." }
func BindUnbindHandler(identity *auth.IdentityService, deviceSvc *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
			Code  string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		code := strings.TrimSpace(req.Code)
		if email == "" || code == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email and code are required")
			return
		}

		if !consumeVerifyCode(email, code) {
			writeError(w, http.StatusBadRequest, "INVALID_CODE", "Verification code is invalid or expired")
			return
		}

		// Delete all machines for this user
		users := identity.UsersRepo()
		if users == nil {
			writeError(w, http.StatusInternalServerError, "NO_USER_REPO", "User repository not available")
			return
		}

		user, err := identity.LookupUserByEmail(r.Context(), email)
		if err != nil || user == nil {
			writeError(w, http.StatusBadRequest, "NOT_BOUND", "This email is not bound")
			return
		}

		deleted, err := deviceSvc.ForceDeleteMachinesByUser(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "UNBIND_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":               true,
			"deleted_machines": deleted,
			"email":            email,
		})
	}
}


