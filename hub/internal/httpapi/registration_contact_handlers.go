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
	stdmail "net/mail"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
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

func RegistrationContactSendCodeHandler(identity *auth.IdentityService, mailer *mail.Service, system store.SystemSettingsRepository, smsFactory registrationSMSProviderFactory) http.HandlerFunc {
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
			existing, err := lookupEmailIdentityUser(r.Context(), identity, principal.TenantID, email)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "EMAIL_LOOKUP_FAILED", err.Error())
				return
			}
			if existing != nil && existing.ID != user.ID {
				writeError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "Email is already registered")
				return
			}
			if err := identity.CanBindVerifiedEmailToUser(auth.WithTenant(r.Context(), principal.TenantID), user, email); err != nil {
				switch {
				case errors.Is(err, auth.ErrRoutedToAnotherHub):
					writeError(w, http.StatusConflict, "EMAIL_ROUTED_TO_ANOTHER_HUB", err.Error())
					return
				case errors.Is(err, auth.ErrEmailDomainNotAllowed):
					writeError(w, http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", err.Error())
					return
				default:
					writeError(w, http.StatusInternalServerError, "EMAIL_BIND_CHECK_FAILED", err.Error())
					return
				}
			}
			if mailer == nil {
				writeError(w, http.StatusServiceUnavailable, "MAIL_NOT_CONFIGURED", "Mail delivery is not configured")
				return
			}
			code, err := generateVerifyCode()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CODE_GEN_FAILED", "Failed to generate verification code")
				return
			}
			key := registrationContactEmailKey(user.ID, email)
			previousCode := snapshotVerifyCode(principal.TenantID, key)
			if !storeVerifyCode(principal.TenantID, key, code) {
				writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Please wait 60 seconds before requesting a new code")
				return
			}
			body := fmt.Sprintf("您的注册资料验证码是: %s\r\n\r\n验证码 %d 分钟内有效。如非本人操作，请忽略此消息。", code, int(verifyCodeTTL.Minutes()))
			if err := mailer.Send(auth.WithTenant(r.Context(), principal.TenantID), []string{email}, "MaClaw 注册资料验证码", body); err != nil {
				if !rollbackVerifyCode(principal.TenantID, key, code, previousCode) {
					log.Printf("[registration-contact] send code rollback skipped tenant_id=%s user_id=%s email=%s reason=code_replaced", principal.TenantID, user.ID, registrationEmailLogIdentity(email))
				}
				status, deliveryCode := registrationEmailDeliveryError(err)
				writeError(w, status, deliveryCode, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kind": "email", "expires_min": int(verifyCodeTTL.Minutes()), "code_length": 6})
		case "phone":
			forwardRegistrationContactSMS(w, r, RegistrationSMSSendCodeRequest{
				PhoneNumber:  req.PhoneNumber,
				TenantID:     principal.TenantID,
				MachineID:    req.MachineID,
				MachineToken: req.MachineToken,
			}, RegistrationSMSSendCodeHandler(identity, system, smsFactory))
		default:
			writeError(w, http.StatusBadRequest, "INVALID_CONTACT_KIND", "contact kind must be email or phone")
		}
	}
}

func RegistrationContactVerifyHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, smsFactory registrationSMSProviderFactory) http.HandlerFunc {
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
			key := registrationContactEmailKey(user.ID, email)
			// Bind policy and Hub routing can change after the code was delivered.
			// Check them before consuming the one-time code so a recoverable policy
			// correction does not force the user to request a new email.
			if err := identity.CanBindVerifiedEmailToUser(auth.WithTenant(r.Context(), principal.TenantID), user, email); err != nil {
				writeRegistrationContactEmailBindError(w, err)
				return
			}
			valid, locked := consumeVerifyCode(principal.TenantID, key, strings.TrimSpace(req.VerifyCode))
			if !valid {
				if locked {
					writeError(w, http.StatusTooManyRequests, "VERIFY_LOCKED", "Too many attempts. Please request a new code")
				} else {
					writeError(w, http.StatusBadRequest, "INVALID_VERIFY_CODE", "Invalid verification code")
				}
				return
			}
			if err := identity.BindVerifiedEmailToUser(auth.WithTenant(r.Context(), principal.TenantID), user, email); err != nil {
				writeRegistrationContactEmailBindError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kind": "email", "email": email})
		case "phone":
			forwardRegistrationContactSMS(w, r, RegistrationSMSVerifyAndStartRequest{
				PhoneNumber:  req.PhoneNumber,
				VerifyCode:   req.VerifyCode,
				TenantID:     principal.TenantID,
				MachineID:    req.MachineID,
				MachineToken: req.MachineToken,
			}, RegistrationSMSVerifyAndStartHandler(identity, system, smsFactory))
		default:
			writeError(w, http.StatusBadRequest, "INVALID_CONTACT_KIND", "contact kind must be email or phone")
		}
	}
}

func writeRegistrationContactEmailBindError(w http.ResponseWriter, err error) {
	if isRegistrationContactIdentityConflict(err) {
		writeError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "Email is already registered")
		return
	}
	if errors.Is(err, auth.ErrEmailDomainNotAllowed) {
		writeError(w, http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", err.Error())
		return
	}
	if errors.Is(err, auth.ErrRoutedToAnotherHub) {
		writeError(w, http.StatusConflict, "EMAIL_ROUTED_TO_ANOTHER_HUB", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "EMAIL_BIND_FAILED", err.Error())
}

func RegistrationCurrentProfileHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		if identity == nil || identity.UsersRepo() == nil {
			writeError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		user, err := identity.UsersRepo().GetByID(auth.WithTenant(r.Context(), principal.TenantID), principal.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "Current user was not found")
			return
		}
		phoneNumber, err := identity.BoundPhoneNumberForUser(auth.WithTenant(r.Context(), principal.TenantID), user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PHONE_LOOKUP_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"tenant_id":    principal.TenantID,
			"tenant_name":  identity.TenantDisplayName(auth.WithTenant(r.Context(), principal.TenantID), principal.TenantID),
			"user_id":      principal.UserID,
			"machine_id":   principal.MachineID,
			"email":        user.Email,
			"phone_number": phoneNumber,
		})
	}
}

func forwardRegistrationContactSMS(w http.ResponseWriter, r *http.Request, payload any, handler http.HandlerFunc) {
	data, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INVALID_SMS_FORWARD_PAYLOAD", err.Error())
		return
	}
	next := r.Clone(r.Context())
	next.Body = io.NopCloser(bytes.NewReader(data))
	next.ContentLength = int64(len(data))
	next.Header = r.Header.Clone()
	next.Header.Set("Content-Type", "application/json")
	handler(w, next)
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
	if email == "" || strings.ContainsAny(email, "\r\n") {
		return false
	}
	address, err := stdmail.ParseAddress(email)
	if err != nil || !strings.EqualFold(strings.TrimSpace(address.Address), email) {
		return false
	}
	at := strings.LastIndexByte(email, '@')
	return at > 0 && at < len(email)-1 && strings.Contains(email[at+1:], ".")
}

func nowForRegistrationContact() time.Time {
	return time.Now()
}
