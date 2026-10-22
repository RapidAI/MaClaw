package guiapp

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// windowsMSVCToolchain is a host-side discovery result for MSVC / Visual Studio.
// coding bash injects vcvars into the process environment so the model can run
// `cl` without quoting vcvars64.bat through PowerShell.
type windowsMSVCToolchain struct {
	DisplayName string
	InstallPath string
	Version     string
	VCVars64    string
}

var (
	windowsMSVCDetectOnce    sync.Once
	windowsMSVCDetected      *windowsMSVCToolchain
	windowsMSVCEnvOnce       sync.Once
	windowsMSVCEnv           []string
	windowsMSVCEnvMu         sync.Mutex
	windowsMSVCEnvHadCl      bool
	windowsMinGWBinOnce      sync.Once
	windowsMinGWBin          string
	windowsCMakeBinOnce      sync.Once
	windowsCMakeBin          string
	windowsToolDirMu         sync.Mutex
	privateCMakeMu           sync.Mutex
	privateInstalledCMakeDir string
	windowsNinjaBinOnce      sync.Once
	windowsNinjaBin          string
	windowsCMakeInstalling   bool
	windowsSDKOnce           sync.Once
	windowsSDKCachedRoot     string
	windowsSDKCachedVersion  string
)

// installPrivateCMakeFn installs CMake for a machine that has none.
// Tests replace it. The default does not ask the user to install anything.
var installPrivateCMakeFn = defaultInstallPrivateCMake

// detectWindowsMSVCToolchain finds a local Visual Studio install that includes
// C++ tools. Cached for the process lifetime. Returns nil on non-Windows or when
// no suitable install is found.
func detectWindowsMSVCToolchain() *windowsMSVCToolchain {
	if runtime.GOOS != "windows" {
		return nil
	}
	windowsMSVCDetectOnce.Do(func() {
		windowsMSVCDetected = probeWindowsMSVCToolchain()
		if windowsMSVCDetected != nil {
			log.Printf("[msvc-detect] found %s path=%q vcvars=%q",
				windowsMSVCDetected.DisplayName,
				windowsMSVCDetected.InstallPath,
				windowsMSVCDetected.VCVars64,
			)
			// Prompt construction already detected VS; capture vcvars now so
			// the first bash compile does not wait on cmd.exe + set.
			go func() { _ = windowsMSVCInjectedEnviron() }()
		}
	})
	return windowsMSVCDetected
}

func probeWindowsMSVCToolchain() *windowsMSVCToolchain {
	vswhere := windowsVswherePath()
	if vswhere != "" {
		if tc := probeWindowsMSVCViaVswhere(vswhere); tc != nil {
			return tc
		}
	}
	// vswhere can be missing or return nothing for a per-user / preview
	// install. The registry and the process environment still name it.
	if tc := probeWindowsMSVCFromEnv(); tc != nil {
		return tc
	}
	if tc := probeWindowsMSVCFromRegistry(); tc != nil {
		return tc
	}
	return probeWindowsMSVCFromCommonPaths()
}

// probeWindowsMSVCFromEnv uses a developer prompt that already exported
// VSINSTALLDIR. The GUI itself is not launched from that prompt, so this is
// only a hit when the parent process was.
func probeWindowsMSVCFromEnv() *windowsMSVCToolchain {
	if runtime.GOOS != "windows" {
		return nil
	}
	dir := strings.TrimRight(strings.TrimSpace(os.Getenv("VSINSTALLDIR")), `\/`)
	if dir == "" {
		return nil
	}
	return toolchainFromInstallPath(dir, "VSINSTALLDIR", "")
}

// vswhereJSONEntry is the subset of vswhere -format json we need.
type vswhereJSONEntry struct {
	InstallationPath    string `json:"installationPath"`
	DisplayName         string `json:"displayName"`
	InstallationVersion string `json:"installationVersion"`
}

func probeWindowsMSVCViaVswhere(vswhere string) *windowsMSVCToolchain {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// One process: prefer C++ toolset installs; fall back to any latest product.
	attempts := [][]string{
		{
			"-latest", "-products", "*",
			"-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
			"-format", "json",
		},
		{
			"-latest", "-products", "*",
			"-format", "json",
		},
	}
	for _, args := range attempts {
		entries := runVswhereJSON(ctx, vswhere, args...)
		for _, e := range entries {
			tc := toolchainFromInstallPath(e.InstallationPath, e.DisplayName, e.InstallationVersion)
			if tc != nil {
				return tc
			}
		}
	}
	return nil
}

func runVswhereJSON(ctx context.Context, vswhere string, args ...string) []vswhereJSONEntry {
	cmd := exec.CommandContext(ctx, vswhere, args...)
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	// vswhere may return a single object or an array depending on flags/version.
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '{' {
		var one vswhereJSONEntry
		if json.Unmarshal(trimmed, &one) == nil && strings.TrimSpace(one.InstallationPath) != "" {
			return []vswhereJSONEntry{one}
		}
		return nil
	}
	var many []vswhereJSONEntry
	if json.Unmarshal(trimmed, &many) != nil {
		return nil
	}
	return many
}

func windowsVswherePath() string {
	seen := map[string]bool{}
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft Visual Studio", "Installer", "vswhere.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft Visual Studio", "Installer", "vswhere.exe"),
		`C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe`,
		`C:\Program Files\Microsoft Visual Studio\Installer\vswhere.exe`,
	}
	for _, p := range candidates {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func probeWindowsMSVCFromCommonPaths() *windowsMSVCToolchain {
	var installs []vsInstallRef
	forEachCommonWindowsVSInstall(func(install, displayName, version string) bool {
		installs = append(installs, vsInstallRef{path: install, name: displayName, version: version})
		return false
	})
	return newestWindowsMSVCToolchain(installs)
}

// vsInstallRef is one Visual Studio root found in the registry or on disk.
type vsInstallRef struct {
	path, name, version string
}

// newestWindowsMSVCToolchain keeps the install whose cl.exe toolset is highest.
// A root that only has vcvars64.bat loses to one that also has cl.exe.
func newestWindowsMSVCToolchain(installs []vsInstallRef) *windowsMSVCToolchain {
	var best *windowsMSVCToolchain
	bestCl := ""
	seen := map[string]bool{}
	for _, inst := range installs {
		path := strings.Trim(strings.TrimSpace(inst.path), `"`)
		path = strings.TrimRight(path, `\/`)
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if seen[key] {
			continue
		}
		seen[key] = true
		tc := toolchainFromInstallPath(path, inst.name, inst.version)
		if tc == nil {
			continue
		}
		_, clDir := discoverWindowsMSVCClDir(path)
		cl := ""
		if clDir != "" {
			cl = filepath.Join(clDir, "cl.exe")
			if strings.TrimSpace(tc.Version) == "" || !strings.Contains(tc.Version, ".") {
				tc.Version = msvcToolsetVersion(cl)
			}
		}
		if best == nil || (cl != "" && (bestCl == "" || msvcClBetter(cl, bestCl))) {
			best = tc
			bestCl = cl
		}
	}
	return best
}

// forEachCommonWindowsVSInstall walks the well-known VS roots, newest product
// line first. fn returns true to stop.
func forEachCommonWindowsVSInstall(fn func(install, displayName, version string) bool) {
	roots := uniqueNonEmptyStrings([]string{
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft Visual Studio"),
		`C:\Program Files\Microsoft Visual Studio`,
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft Visual Studio"),
		`C:\Program Files (x86)\Microsoft Visual Studio`,
	})
	// Directory names, newest product line first. VS 2026 is "18"; 2017 is "2017".
	years := []string{"18", "2026", "2022", "2019", "2017"}
	editions := []string{"Community", "Professional", "Enterprise", "BuildTools", "Preview"}
	for _, root := range roots {
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			continue
		}
		for _, year := range years {
			yearDir := filepath.Join(root, year)
			if fi, err := os.Stat(yearDir); err != nil || !fi.IsDir() {
				continue
			}
			for _, edition := range editions {
				install := filepath.Join(yearDir, edition)
				if fi, err := os.Stat(install); err != nil || !fi.IsDir() {
					continue
				}
				if fn(install, "Visual Studio "+edition+" "+year, year) {
					return
				}
			}
		}
	}
}

func uniqueNonEmptyStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	return out
}

func toolchainFromInstallPath(installPath, displayName, version string) *windowsMSVCToolchain {
	installPath = strings.TrimSpace(installPath)
	if installPath == "" {
		return nil
	}
	// Normalize quotes vswhere sometimes leaves.
	installPath = strings.Trim(installPath, "\"")
	vcvars := filepath.Join(installPath, "VC", "Auxiliary", "Build", "vcvars64.bat")
	if fi, err := os.Stat(vcvars); err != nil || fi.IsDir() {
		return nil
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = filepath.Base(installPath)
	}
	return &windowsMSVCToolchain{
		DisplayName: strings.TrimSpace(displayName),
		InstallPath: installPath,
		Version:     strings.TrimSpace(version),
		VCVars64:    vcvars,
	}
}

// formatWindowsMSVCToolchainHint tells the model which compilers the host
// already injected into coding bash. Do not teach vcvars quoting — that is
// what made hello-world look like a failed compile.
func formatWindowsMSVCToolchainHint(tc *windowsMSVCToolchain) string {
	mingw := strings.TrimSpace(detectWindowsMinGWBin())
	if tc == nil || strings.TrimSpace(tc.VCVars64) == "" {
		if mingw != "" {
			return strings.TrimSpace(`
## Host C/C++ toolchain
No Visual Studio C++ tools were auto-detected.
g++ is already on PATH for coding bash.
Compile/run:
  g++ -std=c++17 -o hello.exe hello.cpp && .\hello.exe
Do NOT scan Program Files or WinGet packages. Do NOT wrap vcvars. Do NOT retry cmd /c quoting.
` + formatWindowsCMakePathHint())
		}
		return strings.TrimSpace(`
## Host C/C++ toolchain
No Visual Studio C++ tools were auto-detected on this host.
cl.exe is not on the coding bash PATH.
Use the project build script.
Do NOT wrap vcvars64.bat. Do NOT retry cmd /c quoting.
Do NOT scan Program Files or WinGet packages.
` + formatWindowsCMakePathHint())
	}

	var b strings.Builder
	b.WriteString("\n## Host C/C++ toolchain (auto-detected)\n")
	b.WriteString("Visual Studio C++ tools ARE installed. Host bash already has cl.exe, INCLUDE, and LIB (vcvars injected).\n")
	if tc.DisplayName != "" {
		b.WriteString("- Product: ")
		b.WriteString(tc.DisplayName)
		b.WriteByte('\n')
	}
	if tc.Version != "" {
		b.WriteString("- Version: ")
		b.WriteString(tc.Version)
		b.WriteByte('\n')
	}
	b.WriteString("- Install path: ")
	b.WriteString(tc.InstallPath)
	b.WriteByte('\n')
	if mingw != "" {
		b.WriteString("- g++ is also on PATH\n")
	}
	b.WriteString("Compile/run with ONE bash command:\n")
	b.WriteString("  cl /utf-8 /EHsc /Fe:hello.exe hello.cpp && .\\hello.exe\n")
	if mingw != "" {
		b.WriteString("Or: g++ -std=c++17 -o hello.exe hello.cpp && .\\hello.exe\n")
	}
	if cmake := formatWindowsCMakePathHint(); cmake != "" {
		b.WriteString(cmake)
	}
	b.WriteString("Rules:\n")
	b.WriteString("- Do NOT wrap vcvars64.bat or cmd /c call. cl is already on PATH.\n")
	b.WriteString("- Do NOT run where.exe / Get-ChildItem / Get-Command looking for cl or g++.\n")
	b.WriteString("- Do NOT recursively search Program Files or WinGet packages.\n")
	b.WriteString("- Avoid Launch-VsDevShell.ps1 (PowerShell execution policy often blocks it).\n")
	return b.String()
}

// codingWindowsToolchainPrompt is the system-prompt section for local bash.
// "cl is already on PATH" is only true after vcvars, or the filesystem
// fallback, has actually put cl.exe on the injected PATH.
func codingWindowsToolchainPrompt() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	tc := detectWindowsMSVCToolchain()
	if tc != nil && windowsEnvHasFile(windowsMSVCInjectedEnviron(), "cl.exe") {
		return formatWindowsMSVCToolchainHint(tc)
	}
	if tc != nil {
		return formatWindowsMSVCToolchainUnavailable(tc)
	}
	return formatWindowsMSVCToolchainHint(nil)
}

// formatWindowsMSVCToolchainUnavailable is the prompt when Visual Studio is
// installed but neither vcvars nor the layout fallback put cl.exe on PATH.
// It must not repeat the "cl is already on PATH" rules.
func formatWindowsMSVCToolchainUnavailable(tc *windowsMSVCToolchain) string {
	var b strings.Builder
	b.WriteString("\n## Host C/C++ toolchain\n")
	b.WriteString("Visual Studio is installed, but cl.exe is not on the coding bash PATH.\n")
	if tc != nil && strings.TrimSpace(tc.InstallPath) != "" {
		b.WriteString("- Install path: ")
		b.WriteString(tc.InstallPath)
		b.WriteByte('\n')
	}
	if mingw := strings.TrimSpace(detectWindowsMinGWBin()); mingw != "" {
		b.WriteString("g++ is on PATH. Compile/run:\n")
		b.WriteString("  g++ -std=c++17 -o hello.exe hello.cpp && .\\hello.exe\n")
	}
	if cmake := formatWindowsCMakePathHint(); cmake != "" {
		b.WriteString(cmake)
	}
	b.WriteString("Do not report that Visual Studio is missing. Do not wrap vcvars64.bat. Do not retry cmd /c quoting.\n")
	return b.String()
}

// codingClMissingRecoveryHint is the retry nudge after cl or vcvars failed to
// launch. It must match the injected environment: a miss is not "cl is on PATH".
func codingClMissingRecoveryHint() string {
	if windowsEnvHasFile(windowsMSVCInjectedEnviron(), "cl.exe") {
		return "cl.exe is already on PATH for coding bash. Run `cl /utf-8 /EHsc /Fe:app.exe file.cpp && .\\app.exe` without wrapping vcvars. Do not retry cmd /c quoting."
	}
	if strings.TrimSpace(detectWindowsMinGWBin()) != "" {
		return "cl.exe is not on the coding bash PATH. Compile with `g++ -std=c++17 -o app.exe file.cpp && .\\app.exe`. Do not wrap vcvars64.bat. Do not retry cmd /c quoting."
	}
	return "cl.exe is not on the coding bash PATH. Use the project build script. Do not wrap vcvars64.bat. Do not retry cmd /c quoting."
}

