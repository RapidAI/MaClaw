package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestNormalizeAgentTypeStripsControlsAndCapsLength(t *testing.T) {
	if got := normalizeAgentType("  Codex \n"); got != "Codex" {
		t.Fatalf("normalizeAgentType = %q, want Codex", got)
	}
	if got := normalizeAgentType(" \t\r\n "); got != "" {
		t.Fatalf("blank agent type = %q, want empty", got)
	}
	long := strings.Repeat("a", maxAgentTypeRunes+10)
	if got := normalizeAgentType(long); len([]rune(got)) != maxAgentTypeRunes {
		t.Fatalf("capped length = %d, want %d", len([]rune(got)), maxAgentTypeRunes)
	}
}

func TestSaveSettingsRejectsNonASCIIAgentType(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	app := NewApp()
	_, err := app.SaveSettings(Settings{APIKey: defaultProxyAPIKey, AgentType: "Codex 工具"})
	if err == nil || !strings.Contains(err.Error(), "User-Agent") {
		t.Fatalf("SaveSettings error = %v, want a User-Agent validation error", err)
	}
	saved, loadErr := loadSettings()
	if loadErr != nil {
		t.Fatalf("load settings: %v", loadErr)
	}
	if saved.AgentType != "" {
		t.Fatalf("rejected agent type was stored as %q", saved.AgentType)
	}
	app.stopProxy()
}

func TestProxyClientNameDefaultsToQAgent(t *testing.T) {
	if got := proxyClientName(Settings{}); got != corelib.CodeGenClientName {
		t.Fatalf("empty client name = %q, want %q", got, corelib.CodeGenClientName)
	}
	if got := proxyClientName(Settings{AgentType: "opencode"}); got != "opencode" {
		t.Fatalf("client name = %q, want opencode", got)
	}
}

func TestSaveSettingsPersistsAgentTypeAndKeepsOmittedValue(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	app := NewApp()
	if _, err := app.SaveSettings(Settings{APIKey: defaultProxyAPIKey, AgentType: "  claude code  "}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	saved, err := loadSettings()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if saved.AgentType != "claude code" {
		t.Fatalf("saved agent type = %q, want claude code", saved.AgentType)
	}

	if _, err := app.SaveSettings(Settings{APIKey: defaultProxyAPIKey}); err != nil {
		t.Fatalf("SaveSettings omit agent: %v", err)
	}
	saved, err = loadSettings()
	if err != nil {
		t.Fatalf("reload settings: %v", err)
	}
	if saved.AgentType != "claude code" {
		t.Fatalf("omitted agent type = %q, want the saved claude code", saved.AgentType)
	}
	app.stopProxy()
}

func TestPostChatSendsClientNameAndLetsVendorOverride(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	resp, err := postChat(context.Background(), upstream.Client(), upstream.URL, "key", "Codex", []byte(`{}`), "application/json", false, nil)
	if err != nil {
		t.Fatalf("postChat: %v", err)
	}
	_ = resp.Body.Close()
	if got != "Codex" {
		t.Fatalf("User-Agent = %q, want Codex", got)
	}

	resp, err = postChat(context.Background(), upstream.Client(), upstream.URL, "key", "Codex", []byte(`{}`), "application/json", false, func(h http.Header) {
		h.Set("User-Agent", "vendor-required")
	})
	if err != nil {
		t.Fatalf("postChat override: %v", err)
	}
	_ = resp.Body.Close()
	if got != "vendor-required" {
		t.Fatalf("vendor User-Agent = %q, want vendor-required", got)
	}
}
