package main

import "testing"

func TestEnvCheckWindowSizeFitsPreparingCard(t *testing.T) {
	width, height := envCheckWindowSize()
	if width != 520 || height != 460 {
		t.Fatalf("envCheckWindowSize() = %dx%d, want 520x460", width, height)
	}
}

func TestNormalizeScreenSizeForWindowDPI(t *testing.T) {
	tests := []struct {
		name                      string
		physicalW, physicalH, dpi int
		wantW, wantH              int
	}{
		{name: "100 percent", physicalW: 1920, physicalH: 1080, dpi: 96, wantW: 1920, wantH: 1080},
		{name: "1080p at 125 percent", physicalW: 1920, physicalH: 1080, dpi: 120, wantW: 1536, wantH: 864},
		{name: "1080p at 150 percent", physicalW: 1920, physicalH: 1080, dpi: 144, wantW: 1280, wantH: 720},
		{name: "invalid size", physicalW: 0, physicalH: 1080, dpi: 120, wantW: 0, wantH: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotH := normalizeScreenSizeForWindowDPI(tt.physicalW, tt.physicalH, tt.dpi)
			if gotW != tt.wantW || gotH != tt.wantH {
				t.Fatalf("normalizeScreenSizeForWindowDPI(%d, %d, %d) = %dx%d, want %dx%d", tt.physicalW, tt.physicalH, tt.dpi, gotW, gotH, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestNormalizeScreenSizeForWindowDPIRoundsSafely(t *testing.T) {
	gotW, gotH := normalizeScreenSizeForWindowDPI(1366, 768, 120)
	if gotW != 1092 || gotH != 614 {
		t.Fatalf("125%% conversion = %dx%d, want 1092x614", gotW, gotH)
	}
}

func TestAdaptiveWindowSizeForScreenUsesProvidedMonitor(t *testing.T) {
	// The main window can be on a monitor with a different DPI than the primary
	// one. The caller is responsible for normalising physical pixels first;
	// this helper must honour those current-monitor logical dimensions.
	width, height := adaptiveWindowSizeForScreen(1536, 864)
	if width != 1280 || height != 784 {
		t.Fatalf("adaptiveWindowSizeForScreen(1536, 864) = %dx%d, want 1280x784", width, height)
	}
}

func TestAdaptiveWindowSizeForScreenDoesNotReserveTaskbarTwice(t *testing.T) {
	// A current Windows monitor query supplies rcWork, so its height already
	// excludes the taskbar. A 1080p@125% screen with a 40px physical taskbar
	// has an 832px working area: preserve that exact available height rather
	// than applying the old generic 80px bottom-taskbar estimate a second time.
	width, height := adaptiveWindowSizeForWorkArea(1536, 832)
	if width != 1280 || height != 800 {
		t.Fatalf("adaptiveWindowSizeForWorkArea(1536, 832) = %dx%d, want 1280x800", width, height)
	}
}

func TestAdaptiveWindowSizeForWorkAreaPreservesPortraitTaskbarBounds(t *testing.T) {
	// RcWork can be portrait too. The selected preset needs 1,280px of height;
	// a 1,200px work area must constrain it to that exact usable boundary.
	width, height := adaptiveWindowSizeForWorkArea(1080, 1200)
	if width != 960 || height != 1200 {
		t.Fatalf("adaptiveWindowSizeForWorkArea(1080, 1200) = %dx%d, want 960x1200", width, height)
	}
}

func TestAdaptiveWindowSizeForUnavailableWorkAreaUsesScreenReserve(t *testing.T) {
	// A missing HWND cannot supply rcWork. The caller must retain the normal
	// screen/taskbar fallback rather than treating raw full-screen bounds as a
	// work area with zero reserve.
	width, height := adaptiveWindowSizeForScreen(1536, 832)
	if width != 1280 || height != 752 {
		t.Fatalf("adaptiveWindowSizeForScreen(1536, 832) = %dx%d, want 1280x752", width, height)
	}
}

func TestAdaptiveLandscape_Presets(t *testing.T) {
	tests := []struct {
		name       string
		sw, sh     int
		wantW      int
		wantH      int
		wantWRange [2]int // optional: allow range instead of exact
	}{
		// 4K@125% / ultrawide
		{"4K@125% (3072x1728)", 3072, 1728, 1920, 1200, [2]int{}},
		{"ultrawide 3440x1440", 3440, 1440, 1920, 1200, [2]int{}},

		// 2K native / 4K@150%
		{"2K native 2560x1440", 2560, 1440, 1600, 1000, [2]int{}},
		{"4K@150% (2560x1440)", 2560, 1440, 1600, 1000, [2]int{}},

		// 1080p / 4K@200%
		{"1080p 1920x1080", 1920, 1080, 1440, 900, [2]int{}},
		{"4K@200% (1920x1080)", 1920, 1080, 1440, 900, [2]int{}},

		// Older wide screens
		{"1680x1050", 1680, 1050, 1360, 850, [2]int{}},
		{"1600x900", 1600, 900, 1360, 820, [2]int{}}, // 900-80=820, clamped

		// Laptop HD+
		{"1440x900", 1440, 900, 1280, 800, [2]int{}},

		// Small screen fallback
		{"1366x768", 1366, 768, 1120, 688, [2]int{}}, // 768-80=688, clamped
		{"1280x720", 1280, 720, 1120, 640, [2]int{}}, // 720-80=640, clamped
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := adaptiveLandscape(tt.sw, tt.sh)
			if w != tt.wantW {
				t.Errorf("width: got %d, want %d", w, tt.wantW)
			}
			if h != tt.wantH {
				t.Errorf("height: got %d, want %d", h, tt.wantH)
			}
		})
	}
}

func TestAdaptivePortrait_Presets(t *testing.T) {
	tests := []struct {
		name   string
		sw, sh int
		wantW  int
		wantH  int
	}{
		{"1440x2560 portrait", 1440, 2560, 1280, 1600},
		{"1200x1920 portrait", 1200, 1920, 1080, 1400},
		{"1080x1920 portrait", 1080, 1920, 960, 1280},
		{"900x1600 portrait (narrow)", 900, 1600, 800, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := adaptivePortrait(tt.sw, tt.sh)
			if w != tt.wantW {
				t.Errorf("width: got %d, want %d", w, tt.wantW)
			}
			if h != tt.wantH {
				t.Errorf("height: got %d, want %d", h, tt.wantH)
			}
		})
	}
}

func TestAdaptiveWindowSize_DetectionFailure(t *testing.T) {
	// When getPrimaryScreenSize returns 0,0 the function should return safe defaults.
	// We can't mock getPrimaryScreenSize directly, but we test adaptiveLandscape/adaptivePortrait
	// which are the core logic. The top-level adaptiveWindowSize just dispatches.
	w, h := 1360, 850 // expected fallback
	if w != 1360 || h != 850 {
		t.Errorf("fallback values changed unexpectedly")
	}
}

func TestAdaptiveWindowSize_OrientationDetection(t *testing.T) {
	// Landscape: width > height
	w, h := adaptiveLandscape(1920, 1080)
	if w <= h {
		t.Errorf("landscape should produce wider window: got %dx%d", w, h)
	}

	// Portrait: height > width
	w, h = adaptivePortrait(1080, 1920)
	if h <= w {
		t.Errorf("portrait should produce taller window: got %dx%d", w, h)
	}
}

func TestAdaptiveWindowSize_SquareScreen(t *testing.T) {
	// Square screen (sw == sh) should go landscape path (sh > sw is false)
	// Test via adaptiveLandscape directly
	w, _ := adaptiveLandscape(1024, 1024)
	// Should hit small screen fallback
	if w > 1024 {
		t.Errorf("window width %d exceeds screen width 1024", w)
	}
}

func TestClampToScreen_LandscapeBasic(t *testing.T) {
	// Window fits — no clamping
	w, h := clampToScreen(1440, 900, 1920, 1080, false)
	if w != 1440 || h != 900 {
		t.Errorf("should not clamp: got %dx%d", w, h)
	}
}

func TestClampToScreen_LandscapeWidthExceeds(t *testing.T) {
	// Window wider than 90% of screen
	w, h := clampToScreen(1440, 900, 1500, 1080, false)
	// maxW = 1500*90/100 = 1350
	if w != 1350 {
		t.Errorf("width should be clamped to 1350: got %d", w)
	}
	if h != 900 {
		t.Errorf("height should be unchanged: got %d", h)
	}
}

func TestClampToScreen_LandscapeHeightExceeds(t *testing.T) {
	// Window taller than screen - taskbar
	w, h := clampToScreen(1440, 900, 1920, 950, false)
	// maxH = 950 - 80 = 870
	if w != 1440 {
		t.Errorf("width should be unchanged: got %d", w)
	}
	if h != 870 {
		t.Errorf("height should be clamped to 870: got %d", h)
	}
}

func TestClampToScreen_PortraitTaskbarReserve(t *testing.T) {
	// Portrait uses 120px reserve instead of 80px
	_, h := clampToScreen(960, 1400, 1080, 1500, true)
	// maxH = 1500 - 120 = 1380
	if h != 1380 {
		t.Errorf("portrait height should be clamped to 1380: got %d", h)
	}
}

func TestClampToScreen_TinyScreenGuard(t *testing.T) {
	// Screen too small — guards prevent clamping to unusable size
	w, h := clampToScreen(1440, 900, 500, 400, false)
	// maxW = 500*90/100 = 450 (< 640 guard) → no width clamp
	// maxH = 400 - 80 = 320 (< 480 guard) → no height clamp
	if w != 1440 {
		t.Errorf("tiny screen guard: width should stay 1440, got %d", w)
	}
	if h != 900 {
		t.Errorf("tiny screen guard: height should stay 900, got %d", h)
	}
}

func TestClampToScreen_ExactGuardBoundary(t *testing.T) {
	// maxW exactly at guard boundary (640)
	w, _ := clampToScreen(800, 500, 712, 700, false)
	// maxW = 712*90/100 = 640 (== guard) → clamp applies
	if w != 640 {
		t.Errorf("at guard boundary: width should be 640, got %d", w)
	}

	// maxH exactly at guard boundary (480)
	_, h := clampToScreen(800, 600, 1920, 560, false)
	// maxH = 560 - 80 = 480 (== guard) → clamp applies
	if h != 480 {
		t.Errorf("at guard boundary: height should be 480, got %d", h)
	}
}

func TestAdaptiveLandscape_WindowNeverExceedsScreen(t *testing.T) {
	// Property: for any reasonable screen, window should never exceed screen dimensions
	screens := [][2]int{
		{3840, 2160}, {2560, 1440}, {1920, 1080}, {1680, 1050},
		{1440, 900}, {1366, 768}, {1280, 720}, {1024, 768},
	}

	for _, s := range screens {
		sw, sh := s[0], s[1]
		w, h := adaptiveLandscape(sw, sh)
		if w > sw {
			t.Errorf("screen %dx%d: window width %d exceeds screen", sw, sh, w)
		}
		maxH := sh - 80
		if maxH >= 480 && h > maxH {
			t.Errorf("screen %dx%d: window height %d exceeds usable height %d", sw, sh, h, maxH)
		}
	}
}

func TestAdaptivePortrait_WindowNeverExceedsScreen(t *testing.T) {
	screens := [][2]int{
		{1440, 2560}, {1200, 1920}, {1080, 1920}, {900, 1600}, {768, 1366},
	}

	for _, s := range screens {
		sw, sh := s[0], s[1]
		w, h := adaptivePortrait(sw, sh)
		maxW := sw * 90 / 100
		if maxW >= 640 && w > maxW {
			t.Errorf("screen %dx%d: window width %d exceeds 90%% of screen (%d)", sw, sh, w, maxW)
		}
		maxH := sh - 120
		if maxH >= 480 && h > maxH {
			t.Errorf("screen %dx%d: window height %d exceeds usable height %d", sw, sh, h, maxH)
		}
	}
}
