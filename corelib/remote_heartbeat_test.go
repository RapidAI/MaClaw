package corelib

import "testing"

func TestNormalizeRemoteHeartbeatIntervalSec(t *testing.T) {
	if got := NormalizeRemoteHeartbeatIntervalSec(0); got != DefaultRemoteHeartbeatSec {
		t.Fatalf("NormalizeRemoteHeartbeatIntervalSec(0) = %d, want %d", got, DefaultRemoteHeartbeatSec)
	}
	if got := NormalizeRemoteHeartbeatIntervalSec(-1); got != DefaultRemoteHeartbeatSec {
		t.Fatalf("NormalizeRemoteHeartbeatIntervalSec(-1) = %d, want %d", got, DefaultRemoteHeartbeatSec)
	}
	if got := NormalizeRemoteHeartbeatIntervalSec(3); got != MinRemoteHeartbeatSec {
		t.Fatalf("NormalizeRemoteHeartbeatIntervalSec(3) = %d, want %d", got, MinRemoteHeartbeatSec)
	}
	if got := NormalizeRemoteHeartbeatIntervalSec(5); got != 5 {
		t.Fatalf("NormalizeRemoteHeartbeatIntervalSec(5) = %d, want 5", got)
	}
	if got := NormalizeRemoteHeartbeatIntervalSec(30); got != 30 {
		t.Fatalf("NormalizeRemoteHeartbeatIntervalSec(30) = %d, want 30", got)
	}
}
