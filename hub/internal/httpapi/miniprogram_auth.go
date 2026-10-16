package httpapi

// Mini program sign-in API.
//
// The WeChat mini program is a remote third-party client, so it cannot use the
// desktop enrollment endpoints: those issue machine credentials and expect a
// Wails desktop reported for every call. These endpoints issue a *viewer* token
// for an already existing Hub user instead, and deliberately do not create
// accounts — connecting a mini program to a GUI requires an account that owns
// that machine.
//
// Every response carries the routing of the Hub that answered (`hub`), so the
// mini program can pin subsequent Device Gateway / IM Gateway traffic to it.
// When the account belongs to a different Hub in the mesh, the response is a
// 409 with that Hub's identifier so the client can be pointed somewhere else
// instead of silently authenticating the wrong node.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	miniprogramSessionTokenTTL     = 30 * 24 * time.Hour
	miniprogramVerifyCodeKeyPrefix = "miniprogram:"
	miniprogramEmailSubject        = "码卡龙小程序登录验证码"
)

type miniprogramSendCodeRequest struct {
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
	TenantID    string `json:"tenant_id,omitempty"`
}

type miniprogramVerifyRequest struct {
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
	VerifyCode  string `json:"verify_code"`
	TenantID    string `json:"tenant_id,omitempty"`
	ClientID    string `json:"client_id,omitempty"`
}

// miniprogramSession describes the signed-in viewer together with the Hub that
// should serve its gateway traffic.
type miniprogramSession struct {
	AccessToken string                  `json:"access_token,omitempty"`
	OK          bool                    `json:"ok"`
	Status      string                  `json:"status"`
	TenantID    string                  `json:"tenant_id,omitempty"`
	UserID      string                  `json:"user_id,omitempty"`
	SN          string                  `json:"sn,omitempty"`
	Email       string                  `json:"email,omitempty"`
	PhoneNumber string                  `json:"phone_number,omitempty"`
	ExpiresIn   int                     `json:"expires_in,omitempty"`
	Hub         *auth.EmailLoginHub     `json:"hub,omitempty"`
	HubURL      string                  `json:"hub_url,omitempty"`
	HubID       string                  `json:"hub_id,omitempty"`
	User        *miniprogramSessionUser `json:"user,omitempty"`
}

type miniprogramSessionUser struct {
	ID    string `json:"id,omitempty"`
	SN    string `json:"sn,omitempty"`
	Email string `json:"email,omitempty"`
}

