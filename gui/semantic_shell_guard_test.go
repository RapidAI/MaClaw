package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestTrustedShellAppliesEveryLocalShellGuard pins the managed shell adapter to
// the same guard set every other local shell path in the tree applies.
//
// This matters because the adapter replaces the legacy bash tool on managed
// turns. If it guards less than the tool it replaces, migrating a shell family
// to the managed path widens what the model can do instead of narrowing it, and
// a shell grant starts carrying capabilities the plan never selected.
//
// Each case asserts the adapter returns the guard's own rejection text, so the
// test fails if the adapter grows a private lookalike check instead of using
// the shared guard.
func TestTrustedShellAppliesEveryLocalShellGuard(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		guard   func(string) (string, bool)
	}{
		{"remote host hop", "ssh user@host uptime", tool.RejectRawSSHCommand},
		{"whole browser process tree", "taskkill /im chrome.exe", tool.RejectBroadBrowserKillCommand},
		{"authenticated side effect", `curl -X POST https://example.com/publish -H "cookie: a=b"`, tool.RejectBrowserSideEffectHTTPCommand},
		{"second browser control plane", "npx playwright screenshot https://example.com out.png", tool.RejectShellBrowserAutomationCommand},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, tripped := tc.guard(tc.command)
			if !tripped {
				t.Fatalf("probe command no longer trips its own guard: %q", tc.command)
			}
			executed := 0
			h := &IMMessageHandler{}
			h.semanticTrustedShell = func(string, string, time.Duration) (string, error) {
				executed++
				return "ran", nil
			}
			out, err := h.executeTrustedShell("user-1", tc.command, time.Second)
			if err == nil || err.Error() != want {
				t.Fatalf("out=%q err=%v, want rejection %q", out, err, want)
			}
			if executed != 0 {
				t.Fatal("command reached the executor before the guard rejected it")
			}
		})
	}
}

// TestTrustedShellStillRunsAnOrdinaryCommand is the positive control for the
// case above. Without it, a change that rejected every command would satisfy
// all four rejection assertions.
func TestTrustedShellStillRunsAnOrdinaryCommand(t *testing.T) {
	var got string
	h := &IMMessageHandler{}
	h.semanticTrustedShell = func(_, command string, _ time.Duration) (string, error) {
		got = command
		return "hi", nil
	}
	out, err := h.executeTrustedShell("user-1", "echo hi", time.Second)
	if err != nil || out != "hi" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got != "echo hi" {
		t.Fatalf("executor received %q, want the command unchanged", got)
	}
}
