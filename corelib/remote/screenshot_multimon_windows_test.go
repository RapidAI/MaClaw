//go:build windows

package remote

import (
	"testing"
	"unsafe"
)

func TestMonitorInfoExMatchesWindowsLayout(t *testing.T) {
	if unsafe.Sizeof(monitorInfoEx{}) != 104 {
		t.Fatalf("MONITORINFOEXW size = %d, want 104", unsafe.Sizeof(monitorInfoEx{}))
	}
}

func TestEnumDisplaysMatchVirtualScreen(t *testing.T) {
	displays, err := EnumDisplays()
	if err != nil {
		t.Fatal(err)
	}
	if !displays[0].Primary || displays[0].Index != 0 {
		t.Fatalf("screen index 0 must be the primary monitor, got %+v", displays[0])
	}
	vx, _, _ := procGetSystemMetrics.Call(uintptr(smXVirtualScreen))
	vy, _, _ := procGetSystemMetrics.Call(uintptr(smYVirtualScreen))
	vw, _, _ := procGetSystemMetrics.Call(uintptr(smCXVirtualScreen))
	vh, _, _ := procGetSystemMetrics.Call(uintptr(smCYVirtualScreen))
	minX, minY := displays[0].X, displays[0].Y
	maxX, maxY := minX+displays[0].Width, minY+displays[0].Height
	primary := displays[0].Primary
	for _, d := range displays[1:] {
		if d.X < minX {
			minX = d.X
		}
		if d.Y < minY {
			minY = d.Y
		}
		if d.X+d.Width > maxX {
			maxX = d.X + d.Width
		}
		if d.Y+d.Height > maxY {
			maxY = d.Y + d.Height
		}
		primary = primary || d.Primary
	}
	if !primary {
		t.Fatal("no primary display")
	}
	originX, originY := int(int32(vx)), int(int32(vy))
	if minX != originX || minY != originY || maxX-minX != int(vw) || maxY-minY != int(vh) {
		t.Fatalf("display union (%d,%d %dx%d) != virtual screen (%d,%d %dx%d) displays=%+v",
			minX, minY, maxX-minX, maxY-minY, originX, originY, int(vw), int(vh), displays)
	}
}
