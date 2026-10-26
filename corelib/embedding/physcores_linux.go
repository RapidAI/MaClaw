//go:build linux

package embedding

import (
	"os"
	"strings"
)

// physicalCores counts distinct (physical id, core id) pairs in
// /proc/cpuinfo — the width the memory-bound embedding kernels can use.
// Returns 0 when the file is unavailable (callers fall back to NumCPU).
func physicalCores() int {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	type coreID struct{ pkg, core string }
	seen := map[coreID]bool{}
	var pkg, core string
	flush := func() {
		if core != "" {
			seen[coreID{pkg, core}] = true
		}
		pkg, core = "", ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			// Blank line ends a processor block.
			if strings.TrimSpace(line) == "" {
				flush()
			}
			continue
		}
		switch strings.TrimSpace(key) {
		case "physical id":
			pkg = strings.TrimSpace(val)
		case "core id":
			core = strings.TrimSpace(val)
		case "processor":
			// A new block started without a blank separator.
			flush()
		}
	}
	flush()
	if len(seen) > 0 {
		return len(seen)
	}
	// ARM kernels often omit physical/core ids but do report "cpu cores".
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "cpu cores") {
			if _, val, ok := strings.Cut(line, ":"); ok {
				if n := atoiTrim(val); n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

func atoiTrim(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
