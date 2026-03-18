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
// In-memory verification code store with rate limiting and auto-cleanup.
// ──────────────────────────────────────────────────────────────────────────────

const (
	verifyCodeTTL      = 5 * time.Minute
	verifyCooldown     = 60 * time.Second // min interval between send-code requests per email
	verifyMaxAttempts  = 5                // max wrong code attempts before lockout
	verifyCleanupEvery = 10 * time.Minute
)

type verifyEntry struct {
	Code      string
	ExpiresAt time.Time
	SentAt    time.Time
	Attempts  int
}

var (
	verifyCodes      = map[string]*verifyEntry{}
	verifyMu         sync.Mutex
	verifyLastClean  = time.Now()
)

func generateVerifyCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", fmt.Errorf("generating verify code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// storeVerifyCode stores a code and returns false if cooldown hasn't elapsed.
func storeVerifyCode(email, code string) bool {
	verifyMu.Lock()
	defer verifyMu.Unlock()
	cleanupExpiredLocked()
	if entry, ok := verifyCodes[email]; ok {
		if time.Since(entry.SentAt) < verifyCooldown {
			return false // rate limited
		}
	}
	now := time.Now()
	verifyCodes[email] = &verifyEntry{
		Code:      code,
		ExpiresAt: now.Add(verifyCodeTTL),
		SentAt:    now,
	}
	return true
}

func consumeVerifyCode(email, code string) (ok bool, locked bool) {
	verifyMu.Lock()
	defer verifyMu.Unlock()
	entry, exists := verifyCodes[email]
	if !exists || time.Now().After(entry.ExpiresAt) {
		delete(verifyCodes, email)
		return false, false
	}
	if entry.Attempts >= verifyMaxAttempts {
		delete(verifyCodes, email) // force re-send
		return false, true
	}
	if entry.Code != code {
		entry.Attempts++
		return false, entry.Attempts >= verifyMaxAttempts
	}
	delete(verifyCodes, email)
	return true, false
}

// cleanupExpiredLocked removes stale entries. Must be called with verifyMu held.
func cleanupExpiredLocked() {
	if time.Since(verifyLastClean) < verifyCleanupEvery {
		return
	}
	now := time.Now()
	for k, v := range verifyCodes {
		if now.After(v.ExpiresAt) {
			delete(verifyCodes, k)
		}
	}
	verifyLastClean = now
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
			Channel string `json:"channel"`
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
		if channel != "email" && channel != "feishu" && channel != "both" {
			channel = "email"
		}

		// Must be a bound user
		user, err := identity.LookupUserByEmail(r.Context(), email)
		if err != nil || user == nil {
			writeError(w, http.StatusBadRequest, "NOT_BOUND", "This email is not bound")
			return
		}

		code, err := generateVerifyCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CODE_GEN_FAILED", "Failed to generate verification code")
			return
		}

		if !storeVerifyCode(email, code) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Please wait 60 seconds before requesting a new code")
			return
		}

		sentVia := []string{}
		codeMsg := fmt.Sprintf("您的解绑验证码是: %s\r\n\r\n验证码 %d 分钟内有效。如非本人操作，请忽略此消息。", code, int(verifyCodeTTL.Minutes()))

		if channel == "email" || channel == "both" {
			if mailer != nil {
				if sendErr := mailer.Send(r.Context(), []string{email}, "MaClaw 解绑验证码", codeMsg); sendErr != nil {
					log.Printf("[bind] send email verify code failed for %s: %v", email, sendErr)
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

		ok, locked := consumeVerifyCode(email, code)
		if locked {
			writeError(w, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", "Too many wrong attempts, please request a new code")
			return
		}
		if !ok {
			writeError(w, http.StatusBadRequest, "INVALID_CODE", "Verification code is invalid or expired")
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
