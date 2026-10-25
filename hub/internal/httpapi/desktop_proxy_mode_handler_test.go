package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

func TestDesktopProxyModeServiceReadIsTokenGatedAndReadOnly(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "svc-token")
	pool := desktoppool.New(newDesktopSettingsMem(), nil)
	if _, err := pool.SetTenantProxyModePolicy(context.Background(), "tenant-a", desktoppool.ProxyModePolicy{Default: "direct", Locked: true}); err != nil {
		t.Fatal(err)
	}
	h := PostDesktopProxyModeHandler(pool)
	call := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/proxy-mode", strings.NewReader(`{"tenant_id":"tenant-a","user_id":"alice","mode":"proxy"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	if rec := call("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d", rec.Code)
	}
	rec := call("svc-token")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mode":"direct"`) || !strings.Contains(rec.Body.String(), `"locked":true`) {
		t.Fatalf("read = %d %s", rec.Code, rec.Body.String())
	}
	// A "mode" in the body is ignored: this endpoint never writes.
	if v, _ := pool.ProxyMode(context.Background(), "tenant-a", "alice"); v.UserMode != "" {
		t.Fatalf("service read wrote a mode: %+v", v)
	}
}

func TestWriteProxyModeErrorLocked(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProxyModeError(rec, desktoppool.ErrProxyModeLocked)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "DESKTOP_PROXY_MODE_LOCKED") {
		t.Fatalf("locked = %d %s", rec.Code, rec.Body.String())
	}
}
