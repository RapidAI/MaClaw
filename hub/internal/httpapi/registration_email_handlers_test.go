package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
	if mixedSendRec.Code != http.StatusInternalServerError || !bytes.Contains(mixedSendRec.Body.Bytes(), []byte("MAIL_NOT_CONFIGURED")) {
		t.Fatalf("mixed policy should permit email send before mail delivery, status=%d body=%s", mixedSendRec.Code, mixedSendRec.Body.String())
	}

	mixedVerifyReq := httptest.NewRequest(http.MethodPost, "/api/enroll/email/verify-and-start", bytes.NewBufferString(`{"email":"`+email+`","tenant_id":"`+tenantID+`","verify_code":"654321"}`))
	mixedVerifyRec := httptest.NewRecorder()
	RegistrationEmailVerifyAndStartHandler(nil, nil, nil, settings).ServeHTTP(mixedVerifyRec, mixedVerifyReq)
	if mixedVerifyRec.Code != http.StatusBadRequest || !bytes.Contains(mixedVerifyRec.Body.Bytes(), []byte("INVALID_VERIFY_CODE")) {
		t.Fatalf("mixed policy should allow email verification, status=%d body=%s", mixedVerifyRec.Code, mixedVerifyRec.Body.String())
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
