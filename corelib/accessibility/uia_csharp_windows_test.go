//go:build windows

package accessibility

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCompileAndPingCSharpUIASidecar(t *testing.T) {
	dir := t.TempDir()
	corelib.SetMaclawBaseDir(dir)
	ResetUIACsharpCache()

	exe := uiaCSharpSidecarPath()
	if exe == "" {
		if findCSC() == "" {
			t.Skip("csc.exe not available")
		}
		t.Fatal("expected csharp sidecar path after compile attempt")
	}
	if st, err := os.Stat(exe); err != nil || st.Size() == 0 {
		t.Fatalf("exe missing: %v", err)
	}
	// Must live under our temp base bin/
	if filepath.Base(exe) != uiaCSharpExeName {
		t.Fatalf("unexpected name %s", exe)
	}

	// Start sidecar and ping via package global.
	globalUIASidecar.mu.Lock()
	globalUIASidecar.stop()
	err := globalUIASidecar.startProcess(exe, nil, "csharp")
	globalUIASidecar.mu.Unlock()
	if err != nil {
		t.Fatalf("start csharp: %v", err)
	}
	defer func() {
		globalUIASidecar.mu.Lock()
		globalUIASidecar.stop()
		globalUIASidecar.mu.Unlock()
	}()

	if !UIASidecarAlive() {
		t.Fatal("sidecar not alive")
	}
	if UIABackend() != "csharp" {
		t.Fatalf("backend=%q want csharp", UIABackend())
	}
	// enum top-level windows should not error; on a wedged-UIA machine the
	// desktop query can block until the sidecar deadline — skip there.
	els, err := globalUIASidecar.enum("", 1)
	if err != nil {
		if strings.Contains(err.Error(), "timed out") {
			t.Skipf("UIA unresponsive on this machine (enum timed out): %v", err)
		}
		t.Fatalf("enum: %v", err)
	}
	t.Logf("top-level windows via csharp: %d", len(els))
}
