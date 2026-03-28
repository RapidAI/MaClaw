package configfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureClaudeSecurityHook_FirstInstall(t *testing.T) {
	home := t.TempDir()
	err := EnsureClaudeSecurityHook(home, "/usr/bin/maclaw-tool", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(home, ".claude", "hooks", "maclaw-security.json")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("hook file not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, securityHookMarker) {
		t.Error("hook file missing marker")
	}
	if !strings.Contains(content, "security-check") {
		t.Error("hook file missing security-check command")
	}
	if !strings.Contains(content, "audit-record") {
		t.Error("hook file missing audit-record command")
	}
}

func TestEnsureClaudeSecurityHook_Idempotent(t *testing.T) {
	home := t.TempDir()
	EnsureClaudeSecurityHook(home, "/usr/bin/maclaw-tool", "test", nil)
	hookPath := filepath.Join(home, ".claude", "hooks", "maclaw-security.json")
	first, _ := os.ReadFile(hookPath)

	// Call again
	EnsureClaudeSecurityHook(home, "/usr/bin/maclaw-tool", "test", nil)
	second, _ := os.ReadFile(hookPath)

	if string(first) != string(second) {
		t.Error("second call modified the file — not idempotent")
	}
}

func TestEnsureClaudeSecurityHook_DoesNotAffectStopHook(t *testing.T) {
	home := t.TempDir()
	hooksDir := filepath.Join(home, ".claude", "hooks")
	os.MkdirAll(hooksDir, 0o755)

	stopContent := `{"_comment":"maclaw-anti-premature-exit","hooks":{"Stop":[]}}`
	stopPath := filepath.Join(hooksDir, "maclaw-stop.json")
	os.WriteFile(stopPath, []byte(stopContent), 0o644)

	EnsureClaudeSecurityHook(home, "/usr/bin/maclaw-tool", "test", nil)

	after, _ := os.ReadFile(stopPath)
	if string(after) != stopContent {
		t.Error("security hook injection modified stop hook file")
	}
}

func TestEnsureClaudeSecurityHook_WindowsPath(t *testing.T) {
	home := t.TempDir()
	EnsureClaudeSecurityHook(home, `C:\Program Files\maclaw\maclaw-tool.exe`, "test", nil)
	hookPath := filepath.Join(home, ".claude", "hooks", "maclaw-security.json")
	data, _ := os.ReadFile(hookPath)
	content := string(data)
	// Should have escaped backslashes
	if strings.Contains(content, `C:\Program`) && !strings.Contains(content, `C:\\Program`) {
		t.Error("backslashes not escaped in hook command")
	}
}
