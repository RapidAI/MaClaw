package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	registrationAuthConfigKey          = "registration_auth_config"
	registrationAuthMethodEmail        = "email"
	registrationAuthMethodPhone        = "phone"
	registrationAuthAliyunSMSBuyURL    = "https://common-buy.aliyun.com/?commodityCode=dypns_smsverify_public_cn#buy"
	registrationAuthAliyunDypnsAPIName = "Dypnsapi"
	registrationAuthDefaultTemplate    = "100001"
	registrationAuthDefaultSignName    = "速通互联验证平台"
	registrationAuthDefaultTTLMinutes  = 5
	registrationAuthDefaultCodeLength  = 4
	registrationAuthDefaultDailyLimit  = 3
)

type RegistrationAuthConfig struct {
	Method                string `json:"method"`
	AliyunAccessKeyID     string `json:"aliyun_access_key_id,omitempty"`
	AliyunAccessKeySecret string `json:"aliyun_access_key_secret,omitempty"`
	AliyunSignName        string `json:"aliyun_sign_name,omitempty"`
	AliyunTemplateCode    string `json:"aliyun_template_code,omitempty"`
	CodeTTLMinutes        int    `json:"code_ttl_minutes,omitempty"`
	CodeLength            int    `json:"code_length,omitempty"`
	DailySMSLimit         int    `json:"daily_sms_limit,omitempty"`
	AliyunSMSBuyURL       string `json:"aliyun_sms_buy_url,omitempty"`
	Provider              string `json:"provider,omitempty"`
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
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
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

func PublicRegistrationAuthConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, err := loadRegistrationAuthConfigForTenant(r, system, tenantIDFromClientHint(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "REGISTRATION_AUTH_LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"method":             cfg.Method,
			"code_ttl_minutes":   cfg.CodeTTLMinutes,
			"code_length":        cfg.CodeLength,
			"daily_sms_limit":    cfg.DailySMSLimit,
			"provider":           cfg.Provider,
			"aliyun_sms_buy_url": cfg.AliyunSMSBuyURL,
		})
	}
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
	if cfg.DailySMSLimit <= 0 {
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
	case registrationAuthMethodPhone:
		if cfg.AliyunAccessKeyID == "" || cfg.AliyunAccessKeySecret == "" {
			return errInvalidRegistrationAuth("Aliyun AccessKey ID and AccessKey Secret are required for phone registration")
		}
		if cfg.AliyunSignName == "" {
			return errInvalidRegistrationAuth("Aliyun SignName is required for phone registration")
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
		return errInvalidRegistrationAuth("registration auth method must be email or phone")
	}
}

type errInvalidRegistrationAuth string

func (e errInvalidRegistrationAuth) Error() string {
	return string(e)
}
