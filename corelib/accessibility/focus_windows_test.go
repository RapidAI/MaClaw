//go:build windows

package accessibility

import (
	"testing"
	"unsafe"
)

func TestFocusShowKeepsVisibleWindowRect(t *testing.T) {
	cmd, ok := focusShowCommand(false, false)
	if !ok || cmd != swShow {
		t.Fatalf("visible window cmd=%d ok=%v", cmd, ok)
	}
	cmd, ok = focusShowCommand(false, true)
	if !ok || cmd != swShow {
		t.Fatalf("maximized window cmd=%d ok=%v", cmd, ok)
	}
	alt := focusAltTapEvents()
	if len(alt) != 2 || alt[0] != 0 || alt[1] != keyeventfKeyup {
		t.Fatalf("alt tap = %v", alt)
	}
	cmd, ok = focusShowCommand(true, false)
	if !ok || cmd != swRestore {
		t.Fatalf("minimized normal cmd=%d ok=%v", cmd, ok)
	}
	cmd, ok = focusShowCommand(true, true)
	if !ok || cmd != swMaximize {
		t.Fatalf("minimized from maximized cmd=%d ok=%v", cmd, ok)
	}
	flags := focusRaiseFlags()
	if flags&swpNoMove == 0 || flags&swpNoSize == 0 || flags&swpShowWindow != 0 {
		t.Fatalf("raise flags %#x move, resize, or show the window", flags)
	}
	if focusRaiseInsertAfter() != 0 {
		t.Fatal("raise must use HWND_TOP")
	}
	if unsafe.Sizeof(focusPlacement{}) != 44 {
		t.Fatalf("WINDOWPLACEMENT size %d", unsafe.Sizeof(focusPlacement{}))
	}
}
