package main

import "testing"

func TestNormalizeRemoteHeartbeatIntervalSecDefaultsToThirtySeconds(t *testing.T) {
	if got := normalizeRemoteHeartbeatIntervalSec(0); got != 30 {
		t.Fatalf("normalizeRemoteHeartbeatIntervalSec(0) = %d, want 30", got)
	}
	if got := normalizeRemoteHeartbeatIntervalSec(-1); got != 30 {
		t.Fatalf("normalizeRemoteHeartbeatIntervalSec(-1) = %d, want 30", got)
	}
	if got := normalizeRemoteHeartbeatIntervalSec(3); got != 5 {
		t.Fatalf("normalizeRemoteHeartbeatIntervalSec(3) = %d, want 5", got)
	}
	if got := normalizeRemoteHeartbeatIntervalSec(5); got != 5 {
		t.Fatalf("normalizeRemoteHeartbeatIntervalSec(5) = %d, want 5", got)
	}
	if got := normalizeRemoteHeartbeatIntervalSec(30); got != 30 {
		t.Fatalf("normalizeRemoteHeartbeatIntervalSec(30) = %d, want 30", got)
	}
}
