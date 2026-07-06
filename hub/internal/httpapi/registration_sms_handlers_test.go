package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type fakeRegistrationSMSProvider struct {
	sendReq   aliyunSMSVerifyCodeSendRequest
	checkReq  aliyunSMSVerifyCodeCheckRequest
	checkPass bool
	sendCount int
	sendErr   error
}

func (p *fakeRegistrationSMSProvider) SendVerifyCode(ctx context.Context, req aliyunSMSVerifyCodeSendRequest) error {
	_ = ctx
	p.sendReq = req
	p.sendCount++
	return p.sendErr
}

func (p *fakeRegistrationSMSProvider) CheckVerifyCode(ctx context.Context, req aliyunSMSVerifyCodeCheckRequest) (bool, error) {
	_ = ctx
	p.checkReq = req
	return p.checkPass, nil
}

type fakeRegistrationSMSRouteSyncer struct {
	allowed     bool
	targetHubID string
	checked     bool
}

func (s *fakeRegistrationSMSRouteSyncer) SyncUserRoute(ctx context.Context, email string, tenantIDOpt ...string) error {
	_, _, _ = ctx, email, tenantIDOpt
	return nil
}

func (s *fakeRegistrationSMSRouteSyncer) SyncUserRouteReplaceAll(ctx context.Context, email string, tenantIDOpt ...string) error {
	_, _, _ = ctx, email, tenantIDOpt
	return nil
}

func (s *fakeRegistrationSMSRouteSyncer) AllowsUserRoute(ctx context.Context, email string, tenantIDOpt ...string) (bool, string, error) {
	_, _, _ = ctx, email, tenantIDOpt
	s.checked = true
	return s.allowed, s.targetHubID, nil
}

var _ auth.UserRouteSyncer = (*fakeRegistrationSMSRouteSyncer)(nil)
var _ auth.UserRouteValidator = (*fakeRegistrationSMSRouteSyncer)(nil)

func TestEnsurePhoneIdentityCanRegisterAllowsCurrentUserToClaimPlaceholder(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	ctx := auth.WithTenant(context.Background(), store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(ctx, "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start email enrollment: %v", err)
	}
	if _, err := identity.StartEnrollment(ctx, "phone:19900001111", "phone-desk", "windows", "client-phone", "", auth.WithPhoneVerifiedRegistration()); err != nil {
		t.Fatalf("start phone placeholder enrollment: %v", err)
	}
	currentUser, err := identity.UsersRepo().GetByID(ctx, enrolled.UserID)
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}

	err = ensurePhoneIdentityCanRegister(context.Background(), identity, store.DefaultTenantID, "phone:19900001111", currentUser)
	if err != nil {
		t.Fatalf("current user should be able to claim placeholder phone identity: %v", err)
	}
	err = ensurePhoneIdentityCanRegister(context.Background(), identity, store.DefaultTenantID, "phone:19900001111", nil)
	if _, ok := err.(errPhoneAlreadyRegistered); !ok {
		t.Fatalf("anonymous registration should still see placeholder as registered, got %T %v", err, err)
	}
}

func TestEnsurePhoneIdentityCanRegisterAllowsCurrentUserToClaimPlaceholderWithStaleRoute(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	ctx := auth.WithTenant(context.Background(), store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(ctx, "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start email enrollment: %v", err)
	}
	if _, err := identity.StartEnrollment(ctx, "phone:19900001111", "phone-desk", "windows", "client-phone", "", auth.WithPhoneVerifiedRegistration()); err != nil {
		t.Fatalf("start phone placeholder enrollment: %v", err)
	}
	currentUser, err := identity.UsersRepo().GetByID(ctx, enrolled.UserID)
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}
	routeSyncer := &fakeRegistrationSMSRouteSyncer{allowed: false, targetHubID: "stale-hub"}
	identity.SetUserRouteSyncer(routeSyncer)

	err = ensurePhoneIdentityCanRegister(context.Background(), identity, store.DefaultTenantID, "phone:19900001111", currentUser)
	if err != nil {
		t.Fatalf("current user should be able to claim placeholder phone identity despite stale route: %v", err)
	}
	if routeSyncer.checked {
		t.Fatal("stale route should not be checked after local placeholder is claimable")
	}
}

func TestEnsurePhoneIdentityCanRegisterRejectsCrossTenantPlaceholderClaim(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	ctx := auth.WithTenant(context.Background(), store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(ctx, "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start email enrollment: %v", err)
	}
	if _, err := identity.StartEnrollment(auth.WithTenant(context.Background(), "tenant_other"), "phone:19900001111", "phone-desk", "windows", "client-phone", "", auth.WithPhoneVerifiedRegistration()); err != nil {
		t.Fatalf("start other tenant phone placeholder enrollment: %v", err)
	}
	currentUser, err := identity.UsersRepo().GetByID(ctx, enrolled.UserID)
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}

	err = ensurePhoneIdentityCanRegister(context.Background(), identity, store.DefaultTenantID, "phone:19900001111", currentUser)
	if _, ok := err.(errPhoneAlreadyRegistered); !ok {
		t.Fatalf("cross-tenant placeholder claim should be rejected, got %T %v", err, err)
	}
}

