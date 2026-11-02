package guiapp

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// Durable tool-path memory for the coding workbench command environment.
//
// A machine regularly has a toolchain installed outside the PATH this process
// inherits — `C:\Program Files\Go\bin` without a PATH entry is the reported
// case (2026-11-01 embedding task: `go build ./...` failed at the verify gate
// while go.exe sat in Program Files). The language-toolchain detectors below
// probe well-known install locations beyond PATH, and a tool they find there
// is recorded in a small registry under the data directory. A later process
// start reads the registry instead of re-probing, so a tool found once stays
// found; every read revalidates that the executable still exists, so an
// uninstalled tool never comes back from the registry. Only an off-PATH
// install is remembered: a PATH hit may be situational (an activated venv)
// and is rediscovered cheaply on every start anyway. The compiler detectors
// (MinGW, CMake, Ninja) keep their existing probe-per-process behavior.

// rememberedToolPathsPathFn resolves the registry file. Overridable so tests
// never write into a real data directory.
var rememberedToolPathsPathFn = defaultRememberedToolPathsPath

func defaultRememberedToolPathsPath() string {
	return filepath.Join(corelib.MaclawDataDir(), "tool_paths.json")
}

type rememberedToolEntry struct {
	Dir           string `json:"dir"`
	UpdatedAtUnix int64  `json:"updated_at_unix,omitempty"`
}

type rememberedToolPaths struct {
	Version int                            `json:"version"`
	Tools   map[string]rememberedToolEntry `json:"tools"`
}

var rememberedToolPathsMu sync.Mutex

// loadRememberedToolDir returns the remembered directory for exeName when the
// executable is still there. A vanished or corrupt entry is dropped so the
// next probe starts clean.
func loadRememberedToolDir(exeName string) string {
	exeName = strings.ToLower(strings.TrimSpace(exeName))
	if exeName == "" {
		return ""
	}
	rememberedToolPathsMu.Lock()
	defer rememberedToolPathsMu.Unlock()
	paths, err := readRememberedToolPaths()
	if err != nil || paths == nil {
		return ""
	}
	entry, ok := paths.Tools[exeName]
	if !ok {
		return ""
	}
	dir := strings.TrimSpace(entry.Dir)
	if dir == "" || !existingToolFile(filepath.Join(dir, exeName)) {
		dropRememberedToolEntryLocked(paths, exeName)
		return ""
	}
	return dir
}

// rememberToolDir records where a tool was found. A failed write is logged
// and ignored: the memory is an accelerator, not a requirement.
func rememberToolDir(exeName, dir string) {
	exeName = strings.ToLower(strings.TrimSpace(exeName))
	dir = strings.TrimSpace(dir)
	if exeName == "" || dir == "" {
		return
	}
	rememberedToolPathsMu.Lock()
	defer rememberedToolPathsMu.Unlock()
	paths, err := readRememberedToolPaths()
	if err != nil || paths == nil {
		paths = &rememberedToolPaths{Version: 1, Tools: map[string]rememberedToolEntry{}}
	}
	if paths.Tools == nil {
		paths.Tools = map[string]rememberedToolEntry{}
	}
	if prev, ok := paths.Tools[exeName]; ok && strings.EqualFold(strings.TrimSpace(prev.Dir), dir) {
		return
	}
	paths.Tools[exeName] = rememberedToolEntry{Dir: dir, UpdatedAtUnix: time.Now().Unix()}
	data, err := json.Marshal(paths)
	if err != nil {
		return
	}
	path := rememberedToolPathsPathFn()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[tool-paths] mkdir registry directory failed: %v", err)
		return
	}
	if err := atomicWriteFile(path, data); err != nil {
		log.Printf("[tool-paths] persist %s at %s failed: %v", exeName, dir, err)
		return
	}
	log.Printf("[tool-paths] remembered %s at %s", exeName, dir)
}

// dropRememberedToolEntryLocked removes one entry and persists. Callers hold
// rememberedToolPathsMu.
func dropRememberedToolEntryLocked(paths *rememberedToolPaths, exeName string) {
	entry, ok := paths.Tools[exeName]
	if ok {
		log.Printf("[tool-paths] dropped %s: no longer at %s", exeName, entry.Dir)
	}
	delete(paths.Tools, exeName)
	data, err := json.Marshal(paths)
	if err != nil {
		return
	}
	path := rememberedToolPathsPathFn()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := atomicWriteFile(path, data); err != nil {
		log.Printf("[tool-paths] drop %s failed: %v", exeName, err)
	}
}

