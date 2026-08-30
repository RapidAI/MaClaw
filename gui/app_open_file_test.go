package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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

func TestStartSystemOpenWindowsRejectsEmptyPath(t *testing.T) {
	if err := startSystemOpenWindows(""); err == nil {
		t.Fatal("expected error for empty path")
	}
	if err := startSystemOpenWindows("   "); err == nil {
		t.Fatal("expected error for whitespace path")
	}
}

func TestStartSystemOpenWindowsReportsMissingFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		if err := startSystemOpenWindows(`C:\maclaw-open-missing-xyz.pdf`); err == nil {
			t.Fatal("non-windows stub must error")
		}
		return
	}
	missing := filepath.Join(t.TempDir(), "maclaw-open-missing-xyz.pdf")
	if err := startSystemOpenWindows(missing); err == nil {
		t.Fatal("expected error for a missing file")
	}
}

func TestOpenFileOrShowInFolderExpandsTildeBeforeStat(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	app := &App{}
	// Missing path under ~ — Stat must run against the expanded absolute path (not literal "~").
	err = app.OpenFileOrShowInFolder("~/.maclaw-open-file-test-missing-xyz")
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	var pe *os.PathError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *os.PathError, got %T: %v", err, err)
	}
	if strings.Contains(pe.Path, "~") {
		t.Fatalf("path still contains tilde after expand: %q", pe.Path)
	}
	if !pathHasHomePrefix(pe.Path, home) {
		t.Fatalf("expanded path %q is not under home %q", pe.Path, home)
	}

	want := filepath.Clean(corelib.ExpandHomePath("~/.maclaw-open-file-test-missing-xyz"))
	if pe.Path != want {
		t.Fatalf("stat path = %q, want expanded %q", pe.Path, want)
	}
}

func TestOpenFileOrShowInFolderRespectsTestHomeDir(t *testing.T) {
	fakeHome := t.TempDir()
	app := &App{testHomeDir: fakeHome}
	err := app.OpenFileOrShowInFolder("~/.maclaw-open-file-test-missing-xyz")
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	var pe *os.PathError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *os.PathError, got %T: %v", err, err)
	}
	want := filepath.Clean(filepath.Join(fakeHome, ".maclaw-open-file-test-missing-xyz"))
	if pe.Path != want {
		t.Fatalf("stat path = %q, want test-home expanded %q", pe.Path, want)
	}
}

func TestShowItemInFolderRejectsEmptyPath(t *testing.T) {
	app := &App{}
	if err := app.ShowItemInFolder("   "); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestOpenProjectDirectoryExpandsTildeBeforeStat(t *testing.T) {
	// Do not create the dir / launch explorer — only assert tilde expands before Stat.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	app := &App{}
	err = app.OpenProjectDirectory("~/.maclaw-open-proj-test-missing-xyz")
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	var pe *os.PathError
	if !errors.As(err, &pe) {
		// may be wrapped; still require expanded path in error text
		if !strings.Contains(err.Error(), home) || strings.Contains(err.Error(), "~") {
			t.Fatalf("error should reference expanded home path, got %v", err)
		}
		return
	}
	if strings.Contains(pe.Path, "~") {
		t.Fatalf("path still contains tilde after expand: %q", pe.Path)
	}
	if !pathHasHomePrefix(pe.Path, home) {
		t.Fatalf("expanded path %q is not under home %q", pe.Path, home)
	}
}

func TestNormalizeOpenPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	got, err := normalizeOpenPath("~/.maclaw")
	if err != nil {
		t.Fatalf("normalizeOpenPath error: %v", err)
	}
	want := filepath.Clean(filepath.Join(home, ".maclaw"))
	if got != want {
		t.Fatalf("normalizeOpenPath(~/.maclaw) = %q, want %q", got, want)
	}
	if _, err := normalizeOpenPath("   "); err == nil {
		t.Fatal("expected empty path error")
	}

	fakeHome := t.TempDir()
	got, err = normalizeOpenPathHome("~/.maclaw", fakeHome)
	if err != nil {
		t.Fatalf("normalizeOpenPathHome error: %v", err)
	}
	want = filepath.Clean(filepath.Join(fakeHome, ".maclaw"))
	if got != want {
		t.Fatalf("normalizeOpenPathHome = %q, want %q", got, want)
	}
}

func pathHasHomePrefix(path, home string) bool {
	if path == home {
		return true
	}
	sep := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(path), strings.ToLower(home+sep)) ||
			strings.EqualFold(path, home)
	}
	return strings.HasPrefix(path, home+sep) || path == home
}
