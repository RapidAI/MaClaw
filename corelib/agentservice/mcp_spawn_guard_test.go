package agentservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The spawn guard keeps local MCP binaries out of world-writable locations.
// The env hook exists so the helper-process pattern in MaClawSrv can
// re-execute the test binary from %TEMP%\go-build*; these tests pin both
// sides of that contract.
func TestValidateLocalMCPSpawnTargetRejectsTemp(t *testing.T) {
	t.Setenv("MACLAW_MCP_SPAWN_TRUST_DIRS", "")
	dir := filepath.Join(t.TempDir(), "nested")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "helper.exe")
	if err := os.WriteFile(target, []byte("mz"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := validateLocalMCPSpawnTarget(target)
	if err == nil {
		t.Fatalf("temp-dir binary should be rejected")
	}
	if !strings.Contains(err.Error(), "untrusted directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateLocalMCPSpawnTargetHonorsTrustHook(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gobuild", "b001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "helper.exe")
	if err := os.WriteFile(target, []byte("mz"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateLocalMCPSpawnTarget(target); err == nil {
		t.Fatalf("temp-dir binary should be rejected without the hook")
	}
	t.Setenv("MACLAW_MCP_SPAWN_TRUST_DIRS", dir)
	if err := validateLocalMCPSpawnTarget(target); err != nil {
		t.Fatalf("trusted test dir should pass: %v", err)
	}
	// The hook is surgical: a sibling directory under the same temp root
	// stays rejected.
	sibling := filepath.Join(filepath.Dir(dir), "b002", "helper.exe")
	if err := os.MkdirAll(filepath.Dir(sibling), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("mz"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateLocalMCPSpawnTarget(sibling); err == nil {
		t.Fatalf("sibling outside the trusted dir should stay rejected")
	}
}
