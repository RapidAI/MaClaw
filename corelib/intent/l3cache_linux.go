//go:build linux

package intent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	l3Once  sync.Once
	l3Bytes int64
)

// l3CacheBytes reports the last-level (L3) cache visible to one core: the
// largest single LLC domain, not the sum. Multi-CCX parts expose several
// domains in sysfs, but a prediction runs on one core and only gets one.
// Returns 0 when sysfs is unavailable.
func l3CacheBytes() int64 {
	l3Once.Do(func() {
		l3Bytes = queryL3()
	})
	return l3Bytes
}

func queryL3() int64 {
	const root = "/sys/devices/system/cpu"
	per, err := os.ReadFile(filepath.Join(root, "cpu0/cache/index3/size"))
	if err != nil {
		return 0
	}
	size := parseLinuxCacheSize(strings.TrimSpace(string(per)))
	if size <= 0 {
		return 0
	}
	// Domains can differ in size on heterogeneous parts; keep the largest one,
	// which is the amount a core in the best domain can use.
	largest := size
	entries, _ := filepath.Glob(filepath.Join(root, "cpu[0-9]*/cache/index3/size"))
	for _, e := range entries {
		b, err := os.ReadFile(e)
		if err != nil {
			continue
		}
		if n := parseLinuxCacheSize(strings.TrimSpace(string(b))); n > largest {
			largest = n
		}
	}
	return largest
}

// parseLinuxCacheSize reads sysfs' "32K" / "2M" form.
func parseLinuxCacheSize(s string) int64 {
	if s == "" {
		return 0
	}
	mult := int64(1)
	switch s[len(s)-1] {
	case 'K', 'k':
		mult, s = 1<<10, s[:len(s)-1]
	case 'M', 'm':
		mult, s = 1<<20, s[:len(s)-1]
	case 'G', 'g':
		mult, s = 1<<30, s[:len(s)-1]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n * mult
}
