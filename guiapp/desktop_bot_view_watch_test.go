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

// waitDesktopViewSubs blocks until the bot's shared watcher has at least want
// subscribers, so a test can hold the first poll until every turn is in.
func waitDesktopViewSubs(t *testing.T, botID string, want int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		desktopViewWatchMu.Lock()
		w := desktopViewWatchers[botID]
		subs := 0
		if w != nil {
			w.mu.Lock()
			subs = w.subs
			w.mu.Unlock()
		}
		desktopViewWatchMu.Unlock()
		if subs >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("watcher for %q never reached %d subscribers", botID, want)
}

// Two concurrent turns of one bot used to run two 400ms tickers against the
// same /desktop address. They must now share a single poll loop: both turns
// read the live state at once, the request rate must not multiply with the
// number of in-flight turns, and the loop must stop once no turn references
// it. The first poll is held until both turns subscribed, so every assertion
// below is a consequence of releasing that one poll, not of wall-clock luck.
func TestPublishDesktopBotViewSharesOneWatcherPerBot(t *testing.T) {
	const botID = "bot-watch-shared"
	release := make(chan struct{})
	var calls int32
	desktopBotWatchOverride = func(context.Context, string) (string, bool, string, error) {
		atomic.AddInt32(&calls, 1)
		<-release
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
		waitDesktopViewWatchersEmpty(t, 5*time.Second)
	})

	go app.publishDesktopBotView(ctx1, "req-1", "alice:bot_1", botID)
	go app.publishDesktopBotView(ctx2, "req-2", "alice:bot_1", botID)

	// Both turns must subscribe to ONE shared watcher before the first poll
	// is allowed to answer.
	waitDesktopViewSubs(t, botID, 2, 5*time.Second)
	close(release)
	released := time.Now()

	// The single first poll must announce the handoff to both turns under
	// their own request ids.
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
		case <-time.After(5 * time.Second):
			t.Fatalf("a turn never got its desktop read: %v", first)
		}
	}

	// While both turns are in flight there is exactly one watcher for the bot.
	desktopViewWatchMu.Lock()
	if len(desktopViewWatchers) != 1 || desktopViewWatchers[botID] == nil {
		t.Fatalf("turns did not share one watcher: %v", desktopViewWatchers)
	}
	desktopViewWatchMu.Unlock()

	// Let the shared loop tick for ~1s after the release. Two private loops
	// would poll at 0/400/800ms each (six calls); the shared loop polls at
	// most three times in that window, however loaded the machine is.
	time.Sleep(time.Until(released.Add(950 * time.Millisecond)))
	cancel1()
	cancel2()
	waitDesktopViewWatchersEmpty(t, 5*time.Second)
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
	case <-time.After(5 * time.Second):
		t.Fatal("publishDesktopBotView did not return after its context ended")
	}
	waitDesktopViewWatchersEmpty(t, 5*time.Second)
}
