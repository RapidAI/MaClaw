package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

type RegistrationEmailSendCodeRequest struct {
	Email    string `json:"email"`
	TenantID string `json:"tenant_id,omitempty"`
}

type RegistrationEmailVerifyAndStartRequest struct {
	EnrollStartRequest
	VerifyCode string `json:"verify_code"`
}

func registrationEmailLogIdentity(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	local := email[:at]
	if len(local) > 2 {
		local = local[:2]
	}
	return local + "***" + email[at:]
}

// RegistrationEmailSendCodeHandler sends the login code before a machine is
// enrolled. The same endpoint deliberately serves new and returning users so
// the client never needs to reveal whether an account already exists.
func RegistrationEmailSendCodeHandler(identity *auth.IdentityService, mailer *mail.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		if identity == nil {
			writeError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		var req RegistrationEmailSendCodeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		log.Printf("[onboarding-email] send_code_begin email=%s tenant_hint=%s", registrationEmailLogIdentity(email), strings.TrimSpace(req.TenantID))
		if !looksLikeRegistrationContactEmail(email) {
			writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
			return
		}
		tenantID := strings.TrimSpace(req.TenantID)
		if tenantID == "" {
			var err error
			tenantID, err = tenantIDForEmailRequest(r, identity, email)
			if err != nil {
				writeError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", err.Error())
				return
			}
		}
		ctx := auth.WithTenant(r.Context(), tenantID)
		if err := identity.ValidateEmailEnrollment(ctx, email); err != nil {
			switch {
			case errors.Is(err, auth.ErrEmailBlocked):
				writeError(w, http.StatusForbidden, "EMAIL_BLOCKED", err.Error())
			case errors.Is(err, auth.ErrEmailDomainNotAllowed):
				writeError(w, http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", err.Error())
			case errors.Is(err, auth.ErrRoutedToAnotherHub):
				writeError(w, http.StatusConflict, "EMAIL_ROUTED_TO_ANOTHER_HUB", err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "EMAIL_VALIDATION_FAILED", err.Error())
			}
			return
		}
		if mailer == nil {
			writeError(w, http.StatusInternalServerError, "MAIL_NOT_CONFIGURED", "Mail delivery is not configured")
			return
		}
		code, err := generateVerifyCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CODE_GEN_FAILED", "Failed to generate verification code")
			return
		}
		key := "enroll:" + email
		previousCode := snapshotVerifyCode(tenantID, key)
		if !storeVerifyCode(tenantID, key, code) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Please wait 60 seconds before requesting a new code")
			return
		}
		body := fmt.Sprintf("您的登录验证码是: %s\r\n\r\n验证码 %d 分钟内有效。如非本人操作，请忽略此消息。", code, int(verifyCodeTTL.Minutes()))
		if err := mailer.Send(ctx, []string{email}, "MaClaw 登录验证码", body); err != nil {
			log.Printf("[onboarding-email] send_code_failed email=%s tenant_id=%s elapsed=%s err=%v", registrationEmailLogIdentity(email), tenantID, time.Since(startedAt), err)
			if !rollbackVerifyCode(tenantID, key, code, previousCode) {
				log.Printf("[onboarding-email] send_code_rollback_skipped email=%s tenant_id=%s reason=code_replaced", registrationEmailLogIdentity(email), tenantID)
			}
			writeError(w, http.StatusBadGateway, "MAIL_SEND_FAILED", err.Error())
			return
		}
		log.Printf("[onboarding-email] send_code_succeeded email=%s tenant_id=%s elapsed=%s expires_min=%d", registrationEmailLogIdentity(email), tenantID, time.Since(startedAt), int(verifyCodeTTL.Minutes()))
		result := map[string]any{
			"ok": true, "kind": "email", "tenant_id": tenantID,
			"expires_min": int(verifyCodeTTL.Minutes()), "code_length": 6,
			"resend_cooldown_seconds": int(verifyCooldown.Seconds()),
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func RegistrationEmailVerifyAndStartHandler(identity *auth.IdentityService, invSvc *invitation.Service, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		var req RegistrationEmailVerifyAndStartRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		log.Printf("[onboarding-email] verify_begin email=%s tenant_hint=%s client_id=%s machine_name=%q", registrationEmailLogIdentity(email), strings.TrimSpace(req.TenantID), strings.TrimSpace(req.ClientID), strings.TrimSpace(req.MachineName))
		if !looksLikeRegistrationContactEmail(email) {
			writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
			return
		}
		tenantID := strings.TrimSpace(req.TenantID)
		if tenantID == "" {
			var err error
			tenantID, err = tenantIDForEmailRequest(r, identity, email)
			if err != nil {
				writeError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", err.Error())
				return
			}
		}
		key := "enroll:" + email
		valid, locked := consumeVerifyCode(tenantID, key, strings.TrimSpace(req.VerifyCode))
		if !valid {
			log.Printf("[onboarding-email] verify_rejected email=%s tenant_id=%s locked=%t elapsed=%s", registrationEmailLogIdentity(email), tenantID, locked, time.Since(startedAt))
			if locked {
				writeError(w, http.StatusTooManyRequests, "VERIFY_LOCKED", "Too many attempts. Please request a new code")
			} else {
				writeError(w, http.StatusBadRequest, "INVALID_VERIFY_CODE", "Invalid or expired verification code")
			}
			return
		}
		log.Printf("[onboarding-email] verify_succeeded email=%s tenant_id=%s client_id=%s elapsed=%s", registrationEmailLogIdentity(email), tenantID, strings.TrimSpace(req.ClientID), time.Since(startedAt))

		req.Email = email
		req.TenantID = tenantID
		data, err := json.Marshal(req.EnrollStartRequest)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ENROLL_FAILED", err.Error())
			return
		}
		next := r.Clone(withEmailVerifiedEnrollment(r.Context(), tenantID))
		next.Body = io.NopCloser(bytes.NewReader(data))
		next.ContentLength = int64(len(data))
		next.Header = r.Header.Clone()
		next.Header.Set("Content-Type", "application/json")
		EnrollStartHandler(identity, invSvc, securitySvc)(w, next)
	}
}

func withEmailVerifiedEnrollment(ctx context.Context, tenantID string) context.Context {
	ctx = context.WithValue(ctx, emailVerifiedEnrollmentContextKey{}, true)
	return context.WithValue(ctx, verifiedEnrollmentTenantContextKey{}, strings.TrimSpace(tenantID))
}