func windowsMSVCInjectedEnviron() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	windowsMSVCEnvOnce.Do(func() {
		tc := detectWindowsMSVCToolchain()
		install := ""
		if tc != nil {
			install = tc.InstallPath
			if strings.TrimSpace(tc.VCVars64) != "" {
				windowsMSVCEnv = captureWindowsVCVarsEnviron(tc.VCVars64)
			}
		}
		if !windowsEnvHasFile(windowsMSVCEnv, "cl.exe") {
			log.Printf("[msvc-detect] vcvars capture missed cl.exe; scanning install layout and registry")
			windowsMSVCEnv = synthesizeWindowsMSVCEnviron(install)
		}
		windowsMSVCEnvHadCl = windowsEnvHasFile(windowsMSVCEnv, "cl.exe")
		if windowsMSVCEnvHadCl {
			log.Printf("[msvc-detect] injected %d env vars; cl.exe is on PATH", len(windowsMSVCEnv))
		} else {
			log.Printf("[msvc-detect] cl.exe still absent after vcvars and layout scan")
		}
	})
	install := ""
	if tc := detectWindowsMSVCToolchain(); tc != nil {
		install = tc.InstallPath
	}
	windowsMSVCEnvMu.Lock()
	defer windowsMSVCEnvMu.Unlock()
	if next, ok := refreshWindowsMSVCEnviron(windowsMSVCEnv, windowsMSVCEnvHadCl, install); ok {
		log.Printf("[msvc-detect] cl.exe moved; injected %d env vars from the install layout", len(next))
		windowsMSVCEnv = next
	}
	return windowsMSVCEnv
}

// refreshWindowsMSVCEnviron rebuilds a captured environment whose cl.exe
// has disappeared. It does not run vcvars again. A capture that never
// produced cl.exe stays failed for the process lifetime.
func refreshWindowsMSVCEnviron(env []string, hadCl bool, install string) ([]string, bool) {
	if !hadCl || windowsEnvHasFile(env, "cl.exe") {
		return env, false
	}
	fresh := synthesizeWindowsMSVCEnviron(install)
	if !windowsEnvHasFile(fresh, "cl.exe") {
		return env, false
	}
	return fresh, true
}

// vcvarsCaptureBatScript is the cmd script that loads vcvars and prints the
// resulting environment. The quotes stay in the file: passing
// `call "C:\Program Files\...\vcvars64.bat"` as an exec.Command argument
// makes Go escape them, and cmd /s then skips `set`, so cl.exe never lands
// on PATH.
func vcvarsCaptureBatScript(vcvars string) string {
	// Quotes, expansions, and cmd metacharacters would break out of `call "..."`.
	if strings.TrimSpace(vcvars) == "" || strings.ContainsAny(vcvars, "\"\r\n&|<>^%!") {
		return ""
	}
	return "@echo off\r\ncall \"" + vcvars + "\" >nul\r\nset\r\n"
}

