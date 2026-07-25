package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type registrationSMSProvider interface {
	SendVerifyCode(ctx context.Context, req aliyunSMSVerifyCodeSendRequest) error
	CheckVerifyCode(ctx context.Context, req aliyunSMSVerifyCodeCheckRequest) (bool, error)
}

type registrationSMSProviderFactory func(RegistrationAuthConfig) registrationSMSProvider

const registrationSMSDailyUsageKey = "registration_sms_daily_usage"

var registrationSMSDailyUsageMu sync.Mutex

type RegistrationSMSSendCodeRequest struct {
	PhoneNumber  string `json:"phone_number"`
	TenantID     string `json:"tenant_id,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
}

type RegistrationSMSVerifyAndStartRequest struct {
	PhoneNumber          string `json:"phone_number"`
	VerifyCode           string `json:"verify_code"`
	MachineName          string `json:"machine_name"`
	Platform             string `json:"platform"`
	Hostname             string `json:"hostname"`
	Arch                 string `json:"arch"`
	AppVersion           string `json:"app_version"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
	ClientID             string `json:"client_id"`
	InvitationCode       string `json:"invitation_code"`
	TenantID             string `json:"tenant_id,omitempty"`
	MachineID            string `json:"machine_id,omitempty"`
	MachineToken         string `json:"machine_token,omitempty"`
	GroupID              string `json:"group_id"`
	Language             string `json:"language,omitempty"`
}

func RegistrationSMSSendCodeHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory) http.HandlerFunc {
	return registrationSMSSendCodeHandler(identity, system, factory, false)
}

// MobileRegistrationSMSSendCodeHandler keeps the public mobile phone-login API
// available when desktop/web onboarding uses email verification. The endpoint
// selects a product capability; it does not by itself attest the calling app.
func MobileRegistrationSMSSendCodeHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory) http.HandlerFunc {
	return registrationSMSSendCodeHandler(identity, system, factory, true)
}

func registrationSMSSendCodeHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory, allowPhoneWhenEmail bool) http.HandlerFunc {
	if factory == nil {
		factory = aliyunDypnsProviderForRegistration
	}
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		var req RegistrationSMSSendCodeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			log.Printf("[onboarding-sms] send_code_rejected code=INVALID_JSON allow_phone_when_email=%t err=%v", allowPhoneWhenEmail, err)
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		phoneLog := registrationPhoneLogIdentity(req.PhoneNumber)
		tenantHint := strings.TrimSpace(req.TenantID)
		tenantID := tenantIDForSMSRegistration(r, req.TenantID)
		hasMachine := strings.TrimSpace(req.MachineID) != "" || strings.TrimSpace(req.MachineToken) != ""
		log.Printf("[onboarding-sms] send_code_begin phone=%s tenant_hint=%s tenant_id=%s allow_phone_when_email=%t machine=%t", phoneLog, tenantHint, tenantID, allowPhoneWhenEmail, hasMachine)
		var currentUser *store.User
		if hasMachine {
			principal, user, ok := authenticateRegistrationContactUser(w, r, identity, tenantID, req.MachineID, req.MachineToken)
			if !ok {
				log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=MACHINE_UNAUTHORIZED elapsed=%s", phoneLog, tenantID, time.Since(startedAt))
				return
			}
			tenantID = principal.TenantID
			currentUser = user
		}
		cfg, err := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		if err != nil {
			log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=REGISTRATION_AUTH_LOAD_FAILED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
			return
		}
		storedMethod := cfg.Method
		cfg, ok := registrationSMSEffectiveAuthConfig(cfg, allowPhoneWhenEmail)
		if !ok {
			log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=PHONE_REGISTRATION_DISABLED stored_method=%s allow_phone_when_email=%t elapsed=%s", phoneLog, tenantID, storedMethod, allowPhoneWhenEmail, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "PHONE_REGISTRATION_DISABLED", "Phone registration is not enabled")
			return
		}
		phoneNumber := normalizePhoneNumber(req.PhoneNumber)
		phoneLog = registrationPhoneLogIdentity(phoneNumber)
		phoneIdentity, err := phoneRegistrationIdentity(phoneNumber)
		if err != nil {
			log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=INVALID_PHONE_NUMBER stored_method=%s err=%v elapsed=%s", phoneLog, tenantID, storedMethod, err, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", err.Error())
			return
		}
		existingUser, err := lookupPhoneIdentityUser(r.Context(), identity, tenantID, phoneNumber)
		if err != nil {
			log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=PHONE_REGISTRATION_LOOKUP_FAILED stored_method=%s err=%v elapsed=%s", phoneLog, tenantID, storedMethod, err, time.Since(startedAt))
			writePhoneRegistrationLookupError(w, errPhoneRegistrationLookup{err: err})
			return
		}
		if currentUser != nil && existingUser != nil && existingUser.ID != currentUser.ID && !canClaimPhoneIdentityForCurrentUser(existingUser, currentUser, phoneIdentity) {
			log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=PHONE_ALREADY_REGISTERED existing_user=%s current_user=%s elapsed=%s", phoneLog, tenantID, existingUser.ID, currentUser.ID, time.Since(startedAt))
			writeError(w, http.StatusConflict, "PHONE_ALREADY_REGISTERED", "Phone number is already registered")
			return
		}
		business := registrationSMSBusinessRegister
		if existingUser != nil {
			business = registrationSMSBusinessVerifyBoundPhone
		} else if err := ensurePhoneIdentityCanRegister(r.Context(), identity, tenantID, phoneIdentity, currentUser); err != nil {
			code := registrationPhoneRejectCode(err)
			log.Printf("[onboarding-sms] send_code_rejected phone=%s account=%s tenant_id=%s code=%s stored_method=%s err=%v elapsed=%s", phoneLog, registrationAccountLogIdentity(phoneIdentity), tenantID, code, storedMethod, err, time.Since(startedAt))
			writePhoneRegistrationLookupError(w, err)
			return
		}
		smsReq, err := buildAliyunSMSVerifyCodeSendRequest(cfg, business, phoneNumber)
		if err != nil {
			log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=INVALID_SMS_VERIFY_REQUEST err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "INVALID_SMS_VERIFY_REQUEST", err.Error())
			return
		}
		tenantSystem := ScopedSystemSettingsForTenant(tenantID, system)
		usageNow := time.Now()
		remaining, err := reserveRegistrationSMSSend(r.Context(), tenantSystem, phoneNumber, cfg.DailySMSLimit, usageNow)
		if err != nil {
			if limitErr, ok := err.(errRegistrationSMSDailyLimit); ok {
				log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=SMS_DAILY_LIMIT_REACHED limit=%d elapsed=%s", phoneLog, tenantID, limitErr.Limit, time.Since(startedAt))
				writeError(w, http.StatusTooManyRequests, "SMS_DAILY_LIMIT_REACHED", limitErr.Error())
				return
			}
			log.Printf("[onboarding-sms] send_code_rejected phone=%s tenant_id=%s code=SMS_DAILY_LIMIT_CHECK_FAILED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writeError(w, http.StatusInternalServerError, "SMS_DAILY_LIMIT_CHECK_FAILED", err.Error())
			return
		}
		if err := factory(cfg).SendVerifyCode(r.Context(), smsReq); err != nil {
			_ = releaseRegistrationSMSSend(r.Context(), tenantSystem, phoneNumber, usageNow)
			log.Printf("[onboarding-sms] send_code_failed phone=%s tenant_id=%s purpose=%s stored_method=%s err=%v elapsed=%s", phoneLog, tenantID, business, storedMethod, err, time.Since(startedAt))
			writeError(w, http.StatusBadGateway, "SMS_VERIFY_SEND_FAILED", err.Error())
			return
		}
		existing := existingUser != nil
		log.Printf("[onboarding-sms] send_code_succeeded phone=%s tenant_id=%s purpose=%s stored_method=%s existing_user=%t remaining=%d elapsed=%s", phoneLog, tenantID, business, storedMethod, existing, remaining, time.Since(startedAt))
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                      true,
			"tenant_id":               tenantID,
			"expires_min":             cfg.CodeTTLMinutes,
			"code_length":             cfg.CodeLength,
			"purpose":                 business,
			"daily_sms_limit":         cfg.DailySMSLimit,
			"daily_sms_remaining":     remaining,
			"resend_cooldown_seconds": registrationSMSResendCooldownSeconds,
		})
	}
}

func RegistrationSMSVerifyAndStartHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory) http.HandlerFunc {
	return registrationSMSVerifyAndStartHandler(identity, system, factory, false)
}

// MobileRegistrationSMSVerifyAndStartHandler verifies the code issued by the
// mobile phone-login API. Its authorization decision deliberately does not
// depend on the forgeable client_id request field.
func MobileRegistrationSMSVerifyAndStartHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory) http.HandlerFunc {
	return registrationSMSVerifyAndStartHandler(identity, system, factory, true)
}

func registrationSMSVerifyAndStartHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, factory registrationSMSProviderFactory, allowPhoneWhenEmail bool) http.HandlerFunc {
	if factory == nil {
		factory = aliyunDypnsProviderForRegistration
	}
	return func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		var req RegistrationSMSVerifyAndStartRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			log.Printf("[onboarding-sms] verify_rejected code=INVALID_JSON allow_phone_when_email=%t err=%v", allowPhoneWhenEmail, err)
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if identity == nil {
			log.Printf("[onboarding-sms] verify_rejected code=IDENTITY_UNAVAILABLE")
			writeError(w, http.StatusInternalServerError, "IDENTITY_UNAVAILABLE", "Identity service is unavailable")
			return
		}
		phoneLog := registrationPhoneLogIdentity(req.PhoneNumber)
		tenantHint := strings.TrimSpace(req.TenantID)
		tenantID := tenantIDForSMSRegistration(r, req.TenantID)
		clientID := strings.TrimSpace(req.ClientID)
		hasMachine := strings.TrimSpace(req.MachineID) != "" || strings.TrimSpace(req.MachineToken) != ""
		log.Printf("[onboarding-sms] verify_begin phone=%s tenant_hint=%s tenant_id=%s client_id=%s machine_name=%q allow_phone_when_email=%t machine=%t", phoneLog, tenantHint, tenantID, clientID, strings.TrimSpace(req.MachineName), allowPhoneWhenEmail, hasMachine)
		var currentPrincipal *auth.MachinePrincipal
		var currentUser *store.User
		if hasMachine {
			principal, user, ok := authenticateRegistrationContactUser(w, r, identity, tenantID, req.MachineID, req.MachineToken)
			if !ok {
				log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=MACHINE_UNAUTHORIZED elapsed=%s", phoneLog, tenantID, time.Since(startedAt))
				return
			}
			currentPrincipal = principal
			currentUser = user
			tenantID = principal.TenantID
		}
		cfg, err := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		if err != nil {
			log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=REGISTRATION_AUTH_LOAD_FAILED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
			return
		}
		storedMethod := cfg.Method
		cfg, ok := registrationSMSEffectiveAuthConfig(cfg, allowPhoneWhenEmail)
		if !ok {
			log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=PHONE_REGISTRATION_DISABLED stored_method=%s allow_phone_when_email=%t elapsed=%s", phoneLog, tenantID, storedMethod, allowPhoneWhenEmail, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "PHONE_REGISTRATION_DISABLED", "Phone registration is not enabled")
			return
		}
		phoneNumber := normalizePhoneNumber(req.PhoneNumber)
		phoneLog = registrationPhoneLogIdentity(phoneNumber)
		phoneIdentity, err := phoneRegistrationIdentity(phoneNumber)
		if err != nil {
			log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=INVALID_PHONE_NUMBER stored_method=%s err=%v elapsed=%s", phoneLog, tenantID, storedMethod, err, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "INVALID_PHONE_NUMBER", err.Error())
			return
		}
		existingUser, err := lookupPhoneIdentityUser(r.Context(), identity, tenantID, phoneNumber)
		if err != nil {
			log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=PHONE_REGISTRATION_LOOKUP_FAILED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writePhoneRegistrationLookupError(w, errPhoneRegistrationLookup{err: err})
			return
		}
		if existingUser == nil {
			if err := ensurePhoneIdentityCanRegister(r.Context(), identity, tenantID, phoneIdentity, currentUser); err != nil {
				code := registrationPhoneRejectCode(err)
				log.Printf("[onboarding-sms] verify_rejected phone=%s account=%s tenant_id=%s code=%s stored_method=%s err=%v elapsed=%s", phoneLog, registrationAccountLogIdentity(phoneIdentity), tenantID, code, storedMethod, err, time.Since(startedAt))
				writePhoneRegistrationLookupError(w, err)
				return
			}
		}
		checkReq, err := buildAliyunSMSVerifyCodeCheckRequest(phoneNumber, req.VerifyCode, cfg.CodeLength)
		if err != nil {
			log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=INVALID_SMS_VERIFY_REQUEST err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "INVALID_SMS_VERIFY_REQUEST", err.Error())
			return
		}
		verified, err := factory(cfg).CheckVerifyCode(r.Context(), checkReq)
		if err != nil {
			log.Printf("[onboarding-sms] verify_failed phone=%s tenant_id=%s code=SMS_VERIFY_CHECK_FAILED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
			writeError(w, http.StatusBadGateway, "SMS_VERIFY_CHECK_FAILED", err.Error())
			return
		}
		if !verified {
			log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=INVALID_SMS_VERIFY_CODE stored_method=%s elapsed=%s", phoneLog, tenantID, storedMethod, time.Since(startedAt))
			writeError(w, http.StatusBadRequest, "INVALID_SMS_VERIFY_CODE", "Invalid SMS verification code")
			return
		}
		if currentUser != nil {
			if existingUser != nil && existingUser.ID != currentUser.ID && !canClaimPhoneIdentityForCurrentUser(existingUser, currentUser, phoneIdentity) {
				log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=PHONE_ALREADY_REGISTERED existing_user=%s current_user=%s elapsed=%s", phoneLog, tenantID, existingUser.ID, currentUser.ID, time.Since(startedAt))
				writeError(w, http.StatusConflict, "PHONE_ALREADY_REGISTERED", "Phone number is already registered")
				return
			}
			ctx := auth.WithTenant(r.Context(), currentPrincipal.TenantID)
			if err := identity.BindVerifiedPhoneToUser(ctx, currentUser, phoneNumber); err != nil {
				if isRegistrationContactIdentityConflict(err) {
					log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=PHONE_ALREADY_REGISTERED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
					writeError(w, http.StatusConflict, "PHONE_ALREADY_REGISTERED", "Phone number is already registered")
					return
				}
				log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=PHONE_BIND_FAILED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
				writeError(w, http.StatusInternalServerError, "PHONE_BIND_FAILED", err.Error())
				return
			}
			tenantSystem := ScopedSystemSettingsForTenant(currentPrincipal.TenantID, system)
			if changed, err := llmservice.BackfillRegistryUserIDs(ctx, tenantSystem, identity.UsersRepo(), currentPrincipal.TenantID); err != nil {
				log.Printf("[registration-sms] tenant-scoped LLM registry backfill after profile phone bind failed for tenant=%s user=%s: %v", currentPrincipal.TenantID, currentUser.ID, err)
			} else if changed {
				invalidateLLMRuntimeCaches(tenantSystem)
			}
			log.Printf("[onboarding-sms] verify_succeeded phone=%s tenant_id=%s kind=bind_profile user_id=%s stored_method=%s elapsed=%s", phoneLog, currentPrincipal.TenantID, currentUser.ID, storedMethod, time.Since(startedAt))
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":              true,
				"kind":            "phone",
				"tenant_id":       currentPrincipal.TenantID,
				"phone_number":    phoneNumber,
				"credits_account": phoneIdentity,
			})
			return
		}
		ctx := auth.WithTenant(r.Context(), tenantID)
		enrollOpts := []auth.EnrollOption{auth.WithPhoneVerifiedRegistration()}
		if lang := strings.TrimSpace(req.Language); lang != "" {
			enrollOpts = append(enrollOpts, auth.WithLanguage(lang))
		} else if acceptLang := r.Header.Get("Accept-Language"); strings.Contains(acceptLang, "zh") {
			enrollOpts = append(enrollOpts, auth.WithLanguage("zh"))
		} else if acceptLang != "" {
			enrollOpts = append(enrollOpts, auth.WithLanguage("en"))
		}
		enrollAccount := phoneIdentity
		if existingUser != nil {
			enrollAccount = existingUser.Email
			if err := identity.BindVerifiedPhoneToUser(ctx, existingUser, req.PhoneNumber); err != nil {
				log.Printf("[onboarding-sms] verify_rejected phone=%s tenant_id=%s code=PHONE_BIND_FAILED err=%v elapsed=%s", phoneLog, tenantID, err, time.Since(startedAt))
				writeError(w, http.StatusInternalServerError, "PHONE_BIND_FAILED", err.Error())
				return
			}
			tenantSystem := ScopedSystemSettingsForTenant(tenantID, system)
			if changed, err := llmservice.BackfillRegistryUserIDs(ctx, tenantSystem, identity.UsersRepo(), tenantID); err != nil {
				log.Printf("[registration-sms] tenant-scoped LLM registry backfill after phone bind failed for tenant=%s user=%s: %v", tenantID, existingUser.ID, err)
			} else if changed {
				invalidateLLMRuntimeCaches(tenantSystem)
			}
		}
		accountLog := registrationAccountLogIdentity(enrollAccount)
		resp, err := identity.StartEnrollment(ctx, enrollAccount, req.MachineName, req.Platform, req.ClientID, req.InvitationCode, enrollOpts...)
		if err != nil {
			log.Printf("[onboarding-sms] enroll_failed phone=%s tenant_id=%s account=%s stored_method=%s err=%v elapsed=%s", phoneLog, tenantID, accountLog, storedMethod, err, time.Since(startedAt))
			writeEnrollmentStartError(w, err, resp)
			return
		}
		respMap := enrollmentStartResponseMap(resp)
		respMap["phone_number"] = phoneNumber
		respMap["credits_account"] = phoneIdentity
		if existingUser != nil {
			respMap["rebound_existing_user"] = true
		}
		status := ""
		if resp != nil {
			status = resp.Status
		}
		log.Printf("[onboarding-sms] verify_succeeded phone=%s tenant_id=%s kind=enroll status=%s account=%s stored_method=%s rebound=%t elapsed=%s", phoneLog, tenantID, status, accountLog, storedMethod, existingUser != nil, time.Since(startedAt))
		writeJSON(w, http.StatusOK, respMap)
	}
}

