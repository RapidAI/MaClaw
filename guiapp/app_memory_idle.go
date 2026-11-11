package guiapp

import (
	"log"
	"os"
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
		t0 := time.Now()
		modelmanager.ReleaseMemory()
		log.Printf("[mem] idle release: returned freed heap to OS in %v", time.Since(t0))
	}
}