func captureWindowsVCVarsEnviron(vcvars string) []string {
	vcvars = strings.TrimSpace(vcvars)
	script := vcvarsCaptureBatScript(vcvars)
	if script == "" {
		log.Printf("[msvc-detect] vcvars path is empty or not safe to quote path=%q", vcvars)
		return nil
	}
	bat, err := os.CreateTemp("", "maclaw-vcvars-*.bat")
	if err != nil {
		log.Printf("[msvc-detect] vcvars bat create failed: %v", err)
		return nil
	}
	batPath := bat.Name()
	defer os.Remove(batPath)
	// cmd reads the bat in the ANSI code page. UTF-8 leaves a non-ASCII
	// vcvars path unreadable, and capture then reports that cl is missing.
	if err := writeWindowsCmdBat(bat, script); err != nil {
		bat.Close()
		log.Printf("[msvc-detect] vcvars bat write failed: %v", err)
		return nil
	}
	if err := bat.Close(); err != nil {
		log.Printf("[msvc-detect] vcvars bat close failed: %v", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := commandWindowsBat(ctx, batPath)
	out, err := cmd.Output()
	env := parseWindowsSETOutput(out)
	// vcvars can exit non-zero after a warning and still have printed `set`.
	// Keep that environment when cl.exe is actually on PATH.
	if !windowsEnvHasFile(env, "cl.exe") {
		log.Printf("[msvc-detect] vcvars env capture failed path=%q err=%v bytes=%d", vcvars, err, len(out))
		return nil
	}
	if err != nil {
		log.Printf("[msvc-detect] vcvars exit %v; cl.exe is on PATH, keeping %d vars", err, len(env))
	}
	return env
}

// commandWindowsBat runs a batch file whose directory may contain spaces or
// parentheses. The /c argument is only the file name. CreateProcess gets the
// directory separately, so cmd quote-stripping cannot split the path.
// /u makes `set` write UTF-16LE.
func commandWindowsBat(ctx context.Context, batPath string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, passthroughRuntimeProgram("cmd.exe"), "/d", "/u", "/c", filepath.Base(batPath))
	cmd.Dir = filepath.Dir(batPath)
	hideCommandWindow(cmd)
	cmd.Stderr = io.Discard
	return cmd
}

func windowsEnvHasFile(env []string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, item := range env {
		eq := strings.IndexByte(item, '=')
		if eq <= 0 || !strings.EqualFold(item[:eq], "PATH") {
			continue
		}
		for _, dir := range strings.Split(item[eq+1:], string(os.PathListSeparator)) {
			if existingToolFile(filepath.Join(strings.TrimSpace(dir), name)) {
				return true
			}
		}
	}
	return false
}

// windowsMSVCCompileEnv is a compiler layout discovered without running
// vcvars: the cl.exe directory plus the matching Windows SDK.
type windowsMSVCCompileEnv struct {
	VSInstall  string
	Toolset    string
	ClDir      string
	SDKRoot    string
	SDKVersion string
}

// synthesizeWindowsMSVCEnviron builds PATH, INCLUDE, and LIB from the
// install tree, the process environment, and the registry. It is the
// fallback when `call vcvars64.bat && set` does not put cl.exe on PATH.
func synthesizeWindowsMSVCEnviron(installPath string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	layout := discoverWindowsMSVCLayout(installPath)
	if layout.ClDir == "" {
		return nil
	}
	env := environFromWindowsMSVCLayout(layout)
	if !windowsEnvHasFile(env, "cl.exe") {
		return nil
	}
	return env
}

func discoverWindowsMSVCLayout(preferredInstall string) windowsMSVCCompileEnv {
	// The known install is enough when it contains cl.exe. Registry and the
	// year/edition matrix are only for the case where that path has no toolset.
	candidates := []string{
		preferredInstall,
		os.Getenv("VSINSTALLDIR"),
		installPathFromVCTools(os.Getenv("VCToolsInstallDir")),
	}
	if layout, ok := firstWindowsMSVCLayout(candidates); ok {
		return layout
	}
	// Registry order is not newest-first. Take the highest toolset.
	if layout, ok := bestWindowsMSVCLayout(registryVSInstallPaths()); ok {
		return layout
	}
	if layout, ok := bestWindowsMSVCLayout(commonWindowsVSInstallPaths()); ok {
		return layout
	}
	if cl := preferMSVCCl(globWindowsClExe()); cl != "" {
		toolset := msvcToolsetRoot(cl)
		install := toolset
		for i := 0; i < 4; i++ {
			install = filepath.Dir(install)
		}
		return layoutWithSDK(install, toolset, filepath.Dir(cl))
	}
	return windowsMSVCCompileEnv{}
}

func firstWindowsMSVCLayout(installs []string) (windowsMSVCCompileEnv, bool) {
	return selectWindowsMSVCLayout(installs, true)
}

func bestWindowsMSVCLayout(installs []string) (windowsMSVCCompileEnv, bool) {
	return selectWindowsMSVCLayout(installs, false)
}

func selectWindowsMSVCLayout(installs []string, stopAtFirst bool) (windowsMSVCCompileEnv, bool) {
	var best windowsMSVCCompileEnv
	found := false
	seen := map[string]bool{}
	for _, inst := range installs {
		inst = strings.Trim(strings.TrimSpace(inst), `"`)
		inst = strings.TrimRight(inst, `\/`)
		if inst == "" {
			continue
		}
		key := strings.ToLower(inst)
		if seen[key] {
			continue
		}
		seen[key] = true
		toolset, clDir := discoverWindowsMSVCClDir(inst)
		if clDir == "" {
			continue
		}
		layout := layoutWithSDK(inst, toolset, clDir)
		if stopAtFirst {
			return layout, true
		}
		if !found || msvcLayoutBetter(layout, best) {
			best = layout
			found = true
		}
	}
	return best, found
}

func msvcLayoutBetter(candidate, current windowsMSVCCompileEnv) bool {
	return msvcClBetter(
		filepath.Join(candidate.ClDir, "cl.exe"),
		filepath.Join(current.ClDir, "cl.exe"),
	)
}

func layoutWithSDK(install, toolset, clDir string) windowsMSVCCompileEnv {
	sdkRoot, sdkVer := discoverWindowsSDK()
	return windowsMSVCCompileEnv{
		VSInstall:  install,
		Toolset:    toolset,
		ClDir:      clDir,
		SDKRoot:    sdkRoot,
		SDKVersion: sdkVer,
	}
}

func installPathFromVCTools(vcTools string) string {
	vcTools = strings.TrimRight(strings.TrimSpace(vcTools), `\/`)
	if vcTools == "" {
		return ""
	}
	// VCToolsInstallDir is <install>\VC\Tools\MSVC\<ver>\.
	parent := vcTools
	for i := 0; i < 4; i++ {
		parent = filepath.Dir(parent)
	}
	return parent
}

func discoverWindowsMSVCClDir(installPath string) (toolset, clDir string) {
	installPath = strings.TrimRight(strings.TrimSpace(installPath), `\/`)
	if installPath == "" {
		return "", ""
	}
	verFile := filepath.Join(installPath, "VC", "Auxiliary", "Build", "Microsoft.VCToolsVersion.default.txt")
	if raw, err := os.ReadFile(verFile); err == nil {
		ver := msvcVersionText(raw)
		root := filepath.Join(installPath, "VC", "Tools", "MSVC", ver)
		if clDir, ok := existingMSVCClDir(root); ok {
			return root, clDir
		}
	}
	matches, _ := filepath.Glob(filepath.Join(installPath, "VC", "Tools", "MSVC", "*", "bin", "Host*", "*", "cl.exe"))
	best := preferMSVCCl(matches)
	if best == "" {
		return "", ""
	}
	return msvcToolsetRoot(best), filepath.Dir(best)
}

func msvcVersionText(raw []byte) string {
	text := strings.TrimSpace(windowsSETOutputString(raw))
	text = strings.TrimPrefix(text, "\uFEFF")
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	// The file is joined onto the install tree. Reject anything that is not a
	// toolset folder name so a stray path cannot escape that tree.
	if text == "" || strings.ContainsAny(text, `\/`) || strings.Contains(text, "..") {
		return ""
	}
	return text
}

func existingMSVCClDir(toolsetRoot string) (string, bool) {
	for _, rel := range []string{
		filepath.Join("bin", "Hostx64", "x64", "cl.exe"),
		filepath.Join("bin", "Hostarm64", "arm64", "cl.exe"),
		filepath.Join("bin", "Hostx86", "x64", "cl.exe"),
	} {
		cl := filepath.Join(toolsetRoot, rel)
		if existingToolFile(cl) {
			return filepath.Dir(cl), true
		}
	}
	return "", false
}

func preferMSVCCl(matches []string) string {
	best := ""
	for _, match := range matches {
		if !existingToolFile(match) {
			continue
		}
		if best == "" || msvcClBetter(match, best) {
			best = match
		}
	}
	return best
}

func msvcClBetter(candidate, best string) bool {
	if cmp := compareDottedVersion(msvcToolsetVersion(candidate), msvcToolsetVersion(best)); cmp != 0 {
		return cmp > 0
	}
	return msvcHostScore(candidate) > msvcHostScore(best)
}

func msvcToolsetRoot(clExe string) string {
	// <toolset>\bin\Hostx64\x64\cl.exe
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(clExe))))
}

func msvcToolsetVersion(clExe string) string {
	return filepath.Base(msvcToolsetRoot(clExe))
}

func msvcHostScore(clExe string) int {
	lower := strings.ToLower(filepath.ToSlash(clExe))
	switch {
	case strings.Contains(lower, "/hostx64/x64/"):
		return 3
	case strings.Contains(lower, "/hostarm64/arm64/"):
		return 2
	case strings.Contains(lower, "/hostx86/x64/"):
		return 1
	default:
		return 0
	}
}

func commonWindowsVSInstallPaths() []string {
	var out []string
	forEachCommonWindowsVSInstall(func(install, _, _ string) bool {
		out = append(out, install)
		return false
	})
	return out
}

