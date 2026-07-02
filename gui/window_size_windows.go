//go:build windows

package main

import "syscall"

// getPrimaryScreenSize returns the primary monitor resolution in logical pixels
// (DPI-scaled) using the Windows GetSystemMetrics API.
//
// Important: Wails apps are DPI-aware (system-aware via manifest), so
// GetSystemMetrics(SM_CXSCREEN) returns LOGICAL pixels, not physical:
//   - 4K (3840 physical) @ 150% scaling → returns 2560
//   - 4K (3840 physical) @ 200% scaling → returns 1920
//   - 1080p @ 100% → returns 1920
//   - 1080p @ 125% → returns 1536
//
// This is correct for our use case: Wails window dimensions are in logical
// pixels, so the preset table in window_size.go should match logical resolution.
func getPrimaryScreenSize() (width, height int) {
	user32 := syscall.NewLazyDLL("user32.dll")
	getSystemMetrics := user32.NewProc("GetSystemMetrics")
	if getSystemMetrics.Find() != nil {
		return 0, 0
	}

	const (
		smCXScreen = 0 // SM_CXSCREEN — primary screen width (logical px)
		smCYScreen = 1 // SM_CYSCREEN — primary screen height (logical px)
	)

	w, _, _ := getSystemMetrics.Call(uintptr(smCXScreen))
	h, _, _ := getSystemMetrics.Call(uintptr(smCYScreen))

	return int(w), int(h)
}
