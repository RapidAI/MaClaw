package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
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
	windowsMSVCDetectOnce sync.Once
	windowsMSVCDetected   *windowsMSVCToolchain
	windowsMSVCEnvOnce    sync.Once
	windowsMSVCEnv        []string
	windowsMinGWBinOnce   sync.Once
	windowsMinGWBin       string
)

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
	return probeWindowsMSVCFromCommonPaths()
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
	// Year / edition matrix: VS 2026 uses "18", 2022 uses "2022", etc.
	roots := uniqueNonEmptyStrings([]string{
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft Visual Studio"),
		`C:\Program Files\Microsoft Visual Studio`,
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft Visual Studio"),
		`C:\Program Files (x86)\Microsoft Visual Studio`,
	})
	// Prefer newer product lines first.
	years := []string{"18", "2022", "17", "2026", "2019", "16"}
	editions := []string{"Community", "Professional", "Enterprise", "BuildTools", "Preview"}
	for _, root := range roots {
		for _, year := range years {
			for _, ed := range editions {
				install := filepath.Join(root, year, ed)
				if tc := toolchainFromInstallPath(install, "Visual Studio "+ed+" "+year, year); tc != nil {
					return tc
				}
			}
		}
	}
	return nil
}

func uniqueNonEmptyStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
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
Do NOT scan Program Files or WinGet packages. Do NOT wrap vcvars.
`)
		}
		return strings.TrimSpace(`
## Host C/C++ toolchain
No Visual Studio C++ tools were auto-detected on this host.
cl.exe is usually NOT on PATH even when Visual Studio is installed.
If you need MSVC: locate vcvars64.bat via
  & "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe" -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
then compile with:
  cmd /c 'call "<install>\VC\Auxiliary\Build\vcvars64.bat" && cl /utf-8 /EHsc ...'
Keep cmd /c '...'. Do NOT scan Program Files. Prefer project build scripts.
`)
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
	b.WriteString("Rules:\n")
	b.WriteString("- Do NOT wrap vcvars64.bat or cmd /c call. cl is already on PATH.\n")
	b.WriteString("- Do NOT run where.exe / Get-ChildItem / Get-Command looking for cl or g++.\n")
	b.WriteString("- Do NOT recursively search Program Files or WinGet packages.\n")
	b.WriteString("- Avoid Launch-VsDevShell.ps1 (PowerShell execution policy often blocks it).\n")
	return b.String()
}

func windowsMSVCInjectedEnviron() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	windowsMSVCEnvOnce.Do(func() {
		tc := detectWindowsMSVCToolchain()
		if tc == nil || strings.TrimSpace(tc.VCVars64) == "" {
			return
		}
		windowsMSVCEnv = captureWindowsVCVarsEnviron(tc.VCVars64)
		if len(windowsMSVCEnv) == 0 {
			windowsMSVCEnv = captureWindowsVCVarsEnviron(tc.VCVars64)
		}
		if len(windowsMSVCEnv) > 0 {
			log.Printf("[msvc-detect] injected %d env vars from vcvars", len(windowsMSVCEnv))
		}
	})
	return windowsMSVCEnv
}

func captureWindowsVCVarsEnviron(vcvars string) []string {
	vcvars = strings.TrimSpace(vcvars)
	if vcvars == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, passthroughRuntimeProgram("cmd.exe"), "/d", "/s", "/c",
		`call "`+vcvars+`" >nul && set`)
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		log.Printf("[msvc-detect] vcvars env capture failed path=%q err=%v", vcvars, err)
		return nil
	}
	return parseWindowsSETOutput(out)
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
	env[i] = "Path=" + joinUniqueWindowsPath(extra, current)
	return env
}

func joinUniqueWindowsPath(parts ...string) string {
	seen := map[string]struct{}{}
	var out []string
	for _, part := range parts {
		for _, entry := range strings.Split(part, string(os.PathListSeparator)) {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			key := strings.ToLower(entry)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, entry)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func prewarmWindowsCodingToolchain() {
	if runtime.GOOS != "windows" {
		return
	}
	go func() {
		_ = detectWindowsMSVCToolchain()
		_ = windowsMSVCInjectedEnviron()
		_ = detectWindowsMinGWBin()
	}()
}

func detectWindowsMinGWBin() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	windowsMinGWBinOnce.Do(func() {
		windowsMinGWBin = findWindowsMinGWBin()
		if windowsMinGWBin != "" {
			log.Printf("[mingw-detect] using %s", windowsMinGWBin)
		}
	})
	return windowsMinGWBin
}

func findWindowsMinGWBin() string {
	if p, err := exec.LookPath("g++"); err == nil && strings.TrimSpace(p) != "" {
		return filepath.Dir(p)
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
	for _, exe := range matches {
		if fi, err := os.Stat(exe); err == nil && !fi.IsDir() {
			return filepath.Dir(exe)
		}
	}
	return ""
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