func aliyunDypnsProviderForRegistration(cfg RegistrationAuthConfig) registrationSMSProvider {
	return aliyunDypnsClient{
		AccessKeyID:     cfg.AliyunAccessKeyID,
		AccessKeySecret: cfg.AliyunAccessKeySecret,
	}
}

// registrationSMSEffectiveAuthConfig makes an email-first Hub usable by the
// separate mobile phone-login capability without relaxing desktop/web rules.
// The SMS provider credentials are still required and validated when a code is
// sent; a Hub with no configured SMS provider continues to fail safely.
func registrationSMSEffectiveAuthConfig(cfg RegistrationAuthConfig, allowPhoneWhenEmail bool) (RegistrationAuthConfig, bool) {
	if cfg.Method == registrationAuthMethodPhone || cfg.Method == registrationAuthMethodMixed {
		// The SMS request builder validates credentials as a phone flow while
		// preserving the stored mixed registration policy for public clients.
		cfg.Method = registrationAuthMethodPhone
		return cfg, true
	}
	if allowPhoneWhenEmail && cfg.Method == registrationAuthMethodEmail {
		cfg.Method = registrationAuthMethodPhone
		return cfg, true
	}
	return cfg, false
}

func lookupPhoneIdentityUser(ctx context.Context, identity *auth.IdentityService, tenantID, phoneNumber string) (*store.User, error) {
	if identity == nil {
		return nil, nil
	}
	return identity.LookupUserByPhone(auth.WithTenant(ctx, tenantID), phoneNumber)
}

