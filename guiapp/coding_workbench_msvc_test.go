package guiapp

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

func TestFormatWindowsMSVCToolchainHintDetected(t *testing.T) {
	hint := formatWindowsMSVCToolchainHint(&windowsMSVCToolchain{
		DisplayName: "Visual Studio Community 2026",
		InstallPath: `C:\Program Files\Microsoft Visual Studio\18\Community`,
		Version:     "18.7.1",
		VCVars64:    `C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat`,
	})
	if !strings.Contains(hint, "ARE installed") {
		t.Fatalf("expected installed notice, got %q", hint)
	}
	if !strings.Contains(hint, "already has cl.exe") {
		t.Fatalf("expected injected cl PATH, got %q", hint)
	}
	if !strings.Contains(hint, "cl /utf-8") {
		t.Fatalf("expected bare cl recipe, got %q", hint)
	}
	if !strings.Contains(hint, "&&") {
		t.Fatalf("expected short-circuit compile+run, got %q", hint)
	}
	if strings.Contains(hint, "cmd /c 'call") {
		t.Fatalf("detected hint must not teach vcvars quoting: %q", hint)
	}
	if !strings.Contains(hint, "Do NOT wrap vcvars") && !strings.Contains(hint, "Do NOT run where.exe") {
		t.Fatalf("expected anti-probe / no-vcvars rules, got %q", hint)
	}
}

func TestNormalizeWindowsMSVCCompileCommandRepairsMangledQuotes(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "backslash-eaten quotes",
			command: `cmd /c "call \C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat\ && cl /utf-8 hello.cpp"`,
			want:    `call "C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat" && cl /utf-8 hello.cpp`,
		},
		{
			name:    "escaped inner quotes",
			command: `cmd /c "call \"C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat\" && cl /utf-8 hello.cpp"`,
			want:    `call "C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat" && cl /utf-8 hello.cpp`,
		},
		{
			name:    "powershell-safe recipe",
			command: `cmd /c 'call "C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat" && cl /utf-8 hello.cpp && .\hello.exe'`,
			want:    `call "C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat" && cl /utf-8 hello.cpp && .\hello.exe`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeWindowsMSVCCompileCommand(tc.command)
			if !ok {
				t.Fatal("expected MSVC recipe")
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if _, ok := normalizeWindowsMSVCCompileCommand(`go test ./gui`); ok {
		t.Fatal("ordinary commands must not be treated as MSVC recipes")
	}
}

func TestDetectWindowsMinGWBinFindsCompilerAddedAfterMiss(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	windowsToolDirMu.Lock()
	origBin := windowsMinGWBin
	origOnce := windowsMinGWBinOnce
	windowsMinGWBin = ""
	windowsMinGWBinOnce = sync.Once{}
	windowsMinGWBinOnce.Do(func() {})
	windowsToolDirMu.Unlock()
	t.Cleanup(func() {
		windowsToolDirMu.Lock()
		windowsMinGWBin = origBin
		windowsMinGWBinOnce = origOnce
		windowsToolDirMu.Unlock()
	})

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "g++.exe"), []byte("g++"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got := detectWindowsMinGWBin(); got != dir {
		t.Fatalf("detect = %q, want %q", got, dir)
	}
}

func TestDetectWindowsMinGWBinDropsMissingCachedDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	gone := t.TempDir()
	if err := os.WriteFile(filepath.Join(gone, "g++.exe"), []byte("g++"), 0o644); err != nil {
		t.Fatal(err)
	}
	live := t.TempDir()
	if err := os.WriteFile(filepath.Join(live, "g++.exe"), []byte("g++"), 0o644); err != nil {
		t.Fatal(err)
	}
	windowsToolDirMu.Lock()
	origBin := windowsMinGWBin
	origOnce := windowsMinGWBinOnce
	windowsMinGWBin = gone
	windowsMinGWBinOnce = sync.Once{}
	windowsMinGWBinOnce.Do(func() {})
	windowsToolDirMu.Unlock()
	t.Cleanup(func() {
		windowsToolDirMu.Lock()
		windowsMinGWBin = origBin
		windowsMinGWBinOnce = origOnce
		windowsToolDirMu.Unlock()
	})

	if err := os.Remove(filepath.Join(gone, "g++.exe")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", live+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got := detectWindowsMinGWBin(); got != live {
		t.Fatalf("detect = %q, want %q", got, live)
	}
}

