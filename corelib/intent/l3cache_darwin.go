//go:build darwin

package intent

import (
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	l3Once  sync.Once
	l3Bytes int64
)

// l3CacheBytes reports the last-level cache visible to one core.
//
// Intel Macs expose hw.l3cachesize. Apple Silicon does not (the sysctl fails),
// so the P-core cluster's L2 is used instead: there is no shared L3 a single
// prediction can rely on, and a hidden matrix larger than that cluster cache is
// re-read from memory every call — the condition the Q8P policy tests for.
// The load-time accuracy probe still gates the switch.
func l3CacheBytes() int64 {
	l3Once.Do(func() {
		for _, key := range []string{"hw.l3cachesize", "hw.perflevel0.l2cachesize"} {
			s, err := unix.Sysctl(key)
			if err != nil {
				continue
			}
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err == nil && n > 0 {
				l3Bytes = n
				return
			}
		}
	})
	return l3Bytes
}
