package httpapi

import "testing"

func TestOpenCodeCatalogBaseUsesZenForGoAndKeyless(t *testing.T) {
	if got := openCodeCatalogBase("https://opencode.ai/zen/go/v1", "OpenCode"); got != "https://opencode.ai/zen/v1" {
		t.Fatalf("go catalog = %s", got)
	}
	if got := openCodeCatalogBase("", "OpenCode"); got != "https://opencode.ai/zen/v1" {
		t.Fatalf("empty catalog = %s", got)
	}
	if got := openCodeCatalogBase("https://api.deepseek.com/v1", "OpenCode"); got != "https://api.deepseek.com/v1" {
		t.Fatalf("other host = %s", got)
	}
	if got := openCodeCatalogBase("", "Codex"); got != "" {
		t.Fatalf("codex empty = %q", got)
	}
}
