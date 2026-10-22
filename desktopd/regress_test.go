package desktopd

import (
	"os/exec"
	"strings"
	"testing"
)

// The offline regression script parses, and its read-back verdict agrees
// with corelib/browser's on the same cases (applied, not applied, too short).
func TestRegressScriptVerdict(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	out, err := exec.Command("python3", "-c", `
import importlib.util
s = importlib.util.spec_from_file_location("r", "regress/regress.py"); r = importlib.util.module_from_spec(s); s.loader.exec_module(r)
print(r.verdict("Hello  world, 你好", ["hello world,\u00a0你好 more"]), r.verdict("long dropped text", ["", "请输入"]), r.verdict("x", [""]), len(r.TASKS))
`).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "applied not_applied unknown 5" {
		t.Fatalf("got %q", got)
	}
}