func TestCanClaimPhoneIdentityForCurrentUserAllowsSameEmailDuplicate(t *testing.T) {
	current := &store.User{ID: "current", TenantID: store.DefaultTenantID, Email: "znsoft@163.com"}
	duplicateSameEmail := &store.User{ID: "duplicate", TenantID: store.DefaultTenantID, Email: "ZNSOFT@163.com"}
	if !canClaimPhoneIdentityForCurrentUser(duplicateSameEmail, current, "phone:17090134628") {
		t.Fatal("same-tenant duplicate with same email should be claimable")
	}
	otherEmail := &store.User{ID: "other", TenantID: store.DefaultTenantID, Email: "other@example.com"}
	if canClaimPhoneIdentityForCurrentUser(otherEmail, current, "phone:17090134628") {
		t.Fatal("different email owner should not be claimable")
	}
}

func TestRegistrationCurrentProfileHandlerReturnsBoundPhoneForMachine(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	ctx := auth.WithTenant(context.Background(), store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(ctx, "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	user, err := identity.UsersRepo().GetByID(ctx, enrolled.UserID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if err := identity.BindVerifiedPhoneToUser(ctx, user, "17090134628"); err != nil {
		t.Fatalf("BindVerifiedPhoneToUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/enroll/profile/current", nil)
	req.Header.Set("X-Machine-ID", enrolled.MachineID)
	req.Header.Set("Authorization", "Bearer "+enrolled.MachineToken)
	rr := httptest.NewRecorder()

	RegistrationCurrentProfileHandler(identity).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		OK          bool   `json:"ok"`
		UserID      string `json:"user_id"`
		MachineID   string `json:"machine_id"`
		Email       string `json:"email"`
		PhoneNumber string `json:"phone_number"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.OK || body.UserID != enrolled.UserID || body.MachineID != enrolled.MachineID || body.Email != "owner@example.com" || body.PhoneNumber != "17090134628" {
		t.Fatalf("profile body = %+v", body)
	}
}

func TestRegistrationSMSSendCodeHandlerUsesTenantPhoneConfig(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	settings := &testSystemSettingsRepo{}
	saveRegistrationAuthTestConfig(t, settings, "tenant_acme")
	provider := &fakeRegistrationSMSProvider{}

	body := bytes.NewBufferString(`{"tenant_id":"tenant_acme","phone_number":"19900001111"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, settings, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		if cfg.AliyunAccessKeyID != "ak" || cfg.AliyunAccessKeySecret != "secret" {
			t.Fatalf("unexpected config passed to provider: %+v", cfg)
		}
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.sendReq.PhoneNumber != "19900001111" || provider.sendReq.TemplateCode != "100001" || provider.sendReq.TemplateParam != `{"code":"##code##","min":"5"}` {
		t.Fatalf("unexpected send request: %+v", provider.sendReq)
	}
	if !strings.Contains(rr.Body.String(), `"tenant_id":"tenant_acme"`) || !strings.Contains(rr.Body.String(), `"code_length":6`) {
		t.Fatalf("unexpected response: %s", rr.Body.String())
	}
}

func TestRegistrationSMSSendCodeHandlerRejectsExistingPhoneUserBeforeSending(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	if _, err := identity.ManualBindForTenant(context.Background(), "tenant_other", "phone:19900001111"); err != nil {
		t.Fatalf("create phone user: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{}

	body := bytes.NewBufferString(`{"phone_number":"19900001111"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "PHONE_ALREADY_REGISTERED") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.sendReq.PhoneNumber != "" {
		t.Fatalf("expected SMS provider not to be called, got %+v", provider.sendReq)
	}
}

func TestRegistrationSMSVerifyAndStartWithMachineCredentialsBindsCurrentMachineUser(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	sendBody := bytes.NewBufferString(`{"phone_number":"19900001111"}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", sendBody)
	sendRR := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(sendRR, sendReq)
	if sendRR.Code != http.StatusOK {
		t.Fatalf("send status = %d body=%s", sendRR.Code, sendRR.Body.String())
	}
	if provider.sendReq.TemplateCode != registrationSMSTemplateByBusiness[registrationSMSBusinessRegister] {
		t.Fatalf("template = %q, want registration", provider.sendReq.TemplateCode)
	}
	if !strings.Contains(sendRR.Body.String(), `"purpose":"registration"`) {
		t.Fatalf("send response missing registration purpose: %s", sendRR.Body.String())
	}

	verifyBody := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"123456","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", verifyBody)
	verifyRR := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(verifyRR, verifyReq)
	if verifyRR.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", verifyRR.Code, verifyRR.Body.String())
	}
	if !strings.Contains(verifyRR.Body.String(), `"kind":"phone"`) {
		t.Fatalf("verify response should be profile phone bind response, got %s", verifyRR.Body.String())
	}
	var verifyGot struct {
		PhoneNumber    string `json:"phone_number"`
		CreditsAccount string `json:"credits_account"`
	}
	if err := json.Unmarshal(verifyRR.Body.Bytes(), &verifyGot); err != nil {
		t.Fatalf("decode verify response: %v", err)
	}
	if verifyGot.PhoneNumber != "19900001111" || verifyGot.CreditsAccount != "phone:19900001111" {
		t.Fatalf("unexpected verify credits response: %+v body=%s", verifyGot, verifyRR.Body.String())
	}
	user, err := identity.LookupUserByPhone(auth.WithTenant(context.Background(), store.DefaultTenantID), "19900001111")
	if err != nil || user == nil || user.ID != enrolled.UserID {
		t.Fatalf("bound phone user = %#v err=%v, want user %s", user, err, enrolled.UserID)
	}
}

func TestRegistrationSMSVerifyAndStartWithMachineCredentialsClaimsPhoneIdentityUser(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start email enrollment: %v", err)
	}
	phoneUser, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "phone:19900001111", "phone-desk", "windows", "client-phone", "", auth.WithPhoneVerifiedRegistration())
	if err != nil {
		t.Fatalf("start phone enrollment: %v", err)
	}
	if phoneUser.UserID == enrolled.UserID {
		t.Fatal("test setup should create a distinct synthetic phone user")
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	sendBody := bytes.NewBufferString(`{"phone_number":"19900001111","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", sendBody)
	sendRR := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(sendRR, sendReq)
	if sendRR.Code != http.StatusOK {
		t.Fatalf("send status = %d body=%s", sendRR.Code, sendRR.Body.String())
	}
	if provider.sendCount != 1 {
		t.Fatalf("provider send count = %d, want 1", provider.sendCount)
	}

	verifyBody := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"123456","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", verifyBody)
	verifyRR := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(verifyRR, verifyReq)
	if verifyRR.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", verifyRR.Code, verifyRR.Body.String())
	}
	user, err := identity.LookupUserByPhone(auth.WithTenant(context.Background(), store.DefaultTenantID), "19900001111")
	if err != nil || user == nil || user.ID != enrolled.UserID {
		t.Fatalf("bound phone user = %#v err=%v, want user %s", user, err, enrolled.UserID)
	}
}

func TestRegistrationSMSVerifyAndStartWithMachineCredentialsClaimsLegacyPhoneOnlyUser(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start email enrollment: %v", err)
	}
	now := time.Now().UTC()
	legacyUser := &store.User{
		ID:               "legacy_phone_only_user",
		TenantID:         store.DefaultTenantID,
		Email:            "",
		SN:               "LEGACY001",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := identity.UsersRepo().Create(context.Background(), legacyUser); err != nil {
		t.Fatalf("create legacy user: %v", err)
	}
	if err := identity.UsersRepo().UpsertIdentity(context.Background(), &store.UserIdentity{
		ID:         "legacy_phone_only_identity",
		TenantID:   store.DefaultTenantID,
		UserID:     legacyUser.ID,
		Type:       "phone",
		Value:      "19900001111",
		Verified:   true,
		VerifiedAt: &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed legacy phone identity: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	sendBody := bytes.NewBufferString(`{"phone_number":"19900001111","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", sendBody)
	sendRR := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(sendRR, sendReq)
	if sendRR.Code != http.StatusOK {
		t.Fatalf("send status = %d body=%s", sendRR.Code, sendRR.Body.String())
	}

	verifyBody := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"123456","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", verifyBody)
	verifyRR := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(verifyRR, verifyReq)
	if verifyRR.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", verifyRR.Code, verifyRR.Body.String())
	}
	user, err := identity.LookupUserByPhone(auth.WithTenant(context.Background(), store.DefaultTenantID), "19900001111")
	if err != nil || user == nil || user.ID != enrolled.UserID {
		t.Fatalf("bound phone user = %#v err=%v, want user %s", user, err, enrolled.UserID)
	}
}

func TestRegistrationSMSVerifyAndStartWithMachineCredentialsRejectsPhoneUserWithEmailIdentity(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start email enrollment: %v", err)
	}
	now := time.Now().UTC()
	otherUser := &store.User{
		ID:               "legacy_real_user",
		TenantID:         store.DefaultTenantID,
		Email:            "",
		SN:               "REAL001",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := identity.UsersRepo().Create(context.Background(), otherUser); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if err := identity.UsersRepo().UpsertIdentity(context.Background(), &store.UserIdentity{
		ID:         "legacy_real_phone_identity",
		TenantID:   store.DefaultTenantID,
		UserID:     otherUser.ID,
		Type:       "phone",
		Value:      "19900001111",
		Verified:   true,
		VerifiedAt: &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed phone identity: %v", err)
	}
	if err := identity.UsersRepo().UpsertIdentity(context.Background(), &store.UserIdentity{
		ID:         "legacy_real_email_identity",
		TenantID:   store.DefaultTenantID,
		UserID:     otherUser.ID,
		Type:       "email",
		Value:      "other@example.com",
		Verified:   true,
		VerifiedAt: &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed email identity: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	body := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"123456","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", body)
	rr := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "PHONE_ALREADY_REGISTERED") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	user, err := identity.LookupUserByPhone(auth.WithTenant(context.Background(), store.DefaultTenantID), "19900001111")
	if err != nil || user == nil || user.ID != otherUser.ID {
		t.Fatalf("phone owner = %#v err=%v, want other user %s", user, err, otherUser.ID)
	}
}

func TestRegistrationSMSVerifyAndStartWithMachineCredentialsUsesMachineTenantConfig(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	tenantID := "tenant_acme"
	cfg := normalizeRegistrationAuthConfig(RegistrationAuthConfig{
		Method:                registrationAuthMethodPhone,
		AliyunAccessKeyID:     "ak-acme",
		AliyunAccessKeySecret: "secret-acme",
		AliyunSignName:        registrationAuthDefaultSignName,
		AliyunTemplateCode:    registrationAuthDefaultTemplate,
		CodeTTLMinutes:        5,
		CodeLength:            4,
		DailySMSLimit:         registrationAuthDefaultDailyLimit,
	})
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := ScopedSystemSettingsForTenant(tenantID, st.System).Set(context.Background(), registrationAuthConfigKey, string(data)); err != nil {
		t.Fatalf("save tenant config: %v", err)
	}
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), tenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	verifyBody := bytes.NewBufferString(`{"tenant_id":"` + tenantID + `","phone_number":"19900001111","verify_code":"1234","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", verifyBody)
	verifyRR := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(got RegistrationAuthConfig) registrationSMSProvider {
		if got.AliyunAccessKeyID != "ak-acme" || got.CodeLength != 4 {
			t.Fatalf("provider config = %+v, want tenant_acme config", got)
		}
		return provider
	}).ServeHTTP(verifyRR, verifyReq)

	if verifyRR.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", verifyRR.Code, verifyRR.Body.String())
	}
	if provider.checkReq.VerifyCode != "1234" {
		t.Fatalf("check code = %q, want 4-digit tenant code", provider.checkReq.VerifyCode)
	}
	if !strings.Contains(verifyRR.Body.String(), `"tenant_id":"tenant_acme"`) {
		t.Fatalf("verify response should use machine tenant: %s", verifyRR.Body.String())
	}
}

func TestRegistrationSMSSendCodeWithMachineCredentialsRejectsOtherUsersPhone(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	other, err := identity.ManualBindForTenant(context.Background(), store.DefaultTenantID, "other@example.com")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	if err := identity.BindVerifiedPhoneToUser(auth.WithTenant(context.Background(), store.DefaultTenantID), other, "19900001111"); err != nil {
		t.Fatalf("bind other phone: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{}

	body := bytes.NewBufferString(`{"phone_number":"19900001111","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "PHONE_ALREADY_REGISTERED") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.sendCount != 0 {
		t.Fatalf("provider send count = %d, want 0", provider.sendCount)
	}
}

func TestRegistrationContactPhoneSendUsesRegistrationSMSEndpointCompat(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{}
	body := bytes.NewBufferString(`{"kind":"phone","phone_number":"19900001111","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/profile/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationContactSendCodeHandler(identity, nil, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK || provider.sendCount != 1 || !strings.Contains(rr.Body.String(), `"purpose":"registration"`) {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegistrationContactPhoneVerifyUsesRegistrationSMSEndpointCompat(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}
	body := bytes.NewBufferString(`{"kind":"phone","phone_number":"19900001111","verify_code":"123456","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/profile/verify", body)
	rr := httptest.NewRecorder()
	RegistrationContactVerifyHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"kind":"phone"`) {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	user, err := identity.LookupUserByPhone(auth.WithTenant(context.Background(), store.DefaultTenantID), "19900001111")
	if err != nil || user == nil || user.ID != enrolled.UserID {
		t.Fatalf("bound phone user = %#v err=%v, want user %s", user, err, enrolled.UserID)
	}
}

func TestRegistrationContactEmailSendRejectsExistingTenantUser(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	if _, err := identity.ManualBindForTenant(context.Background(), store.DefaultTenantID, "taken@example.com"); err != nil {
		t.Fatalf("create existing email user: %v", err)
	}

	body := bytes.NewBufferString(`{"kind":"email","email":"taken@example.com","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/profile/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationContactSendCodeHandler(identity, nil, nil, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "EMAIL_ALREADY_REGISTERED") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegistrationContactEmailSendWithoutMailerDoesNotReserveCode(t *testing.T) {
	identity, _, _ := newPreservationTestIdentity(t)
	enrolled, err := identity.StartEnrollment(auth.WithTenant(context.Background(), store.DefaultTenantID), "owner@example.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	handler := RegistrationContactSendCodeHandler(identity, nil, nil, nil)

	for i := 0; i < 2; i++ {
		body := bytes.NewBufferString(`{"kind":"email","email":"new-contact@example.com","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/enroll/profile/send-code", body)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "MAIL_NOT_CONFIGURED") {
			t.Fatalf("attempt %d status = %d body=%s", i+1, rr.Code, rr.Body.String())
		}
	}
}

func TestRegistrationContactEmailSendRejectsTenantDomainMismatch(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	identity.SetTenantRepository(st.Tenants)
	ctx := context.Background()
	tenantSettings, ok := st.Tenants.(interface {
		UpdateSettings(context.Context, string, string, string, string) error
	})
	if !ok {
		t.Fatal("tenant repository does not support settings updates")
	}
	if err := tenantSettings.UpdateSettings(ctx, store.DefaultTenantID, "Default Tenant", "qianxin.com", `{"email_domains":["qianxin.com"],"allow_user_registration":true}`); err != nil {
		t.Fatalf("update tenant settings: %v", err)
	}
	enrolled, err := identity.StartEnrollment(auth.WithTenant(ctx, store.DefaultTenantID), "owner@qianxin.com", "desk", "windows", "client-1", "")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}

	body := bytes.NewBufferString(`{"kind":"email","email":"znsoft@163.com","machine_id":"` + enrolled.MachineID + `","machine_token":"` + enrolled.MachineToken + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/profile/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationContactSendCodeHandler(identity, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "EMAIL_DOMAIN_NOT_ALLOWED") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegistrationSMSSendCodeHandlerSendsVerifyBoundPhoneForExistingTenantIdentity(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	seedBindUser(t, identity, store.DefaultTenantID, "buyer@example.com")
	user, err := identity.UsersRepo().GetByTenantEmail(context.Background(), store.DefaultTenantID, "buyer@example.com")
	if err != nil || user == nil {
		t.Fatalf("load seeded user: user=%#v err=%v", user, err)
	}
	if err := identity.BindVerifiedPhoneToUser(auth.WithTenant(context.Background(), store.DefaultTenantID), user, "19900001111"); err != nil {
		t.Fatalf("bind phone: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{}

	body := bytes.NewBufferString(`{"phone_number":"19900001111"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.sendReq.TemplateCode != registrationSMSTemplateByBusiness[registrationSMSBusinessVerifyBoundPhone] {
		t.Fatalf("template = %q, want verify-bound-phone", provider.sendReq.TemplateCode)
	}
	if !strings.Contains(rr.Body.String(), `"purpose":"verify_bound_phone"`) {
		t.Fatalf("response missing purpose: %s", rr.Body.String())
	}
}

func TestRegistrationSMSSendCodeHandlerEnforcesDailyPhoneLimit(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfigWithLimit(t, st.System, store.DefaultTenantID, 3)
	provider := &fakeRegistrationSMSProvider{}
	handler := RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	})

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", bytes.NewBufferString(`{"phone_number":"19900001111"}`))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("send %d status = %d body=%s", i+1, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"daily_sms_limit":3`) {
			t.Fatalf("send %d response missing daily limit: %s", i+1, rr.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", bytes.NewBufferString(`{"phone_number":"19900001111"}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests || !strings.Contains(rr.Body.String(), "SMS_DAILY_LIMIT_REACHED") {
		t.Fatalf("fourth send status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.sendCount != 3 {
		t.Fatalf("provider send count = %d, want 3", provider.sendCount)
	}
}

func TestRegistrationSMSSendCodeHandlerReleasesDailyLimitWhenProviderFails(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfigWithLimit(t, st.System, store.DefaultTenantID, 1)
	provider := &fakeRegistrationSMSProvider{sendErr: errors.New("sms down")}
	handler := RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	})

	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", bytes.NewBufferString(`{"phone_number":"19900001111"}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "SMS_VERIFY_SEND_FAILED") {
		t.Fatalf("failed send status = %d body=%s", rr.Code, rr.Body.String())
	}

	provider.sendErr = nil
	req = httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", bytes.NewBufferString(`{"phone_number":"19900001111"}`))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("second send should not be rate-limited after provider failure: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"daily_sms_remaining":0`) {
		t.Fatalf("unexpected second send response: %s", rr.Body.String())
	}
}

func TestReserveRegistrationSMSSendDefaultsLimitWithoutSystemStore(t *testing.T) {
	remaining, err := reserveRegistrationSMSSend(context.Background(), nil, "19900001111", 0, time.Now())
	if err != nil {
		t.Fatalf("reserve without system store: %v", err)
	}
	if remaining != registrationAuthDefaultDailyLimit {
		t.Fatalf("remaining = %d, want default limit %d", remaining, registrationAuthDefaultDailyLimit)
	}
}

func TestReserveRegistrationSMSSendResetsOnNextDay(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	now := time.Date(2026, 7, 2, 23, 59, 0, 0, time.Local)
	if remaining, err := reserveRegistrationSMSSend(context.Background(), settings, "19900001111", 1, now); err != nil || remaining != 0 {
		t.Fatalf("first reserve remaining=%d err=%v", remaining, err)
	}
	if _, err := reserveRegistrationSMSSend(context.Background(), settings, "19900001111", 1, now); err == nil {
		t.Fatal("expected same-day limit error")
	}
	nextDay := now.Add(2 * time.Minute)
	if remaining, err := reserveRegistrationSMSSend(context.Background(), settings, "19900001111", 1, nextDay); err != nil || remaining != 0 {
		t.Fatalf("next-day reserve remaining=%d err=%v", remaining, err)
	}
}

func TestReserveRegistrationSMSSendStoresHashedPhoneKey(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local)
	if _, err := reserveRegistrationSMSSend(context.Background(), settings, "19900001111", 3, now); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	raw := settings.values[registrationSMSDailyUsageKey]
	if strings.Contains(raw, "19900001111") {
		t.Fatalf("daily usage should not store phone number in plaintext: %s", raw)
	}
	var usage registrationSMSDailyUsage
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if len(usage.Counts) != 1 {
		t.Fatalf("counts = %#v", usage.Counts)
	}
	for key, count := range usage.Counts {
		if len(key) != 64 || count != 1 {
			t.Fatalf("unexpected hashed count key=%q count=%d", key, count)
		}
	}
}

func TestRegistrationSMSDailyUsagePhoneHashDoesNotTreatLongDigitsAsHash(t *testing.T) {
	longDigits := "1234567890123456789012345678901234567890123456789012345678901234"
	key := registrationSMSDailyUsagePhoneHash(longDigits)
	if key == longDigits {
		t.Fatal("long digit phone input should be hashed, not treated as an existing hash key")
	}
	if len(key) != 64 || !isHexString(key) {
		t.Fatalf("unexpected key = %q", key)
	}
}

func TestReserveRegistrationSMSSendHonorsLegacyPlaintextPhoneCount(t *testing.T) {
	settings := &testSystemSettingsRepo{values: map[string]string{
		registrationSMSDailyUsageKey: `{"date":"2026-07-02","counts":{"19900001111":1}}`,
	}}
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local)
	if _, err := reserveRegistrationSMSSend(context.Background(), settings, "19900001111", 1, now); err == nil {
		t.Fatal("expected legacy plaintext count to enforce daily limit")
	}
	raw := settings.values[registrationSMSDailyUsageKey]
	if strings.Contains(raw, "19900001111") {
		t.Fatalf("legacy plaintext phone should be cleaned after limit check: %s", raw)
	}
	if !strings.Contains(raw, registrationSMSDailyUsagePhoneHash("19900001111")) {
		t.Fatalf("legacy count should be retained under hashed key: %s", raw)
	}
}

func TestReserveRegistrationSMSSendDropsUnknownLegacyUsageKeys(t *testing.T) {
	settings := &testSystemSettingsRepo{values: map[string]string{
		registrationSMSDailyUsageKey: `{"date":"2026-07-02","counts":{"not-a-phone":5,"19900001111":1}}`,
	}}
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local)
	if _, err := reserveRegistrationSMSSend(context.Background(), settings, "19900001111", 3, now); err != nil {
		t.Fatalf("reserve with legacy keys: %v", err)
	}
	raw := settings.values[registrationSMSDailyUsageKey]
	if strings.Contains(raw, "not-a-phone") || strings.Contains(raw, "19900001111") {
		t.Fatalf("legacy plaintext keys should be cleaned: %s", raw)
	}
}

func TestRegistrationSMSSendCodeHandlerRejectsInvalidPhoneBeforeSending(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	provider := &fakeRegistrationSMSProvider{}

	body := bytes.NewBufferString(`{"phone_number":"12-3"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "INVALID_PHONE_NUMBER") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.sendReq.PhoneNumber != "" {
		t.Fatalf("expected SMS provider not to be called, got %+v", provider.sendReq)
	}
}

func TestRegistrationSMSSendCodeHandlerRejectsOverlongPhoneBeforeSending(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	provider := &fakeRegistrationSMSProvider{}

	body := bytes.NewBufferString(`{"phone_number":"123456789012345678901"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/send-code", body)
	rr := httptest.NewRecorder()
	RegistrationSMSSendCodeHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "INVALID_PHONE_NUMBER") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.sendReq.PhoneNumber != "" {
		t.Fatalf("expected SMS provider not to be called, got %+v", provider.sendReq)
	}
}

func TestRegistrationSMSVerifyAndStartRejectsExistingPhoneUser(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	if _, err := identity.ManualBindForTenant(context.Background(), "tenant_other", "phone:19900001111"); err != nil {
		t.Fatalf("create phone user: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	body := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"303246","machine_name":"desktop","platform":"windows","client_id":"cid-phone"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", body)
	rr := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "PHONE_ALREADY_REGISTERED") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.checkReq.PhoneNumber != "" {
		t.Fatalf("expected SMS provider not to be called, got %+v", provider.checkReq)
	}
}

func TestRegistrationSMSVerifyAndStartRebindsExistingPhoneIdentityToCanonicalUser(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	seedBindUser(t, identity, store.DefaultTenantID, "buyer@example.com")
	user, err := identity.UsersRepo().GetByTenantEmail(context.Background(), store.DefaultTenantID, "buyer@example.com")
	if err != nil || user == nil {
		t.Fatalf("load seeded user: user=%#v err=%v", user, err)
	}
	if err := identity.BindVerifiedPhoneToUser(auth.WithTenant(context.Background(), store.DefaultTenantID), user, "19900001111"); err != nil {
		t.Fatalf("bind phone: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	body := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"303246","machine_name":"desktop","platform":"windows","client_id":"cid-phone-existing"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", body)
	rr := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Email               string `json:"email"`
		PhoneNumber         string `json:"phone_number"`
		ReboundExistingUser bool   `json:"rebound_existing_user"`
		MachineID           string `json:"machine_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Email != "buyer@example.com" || got.PhoneNumber != "19900001111" || !got.ReboundExistingUser || got.MachineID == "" {
		t.Fatalf("unexpected response: %+v body=%s", got, rr.Body.String())
	}
	phoneUser, err := st.Users.GetByTenantEmail(context.Background(), store.DefaultTenantID, "phone:19900001111")
	if err != nil {
		t.Fatalf("load phone account: %v", err)
	}
	if phoneUser != nil {
		t.Fatalf("existing phone identity should not create phone account: %#v", phoneUser)
	}
}

func TestRegistrationSMSVerifyAndStartBackfillsTenantScopedPhoneGrant(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	const tenantID = "tenant_b"
	saveRegistrationAuthTestConfig(t, st.System, tenantID)
	seedBindUser(t, identity, tenantID, "buyer@example.com")
	user, err := identity.UsersRepo().GetByTenantEmail(context.Background(), tenantID, "buyer@example.com")
	if err != nil || user == nil {
		t.Fatalf("load seeded user: user=%#v err=%v", user, err)
	}
	if err := identity.BindVerifiedPhoneToUser(auth.WithTenant(context.Background(), tenantID), user, "19900001111"); err != nil {
		t.Fatalf("bind phone: %v", err)
	}
	tenantSystem := ScopedSystemSettingsForTenant(tenantID, st.System)
	if err := llmservice.SaveRegistry(context.Background(), tenantSystem, &llmservice.Registry{
		Grants: []llmservice.Grant{{ID: "grant-phone", Email: "phone:19900001111", ServiceGroupID: "coding-basic"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	body := bytes.NewBufferString(`{"tenant_id":"tenant_b","phone_number":"19900001111","verify_code":"303246","machine_name":"desktop","platform":"windows","client_id":"cid-phone-existing"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", body)
	rr := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	reg, err := llmservice.LoadRegistry(context.Background(), tenantSystem)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(reg.Grants) != 1 || reg.Grants[0].UserID != user.ID {
		t.Fatalf("expected tenant-scoped phone grant backfilled to user ID, grants=%#v", reg.Grants)
	}
}

func TestRegistrationSMSVerifyAndStartContinuesWhenTenantBackfillFails(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	const tenantID = "tenant_b"
	saveRegistrationAuthTestConfig(t, st.System, tenantID)
	seedBindUser(t, identity, tenantID, "buyer@example.com")
	user, err := identity.UsersRepo().GetByTenantEmail(context.Background(), tenantID, "buyer@example.com")
	if err != nil || user == nil {
		t.Fatalf("load seeded user: user=%#v err=%v", user, err)
	}
	if err := identity.BindVerifiedPhoneToUser(auth.WithTenant(context.Background(), tenantID), user, "19900001111"); err != nil {
		t.Fatalf("bind phone: %v", err)
	}
	tenantSystem := ScopedSystemSettingsForTenant(tenantID, st.System)
	if err := tenantSystem.Set(context.Background(), llmservice.RegistryKey, "{not-json"); err != nil {
		t.Fatalf("seed broken registry: %v", err)
	}
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	body := bytes.NewBufferString(`{"tenant_id":"tenant_b","phone_number":"19900001111","verify_code":"303246","machine_name":"desktop","platform":"windows","client_id":"cid-phone-existing"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", body)
	rr := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRegistrationSMSVerifyAndStartRejectsRoutedPhoneBeforeCheckingCode(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	routeSyncer := &fakeRegistrationSMSRouteSyncer{allowed: false, targetHubID: "hub_other"}
	identity.SetUserRouteSyncer(routeSyncer)
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	body := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"303246","machine_name":"desktop","platform":"windows","client_id":"cid-phone"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", body)
	rr := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "PHONE_ALREADY_REGISTERED") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !routeSyncer.checked {
		t.Fatal("expected route check before SMS verification")
	}
	if provider.checkReq.PhoneNumber != "" {
		t.Fatalf("expected SMS provider check not to be called, got %+v", provider.checkReq)
	}
}

func TestRegistrationSMSEnrollmentRouteErrorUsesPhoneAlreadyRegisteredCode(t *testing.T) {
	rr := httptest.NewRecorder()
	writeEnrollmentStartError(rr, auth.ErrRoutedToAnotherHub, nil)

	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "PHONE_ALREADY_REGISTERED") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "EMAIL_ROUTED_TO_ANOTHER_HUB") {
		t.Fatalf("phone SMS enrollment leaked email route code: %s", rr.Body.String())
	}
}

func TestRegistrationSMSVerifyAndStartCreatesPhoneIdentity(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	provider := &fakeRegistrationSMSProvider{checkPass: true}

	body := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"303246","machine_name":"desktop","platform":"windows","client_id":"cid-phone"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if provider.checkReq.PhoneNumber != "19900001111" || provider.checkReq.VerifyCode != "303246" {
		t.Fatalf("unexpected check request: %+v", provider.checkReq)
	}
	var got struct {
		Status         string `json:"status"`
		Email          string `json:"email"`
		PhoneNumber    string `json:"phone_number"`
		CreditsAccount string `json:"credits_account"`
		MachineID      string `json:"machine_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != "approved" || got.Email != "phone:19900001111" || got.PhoneNumber != "19900001111" || got.CreditsAccount != "phone:19900001111" || got.MachineID == "" {
		t.Fatalf("unexpected response: %+v body=%s", got, rr.Body.String())
	}
	user, err := st.Users.GetByTenantEmail(context.Background(), store.DefaultTenantID, "phone:19900001111")
	if err != nil {
		t.Fatalf("load phone user: %v", err)
	}
	if user == nil {
		t.Fatal("expected phone identity user to be created")
	}
}

func TestRegistrationSMSVerifyAndStartRejectsFailedCode(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	saveRegistrationAuthTestConfig(t, st.System, store.DefaultTenantID)
	provider := &fakeRegistrationSMSProvider{checkPass: false}

	body := bytes.NewBufferString(`{"phone_number":"19900001111","verify_code":"303246"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/sms/verify-and-start", body)
	rr := httptest.NewRecorder()
	RegistrationSMSVerifyAndStartHandler(identity, st.System, func(cfg RegistrationAuthConfig) registrationSMSProvider {
		return provider
	}).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "INVALID_SMS_VERIFY_CODE") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func saveRegistrationAuthTestConfig(t *testing.T, settings store.SystemSettingsRepository, tenantID string) {
	t.Helper()
	saveRegistrationAuthTestConfigWithLimit(t, settings, tenantID, registrationAuthDefaultDailyLimit)
}

func saveRegistrationAuthTestConfigWithLimit(t *testing.T, settings store.SystemSettingsRepository, tenantID string, dailyLimit int) {
	t.Helper()
	cfg := normalizeRegistrationAuthConfig(RegistrationAuthConfig{
		Method:                registrationAuthMethodPhone,
		AliyunAccessKeyID:     "ak",
		AliyunAccessKeySecret: "secret",
		AliyunSignName:        registrationAuthDefaultSignName,
		AliyunTemplateCode:    registrationAuthDefaultTemplate,
		CodeTTLMinutes:        5,
		CodeLength:            6,
		DailySMSLimit:         dailyLimit,
	})
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := ScopedSystemSettingsForTenant(tenantID, settings).Set(context.Background(), registrationAuthConfigKey, string(data)); err != nil {
		t.Fatalf("save config: %v", err)
	}
}
