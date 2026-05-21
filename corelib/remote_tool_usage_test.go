package corelib

import "testing"

func TestIsRemoteCodingToolTokenUsageProvider(t *testing.T) {
	for _, provider := range []string{
		" Codex:gpt-5.4 ",
		"CLAUDE:sonnet",
		"Gemini:2.5-pro",
		"remote:opencode",
	} {
		if !IsRemoteCodingToolTokenUsageProvider(provider) {
			t.Fatalf("expected %q to be remote coding tool usage", provider)
		}
	}
	for _, provider := range []string{"", "provider-a", "MaClaw 官方", "custom-codex-provider"} {
		if IsRemoteCodingToolTokenUsageProvider(provider) {
			t.Fatalf("expected %q to remain normal provider usage", provider)
		}
	}
}

func TestFilterRemoteCodingToolTokenUsage(t *testing.T) {
	usage := map[string]*TokenUsageStat{
		"codex:gpt-5.4": {InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280},
		"provider-a":    {InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		"provider-b":    nil,
	}

	filtered := FilterRemoteCodingToolTokenUsage(usage)
	if _, ok := filtered["codex:gpt-5.4"]; ok {
		t.Fatalf("remote coding tool usage key should be filtered: %#v", filtered)
	}
	if filtered["provider-b"] != nil {
		t.Fatalf("nil normal provider usage should remain nil: %#v", filtered["provider-b"])
	}
	filtered["provider-a"].InputTokens = 999
	if usage["provider-a"].InputTokens != 10 {
		t.Fatalf("normal usage was not cloned: %#v", usage["provider-a"])
	}
}
