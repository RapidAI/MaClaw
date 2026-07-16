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
}

func TestClampXY(t *testing.T) {
	x, y := clampXY(-10, -5)
	if x < 0 || y < 0 {
		t.Fatalf("clamp failed: %d,%d", x, y)
	}
}
