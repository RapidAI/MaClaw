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

func TestDesktopStepLogInvalidActionNotVerbatim(t *testing.T) {
	line := desktopStepLogLine("u", map[string]any{"action": "bash<tool_sep:1>secret cmd"}, "", errors.New("x"), 0)
	if strings.Contains(line, "secret") || !strings.Contains(line, "action=invalid(") {
		t.Fatal(line)
	}
}

func TestRecoverDesktopLeakedArgs(t *testing.T) {
	raw := "bash<tool_sep:6124c78e>\n<arg_key:6124c78e>command</arg_key:6124c78e>\n<arg_value:6124c78e>cd /tmp/img && ls -lh</arg_value:6124c78e>"
	got, ok := recoverDesktopLeakedArgs(map[string]any{"action": raw})
	if !ok || got["action"] != "bash" || got["command"] != "cd /tmp/img && ls -lh" {
		t.Fatalf("%v %#v", ok, got)
	}
	// Unterminated value (seen live) still recovers.
	got, ok = recoverDesktopLeakedArgs(map[string]any{"action": "bash<tool_sep:1><arg_key:1>command</arg_key:1><arg_value:1>echo hi"})
	if !ok || got["command"] != "echo hi" {
		t.Fatalf("%#v", got)
	}
	if _, ok := recoverDesktopLeakedArgs(map[string]any{"action": "rm<tool_sep:1>"}); ok {
		t.Fatal("unknown action recovered")
	}
	if _, ok := recoverDesktopLeakedArgs(map[string]any{"action": "bash", "command": "ls"}); ok {
		t.Fatal("clean args touched")
	}
}

func TestRecoverDesktopLeakedDSMLArgs(t *testing.T) {
	raw := "bash\">\n<｜｜DSML｜｜ parameter name=\"command\" string=\"true\">cd /tmp && ls; echo /done</｜｜DSML｜｜ parameter>\n<｜DSML｜parameter name=\"timeout\" string=\"false\">30"
	got, ok := recoverDesktopLeakedArgs(map[string]any{"action": raw})
	if !ok || got["action"] != "bash" || got["command"] != "cd /tmp && ls; echo /done" || got["timeout"] != "30" {
		t.Fatalf("%v %#v", ok, got)
	}
	if _, ok := recoverDesktopLeakedArgs(map[string]any{"action": "rm\">\n<｜DSML｜parameter name=\"x\">y"}); ok {
		t.Fatal("unknown action recovered")
	}
}
