package guiapp

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Runtime recovery for the "page renders but user input dies" WebView2 failure
// mode (observed 2026-10-06: JS heartbeat kept ticking, all mouse/keyboard
// events were silently dropped, and only a minimize/restore cycle fixed it).
// The boot-time watches (watchFrontendPaint / watchEnvCheckPromote) cannot see
// this failure because the render heartbeat keeps running; nothing else
// watches input liveness.
//
// Two detectors share the nudge -> reload -> escalate ladder:
//   - Idle-based (slow path): requires minutes of continuous active-foreground
//     tenure before the first nudge.
//   - Click-echo (fast path, frontend_click_echo_windows.go): a WH_MOUSE_LL
//     hook counts clicks targeted at the main window while the page reports
//     its own input promptly (throttled reporter in the agent below). Clicks
//     that reach the HWND but never echo back are direct dead-pipeline
//     evidence, so recovery fires within seconds and cannot false-positive on
//     a healthy page (every click echoes back immediately there).
//
// Mechanism: WindowExecJS injects a self-installing document-level input
// tracker (idempotent, survives reloads by re-installing). It reports the
// timestamp of the most recent user input event through the existing
// /maclaw-boot/ping channel (stage=input). The watchdog combines that with
// system-wide input activity (GetLastInputInfo) and foreground-window state:
// dead input requires this window to have been foreground AND the user actively
// inputting somewhere, continuously for inputDeadAfter, yet the page has seen
// no input events for inputDeadAfter.
//
// The continuous active-foreground requirement is the load-bearing guard
// against the false positive that made the window visibly minimise itself in
// normal use: a 30s-tick instantaneous foreground check fired the nudge
// whenever the user switched back to MaClaw after a couple of minutes
// elsewhere — the focusing click lands on the native frameless drag region,
// produces no DOM event, and the tick's stale sysIdle (input to the previous
// app) completed the "dead" signature on a perfectly healthy page. Sampling
// the tenure once per second with the user-active condition folded in closes
// both the obvious variant (any app switch resets the tenure) and the subtle
// one (a sub-second alt-tab blip a 1s sampler can miss, after minutes of
// parked-mouse reading, no longer counts as active tenure either). Only input
// demonstrably routed to this window for minutes without reaching the page
// counts as dead. Recovery escalates nudge (minimize/restore/foreground,
// proven fix) -> reload -> repeated escalation logs.

const (
	inputWatchTick   = 30 * time.Second
	inputWatchSample = 1 * time.Second  // cheap foreground continuity sampling
	inputDeadAfter   = 2 * time.Minute  // page input silence that counts as dead
	inputSysIdleMax  = 45 * time.Second // user counts as "active" while system-wide idle is below this
	inputNudgeGap    = 90 * time.Second
	inputEscalateGap = 10 * time.Minute
)

// frontendInputAgentJS installs (once per document) a passive input tracker
// and reports the last user-input timestamp via the boot-ping channel. It runs
// on every watchdog tick so a reload re-installs it automatically; the fetch
// itself also proves the JS event loop is alive, cleanly separating "JS alive"
// from "input alive".
const frontendInputAgentJS = `(function(){
  try {
    var w = window;
    if (!w.__MACLAW_INPUT_TRACK) {
      w.__MACLAW_INPUT_TRACK = { last: Date.now(), reported: 0, evts: [] };
      var upd = function(kind){
        var t = w.__MACLAW_INPUT_TRACK;
        var now = Date.now();
        t.last = now;
        // Recent-event FIFO for the click-latency matcher: pairing these
        // client timestamps against native click timestamps measures how late
        // input delivery is, not just whether it stopped. Events are typed so
        // the matcher can compare pointer events (clicks) against native
        // clicks; mouse movement must not inflate click latency.
        t.evts.push(kind + ':' + now);
        if (t.evts.length > 8) t.evts.shift();
        // Prompt echo for the click-echo watchdog: report within ~1s instead
        // of waiting for the next 30s tick injection. Throttled so a moving
        // mouse cannot flood the ping channel.
        if (now - t.reported > 1000) {
          t.reported = now;
          fetch('/maclaw-boot/ping?stage=input&ts=' + now + '&evts=' + t.evts.join(','));
        }
      };
      document.addEventListener('pointerdown', function(){ upd('p'); }, true);
      document.addEventListener('keydown', function(){ upd('k'); }, true);
      document.addEventListener('wheel', function(){ upd('w'); }, true);
      document.addEventListener('mousemove', function(){ upd('m'); }, true);
    }
    fetch('/maclaw-boot/ping?stage=input&ts=' + w.__MACLAW_INPUT_TRACK.last + '&evts=' + w.__MACLAW_INPUT_TRACK.evts.join(','));
  } catch (e) {}
})();`