func globWindowsClExe() []string {
	patterns := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft Visual Studio", "*", "*", "VC", "Tools", "MSVC", "*", "bin", "Host*", "*", "cl.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft Visual Studio", "*", "*", "VC", "Tools", "MSVC", "*", "bin", "Host*", "*", "cl.exe"),
		`C:\Program Files\Microsoft Visual Studio\*\*\VC\Tools\MSVC\*\bin\Host*\*\cl.exe`,
		`C:\Program Files (x86)\Microsoft Visual Studio\*\*\VC\Tools\MSVC\*\bin\Host*\*\cl.exe`,
	}
	seenPattern := map[string]bool{}
	seen := map[string]bool{}
	var out []string
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || strings.HasPrefix(pattern, `\`) {
			continue
		}
		patternKey := strings.ToLower(pattern)
		if seenPattern[patternKey] {
			continue
		}
		seenPattern[patternKey] = true
		found, _ := filepath.Glob(pattern)
		for _, path := range found {
			key := strings.ToLower(path)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, path)
		}
	}
	return out
}

func discoverWindowsSDK() (root, version string) {
	if runtime.GOOS != "windows" {
		return "", ""
	}
	windowsSDKOnce.Do(func() {
		windowsSDKCachedRoot, windowsSDKCachedVersion = loadWindowsSDK()
	})
	return windowsSDKCachedRoot, windowsSDKCachedVersion
}

func loadWindowsSDK() (root, version string) {
	root = strings.TrimRight(strings.TrimSpace(os.Getenv("WindowsSdkDir")), `\/`)
	version = strings.Trim(strings.TrimSpace(os.Getenv("WindowsSDKVersion")), `\/`)
	if root == "" {
		root = strings.TrimRight(windowsSDKRootFromRegistry(), `\/`)
	}
	if root == "" {
		for _, candidate := range []string{
			`C:\Program Files (x86)\Windows Kits\10`,
			`C:\Program Files\Windows Kits\10`,
		} {
			if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
				root = candidate
				break
			}
		}
	}
	if root == "" {
		return "", ""
	}
	if version == "" || !windowsSDKVersionUsable(root, version) {
		version = newestWindowsSDKVersion(root)
	}
	if version == "" {
		return "", ""
	}
	return root, version
}

func windowsSDKVersionUsable(root, version string) bool {
	fi, err := os.Stat(filepath.Join(root, "Include", version, "ucrt"))
	return err == nil && fi.IsDir()
}

func newestWindowsSDKVersion(root string) string {
	entries, err := os.ReadDir(filepath.Join(root, "Include"))
	if err != nil {
		return ""
	}
	best := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !windowsSDKVersionUsable(root, name) {
			continue
		}
		if best == "" || compareDottedVersion(name, best) > 0 {
			best = name
		}
	}
	return best
}

func compareDottedVersion(left, right string) int {
	lp := dottedVersionParts(left)
	rp := dottedVersionParts(right)
	n := len(lp)
	if len(rp) > n {
		n = len(rp)
	}
	for i := 0; i < n; i++ {
		lv, rv := 0, 0
		if i < len(lp) {
			lv = lp[i]
		}
		if i < len(rp) {
			rv = rp[i]
		}
		if lv > rv {
			return 1
		}
		if lv < rv {
			return -1
		}
	}
	return 0
}

func dottedVersionParts(version string) []int {
	fields := strings.FieldsFunc(version, func(r rune) bool { return r == '.' })
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		n := 0
		for _, r := range field {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

func environFromWindowsMSVCLayout(layout windowsMSVCCompileEnv) []string {
	if strings.TrimSpace(layout.ClDir) == "" {
		return nil
	}
	var includes, libs, paths []string
	addDir := func(dst *[]string, dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			return
		}
		*dst = append(*dst, dir)
	}
	arch := msvcLibArch(layout.ClDir)
	addDir(&paths, layout.ClDir)
	if arch == "x64" || arch == "arm64" {
		addDir(&paths, filepath.Join(layout.VSInstall, "MSBuild", "Current", "Bin", "amd64"))
	}
	addDir(&paths, filepath.Join(layout.VSInstall, "MSBuild", "Current", "Bin"))
	addDir(&includes, filepath.Join(layout.Toolset, "include"))
	addDir(&includes, filepath.Join(layout.Toolset, "atlmfc", "include"))
	addDir(&libs, filepath.Join(layout.Toolset, "lib", arch))
	addDir(&libs, filepath.Join(layout.Toolset, "atlmfc", "lib", arch))
	if layout.SDKRoot != "" && layout.SDKVersion != "" {
		inc := filepath.Join(layout.SDKRoot, "Include", layout.SDKVersion)
		lib := filepath.Join(layout.SDKRoot, "Lib", layout.SDKVersion)
		for _, part := range []string{"ucrt", "shared", "um", "winrt"} {
			addDir(&includes, filepath.Join(inc, part))
		}
		for _, part := range []string{"ucrt", "um"} {
			addDir(&libs, filepath.Join(lib, part, arch))
		}
		addDir(&paths, filepath.Join(layout.SDKRoot, "bin", layout.SDKVersion, arch))
	}
	const sep = ";"
	var env []string
	if len(paths) > 0 {
		env = append(env, "Path="+strings.Join(paths, sep))
	}
	if len(includes) > 0 {
		env = append(env, "INCLUDE="+strings.Join(includes, sep))
	}
	if len(libs) > 0 {
		env = append(env, "LIB="+strings.Join(libs, sep))
		env = append(env, "LIBPATH="+strings.Join(libs, sep))
	}
	if layout.VSInstall != "" {
		env = append(env, "VSINSTALLDIR="+windowsTrailingSlash(layout.VSInstall))
	}
	if layout.Toolset != "" {
		env = append(env, "VCToolsInstallDir="+windowsTrailingSlash(layout.Toolset))
	}
	if layout.SDKRoot != "" {
		env = append(env, "WindowsSdkDir="+windowsTrailingSlash(layout.SDKRoot))
	}
	if layout.SDKVersion != "" {
		env = append(env, `WindowsSDKVersion=`+layout.SDKVersion+`\`)
	}
	return env
}

func msvcLibArch(clDir string) string {
	arch := strings.ToLower(filepath.Base(strings.TrimRight(clDir, `\/`)))
	switch arch {
	case "x64", "x86", "arm64", "arm":
		return arch
	default:
		return "x64"
	}
}

func windowsTrailingSlash(path string) string {
	path = strings.TrimRight(strings.TrimSpace(path), `\/`)
	if path == "" {
		return ""
	}
	return path + `\`
}

func parseWindowsSETOutput(raw []byte) []string {
	text := windowsSETOutputString(raw)
	var env []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			continue
		}
		env = append(env, key+"="+line[eq+1:])
	}
	return env
}

func windowsSETOutputString(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		return decodeUTF16LE(raw[2:])
	}
	if looksLikeUTF16LE(raw) {
		return decodeUTF16LE(raw)
	}
	return string(raw)
}

func decodeUTF16LE(raw []byte) string {
	u16 := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		u16 = append(u16, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	return string(utf16.Decode(u16))
}

func looksLikeUTF16LE(raw []byte) bool {
	if len(raw) < 8 || len(raw)%2 != 0 {
		return false
	}
	limit := len(raw)
	if limit > 64 {
		limit = 64
	}
	nuls := 0
	pairs := 0
	for i := 1; i < limit; i += 2 {
		pairs++
		if raw[i] == 0 {
			nuls++
		}
	}
	return pairs > 0 && nuls*4 >= pairs*3
}

func mergeWindowsToolchainEnviron(base, overlay []string) []string {
	if len(overlay) == 0 {
		return base
	}
	index := make(map[string]int, len(base))
	for i, item := range base {
		eq := strings.IndexByte(item, '=')
		if eq <= 0 {
			continue
		}
		index[strings.ToUpper(item[:eq])] = i
	}
	for _, item := range overlay {
		eq := strings.IndexByte(item, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToUpper(item[:eq])
		if key == "PATH" {
			base = prependWindowsPathValue(base, index, item[eq+1:])
			continue
		}
		if i, ok := index[key]; ok {
			base[i] = item
			continue
		}
		index[key] = len(base)
		base = append(base, item)
	}
	return base
}

func prependWindowsPathValue(env []string, index map[string]int, extra string) []string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return env
	}
	i, ok := index["PATH"]
	if !ok {
		index["PATH"] = len(env)
		return append(env, "Path="+extra)
	}
	eq := strings.IndexByte(env[i], '=')
	current := ""
	if eq >= 0 {
		current = env[i][eq+1:]
	}
	// Directories already on PATH keep their current order. vcvars copies the
	// process PATH into its block; prepending that whole block would put
	// WindowsApps back ahead of a compiler directory just moved forward.
	fresh := windowsPathEntriesMissingFrom(extra, current)
	env[i] = "Path=" + joinUniqueWindowsPath(fresh, current)
	return env
}

// windowsPathEntriesMissingFrom returns overlay PATH entries that are not
// already in base, in overlay order.
func windowsPathEntriesMissingFrom(extra, current string) string {
	seen := map[string]struct{}{}
	for _, entry := range strings.Split(current, string(os.PathListSeparator)) {
		if key := windowsPathListKey(entry); key != "" {
			seen[key] = struct{}{}
		}
	}
	var out []string
	for _, entry := range strings.Split(extra, string(os.PathListSeparator)) {
		entry = strings.TrimSpace(entry)
		key := windowsPathListKey(entry)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, entry)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func joinUniqueWindowsPath(parts ...string) string {
	seen := map[string]struct{}{}
	var out []string
	for _, part := range parts {
		for _, entry := range strings.Split(part, string(os.PathListSeparator)) {
			entry = strings.TrimSpace(entry)
			key := windowsPathListKey(entry)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, entry)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// windowsPathListKey compares PATH entries the way CreateProcess searches
// them. vcvars and the registry often differ only by case or a trailing
// separator; both copies otherwise land in the 32767-unit environment block.
func windowsPathListKey(entry string) string {
	entry = strings.Trim(strings.TrimSpace(entry), `"`)
	if entry == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(entry))
}

