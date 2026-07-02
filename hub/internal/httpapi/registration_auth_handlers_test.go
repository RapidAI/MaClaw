package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetRegistrationAuthConfigDefaultsToEmail(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/settings/registration-auth", nil)
	rr := httptest.NewRecorder()

	GetRegistrationAuthConfigHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got RegistrationAuthConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Method != registrationAuthMethodEmail {
		t.Fatalf("method = %q, want email", got.Method)
	}
	if got.AliyunSMSBuyURL != registrationAuthAliyunSMSBuyURL {
		t.Fatalf("buy url = %q", got.AliyunSMSBuyURL)
	}
	if got.AliyunSignName != registrationAuthDefaultSignName || got.AliyunTemplateCode != registrationAuthDefaultTemplate || got.CodeTTLMinutes != registrationAuthDefaultTTLMinutes || got.CodeLength != registrationAuthDefaultCodeLength || got.DailySMSLimit != registrationAuthDefaultDailyLimit {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestUpdateRegistrationAuthConfigRequiresAliyunKeysForPhone(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	body := bytes.NewBufferString(`{"method":"phone","aliyun_access_key_id":"ak","aliyun_sign_name":"速通互联验证平台","aliyun_template_code":"100001"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/settings/registration-auth", body)
	rr := httptest.NewRecorder()

	UpdateRegistrationAuthConfigHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUpdateRegistrationAuthConfigSavesPhoneSettings(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	body := bytes.NewBufferString(`{"method":"phone","aliyun_access_key_id":"ak","aliyun_access_key_secret":"secret","aliyun_sign_name":"速通互联验证平台","aliyun_template_code":"100003","code_ttl_minutes":5,"code_length":4}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/settings/registration-auth", body)
	rr := httptest.NewRecorder()

	UpdateRegistrationAuthConfigHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got RegistrationAuthConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Method != registrationAuthMethodPhone || got.AliyunAccessKeyID != "ak" || got.AliyunAccessKeySecret != "secret" {
		t.Fatalf("unexpected config: %+v", got)
	}
	if got.AliyunSignName != registrationAuthDefaultSignName || got.AliyunTemplateCode != registrationAuthDefaultTemplate || got.CodeTTLMinutes != 5 || got.CodeLength != 4 {
		t.Fatalf("unexpected aliyun sms config: %+v", got)
	}
	raw := settings.values[registrationAuthConfigKey]
	if !strings.Contains(raw, `"method":"phone"`) || !strings.Contains(raw, `"aliyun_access_key_secret":"secret"`) {
		t.Fatalf("stored config = %s", raw)
	}
}

func TestUpdateRegistrationAuthConfigSavesDailySMSLimit(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	body := bytes.NewBufferString(`{"method":"phone","aliyun_access_key_id":"ak","aliyun_access_key_secret":"secret","aliyun_sign_name":"sms-platform","code_ttl_minutes":5,"code_length":4,"daily_sms_limit":6}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/settings/registration-auth", body)
	rr := httptest.NewRecorder()

	UpdateRegistrationAuthConfigHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got RegistrationAuthConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.DailySMSLimit != 6 {
		t.Fatalf("daily sms limit = %d, want 6; config=%+v", got.DailySMSLimit, got)
	}
	raw := settings.values[registrationAuthConfigKey]
	if !strings.Contains(raw, `"daily_sms_limit":6`) {
		t.Fatalf("stored config = %s", raw)
	}
}

func TestPublicRegistrationAuthConfigIncludesDailySMSLimit(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	body := bytes.NewBufferString(`{"method":"phone","aliyun_access_key_id":"ak","aliyun_access_key_secret":"secret","aliyun_sign_name":"sms-platform","code_ttl_minutes":5,"code_length":4,"daily_sms_limit":6}`)
	saveReq := httptest.NewRequest(http.MethodPut, "/api/admin/settings/registration-auth", body)
	saveRec := httptest.NewRecorder()
	UpdateRegistrationAuthConfigHandler(settings).ServeHTTP(saveRec, saveReq)
	if saveRec.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", saveRec.Code, saveRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/enroll/registration-auth", nil)
	rr := httptest.NewRecorder()
	PublicRegistrationAuthConfigHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("public status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["daily_sms_limit"] != float64(6) {
		t.Fatalf("daily_sms_limit = %#v body=%s", got["daily_sms_limit"], rr.Body.String())
	}
	if _, ok := got["aliyun_access_key_secret"]; ok {
		t.Fatalf("public config leaked secret: %s", rr.Body.String())
	}
}

func TestBuildAliyunSMSVerifyCodeSendRequestUsesBusinessTemplate(t *testing.T) {
	cfg := RegistrationAuthConfig{
		Method:                registrationAuthMethodPhone,
		AliyunAccessKeyID:     "ak",
		AliyunAccessKeySecret: "secret",
		AliyunSignName:        registrationAuthDefaultSignName,
		CodeTTLMinutes:        7,
		CodeLength:            4,
	}
	got, err := buildAliyunSMSVerifyCodeSendRequest(cfg, registrationSMSBusinessRegister, "19900001111")
	if err != nil {
		t.Fatalf("build send request: %v", err)
	}
	if got.Action != registrationSMSSendVerifyCodeActionName || got.TemplateCode != "100001" || got.SignName != registrationAuthDefaultSignName || got.PhoneNumber != "19900001111" || got.CodeLength != 4 {
		t.Fatalf("unexpected send request: %+v", got)
	}
	if got.TemplateParam != `{"code":"##code##","min":"7"}` {
		t.Fatalf("template param = %s", got.TemplateParam)
	}

	reset, err := buildAliyunSMSVerifyCodeSendRequest(cfg, registrationSMSBusinessResetPassword, "19900001111")
	if err != nil {
		t.Fatalf("build reset request: %v", err)
	}
	if reset.TemplateCode != "100003" {
		t.Fatalf("reset template = %q, want 100003", reset.TemplateCode)
	}
}

func TestBuildAliyunSMSVerifyCodeCheckRequest(t *testing.T) {
	got, err := buildAliyunSMSVerifyCodeCheckRequest("19900001111", "3032")
	if err != nil {
		t.Fatalf("build check request: %v", err)
	}
	if got.Action != registrationSMSCheckVerifyCodeActionName || got.PhoneNumber != "19900001111" || got.VerifyCode != "3032" {
		t.Fatalf("unexpected check request: %+v", got)
	}
}
