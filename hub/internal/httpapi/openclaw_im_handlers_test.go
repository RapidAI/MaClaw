package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// stubSystemSettings is a minimal in-memory SystemSettingsRepository for tests.
type stubSystemSettings struct {
	data map[string]string
}

func (s *stubSystemSettings) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}

func (s *stubSystemSettings) Set(_ context.Context, key, value string) error {
	s.data[key] = value
	return nil
}

func makeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestOpenclawIMWebhookTestIncludesTenantHint(t *testing.T) {
	var gotTenantHeader string
	var gotPayload struct {
		TenantID string `json:"tenant_id"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenantHeader = r.Header.Get("X-Tenant-ID")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := OpenclawIMConfigState{Enabled: true, WebhookURL: server.URL, Secret: "tenant-secret"}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{"tenant:tenant_a:" + openclawIMConfigKey: string(cfgJSON)}}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/settings/openclaw_im/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "tenant-admin", Scope: "tenant", TenantID: "tenant_a"}))
	rec := httptest.NewRecorder()

	TestOpenclawIMWebhookHandler(sys)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("test webhook status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotTenantHeader != "tenant_a" || gotPayload.TenantID != "tenant_a" {
		t.Fatalf("tenant hint header/body = %q/%q", gotTenantHeader, gotPayload.TenantID)
	}
}

func TestOpenclawIMWebhookHandler_Success(t *testing.T) {
	secret := "test-secret-123"
	cfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/hook", Secret: secret}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{openclawIMConfigKey: string(cfgJSON)}}

	plugin := im.NewWebhookIMPlugin("openclaw", func(context.Context) im.WebhookConfig {
		return im.WebhookConfig{WebhookURL: cfg.WebhookURL, Secret: cfg.Secret}
	})

	// Register a message handler to capture injected messages.
	var received *im.IncomingMessage
	plugin.ReceiveMessage(func(msg im.IncomingMessage) {
		received = &msg
	})

	handler := OpenclawIMWebhookHandler(sys, plugin)

	msg := im.IncomingMessage{PlatformUID: "user-abc", Text: "查看设备", MessageType: "text"}
	body, _ := json.Marshal(msg)

	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenClaw-Signature", makeSignature(body, secret))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if received == nil {
		t.Fatal("message was not injected into plugin")
	}
	if received.PlatformUID != "user-abc" {
		t.Errorf("expected platform_uid=user-abc, got %s", received.PlatformUID)
	}
	if received.Text != "查看设备" {
		t.Errorf("expected text=查看设备, got %s", received.Text)
	}
	if received.PlatformName != "openclaw" {
		t.Errorf("expected platform_name=openclaw, got %s", received.PlatformName)
	}
}

func TestOpenclawIMWebhookHandler_BadSignature(t *testing.T) {
	secret := "real-secret"
	cfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/hook", Secret: secret}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{openclawIMConfigKey: string(cfgJSON)}}

	plugin := im.NewWebhookIMPlugin("openclaw", func(context.Context) im.WebhookConfig {
		return im.WebhookConfig{}
	})

	handler := OpenclawIMWebhookHandler(sys, plugin)

	body := []byte(`{"platform_uid":"u1","text":"hello","message_type":"text"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenClaw-Signature", makeSignature(body, "wrong-secret"))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenclawIMWebhookHandler_Disabled(t *testing.T) {
	cfg := OpenclawIMConfigState{Enabled: false}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{openclawIMConfigKey: string(cfgJSON)}}

	plugin := im.NewWebhookIMPlugin("openclaw", func(context.Context) im.WebhookConfig {
		return im.WebhookConfig{}
	})

	handler := OpenclawIMWebhookHandler(sys, plugin)

	body := []byte(`{"platform_uid":"u1","text":"hello","message_type":"text"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenclawIMWebhookHandler_MissingPlatformUID(t *testing.T) {
	cfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/hook"}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{openclawIMConfigKey: string(cfgJSON)}}

	plugin := im.NewWebhookIMPlugin("openclaw", func(context.Context) im.WebhookConfig {
		return im.WebhookConfig{}
	})

	handler := OpenclawIMWebhookHandler(sys, plugin)

	body := []byte(`{"text":"hello","message_type":"text"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// loadOpenclawIMConfig defaults to DefaultOpenclawIMSecret, so sign with it.
	req.Header.Set("X-OpenClaw-Signature", makeSignature(body, DefaultOpenclawIMSecret))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenclawIMWebhookHandler_NoSecretConfigured(t *testing.T) {
	// When secret is empty in DB, loadOpenclawIMConfig fills DefaultOpenclawIMSecret.
	// The handler will verify against that default, so we must sign with it.
	cfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/hook", Secret: ""}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{openclawIMConfigKey: string(cfgJSON)}}

	plugin := im.NewWebhookIMPlugin("openclaw", func(context.Context) im.WebhookConfig {
		return im.WebhookConfig{}
	})
	var received bool
	plugin.ReceiveMessage(func(msg im.IncomingMessage) { received = true })

	handler := OpenclawIMWebhookHandler(sys, plugin)

	body := []byte(`{"platform_uid":"u1","text":"hello","message_type":"text"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenClaw-Signature", makeSignature(body, DefaultOpenclawIMSecret))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !received {
		t.Fatal("message was not injected")
	}
}

func TestOpenclawIMWebhookHandler_TenantScopedSecret(t *testing.T) {
	globalCfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/global", Secret: "global-secret"}
	tenantCfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/tenant-a", Secret: "tenant-a-secret"}
	globalJSON, _ := json.Marshal(globalCfg)
	tenantJSON, _ := json.Marshal(tenantCfg)
	sys := &stubSystemSettings{data: map[string]string{
		openclawIMConfigKey:                      string(globalJSON),
		"tenant:tenant_a:" + openclawIMConfigKey: string(tenantJSON),
	}}

	plugin := im.NewWebhookIMPlugin("openclaw", func(context.Context) im.WebhookConfig { return im.WebhookConfig{} })
	var received *im.IncomingMessage
	plugin.ReceiveMessage(func(msg im.IncomingMessage) { received = &msg })

	handler := OpenclawIMWebhookHandler(sys, plugin)
	msg := im.IncomingMessage{TenantID: "tenant_a", PlatformUID: "user-a", Text: "hello", MessageType: "text"}
	body, _ := json.Marshal(msg)

	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenClaw-Signature", makeSignature(body, globalCfg.Secret))
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected global secret to be rejected for tenant payload, got %d: %s", w.Code, w.Body.String())
	}
	if received != nil {
		t.Fatal("message should not be injected when signed with the wrong tenant secret")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenClaw-Signature", makeSignature(body, tenantCfg.Secret))
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected tenant secret to pass, got %d: %s", w.Code, w.Body.String())
	}
	if received == nil {
		t.Fatal("message was not injected")
	}
	if received.TenantID != "tenant_a" {
		t.Fatalf("expected tenant_id tenant_a, got %q", received.TenantID)
	}
}

func TestOpenclawIMWebhookHandler_TenantHintHeader(t *testing.T) {
	tenantCfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/tenant-b", Secret: "tenant-b-secret"}
	tenantJSON, _ := json.Marshal(tenantCfg)
	sys := &stubSystemSettings{data: map[string]string{
		"tenant:tenant_b:" + openclawIMConfigKey: string(tenantJSON),
	}}

	plugin := im.NewWebhookIMPlugin("openclaw", func(context.Context) im.WebhookConfig { return im.WebhookConfig{} })
	var received *im.IncomingMessage
	plugin.ReceiveMessage(func(msg im.IncomingMessage) { received = &msg })

	body := []byte(`{"platform_uid":"user-b","text":"hello","message_type":"text"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant_b")
	req.Header.Set("X-OpenClaw-Signature", makeSignature(body, tenantCfg.Secret))
	w := httptest.NewRecorder()

	OpenclawIMWebhookHandler(sys, plugin)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if received == nil || received.TenantID != "tenant_b" {
		t.Fatalf("expected injected tenant_b message, got %#v", received)
	}
}

func TestOpenclawIMWebhookHandler_DefaultTenantUsesLegacyConfig(t *testing.T) {
	cfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/hook", Secret: "legacy-secret"}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{openclawIMConfigKey: string(cfgJSON)}}

	plugin := im.NewWebhookIMPlugin("openclaw", func(context.Context) im.WebhookConfig { return im.WebhookConfig{} })
	var received *im.IncomingMessage
	plugin.ReceiveMessage(func(msg im.IncomingMessage) { received = &msg })

	msg := im.IncomingMessage{TenantID: store.DefaultTenantID, PlatformUID: "legacy-user", Text: "hello", MessageType: "text"}
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenClaw-Signature", makeSignature(body, cfg.Secret))
	w := httptest.NewRecorder()

	OpenclawIMWebhookHandler(sys, plugin)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if received == nil || received.TenantID != store.DefaultTenantID {
		t.Fatalf("expected default tenant message, got %#v", received)
	}
}
