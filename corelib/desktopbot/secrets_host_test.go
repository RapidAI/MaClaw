package desktopbot

import (
	"strings"
	"testing"
)

func TestCaptureGitLabHostKeepsTheTokenPattern(t *testing.T) {
	text, found := CaptureInlineSecrets("GITHUB_TOKEN=ghp_abc123 和 GITLAB_HOST=https://gitlab.example.com/extra")
	if found["GITHUB_TOKEN"] != "ghp_abc123" || found["GITLAB_HOST"] != "https://gitlab.example.com/extra" {
		t.Fatalf("found %#v", found)
	}
	if strings.Contains(text, "ghp_abc123") || strings.Contains(text, "gitlab.example.com/extra") {
		t.Fatalf("value stayed in the text: %s", text)
	}
	if _, ok := found["SSH_PASSWORD"]; ok {
		t.Fatal("a named token was stored as the ssh password")
	}
}
