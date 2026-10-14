package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDesktopStepLogLineRedactsFreeText(t *testing.T) {
	line := desktopStepLogLine("u1", map[string]any{"action": "app_run", "text": "my password 123", "program": "libreoffice", "url": "https://x.com/a?token=abc"}, "done\nmore", nil, 1500*time.Millisecond)
	for _, want := range []string{"action=app_run", "program=libreoffice", "text=<15>", "took=1.5s", "ok first=done", "url=https://x.com/a?…"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %s", want, line)
		}
	}
	if strings.Contains(line, "password") || strings.Contains(line, "abc") {
		t.Fatalf("leaked: %s", line)
	}
	if l := desktopStepLogLine("u1", map[string]any{"action": "click"}, "", errors.New("boom"), 0); !strings.Contains(l, "err=boom") {
		t.Fatal(l)
	}
}
