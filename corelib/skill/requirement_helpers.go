package skill

// requirement_helpers.go provides the low-level OS interaction functions used
// by the built-in Checkers and Fixers. Separated from checker logic so they
// can be overridden in tests via the exported function variables.

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/pyenv"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// --- Overridable function variables for testability ---

var commandExists = defaultCommandExists

func defaultCommandExists(name string) bool {
	_, err := exec.LookPath(name)
	if err == nil {
		return true
	}
	// Fallback: for python/pip family commands, check the bundled environment.
	// This avoids requiring users to have Python in system PATH when maclaw
	// ships its own private Python installation.
	if isBundledPythonCommand(name) {
		return bundledPythonCommandExists(name)
	}
	return false
}

// isBundledPythonCommand returns true for commands that the bundled Python
// environment can satisfy (python, python3, pip, pip3).
func isBundledPythonCommand(name string) bool {
	switch strings.ToLower(name) {
	case "python", "python3", "pip", "pip3":
		return true
	}
	return false
}

// bundledPythonCommandExists checks if the bundled Python environment can
// provide the requested command. Results are cached to avoid repeated
// subprocess spawning.
var bundledPipAvailable int32 // 0=unknown, 1=yes, -1=no
var bundledPipCheckOnce sync.Once

func bundledPythonCommandExists(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "python", "python3":
		p := resolveBundledPython()
		return p != ""
	case "pip", "pip3":
		bundledPipCheckOnce.Do(func() {
			p := resolveBundledPython()
			if p == "" {
				bundledPipAvailable = -1
				return
			}
			cmd := coretool.Command(p, "-m", "pip", "--version")
			cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
			if cmd.Run() == nil {
				bundledPipAvailable = 1
			} else {
				bundledPipAvailable = -1
			}
		})
		return bundledPipAvailable == 1
	}
	return false
}

var envLookup = defaultEnvLookup

func defaultEnvLookup(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

var findPythonExecutable = defaultFindPython

func defaultFindPython() string {
	// 1. System PATH (original logic)
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("python3"); err == nil {
			return "python3"
		}
	}
	if _, err := exec.LookPath("python"); err == nil {
		return "python"
	}
	// 2. maclaw bundled Python (fallback)
	if p := resolveBundledPython(); p != "" {
		return p
	}
	return ""
}

// FindPython returns the best available Python executable path.
// This is the exported API for callers outside the skill package (e.g. GUI
// resume parsing, PDF text extraction) that need to run Python scripts.
// Priority: system PATH python > bundled install Python > bundled venv Python.
// Returns "" if no Python is available.
func FindPython() string {
	return findPythonExecutable()
}

// MapBarePipToModule replaces bare `pip`/`pip3` command invocations with
// `python -m pip` when pip.exe is not available on PATH. This handles the
// common case where maclaw's bundled Python has pip as a module but no
// standalone pip.exe, and SkillHub-installed skills that use bare `pip`.
func MapBarePipToModule(command string) string {
	if !PipNeedsModuleMapping() {
		return command
	}
	lines := strings.Split(command, "\n")
	changed := false
	for i, line := range lines {
		newLine := mapPipInLine(line)
		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}
	if changed {
		return strings.Join(lines, "\n")
	}
	return command
}

// mapPipInLine handles pip replacement in a single line, including after
// shell operators (&&, ||, ;).
func mapPipInLine(line string) string {
	result := line
	// Check at beginning of line
	trimmed := strings.TrimSpace(result)
	lower := strings.ToLower(trimmed)
	replaced := false
	for _, cmd := range []string{"pip3", "pip"} {
		if strings.HasPrefix(lower, cmd+" ") || strings.HasPrefix(lower, cmd+"\t") || lower == cmd {
			idx := strings.Index(strings.ToLower(result), cmd)
			if idx >= 0 {
				result = result[:idx] + "python -m pip" + result[idx+len(cmd):]
				replaced = true
				break
			}
		}
	}
	if !replaced {
		// Check after shell operators
		for _, sep := range []string{" && ", " || ", "; "} {
			if !strings.Contains(result, sep) {
				continue
			}
			parts := strings.Split(result, sep)
			anyPartChanged := false
			for j := 1; j < len(parts); j++ {
				pt := strings.TrimSpace(parts[j])
				pl := strings.ToLower(pt)
				for _, cmd := range []string{"pip3", "pip"} {
					if strings.HasPrefix(pl, cmd+" ") || strings.HasPrefix(pl, cmd+"\t") || pl == cmd {
						leading := parts[j][:len(parts[j])-len(strings.TrimLeft(parts[j], " \t"))]
						parts[j] = leading + "python -m pip" + pt[len(cmd):]
						anyPartChanged = true
						break
					}
				}
			}
			if anyPartChanged {
				result = strings.Join(parts, sep)
				break
			}
		}
	}
	return result
}

