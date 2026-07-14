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
	if !strings.Contains(hint, "vcvars64.bat") {
		t.Fatalf("expected vcvars path, got %q", hint)
	}
	if !strings.Contains(hint, "Do NOT run bare") {
		t.Fatalf("expected anti-probe rules, got %q", hint)
	}
	if !strings.Contains(hint, "cmd /c") {
		t.Fatalf("expected cmd /c compile recipe, got %q", hint)
	}
}

func TestFormatWindowsMSVCToolchainHintMissing(t *testing.T) {
	hint := formatWindowsMSVCToolchainHint(nil)
	if !strings.Contains(hint, "No Visual Studio") {
		t.Fatalf("expected missing notice, got %q", hint)
	}
	if !strings.Contains(hint, "vswhere.exe") {
		t.Fatalf("expected vswhere guidance, got %q", hint)
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
