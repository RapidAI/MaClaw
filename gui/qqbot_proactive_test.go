package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestQQResolveProactiveOpenID_ConfigBeatsLast(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		QQBotOwnerOpenID: "openid-from-config",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	m := &qqBotGatewayManager{app: app, lastOpenID: "openid-from-chat"}
	if got := m.ResolveProactiveOpenID("self"); got != "openid-from-config" {
		t.Fatalf("ResolveProactiveOpenID(self)=%q, want config openid", got)
	}
	if got := m.ResolveProactiveOpenID(""); got != "openid-from-config" {
		t.Fatalf("empty self=%q", got)
	}
	if got := m.ResolveProactiveOpenID("explicit-oid"); got != "explicit-oid" {
		t.Fatalf("explicit = %q", got)
	}
	if !m.HasProactiveSession() {
		t.Fatal("expected HasProactiveSession with config openid")
	}
}

func TestQQResolveProactiveOpenID_LastOpenIDFallback(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_ = app.SaveConfig(corelib.AppConfig{})
	m := &qqBotGatewayManager{app: app, lastOpenID: "oid-chat"}
	if got := m.ResolveProactiveOpenID("self"); got != "oid-chat" {
		t.Fatalf("got %q, want last chat openid", got)
	}
}

func TestQQHasProactiveSession_Empty(t *testing.T) {
	m := &qqBotGatewayManager{}
	if m.HasProactiveSession() {
		t.Fatal("empty manager should not have session")
	}
	m.lastOpenID = "x"
	if !m.HasProactiveSession() {
		t.Fatal("lastOpenID should count as session")
	}
}

func TestDeliverToOwnerChannel_QQNeedsPeerOrHub(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	// No gateway, no hub → clear error
	svc := app.watchService()
	err := svc.deliverToOwnerChannel("qq", "hello watch")
	if err == nil {
		t.Fatal("expected error without qq gateway/hub")
	}
}

func TestDeliverToOwnerChannel_TelegramNeedsPeerOrHub(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	if err := svc.deliverToOwnerChannel("telegram", "hello"); err == nil {
		t.Fatal("expected error without telegram gateway/hub")
	}
}

func TestTelegramHasProactiveSession(t *testing.T) {
	m := &telegramGatewayManager{}
	if m.HasProactiveSession() {
		t.Fatal("empty should be false")
	}
	m.lastChatID = 42
	if !m.HasProactiveSession() {
		t.Fatal("lastChatID should enable session")
	}
}

func TestTelegramResolveProactiveChatID_ConfigBeatsLast(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var ownerChat corelib.DecimalInt64String
	_ = ownerChat.SetString("999001")
	if err := app.SaveConfig(corelib.AppConfig{
		TelegramBotOwnerChatID: ownerChat,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	m := &telegramGatewayManager{app: app, lastChatID: 42}
	if got := m.ResolveProactiveChatID(0); got != 999001 {
		t.Fatalf("self resolve=%d, want config 999001", got)
	}
	if got := m.ResolveProactiveChatID(123); got != 123 {
		t.Fatalf("explicit=%d", got)
	}
	if !m.HasProactiveSession() {
		t.Fatal("config chat_id should enable session")
	}
}

func TestTelegramResolveProactiveChatID_LastFallback(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_ = app.SaveConfig(corelib.AppConfig{})
	m := &telegramGatewayManager{app: app, lastChatID: 77}
	if got := m.ResolveProactiveChatID(0); got != 77 {
		t.Fatalf("got %d", got)
	}
}

func TestStatusDetailLocalOrHub(t *testing.T) {
	if got := statusDetailLocalOrHub(false, "connected", false, false, "hint"); got != "未在设置中启用" {
		t.Fatalf("disabled: %q", got)
	}
	if got := statusDetailLocalOrHub(true, "connected", true, false, "hint"); got != "本地可推送" {
		t.Fatalf("local: %q", got)
	}
	if got := statusDetailLocalOrHub(true, "disconnected", false, true, "hint"); got != "可经 Hub 推送" {
		t.Fatalf("hub: %q", got)
	}
}

func TestDeliverLocalOrHub_NoPathHint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	err := svc.deliverLocalOrHub("X", "hi", nil, "custom no path")
	if err == nil || err.Error() != "custom no path" {
		t.Fatalf("want custom no path, got %v", err)
	}
}

func TestDeliverLocalOrHub_LocalSuccessSkipsHub(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	called := 0
	err := svc.deliverLocalOrHub("X", "hi", func() error {
		called++
		return nil
	}, "unused")
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("local calls=%d", called)
	}
}

func TestDeliverLocalOrHub_LocalFailNoHub(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	err := svc.deliverLocalOrHub("X", "hi", func() error {
		return fmt.Errorf("local boom")
	}, "unused")
	if err == nil {
		t.Fatal("expected error")
	}
	// Local failed and Hub is not connected → combined or hub-only style message.
	msg := err.Error()
	if !strings.Contains(msg, "local boom") && !strings.Contains(msg, "Hub") {
		t.Fatalf("want local/hub failure detail, got %q", msg)
	}
}

func TestDeliverToOwnerChannel_EmptyAndUnknown(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	svc := app.watchService()
	if err := svc.deliverToOwnerChannel("qq", "  "); err == nil {
		t.Fatal("empty text should fail")
	}
	if err := svc.deliverToOwnerChannel("nope", "x"); err == nil {
		t.Fatal("unknown channel should fail")
	}
}
