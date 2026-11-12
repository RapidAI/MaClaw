package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestBotScreenEnsuredBeforeTheFirstBash(t *testing.T) {
	req := d97Env(t)
	var opens atomic.Int32
	openBotScreen = func(context.Context, agentruntime.Scope) error {
		opens.Add(1)
		return nil
	}
	calls := 0
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		if desktopArg(args, "action") != "bash" {
			return "ok", nil
		}
		calls++
		return "bash ok", nil
	}
	out, err := invoke(t, req, map[string]any{"action": "bash", "command": "echo hi"})
	if err != nil || !strings.Contains(out, "bash ok") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if calls != 1 || opens.Load() != 1 {
		t.Fatalf("calls=%d opens=%d", calls, opens.Load())
	}
	if desktopOutageWasSeen(req.Scope.RunID) {
		t.Fatal("a successful open was an outage")
	}
}

func TestThreeBotsOpenScreensConcurrently(t *testing.T) {
	d97Env(t)
	prev := openBotScreen
	t.Cleanup(func() { openBotScreen = prev })
	var mu sync.Mutex
	seen := map[string]int{}
	started := make(chan struct{})
	openBotScreen = func(_ context.Context, scope agentruntime.Scope) error {
		<-started
		_, bot := desktop.SplitDesktopUser(scope.UserID)
		mu.Lock()
		seen[bot]++
		mu.Unlock()
		return nil
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 6)
	for _, bot := range []string{"bot_a", "bot_b", "bot_c"} {
		wg.Add(1)
		go func(bot string) {
			defer wg.Done()
			scope := agentruntime.Scope{
				TenantID: "tenant-d97",
				UserID:   desktop.BotDesktopUser("daniel", bot),
				RunID:    "run-" + bot,
			}
			errCh <- ensureBotScreen(context.Background(), scope, false)
			errCh <- ensureBotScreen(context.Background(), scope, false)
		}(bot)
	}
	time.Sleep(20 * time.Millisecond)
	close(started)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 || seen["bot_a"] != 1 || seen["bot_b"] != 1 || seen["bot_c"] != 1 {
		t.Fatalf("opens=%v", seen)
	}
}

func TestClosedScreenIsNotAnOutageAnd4xxDoesNotWait(t *testing.T) {
	if desktopOutageFailure(errors.New(d97NotOpen)) {
		t.Fatal("not open yet waited as an outage")
	}
	if desktopOutageFailure(errors.New("docker service rejected the desktop request (HTTP 400): invalid desktop request: bad command")) {
		t.Fatal("HTTP 400 waited")
	}
	if !desktopOutageFailure(errors.New("docker service rejected the desktop request (HTTP 502): 502 Bad Gateway")) {
		t.Fatal("HTTP 502 must still be an outage")
	}
	if !desktopOutageFailure(errors.New(d97Refused)) {
		t.Fatal("connection refused must still be an outage")
	}
}
