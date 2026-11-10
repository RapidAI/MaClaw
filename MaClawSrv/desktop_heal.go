package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// Transport self-heal (papers 2026-11-11 run_5ecb9d6d). The browser was fine
// inside the desktop (127.0.0.1:18037 answered), but every official step
// from MaClawSrv failed at the CDP dial — "websocket: bad handshake", later
// "read tcp …:443: i/o timeout" — and two such failures blocked publish:csdn
// for the rest of the run, with nobody trying to reconnect. A failure to
// even reach the browser (the dial, the target list) means nothing was sent
// to the page, so the step is safe to run again on a fresh route. Before it
// counts toward a block, the run drops its cached browser binding and the
// round's cached endpoint, closes this process's idle connections and asks
// Hub for the desktop session again (Hub's ensure restarts a browser that
// is gone), then runs the step once more. A step that works again clears the
// run's infrastructure blocks: the route is back, so the method is too.

// desktopTransportErrRe: the step never reached the browser.
var desktopTransportErrRe = regexp.MustCompile(`(?i)cdp websocket connection failed|cdp dial:|discover targets:|bad handshake|desktop browser: .*(i/o timeout|connection refused|connection reset|eof)`)

func desktopTransportFailure(err error) bool {
	return err != nil && desktopTransportErrRe.MatchString(err.Error())
}

// desktopHealActions reach the browser over CDP and can be run again when
// the dial itself failed.
var desktopHealActions = map[string]bool{"navigate": true, "probe": true, "script": true, "click": true, "type": true, "press": true, "wait": true, "upload": true, "screenshot": true, "publish": true, "batch": true, "clipboard_set": true}

// desktopHealPerRun bounds the heals of one run.
const desktopHealPerRun = 6

var (
	desktopHealMu    sync.Mutex
	desktopHealCount = map[string]int{}
	// desktopHealPause is the wait before the retry (tests set it to 0).
	desktopHealPause = 1500 * time.Millisecond
	// desktopHealSession is swapped in tests; production asks Hub again.
	desktopHealSession = func(ctx context.Context, scope agentruntime.Scope, userKey string) error {
		_, err := ensureDesktopRound(ctx, scope, userKey, true)
		return err
	}
)

func desktopHealDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_TRANSPORT_HEAL")))
	return v == "off" || v == "0" || v == "false"
}

// desktopHealAllowed: this failure may be healed and the step re-run. A
// publish that already went out (a record, a submit) is never re-run.
func desktopHealAllowed(scope agentruntime.Scope, action string, err error) bool {
	if desktopHealDisabled() || !desktopHealActions[action] || !desktopTransportFailure(err) {
		return false
	}
	if action == "publish" {
		if _, has := desktopPublishRecordOf(scope.RunID); has || desktopRunSubmittedUnconfirmed(scope) {
			return false
		}
	}
	desktopHealMu.Lock()
	defer desktopHealMu.Unlock()
	if len(desktopHealCount) > 4096 {
		desktopHealCount = map[string]int{}
	}
	if desktopHealCount[scope.RunID] >= desktopHealPerRun {
		return false
	}
	desktopHealCount[scope.RunID]++
	return true
}

// desktopHealTransport resets every cached piece of the route to this run's
// browser and asks for the desktop session again. It returns what it did.
func desktopHealTransport(ctx context.Context, scope agentruntime.Scope) string {
	var did []string
	userKey, _ := desktopUserKey(desktopOwner(scope))
	if userKey != "" {
		if value, ok := desktopBindings.Load(userKey); ok {
			if binding, _ := value.(*desktopBinding); binding != nil && binding.mu.TryLock() {
				binding.session = nil
				binding.addr = ""
				binding.mu.Unlock()
			}
			desktopBindings.CompareAndDelete(userKey, value)
			did = append(did, "dropped the browser binding")
		}
	}
	if runID := strings.TrimSpace(scope.RunID); runID != "" {
		if value, ok := desktopRoundWakes.Load(runID); ok {
			if wake, _ := value.(*desktopRoundWake); wake != nil && wake.mu.TryLock() {
				wake.have = false
				wake.endpoint = desktopEndpoint{}
				wake.mu.Unlock()
				did = append(did, "forgot the cached endpoint")
			}
		}
	}
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.CloseIdleConnections()
		did = append(did, "closed idle connections")
	}
	if desktopHealPause > 0 {
		select {
		case <-time.After(desktopHealPause):
		case <-ctx.Done():
		}
	}
	if desktopHealSession != nil && ctx.Err() == nil {
		hctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := desktopHealSession(hctx, scope, userKey)
		cancel()
		if err != nil {
			did = append(did, "session again failed: "+desktopClipWhy(err.Error()))
		} else {
			did = append(did, "asked for the desktop session again")
		}
	}
	return strings.Join(did, ", ")
}