func loadRegistrationAuthConfigForTenant(r *http.Request, system store.SystemSettingsRepository, tenantID string) (RegistrationAuthConfig, error) {
	return loadRegistrationAuthConfig(r, ScopedSystemSettingsForTenant(tenantID, system))
}

func tenantIDForSMSRegistration(r *http.Request, requestTenantID string) string {
	if tenantID := strings.TrimSpace(requestTenantID); tenantID != "" {
		return tenantID
	}
	return tenantIDFromClientHint(r)
}

type registrationSMSDailyUsage struct {
	Date   string         `json:"date"`
	Counts map[string]int `json:"counts"`
}

type errRegistrationSMSDailyLimit struct {
	Limit int
}

func (e errRegistrationSMSDailyLimit) Error() string {
	if e.Limit > 0 {
		return "daily SMS verification limit reached; max " + strconv.Itoa(e.Limit) + " per day"
	}
	return "daily SMS verification limit reached"
}

func reserveRegistrationSMSSend(ctx context.Context, system store.SystemSettingsRepository, phoneNumber string, limit int, now time.Time) (int, error) {
	if limit <= 0 {
		limit = registrationAuthDefaultDailyLimit
	}
	if system == nil {
		return limit, nil
	}
	phoneNumber = normalizePhoneNumber(phoneNumber)
	if phoneNumber == "" {
		return 0, errors.New("valid phone number is required")
	}
	registrationSMSDailyUsageMu.Lock()
	defer registrationSMSDailyUsageMu.Unlock()
	usage, err := loadRegistrationSMSDailyUsage(ctx, system, now)
	if err != nil {
		return 0, err
	}
	phoneKey := registrationSMSDailyUsagePhoneHash(phoneNumber)
	count := usage.Counts[phoneKey]
	if count >= limit {
		_ = saveRegistrationSMSDailyUsage(ctx, system, usage)
		return 0, errRegistrationSMSDailyLimit{Limit: limit}
	}
	count++
	usage.Counts[phoneKey] = count
	if err := saveRegistrationSMSDailyUsage(ctx, system, usage); err != nil {
		return 0, err
	}
	return limit - count, nil
}

