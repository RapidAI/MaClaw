package device

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestAccessGuideIsPlatformSpecificAndNonElevating(t *testing.T) {
	for _, platform := range []string{"windows", "darwin", "linux"} {
		guide, _, _ := accessGuide(platform, errors.New("permission denied"))
		if guide == "" || strings.Contains(strings.ToLower(guide), "sudo") {
			t.Fatalf("platform=%s guide=%q", platform, guide)
		}
	}
}

func TestDiagnoseAccessRejectsUnsafePath(t *testing.T) {
	host := DiagnoseAccess("COM4; erase_flash")
	if host.Status != "unsupported" || host.Platform != runtime.GOOS {
		t.Fatalf("host=%+v", host)
	}
}
