package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
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

func TestOpenclawIMWebhookHandler_Success(t *testing.T) {
	secret := "test-secret-123"
	cfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/hook", Secret: secret}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{openclawIMConfigKey: string(cfgJSON)}}

	plugin := im.NewWebhookIMPlugin("openclaw", func() im.WebhookConfig {
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

	plugin := im.NewWebhookIMPlugin("openclaw", func() im.WebhookConfig {
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

	plugin := im.NewWebhookIMPlugin("openclaw", func() im.WebhookConfig {
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

	plugin := im.NewWebhookIMPlugin("openclaw", func() im.WebhookConfig {
		return im.WebhookConfig{}
	})

	handler := OpenclawIMWebhookHandler(sys, plugin)

	body := []byte(`{"text":"hello","message_type":"text"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenclawIMWebhookHandler_NoSecretConfigured(t *testing.T) {
	// When no secret is configured, signature verification should be skipped.
	cfg := OpenclawIMConfigState{Enabled: true, WebhookURL: "http://example.com/hook", Secret: ""}
	cfgJSON, _ := json.Marshal(cfg)
	sys := &stubSystemSettings{data: map[string]string{openclawIMConfigKey: string(cfgJSON)}}

	plugin := im.NewWebhookIMPlugin("openclaw", func() im.WebhookConfig {
		return im.WebhookConfig{}
	})
	var received bool
	plugin.ReceiveMessage(func(msg im.IncomingMessage) { received = true })

	handler := OpenclawIMWebhookHandler(sys, plugin)

	body := []byte(`{"platform_uid":"u1","text":"hello","message_type":"text"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/openclaw_im/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No signature header.
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !received {
		t.Fatal("message was not injected")
	}
}
