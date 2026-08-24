package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestFormatWindowsMSVCToolchainHintMissing(t *testing.T) {
	hint := formatWindowsMSVCToolchainHint(nil)
	if !strings.Contains(hint, "No Visual Studio") {
		t.Fatalf("expected missing notice, got %q", hint)
	}
	if !strings.Contains(hint, "vswhere.exe") && !strings.Contains(hint, "g++ is already on PATH") {
		t.Fatalf("expected vswhere or injected g++ guidance, got %q", hint)
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