// MiniprogramEmailSendCodeHandler emails a six-digit login code to an existing
// Hub user. Unknown addresses are reported instead of enrolled so the endpoint
// cannot be used to probe or create accounts.
func MiniprogramEmailSendCodeHandler(identity *auth.IdentityService, mailer *mail.Service, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		if identity == nil {
			writeMiniprogramError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		var req miniprogramSendCodeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			log.Printf("[miniprogram-email] send_code_rejected code=INVALID_JSON err=%v", err)
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		if !looksLikeRegistrationContactEmail(email) {
			log.Printf("[miniprogram-email] send_code_rejected code=INVALID_EMAIL")
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
			return
		}
		if mailer == nil {
			log.Printf("[miniprogram-email] send_code_rejected code=MAIL_NOT_CONFIGURED email=%s", registrationEmailLogIdentity(email))
			writeMiniprogramError(w, http.StatusServiceUnavailable, "MAIL_NOT_CONFIGURED", "Mail delivery is not configured")
			return
		}
		tenantID, ok := miniprogramTenantForEmail(w, r, identity, email, "send_code")
		if !ok {
			return
		}
		user, err := identity.LookupUserByEmail(auth.WithTenant(r.Context(), tenantID), email)
		if err != nil {
			log.Printf("[miniprogram-email] send_code_rejected email=%s tenant_id=%s code=LOOKUP_FAILED err=%v elapsed=%s", registrationEmailLogIdentity(email), tenantID, err, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil {
			log.Printf("[miniprogram-email] send_code_rejected email=%s tenant_id=%s code=ACCOUNT_NOT_FOUND elapsed=%s", registrationEmailLogIdentity(email), tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "No MaClaw account uses this email")
			return
		}
		if user.Status != "active" {
			log.Printf("[miniprogram-email] send_code_rejected email=%s tenant_id=%s code=ACCOUNT_INACTIVE status=%s elapsed=%s", registrationEmailLogIdentity(email), tenantID, user.Status, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusForbidden, "ACCOUNT_INACTIVE", "This account is not active")
			return
		}

		code, err := generateVerifyCode()
		if err != nil {
			log.Printf("[miniprogram-email] send_code_rejected email=%s tenant_id=%s code=CODE_GEN_FAILED err=%v", registrationEmailLogIdentity(email), tenantID, err)
			writeMiniprogramError(w, http.StatusInternalServerError, "CODE_GEN_FAILED", "Failed to generate verification code")
			return
		}
		key := miniprogramVerifyCodeKeyPrefix + email
		previousCode := snapshotVerifyCode(tenantID, key)
		if !storeVerifyCode(tenantID, key, code) {
			log.Printf("[miniprogram-email] send_code_rejected email=%s tenant_id=%s code=RATE_LIMITED elapsed=%s", registrationEmailLogIdentity(email), tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Please wait before requesting a new code")
			return
		}
		body := fmt.Sprintf("您的登录验证码是: %s\r\n\r\n验证码 %d 分钟内有效。如非本人操作，请忽略此消息。", code, int(verifyCodeTTL.Minutes()))
		if err := mailer.Send(r.Context(), []string{email}, miniprogramEmailSubject, body); err != nil {
			status, deliveryCode := registrationEmailDeliveryError(err)
			log.Printf("[miniprogram-email] send_code_failed email=%s tenant_id=%s code=%s elapsed=%s err=%v", registrationEmailLogIdentity(email), tenantID, deliveryCode, time.Since(startedAt), err)
			if !rollbackVerifyCode(tenantID, key, code, previousCode) {
				log.Printf("[miniprogram-email] send_code_rollback_skipped email=%s tenant_id=%s reason=code_replaced", registrationEmailLogIdentity(email), tenantID)
			}
			writeMiniprogramError(w, status, deliveryCode, err.Error())
			return
		}
		log.Printf("[miniprogram-email] send_code_succeeded email=%s tenant_id=%s elapsed=%s", registrationEmailLogIdentity(email), tenantID, time.Since(startedAt))
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                      true,
			"kind":                    "email",
			"tenant_id":               tenantID,
			"expires_min":             int(verifyCodeTTL.Minutes()),
			"code_length":             6,
			"resend_cooldown_seconds": int(verifyCooldown.Seconds()),
			"hub":                     miniprogramHubPayload(identity),
		})
	}
}

// MiniprogramEmailVerifyHandler exchanges the emailed code for a viewer token.
func MiniprogramEmailVerifyHandler(identity *auth.IdentityService, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		var req miniprogramVerifyRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			log.Printf("[miniprogram-email] verify_rejected code=INVALID_JSON err=%v", err)
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		email := strings.TrimSpace(strings.ToLower(req.Email))
		if identity == nil {
			writeMiniprogramError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		if !looksLikeRegistrationContactEmail(email) {
			log.Printf("[miniprogram-email] verify_rejected code=INVALID_EMAIL")
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
			return
		}
		tenantID, ok := miniprogramTenantForEmail(w, r, identity, email, "verify")
		if !ok {
			return
		}
		// Validate before consuming the one-time code so a recoverable server
		// side problem (policy or routing) does not cost the user their code.
		user, err := identity.LookupUserByEmail(auth.WithTenant(r.Context(), tenantID), email)
		if err != nil {
			log.Printf("[miniprogram-email] verify_rejected email=%s tenant_id=%s code=LOOKUP_FAILED err=%v", registrationEmailLogIdentity(email), tenantID, err)
			writeMiniprogramError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil {
			log.Printf("[miniprogram-email] verify_rejected email=%s tenant_id=%s code=ACCOUNT_NOT_FOUND elapsed=%s", registrationEmailLogIdentity(email), tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "No MaClaw account uses this email")
			return
		}
		if user.Status != "active" {
			log.Printf("[miniprogram-email] verify_rejected email=%s tenant_id=%s code=ACCOUNT_INACTIVE elapsed=%s", registrationEmailLogIdentity(email), tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusForbidden, "ACCOUNT_INACTIVE", "This account is not active")
			return
		}

		key := miniprogramVerifyCodeKeyPrefix + email
		valid, locked := consumeVerifyCode(tenantID, key, strings.TrimSpace(req.VerifyCode))
		if !valid {
			code := "INVALID_VERIFY_CODE"
			status := http.StatusBadRequest
			if locked {
				code = "VERIFY_LOCKED"
				status = http.StatusTooManyRequests
			}
			log.Printf("[miniprogram-email] verify_rejected email=%s tenant_id=%s code=%s locked=%t elapsed=%s", registrationEmailLogIdentity(email), tenantID, code, locked, time.Since(startedAt))
			writeMiniprogramError(w, status, code, "Invalid or expired verification code")
			return
		}

		if !miniprogramIssueSession(w, r, identity, tenantID, user, req.ClientID, "email") {
			return
		}
		log.Printf("[miniprogram-email] verify_succeeded email=%s tenant_id=%s user_id=%s client_id=%s elapsed=%s", registrationEmailLogIdentity(email), tenantID, user.ID, strings.TrimSpace(req.ClientID), time.Since(startedAt))
	}
}

// MiniprogramPhoneSendCodeHandler sends a login SMS through Aliyun DYPNS. The
// code itself is generated and verified by the provider, so unlike the email
// flow nothing is stored locally.
func MiniprogramPhoneSendCodeHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory) http.HandlerFunc {
	if factory == nil {
		factory = aliyunDypnsProviderForRegistration
	}
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		var req miniprogramSendCodeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			log.Printf("[miniprogram-phone] send_code_rejected code=INVALID_JSON err=%v", err)
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		tenantID := tenantIDForSMSRegistration(r, req.TenantID)
		phoneNumber := normalizePhoneNumber(req.PhoneNumber)
		phoneLog := registrationPhoneLogIdentity(phoneNumber)
		if !validRegistrationPhoneNumber(phoneNumber) {
			log.Printf("[miniprogram-phone] send_code_rejected phone=%s code=INVALID_PHONE_NUMBER elapsed=%s", phoneLog, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", "Valid phone number is required")
			return
		}
		if identity == nil {
			writeMiniprogramError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		cfg, err := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		if err != nil {
			log.Printf("[miniprogram-phone] send_code_rejected phone=%s tenant_id=%s code=REGISTRATION_AUTH_LOAD_FAILED err=%v", phoneLog, tenantID, err)
			writeMiniprogramError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
			return
		}
		storedMethod := cfg.Method
		cfg, allowed := registrationSMSEffectiveAuthConfig(cfg, true)
		if !allowed {
			log.Printf("[miniprogram-phone] send_code_rejected phone=%s tenant_id=%s code=PHONE_REGISTRATION_DISABLED stored_method=%s", phoneLog, tenantID, storedMethod)
			writeMiniprogramError(w, http.StatusBadRequest, "PHONE_REGISTRATION_DISABLED", "Phone sign-in is not enabled on this Hub")
			return
		}
		existing, err := identity.LookupUserByPhone(auth.WithTenant(r.Context(), tenantID), phoneNumber)
		if err != nil {
			log.Printf("[miniprogram-phone] send_code_rejected phone=%s tenant_id=%s code=LOOKUP_FAILED err=%v", phoneLog, tenantID, err)
			writeMiniprogramError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}
		if existing == nil {
			log.Printf("[miniprogram-phone] send_code_rejected phone=%s tenant_id=%s code=ACCOUNT_NOT_FOUND elapsed=%s", phoneLog, tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "No MaClaw account uses this phone number")
			return
		}
		if existing.Status != "active" {
			log.Printf("[miniprogram-phone] send_code_rejected phone=%s tenant_id=%s code=ACCOUNT_INACTIVE elapsed=%s", phoneLog, tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusForbidden, "ACCOUNT_INACTIVE", "This account is not active")
			return
		}
		business := registrationSMSBusinessVerifyBoundPhone
		smsReq, err := buildAliyunSMSVerifyCodeSendRequest(cfg, business, phoneNumber)
		if err != nil {
			log.Printf("[miniprogram-phone] send_code_rejected phone=%s tenant_id=%s code=INVALID_SMS_VERIFY_REQUEST err=%v", phoneLog, tenantID, err)
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_SMS_VERIFY_REQUEST", err.Error())
			return
		}
		tenantSystem := ScopedSystemSettingsForTenant(tenantID, system)
		usageNow := time.Now()
		remaining, err := reserveRegistrationSMSSend(r.Context(), tenantSystem, phoneNumber, cfg.DailySMSLimit, usageNow)
		if err != nil {
			if limitErr, ok := err.(errRegistrationSMSDailyLimit); ok {
				log.Printf("[miniprogram-phone] send_code_rejected phone=%s tenant_id=%s code=SMS_DAILY_LIMIT_REACHED limit=%d", phoneLog, tenantID, limitErr.Limit)
				writeMiniprogramError(w, http.StatusTooManyRequests, "SMS_DAILY_LIMIT_REACHED", limitErr.Error())
				return
			}
			log.Printf("[miniprogram-phone] send_code_rejected phone=%s tenant_id=%s code=SMS_DAILY_LIMIT_CHECK_FAILED err=%v", phoneLog, tenantID, err)
			writeMiniprogramError(w, http.StatusInternalServerError, "SMS_DAILY_LIMIT_CHECK_FAILED", err.Error())
			return
		}
		if err := factory(cfg).SendVerifyCode(r.Context(), smsReq); err != nil {
			_ = releaseRegistrationSMSSend(r.Context(), tenantSystem, phoneNumber, usageNow)
			log.Printf("[miniprogram-phone] send_code_failed phone=%s tenant_id=%s err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusBadGateway, "SMS_VERIFY_SEND_FAILED", err.Error())
			return
		}
		log.Printf("[miniprogram-phone] send_code_succeeded phone=%s tenant_id=%s remaining=%d elapsed=%s", phoneLog, tenantID, remaining, time.Since(startedAt))
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                      true,
			"kind":                    "phone",
			"tenant_id":               tenantID,
			"expires_min":             cfg.CodeTTLMinutes,
			"code_length":             cfg.CodeLength,
			"resend_cooldown_seconds": registrationSMSResendCooldownSeconds,
			"daily_sms_limit":         cfg.DailySMSLimit,
			"daily_sms_remaining":     remaining,
			"hub":                     miniprogramHubPayload(identity),
		})
	}
}

// MiniprogramPhoneVerifyHandler verifies the provider-issued SMS code and
// returns a viewer token for the bound account.
func MiniprogramPhoneVerifyHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory) http.HandlerFunc {
	if factory == nil {
		factory = aliyunDypnsProviderForRegistration
	}
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		var req miniprogramVerifyRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			log.Printf("[miniprogram-phone] verify_rejected code=INVALID_JSON err=%v", err)
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if identity == nil {
			writeMiniprogramError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		tenantID := tenantIDForSMSRegistration(r, req.TenantID)
		phoneNumber := normalizePhoneNumber(req.PhoneNumber)
		phoneLog := registrationPhoneLogIdentity(phoneNumber)
		if !validRegistrationPhoneNumber(phoneNumber) {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s code=INVALID_PHONE_NUMBER", phoneLog)
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", "Valid phone number is required")
			return
		}
		cfg, err := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		if err != nil {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s tenant_id=%s code=REGISTRATION_AUTH_LOAD_FAILED err=%v", phoneLog, tenantID, err)
			writeMiniprogramError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
			return
		}
		if _, allowed := registrationSMSEffectiveAuthConfig(cfg, true); !allowed {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s tenant_id=%s code=PHONE_REGISTRATION_DISABLED", phoneLog, tenantID)
			writeMiniprogramError(w, http.StatusBadRequest, "PHONE_REGISTRATION_DISABLED", "Phone sign-in is not enabled on this Hub")
			return
		}
		checkReq, err := buildAliyunSMSVerifyCodeCheckRequest(phoneNumber, strings.TrimSpace(req.VerifyCode))
		if err != nil {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s tenant_id=%s code=INVALID_SMS_VERIFY_REQUEST err=%v", phoneLog, tenantID, err)
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_SMS_VERIFY_REQUEST", err.Error())
			return
		}
		passed, err := factory(cfg).CheckVerifyCode(r.Context(), checkReq)
		if err != nil {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s tenant_id=%s code=SMS_VERIFY_CHECK_FAILED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusBadGateway, "SMS_VERIFY_CHECK_FAILED", err.Error())
			return
		}
		if !passed {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s tenant_id=%s code=INVALID_VERIFY_CODE elapsed=%s", phoneLog, tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusBadRequest, "INVALID_VERIFY_CODE", "Invalid or expired verification code")
			return
		}
		user, err := identity.LookupUserByPhone(auth.WithTenant(r.Context(), tenantID), phoneNumber)
		if err != nil {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s tenant_id=%s code=LOOKUP_FAILED err=%v", phoneLog, tenantID, err)
			writeMiniprogramError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s tenant_id=%s code=ACCOUNT_NOT_FOUND elapsed=%s", phoneLog, tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "No MaClaw account uses this phone number")
			return
		}
		if user.Status != "active" {
			log.Printf("[miniprogram-phone] verify_rejected phone=%s tenant_id=%s code=ACCOUNT_INACTIVE elapsed=%s", phoneLog, tenantID, time.Since(startedAt))
			writeMiniprogramError(w, http.StatusForbidden, "ACCOUNT_INACTIVE", "This account is not active")
			return
		}
		if !miniprogramIssueSession(w, r, identity, tenantID, user, req.ClientID, "phone") {
			return
		}
		log.Printf("[miniprogram-phone] verify_succeeded phone=%s tenant_id=%s user_id=%s client_id=%s elapsed=%s", phoneLog, tenantID, user.ID, strings.TrimSpace(req.ClientID), time.Since(startedAt))
	}
}

