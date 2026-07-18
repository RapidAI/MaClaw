package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFileToolPath(t *testing.T) {
	project := filepath.Clean("/tmp/project")
	got, err := ResolveFileToolPath("docs/a.md", func() string { return project })
	if err != nil {
		t.Fatalf("ResolveFileToolPath error: %v", err)
	}
	want := filepath.Join(project, "docs", "a.md")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestResolveFileToolPath_Absolute(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "x.txt")
	got, err := ResolveFileToolPath(abs, nil)
	if err != nil {
		t.Fatalf("ResolveFileToolPath error: %v", err)
	}
	if got != abs {
		t.Fatalf("path = %q, want %q", got, abs)
	}
}

func TestResolveFileToolPath_TildeHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	got, err := ResolveFileToolPath("~/.maclaw/workspace/note.md", nil)
	if err != nil {
		t.Fatalf("ResolveFileToolPath tilde error: %v", err)
	}
	want := filepath.Clean(filepath.Join(home, ".maclaw", "workspace", "note.md"))
	if got != want {
		t.Fatalf("tilde path = %q, want %q", got, want)
	}
	// ~otheruser must not be treated as home expansion.
	other, err := ResolveFileToolPath("~otheruser/docs", nil)
	if err != nil {
		t.Fatalf("ResolveFileToolPath ~otheruser error: %v", err)
	}
	if other == filepath.Join(home, "otheruser", "docs") || other == filepath.Clean(filepath.Join(home, "otheruser/docs")) {
		t.Fatalf("~otheruser was incorrectly expanded to home: %q", other)
	}
}

func TestResolveFileToolPath_EmptyUsesProjectDir(t *testing.T) {
	project := filepath.Clean("/tmp/project-root")
	got, err := ResolveFileToolPath("", func() string { return project })
	if err != nil {
		t.Fatalf("ResolveFileToolPath empty error: %v", err)
	}
	if got != project {
		t.Fatalf("empty path = %q, want project dir %q", got, project)
	}
}

func TestResolveFileToolPath_EmptyWithoutProjectErrors(t *testing.T) {
	if _, err := ResolveFileToolPath("", nil); err == nil {
		t.Fatal("expected error when empty path and no project resolver")
	}
}

func TestWriteTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	size, err := WriteTextFile(path, "hello", "overwrite")
	if err != nil {
		t.Fatalf("WriteTextFile overwrite error: %v", err)
	}
	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}
	if _, err := WriteTextFile(path, " world", "append"); err != nil {
		t.Fatalf("WriteTextFile append error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("content = %q, want %q", string(data), "hello world")
	}
}

func TestWriteTextFile_EmptyContentAllowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	size, err := WriteTextFile(path, "", "overwrite")
	if err != nil {
		t.Fatalf("WriteTextFile empty error: %v", err)
	}
	if size != 0 {
		t.Fatalf("size = %d, want 0", size)
	}
}

func TestWriteTextFile_InvalidMode(t *testing.T) {
	_, err := WriteTextFile(filepath.Join(t.TempDir(), "x.txt"), "a", "edit")
	if err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestEditTextFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edit.txt")
	if err := os.WriteFile(path, []byte("a b a"), 0o644); err != nil {
		t.Fatalf("seed file error: %v", err)
	}
	res, err := EditTextFile(path, "a", "x", false)
	if err != nil {
		t.Fatalf("EditTextFile error: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("count = %d, want 1", res.Count)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "x b a" {
		t.Fatalf("content = %q, want %q", string(data), "x b a")
	}
	res, err = EditTextFile(path, "a", "y", true)
	if err != nil {
		t.Fatalf("EditTextFile replaceAll error: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("count = %d, want 1", res.Count)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "x b y" {
		t.Fatalf("content = %q, want %q", string(data), "x b y")
	}
}

func TestEditTextFile_NotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edit.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("seed file error: %v", err)
	}
	if _, err := EditTextFile(path, "zzz", "x", false); err == nil {
		t.Fatal("expected not found error")
	}
}
