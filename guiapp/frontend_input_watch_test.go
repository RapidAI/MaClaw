package guiapp

import (
	"testing"
	"time"
)

func TestInputWatchDecisionSceneInvalidResetsOffenses(t *testing.T) {
	dead := 5 * time.Minute
	active := 10 * time.Second
	cases := []struct {
		name          string
		pageIdle      time.Duration
		sysIdle       time.Duration
		foregroundFor time.Duration
		supported     bool
	}{
		{"unsupported platform", dead, active, dead, false},
		// Transient focus gain: the user just switched back / clicked the
		// native title bar a few seconds ago. Must never nudge on its own.
		{"foreground tenure too short", dead, active, 30 * time.Second, true},
		{"foreground tenure at threshold", dead, active, inputDeadAfter, true},
		{"not foreground", dead, active, 0, true},
		{"page receiving input", 30 * time.Second, active, dead, true},
		{"user away from machine", dead, time.Hour, dead, true},
	}
	for _, c := range cases {
		act, n, r := inputWatchDecision(c.pageIdle, c.sysIdle, c.foregroundFor, c.supported, 2, 1, time.Minute)
		if act != inputWatchNone || n != 0 || r != 0 {
			t.Fatalf("%s: got action=%d offenses=%d reloads=%d, want none/0/0", c.name, act, n, r)
		}
	}
}

func TestInputWatchDeadEscalates(t *testing.T) {
	dead := 5 * time.Minute
	active := 10 * time.Second

	// 1st detection: nudge.
	act, n, r := inputWatchDecision(dead, active, dead, true, 0, 0, time.Hour)
	if act != inputWatchNudge || n != 1 || r != 0 {
		t.Fatalf("first: got action=%d offenses=%d reloads=%d, want nudge/1/0", act, n, r)
	}
	// 2nd detection too soon after nudge: hold (tick is shorter than the gap).
	act, n, r = inputWatchDecision(dead, active, dead, true, 1, 0, 30*time.Second)
	if act != inputWatchNone || n != 2 || r != 0 {
		t.Fatalf("hold: got action=%d offenses=%d reloads=%d, want none/2/0", act, n, r)
	}
	// Gap cleared: reload fires even though offenses has climbed past 2
	// (regression: the reload branch keyed on offense number was unreachable).
	act, n, r = inputWatchDecision(dead, active, dead, true, 3, 0, 2*time.Minute)
	if act != inputWatchReload || n != 4 || r != 1 {
		t.Fatalf("late reload: got action=%d offenses=%d reloads=%d, want reload/4/1", act, n, r)
	}
	// Reload must not repeat.
	act, n, r = inputWatchDecision(dead, active, dead, true, 4, 1, 5*time.Minute)
	if act != inputWatchNone || n != 5 || r != 1 {
		t.Fatalf("no-repeat: got action=%d offenses=%d reloads=%d, want none/5/1", act, n, r)
	}
	// Before escalate gap: hold.
	act, n, r = inputWatchDecision(dead, active, dead, true, 5, 1, time.Minute)
	if act != inputWatchNone || n != 6 || r != 1 {
		t.Fatalf("hold-3: got action=%d offenses=%d reloads=%d, want none/6/1", act, n, r)
	}
	// After escalate gap: escalate.
	act, n, r = inputWatchDecision(dead, active, dead, true, 6, 1, 11*time.Minute)
	if act != inputWatchEscalate || n != 7 || r != 1 {
		t.Fatalf("escalate: got action=%d offenses=%d reloads=%d, want escalate/7/1", act, n, r)
	}
}

func TestNoteFrontendInputEventMonotonicAndClamped(t *testing.T) {
	t.Cleanup(func() { frontendLastInputAt.Store(0) })
	frontendLastInputAt.Store(0)

	now := time.Now().UnixMilli()
	noteFrontendInputEvent(0) // bogus -> clamped to now
	if got := frontendLastInputAt.Load(); got == 0 {
		t.Fatal("zero timestamp not clamped to now")
	}
	noteFrontendInputEvent(now + 10_000) // future -> clamped, monotonic
	if got := frontendLastInputAt.Load(); got > now+5000 {
		t.Fatalf("future timestamp accepted: %d", got)
	}
	frontendLastInputAt.Store(now)
	noteFrontendInputEvent(now - 60_000) // older value must not regress
	if got := frontendLastInputAt.Load(); got != now {
		t.Fatalf("regressed to older ts: %d", got)
	}
	noteFrontendInputEvent(now + 1000) // newer value accepted
	if got := frontendLastInputAt.Load(); got != now+1000 {
		t.Fatalf("newer ts rejected: %d", got)
	}
}
