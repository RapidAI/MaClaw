package main

// adaptiveWindowSize selects the best window dimensions based on the primary
// screen resolution. Handles:
//   - Landscape vs portrait orientation (portrait uses narrower, taller window)
//   - 4K vs 2K via logical pixel thresholds (platform APIs return DPI-scaled values)
//   - Taskbar/dock compensation (80px landscape, 120px portrait)
//
// All values are in LOGICAL pixels/points — matching what Wails uses for window
// dimensions. Platform-specific getPrimaryScreenSize() implementations normalize
// physical pixels to logical coordinates (Windows: DPI-aware GetSystemMetrics;
// macOS: Retina physical ÷ 2; Linux: xrandr current mode).
//
// Falls back to 1360×850 if screen detection fails.
func adaptiveWindowSize() (width, height int) {
	sw, sh := getPrimaryScreenSize()

	// Detection failed — safe default for 1080p+
	if sw <= 0 || sh <= 0 {
		return 1360, 850
	}

	portrait := sh > sw

	if portrait {
		return adaptivePortrait(sw, sh)
	}
	return adaptiveLandscape(sw, sh)
}

// adaptiveLandscape handles normal wide screens (16:9, 16:10, 21:9).
func adaptiveLandscape(sw, sh int) (int, int) {
	type preset struct {
		minScreenWidth int
		winWidth       int
		winHeight      int
	}

	// Presets are in LOGICAL pixels (DPI-scaled on Windows, points on macOS).
	// This is what getPrimaryScreenSize() returns on all platforms, and what
	// Wails uses for window dimensions.
	//
	// Logical resolution examples:
	//   - 4K@125% or ultrawide 3440@100%: ~3000+ logical px
	//   - 4K@150% or native 2K: ~2560 logical px
	//   - 4K@200% or native 1080p: ~1920 logical px
	//   - 1080p@125%: ~1536 logical px
	presets := []preset{
		{3000, 1920, 1200}, // Ultra-wide / 4K@125% — ~64% of screen width
		{2560, 1600, 1000}, // 2K native / 4K@150% — ~62% of screen width
		{1920, 1440, 900},  // 1080p / 4K@200% — ~75% of screen width
		{1600, 1360, 850},  // 1680×1050 / older wide
		{1440, 1280, 800},  // laptop HD+
	}

	for _, p := range presets {
		if sw >= p.minScreenWidth {
			return clampToScreen(p.winWidth, p.winHeight, sw, sh, false)
		}
	}

	// Small screen fallback (< 1440 wide)
	return clampToScreen(1120, 700, sw, sh, false)
}

// adaptivePortrait handles rotated/vertical monitors.
// Uses narrower, taller window proportions — better for reading chat history.
func adaptivePortrait(sw, sh int) (int, int) {
	type preset struct {
		minScreenWidth int // Note: in portrait, sw is the shorter dimension
		winWidth       int
		winHeight      int
	}

	// In portrait mode, sw < sh. Typical portrait resolutions:
	// 1080×1920, 1440×2560, 1200×1920
	presets := []preset{
		{1440, 1280, 1600}, // 1440×2560 portrait
		{1200, 1080, 1400}, // 1200×1920 portrait
		{1080, 960, 1280},  // 1080×1920 portrait (standard)
	}

	for _, p := range presets {
		if sw >= p.minScreenWidth {
			return clampToScreen(p.winWidth, p.winHeight, sw, sh, true)
		}
	}

	// Very narrow portrait fallback
	return clampToScreen(800, 1000, sw, sh, true)
}

// clampToScreen ensures the window fits within usable screen area.
// Reserves space for taskbar/dock:
//   - Landscape: 80px vertical (bottom taskbar)
//   - Portrait: 120px vertical (taskbar may be on side, but window needs margin)
//
// Also ensures window doesn't exceed 90% of screen width.
// Guard thresholds (640×480) prevent shrinking to unusable sizes on misconfigured
// screens that report tiny logical resolutions.
func clampToScreen(w, h, sw, sh int, portrait bool) (int, int) {
	taskbarReserve := 80
	if portrait {
		taskbarReserve = 120
	}

	maxW := sw * 90 / 100
	maxH := sh - taskbarReserve

	// Only clamp if screen is large enough to be usable
	if w > maxW && maxW >= 640 {
		w = maxW
	}
	if h > maxH && maxH >= 480 {
		h = maxH
	}

	return w, h
}

