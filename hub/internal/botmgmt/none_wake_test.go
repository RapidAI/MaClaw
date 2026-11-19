package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNoneBotReplyLeavesTheOwnersKeyboard(t *testing.T) {
	raw, err := json.Marshal(record{
		BaseURL:     "http://maclawsrv.example",
		AccessToken: "secret-token",
		Bots: []Bot{
			{ID: "bot_shared", Name: "desk", InstanceID: "inst_shared", OwnerUserID: "alice"},
			{ID: "bot_none", Name: "duty", InstanceID: "inst_none", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeNone},
		},
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
	svc.noteDesktopHeld("tenant-a", "alice", "bot_shared")

	reply, err := svc.settleDesktopCommand("tenant-a", "alice", "bot_none", "inst_none", "http://dockerd.example/vnc.html", true, desktopCommandBody{
		Text:      "prose",
		Handoff:   true,
		Attention: "login",
		Metadata:  map[string]string{"send_to_user": "好了"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.SendToUser, "好了") || reply.TaskOutcome != "unverified" || reply.Handoff || reply.NovncURL != "" {
		t.Fatalf("reply = %+v", reply)
	}
	key := desktopViewKey("tenant-a", "alice")
	if got := svc.desktopHeld[key]; got != "bot_shared" {
		t.Fatalf("keyboard holder = %q", got)
	}
	if desk.open != 0 || desk.stop != 0 {
		t.Fatalf("desktop calls open=%d stop=%d", desk.open, desk.stop)
	}

	_, err = svc.settleDesktopCommand("tenant-a", "alice", "bot_none", "inst_none", "http://dockerd.example/vnc.html", true, desktopCommandBody{
		CallErr: errors.New("srv down"),
		Handoff: true,
	})
	if err == nil || err.Error() != "srv down" {
		t.Fatalf("call error = %v", err)
	}
	if got := svc.desktopHeld[key]; got != "bot_shared" {
		t.Fatalf("keyboard holder after failure = %q", got)
	}
	if desk.stop != 0 {
		t.Fatalf("failure stopped the desktop")
	}
}

func TestNoneBotHandoffDoesNotOpenTheDesktop(t *testing.T) {
	raw, err := json.Marshal(record{
		BaseURL:     "http://maclawsrv.example",
		AccessToken: "secret-token",
		Bots: []Bot{
			{ID: "bot_shared", Name: "desk", InstanceID: "inst_shared", OwnerUserID: "alice"},
			{ID: "bot_none", Name: "duty", InstanceID: "inst_none", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeNone},
		},
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
	svc.noteDesktopHeld("tenant-a", "alice", "bot_shared")

	url, opened, err := svc.openDesktop(context.Background(), "tenant-a", "alice", "inst_none", "bot_none")
	if err != nil || opened || url != "" {
		t.Fatalf("none open url=%q opened=%v err=%v", url, opened, err)
	}
	if desk.open != 0 {
		t.Fatalf("none handoff opened the desktop %d times", desk.open)
	}
	key := desktopViewKey("tenant-a", "alice")
	if got := svc.desktopHeld[key]; got != "bot_shared" {
		t.Fatalf("keyboard holder = %q", got)
	}

	url, opened, err = svc.openDesktop(context.Background(), "tenant-a", "alice", "inst_shared", "bot_shared")
	if err != nil || !opened || url == "" {
		t.Fatalf("shared open url=%q opened=%v err=%v", url, opened, err)
	}
	if desk.open != 1 {
		t.Fatalf("shared open count = %d", desk.open)
	}
}

func TestNoneBotDropsALeftoverKeyboardPin(t *testing.T) {
	raw, err := json.Marshal(record{
		BaseURL:     "http://maclawsrv.example",
		AccessToken: "secret-token",
		Bots: []Bot{
			{ID: "bot_shared", Name: "desk", InstanceID: "inst_shared", OwnerUserID: "alice"},
			{ID: "bot_none", Name: "duty", InstanceID: "inst_none", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeNone},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	key := desktopViewKey("tenant-a", "alice")
	svc.mu.Lock()
	svc.desktopHeld = map[string]string{key: "bot_none"}
	svc.desktopAwaiting = map[string]bool{key: true}
	svc.mu.Unlock()

	if svc.personHasBotKeyboard("tenant-a", "alice", "bot_none") {
		t.Fatal("a bot with no computer still reported the keyboard")
	}
	if got := svc.desktopHeld[key]; got != "" {
		t.Fatalf("leftover holder after the check = %q", got)
	}

	svc.mu.Lock()
	svc.desktopHeld = map[string]string{key: "bot_shared"}
	svc.desktopAwaiting = map[string]bool{key: true}
	svc.mu.Unlock()
	reply, err := svc.settleDesktopCommand("tenant-a", "alice", "bot_none", "inst_none", "", false, desktopCommandBody{
		Metadata: map[string]string{"send_to_user": "好了"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.SendToUser, "好了") || reply.TaskOutcome != "unverified" {
		t.Fatalf("reply = %+v", reply)
	}
	if got := svc.desktopHeld[key]; got != "bot_shared" {
		t.Fatalf("shared holder = %q", got)
	}
}

func TestNoneBotDoesNotParkOnTheDesktopQueue(t *testing.T) {
	fake := &lifeSrv{intake: `{"kind":"new","instruction":"","ack":"先记着。","pace":"direct"}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	svc.BotParallelRuns = 1
	svc.queueDrainOff = true
	if _, err := svc.SetBotDesktopMode(context.Background(), "tenant-a", "bot_alice", desktop.BotDesktopModeNone); err != nil {
		t.Fatal(err)
	}
	first, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "第一句", "execute", "turn-a")
	if err != nil || first.Queued || first.RunID == "" {
		t.Fatalf("first = %+v %v", first, err)
	}
	second, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "第二句", "execute", "turn-b")
	if !errors.Is(err, ErrNoDesktopTurn) {
		t.Fatalf("second = %+v %v", second, err)
	}
	if second.Queued {
		t.Fatal("a bot with no computer was parked on the desktop queue")
	}
	view := svc.DesktopQueueStatus("tenant-a", "alice", "bot_alice", "turn-b")
	if view.State != "missing" {
		t.Fatalf("queue view = %+v", view)
	}
	if len(fake.posted()) != 1 {
		t.Fatalf("posted = %v", fake.posted())
	}
	desk := svc.Desktop.(*desktopCounter)
	desk.mu.Lock()
	opened := desk.open
	desk.mu.Unlock()
	if opened != 0 {
		t.Fatalf("desktop opened %d times", opened)
	}
}
