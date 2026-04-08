package freeproxy

import "testing"

func TestNormalizeChatContentForPrompt_ContentFallback(t *testing.T) {
	got := normalizeChatContentForPrompt([]interface{}{
		map[string]interface{}{"type": "text", "content": "hello"},
		map[string]interface{}{"type": "output_text", "text": "world"},
	})
	if got != "hello\nworld" {
		t.Fatalf("normalizeChatContentForPrompt() = %q, want %q", got, "hello\nworld")
	}
}

func TestNormalizeChatContentForPrompt_NilBecomesEmpty(t *testing.T) {
	if got := normalizeChatContentForPrompt(nil); got != "" {
		t.Fatalf("normalizeChatContentForPrompt(nil) = %q, want empty string", got)
	}
}
