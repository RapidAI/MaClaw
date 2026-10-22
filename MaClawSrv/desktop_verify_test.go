package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestDesktopStepsInsertedText(t *testing.T) {
	raw := []any{
		map[string]any{"action": "type", "text": "first"},
		map[string]any{"action": "key", "key": "Return"},
		map[string]any{"action": "paste", "text": "正文第二段"},
	}
	if got := desktopStepsInsertedText(raw); got != "正文第二段" {
		t.Fatalf("got %q", got)
	}
	if got := desktopStepsInsertedText([]any{map[string]any{"action": "key", "key": "ctrl+v"}}); got != "" {
		t.Fatalf("clipboard paste has no known text, got %q", got)
	}
}

func TestDesktopWithReadback(t *testing.T) {
	out, err := desktopWithReadback("type", "typed 5 chars", nil, browser.ReadbackNotApplied)
	if err != nil || !strings.Contains(out, desktopNotAppliedMarker) || !strings.Contains(out, "Ctrl+V") {
		t.Fatalf("not applied must be reported: %q %v", out, err)
	}
	for _, v := range []string{browser.ReadbackApplied, browser.ReadbackUnknown} {
		if out, _ := desktopWithReadback("type", "ok", nil, v); out != "ok" {
			t.Fatalf("%s changed result: %q", v, out)
		}
	}
	if _, err := desktopWithReadback("type", "", errors.New("x"), browser.ReadbackNotApplied); err == nil {
		t.Fatal("error must pass through")
	}
	t.Setenv(desktopVerifyEnv, "off")
	if desktopVerifyEnabled() {
		t.Fatal("flag off")
	}
}
