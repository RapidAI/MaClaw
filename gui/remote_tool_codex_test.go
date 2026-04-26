package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCodexAdapterBuildCommandPinsSafeModelProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AppData", filepath.Join(tmpHome, "AppData", "Roaming"))
	t.Setenv("PATH", os.Getenv("PATH"))

	toolsDir := filepath.Join(tmpHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll toolsDir: %v", err)
	}
	fakeBin := filepath.Join(toolsDir, "codex")
	if runtime.GOOS == "windows" {
		fakeBin = filepath.Join(toolsDir, "codex.exe")
	}
	if err := os.WriteFile(fakeBin, []byte("stub"), 0o755); err != nil {
		t.Fatalf("WriteFile fake codex: %v", err)
	}

	adapter := NewCodexAdapter(&App{})
	cmd, err := adapter.BuildCommand(LaunchSpec{
		Tool:        "codex",
		ProjectPath: tmpHome,
		ModelName:   "openai",
		ModelID:     "codex/gpt-5.4",
		Env: map[string]string{
			"OPENAI_API_KEY":  "sk-test",
			"OPENAI_BASE_URL": "http://api.rapidai.tech:20128/v1",
		},
	})
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}
	if !containsArgPair(cmd.Args, "-c", `model_provider="openai-compatible"`) {
		t.Fatalf("args %v missing safe model_provider override", cmd.Args)
	}
	if containsArgPair(cmd.Args, "-c", `model_provider="openai"`) {
		t.Fatalf("args %v should not pin reserved openai provider", cmd.Args)
	}
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}
