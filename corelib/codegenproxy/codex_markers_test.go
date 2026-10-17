package codegenproxy

import (
	"strings"
	"testing"
)

func TestStripCodexClientMarkersRewritesSystemIdentityOnly(t *testing.T) {
	body := []byte(`{"model":"big-pickle","messages":[` +
		`{"role":"system","content":"You are a coding agent running in the Codex CLI, a terminal-based coding assistant. Codex CLI is an open source project led by OpenAI. You are expected to be precise, safe, and helpful.\n\nWithin this context, Codex refers to the open-source agentic coding interface (not the old Codex language model built by OpenAI).\n\n# How you work"},` +
		`{"role":"developer","content":[{"type":"text","text":"You are Codex, based on GPT-5.4. You are running as a coding agent in the Codex CLI on a user's computer."}]},` +
		`{"role":"user","content":"the file mentions Codex CLI"}]}`)

	got := string(StripCodexClientMarkers("https://opencode.ai/zen/v1/chat/completions", body))
	if strings.Contains(got, "You are Codex") || strings.Contains(got, "open-source agentic coding interface") || strings.Contains(got, "running in the Codex CLI") {
		t.Fatalf("identity remained: %s", got)
	}
	if !strings.Contains(got, "You are a coding agent running in the terminal.") || !strings.Contains(got, "# How you work") {
		t.Fatalf("operational prompt was dropped: %s", got)
	}
	if !strings.Contains(got, "You are a coding agent running on the user's computer.") {
		t.Fatalf("developer identity was not rewritten: %s", got)
	}
	if !strings.Contains(got, "the file mentions Codex CLI") {
		t.Fatalf("user text was rewritten: %s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("deleted sentence left a blank hole: %s", got)
	}
}

func TestStripCodexClientMarkersKeepsOfficialCodexHost(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are a coding agent running in the Codex CLI, a terminal-based coding assistant. Codex CLI is an open source project led by OpenAI."}]}`)
	for _, rawURL := range []string{
		"https://api.openai.com/v1/chat/completions",
		"https://chatgpt.com/backend-api/codex/responses",
	} {
		got := string(StripCodexClientMarkers(rawURL, body))
		if !strings.Contains(got, "Codex CLI") {
			t.Fatalf("official host %s lost the Codex prompt: %s", rawURL, got)
		}
	}
}

func TestStripCodexClientMarkersLeavesUnrelatedBodyUntouched(t *testing.T) {
	body := []byte(`{"model":"qwen","messages":[{"role":"user","content":"hello"}]}`)
	if string(StripCodexClientMarkers("https://www.workbuddy.ai/v2/chat/completions", body)) != string(body) {
		t.Fatal("unrelated body was rewritten")
	}
}

func TestStripCodexClientMarkersKeepsUserMentionAndLiteralJSON(t *testing.T) {
	mention := []byte(`{"temperature":0.2,"messages":[{"role":"user","content":"see the Codex CLI docs <file>"}]}`)
	if got := StripCodexClientMarkers("https://opencode.ai/zen/v1/chat/completions", mention); string(got) != string(mention) {
		t.Fatalf("user mention was re-encoded: %s", got)
	}

	body := []byte(`{"temperature":0.2,"messages":[{"role":"system","content":"You are Codex, based on GPT-5. You are running as a coding agent in the Codex CLI on a user's computer."},{"role":"user","content":"<file>"}]}`)
	got := string(StripCodexClientMarkers("https://example.com/v1/chat/completions", body))
	if strings.Contains(got, "You are Codex") || strings.Contains(got, `\u003c`) {
		t.Fatalf("rewrite escaped or kept the identity: %s", got)
	}
	if !strings.Contains(got, "<file>") || !strings.Contains(got, "0.2") {
		t.Fatalf("literal payload was dropped: %s", got)
	}
}
