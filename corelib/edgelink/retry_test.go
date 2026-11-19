package edgelink

import (
	"testing"
	"time"
)

func TestLinkRetryCapsAtThirtySeconds(t *testing.T) {
	wait := time.Duration(0)
	want := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}
	for i, step := range want {
		wait = nextLinkWait(wait, false)
		if wait != step {
			t.Fatalf("step %d = %s, want %s", i, wait, step)
		}
	}
	if again := nextLinkWait(30*time.Second, true); again != 2*time.Second {
		t.Fatalf("after a live link = %s", again)
	}
}
