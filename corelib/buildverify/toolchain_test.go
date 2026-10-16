package buildverify

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLookupReviewedProgramUsesLogonPathWhenProcessPathMisses(t *testing.T) {
	dir := t.TempDir()
	name := "maclaw-buildverify-not-installed"
	file := name
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := logonPathDirs
	logonPathDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { logonPathDirs = orig })

	got := defaultLookupReviewedProgram(name)
	want := filepath.Join(dir, file)
	// PATHEXT on Windows is often ".EXE". CreateProcess treats that as the
	// same file as the ".exe" we wrote; the spelling comes from PATHEXT.
	if !strings.EqualFold(got, want) {
		t.Fatalf("resolved %q, want %q", got, want)
	}
}

func TestResolveReviewedProgramPrependsOnlyTheLogonHit(t *testing.T) {
	dir := t.TempDir()
	name := "maclaw-buildverify-not-installed"
	file := name
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := logonPathDirs
	logonPathDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { logonPathDirs = orig })

	got, prepend := resolveReviewedProgram(name)
	if !strings.EqualFold(got, filepath.Join(dir, file)) || !strings.EqualFold(prepend, dir) {
		t.Fatalf("path=%q prepend=%q", got, prepend)
	}
	env := prependPathDir(prepend)
	var path string
	for _, entry := range env {
		key, val, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "PATH") {
			path = val
			break
		}
	}
	if !strings.HasPrefix(strings.ToLower(path), strings.ToLower(dir+string(os.PathListSeparator))) && !strings.EqualFold(path, dir) {
		t.Fatalf("PATH=%q does not start with %q", path, dir)
	}
}

func TestLookupReviewedProgramLeavesAPathAlone(t *testing.T) {
	got := defaultLookupReviewedProgram(filepath.Join("already", "absolute-or-relative"))
	if got != filepath.Join("already", "absolute-or-relative") {
		t.Fatalf("path was rewritten: %q", got)
	}
}
