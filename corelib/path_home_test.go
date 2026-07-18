package corelib

import (
	"os"
	"path/filepath"
	"testing"
)

// Thin wrapper smoke test — full coverage lives in maclawpath.
func TestExpandHomePathWrapper(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	got := ExpandHomePath("~/.maclaw")
	want := filepath.Clean(filepath.Join(home, ".maclaw"))
	if got != want {
		t.Fatalf("ExpandHomePath(~/.maclaw) = %q, want %q", got, want)
	}
}
