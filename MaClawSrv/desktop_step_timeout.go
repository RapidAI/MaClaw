package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Hard timeout per desktop tool call. A step that never returns (seen live
// 10-22: a navigate follow-up hung >3 min and the run stalled with no event)
// is abandoned after MACLAW_DESKTOP_STEP_TIMEOUT seconds (default 90) and
// recorded with failure class timeout. Shell, install and batch actions have
// their own limits and get MACLAW_DESKTOP_STEP_TIMEOUT_LONG (default 900).
// 0 disables the hard timeout.

const (
	desktopStepTimeoutEnv     = "MACLAW_DESKTOP_STEP_TIMEOUT"
	desktopStepTimeoutLongEnv = "MACLAW_DESKTOP_STEP_TIMEOUT_LONG"
)

var desktopStepLongActions = map[string]bool{
	"bash": true, "install": true, "task_run": true, "skill_run": true, "deliver": true,
	"app_run": true, "computer": true, "file_write": true,
}

// operateDesktopFunc is swapped in tests.
var operateDesktopFunc = operateDesktop

func desktopEnvSeconds(name string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return def
	}
	return time.Duration(f * float64(time.Second))
}

func desktopStepTimeout(action string) time.Duration {
	if desktopStepLongActions[action] {
		return desktopEnvSeconds(desktopStepTimeoutLongEnv, 900*time.Second)
	}
	if action == "publish" {
		// The publish pipeline bounds each of its own steps and reports
		// where it stopped (and whether submit was clicked); killing it
		// earlier reopens the tab mid-publish and loses that report.
		return desktopEnvSeconds(desktopStepTimeoutEnv+"_PUBLISH", 900*time.Second)
	}
	if action == "navigate" {
		// A navigate that has not returned in 45s is a hung tab; the
		// unstick path reopens it, so waiting longer only costs time.
		return desktopEnvSeconds(desktopStepTimeoutEnv, 45*time.Second)
	}
	return desktopEnvSeconds(desktopStepTimeoutEnv, 90*time.Second)
}

type desktopStepOutcome struct {
	out string
	err error
}

func operateDesktopWithTimeout(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	limit := desktopStepTimeout(action)
	if limit <= 0 {
		return operateDesktopFunc(ctx, scope, args)
	}
	stepCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	done := make(chan desktopStepOutcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- desktopStepOutcome{err: fmt.Errorf("desktop %s failed: %v", action, r)}
			}
		}()
		out, err := operateDesktopFunc(stepCtx, scope, args)
		done <- desktopStepOutcome{out, err}
	}()
	select {
	case r := <-done:
		if desktopPageHung(r.out, r.err) && desktopUnstickDue(scope) {
			if note := desktopUnstickAfterTimeout(scope, action, args); note != "" {
				if r.err != nil {
					return "", fmt.Errorf("%v; %s", r.err, note)
				}
				return r.out + "\n" + note, nil
			}
		}
		return r.out, r.err
	case <-stepCtx.Done():
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		log.Printf("[desktop-step] action=%s timed out after %s (hard step timeout)", action, limit)
		desktopDropStuckBinding(scope)
		if note := desktopUnstickAfterTimeout(scope, action, args); note != "" {
			return "", fmt.Errorf("desktop %s timed out after %ds (hard step timeout); %s", action, int(limit.Seconds()), note)
		}
		return "", fmt.Errorf("desktop %s timed out after %ds (hard step timeout); take a screenshot to see the real state, use a different action, or call request_takeover", action, int(limit.Seconds()))
	}
}

// desktopEmitStepStart writes a step_start event before the call runs, so a
// step that hangs is visible in the run file.
func desktopEmitStepStart(scope, owner agentruntime.Scope, args map[string]any, model string) {
	if !desktopEventsEnabled() {
		return
	}
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	if !desktopActionAllowed(action) {
		action = "invalid"
	}
	ev := desktopEvent{Event: "step_start", Model: model, Action: action, Args: desktopTrajectoryArgs(args)}
	if st := desktopRunFor(owner); st != nil {
		st.mu.Lock()
		ev.Step, ev.Site = st.seq+1, st.site
		st.mu.Unlock()
	}
	desktopEmit(scope, ev)
}

