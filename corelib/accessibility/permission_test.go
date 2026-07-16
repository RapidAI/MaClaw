package accessibility

import (
	"runtime"
	"testing"
)

func TestProbeDesktopPermissionsShape(t *testing.T) {
	out := ProbeDesktopPermissions()
	if out == nil {
		t.Fatal("nil")
	}
	plat, _ := out["platform"].(string)
	if plat == "" {
		t.Fatalf("missing platform: %#v", out)
	}
	if _, ok := out["ok"].(bool); !ok {
		// linux stub has ok bool
		t.Fatalf("missing ok bool: %#v", out)
	}
	switch runtime.GOOS {
	case "windows":
		if plat != "windows" {
			t.Fatalf("platform=%q", plat)
		}
		// Should not panic; may warm UIA.
		t.Logf("windows perms: %#v", out)
	case "darwin":
		if plat != "darwin" {
			t.Fatalf("platform=%q", plat)
		}
		if _, ok := out["accessibility_trusted"].(bool); !ok {
			t.Fatalf("missing accessibility_trusted: %#v", out)
		}
		t.Logf("darwin perms: %#v", out)
	default:
		if skipped, _ := out["skipped"].(bool); !skipped {
			t.Logf("other platform probe: %#v", out)
		}
	}
}