func TestFirstExistingWindowsToolDir(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cmake.exe")
	if err := os.WriteFile(exe, []byte("cmake"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing", "cmake.exe")
	if got := firstExistingWindowsToolDir([]string{missing, exe}); got != dir {
		t.Fatalf("dir = %q, want %q", got, dir)
	}
	if got := firstExistingWindowsToolDir([]string{"", missing}); got != "" {
		t.Fatalf("missing tools returned %q", got)
	}
	aliasDir := filepath.Join(dir, "WindowsApps")
	if err := os.MkdirAll(aliasDir, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasDir, "cmake.exe")
	if err := os.WriteFile(alias, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := firstExistingWindowsToolDir([]string{alias, exe}); got != dir {
		t.Fatalf("app alias was selected: %q", got)
	}
}

func TestExtractZipSafeFindsCMakeAndSkipsTraversal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "cmake.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	evil, err := zw.Create("../evil.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := evil.Write([]byte("nope")); err != nil {
		t.Fatal(err)
	}
	entry, err := zw.Create("cmake-3.31.8-windows-x86_64/bin/cmake.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("cmake")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractZipSafe(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.exe")); err == nil {
		t.Fatal("zip entry escaped the destination")
	}
	bin := findNamedExeDir(dest, "cmake.exe")
	if filepath.Base(bin) != "bin" {
		t.Fatalf("cmake bin = %q", bin)
	}
}

func TestWindowsCMakeCandidatesIncludeInstallDirs(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmake discovery is Windows-only")
	}
	joined := strings.ToLower(strings.Join(windowsCMakeCandidates(), "\n"))
	if !strings.Contains(joined, `\cmake\bin\cmake.exe`) {
		t.Fatalf("candidates = %q", joined)
	}
}

func TestCodingCommandEnvIncludesDetectedCMake(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmake PATH injection is Windows-only")
	}
	bin := detectWindowsCMakeBin()
	if bin == "" {
		t.Skip("cmake is not installed")
	}
	var path string
	for _, item := range codingCommandEnv() {
		if strings.HasPrefix(strings.ToLower(item), "path=") {
			path = item
			break
		}
	}
	lowerPath := strings.ToLower(path)
	binAt := strings.Index(lowerPath, strings.ToLower(bin))
	if binAt < 0 {
		t.Fatalf("PATH missing %s", bin)
	}
	if alias := strings.Index(lowerPath, `\windowsapps`); alias >= 0 && binAt > alias {
		t.Fatalf("compiler dir is behind WindowsApps: %s", path)
	}
	if vc := strings.Index(lowerPath, `\vc\tools\msvc\`); vc >= 0 && binAt < vc {
		t.Fatalf("cl dir is behind the detected compiler: %s", path)
	}
}

func TestFormatWindowsMSVCToolchainHintMissing(t *testing.T) {
	hint := formatWindowsMSVCToolchainHint(nil)
	if !strings.Contains(hint, "No Visual Studio") {
		t.Fatalf("expected missing notice, got %q", hint)
	}
	if strings.Contains(hint, "cmd /c '") || strings.Contains(hint, `cmd /c "`) || strings.Contains(hint, "cmd /c call") {
		t.Fatalf("missing hint must not teach cmd /c quoting: %q", hint)
	}
	if !strings.Contains(hint, "retry cmd /c quoting") {
		t.Fatalf("missing hint must forbid quoting retries: %q", hint)
	}
	if !strings.Contains(hint, "g++ is already on PATH") && !strings.Contains(hint, "project build script") {
		t.Fatalf("expected g++ or the project build script, got %q", hint)
	}
}

