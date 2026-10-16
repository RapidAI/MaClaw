//go:build windows

package guiapp

import (
	"testing"
	"time"
	"unsafe"
)

func TestWindowTitleMatchesMain(t *testing.T) {
	cases := []struct {
		actual, want string
		ok           bool
	}{
		{"MaClaw", "MaClaw", true},
		{"MaClaw - workspace", "MaClaw", true},
		{"TigerClaw", "TigerClaw", true},
		{"QAgent", "QAgent", true},
		{"码卡龙 MaClaw", "MaClaw", true},
		{"Chrome", "MaClaw", false},
		{"", "MaClaw", false},
		// Short accidental titles must not match via reverse Contains.
		{"Ma", "MaClaw", false},
		{"C", "MaClaw", false},
	}
	for _, c := range cases {
		if got := windowTitleMatchesMain(c.actual, c.want); got != c.ok {
			t.Errorf("windowTitleMatchesMain(%q,%q)=%v want %v", c.actual, c.want, got, c.ok)
		}
	}
}

func TestAbs32(t *testing.T) {
	if abs32(-3) != 3 || abs32(3) != 3 || abs32(0) != 0 {
		t.Fatal("abs32")
	}
}

func TestScheduleClampSupersedes(t *testing.T) {
	// Ensure generation advances so a later schedule invalidates an earlier one.
	g1 := clampScheduleGen.Add(1)
	g2 := clampScheduleGen.Add(1)
	if g2 <= g1 {
		t.Fatalf("clampScheduleGen should advance: %d then %d", g1, g2)
	}
	if clampScheduleGen.Load() != g2 {
		t.Fatal("load should match last Add")
	}
}

func TestEnsureEnumWindowsCallbackStable(t *testing.T) {
	// NewCallback must be created once; repeated ensure returns the same ptr.
	a := ensureEnumWindowsCallback()
	b := ensureEnumWindowsCallback()
	if a == 0 || a != b {
		t.Fatalf("callback not stable: %v vs %v", a, b)
	}
}

func TestClampMinGapPositive(t *testing.T) {
	if clampMinGap <= 0 || clampMinGap > time.Second {
		t.Fatalf("clampMinGap out of range: %v", clampMinGap)
	}
}

func TestWindowRectOverflowsWorkArea(t *testing.T) {
	work := workAreaRect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}
	// Exact fit
	if windowRectOverflowsWorkArea(work, work) {
		t.Fatal("exact fit should not overflow")
	}
	// Under taskbar
	under := workAreaRect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	if !windowRectOverflowsWorkArea(under, work) {
		t.Fatal("bottom past work area should overflow")
	}
	// Off left edge (DWM border)
	off := workAreaRect{Left: -8, Top: -8, Right: 1928, Bottom: 1048}
	if !windowRectOverflowsWorkArea(off, work) {
		t.Fatal("negative origin overflow")
	}
	// Tolerance: up to work.Bottom+1 inclusive is not treated as overflow.
	if windowRectOverflowsWorkArea(workAreaRect{0, 0, 1920, 1041}, work) {
		t.Fatal("1px bottom should be within tolerance")
	}
	if !windowRectOverflowsWorkArea(workAreaRect{0, 0, 1920, 1042}, work) {
		t.Fatal("2px past bottom should overflow")
	}
}

func TestWindowPlacementSize(t *testing.T) {
	if got := unsafe.Sizeof(windowPlacement{}); got != 44 {
		t.Fatalf("WINDOWPLACEMENT size %d want 44 so Get/SetWindowPlacement length is valid", got)
	}
}

func TestApplyPreservedNormalPlacementKeepsMaximized(t *testing.T) {
	cur := windowPlacement{Length: 1, Flags: 2, ShowCmd: 1, NormalPosition: workAreaRect{0, 0, 1920, 1040}}
	saved := workAreaRect{Left: 80, Top: 60, Right: 1180, Bottom: 780}
	got := applyPreservedNormalPlacement(cur, saved)
	if got.ShowCmd != swShowMaximized {
		t.Fatalf("ShowCmd=%d want SW_SHOWMAXIMIZED so clamp does not restore", got.ShowCmd)
	}
	if got.NormalPosition != saved {
		t.Fatalf("NormalPosition=%v want %v", got.NormalPosition, saved)
	}
	if got.Length != uint32(unsafe.Sizeof(got)) {
		t.Fatalf("Length=%d want sizeof", got.Length)
	}
	if got.Flags != cur.Flags {
		t.Fatalf("Flags changed: %d", got.Flags)
	}
}

