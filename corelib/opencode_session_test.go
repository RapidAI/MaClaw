package corelib

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIsOpenCodeURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url  string
		want bool
	}{
		{"https://opencode.ai/zen/go/v1", true},
		{"https://opencode.ai/zen/v1/chat/completions", true},
		{"https://api.opencode.ai/zen/go/v1", true},
		{"https://opencode.ai.evil.example/zen/go/v1", false},
		{"https://evil.example/redirect?next=https://opencode.ai/zen/v1", false},
		{"https://api.deepseek.com/v1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsOpenCodeURL(tc.url); got != tc.want {
			t.Errorf("IsOpenCodeURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestSetOpenCodeSessionHeaderIfNeeded(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://opencode.ai/zen/go/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetOpenCodeSessionHeaderIfNeeded(req, "conv-1")
	if got := req.Header.Get(OpenCodeSessionHeader); got != "conv-1" {
		t.Fatalf("session header = %q, want conv-1", got)
	}

	SetOpenCodeSessionHeaderIfNeeded(req, "conv-2")
	if got := req.Header.Get(OpenCodeSessionHeader); got != "conv-1" {
		t.Fatalf("existing session header overwritten: %q", got)
	}

	other, err := http.NewRequest(http.MethodPost, "https://api.deepseek.com/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetOpenCodeSessionHeaderIfNeeded(other, "conv-1")
	if got := other.Header.Get(OpenCodeSessionHeader); got != "" {
		t.Fatalf("non-OpenCode header = %q, want empty", got)
	}

	lookalike, err := http.NewRequest(http.MethodPost, "https://opencode.ai.evil.example/zen/go/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetOpenCodeSessionHeaderIfNeeded(lookalike, "conv-1")
	if got := lookalike.Header.Get(OpenCodeSessionHeader); got != "" {
		t.Fatalf("lookalike header = %q, want empty", got)
	}
}

func TestSetOpenCodeSessionHeaderGeneratesIDWhenMissing(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodPost, "https://opencode.ai/zen/go/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetOpenCodeSessionHeaderIfNeeded(req, "")
	got := req.Header.Get(OpenCodeSessionHeader)
	if got == "" {
		t.Fatal("expected generated session id")
	}
	if strings.Count(got, "-") != 4 {
		t.Fatalf("generated id %q is not UUID-shaped", got)
	}
}

func TestSetOpenCodeSessionHeaderUsesContext(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequestWithContext(
		WithOpenCodeSessionID(context.Background(), "from-ctx"),
		http.MethodPost,
		"https://opencode.ai/zen/go/v1/chat/completions",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	ApplyOpenCodeSessionHeader(req, MaclawLLMConfig{})
	if got := req.Header.Get(OpenCodeSessionHeader); got != "from-ctx" {
		t.Fatalf("session header = %q, want from-ctx", got)
	}
}

func TestStableOpenCodeSessionIDIsDeterministic(t *testing.T) {
	t.Parallel()
	first := StableOpenCodeSessionID("hub-1", "tenant-1", "hello world")
	second := StableOpenCodeSessionID("hub-1", "tenant-1", "hello world")
	if first == "" || first != second {
		t.Fatalf("stable id not reused: %q vs %q", first, second)
	}
	other := StableOpenCodeSessionID("hub-1", "tenant-1", "other prompt")
	if other == first {
		t.Fatal("distinct conversation seeds produced the same session id")
	}
}

func TestOpenCodeSessionHeaderIfNeeded(t *testing.T) {
	t.Parallel()
	name, value, ok := OpenCodeSessionHeaderIfNeeded("https://opencode.ai/zen/go/v1", "abc")
	if !ok || name != OpenCodeSessionHeader || value != "abc" {
		t.Fatalf("OpenCodeSessionHeaderIfNeeded = %q %q %v", name, value, ok)
	}
	if _, _, ok := OpenCodeSessionHeaderIfNeeded("https://api.openai.com/v1", "abc"); ok {
		t.Fatal("non-OpenCode URL returned a session header")
	}
}

func TestBindOpenCodeSessionID(t *testing.T) {
	t.Parallel()
	ctx := WithOpenCodeSessionID(context.Background(), "bound")
	cfg := BindOpenCodeSessionID(ctx, MaclawLLMConfig{})
	if cfg.SessionID != "bound" {
		t.Fatalf("SessionID = %q, want bound", cfg.SessionID)
	}
	keep := BindOpenCodeSessionID(ctx, MaclawLLMConfig{SessionID: "explicit"})
	if keep.SessionID != "explicit" {
		t.Fatalf("explicit SessionID overwritten: %q", keep.SessionID)
	}
}

func TestOpenCodeConversationSeedFromMessagesAndInput(t *testing.T) {
	t.Parallel()
	if got := OpenCodeConversationSeedFromMessages([]any{map[string]any{"role": "user", "content": "hello from parts"}}); got != "hello from parts" {
		t.Fatalf("messages seed = %q", got)
	}
	if got := OpenCodeConversationSeedFromMessages([]interface{}{map[string]string{"role": "user", "content": "typed string map"}}); got != "typed string map" {
		t.Fatalf("map[string]string seed = %q", got)
	}
	if got := OpenCodeConversationSeed(map[string]any{
		"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "responses turn"}}}},
	}); got != "responses turn" {
		t.Fatalf("responses input seed = %q", got)
	}
}

func TestOpenCodeConversationSeedIgnoresGrowingHistoryWithoutUser(t *testing.T) {
	t.Parallel()
	first := OpenCodeConversationSeedFromMessages([]any{map[string]any{"role": "system", "content": "policy"}})
	second := OpenCodeConversationSeedFromMessages([]any{
		map[string]any{"role": "system", "content": "policy"},
		map[string]any{"role": "assistant", "content": "hello"},
	})
	if first != "" || second != "" {
		t.Fatalf("system/assistant-only history must not become a seed: %q %q", first, second)
	}
}

func TestTruncateOpenCodeSeedDoesNotSplitRunes(t *testing.T) {
	t.Parallel()
	seed := strings.Repeat("你", openCodeSeedByteLimit)
	got := truncateOpenCodeSeed(seed)
	if got == "" || !utf8.ValidString(got) {
		t.Fatalf("truncated seed is not valid UTF-8: %q", got)
	}
	if len(got) > openCodeSeedByteLimit {
		t.Fatalf("truncated seed length = %d", len(got))
	}
}

func TestApplyOpenCodeSessionHeaderOnHubManagedURL(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodPost, "https://hub.example/api/llm/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	ApplyOpenCodeSessionHeader(req, MaclawLLMConfig{
		URL:        "https://hub.mypapers.top/api/llm/v1",
		Model:      "auto",
		HubManaged: true,
		SessionID:  "hub-conv",
	})
	if got := req.Header.Get(OpenCodeSessionHeader); got != "hub-conv" {
		t.Fatalf("hub-managed session header = %q, want hub-conv", got)
	}
}

func TestOpenCodeSessionHeaderForConfig(t *testing.T) {
	t.Parallel()
	name, value, ok := OpenCodeSessionHeaderForConfig(MaclawLLMConfig{URL: "https://opencode.ai/zen/go/v1", SessionID: "abc"})
	if !ok || name != OpenCodeSessionHeader || value != "abc" {
		t.Fatalf("OpenCodeSessionHeaderForConfig = %q %q %v", name, value, ok)
	}
	if _, _, ok := OpenCodeSessionHeaderForConfig(MaclawLLMConfig{URL: "https://api.openai.com/v1", SessionID: "abc"}); ok {
		t.Fatal("non-OpenCode config returned a session header")
	}
}
