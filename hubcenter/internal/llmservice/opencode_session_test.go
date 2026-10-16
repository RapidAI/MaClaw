package llmservice

import (
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestOpenCodeSessionIDForProxyPrefersIncomingHeader(t *testing.T) {
	header := make(http.Header)
	header.Set(corelib.OpenCodeSessionHeader, "client-session")
	got := openCodeSessionIDForProxy(&ProxyRequest{
		HubID:    "hub",
		TenantID: "tenant",
		Header:   header,
		Body:     map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	if got != "client-session" {
		t.Fatalf("session id = %q, want client-session", got)
	}
}

func TestOpenCodeSessionIDForProxyStableAcrossTurns(t *testing.T) {
	firstBody := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "fix the login bug"}},
	}
	secondBody := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "fix the login bug"},
			map[string]any{"role": "assistant", "content": "looking"},
			map[string]any{"role": "user", "content": "also add tests"},
		},
	}
	first := openCodeSessionIDForProxy(&ProxyRequest{HubID: "hub", TenantID: "tenant", Body: firstBody})
	second := openCodeSessionIDForProxy(&ProxyRequest{HubID: "hub", TenantID: "tenant", Body: secondBody})
	if first == "" || first != second {
		t.Fatalf("session id not stable across turns: %q vs %q", first, second)
	}
	other := openCodeSessionIDForProxy(&ProxyRequest{
		HubID:    "hub",
		TenantID: "tenant",
		Body:     map[string]any{"messages": []any{map[string]any{"role": "user", "content": "write a poem"}}},
	})
	if other == first {
		t.Fatal("distinct conversations reused the same OpenCode session id")
	}
}

func TestOpenCodeSessionIDForProxyUsesResponsesInput(t *testing.T) {
	got := openCodeSessionIDForProxy(&ProxyRequest{
		HubID:    "hub",
		TenantID: "tenant",
		Body: map[string]any{
			"input": []any{map[string]any{"role": "user", "content": "responses turn"}},
		},
	})
	want := corelib.StableOpenCodeSessionID("hub", "tenant", "responses turn")
	if got != want {
		t.Fatalf("session id = %q, want %q", got, want)
	}
}
