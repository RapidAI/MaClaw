package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGetDesktopBotScreenReadsTheBotDisplay(t *testing.T) {
	t.Cleanup(func() { desktopBotExchangeOverride = nil })
	var method, path string
	desktopBotExchangeOverride = func(_ context.Context, gotMethod, gotPath string, body any, dest any, timeout time.Duration, _ map[string]string) (int, error) {
		method, path = gotMethod, gotPath
		if body != nil {
			t.Fatal("GET desktop has no body")
		}
		if timeout != 20*time.Second {
			t.Fatalf("timeout = %s", timeout)
		}
		fillDesktopBotDest(t, dest, map[string]any{
			"novnc_url":        "/api/v1/desktop-handoff/bot/vnc.html",
			"user_control":     true,
			"attention_reason": "请登录 Google",
			"desktop_mode":     "isolated",
		})
		return http.StatusOK, nil
	}
	app := &App{}
	got, err := app.GetDesktopBotScreen("bot/1")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet || path != "/api/v1/bots/bot%2F1/desktop" {
		t.Fatalf("call = %s %s", method, path)
	}
	if got.DesktopMode != "isolated" || got.AttentionReason != "请登录 Google" || !got.UserControl {
		t.Fatalf("screen = %#v", got)
	}
	// A configured Hub turns the relative path into an absolute viewer URL.
	const rawNovnc = "/api/v1/desktop-handoff/bot/vnc.html"
	if got.NovncURL != rawNovnc && !strings.HasSuffix(got.NovncURL, rawNovnc) {
		t.Fatalf("novnc = %q", got.NovncURL)
	}
	if _, err := app.GetDesktopBotScreen(" "); err == nil {
		t.Fatal("empty bot id should be rejected")
	}
}

func TestHandBackDesktopBotOmitsTheBody(t *testing.T) {
	t.Cleanup(func() { desktopBotExchangeOverride = nil })
	var method, path string
	var sawBody any
	desktopBotExchangeOverride = func(_ context.Context, gotMethod, gotPath string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		method, path, sawBody = gotMethod, gotPath, body
		fillDesktopBotDest(t, dest, map[string]any{
			"handed_back": true,
			"accepted":    true,
			"run_id":      "run_1",
			"status":      "running",
		})
		return http.StatusAccepted, nil
	}
	got, err := (&App{}).HandBackDesktopBot("bot_1")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/v1/bots/bot_1/desktop/handback" {
		t.Fatalf("call = %s %s", method, path)
	}
	if sawBody != nil {
		t.Fatalf("body = %#v", sawBody)
	}
	if !got.HandedBack || !got.Accepted || got.RunID != "run_1" || got.Status != "running" {
		t.Fatalf("handback = %#v", got)
	}
}

func TestSetDesktopBotDesktopModeUsesTheAdminRoute(t *testing.T) {
	t.Cleanup(func() { desktopBotExchangeOverride = nil })
	var method, path string
	var raw []byte
	desktopBotExchangeOverride = func(_ context.Context, gotMethod, gotPath string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		method, path = gotMethod, gotPath
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = encoded
		fillDesktopBotDest(t, dest, map[string]any{"id": "bot_1", "desktop_mode": "isolated"})
		return http.StatusOK, nil
	}
	mode, err := (&App{}).SetDesktopBotDesktopMode("bot_1", "isolated")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || path != "/api/admin/bots/bot_1/desktop-mode" {
		t.Fatalf("call = %s %s", method, path)
	}
	if string(raw) != `{"desktop_mode":"isolated"}` {
		t.Fatalf("body = %s", raw)
	}
	if mode != "isolated" {
		t.Fatalf("mode = %q", mode)
	}
	if _, err := (&App{}).SetDesktopBotDesktopMode("bot_1", "own"); err == nil {
		t.Fatal("unknown mode should be rejected")
	}
}

func TestDesktopBotDesktopAdminHidesTheSwitchForAMachineToken(t *testing.T) {
	t.Cleanup(func() { desktopBotExchangeOverride = nil })
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
		if method != http.MethodGet || path != "/api/admin/bots/settings" {
			t.Fatalf("call = %s %s", method, path)
		}
		return http.StatusForbidden, errors.New("Tenant admin authorization required")
	}
	ok, err := (&App{}).DesktopBotDesktopAdmin()
	if err != nil || ok {
		t.Fatalf("admin = %v err = %v", ok, err)
	}
	desktopBotExchangeOverride = func(_ context.Context, _ string, _ string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
		return http.StatusOK, nil
	}
	ok, err = (&App{}).DesktopBotDesktopAdmin()
	if err != nil || !ok {
		t.Fatalf("admin = %v err = %v", ok, err)
	}
}