func TestPrependWindowsPathEntriesPutsToolBeforeAlias(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path cleaning")
	}
	env := prependWindowsPathEntries(
		[]string{`Path=C:\Users\me\AppData\Local\Microsoft\WindowsApps;C:\mingw64\bin\`},
		`C:\mingw64\bin`,
	)
	if len(env) != 1 || env[0] != `Path=C:\mingw64\bin;C:\Users\me\AppData\Local\Microsoft\WindowsApps` {
		t.Fatalf("env = %#v", env)
	}
	env = prependWindowsPathEntries(env, `C:\mingw64\bin`)
	env = mergeWindowsToolchainEnviron(env, []string{
		`Path=C:\VS\VC\Tools\MSVC\14.51\bin\Hostx64\x64`,
		`INCLUDE=C:\VS\include`,
	})
	if env[0] != `Path=C:\VS\VC\Tools\MSVC\14.51\bin\Hostx64\x64;C:\mingw64\bin;C:\Users\me\AppData\Local\Microsoft\WindowsApps` {
		t.Fatalf("env = %#v", env)
	}
	if len(env) != 2 || env[1] != `INCLUDE=C:\VS\include` {
		t.Fatalf("env = %#v", env)
	}
	// vcvars echoes the process PATH after its own bins. That copy must not
	// pull WindowsApps back in front of the compiler directory.
	env = mergeWindowsToolchainEnviron(
		[]string{env[0]},
		[]string{`Path=C:\VS\VC\Tools\MSVC\14.51\bin\Hostx64\x64;C:\Users\me\AppData\Local\Microsoft\WindowsApps;C:\mingw64\bin`},
	)
	if env[0] != `Path=C:\VS\VC\Tools\MSVC\14.51\bin\Hostx64\x64;C:\mingw64\bin;C:\Users\me\AppData\Local\Microsoft\WindowsApps` {
		t.Fatalf("vcvars copy reordered PATH: %#v", env)
	}
}

func TestRefreshWindowsMSVCEnvironReplacesMissingCl(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	root := t.TempDir()
	writeFakeMSVCInstall(t, root, "14.51.36231")
	dead := t.TempDir()
	if err := os.WriteFile(filepath.Join(dead, "cl.exe"), []byte("cl"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := []string{"Path=" + dead}
	if !windowsEnvHasFile(stale, "cl.exe") {
		t.Fatal("fixture cl.exe is missing")
	}
	if _, ok := refreshWindowsMSVCEnviron(stale, true, root); ok {
		t.Fatal("refreshed while cl.exe still exists")
	}
	if err := os.Remove(filepath.Join(dead, "cl.exe")); err != nil {
		t.Fatal(err)
	}
	next, ok := refreshWindowsMSVCEnviron(stale, true, root)
	if !ok || !windowsEnvHasFile(next, "cl.exe") {
		t.Fatalf("ok=%v env=%v", ok, next)
	}
	if !strings.Contains(strings.ToLower(strings.Join(next, "\n")), strings.ToLower(root)) {
		t.Fatalf("env = %v", next)
	}
	if _, ok := refreshWindowsMSVCEnviron(nil, false, root); ok {
		t.Fatal("a capture that never had cl.exe was rebuilt")
	}
}

func TestJoinUniqueWindowsPathCollapsesTrailingSeparator(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path cleaning")
	}
	got := joinUniqueWindowsPath(`C:\Windows\System32\`, `C:\windows\system32`)
	if got != `C:\Windows\System32\` {
		t.Fatalf("PATH = %q", got)
	}
	env := appendWindowsPathEntries(
		[]string{`Path=C:\Windows\System32\`},
		`C:\windows\system32`,
		`C:\Program Files\Git\cmd`,
	)
	if len(env) != 1 || env[0] != `Path=C:\Windows\System32\;C:\Program Files\Git\cmd` {
		t.Fatalf("env = %#v", env)
	}
}

func TestCommandWindowsBatCallsNonASCIIPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	dir := filepath.Join(t.TempDir(), "编译工具 (x86)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(dir, "inner.bat")
	innerScript := "@echo off\r\nset MACLAW_ACP_PROBE=ok\r\n"
	if err := os.WriteFile(inner, []byte(innerScript), 0o644); err != nil {
		t.Fatal(err)
	}
	script := vcvarsCaptureBatScript(inner)
	if script == "" {
		t.Fatal("capture script rejected a non-ASCII path")
	}
	batPath := filepath.Join(dir, "maclaw-probe.bat")
	f, err := os.Create(batPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeWindowsCmdBat(f, script); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := commandWindowsBat(context.Background(), batPath)
	out, err := cmd.Output()
	joined := strings.Join(parseWindowsSETOutput(out), "\n")
	if !strings.Contains(joined, "MACLAW_ACP_PROBE=ok") {
		t.Fatalf("non-ascii bat failed err=%v out=%q env=%q", err, out, joined)
	}
}

func TestExistingToolFileSkipsNetworkPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	start := time.Now()
	if existingToolFile(`\\maclaw-no-such\share\cl.exe`) {
		t.Fatal("UNC path was treated as a local tool")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("UNC lookup blocked for %s", time.Since(start))
	}
	if !windowsToolPathIsLocal(`C:\Windows\System32\cmd.exe`) {
		t.Fatal("system32 should be a local path")
	}
}

func TestParseWindowsSETOutputDecodesUTF16LEWithoutBOM(t *testing.T) {
	text := "Path=C:\\Program Files\\MSVC\\bin;C:\\用户\\bin\r\nINCLUDE=C:\\inc\r\n"
	raw := utf16LEBytes(text, false)
	env := parseWindowsSETOutput(raw)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "用户") {
		t.Fatalf("UTF-16 path was not decoded: %q", joined)
	}
	if !strings.Contains(joined, `Path=C:\Program Files\MSVC\bin;C:\用户\bin`) && !strings.Contains(joined, "PATH=C:\\Program Files\\MSVC\\bin;C:\\用户\\bin") {
		t.Fatalf("env = %q", joined)
	}
	if !strings.Contains(joined, "INCLUDE=C:\\inc") {
		t.Fatalf("INCLUDE missing: %q", joined)
	}
}

func TestParseWindowsSETOutputAndMergePath(t *testing.T) {
	env := parseWindowsSETOutput([]byte("INCLUDE=C:\\inc\r\nPATH=C:\\vc\\bin\r\nLIB=C:\\lib\r\n"))
	if len(env) != 3 {
		t.Fatalf("parsed %#v", env)
	}
	utf16 := []byte{
		'P', 0, 'A', 0, 'T', 0, 'H', 0, '=', 0,
		'C', 0, ':', 0, '\\', 0, 'v', 0, 'c', 0, '\\', 0, 'b', 0, 'i', 0, 'n', 0,
		'\r', 0, '\n', 0,
	}
	if got := parseWindowsSETOutput(utf16); len(got) != 1 || got[0] != `PATH=C:\vc\bin` {
		t.Fatalf("utf16-le without BOM = %#v", got)
	}
	merged := mergeWindowsToolchainEnviron([]string{`Path=C:\Windows`, "FOO=old"}, env)
	var path, include string
	for _, item := range merged {
		switch {
		case strings.HasPrefix(strings.ToUpper(item), "PATH="):
			path = item[5:]
		case strings.HasPrefix(item, "INCLUDE="):
			include = item[8:]
		}
	}
	if include != `C:\inc` {
		t.Fatalf("INCLUDE = %q", include)
	}
	if !strings.HasPrefix(strings.ToLower(path), `c:\vc\bin`) || !strings.Contains(strings.ToLower(path), `c:\windows`) {
		t.Fatalf("PATH should prepend vcvars then keep host: %q", path)
	}
}

func TestToolchainFromInstallPathRequiresVCVars(t *testing.T) {
	tmp := t.TempDir()
	if tc := toolchainFromInstallPath(tmp, "fake", "1"); tc != nil {
		t.Fatalf("expected nil without vcvars, got %#v", tc)
	}
	vcvars := filepath.Join(tmp, "VC", "Auxiliary", "Build", "vcvars64.bat")
	if err := os.MkdirAll(filepath.Dir(vcvars), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vcvars, []byte("@echo off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := toolchainFromInstallPath(tmp, "Test VS", "18.0")
	if tc == nil || tc.VCVars64 != vcvars {
		t.Fatalf("toolchain = %#v", tc)
	}
}

func TestDetectWindowsMSVCToolchainOnThisHost(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	// Soft assertion: when vswhere + VC tools exist, detection must succeed.
	// CI images without VS should not fail the suite.
	vswhere := windowsVswherePath()
	if vswhere == "" {
		t.Skip("vswhere not installed")
	}
	tc := detectWindowsMSVCToolchain()
	if tc == nil {
		// Build Tools may be absent on developer machines that only have the IDE
		// without C++ workload — skip rather than fail.
		t.Skip("no MSVC toolchain with vcvars64.bat on this host")
	}
	if _, err := os.Stat(tc.VCVars64); err != nil {
		t.Fatalf("vcvars missing: %v", err)
	}
	if !strings.Contains(strings.ToLower(tc.InstallPath), "visual studio") {
		t.Fatalf("unexpected install path %q", tc.InstallPath)
	}
}

func TestWindowsCodingBashInvocationUsesCmdForMSVCRecipe(t *testing.T) {
	command := `cmd /c "call \C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat\ && cl /utf-8 hello.cpp"`
	shell, args := windowsCodingBashInvocation(command)
	if !strings.Contains(strings.ToLower(shell), "cmd") {
		t.Fatalf("shell = %q, want cmd.exe", shell)
	}
	if len(args) != 4 || args[0] != "/d" || args[1] != "/s" || args[2] != "/c" {
		t.Fatalf("args = %#v", args)
	}
	if !strings.Contains(args[3], `call "C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat"`) {
		t.Fatalf("repaired command = %q", args[3])
	}
	if strings.Contains(args[3], `call \C:`) || strings.HasPrefix(strings.TrimSpace(args[3]), "cmd ") {
		t.Fatalf("should unwrap and repair, got %q", args[3])
	}
}

func TestWindowsCodingBashInvocationRewritesCompileThenRunInsideCmdC(t *testing.T) {
	command := `cmd /c 'call "C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat" && cl /utf-8 /EHsc /Fe:hello.exe hello.cpp ; .\hello.exe'`
	_, args := windowsCodingBashInvocation(command)
	if len(args) < 4 {
		t.Fatalf("args = %#v", args)
	}
	inner := args[3]
	if !strings.Contains(inner, "&&") || strings.Contains(inner, ";") {
		t.Fatalf("inner compile+run must short-circuit, got %q", inner)
	}
	if !strings.Contains(inner, `.\hello.exe`) && !strings.Contains(inner, `hello.exe`) {
		t.Fatalf("inner = %q", inner)
	}
}

func TestNormalizeSubAgentVerificationCommandForExecutionRewritesCompileThenRun(t *testing.T) {
	command := `g++ -std=c++11 -o snake.exe snake.cpp ; .\snake.exe`
	got := normalizeSubAgentVerificationCommandForExecution(command)
	if runtime.GOOS == "windows" {
		if strings.Contains(got, ";") || !strings.Contains(got, "&&") {
			t.Fatalf("compile+run must be normalized before execution, got %q", got)
		}
		if suppressesVerificationFailure(got) || !isSubAgentVerificationCommand(got) {
			t.Fatalf("normalized command must be auditable verification, got %q", got)
		}
		return
	}
	if got != command {
		t.Fatalf("non-Windows command should remain unchanged, got %q", got)
	}
}

func TestVCVarsCaptureScriptKeepsQuotesInsideTheBat(t *testing.T) {
	script := vcvarsCaptureBatScript(`C:\Program Files\Microsoft Visual Studio\18\Community\VC\Auxiliary\Build\vcvars64.bat`)
	if !strings.Contains(script, "call \"C:\\Program Files\\Microsoft Visual Studio\\18\\Community\\VC\\Auxiliary\\Build\\vcvars64.bat\"") {
		t.Fatalf("script = %q", script)
	}
	if strings.Contains(script, `\"`) {
		t.Fatalf("script must not pre-escape quotes for exec.Command: %q", script)
	}
	if !strings.Contains(script, "\r\nset\r\n") {
		t.Fatalf("script must print the environment after vcvars: %q", script)
	}
}

