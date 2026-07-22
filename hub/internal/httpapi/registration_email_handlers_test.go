package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