func releaseRegistrationSMSSend(ctx context.Context, system store.SystemSettingsRepository, phoneNumber string, now time.Time) error {
	if system == nil {
		return nil
	}
	phoneNumber = normalizePhoneNumber(phoneNumber)
	if phoneNumber == "" {
		return nil
	}
	registrationSMSDailyUsageMu.Lock()
	defer registrationSMSDailyUsageMu.Unlock()
	usage, err := loadRegistrationSMSDailyUsage(ctx, system, now)
	if err != nil {
		return err
	}
	phoneKey := registrationSMSDailyUsagePhoneHash(phoneNumber)
	if usage.Counts[phoneKey] > 0 {
		usage.Counts[phoneKey]--
	}
	if usage.Counts[phoneKey] <= 0 {
		delete(usage.Counts, phoneKey)
	}
	return saveRegistrationSMSDailyUsage(ctx, system, usage)
}

func loadRegistrationSMSDailyUsage(ctx context.Context, system store.SystemSettingsRepository, now time.Time) (registrationSMSDailyUsage, error) {
	date := now.Format("2006-01-02")
	usage := registrationSMSDailyUsage{Date: date, Counts: map[string]int{}}
	raw, err := system.Get(ctx, registrationSMSDailyUsageKey)
	if err != nil {
		return usage, err
	}
	if strings.TrimSpace(raw) == "" {
		return usage, nil
	}
	var stored registrationSMSDailyUsage
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return usage, nil
	}
	if stored.Date != date {
		return usage, nil
	}
	if stored.Counts == nil {
		stored.Counts = map[string]int{}
	}
	stored.Counts = normalizeRegistrationSMSDailyUsageCounts(stored.Counts)
	return stored, nil
}

func saveRegistrationSMSDailyUsage(ctx context.Context, system store.SystemSettingsRepository, usage registrationSMSDailyUsage) error {
	if usage.Counts == nil {
		usage.Counts = map[string]int{}
	}
	data, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	return system.Set(ctx, registrationSMSDailyUsageKey, string(data))
}

