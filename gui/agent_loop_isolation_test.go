package main

// agent_loop_isolation_test.go verifies that the per-loop config-template
// pattern introduced in the multi-tab concurrency fix is correct:
//
//  1. GoalAnchor.AnchorInterval() returns the interval stored in the template
//     and each loop gets a fresh GoalAnchor with its own userText.
//  2. NewAdaptiveRetryForLoop produces isolated instances — failure state in
//     one instance does not bleed into another.
//  3. prepareAgentLoopHarnessState produces a GoalAnchor whose OriginalGoal
//     matches the userText passed to it, not the handler-level template's text.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// GoalAnchor config-template contract
// ---------------------------------------------------------------------------

func TestGoalAnchorAnchorInterval_ReturnsStoredValue(t *testing.T) {
	for _, interval := range []int{1, 5, 10, 20} {
		g := NewGoalAnchor("some task", interval)
		if got := g.AnchorInterval(); got != interval {
			t.Errorf("interval=%d: AnchorInterval() = %d, want %d", interval, got, interval)
		}
	}
}

func TestGoalAnchorAnchorInterval_DefaultFallback(t *testing.T) {
	// interval <= 0 should be clamped to 5 inside NewGoalAnchor.
	g := NewGoalAnchor("task", 0)
	if got := g.AnchorInterval(); got != 5 {
		t.Errorf("zero interval: AnchorInterval() = %d, want 5", got)
	}
	g2 := NewGoalAnchor("task", -3)
	if got := g2.AnchorInterval(); got != 5 {
		t.Errorf("negative interval: AnchorInterval() = %d, want 5", got)
	}
}

// prepareAgentLoopHarnessState must create a fresh GoalAnchor for each loop
// whose OriginalGoal is the per-loop userText, not the template's text.
func TestPrepareAgentLoopHarnessState_GoalAnchorPerLoopUserText(t *testing.T) {
	h := &IMMessageHandler{}

	// Set a handler-level template with a custom interval.
	h.goalAnchor = NewGoalAnchor("template task — never used as goal", 7)

	s1 := h.prepareAgentLoopHarnessState("user-A", "开发一个贪吃蛇游戏")
	s2 := h.prepareAgentLoopHarnessState("user-B", "查询北京天气")

	if s1.GoalAnchor == s2.GoalAnchor {
		t.Fatal("both loops share the same GoalAnchor pointer; expected distinct instances")
	}
	if s1.GoalAnchor == h.goalAnchor {
		t.Fatal("loop GoalAnchor is the handler template; expected a fresh instance")
	}
	if s1.GoalAnchor.OriginalGoal() != "开发一个贪吃蛇游戏" {
		t.Errorf("loop-A goal = %q, want %q", s1.GoalAnchor.OriginalGoal(), "开发一个贪吃蛇游戏")
	}
	if s2.GoalAnchor.OriginalGoal() != "查询北京天气" {
		t.Errorf("loop-B goal = %q, want %q", s2.GoalAnchor.OriginalGoal(), "查询北京天气")
	}
	// Interval must come from the template.
	if s1.GoalAnchor.AnchorInterval() != 7 {
		t.Errorf("loop-A interval = %d, want 7 (from template)", s1.GoalAnchor.AnchorInterval())
	}
}

func TestPrepareAgentLoopHarnessState_NoHandlerTemplate_UsesDefault(t *testing.T) {
	h := &IMMessageHandler{} // no goalAnchor set
	s := h.prepareAgentLoopHarnessState("user-A", "some task")
	if s.GoalAnchor == nil {
		t.Fatal("GoalAnchor should not be nil even without a template")
	}
	if s.GoalAnchor.AnchorInterval() != 5 {
		t.Errorf("default interval = %d, want 5", s.GoalAnchor.AnchorInterval())
	}
}

