package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestEnsureHiddenHostConsoleLeavesExistingConsole(t *testing.T) {
	before := windowsConsoleWindow()
	if before == 0 {
		t.Skip("no console attached")
	}
	ensureHiddenHostConsole()
	if got := windowsConsoleWindow(); got != before {
		t.Fatalf("must not replace an existing developer/test console: before=%v after=%v", before, got)
	}
}

func TestHideCommandWindowSetsCreateNoWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/c", "echo ok")
	hideCommandWindow(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&_CREATE_NO_WINDOW == 0 {
		t.Fatal("hideCommandWindow must set CREATE_NO_WINDOW")
	}
	hideCommandWindow(cmd)
	if cmd.SysProcAttr.CreationFlags&_CREATE_NO_WINDOW == 0 {
		t.Fatal("hideCommandWindow must be idempotent")
	}
}

func TestIsWSLShell_WindowsSystem32(t *testing.T) {
	if !isWSLShell(`C:\Windows\System32\bash.exe`) {
		t.Fatal("expected C:\\Windows\\System32\\bash.exe to be detected as WSL")
	}
	if !isWSLShell(`C:\WINDOWS\system32\bash.exe`) {
		t.Fatal("expected case-insensitive System32 path to be detected as WSL")
	}
}

func TestIsWSLShell_WindowsApps(t *testing.T) {
	if !isWSLShell(`C:\Program Files\WindowsApps\bash.exe`) {
		t.Fatal("expected WindowsApps path to be detected as WSL")
	}
	// Also test the Microsoft\WindowsApps alias path.
	if !isWSLShell(`C:\Users\testuser\AppData\Local\Microsoft\WindowsApps\bash.exe`) {
		t.Fatal("expected Microsoft\\WindowsApps path to be detected as WSL")
	}
}

func TestIsWSLShell_GitBashNotWSL(t *testing.T) {
	if isWSLShell(`C:\Program Files\Git\bin\sh.exe`) {
		t.Fatal("expected Git Bash to NOT be detected as WSL")
	}
	if isWSLShell(`C:\Program Files\Git\usr\bin\bash.exe`) {
		t.Fatal("expected Git Bash bash.exe to NOT be detected as WSL")
	}
}

func TestIsWSLShell_MSYS2NotWSL(t *testing.T) {
	if isWSLShell(`C:\msys64\usr\bin\bash.exe`) {
		t.Fatal("expected MSYS2 bash to NOT be detected as WSL")
	}
}

func TestIsWSLShell_CygwinNotWSL(t *testing.T) {
	if isWSLShell(`C:\cygwin64\bin\bash.exe`) {
		t.Fatal("expected Cygwin bash to NOT be detected as WSL")
	}
}

func TestFindSh_ReturnsNonEmptyOnError(t *testing.T) {
	app := &App{}
	path, err := app.findSh()
	// On this machine Git Bash is likely installed, so we expect success.
	// If not, the error message should mention Git Bash.
	if err != nil {
		if !strings.Contains(err.Error(), "Git Bash") && !strings.Contains(err.Error(), "Git for Windows") {
			t.Fatalf("error should mention Git Bash/Git for Windows, got: %s", err.Error())
		}
		return
	}
	if path == "" {
		t.Fatal("expected non-empty shell path when no error")
	}
	if !strings.Contains(strings.ToLower(path), "sh.exe") && !strings.Contains(strings.ToLower(path), "bash.exe") {
		t.Fatalf("expected shell path to contain sh.exe or bash.exe, got: %s", path)
	}
}

func TestVCRedistRegistryPathAndInstaller(t *testing.T) {
	tests := []struct {
		name               string
		processorArch      string
		processorArchW6432 string
		wantRegPath        string
		wantFileName       string
	}{
		{
			name:          "amd64",
			processorArch: "AMD64",
			wantRegPath:   `SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64`,
			wantFileName:  "vc_redist.x64.exe",
		},
		{
			name:          "native arm64",
			processorArch: "ARM64",
			wantRegPath:   `SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\ARM64`,
			wantFileName:  "vc_redist.arm64.exe",
		},
		{
			name:               "emulated arm64",
			processorArch:      "AMD64",
			processorArchW6432: "arm64",
			wantRegPath:        `SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\ARM64`,
			wantFileName:       "vc_redist.arm64.exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PROCESSOR_ARCHITECTURE", tt.processorArch)
			t.Setenv("PROCESSOR_ARCHITEW6432", tt.processorArchW6432)

			if got := vcRedistRegistryPath(); got != tt.wantRegPath {
				t.Fatalf("vcRedistRegistryPath() = %q, want %q", got, tt.wantRegPath)
			}

			_, gotFileName := vcRedistInstaller()
			if gotFileName != tt.wantFileName {
				t.Fatalf("vcRedistInstaller() fileName = %q, want %q", gotFileName, tt.wantFileName)
			}
		})
	}
}

func TestWindowsDirFallbacks(t *testing.T) {
	t.Setenv("SystemRoot", `C:\WindowsRoot`)
	t.Setenv("windir", `D:\WindowsDir`)
	if got := windowsDir(); got != `C:\WindowsRoot` {
		t.Fatalf("windowsDir() with SystemRoot = %q, want C:\\WindowsRoot", got)
	}

	t.Setenv("SystemRoot", "")
	t.Setenv("windir", `D:\WindowsDir`)
	if got := windowsDir(); got != `D:\WindowsDir` {
		t.Fatalf("windowsDir() with windir = %q, want D:\\WindowsDir", got)
	}

	t.Setenv("SystemRoot", "")
	t.Setenv("windir", "")
	if got := windowsDir(); got != `C:\Windows` {
		t.Fatalf("windowsDir() fallback = %q, want C:\\Windows", got)
	}
}
