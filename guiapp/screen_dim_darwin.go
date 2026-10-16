//go:build darwin

package guiapp

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func init() {
	platformGetIdleDuration = getIdleDurationDarwin
	platformDimDisplay = dimDisplayDarwin
}

func getIdleDurationDarwin() time.Duration {
	// ioreg -c IOHIDSystem -d 4 returns HIDIdleTime in nanoseconds.
	// Using -d 4 to limit depth and reduce output size.
	out, err := exec.Command("ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "HIDIdleTime") {
			continue
		}
		// Format: "HIDIdleTime" = 1234567890  or  HIDIdleTime = 1234567890
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		valStr := strings.TrimSpace(line[idx+1:])
		ns, err := strconv.ParseInt(valStr, 10, 64)
		if err == nil && ns >= 0 {
			return time.Duration(ns)
		}
	}
	return 0
}

func dimDisplayDarwin() {
	_ = exec.Command("pmset", "displaysleepnow").Run()
}
