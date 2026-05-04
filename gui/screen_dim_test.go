package main

import (
	"testing"
	"time"
)

func TestScreenDimStateDimsOnceUntilUserActivityAfterGrace(t *testing.T) {
	timeout := time.Minute
	now := time.Unix(1000, 0)
	state := screenDimState{}

	if state.tick(30*time.Second, timeout, now) {
		t.Fatal("should not dim before timeout")
	}
	if !state.tick(timeout, timeout, now.Add(time.Second)) {
		t.Fatal("should dim exactly when idle reaches timeout")
	}
	if state.tick(timeout+30*time.Second, timeout, now.Add(2*time.Second)) {
		t.Fatal("should not repeatedly dim while already dimmed")
	}
	if state.tick(0, timeout, now.Add(3*time.Second)) {
		t.Fatal("should ignore transient idle reset during post-dim grace period")
	}
	if state.tick(2*time.Second, timeout, now.Add(screenDimActivityGrace+3*time.Second)) {
		t.Fatal("user activity should only re-arm, not dim immediately")
	}
	if !state.tick(timeout, timeout, now.Add(screenDimActivityGrace+4*time.Second)) {
		t.Fatal("should dim again after late real activity and another full timeout")
	}
}

func TestScreenDimStateIgnoresTransientIdleDropAfterGrace(t *testing.T) {
	timeout := time.Minute
	now := time.Unix(1500, 0)
	state := screenDimState{}

	if !state.tick(timeout, timeout, now) {
		t.Fatal("should dim from a fresh state")
	}
	if state.tick(0, timeout, now.Add(time.Second)) {
		t.Fatal("transient drop during grace should not immediately re-dim")
	}
	if state.tick(2*time.Minute, timeout, now.Add(screenDimActivityGrace+2*time.Second)) {
		t.Fatal("transient drop should be ignored once idle is still beyond timeout")
	}
	if state.tick(3*time.Minute, timeout, now.Add(screenDimActivityGrace+3*time.Second)) {
		t.Fatal("should stay dimmed after ignored transient activity")
	}
}

func TestScreenDimStateIgnoresTransientIdleDropOnNextPoll(t *testing.T) {
	timeout := time.Minute
	now := time.Unix(1750, 0)
	state := screenDimState{}

	if !state.tick(timeout, timeout, now) {
		t.Fatal("should dim from a fresh state")
	}
	if state.tick(0, timeout, now.Add(screenDimPollInterval)) {
		t.Fatal("transient idle drop on the next poll should not re-dim")
	}
	if state.tick(2*time.Minute, timeout, now.Add(screenDimActivityGrace+time.Second)) {
		t.Fatal("next-poll transient drop should be ignored when idle recovers past timeout")
	}
}

func TestScreenDimStateClearsPendingActivityBetweenDimCycles(t *testing.T) {
	timeout := time.Minute
	now := time.Unix(2000, 0)
	state := screenDimState{}

	if !state.tick(timeout, timeout, now) {
		t.Fatal("should dim from a fresh state")
	}
	if state.tick(0, timeout, now.Add(time.Second)) {
		t.Fatal("early activity should not immediately re-dim")
	}
	if state.tick(2*time.Second, timeout, now.Add(screenDimActivityGrace+time.Second)) {
		t.Fatal("pending real activity should only re-arm after grace")
	}
	if !state.tick(timeout, timeout, now.Add(screenDimActivityGrace+2*time.Second)) {
		t.Fatal("should dim again after re-arming")
	}
	if state.tick(2*time.Minute, timeout, now.Add(2*screenDimActivityGrace)) {
		t.Fatal("stale pending activity must not re-arm a new dim cycle")
	}
}
