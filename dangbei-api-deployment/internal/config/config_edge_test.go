package config

import (
	"encoding/json"
	"testing"
)

// ─── GetModelConfig edge cases ───────────────────────────────────────

func TestGetModelConfigDeepSeekChat(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-chat")
	if !ok {
		t.Fatal("expected ok for deepseek-chat")
	}
	if thinking || search {
		t.Fatalf("expected no thinking/search for deepseek-chat, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekReasoner(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-reasoner")
	if !ok {
		t.Fatal("expected ok for deepseek-reasoner")
	}
	if !thinking || search {
		t.Fatalf("expected thinking=true search=false, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekChatSearch(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-chat-search")
	if !ok {
		t.Fatal("expected ok for deepseek-chat-search")
	}
	if thinking || !search {
		t.Fatalf("expected thinking=false search=true, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigDeepSeekReasonerSearch(t *testing.T) {
	thinking, search, ok := GetModelConfig("deepseek-reasoner-search")
	if !ok {
		t.Fatal("expected ok for deepseek-reasoner-search")
	}
	if !thinking || !search {
		t.Fatalf("expected both true, got thinking=%v search=%v", thinking, search)
	}
}

func TestGetModelConfigCaseInsensitive(t *testing.T) {
	thinking, search, ok := GetModelConfig("DeepSeek-Chat")
	if !ok {
		t.Fatal("expected ok for case-insensitive deepseek-chat")
	}
	if thinking || search {
		t.Fatalf("expected no thinking/search for case-insensitive deepseek-chat")
	}
}

func TestGetModelConfigUnknownModel(t *testing.T) {
	_, _, ok := GetModelConfig("gpt-4")
	if ok {
		t.Fatal("expected not ok for unknown model")
	}
}

func TestGetModelConfigEmpty(t *testing.T) {
	_, _, ok := GetModelConfig("")
	if ok {
		t.Fatal("expected not ok for empty model")
	}
}

// ─── lower function ──────────────────────────────────────────────────

func TestLowerFunction(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello", "hello"},
		{"ALLCAPS", "allcaps"},
		{"already-lower", "already-lower"},
		{"Mixed-CASE-123", "mixed-case-123"},
		{"", ""},
	}
	for _, tc := range tests {
		got := lower(tc.input)
		if got != tc.expected {
			t.Errorf("lower(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// ─── Config JSON roundtrip (standard encoding/json) ──────────────────

func TestConfigJSONRoundtrip(t *testing.T) {
	cfg := Config{
		Keys: []string{"key1", "key2"},
		Accounts: []Account{
			{Name: "user", Token: "tok", Status: "active"},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(decoded.Keys) != 2 || decoded.Keys[0] != "key1" {
		t.Fatalf("unexpected keys: %#v", decoded.Keys)
	}
	if len(decoded.Accounts) != 1 || decoded.Accounts[0].Name != "user" || decoded.Accounts[0].Token != "tok" {
		t.Fatalf("unexpected accounts: %#v", decoded.Accounts)
	}
}

func TestConfigGetNextTokenPrefersActive(t *testing.T) {
	cfg := Config{
		Accounts: []Account{
			{Name: "failed", Token: "t-failed", Status: "failed"},
			{Name: "active", Token: "t-active", Status: "active"},
		},
	}
	if got := cfg.GetNextToken(); got != "t-active" {
		t.Fatalf("GetNextToken = %q, want t-active", got)
	}
}

func TestConfigGetNextTokenFallsBackToFirst(t *testing.T) {
	cfg := Config{
		Accounts: []Account{
			{Name: "only", Token: "t-only", Status: "unknown"},
		},
	}
	if got := cfg.GetNextToken(); got != "t-only" {
		t.Fatalf("GetNextToken = %q, want t-only", got)
	}
}

func TestConfigGetAccountStats(t *testing.T) {
	cfg := Config{
		Accounts: []Account{
			{Status: "active"},
			{Status: "active"},
			{Status: "failed"},
			{Status: ""},
		},
	}
	stats := cfg.GetAccountStats()
	if stats["total"] != 4 || stats["active"] != 2 || stats["failed"] != 1 || stats["unknown"] != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}