// PipNeedsModuleMapping returns true when `pip` is not available as a
// standalone executable but `python -m pip` works. Result is cached.
var PipNeedsModuleMapping = sync.OnceValue(func() bool {
	// If pip.exe exists on PATH, no mapping needed.
	if _, err := exec.LookPath("pip"); err == nil {
		return false
	}
	// Check if python -m pip is available.
	python := findPythonExecutable()
	if python == "" {
		return false
	}
	cmd := coretool.Command(python, "-m", "pip", "--version")
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	return cmd.Run() == nil
})

// resolveBundledPython returns the path to maclaw's bundled Python executable.
// Priority: venv Python (has installed packages) > install Python (bare).
// Returns "" if no bundled Python is available.
//
// Result is cached after first successful resolution to avoid repeated
// subprocess spawning (pyenv.Detect calls `python --version`).
var bundledPythonPath string
var bundledPythonOnce sync.Once

func resolveBundledPython() string {
	bundledPythonOnce.Do(func() {
		bundledPythonPath = doResolveBundledPython()
	})
	return bundledPythonPath
}

func doResolveBundledPython() string {
	// Priority: install Python > venv Python.
	//
	// Rationale: The install Python (`~/.maclaw/python/install/python.exe`) has
	// pip module built-in and all uv-installed packages in its site-packages.
	// The venv Python is a lightweight wrapper that may NOT have pip module
	// (uv creates minimal venvs without pip) and may not see all installed
	// packages depending on uv's site-packages layout.
	//
	// Verified empirically:
	//   install/python.exe -m pip --version  → pip 24.3.1 ✓
	//   venv/Scripts/python.exe -m pip       → No module named pip ✗
	//   install/python.exe -c "import pymupdf" → works ✓
	//   venv/Scripts/python.exe -c "import pymupdf" → ModuleNotFoundError ✗
	st := pyenv.Detect()
	if st.Available && st.IsPrivate {
		return st.PythonPath
	}
	// Fallback to venv Python (unlikely to be better, but covers edge cases
	// where Detect() fails but venv exists).
	if p, err := pyenv.VenvPython(); err == nil {
		return p
	}
	return ""
}

var checkPipInstalled = defaultCheckPipInstalled

func defaultCheckPipInstalled(python, name string) bool {
	cmd := coretool.Command(python, "-m", "pip", "show", name)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	return cmd.Run() == nil
}

// installPipPkgScoped installs a pip package with correct scoping:
//   - If the resolved python is the bundled private Python → install normally
//     (bundled Python's site-packages is private, no global pollution)
//   - If the resolved python is a system Python → install with --user flag
//     (per-user scope, no admin needed, Python auto-discovers user site-packages)
//
// The install scope is determined by the Python executable identity, ensuring
// install-check-runtime symmetry: packages are always installed to a location
// that the same Python's sys.path already includes.
var installPipPkgScoped = defaultInstallPipPkgScoped

func defaultInstallPipPkgScoped(python, pkg string) error {
	needsUserScope := !isIsolatedPythonEnvironment(python)
	var args []string
	if needsUserScope {
		// System Python outside any virtual environment: install to user scope
		// (--user). This avoids admin permissions while keeping packages
		// accessible via Python's standard user site-packages directory.
		args = []string{"-m", "pip", "install", "--quiet", "--user", pkg}
	} else {
		// Isolated Python (bundled maclaw Python or active virtualenv): install
		// normally. The environment is inherently isolated — no global pollution.
		// Using --user here would fail with "User site-packages are not visible
		// in this environment" in virtualenvs.
		args = []string{"-m", "pip", "install", "--quiet", pkg}
	}
	cmd := coretool.Command(python, args...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pip install %s failed: %v\n%s", pkg, err, strings.TrimSpace(string(out)))
	}
	scope := "isolated"
	if needsUserScope {
		scope = "user"
	}
	log.Printf("[requirement] pip install %s success (scope=%s python=%s)", pkg, scope, python)
	return nil
}

