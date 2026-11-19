package botmgmt

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLiveDesktopPictureDropsAStoppedFrame(t *testing.T) {
	url, state := liveDesktopPicture("http://hub/stale/vnc.html", DesktopStateDisplayStopped)
	if url != "" || state != DesktopStateDisplayStopped {
		t.Fatalf("display stopped url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("http://hub/stale/vnc.html", DesktopStateAsleep)
	if url != "" || state != DesktopStateAsleep {
		t.Fatalf("asleep url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("http://hub/live/vnc.html", DesktopStateRunning)
	if url != "http://hub/live/vnc.html" || state != DesktopStateRunning {
		t.Fatalf("running url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("", DesktopStateRunning)
	if url != "" || state != DesktopStateAsleep {
		t.Fatalf("running without a page url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("", "")
	if url != "" || state != "" {
		t.Fatalf("unknown empty url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture(" http://hub/live/vnc.html ", "")
	if url != "" || state != "" {
		t.Fatalf("a page without a state is not a picture url=%q state=%q", url, state)
	}
	url, state = liveDesktopPicture("http://hub/stale/vnc.html", DesktopStateUnavailable)
	if url != "" || state != DesktopStateUnavailable {
		t.Fatalf("unavailable url=%q state=%q", url, state)
	}
}

type countingView struct {
	desktopCounter
	mu      sync.Mutex
	calls   int
	block   chan struct{}
	entered chan struct{}
}

func (c *countingView) DesktopViewState(context.Context, string, string) (string, string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.entered != nil {
		select {
		case <-c.entered:
		default:
			close(c.entered)
		}
	}
	if c.block != nil {
		<-c.block
	}
	return DesktopStateDisplayStopped, "http://hub/stale/vnc.html", nil
}

func (c *countingView) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestDesktopPictureReadsTheScreenOnce(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &countingView{desktopCounter: desktopCounter{url: "http://dockerd.example/vnc.html"}}
	svc.Desktop = desk
	ctx := context.Background()
	if _, _, err := svc.WakeDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	if desk.callCount() != 0 {
		t.Fatalf("wake asked %d times before a page existed", desk.callCount())
	}
	url, _, state, err := svc.DesktopPicture(ctx, "tenant-a", "alice", "bot_alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if desk.callCount() != 1 {
		t.Fatalf("hold asked desktopd %d times, want 1", desk.callCount())
	}
	if url != "" || state != DesktopStateDisplayStopped {
		t.Fatalf("url=%q state=%q", url, state)
	}
}

func TestHoldWaitsUntilWakeReleasesTheGate(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.Desktop = &countingView{desktopCounter: desktopCounter{url: "http://dockerd.example/vnc.html"}}
	ctx := context.Background()
	if _, _, err := svc.WakeDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	desk := &countingView{
		desktopCounter: desktopCounter{url: "http://dockerd.example/vnc.html"},
		entered:        make(chan struct{}),
		block:          make(chan struct{}),
	}
	svc.Desktop = desk
	wakeDone := make(chan error, 1)
	go func() {
		_, _, err := svc.WakeDesktopView(ctx, "tenant-a", "alice", "bot_alice")
		wakeDone <- err
	}()
	select {
	case <-desk.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("wake did not read the screen")
	}
	holdDone := make(chan error, 1)
	go func() {
		_, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice")
		holdDone <- err
	}()
	select {
	case err := <-holdDone:
		t.Fatalf("hold finished while wake held the gate: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if desk.callCount() != 1 {
		t.Fatalf("hold read the screen while wake held the gate: %d calls", desk.callCount())
	}
	close(desk.block)
	select {
	case err := <-wakeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wake did not finish")
	}
	select {
	case err := <-holdDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hold did not finish after wake")
	}
}
