package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestPetTravelBoxUsesTheWindowMonitor(t *testing.T) {
	minX, maxX, minY, maxY := petTravelBox(1920, 1040, 200, petpack.WindowRect{}, false)
	if minX != 0 || minY != 0 || maxX != 1720 || maxY != 840 {
		t.Fatalf("roam box = (%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
	// A patrol whose monitor work area could not be read stays on that same
	// primary box. An empty rect must not invent a second origin.
	minX, maxX, minY, maxY = petTravelBox(1920, 1040, 200, petpack.WindowRect{}, true)
	if minX != 0 || minY != 0 || maxX != 1720 || maxY != 840 {
		t.Fatalf("patrol without a work area = (%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
	// A display to the left of the primary. The primary width must not pull
	// the pet back across the origin.
	minX, maxX, minY, maxY = petTravelBox(1920, 1040, 200, petpack.WindowRect{Left: -1920, Top: 0, Right: 0, Bottom: 1080}, true)
	if minX != -1920 || maxX != -200 || minY != 0 || maxY != 880 {
		t.Fatalf("left monitor box = (%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
	minX, maxX, minY, maxY = petTravelBox(1920, 1040, 200, petpack.WindowRect{Left: 1920, Top: 40, Right: 3840, Bottom: 1080}, true)
	if minX != 1920 || maxX != 3640 || minY != 40 || maxY != 880 {
		t.Fatalf("right monitor box = (%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
}

func TestPetTravelBoundsStaysOnThePetMonitor(t *testing.T) {
	left := petpack.WindowRect{Left: -1920, Top: 0, Right: 0, Bottom: 1080}
	right := petpack.WindowRect{Left: 1920, Top: 40, Right: 3840, Bottom: 1080}
	minX, maxX, minY, maxY := petTravelBounds(1920, 1040, 200, petpack.WindowRect{}, left, false)
	if minX != -1920 || maxX != -200 || minY != 0 || maxY != 880 {
		t.Fatalf("roam on left = (%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
	// The window's monitor wins over the monitor she was standing on.
	minX, maxX, minY, maxY = petTravelBounds(1920, 1040, 200, right, left, true)
	if minX != 1920 || maxX != 3640 || minY != 40 || maxY != 880 {
		t.Fatalf("patrol on right = (%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
	// A patrol whose window work area could not be read still uses hers.
	minX, maxX, minY, maxY = petTravelBounds(1920, 1040, 200, petpack.WindowRect{}, left, true)
	if minX != -1920 || maxX != -200 || minY != 0 || maxY != 880 {
		t.Fatalf("patrol without window work = (%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
	minX, maxX, minY, maxY = petTravelBounds(1920, 1040, 200, petpack.WindowRect{}, petpack.WindowRect{}, false)
	if minX != 0 || maxX != 1720 || minY != 0 || maxY != 840 {
		t.Fatalf("primary = (%d,%d)-(%d,%d)", minX, minY, maxX, maxY)
	}
}

func TestClampPetScreenPositionKeepsAnotherMonitor(t *testing.T) {
	prev := petWorkAreaAt
	petWorkAreaAt = func(x, y, sz int) (petpack.WindowRect, bool) {
		if x+sz <= 0 {
			return petpack.WindowRect{Left: -1920, Top: 0, Right: 0, Bottom: 1080}, true
		}
		if x >= 1920 {
			return petpack.WindowRect{Left: 1920, Top: 40, Right: 3840, Bottom: 1080}, true
		}
		return petpack.WindowRect{}, false
	}
	t.Cleanup(func() { petWorkAreaAt = prev })

	x, y := clampPetScreenPosition(-400, 100, 200)
	if x != -400 || y != 100 {
		t.Fatalf("left monitor = (%d,%d)", x, y)
	}
	// Past the left display, still on that display's side, stays on it.
	x, y = clampPetScreenPosition(-4000, 2000, 200)
	if x != -1920 || y != 880 {
		t.Fatalf("left clamp = (%d,%d)", x, y)
	}
	x, y = clampPetScreenPosition(2000, 40, 200)
	if x != 2000 || y != 40 {
		t.Fatalf("right monitor = (%d,%d)", x, y)
	}
	x, y = clampPetScreenPosition(3900, 40, 200)
	if x != 3640 || y != 40 {
		t.Fatalf("right clamp = (%d,%d)", x, y)
	}
}

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
	prevArea := petWorkAreaAt
	platformGetScreenWidth = func() int { return 1280 }
	platformGetScreenHeight = func() int { return 800 }
	// This case is the primary-size fallback. A real monitor would keep 2000
	// when that display is actually that wide.
	petWorkAreaAt = nil
	t.Cleanup(func() {
		platformGetScreenWidth, platformGetScreenHeight = prevW, prevH
		petWorkAreaAt = prevArea
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
	prevArea := petWorkAreaAt
	platformGetScreenWidth = func() int { return 1280 }
	platformGetScreenHeight = func() int { return 800 }
	petWorkAreaAt = nil
	t.Cleanup(func() {
		platformGetScreenWidth, platformGetScreenHeight = prevW, prevH
		petWorkAreaAt = prevArea
	})

	m := &FloatingAssistantManager{}
	x, y := m.loadOrDefaultPosition(corelib.AppConfig{PetSize: 88})
	if x < 0 || y < 0 || x+104 > 1280 || y+104 > 800 {
		t.Fatalf("default position off-screen: (%d,%d)", x, y)
	}
}