// isIsolatedPythonEnvironment returns true if the Python executable lives in
// an isolated environment where global `pip install` is safe (won't pollute
// the system). Two cases qualify:
//  1. maclaw's bundled private Python (isBundledPythonPath)
//  2. A virtualenv / venv (detected by checking for pyvenv.cfg or VIRTUAL_ENV)
//
// In both cases, --user is unnecessary (isolated) and may even fail (virtualenvs
// disable user site-packages by default).
func isIsolatedPythonEnvironment(pythonPath string) bool {
	if isBundledPythonPath(pythonPath) {
		return true
	}
	// Check for virtualenv/venv indicators:
	// The standard signal is a pyvenv.cfg file in the same directory as the
	// Python executable (or one level up on Windows where python.exe is in Scripts/).
	resolved := pythonPath
	if !filepath.IsAbs(pythonPath) {
		if p, err := exec.LookPath(pythonPath); err == nil {
			resolved = p
		}
	}
	absPath, err := filepath.Abs(resolved)
	if err != nil {
		return false
	}
	dir := filepath.Dir(absPath)
	// pyvenv.cfg in the same directory (Linux/macOS: bin/python → pyvenv.cfg is in parent)
	if _, err := os.Stat(filepath.Join(dir, "pyvenv.cfg")); err == nil {
		return true
	}
	// pyvenv.cfg one level up (common layout: venv/bin/python, venv/pyvenv.cfg)
	parent := filepath.Dir(dir)
	if parent != dir {
		if _, err := os.Stat(filepath.Join(parent, "pyvenv.cfg")); err == nil {
			return true
		}
	}
	return false
}

// isBundledPythonPath returns true if the python executable path points to
// maclaw's private bundled Python installation. This determines install scope:
// bundled Python can install globally (it's private), system Python cannot.
func isBundledPythonPath(pythonPath string) bool {
	if pythonPath == "" {
		return false
	}
	bundled := resolveBundledPython()
	if bundled == "" {
		return false
	}
	// Resolve bare command names (e.g. "python") to their actual path.
	// Without this, filepath.Abs("python") produces a bogus path like
	// "{cwd}/python" instead of the actual executable location.
	resolved := pythonPath
	if !filepath.IsAbs(pythonPath) {
		if p, err := exec.LookPath(pythonPath); err == nil {
			resolved = p
		}
	}
	// Normalize and compare — on Windows paths are case-insensitive.
	absP, err1 := filepath.Abs(resolved)
	absB, err2 := filepath.Abs(bundled)
	if err1 != nil || err2 != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(absP, absB)
	}
	return absP == absB
}

// checkNpmInstalledInDir checks if an npm package is installed, optionally
// scoped to a specific directory. Checks local (dir or cwd) first, then global.
var checkNpmInstalledInDir = defaultCheckNpmInstalledInDir

func defaultCheckNpmInstalledInDir(name, dir string) bool {
	cmd := coretool.Command("npm", "list", name, "--depth=0")
	if dir != "" {
		cmd.Dir = dir
	}
	if cmd.Run() == nil {
		return true
	}
	cmd = coretool.Command("npm", "list", "-g", name, "--depth=0")
	return cmd.Run() == nil
}

// installNpmPkgInDir installs an npm package, optionally in a specific
// directory. When dir is non-empty, `npm install` runs with Dir set,
// installing locally to that directory (no elevated permissions needed).
var installNpmPkgInDir = defaultInstallNpmPkgInDir

func defaultInstallNpmPkgInDir(pkg, dir string) error {
	cmd := coretool.Command("npm", "install", "--silent", pkg)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm install %s failed: %v\n%s", pkg, err, strings.TrimSpace(string(out)))
	}
	log.Printf("[requirement] npm install %s success (dir=%q)", pkg, dir)
	return nil
}

// --- Stable PATH injection for skill subprocess execution ---