// frontendLastInputAt is the unix-millisecond timestamp of the most recent
// user input event the page reported. 0 = no report yet (skip decisions).
var frontendLastInputAt atomic.Int64

type inputWatchAction int

const (
	inputWatchNone inputWatchAction = iota
	inputWatchNudge
	inputWatchReload
	inputWatchEscalate
)

// noteFrontendInputEvent records a page-reported input timestamp. Called from
// the asset middleware for boot-ping stage=input. Stale or clock-skewed
// timestamps are clamped to "just now" so a broken reporter cannot fake a
// permanently-fresh or permanently-dead page.
func noteFrontendInputEvent(ts int64) {
	now := time.Now().UnixMilli()
	if ts <= 0 || ts > now+5000 {
		ts = now
	}
	for {
		old := frontendLastInputAt.Load()
		if ts <= old || frontendLastInputAt.CompareAndSwap(old, ts) {
			return
		}
	}
}

// inputWatchDecision is the pure policy core of the input watchdog. Input is
// considered dead only when ALL of these hold: the platform probe is
// supported, the MaClaw window has been foreground continuously for at least
// inputDeadAfter (foregroundFor — already sampled with the user-active
// condition folded in, so transient focus gains and parked-mouse idling reset
// it), the user is still actively producing input at decision time (sysIdle
// small), and the page has seen no input events for inputDeadAfter (pageIdle
// large). Any violated condition resets the offense streak. Escalation ladder:
// 1st offense nudge; one reload once inputNudgeGap has passed since the nudge
// (tracked by reloads, NOT the offense count — the 30s tick is shorter than
// the gap, so an offense-numbered branch would be unreachable); afterwards
// escalate (log loudly + nudge, once per inputEscalateGap). Returns the action
// plus updated offense/reload counters.
func inputWatchDecision(pageIdle, sysIdle, foregroundFor time.Duration, supported bool, offenses, reloads int, sinceLastAction time.Duration) (inputWatchAction, int, int) {
	if !supported || foregroundFor <= inputDeadAfter || pageIdle <= inputDeadAfter || sysIdle >= inputSysIdleMax {
		return inputWatchNone, 0, 0
	}
	offenses++
	switch {
	case offenses == 1:
		return inputWatchNudge, offenses, reloads
	case offenses >= 2 && reloads == 0 && sinceLastAction >= inputNudgeGap:
		// Reload exactly once, at the first tick whose gap since the nudge
		// clears inputNudgeGap (regardless of how far offenses has climbed
		// while holding).
		return inputWatchReload, offenses, reloads + 1
	default:
		if offenses >= 3 && sinceLastAction >= inputEscalateGap {
			return inputWatchEscalate, offenses, reloads
		}
		return inputWatchNone, offenses, reloads
	}
}

// clickEchoWindow is how far back the fast path looks for click evidence;
// clickEchoMinClicks clicks inside it targeted at the main window with no page
// echo at all is treated as a dead input pipeline.
const (
	clickEchoWindow    = 10 * time.Second
	clickEchoMinClicks = 3
	// clickLatencyAlert is matched click-to-page delivery latency that counts
	// as dead-pipeline evidence on its own (delayed-delivery variant).
	clickLatencyAlert = 3 * time.Second
	// clickLatencySampleWindow bounds both the latency evidence and the
	// periodic elevated-latency log line.
	clickLatencySampleWindow = 2 * time.Minute
)

