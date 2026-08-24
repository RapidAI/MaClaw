package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
)

func TestApplyQQBotQRCredentialsWritesAppConfig(t *testing.T) {
	cfg := corelib.AppConfig{}
	if err := applyQQBotQRCredentials(&cfg, &qqbot.QRCredentials{AppID: "102088001", AppSecret: "sec", UserOpenID: "owner-1"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !cfg.QQBotEnabled || cfg.QQBotAppID != "102088001" || cfg.QQBotAppSecret != "sec" || cfg.QQBotOwnerOpenID != "owner-1" {
		t.Fatalf("cfg = %#v", cfg)
	}
	if cfg.QQBotLocalMode == nil || !*cfg.QQBotLocalMode {
		t.Fatal("local mode should default on")
	}

	cfg.QQBotOwnerOpenID = "keep-me"
	if err := applyQQBotQRCredentials(&cfg, &qqbot.QRCredentials{AppID: "102088002", AppSecret: "sec2", UserOpenID: "other"}); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if cfg.QQBotOwnerOpenID != "keep-me" {
		t.Fatalf("existing owner should be kept, got %q", cfg.QQBotOwnerOpenID)
	}
}

func TestTUIQQBotQRStatusMessage(t *testing.T) {
	if got := tuiQQBotQRStatusMessage("en", qqbot.QRLoginStatusPending); !strings.Contains(got, "Confirm") {
		t.Fatalf("en pending = %q", got)
	}
	if got := tuiQQBotQRStatusMessage("zh", qqbot.QRLoginStatusWait); !strings.Contains(got, "扫码") {
		t.Fatalf("zh wait = %q", got)
	}
}

func TestPersistQQBotQRCredentialsKeepsExistingFields(t *testing.T) {
	store := commands.NewFileConfigStore(t.TempDir())
	orig := corelib.AppConfig{MaclawLLMKey: "keep-key", QQBotOwnerOpenID: "owner"}
	if err := store.SaveConfig(orig); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := persistQQBotQRCredentials(store, &qqbot.QRCredentials{AppID: "102088001", AppSecret: "sec", UserOpenID: "other"}); err != nil {
		t.Fatalf("persist: %v", err)
	}
	got, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.MaclawLLMKey != "keep-key" {
		t.Fatalf("llm key wiped: %#v", got)
	}
	if got.QQBotOwnerOpenID != "owner" || got.QQBotAppID != "102088001" || !got.QQBotEnabled {
		t.Fatalf("qq fields = %#v", got)
	}
}

func TestPersistQQBotQRCredentialsDoesNotWipeOnLoadError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := commands.NewFileConfigStore(dir)
	err := persistQQBotQRCredentials(store, &qqbot.QRCredentials{AppID: "102088001", AppSecret: "sec"})
	if err == nil || !errors.Is(err, errQQBotQRConfigLoad) {
		t.Fatalf("err = %v, want load error", err)
	}
	data, readErr := os.ReadFile(cfgPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "{not-json" {
		t.Fatalf("config was overwritten: %q", data)
	}
}

func TestPollQQBotQRLoginCmdMapsMissingSessionToExpired(t *testing.T) {
	prev := tuiQQBotQR
	tuiQQBotQR = qqbot.NewQRClient()
	t.Cleanup(func() { tuiQQBotQR = prev })
	msg, ok := pollQQBotQRLoginCmd("en", "missing-token")().(views.ConfigQQBotPollResultMsg)
	if !ok {
		t.Fatal("expected poll result")
	}
	if msg.Status != qqbot.QRLoginStatusExpired.String() || !msg.Completed || msg.Success {
		t.Fatalf("missing session = %#v", msg)
	}
}
