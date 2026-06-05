package main

import (
	"strings"
	"testing"
)

func TestSummarizeLaunchArgsDoesNotExposeArgumentPayload(t *testing.T) {
	summary := summarizeLaunchArgs([]string{
		"-p",
		"Browser: SECRET_PROMPT",
		"--append-system-prompt",
		"Tool: SECRET_SYSTEM_PROMPT",
		"--model=gpt-test",
	})

	for _, leaked := range []string{"SECRET_PROMPT", "SECRET_SYSTEM_PROMPT", "Browser:", "Tool:", "gpt-test"} {
		if strings.Contains(summary, leaked) {
			t.Fatalf("launch arg summary leaked %q: %s", leaked, summary)
		}
	}
	for _, expected := range []string{"count=5", "flags=", "--append-system-prompt", "--model"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("launch arg summary missing %q: %s", expected, summary)
		}
	}
}
