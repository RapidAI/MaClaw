//go:build windows

package guiautomation

import (
	"errors"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestDesktopInputDenialIsPrivilegeIsolation(t *testing.T) {
	if !desktopInputDenied(0, syscall.Errno(0)) {
		t.Fatal("a false cursor move with no last error is denied input")
	}
	if !desktopInputDenied(0, syscall.ERROR_ACCESS_DENIED) {
		t.Fatal("SendInput access denied is denied input")
	}
	if desktopInputDenied(1, syscall.ERROR_ACCESS_DENIED) {
		t.Fatal("a delivered event is not a denial")
	}
	// 87 is ERROR_INVALID_PARAMETER. A bad coordinate must not latch.
	if desktopInputDenied(0, syscall.Errno(87)) {
		t.Fatal("a rejected coordinate is not a denial")
	}
	cursor := inputOSError("SetCursorPos", 0, 1, syscall.Errno(0))
	if !errors.Is(cursor, ErrInputDenied) || !strings.Contains(cursor.Error(), "SetCursorPos:") {
		t.Fatal(cursor)
	}
	coord := inputOSError("SetCursorPos", 0, 1, syscall.Errno(87))
	if errors.Is(coord, ErrInputDenied) {
		t.Fatal(coord)
	}
	keys := inputOSError("SendInput", 0, 4, syscall.ERROR_ACCESS_DENIED)
	if !errors.Is(keys, ErrInputDenied) || !strings.Contains(keys.Error(), "sent 0/4") {
		t.Fatal(keys)
	}
}

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