// inputWatchLadder is the shared recovery escalation: first detection nudges,
// the next fires the single reload once the nudge gap has passed, then
// escalate (log loudly + nudge) once per inputEscalateGap.
func inputWatchLadder(actions, reloads int, sinceLastAction time.Duration) (inputWatchAction, int, int) {
	actions++
	switch {
	case actions == 1:
		return inputWatchNudge, actions, reloads
	case actions >= 2 && reloads == 0 && sinceLastAction >= inputNudgeGap:
		return inputWatchReload, actions, reloads + 1
	default:
		if actions >= 3 && sinceLastAction >= inputEscalateGap {
			return inputWatchEscalate, actions, reloads
		}
		return inputWatchNone, actions, reloads
	}
}

// clickEchoEvidence reports whether the dropped-input signature holds: at
// least clickEchoMinClicks native clicks on the main window inside the window,
// and page silence at least as old as the oldest of them. On a healthy page
// every click echoes back within ~1s, so a small pageIdle clears the detector.
func clickEchoEvidence(clicks int, oldestClickAge, pageIdle time.Duration) bool {
	return clicks >= clickEchoMinClicks && oldestClickAge > 0 && pageIdle >= oldestClickAge
}

// clickEchoDecision is the pure policy core of the fast path, fed either
// clickEchoEvidence or a latency sample at/above clickLatencyAlert.
func clickEchoDecision(deadEvidence, supported bool, actions, reloads int, sinceLastAction time.Duration) (inputWatchAction, int, int) {
	if !supported || !deadEvidence {
		return inputWatchNone, 0, 0
	}
	return inputWatchLadder(actions, reloads, sinceLastAction)
}

