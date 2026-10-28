package botmgmt

import (
	"context"
	"testing"
	"time"
)

func TestPendingDeliveriesSkipHubStartedTurns(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.mu.Lock()
	svc.ensureDesktopHydrated("tenant-a")
	if svc.desktopAdmits == nil {
		svc.desktopAdmits = map[string]*desktopAdmitState{}
	}
	now := svc.now()
	for run, key := range map[string]string{"run_chat": "turn-1", "run_del": "xdel-note_1", "run_wake": "xwake-note_2"} {
		svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", "bot_alice", run)] = &desktopAdmitState{
			done: true, finished: now.Add(-time.Second), status: "succeeded", userID: "alice", botID: "bot_alice",
			runID: run, clientMessageID: key, mediaFetched: true, reply: Reply{Text: "done " + run},
		}
	}
	svc.mu.Unlock()
	pending, err := svc.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].RunID != "run_chat" {
		t.Fatalf("only the chat turn is a pending chat reply, got %#v", pending)
	}
}
