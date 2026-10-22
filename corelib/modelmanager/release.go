package modelmanager

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Memory release shared by every model host (guiapp, maclawsrv, model-serve).
//
// Runtimes with their own scratch arenas (e.g. corelib/onnxrt) register a
// releaser; ReleaseMemory drops those arenas and returns freed heap pages to
// the OS so RSS actually falls after an inference or an unload.

var (
	releaseMu sync.Mutex
	releasers []func()
)

// RegisterReleaser adds a function that drops cached scratch memory.
func RegisterReleaser(f func()) {
	if f == nil {
		return
	}
	releaseMu.Lock()
	releasers = append(releasers, f)
	releaseMu.Unlock()
}

// ReleaseMemory runs every registered releaser, then debug.FreeOSMemory.
// Set MACLAW_MODEL_FREE_OS_MEMORY=off to skip the FreeOSMemory step.
func ReleaseMemory() {
	releaseMu.Lock()
	fs := append([]func(){}, releasers...)
	releaseMu.Unlock()
	for _, f := range fs {
		f()
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_MODEL_FREE_OS_MEMORY"))); v == "off" || v == "0" || v == "false" {
		return
	}
	debug.FreeOSMemory()
}

// releaseAfterUnload is replaced in tests.
var releaseAfterUnload = func() { go ReleaseMemory() }

// IdleUnloadDelay returns def unless MACLAW_MODEL_IDLE_UNLOAD_SEC overrides it
// (seconds; 0 disables idle unload).
func IdleUnloadDelay(def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv("MACLAW_MODEL_IDLE_UNLOAD_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}