func prewarmWindowsCodingToolchain() {
	if runtime.GOOS != "windows" {
		return
	}
	go func() {
		_ = detectWindowsMSVCToolchain()
		_ = windowsMSVCInjectedEnviron()
		_ = detectWindowsMinGWBin()
		_ = detectWindowsCMakeBin()
		_ = detectWindowsNinjaBin()
	}()
}

func formatWindowsCMakePathHint() string {
	if detectWindowsCMakeBin() == "" {
		return ""
	}
	return "cmake and ctest are already on PATH for coding bash. Use cmake --build and ctest directly. Do NOT scan Program Files for cmake.exe.\n"
}

// detectWindowsCMakeBin finds the directory that contains cmake.exe.
// Coding bash and the step verify gate share codingCommandEnv, so a GUI
// process whose PATH does not include CMake can still run cmake --build.
func detectWindowsCMakeBin() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	// A cmake.exe unpacked for this process stays ahead of one found on disk.
	if exe := cmakeExeInDir(privateCMakeDir()); exe != "" {
		return filepath.Dir(exe)
	}
	return detectWindowsToolDir(&windowsCMakeBin, &windowsCMakeBinOnce, "cmake.exe", "[cmake-detect]", findWindowsCMakeBin)
}

func cmakeExeInDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	exe := filepath.Join(dir, "cmake.exe")
	if !existingToolFile(exe) {
		return ""
	}
	return exe
}

func privateCMakeDir() string {
	privateCMakeMu.Lock()
	defer privateCMakeMu.Unlock()
	return privateInstalledCMakeDir
}

func setPrivateCMakeDir(dir string) {
	privateCMakeMu.Lock()
	privateInstalledCMakeDir = strings.TrimSpace(dir)
	privateCMakeMu.Unlock()
}

func findWindowsCMakeBin() string {
	return firstExistingWindowsToolDir(windowsCMakeCandidates())
}

func windowsCMakeCandidates() []string {
	var matches []string
	matches = append(matches, windowsPathExeCandidates("cmake.exe")...)
	programFiles := strings.TrimSpace(os.Getenv("ProgramFiles"))
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	programFilesX86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)"))
	if programFilesX86 == "" {
		programFilesX86 = `C:\Program Files (x86)`
	}
	matches = append(matches,
		filepath.Join(programFiles, "CMake", "bin", "cmake.exe"),
		filepath.Join(programFilesX86, "CMake", "bin", "cmake.exe"),
		`C:\ProgramData\chocolatey\bin\cmake.exe`,
	)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		matches = append(matches, filepath.Join(home, "scoop", "apps", "cmake", "current", "bin", "cmake.exe"))
	}
	matches = append(matches, windowsVSCMakeCandidates()...)
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		for _, pattern := range []string{
			filepath.Join(local, "Microsoft", "WinGet", "Links", "cmake.exe"),
			filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "bin", "cmake.exe"),
			filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "*", "bin", "cmake.exe"),
			filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "mingw64", "bin", "cmake.exe"),
			filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "*", "mingw64", "bin", "cmake.exe"),
		} {
			found, _ := filepath.Glob(pattern)
			matches = append(matches, found...)
		}
	}
	if tools := strings.TrimSpace(remote.ToolsDir()); tools != "" {
		matches = append(matches, filepath.Join(tools, "cmake", "bin", "cmake.exe"))
		found, _ := filepath.Glob(filepath.Join(tools, "*", "bin", "cmake.exe"))
		matches = append(matches, found...)
	}
	if mingw := detectWindowsMinGWBin(); mingw != "" {
		matches = append(matches, filepath.Join(mingw, "cmake.exe"))
	}
	for _, dir := range windowsLogonPathDirs() {
		matches = append(matches, filepath.Join(dir, "cmake.exe"))
	}
	return matches
}

func windowsVSCMakeCandidates() []string {
	var matches []string
	if tc := detectWindowsMSVCToolchain(); tc != nil && strings.TrimSpace(tc.InstallPath) != "" {
		candidate := filepath.Join(tc.InstallPath, "Common7", "IDE", "CommonExtensions", "Microsoft", "CMake", "CMake", "bin", "cmake.exe")
		matches = append(matches, candidate)
		if existingToolFile(candidate) {
			return matches
		}
	}
	for _, root := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft Visual Studio"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft Visual Studio"),
	} {
		if strings.TrimSpace(root) == "" || strings.HasPrefix(root, string(os.PathSeparator)) {
			continue
		}
		found, _ := filepath.Glob(filepath.Join(root, "*", "*", "Common7", "IDE", "CommonExtensions", "Microsoft", "CMake", "CMake", "bin", "cmake.exe"))
		matches = append(matches, found...)
	}
	return matches
}

func detectWindowsNinjaBin() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	found := detectWindowsToolDir(&windowsNinjaBin, &windowsNinjaBinOnce, "ninja.exe", "[ninja-detect]", func() string {
		return firstExistingWindowsToolDir(windowsNinjaCandidates())
	})
	// A CMake install ships ninja.exe beside cmake.exe. Prefer that copy, and
	// look again after a miss so a later install is visible.
	if dir := detectWindowsCMakeBin(); dir != "" && existingToolFile(filepath.Join(dir, "ninja.exe")) {
		return dir
	}
	return found
}

