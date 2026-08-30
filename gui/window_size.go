package main

// envCheckWindowSize returns the compact window dimensions used during
// environment check/preparation phase. The window stays compact (520x460) so
// the preparing card can show the MaClaw mark, copy, and progress bar, then
// expands to adaptiveWindowSize() after the check completes.
func envCheckWindowSize() (width, height int) {
	return 520, 460
}

// normalizeScreenSizeForWindowDPI converts physical display pixels to the
// 96-DPI logical units accepted by Wails' WindowSetSize API. Windows Wails
// processes are DPI-aware, so GetSystemMetrics returns physical dimensions;
// Wails subsequently scales window dimensions up to the current window DPI.
// Passing the physical metrics through unchanged would therefore scale a
// 1920x1080 display at 125% twice and can push a frameless window outside its
// visible work area.
func normalizeScreenSizeForWindowDPI(width, height, dpi int) (int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	if dpi <= 0 {
		dpi = 96
	}
	return scaleToDefaultWindowDPI(width, dpi), scaleToDefaultWindowDPI(height, dpi)
}

// scaleToDefaultWindowDPI intentionally matches Wails' ScaleToDefaultDPI:
// both use truncation rather than rounding. Using a different rounding rule
// can make an adaptive size exceed Wails' reconstructed physical client size
// by one pixel at fractional DPI, precisely the edge we are protecting.
func scaleToDefaultWindowDPI(pixels, dpi int) int {
	if pixels <= 0 {
		return 0
	}
	if dpi <= 0 {
		dpi = 96
	}
	return pixels * 96 / dpi
}

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
	return adaptiveWindowSizeForScreen(sw, sh)
}

// adaptiveWindowSizeForScreen selects a window size for a known full display
// size. It applies a conservative taskbar reserve because raw screen metrics
// include the system work-area exclusion.
func adaptiveWindowSizeForScreen(sw, sh int) (width, height int) {
	return adaptiveWindowSizeForAvailableArea(sw, sh, taskbarReserveLandscape, taskbarReservePortrait)
}

// adaptiveWindowSizeForWorkArea selects a size from an OS-reported work area
// such as Windows MONITORINFO.rcWork. Unlike raw screen dimensions, this has
// already excluded the taskbar, so it must not reserve taskbar space again.
func adaptiveWindowSizeForWorkArea(sw, sh int) (width, height int) {
	return adaptiveWindowSizeForAvailableArea(sw, sh, 0, 0)
}

const (
	taskbarReserveLandscape = 80
	taskbarReservePortrait  = 120
)

func adaptiveWindowSizeForAvailableArea(sw, sh, landscapeReserve, portraitReserve int) (width, height int) {

	// Detection failed — safe default for 1080p+
	if sw <= 0 || sh <= 0 {
		return 1360, 850
	}

	portrait := sh > sw

	if portrait {
		return adaptivePortraitWithReserve(sw, sh, portraitReserve)
	}
	return adaptiveLandscapeWithReserve(sw, sh, landscapeReserve)
}

// adaptiveLandscape handles normal wide screens (16:9, 16:10, 21:9).
func adaptiveLandscape(sw, sh int) (int, int) {
	return adaptiveLandscapeWithReserve(sw, sh, taskbarReserveLandscape)
}

func adaptiveLandscapeWithReserve(sw, sh, taskbarReserve int) (int, int) {
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
			return clampToScreenWithReserve(p.winWidth, p.winHeight, sw, sh, taskbarReserve)
		}
	}

	// Small screen fallback (< 1440 wide)
	return clampToScreenWithReserve(1120, 700, sw, sh, taskbarReserve)
}

// adaptivePortrait handles rotated/vertical monitors.
// Uses narrower, taller window proportions — better for reading chat history.
func adaptivePortrait(sw, sh int) (int, int) {
	return adaptivePortraitWithReserve(sw, sh, taskbarReservePortrait)
}

func adaptivePortraitWithReserve(sw, sh, taskbarReserve int) (int, int) {
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
			return clampToScreenWithReserve(p.winWidth, p.winHeight, sw, sh, taskbarReserve)
		}
	}

	// Very narrow portrait fallback
	return clampToScreenWithReserve(800, 1000, sw, sh, taskbarReserve)
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
	reserve := taskbarReserveLandscape
	if portrait {
		reserve = taskbarReservePortrait
	}
	return clampToScreenWithReserve(w, h, sw, sh, reserve)
}

func clampToScreenWithReserve(w, h, sw, sh, taskbarReserve int) (int, int) {
	if taskbarReserve < 0 {
		taskbarReserve = 0
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
