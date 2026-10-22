package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestDesktopErrorClassLoginWall(t *testing.T) {
	out := "navigated to https://zhuanlan.zhihu.com/write\n" + browser.LoginWallMarker + " blank: needs the person"
	if got := desktopErrorClass(out, nil); got != "login_wall" {
		t.Fatalf("class=%q", got)
	}
	if got := desktopErrorClass("", errors.New("desktop wait timed out after 90s")); got != "timeout" {
		t.Fatalf("class=%q", got)
	}
}

func TestDesktopLoginWallAsksTakeoverAtOnce(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u-wall", RunID: "run-wall"}
	owner := desktopOwner(scope)
	desktopRunFor(owner)
	out := "navigated" + browser.LoginWallMarker
	warn := desktopFailClassRecord(owner, "navigate", map[string]any{"action": "navigate", "url": "https://zhuanlan.zhihu.com/write"}, out, nil)
	if !strings.Contains(warn, "request_takeover") {
		t.Fatalf("warn=%q", warn)
	}
}

func TestDesktopStepHardTimeout(t *testing.T) {
	dir := t.TempDir()
	oldRoot := desktopEventsRoot
	desktopEventsRoot = dir
	defer func() { desktopEventsRoot = oldRoot }()
	old := operateDesktopFunc
	defer func() { operateDesktopFunc = old }()
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		select {
		case <-time.After(5 * time.Second):
			return "late", nil
		case <-ctx.Done():
			time.Sleep(2 * time.Second) // a call that ignores cancel must not hold the run
			return "", ctx.Err()
		}
	}
	t.Setenv(desktopStepTimeoutEnv, "0.2")
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u-to", RunID: "run-to-1"}
	start := time.Now()
	desktopEmitStepStart(scope, desktopOwner(scope), map[string]any{"action": "wait"}, "m")
	_, err := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "wait"})
	if err == nil || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("err=%v took=%s", err, time.Since(start))
	}
	if desktopErrorClass("", err) != "timeout" {
		t.Fatalf("class=%q", desktopErrorClass("", err))
	}
	desktopEmitStep(scope, desktopOwner(scope), map[string]any{"action": "wait"}, "m", "", err, 200*time.Millisecond, "")
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("files=%v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if !strings.Contains(string(raw), `"event":"step_start"`) || !strings.Contains(string(raw), `"result":"timeout"`) {
		t.Fatalf("events: %s", raw)
	}
	if s := desktopSummarizeRun(files[0]); s.byRes["step_start"] != 0 || s.byRes["timeout"] != 1 {
		t.Fatalf("summary=%v", s.byRes)
	}
	if desktopStepTimeout("bash") < 5*time.Minute {
		t.Fatal("bash should get the long timeout")
	}
}

func TestDesktopPendingStepStartInSummary(t *testing.T) {
	dir := t.TempDir()
	old := desktopEventsRoot
	desktopEventsRoot = dir
	defer func() { desktopEventsRoot = old }()
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u-pend", RunID: "run-pend"}
	desktopEmitStepStart(scope, desktopOwner(scope), map[string]any{"action": "probe"}, "m")
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*.jsonl"))
	if s := desktopSummarizeRun(files[0]); s.byRes["step_start"] != 1 {
		t.Fatalf("summary=%v", s.byRes)
	}
	raw, _ := os.ReadFile(files[0])
	if p := desktopEventPretty(strings.TrimSpace(string(raw))); !strings.Contains(p, "probe") || !strings.Contains(p, "started") {
		t.Fatalf("pretty=%q", p)
	}
}

func TestDesktopZhihuSeedExperience(t *testing.T) {
	desktopExperienceRoot = t.TempDir()
	defer func() { desktopExperienceRoot = "" }()
	dir := t.TempDir()
	old := desktopEventsRoot
	desktopEventsRoot = dir
	defer func() { desktopEventsRoot = old }()
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u-zh", RunID: "run-zh"}
	for _, site := range []string{"zhuanlan.zhihu.com", "www.zhihu.com", "zhihu.com"} {
		text := desktopExperienceText(scope, site)
		for _, want := range []string{"Site playbook for " + site, "导入文档", "ctrl+v", "text length", "发布", "request_takeover"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: missing %q in %q", site, want, text)
			}
		}
	}
	if desktopExperienceText(scope, "example.com") != "" {
		t.Fatal("no seed for example.com")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*.jsonl"))
	raw, _ := os.ReadFile(files[0])
	if strings.Count(string(raw), `"event":"experience_hit"`) != 3 {
		t.Fatalf("events: %s", raw)
	}
}