// readRememberedToolPaths reads the registry file. A missing file is not an
// error: the caller starts with an empty registry. Callers hold
// rememberedToolPathsMu.
func readRememberedToolPaths() (*rememberedToolPaths, error) {
	data, err := os.ReadFile(rememberedToolPathsPathFn())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var paths rememberedToolPaths
	if err := json.Unmarshal(data, &paths); err != nil {
		log.Printf("[tool-paths] registry unreadable, starting empty: %v", err)
		return nil, err
	}
	return &paths, nil
}

var (
	windowsGoBinOnce     sync.Once
	windowsGoBin         string
	windowsNodeBinOnce   sync.Once
	windowsNodeBin       string
	windowsPythonBinOnce sync.Once
	windowsPythonBin     string
)

// detectWindowsGoBin finds the directory that contains go.exe. The step
// verify gate and coding bash share codingCommandEnv, so a machine whose PATH
// lacks the Go install can still run `go build ./...` after this probe.
func detectWindowsGoBin() string {
	return detectRememberedWindowsToolDir(&windowsGoBin, &windowsGoBinOnce, "go.exe", "[go-detect]", windowsGoCandidates)
}

// detectWindowsNodeBin finds the directory that contains node.exe.
func detectWindowsNodeBin() string {
	return detectRememberedWindowsToolDir(&windowsNodeBin, &windowsNodeBinOnce, "node.exe", "[node-detect]", windowsNodeCandidates)
}

// detectWindowsPythonBin finds the directory that contains python.exe. The
// Microsoft Store alias stub is filtered out by existingToolFile.
func detectWindowsPythonBin() string {
	return detectRememberedWindowsToolDir(&windowsPythonBin, &windowsPythonBinOnce, "python.exe", "[python-detect]", windowsPythonCandidates)
}

// detectRememberedWindowsToolDir is the remembered-registry variant of
// detectWindowsToolDir: process PATH and the registry logon Path are checked
// live on every call and their hits are recorded; the durable registry is
// consulted before the once-only slow probe; the slow probe's hit is recorded
// too. A remembered directory is only used while the executable is still in
// it, and an uninstalled tool is dropped from the registry on the read that
// notices.
func detectRememberedWindowsToolDir(slot *string, once *sync.Once, exeName, tag string, slow func() []string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	if dir := loadWindowsToolDir(slot); dir != "" {
		if existingToolFile(filepath.Join(dir, exeName)) {
			return dir
		}
		clearWindowsToolDir(slot, dir, tag)
	}
	if dir := firstExistingWindowsToolDir(windowsLiveToolCandidates(exeName)); dir != "" {
		// A PATH hit is not remembered: the PATH that produced it can be
		// situational (an activated venv's Scripts dir, a per-launch shim
		// directory). Recording it would inject that context into every later
		// process. Only a well-known install location discovered by the slow
		// probe is stable enough to remember — that is also the case the
		// registry exists for.
		return storeWindowsToolDir(slot, dir, tag)
	}
	// A tool found by an earlier process beats a fresh slow probe: this is
	// what keeps the second run reliable when PATH says nothing.
	if dir := loadRememberedToolDir(exeName); dir != "" {
		return storeWindowsToolDir(slot, dir, tag)
	}
	once.Do(func() {
		if loadWindowsToolDir(slot) != "" {
			return
		}
		if dir := firstExistingWindowsToolDir(slow()); dir != "" {
			rememberToolDir(exeName, dir)
			storeWindowsToolDir(slot, dir, tag)
		}
	})
	return loadWindowsToolDir(slot)
}

func windowsProgramFilesDirs() (string, string) {
	programFiles := strings.TrimSpace(os.Getenv("ProgramFiles"))
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	programFilesX86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)"))
	if programFilesX86 == "" {
		programFilesX86 = `C:\Program Files (x86)`
	}
	return programFiles, programFilesX86
}

