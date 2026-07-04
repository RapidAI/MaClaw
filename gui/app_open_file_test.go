package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWindowsSystemExecutableUsesSystemRootWhenPathIsStripped(t *testing.T) {
	if runtime.GOOS != "windows" {
		if got := windowsSystemExecutable("explorer.exe"); got != "explorer.exe" {
			t.Fatalf("windowsSystemExecutable on non-Windows = %q", got)
		}
		return
	}

	root := t.TempDir()
	system32 := filepath.Join(root, "System32")
	if err := os.MkdirAll(system32, 0o755); err != nil {
		t.Fatalf("MkdirAll(System32) error = %v", err)
	}
	want := filepath.Join(system32, "explorer.exe")
	if err := os.WriteFile(want, []byte("stub"), 0o755); err != nil {
		t.Fatalf("WriteFile(explorer) error = %v", err)
	}
	t.Setenv("SystemRoot", root)
	t.Setenv("windir", "")

	if got := windowsSystemExecutable("explorer.exe"); got != want {
		t.Fatalf("windowsSystemExecutable = %q, want %q", got, want)
	}
}
