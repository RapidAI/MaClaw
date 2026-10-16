package corelib

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
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
	want := OpenCodeClientSessionID("conv-1")
	if got := req.Header.Get(OpenCodeSessionHeader); got != want {
		t.Fatalf("session header = %q, want %q", got, want)
	}

	SetOpenCodeSessionHeaderIfNeeded(req, "conv-2")
	if got := req.Header.Get(OpenCodeSessionHeader); got != want {
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
	if !isOfficialOpenCodeSessionID(got) {
		t.Fatalf("generated id %q is not an official OpenCode session", got)
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
	if got := req.Header.Get(OpenCodeSessionHeader); got != OpenCodeClientSessionID("from-ctx") {
		t.Fatalf("session header = %q, want the official id for from-ctx", got)
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
	if !ok || name != OpenCodeSessionHeader || value != OpenCodeClientSessionID("abc") {
		t.Fatalf("OpenCodeSessionHeaderIfNeeded = %q %q %v", name, value, ok)
	}
	official := OpenCodeClientSessionID("keep")
	if _, kept, ok := OpenCodeSessionHeaderIfNeeded("https://opencode.ai/zen/go/v1", official); !ok || kept != official {
		t.Fatalf("official session id rewritten: %q", kept)
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
	if !ok || name != OpenCodeSessionHeader || value != OpenCodeClientSessionID("abc") {
		t.Fatalf("OpenCodeSessionHeaderForConfig = %q %q %v", name, value, ok)
	}
	if _, _, ok := OpenCodeSessionHeaderForConfig(MaclawLLMConfig{URL: "https://api.openai.com/v1", SessionID: "abc"}); ok {
		t.Fatal("non-OpenCode config returned a session header")
	}
}

func TestApplyOpenCodeClientIdentityOnlyOnOpenCodeHost(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodPost, "https://opencode.ai/zen/go/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "opencode")
	ApplyOpenCodeSessionHeader(req, MaclawLLMConfig{URL: req.URL.String(), AgentType: "opencode", SessionID: "conv-7"})
	if got := req.Header.Get("User-Agent"); got != "opencode/"+OpenCodeClientVersion {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := req.Header.Get(OpenCodeSessionHeader); got != OpenCodeClientSessionID("conv-7") {
		t.Fatalf("session = %q", got)
	}
	if req.Header.Get("x-opencode-client") != "" || req.Header.Get("x-opencode-request") != "" {
		t.Fatal("unneeded OpenCode headers were added")
	}

	again, err := http.NewRequest(http.MethodPost, "https://opencode.ai/zen/go/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	again.Header.Set("User-Agent", "opencode/9.9.9")
	official := OpenCodeClientSessionID("keep")
	again.Header.Set(OpenCodeSessionHeader, official)
	ApplyOpenCodeSessionHeader(again, MaclawLLMConfig{URL: again.URL.String(), AgentType: "opencode"})
	if got := again.Header.Get("User-Agent"); got != "opencode/9.9.9" {
		t.Fatalf("custom User-Agent = %q", got)
	}
	if got := again.Header.Get(OpenCodeSessionHeader); got != official {
		t.Fatalf("official session rewritten: %q", got)
	}

	blank, err := http.NewRequest(http.MethodPost, "https://opencode.ai/zen/go/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	ApplyOpenCodeSessionHeader(blank, MaclawLLMConfig{URL: blank.URL.String(), AgentType: "OpenCode"})
	if got := blank.Header.Get("User-Agent"); got != "opencode/"+OpenCodeClientVersion {
		t.Fatalf("filled User-Agent = %q", got)
	}

	glm, err := http.NewRequest(http.MethodPost, "https://open.bigmodel.cn/api/paas/v4/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	glm.Header.Set("User-Agent", "opencode")
	ApplyOpenCodeSessionHeader(glm, MaclawLLMConfig{URL: glm.URL.String(), AgentType: "opencode", SessionID: "conv-7"})
	if got := glm.Header.Get("User-Agent"); got != "opencode" {
		t.Fatalf("GLM User-Agent = %q, want opencode", got)
	}
	if got := glm.Header.Get(OpenCodeSessionHeader); got != "" {
		t.Fatalf("GLM session header = %q, want empty", got)
	}
}

func TestOpenCodeClientIdentityMatchesOfficialClient(t *testing.T) {
	t.Parallel()
	if got := OpenCodeClientUserAgent("opencode"); got != "opencode/"+OpenCodeClientVersion {
		t.Fatalf("preset user agent = %q", got)
	}
	if got := OpenCodeClientUserAgent("OpenCode"); got != "opencode/"+OpenCodeClientVersion {
		t.Fatalf("OpenCode user agent = %q", got)
	}
	if got := OpenCodeClientUserAgent("opencode/9.9.9"); got != "opencode/9.9.9" {
		t.Fatalf("custom version = %q", got)
	}
	if got := OpenCodeClientUserAgent("Codex"); got != "Codex" {
		t.Fatalf("other identity = %q", got)
	}
	saved := MaclawLLMConfig{URL: "https://opencode.ai/zen/go/v1", AgentType: "opencode"}
	if got := saved.UserAgent(); got != "opencode" {
		t.Fatalf("saved UserAgent = %q, want opencode", got)
	}
	if got := OpenCodeWireUserAgent(saved.URL, saved.UserAgent()); got != "opencode/"+OpenCodeClientVersion {
		t.Fatalf("wire UserAgent = %q", got)
	}
	if got := (MaclawLLMConfig{URL: "https://opencode.ai/zen/go/v1", AgentType: "OpenCode"}).UserAgent(); got != "OpenCode" {
		t.Fatalf("saved OpenCode UserAgent = %q", got)
	}
	if got := (MaclawLLMConfig{AgentType: "opencode"}).UserAgent(); got != "opencode" {
		t.Fatalf("saved UserAgent = %q, want opencode", got)
	}
	if got := (MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", AgentType: "opencode"}).UserAgent(); got != "opencode" {
		t.Fatalf("GLM UserAgent = %q, want opencode", got)
	}
	if !IsGLMCodingPlanUserAgent((MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/paas/v4", AgentType: "opencode"}).UserAgent()) {
		t.Fatal("GLM exact opencode match was rewritten")
	}

	now := time.Date(2026, 10, 15, 12, 34, 56, 0, time.UTC)
	id := openCodeClientSessionIDAt(now, "codexproxy", "hello")
	if !regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`).MatchString(id) {
		t.Fatalf("session id = %q", id)
	}
	if id != openCodeClientSessionIDAt(now, "codexproxy", "hello") {
		t.Fatal("session id changed for the same conversation")
	}
	if id == openCodeClientSessionIDAt(now, "codexproxy", "other") {
		t.Fatal("session id ignored the conversation")
	}
	encoded, err := strconv.ParseUint(id[4:16], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	gotMS := int64(encoded / 0x1000)
	wantMS := now.UnixMilli() & ((int64(1) << 36) - 1)
	if gotMS != wantMS {
		t.Fatalf("session timestamp = %d, want %d", gotMS, wantMS)
	}
}
