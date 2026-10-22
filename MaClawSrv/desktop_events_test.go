package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDesktopEventsStreamAndCLI(t *testing.T) {
	dir := t.TempDir()
	old := desktopEventsRoot
	desktopEventsRoot = dir
	defer func() { desktopEventsRoot = old }()
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u-events", InstanceID: "i", RunID: "run-events-1"}
	owner := desktopOwner(scope)
	desktopEmitStep(scope, owner, map[string]any{"action": "type", "text": "hunter2-secret-password", "cookie": "sid=abc"}, "openai/gpt-x", "ok", nil, 120*time.Millisecond, "")
	desktopEmitStep(scope, owner, map[string]any{"action": "click", "x": 3}, "openai/gpt-x", "", errors.New("element not found"), 50*time.Millisecond, "")
	desktopEmitStep(scope, owner, map[string]any{"action": "click"}, "openai/gpt-x", "", errors.New("blocked"), 0, "guard")
	desktopEmitNote(scope, "takeover_request", "human", "login")

	files, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("files=%v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if strings.Contains(string(raw), "hunter2") || strings.Contains(string(raw), "sid=abc") {
		t.Fatalf("secret leaked: %s", raw)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines=%d", len(lines))
	}
	var ev desktopEvent
	if err := json.Unmarshal([]byte(lines[1]), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Result != "element_not_found" || ev.RunID != "run-events-1" || ev.Model != "openai/gpt-x" {
		t.Fatalf("ev=%+v", ev)
	}
	_ = json.Unmarshal([]byte(lines[2]), &ev)
	if ev.Result != "guard" {
		t.Fatalf("guard ev=%+v", ev)
	}

	var buf bytes.Buffer
	if code := desktopRunsMain([]string{"list", "--root", dir}, &buf); code != 0 || !strings.Contains(buf.String(), "run-events-1") {
		t.Fatalf("list %d %s", code, buf.String())
	}
	buf.Reset()
	if code := desktopRunsMain([]string{"tail", "run-events-1", "--root", dir}, &buf); code != 0 || !strings.Contains(buf.String(), "takeover_request") {
		t.Fatalf("tail %d %s", code, buf.String())
	}
	buf.Reset()
	if code := desktopRunsMain([]string{"failures", "--root", dir}, &buf); code != 0 || !strings.Contains(buf.String(), "element_not_found click") {
		t.Fatalf("failures %d %s", code, buf.String())
	}
	buf.Reset()
	if code := desktopRunsMain([]string{"find", "--root", dir, "--result", "guard"}, &buf); code != 0 || strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("find %d %s", code, buf.String())
	}
}

func TestDesktopResultClass(t *testing.T) {
	cases := map[string]string{"": "ok"}
	_ = cases
	if got := desktopResultClass("ok", nil, ""); got != "ok" {
		t.Fatal(got)
	}
	if got := desktopResultClass("", errors.New("timeout waiting"), ""); got != "timeout" {
		t.Fatal(got)
	}
	if got := desktopResultClass("x", nil, "budget"); got != "refused" {
		t.Fatal(got)
	}
}
