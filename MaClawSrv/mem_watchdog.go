package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"time"
)

// memWatchdogThresholdMB returns the heap size that triggers a profile dump.
func memWatchdogThresholdMB() uint64 {
	if v, err := strconv.Atoi(os.Getenv("MACLAW_MEM_DUMP_MB")); err == nil && v > 0 {
		return uint64(v)
	}
	return 800
}

// startMemWatchdog logs memory each minute and writes heap and goroutine
// profiles under dir the first time the heap passes each doubling of the
// threshold, so a leak leaves evidence before the host runs out of memory.
func startMemWatchdog(dir string) {
	if os.Getenv("MACLAW_MEM_WATCHDOG") == "off" {
		return
	}
	if limit, err := strconv.Atoi(os.Getenv("MACLAW_GO_MEMLIMIT_MB")); err == nil && limit > 0 {
		debug.SetMemoryLimit(int64(limit) << 20)
	}
	go func() {
		next := memWatchdogThresholdMB()
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		n := 0
		for range tick.C {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			heapMB := ms.HeapInuse >> 20
			n++
			if n%3 == 0 || heapMB >= next {
				srvLog().Info("[mem] stats", slog.Uint64("heap_inuse_mb", heapMB), slog.Uint64("sys_mb", ms.Sys>>20), slog.Int("goroutines", runtime.NumGoroutine()))
			}
			if heapMB >= next {
				memDumpProfiles(dir, heapMB)
				next *= 2
			}
		}
	}()
}

func memDumpProfiles(dir string, heapMB uint64) {
	_ = os.MkdirAll(dir, 0o700)
	stamp := time.Now().Format("20060102-150405")
	for _, name := range []string{"heap", "goroutine"} {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s-%dmb.pprof", name, stamp, heapMB))
		f, err := os.Create(path)
		if err != nil {
			continue
		}
		_ = pprof.Lookup(name).WriteTo(f, 0)
		_ = f.Close()
		srvLog().Warn("[mem] profile written", slog.String("path", path))
	}
}
