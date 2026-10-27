package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopTidyOncePerRunExecuteOnly(t *testing.T) {
	calls := 0
	old := desktopTidyBash
	desktopTidyBash = func(ctx context.Context, scope agentruntime.Scope, cmd string) (string, error) {
		if cmd == desktopTabTidyScript {
			return `{"done": true, "closed": []}`, nil
		}
		calls++
		if strings.Contains(cmd, "windowclose") || strings.Contains(cmd, "kill") {
			t.Fatal("tidy must never close windows")
		}
		return "minimized 0x1\n", nil
	}
	defer func() { desktopTidyBash = old }()
	scope := agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run-tidy-1"}
	exec := context.WithValue(context.Background(), desktopPhaseKey{}, "execute")
	plan := context.WithValue(context.Background(), desktopPhaseKey{}, "plan")
	desktopTidyOnce(plan, scope, "screenshot")
	desktopTidyOnce(exec, scope, "deliver")
	desktopLastStep.Delete("t/u") // a fresh person, not a follow-up
	desktopTidyOnce(exec, scope, "app_open")
	desktopTidyOnce(exec, scope, "app_run")
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	t.Setenv("MACLAW_DESKTOP_TIDY", "off")
	desktopTidyOnce(exec, agentruntime.Scope{UserID: "u", RunID: "run-tidy-2"}, "app_open")
	if calls != 1 {
		t.Fatal("tidy ran while disabled")
	}
}

func TestDesktopTidyScriptPassesBashValidator(t *testing.T) {
	for _, script := range []string{desktopTidyScript, desktopTabTidyScript} {
		if _, err := desktop.BashArgs(desktopContainerName(), script); err != nil {
			t.Fatalf("tidy script rejected: %v", err)
		}
	}
}

func TestDesktopTidySkipsTheSameFlowAndShowsTheBrowser(t *testing.T) {
	calls := 0
	old := desktopTidyBash
	tabs := 0
	desktopTidyBash = func(ctx context.Context, scope agentruntime.Scope, cmd string) (string, error) {
		if cmd == desktopTabTidyScript {
			tabs++
			return `{"done": true, "closed": []}`, nil
		}
		calls++
		return "minimized 0x1\nfront 0x2\n", nil
	}
	defer func() { desktopTidyBash = old }()
	exec := context.WithValue(context.Background(), desktopPhaseKey{}, "execute")
	desktopLastStep.Delete("t/flow")
	desktopTidyOnce(exec, agentruntime.Scope{TenantID: "t", UserID: "flow", RunID: "r1"}, "app_open")
	desktopTidyOnce(exec, agentruntime.Scope{TenantID: "t", UserID: "flow", RunID: "r2"}, "app_open")
	if calls != 1 {
		t.Fatalf("follow-up run tidied again: calls=%d", calls)
	}
	// Stale login tabs are closed for every run, also in the same flow.
	if tabs != 2 {
		t.Fatalf("tab tidy calls=%d, want 2", tabs)
	}
	if !strings.Contains(desktopTidyScript, "windowactivate") {
		t.Fatal("tidy leaves the screen empty")
	}
}
