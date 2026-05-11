package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolveCreateSessionProviderUsesExplicitProvider(t *testing.T) {
	cfg := createSessionProviderTestConfig()

	got := resolveCreateSessionProvider(cfg, "claude", "DeepSeek")

	if got.Error != "" {
		t.Fatalf("expected no error, got %q", got.Error)
	}
	if got.ResolvedProvider != "DeepSeek" {
		t.Fatalf("expected DeepSeek, got %q", got.ResolvedProvider)
	}
	if len(got.Hints) != 0 {
		t.Fatalf("expected no fallback hints, got %#v", got.Hints)
	}
}

func TestResolveCreateSessionProviderUsesConfiguredDefaultProvider(t *testing.T) {
	cfg := createSessionProviderTestConfig()
	cfg.DefaultTool = "claude"
	cfg.DefaultToolProvider = "DeepSeek"

	got := resolveCreateSessionProvider(cfg, "Claude", "")

	if got.Error != "" {
		t.Fatalf("expected no error, got %q", got.Error)
	}
	if got.ResolvedProvider != "DeepSeek" {
		t.Fatalf("expected default provider DeepSeek, got %q", got.ResolvedProvider)
	}
}

func TestResolveCreateSessionProviderFallsBackFromInvalidConfiguredDefaultProvider(t *testing.T) {
	cfg := createSessionProviderTestConfig()
	cfg.DefaultTool = "claude"
	cfg.DefaultToolProvider = "MissingProvider"

	got := resolveCreateSessionProvider(cfg, "claude", "")

	if got.Error != "" {
		t.Fatalf("expected fallback to auto provider, got %q", got.Error)
	}
	if got.ResolvedProvider != "Original" {
		t.Fatalf("expected fallback to Original, got %q", got.ResolvedProvider)
	}
}

func TestResolveCreateSessionProviderReportsUnknownTool(t *testing.T) {
	got := resolveCreateSessionProvider(corelib.AppConfig{}, "unknown-tool", "")

	if got.Error == "" {
		t.Fatal("expected an error for unknown tool")
	}
}

func createSessionProviderTestConfig() corelib.AppConfig {
	return corelib.AppConfig{
		Claude: corelib.ToolConfig{
			CurrentModel: "Original",
			Models: []corelib.ModelConfig{
				{ModelName: "Original", ModelId: "orig-id", IsBuiltin: true},
				{ModelName: "DeepSeek", ModelId: "ds-id", ApiKey: "sk-test"},
			},
		},
	}
}
