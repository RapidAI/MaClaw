package guiapp

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Startup and runtime performance instrumentation.
//
// This exists because low-end hardware reports ("app is unusably slow after
// launch") cannot be diagnosed from the existing logs: those record what the
// process did, not when the user could first see or touch the UI. The marks
// below pin down the user-visible moments plus the cost of the WebView that
// renders them, so a slow machine can be compared against a fast one on the
// same scale.
//
// Output goes to the normal application log with a [perf] prefix. The one-shot
// startup marks are always recorded because they are what makes a user bug
// report actionable. The periodic sampler is separate and can be disabled.
//
// MACLAW_PERF_PROBE=off disables the periodic sampler; =verbose adds per-stage
// detail.
const perfProbeEnvKey = "MACLAW_PERF_PROBE"

var (
	perfProbeVerbose  atomic.Bool
	perfProbeDisabled atomic.Bool
	perfProbeOnce     sync.Once
)

func initPerfProbe() {
	perfProbeOnce.Do(func() {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(perfProbeEnvKey))) {
		case "off", "0", "false", "no":
			perfProbeDisabled.Store(true)
		case "verbose", "2", "all":
			perfProbeVerbose.Store(true)
		}
	})
}

// perfProbeEnabled reports whether periodic sampling should run.
func perfProbeEnabled() bool {
	initPerfProbe()
	return !perfProbeDisabled.Load()
}

func perfProbeVerboseEnabled() bool {
	initPerfProbe()
	return perfProbeVerbose.Load()
}

// Startup timeline. A zero value means "not reached"; every recorded value is
// measured from processStartMark so the marks share one origin.
var (
	processStartMark = time.Now()
	startupDoneMark  atomic.Int64 // App.startup returned
	htmlReadyMark    atomic.Int64 // index.html boot-ping
	reactReadyMark   atomic.Int64 // main.tsx evaluated
	domReadyMark     atomic.Int64 // Wails OnDomReady
	firstPaintMark   atomic.Int64 // renderer reported first contentful paint
	interactiveMark  atomic.Int64 // renderer reported a usable UI
)

func perfMarkElapsed() time.Duration {
	return time.Since(processStartMark)
}

// perfMarkStartupDone records the end of App.startup.
func perfMarkStartupDone() {
	if startupDoneMark.CompareAndSwap(0, int64(perfMarkElapsed())) {
		logPerfTimeline("startup-done")
		scheduleSteadyStateMemoryReport()
	}
}

// scheduleSteadyStateMemoryReport logs the heap once startup has settled.
//
// Every existing memory snapshot is taken during startup, which is exactly when
// the numbers are least representative: a lazy gateway or model load can peak
// and release. What a low-memory machine cares about is the number it has to
// live with, so this reports again after the burst is over.
func scheduleSteadyStateMemoryReport() {
	go func() {
		time.Sleep(90 * time.Second)
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		// Deliberately does not force a collection. A one-shot FreeOSMemory only
		// returned ~286MiB against a multi-gigabyte working set, while costing a
		// stop-the-world pause; the memory is live data (intent heads, model
		// weights), not garbage the GC was sitting on.
		bootLog("[perf] steady-state memory heap=%dMiB sys=%dMiB objects=%d goroutines=%d gc=%d",
			before.HeapAlloc/1048576, before.Sys/1048576, before.HeapObjects, runtime.NumGoroutine(), before.NumGC)
		writeHeapProfileOnRequest()
	}()
}

func perfMarkHTMLReady() {
	if htmlReadyMark.CompareAndSwap(0, int64(perfMarkElapsed())) {
		logPerfTimeline("html-ready")
	}
}

func perfMarkReactReady() {
	if reactReadyMark.CompareAndSwap(0, int64(perfMarkElapsed())) {
		logPerfTimeline("react-ready")
	}
}

func perfMarkDOMReady() {
	if domReadyMark.CompareAndSwap(0, int64(perfMarkElapsed())) {
		logPerfTimeline("dom-ready")
	}
}

// perfMarkFirstPaint records the first frame the user could see. Called from
// the frontend through LogFrontendDiagnostic so the timestamp reflects the
// renderer's own paint rather than a backend guess.
func perfMarkFirstPaint() {
	if firstPaintMark.CompareAndSwap(0, int64(perfMarkElapsed())) {
		logPerfTimeline("first-paint")
	}
}

// perfMarkInteractive records the moment the UI became usable. This is the
// number that matters for "the app is slow to start": everything before it is
// a splash screen.
func perfMarkInteractive() {
	if interactiveMark.CompareAndSwap(0, int64(perfMarkElapsed())) {
		logPerfTimeline("interactive")
		logPerfSummary()
	}
}

