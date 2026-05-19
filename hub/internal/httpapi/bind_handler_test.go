package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func newBindHandlerStore(t *testing.T) (*store.Store, func()) {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "bind-handler.db")})
	if err != nil {
		t.Fatalf("new sqlite provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	return st, func() { _ = provider.Close() }
}

func newBindHandlerIdentity(t *testing.T) (*auth.IdentityService, func()) {
	t.Helper()
	st, cleanup := newBindHandlerStore(t)
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	return identity, cleanup
}

func seedBindUser(t *testing.T, identity *auth.IdentityService, tenantID, email string) {
	t.Helper()
	now := time.Now().UTC()
	if err := identity.UsersRepo().Create(context.Background(), &store.User{
		ID:               "user_" + strings.ReplaceAll(tenantID, "-", "_") + "_" + strings.ReplaceAll(email, "@", "_"),
		TenantID:         tenantID,
		Email:            email,
		SN:               "SN-" + tenantID,
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func postBindJSON(t *testing.T, handler http.HandlerFunc, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestBindQueryAutoResolvesTenantAndReturnsTenantID(t *testing.T) {
	identity, cleanup := newBindHandlerIdentity(t)
	defer cleanup()
	seedBindUser(t, identity, "tenant_a", "user@example.com")

	rec := postBindJSON(t, BindQueryHandler(identity), "/api/bind/query", map[string]string{"email": "user@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("query status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Bound    bool   `json:"bound"`
		TenantID string `json:"tenant_id"`
		SN       string `json:"sn"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Bound || payload.TenantID != "tenant_a" || payload.SN == "" {
		t.Fatalf("unexpected query response: %+v body=%s", payload, rec.Body.String())
	}
}

func TestBindQueryRejectsAmbiguousEmailWithoutTenantHint(t *testing.T) {
	identity, cleanup := newBindHandlerIdentity(t)
	defer cleanup()
	seedBindUser(t, identity, "tenant_a", "shared@example.com")
	seedBindUser(t, identity, "tenant_b", "shared@example.com")

	rec := postBindJSON(t, BindQueryHandler(identity), "/api/bind/query", map[string]string{"email": "shared@example.com"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "TENANT_AMBIGUOUS") {
		t.Fatalf("expected tenant ambiguity, status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = postBindJSON(t, BindQueryHandler(identity), "/api/bind/query?tenant_id=tenant_b", map[string]string{"email": "shared@example.com"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tenant_id":"tenant_b"`) {
		t.Fatalf("expected hinted tenant response, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBindSendCodeAndUnbindUseResolvedTenant(t *testing.T) {
	st, cleanup := newBindHandlerStore(t)
	defer cleanup()
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	seedBindUser(t, identity, "tenant_a", "unbind@example.com")

	resetVerifyCodesForTest()
	defer resetVerifyCodesForTest()
	if !storeVerifyCode("tenant_a", "unbind@example.com", "123456") {
		t.Fatal("store verify code")
	}

	rec := postBindJSON(t, BindUnbindHandler(identity, device.NewService(st.Machines, device.NewRuntime()), nil, nil, nil), "/api/bind/unbind", map[string]string{"email": "unbind@example.com", "code": "123456"})
	if rec.Code != http.StatusOK {
		t.Fatalf("unbind status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tenant_id":"tenant_a"`) {
		t.Fatalf("expected tenant_id in unbind response: %s", rec.Body.String())
	}
	user, err := st.Users.GetByTenantEmail(context.Background(), "tenant_a", "unbind@example.com")
	if err != nil {
		t.Fatalf("lookup after unbind: %v", err)
	}
	if user != nil {
		t.Fatalf("tenant user should be deleted after unbind: %#v", user)
	}
}

func resetVerifyCodesForTest() {
	verifyMu.Lock()
	defer verifyMu.Unlock()
	verifyCodes = map[string]*verifyEntry{}
	verifyLastClean = time.Now()
}
