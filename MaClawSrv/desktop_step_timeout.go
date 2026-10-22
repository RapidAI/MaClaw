package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
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
		return r.out, r.err
	case <-stepCtx.Done():
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		log.Printf("[desktop-step] action=%s timed out after %s (hard step timeout)", action, limit)
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
