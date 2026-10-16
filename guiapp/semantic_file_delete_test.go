package guiapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteTrustedWorkspaceFileRemovesOnlyTheNamedFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "知识库", "api2.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "keep.md")
	if err := os.WriteFile(other, []byte("stay"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := deleteTrustedWorkspaceFile(dir, filepath.Join("知识库", "api2.md"))
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target still present: %v", statErr)
	}
	if _, statErr := os.Stat(other); statErr != nil {
		t.Fatalf("sibling file changed: %v", statErr)
	}
	if got == "" {
		t.Fatal("empty removal result")
	}
	if _, err := deleteTrustedWorkspaceFile(dir, filepath.Join("知识库", "api2.md")); err == nil {
		t.Fatal("missing file must be rejected")
	}
	nested := filepath.Join(dir, "知识库")
	if _, err := deleteTrustedWorkspaceFile(dir, "知识库"); err == nil {
		t.Fatal("directory must be rejected")
	}
	if _, statErr := os.Stat(nested); statErr != nil {
		t.Fatalf("directory was removed: %v", statErr)
	}
	outside := filepath.Join(filepath.Dir(dir), "outside.md")
	if err := os.WriteFile(outside, []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if _, err := deleteTrustedWorkspaceFile(dir, filepath.Join("..", "outside.md")); err == nil {
		t.Fatal("path outside the workspace must be rejected")
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("outside file changed: %v", statErr)
	}
}

func TestDeleteTrustedWorkspaceFileRejectsAncestorSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(target, []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "out")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := deleteTrustedWorkspaceFile(dir, filepath.Join("out", "secret.md")); err == nil {
		t.Fatal("path through a symlink that leaves the workspace must be rejected")
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Fatalf("outside file changed: %v", statErr)
	}
}

func TestSemanticFileDeleteInvocationArgsFoldsFilePath(t *testing.T) {
	got := semanticFileDeleteInvocationArgs(`{"file_path":"知识库/api2.md"}`)
	if got != `{"path":"知识库/api2.md"}` {
		t.Fatalf("folded args=%s", got)
	}
}
