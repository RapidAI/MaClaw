//go:build windows

package systray

import (
	"strings"
	"testing"
	"unsafe"
)

func TestNotifyIconDataMatchesWindows64Layout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout constants are for 64-bit")
	}
	var nid notifyIconData
	if unsafe.Sizeof(nid) != 976 {
		t.Fatalf("sizeof = %d, want 976", unsafe.Sizeof(nid))
	}
	if unsafe.Offsetof(nid.Info) != 304 || unsafe.Offsetof(nid.InfoFlags) != 948 {
		t.Fatalf("Info=%d InfoFlags=%d", unsafe.Offsetof(nid.Info), unsafe.Offsetof(nid.InfoFlags))
	}
	if notifyIconV2Size(&nid) != 952 {
		t.Fatalf("v2 size = %d, want 952", notifyIconV2Size(&nid))
	}
}

func TestNotifyIconDataTimeoutIsAUnion(t *testing.T) {
	var nid notifyIconData
	if unsafe.Offsetof(nid.InfoTitle) != unsafe.Offsetof(nid.Timeout)+4 {
		t.Fatalf("InfoTitle offset %d, Timeout offset %d", unsafe.Offsetof(nid.InfoTitle), unsafe.Offsetof(nid.Timeout))
	}
}

func TestCopyNULTerminatedUTF16FitsAndClearsTheTail(t *testing.T) {
	var dst [8]uint16
	for i := range dst {
		dst[i] = 0xFFFF
	}
	if err := copyNULTerminatedUTF16(dst[:], "任务"); err != nil {
		t.Fatal(err)
	}
	if dst[0] == 0 || dst[2] != 0 || dst[7] != 0 {
		t.Fatalf("dst = %v", dst)
	}
}

func TestBalloonSoundAliasStaysAllocated(t *testing.T) {
	if balloonSoundAlias == nil || *balloonSoundAlias == 0 {
		t.Fatal("notification sound alias was not encoded")
	}
}

func TestBalloonInfoFlagsUsesTheAppIcon(t *testing.T) {
	const (
		niifUser    = uint32(0x4)
		niifNoSound = uint32(0x10)
	)
	for _, flag := range []uint32{0, 1, 2, 3} {
		got := balloonInfoFlags(flag, true)
		if got != niifUser {
			t.Fatalf("flag %d -> %#x", flag, got)
		}
	}
	if got := balloonInfoFlags(3|niifNoSound, true); got != niifUser|niifNoSound {
		t.Fatalf("kept sound bit: %#x", got)
	}
	if got := balloonInfoFlags(2, false); got != 2 {
		t.Fatalf("fallback without icon: %#x", got)
	}
}

func TestCopyNULTerminatedUTF16CutsWithoutDroppingTheNUL(t *testing.T) {
	var dst [4]uint16
	if err := copyNULTerminatedUTF16(dst[:], strings.Repeat("项", 10)); err != nil {
		t.Fatal(err)
	}
	if dst[3] != 0 {
		t.Fatalf("missing NUL: %v", dst)
	}
	if dst[0] == 0 {
		t.Fatal("expected truncated text")
	}
}