// MiniprogramSessionHandler resolves the bearer token carried by the mini
// program so a stored session can be revalidated on launch without asking the
// user to sign in again.
func MiniprogramSessionHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if identity == nil {
			writeMiniprogramError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer"))
		if raw == "" {
			writeMiniprogramError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Bearer token is required")
			return
		}
		principal, err := identity.AuthenticateViewer(r.Context(), raw)
		if err != nil || principal == nil {
			log.Printf("[miniprogram-session] rejected code=UNAUTHORIZED err=%v", err)
			writeMiniprogramError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Session is invalid or expired")
			return
		}
		user, err := identity.UsersRepo().GetByID(r.Context(), principal.UserID)
		if err != nil || user == nil {
			log.Printf("[miniprogram-session] rejected code=USER_LOOKUP_FAILED user_id=%s err=%v", principal.UserID, err)
			writeMiniprogramError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Session user is unavailable")
			return
		}
		writeMiniprogramSession(w, principal.TenantID, user, "", miniprogramSessionTokenTTL.Seconds(), identity)
	}
}

// miniprogramIssueSession issues a viewer token and writes the session payload.
func miniprogramIssueSession(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService, tenantID string, user *store.User, clientID, kind string) bool {
	raw, err := identity.IssueViewerTokenForUser(auth.WithTenant(r.Context(), tenantID), user.ID)
	if err != nil {
		log.Printf("[miniprogram-%s] session_issue_failed user_id=%s tenant_id=%s err=%v", kind, user.ID, tenantID, err)
		writeMiniprogramError(w, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", "Failed to issue a session token")
		return false
	}
	log.Printf("[miniprogram-%s] token_issued user_id=%s tenant_id=%s client_id=%s", kind, user.ID, tenantID, strings.TrimSpace(clientID))
	writeMiniprogramSession(w, tenantID, user, raw, miniprogramSessionTokenTTL.Seconds(), identity)
	return true
}

func writeMiniprogramSession(w http.ResponseWriter, tenantID string, user *store.User, accessToken string, expiresInSeconds float64, identity *auth.IdentityService) {
	hub := miniprogramHubPayload(identity)
	session := miniprogramSession{
		OK:          true,
		Status:      "verified",
		TenantID:    tenantID,
		AccessToken: accessToken,
		UserID:      user.ID,
		SN:          user.SN,
		Email:       user.Email,
		ExpiresIn:   int(expiresInSeconds),
		Hub:         hub,
		User: &miniprogramSessionUser{
			ID:    user.ID,
			SN:    user.SN,
			Email: user.Email,
		},
	}
	if hub != nil {
		session.HubURL = hub.BaseURL
		session.HubID = hub.ID
	}
	writeJSON(w, http.StatusOK, session)
}

func miniprogramHubPayload(identity *auth.IdentityService) *auth.EmailLoginHub {
	if identity == nil {
		return nil
	}
	return identity.CurrentLoginHubPayload()
}

// miniprogramTenantForEmail resolves the tenant for a contact, translating the
// "this account lives on another Hub" error into the 409 the mini program
// expects. It writes the failure itself so callers stay linear.
func miniprogramTenantForEmail(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService, email, stage string) (string, bool) {
	tenantID, err := tenantIDForEmailRequest(r, identity, email)
	if err != nil {
		if errors.Is(err, auth.ErrRoutedToAnotherHub) {
			targetHubID := strings.TrimSpace(strings.TrimPrefix(err.Error(), auth.ErrRoutedToAnotherHub.Error()+": "))
			if targetHubID == auth.ErrRoutedToAnotherHub.Error() {
				targetHubID = ""
			}
			log.Printf("[miniprogram-email] %s_rejected email=%s code=EMAIL_ROUTED_TO_ANOTHER_HUB target_hub=%s", stage, registrationEmailLogIdentity(email), targetHubID)
			writeJSON(w, http.StatusConflict, map[string]any{
				"ok":       false,
				"status":   "routed_to_another_hub",
				"error":    "EMAIL_ROUTED_TO_ANOTHER_HUB",
				"message":  "This account is served by another Hub in the cluster",
				"hub_id":   targetHubID,
				"hub":      miniprogramHubPayload(identity),
				"question": "Please sign in from the Hub that owns this account",
			})
			return "", false
		}
		log.Printf("[miniprogram-email] %s_rejected email=%s code=TENANT_AMBIGUOUS err=%v", stage, registrationEmailLogIdentity(email), err)
		writeMiniprogramError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", err.Error())
		return "", false
	}
	return tenantID, true
}

func writeMiniprogramError(w http.ResponseWriter, status int, code, message string) {
	writeError(w, status, code, message)
}
