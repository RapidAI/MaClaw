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

// runOnDesktop puts a run Hub is following, as the watcher keeps it.
func runOnDesktop(svc *Service, runID string, started, seen time.Time) *desktopAdmitState {
	state := &desktopAdmitState{userID: "alice", botID: "bot_alice", runID: runID, status: "running", started: started, seen: seen}
	svc.mu.Lock()
	svc.ensureDesktopHydrated("tenant-a")
	if svc.desktopAdmits == nil {
		svc.desktopAdmits = map[string]*desktopAdmitState{}
	}
	svc.desktopAdmits[desktopAdmitKey("tenant-a", "alice", "bot_alice", runID)] = state
	svc.mu.Unlock()
	return state
}

// One long operation: the run wakes the desktop (the only step Hub sees,
// because the browser publish goes straight over CDP) and keeps working for
// longer than the step window. The tab stays live while the run is going,
// and goes back to asleep when it ends, without ever starting the desktop.
func TestHoldDesktopViewStaysLiveThroughOneLongDesktopOperation(t *testing.T) {
	svc, desk, now := newInUseService(t)
	t0 := *now
	state := runOnDesktop(svc, "run_long", t0.Add(-time.Second), t0)
	desk.used("tenant-a", "alice", t0) // the run's session wake
	at := func(d time.Duration) {
		later := t0.Add(d)
		svc.Now = func() time.Time { return later }
		svc.noteDesktopRunSeen(state) // the watcher still reads it as running
	}
	at(5 * time.Minute)
	novnc, _, err := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || novnc == "" {
		t.Fatalf("long desktop operation fell asleep after %s: %q err=%v", 5*time.Minute, novnc, err)
	}
	// It ends (done, failed or cancelled all settle the same way): asleep.
	state.mu.Lock()
	state.done = true
	state.mu.Unlock()
	if novnc, _, _ := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice"); novnc != "" {
		t.Fatalf("finished run still shown: %q", novnc)
	}
	if desk.open != 0 || desk.stop != 0 {
		t.Fatalf("watching touched the desktop open=%d stop=%d", desk.open, desk.stop)
	}
}

// A run the watcher can no longer read (MaClawSrv gone) is not taken as
// driving the desktop once the window passes, so a crash leaves no stale live tab.
func TestHoldDesktopViewDropsARunNobodyCanRead(t *testing.T) {
	svc, desk, now := newInUseService(t)
	t0 := *now
	runOnDesktop(svc, "run_lost", t0.Add(-time.Second), t0)
	desk.used("tenant-a", "alice", t0)
	later := t0.Add(BotDesktopInUseWindow + time.Minute)
	svc.Now = func() time.Time { return later }
	if novnc, _, _ := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice"); novnc != "" {
		t.Fatalf("unreadable run kept the tab live: %q", novnc)
	}
}

// A bot task that never touched the screen (chat, research, shell work)
// does not count, even while it runs and the desktop is still remembered
// from an earlier run's step.
func TestHoldDesktopViewIgnoresARunThatNeverUsedTheScreen(t *testing.T) {
	svc, desk, now := newInUseService(t)
	t0 := *now
	desk.used("tenant-a", "alice", t0) // an earlier run's step
	state := runOnDesktop(svc, "run_chat", t0.Add(3*time.Minute), t0.Add(3*time.Minute))
	later := t0.Add(5 * time.Minute)
	svc.Now = func() time.Time { return later }
	svc.noteDesktopRunSeen(state)
	if novnc, _, _ := svc.HoldDesktopView(context.Background(), "tenant-a", "alice", "bot_alice"); novnc != "" {
		t.Fatalf("a run that never used the screen showed it: %q", novnc)
	}
}
