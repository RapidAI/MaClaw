//go:build windows

package guiautomation

import (
	"testing"
	"unsafe"
)

func TestWinInputSize(t *testing.T) {
	// SendInput expects sizeof(INPUT) == 40 on 64-bit Windows.
	sz := unsafe.Sizeof(winInput{})
	if sz != 40 {
		t.Fatalf("sizeof(winInput)=%d want 40 (SendInput layout)", sz)
	}
	if got := unsafe.Sizeof(winInputMouse{}); got != 40 {
		t.Fatalf("sizeof(winInputMouse)=%d want 40", got)
	}
	if got := unsafe.Sizeof(winMouseInput{}); got != 32 {
		t.Fatalf("sizeof(winMouseInput)=%d want 32", got)
	}
}

func TestClampXYInsideVirtualScreen(t *testing.T) {
	x, y := clampXY(-100000, 100000)
	vx := systemMetric(smXVirtualScreen)
	vy := systemMetric(smYVirtualScreen)
	vw := systemMetric(smCXVirtualScreen)
	vh := systemMetric(smCYVirtualScreen)
	if vw <= 0 || vh <= 0 {
		t.Skip("virtual screen metrics unavailable")
	}
	if x < vx || x >= vx+vw || y < vy || y >= vy+vh {
		t.Fatalf("clamp (%d,%d) outside virtual %d,%d %dx%d", x, y, vx, vy, vw, vh)
	}
}
