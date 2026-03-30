package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClaudeAdapterResolveClaudeExecutablePrefersExeOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific path resolution")
	}

	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "claude.cmd")
	exePath := filepath.Join(dir, "claude.exe")

	if err := os.WriteFile(cmdPath, []byte("@echo off"), 0o644); err != nil {
		t.Fatalf("WriteFile(cmd) error = %v", err)
	}
	if err := os.WriteFile(exePath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(exe) error = %v", err)
	}

	adapter := NewClaudeAdapter(&App{})
	got := adapter.resolveClaudeExecutable(cmdPath)
	if got != exePath {
		t.Fatalf("resolveClaudeExecutable() = %q, want %q", got, exePath)
	}
}

func TestClaudeAdapterBuildCommandEnvPreservesBaseEntries(t *testing.T) {
	t.Setenv("PATH", `C:\Windows\System32`)
	t.Setenv("AppData", `C:\Users\tester\AppData\Roaming`)
	t.Setenv("USERPROFILE", `C:\Users\tester`)
	t.Setenv("HOME", `C:\Users\tester`)

	adapter := NewClaudeAdapter(&App{})
	env := adapter.buildCommandEnv(map[string]string{
		"ANTHROPIC_MODEL": "claude-test",
		"PATH":            `D:\custom\bin`,
	})

	if env["ANTHROPIC_MODEL"] != "claude-test" {
		t.Fatalf("ANTHROPIC_MODEL = %q, want %q", env["ANTHROPIC_MODEL"], "claude-test")
	}
	if env["CLAUDE_CODE_USE_COLORS"] != "true" {
		t.Fatalf("CLAUDE_CODE_USE_COLORS = %q, want %q", env["CLAUDE_CODE_USE_COLORS"], "true")
	}
	// CLAUDE_CODE_MAX_OUTPUT_TOKENS should be set to 128000.
	if env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != "128000" {
		t.Fatalf("CLAUDE_CODE_MAX_OUTPUT_TOKENS = %q, want %q", env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"], "128000")
	}
	if !strings.Contains(env["PATH"], `D:\custom\bin`) {
		t.Fatalf("PATH %q should include custom base PATH", env["PATH"])
	}
	if !strings.Contains(env["PATH"], `C:\Program Files\nodejs`) {
		t.Fatalf("PATH %q should include Node.js path", env["PATH"])
	}
}

// TestClaudeAdapterBuildCommandIncludesPrintFlag verifies that BuildCommand
// produces args containing "-p" (print mode) which is required for Claude Code
// 2.x to accept --output-format/--input-format stream-json.
func TestClaudeAdapterBuildCommandIncludesPrintFlag(t *testing.T) {
	// Create a temp HOME with a fake claude executable so ResolveToolPath finds it.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome) // Windows

	toolsDir := filepath.Join(tmpHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	var fakeBin string
	if runtime.GOOS == "windows" {
		fakeBin = filepath.Join(toolsDir, "claude.exe")
	} else {
		fakeBin = filepath.Join(toolsDir, "claude")
	}
	if err := os.WriteFile(fakeBin, []byte("stub"), 0o755); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	adapter := NewClaudeAdapter(&App{})

	spec := LaunchSpec{
		Tool:        "claude",
		ProjectPath: tmpHome,
		YoloMode:    true,
		Env: map[string]string{
			"ANTHROPIC_AUTH_TOKEN": "sk-test",
		},
	}

	cmd, err := adapter.BuildCommand(spec)
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	// Check that -p is present in args.
	foundP := false
	foundStreamJSON := false
	foundMaxTurns := false
	foundMaxOutputTokens := false
	for i, arg := range cmd.Args {
		if arg == "-p" {
			foundP = true
		}
		if arg == "--output-format" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "stream-json" {
			foundStreamJSON = true
		}
		if arg == "--max-turns" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "200" {
			foundMaxTurns = true
		}
		if arg == "--max-output-tokens" && i+1 < len(cmd.Args) && cmd.Args[i+1] == "128000" {
			foundMaxOutputTokens = true
		}
	}

	if !foundP {
		t.Fatalf("BuildCommand args %v missing -p flag", cmd.Args)
	}
	if !foundStreamJSON {
		t.Fatalf("BuildCommand args %v missing --output-format stream-json", cmd.Args)
	}
	if !foundMaxTurns {
		t.Fatalf("BuildCommand args %v missing --max-turns 200", cmd.Args)
	}
	if !foundMaxOutputTokens {
		t.Fatalf("BuildCommand args %v missing --max-output-tokens 128000", cmd.Args)
	}
}
