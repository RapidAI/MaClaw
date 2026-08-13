package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	registrationAuthConfigKey          = "registration_auth_config"
	registrationAuthMethodEmail        = "email"
	registrationAuthMethodPhone        = "phone"
	registrationAuthMethodMixed        = "mixed"
	registrationAuthAliyunSMSBuyURL    = "https://common-buy.aliyun.com/?commodityCode=dypns_smsverify_public_cn#buy"
	registrationAuthAliyunDypnsAPIName = "Dypnsapi"
	registrationAuthDefaultTemplate    = "100001"
	registrationAuthDefaultSignName    = "速通互联验证平台"
	registrationAuthDefaultTTLMinutes  = 5
	registrationAuthDefaultCodeLength  = 6
	registrationAuthDefaultDailyLimit  = 3
)

type RegistrationAuthConfig struct {
	Method string `json:"method"`
	// EmailVerificationDisabled permits invitation-code registrations to skip
	// the email OTP. It never permits an unauthenticated registration: the
	// regular enrollment path still validates and consumes the invitation code.
	EmailVerificationDisabled bool   `json:"email_verification_disabled,omitempty"`
	AliyunAccessKeyID         string `json:"aliyun_access_key_id,omitempty"`
	AliyunAccessKeySecret     string `json:"aliyun_access_key_secret,omitempty"`
	AliyunSignName            string `json:"aliyun_sign_name,omitempty"`
	AliyunTemplateCode        string `json:"aliyun_template_code,omitempty"`
	CodeTTLMinutes            int    `json:"code_ttl_minutes,omitempty"`
	CodeLength                int    `json:"code_length,omitempty"`
	DailySMSLimit             int    `json:"daily_sms_limit,omitempty"`
	AliyunSMSBuyURL           string `json:"aliyun_sms_buy_url,omitempty"`
	Provider                  string `json:"provider,omitempty"`
}

func (c RegistrationAuthConfig) EmailVerificationRequired() bool {
	return (c.Method == registrationAuthMethodEmail || c.Method == registrationAuthMethodMixed) && !c.EmailVerificationDisabled
}

func GetRegistrationAuthConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if system == nil {
			writeError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_UNAVAILABLE", "System settings are unavailable")
			return
		}
		cfg, err := loadRegistrationAuthConfig(r, scopedSystemSettingsForRequest(r, system))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateRegistrationAuthConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if system == nil {
			writeError(w, http.StatusInternalServerError, "SYSTEM_SETTINGS_UNAVAILABLE", "System settings are unavailable")
			return
		}
		var cfg RegistrationAuthConfig
		if err := decodeRegistrationAuthConfig(r, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		cfg = normalizeRegistrationAuthConfig(cfg)
		if err := validateRegistrationAuthConfig(cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REGISTRATION_AUTH_CONFIG", err.Error())
			return
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_SAVE_FAILED", err.Error())
			return
		}
		if err := scopedSystemSettingsForRequest(r, system).Set(r.Context(), registrationAuthConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

const maxRegistrationAuthConfigBodyBytes = 64 << 10

func decodeRegistrationAuthConfig(r *http.Request, cfg *RegistrationAuthConfig) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRegistrationAuthConfigBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxRegistrationAuthConfigBodyBytes {
		return errInvalidRegistrationAuth("request body exceeds size limit")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(cfg); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errInvalidRegistrationAuth("request body must contain one JSON object")
		}
		return err
	}
	return nil
}

func PublicRegistrationAuthConfigHandler(system store.SystemSettingsRepository, resolvers ...tenantResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var resolver tenantResolver
		if len(resolvers) > 0 {
			resolver = resolvers[0]
		}
		tenantHint := tenantIDFromClientHint(r)
		hasExplicitTenantHint := registrationAuthHasExplicitTenantHint(r)
		tenantID := tenantHint
		emailHint := strings.TrimSpace(r.URL.Query().Get("email"))
		// An explicit tenant hint comes from a HubCenter invitation route. Do
		// not replace it with the email's existing-account route: doing so can
		// advertise one tenant's registration method while the subsequent
		// invitation-bound OTP request is sent to another tenant.
		if emailHint != "" && resolver != nil && !hasExplicitTenantHint {
			if resolved, found, ambiguous, resolveErr := resolver.ResolveTenantByEmail(r.Context(), emailHint); resolveErr != nil {
				log.Printf("[onboarding-auth] public_config_rejected code=REGISTRATION_AUTH_TENANT_LOOKUP_FAILED tenant_hint=%s err=%v", tenantHint, resolveErr)
				writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_TENANT_LOOKUP_FAILED", resolveErr.Error())
				return
			} else if ambiguous {
				log.Printf("[onboarding-auth] public_config_rejected code=TENANT_AMBIGUOUS tenant_hint=%s email=%s", tenantHint, registrationEmailLogIdentity(emailHint))
				writeError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", "email is associated with multiple tenants; tenant_id is required")
				return
			} else if found && strings.TrimSpace(resolved) != "" {
				tenantID = strings.TrimSpace(resolved)
			}
		}
		cfg, err := loadRegistrationAuthConfigForTenant(r, system, tenantID)
		if err != nil {
			log.Printf("[onboarding-auth] public_config_rejected code=REGISTRATION_AUTH_LOAD_FAILED tenant_id=%s err=%v", tenantID, err)
			writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
			return
		}
		if emailHint != "" {
			log.Printf("[onboarding-auth] public_config tenant_hint=%s tenant_id=%s method=%s code_len=%d daily_sms_limit=%d email=%s",
				tenantHint, tenantID, cfg.Method, cfg.CodeLength, cfg.DailySMSLimit, registrationEmailLogIdentity(emailHint))
		} else {
			log.Printf("[onboarding-auth] public_config tenant_hint=%s tenant_id=%s method=%s code_len=%d daily_sms_limit=%d",
				tenantHint, tenantID, cfg.Method, cfg.CodeLength, cfg.DailySMSLimit)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"method":                      cfg.Method,
			"email_verification_required": cfg.EmailVerificationRequired(),
			"code_ttl_minutes":            cfg.CodeTTLMinutes,
			"code_length":                 cfg.CodeLength,
			"daily_sms_limit":             cfg.DailySMSLimit,
			"tenant_id":                   tenantID,
			"provider":                    cfg.Provider,
			"aliyun_sms_buy_url":          cfg.AliyunSMSBuyURL,
		})
	}
}