// desktopUnstickFunc is swapped in tests.
var desktopUnstickFunc = func(ctx context.Context, scope agentruntime.Scope, url string) (map[string]any, error) {
	return desktopCtlCall(ctx, scope, "", map[string]any{"op": desktop.CtlBotState, "unstick": url})
}

// desktopUnstickAfterTimeout: a browser step that hit the hard timeout most
// often sits on a tab whose renderer hung (10-22: zhuanlan.zhihu.com/write
// kept loading and every later screenshot, probe and app_list timed out at
// 90 s for 8 minutes). The supervisor closes that tab through the browser's
// DevTools endpoint and opens its URL again; the model is told so.
// Isolated bot displays only. MACLAW_DESKTOP_UNSTICK=off disables it.
func desktopUnstickAfterTimeout(scope agentruntime.Scope, action string, args map[string]any) string {
	if desktopStepLongActions[action] || !desktopIsolatedBot(scope) {
		return ""
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_UNSTICK"))); v == "off" || v == "0" || v == "false" {
		return ""
	}
	url := ""
	if action == "navigate" {
		url = strings.TrimSpace(desktopArg(args, "url"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := desktopUnstickFunc(ctx, scope, url)
	if err != nil {
		log.Printf("[desktop-step] unstick after %s timeout failed: %v", action, err)
		return ""
	}
	res, _ := out["unstick"].(map[string]any)
	if done, _ := res["done"].(bool); !done {
		log.Printf("[desktop-step] unstick after %s timeout not done: %v", action, res["why"])
		return ""
	}
	reopened, _ := res["reopened"].(string)
	closed, _ := res["closed"].(string)
	log.Printf("[desktop-step] unstick after %s timeout: closed=%s reopened=%s", action, closed, reopened)
	desktopEmitNote(scope, "page_unstick", "bot", "closed "+closed+" reopened "+reopened)
	return "the browser tab had stopped responding (" + closed + "); it was closed and opened again at " + reopened + " and is ready. Continue with your next page action on it (script, probe or rich_fill); no screenshot is needed and do not repeat the same navigate"
}

// desktopDropStuckBinding: the step that timed out is still running and
// still holds its desktop binding's lock (10-22 run_d28648ae / run_5c2a8086:
// a navigate hung on a CDP call that ignores the context, and every later
// screenshot, probe and app_list waited on that lock until its own 90 s
// timeout, run after run). The stuck binding is taken out of the map so the
// next step opens a fresh one; the old goroutine drops it when it returns.
func desktopDropStuckBinding(scope agentruntime.Scope) {
	userKey, err := desktopUserKey(scope)
	if err != nil {
		return
	}
	value, ok := desktopBindings.Load(userKey)
	if !ok {
		return
	}
	binding, _ := value.(*desktopBinding)
	if binding == nil {
		return
	}
	if binding.mu.TryLock() {
		binding.mu.Unlock()
		return
	}
	if desktopBindings.CompareAndDelete(userKey, value) {
		log.Printf("[desktop-step] dropped a desktop binding held by a hung step user=%s", userKey)
	}
}

// A step can come back on its own budget with the page silent (a CDP
// Runtime.evaluate timeout) before the hard step timeout fires; that is the
// same hung renderer, so the tab is reopened the same way, at most once a
// minute per desktop.
var desktopPageHungRe = regexp.MustCompile(`cdp timeout: (Runtime\.evaluate|Page\.enable)|page did not answer`)

func desktopPageHung(out string, err error) bool {
	if err != nil && desktopPageHungRe.MatchString(err.Error()) {
		return true
	}
	return desktopPageHungRe.MatchString(out)
}

var (
	desktopUnstickMu   sync.Mutex
	desktopUnstickLast = map[string]time.Time{}
)

func desktopUnstickDue(scope agentruntime.Scope) bool {
	key := scope.TenantID + "/" + scope.UserID
	desktopUnstickMu.Lock()
	defer desktopUnstickMu.Unlock()
	if time.Since(desktopUnstickLast[key]) < time.Minute {
		return false
	}
	desktopUnstickLast[key] = time.Now()
	return true
}
