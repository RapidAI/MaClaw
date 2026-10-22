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

func TestDesktopReadbackVerdictFallsThroughToThePage(t *testing.T) {
	const typed = "概率的本质是什么"
	got := desktopReadbackVerdict(typed, []string{"Save"}, func(s string) string {
		return browser.ReadbackVerdict(s, []string{typed})
	})
	if got != browser.ReadbackApplied {
		t.Fatalf("page confirm: %s", got)
	}
	got = desktopReadbackVerdict(typed, []string{typed}, func(string) string {
		return browser.ReadbackNotApplied
	})
	if got != browser.ReadbackNotApplied {
		t.Fatalf("focused page wins: %s", got)
	}
	got = desktopReadbackVerdict(typed, []string{typed}, func(string) string {
		return browser.ReadbackUnknown
	})
	if got != browser.ReadbackApplied {
		t.Fatalf("native hit: %s", got)
	}
	got = desktopReadbackVerdict(typed, []string{"notes.txt"}, func(string) string {
		return browser.ReadbackUnknown
	})
	if got != browser.ReadbackNotApplied {
		t.Fatalf("kept miss: %s", got)
	}
	got = desktopReadbackVerdict(typed, nil, func(string) string {
		return browser.ReadbackUnknown
	})
	if got != browser.ReadbackUnknown {
		t.Fatalf("unknown: %s", got)
	}
	got = desktopReadbackVerdict(typed, nil, func(string) string {
		return browser.ReadbackNotApplied
	})
	if got != browser.ReadbackNotApplied {
		t.Fatalf("page miss: %s", got)
	}
	got = desktopReadbackVerdict(typed, []string{"other"}, nil)
	if got != browser.ReadbackNotApplied {
		t.Fatalf("no page: %s", got)
	}
	got = desktopReadbackVerdict(typed, []string{typed, "notes.txt"}, func(string) string {
		return browser.ReadbackUnknown
	})
	if got != browser.ReadbackUnknown {
		t.Fatalf("split fields: %s", got)
	}
	got = desktopReadbackVerdict(typed, []string{typed, "prefix " + typed}, func(string) string {
		return browser.ReadbackUnknown
	})
	if got != browser.ReadbackApplied {
		t.Fatalf("every field: %s", got)
	}
}

func TestDesktopWithReadback(t *testing.T) {
	out, err := desktopWithReadback("type", "typed 5 chars", nil, browser.ReadbackNotApplied)
	if err != nil || !strings.Contains(out, desktopNotAppliedMarker) || !strings.Contains(out, "Ctrl+V") {
		t.Fatalf("not applied must be reported: %q %v", out, err)
	}
	if out, _ := desktopWithReadback("type", "ok", nil, browser.ReadbackUnknown); out != "ok" {
		t.Fatalf("unknown changed result: %q", out)
	}
	if out, _ := desktopWithReadback("type", "ok", nil, browser.ReadbackApplied); !strings.Contains(out, desktopAppliedMarker) {
		t.Fatalf("applied must be marked: %q", out)
	}
	if _, err := desktopWithReadback("type", "", errors.New("x"), browser.ReadbackNotApplied); err == nil {
		t.Fatal("error must pass through")
	}
	t.Setenv(desktopVerifyEnv, "off")
	if desktopVerifyEnabled() {
		t.Fatal("flag off")
	}
}
