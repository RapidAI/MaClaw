//go:build windows

package tool

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// noWindowPatchScript is a Python sitecustomize module that monkey-patches
// subprocess.Popen to automatically add CREATE_NO_WINDOW (0x08000000) to
// creationflags on Windows. This prevents child processes spawned by skill
// scripts from flashing a visible console window.
//
// We use sitecustomize.py because:
// - sitecustomize is ALWAYS executed by site.py regardless of ENABLE_USER_SITE
// - usercustomize is SKIPPED in virtualenv/venv/isolated environments
// - maclaw's bundled Python is isolated → usercustomize would not load
//
// Injected via PYTHONPATH prepend into a private directory. This means any
// existing sitecustomize.py in the Python installation's site-packages will
// be shadowed during maclaw skill execution. This is acceptable because:
// - maclaw's bundled Python has no custom sitecustomize
// - The PYTHONPATH injection only affects maclaw-spawned subprocesses
// - User's terminal Python sessions are unaffected
//
// The patch is safe:
// - Only activates on Windows (sys.platform == 'win32')
// - Preserves any explicit creationflags the caller already set (uses |=)
// - Does not interfere with CREATE_NEW_CONSOLE if explicitly requested
// - os.system() calls are unaffected (cmd.exe inherits CREATE_NO_WINDOW)
// - Idempotent: re-importing the module does not double-patch
const noWindowPatchScript = `"""Auto-injected by MaClaw to suppress console windows for subprocesses."""
import sys
if sys.platform == 'win32':
    import subprocess as _sp
    if not getattr(_sp.Popen, '_maclaw_nowindow_patched', False):
        _MACLAW_NO_WINDOW = 0x08000000
        _orig_popen_init = _sp.Popen.__init__

        def _patched_popen_init(self, *args, **kwargs):
            if 'creationflags' not in kwargs:
                kwargs['creationflags'] = _MACLAW_NO_WINDOW
            elif not (kwargs['creationflags'] & _sp.CREATE_NEW_CONSOLE):
                kwargs['creationflags'] |= _MACLAW_NO_WINDOW
            _orig_popen_init(self, *args, **kwargs)

        _sp.Popen.__init__ = _patched_popen_init
        _sp.Popen._maclaw_nowindow_patched = True
`

var (
	noWindowSiteDir     string
	noWindowSiteDirOnce sync.Once
)

// ensureNoWindowSiteCustomize writes the sitecustomize.py file to a
// private directory under the maclaw data dir. Returns the directory
// path (to be prepended to PYTHONPATH) or "" on failure.
func ensureNoWindowSiteCustomize() string {
	noWindowSiteDirOnce.Do(func() {
		base := maclawBaseDirFallback()
		if base == "" {
			return
		}
		dir := filepath.Join(base, "python_nowindow_site")
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("[tool] failed to create nowindow site dir: %v", err)
			return
		}
		target := filepath.Join(dir, "sitecustomize.py")

		// Only rewrite if content changed (avoid unnecessary disk writes).
		existing, _ := os.ReadFile(target)
		if string(existing) == noWindowPatchScript {
			noWindowSiteDir = dir
			return
		}
		if err := os.WriteFile(target, []byte(noWindowPatchScript), 0644); err != nil {
			log.Printf("[tool] failed to write nowindow sitecustomize.py: %v", err)
			return
		}
		// Clean up old usercustomize.py if it exists from a previous version.
		os.Remove(filepath.Join(dir, "usercustomize.py"))
		// Ensure no stray .py files that could interfere with skill imports.
		// Only sitecustomize.py should exist in this directory.
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			name := entry.Name()
			if name != "sitecustomize.py" && strings.HasSuffix(name, ".py") {
				os.Remove(filepath.Join(dir, name))
			}
		}
		noWindowSiteDir = dir
	})
	return noWindowSiteDir
}

// AppendNoWindowEnv prepends the no-window sitecustomize directory to
// PYTHONPATH in the given environment slice, so that all Python subprocesses
// launched by skill scripts automatically suppress console window creation.
//
// If the directory cannot be prepared (e.g. permission error), returns
// the environment unchanged.
func AppendNoWindowEnv(env []string) []string {
	siteDir := ensureNoWindowSiteCustomize()
	if siteDir == "" {
		return env
	}

	// Prepend to PYTHONPATH so our sitecustomize.py is found first by site.py.
	for i, e := range env {
		if strings.HasPrefix(e, "PYTHONPATH=") {
			existing := e[len("PYTHONPATH="):]
			// Check if already present (exact segment match, not substring).
			for _, seg := range strings.Split(existing, ";") {
				if strings.EqualFold(seg, siteDir) {
					return env
				}
			}
			env[i] = "PYTHONPATH=" + siteDir + ";" + existing
			return env
		}
	}
	return append(env, "PYTHONPATH="+siteDir)
}
