package main

// Startup maintenance for the tool-result spill store (corelib/toolresult).
//
// Context checkpoints (on by default) and oversized tool results spill full
// payloads to maclawpath.ToolResultsDir() as read-back handles. Without a
// retention pass the directory grows unboundedly — long-running tasks create
// one checkpoint handle per compaction event. Handles older than the
// retention window can no longer belong to an active session, so pruning them
// only removes lossless read-back for very old conversations.

import (
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

// toolResultHandleRetention is how long spilled handles are kept. 14 days
// covers any realistically resumable session while bounding disk growth.
const toolResultHandleRetention = 14 * 24 * time.Hour

// maybePruneToolResultsOnStartup prunes stale tool-result handles shortly
// after startup, off the critical path (mirrors the tool-cache maintenance
// pattern: delayed goroutine, cancellable via a.ctx).
func (a *App) maybePruneToolResultsOnStartup() {
	// Shared "running under go test without an isolated home" guard (same
	// heuristic as tool-cache maintenance): without it, App tests would prune
	// the developer's real handle store 60s after the test binary starts.
	if a.isToolCacheMaintenanceSuppressedForTest() {
		return
	}
	go func() {
		timer := time.NewTimer(60 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-a.ctx.Done():
			return
		}
		result, err := toolresult.PruneOlderThan("", toolResultHandleRetention)
		if err != nil {
			log.Printf("[toolresult] startup prune failed: %v", err)
			return
		}
		if result.RemovedFiles > 0 {
			log.Printf("[toolresult] startup prune removed %d handles (freed=%d bytes, dirs=%d)",
				result.RemovedFiles, result.FreedBytes, result.RemovedDirs)
		}
	}()
}
