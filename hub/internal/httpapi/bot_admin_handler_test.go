package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestBotSettingsHandlerHidesToken(t *testing.T) {
	svc := botmgmt.NewService(&botSettingsMem{})
	if _, err := svc.SaveConnection(context.Background(), "tenant-a", "https://maclawsrv.example", "secret-token", true); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/bots/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: "tenant-a"}))
	rec := httptest.NewRecorder()
	GetBotSettingsAdminHandler(svc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token") || !strings.Contains(rec.Body.String(), `"token_set":true`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"grants":[]`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestBotAccessIsClosedByDefault(t *testing.T) {
	svc := botmgmt.NewService(&botSettingsMem{})
	authn := fakeVEMachineAuth{token: "machine-token", principals: map[string]*auth.MachinePrincipal{
		"machine-1": {TenantID: "tenant-a", UserID: "alice", MachineID: "machine-1"},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/bots/access", nil)
	req.Header.Set("Authorization", "Bearer machine-token")
	req.Header.Set("X-Machine-ID", "machine-1")
	rec := httptest.NewRecorder()
	GetBotAccessHandler(svc, authn).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) || !strings.Contains(rec.Body.String(), "服务器没有开通bot功能") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type botSettingsMem struct {
	value string
}

func (m *botSettingsMem) Get(context.Context, string) (string, error) {
	if m.value == "" {
		return "", context.Canceled
	}
	return m.value, nil
}

func (m *botSettingsMem) Set(_ context.Context, _, valueJSON string) error {
	m.value = valueJSON
	return nil
}
