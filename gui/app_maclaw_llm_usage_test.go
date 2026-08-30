package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestGetOpenAIUsageRejectsNonOpenAIOAuthProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "xAI-Grok", URL: "https://api.x.ai/v1", AuthType: "oauth", Key: "xai-oauth-token", Model: "grok-4.6"},
		},
		MaclawLLMCurrentProvider: "xAI-Grok",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err := app.GetOpenAIUsage()
	if err == nil {
		t.Fatal("expected GetOpenAIUsage to reject xAI-Grok")
	}
	if !strings.Contains(err.Error(), "OAuth") && !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("error = %q, want OAuth/unsupported rejection", err)
	}
}

func TestGetOpenAIUsageRejectsCodexOAuthWithoutHTTP(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "OpenAI", URL: "https://chatgpt.com/backend-api/codex", AuthType: "oauth", Key: "chatgpt-jwt", Model: "gpt-5.6-luna"},
		},
		MaclawLLMCurrentProvider: "OpenAI",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err := app.GetOpenAIUsage()
	if err == nil {
		t.Fatal("expected GetOpenAIUsage to reject Codex OAuth")
	}
	if !strings.Contains(err.Error(), "ChatGPT/Codex") && !strings.Contains(err.Error(), "Admin API Key") {
		t.Fatalf("error = %q, want Codex/admin-key rejection", err)
	}
}
