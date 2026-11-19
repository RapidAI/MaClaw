//go:build windows

package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodingDirectoryFinalPathWindowsResolvesADirectory(t *testing.T) {
	dir := t.TempDir()
	got, ok := codingDirectoryFinalPathWindows(dir)
	if !ok {
		t.Fatal("expected a final path for a real directory")
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("final = %q, want %q", got, want)
	}
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := codingDirectoryFinalPathWindows(file); ok {
		t.Fatal("a file must not resolve as a directory final path")
	}
}
