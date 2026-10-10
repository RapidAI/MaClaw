package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
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

// botAuditMem records what an admin action was audited as.
type botAuditMem struct {
	logs []*store.AdminAuditLog
}

func (m *botAuditMem) Create(_ context.Context, entry *store.AdminAuditLog) error {
	m.logs = append(m.logs, entry)
	return nil
}

func (m *botAuditMem) List(context.Context, store.AdminAuditLogFilter) ([]*store.AdminAuditLog, error) {
	return m.logs, nil
}

func TestBotSettingsSaveIsAuditedWithoutSecrets(t *testing.T) {
	svc := botmgmt.NewService(&botSettingsMem{})
	audit := &botAuditMem{}
	body := `{"base_url":"https://maclawsrv.example","access_token":"secret-token","admin_secret":"root-admin-secret"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/bots/settings", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "admin-1", Scope: "tenant", TenantID: "tenant-a"}))
	rec := httptest.NewRecorder()
	PutBotSettingsAdminHandler(svc, audit).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(audit.logs) != 1 {
		t.Fatalf("audit records = %d want 1", len(audit.logs))
	}
	entry := audit.logs[0]
	if entry.Action != "bot.settings.update" || entry.AdminUserID != "admin-1" {
		t.Fatalf("audit entry = %+v", entry)
	}
	if strings.Contains(entry.PayloadJSON, "secret-token") || strings.Contains(entry.PayloadJSON, "root-admin-secret") {
		t.Fatalf("audit payload leaked a secret: %s", entry.PayloadJSON)
	}
	if !strings.Contains(entry.PayloadJSON, `"token_set":true`) || !strings.Contains(entry.PayloadJSON, `"admin_secret_set":true`) {
		t.Fatalf("audit payload = %s", entry.PayloadJSON)
	}
}

func TestBotLLMSettingsSaveIsAuditedWithoutTheKey(t *testing.T) {
	svc := botmgmt.NewService(&botSettingsMem{})
	audit := &botAuditMem{}
	body := `{"current":"lp_abcdef0123456789","providers":[{"id":"lp_abcdef0123456789","name":"OpenAI","protocol":"openai","url":"https://api.openai.com/v1","key":"sk-live-secret","model":"gpt-4o"}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/bots/llm", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "admin-1", Scope: "tenant", TenantID: "tenant-a"}))
	rec := httptest.NewRecorder()
	PutBotLLMSettingsAdminHandler(svc, audit).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-live-secret") || !strings.Contains(rec.Body.String(), `"key_set":true`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if len(audit.logs) != 1 || audit.logs[0].Action != "bot.llm.update" || audit.logs[0].AdminUserID != "admin-1" {
		t.Fatalf("audit = %+v", audit.logs)
	}
	if strings.Contains(audit.logs[0].PayloadJSON, "sk-live-secret") || !strings.Contains(audit.logs[0].PayloadJSON, `"protocol":"openai"`) {
		t.Fatalf("audit payload = %s", audit.logs[0].PayloadJSON)
	}
}

func TestBotGrantChangesAreAudited(t *testing.T) {
	svc := botmgmt.NewService(&botSettingsMem{})
	audit := &botAuditMem{}
	admin := func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), adminUserContextKey, &store.AdminUser{ID: "admin-1", Scope: "tenant", TenantID: "tenant-a"}))
	}
	create := admin(httptest.NewRequest(http.MethodPost, "/api/admin/bots/grants", strings.NewReader(`{"scope":"user","target_id":"u_alice"}`)))
	createRec := httptest.NewRecorder()
	PostBotGrantAdminHandler(svc, audit).ServeHTTP(createRec, create)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created botmgmt.Grant
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode created grant: %v body=%s", err, createRec.Body.String())
	}
	del := admin(httptest.NewRequest(http.MethodDelete, "/api/admin/bots/grants/"+created.ID, nil))
	// A bare httptest request has no mux pattern, so the wildcard the handler
	// reads has to be set here.
	del.SetPathValue("id", created.ID)
	delRec := httptest.NewRecorder()
	DeleteBotGrantAdminHandler(svc, audit).ServeHTTP(delRec, del)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}
	if len(audit.logs) != 2 || audit.logs[0].Action != "bot.grant.create" || audit.logs[1].Action != "bot.grant.delete" {
		t.Fatalf("audit actions = %+v", audit.logs)
	}
	if !strings.Contains(audit.logs[0].PayloadJSON, "u_alice") {
		t.Fatalf("grant audit payload = %s", audit.logs[0].PayloadJSON)
	}
}

func TestAdminBotErrorShowsUpstreamStatusButUserErrorDoesNot(t *testing.T) {
	err := &upstream.StatusError{Base: botmgmt.ErrSrv, Status: http.StatusUnauthorized, Detail: "unauthorized"}

	admin := httptest.NewRecorder()
	writeBotError(admin, err)
	if admin.Code != http.StatusBadGateway || !strings.Contains(admin.Body.String(), "HTTP 401") {
		t.Fatalf("admin status=%d body=%s", admin.Code, admin.Body.String())
	}

	user := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bots/bot_x/messages", nil)
	writeBotUserError(user, req, &auth.MachinePrincipal{TenantID: "t_demo", UserID: "u_demo"}, err)
	if user.Code != http.StatusBadGateway ||
		strings.Contains(user.Body.String(), "401") ||
		strings.Contains(user.Body.String(), "unauthorized") ||
		!strings.Contains(user.Body.String(), "MACLAWSRV_REQUEST_FAILED") {
		t.Fatalf("user status=%d body=%s", user.Code, user.Body.String())
	}
}
