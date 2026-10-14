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
	if _, err := desktop.BashArgs(desktopContainerName(), desktopTidyScript); err != nil {
		t.Fatalf("tidy script rejected: %v", err)
	}
}

func TestDesktopTidySkipsTheSameFlowAndShowsTheBrowser(t *testing.T) {
	calls := 0
	old := desktopTidyBash
	desktopTidyBash = func(ctx context.Context, scope agentruntime.Scope, cmd string) (string, error) {
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
	if !strings.Contains(desktopTidyScript, "windowactivate") {
		t.Fatal("tidy leaves the screen empty")
	}
}
