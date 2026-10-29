package botmgmt

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

type ctlDesktop struct {
	desktopCounter
	mu    sync.Mutex
	ops   []string
	users []string
}

func (d *ctlDesktop) Ctl(_ context.Context, _, userID string, request map[string]any) (map[string]any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	op, _ := request["op"].(string)
	d.ops = append(d.ops, op)
	d.users = append(d.users, userID)
	return map[string]any{"ok": true}, nil
}

func isolatedViewRecordJSON(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(record{
		BaseURL: "http://maclawsrv.example", AccessToken: "secret-token",
		Bots:   []Bot{{ID: "bot_iso", Name: "iso", InstanceID: "inst_iso", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeIsolated}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func newViewHoldService(t *testing.T) (*Service, *ctlDesktop) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-v"), isolatedViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &ctlDesktop{desktopCounter: desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}}
	svc.Desktop = desk
	desktopViewHoldMu.Lock()
	desktopViewHoldSent = map[string]time.Time{}
	desktopViewHoldMu.Unlock()
	return svc, desk
}

func TestHoldDesktopViewSendsViewHoldWhenDemoSkillOn(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "1")
	svc, desk := newViewHoldService(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, _, err := svc.HoldDesktopView(ctx, "tenant-v", "alice", "bot_iso"); err != nil {
			t.Fatal(err)
		}
	}
	if len(desk.ops) != 1 || desk.ops[0] != desktop.CtlViewHold || desk.users[0] != desktop.BotDesktopUser("alice", "bot_iso") {
		t.Fatalf("ops=%v users=%v (one throttled hold to the bot display)", desk.ops, desk.users)
	}
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-v", "alice", "bot_iso"); err != nil {
		t.Fatal(err)
	}
	if desk.ops[len(desk.ops)-1] != desktop.CtlViewRelease {
		t.Fatalf("ops=%v want release", desk.ops)
	}
}

func TestHoldDesktopViewNoViewHoldWhenDemoSkillOff(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "")
	svc, desk := newViewHoldService(t)
	if _, _, err := svc.HoldDesktopView(context.Background(), "tenant-v", "alice", "bot_iso"); err != nil {
		t.Fatal(err)
	}
	_ = svc.ReleaseUserDesktopView(context.Background(), "tenant-v", "alice", "bot_iso")
	if len(desk.ops) != 0 {
		t.Fatalf("flag off sent %v", desk.ops)
	}
}