func ensureWindowsSystemDirsInPATH(env []string) []string {
	if runtime.GOOS != "windows" {
		return env
	}

	root := strings.TrimSpace(envLookup("SystemRoot"))
	if root == "" {
		root = strings.TrimSpace(envLookup("windir"))
	}
	if root == "" {
		root = `C:\Windows`
	}

	dirs := []string{
		filepath.Join(root, "System32"),
		root,
		filepath.Join(root, "System32", "Wbem"),
		filepath.Join(root, "System32", "WindowsPowerShell", "v1.0"),
	}
	return prependDirsToPATH(env, dirs)
}

func prependDirsToPATH(env []string, dirs []string) []string {
	if len(dirs) == 0 {
		return env
	}

	pathKey := "PATH"
	if runtime.GOOS == "windows" {
		pathKey = "Path"
	}

	pathIdx := -1
	currentPath := ""
	for i, item := range env {
		name, val, ok := strings.Cut(item, "=")
		if ok && envNameEqual(name, pathKey) {
			pathIdx = i
			currentPath = val
			pathKey = name
			break
		}
	}

	sep := string(os.PathListSeparator)
	currentParts := strings.Split(currentPath, sep)
	currentSet := make(map[string]bool, len(currentParts))
	for _, p := range currentParts {
		norm := strings.TrimRight(p, string(os.PathSeparator))
		if runtime.GOOS == "windows" {
			norm = strings.ToLower(norm)
		}
		if norm != "" {
			currentSet[norm] = true
		}
	}

	var prepend []string
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		norm := strings.TrimRight(dir, string(os.PathSeparator))
		if runtime.GOOS == "windows" {
			norm = strings.ToLower(norm)
		}
		if norm == "" || currentSet[norm] {
			continue
		}
		currentSet[norm] = true
		prepend = append(prepend, dir)
	}
	if len(prepend) == 0 {
		return env
	}

	newPath := strings.Join(prepend, sep)
	if currentPath != "" {
		newPath = newPath + sep + currentPath
	}

	assignment := pathKey + "=" + newPath
	if pathIdx >= 0 {
		env[pathIdx] = assignment
	} else {
		env = append(env, assignment)
	}
	return env
}

// bundledPythonPathDirs caches the directory paths that should be prepended
// to PATH so that skill bash steps can find python/pip without requiring
// the user to have Python in system PATH.
//
// Computed once on first call. Empty if no bundled Python is available.
var bundledPythonPathDirs []string
var bundledPythonPathOnce sync.Once

// computeBundledPythonPathDirs detects the bundled Python installation and
// returns the directory paths containing python.exe and pip.exe (or the Scripts
// dir on Windows / bin dir on Unix).
func computeBundledPythonPathDirs() []string {
	// Reuse the cached bundled python path to avoid a second pyenv.Detect() call.
	pythonPath := resolveBundledPython()
	if pythonPath == "" {
		return nil
	}

	seen := make(map[string]bool)
	var dirs []string
	addDir := func(p string) {
		if p == "" {
			return
		}
		dir := filepath.Dir(p)
		if dir == "" || dir == "." || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	// Private install dir first — has python.exe, pip module, and installed packages.
	addDir(pythonPath) // e.g. ~/.maclaw/python/install/
	// install/Scripts (Windows) — has pip.exe and other CLI tools
	if runtime.GOOS == "windows" {
		installScripts := filepath.Join(filepath.Dir(pythonPath), "Scripts")
		if _, err := os.Stat(installScripts); err == nil {
			if !seen[installScripts] {
				seen[installScripts] = true
				dirs = append(dirs, installScripts)
			}
		}
	}

	// Venv Scripts/bin — secondary, may have additional CLI tools
	if venvPy, err := pyenv.VenvPython(); err == nil {
		addDir(venvPy)
	}

	return dirs
}

// ensureBundledPythonInPATH prepends the bundled Python directories to PATH
// in the given environment slice if they are not already present. This ensures
// that `pip`, `python` etc. are resolvable in skill bash subprocesses even
// when the user has no system Python in PATH.
//
// This function is safe to call repeatedly (idempotent) and caches the
// bundled paths on first use.
func ensureBundledPythonInPATH(env []string) []string {
	bundledPythonPathOnce.Do(func() {
		bundledPythonPathDirs = computeBundledPythonPathDirs()
	})
	if len(bundledPythonPathDirs) == 0 {
		return env
	}
	return prependDirsToPATH(env, bundledPythonPathDirs)
}
