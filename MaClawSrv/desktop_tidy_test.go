package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
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