func normalizeRegistrationSMSDailyUsageCounts(counts map[string]int) map[string]int {
	out := map[string]int{}
	for key, count := range counts {
		if count <= 0 {
			continue
		}
		normalizedKey := normalizeRegistrationSMSDailyUsageKey(key)
		if normalizedKey == "" {
			continue
		}
		out[normalizedKey] += count
	}
	return out
}

func registrationSMSDailyUsagePhoneHash(phoneNumber string) string {
	phoneNumber = normalizePhoneNumber(phoneNumber)
	if phoneNumber == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(phoneNumber))
	return hex.EncodeToString(sum[:])
}

func normalizeRegistrationSMSDailyUsageKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if len(key) == 64 && isHexString(key) {
		return key
	}
	return registrationSMSDailyUsagePhoneHash(key)
}

func isHexString(value string) bool {
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return value != ""
}

func phoneRegistrationIdentity(phoneNumber string) (string, error) {
	phoneNumber = normalizePhoneNumber(phoneNumber)
	if !validRegistrationPhoneNumber(phoneNumber) {
		return "", errors.New("valid phone number is required")
	}
	return "phone:" + phoneNumber, nil
}

func phoneIdentityExistsInHub(ctx context.Context, users store.UserRepository, phoneIdentity string) (*store.User, error) {
	if users == nil {
		return nil, nil
	}
	phoneIdentity = strings.TrimSpace(strings.ToLower(phoneIdentity))
	if phoneIdentity == "" {
		return nil, nil
	}
	items, err := users.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, user := range items {
		if user == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(user.Email), phoneIdentity) {
			return user, nil
		}
	}
	return nil, nil
}

func isClaimablePhoneIdentityUser(user *store.User, phoneIdentity string) bool {
	if user == nil {
		return false
	}
	email := strings.TrimSpace(user.Email)
	if strings.EqualFold(email, strings.TrimSpace(phoneIdentity)) {
		return true
	}
	return email == "" || !strings.Contains(strings.ToLower(email), "@")
}

func canClaimPhoneIdentityForCurrentUser(existing, currentUser *store.User, phoneIdentity string) bool {
	if existing == nil || currentUser == nil {
		return false
	}
	if store.NormalizeTenantID(existing.TenantID) != store.NormalizeTenantID(currentUser.TenantID) {
		return false
	}
	existingEmail := strings.TrimSpace(strings.ToLower(existing.Email))
	currentEmail := strings.TrimSpace(strings.ToLower(currentUser.Email))
	if existingEmail != "" && currentEmail != "" && existingEmail == currentEmail {
		return true
	}
	return isClaimablePhoneIdentityUser(existing, phoneIdentity)
}

func ensurePhoneIdentityCanRegister(ctx context.Context, identity *auth.IdentityService, tenantID, phoneIdentity string, currentUser *store.User) error {
	if identity == nil {
		return nil
	}
	if identity.UsersRepo() != nil {
		existing, err := phoneIdentityExistsInHub(ctx, identity.UsersRepo(), phoneIdentity)
		if err != nil {
			return errPhoneRegistrationLookup{err: err}
		}
		if existing != nil {
			if !canClaimPhoneIdentityForCurrentUser(existing, currentUser, phoneIdentity) {
				return errPhoneAlreadyRegistered{}
			}
			return nil
		}
	}
	if err := identity.CanRegisterUserRoute(auth.WithTenant(ctx, tenantID), phoneIdentity); err != nil {
		if errors.Is(err, auth.ErrRoutedToAnotherHub) {
			return errPhoneAlreadyRegistered{}
		}
		return errPhoneRegistrationRouteCheck{err: err}
	}
	return nil
}

type errPhoneAlreadyRegistered struct{}

func (errPhoneAlreadyRegistered) Error() string {
	return "phone number is already registered"
}

type errPhoneRegistrationLookup struct {
	err error
}

func (e errPhoneRegistrationLookup) Error() string {
	return e.err.Error()
}

func (e errPhoneRegistrationLookup) Unwrap() error {
	return e.err
}

type errPhoneRegistrationRouteCheck struct {
	err error
}

