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
      w.__MACLAW_INPUT_TRACK = { last: Date.now() };
      var upd = function(){ w.__MACLAW_INPUT_TRACK.last = Date.now(); };
      document.addEventListener('pointerdown', upd, true);
      document.addEventListener('keydown', upd, true);
      document.addEventListener('wheel', upd, true);
      document.addEventListener('mousemove', upd, true);
    }
    fetch('/maclaw-boot/ping?stage=input&ts=' + w.__MACLAW_INPUT_TRACK.last);
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
		offenses := 0
		reloads := 0
		var lastAction time.Time
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
