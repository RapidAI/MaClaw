//go:build windows

package guiapp

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsDriveKindIsLocal(t *testing.T) {
	assertKind := func(kind uint32, wantLocal, wantRemember bool) {
		t.Helper()
		local, remember := windowsDriveKindIsLocal(kind)
		if local != wantLocal || remember != wantRemember {
			t.Fatalf("kind %d local=%v remember=%v, want local=%v remember=%v", kind, local, remember, wantLocal, wantRemember)
		}
	}
	assertKind(windows.DRIVE_UNKNOWN, false, false)
	assertKind(windows.DRIVE_NO_ROOT_DIR, false, false)
	assertKind(windows.DRIVE_REMOVABLE, true, true)
	assertKind(windows.DRIVE_FIXED, true, true)
	assertKind(windows.DRIVE_REMOTE, false, true)
	assertKind(windows.DRIVE_CDROM, false, true)
	assertKind(windows.DRIVE_RAMDISK, true, true)
}

func TestWindowsDriveLetterDoesNotCacheMissingVolume(t *testing.T) {
	var missing string
	for letter := byte('Z'); letter >= 'A'; letter-- {
		vol := string(letter) + ":"
		root, err := windows.UTF16PtrFromString(vol + `\`)
		if err != nil {
			continue
		}
		if windows.GetDriveType(root) == windows.DRIVE_NO_ROOT_DIR {
			missing = vol
			break
		}
	}
	if missing == "" {
		t.Skip("every drive letter has a root")
	}
	key := strings.ToUpper(missing)
	windowsDriveLocal.Delete(key)
	if windowsDriveLetterIsLocal(missing) {
		t.Fatalf("%s was treated as local", missing)
	}
	if _, ok := windowsDriveLocal.Load(key); ok {
		t.Fatalf("%s was cached", missing)
	}

	windowsDriveLocal.Delete("C:")
	if !windowsDriveLetterIsLocal("C:") {
		t.Fatal("C: should be local")
	}
	v, ok := windowsDriveLocal.Load("C:")
	if !ok || v != true {
		t.Fatalf("C: cache = %v ok=%v", v, ok)
	}
}
