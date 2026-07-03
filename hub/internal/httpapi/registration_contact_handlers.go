package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type RegistrationContactSendCodeRequest struct {
	Kind         string `json:"kind"`
	Email        string `json:"email,omitempty"`
	PhoneNumber  string `json:"phone_number,omitempty"`
	TenantID     string `json:"tenant_id,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
}

type RegistrationContactVerifyRequest struct {
	Kind         string `json:"kind"`
	Email        string `json:"email,omitempty"`
	PhoneNumber  string `json:"phone_number,omitempty"`
	VerifyCode   string `json:"verify_code"`
	TenantID     string `json:"tenant_id,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
}

func RegistrationContactSendCodeHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, mailer *mail.Service, factory registrationSMSProviderFactory) http.HandlerFunc {
	if factory == nil {
		factory = aliyunDypnsProviderForRegistration
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req RegistrationContactSendCodeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		principal, user, ok := authenticateRegistrationContactUser(w, r, identity, req.TenantID, req.MachineID, req.MachineToken)
		if !ok {
			return
		}
		kind := normalizeRegistrationContactKind(req.Kind)
		switch kind {
		case "email":
			email := strings.TrimSpace(strings.ToLower(req.Email))
			if !looksLikeRegistrationContactEmail(email) {
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
				return
			}
			if existing, err := lookupEmailIdentityUser(r.Context(), identity, principal.TenantID, email); err != nil {
				writeError(w, http.StatusInternalServerError, "EMAIL_LOOKUP_FAILED", err.Error())
				return
			} else if existing != nil && existing.ID != user.ID {
				writeError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "Email is already registered")
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
			if !storeVerifyCode(principal.TenantID, registrationContactEmailKey(user.ID, email), code) {
				writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Please wait 60 seconds before requesting a new code")
				return
			}
			body := fmt.Sprintf("您的注册资料验证码是: %s\r\n\r\n验证码 %d 分钟内有效。如非本人操作，请忽略此消息。", code, int(verifyCodeTTL.Minutes()))
			if err := mailer.Send(auth.WithTenant(r.Context(), principal.TenantID), []string{email}, "MaClaw 注册资料验证码", body); err != nil {
				deleteVerifyCode(principal.TenantID, registrationContactEmailKey(user.ID, email))
				writeError(w, http.StatusBadGateway, "MAIL_SEND_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kind": "email", "expires_min": int(verifyCodeTTL.Minutes()), "code_length": 6})
		case "phone":
			cfg, err := loadRegistrationAuthConfigForTenant(r, system, principal.TenantID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
				return
			}
			phone := normalizePhoneNumber(req.PhoneNumber)
			if !validRegistrationPhoneNumber(phone) {
				writeError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", "valid phone number is required")
				return
			}
			if existing, err := lookupPhoneIdentityUser(r.Context(), identity, principal.TenantID, phone); err != nil {
				writePhoneRegistrationLookupError(w, errPhoneRegistrationLookup{err: err})
				return
			} else if existing != nil && existing.ID != user.ID {
				writeError(w, http.StatusConflict, "PHONE_ALREADY_REGISTERED", "Phone number is already registered")
				return
			}
			smsReq, err := buildAliyunSMSVerifyCodeSendRequest(cfg, registrationSMSBusinessBindNewPhone, phone)
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_SMS_VERIFY_REQUEST", err.Error())
				return
			}
			tenantSystem := ScopedSystemSettingsForTenant(principal.TenantID, system)
			usageNow := nowForRegistrationContact()
			remaining, err := reserveRegistrationSMSSend(r.Context(), tenantSystem, phone, cfg.DailySMSLimit, usageNow)
			if err != nil {
				if limitErr, ok := err.(errRegistrationSMSDailyLimit); ok {
					writeError(w, http.StatusTooManyRequests, "SMS_DAILY_LIMIT_REACHED", limitErr.Error())
					return
				}
				writeError(w, http.StatusInternalServerError, "SMS_DAILY_LIMIT_CHECK_FAILED", err.Error())
				return
			}
			if err := factory(cfg).SendVerifyCode(r.Context(), smsReq); err != nil {
				_ = releaseRegistrationSMSSend(r.Context(), tenantSystem, phone, usageNow)
				writeError(w, http.StatusBadGateway, "SMS_VERIFY_SEND_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kind": "phone", "expires_min": cfg.CodeTTLMinutes, "code_length": cfg.CodeLength, "daily_sms_remaining": remaining})
		default:
			writeError(w, http.StatusBadRequest, "INVALID_CONTACT_KIND", "contact kind must be email or phone")
		}
	}
}

func RegistrationContactVerifyHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory) http.HandlerFunc {
	if factory == nil {
		factory = aliyunDypnsProviderForRegistration
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req RegistrationContactVerifyRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		principal, user, ok := authenticateRegistrationContactUser(w, r, identity, req.TenantID, req.MachineID, req.MachineToken)
		if !ok {
			return
		}
		kind := normalizeRegistrationContactKind(req.Kind)
		switch kind {
		case "email":
			email := strings.TrimSpace(strings.ToLower(req.Email))
			if !looksLikeRegistrationContactEmail(email) {
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "Valid email is required")
				return
			}
			if existing, err := lookupEmailIdentityUser(r.Context(), identity, principal.TenantID, email); err != nil {
				writeError(w, http.StatusInternalServerError, "EMAIL_LOOKUP_FAILED", err.Error())
				return
			} else if existing != nil && existing.ID != user.ID {
				writeError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "Email is already registered")
				return
			}
			valid, locked := consumeVerifyCode(principal.TenantID, registrationContactEmailKey(user.ID, email), strings.TrimSpace(req.VerifyCode))
			if !valid {
				if locked {
					writeError(w, http.StatusTooManyRequests, "VERIFY_LOCKED", "Too many attempts. Please request a new code")
				} else {
					writeError(w, http.StatusBadRequest, "INVALID_VERIFY_CODE", "Invalid verification code")
				}
				return
			}
			if err := identity.BindVerifiedEmailToUser(auth.WithTenant(r.Context(), principal.TenantID), user, email); err != nil {
				if isRegistrationContactIdentityConflict(err) {
					writeError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "Email is already registered")
					return
				}
				writeError(w, http.StatusInternalServerError, "EMAIL_BIND_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kind": "email", "email": email})
		case "phone":
			cfg, err := loadRegistrationAuthConfigForTenant(r, system, principal.TenantID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
				return
			}
			phone := normalizePhoneNumber(req.PhoneNumber)
			checkReq, err := buildAliyunSMSVerifyCodeCheckRequest(phone, req.VerifyCode, cfg.CodeLength)
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_SMS_VERIFY_REQUEST", err.Error())
				return
			}
			valid, err := factory(cfg).CheckVerifyCode(r.Context(), checkReq)
			if err != nil {
				writeError(w, http.StatusBadGateway, "SMS_VERIFY_CHECK_FAILED", err.Error())
				return
			}
			if !valid {
				writeError(w, http.StatusBadRequest, "INVALID_SMS_VERIFY_CODE", "Invalid SMS verification code")
				return
			}
			if existing, err := lookupPhoneIdentityUser(r.Context(), identity, principal.TenantID, phone); err != nil {
				writePhoneRegistrationLookupError(w, errPhoneRegistrationLookup{err: err})
				return
			} else if existing != nil && existing.ID != user.ID {
				writeError(w, http.StatusConflict, "PHONE_ALREADY_REGISTERED", "Phone number is already registered")
				return
			}
			if err := identity.BindVerifiedPhoneToUser(auth.WithTenant(r.Context(), principal.TenantID), user, phone); err != nil {
				if isRegistrationContactIdentityConflict(err) {
					writeError(w, http.StatusConflict, "PHONE_ALREADY_REGISTERED", "Phone number is already registered")
					return
				}
				writeError(w, http.StatusInternalServerError, "PHONE_BIND_FAILED", err.Error())
				return
			}
			tenantSystem := ScopedSystemSettingsForTenant(principal.TenantID, system)
			if changed, err := llmservice.BackfillRegistryUserIDs(auth.WithTenant(r.Context(), principal.TenantID), tenantSystem, identity.UsersRepo(), principal.TenantID); err == nil && changed {
				invalidateLLMRuntimeCaches(tenantSystem)
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kind": "phone", "phone_number": phone})
		default:
			writeError(w, http.StatusBadRequest, "INVALID_CONTACT_KIND", "contact kind must be email or phone")
		}
	}
}

func authenticateRegistrationContactUser(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService, tenantID, machineID, machineToken string) (*auth.MachinePrincipal, *store.User, bool) {
	if identity == nil {
		writeError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
		return nil, nil, false
	}
	principal, err := identity.AuthenticateMachine(auth.WithTenant(r.Context(), strings.TrimSpace(tenantID)), strings.TrimSpace(machineID), strings.TrimSpace(machineToken))
	if err != nil || principal == nil {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "Machine credentials are invalid")
		return nil, nil, false
	}
	if strings.TrimSpace(tenantID) != "" && tenantID != principal.TenantID {
		writeError(w, http.StatusForbidden, "TENANT_MISMATCH", "Machine does not belong to this tenant")
		return nil, nil, false
	}
	user, err := identity.UsersRepo().GetByID(auth.WithTenant(r.Context(), principal.TenantID), principal.UserID)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "USER_NOT_FOUND", "Current user was not found")
		return nil, nil, false
	}
	return principal, user, true
}

func lookupEmailIdentityUser(ctx context.Context, identity *auth.IdentityService, tenantID, email string) (*store.User, error) {
	if identity == nil || identity.UsersRepo() == nil {
		return nil, nil
	}
	return identity.UsersRepo().GetByTenantIdentity(auth.WithTenant(ctx, tenantID), tenantID, "email", email)
}

func isRegistrationContactIdentityConflict(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already belongs to another user")
}

func normalizeRegistrationContactKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "mail" {
		return "email"
	}
	if kind == "mobile" || kind == "phone_number" {
		return "phone"
	}
	return kind
}

func registrationContactEmailKey(userID, email string) string {
	return "profile:" + strings.TrimSpace(userID) + ":" + strings.TrimSpace(strings.ToLower(email))
}

func looksLikeRegistrationContactEmail(email string) bool {
	email = strings.TrimSpace(email)
	return email != "" && strings.Contains(email, "@") && strings.Contains(email[strings.LastIndex(email, "@")+1:], ".")
}

func nowForRegistrationContact() time.Time {
	return time.Now()
}
