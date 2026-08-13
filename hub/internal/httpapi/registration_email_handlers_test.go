package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type registrationEmailTestMailer struct {
	subject string
}

func (m *registrationEmailTestMailer) Send(_ context.Context, _ []string, subject, _ string) error {
	m.subject = subject
	return nil
}

func (m *registrationEmailTestMailer) SendRegistrationVerification(ctx context.Context, to, confirmURL, code string, lang ...string) error {
	return m.Send(ctx, []string{to}, "registration-verification", confirmURL+code)
}

func (m *registrationEmailTestMailer) SendLoginConfirmation(ctx context.Context, to, confirmURL string) error {
	return m.Send(ctx, []string{to}, "login", confirmURL)
}

func TestRegistrationEmailHandlersHonorPhoneAndMixedPolicies(t *testing.T) {
	const email = "mixed-policy@example.com"
	const tenantID = "tenant-email-policy"
	key := "enroll:" + email
	settings := &testSystemSettingsRepo{}

	saveRegistrationAuthTestConfig(t, settings, tenantID)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/enroll/email/send-code", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+tenantID+`"}`))
	sendRec := httptest.NewRecorder()
	identity, _, _ := newPreservationTestIdentity(t)
	RegistrationEmailSendCodeHandler(identity, nil, settings).ServeHTTP(sendRec, sendReq)
	if sendRec.Code != http.StatusBadRequest || !bytes.Contains(sendRec.Body.Bytes(), []byte("EMAIL_REGISTRATION_DISABLED")) {
		t.Fatalf("phone policy send status=%d body=%s", sendRec.Code, sendRec.Body.String())
	}

	if !storeVerifyCode(tenantID, key, "123456") {
		t.Fatal("store verification code")
	}
	t.Cleanup(func() { deleteVerifyCode(tenantID, key) })
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/enroll/email/verify-and-start", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+tenantID+`","verify_code":"654321"}`))
	verifyRec := httptest.NewRecorder()
	RegistrationEmailVerifyAndStartHandler(nil, nil, nil, settings).ServeHTTP(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusBadRequest || !bytes.Contains(verifyRec.Body.Bytes(), []byte("EMAIL_REGISTRATION_DISABLED")) {
		t.Fatalf("phone policy verify status=%d body=%s", verifyRec.Code, verifyRec.Body.String())
	}

	saveRegistrationSMSCredentialsForMixedMode(t, settings, tenantID)
	mixedSendReq := httptest.NewRequest(http.MethodPost, "/api/enroll/email/send-code", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+tenantID+`"}`))
	mixedSendRec := httptest.NewRecorder()
	RegistrationEmailSendCodeHandler(identity, nil, settings).ServeHTTP(mixedSendRec, mixedSendReq)
	if mixedSendRec.Code != http.StatusServiceUnavailable || !bytes.Contains(mixedSendRec.Body.Bytes(), []byte("MAIL_NOT_CONFIGURED")) {
		t.Fatalf("mixed policy should permit email send before mail delivery, status=%d body=%s", mixedSendRec.Code, mixedSendRec.Body.String())
	}

	mixedVerifyReq := httptest.NewRequest(http.MethodPost, "/api/enroll/email/verify-and-start", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+tenantID+`","verify_code":"654321"}`))
	mixedVerifyRec := httptest.NewRecorder()
	RegistrationEmailVerifyAndStartHandler(nil, nil, nil, settings).ServeHTTP(mixedVerifyRec, mixedVerifyReq)
	if mixedVerifyRec.Code != http.StatusBadRequest || !bytes.Contains(mixedVerifyRec.Body.Bytes(), []byte("INVALID_VERIFY_CODE")) {
		t.Fatalf("mixed policy should allow email verification, status=%d body=%s", mixedVerifyRec.Code, mixedVerifyRec.Body.String())
	}
}