func (e errPhoneRegistrationRouteCheck) Error() string {
	return e.err.Error()
}

func (e errPhoneRegistrationRouteCheck) Unwrap() error {
	return e.err
}

func writePhoneRegistrationLookupError(w http.ResponseWriter, err error) {
	switch err.(type) {
	case errPhoneAlreadyRegistered:
		writeError(w, http.StatusConflict, "PHONE_ALREADY_REGISTERED", "Phone number is already registered")
	case errPhoneRegistrationLookup:
		writeError(w, http.StatusInternalServerError, "PHONE_REGISTRATION_LOOKUP_FAILED", err.Error())
	case errPhoneRegistrationRouteCheck:
		writeError(w, http.StatusInternalServerError, "PHONE_REGISTRATION_ROUTE_CHECK_FAILED", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "PHONE_REGISTRATION_LOOKUP_FAILED", err.Error())
	}
}

func normalizePhoneNumber(phoneNumber string) string {
	phoneNumber = strings.TrimSpace(phoneNumber)
	var b strings.Builder
	for _, r := range phoneNumber {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validRegistrationPhoneNumber(phoneNumber string) bool {
	phoneNumber = normalizePhoneNumber(phoneNumber)
	return len(phoneNumber) >= 6 && len(phoneNumber) <= 20
}

func enrollmentStartResponseMap(resp *auth.EnrollmentResult) map[string]any {
	respMap := map[string]any{
		"brand": brand.Current().DisplayName,
	}
	if resp == nil {
		return respMap
	}
	respMap["status"] = resp.Status
	if resp.TenantID != "" {
		respMap["tenant_id"] = resp.TenantID
	}
	if resp.TenantName != "" {
		respMap["tenant_name"] = resp.TenantName
	}
	if resp.Message != "" {
		respMap["message"] = resp.Message
	}
	if resp.UserID != "" {
		respMap["user_id"] = resp.UserID
	}
	if resp.Email != "" {
		respMap["email"] = resp.Email
	}
	if resp.SN != "" {
		respMap["sn"] = resp.SN
	}
	if resp.MachineID != "" {
		respMap["machine_id"] = resp.MachineID
	}
	if resp.MachineToken != "" {
		respMap["machine_token"] = resp.MachineToken
	}
	if resp.ViewerToken != "" {
		respMap["viewer_token"] = resp.ViewerToken
	}
	if resp.ExpiresAt != "" {
		respMap["expires_at"] = resp.ExpiresAt
	}
	return respMap
}

func writeEnrollmentStartError(w http.ResponseWriter, err error, resp *auth.EnrollmentResult) {
	switch {
	case errors.Is(err, auth.ErrRoutedToAnotherHub):
		writeError(w, http.StatusConflict, "PHONE_ALREADY_REGISTERED", "Phone number is already registered")
	case errors.Is(err, auth.ErrRegistrationDisabled):
		writeError(w, http.StatusForbidden, "REGISTRATION_DISABLED", err.Error())
	case errors.Is(err, auth.ErrEmailDomainNotAllowed):
		writeError(w, http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", err.Error())
	case errors.Is(err, auth.ErrInvitationExpired):
		errResp := map[string]any{
			"ok":      false,
			"code":    "INVITATION_EXPIRED",
			"message": err.Error(),
		}
		if resp != nil && resp.ExpiresAt != "" {
			errResp["expires_at"] = resp.ExpiresAt
		}
		writeJSON(w, http.StatusForbidden, errResp)
	case errors.Is(err, auth.ErrInvitationCodeRequired):
		writeError(w, http.StatusBadRequest, "INVITATION_CODE_REQUIRED", err.Error())
	case errors.Is(err, auth.ErrInvalidInvitationCode):
		writeError(w, http.StatusBadRequest, "INVALID_INVITATION_CODE", err.Error())
	case errors.Is(err, auth.ErrEmailBlocked):
		writeError(w, http.StatusForbidden, "EMAIL_BLOCKED", err.Error())
	case errors.Is(err, auth.ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, "INVALID_PHONE_IDENTITY", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "ENROLL_FAILED", err.Error())
	}
}
