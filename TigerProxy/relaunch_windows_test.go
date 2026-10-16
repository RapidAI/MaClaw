//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsPowerShellExeIsInstalledAbsolutePath(t *testing.T) {
	path := windowsPowerShellExe()
	if !filepath.IsAbs(path) {
		t.Fatalf("powershell path = %q, want an absolute path", path)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		t.Fatalf("stat %q: %v", path, err)
	}
}