// windowsLocalInstallCandidates appends the per-user install locations the
// common Windows installers use: scoop shims, Chocolatey, and WinGet links.
func windowsLocalInstallCandidates(exeName string) []string {
	var matches []string
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		matches = append(matches,
			filepath.Join(home, "scoop", "shims", exeName),
		)
	}
	matches = append(matches, `C:\ProgramData\chocolatey\bin\`+exeName)
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		matches = append(matches, filepath.Join(local, "Microsoft", "WinGet", "Links", exeName))
	}
	programFiles, _ := windowsProgramFilesDirs()
	matches = append(matches, filepath.Join(programFiles, "Microsoft", "WinGet", "Links", exeName))
	return matches
}

func windowsGoCandidates() []string {
	var matches []string
	// A GOROOT set for this process names the toolchain the user chose.
	if root := strings.TrimSpace(os.Getenv("GOROOT")); root != "" {
		matches = append(matches, filepath.Join(root, "bin", "go.exe"))
	}
	programFiles, programFilesX86 := windowsProgramFilesDirs()
	matches = append(matches,
		filepath.Join(programFiles, "Go", "bin", "go.exe"),
		filepath.Join(programFilesX86, "Go", "bin", "go.exe"),
		`C:\Go\bin\go.exe`,
	)
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		matches = append(matches, filepath.Join(local, "Programs", "Go", "bin", "go.exe"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		matches = append(matches, filepath.Join(home, "scoop", "apps", "go", "current", "bin", "go.exe"))
	}
	return append(matches, windowsLocalInstallCandidates("go.exe")...)
}

func windowsNodeCandidates() []string {
	programFiles, programFilesX86 := windowsProgramFilesDirs()
	matches := []string{
		filepath.Join(programFiles, "nodejs", "node.exe"),
		filepath.Join(programFilesX86, "nodejs", "node.exe"),
	}
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		// nvm-windows keeps one directory per version.
		found, _ := filepath.Glob(filepath.Join(appData, "nvm", "*", "node.exe"))
		matches = append(matches, found...)
	}
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		matches = append(matches, filepath.Join(local, "Programs", "nodejs", "node.exe"))
	}
	return append(matches, windowsLocalInstallCandidates("node.exe")...)
}

func windowsPythonCandidates() []string {
	programFiles, programFilesX86 := windowsProgramFilesDirs()
	var matches []string
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		found, _ := filepath.Glob(filepath.Join(local, "Programs", "Python", "Python*", "python.exe"))
		matches = append(matches, found...)
	}
	for _, root := range []string{programFiles, programFilesX86, `C:\`} {
		if strings.HasPrefix(root, string(os.PathSeparator)) {
			continue
		}
		found, _ := filepath.Glob(filepath.Join(root, "Python*", "python.exe"))
		matches = append(matches, found...)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		matches = append(matches,
			filepath.Join(home, "anaconda3", "python.exe"),
			filepath.Join(home, "miniconda3", "python.exe"),
		)
	}
	return append(matches, windowsLocalInstallCandidates("python.exe")...)
}

// probedWindowsToolchainBins returns the probed toolchain directories in
// codingCommandEnv prepend order: build tool first, then language runtimes.
func probedWindowsToolchainBins() []string {
	var dirs []string
	for _, detect := range []func() string{detectWindowsGoBin, detectWindowsNodeBin, detectWindowsPythonBin} {
		if dir := detect(); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// probedWindowsCommandDirs returns every build-tool directory this host
// probes, in codingCommandEnv prepend order: compilers first (cl.exe must
// stay ahead of any MinGW link.exe, and a compiler dir never holds go.exe),
// then the language toolchains. codingCommandEnv and the corelib command
// environment augmenter both prepend this list, so the coding bash, the step
// verify gate, hooks, the general assistant's bash, background tasks, and
// craft scripts resolve the same tools the same way.
func probedWindowsCommandDirs() []string {
	dirs := []string{}
	if mingw := detectWindowsMinGWBin(); mingw != "" {
		dirs = append(dirs, mingw)
	}
	if cmake := detectWindowsCMakeBin(); cmake != "" {
		dirs = append(dirs, cmake)
	}
	if ninja := detectWindowsNinjaBin(); ninja != "" {
		dirs = append(dirs, ninja)
	}
	return append(dirs, probedWindowsToolchainBins()...)
}

// installCorelibCommandEnvAugmenter wires the same toolchain repair into the
// corelib command surfaces (background tasks, craft scripts) that repair
// themselves in codingCommandEnv. Registered once at app startup.
func installCorelibCommandEnvAugmenter() {
	if runtime.GOOS != "windows" {
		return
	}
	coretool.SetCommandEnvAugmenter(func(env []string) []string {
		if dirs := probedWindowsCommandDirs(); len(dirs) > 0 {
			return prependWindowsPathEntries(env, dirs...)
		}
		return env
	})
}
