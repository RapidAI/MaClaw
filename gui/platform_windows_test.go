package main

import (
	"strings"
	"testing"
)

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
	// Also test the Microsoft\WindowsApps alias path
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
