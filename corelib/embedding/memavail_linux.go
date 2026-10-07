//go:build linux

package embedding

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// availableMemory returns MemAvailable from /proc/meminfo, capped by the
// cgroup v2/v1 memory limit headroom when the process runs in a container.
func availableMemory() (uint64, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var avail uint64
	ok := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemAvailable:") {
			fs := strings.Fields(line)
			if len(fs) >= 2 {
				if kb, err := strconv.ParseUint(fs[1], 10, 64); err == nil {
					avail, ok = kb*1024, true
				}
			}
			break
		}
	}
	if !ok {
		return 0, false
	}
	if lim, used, cg := cgroupMemory(); cg && lim > used && lim-used < avail {
		avail = lim - used
	}
	return avail, true
}

func cgroupMemory() (limit, used uint64, ok bool) {
	read := func(p string) (uint64, bool) {
		b, err := os.ReadFile(p)
		if err != nil {
			return 0, false
		}
		s := strings.TrimSpace(string(b))
		if s == "max" || s == "" {
			return 0, false
		}
		v, err := strconv.ParseUint(s, 10, 64)
		return v, err == nil && v < 1<<60
	}
	if l, ok1 := read("/sys/fs/cgroup/memory.max"); ok1 {
		if u, ok2 := read("/sys/fs/cgroup/memory.current"); ok2 {
			return l, u, true
		}
	}
	if l, ok1 := read("/sys/fs/cgroup/memory/memory.limit_in_bytes"); ok1 {
		if u, ok2 := read("/sys/fs/cgroup/memory/memory.usage_in_bytes"); ok2 {
			return l, u, true
		}
	}
	return 0, 0, false
}
