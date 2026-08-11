package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RemoteActivationResult struct {
	Status              string `json:"status"`
	HubID               string `json:"hub_id,omitempty"`
	TenantID            string `json:"tenant_id,omitempty"`
	TenantName          string `json:"tenant_name,omitempty"`
	Message             string `json:"message,omitempty"`
	Code                string `json:"code,omitempty"`
	UserID              string `json:"user_id,omitempty"`
	Email               string `json:"email,omitempty"`
	PhoneNumber         string `json:"phone_number,omitempty"`
	SN                  string `json:"sn,omitempty"`
	MachineID           string `json:"machine_id,omitempty"`
	MachineToken        string `json:"machine_token,omitempty"`
	ViewerToken         string `json:"viewer_token,omitempty"`
	ExpiresAt           string `json:"expires_at,omitempty"`
	VIPFlag             bool   `json:"vip_flag,omitempty"`
	ReboundExistingUser bool   `json:"rebound_existing_user,omitempty"`
}

type RemoteRegistrationAuthResult struct {
	Method         string `json:"method"`
	TenantID       string `json:"tenant_id,omitempty"`
	CodeTTLMinutes int    `json:"code_ttl_minutes,omitempty"`
	CodeLength     int    `json:"code_length,omitempty"`
	Provider       string `json:"provider,omitempty"`
}

type RemoteRegistrationTargetResult struct {
	Identity       string `json:"identity"`
	HubURL         string `json:"hub_url"`
	HubID          string `json:"hub_id,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
	Method         string `json:"method"`
	CodeTTLMinutes int    `json:"code_ttl_minutes,omitempty"`
	CodeLength     int    `json:"code_length,omitempty"`
	Provider       string `json:"provider,omitempty"`
}

type RemoteSMSSendResult struct {
	OK                bool   `json:"ok"`
	Code              string `json:"code,omitempty"`
	TenantID          string `json:"tenant_id,omitempty"`
	ExpiresMin        int    `json:"expires_min,omitempty"`
	CodeLength        int    `json:"code_length,omitempty"`
	Purpose           string `json:"purpose,omitempty"`
	DailySMSRemaining int    `json:"daily_sms_remaining,omitempty"`
	Message           string `json:"message,omitempty"`
}

func (a *App) SendRemoteRegistrationEmail(hubURL string, email string, tenantID string) (RemoteRegistrationContactResult, error) {
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	email = strings.TrimSpace(strings.ToLower(email))
	if hubURL == "" {
		return RemoteRegistrationContactResult{}, fmt.Errorf("hub URL is required")
	}
	if email == "" || !strings.Contains(email, "@") {
		return RemoteRegistrationContactResult{}, fmt.Errorf("INVALID_EMAIL: valid email is required")
	}
	payload := map[string]string{"email": email}
	if strings.TrimSpace(tenantID) != "" {
		payload["tenant_id"] = strings.TrimSpace(tenantID)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	resp, err := hubHTTPClient.Post(hubURL+"/api/enroll/email/send-code", "application/json", bytes.NewReader(data))
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	defer resp.Body.Close()
	var result RemoteRegistrationContactResult
	if err := remote.DecodeHTTPJSONResponse(resp, &result, "send registration email code"); err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	if resp.StatusCode >= 300 {
		return RemoteRegistrationContactResult{}, remoteRegistrationContactError(result, "send email code failed: "+resp.Status)
	}
	return result, nil
}

type RemoteRegistrationContactResult struct {
	OK                    bool   `json:"ok"`
	Kind                  string `json:"kind,omitempty"`
	TenantID              string `json:"tenant_id,omitempty"`
	Email                 string `json:"email,omitempty"`
	PhoneNumber           string `json:"phone_number,omitempty"`
	ExpiresMin            int    `json:"expires_min,omitempty"`
	CodeLength            int    `json:"code_length,omitempty"`
	Purpose               string `json:"purpose,omitempty"`
	DailySMSRemaining     int    `json:"daily_sms_remaining,omitempty"`
	ResendCooldownSeconds int    `json:"resend_cooldown_seconds,omitempty"`
	Code                  string `json:"code,omitempty"`
	Message               string `json:"message,omitempty"`
}

type RemoteRegistrationProfileResult struct {
	OK          bool   `json:"ok"`
	TenantID    string `json:"tenant_id,omitempty"`
	TenantName  string `json:"tenant_name,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	MachineID   string `json:"machine_id,omitempty"`
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Message     string `json:"message,omitempty"`
	Code        string `json:"code,omitempty"`
}

type RemoteProbeResult struct {
	InvitationCodeRequired bool   `json:"invitation_code_required"`
	TenantID               string `json:"tenant_id,omitempty"`
	TenantName             string `json:"tenant_name,omitempty"`
	PhoneNumber            string `json:"phone_number,omitempty"`
	Status                 string `json:"status,omitempty"`
	Message                string `json:"message,omitempty"`
}

type RemoteActivationStatus struct {
	Activated  bool   `json:"activated"`
	HubID      string `json:"hub_id,omitempty"`
	Email      string `json:"email"`
	SN         string `json:"sn"`
	TenantID   string `json:"tenant_id,omitempty"`
	TenantName string `json:"tenant_name,omitempty"`
	MachineID  string `json:"machine_id"`
	HubURL     string `json:"hub_url"`
}