// desktopClearInfraBlocks: a step reached the browser again, so blocks and
// counts that only the infrastructure caused are lifted.
func desktopClearInfraBlocks(scope agentruntime.Scope) bool {
	st := desktopRunFor(scope)
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	cleared := false
	for key := range st.failures {
		if strings.HasSuffix(key, "|infra_timeout") {
			delete(st.failures, key)
			cleared = true
		}
	}
	for intent, class := range st.blocked {
		if class == "infra_timeout" {
			delete(st.blocked, intent)
			cleared = true
		}
	}
	if cleared {
		log.Printf("[desktop-heal] run=%s infrastructure blocks cleared: the browser answers again", scope.RunID)
	}
	return cleared
}

// desktopReachesBrowser: a successful step of this action talked to the browser.
func desktopReachesBrowser(action string) bool {
	return desktopHealActions[action] && action != "screenshot" && action != "clipboard_set"
}

// desktopHealTake spends one of the run's heals on letting an
// infrastructure-blocked call through (false when none are left).
func desktopHealTake(runID string) bool {
	if desktopHealDisabled() || strings.TrimSpace(runID) == "" {
		return false
	}
	desktopHealMu.Lock()
	defer desktopHealMu.Unlock()
	if desktopHealCount[runID] >= desktopHealPerRun {
		return false
	}
	desktopHealCount[runID]++
	return true
}

// Whether this run's browser route currently works: the time of the last
// step that reached the browser and of the last transport failure.
var (
	desktopRouteMu     sync.Mutex
	desktopRouteOK     = map[string]time.Time{}
	desktopRouteFailed = map[string]time.Time{}
)

func desktopNoteRoute(runID, action string, err error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	desktopRouteMu.Lock()
	defer desktopRouteMu.Unlock()
	if len(desktopRouteOK)+len(desktopRouteFailed) > 8192 {
		desktopRouteOK, desktopRouteFailed = map[string]time.Time{}, map[string]time.Time{}
	}
	switch {
	case err == nil && desktopReachesBrowser(action):
		desktopRouteOK[runID] = time.Now()
	case desktopTransportFailure(err):
		desktopRouteFailed[runID] = time.Now()
	}
}

// desktopRouteDown: the last word about this run's browser route is a
// failure to reach it (no step has reached the browser since).
func desktopRouteDown(runID string) bool {
	desktopRouteMu.Lock()
	defer desktopRouteMu.Unlock()
	failed, f := desktopRouteFailed[strings.TrimSpace(runID)]
	return f && !desktopRouteOK[strings.TrimSpace(runID)].After(failed)
}

// desktopRouteDownNote replaces "the page holds content you entered" when
// the browser was never reached: nothing could have been entered through it.
const desktopRouteDownNote = "the browser did not answer at all: this run's connection to it has been failing (see the earlier connection errors), so no page content was read or changed by this step and nothing was closed or reloaded. Take a screenshot to see the screen; the next browser step reconnects first. Do not claim the editor is open or filled unless a step showed it"

// desktopRouteEverFailed: this run had a failure to reach the browser at
// all. Only such blocks are let through to heal; a slow page or site
// (cdp timeout inside a step) stays blocked.
func desktopRouteEverFailed(runID string) bool {
	desktopRouteMu.Lock()
	defer desktopRouteMu.Unlock()
	_, f := desktopRouteFailed[strings.TrimSpace(runID)]
	return f
}
