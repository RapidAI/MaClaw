package desktopd

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/desktopmcp"
)

func TestViewHoldTableExpiresAndCaps(t *testing.T) {
	now := time.Unix(1000, 0)
	tab := &viewHoldTable{until: map[string]time.Time{}, now: func() time.Time { return now }}
	tab.hold("k", float64(30))
	if !tab.active("k") || tab.active("other") || tab.active("") {
		t.Fatal("hold not active")
	}
	now = now.Add(31 * time.Second)
	if tab.active("k") {
		t.Fatal("hold must lapse")
	}
	tab.hold("k", 99999)
	now = now.Add(viewHoldMax - time.Second)
	if !tab.active("k") {
		t.Fatal("capped hold should still be active")
	}
	now = now.Add(2 * time.Second)
	if tab.active("k") {
		t.Fatal("hold beyond cap")
	}
	tab.hold("k", nil)
	tab.release("k")
	if tab.active("k") {
		t.Fatal("release")
	}
}

func TestMCPCannotSendViewHold(t *testing.T) {
	b := mcpBackend{}
	for _, op := range []string{desktop.CtlViewHold, desktop.CtlViewRelease} {
		if _, err := b.Ctl(context.Background(), desktopmcp.Identity{}, map[string]any{"op": op}); err == nil {
			t.Fatalf("%s accepted from a bot", op)
		}
	}
}
