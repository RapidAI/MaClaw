package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestCenterUserExistsHandlerSupportsPhoneIdentity(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	const (
		secret   = "secret_phone_route"
		email    = "znsoft@163.com"
		phone    = "17090134628"
		tenantID = store.DefaultTenantID
		userID   = "usr_center_phone_route"
	)
	if err := services.store.System.Set(ctx, "center_registration", `{"registered":true,"hub_id":"hub_phone_route","hub_secret":"`+secret+`"}`); err != nil {
		t.Fatalf("set center registration: %v", err)
	}
	now := time.Now().UTC()
	if err := services.store.Users.Create(ctx, &store.User{
		ID:               userID,
		TenantID:         tenantID,
		Email:            email,
		Status:           "active",
		EnrollmentStatus: "approved",
		EmailVerified:    true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := services.store.Users.UpsertIdentity(ctx, &store.UserIdentity{
		ID:        "id_center_phone_route",
		TenantID:  tenantID,
		UserID:    userID,
		Type:      "phone",
		Value:     phone,
		Verified:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert phone identity: %v", err)
	}

	for _, account := range []string{"phone:" + phone, phone, "+1 (709) 013-4628"} {
		t.Run(account, func(t *testing.T) {
			rec := doCenterUserExistsRequest(t, services.handler, secret, map[string]string{
				"email":     account,
				"tenant_id": tenantID,
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var body struct {
				Exists bool `json:"exists"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !body.Exists {
				t.Fatalf("exists = false, want true")
			}
		})
	}
}

func doCenterUserExistsRequest(t *testing.T, handler http.Handler, secret string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/center/user-exists", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-HubCenter-Verify", sha256HexForCenterUserExistsTest(secret))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func sha256HexForCenterUserExistsTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum)
}
