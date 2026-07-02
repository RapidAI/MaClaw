//go:build darwin

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// getPrimaryScreenSize returns the primary display resolution on macOS in
// LOGICAL points (not physical Retina pixels).
//
// Note: system_profiler takes ~200-500ms on first call (IOKit query).
// This is acceptable as a one-time startup cost before window creation.
//
// Wails window dimensions are in logical points on macOS. system_profiler
// reports physical pixels with a "Retina" suffix for HiDPI displays.
// We detect "Retina" and halve the resolution to get logical points.
//
// Multi-display: prioritizes the display marked "Main Display: Yes".
//
// system_profiler output structure (per display block):
//
//	Color LCD:
//	    Display Type: Built-In Retina LCD
//	    Resolution: 3024 x 1964 Retina     ← Resolution comes BEFORE Main Display
//	    Main Display: Yes                   ← marker is after Resolution
//
// So we collect all display blocks with their resolution + isMain flag,
// then pick the main one (or first if no main found).
func getPrimaryScreenSize() (width, height int) {
	out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
	if err != nil {
		return 0, 0
	}

	type displayInfo struct {
		w, h   int
		isMain bool
	}

	var displays []displayInfo
	var currentW, currentH int
	var currentIsMain bool
	inDisplayBlock := false

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Resolution line starts a display's metrics
		if strings.HasPrefix(trimmed, "Resolution:") {
			// If we were already tracking a display, save it
			if inDisplayBlock && currentW > 0 {
				displays = append(displays, displayInfo{currentW, currentH, currentIsMain})
			}
			// Start new display block
			inDisplayBlock = true
			currentIsMain = false
			currentW, currentH = parseResolutionLine(trimmed)
			continue
		}

		// Main Display marker belongs to the current display block
		if inDisplayBlock && strings.HasPrefix(trimmed, "Main Display:") && strings.Contains(trimmed, "Yes") {
			currentIsMain = true
			continue
		}
	}
	// Save last display block
	if inDisplayBlock && currentW > 0 {
		displays = append(displays, displayInfo{currentW, currentH, currentIsMain})
	}

	if len(displays) == 0 {
		return 0, 0
	}

	// Pick main display, fallback to first
	for _, d := range displays {
		if d.isMain {
			return d.w, d.h
		}
	}
	return displays[0].w, displays[0].h
}

// parseResolutionLine extracts logical width/height from a system_profiler Resolution line.
// Format: "Resolution: 3024 x 1964 Retina" or "Resolution: 1920 x 1080"
// Returns logical points (physical ÷ 2 for Retina).
func parseResolutionLine(line string) (int, int) {
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return 0, 0
	}

	w, errW := strconv.Atoi(parts[1])
	h, errH := strconv.Atoi(parts[3])
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0
	}

	// Retina suffix means physical pixels — divide by 2 for logical points
	for _, p := range parts[4:] {
		if strings.EqualFold(p, "Retina") {
			w /= 2
			h /= 2
			break
		}
	}

	return w, h
}
