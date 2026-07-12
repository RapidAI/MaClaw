package ws

import (
	"testing"
	"time"
)

func TestShouldLogMachineHeartbeatRateLimits(t *testing.T) {
	ctx := &ConnContext{}
	if !shouldLogMachineHeartbeat(ctx, 0, 30) {
		t.Fatal("first heartbeat should log")
	}
	if shouldLogMachineHeartbeat(ctx, 0, 30) {
		t.Fatal("immediate repeat should not log")
	}
	if !shouldLogMachineHeartbeat(ctx, 1, 30) {
		t.Fatal("session count change should log")
	}
	if !shouldLogMachineHeartbeat(ctx, 1, 10) {
		t.Fatal("interval change should log")
	}
	ctx.lastHBLogAt = time.Now().Add(-2 * time.Minute)
	if !shouldLogMachineHeartbeat(ctx, 1, 10) {
		t.Fatal("stale last log should log again")
	}
}