func TestRegistrationEmailStartWithInvitationSkipsOTPOnlyWhenEnabled(t *testing.T) {
	const tenantID = store.DefaultTenantID
	const email = "invite-no-otp@example.com"
	identity, st, _ := newPreservationTestIdentity(t)
	settings := &testSystemSettingsRepo{}
	invites := invitation.NewService(st.InvitationCodes, st.System)
	identity = auth.NewIdentityService(
		st.Users, st.Enrollments, st.EmailBlocks, st.Machines,
		st.ViewerTokens, st.LoginTokens, st.System,
		invites, "open", true, nil, "http://127.0.0.1:9399",
	)
	codes, err := invites.GenerateCodesForTenant(context.Background(), tenantID, 1, 0, false)
	if err != nil || len(codes) != 1 {
		t.Fatalf("generate invitation code: codes=%v err=%v", codes, err)
	}

	request := func(code string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/enroll/email/start-with-invitation", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+tenantID+`","invitation_code":"`+code+`","client_id":"invite-no-otp"}`))
		rec := httptest.NewRecorder()
		RegistrationEmailStartWithInvitationHandler(identity, invites, nil, settings).ServeHTTP(rec, req)
		return rec
	}

	if rec := request(codes[0].Code); rec.Code != http.StatusForbidden || !bytes.Contains(rec.Body.Bytes(), []byte("EMAIL_VERIFICATION_REQUIRED")) {
		t.Fatalf("default policy status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := settings.Set(context.Background(), registrationAuthConfigKey, `{"method":"email","email_verification_disabled":true}`); err != nil {
		t.Fatal(err)
	}
	if rec := request(codes[0].Code); rec.Code != http.StatusOK {
		t.Fatalf("disabled policy status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request("NOT-A-VALID-CODE"); rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("INVALID_INVITATION_CODE")) {
		t.Fatalf("invalid invitation status=%d body=%s", rec.Code, rec.Body.String())
	}
	user, err := identity.UsersRepo().GetByTenantEmail(context.Background(), tenantID, email)
	if err != nil || user == nil {
		t.Fatalf("expected enrollment user, user=%+v err=%v", user, err)
	}
	if user.EmailVerified {
		t.Fatalf("invitation-only no-OTP enrollment must not claim the email is verified: %+v", user)
	}
	bound, err := st.InvitationCodes.GetByTenantCode(context.Background(), tenantID, codes[0].Code)
	if err != nil || bound == nil || bound.Status != "used" || bound.UsedByEmail != email {
		t.Fatalf("invitation code was not consumed by the registration: code=%+v err=%v", bound, err)
	}
}

func TestInvitationOnlyEnrollmentLoginDoesNotRequireAnotherInvitation(t *testing.T) {
	const tenantID = store.DefaultTenantID
	const email = "invite-login@example.com"
	_, st, _ := newPreservationTestIdentity(t)
	invites := invitation.NewService(st.InvitationCodes, st.System)
	mailer := &registrationEmailTestMailer{}
	identity := auth.NewIdentityService(
		st.Users, st.Enrollments, st.EmailBlocks, st.Machines,
		st.ViewerTokens, st.LoginTokens, st.System,
		invites, "open", true, mailer, "http://127.0.0.1:9399",
	)
	settings := &testSystemSettingsRepo{}
	if err := settings.Set(context.Background(), registrationAuthConfigKey, `{"method":"email","email_verification_disabled":true}`); err != nil {
		t.Fatal(err)
	}
	codes, err := invites.GenerateCodesForTenant(context.Background(), tenantID, 1, 0, false)
	if err != nil || len(codes) != 1 {
		t.Fatalf("generate invitation code: codes=%v err=%v", codes, err)
	}

	enrollReq := httptest.NewRequest(http.MethodPost, "/api/enroll/email/start-with-invitation", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+tenantID+`","invitation_code":"`+codes[0].Code+`","client_id":"invite-login"}`))
	enrollRec := httptest.NewRecorder()
	RegistrationEmailStartWithInvitationHandler(identity, invites, nil, settings).ServeHTTP(enrollRec, enrollReq)
	if enrollRec.Code != http.StatusOK {
		t.Fatalf("enrollment status=%d body=%s", enrollRec.Code, enrollRec.Body.String())
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/email/request", bytes.NewBufferString(`{"email":"`+email+`"}`))
	loginRec := httptest.NewRecorder()
	EmailRequestLoginHandler(identity).ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	if mailer.subject != "registration-verification" {
		t.Fatalf("unverified invitation user should verify email ownership without another invitation, got %q", mailer.subject)
	}
}

func TestRegistrationEmailVerifyAndStartAllowsPublicEmailWhenTenantDoesNotRestrictDomains(t *testing.T) {
	const tenantID = store.DefaultTenantID
	const email = "znsoft@163.com"
	identity, st, _ := newPreservationTestIdentity(t)
	identity.SetTenantRepository(st.Tenants)
	tenantSettings, ok := st.Tenants.(interface {
		UpdateSettings(context.Context, string, string, string, string) error
	})
	if !ok {
		t.Fatal("tenant repository does not support settings updates")
	}
	if err := tenantSettings.UpdateSettings(context.Background(), tenantID, "Default Tenant", "qianxin.com", `{"email_domains":["qianxin.com"],"allow_user_registration":true}`); err != nil {
		t.Fatalf("update tenant settings: %v", err)
	}
	settings := &testSystemSettingsRepo{}
	saveRegistrationSMSCredentialsForMixedMode(t, settings, tenantID)
	key := "enroll:" + email
	deleteVerifyCode(tenantID, key)
	if !storeVerifyCode(tenantID, key, "123456") {
		t.Fatal("store verification code")
	}
	t.Cleanup(func() { deleteVerifyCode(tenantID, key) })

	body := bytes.NewBufferString(`{"email":"` + email + `","tenant_id":"` + tenantID + `","verify_code":"123456","machine_name":"new-device","platform":"windows","client_id":"cid-public-email"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/email/verify-and-start", body)
	rec := httptest.NewRecorder()
	RegistrationEmailVerifyAndStartHandler(identity, nil, nil, settings).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	user, err := identity.UsersRepo().GetByTenantEmail(context.Background(), tenantID, email)
	if err != nil || user == nil {
		t.Fatalf("public email user was not enrolled: user=%+v err=%v", user, err)
	}
}

func TestRegistrationEmailVerifyAndStartPreservesCodeWhenTenantPolicyRejectsEmail(t *testing.T) {
	const tenantID = store.DefaultTenantID
	const email = "znsoft@163.com"
	identity, st, _ := newPreservationTestIdentity(t)
	identity.SetTenantRepository(st.Tenants)
	tenantSettings, ok := st.Tenants.(interface {
		UpdateSettings(context.Context, string, string, string, string) error
	})
	if !ok {
		t.Fatal("tenant repository does not support settings updates")
	}
	if err := tenantSettings.UpdateSettings(context.Background(), tenantID, "Default Tenant", "qianxin.com", `{"email_domains":["qianxin.com"],"allow_user_registration":true,"restrict_email_domains":true}`); err != nil {
		t.Fatalf("update tenant settings: %v", err)
	}
	settings := &testSystemSettingsRepo{}
	saveRegistrationSMSCredentialsForMixedMode(t, settings, tenantID)
	key := "enroll:" + email
	deleteVerifyCode(tenantID, key)
	if !storeVerifyCode(tenantID, key, "123456") {
		t.Fatal("store verification code")
	}
	t.Cleanup(func() { deleteVerifyCode(tenantID, key) })

	req := httptest.NewRequest(http.MethodPost, "/api/enroll/email/verify-and-start", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+tenantID+`","verify_code":"123456"}`))
	rec := httptest.NewRecorder()
	RegistrationEmailVerifyAndStartHandler(identity, nil, nil, settings).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !bytes.Contains(rec.Body.Bytes(), []byte("EMAIL_DOMAIN_NOT_ALLOWED")) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ok, _ := consumeVerifyCode(tenantID, key, "123456"); !ok {
		t.Fatal("tenant-policy rejection must not consume the verification code")
	}
}

func TestRegistrationEmailDeliveryErrorClassification(t *testing.T) {
	status, code := registrationEmailDeliveryError(mail.ErrDeliveryNotConfigured)
	if status != http.StatusServiceUnavailable || code != "MAIL_NOT_CONFIGURED" {
		t.Fatalf("unconfigured mail mapped to status=%d code=%s", status, code)
	}
	status, code = registrationEmailDeliveryError(errors.New("smtp authentication failed"))
	if status != http.StatusBadGateway || code != "MAIL_SEND_FAILED" {
		t.Fatalf("smtp failure mapped to status=%d code=%s", status, code)
	}
}

func TestRegistrationEmailPublicEnrollmentRejectsUnknownAndInactiveTenants(t *testing.T) {
	identity, st, _ := newPreservationTestIdentity(t)
	identity.SetTenantRepository(st.Tenants)
	now := time.Now().UTC()
	for _, tenant := range []*store.Tenant{
		{ID: "tenant_inactive", Slug: "tenant-inactive", Name: "Inactive", Status: "inactive", CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant_deleted", Slug: "tenant-deleted", Name: "Deleted", Status: "deleted", CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now, DeletedAt: &now},
	} {
		if err := st.Tenants.Create(context.Background(), tenant); err != nil {
			t.Fatalf("create tenant %s: %v", tenant.ID, err)
		}
	}

	for _, item := range []struct {
		tenantID   string
		statusCode int
		code       string
	}{
		{tenantID: "tenant_missing", statusCode: http.StatusNotFound, code: "TENANT_NOT_FOUND"},
		{tenantID: "tenant_inactive", statusCode: http.StatusForbidden, code: "TENANT_INACTIVE"},
		{tenantID: "tenant_deleted", statusCode: http.StatusForbidden, code: "TENANT_INACTIVE"},
	} {
		email := "new-" + item.tenantID + "@example.com"
		sendReq := httptest.NewRequest(http.MethodPost, "/api/enroll/email/send-code", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+item.tenantID+`"}`))
		sendRec := httptest.NewRecorder()
		RegistrationEmailSendCodeHandler(identity, nil).ServeHTTP(sendRec, sendReq)
		if sendRec.Code != item.statusCode || !bytes.Contains(sendRec.Body.Bytes(), []byte(item.code)) {
			t.Errorf("send tenant=%s status=%d body=%s", item.tenantID, sendRec.Code, sendRec.Body.String())
		}

		key := "enroll:" + email
		deleteVerifyCode(item.tenantID, key)
		if !storeVerifyCode(item.tenantID, key, "123456") {
			t.Fatalf("store verification code for %s", item.tenantID)
		}
		t.Cleanup(func() { deleteVerifyCode(item.tenantID, key) })
		verifyReq := httptest.NewRequest(http.MethodPost, "/api/enroll/email/verify-and-start", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+item.tenantID+`","verify_code":"123456"}`))
		verifyRec := httptest.NewRecorder()
		RegistrationEmailVerifyAndStartHandler(identity, nil, nil).ServeHTTP(verifyRec, verifyReq)
		if verifyRec.Code != item.statusCode || !bytes.Contains(verifyRec.Body.Bytes(), []byte(item.code)) {
			t.Errorf("verify tenant=%s status=%d body=%s", item.tenantID, verifyRec.Code, verifyRec.Body.String())
		}
		if ok, _ := consumeVerifyCode(item.tenantID, key, "123456"); !ok {
			t.Errorf("tenant=%s rejection must not consume verification code", item.tenantID)
		}
	}
}

func TestRegistrationEmailVerifyAndStartRejectsInvalidCode(t *testing.T) {
	email := "otp-login@example.com"
	tenantID := DefaultTenantID
	deleteVerifyCode(tenantID, "enroll:"+email)
	if !storeVerifyCode(tenantID, "enroll:"+email, "123456") {
		t.Fatal("store verification code")
	}
	t.Cleanup(func() { deleteVerifyCode(tenantID, "enroll:"+email) })

	body, err := json.Marshal(RegistrationEmailVerifyAndStartRequest{
		EnrollStartRequest: EnrollStartRequest{Email: email, TenantID: tenantID},
		VerifyCode:         "654321",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/enroll/email/verify-and-start", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	RegistrationEmailVerifyAndStartHandler(nil, nil, nil)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("INVALID_VERIFY_CODE")) {
		t.Fatalf("expected INVALID_VERIFY_CODE, body=%s", rec.Body.String())
	}
	if ok, _ := consumeVerifyCode(tenantID, "enroll:"+email, "123456"); !ok {
		t.Fatal("wrong attempt should not consume the correct code")
	}
}

func TestRegistrationEmailCodeIsOneTime(t *testing.T) {
	email := "one-time@example.com"
	key := "enroll:" + email
	deleteVerifyCode(DefaultTenantID, key)
	if !storeVerifyCode(DefaultTenantID, key, "123456") {
		t.Fatal("store verification code")
	}
	if ok, _ := consumeVerifyCode(DefaultTenantID, key, "123456"); !ok {
		t.Fatal("first verification should succeed")
	}
	if ok, _ := consumeVerifyCode(DefaultTenantID, key, "123456"); ok {
		t.Fatal("verification code must not be reusable")
	}
}

func TestRegistrationEmailFailedResendRestoresPreviousCode(t *testing.T) {
	tenantID := DefaultTenantID
	key := "enroll:rollback@example.com"
	deleteVerifyCode(tenantID, key)
	t.Cleanup(func() { deleteVerifyCode(tenantID, key) })

	verifyMu.Lock()
	verifyCodes[verifyCodeKey(tenantID, key)] = &verifyEntry{
		Code: "123456", ExpiresAt: time.Now().Add(3 * time.Minute), SentAt: time.Now().Add(-2 * time.Minute), Attempts: 1,
	}
	verifyMu.Unlock()

	previous := snapshotVerifyCode(tenantID, key)
	if previous == nil || !storeVerifyCode(tenantID, key, "654321") {
		t.Fatal("expected replacement code to be stored")
	}
	if !rollbackVerifyCode(tenantID, key, "654321", previous) {
		t.Fatal("expected failed replacement to roll back")
	}
	if ok, _ := consumeVerifyCode(tenantID, key, "123456"); !ok {
		t.Fatal("previous delivered code should remain valid after failed resend")
	}
}

func TestRegistrationEmailRollbackDoesNotReplaceNewerCode(t *testing.T) {
	tenantID := DefaultTenantID
	key := "enroll:rollback-race@example.com"
	deleteVerifyCode(tenantID, key)
	t.Cleanup(func() { deleteVerifyCode(tenantID, key) })

	verifyMu.Lock()
	verifyCodes[verifyCodeKey(tenantID, key)] = &verifyEntry{
		Code: "777777", ExpiresAt: time.Now().Add(5 * time.Minute), SentAt: time.Now(),
	}
	verifyMu.Unlock()
	if rollbackVerifyCode(tenantID, key, "654321", &verifyEntry{Code: "123456", ExpiresAt: time.Now().Add(time.Minute)}) {
		t.Fatal("rollback must not overwrite a newer code")
	}
	if ok, _ := consumeVerifyCode(tenantID, key, "777777"); !ok {
		t.Fatal("newer code should remain valid")
	}
}

func TestRegistrationEmailRejectsDisplayNamesAndHeaderInjection(t *testing.T) {
	invalid := []string{
		"User <user@example.com>",
		"user@example.com\r\nBcc: victim@example.com",
		"missing-domain@example",
	}
	for _, email := range invalid {
		if looksLikeRegistrationContactEmail(email) {
			t.Fatalf("accepted invalid email %q", email)
		}
	}
	if !looksLikeRegistrationContactEmail("user+tag@example.com") {
		t.Fatal("rejected valid tagged email")
	}
}
