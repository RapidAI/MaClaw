package guiapp

import (
	"fmt"
	"log"
	"os"
	"runtime"
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
	}
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
