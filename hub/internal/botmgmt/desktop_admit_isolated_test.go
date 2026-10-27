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