// logPerfTimeline emits one line per milestone. Marks not yet reached are
// omitted rather than reported as zero, so a startup that dies before first
// paint still leaves a readable trail.
//
// The line is mirrored through bootLog as well: marks that arrive from the
// renderer (first-paint, interactive) run on Wails' IPC goroutine, where the
// shared log.Printf sink has been observed to swallow the line while sibling
// lines from the same binding still reach the file. bootLog owns a separate
// file handle, so the milestone is never lost.
func logPerfTimeline(stage string) {
	line := fmt.Sprintf("[perf] mark=%s at=%s", stage, perfMarkElapsed().Round(time.Millisecond))
	log.Printf("%s", line)
	bootLog("%s", line)
}

// waitForFirstPaintOrTimeout blocks until the renderer reports its first
// painted frame, or until limit elapses.
//
// Backend startup runs on its own goroutine and therefore does not block the
// window, but on a low-core machine it competes with the renderer for CPU and
// disk while the user is waiting for the first frame. Yielding the heavy init
// work for that window measurably shortens time-to-first-paint.
//
// It waits for the first paint rather than a fully interactive UI: the paint is
// the last moment where stealing CPU is visible, and waiting longer only delays
// the AI stack (which gates cloud task restore) for no perceptual gain.
//
// The wait is always bounded, so a renderer that never reports cannot stall
// startup. It intentionally does not depend on MACLAW_PERF_PROBE: that switch
// turns off sampling, not a startup behaviour change, and coupling them meant
// disabling diagnostics also silently disabled the first-paint protection.
func waitForFirstPaintOrTimeout(limit time.Duration) {
	if limit <= 0 {
		return
	}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if firstPaintMark.Load() > 0 || interactiveMark.Load() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	bootLog("startup yield: first paint not reported within %s, continuing", limit)
}

// logPerfSummary emits the single line most bug reports will need: how long
// the user actually waited, plus rough machine capability.
func logPerfSummary() {
	marks := []struct {
		name string
		at   *atomic.Int64
	}{
		{"startup-done", &startupDoneMark},
		{"html-ready", &htmlReadyMark},
		{"react-ready", &reactReadyMark},
		{"dom-ready", &domReadyMark},
		{"first-paint", &firstPaintMark},
		{"interactive", &interactiveMark},
	}
	parts := make([]string, 0, len(marks))
	for _, mark := range marks {
		if v := mark.at.Load(); v > 0 {
			parts = append(parts, mark.name+"="+time.Duration(v).Round(time.Millisecond).String())
		}
	}
	log.Printf("[perf] summary stages=%s cpus=%d go=%s",
		strings.Join(parts, ","), runtime.NumCPU(), runtime.Version())
	bootLog("[perf] summary stages=%s", strings.Join(parts, ","))
}

// logStartupPhase reports one startup phase on both sinks.
//
// The maclaw.log sink passes through detailAwareLogWriter, whose gate only lets
// known keywords reach disk. Adjacent lines from the same goroutine have been
// observed to diverge there (one written, the next dropped), so phase lines are
// marked with the err= keyword that the gate always admits and mirrored through
// bootLog, which owns a separate file handle.
func logStartupPhase(name string, started, startupBegin time.Time) {
	line := fmt.Sprintf("[startup] %s err=none took=%s at=%s",
		name, time.Since(started).Round(time.Millisecond), time.Since(startupBegin).Round(time.Millisecond))
	log.Printf("%s", line)
	bootLog("[perf] %s", line)
}

// writeHeapProfileOnRequest dumps a heap profile when MACLAW_HEAP_PROFILE=1.
//
// The steady-state heap is a gigabyte on a typical machine, and no existing log
// says what holds it. A profile is the only way to attribute that, and asking a
// user to reproduce with an env var is far cheaper than guessing. The file is
// written next to the other diagnostics so a bug report can carry it.
func writeHeapProfileOnRequest() {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_HEAP_PROFILE"))); v != "1" && v != "true" && v != "yes" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".maclaw", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, "heap.pprof")
	f, err := os.Create(path)
	if err != nil {
		bootLog("[perf] heap profile create failed: %v", err)
		return
	}
	defer f.Close()
	// A fresh collection makes the profile describe live data, not garbage.
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		bootLog("[perf] heap profile write failed: %v", err)
		return
	}
	bootLog("[perf] heap profile written path=%q", path)
}
