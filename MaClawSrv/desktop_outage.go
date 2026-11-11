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
)

// Desktop service outage ride-out (papers 2026-11-11 08:32–08:35, Bot 2
// run_69cbb334 and Bot 3 run_ef8bef3b). Two things made both runs reply and
// go idle without doing the work:
//
//   - Bot 3's first two bash calls (the ssh to the person's server) came back
//     "this bot's desktop is not open yet": the isolated bot's own screen was
//     still being opened. That counted as infra_timeout twice, so bash — the
//     only way to reach the server — was blocked for the rest of the run.
//   - Hub was redeployed at 08:34:10 and listened again at 08:34:29. Every
//     desktop call in those 19 seconds failed at the dial ("connect:
//     connection refused"); within 14 s each method was blocked after two
//     strikes and the model ended both runs, "succeeded", with nothing done.
//
// Neither failure reached the desktop: a refused or timed-out dial sends
// nothing, and "not open yet" is refused before anything runs. So the step
// is safe to run again. Before such a failure counts, the step waits for the
// service to come back (asking Hub for the desktop session each time, which
// also opens an isolated bot's own screen) and runs again, within a bounded
// window per step and per run. Only when the window is spent does the
// failure count as before, worded as the infrastructure.

// desktopOutageErrRe: the call never reached the desktop.
var desktopOutageErrRe = regexp.MustCompile(`(?i)desktop service is unreachable: .*dial tcp [^ ]*: (connect: )?(connection refused|i/o timeout|no route to host|network is unreachable)|desktop service is unreachable: .*dial tcp: lookup |desktop is not open yet|docker service request failed: .*dial tcp [^ ]*: (connect: )?connection refused`)

// desktopInfraBlockTTL: an infra_timeout block lifts by itself after this.
var desktopInfraBlockTTL = 2 * time.Minute

// desktopColdStart: the isolated bot's own screen was not open yet.
func desktopColdStart(err error) bool {
	return err != nil && strings.Contains(err.Error(), "desktop is not open yet")
}

func desktopOutageFailure(err error) bool {
	return err != nil && desktopOutageErrRe.MatchString(err.Error())
}

func desktopOutageDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_OUTAGE_WAIT")))
	return v == "off" || v == "0" || v == "false"
}

