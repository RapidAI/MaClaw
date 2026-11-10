package botmgmt

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// peekDesktop is a desktop that already has isolated screens running. Peek
// returns that bot's own noVNC page and never starts anything.
type peekDesktop struct {
	activityDesktop
	mu        sync.Mutex
	views     map[string]string
	peekUsers []string
}

func (d *peekDesktop) PeekDesktopView(_ context.Context, tenantID, userID string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.peekUsers = append(d.peekUsers, userID)
	if d.views == nil {
		return "", nil
	}
	return d.views[tenantID+"/"+userID], nil
}

func isolatedPairRecordJSON(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(record{
		BaseURL:     "http://maclawsrv.example",
		AccessToken: "secret-token",
		Bots: []Bot{
			{ID: "bot_1", Name: "one", InstanceID: "inst_1", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeIsolated},
			{ID: "bot_2", Name: "two", InstanceID: "inst_2", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeIsolated},
		},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func newIsolatedPeekService(t *testing.T) (*Service, *peekDesktop, *time.Time) {
	t.Helper()
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), isolatedPairRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	now := time.Now()
	svc.Now = func() time.Time { return now }
	u1 := desktop.BotDesktopUser("alice", "bot_1")
	u2 := desktop.BotDesktopUser("alice", "bot_2")
	desk := &peekDesktop{
		views: map[string]string{
			"tenant-a/" + u1: "https://desktop:tokenbot1@dockerd.example/v1/desktops/tunnel/owner/vnc/vnc.html?autoconnect=1&resize=scale",
			"tenant-a/" + u2: "https://desktop:tokenbot2@dockerd.example/v1/desktops/tunnel/owner/vnc/vnc.html?autoconnect=1&resize=scale",
		},
	}
	svc.Desktop = desk
	return svc, desk, &now
}

// An isolated bot that drove the screen through screenshot/ctl (no session
// step, so no stored noVNC page) still shows its own live picture.
func TestHoldDesktopViewIsolatedBotWithoutSessionShowsOwnLiveURL(t *testing.T) {
	svc, desk, now := newIsolatedPeekService(t)
	u2 := desktop.BotDesktopUser("alice", "bot_2")
	desk.used("tenant-a", u2, *now)
	// activityDesktop.novnc stays empty: there was no session wake.
	novnc, keyboard, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_2")
	if err != nil || keyboard {
		t.Fatalf("err=%v keyboard=%v", err, keyboard)
	}
	if !strings.HasPrefix(novnc, "/api/v1/desktop-handoff/") {
		t.Fatalf("isolated bot without a session showed no live page: %q", novnc)
	}
	if raw := rememberedDesktopRaw(svc, "tenant-a", u2); !strings.Contains(raw, "tokenbot2") {
		t.Fatalf("remembered raw was not bot 2's page: %q", raw)
	}
	if desk.open != 0 {
		t.Fatalf("showing the picture started the desktop open=%d", desk.open)
	}
	desk.mu.Lock()
	peeks := append([]string{}, desk.peekUsers...)
	desk.mu.Unlock()
	if len(peeks) != 1 || peeks[0] != u2 {
		t.Fatalf("peek users=%v, want only %s", peeks, u2)
	}
}

// Two isolated bots in the same owner's container get distinct view URLs.
func TestHoldDesktopViewTwoIsolatedBotsGetDistinctScreenURLs(t *testing.T) {
	svc, desk, now := newIsolatedPeekService(t)
	u1 := desktop.BotDesktopUser("alice", "bot_1")
	u2 := desktop.BotDesktopUser("alice", "bot_2")
	desk.used("tenant-a", u1, *now)
	desk.used("tenant-a", u2, *now)

	one, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_1")
	if err != nil || !strings.HasPrefix(one, "/api/v1/desktop-handoff/") {
		t.Fatalf("bot_1 novnc=%q err=%v", one, err)
	}
	two, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_2")
	if err != nil || !strings.HasPrefix(two, "/api/v1/desktop-handoff/") {
		t.Fatalf("bot_2 novnc=%q err=%v", two, err)
	}
	if one == two {
		t.Fatalf("both bots showed the same gated page %q", one)
	}
	raw1 := rememberedDesktopRaw(svc, "tenant-a", u1)
	raw2 := rememberedDesktopRaw(svc, "tenant-a", u2)
	if !strings.Contains(raw1, "tokenbot1") || !strings.Contains(raw2, "tokenbot2") || raw1 == raw2 {
		t.Fatalf("raw pages not distinct: %q %q", raw1, raw2)
	}
	if strings.Contains(raw2, "tokenbot1") || strings.Contains(raw1, "tokenbot2") {
		t.Fatal("one bot's panel received the other bot's screen")
	}
	if desk.open != 0 {
		t.Fatalf("open=%d", desk.open)
	}
}

// Opening the 电脑 tab never starts the desktop, even when the bot is on
// screen and the URL has to be derived from the running display.
func TestHoldDesktopViewPeekDoesNotStartDesktop(t *testing.T) {
	svc, desk, now := newIsolatedPeekService(t)
	novnc, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_2")
	if err != nil || novnc != "" || desk.open != 0 {
		t.Fatalf("idle open started or showed a picture novnc=%q open=%d err=%v", novnc, desk.open, err)
	}
	desk.mu.Lock()
	idlePeeks := len(desk.peekUsers)
	desk.mu.Unlock()
	if idlePeeks != 0 {
		t.Fatalf("idle watch peeked %d times", idlePeeks)
	}

	desk.used("tenant-a", desktop.BotDesktopUser("alice", "bot_2"), *now)
	live, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_2")
	if err != nil || !strings.HasPrefix(live, "/api/v1/desktop-handoff/") {
		t.Fatalf("in-use novnc=%q err=%v", live, err)
	}
	if desk.open != 0 || desk.stop != 0 {
		t.Fatalf("deriving the URL started or stopped the desktop open=%d stop=%d", desk.open, desk.stop)
	}
	stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_2")
	if err != nil || !stopped {
		t.Fatalf("watch pinned idle stop: stopped=%v err=%v", stopped, err)
	}
}

func rememberedDesktopRaw(svc *Service, tenantID, userID string) string {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.ensureDesktopHydrated(tenantID)
	return svc.desktopView[desktopViewKey(tenantID, userID)].raw
}
