package tool

import (
	"os"
	"runtime"
	"testing"
)

func TestResolveWindowsPowerShell(t *testing.T) {
	path, err := ResolveWindowsPowerShell()
	if runtime.GOOS != "windows" {
		if path != "bash" || err != nil {
			t.Fatalf("non-windows: expected (bash, nil), got (%q, %v)", path, err)
		}
		return
	}
	// On Windows, must find a PowerShell executable.
	if err != nil {
		t.Fatalf("Windows: ResolveWindowsPowerShell failed: %v", err)
	}
	if path == "" {
		t.Fatal("Windows: resolved path is empty")
	}
	// Verify the resolved path actually exists on disk.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Windows: resolved path %q does not exist: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("Windows: resolved path %q is a directory, not a file", path)
	}
	t.Logf("Resolved PowerShell: %s", path)
}

func TestResolveWindowsPowerShell_Cached(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	// Call twice — second call should return same cached result.
	p1, err1 := ResolveWindowsPowerShell()
	p2, err2 := ResolveWindowsPowerShell()
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if p1 != p2 {
		t.Fatalf("cached result mismatch: %q vs %q", p1, p2)
	}
}

func TestDoResolvePowerShell_SkipsRelativePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	// doResolvePowerShell should never return a relative path even if
	// knownPaths construction produces one (e.g. empty SystemRoot).
	// We can't easily simulate empty SystemRoot without affecting LookPath,
	// so just verify the result is absolute.
	p, err := doResolvePowerShell()
	if err != nil {
		t.Skipf("PowerShell not found on this system: %v", err)
	}
	if !os.IsPathSeparator(p[0]) && (len(p) < 3 || p[1] != ':') {
		t.Fatalf("resolved path is not absolute: %q", p)
	}
}