func windowsNinjaCandidates() []string {
	var matches []string
	matches = append(matches, windowsPathExeCandidates("ninja.exe")...)
	if dir := detectWindowsCMakeBin(); dir != "" {
		matches = append(matches, filepath.Join(dir, "ninja.exe"))
	}
	if dir := detectWindowsMinGWBin(); dir != "" {
		matches = append(matches, filepath.Join(dir, "ninja.exe"))
	}
	if tc := detectWindowsMSVCToolchain(); tc != nil && strings.TrimSpace(tc.InstallPath) != "" {
		matches = append(matches, filepath.Join(tc.InstallPath, "Common7", "IDE", "CommonExtensions", "Microsoft", "CMake", "Ninja", "ninja.exe"))
	}
	for _, dir := range windowsLogonPathDirs() {
		matches = append(matches, filepath.Join(dir, "ninja.exe"))
	}
	return matches
}

func detectWindowsMSBuild() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	if dir := firstExistingWindowsToolDir(windowsPathExeCandidates("MSBuild.exe")); dir != "" {
		return filepath.Join(dir, "MSBuild.exe")
	}
	if tc := detectWindowsMSVCToolchain(); tc != nil && strings.TrimSpace(tc.InstallPath) != "" {
		for _, rel := range []string{
			filepath.Join("MSBuild", "Current", "Bin", "amd64", "MSBuild.exe"),
			filepath.Join("MSBuild", "Current", "Bin", "MSBuild.exe"),
		} {
			candidate := filepath.Join(tc.InstallPath, rel)
			if existingToolFile(candidate) {
				return candidate
			}
		}
	}
	return ""
}

// ensurePrivateCMakeInstalled installs CMake when the host has no copy.
// A failed attempt is not cached: the GUI process stays up, and the network
// may work on the next verify. An existing cmake.exe always wins, so a stale
// install directory cannot hide one found on disk.
func ensurePrivateCMakeInstalled(ctx context.Context) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ""
	}
	if exe := cmakeExeInDir(privateCMakeDir()); exe != "" {
		return exe
	}
	if exe := cmakeExeInDir(detectWindowsCMakeBin()); exe != "" {
		return exe
	}
	privateCMakeMu.Lock()
	if windowsCMakeInstalling {
		privateCMakeMu.Unlock()
		return ""
	}
	if exe := cmakeExeInDir(privateInstalledCMakeDir); exe != "" {
		privateCMakeMu.Unlock()
		return exe
	}
	windowsCMakeInstalling = true
	installFn := installPrivateCMakeFn
	privateCMakeMu.Unlock()

	defer func() {
		privateCMakeMu.Lock()
		windowsCMakeInstalling = false
		privateCMakeMu.Unlock()
	}()
	if installFn == nil || ctx.Err() != nil {
		return ""
	}
	dir := strings.TrimSpace(installFn(ctx))
	setPrivateCMakeDir(dir)
	return cmakeExeInDir(dir)
}

func defaultInstallPrivateCMake(ctx context.Context) string {
	// A private unzip needs no administrator. winget installs of Kitware.CMake
	// usually demand elevation, which an ordinary user cannot approve.
	if bin := installPortableCMake(ctx); bin != "" {
		return bin
	}
	if ctx != nil && ctx.Err() != nil {
		return ""
	}
	return installCMakeWithWinget(ctx)
}

const windowsCMakeRelease = "3.31.8"

const (
	maxCMakeZipBytes     = 80 << 20
	maxCMakeExtractBytes = 250 << 20
)

func windowsCMakeZipURL() string {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	return "https://cmake.org/files/v3.31/cmake-" + windowsCMakeRelease + "-windows-" + arch + ".zip"
}

var downloadPrivateCMakeZip = func(ctx context.Context, url, dest string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cmake download status %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxCMakeZipBytes+1))
	if err != nil {
		return err
	}
	if n > maxCMakeZipBytes {
		return fmt.Errorf("cmake download exceeds %d bytes", maxCMakeZipBytes)
	}
	return nil
}

func installPortableCMake(ctx context.Context) string {
	root := strings.TrimSpace(remote.ToolsDir())
	if root == "" {
		log.Printf("[cmake-install] tools directory is empty")
		return ""
	}
	dest := filepath.Join(root, "cmake")
	if bin := findNamedExeDir(dest, "cmake.exe"); bin != "" {
		return bin
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		log.Printf("[cmake-install] mkdir: %v", err)
		return ""
	}
	zipPath := filepath.Join(dest, "cmake-windows.zip")
	if err := downloadPrivateCMakeZip(ctx, windowsCMakeZipURL(), zipPath); err != nil {
		log.Printf("[cmake-install] download failed: %v", err)
		_ = os.Remove(zipPath)
		return ""
	}
	defer os.Remove(zipPath)
	if err := extractZipSafe(zipPath, dest); err != nil {
		log.Printf("[cmake-install] extract failed: %v", err)
		_ = os.RemoveAll(dest)
		return ""
	}
	return findNamedExeDir(dest, "cmake.exe")
}

func installCMakeWithWinget(ctx context.Context) string {
	winget, err := exec.LookPath("winget")
	if err != nil || strings.TrimSpace(winget) == "" {
		log.Printf("[cmake-install] winget is not available")
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Elevation cannot be approved by an ordinary user. Fail fast instead of
	// holding the verify step for the whole portable-download budget again.
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, winget, "install", "--id", "Kitware.CMake", "-e",
		"--scope", "user", "--silent", "--disable-interactivity",
		"--accept-package-agreements", "--accept-source-agreements")
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	log.Printf("[cmake-install] winget finished err=%v output=%s", err, truncateRunesForSubAgent(string(out), 400))
	return findWindowsCMakeBin()
}

func findNamedExeDir(root, name string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	var dir string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), name) {
			dir = filepath.Dir(path)
			return fs.SkipAll
		}
		return nil
	})
	return dir
}

func extractZipSafe(zipPath, dest string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	dest = filepath.Clean(dest)
	destPrefix := strings.ToLower(dest + string(os.PathSeparator))
	var written int64
	for _, file := range reader.File {
		rel := filepath.Clean(filepath.FromSlash(file.Name))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			continue
		}
		target := filepath.Join(dest, rel)
		if !strings.HasPrefix(strings.ToLower(target), destPrefix) {
			continue
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if file.UncompressedSize64 > maxCMakeExtractBytes || written > maxCMakeExtractBytes {
			return fmt.Errorf("cmake zip expands past %d bytes", maxCMakeExtractBytes)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			src.Close()
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(src, maxCMakeExtractBytes-written+1))
		out.Close()
		src.Close()
		written += n
		if copyErr != nil {
			return copyErr
		}
		if written > maxCMakeExtractBytes {
			return fmt.Errorf("cmake zip expands past %d bytes", maxCMakeExtractBytes)
		}
	}
	return nil
}

// windowsPathExeCandidates is name on the process PATH. Discovery stats the
// results through existingToolFile, which does not touch network drives.
func windowsPathExeCandidates(name string) []string {
	name = strings.TrimSpace(name)
	if runtime.GOOS != "windows" || name == "" {
		return nil
	}
	var out []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}

func firstExistingWindowsToolDir(candidates []string) string {
	for _, exe := range candidates {
		exe = strings.TrimSpace(exe)
		if exe == "" || isWindowsAppExecutionAlias(exe) {
			continue
		}
		if existingToolFile(exe) {
			return filepath.Dir(exe)
		}
	}
	return ""
}

