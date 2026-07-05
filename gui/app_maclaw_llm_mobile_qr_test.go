package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCreateMobileLLMDesktopQRSessionUsesHubSessionEndpoint(t *testing.T) {
	var seenAuth string
	var seenBody map[string]any
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mobile/llm/desktop-qr-sessions" {
			t.Fatalf("path = %s, want desktop QR session endpoint", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"created","session_id":"mlqr_test","expires_at":"2026-07-02T12:00:00Z","qr_payload":"{\"v\":2,\"type\":\"maclaw_mobile_llm_authorization\",\"session_id\":\"mlqr_test\",\"hub_url\":\"https://tenant-a.maclaw.top\"}"}`))
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL + "/",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	session, err := app.CreateMobileLLMDesktopQRSession(
		"OpenAI Compatible",
		" https://llm.example.com/v1/ ",
		" sk-test ",
		"gpt-4.1-mini",
		[]string{"gpt-4.1-mini"},
		"openai",
	)
	if err != nil {
		t.Fatalf("CreateMobileLLMDesktopQRSession: %v", err)
	}

	if seenAuth != "Bearer viewer-token" {
		t.Fatalf("Authorization = %q, want viewer bearer token", seenAuth)
	}
	if seenBody["name"] != "OpenAI Compatible" || seenBody["url"] != "https://llm.example.com/v1/" || seenBody["key"] != "sk-test" || seenBody["model"] != "gpt-4.1-mini" {
		t.Fatalf("body = %#v, want provider config", seenBody)
	}
	if session.SessionID != "mlqr_test" || !strings.Contains(session.QRPayload, "maclaw_mobile_llm_authorization") {
		t.Fatalf("session = %#v, want mobile QR session payload", session)
	}
	if strings.Contains(session.QRPayload, "sk-test") {
		t.Fatalf("qr payload leaked API key: %s", session.QRPayload)
	}
}

func TestCreateMobileLLMDesktopQRSessionRequiresHubLogin(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	_, err := app.CreateMobileLLMDesktopQRSession("Provider", "https://llm.example.com/v1", "sk-test", "model", nil, "openai")
	if err == nil || !strings.Contains(err.Error(), "Hub login is required") {
		t.Fatalf("error = %v, want Hub login requirement", err)
	}
}

func TestCreateMobileAuthDesktopQRSessionUsesHubAuthSessionEndpoint(t *testing.T) {
	var seenAuth string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mobile/auth/desktop-qr-sessions" {
			t.Fatalf("path = %s, want mobile auth QR session endpoint", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"created","session_id":"maqr_test","expires_at":"2026-07-05T12:00:00Z","qr_payload":"{\"v\":2,\"type\":\"maclaw_mobile_desktop_authorization\",\"session_id\":\"maqr_test\",\"hub_url\":\"https://tenant-a.maclaw.top\"}"}`))
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.URL + "/",
		RemoteViewerToken:  "viewer-token",
		RemoteMachineID:    "m_123",
		RemoteMachineToken: "mt_123",
		RemoteEmail:        "phone:19900001111",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	session, err := app.CreateMobileAuthDesktopQRSession()
	if err != nil {
		t.Fatalf("CreateMobileAuthDesktopQRSession: %v", err)
	}

	if seenAuth != "Bearer viewer-token" {
		t.Fatalf("Authorization = %q, want viewer bearer token", seenAuth)
	}
	if session.SessionID != "maqr_test" || !strings.Contains(session.QRPayload, "maclaw_mobile_desktop_authorization") {
		t.Fatalf("session = %#v, want mobile auth QR session payload", session)
	}
}

func TestCreateMobileAuthDesktopQRSessionRequiresBoundPhone(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       "https://hub.example.com",
		RemoteViewerToken:  "viewer-token",
		RemoteMachineID:    "m_123",
		RemoteMachineToken: "mt_123",
		RemoteEmail:        "dev@example.com",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	_, err := app.CreateMobileAuthDesktopQRSession()
	if err == nil || !strings.Contains(err.Error(), "requires a bound phone number") {
		t.Fatalf("error = %v, want bound phone requirement", err)
	}
}
