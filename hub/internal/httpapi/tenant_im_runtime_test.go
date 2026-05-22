package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type testTenantIMRuntimeReloader struct {
	tenantID string
	platform string
	err      error
}

func (r *testTenantIMRuntimeReloader) ReloadTenantIM(_ context.Context, tenantID, platform string) error {
	r.tenantID = tenantID
	r.platform = platform
	return r.err
}

func tenantAdminRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: "tenant_a"}))
	return req
}

func TestTenantIMConfigSaveReturnsRuntimeReloadWarning(t *testing.T) {
	system := &stubSystemSettings{data: map[string]string{}}
	reloader := &testTenantIMRuntimeReloader{err: errors.New("start failed")}
	rec := httptest.NewRecorder()
	UpdateQQBotConfigHandler(system, nil, reloader)(rec, tenantAdminRequest(http.MethodPost, "/api/admin/settings/qqbot", `{"enabled":true,"app_id":"app","app_secret":"secret"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		RuntimeReloadOK    bool   `json:"runtime_reload_ok"`
		RuntimeReloadError string `json:"runtime_reload_error"`
		AppSecret          string `json:"app_secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.RuntimeReloadOK || body.RuntimeReloadError != "start failed" {
		t.Fatalf("runtime reload response = ok %v err %q", body.RuntimeReloadOK, body.RuntimeReloadError)
	}
	if body.AppSecret != maskSecret("secret") {
		t.Fatalf("masked config secret = %q", body.AppSecret)
	}
}

func TestTenantIMConfigSaveReloadsTenantRuntime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		handler  func(store.SystemSettingsRepository, *testTenantIMRuntimeReloader) http.HandlerFunc
		body     string
	}{
		{name: "qqbot", platform: "qqbot", handler: func(s store.SystemSettingsRepository, r *testTenantIMRuntimeReloader) http.HandlerFunc {
			return UpdateQQBotConfigHandler(s, nil, r)
		}, body: `{"enabled":true,"app_id":"app","app_secret":"secret"}`},
		{name: "wecom", platform: "wecom", handler: func(s store.SystemSettingsRepository, r *testTenantIMRuntimeReloader) http.HandlerFunc {
			return UpdateWeComConfigHandler(s, nil, r)
		}, body: `{"enabled":true,"bot_id":"bot","secret":"secret"}`},
		{name: "dingtalk", platform: "dingtalk", handler: func(s store.SystemSettingsRepository, r *testTenantIMRuntimeReloader) http.HandlerFunc {
			return UpdateDingTalkConfigHandler(s, nil, r)
		}, body: `{"enabled":true,"client_id":"client","client_secret":"secret"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			system := &stubSystemSettings{data: map[string]string{}}
			reloader := &testTenantIMRuntimeReloader{}
			rec := httptest.NewRecorder()
			tc.handler(system, reloader)(rec, tenantAdminRequest(http.MethodPost, "/api/admin/settings/"+tc.name, tc.body))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if reloader.tenantID != "tenant_a" || reloader.platform != tc.platform {
				t.Fatalf("runtime reload = tenant %q platform %q", reloader.tenantID, reloader.platform)
			}
		})
	}
}
