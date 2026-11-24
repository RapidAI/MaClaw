package botmgmt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

// pinningDesktop records the computer each picture call was pinned to.
// "<cloud>" is an explicit empty pin (desktop-service assignment). An
// unpinned call would follow the user's current computer instead.
type pinningDesktop struct {
	desktopCounter
	nodes []string
	url   string
}

func (d *pinningDesktop) note(ctx context.Context) {
	id, ok := desktoppool.PinnedComputer(ctx)
	switch {
	case !ok:
		d.nodes = append(d.nodes, "<unpinned>")
	case id == "":
		d.nodes = append(d.nodes, "<cloud>")
	default:
		d.nodes = append(d.nodes, id)
	}
}

func (d *pinningDesktop) DesktopViewState(ctx context.Context, _, _ string) (string, string, error) {
	d.note(ctx)
	return DesktopStateRunning, d.url, nil
}

func (d *pinningDesktop) PeekDesktopView(ctx context.Context, tenantID, userID string) (string, error) {
	_, novnc, err := d.DesktopViewState(ctx, tenantID, userID)
	return novnc, err
}

func (d *pinningDesktop) Open(ctx context.Context, tenantID, userID string) (string, error) {
	d.note(ctx)
	return d.url, nil
}

func seedPinnedOwner(t *testing.T, bots []Bot) *Service {
	t.Helper()
	raw, err := json.Marshal(record{
		BaseURL:     "http://maclawsrv.example",
		AccessToken: "secret-token",
		OwnerNode:   map[string]string{"alice": "f8f54ad9offline"},
		Bots:        bots,
		Grants:      []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	return NewService(settings)
}

// An isolated cloud bot stores no node_id. The account owner_node pin must
// not send its picture or wake to that LAN node.
func TestCloudBotPictureIgnoresOwnerNodePin(t *testing.T) {
	svc := seedPinnedOwner(t, []Bot{{
		ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice",
		DesktopMode: desktop.BotDesktopModeIsolated,
	}})
	desk := &pinningDesktop{url: "http://dockerd.example/v1/desktops/tunnel/k/vnc/vnc.html"}
	svc.Desktop = desk
	ctx := context.Background()
	deskUser := desktop.BotDesktopUser("alice", "bot_alice")
	if !svc.noteDesktopUserView("tenant-a", deskUser, 0) {
		t.Fatal("note view")
	}
	if node, ok := svc.DesktopComputerNode(ctx, "tenant-a", "alice", "bot_alice"); !ok || node != "" {
		t.Fatalf("computer node=%q ok=%v, empty node_id must stay the cloud computer", node, ok)
	}
	if got := desktopComputerForBot(Bot{OwnerUserID: "alice"}); got != "" {
		t.Fatalf("desktop computer = %q, owner_node must not fill an empty bot node", got)
	}

	held, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice")
	if err != nil || held == "" {
		t.Fatalf("hold url=%q err=%v", held, err)
	}
	woke, _, err := svc.WakeDesktopView(ctx, "tenant-a", "alice", "bot_alice")
	if err != nil || woke == "" {
		t.Fatalf("wake url=%q err=%v", woke, err)
	}
	if len(desk.nodes) == 0 {
		t.Fatal("picture path never pinned a computer")
	}
	for _, node := range desk.nodes {
		if node != "<cloud>" {
			t.Fatalf("picture pinned %q, want the cloud computer not owner_node; all=%v", node, desk.nodes)
		}
	}
}

func TestBotNodeStillPinsItsOwnComputer(t *testing.T) {
	svc := seedPinnedOwner(t, []Bot{{
		ID: "bot_local", Name: "lab", InstanceID: "inst_local", OwnerUserID: "alice",
		NodeID: "node-local", DesktopMode: desktop.BotDesktopModeIsolated,
	}})
	if node, ok := svc.DesktopComputerNode(context.Background(), "tenant-a", "alice", "bot_local"); !ok || node != "node-local" {
		t.Fatalf("computer node=%q ok=%v", node, ok)
	}
	if got := desktopComputerForBot(Bot{NodeID: " node-local ", OwnerUserID: "alice"}); got != "node-local" {
		t.Fatalf("desktop computer = %q", got)
	}
}
