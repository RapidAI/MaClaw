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

// ensureBotScreen opens an isolated bot's screen once per run before the
// first desktop action. force opens it again after "not open yet". Several
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
