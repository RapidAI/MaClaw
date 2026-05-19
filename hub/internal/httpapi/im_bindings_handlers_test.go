package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/dingtalk"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/wecom"
)

type imBindingTestUsers struct{}

func (imBindingTestUsers) Create(context.Context, *store.User) error               { return nil }
func (imBindingTestUsers) GetByID(context.Context, string) (*store.User, error)    { return nil, nil }
func (imBindingTestUsers) GetByEmail(context.Context, string) (*store.User, error) { return nil, nil }
func (imBindingTestUsers) GetByTenantEmail(context.Context, string, string) (*store.User, error) {
	return nil, nil
}

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
func (imBindingTestUsers) List(context.Context) ([]*store.User, error) { return nil, nil }
func (imBindingTestUsers) ListByTenant(context.Context, string) ([]*store.User, error) {
	return nil, nil
}
func (imBindingTestUsers) DeleteByEmail(context.Context, string) error               { return nil }
func (imBindingTestUsers) DeleteByTenantEmail(context.Context, string, string) error { return nil }
func (imBindingTestUsers) UpdateSmartRoute(context.Context, string, bool) error      { return nil }

func requestWithTenantAdmin(method, target, tenantID string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	admin := &store.AdminUser{ID: "admin-" + tenantID, Scope: "tenant", TenantID: tenantID}
	return req.WithContext(context.WithValue(req.Context(), adminUserContextKey, admin))
}

func requestWithGlobalTenant(method, target, tenantID string) *http.Request {
	req := httptest.NewRequest(method, target+"?tenant_id="+tenantID, nil)
	admin := &store.AdminUser{ID: "global-admin", Scope: "global"}
	return req.WithContext(context.WithValue(req.Context(), adminUserContextKey, admin))
}

func decodeBindingsCount(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var payload struct {
		Bindings []map[string]any `json:"bindings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode bindings response: %v body=%s", err, rec.Body.String())
	}
	return len(payload.Bindings)
}

func TestFeishuBindingsHandlerFiltersByTenant(t *testing.T) {
	notifier := feishu.New("", "", imBindingTestUsers{}, &stubSystemSettings{data: map[string]string{}}, nil)
	notifier.BindOpenIDForTenant("tenant_a", "same@example.com", "open-a", "")
	notifier.BindOpenIDForTenant("tenant_b", "same@example.com", "open-b", "")

	rec := httptest.NewRecorder()
	GetFeishuBindingsHandler(notifier)(rec, requestWithTenantAdmin(http.MethodGet, "/api/admin/feishu/bindings", "tenant_a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant A list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeBindingsCount(t, rec); got != 1 {
		t.Fatalf("tenant A bindings count=%d", got)
	}
	if !json.Valid(rec.Body.Bytes()) || !containsStr(rec.Body.String(), "open-a") || containsStr(rec.Body.String(), "open-b") {
		t.Fatalf("unexpected tenant A response: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	DeleteFeishuBindingHandler(notifier)(rec, requestWithTenantAdmin(http.MethodDelete, "/api/admin/feishu/bindings?email=same@example.com", "tenant_a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant A delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := notifier.ResolveOpenIDByTenantEmail("tenant_a", "same@example.com"); got != "" {
		t.Fatalf("tenant A binding still exists: %q", got)
	}
	if got := notifier.ResolveOpenIDByTenantEmail("tenant_b", "same@example.com"); got != "open-b" {
		t.Fatalf("tenant B binding was affected: %q", got)
	}
}

func TestRemoteIMBindingsHandlersFilterByTenant(t *testing.T) {
	system := &stubSystemSettings{data: map[string]string{}}
	users := imBindingTestUsers{}

	dt := dingtalk.New(func() dingtalk.Config { return dingtalk.Config{} }, users, system, nil)
	dt.BindTenantEmail("staff-a", "tenant_a", "same@example.com")
	dt.BindTenantEmail("staff-b", "tenant_b", "same@example.com")
	assertTenantBindingList(t, GetDingTalkBindingsHandler(dt), "/api/admin/dingtalk/bindings", "staff-a", "staff-b")
	DeleteDingTalkBindingHandler(dt)(httptest.NewRecorder(), requestWithTenantAdmin(http.MethodDelete, "/api/admin/dingtalk/bindings?staff_id=staff-a", "tenant_a"))
	if got := dt.LookupByTenantEmail("tenant_b", "same@example.com"); got != "staff-b" {
		t.Fatalf("dingtalk tenant B binding affected: %q", got)
	}

	wc := wecom.New(func() wecom.Config { return wecom.Config{} }, users, system, nil)
	wc.BindTenantEmail("user-a", "tenant_a", "same@example.com")
	wc.BindTenantEmail("user-b", "tenant_b", "same@example.com")
	assertTenantBindingList(t, GetWeComBindingsHandler(wc), "/api/admin/wecom/bindings", "user-a", "user-b")
	DeleteWeComBindingHandler(wc)(httptest.NewRecorder(), requestWithTenantAdmin(http.MethodDelete, "/api/admin/wecom/bindings?userid=user-a", "tenant_a"))
	if got := wc.LookupByTenantEmail("tenant_b", "same@example.com"); got != "user-b" {
		t.Fatalf("wecom tenant B binding affected: %q", got)
	}

	qq := qqbot.New(func() qqbot.Config { return qqbot.Config{} }, users, system, nil)
	qq.BindTenantEmail("open-a", "tenant_a", "same@example.com")
	qq.BindTenantEmail("open-b", "tenant_b", "same@example.com")
	assertTenantBindingList(t, GetQQBotBindingsHandler(qq), "/api/admin/qqbot/bindings", "open-a", "open-b")
	DeleteQQBotBindingHandler(qq)(httptest.NewRecorder(), requestWithTenantAdmin(http.MethodDelete, "/api/admin/qqbot/bindings?open_id=open-a", "tenant_a"))
	if got := qq.LookupByTenantEmail("tenant_b", "same@example.com"); got != "open-b" {
		t.Fatalf("qqbot tenant B binding affected: %q", got)
	}
}

func assertTenantBindingList(t *testing.T, handler http.HandlerFunc, target, want, forbidden string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(rec, requestWithGlobalTenant(http.MethodGet, target, "tenant_a"))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := decodeBindingsCount(t, rec); got != 1 {
		t.Fatalf("bindings count=%d body=%s", got, rec.Body.String())
	}
	if !containsStr(rec.Body.String(), want) || containsStr(rec.Body.String(), forbidden) {
		t.Fatalf("unexpected tenant binding response: %s", rec.Body.String())
	}
}
