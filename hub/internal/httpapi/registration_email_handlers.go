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
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type RegistrationEmailSendCodeRequest struct {
	Email    string `json:"email"`
	TenantID string `json:"tenant_id,omitempty"`
}

type RegistrationEmailVerifyAndStartRequest struct {
	EnrollStartRequest
	VerifyCode string `json:"verify_code"`
}

// RegistrationEmailSendCodeHandler sends the login code before a machine is
// enrolled. The same endpoint deliberately serves new and returning users so
// the client never needs to reveal whether an account already exists.
func RegistrationEmailSendCodeHandler(identity *auth.IdentityService, mailer *mail.Service, systems ...store.SystemSettingsRepository) http.HandlerFunc {
	var system store.SystemSettingsRepository
	if len(systems) > 0 {
		system = systems[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		if identity == nil {
			log.Printf("[onboarding-email] send_code_rejected code=IDENTITY_UNAVAILABLE")
			writeError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		var req RegistrationEmailSendCodeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			log.Printf("[onboarding-email] send_code_rejected code=INVALID_JSON err=%v", err)
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		emailLog := registrationEmailLogIdentity(email)
		domainLog := registrationEmailDomainLog(email)
		tenantHint := strings.TrimSpace(req.TenantID)
		log.Printf("[onboarding-email] send_code_begin email=%s domain=%s tenant_hint=%s", emailLog, domainLog, tenantHint)
		if !looksLikeRegistrationContactEmail(email) {
			log.Printf("[onboarding-email] send_code_rejected email=%s domain=%s code=INVALID_EMAIL elapsed=%s", emailLog, domainLog, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
			return
		}
		tenantID := tenantHint
		tenantSource := "hint"
		if tenantID == "" {
			var err error
			tenantID, err = tenantIDForEmailRequest(r, identity, email)
			if err != nil {
				log.Printf("[onboarding-email] send_code_rejected email=%s domain=%s code=TENANT_AMBIGUOUS err=%v elapsed=%s", emailLog, domainLog, err, time.Since(startedAt))
				writeError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", err.Error())
				return
			}
			tenantSource = "resolve"
		}
		authMethod := registrationAuthMethodEmail
		if system != nil {
			cfg, configErr := loadRegistrationAuthConfigForTenant(r, system, tenantID)
			if configErr != nil {
				log.Printf("[onboarding-email] send_code_rejected email=%s domain=%s tenant_id=%s tenant_source=%s code=REGISTRATION_AUTH_LOAD_FAILED err=%v elapsed=%s", emailLog, domainLog, tenantID, tenantSource, configErr, time.Since(startedAt))
				writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", configErr.Error())
				return
			}
			authMethod = cfg.Method
			if cfg.Method == registrationAuthMethodPhone {
				log.Printf("[onboarding-email] send_code_rejected email=%s domain=%s tenant_id=%s tenant_source=%s method=%s code=EMAIL_REGISTRATION_DISABLED elapsed=%s", emailLog, domainLog, tenantID, tenantSource, authMethod, time.Since(startedAt))
				writeError(w, http.StatusBadRequest, "EMAIL_REGISTRATION_DISABLED", "Email registration is not enabled")
				return
			}
		}
		ctx := auth.WithTenant(r.Context(), tenantID)
		if err := identity.ValidateEmailEnrollment(ctx, email); err != nil {
			code := "EMAIL_VALIDATION_FAILED"
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, auth.ErrEmailBlocked):
				code = "EMAIL_BLOCKED"
				status = http.StatusForbidden
			case errors.Is(err, auth.ErrEmailDomainNotAllowed):
				code = "EMAIL_DOMAIN_NOT_ALLOWED"
				status = http.StatusForbidden
			case errors.Is(err, auth.ErrRoutedToAnotherHub):
				code = "EMAIL_ROUTED_TO_ANOTHER_HUB"
				status = http.StatusConflict
			}
			log.Printf("[onboarding-email] send_code_rejected email=%s domain=%s tenant_id=%s tenant_source=%s method=%s code=%s err=%v elapsed=%s", emailLog, domainLog, tenantID, tenantSource, authMethod, code, err, time.Since(startedAt))
			writeError(w, status, code, err.Error())
			return
		}
		if mailer == nil {
			log.Printf("[onboarding-email] send_code_rejected email=%s domain=%s tenant_id=%s method=%s code=MAIL_NOT_CONFIGURED elapsed=%s", emailLog, domainLog, tenantID, authMethod, time.Since(startedAt))
			writeError(w, http.StatusInternalServerError, "MAIL_NOT_CONFIGURED", "Mail delivery is not configured")
			return
		}
		code, err := generateVerifyCode()
		if err != nil {
			log.Printf("[onboarding-email] send_code_rejected email=%s tenant_id=%s code=CODE_GEN_FAILED err=%v elapsed=%s", emailLog, tenantID, err, time.Since(startedAt))
			writeError(w, http.StatusInternalServerError, "CODE_GEN_FAILED", "Failed to generate verification code")
			return
		}
		key := "enroll:" + email
		previousCode := snapshotVerifyCode(tenantID, key)
		if !storeVerifyCode(tenantID, key, code) {
			log.Printf("[onboarding-email] send_code_rejected email=%s tenant_id=%s code=RATE_LIMITED elapsed=%s", emailLog, tenantID, time.Since(startedAt))
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Please wait 60 seconds before requesting a new code")
			return
		}
		body := fmt.Sprintf("您的登录验证码是: %s\r\n\r\n验证码 %d 分钟内有效。如非本人操作，请忽略此消息。", code, int(verifyCodeTTL.Minutes()))
		if err := mailer.Send(ctx, []string{email}, "MaClaw 登录验证码", body); err != nil {
			log.Printf("[onboarding-email] send_code_failed email=%s domain=%s tenant_id=%s method=%s elapsed=%s err=%v", emailLog, domainLog, tenantID, authMethod, time.Since(startedAt), err)
			if !rollbackVerifyCode(tenantID, key, code, previousCode) {
				log.Printf("[onboarding-email] send_code_rollback_skipped email=%s tenant_id=%s reason=code_replaced", emailLog, tenantID)
			}
			writeError(w, http.StatusBadGateway, "MAIL_SEND_FAILED", err.Error())
			return
		}
		log.Printf("[onboarding-email] send_code_succeeded email=%s domain=%s tenant_id=%s tenant_source=%s method=%s elapsed=%s expires_min=%d", emailLog, domainLog, tenantID, tenantSource, authMethod, time.Since(startedAt), int(verifyCodeTTL.Minutes()))
		result := map[string]any{
			"ok": true, "kind": "email", "tenant_id": tenantID,
			"expires_min": int(verifyCodeTTL.Minutes()), "code_length": 6,
			"resend_cooldown_seconds": int(verifyCooldown.Seconds()),
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func RegistrationEmailVerifyAndStartHandler(identity *auth.IdentityService, invSvc *invitation.Service, securitySvc *security.SecurityService, systems ...store.SystemSettingsRepository) http.HandlerFunc {
	var system store.SystemSettingsRepository
	if len(systems) > 0 {
		system = systems[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		var req RegistrationEmailVerifyAndStartRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			log.Printf("[onboarding-email] verify_rejected code=INVALID_JSON err=%v", err)
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		emailLog := registrationEmailLogIdentity(email)
		domainLog := registrationEmailDomainLog(email)
		tenantHint := strings.TrimSpace(req.TenantID)
		clientID := strings.TrimSpace(req.ClientID)
		log.Printf("[onboarding-email] verify_begin email=%s domain=%s tenant_hint=%s client_id=%s machine_name=%q", emailLog, domainLog, tenantHint, clientID, strings.TrimSpace(req.MachineName))
		if !looksLikeRegistrationContactEmail(email) {
			log.Printf("[onboarding-email] verify_rejected email=%s domain=%s code=INVALID_EMAIL elapsed=%s", emailLog, domainLog, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
			return
		}
		tenantID := tenantHint
		tenantSource := "hint"
		if tenantID == "" {
			var err error
			tenantID, err = tenantIDForEmailRequest(r, identity, email)
			if err != nil {
				log.Printf("[onboarding-email] verify_rejected email=%s domain=%s code=TENANT_AMBIGUOUS err=%v elapsed=%s", emailLog, domainLog, err, time.Since(startedAt))
				writeError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", err.Error())
				return
			}
			tenantSource = "resolve"
		}
		authMethod := registrationAuthMethodEmail
		if system != nil {
			cfg, configErr := loadRegistrationAuthConfigForTenant(r, system, tenantID)
			if configErr != nil {
				log.Printf("[onboarding-email] verify_rejected email=%s domain=%s tenant_id=%s tenant_source=%s code=REGISTRATION_AUTH_LOAD_FAILED err=%v elapsed=%s", emailLog, domainLog, tenantID, tenantSource, configErr, time.Since(startedAt))
				writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", configErr.Error())
				return
			}
			authMethod = cfg.Method
			if cfg.Method == registrationAuthMethodPhone {
				log.Printf("[onboarding-email] verify_rejected email=%s domain=%s tenant_id=%s tenant_source=%s method=%s code=EMAIL_REGISTRATION_DISABLED elapsed=%s", emailLog, domainLog, tenantID, tenantSource, authMethod, time.Since(startedAt))
				writeError(w, http.StatusBadRequest, "EMAIL_REGISTRATION_DISABLED", "Email registration is not enabled")
				return
			}
		}
		key := "enroll:" + email
		valid, locked := consumeVerifyCode(tenantID, key, strings.TrimSpace(req.VerifyCode))
		if !valid {
			code := "INVALID_VERIFY_CODE"
			if locked {
				code = "VERIFY_LOCKED"
			}
			log.Printf("[onboarding-email] verify_rejected email=%s domain=%s tenant_id=%s method=%s code=%s locked=%t elapsed=%s", emailLog, domainLog, tenantID, authMethod, code, locked, time.Since(startedAt))
			if locked {
				writeError(w, http.StatusTooManyRequests, "VERIFY_LOCKED", "Too many attempts. Please request a new code")
			} else {
				writeError(w, http.StatusBadRequest, "INVALID_VERIFY_CODE", "Invalid or expired verification code")
			}
			return
		}
		log.Printf("[onboarding-email] verify_succeeded email=%s domain=%s tenant_id=%s tenant_source=%s method=%s client_id=%s elapsed=%s", emailLog, domainLog, tenantID, tenantSource, authMethod, clientID, time.Since(startedAt))

		req.Email = email
		req.TenantID = tenantID
		data, err := json.Marshal(req.EnrollStartRequest)
		if err != nil {
			log.Printf("[onboarding-email] verify_rejected email=%s tenant_id=%s code=ENROLL_FAILED err=%v elapsed=%s", emailLog, tenantID, err, time.Since(startedAt))
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
