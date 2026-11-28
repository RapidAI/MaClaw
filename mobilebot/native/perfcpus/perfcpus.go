// Package perfcpus caps the voice library's Go threads to the phone's
// performance cores. It reads cpu_capacity and does not pin threads:
// sched_setaffinity on the init thread would not cover later Go Ms.
// Android places the fewer busy threads on the big cores once they have
// work. A missing tree or a homogeneous chip leaves GOMAXPROCS alone.
package perfcpus

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// cpuRoot is the sysfs CPU tree. Tests point it at a temp directory.
var cpuRoot = "/sys/devices/system/cpu"

// Apply sets GOMAXPROCS on Android when the big-core count is a useful cap.
// procs is that count, total is NumCPU, and applied is false when the
// runtime is left as it was.
func Apply() (procs, total int, applied bool) {
	return applyRoot(cpuRoot, runtime.GOOS == "android")
}

func applyRoot(root string, enabled bool) (procs, total int, applied bool) {
	total = runtime.NumCPU()
	if !enabled {
		return 0, total, false
	}
	caps, ok := ReadCapacities(root, total)
	if !ok {
		return 0, total, false
	}
	procs = PerformanceCount(caps, total)
	if procs < 2 || procs >= total {
		return 0, total, false
	}
	runtime.GOMAXPROCS(procs)
	return procs, total, true
}

// ReadCapacities reads cpu0..cpu{n-1}/cpu_capacity. One missing or
// unparsable file fails the whole read so a partial tree cannot pick a
// wrong thread count.
func ReadCapacities(root string, n int) ([]int, bool) {
	if n <= 0 {
		return nil, false
	}
	out := make([]int, n)
	for i := 0; i < n; i++ {
		b, err := os.ReadFile(filepath.Join(root, "cpu"+strconv.Itoa(i), "cpu_capacity"))
		if err != nil {
			return nil, false
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil || v < 0 {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// PerformanceCount returns how many Go procs fit the cores whose capacity
// is at least half the largest. 0 means do not change GOMAXPROCS: the list
// does not match numCPU, every core qualifies, or the only legal cap is
// the whole chip. A single qualifying core is raised to 2 when the chip
// has room, so one prime core does not serialize the runtime.
func PerformanceCount(capacities []int, numCPU int) int {
	if len(capacities) != numCPU || numCPU < 2 {
		return 0
	}
	max := 0
	for _, c := range capacities {
		if c > max {
			max = c
		}
	}
	if max <= 0 {
		return 0
	}
	n := 0
	for _, c := range capacities {
		if c > 0 && c*2 >= max {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	if n == 1 {
		n = 2
	}
	if n < 2 || n >= numCPU {
		return 0
	}
	return n
}
