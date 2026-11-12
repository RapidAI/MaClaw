package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func budgetStep(t *testing.T, scope agentruntime.Scope, out string) error {
	t.Helper()
	if err := desktopBudgetCheck(scope, "bash"); err != nil {
		return err
	}
	desktopRecordStep(context.Background(), scope, map[string]any{"action": "bash"}, out, nil, time.Millisecond)
	return nil
}

func TestDesktopRunBudgetGrowsWhileProgressing(t *testing.T) {
	t.Setenv(desktopTrajectoryEnv, "off")
	t.Setenv(desktopRunMaxActionsEnv, "20")
	t.Setenv(desktopRunExtendActionsEnv, "20")
	scope := agentruntime.Scope{TenantID: "t", UserID: "grow-user", RunID: "grow-run-" + t.Name()}
	// A training poll: every output shows a new step.
	for i := 0; i < 70; i++ {
		if err := budgetStep(t, scope, fmt.Sprintf("epoch 1 step %d/804 loss=0.3", i*10)); err != nil {
			t.Fatalf("progressing run refused at action %d: %v", i, err)
		}
	}
	if f := desktopBudgetLogFields(scope); !strings.Contains(f, "actions=70/80") {
		t.Fatalf("budget must have grown in chunks of 20, got %s", f)
	}
}

func TestDesktopRunBudgetStopsOnStallAndSaysWhere(t *testing.T) {
	t.Setenv(desktopTrajectoryEnv, "off")
	t.Setenv(desktopRunMaxActionsEnv, "20")
	t.Setenv(desktopRunExtendActionsEnv, "20")
	scope := agentruntime.Scope{TenantID: "t", UserID: "stall-user", RunID: "stall-run-" + t.Name()}
	for i := 0; i < 10; i++ {
		if err := budgetStep(t, scope, fmt.Sprintf("wrote part %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	for i := 0; i < 60 && err == nil; i++ {
		err = budgetStep(t, scope, "still waiting")
	}
	if err == nil || !strings.Contains(err.Error(), "lack of progress") || !strings.Contains(err.Error(), "last new result was at action 11") {
		t.Fatalf("a stalled run must stop and name the last new result, got %v", err)
	}
}

func TestDesktopRunBudgetFixedWhenExtendOff(t *testing.T) {
	t.Setenv(desktopTrajectoryEnv, "off")
	t.Setenv(desktopRunMaxActionsEnv, "5")
	t.Setenv(desktopRunExtendActionsEnv, "0")
	scope := agentruntime.Scope{TenantID: "t", UserID: "fixed-user", RunID: "fixed-run-" + t.Name()}
	var err error
	for i := 0; i < 6 && err == nil; i++ {
		err = budgetStep(t, scope, fmt.Sprintf("new %d", i))
	}
	if err == nil || !strings.Contains(err.Error(), "budget spent") {
		t.Fatalf("extension off keeps the fixed cap, got %v", err)
	}
}

func TestDesktopBudgetScreenActionsIgnoreCoordinates(t *testing.T) {
	st := &desktopRunState{}
	for i := 0; i < 5; i++ {
		st.actions++
		desktopBudgetNoteOutcomeLocked(st, "click", fmt.Sprintf("clicked at %d,%d", i, i), false)
	}
	if len(st.budget.novelAt) != 1 {
		t.Fatalf("clicks differing only in coordinates are one outcome, got %d", len(st.budget.novelAt))
	}
	st.actions++
	desktopBudgetNoteOutcomeLocked(st, "screenshot", "new picture", false)
	st.actions++
	desktopBudgetNoteOutcomeLocked(st, "bash", "boom", true)
	if len(st.budget.novelAt) != 1 {
		t.Fatal("screenshots and failures are not progress")
	}
}
