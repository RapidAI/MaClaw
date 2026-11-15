package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestNoneBotWakeDoesNotOpenTheDesktop(t *testing.T) {
	raw, err := json.Marshal(record{
		BaseURL:     "http://maclawsrv.example",
		AccessToken: "secret-token",
		Bots: []Bot{{
			ID: "bot_alice", Name: "duty", InstanceID: "inst_alice",
			OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeNone,
		}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example/vnc.html"}
	svc.Desktop = desk

	if _, _, err := svc.WakeDesktopView(context.Background(), "tenant-a", "alice", "bot_alice"); !errors.Is(err, ErrNoDesktop) {
		t.Fatalf("wake: %v", err)
	}
	if _, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice"); !errors.Is(err, ErrNoDesktop) {
		t.Fatalf("watch: %v", err)
	}
	if desk.open != 0 || desk.stop != 0 {
		t.Fatalf("desktop calls open=%d stop=%d", desk.open, desk.stop)
	}
}