// watchFrontendInput starts the runtime input-liveness watchdog goroutine.
// Active-foreground tenure is sampled cheaply every inputWatchSample so the
// dead-input signature can require an uninterrupted foreground+user-active
// stay; the page-side checks and recovery actions keep the inputWatchTick
// cadence.
func (a *App) watchFrontendInput() {
	if a == nil {
		return
	}
	go func() {
		bootLog("watchFrontendInput goroutine start ctx_nil=%v", a.ctx == nil)
		startClickEchoMonitor()
		offenses := 0
		reloads := 0
		var lastAction time.Time
		fastActions := 0
		fastReloads := 0
		var lastFastAction time.Time
		var lastLatencyLog time.Time
		lastTick := time.Now()
		var activeForegroundSince time.Time // zero = not (foreground + user-active) now
		for {
			time.Sleep(inputWatchSample)
			if !frontendInputWatchSupported() {
				return
			}
			if a.ctx == nil {
				continue
			}
			// Fast path: native clicks on the main window that never echo back
			// to the page. Runs at sampler cadence so a dead pipeline is
			// detected within seconds of the user starting to click.
			if clickEchoMonitorRunning() && a.frontendHTMLReady.Load() && !computerUseSessionActive() {
				clickEchoEnsureHWND()
				clicks := recentWindowClicks(clickEchoWindow)
				if clicks == 0 {
					// No click evidence at all: a new dead-input episode starts
					// from nudge, not from whatever a previous episode reached
					// (the reload/escalate ladder must not carry across
					// episodes separated by a working page).
					fastActions, fastReloads = 0, 0
				}
				latency := recentMaxClickLatency(clickLatencySampleWindow)
				if latency > 500*time.Millisecond && time.Since(lastLatencyLog) > time.Minute {
					lastLatencyLog = time.Now()
					log.Printf("[input-watch] click latency elevated max=%s clicks=%d; monitoring", latency.Round(time.Second), clicks)
					bootLog("input watchdog click latency elevated max=%s clicks=%d", latency.Round(time.Second), clicks)
				}
				stored := frontendLastInputAt.Load()
				deadEvidence := latency >= clickLatencyAlert
				var oldestAge time.Duration
				var pageIdle time.Duration
				if stored != 0 {
					pageIdle = time.Since(time.UnixMilli(stored))
					oldestAge = oldestWindowClickAge(clickEchoWindow)
					deadEvidence = deadEvidence || clickEchoEvidence(clicks, oldestAge, pageIdle)
				}
				if deadEvidence && isMaclawWindowForeground() {
					action, n, r := clickEchoDecision(deadEvidence, true, fastActions, fastReloads, time.Since(lastFastAction))
					fastActions, fastReloads = n, r
					switch action {
					case inputWatchNudge:
						lastFastAction = time.Now()
						log.Printf("[input-watch] input pipeline dead (latency=%s clicks=%d pageIdle=%s); nudging window", latency.Round(time.Millisecond), clicks, pageIdle.Round(time.Second))
						bootLog("input watchdog click-echo nudge latency=%s clicks=%d pageIdle=%s", latency.Round(time.Millisecond), clicks, pageIdle.Round(time.Second))
						captureMainWindowPNG("input-echo-nudge")
						nudgeMainWindowInput()
					case inputWatchReload:
						lastFastAction = time.Now()
						log.Printf("[input-watch] input still dead after nudge; reloading frontend")
						bootLog("input watchdog click-echo reload")
						captureMainWindowPNG("input-echo-reload")
						runtime.WindowExecJS(a.ctx, "window.location.reload();")
						frontendLastInputAt.Store(time.Now().UnixMilli())
					case inputWatchEscalate:
						lastFastAction = time.Now()
						log.Printf("[input-watch] input still dead after nudge+reload (actions=%d); manual GUI restart may be required", fastActions)
						bootLog("input watchdog click-echo escalate actions=%d", fastActions)
						captureMainWindowPNG("input-echo-escalate")
						nudgeMainWindowInput()
					}
				}
			}
			// Active-foreground tenure: a 1s sample only counts when the main
			// window is foreground AND the user just produced input system-wide.
			// Parked-mouse reading never accumulates it; continuous typing into
			// the window does — the two signatures we must tell apart.
			if isMaclawWindowForeground() && systemInputIdleDuration() < inputSysIdleMax {
				if activeForegroundSince.IsZero() {
					activeForegroundSince = time.Now()
				}
			} else {
				activeForegroundSince = time.Time{}
			}
			if time.Since(lastTick) < inputWatchTick {
				continue
			}
			lastTick = time.Now()
			if !a.frontendHTMLReady.Load() {
				continue
			}
			foregroundFor := time.Duration(0)
			if !activeForegroundSince.IsZero() {
				foregroundFor = time.Since(activeForegroundSince)
			}
			// Self-healing injection: also refreshes the report each tick.
			runtime.WindowExecJS(a.ctx, frontendInputAgentJS)
			stored := frontendLastInputAt.Load()
			if stored == 0 {
				continue // first report not in yet
			}
			pageIdle := time.Since(time.UnixMilli(stored))
			sysIdle := systemInputIdleDuration()
			// Skip while a computer-use session is driving the desktop: its
			// synthetic input keeps sysIdle small and can hover windows other
			// than the page, mimicking the dead-input signature.
			if computerUseSessionActive() {
				continue
			}
			action, n, r := inputWatchDecision(pageIdle, sysIdle, foregroundFor, true, offenses, reloads, time.Since(lastAction))
			offenses, reloads = n, r
			switch action {
			case inputWatchNudge:
				lastAction = time.Now()
				log.Printf("[input-watch] frontend input stalled (pageIdle=%s sysIdle=%s foregroundFor=%s); nudging window", pageIdle.Round(time.Second), sysIdle.Round(time.Second), foregroundFor.Round(time.Second))
				bootLog("input watchdog nudge offenses=%d pageIdle=%s sysIdle=%s foregroundFor=%s", offenses, pageIdle.Round(time.Second), sysIdle.Round(time.Second), foregroundFor.Round(time.Second))
				captureMainWindowPNG("input-dead-nudge")
				nudgeMainWindowInput()
			case inputWatchReload:
				lastAction = time.Now()
				log.Printf("[input-watch] input still stalled after nudge; reloading frontend")
				bootLog("input watchdog reload offenses=%d", offenses)
				captureMainWindowPNG("input-dead-reload")
				runtime.WindowExecJS(a.ctx, "window.location.reload();")
				// Fresh grace window until the reloaded page reports again.
				frontendLastInputAt.Store(time.Now().UnixMilli())
			case inputWatchEscalate:
				lastAction = time.Now()
				log.Printf("[input-watch] input pipeline still dead after nudge+reload (offenses=%d); manual GUI restart may be required", offenses)
				bootLog("input watchdog escalate offenses=%d", offenses)
				captureMainWindowPNG("input-dead-escalate")
				nudgeMainWindowInput()
			}
		}
	}()
}
