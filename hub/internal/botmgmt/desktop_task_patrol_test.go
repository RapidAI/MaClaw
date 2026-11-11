package botmgmt

import (
	"testing"
	"time"
)

func TestPatrolPicksStallAndNoReceipt(t *testing.T) {
	svc := NewService(&memSettings{})
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.Local)
	svc.Now = func() time.Time { return now }
	svc.ensureTaskPatrol()
	svc.mu.Lock()
	svc.desktopAdmits = map[string]*desktopAdmitState{
		desktopAdmitKey("tenant-a", "alice", "bot_a", "run_stall"): {
			runID: "run_stall", botID: "bot_a", userID: "alice", status: "running",
			started: now.Add(-30 * time.Minute), activity: "点击",
		},
		desktopAdmitKey("tenant-a", "alice", "bot_a", "run_ok"): {
			runID: "run_ok", botID: "bot_a", userID: "alice", status: "succeeded",
			started: now.Add(-5 * time.Minute), done: true, delivered: false,
			finished: now,
		},
	}
	svc.mu.Unlock()
	svc.patrolMu.Lock()
	svc.patrolSight = map[string]*patrolSight{
		"tenant-a\x00alice\x00run_stall": {fingerprint: "running|点击|||||", changed: now.Add(-patrolStallAfter - time.Minute), notified: map[string]bool{}},
		"tenant-a\x00alice\x00run_ok":    {fingerprint: "succeeded||||||", changed: now.Add(-1 * time.Minute), notified: map[string]bool{}},
	}
	svc.patrolMu.Unlock()
	svc.patrolDesktopTasksOnce()
	svc.patrolMu.Lock()
	stall := svc.patrolSight["tenant-a\x00alice\x00run_stall"]
	ok := svc.patrolSight["tenant-a\x00alice\x00run_ok"]
	if stall == nil || !stall.notified["stalled"] {
		svc.patrolMu.Unlock()
		t.Fatal("stall not notified")
	}
	if ok == nil || !ok.notified["no_receipt"] {
		svc.patrolMu.Unlock()
		t.Fatal("no_receipt not notified")
	}
	svc.patrolMu.Unlock()
	svc.patrolDesktopTasksOnce()
	svc.patrolMu.Lock()
	defer svc.patrolMu.Unlock()
	if len(svc.patrolSight["tenant-a\x00alice\x00run_stall"].notified) != 1 {
		t.Fatalf("spam: %#v", svc.patrolSight["tenant-a\x00alice\x00run_stall"].notified)
	}
}
