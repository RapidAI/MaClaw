package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestRunAdmissionLeavesTheDesktopAsleepUntilAStepNeedsIt(t *testing.T) {
	opened := 0
	var during string
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		opened++
		during = loadDesktopActivity("run_lazy")
		return desktopEndpoint{CDP: "http://x:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() {
		desktopRemoteSession, desktopRemoteStop = nil, nil
		desktopRoundWakes.Delete("run_lazy")
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopRunsMu.Unlock()
	})
	// The label shows only for a slow wake; zero shows it at once.
	oldAfter := desktopWakeLabelAfter
	desktopWakeLabelAfter = 0
	t.Cleanup(func() { desktopWakeLabelAfter = oldAfter })
	release := occupyUserDesktop(context.Background(), "tenant", "lazy", "")
	defer release()
	if opened != 0 {
		t.Fatalf("admission woke the desktop: %d", opened)
	}
	scope := agentruntime.Scope{TenantID: "tenant", UserID: "lazy", RunID: "run_lazy"}
	if _, ok := desktopRoundAwake(scope); ok {
		t.Fatal("awake before any step")
	}
	for i := 0; i < 2; i++ {
		if _, err := ensureDesktopRound(context.Background(), scope, "", true); err != nil {
			t.Fatal(err)
		}
	}
	if opened != 1 || during != "正在唤醒电脑" {
		t.Fatalf("opened=%d during=%q", opened, during)
	}
	if _, ok := desktopRoundAwake(scope); !ok {
		t.Fatal("woken session not reused")
	}
}

func TestDesktopWakeFailureIsWordedForThePerson(t *testing.T) {
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{}, errors.New("pool exhausted")
	}
	t.Cleanup(func() { desktopRemoteSession = nil; desktopRoundWakes.Delete("run_wf") })
	_, err := ensureDesktopRound(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "u", RunID: "run_wf"}, "", true)
	var we desktopWakeError
	if !errors.As(err, &we) || !strings.Contains(err.Error(), "电脑没能唤醒") {
		t.Fatalf("err=%v", err)
	}
}
