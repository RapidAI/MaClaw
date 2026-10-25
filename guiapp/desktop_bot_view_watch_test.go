package guiapp

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitDesktopViewWatchersEmpty(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		desktopViewWatchMu.Lock()
		n := len(desktopViewWatchers)
		desktopViewWatchMu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("shared desktop view watchers did not stop within %v", d)
}

// Two concurrent turns of one bot used to run two 400ms tickers against the
// same /desktop address. They must now share a single poll loop: both turns
// still read the live state at once, but the request rate must not multiply
// with the number of in-flight turns, and the loop must stop once no turn
// references it.
func TestPublishDesktopBotViewSharesOneWatcherPerBot(t *testing.T) {
	const botID = "bot-watch-shared"
	var calls int32
	desktopBotWatchOverride = func(context.Context, string) (string, bool, string, error) {
		atomic.AddInt32(&calls, 1)
		return "https://hub.example/vnc", true, "", nil
	}
	t.Cleanup(func() { desktopBotWatchOverride = nil })

	views := make(chan string, 16)
	prevSink := desktopBotViewSink
	desktopBotViewSink = func(payload string) { views <- payload }
	t.Cleanup(func() { desktopBotViewSink = prevSink })

	app := &App{}
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel1()
		cancel2()
		waitDesktopViewWatchersEmpty(t, 2*time.Second)
	})

	start := time.Now()
	go app.publishDesktopBotView(ctx1, "req-1", "alice:bot_1", botID)
	go app.publishDesktopBotView(ctx2, "req-2", "alice:bot_1", botID)

	// Both turns must receive their first desktop read immediately; joining
	// the shared watcher must not cost a 400ms tick.
	first := map[string]bool{}
	for len(first) < 2 {
		select {
		case payload := <-views:
			if !strings.Contains(payload, `"user_control":true`) {
				continue
			}
			if strings.Contains(payload, "req-1") {
				first["req-1"] = true
			}
			if strings.Contains(payload, "req-2") {
				first["req-2"] = true
			}
		case <-time.After(400 * time.Millisecond):
			t.Fatalf("a turn waited a tick for its first desktop read: %v", first)
		}
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("first desktop read took %v; watcher startup added a tick", elapsed)
	}

	// Let the shared loop tick for ~1s. Two private loops would poll at
	// 0/400/800ms each (six calls); the shared loop polls at most three
	// times in that window.
	time.Sleep(time.Until(start.Add(950 * time.Millisecond)))
	cancel1()
	cancel2()
	waitDesktopViewWatchersEmpty(t, 2*time.Second)
	if got := atomic.LoadInt32(&calls); got > 3 {
		t.Fatalf("shared watcher polled %d times in ~1s for two turns; private loops would double that", got)
	}
}

// A turn whose context ends while the bot has no other in-flight turn must
// leave nothing behind: the watcher stops by itself once the subscriber
// count reaches zero.
func TestPublishDesktopBotViewWatcherStopsWhenIdle(t *testing.T) {
	const botID = "bot-watch-idle"
	desktopBotWatchOverride = func(context.Context, string) (string, bool, string, error) {
		return "https://hub.example/vnc", false, "", nil
	}
	t.Cleanup(func() { desktopBotWatchOverride = nil })

	app := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		app.publishDesktopBotView(ctx, "req-idle", "alice:bot_1", botID)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishDesktopBotView did not return after its context ended")
	}
	waitDesktopViewWatchersEmpty(t, 2*time.Second)
}
