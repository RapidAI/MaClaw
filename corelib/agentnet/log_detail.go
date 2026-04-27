package agentnet

import (
	"log"
	"sync/atomic"
)

var logDetailEnabled atomic.Bool

// SetLogDetailEnabled updates the in-process detailed log gate for agentnet.
func SetLogDetailEnabled(enabled bool) {
	logDetailEnabled.Store(enabled)
}

// logDetail logs a message only when detailed logging is enabled.
// Error-level messages (containing "failed", "error", etc.) should use
// log.Printf directly so they are always recorded.
func logDetail(format string, args ...interface{}) {
	if logDetailEnabled.Load() {
		log.Printf(format, args...)
	}
}
