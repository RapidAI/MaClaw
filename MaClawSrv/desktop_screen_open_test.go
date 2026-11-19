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

func TestBashDoesNotOpenTheBotScreen(t *testing.T) {
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
	if calls != 1 || opens.Load() != 0 {
		t.Fatalf("calls=%d opens=%d", calls, opens.Load())
	}
	if desktopOutageWasSeen(req.Scope.RunID) {
		t.Fatal("a shell step was an outage")
	}
}

func TestOpenScreenActionOpensTheBotScreen(t *testing.T) {
	req := d97Env(t)
	var opens atomic.Int32
	openBotScreen = func(context.Context, agentruntime.Scope) error {
		opens.Add(1)
		return nil
	}
	operateDesktopFunc = operateDesktop
	out, err := invoke(t, req, map[string]any{"action": "open_screen"})
	if err != nil || !strings.Contains(out, "Screen is open") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if opens.Load() != 1 {
		t.Fatalf("opens=%d", opens.Load())
	}
	if !botScreenAlready(req.Scope.RunID) {
		t.Fatal("open_screen did not mark this run's screen")
	}
}

func TestBashWindowPixmapDoesNotRun(t *testing.T) {
	req := d97Env(t)
	operateDesktopFunc = operateDesktop
	_, err := invoke(t, req, map[string]any{"action": "bash", "command": "import -window 0x1a2b snake.png"})
	if err == nil || !strings.Contains(err.Error(), "action=screenshot") {
		t.Fatalf("err=%v", err)
	}
}

func TestScreenshotBeforeOpenScreenIsRefused(t *testing.T) {
	req := d97Env(t)
	opens := 0
	openBotScreen = func(context.Context, agentruntime.Scope) error {
		opens++
		return nil
	}
	operateDesktopFunc = operateDesktop
	_, err := invoke(t, req, map[string]any{"action": "screenshot"})
	if err == nil || !strings.Contains(err.Error(), "action=open_screen") || !strings.Contains(err.Error(), "BOT_SCREEN_CLOSED") {
		t.Fatalf("err=%v", err)
	}
	if opens != 0 {
		t.Fatalf("opens=%d", opens)
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
	if desktopOutageFailure(errors.New(d97NotOpen)) || desktopOutageFailure(errors.New("BOT_SCREEN_CLOSED: this bot's desktop is not open yet")) {
		t.Fatal("not open yet waited as an outage")
	}
	if !desktopNonRetryableStatus(errors.New("docker service rejected the desktop request (HTTP 400): invalid desktop request: bad command")) {
		t.Fatal("a plain HTTP 400 should not be waited out")
	}
	if desktopOutageFailure(errors.New("docker service rejected the desktop request (HTTP 400): invalid desktop request: bad command")) {
		t.Fatal("HTTP 400 waited")
	}
	for _, msg := range []string{
		"docker service rejected the desktop request (HTTP 409): DESKTOP_ASLEEP",
		"docker service rejected the desktop request (HTTP 423): locked",
		"docker service rejected the desktop request (HTTP 429): slow down",
		"桌面正在唤醒，再试一次。",
	} {
		if desktopNonRetryableStatus(errors.New(msg)) {
			t.Fatalf("recoverable status treated as a hard 4xx: %s", msg)
		}
	}
	if !desktopOutageFailure(errors.New("docker service rejected the desktop request (HTTP 502): 502 Bad Gateway")) {
		t.Fatal("HTTP 502 must still be an outage")
	}
	if !desktopOutageFailure(errors.New(d97Refused)) {
		t.Fatal("connection refused must still be an outage")
	}
}

func TestClosedScreenDoesNotOpenItself(t *testing.T) {
	req := d97Env(t)
	opens := 0
	openBotScreen = func(context.Context, agentruntime.Scope) error {
		opens++
		return nil
	}
	calls := 0
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		calls++
		return "", errors.New("BOT_SCREEN_CLOSED: this bot's desktop is not open yet")
	}
	_, err := invoke(t, req, map[string]any{"action": "bash", "command": "echo hi"})
	if err == nil || !strings.Contains(err.Error(), "action=open_screen") || !strings.Contains(err.Error(), "BOT_SCREEN_CLOSED") {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 || opens != 0 {
		t.Fatalf("calls=%d opens=%d", calls, opens)
	}
	if desktopOutageWasSeen(req.Scope.RunID) {
		t.Fatal("the bounded retry was an outage wait")
	}
}
