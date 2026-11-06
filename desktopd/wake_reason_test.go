package desktopd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestAllowedWakeReason(t *testing.T) {
	if !AllowedWakeReason(WakeReasonBotDesktop) || !AllowedWakeReason(WakeReasonHumanWake) {
		t.Fatal("allowed reasons rejected")
	}
	if AllowedWakeReason("") || AllowedWakeReason("bash") || AllowedWakeReason("health") {
		t.Fatal("denied reason accepted")
	}
}

func inspectLine(t *testing.T, running bool, tenant, user string) string {
	t.Helper()
	um, err := desktop.UserMounts(tenant, user)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, 0, len(um))
	for _, m := range um {
		parts = append(parts, m.Volume+"="+m.Target)
	}
	run := "false"
	if running {
		run = "true"
	}
	return run + "|1g|" + strings.Join(parts, " ") + "|maclaw-gui:2|maclaw-gui:2|"
}

func TestEnsureWithoutReasonLeavesStoppedDesktop(t *testing.T) {
	var started bool
	svc := &Service{
		EnforceWakeReason: true,
		Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.HasPrefix(joined, "inspect ") {
				return inspectLine(t, false, "tenant", "alice"), nil
			}
			if strings.HasPrefix(joined, "start ") {
				started = true
				return "", nil
			}
			return "", nil
		},
	}
	err := svc.Ensure(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", Memory: "3g", CPUs: "1.5", ShmSize: "1g"})
	if !errors.Is(err, ErrWakeDenied) {
		t.Fatalf("want ErrWakeDenied, got %v", err)
	}
	if started {
		t.Fatal("docker start ran without wake reason")
	}
}

func TestEnsureWithHumanReasonStartsStoppedDesktop(t *testing.T) {
	var started bool
	svc := &Service{
		EnforceWakeReason: true,
		Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.HasPrefix(joined, "inspect ") {
				return inspectLine(t, false, "tenant", "alice"), nil
			}
			if strings.HasPrefix(joined, "start ") {
				started = true
				return "", nil
			}
			return "", nil
		},
	}
	if err := svc.Ensure(context.Background(), Spec{
		TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2",
		Memory: "3g", CPUs: "1.5", ShmSize: "1g",
		WakeReason: WakeReasonHumanWake,
	}); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("human-wake did not start")
	}
}

func TestEnsureRunningNeedsNoReason(t *testing.T) {
	var started bool
	svc := &Service{
		EnforceWakeReason: true,
		Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.HasPrefix(joined, "inspect ") {
				return inspectLine(t, true, "tenant", "alice"), nil
			}
			if strings.HasPrefix(joined, "start ") {
				started = true
			}
			return "", nil
		},
	}
	if err := svc.Ensure(context.Background(), Spec{
		TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2",
		Memory: "3g", CPUs: "1.5", ShmSize: "1g",
	}); err != nil {
		t.Fatal(err)
	}
	if started {
		t.Fatal("start called on already-running desktop")
	}
}