func normalizeRemoteRegistrationPhoneNumber(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type RemoteHubCenterHub struct {
	HubID          string `json:"hub_id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	PWAURL         string `json:"pwa_url"`
	Visibility     string `json:"visibility"`
	EnrollmentMode string `json:"enrollment_mode"`
	Status         string `json:"status"`
}

var remoteEnrollTimeout = remote.EnrollTimeout

const (
	defaultRemoteRegistrationSMSCodeLength = 6
	skillMarketAutoLoginRetryDelay         = 15 * time.Minute
)

func sanitizeHubCenterRegistrationURLs(preferred string, discovered []string) (string, []string) {
	candidates := append([]string{preferred}, discovered...)
	candidates = append(candidates, remote.DefaultRemoteHubCenterURLs...)
	normalized := remote.NormalizeHubCenterURLs(candidates)
	public := make([]string, 0, len(normalized))
	for _, value := range normalized {
		if value == "" || remote.IsLoopbackURL(value) {
			continue
		}
		public = append(public, value)
	}
	if len(public) == 0 {
		return "", nil
	}
	return public[0], public
}

func hasPublicHubCenterURL(values []string) bool {
	for _, value := range remote.NormalizeHubCenterURLs(values) {
		if value != "" && !remote.IsLoopbackURL(value) {
			return true
		}
	}
	return false
}

func (a *App) ProbeRemoteHub(hubURL string, identity string) (RemoteProbeResult, error) {
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return RemoteProbeResult{}, fmt.Errorf("hub URL is required")
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return RemoteProbeResult{}, fmt.Errorf("user identity is required")
	}

	payload := remoteProbeIdentityPayload(identity)
	if tenantID := a.remoteProbeTenantID(hubURL, identity); tenantID != "" {
		payload["tenant_id"] = tenantID
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return RemoteProbeResult{}, err
	}

	resp, err := hubHTTPClient.Post(strings.TrimRight(hubURL, "/")+"/api/entry/probe", "application/json", bytes.NewReader(data))
	if err != nil {
		return RemoteProbeResult{}, err
	}
	defer resp.Body.Close()

	var result RemoteProbeResult
	if err := remote.DecodeHTTPJSONResponse(resp, &result, "hub probe"); err != nil {
		return RemoteProbeResult{}, err
	}
	if resp.StatusCode >= 300 {
		if result.Message != "" {
			return RemoteProbeResult{}, fmt.Errorf("%s", result.Message)
		}
		return RemoteProbeResult{}, fmt.Errorf("probe failed: %s", resp.Status)
	}

	return result, nil
}

func (a *App) GetRemoteRegistrationProfile() (RemoteRegistrationProfileResult, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteRegistrationProfileResult{}, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if hubURL == "" {
		return RemoteRegistrationProfileResult{}, fmt.Errorf("hub URL is required")
	}
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	machineToken := strings.TrimSpace(cfg.RemoteMachineToken)
	if machineID == "" || machineToken == "" {
		return RemoteRegistrationProfileResult{}, fmt.Errorf("machine credentials are required")
	}

	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/enroll/profile/current", nil)
	if err != nil {
		return RemoteRegistrationProfileResult{}, err
	}
	req.Header.Set("X-Machine-ID", machineID)
	req.Header.Set("Authorization", "Bearer "+machineToken)
	resp, err := hubHTTPClient.Do(req)
	if err != nil {
		return RemoteRegistrationProfileResult{}, err
	}
	defer resp.Body.Close()

	var result RemoteRegistrationProfileResult
	if err := remote.DecodeHTTPJSONResponse(resp, &result, "registration profile"); err != nil {
		return RemoteRegistrationProfileResult{}, err
	}
	if resp.StatusCode >= 300 {
		return RemoteRegistrationProfileResult{}, remoteRegistrationProfileError(result, "load registration profile failed: "+resp.Status)
	}

	phone := normalizeRemoteRegistrationPhoneNumber(result.PhoneNumber)
	tenantID := strings.TrimSpace(result.TenantID)
	tenantName := strings.TrimSpace(result.TenantName)
	userID := strings.TrimSpace(result.UserID)
	email := strings.TrimSpace(result.Email)
	if phone != "" || tenantID != "" || tenantName != "" || userID != "" || email != "" {
		if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
			if phone != "" {
				cfg.RemoteMobile = phone
			}
			if tenantID != "" {
				cfg.RemoteTenantID = tenantID
			}
			if tenantName != "" {
				cfg.RemoteTenantName = tenantName
			}
			if userID != "" {
				cfg.RemoteUserID = userID
			}
			if email != "" && strings.TrimSpace(cfg.RemoteEmail) == "" {
				cfg.RemoteEmail = email
			}
		}); err != nil {
			return RemoteRegistrationProfileResult{}, err
		}
	}
	result.PhoneNumber = phone
	a.emitRemoteStateChanged()
	return result, nil
}

func (a *App) remoteProbeTenantID(hubURL string, identity string) string {
	if a == nil {
		return ""
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	tenantID := strings.TrimSpace(cfg.RemoteTenantID)
	if tenantID == "" {
		return ""
	}
	if strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/") != strings.TrimRight(strings.TrimSpace(hubURL), "/") {
		return ""
	}
	identity = strings.TrimSpace(identity)
	if strings.EqualFold(identity, strings.TrimSpace(cfg.RemoteEmail)) {
		return tenantID
	}
	configuredPhone := normalizeRemoteRegistrationPhoneNumber(cfg.RemoteMobile)
	if configuredPhone != "" && normalizeRemoteRegistrationPhoneNumber(identity) == configuredPhone {
		return tenantID
	}
	return ""
}

func remoteProbeIdentityPayload(identity string) map[string]string {
	identity = strings.TrimSpace(identity)
	if strings.HasPrefix(strings.ToLower(identity), "phone:") {
		if phone := normalizeRemoteRegistrationPhoneNumber(identity); phone != "" {
			return map[string]string{"phone_number": phone}
		}
	}
	if !strings.Contains(identity, "@") {
		if phone := normalizeRemoteRegistrationPhoneNumber(identity); len(phone) >= 6 {
			return map[string]string{"phone_number": phone}
		}
	}
	return map[string]string{"email": identity}
}

func (a *App) GetRemoteRegistrationAuth(hubURL string, tenantID string) (RemoteRegistrationAuthResult, error) {
	return getRemoteRegistrationAuth(hubURL, tenantID, "")
}

func getRemoteRegistrationAuth(hubURL string, tenantID string, identity string) (RemoteRegistrationAuthResult, error) {
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return RemoteRegistrationAuthResult{Method: "email", CodeTTLMinutes: 5, CodeLength: defaultRemoteRegistrationSMSCodeLength}, nil
	}
	authURL := strings.TrimRight(hubURL, "/") + "/api/enroll/registration-auth"
	if strings.TrimSpace(tenantID) != "" {
		parsed, err := url.Parse(authURL)
		if err == nil {
			q := parsed.Query()
			q.Set("tenant_id", strings.TrimSpace(tenantID))
			parsed.RawQuery = q.Encode()
			authURL = parsed.String()
		}
	}
	if strings.Contains(strings.TrimSpace(identity), "@") {
		parsed, err := url.Parse(authURL)
		if err == nil {
			q := parsed.Query()
			q.Set("email", strings.TrimSpace(identity))
			parsed.RawQuery = q.Encode()
			authURL = parsed.String()
		}
	}
	resp, err := hubHTTPClient.Get(authURL)
	if err != nil {
		return RemoteRegistrationAuthResult{}, fmt.Errorf("load registration auth config: %w", err)
	}
	defer resp.Body.Close()
	// Older Hubs predate the public registration-auth endpoint. Their
	// registration flow is email-based, so retain a usable, safe default rather
	// than blocking email sign-in solely because the optional capability endpoint
	// is unavailable.
	if resp.StatusCode == http.StatusNotFound {
		return RemoteRegistrationAuthResult{
			Method:         "email",
			TenantID:       strings.TrimSpace(tenantID),
			CodeTTLMinutes: 5,
			CodeLength:     defaultRemoteRegistrationSMSCodeLength,
		}, nil
	}
	var result RemoteRegistrationAuthResult
	if err := remote.DecodeHTTPJSONResponse(resp, &result, "registration auth config"); err != nil {
		return RemoteRegistrationAuthResult{}, err
	}
	if resp.StatusCode >= 300 {
		return RemoteRegistrationAuthResult{}, fmt.Errorf("load registration auth config failed: %s", resp.Status)
	}
	if strings.TrimSpace(result.Method) == "" {
		return RemoteRegistrationAuthResult{}, fmt.Errorf("registration auth config missing method")
	}
	result.Method = strings.ToLower(strings.TrimSpace(result.Method))
	if result.Method != "email" && result.Method != "phone" && result.Method != "mixed" {
		return RemoteRegistrationAuthResult{}, fmt.Errorf("registration auth config has unsupported method %q", result.Method)
	}
	result.TenantID = strings.TrimSpace(result.TenantID)
	if result.CodeTTLMinutes <= 0 {
		result.CodeTTLMinutes = 5
	}
	if result.CodeLength < 4 || result.CodeLength > 8 {
		result.CodeLength = defaultRemoteRegistrationSMSCodeLength
	}
	return result, nil
}

func (a *App) ResolveRemoteRegistrationTarget(identity string) (RemoteRegistrationTargetResult, error) {
	return a.resolveRemoteRegistrationTarget(identity, "")
}

func (a *App) ResolveRemoteRegistrationTargetWithInvitation(identity string, invitationCode string) (RemoteRegistrationTargetResult, error) {
	return a.resolveRemoteRegistrationTarget(identity, invitationCode)
}

func (a *App) resolveRemoteRegistrationTarget(identity string, invitationCode string) (RemoteRegistrationTargetResult, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return RemoteRegistrationTargetResult{}, fmt.Errorf("user identity is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteRegistrationTargetResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, _, _, err := remote.NewEnrollmentClient().ResolveHubs(ctx, identity, strings.TrimSpace(invitationCode), cfg.RemoteHubCenterURL, cfg.RemoteHubCenterURLs)
	if err != nil {
		return RemoteRegistrationTargetResult{}, err
	}
	hubURL, hubID, tenantID, err := remote.PickBestHubWithTenantAndID(*result)
	if err != nil {
		if strings.TrimSpace(cfg.RemoteHubURL) != "" && canFallbackToConfiguredHubForPhoneRoute(identity, result) {
			if fallback, fallbackErr := a.resolveRemoteRegistrationTargetFromHub(identity, cfg.RemoteHubURL, cfg.RemoteHubID, cfg.RemoteTenantID); fallbackErr == nil {
				return fallback, nil
			}
		}
		return RemoteRegistrationTargetResult{}, err
	}
	return a.resolveRemoteRegistrationTargetFromHub(identity, hubURL, hubID, tenantID)
}

func (a *App) resolveRemoteRegistrationTargetFromHub(identity, hubURL, hubID, tenantID string) (RemoteRegistrationTargetResult, error) {
	auth, err := getRemoteRegistrationAuth(hubURL, tenantID, identity)
	if err != nil {
		return RemoteRegistrationTargetResult{}, err
	}
	return RemoteRegistrationTargetResult{
		Identity:       identity,
		HubURL:         strings.TrimRight(strings.TrimSpace(hubURL), "/"),
		HubID:          hubID,
		TenantID:       firstNonEmpty(auth.TenantID, tenantID),
		Method:         auth.Method,
		CodeTTLMinutes: auth.CodeTTLMinutes,
		CodeLength:     auth.CodeLength,
		Provider:       auth.Provider,
	}, nil
}

func canFallbackToConfiguredHubForPhoneRoute(identity string, result *remote.HubCenterResolveResult) bool {
	if !isRemoteRegistrationPhoneIdentity(identity) || result == nil || len(result.Hubs) != 0 {
		return false
	}
	message := strings.TrimSpace(strings.ToLower(result.Message))
	return message == "no phone route found"
}

func isRemoteRegistrationPhoneIdentity(identity string) bool {
	identity = strings.TrimSpace(identity)
	if identity == "" || strings.Contains(identity, "@") {
		return false
	}
	return len(normalizeRemoteRegistrationPhoneNumber(identity)) >= 6
}

func (a *App) SendRemoteRegistrationSMS(hubURL string, phoneNumber string, tenantID string) (RemoteSMSSendResult, error) {
	return sendRemoteRegistrationSMS(hubURL, phoneNumber, tenantID, "", "")
}

func sendRemoteRegistrationSMS(hubURL string, phoneNumber string, tenantID string, machineID string, machineToken string) (RemoteSMSSendResult, error) {
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return RemoteSMSSendResult{}, fmt.Errorf("hub URL is required")
	}
	phoneNumber = normalizeRemoteRegistrationPhoneNumber(phoneNumber)
	if len(phoneNumber) < 6 {
		return RemoteSMSSendResult{}, fmt.Errorf("INVALID_PHONE_NUMBER: valid phone number is required")
	}
	payload := map[string]string{"phone_number": phoneNumber}
	if strings.TrimSpace(tenantID) != "" {
		payload["tenant_id"] = strings.TrimSpace(tenantID)
	}
	if strings.TrimSpace(machineID) != "" {
		payload["machine_id"] = strings.TrimSpace(machineID)
	}
	if strings.TrimSpace(machineToken) != "" {
		payload["machine_token"] = strings.TrimSpace(machineToken)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return RemoteSMSSendResult{}, err
	}
	resp, err := hubHTTPClient.Post(strings.TrimRight(hubURL, "/")+"/api/enroll/sms/send-code", "application/json", bytes.NewReader(data))
	if err != nil {
		log.Printf("[registration-contact] phone send failed endpoint=/api/enroll/sms/send-code tenant=%s machine=%s err=%v", strings.TrimSpace(tenantID), strings.TrimSpace(machineID), err)
		return RemoteSMSSendResult{}, err
	}
	defer resp.Body.Close()
	var result RemoteSMSSendResult
	if err := remote.DecodeHTTPJSONResponse(resp, &result, "send registration SMS"); err != nil {
		log.Printf("[registration-contact] phone send decode failed endpoint=/api/enroll/sms/send-code tenant=%s machine=%s status=%d err=%v", strings.TrimSpace(tenantID), strings.TrimSpace(machineID), resp.StatusCode, err)
		return RemoteSMSSendResult{}, err
	}
	if resp.StatusCode >= 300 {
		log.Printf("[registration-contact] phone send rejected endpoint=/api/enroll/sms/send-code tenant=%s machine=%s status=%d code=%s", strings.TrimSpace(tenantID), strings.TrimSpace(machineID), resp.StatusCode, result.Code)
		if result.Code != "" && result.Message != "" {
			return RemoteSMSSendResult{}, fmt.Errorf("%s: %s", result.Code, result.Message)
		}
		if result.Code != "" {
			return RemoteSMSSendResult{}, fmt.Errorf("%s", result.Code)
		}
		if result.Message != "" {
			return RemoteSMSSendResult{}, fmt.Errorf("%s", result.Message)
		}
		return RemoteSMSSendResult{}, fmt.Errorf("send SMS failed: %s", resp.Status)
	}
	return result, nil
}

func (a *App) SendRemoteRegistrationContactCode(kind string, value string) (RemoteRegistrationContactResult, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	kind = normalizeRemoteRegistrationContactKind(kind)
	if kind == "" {
		return RemoteRegistrationContactResult{}, fmt.Errorf("contact kind must be email or phone")
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if hubURL == "" {
		return RemoteRegistrationContactResult{}, fmt.Errorf("hub URL is required")
	}
	if kind == "phone" {
		if strings.TrimSpace(cfg.RemoteMachineID) == "" || strings.TrimSpace(cfg.RemoteMachineToken) == "" {
			return RemoteRegistrationContactResult{}, fmt.Errorf("MACHINE_UNAUTHORIZED: registered machine credentials are required")
		}
		result, err := sendRemoteRegistrationSMS(hubURL, value, cfg.RemoteTenantID, cfg.RemoteMachineID, cfg.RemoteMachineToken)
		if err != nil {
			return RemoteRegistrationContactResult{}, err
		}
		return RemoteRegistrationContactResult{
			OK:                result.OK,
			Kind:              "phone",
			TenantID:          result.TenantID,
			ExpiresMin:        result.ExpiresMin,
			CodeLength:        result.CodeLength,
			Purpose:           result.Purpose,
			DailySMSRemaining: result.DailySMSRemaining,
			Code:              result.Code,
			Message:           result.Message,
		}, nil
	}
	payload, err := remoteRegistrationContactPayload(cfg, kind, value, "")
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	resp, err := hubHTTPClient.Post(hubURL+"/api/enroll/profile/send-code", "application/json", bytes.NewReader(data))
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	defer resp.Body.Close()
	var result RemoteRegistrationContactResult
	if err := remote.DecodeHTTPJSONResponse(resp, &result, "send registration contact code"); err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	if resp.StatusCode >= 300 {
		return RemoteRegistrationContactResult{}, remoteRegistrationContactError(result, "send contact code failed: "+resp.Status)
	}
	return result, nil
}

func (a *App) VerifyRemoteRegistrationContactCode(kind string, value string, verifyCode string) (RemoteRegistrationContactResult, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	kind = normalizeRemoteRegistrationContactKind(kind)
	if kind == "" {
		return RemoteRegistrationContactResult{}, fmt.Errorf("contact kind must be email or phone")
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if hubURL == "" {
		return RemoteRegistrationContactResult{}, fmt.Errorf("hub URL is required")
	}
	if kind == "phone" {
		payload, err := remoteRegistrationSMSContactVerifyPayload(cfg, value, verifyCode)
		if err != nil {
			return RemoteRegistrationContactResult{}, err
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return RemoteRegistrationContactResult{}, err
		}
		resp, err := hubHTTPClient.Post(hubURL+"/api/enroll/sms/verify-and-start", "application/json", bytes.NewReader(data))
		if err != nil {
			log.Printf("[registration-contact] phone verify failed endpoint=/api/enroll/sms/verify-and-start tenant=%s machine=%s err=%v", strings.TrimSpace(cfg.RemoteTenantID), strings.TrimSpace(cfg.RemoteMachineID), err)
			return RemoteRegistrationContactResult{}, err
		}
		defer resp.Body.Close()
		var result RemoteRegistrationContactResult
		if err := remote.DecodeHTTPJSONResponse(resp, &result, "verify registration SMS contact code"); err != nil {
			log.Printf("[registration-contact] phone verify decode failed endpoint=/api/enroll/sms/verify-and-start tenant=%s machine=%s status=%d err=%v", strings.TrimSpace(cfg.RemoteTenantID), strings.TrimSpace(cfg.RemoteMachineID), resp.StatusCode, err)
			return RemoteRegistrationContactResult{}, err
		}
		if resp.StatusCode >= 300 {
			log.Printf("[registration-contact] phone verify rejected endpoint=/api/enroll/sms/verify-and-start tenant=%s machine=%s status=%d code=%s", strings.TrimSpace(cfg.RemoteTenantID), strings.TrimSpace(cfg.RemoteMachineID), resp.StatusCode, result.Code)
			return RemoteRegistrationContactResult{}, remoteRegistrationContactError(result, "verify contact SMS code failed: "+resp.Status)
		}
		phone := normalizeRemoteRegistrationPhoneNumber(result.PhoneNumber)
		if phone == "" {
			phone = normalizeRemoteRegistrationPhoneNumber(value)
		}
		patch := map[string]interface{}{"remote_mobile": phone}
		if email := strings.TrimSpace(result.Email); shouldPatchRemoteEmailFromLogin(cfg.RemoteEmail, email) {
			patch["remote_email"] = email
		}
		if _, err := a.PatchConfigFields(patch); err != nil {
			return RemoteRegistrationContactResult{}, err
		}
		a.emitRemoteStateChanged()
		return result, nil
	}
	payload, err := remoteRegistrationContactPayload(cfg, kind, value, verifyCode)
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	resp, err := hubHTTPClient.Post(hubURL+"/api/enroll/profile/verify", "application/json", bytes.NewReader(data))
	if err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	defer resp.Body.Close()
	var result RemoteRegistrationContactResult
	if err := remote.DecodeHTTPJSONResponse(resp, &result, "verify registration contact code"); err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	if resp.StatusCode >= 300 {
		return RemoteRegistrationContactResult{}, remoteRegistrationContactError(result, "verify contact code failed: "+resp.Status)
	}
	patch := map[string]interface{}{}
	if kind == "email" {
		email := strings.TrimSpace(result.Email)
		if email == "" {
			email = strings.TrimSpace(value)
		}
		patch["remote_email"] = email
	} else {
		phone := normalizeRemoteRegistrationPhoneNumber(result.PhoneNumber)
		if phone == "" {
			phone = normalizeRemoteRegistrationPhoneNumber(value)
		}
		patch["remote_mobile"] = phone
	}
	if _, err := a.PatchConfigFields(patch); err != nil {
		return RemoteRegistrationContactResult{}, err
	}
	a.emitRemoteStateChanged()
	return result, nil
}

func normalizeRemoteRegistrationContactKind(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "email", "mail":
		return "email"
	case "phone", "mobile", "phone_number":
		return "phone"
	default:
		return ""
	}
}

func remoteRegistrationContactPayload(cfg corelib.AppConfig, kind string, value string, verifyCode string) (map[string]string, error) {
	if strings.TrimSpace(cfg.RemoteMachineID) == "" || strings.TrimSpace(cfg.RemoteMachineToken) == "" {
		return nil, fmt.Errorf("MACHINE_UNAUTHORIZED: registered machine credentials are required")
	}
	payload := map[string]string{
		"kind":          kind,
		"tenant_id":     strings.TrimSpace(cfg.RemoteTenantID),
		"machine_id":    strings.TrimSpace(cfg.RemoteMachineID),
		"machine_token": strings.TrimSpace(cfg.RemoteMachineToken),
	}
	if verifyCode = strings.TrimSpace(verifyCode); verifyCode != "" {
		payload["verify_code"] = verifyCode
	}
	if kind == "email" {
		email := strings.TrimSpace(value)
		if email == "" || !strings.Contains(email, "@") {
			return nil, fmt.Errorf("INVALID_EMAIL: valid email is required")
		}
		payload["email"] = email
		return payload, nil
	}
	phone := normalizeRemoteRegistrationPhoneNumber(value)
	if len(phone) < 6 {
		return nil, fmt.Errorf("INVALID_PHONE_NUMBER: valid phone number is required")
	}
	payload["phone_number"] = phone
	return payload, nil
}

func remoteRegistrationSMSContactVerifyPayload(cfg corelib.AppConfig, value string, verifyCode string) (map[string]string, error) {
	if strings.TrimSpace(cfg.RemoteMachineID) == "" || strings.TrimSpace(cfg.RemoteMachineToken) == "" {
		return nil, fmt.Errorf("MACHINE_UNAUTHORIZED: registered machine credentials are required")
	}
	phone := normalizeRemoteRegistrationPhoneNumber(value)
	if len(phone) < 6 {
		return nil, fmt.Errorf("INVALID_PHONE_NUMBER: valid phone number is required")
	}
	verifyCode = strings.TrimSpace(verifyCode)
	if verifyCode == "" {
		return nil, fmt.Errorf("verification code is required")
	}
	return map[string]string{
		"phone_number":  phone,
		"verify_code":   verifyCode,
		"tenant_id":     strings.TrimSpace(cfg.RemoteTenantID),
		"machine_id":    strings.TrimSpace(cfg.RemoteMachineID),
		"machine_token": strings.TrimSpace(cfg.RemoteMachineToken),
	}, nil
}

func remoteRegistrationContactError(result RemoteRegistrationContactResult, fallback string) error {
	if result.Code != "" && result.Message != "" {
		return fmt.Errorf("%s: %s", result.Code, result.Message)
	}
	if result.Code != "" {
		return fmt.Errorf("%s", result.Code)
	}
	if result.Message != "" {
		return fmt.Errorf("%s", result.Message)
	}
	return fmt.Errorf("%s", fallback)
}

func remoteRegistrationProfileError(result RemoteRegistrationProfileResult, fallback string) error {
	if result.Code != "" && result.Message != "" {
		return fmt.Errorf("%s: %s", result.Code, result.Message)
	}
	if result.Code != "" {
		return fmt.Errorf("%s", result.Code)
	}
	if result.Message != "" {
		return fmt.Errorf("%s", result.Message)
	}
	return fmt.Errorf("%s", fallback)
}

// autoRegisterOnStartup is called during startup when email and hub URL are present
// but machine credentials are missing. This can happen when:
//   - The user was unbound by admin (clearMachineCredentials preserved email/hubURL)
//   - A config migration left partial state
//
// Instead of silently re-enrolling (which would recreate a deleted user), we notify
// the frontend to prompt the user whether they want to re-register.
// Delays slightly to ensure frontend event listeners are mounted.
func (a *App) autoRegisterOnStartup(cfg corelib.AppConfig) {
	email := strings.TrimSpace(cfg.RemoteEmail)
	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	if email == "" || hubURL == "" {
		return
	}
	// Wait for frontend to mount event listeners before emitting.
	time.Sleep(3 * time.Second)
	log.Printf("[startup] hub credentials incomplete (email=%s, hub=%s, machine_id missing), prompting user to re-register", email, hubURL)
	a.emitEvent("hub-auth-rejected")
}

func (a *App) ActivateRemote(email string, invitationCode string, mobile string) (RemoteActivationResult, error) {
	return a.activateRemoteEmail(email, "", invitationCode, mobile, "", "", "")
}

func (a *App) ActivateRemoteEmail(hubURL string, email string, verifyCode string, invitationCode string, tenantID string, hubID string) (RemoteActivationResult, error) {
	verifyCode = strings.TrimSpace(verifyCode)
	if verifyCode == "" {
		return RemoteActivationResult{}, fmt.Errorf("verification code is required")
	}
	return a.activateRemoteEmail(email, verifyCode, invitationCode, "", hubURL, tenantID, hubID)
}

func (a *App) activateRemoteEmail(email string, verifyCode string, invitationCode string, mobile string, directHubURL string, tenantID string, hubID string) (RemoteActivationResult, error) {
	start := time.Now()
	cfgLoadStart := time.Now()
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteActivationResult{}, err
	}
	log.Printf("[onboarding] ActivateRemote load_config=%s", time.Since(cfgLoadStart))

	email = strings.TrimSpace(email)
	if email == "" {
		return RemoteActivationResult{}, fmt.Errorf("email is required")
	}

	// Build enrollment config from app config.
	profile := a.currentRemoteMachineProfile(cfg.RemoteHeartbeatSec, 0)
	enrollCfg := remote.EnrollConfig{
		Email:            email,
		VerificationCode: verifyCode,
		InvitationCode:   invitationCode,
		Mobile:           mobile,
		ClientID:         cfg.RemoteClientID,
		HubURL:           strings.TrimSpace(cfg.RemoteHubURL),
		HubCenterURL:     strings.TrimSpace(cfg.RemoteHubCenterURL),
		HubCenterURLs:    cfg.HubCenterBaseURLs(defaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs),
		MachineName:      profile.Name,
		Platform:         profile.Platform,
		Hostname:         profile.Hostname,
		Arch:             profile.Arch,
		AppVersion:       profile.AppVersion,
		HeartbeatSec:     profile.HeartbeatSec,
		TenantID:         strings.TrimSpace(tenantID),
		HubID:            strings.TrimSpace(hubID),
	}
	// Activation must dynamically confirm the HubCenter -> Hub routing instead
	// of reusing a cached Hub URL. The HubCenter shown in About should be the
	// node that actually resolved this registration.
	if strings.TrimSpace(verifyCode) != "" {
		enrollCfg.HubURL = strings.TrimRight(strings.TrimSpace(directHubURL), "/")
		enrollCfg.DirectHub = true
		if enrollCfg.HubURL == "" {
			return RemoteActivationResult{}, fmt.Errorf("hub URL is required")
		}
	} else {
		enrollCfg.HubURL = ""
	}
	if remote.IsLoopbackURL(enrollCfg.HubCenterURL) && hasPublicHubCenterURL(enrollCfg.HubCenterURLs) {
		enrollCfg.HubCenterURL = ""
	}

	// Ensure stable client_id / device key.
	// Prefer config value, then durable OS store (survives wiped app data dir), else create.
	enrollCfg.ClientID = remote.EnsureDeviceKey(enrollCfg.ClientID)
	// Persist only client_id so concurrent settings edits are not overwritten.
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteClientID = enrollCfg.ClientID
	}); err != nil {
		return RemoteActivationResult{}, err
	}

	// Delegate to shared enrollment client.
	enrollClient := &remote.EnrollmentClient{HTTPClient: hubHTTPClient, EnrollTimeout: remoteEnrollTimeout}
	enrollResult, err := enrollClient.Enroll(context.Background(), enrollCfg)
	if err != nil {
		return RemoteActivationResult{}, err
	}

	// Persist credentials atomically via PatchConfig to eliminate the TOCTOU
	// race between LoadConfig and SaveConfig. Only enrollment-specific fields
	// are patched - other fields (LLM settings, UI preferences, etc.) that
	// may have been modified concurrently are untouched.
	persistStart := time.Now()
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteEmail = enrollResult.Email
		cfg.RemoteSN = enrollResult.SN
		cfg.RemoteUserID = enrollResult.UserID
		cfg.RemoteTenantID = enrollResult.TenantID
		cfg.RemoteTenantName = enrollResult.TenantName
		cfg.RemoteMachineID = enrollResult.MachineID
		cfg.RemoteMachineName = profile.Name
		cfg.RemoteMachineToken = enrollResult.MachineToken
		cfg.RemoteNickname = ""
		if strings.TrimSpace(enrollResult.HubID) != "" {
			cfg.RemoteHubID = enrollResult.HubID
		}
		cfg.RemoteHubURL = enrollResult.HubURL
		cfg.RemoteEnabled = true
		cfg.RemoteMobile = normalizeRemoteRegistrationPhoneNumber(enrollResult.PhoneNumber)
		if enrollResult.ViewerToken != "" {
			cfg.RemoteViewerToken = enrollResult.ViewerToken
		} else {
			cfg.RemoteViewerToken = ""
		}
		if enrollResult.ClientID != "" && cfg.RemoteClientID == "" {
			cfg.RemoteClientID = enrollResult.ClientID
		}
		if enrollResult.HubCenterURL != "" || len(enrollResult.DiscoveredURLs) > 0 {
			nextCenterURL, nextCenterURLs := sanitizeHubCenterRegistrationURLs(enrollResult.HubCenterURL, enrollResult.DiscoveredURLs)
			if nextCenterURL != "" {
				cfg.RemoteHubCenterURL = nextCenterURL
			}
			if len(nextCenterURLs) > 0 || len(enrollResult.DiscoveredURLs) > 0 {
				cfg.RemoteHubCenterURLs = nextCenterURLs
			}
		}
	}); err != nil {
		log.Printf("[onboarding] ActivateRemote PatchConfig:failed after=%s err=%v", time.Since(persistStart), err)
		return RemoteActivationResult{}, err
	}
	log.Printf("[onboarding] ActivateRemote PatchConfig=%s machine_id=%s email=%s", time.Since(persistStart), enrollResult.MachineID, enrollResult.Email)

	// If this Hub account already has MaClaw official service entitlement,
	// make it the active LLM provider immediately after registration. This
	// handles re-registration to another Hub where official service is available.
	if freshCfg, loadErr := a.LoadConfig(); loadErr == nil {
		if strings.TrimSpace(freshCfg.RemoteViewerToken) != "" {
			if status, statusErr := a.fetchHubLLMServiceStatusWithTimeout(freshCfg, hubServiceStatusTimeout); statusErr == nil {
				// Update local cache so sidebar shows fresh status immediately.
				storeHubServiceStatusCache(freshCfg.RemoteHubURL, freshCfg.RemoteViewerToken, status)
				if _, syncErr := a.syncHubLLMServiceStatusToConfig(status, true); syncErr != nil {
					log.Printf("[onboarding] ActivateRemote hub_service_sync_failed err=%v", syncErr)
				}
			} else {
				if isHubLLMServiceAuthorizationError(statusErr) {
					if _, syncErr := a.syncHubLLMServiceStatusToConfig(HubLLMServiceStatus{}, false); syncErr != nil {
						log.Printf("[onboarding] ActivateRemote hub_service_clear_failed err=%v", syncErr)
					}
				}
				// Invalidate cache on auth error so next fetch tries fresh.
				clearHubServiceStatusCache()
				log.Printf("[onboarding] ActivateRemote hub_service_status_failed err=%v", statusErr)
			}
		} else if _, syncErr := a.syncHubLLMServiceStatusToConfig(HubLLMServiceStatus{}, false); syncErr != nil {
			log.Printf("[onboarding] ActivateRemote hub_service_clear_failed err=%v", syncErr)
		}
	} else {
		log.Printf("[onboarding] ActivateRemote hub_service_load_config_failed err=%v", loadErr)
	}

	// Auto-acquire SkillMarket session token via machine-login.
	// This allows the user to upload skills immediately after Hub registration
	// without a separate SkillMarket login step.
	go a.acquireSkillMarketTokenAfterEnroll(skillMarketAccountFromEnroll(enrollResult, ""), enrollResult.MachineID, enrollResult.ViewerToken)

	// Convert to GUI result type.
	result := RemoteActivationResult{
		Status:              enrollResult.Status,
		HubID:               enrollResult.HubID,
		TenantID:            enrollResult.TenantID,
		TenantName:          enrollResult.TenantName,
		Message:             enrollResult.Message,
		Code:                enrollResult.Code,
		UserID:              enrollResult.UserID,
		Email:               enrollResult.Email,
		PhoneNumber:         normalizeRemoteRegistrationPhoneNumber(enrollResult.PhoneNumber),
		SN:                  enrollResult.SN,
		MachineID:           enrollResult.MachineID,
		MachineToken:        enrollResult.MachineToken,
		ViewerToken:         enrollResult.ViewerToken,
		ExpiresAt:           enrollResult.ExpiresAt,
		VIPFlag:             enrollResult.VIPFlag,
		ReboundExistingUser: enrollResult.ReboundExistingUser,
	}

	// GUI-specific: emit state change + background hub connection.
	a.emitRemoteStateChanged()
	if a.remoteActivationBackgroundDisabled {
		log.Printf("[onboarding] ActivateRemote background connect skipped")
		log.Printf("[onboarding] ActivateRemote total=%s", time.Since(start))
		return result, nil
	}
	go func(launchedAt time.Time) {
		infraStart := time.Now()
		if a.remoteSessions == nil {
			a.ensureRemoteInfra()
		}
		a.logMemorySnapshot("remoteActivation:before-connect")
		logAfterConnect := func() {
			a.logMemorySnapshot("remoteActivation:after-connect")
		}
		hubClient := a.ensureHubClient()
		log.Printf("[onboarding] ActivateRemote ensure_remote_infra=%s hub_client_ready=%t", time.Since(infraStart), hubClient != nil)
		if hubClient != nil {
			// Hub client created - notify frontend to transition from "degraded" to "ready".
			// Note: markAIAssistantReady() was already called at startup, no need to call again
			// (calling it again would reset first-chat telemetry timestamp).
			a.emitEvent("ai-assistant-init-progress", "ready")
		}
		if hubClient != nil && !hubClient.IsConnected() {
			if err := hubClient.Connect(); err != nil {
				log.Printf("[onboarding] ActivateRemote background_connect_failed total=%s err=%v", time.Since(launchedAt), err)
			} else {
				a.emitRemoteStateChanged()
				logAfterConnect()
				log.Printf("[onboarding] ActivateRemote background_connect_total=%s", time.Since(launchedAt))
			}
		} else {
			logAfterConnect()
		}
	}(time.Now())
	log.Printf("[onboarding] ActivateRemote total=%s", time.Since(start))

	return result, nil
}

func (a *App) ActivateRemoteSMS(hubURL string, phoneNumber string, verifyCode string, invitationCode string, tenantID string, hubID string) (RemoteActivationResult, error) {
	start := time.Now()
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteActivationResult{}, err
	}
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	}
	if hubURL == "" {
		return RemoteActivationResult{}, fmt.Errorf("hub URL is required")
	}
	phoneNumber = normalizeRemoteRegistrationPhoneNumber(phoneNumber)
	if len(phoneNumber) < 6 {
		return RemoteActivationResult{}, fmt.Errorf("valid phone number is required")
	}
	verifyCode = strings.TrimSpace(verifyCode)
	if verifyCode == "" {
		return RemoteActivationResult{}, fmt.Errorf("verification code is required")
	}

	profile := a.currentRemoteMachineProfile(cfg.RemoteHeartbeatSec, 0)
	// Match email activation: preserve the durable desktop identity when app
	// configuration is replaced during an upgrade or a local data cleanup.
	clientID := remote.EnsureDeviceKey(cfg.RemoteClientID)
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteClientID = clientID
	}); err != nil {
		return RemoteActivationResult{}, err
	}
	heartbeat := profile.HeartbeatSec
	if heartbeat <= 0 {
		heartbeat = 30
	} else if heartbeat < 5 {
		heartbeat = 5
	}
	body := map[string]any{
		"phone_number":           phoneNumber,
		"verify_code":            verifyCode,
		"machine_name":           profile.Name,
		"platform":               profile.Platform,
		"hostname":               profile.Hostname,
		"arch":                   profile.Arch,
		"app_version":            profile.AppVersion,
		"heartbeat_interval_sec": heartbeat,
		"client_id":              clientID,
	}
	if strings.TrimSpace(tenantID) != "" {
		body["tenant_id"] = strings.TrimSpace(tenantID)
	}
	if strings.TrimSpace(invitationCode) != "" {
		body["invitation_code"] = strings.TrimSpace(invitationCode)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return RemoteActivationResult{}, err
	}
	resp, err := hubHTTPClient.Post(hubURL+"/api/enroll/sms/verify-and-start", "application/json", bytes.NewReader(data))
	if err != nil {
		return RemoteActivationResult{}, err
	}
	defer resp.Body.Close()
	var enrollResult remote.EnrollResult
	if err := remote.DecodeHTTPJSONResponse(resp, &enrollResult, "SMS registration"); err != nil {
		return RemoteActivationResult{}, err
	}
	if resp.StatusCode >= 300 {
		if enrollResult.Code != "" {
			return RemoteActivationResult{}, fmt.Errorf("%s: %s", enrollResult.Code, enrollResult.Message)
		}
		if enrollResult.Message != "" {
			return RemoteActivationResult{}, fmt.Errorf("%s", enrollResult.Message)
		}
		return RemoteActivationResult{}, fmt.Errorf("SMS registration failed: %s", resp.Status)
	}
	enrollResult.HubURL = hubURL
	enrollResult.ClientID = clientID
	resolvedHubID := strings.TrimSpace(hubID)
	if resolvedHubID == "" {
		resolvedHubID = strings.TrimSpace(enrollResult.HubID)
	}
	enrollResult.HubID = resolvedHubID

	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteEmail = enrollResult.Email
		cfg.RemoteMobile = phoneNumber
		cfg.RemoteSN = enrollResult.SN
		cfg.RemoteUserID = enrollResult.UserID
		cfg.RemoteTenantID = enrollResult.TenantID
		cfg.RemoteTenantName = enrollResult.TenantName
		cfg.RemoteMachineID = enrollResult.MachineID
		cfg.RemoteMachineName = profile.Name
		cfg.RemoteMachineToken = enrollResult.MachineToken
		cfg.RemoteNickname = ""
		if resolvedHubID != "" {
			cfg.RemoteHubID = resolvedHubID
		}
		cfg.RemoteHubURL = enrollResult.HubURL
		cfg.RemoteEnabled = true
		if enrollResult.ViewerToken != "" {
			cfg.RemoteViewerToken = enrollResult.ViewerToken
		} else {
			cfg.RemoteViewerToken = ""
		}
		if enrollResult.ClientID != "" && cfg.RemoteClientID == "" {
			cfg.RemoteClientID = enrollResult.ClientID
		}
	}); err != nil {
		return RemoteActivationResult{}, err
	}

	go a.acquireSkillMarketTokenAfterEnroll(skillMarketAccountFromEnroll(&enrollResult, phoneNumber), enrollResult.MachineID, enrollResult.ViewerToken)

	result := RemoteActivationResult{
		Status:              enrollResult.Status,
		HubID:               enrollResult.HubID,
		TenantID:            enrollResult.TenantID,
		TenantName:          enrollResult.TenantName,
		Message:             enrollResult.Message,
		Code:                enrollResult.Code,
		UserID:              enrollResult.UserID,
		Email:               enrollResult.Email,
		PhoneNumber:         phoneNumber,
		SN:                  enrollResult.SN,
		MachineID:           enrollResult.MachineID,
		MachineToken:        enrollResult.MachineToken,
		ViewerToken:         enrollResult.ViewerToken,
		ExpiresAt:           enrollResult.ExpiresAt,
		VIPFlag:             enrollResult.VIPFlag,
		ReboundExistingUser: enrollResult.ReboundExistingUser,
	}

	a.emitRemoteStateChanged()
	if a.remoteActivationBackgroundDisabled {
		log.Printf("[onboarding] ActivateRemoteSMS background connect skipped")
		log.Printf("[onboarding] ActivateRemoteSMS total=%s", time.Since(start))
		return result, nil
	}
	go func(launchedAt time.Time) {
		if a.remoteSessions == nil {
			a.ensureRemoteInfra()
		}
		hubClient := a.ensureHubClient()
		if hubClient != nil {
			a.emitEvent("ai-assistant-init-progress", "ready")
		}
		if hubClient != nil && !hubClient.IsConnected() {
			if err := hubClient.Connect(); err != nil {
				log.Printf("[onboarding] ActivateRemoteSMS background_connect_failed total=%s err=%v", time.Since(launchedAt), err)
			} else {
				a.emitRemoteStateChanged()
				log.Printf("[onboarding] ActivateRemoteSMS background_connect_total=%s", time.Since(launchedAt))
			}
		}
	}(time.Now())
	log.Printf("[onboarding] ActivateRemoteSMS total=%s", time.Since(start))
	return result, nil
}

func isHTTPTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

func skillMarketAccountFromEnroll(enrollResult *remote.EnrollResult, fallbackPhone string) string {
	if enrollResult == nil {
		if phone := normalizeRemoteRegistrationPhoneNumber(fallbackPhone); phone != "" {
			return "phone:" + phone
		}
		return ""
	}
	if account := strings.TrimSpace(enrollResult.UserID); account != "" {
		return account
	}
	if account := strings.TrimSpace(enrollResult.Email); account != "" {
		return account
	}
	if phone := normalizeRemoteRegistrationPhoneNumber(fallbackPhone); phone != "" {
		return "phone:" + phone
	}
	return ""
}

func normalizedRemotePlatform() string {
	switch remotePlatformGOOS() {
	case "windows":
		return "windows"
	case "darwin":
		return "mac"
	case "linux":
		return "linux"
	default:
		return "linux"
	}
}

func (a *App) GetRemoteActivationStatus() RemoteActivationStatus {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteActivationStatus{}
	}
	return RemoteActivationStatus{
		Activated:  cfg.RemoteMachineID != "" && cfg.RemoteMachineToken != "",
		HubID:      cfg.RemoteHubID,
		Email:      cfg.RemoteEmail,
		SN:         cfg.RemoteSN,
		TenantID:   cfg.RemoteTenantID,
		TenantName: cfg.RemoteTenantName,
		MachineID:  cfg.RemoteMachineID,
		HubURL:     cfg.RemoteHubURL,
	}
}

// VerifyRemoteActivation checks with the server whether the current activation
// is still valid. Only an explicit blocked status clears local machine
// credentials. A not_found probe can be caused by Hub routing, tenant lookup,
// or temporary server-side data drift, so keep credentials and let reconnect or
// a later verification recover instead of silently deleting a usable binding.
// Returns true if activation is still valid, false if it was invalidated.
func (a *App) VerifyRemoteActivation() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true // can't verify, assume valid
	}
	if cfg.RemoteMachineID == "" || cfg.RemoteMachineToken == "" {
		return false // not activated
	}
	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	email := strings.TrimSpace(cfg.RemoteEmail)
	if hubURL == "" || email == "" {
		return true
	}

	probe, err := a.ProbeRemoteHub(hubURL, email)
	if err != nil {
		// Network error - don't clear, assume valid
		return true
	}

	statusKind := normalizeRemoteProbeStatusKind(probe.Status)
	if statusKind == remoteProbeStatusNotFound {
		log.Printf("[verify-activation] server reports status=%q for %s - keeping local machine credentials (probe response: %+v)", probe.Status, email, probe)
		return true
	}
	if statusKind.ShouldClearActivation() {
		// Server explicitly reports this user as blocked.
		// Log the full probe response for post-mortem diagnosis. This is an
		// irreversible action - if it ever happens spuriously, the log entry
		// will help identify the root cause.
		log.Printf("[verify-activation] server reports status=%q for %s - clearing local machine credentials (probe response: %+v)", probe.Status, email, probe)
		a.clearMachineCredentials()
		return false
	}
	return true
}

// clearMachineCredentials removes machine_id, machine_token, sn, and user_id
// from the local config while preserving email and hub URL. It also disconnects
// the hub client. This is used when the server invalidates the user so they
// can re-register without re-entering connection details.
func (a *App) clearMachineCredentials() {
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		a.remoteSessions.hubClient.allowReconnect.Store(false)
		_ = a.remoteSessions.hubClient.Disconnect()
	}

	_ = a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteSN = ""
		cfg.RemoteUserID = ""
		cfg.RemoteTenantID = ""
		cfg.RemoteTenantName = ""
		cfg.RemoteMachineID = ""
		cfg.RemoteMachineName = ""
		cfg.RemoteMachineToken = ""
		cfg.RemoteViewerToken = ""
		cfg.RemoteNickname = ""
		cfg.RemoteHubID = ""
	})

	a.emitRemoteStateChanged()
}

func (a *App) ClearRemoteActivation() error {
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		_ = a.remoteSessions.hubClient.Disconnect()
	}

	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteEmail = ""
		cfg.RemoteMobile = ""
		cfg.RemoteSN = ""
		cfg.RemoteUserID = ""
		cfg.RemoteTenantID = ""
		cfg.RemoteTenantName = ""
		cfg.RemoteMachineID = ""
		cfg.RemoteMachineName = ""
		cfg.RemoteMachineToken = ""
		cfg.RemoteViewerToken = ""
		cfg.RemoteNickname = ""
		cfg.RemoteHubID = ""
		cfg.RemoteHubURL = ""
		cfg.RemoteHubCenterURL = ""
		cfg.RemoteHubCenterURLs = nil
	}); err != nil {
		return err
	}

	a.emitRemoteStateChanged()
	return nil
}

func (a *App) ListRemoteHubs(centerURL string, email string) ([]RemoteHubCenterHub, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}

	email = strings.TrimSpace(email)
	if email == "" {
		email = strings.TrimSpace(cfg.RemoteEmail)
	}
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	// Delegate to the shared enrollment client for hub resolution.
	enrollClient := &remote.EnrollmentClient{HTTPClient: hubHTTPClient}
	result, usedCenter, ordered, err := enrollClient.ResolveHubs(
		context.Background(),
		email,
		"", // no invitation code for hub listing
		strings.TrimSpace(centerURL),
		cfg.HubCenterBaseURLs(defaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs),
	)
	if err != nil {
		return nil, err
	}

	// Persist via the unique enrollment aligner (drops unregistered HA peers /
	// official defaults that were only used as discovery seeds).
	if usedCenter != "" {
		go a.rememberHubCenterSelection(usedCenter, ordered)
	}

	if len(result.Hubs) == 0 {
		msg := result.Message
		if msg == "" {
			msg = "no available hubs found"
		}
		return nil, fmt.Errorf("%s", msg)
	}

	hubs := make([]RemoteHubCenterHub, 0, len(result.Hubs))
	for _, hub := range result.Hubs {
		hubs = append(hubs, RemoteHubCenterHub{
			HubID:          hub.HubID,
			Name:           hub.Name,
			BaseURL:        strings.TrimRight(strings.TrimSpace(hub.BaseURL), "/"),
			PWAURL:         strings.TrimSpace(hub.PWAURL),
			Visibility:     hub.Visibility,
			EnrollmentMode: hub.EnrollmentMode,
			Status:         hub.Status,
		})
	}

	return hubs, nil
}

// generateClientID delegates to the shared corelib implementation.
func generateClientID() string {
	return remote.GenerateClientID()
}

// acquireSkillMarketTokenAfterEnroll calls the HubCenter machine-login endpoint
// to obtain a SkillMarket session token using the Hub enrollment credentials.
// Runs in background - failure is non-fatal.
func (a *App) acquireSkillMarketTokenAfterEnroll(account, machineID, viewerToken string) {
	if account == "" || viewerToken == "" {
		return
	}
	if !a.shouldAttemptSkillMarketAutoLogin() {
		return
	}
	defer a.skillMarketAutoLoginRunning.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[skillmarket-auto-login] load config failed: %v", err)
		return
	}
	// Skip if user already has a valid SkillMarket token.
	if strings.TrimSpace(cfg.SkillMarketSessionToken) != "" {
		return
	}

	baseURL := NewSkillMarketClient(a).baseURL()
	if strings.TrimSpace(baseURL) == "" {
		a.deferSkillMarketAutoLoginRetry(skillMarketAutoLoginRetryDelay)
		log.Printf("[skillmarket-auto-login] no dynamically confirmed HubCenter URL available")
		return
	}
	client := remote.NewSkillMarketAuthClient()
	result, err := client.MachineLogin(ctx, baseURL, account, machineID, viewerToken)
	if err != nil {
		a.deferSkillMarketAutoLoginRetry(skillMarketAutoLoginRetryDelay)
		log.Printf("[skillmarket-auto-login] machine-login failed (non-fatal): %v", err)
		return
	}
	if result.SessionToken == "" {
		a.deferSkillMarketAutoLoginRetry(skillMarketAutoLoginRetryDelay)
		log.Printf("[skillmarket-auto-login] empty token returned")
		return
	}

	// Persist token.
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SkillMarketSessionToken = result.SessionToken
	}); err != nil {
		log.Printf("[skillmarket-auto-login] save token failed: %v", err)
		return
	}
	a.skillMarketAutoLoginNextAttempt.Store(time.Time{})
	log.Printf("[skillmarket-auto-login] success account=%s", account)
}

func (a *App) shouldAttemptSkillMarketAutoLogin() bool {
	if a == nil {
		return false
	}
	now := time.Now()
	if next, ok := a.skillMarketAutoLoginNextAttempt.Load().(time.Time); ok && !next.IsZero() && now.Before(next) {
		return false
	}
	return a.skillMarketAutoLoginRunning.CompareAndSwap(false, true)
}

func (a *App) deferSkillMarketAutoLoginRetry(delay time.Duration) {
	if a == nil {
		return
	}
	if delay <= 0 {
		delay = skillMarketAutoLoginRetryDelay
	}
	a.skillMarketAutoLoginNextAttempt.Store(time.Now().Add(delay))
}
