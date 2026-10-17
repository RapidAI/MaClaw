package app

import (
	"log"
	"runtime/debug"
)

// defaultSoftMemoryLimitMB is sized for an 8GB HubCenter host. The Go heap
// is capped so the process leaves RAM for the kernel; the incident that
// froze the machine was the kernel stalling while it tore down a multi-GB
// cgroup page cache after the process was killed, not the Go heap alone.
const defaultSoftMemoryLimitMB = 1536

// applySoftMemoryLimit sets the runtime soft memory limit. mb == 0 uses the
// default. mb < 0 leaves the limit unset. The previous limit is returned.
func applySoftMemoryLimit(mb int) int64 {
	if mb == 0 {
		mb = defaultSoftMemoryLimitMB
	}
	if mb < 0 {
		log.Printf("[hubcenter] soft memory limit disabled")
		return debug.SetMemoryLimit(-1)
	}
	limit := int64(mb) << 20
	prev := debug.SetMemoryLimit(limit)
	log.Printf("[hubcenter] soft memory limit set to %d MiB", mb)
	return prev
}
