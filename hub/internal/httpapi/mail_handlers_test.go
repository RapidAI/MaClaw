package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type testMailer struct {
	called  bool
	to      []string
	subject string
	body    string
}

type testSystemSettingsRepo struct {
	values map[string]string
}

func (r *testSystemSettingsRepo) Set(ctx context.Context, key, valueJSON string) error {
	_ = ctx
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = valueJSON
	return nil
}

func (r *testSystemSettingsRepo) Get(ctx context.Context, key string) (string, error) {
	_ = ctx
	if r.values == nil {
		return "", nil
	}
	return r.values[key], nil
}

func (m *testMailer) Send(ctx context.Context, to []string, subject string, body string) error {
	_ = ctx
	m.called = true
	m.to = append([]string(nil), to...)
	m.subject = subject
	m.body = body
	return nil
}

func (m *testMailer) SendLoginConfirmation(ctx context.Context, to string, confirmURL string) error {
	return m.Send(ctx, []string{to}, "login", confirmURL)
}

func (m *testMailer) SendRegistrationVerification(ctx context.Context, to string, confirmURL string, code string, lang ...string) error {
	return m.Send(ctx, []string{to}, "registration-verification", confirmURL+"\n"+code)
}

func TestAdminSendTestMailHandlerRequiresMailer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/mail/test", bytes.NewBufferString(`{"email":"admin@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AdminSendTestMailHandler(nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminSendTestMailHandlerSendsMail(t *testing.T) {
	mailer := &testMailer{}
	payload, _ := json.Marshal(map[string]string{"email": "admin@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/mail/test", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	AdminSendTestMailHandler(mailer).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !mailer.called {
		t.Fatal("expected mailer to be called")
	}
	if len(mailer.to) != 1 || mailer.to[0] != "admin@example.com" {
		t.Fatalf("unexpected recipients: %#v", mailer.to)
	}
}

func TestMailConfigHandlersSaveAndLoad(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	service := mail.New(config.Config{}, settings)

	savePayload, _ := json.Marshal(map[string]any{
		"enabled":         true,
		"provider":        "gmail",
		"smtp_host":       "smtp.gmail.com",
		"smtp_port":       587,
		"smtp_encryption": "starttls",
		"smtp_username":   "admin@gmail.com",
		"smtp_password":   "app-password",
		"from_name":       "MaClaw Hub",
		"from_email":      "admin@gmail.com",
	})

	saveReq := httptest.NewRequest(http.MethodPost, "/api/admin/mail/config", bytes.NewReader(savePayload))
	saveReq.Header.Set("Content-Type", "application/json")
	saveRR := httptest.NewRecorder()

	UpdateMailConfigHandler(service).ServeHTTP(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("expected save 200, got %d body=%s", saveRR.Code, saveRR.Body.String())
	}

	var saved mail.ConfigState
	if err := json.Unmarshal(saveRR.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if saved.Provider != "gmail" || saved.SMTPHost != "smtp.gmail.com" || saved.SMTPPort != 587 {
		t.Fatalf("unexpected saved config: %#v", saved)
	}

	loadReq := httptest.NewRequest(http.MethodGet, "/api/admin/mail/config", nil)
	loadRR := httptest.NewRecorder()

	GetMailConfigHandler(service).ServeHTTP(loadRR, loadReq)
	if loadRR.Code != http.StatusOK {
		t.Fatalf("expected load 200, got %d body=%s", loadRR.Code, loadRR.Body.String())
	}

	var loaded mail.ConfigState
	if err := json.Unmarshal(loadRR.Body.Bytes(), &loaded); err != nil {
		t.Fatalf("decode loaded config: %v", err)
	}
	if loaded.Provider != "gmail" || loaded.Username != "admin@gmail.com" || loaded.FromEmail != "admin@gmail.com" {
		t.Fatalf("unexpected loaded config: %#v", loaded)
	}
}

func TestTenantMailSenderNameHandlersAreScoped(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	payload, _ := json.Marshal(map[string]string{"from_name": "  Acme Mail  "})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/mail/sender-name", bytes.NewReader(payload))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: "tenant_acme"}))
	rr := httptest.NewRecorder()

	UpdateTenantMailSenderNameHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected save 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := settings.values["tenant:tenant_acme:"+mail.TenantSenderNameSettingKey]; got != `{"from_name":"Acme Mail"}` {
		t.Fatalf("stored tenant sender name = %s", got)
	}

	loadReq := httptest.NewRequest(http.MethodGet, "/api/admin/mail/sender-name", nil)
	loadReq = loadReq.WithContext(context.WithValue(loadReq.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: "tenant_acme"}))
	loadRR := httptest.NewRecorder()
	GetTenantMailSenderNameHandler(settings).ServeHTTP(loadRR, loadReq)
	if loadRR.Code != http.StatusOK || !bytes.Contains(loadRR.Body.Bytes(), []byte(`"from_name":"Acme Mail"`)) {
		t.Fatalf("expected scoped sender name, got %d body=%s", loadRR.Code, loadRR.Body.String())
	}
}

func TestTenantMailSenderNameHandlerRejectsTooLongName(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	payload, _ := json.Marshal(map[string]string{"from_name": strings.Repeat("x", 81)})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/mail/sender-name", bytes.NewReader(payload))
	rr := httptest.NewRecorder()

	UpdateTenantMailSenderNameHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestTenantMailSenderNameHandlerIgnoresInvalidStoredJSON(t *testing.T) {
	settings := &testSystemSettingsRepo{values: map[string]string{mail.TenantSenderNameSettingKey: `{bad-json`}}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/mail/sender-name", nil)
	rr := httptest.NewRecorder()

	GetTenantMailSenderNameHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"from_name":""`)) {
		t.Fatalf("expected empty sender name fallback, body=%s", rr.Body.String())
	}
}

func TestTenantMailSenderNameHandlerTruncatesLegacyLongStoredName(t *testing.T) {
	longName := strings.Repeat("界", mail.TenantSenderNameMaxRunes+1)
	settings := &testSystemSettingsRepo{values: map[string]string{mail.TenantSenderNameSettingKey: `{"from_name":"` + longName + `"}`}}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/mail/sender-name", nil)
	rr := httptest.NewRecorder()

	GetTenantMailSenderNameHandler(settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var state TenantMailSenderNameState
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode sender name state: %v", err)
	}
	if len([]rune(state.FromName)) != mail.TenantSenderNameMaxRunes {
		t.Fatalf("sender name length = %d, want %d", len([]rune(state.FromName)), mail.TenantSenderNameMaxRunes)
	}
}
