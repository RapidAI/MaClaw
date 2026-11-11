package guiapp

import (
	"log"
	"os"
	goruntime "runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
)

// Idle memory release.
//
// Every modelmanager unload already runs ReleaseMemory, which drops the
// registered scratch arenas and returns freed heap pages to the OS. What that
// does not cover is the transient garbage the Go GC caches in the heap while
// the app works — agent loop buffers, IM payloads, UI-bound strings — and
// which inflates RSS for hours after the work finished. This releaser runs a
// single release pass once the app has been idle for idleMemIdleFor, so the
// resident set actually falls when the user walks away instead of only when
// a model unloads.
//
// Disable with MACLAW_IDLE_MEM_RELEASE=off.
const (
	idleMemCheckInterval = 60 * time.Second
	idleMemIdleFor       = 10 * time.Minute
)

var idleMemReleaseOnce sync.Once

// applyMemoryLimit honors MACLAW_GO_MEMLIMIT_MB, the same knob MaClawSrv
// uses: a soft Go heap cap so the GC collects earlier instead of letting the
// heap double toward its default target on a memory-heavy desktop session.
// Unset or non-positive leaves the runtime default.
func applyMemoryLimit() {
	mb, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_GO_MEMLIMIT_MB")))
	if err != nil || mb <= 0 {
		return
	}
	debug.SetMemoryLimit(int64(mb) << 20)
	log.Printf("[mem] soft heap limit set to %d MiB", mb)
}

// startIdleMemoryReleaser begins the idle-release loop. Safe to call once at
// startup; repeated calls are no-ops.
func startIdleMemoryReleaser() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_IDLE_MEM_RELEASE"))) {
	case "off", "0", "false", "no":
		return
	}
	idleMemReleaseOnce.Do(func() {
		go idleMemoryReleaseLoop()
	})
}

func idleMemoryReleaseLoop() {
	ticker := time.NewTicker(idleMemCheckInterval)
	defer ticker.Stop()
	var idleSince time.Time
	releasedThisIdle := false
	for range ticker.C {
		if globalLLMScheduler.anyLLMBusy() {
			idleSince = time.Time{}
			releasedThisIdle = false
			continue
		}
		if idleSince.IsZero() {
			idleSince = time.Now()
			continue
		}
		if releasedThisIdle || time.Since(idleSince) < idleMemIdleFor {
			continue
		}
		releasedThisIdle = true
		var before, after goruntime.MemStats
		goruntime.ReadMemStats(&before)
		t0 := time.Now()
		modelmanager.ReleaseMemory()
		goruntime.ReadMemStats(&after)
		log.Printf("[mem] idle release: heap=%dMiB->%dMiB sys=%dMiB->%dMiB in %v",
			bytesToMiB(before.HeapAlloc), bytesToMiB(after.HeapAlloc),
			bytesToMiB(before.Sys), bytesToMiB(after.Sys), time.Since(t0))
	}
}
