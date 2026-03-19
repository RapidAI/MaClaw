//go:build darwin

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// macOSMajorVersion returns the major macOS version number (e.g. 15 for Sequoia, 26 for Tahoe).
// Returns 0 if detection fails.
func macOSMajorVersion() int {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return 0
	}
	version := strings.TrimSpace(string(out))
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return 0
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return major
}

// isMacOSTahoeOrLater returns true if running on macOS 26 (Tahoe) or later.
// On macOS 26+, Liquid Glass changes how translucent/frameless windows are
// rendered, which can cause crashes with Wails v2's NSVisualEffectView usage.
func isMacOSTahoeOrLater() bool {
	return macOSMajorVersion() >= 26
}