func TestPlaceOriginKeepsPortraitDisplayLeftOfPrimary(t *testing.T) {
	// Reproduced layout: 1440x2560 portrait display whose origin is negative,
	// sitting left of a 2560x1440 primary.
	monitor := workAreaRect{Left: -1440, Top: -573, Right: 0, Bottom: 1987}
	work := workAreaRect{Left: -1440, Top: -573, Right: 0, Bottom: 1939}
	x, y := placeOriginInWorkArea(-1200, -200, 1100, 800, monitor, work)
	if x != -1200 || y != -200 {
		t.Fatalf("visible second-display window moved to %d,%d", x, y)
	}
}

func TestPlaceOriginPullsDoubledWorkAreaOffsetBack(t *testing.T) {
	monitor := workAreaRect{Left: -1440, Top: -573, Right: 0, Bottom: 1987}
	work := workAreaRect{Left: -1440, Top: -573, Right: 0, Bottom: 1939}
	// WindowGetPosition (-1200,-200) passed through Wails WindowSetPosition
	// becomes work origin + that point, which misses both displays.
	x, y := placeOriginInWorkArea(-1440-1200, -573-200, 1100, 800, monitor, work)
	if x != -1440 || y != -573 {
		t.Fatalf("off-screen restore landed at %d,%d", x, y)
	}
}

func TestPlaceOriginKeepsPrimaryDisplay(t *testing.T) {
	monitor := workAreaRect{Left: 0, Top: 0, Right: 2560, Bottom: 1440}
	work := workAreaRect{Left: 0, Top: 0, Right: 2560, Bottom: 1392}
	x, y := placeOriginInWorkArea(480, 196, 1600, 1000, monitor, work)
	if x != 480 || y != 196 {
		t.Fatalf("primary window moved to %d,%d", x, y)
	}
}

func TestScreenPositionParamsKeepNegativeOrigin(t *testing.T) {
	// A display left of the primary reports a negative screen X. The uintptr
	// passed to SetWindowPos must stay negative; truncating it would jump the
	// window onto the primary or further off the virtual desktop.
	x, y := screenPositionParams(-1700, -40)
	if int32(x) != -1700 || int32(y) != -40 {
		t.Fatalf("screenPositionParams(-1700,-40)=%d,%d", int32(x), int32(y))
	}
}

func TestShouldPreserveClampedNormalPlacement(t *testing.T) {
	work := workAreaRect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}
	normal := workAreaRect{Left: 80, Top: 60, Right: 1180, Bottom: 780}
	if !shouldPreserveClampedNormalPlacement(normal, work) {
		t.Fatal("pre-maximize size should be preserved across clamp")
	}
	wideShort := workAreaRect{Left: 0, Top: 80, Right: 1920, Bottom: 780}
	if !shouldPreserveClampedNormalPlacement(wideShort, work) {
		t.Fatal("full-width but shorter restore rect must still be preserved")
	}
	if shouldPreserveClampedNormalPlacement(work, work) {
		t.Fatal("work-area sized rect is already corrupted and must not be saved")
	}
	if shouldPreserveClampedNormalPlacement(workAreaRect{0, 0, 80, 80}, work) {
		t.Fatal("tiny rect is not a restore size")
	}
	leftWork := workAreaRect{Left: -1440, Top: -573, Right: 0, Bottom: 1939}
	onLeft := workAreaRect{Left: -1200, Top: -200, Right: -100, Bottom: 600}
	if !shouldPreserveClampedNormalPlacement(onLeft, leftWork) {
		t.Fatal("restore rect on the left-hand display must be preserved")
	}
	offLeft := workAreaRect{Left: -2640, Top: -773, Right: -1540, Bottom: 27}
	if shouldPreserveClampedNormalPlacement(offLeft, leftWork) {
		t.Fatal("off-screen restore rect must not be preserved")
	}
}
