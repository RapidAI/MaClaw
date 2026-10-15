package llmservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/lobsterai"
	"github.com/RapidAI/CodeClaw/corelib/qoder"
	"github.com/RapidAI/CodeClaw/corelib/trae"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

func TestProviderAgentTypeStoredAndKeptOnPartialUpdate(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "OpenCode-1", Name: "OpenCode-1", APIURL: "https://opencode.ai/zen/go/v1", APIKey: "sk-secret",
		AgentType: "OpenCode\r\n",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, err := svc.GetProvider(ctx, "OpenCode-1")
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.AgentType != "OpenCode" {
		t.Fatalf("agent type = %q, want OpenCode without the line break", got.AgentType)
	}

	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID: "OpenCode-1", Name: "OpenCode-1", APIURL: "https://opencode.ai/zen/go/v1",
	}); err != nil {
		t.Fatalf("partial update: %v", err)
	}
	got, err = svc.GetProvider(ctx, "OpenCode-1")
	if err != nil || got == nil || got.AgentType != "OpenCode" {
		t.Fatalf("agent type after partial update = %#v err=%v", got, err)
	}

	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID: "OpenCode-1", Name: "OpenCode-1", APIURL: "https://opencode.ai/zen/go/v1", AgentType: "openclaw",
	}); err != nil {
		t.Fatalf("reset default: %v", err)
	}
	got, err = svc.GetProvider(ctx, "OpenCode-1")
	if err != nil || got == nil || got.AgentType != "" {
		t.Fatalf("agent type after default = %#v err=%v", got, err)
	}

	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID: "OpenCode-1", Name: "OpenCode-1", APIURL: "https://opencode.ai/zen/go/v1", AgentType: "ZCode",
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err = svc.GetProvider(ctx, "OpenCode-1")
	if err != nil || got == nil || got.AgentType != "ZCode" {
		t.Fatalf("agent type after replace = %#v err=%v", got, err)
	}

	copied, err := svc.CopyProvider(ctx, "OpenCode-1")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if copied.AgentType != "ZCode" {
		t.Fatalf("copy agent type = %q, want ZCode", copied.AgentType)
	}
}

func TestNormalizeProviderAgentTypeCapsLength(t *testing.T) {
	provider := &llmpool.ProviderConfig{AgentType: strings.Repeat("a", 200)}
	normalizeProviderAgentType(provider)
	if got := len([]rune(provider.AgentType)); got != 128 {
		t.Fatalf("len = %d, want 128", got)
	}
}

func TestNormalizeProviderAgentTypeAliases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Claude Code", "claude code 2.0"},
		{"claude-code/2.0.0", "claude code 2.0"},
		{"tigerclaw", "QAgent"},
		{"__custom__", ""},
		{"OpenClaw", ""},
		{"opencode", "opencode"},
		{"OpenCode", "OpenCode"},
		{"OPENCODE", "OpenCode"},
		{"WorkBuddy", workbuddy.UserAgent},
		{"codebuddy", workbuddy.UserAgent},
		{"CLI/1.0.0 CodeBuddy/1.0.0", workbuddy.UserAgent},
		{"Qoder", qoder.UserAgent},
		{"qoder/0.0.1", qoder.UserAgent},
		{"Trae", trae.UserAgentPrefix + trae.CNProfile().ClientVersion},
		{"trae/0.0.1", trae.UserAgentPrefix + trae.CNProfile().ClientVersion},
		{"LobsterAI", lobsterai.UserAgent},
		{"lobster", lobsterai.UserAgent},
		{"LobsterAI/0.0.1", lobsterai.UserAgent},
		{"qoder/my-agent", "qoder/my-agent"},
		{"CLI/2.63.2 CodeBuddy/2.63.2 extra", "CLI/2.63.2 CodeBuddy/2.63.2 extra"},
	}
	for _, tc := range cases {
		provider := &llmpool.ProviderConfig{AgentType: tc.in}
		normalizeProviderAgentType(provider)
		if provider.AgentType != tc.want {
			t.Fatalf("agent type %q = %q, want %q", tc.in, provider.AgentType, tc.want)
		}
	}
}

func TestOverlayHopCredentialsFillsEmptyAgentType(t *testing.T) {
	dst := &llmpool.ProviderConfig{ID: "OpenCode-1", APIKey: "sk-stored"}
	src := &llmpool.ProviderConfig{ID: "OpenCode-1", APIKey: "sk-from-hop", AgentType: "Claude Code"}
	overlayHopCredentials(dst, src)
	if dst.APIKey != "sk-stored" {
		t.Fatalf("api key = %q, want the stored key", dst.APIKey)
	}
	if dst.AgentType != "claude code 2.0" {
		t.Fatalf("agent type = %q, want claude code 2.0", dst.AgentType)
	}

	kept := &llmpool.ProviderConfig{ID: "OpenCode-1", AgentType: "Codex"}
	overlayHopCredentials(kept, src)
	if kept.AgentType != "Codex" {
		t.Fatalf("stored agent type = %q, want Codex", kept.AgentType)
	}
}

func TestVendorUserAgentPresetsMatchClients(t *testing.T) {
	if trae.CNProfile().ClientVersion != trae.GlobalProfile().ClientVersion {
		t.Fatal("Trae chat client versions diverged; the User-Agent preset needs one identity")
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "admin", "assets", "js", "llm-service-tab.js"))
	if err != nil {
		t.Fatalf("read user agent presets: %v", err)
	}
	js := string(raw)
	for _, preset := range []struct{ label, value string }{
		{"WorkBuddy", workbuddy.UserAgent},
		{"Qoder", qoder.UserAgent},
		{"Trae", traeChatUserAgent()},
		{"LobsterAI", lobsterai.UserAgent},
	} {
		needle := "value: '" + preset.value + "', label: '" + preset.label + "'"
		if !strings.Contains(js, needle) {
			t.Fatalf("preset missing %s", needle)
		}
	}
}