func TestPrepareAgentLoopHarnessState_AdaptiveRetryAlwaysNil(t *testing.T) {
	// AdaptiveRetry must be nil in harnessState — it is populated later
	// by prepareAgentLoopRecorderBundle so each loop gets fresh mutable state.
	h := &IMMessageHandler{}
	h.adaptiveRetry = NewAdaptiveRetry(nil) // handler-level singleton
	s := h.prepareAgentLoopHarnessState("user-A", "task")
	if s.AdaptiveRetry != nil {
		t.Error("harnessState.AdaptiveRetry should be nil; it gets set in prepareAgentLoopRecorderBundle")
	}
}

func TestPrepareAgentLoopHarnessState_ProgressTrackerAlwaysNil(t *testing.T) {
	h := &IMMessageHandler{}
	h.harnessProgressTracker = &HarnessProgressTracker{} // hypothetically set
	s := h.prepareAgentLoopHarnessState("user-A", "task")
	if s.ProgressTracker != nil {
		t.Error("harnessState.ProgressTracker should be nil; sharing a mutable tracker across loops is wrong")
	}
}

// ---------------------------------------------------------------------------
// AdaptiveRetry per-loop isolation
// ---------------------------------------------------------------------------

func TestNewAdaptiveRetryForLoop_FreshMutableState(t *testing.T) {
	template := NewAdaptiveRetry(nil)
	// Simulate failures accumulated on the template (shouldn't happen in prod,
	// but validates isolation regardless).
	template.RecordFailure("bash", FailureNetwork, RetryDecision{Action: RetryActionRetry, Attempt: 1})

	loop1 := NewAdaptiveRetryForLoop(template, nil)
	loop2 := NewAdaptiveRetryForLoop(template, nil)

	// Both fresh instances should see no failures yet.
	if d := loop1.Decide("bash", FailureNetwork, 0); d.Action != RetryActionRetry {
		t.Errorf("loop1: expected retry on first failure, got %s", d.Action)
	}
	if d := loop2.Decide("bash", FailureNetwork, 0); d.Action != RetryActionRetry {
		t.Errorf("loop2: expected retry on first failure, got %s", d.Action)
	}

	// Exhaust loop1 by recording many failures.
	for i := 0; i < defaultMaxFailures+1; i++ {
		loop1.RecordFailure("bash", FailureNetwork, RetryDecision{Action: RetryActionRetry, Attempt: i})
	}
	d1 := loop1.Decide("bash", FailureNetwork, defaultMaxFailures+1)
	if d1.Action != RetryActionDisable {
		t.Errorf("loop1 exhausted: expected disable, got %s", d1.Action)
	}

	// loop2 must be unaffected — still retrying.
	d2 := loop2.Decide("bash", FailureNetwork, 0)
	if d2.Action == RetryActionDisable {
		t.Errorf("loop2 should be independent of loop1 exhaustion, got %s", d2.Action)
	}
}

func TestNewAdaptiveRetryForLoop_InheritsMaxFailures(t *testing.T) {
	template := NewAdaptiveRetry(nil)
	template.maxFailures = 2 // custom override

	loop := NewAdaptiveRetryForLoop(template, nil)
	if loop.MaxFailures() != 2 {
		t.Errorf("expected maxFailures=2 from template, got %d", loop.MaxFailures())
	}
}

func TestNewAdaptiveRetryForLoop_NilTemplate_UsesDefault(t *testing.T) {
	loop := NewAdaptiveRetryForLoop(nil, nil)
	if loop.MaxFailures() != defaultMaxFailures {
		t.Errorf("nil template: expected maxFailures=%d, got %d", defaultMaxFailures, loop.MaxFailures())
	}
}

func TestNewAdaptiveRetryForLoop_ZeroMaxFailures_UsesDefault(t *testing.T) {
	template := &AdaptiveRetry{maxFailures: 0} // zero is treated as "not set"
	loop := NewAdaptiveRetryForLoop(template, nil)
	if loop.MaxFailures() != defaultMaxFailures {
		t.Errorf("zero maxFailures: expected default %d, got %d", defaultMaxFailures, loop.MaxFailures())
	}
}
