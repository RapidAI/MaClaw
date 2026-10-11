package guiapp

import (
	"testing"
)

// resetActiveDesktopRuns clears the process-wide registry so one test cannot
// see another's turns.
func resetActiveDesktopRuns(t *testing.T) {
	t.Helper()
	activeDesktopRunsMu.Lock()
	activeDesktopRuns = make(map[string]*ActiveDesktopRun)
	activeDesktopRunsMu.Unlock()
}

// TestActiveDesktopRunLeavesNothingBehindAfterRealRunID is the regression for a
// leak that made supplements stop working after the first few tasks.
//
// finishDesktopBotTask defers its cleanup with the run id it had at defer time.
// Deferred arguments are evaluated where the defer statement sits, which is
// immediately after registration and before Hub has named the run, so the value
// captured is the empty placeholder. The deferred call then deleted the
// placeholder key, which setActiveDesktopRunID had already removed, and never
// the key the run actually lived under. Every admitted run stayed in the
// registry forever, and the UI then offered finished tasks as supplement
// targets.
func TestActiveDesktopRunLeavesNothingBehindAfterRealRunID(t *testing.T) {
	resetActiveDesktopRuns(t)
	a := &App{}

	run := a.registerActiveDesktopRun("bot-1", "req-1", "session-1", "查一下上周的订单", "execute")
	// This is the real ordering: the defer is armed here, before Hub answers,
	// and must read run.RunID when the function returns.
	func() {
		defer func() { unregisterActiveDesktopRun("bot-1", "req-1", run.RunID) }()
		a.setActiveDesktopRunID("bot-1", "req-1", "run-42")
		if got := a.GetActiveDesktopRuns("bot-1"); len(got) != 1 || got[0].RunID != "run-42" {
			t.Fatalf("run id was not adopted: %+v", got)
		}
	}()

	if left := a.GetActiveDesktopRuns("bot-1"); len(left) != 0 {
		t.Fatalf("a finished run stayed in the registry and will be offered as a supplement target: %+v", left)
	}
}

// TestDeferredCleanupSeesTheFinalRunID states the rule the regression above
// depends on: a bare defer captures the value it had when armed, so cleanup has
// to read run.RunID inside a closure.
func TestDeferredCleanupSeesTheFinalRunID(t *testing.T) {
	resetActiveDesktopRuns(t)
	a := &App{}

	run := a.registerActiveDesktopRun("bot-1", "req-1", "session-1", "任务", "execute")
	// By value, as a bare defer would: the placeholder is frozen at arm time.
	byValue := run.RunID
	a.setActiveDesktopRunID("bot-1", "req-1", "run-7")

	func() { unregisterActiveDesktopRun("bot-1", "req-1", byValue) }()
	if left := a.GetActiveDesktopRuns("bot-1"); len(left) != 1 {
		t.Fatalf("expected the by-value cleanup to miss the renamed key, got %d left", len(left))
	}

	// Through the pointer, as a closure reads it at return time.
	func() { unregisterActiveDesktopRun("bot-1", "req-1", run.RunID) }()
	if left := a.GetActiveDesktopRuns("bot-1"); len(left) != 0 {
		t.Fatalf("closure cleanup should have removed the entry, got %+v", left)
	}
}

// TestConcurrentTurnsKeepTheirOwnEntries covers the reason the registry is keyed
// by bot and run together: one bot can have several turns live, and one turn
// ending must not clear another.
func TestConcurrentTurnsKeepTheirOwnEntries(t *testing.T) {
	resetActiveDesktopRuns(t)
	a := &App{}

	first := a.registerActiveDesktopRun("bot-1", "req-1", "session-1", "任务一", "execute")
	a.setActiveDesktopRunID("bot-1", "req-1", "run-1")
	second := a.registerActiveDesktopRun("bot-1", "req-2", "session-1", "任务二", "execute")
	a.setActiveDesktopRunID("bot-1", "req-2", "run-2")

	unregisterActiveDesktopRun("bot-1", "req-1", first.RunID)
	left := a.GetActiveDesktopRuns("bot-1")
	if len(left) != 1 || left[0].RunID != "run-2" {
		t.Fatalf("ending one turn disturbed another: %+v", left)
	}

	unregisterActiveDesktopRun("bot-1", "req-2", second.RunID)
	if left := a.GetActiveDesktopRuns("bot-1"); len(left) != 0 {
		t.Fatalf("registry not empty after both turns ended: %+v", left)
	}
}

// TestPendingTurnIsNotOfferedAsTarget keeps a run that Hub has not named yet
// out of supplement routing. It has no run id, so nothing could be sent to it.
func TestPendingTurnIsNotOfferedAsTarget(t *testing.T) {
	resetActiveDesktopRuns(t)
	a := &App{}

	a.registerActiveDesktopRun("bot-1", "req-1", "session-1", "任务", "execute")
	runs := a.GetActiveDesktopRuns("bot-1")
	if len(runs) != 1 {
		t.Fatalf("expected the pending turn to be listed for the UI, got %d", len(runs))
	}
	if runs[0].RunID != "" {
		t.Fatalf("a pending turn should report no run id, got %q", runs[0].RunID)
	}
}

// TestGetActiveDesktopRunsHidesUnaddressedPlaceholders pins the fail-safe
// rule: a turn whose run id has not arrived yet has no delivery address, so it
// must not be offered as a supplement target. Offering one would either fail
// opaquely server-side or invite guessing, and both are worse than waiting for
// setActiveDesktopRunID to give the entry its real name.
func TestGetActiveDesktopRunsHidesUnaddressedPlaceholders(t *testing.T) {
	resetActiveDesktopRuns(t)
	a := &App{}

	a.registerActiveDesktopRun("bot-1", "req-1", "session-1", "查一下上周的订单", "execute")
	if got := a.GetActiveDesktopRuns("bot-1"); len(got) != 0 {
		t.Fatalf("a placeholder with no run id was offered as a supplement target: %+v", got)
	}

	a.setActiveDesktopRunID("bot-1", "req-1", "run-42")
	got := a.GetActiveDesktopRuns("bot-1")
	if len(got) != 1 || got[0].RunID != "run-42" {
		t.Fatalf("run id adoption broke: %+v", got)
	}
}

// TestGetActiveDesktopRunsKeepsRealRunsNextToPlaceholders makes sure the
// placeholder filter is not overeager: an admitted turn stays listed even
// while another turn of the same bot is still waiting for its run id.
func TestGetActiveDesktopRunsKeepsRealRunsNextToPlaceholders(t *testing.T) {
	resetActiveDesktopRuns(t)
	a := &App{}

	a.registerActiveDesktopRun("bot-1", "req-1", "session-1", "第一个任务", "execute")
	a.setActiveDesktopRunID("bot-1", "req-1", "run-1")
	a.registerActiveDesktopRun("bot-1", "req-2", "session-1", "第二个任务", "execute")

	got := a.GetActiveDesktopRuns("bot-1")
	if len(got) != 1 || got[0].RunID != "run-1" {
		t.Fatalf("the admitted run disappeared next to a placeholder: %+v", got)
	}
}
