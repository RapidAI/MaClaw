package botmgmt

import (
	"fmt"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

func TestNodeOfflineMessageWaitsThenStops(t *testing.T) {
	svc := NewService(&memSettings{})
	svc.queueDrainOff = true
	svc.Now = func() time.Time { return time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC) }
	if !nodeLinkOffline(fmt.Errorf("maclawsrv request failed: %s", edgenode.ErrOffline)) {
		t.Fatal("a wrapped tunnel drop was treated as a normal failure")
	}
	if nodeLinkOffline(fmt.Errorf("maclawsrv request failed: connection refused")) {
		t.Fatal("a public srv failure was treated as a node drop")
	}
	pos, err := svc.enqueueDesktopMessage("tenant-a", "alice", "bot-1", desktopQueueItem{
		ClientMessageID: "m1",
		Content:         "继续",
		WaitKind:        desktopQueueWaitNode,
		WaitSince:       svc.now(),
	})
	if err != nil || pos != 1 {
		t.Fatalf("queue = %d %v", pos, err)
	}
	svc.expireNodeQueue("tenant-a", "alice", "bot-1")
	key := desktopQueueKey("tenant-a", "alice", "bot-1")
	if svc.desktopQueues[key][0].State == "gave_up" {
		t.Fatal("a fresh tunnel drop was given up immediately")
	}
	svc.Now = func() time.Time { return time.Date(2026, 11, 15, 0, 3, 0, 0, time.UTC) }
	svc.expireNodeQueue("tenant-a", "alice", "bot-1")
	item := svc.desktopQueues[key][0]
	if item.State != "gave_up" || item.Outcome != nodeOfflineOutcome {
		t.Fatalf("expired item = %+v", item)
	}
}
