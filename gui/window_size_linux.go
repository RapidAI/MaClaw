//go:build linux

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// getPrimaryScreenSize returns the primary display resolution on Linux.
// Tries xrandr first (X11), then falls back to xdpyinfo.
//
// Multi-display: prioritizes the output marked "primary". Falls back to
// the first connected output if no primary is designated.
func getPrimaryScreenSize() (width, height int) {
	// Try xrandr — works on most Linux desktops (X11 and XWayland)
	out, err := exec.Command("xrandr", "--current").Output()
	if err == nil {
		w, h := parseXrandrPrimary(string(out))
		if w > 0 && h > 0 {
			return w, h
		}
	}

	// Fallback: xdpyinfo (reports total screen dimensions)
	out, err = exec.Command("xdpyinfo").Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "dimensions:") {
				// "dimensions:    2560x1440 pixels (..."
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					parts := strings.Split(fields[1], "x")
					if len(parts) == 2 {
						w, errW := strconv.Atoi(parts[0])
						h, errH := strconv.Atoi(parts[1])
						if errW == nil && errH == nil && w > 800 && h > 600 {
							return w, h
						}
					}
				}
			}
		}
	}

	return 0, 0
}

// parseXrandrPrimary parses xrandr output and returns the resolution of
// the primary display. Falls back to the first connected display.
func parseXrandrPrimary(output string) (int, int) {
	lines := strings.Split(output, "\n")

	var primaryRes, firstConnectedRes [2]int

	for _, line := range lines {
		if !strings.Contains(line, " connected") {
			continue
		}

		isPrimary := strings.Contains(line, " connected primary")

		// Extract resolution from format: "DP-1 connected primary 2560x1440+0+0 ..."
		fields := strings.Fields(line)
		for _, f := range fields {
			if strings.Contains(f, "x") && strings.Contains(f, "+") {
				// "2560x1440+0+0" → "2560x1440"
				res := strings.Split(f, "+")[0]
				parts := strings.Split(res, "x")
				if len(parts) == 2 {
					w, errW := strconv.Atoi(parts[0])
					h, errH := strconv.Atoi(parts[1])
					if errW == nil && errH == nil && w > 800 && h > 600 {
						if isPrimary {
							primaryRes = [2]int{w, h}
						}
						if firstConnectedRes[0] == 0 {
							firstConnectedRes = [2]int{w, h}
						}
					}
				}
			}
		}
	}

	if primaryRes[0] > 0 {
		return primaryRes[0], primaryRes[1]
	}
	return firstConnectedRes[0], firstConnectedRes[1]
}
