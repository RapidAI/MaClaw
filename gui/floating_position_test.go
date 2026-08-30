package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestClampFloatingPositionKeepsPetOnSmallScreen(t *testing.T) {
	// A 1920-wide default (screenW-150) on a 1280×800 remote desktop.
	x, y := clampFloatingPosition(1770, 100, 104, 1280, 800)
	if x+104 > 1280 {
		t.Fatalf("x=%d overflows 1280-wide screen", x)
	}
	if y+104 > 800 {
		t.Fatalf("y=%d overflows 800-tall screen", y)
	}
	if x != 1280-104 {
		t.Fatalf("x=%d, want %d", x, 1280-104)
	}
	if y != 100 {
		t.Fatalf("y=%d, want 100", y)
	}
}

func TestClampFloatingPositionPreservesOnScreenOrigin(t *testing.T) {
	x, y := clampFloatingPosition(0, 0, 104, 1280, 800)
	if x != 0 || y != 0 {
		t.Fatalf("origin should stay (0,0), got (%d,%d)", x, y)
	}
}

func TestClampFloatingPositionNegativeAndOversize(t *testing.T) {
	x, y := clampFloatingPosition(-40, -20, 104, 1280, 800)
	if x != 0 || y != 0 {
		t.Fatalf("negative → origin, got (%d,%d)", x, y)
	}
	x, y = clampFloatingPosition(50, 50, 200, 100, 90)
	if x != 0 || y != 0 {
		t.Fatalf("window larger than screen should pin to 0, got (%d,%d)", x, y)
	}
}

func TestLoadOrDefaultPositionClampsRestoredCoords(t *testing.T) {
	// Override platform hooks so this test does not depend on GDK/display size.
	prevW, prevH := platformGetScreenWidth, platformGetScreenHeight
	platformGetScreenWidth = func() int { return 1280 }
	platformGetScreenHeight = func() int { return 800 }
	t.Cleanup(func() {
		platformGetScreenWidth, platformGetScreenHeight = prevW, prevH
	})

	m := &FloatingAssistantManager{}
	x, y := m.loadOrDefaultPosition(corelib.AppConfig{
		FloatingBtnPositionSet: true,
		FloatingBtnX:           2000,
		FloatingBtnY:           900,
		PetSize:                88,
	})
	if x+104 > 1280 || y+104 > 800 {
		t.Fatalf("restored position off-screen: (%d,%d)", x, y)
	}
	if x != 1280-104 {
		t.Fatalf("x=%d, want %d", x, 1280-104)
	}
}

func TestLoadOrDefaultPositionDefaultFits1280x800(t *testing.T) {
	prevW, prevH := platformGetScreenWidth, platformGetScreenHeight
	platformGetScreenWidth = func() int { return 1280 }
	platformGetScreenHeight = func() int { return 800 }
	t.Cleanup(func() {
		platformGetScreenWidth, platformGetScreenHeight = prevW, prevH
	})

	m := &FloatingAssistantManager{}
	x, y := m.loadOrDefaultPosition(corelib.AppConfig{PetSize: 88})
	if x < 0 || y < 0 || x+104 > 1280 || y+104 > 800 {
		t.Fatalf("default position off-screen: (%d,%d)", x, y)
	}
}
