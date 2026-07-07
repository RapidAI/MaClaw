package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// ResolveWindowsPowerShell returns the full path to an available PowerShell
// executable on Windows. It uses a multi-level fallback chain:
//
//  1. exec.LookPath("pwsh")        — PowerShell 7+ (cross-platform, preferred)
//  2. exec.LookPath("powershell")  — Windows PowerShell 5.x via PATH
//  3. Known system path fallback   — covers cases where PATH is incomplete
//     (e.g. GUI apps inheriting a stripped PATH, or custom Windows images)
//
// Successful resolution is cached for process lifetime (the executable won't
// move). Failed resolution is retried on each call so that PATH fixes during
// the process lifetime take effect.
//
// On non-Windows, this always returns ("bash", nil).
func ResolveWindowsPowerShell() (string, error) {
	if runtime.GOOS != "windows" {
		return "bash", nil
	}
	// Fast path: return cached successful result.
	if cached := resolvedPSPath.Load(); cached != nil {
		return *cached, nil
	}
	// Slow path: resolve and cache on success.
	p, err := doResolvePowerShell()
	if err != nil {
		return "", err
	}
	resolvedPSPath.Store(&p)
	return p, nil
}

// resolvedPSPath caches the first successfully resolved PowerShell path.
// Using atomic.Pointer instead of sync.OnceValues so that failures are not
// permanently cached — allows recovery if PATH is fixed at runtime.
var resolvedPSPath atomic.Pointer[string]

func doResolvePowerShell() (string, error) {
	// Prefer PowerShell 7+ (pwsh) — better UTF-8 support, faster startup.
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p, nil
	}

	// Try Windows PowerShell 5.x from PATH.
	if p, err := exec.LookPath("powershell"); err == nil {
		return p, nil
	}

	// PATH is broken/incomplete — try well-known absolute paths.
	// This covers the common case where maclaw GUI process inherits a
	// stripped PATH (e.g. launched from Start Menu shortcut, or Windows
	// Defender/indexer modified env).
	seen := make(map[string]struct{}, 4)
	var knownPaths []string
	addPath := func(p string) {
		if p == "" || !filepath.IsAbs(p) {
			return
		}
		norm := filepath.Clean(strings.ToLower(p))
		if _, dup := seen[norm]; dup {
			return
		}
		seen[norm] = struct{}{}
		knownPaths = append(knownPaths, p)
	}

	if sysRoot := os.Getenv("SystemRoot"); sysRoot != "" {
		addPath(filepath.Join(sysRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
	}
	// Hardcoded fallback in case SystemRoot is also missing or non-standard.
	addPath(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`)
	if progFiles := os.Getenv("ProgramFiles"); progFiles != "" {
		addPath(filepath.Join(progFiles, "PowerShell", "7", "pwsh.exe"))
	}
	addPath(`C:\Program Files\PowerShell\7\pwsh.exe`)

	for _, p := range knownPaths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}

	return "", &exec.Error{Name: "powershell", Err: exec.ErrNotFound}
}
