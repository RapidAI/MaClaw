//go:build windows

package main

import (
	"testing"
	"time"
)

func TestWindowTitleMatchesMain(t *testing.T) {
	cases := []struct {
		actual, want string
		ok           bool
	}{
		{"MaClaw", "MaClaw", true},
		{"MaClaw - workspace", "MaClaw", true},
		{"TigerClaw", "TigerClaw", true},
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
