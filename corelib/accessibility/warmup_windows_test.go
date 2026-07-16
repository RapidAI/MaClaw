//go:build windows

package accessibility

import (
	"testing"
)

func TestWarmupUIA(t *testing.T) {
	w := WarmupUIA()
	if w == nil {
		t.Fatal("WarmupUIA returned nil")
	}
	if _, ok := w["ok"].(bool); !ok {
		t.Fatalf("missing ok bool: %#v", w)
	}
	// Backend may be csharp or powershell depending on environment.
	backend, _ := w["backend"].(string)
	alive, _ := w["alive"].(bool)
	ok, _ := w["ok"].(bool)
	t.Logf("warmup ok=%v backend=%q alive=%v windows=%v ms=%v err=%v",
		ok, backend, alive, w["windows"], w["ms"], w["error"])
	if ok {
		if !alive {
			t.Error("ok=true but alive=false")
		}
		if backend != "csharp" && backend != "powershell" {
			t.Errorf("unexpected backend %q", backend)
		}
	}
}

func TestSelfCheckUIA(t *testing.T) {
	out := SelfCheckUIA()
	if out == nil {
		t.Fatal("SelfCheckUIA returned nil")
	}
	if plat, _ := out["platform"].(string); plat != "windows" {
		t.Fatalf("platform=%v want windows", out["platform"])
	}
	// csc_found / csharp_exe may or may not exist; structure must be present.
	if _, ok := out["csc_found"].(bool); !ok {
		t.Fatalf("missing csc_found: %#v", out)
	}
	if _, ok := out["ok"].(bool); !ok {
		t.Fatalf("missing ok: %#v", out)
	}
	t.Logf("selfcheck ok=%v backend_after=%v csharp_path=%v csc=%v error=%v",
		out["ok"], out["backend_after"], out["csharp_path"], out["csc_found"], out["error"])
	// If anything is alive after check, ok must be true (PS fallback counts).
	if alive, _ := out["alive_after"].(bool); alive {
		if ok, _ := out["ok"].(bool); !ok {
			t.Error("alive_after=true but ok=false")
		}
	}
}
