package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	registrationSMSBusinessRegister          = "registration"
	registrationSMSBusinessChangeBoundPhone  = "change_bound_phone"
	registrationSMSBusinessResetPassword     = "reset_password"
	registrationSMSBusinessBindNewPhone      = "bind_new_phone"
	registrationSMSBusinessVerifyBoundPhone  = "verify_bound_phone"
	registrationSMSCheckVerifyCodeActionName = "CheckSmsVerifyCode"
	registrationSMSSendVerifyCodeActionName  = "SendSmsVerifyCode"
)

var registrationSMSTemplateByBusiness = map[string]string{
	registrationSMSBusinessRegister:         "100001",
	registrationSMSBusinessChangeBoundPhone: "100002",
	registrationSMSBusinessResetPassword:    "100003",
	registrationSMSBusinessBindNewPhone:     "100004",
	registrationSMSBusinessVerifyBoundPhone: "100005",
}

type aliyunSMSVerifyCodeSendRequest struct {
	Action        string `json:"action"`
	PhoneNumber   string `json:"phone_number"`
	SignName      string `json:"sign_name"`
	TemplateCode  string `json:"template_code"`
	TemplateParam string `json:"template_param"`
	CodeLength    int    `json:"code_length"`
	TTLMinutes    int    `json:"ttl_minutes"`
}

type aliyunSMSVerifyCodeCheckRequest struct {
	Action      string `json:"action"`
	PhoneNumber string `json:"phone_number"`
	VerifyCode  string `json:"verify_code"`
}

func buildAliyunSMSVerifyCodeSendRequest(cfg RegistrationAuthConfig, business, phoneNumber string) (aliyunSMSVerifyCodeSendRequest, error) {
	cfg = normalizeRegistrationAuthConfig(cfg)
	if err := validateRegistrationAuthConfig(cfg); err != nil {
		return aliyunSMSVerifyCodeSendRequest{}, err
	}
	phoneNumber = normalizePhoneNumber(phoneNumber)
	if !validRegistrationPhoneNumber(phoneNumber) {
		return aliyunSMSVerifyCodeSendRequest{}, errInvalidRegistrationAuth("valid phone number is required")
	}
	templateCode, ok := registrationSMSTemplateByBusiness[strings.TrimSpace(business)]
	if !ok {
		return aliyunSMSVerifyCodeSendRequest{}, errInvalidRegistrationAuth("unknown SMS verification business")
	}
	param, err := json.Marshal(map[string]string{
		"code": "##code##",
		"min":  fmt.Sprintf("%d", cfg.CodeTTLMinutes),
	})
	if err != nil {
		return aliyunSMSVerifyCodeSendRequest{}, err
	}
	return aliyunSMSVerifyCodeSendRequest{
		Action:        registrationSMSSendVerifyCodeActionName,
		PhoneNumber:   phoneNumber,
		SignName:      cfg.AliyunSignName,
		TemplateCode:  templateCode,
		TemplateParam: string(param),
		CodeLength:    cfg.CodeLength,
		TTLMinutes:    cfg.CodeTTLMinutes,
	}, nil
}

func buildAliyunSMSVerifyCodeCheckRequest(phoneNumber, code string, codeLength ...int) (aliyunSMSVerifyCodeCheckRequest, error) {
	phoneNumber = normalizePhoneNumber(phoneNumber)
	code = strings.TrimSpace(code)
	if !validRegistrationPhoneNumber(phoneNumber) {
		return aliyunSMSVerifyCodeCheckRequest{}, errInvalidRegistrationAuth("valid phone number is required")
	}
	wantLength := registrationAuthDefaultCodeLength
	if len(codeLength) > 0 && codeLength[0] > 0 {
		wantLength = codeLength[0]
	}
	if len(code) != wantLength {
		return aliyunSMSVerifyCodeCheckRequest{}, errInvalidRegistrationAuth(fmt.Sprintf("verification code must be %d digits", wantLength))
	}
	if !isDigitsOnly(code) {
		return aliyunSMSVerifyCodeCheckRequest{}, errInvalidRegistrationAuth("verification code must contain digits only")
	}
	return aliyunSMSVerifyCodeCheckRequest{
		Action:      registrationSMSCheckVerifyCodeActionName,
		PhoneNumber: phoneNumber,
		VerifyCode:  code,
	}, nil
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