func TestEnvironFromWindowsMSVCLayoutPutsClAndSDKOnPath(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "VS")
	toolset := filepath.Join(install, "VC", "Tools", "MSVC", "14.51.36231")
	clDir := filepath.Join(toolset, "bin", "Hostx64", "x64")
	sdk := filepath.Join(root, "Kits", "10")
	version := "10.0.26100.0"
	msbuild := filepath.Join(install, "MSBuild", "Current", "Bin", "amd64")
	dirs := []string{
		clDir,
		msbuild,
		filepath.Join(toolset, "include"),
		filepath.Join(toolset, "lib", "x64"),
		filepath.Join(sdk, "Include", version, "ucrt"),
		filepath.Join(sdk, "Include", version, "um"),
		filepath.Join(sdk, "Lib", version, "ucrt", "x64"),
		filepath.Join(sdk, "Lib", version, "um", "x64"),
		filepath.Join(sdk, "bin", version, "x64"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(clDir, "cl.exe"), []byte("cl"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := environFromWindowsMSVCLayout(windowsMSVCCompileEnv{
		VSInstall:  install,
		Toolset:    toolset,
		ClDir:      clDir,
		SDKRoot:    sdk,
		SDKVersion: version,
	})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, clDir) {
		t.Fatalf("PATH missing cl dir: %s", joined)
	}
	if !strings.Contains(joined, msbuild) {
		t.Fatalf("PATH missing MSBuild: %s", joined)
	}
	if runtime.GOOS == "windows" && !windowsEnvHasFile(env, "cl.exe") {
		t.Fatalf("PATH missing cl.exe: %#v", env)
	}
	if !strings.Contains(joined, filepath.Join(sdk, "Include", version, "ucrt")) {
		t.Fatalf("INCLUDE missing ucrt: %s", joined)
	}
	if !strings.Contains(joined, filepath.Join(sdk, "Lib", version, "um", "x64")) {
		t.Fatalf("LIB missing um: %s", joined)
	}
	if !strings.Contains(joined, "WindowsSDKVersion="+version+`\`) {
		t.Fatalf("SDK version missing: %s", joined)
	}
}

func TestDiscoverWindowsMSVCClDirReadsDefaultToolset(t *testing.T) {
	root := t.TempDir()
	ver := "14.51.36231"
	clDir := filepath.Join(root, "VC", "Tools", "MSVC", ver, "bin", "Hostx64", "x64")
	if err := os.MkdirAll(clDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "VC", "Auxiliary", "Build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clDir, "cl.exe"), []byte("cl"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "VC", "Auxiliary", "Build", "Microsoft.VCToolsVersion.default.txt"), []byte(ver+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	toolset, got := discoverWindowsMSVCClDir(root)
	if got != clDir {
		t.Fatalf("cl dir = %q, want %q", got, clDir)
	}
	if !strings.HasSuffix(strings.ToLower(toolset), strings.ToLower(ver)) {
		t.Fatalf("toolset = %q", toolset)
	}
}

func TestCodingCommandEnvPutsClOnPathWhenMSVCInstalled(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	tc := detectWindowsMSVCToolchain()
	if tc == nil {
		t.Skip("no MSVC toolchain on this host")
	}
	env := codingCommandEnv()
	if !windowsEnvHasFile(env, "cl.exe") {
		t.Fatal("coding bash PATH does not contain cl.exe")
	}
	if n := windowsEnvBlockLen(env); n > 32767 {
		t.Fatalf("coding bash environment block is %d UTF-16 units", n)
	}
	cmd := exec.Command(passthroughRuntimeProgram("cmd.exe"), "/d", "/c", "where cl")
	cmd.Env = env
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("where cl: %v\n%s", err, out)
	}
	first := strings.TrimSpace(strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")[0])
	lower := strings.ToLower(first)
	if !strings.Contains(lower, `\vc\tools\msvc\`) || !strings.HasSuffix(lower, `\cl.exe`) {
		t.Fatalf("where cl first hit = %q", first)
	}
	rel, err := filepath.Rel(tc.InstallPath, first)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("cl %q is outside %q", first, tc.InstallPath)
	}
	ps := exec.Command(passthroughRuntimeProgram("powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", "(Get-Command cl.exe).Source")
	ps.Env = env
	hideCommandWindow(ps)
	psOut, psErr := ps.Output()
	if psErr != nil {
		t.Fatalf("powershell cl: %v\n%s", psErr, psOut)
	}
	if got := strings.TrimSpace(string(psOut)); !strings.EqualFold(got, first) {
		t.Fatalf("powershell cl = %q, cmd where = %q", got, first)
	}
}

func TestCommandWindowsBatRunsFromSpacedDirectory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	dir := filepath.Join(t.TempDir(), "Program Files (x86)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	batPath := filepath.Join(dir, "maclaw-probe.bat")
	script := "@echo off\r\nset MACLAW_SPACE_PROBE=ok\r\nset MACLAW_SPACE_PROBE\r\n"
	if err := os.WriteFile(batPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := commandWindowsBat(context.Background(), batPath)
	out, err := cmd.Output()
	joined := strings.Join(parseWindowsSETOutput(out), "\n")
	if !strings.Contains(joined, "MACLAW_SPACE_PROBE=ok") {
		t.Fatalf("spaced bat failed err=%v out=%q env=%q", err, out, joined)
	}
}

func windowsEnvBlockLen(env []string) int {
	n := 1
	for _, item := range env {
		n += len(utf16.Encode([]rune(item))) + 1
	}
	return n
}

func TestCompareDottedVersionRanksMinorNumerically(t *testing.T) {
	if compareDottedVersion("14.51.36231", "14.9.0") <= 0 {
		t.Fatal("14.51 must outrank 14.9")
	}
	if compareDottedVersion("10.0.26100.0", "10.0.22621.0") <= 0 {
		t.Fatal("SDK 26100 must outrank 22621")
	}
	if compareDottedVersion("14.9", "14.9.0") != 0 {
		t.Fatal("a missing trailing component ranks as zero")
	}
}

func TestVCVarsCaptureScriptRejectsMetacharacters(t *testing.T) {
	paths := []string{
		"",
		`C:\Program Files\a&b\vcvars64.bat`,
		`C:\Program Files\a|b\vcvars64.bat`,
		`C:\Program Files\a%b\vcvars64.bat`,
		"C:\\Program Files\\a!b\\vcvars64.bat",
		"C:\\VS\\vcvars64.bat\r\nset",
	}
	for _, path := range paths {
		if script := vcvarsCaptureBatScript(path); script != "" {
			t.Fatalf("path %q produced %q", path, script)
		}
	}
}

func TestDiscoverWindowsMSVCClDirPrefersHigherToolset(t *testing.T) {
	root := t.TempDir()
	writeFakeMSVCCl(t, root, "14.9.0", "Hostx64", "x64")
	want := writeFakeMSVCCl(t, root, "14.51.36231", "Hostx86", "x64")
	_, got := discoverWindowsMSVCClDir(root)
	if got != want {
		t.Fatalf("cl dir = %q, want %q", got, want)
	}
}

func TestDiscoverWindowsMSVCClDirPrefersNativeX64Host(t *testing.T) {
	root := t.TempDir()
	writeFakeMSVCCl(t, root, "14.51.36231", "Hostx86", "x64")
	want := writeFakeMSVCCl(t, root, "14.51.36231", "Hostx64", "x64")
	_, got := discoverWindowsMSVCClDir(root)
	if got != want {
		t.Fatalf("cl dir = %q, want %q", got, want)
	}
}

func TestDiscoverWindowsMSVCClDirHonorsEncodedDefaultToolset(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{name: "utf8-bom", body: append([]byte{0xEF, 0xBB, 0xBF}, []byte("14.40.33807\r\n")...)},
		{name: "utf16-le", body: utf16LEVersion("14.40.33807\r\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			want := writeFakeMSVCCl(t, root, "14.40.33807", "Hostx64", "x64")
			writeFakeMSVCCl(t, root, "14.51.36231", "Hostx64", "x64")
			dir := filepath.Join(root, "VC", "Auxiliary", "Build")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "Microsoft.VCToolsVersion.default.txt"), tc.body, 0o644); err != nil {
				t.Fatal(err)
			}
			_, got := discoverWindowsMSVCClDir(root)
			if got != want {
				t.Fatalf("cl dir = %q, want default toolset %q", got, want)
			}
		})
	}
}

func TestDiscoverWindowsMSVCLayoutPrefersGivenInstall(t *testing.T) {
	root := t.TempDir()
	want := writeFakeMSVCCl(t, root, "14.1.0", "Hostx64", "x64")
	layout := discoverWindowsMSVCLayout(root)
	if layout.ClDir != want {
		t.Fatalf("cl dir = %q, want %q", layout.ClDir, want)
	}
	if !strings.EqualFold(layout.VSInstall, root) {
		t.Fatalf("install = %q, want %q", layout.VSInstall, root)
	}
}

func TestBestWindowsMSVCLayoutPicksNewerInstall(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, "2019")
	newer := filepath.Join(root, "2026")
	olderCl := writeFakeMSVCCl(t, older, "14.29.30133", "Hostx64", "x64")
	newerCl := writeFakeMSVCCl(t, newer, "14.51.36231", "Hostx64", "x64")
	first, ok := firstWindowsMSVCLayout([]string{older, newer})
	if !ok || first.ClDir != olderCl {
		t.Fatalf("first layout = %+v, want %q", first, olderCl)
	}
	best, ok := bestWindowsMSVCLayout([]string{older, newer})
	if !ok || best.ClDir != newerCl {
		t.Fatalf("best layout = %+v, want %q", best, newerCl)
	}
}

func TestEnvironFromWindowsMSVCLayoutUsesClTargetArch(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "VS")
	toolset := filepath.Join(install, "VC", "Tools", "MSVC", "14.51.36231")
	clDir := filepath.Join(toolset, "bin", "Hostarm64", "arm64")
	sdk := filepath.Join(root, "Kits", "10")
	version := "10.0.26100.0"
	for _, dir := range []string{
		clDir,
		filepath.Join(toolset, "lib", "arm64"),
		filepath.Join(sdk, "Lib", version, "um", "arm64"),
		filepath.Join(sdk, "bin", version, "arm64"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(clDir, "cl.exe"), []byte("cl"), 0o644); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(environFromWindowsMSVCLayout(windowsMSVCCompileEnv{
		VSInstall:  install,
		Toolset:    toolset,
		ClDir:      clDir,
		SDKRoot:    sdk,
		SDKVersion: version,
	}), "\n")
	if !strings.Contains(joined, filepath.Join(toolset, "lib", "arm64")) {
		t.Fatalf("LIB missing arm64: %s", joined)
	}
	if strings.Contains(joined, filepath.Join(toolset, "lib", "x64")) {
		t.Fatalf("arm64 cl must not pull x64 libs: %s", joined)
	}
	if !strings.Contains(joined, filepath.Join(sdk, "bin", version, "arm64")) {
		t.Fatalf("SDK bin missing arm64: %s", joined)
	}
}

func TestFormatWindowsMSVCToolchainUnavailableDoesNotClaimCl(t *testing.T) {
	text := formatWindowsMSVCToolchainUnavailable(&windowsMSVCToolchain{
		InstallPath: `C:\VS\18\Community`,
	})
	if strings.Contains(text, "already has cl.exe") || strings.Contains(text, "cl is already on PATH") {
		t.Fatalf("unavailable hint claims cl is injected: %q", text)
	}
	if !strings.Contains(text, "is not on the coding bash PATH") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, `C:\VS\18\Community`) {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "cmd /c '") || strings.Contains(text, `cmd /c "`) {
		t.Fatalf("unavailable hint teaches cmd /c quoting: %q", text)
	}
	if !strings.Contains(text, "Do not retry cmd /c quoting") {
		t.Fatalf("text = %q", text)
	}
}

func TestCodingWindowsToolchainPromptAgreesWithClOnPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		if codingWindowsToolchainPrompt() != "" {
			t.Fatal("non-windows prompt must be empty")
		}
		return
	}
	if detectWindowsMSVCToolchain() == nil {
		t.Skip("no MSVC toolchain on this host")
	}
	prompt := codingWindowsToolchainPrompt()
	hasCl := windowsEnvHasFile(windowsMSVCInjectedEnviron(), "cl.exe")
	if hasCl {
		if !strings.Contains(prompt, "already has cl.exe") {
			t.Fatalf("cl.exe is on PATH but the prompt does not say so: %q", prompt)
		}
		if strings.Contains(prompt, "is not on the coding bash PATH") {
			t.Fatalf("prompt contradicts itself: %q", prompt)
		}
		return
	}
	if strings.Contains(prompt, "already has cl.exe") {
		t.Fatalf("cl.exe is absent but the prompt claims it is on PATH: %q", prompt)
	}
	if !strings.Contains(prompt, "is not on the coding bash PATH") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func writeFakeMSVCCl(t *testing.T, install, version, host, arch string) string {
	t.Helper()
	clDir := filepath.Join(install, "VC", "Tools", "MSVC", version, "bin", host, arch)
	if err := os.MkdirAll(clDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clDir, "cl.exe"), []byte("cl"), 0o644); err != nil {
		t.Fatal(err)
	}
	return clDir
}

