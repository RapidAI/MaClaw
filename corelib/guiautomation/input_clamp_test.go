package guiautomation

import "testing"

func TestClampToVirtual(t *testing.T) {
	// Secondary monitor to the right of a 1920-wide primary must not be pulled back.
	x, y := clampToVirtual(2500, 100, 0, 0, 3840, 1080)
	if x != 2500 || y != 100 {
		t.Fatalf("right monitor: %d,%d", x, y)
	}
	// Monitor left of the primary uses a negative origin.
	x, y = clampToVirtual(-5000, -20, -1920, 0, 3840, 1080)
	if x != -1920 || y != 0 {
		t.Fatalf("left monitor clamp: %d,%d", x, y)
	}
	x, y = clampToVirtual(-100, 40, -1920, 0, 3840, 1080)
	if x != -100 || y != 40 {
		t.Fatalf("point on left monitor should stay: %d,%d", x, y)
	}
}

func TestTypeUsesClipboardForNonASCII(t *testing.T) {
	if typeUsesClipboard("hello") || typeUsesClipboard("Line\nTab\t") {
		t.Fatal("ascii keystrokes should stay on SendInput")
	}
	if !typeUsesClipboard("你好") || !typeUsesClipboard("hello 世界") {
		t.Fatal("non-ascii text should paste, so IM and Office receive it")
	}
}
