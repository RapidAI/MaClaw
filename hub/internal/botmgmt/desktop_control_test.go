package botmgmt

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopControlIsOneMachinePerScreen(t *testing.T) {
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		Bots: []Bot{
			{ID: "bot_shared", Name: "shared", OwnerUserID: "alice"},
			{ID: "bot_other", Name: "other shared", OwnerUserID: "alice"},
			{ID: "bot_own", Name: "isolated", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeIsolated},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }

	granted, err := svc.ClaimDesktopControl(context.Background(), "tenant-a", "alice", "bot_shared", "gui-home")
	if err != nil || !granted {
		t.Fatalf("first claim = %v %v", granted, err)
	}
	granted, err = svc.ClaimDesktopControl(context.Background(), "tenant-a", "alice", "bot_shared", "gui-out")
	if err != nil || granted {
		t.Fatalf("second machine on the same bot = %v %v", granted, err)
	}
	granted, err = svc.ClaimDesktopControl(context.Background(), "tenant-a", "alice", "bot_other", "gui-out")
	if err != nil || granted {
		t.Fatalf("second machine on the other shared bot = %v %v", granted, err)
	}
	if !svc.DesktopInputGranted(context.Background(), "tenant-a", "alice", "bot_shared", "gui-home") {
		t.Fatal("holder lost the screen")
	}
	if svc.DesktopInputGranted(context.Background(), "tenant-a", "alice", "bot_other", "gui-out") {
		t.Fatal("the other machine was granted the shared screen")
	}
	granted, err = svc.ClaimDesktopControl(context.Background(), "tenant-a", "alice", "bot_own", "gui-out")
	if err != nil || !granted {
		t.Fatalf("isolated display = %v %v", granted, err)
	}
	empty, err := svc.ClaimDesktopControl(context.Background(), "tenant-a", "alice", "bot_own", "")
	if err != nil || empty {
		t.Fatalf("empty machine = %v %v", empty, err)
	}
	if err := svc.ReleaseDesktopControl(context.Background(), "tenant-a", "alice", "bot_shared", "gui-out"); err != nil {
		t.Fatal(err)
	}
	if !svc.DesktopInputGranted(context.Background(), "tenant-a", "alice", "bot_shared", "gui-home") {
		t.Fatal("another machine's release cleared the holder")
	}
	if err := svc.ReleaseDesktopControl(context.Background(), "tenant-a", "alice", "bot_shared", "gui-home"); err != nil {
		t.Fatal(err)
	}
	granted, err = svc.ClaimDesktopControl(context.Background(), "tenant-a", "alice", "bot_shared", "gui-out")
	if err != nil || !granted {
		t.Fatalf("claim after release = %v %v", granted, err)
	}
	granted, err = svc.ClaimDesktopControl(context.Background(), "tenant-a", "alice", "bot_other", "gui-out")
	if err != nil || !granted {
		t.Fatalf("same machine on the other shared bot = %v %v", granted, err)
	}
	if err := svc.ReleaseDesktopControl(context.Background(), "tenant-a", "alice", "bot_shared", "gui-out"); err != nil {
		t.Fatal(err)
	}
	if !svc.DesktopInputGranted(context.Background(), "tenant-a", "alice", "bot_other", "gui-out") {
		t.Fatal("a stale release of the previous shared bot dropped the screen")
	}

	now = now.Add(desktopControlTTL - time.Second)
	if !svc.DesktopInputGranted(context.Background(), "tenant-a", "alice", "bot_shared", "gui-out") {
		t.Fatal("a look dropped a live hold")
	}
	now = now.Add(2 * time.Second)
	if svc.DesktopInputGranted(context.Background(), "tenant-a", "alice", "bot_shared", "gui-out") {
		t.Fatal("looking refreshed the hold past its claim")
	}
	granted, err = svc.ClaimDesktopControl(context.Background(), "tenant-a", "alice", "bot_shared", "gui-home")
	if err != nil || !granted {
		t.Fatalf("claim after expiry = %v %v", granted, err)
	}
}