func utf16LEVersion(text string) []byte {
	return utf16LEBytes(text, true)
}

func utf16LEBytes(text string, bom bool) []byte {
	var out []byte
	if bom {
		out = []byte{0xFF, 0xFE}
	}
	for _, r := range text {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

func TestNewestWindowsMSVCToolchainPrefersHigherToolset(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, "2019")
	newer := filepath.Join(root, "2026")
	stub := filepath.Join(root, "stub")
	writeFakeMSVCInstall(t, older, "14.29.30133")
	writeFakeMSVCInstall(t, newer, "14.51.36231")
	writeVCVarsOnly(t, stub)
	got := newestWindowsMSVCToolchain([]vsInstallRef{{path: older}, {path: newer}})
	if got == nil || !strings.EqualFold(got.InstallPath, newer) {
		t.Fatalf("toolchain = %+v, want %q", got, newer)
	}
	if got.Version != "14.51.36231" {
		t.Fatalf("version = %q", got.Version)
	}
	got = newestWindowsMSVCToolchain([]vsInstallRef{{path: stub}, {path: older}})
	if got == nil || !strings.EqualFold(got.InstallPath, older) {
		t.Fatalf("vcvars-only root won: %+v", got)
	}
}

func TestDiscoverWindowsMSVCClDirIgnoresPathInDefaultToolset(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "VS")
	outside := filepath.Join(root, "outside")
	want := writeFakeMSVCCl(t, install, "14.51.36231", "Hostx64", "x64")
	writeFakeMSVCCl(t, outside, "14.99.0", "Hostx64", "x64")
	dir := filepath.Join(install, "VC", "Auxiliary", "Build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	escaped := `..\..\..\outside\VC\Tools\MSVC\14.99.0`
	if err := os.WriteFile(filepath.Join(dir, "Microsoft.VCToolsVersion.default.txt"), []byte(escaped), 0o644); err != nil {
		t.Fatal(err)
	}
	_, got := discoverWindowsMSVCClDir(install)
	if got != want {
		t.Fatalf("cl dir = %q, want %q", got, want)
	}
}

func TestCodingClMissingRecoveryHintMatchesInjection(t *testing.T) {
	hint := codingClMissingRecoveryHint()
	if !strings.Contains(hint, "Do not retry cmd /c quoting") {
		t.Fatalf("hint = %q", hint)
	}
	if windowsEnvHasFile(windowsMSVCInjectedEnviron(), "cl.exe") {
		if !strings.Contains(hint, "already on PATH") || strings.Contains(hint, "not on the coding bash PATH") {
			t.Fatalf("hint = %q", hint)
		}
		return
	}
	if strings.Contains(hint, "already on PATH") || !strings.Contains(hint, "not on the coding bash PATH") {
		t.Fatalf("hint = %q", hint)
	}
}

func writeFakeMSVCInstall(t *testing.T, install, version string) {
	t.Helper()
	writeFakeMSVCCl(t, install, version, "Hostx64", "x64")
	writeVCVarsOnly(t, install)
}

func writeVCVarsOnly(t *testing.T, install string) {
	t.Helper()
	dir := filepath.Join(install, "VC", "Auxiliary", "Build")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vcvars64.bat"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
