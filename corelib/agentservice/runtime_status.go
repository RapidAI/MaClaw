package agentservice

import (
	"strings"
	"sync"
)

// Per-run live status for a model retry or a real route change. The desktop
// status line reads it. A chat row is not written here. The key is the run,
// so a parallel run cannot replace another run's notice.

var runRuntimeStatus sync.Map

// SetRunRuntimeStatus stores the visible sentence for one run.
// An empty sentence clears it.
func SetRunRuntimeStatus(runID, text string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		runRuntimeStatus.Delete(runID)
		return
	}
	runRuntimeStatus.Store(runID, text)
}

// RunRuntimeStatus is the sentence last recorded for this run.
func RunRuntimeStatus(runID string) string {
	value, ok := runRuntimeStatus.Load(strings.TrimSpace(runID))
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

// ClearRunRuntimeStatus drops one run's notice. Another run is left as it is.
func ClearRunRuntimeStatus(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	runRuntimeStatus.Delete(runID)
}