func registrationAuthHasExplicitTenantHint(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.TrimSpace(r.Header.Get("X-Tenant-ID")) != "" || strings.TrimSpace(r.URL.Query().Get("tenant_id")) != ""
}

func loadRegistrationAuthConfig(r *http.Request, system store.SystemSettingsRepository) (RegistrationAuthConfig, error) {
	cfg := defaultRegistrationAuthConfig()
	if system == nil {
		return cfg, nil
	}
	raw, err := system.Get(r.Context(), registrationAuthConfigKey)
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultRegistrationAuthConfig(), nil
	}
	return normalizeRegistrationAuthConfig(cfg), nil
}

func defaultRegistrationAuthConfig() RegistrationAuthConfig {
	return RegistrationAuthConfig{
		Method:             registrationAuthMethodEmail,
		AliyunSignName:     registrationAuthDefaultSignName,
		AliyunTemplateCode: registrationAuthDefaultTemplate,
		CodeTTLMinutes:     registrationAuthDefaultTTLMinutes,
		CodeLength:         registrationAuthDefaultCodeLength,
		DailySMSLimit:      registrationAuthDefaultDailyLimit,
		AliyunSMSBuyURL:    registrationAuthAliyunSMSBuyURL,
		Provider:           registrationAuthAliyunDypnsAPIName,
	}
}

func normalizeRegistrationAuthConfig(cfg RegistrationAuthConfig) RegistrationAuthConfig {
	cfg.Method = strings.ToLower(strings.TrimSpace(cfg.Method))
	if cfg.Method == "" {
		cfg.Method = registrationAuthMethodEmail
	}
	cfg.AliyunAccessKeyID = strings.TrimSpace(cfg.AliyunAccessKeyID)
	cfg.AliyunAccessKeySecret = strings.TrimSpace(cfg.AliyunAccessKeySecret)
	cfg.AliyunSignName = strings.TrimSpace(cfg.AliyunSignName)
	if cfg.AliyunSignName == "" {
		cfg.AliyunSignName = registrationAuthDefaultSignName
	}
	cfg.AliyunTemplateCode = registrationAuthDefaultTemplate
	if cfg.CodeTTLMinutes <= 0 {
		cfg.CodeTTLMinutes = registrationAuthDefaultTTLMinutes
	}
	if cfg.CodeLength <= 0 {
		cfg.CodeLength = registrationAuthDefaultCodeLength
	}
	if cfg.DailySMSLimit == 0 {
		cfg.DailySMSLimit = registrationAuthDefaultDailyLimit
	}
	cfg.AliyunSMSBuyURL = registrationAuthAliyunSMSBuyURL
	cfg.Provider = registrationAuthAliyunDypnsAPIName
	return cfg
}

func validateRegistrationAuthConfig(cfg RegistrationAuthConfig) error {
	switch cfg.Method {
	case registrationAuthMethodEmail:
		return nil
	case registrationAuthMethodPhone, registrationAuthMethodMixed:
		if cfg.AliyunAccessKeyID == "" || cfg.AliyunAccessKeySecret == "" {
			return errInvalidRegistrationAuth("Aliyun AccessKey ID and AccessKey Secret are required when phone registration is enabled")
		}
		if cfg.AliyunSignName == "" {
			return errInvalidRegistrationAuth("Aliyun SignName is required when phone registration is enabled")
		}
		if cfg.CodeTTLMinutes < 1 || cfg.CodeTTLMinutes > 30 {
			return errInvalidRegistrationAuth("verification code TTL must be between 1 and 30 minutes")
		}
		if cfg.CodeLength < 4 || cfg.CodeLength > 8 {
			return errInvalidRegistrationAuth("verification code length must be between 4 and 8 digits")
		}
		if cfg.DailySMSLimit < 1 || cfg.DailySMSLimit > 50 {
			return errInvalidRegistrationAuth("daily SMS limit must be between 1 and 50")
		}
		return nil
	default:
		return errInvalidRegistrationAuth("registration auth method must be email, phone, or mixed")
	}
}

type errInvalidRegistrationAuth string

func (e errInvalidRegistrationAuth) Error() string {
	return string(e)
}
