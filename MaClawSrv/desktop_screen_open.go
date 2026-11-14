package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// errBotScreenUnconfigured means this process has no Hub session to open a
// bot screen with. The step proceeds; desktopd opens the screen on the call
// that needs it.
var errBotScreenUnconfigured = errors.New("bot screen opener is not configured")

// openBotScreen asks Hub for this bot's session, which opens its screen.
// Tests replace it.
var openBotScreen = defaultOpenBotScreen

func defaultOpenBotScreen(ctx context.Context, scope agentruntime.Scope) error {
	if desktopRemoteSession == nil && desktopHubClientFromEnv() == nil {
		return errBotScreenUnconfigured
	}
	// A round that already marked itself woken for a headless call has not
	// opened this bot's screen. Forget that so the session call really runs.
	if runID := strings.TrimSpace(scope.RunID); runID != "" {
		if value, ok := desktopRoundWakes.Load(runID); ok {
			if wake, _ := value.(*desktopRoundWake); wake != nil {
				wake.mu.Lock()
				wake.have = false
				wake.woken = false
				wake.mu.Unlock()
			}
		}
	}
	userKey, err := desktopUserKey(scope)
	if err != nil {
		return err
	}
	_, err = ensureDesktopRound(ctx, scope, userKey, true)
	return err
}

var (
	botScreenMu       sync.Mutex
	botScreenReady    = map[string]bool{}
	botScreenInflight sync.Map
)

type botScreenCall struct {
	done chan struct{}
	err  error
}

func resetBotScreens() {
	botScreenMu.Lock()
	botScreenReady = map[string]bool{}
	botScreenMu.Unlock()
	botScreenInflight.Range(func(key, _ any) bool {
		botScreenInflight.Delete(key)
		return true
	})
}

func botScreenAlready(runID string) bool {
	if runID == "" {
		return false
	}
	botScreenMu.Lock()
	defer botScreenMu.Unlock()
	return botScreenReady[runID]
}

func markBotScreen(runID string) {
	if runID == "" {
		return
	}
	botScreenMu.Lock()
	defer botScreenMu.Unlock()
	if len(botScreenReady) > 4096 {
		botScreenReady = map[string]bool{}
	}
	botScreenReady[runID] = true
}

// desktopOpenScreenFirst is what a screen step says when this bot has not
// called action=open_screen. The model decides; this process does not open
// a display for a shell-only task.
func desktopOpenScreenFirst(err error) error {
	if err != nil && strings.Contains(err.Error(), "action=open_screen") {
		return err
	}
	return fmt.Errorf("%s: this step uses the screen. Call action=open_screen first, then retry this step. Do not call open_screen for ssh, training, compiling, or any command that only needs a shell", desktopbot.CodeBotScreenClosed)
}

// desktopBotMustOpenScreen refuses a visible-desktop step until this run has
// opened its screen. Shell steps, and the owner's desktop, are unchanged.
func desktopBotMustOpenScreen(scope agentruntime.Scope, action string, args map[string]any) error {
	_, bot := desktop.SplitDesktopUser(scope.UserID)
	if bot == "" || action == "open_screen" || !desktopScreenLeaseNeeded(action, args) {
		return nil
	}
	if botScreenAlready(strings.TrimSpace(scope.RunID)) {
		return nil
	}
	return desktopOpenScreenFirst(nil)
}

// openDesktopScreen is action=open_screen. An isolated bot gets its own
// display. The owner's desktop wakes the shared screen.
func openDesktopScreen(ctx context.Context, scope agentruntime.Scope) (string, error) {
	_, bot := desktop.SplitDesktopUser(scope.UserID)
	if bot == "" {
		userKey, err := desktopUserKey(scope)
		if err != nil {
			return "", err
		}
		if _, err := ensureDesktopRound(ctx, scope, userKey, true); err != nil {
			return "", err
		}
		return "Screen is open.", nil
	}
	if err := ensureBotScreen(ctx, scope, true); err != nil {
		return "", err
	}
	return "Screen is open. Browser, screenshot, click, and GUI programs can use it now.", nil
}

// ensureBotScreen opens an isolated bot's screen when the model calls
// action=open_screen. force opens it again after "not open yet". Several
// bots open in parallel; two callers of one bot share one open.
func ensureBotScreen(ctx context.Context, scope agentruntime.Scope, force bool) error {
	_, bot := desktop.SplitDesktopUser(scope.UserID)
	if bot == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runID := strings.TrimSpace(scope.RunID)
	if !force && botScreenAlready(runID) {
		return nil
	}
	key := scope.TenantID + "\x00" + scope.UserID
	started := time.Now()
	err := doBotScreen(ctx, key, func(ctx context.Context) error { return openBotScreen(ctx, scope) })
	if errors.Is(err, errBotScreenUnconfigured) {
		return nil
	}
	if err != nil {
		// A dial failure is the outage path on the step itself. A screen that
		// is still closed is opened by the desktop call, then retried once.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if desktopOutageFailure(err) || desktopColdStart(err) {
			return nil
		}
		return err
	}
	markBotScreen(runID)
	log.Printf("[desktop] bot screen ensured run=%s bot=%s took=%s", runID, bot, time.Since(started).Round(time.Millisecond))
	return nil
}

// botScreenOpenBudget bounds one shared open. It is not tied to the first
// caller's cancel, so one stopped step does not abort the open another bot
// step is waiting on.
const botScreenOpenBudget = 60 * time.Second

func doBotScreen(ctx context.Context, key string, fn func(context.Context) error) error {
	fresh := &botScreenCall{done: make(chan struct{})}
	actual, loaded := botScreenInflight.LoadOrStore(key, fresh)
	call := actual.(*botScreenCall)
	if loaded {
		select {
		case <-call.done:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return call.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() {
		if rec := recover(); rec != nil {
			call.err = fmt.Errorf("bot screen open panicked: %v", rec)
			close(call.done)
			botScreenInflight.Delete(key)
			panic(rec)
		}
	}()
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), botScreenOpenBudget)
	defer cancel()
	call.err = fn(workCtx)
	close(call.done)
	botScreenInflight.Delete(key)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return call.err
}