func desktopOutageEnvDuration(name string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

var (
	// desktopOutageStepWindow is the most one step waits for the service.
	desktopOutageStepWindow = func() time.Duration {
		return desktopOutageEnvDuration("MACLAW_DESKTOP_OUTAGE_STEP_SEC", 90*time.Second)
	}
	// desktopOutageRunWindow is the most one run spends waiting in total.
	desktopOutageRunWindow = func() time.Duration {
		return desktopOutageEnvDuration("MACLAW_DESKTOP_OUTAGE_RUN_SEC", 12*time.Minute)
	}
	// desktopOutageBackoff is the gap before retry n (0-based); tests set it.
	desktopOutageBackoff = func(n int) time.Duration {
		d := 2 * time.Second << uint(n)
		if d > 10*time.Second || d <= 0 {
			d = 10 * time.Second
		}
		return d
	}
	// desktopOutageReconnect resets the route and asks Hub for the session
	// again (which opens an isolated bot's screen). Swapped in tests.
	desktopOutageReconnect = func(ctx context.Context, scope agentruntime.Scope) string {
		return desktopHealTransport(ctx, scope)
	}

	desktopOutageMu    sync.Mutex
	desktopOutageSpent = map[string]time.Duration{}
	desktopOutageSeen  = map[string]bool{}
)

// desktopOutageAllowed: this failure is an outage that may be waited out.
// A publish that already went out is never re-run.
func desktopOutageAllowed(scope agentruntime.Scope, action string, err error) bool {
	if desktopOutageDisabled() || !desktopOutageFailure(err) || strings.TrimSpace(scope.RunID) == "" {
		return false
	}
	if action == "publish" {
		if _, has := desktopPublishRecordOf(scope.RunID); has || desktopRunSubmittedUnconfirmed(scope) {
			return false
		}
	}
	desktopOutageMu.Lock()
	defer desktopOutageMu.Unlock()
	return desktopOutageSpent[scope.RunID] < desktopOutageRunWindow()
}

// desktopOutageNoteSpent records waiting time against the run's window.
func desktopOutageNoteSpent(runID string, d time.Duration) {
	desktopOutageMu.Lock()
	defer desktopOutageMu.Unlock()
	if len(desktopOutageSpent) > 4096 {
		desktopOutageSpent = map[string]time.Duration{}
		desktopOutageSeen = map[string]bool{}
	}
	desktopOutageSpent[runID] += d
	desktopOutageSeen[runID] = true
}

// desktopOutageTakeSeen reports (once) that this run went through an outage.
func desktopOutageTakeSeen(runID string) bool {
	desktopOutageMu.Lock()
	defer desktopOutageMu.Unlock()
	seen := desktopOutageSeen[runID]
	delete(desktopOutageSeen, runID)
	return seen
}

// desktopOutageWasSeen: this run waited for the desktop service and no step
// has succeeded since.
func desktopOutageWasSeen(runID string) bool {
	desktopOutageMu.Lock()
	defer desktopOutageMu.Unlock()
	return desktopOutageSeen[strings.TrimSpace(runID)]
}

// forgetDesktopOutage drops a finished run's bookkeeping.
func forgetDesktopOutage(runID string) {
	desktopOutageMu.Lock()
	defer desktopOutageMu.Unlock()
	delete(desktopOutageSpent, runID)
	delete(desktopOutageSeen, runID)
}

// desktopRideOutOutage waits for the desktop service and runs the step
// again until it gets past the outage or the window is spent. first is the
// failure that started the wait; run executes the step once.
func desktopRideOutOutage(ctx context.Context, scope agentruntime.Scope, action string, first error, run func() (string, error)) (string, error) {
	runID := strings.TrimSpace(scope.RunID)
	window := desktopOutageStepWindow()
	desktopOutageMu.Lock()
	if left := desktopOutageRunWindow() - desktopOutageSpent[runID]; left < window {
		window = left
	}
	desktopOutageMu.Unlock()
	started := time.Now()
	log.Printf("[desktop-outage] run=%s action=%s after %q: waiting up to %s for the desktop service", runID, action, desktopClipWhy(first.Error()), window.Round(time.Second))
	desktopEmitNote(scope, "desktop_outage_wait", "bot", desktopClipWhy(first.Error()))
	err := first
	out := ""
	for attempt := 0; ; attempt++ {
		elapsed := time.Since(started)
		if elapsed >= window || ctx.Err() != nil {
			break
		}
		pause := desktopOutageBackoff(attempt)
		if rest := window - elapsed; pause > rest {
			pause = rest
		}
		if pause > 0 {
			select {
			case <-time.After(pause):
			case <-ctx.Done():
			}
		}
		if ctx.Err() != nil {
			break
		}
		did := desktopOutageReconnect(ctx, scope)
		out, err = run()
		if err == nil || !desktopOutageFailure(err) {
			waited := time.Since(started)
			desktopOutageNoteSpent(runID, waited)
			log.Printf("[desktop-outage] run=%s action=%s back after %s (%d retries; %s)", runID, action, waited.Round(time.Second), attempt+1, did)
			if err == nil {
				out = fmt.Sprintf("(the desktop service was unavailable for about %ds — Hub restarting or this bot's screen still opening; nothing had run, so this step was run again now)\n", int(waited.Seconds())) + out
			}
			return out, err
		}
	}
	waited := time.Since(started)
	desktopOutageNoteSpent(runID, waited)
	desktopSideNoteCause(runID, desktopOutageCause(first))
	log.Printf("[desktop-outage] run=%s action=%s still unreachable after %s", runID, action, waited.Round(time.Second))
	return out, fmt.Errorf("%w (waited %ds for the desktop service to come back and retried; it is still unreachable, so this is the infrastructure, not your method)", err, int(waited.Seconds()))
}

// desktopLookActions only look at the screen or wait; they do no work.
var desktopLookActions = map[string]bool{"screenshot": true, "app_list": true, "window_list": true, "wait": true, "skill_list": true, "probe": true}

// desktopNoteWorkStep counts working steps that succeeded and keeps the last
// step's error, so the end of the run can tell "did nothing, stopped on a
// failure" from a finished job.
func desktopNoteWorkStep(scope agentruntime.Scope, action string, err error) {
	st := desktopRunFor(scope)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err != nil {
		st.lastErr = desktopRuneHead(err.Error(), 200)
		return
	}
	st.lastErr = ""
	if !desktopLookActions[strings.ToLower(strings.TrimSpace(action))] {
		st.workOK++
	}
}

// desktopOutageCause words an outage for the person.
func desktopOutageCause(err error) string {
	if desktopColdStart(err) {
		return "这台 bot 的电脑屏幕一直没能打开"
	}
	return "电脑服务（Hub）重启或断开，等了一阵仍连不上"
}

// desktopRunNeverWorked: the run executed desktop steps, none of them did
// work, and the last one failed. why names the failure in plain words.
func desktopRunNeverWorked(runID string) (string, bool) {
	st := desktopRunForID(runID)
	if st == nil {
		return "", false
	}
	st.mu.Lock()
	failed, work, last := st.lastFailed, st.workOK, st.lastErr
	st.mu.Unlock()
	if !failed || work > 0 {
		return "", false
	}
	if desktopOutageErrRe.MatchString(last) || strings.Contains(strings.ToLower(last), "desktop service is unreachable") {
		return "电脑服务一直连不上（Hub 重启或这台 bot 的屏幕没打开），一步实际工作都没做成", true
	}
	if last == "" {
		return "一步实际工作都没做成", true
	}
	return "一步实际工作都没做成，最后一步出错：" + desktopRuneHead(last, 80), true
}
