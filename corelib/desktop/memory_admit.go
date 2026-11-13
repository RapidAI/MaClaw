package desktop

import (
	"os"
	"strconv"
	"strings"
)

// MemoryAdmitFraction is the container memory line above which a new desktop
// run or screen lease waits. MACLAW_DESKTOP_MEM_PERCENT is 1–99. Anything
// else uses 85%.
func MemoryAdmitFraction() float64 {
	raw := strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_MEM_PERCENT"))
	if raw == "" {
		return 0.85
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || n <= 0 || n >= 100 {
		return 0.85
	}
	return n / 100
}

// MemoryPressured reports whether used/limit is at or over the admission line.
// An unknown limit is not pressure.
func MemoryPressured(used, limit int64) bool {
	fraction := MemoryAdmitFraction()
	if limit <= 0 || fraction <= 0 {
		return false
	}
	return float64(used)/float64(limit) >= fraction
}
