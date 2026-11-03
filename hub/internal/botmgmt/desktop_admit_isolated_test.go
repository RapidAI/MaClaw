package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// TestIsolatedBotsDoNotBlockEachOther: a live admit on one isolated bot's
// own desktop must not make a sibling isolated bot busy, while two bots on
// the owner's shared desktop still exclude each other.
func TestIsolatedBotsDoNotBlockEachOther(t *testing.T) {
	settings := &memSettings{}
	iso := desktop.BotDesktopModeIsolated
	raw, _ := json.Marshal(record{Bots: []Bot{
		{ID: "bot_a", OwnerUserID: "alice", DesktopMode: iso},
		{ID: "bot_b", OwnerUserID: "alice", DesktopMode: iso},
		{ID: "bot_s1", OwnerUserID: "alice"},
		{ID: "bot_s2", OwnerUserID: "alice"},
	}})
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	live := func(bot string) {
		svc.mu.Lock()
		if svc.desktopAdmits == nil {
			svc.desktopAdmits = map[string]*desktopAdmitState{}
		}
		svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", bot, "run_"+bot)] = &desktopAdmitState{runID: "run_" + bot, botID: bot, userID: "alice"}
		svc.mu.Unlock()
	}
	live("bot_a")
	if _, err := svc.claimDesktopTurn("tenant-a", "alice", "bot_b", "k-b", "t", "execute"); err != nil {
		t.Fatalf("isolated sibling refused: %v", err)
	}
	live("bot_s1")
	if _, err := svc.claimDesktopTurn("tenant-a", "alice", "bot_s2", "k-s2", "t", "execute"); !errors.Is(err, ErrDesktopBusy) {
		t.Fatalf("shared desktop not busy: %v", err)
	}
}

// TestPrimaryNotBlockedByWorker: a live worker admit on the shared desktop
// must not refuse the account primary (router). Two non-primary bots on the
// same desktop still exclude each other.
func TestPrimaryNotBlockedByWorker(t *testing.T) {
	settings := &memSettings{}
	raw, _ := json.Marshal(record{Bots: []Bot{
		{ID: "bot_main", OwnerUserID: "alice"},
		{ID: "bot_w1", OwnerUserID: "alice"},
		{ID: "bot_w2", OwnerUserID: "alice"},
	}})
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.mu.Lock()
	if svc.desktopAdmits == nil {
		svc.desktopAdmits = map[string]*desktopAdmitState{}
	}
	svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", "bot_w1", "run_w1")] = &desktopAdmitState{runID: "run_w1", botID: "bot_w1", userID: "alice"}
	svc.mu.Unlock()
	if _, err := svc.claimDesktopTurn("tenant-a", "alice", "bot_main", "k-main", "新任务", ""); err != nil {
		t.Fatalf("primary refused by worker: %v", err)
	}
	if _, err := svc.claimDesktopTurn("tenant-a", "alice", "bot_w2", "k-w2", "t", "execute"); !errors.Is(err, ErrDesktopBusy) {
		t.Fatalf("worker sibling not busy: %v", err)
	}
}
