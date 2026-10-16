package botmgmt

import (
	"testing"
	"time"
)

// TestAdmitPollBackoffLeavesShortRunsAlone is the reason the backoff is keyed
// off elapsed time and not the poll count: a run that finishes quickly must
// poll at the base interval the whole way, or finishing fast would start
// costing more than finishing slowly.
func TestAdmitPollBackoffLeavesShortRunsAlone(t *testing.T) {
	for _, elapsed := range []time.Duration{0, time.Second, 10 * time.Second, 29 * time.Second} {
		if got := admitPollBackoff(time.Second, elapsed); got != time.Second {
			t.Fatalf("elapsed=%v wait=%v, want the base 1s", elapsed, got)
		}
	}
}

func TestAdmitPollBackoffStretchesAsRunDragsOn(t *testing.T) {
	base := time.Second
	cases := []struct {
		elapsed time.Duration
		want    time.Duration
	}{
		{30 * time.Second, base},
		{49 * time.Second, base}, // one step short of the first doubling
		{50 * time.Second, 2 * base},
		{70 * time.Second, 4 * base},
		{90 * time.Second, 8 * base},
		{51 * time.Minute, 8 * base}, // capped
	}
	for _, c := range cases {
		if got := admitPollBackoff(base, c.elapsed); got != c.want {
			t.Fatalf("elapsed=%v wait=%v, want %v", c.elapsed, got, c.want)
		}
	}
}

func TestAdmitPollBackoffIsMonotonicAndBounded(t *testing.T) {
	base := time.Second
	previous := time.Duration(0)
	for elapsed := time.Duration(0); elapsed <= 6*time.Hour; elapsed += time.Second {
		wait := admitPollBackoff(base, elapsed)
		if wait < previous {
			t.Fatalf("wait shrank at elapsed %v: %v < %v", elapsed, wait, previous)
		}
		if wait > admitPollMaxWait {
			t.Fatalf("elapsed %v wait=%v exceeds the ceiling %v", elapsed, wait, admitPollMaxWait)
		}
		previous = wait
	}
}

// TestAdmitPollBackoffScalesWithConfiguredInterval keeps the ceiling relative
// to the base, so a test running with a 10ms interval does not inherit a
// production-length wait between polls.
func TestAdmitPollBackoffScalesWithConfiguredInterval(t *testing.T) {
	if got := admitPollBackoff(10*time.Millisecond, 51*time.Minute); got != 80*time.Millisecond {
		t.Fatalf("10ms base after 51min = %v, want 80ms", got)
	}
}

func TestAdmitPollBackoffGuardrails(t *testing.T) {
	// A zero or negative base would otherwise spin the loop with no sleep at
	// all, so it falls back to the documented default.
	if got := admitPollBackoff(0, time.Second); got != time.Second {
		t.Fatalf("zero base = %v, want 1s", got)
	}
	// An absurdly long configured interval must not push past the ceiling.
	if got := admitPollBackoff(time.Hour, time.Second); got != admitPollMaxWait {
		t.Fatalf("hour base = %v, want the ceiling %v", got, admitPollMaxWait)
	}
}
