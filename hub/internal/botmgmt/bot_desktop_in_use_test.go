package botmgmt

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// activityDesktop is a desktop whose last bot desktop-screen step is set by
// the test, standing in for the Hub desktop pool.
type activityDesktop struct {
	desktopCounter
	useMu sync.Mutex
	at    map[string]time.Time
	novnc string
	reads int
}

func (d *activityDesktop) BotDesktopUse(tenantID, userID string) (time.Time, string) {
	d.useMu.Lock()
	defer d.useMu.Unlock()
	d.reads++
	return d.at[tenantID+"/"+userID], d.novnc
}

func (d *activityDesktop) used(tenantID, userID string, at time.Time) {
	d.useMu.Lock()
	defer d.useMu.Unlock()
	if d.at == nil {
		d.at = map[string]time.Time{}
	}
	d.at[tenantID+"/"+userID] = at
}

func newInUseService(t *testing.T) (*Service, *activityDesktop, *time.Time) {
	t.Helper()
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &activityDesktop{novnc: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	now := time.Now()
	svc.Now = func() time.Time { return now }
	return svc, desk, &now
}

// Opening the 电脑 tab on a bot that is not using the desktop stays asleep,
// reads no more than the in-memory step record, and never starts the desktop.
func TestHoldDesktopViewIdleBotStaysAsleepWithoutWaking(t *testing.T) {
	svc, desk, _ := newInUseService(t)
	novnc, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || novnc != "" {
		t.Fatalf("idle bot showed a picture %q err=%v", novnc, err)
	}
	if desk.open != 0 || desk.stop != 0 {
		t.Fatalf("watch touched the desktop open=%d stop=%d", desk.open, desk.stop)
	}
	// Watching must not keep the desktop up either.
	stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil || !stopped {
		t.Fatalf("watch pinned idle stop: stopped=%v err=%v", stopped, err)
	}
}

// A bot step that drives the screen through the worker's tool route shows
// the live picture on the next poll, with no 唤醒 click and no start.
func TestHoldDesktopViewShowsPictureWhileBotDrivesDesktop(t *testing.T) {
	svc, desk, now := newInUseService(t)
	desk.used("tenant-a", "alice", *now)
	novnc, keyboard, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(novnc, "/api/v1/desktop-handoff/") || keyboard {
		t.Fatalf("bot in use: novnc=%q keyboard=%v", novnc, keyboard)
	}
	if desk.open != 0 {
		t.Fatalf("showing the picture started the desktop open=%d", desk.open)
	}
	// Repeated polls keep the same gated page instead of minting new ones.
	again, _, _ := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	if again != novnc {
		t.Fatalf("second poll changed the page %q -> %q", novnc, again)
	}
	// The picture is shown, not pinned: idle stop still works.
	stopped, err := svc.StopDesktopIfIdle(context.Background(), "tenant-a", "alice", "inst_alice")
	if err != nil || !stopped {
		t.Fatalf("bot-use picture pinned the desktop: stopped=%v err=%v", stopped, err)
	}
}

// Once the bot has not used the screen for the window, the tab goes back to
// asleep even though the page is still remembered.
func TestHoldDesktopViewReturnsToSleepAfterBotStopsUsingDesktop(t *testing.T) {
	svc, desk, now := newInUseService(t)
	desk.used("tenant-a", "alice", *now)
	if novnc, _, _ := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice"); novnc == "" {
		t.Fatal("bot in use showed no picture")
	}
	later := now.Add(BotDesktopInUseWindow + time.Second)
	svc.Now = func() time.Time { return later }
	novnc, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || novnc != "" {
		t.Fatalf("stale picture after the bot moved on: %q err=%v", novnc, err)
	}
	// A bot that used the screen just inside the window is still shown.
	desk.used("tenant-a", "alice", later.Add(-BotDesktopInUseWindow+time.Second))
	if novnc, _, _ := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice"); novnc == "" {
		t.Fatal("bot inside the window showed no picture")
	}
}

// Another person's desktop use never shows on this bot's tab.
func TestHoldDesktopViewIgnoresOtherDesktopsUse(t *testing.T) {
	svc, desk, now := newInUseService(t)
	desk.used("tenant-a", "bob", *now)
	novnc, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || novnc != "" {
		t.Fatalf("another desktop's use leaked: %q err=%v", novnc, err)
	}
}