func existingToolFile(path string) bool {
	// A disconnected redirector (UNC, offline mapped drive) makes os.Stat
	// block. Coding bash waits on this check, so a non-local path counts as
	// absent instead of stalling the process.
	if runtime.GOOS == "windows" && !windowsToolPathIsLocal(path) {
		return false
	}
	fi, err := os.Stat(path)
	// WindowsApps aliases stat as empty files and are not real tools.
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

func isWindowsAppExecutionAlias(path string) bool {
	return strings.Contains(strings.ToLower(filepath.Clean(path)), `\windowsapps\`)
}

func detectWindowsMinGWBin() string {
	return detectWindowsToolDir(&windowsMinGWBin, &windowsMinGWBinOnce, "g++.exe", "[mingw-detect]", findWindowsMinGWBin)
}

// detectWindowsToolDir keeps a directory only while exeName is still in it.
// A miss, or a hit whose file has disappeared, is searched again on the next
// call via the process PATH and the registry logon Path. The slow WinGet
// glob runs once.
func detectWindowsToolDir(slot *string, once *sync.Once, exeName, tag string, slow func() string) string {
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
		return storeWindowsToolDir(slot, dir, tag)
	}
	once.Do(func() {
		if loadWindowsToolDir(slot) != "" {
			return
		}
		if dir := slow(); dir != "" {
			storeWindowsToolDir(slot, dir, tag)
		}
	})
	return loadWindowsToolDir(slot)
}

func loadWindowsToolDir(slot *string) string {
	windowsToolDirMu.Lock()
	defer windowsToolDirMu.Unlock()
	return *slot
}

func storeWindowsToolDir(slot *string, dir, tag string) string {
	dir = strings.TrimSpace(dir)
	windowsToolDirMu.Lock()
	defer windowsToolDirMu.Unlock()
	if dir != "" && *slot == "" {
		*slot = dir
		log.Printf("%s using %s", tag, dir)
	}
	return *slot
}

func clearWindowsToolDir(slot *string, dir, tag string) {
	windowsToolDirMu.Lock()
	defer windowsToolDirMu.Unlock()
	if *slot == dir {
		*slot = ""
		log.Printf("%s dropped %s", tag, dir)
	}
}

// windowsLiveToolCandidates is the process PATH plus the registry logon Path.
// Both can change after the GUI starts. Globs are not included.
func windowsLiveToolCandidates(name string) []string {
	return append(windowsPathExeCandidates(name), windowsLogonExeCandidates(name)...)
}

func windowsLogonExeCandidates(name string) []string {
	name = strings.TrimSpace(name)
	if runtime.GOOS != "windows" || name == "" {
		return nil
	}
	dirs := windowsLogonPathDirs()
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}

func findWindowsMinGWBin() string {
	// A real g++ already on PATH does not need a WinGet glob. Walk PATH
	// ourselves: exec.LookPath stats every directory, including a
	// disconnected redirector. An empty WindowsApps alias falls through.
	if dir := firstExistingWindowsToolDir(windowsPathExeCandidates("g++.exe")); dir != "" {
		return dir
	}
	var matches []string
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		for _, pattern := range []string{
			filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "mingw64", "bin", "g++.exe"),
			filepath.Join(local, "Microsoft", "WinGet", "Packages", "*", "*", "mingw64", "bin", "g++.exe"),
		} {
			found, _ := filepath.Glob(pattern)
			matches = append(matches, found...)
		}
	}
	for _, dir := range []string{`C:\mingw64\bin`, `C:\msys64\ucrt64\bin`, `C:\msys64\mingw64\bin`} {
		matches = append(matches, filepath.Join(dir, "g++.exe"))
	}
	for _, dir := range windowsLogonPathDirs() {
		matches = append(matches, filepath.Join(dir, "g++.exe"))
	}
	return firstExistingWindowsToolDir(matches)
}

var windowsMSVCCallPattern = regexp.MustCompile(`(?i)call\s+\\*"?\\*([A-Za-z]:[^"\n]*?vcvars(?:64|all)\.bat)\\*"?\\*`)

func isWindowsMSVCShellRecipe(command string) bool {
	lower := strings.ToLower(command)
	return strings.Contains(lower, "vcvars64.bat") || strings.Contains(lower, "vcvarsall.bat")
}

// normalizeWindowsMSVCCompileCommand unwraps cmd /c and repairs quote-eaten
// vcvars paths so the command can run through cmd.exe instead of PowerShell.
func normalizeWindowsMSVCCompileCommand(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if !isWindowsMSVCShellRecipe(command) {
		return command, false
	}
	inner := unwrapWindowsCmdC(command)
	if vcvars := extractWindowsVCVarsPath(inner); vcvars != "" {
		if preferred := preferDetectedWindowsVCVars(vcvars); preferred != "" {
			vcvars = preferred
		}
		inner = windowsMSVCCallPattern.ReplaceAllString(inner, `call "`+vcvars+`"`)
	}
	return strings.TrimSpace(inner), true
}

func unwrapWindowsCmdC(command string) string {
	trimmed := strings.TrimSpace(command)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"cmd.exe /d /s /c ", "cmd /d /s /c ", "cmd.exe /c ", "cmd /c "} {
		if strings.HasPrefix(lower, prefix) {
			return unquoteWindowsShellOnce(strings.TrimSpace(trimmed[len(prefix):]))
		}
	}
	return command
}

func unquoteWindowsShellOnce(command string) string {
	if len(command) >= 2 {
		if (command[0] == '"' && command[len(command)-1] == '"') || (command[0] == '\'' && command[len(command)-1] == '\'') {
			return command[1 : len(command)-1]
		}
	}
	return command
}

func extractWindowsVCVarsPath(command string) string {
	match := windowsMSVCCallPattern.FindStringSubmatch(command)
	raw := ""
	if len(match) >= 2 {
		raw = match[1]
	} else {
		raw = windowsVCVarsPathByScan(command)
	}
	return cleanWindowsVCVarsPath(raw)
}

func windowsVCVarsPathByScan(command string) string {
	lower := strings.ToLower(command)
	idx := strings.Index(lower, "vcvars64.bat")
	nameLen := len("vcvars64.bat")
	if idx < 0 {
		idx = strings.Index(lower, "vcvarsall.bat")
		nameLen = len("vcvarsall.bat")
	}
	if idx < 0 {
		return ""
	}
	raw := command[:idx+nameLen]
	drive := -1
	for i := 0; i < len(raw)-1; i++ {
		if raw[i+1] == ':' && ((raw[i] >= 'A' && raw[i] <= 'Z') || (raw[i] >= 'a' && raw[i] <= 'z')) {
			drive = i
		}
	}
	if drive < 0 {
		return ""
	}
	return raw[drive:]
}

func cleanWindowsVCVarsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = strings.ReplaceAll(path, `\"`, "")
	path = strings.ReplaceAll(path, `"`, "")
	path = strings.ReplaceAll(path, `\\`, `\`)
	return filepath.Clean(path)
}

func preferDetectedWindowsVCVars(extracted string) string {
	tc := detectWindowsMSVCToolchain()
	if tc == nil || strings.TrimSpace(tc.VCVars64) == "" {
		return extracted
	}
	if extracted == "" || sameWindowsFilePath(tc.VCVars64, extracted) {
		return tc.VCVars64
	}
	return extracted
}

func sameWindowsFilePath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
