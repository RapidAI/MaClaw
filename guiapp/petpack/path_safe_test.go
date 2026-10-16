package petpack

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeRelRejectsTraversal(t *testing.T) {
	// Clean("a/../../b") = "../b" → reject
	if got := safeRel("a/../../b"); got != "" {
		t.Fatalf("a/../../b → %q, want empty", got)
	}
	if got := safeRel("../x"); got != "" {
		t.Fatalf("../x → %q", got)
	}
	if got := safeRel("/abs/x.png"); got != "" {
		t.Fatalf("abs → %q", got)
	}
	if got := safeRel("native/idle.png"); got != "native/idle.png" {
		t.Fatalf("idle = %q", got)
	}
	if got := safeRel("native/../idle.png"); got != "idle.png" {
		t.Fatalf("native/../idle = %q want idle.png", got)
	}
	if got := safeRel("./native/idle.png"); got != "native/idle.png" {
		t.Fatalf("./native = %q", got)
	}
}

func TestPathUnderRoot(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "pack", "idle.png")
	if err := pathUnderRoot(root, inner); err != nil {
		t.Fatal(err)
	}
	if err := pathUnderRoot(root, root); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "..", "other")
	if err := pathUnderRoot(root, outside); err == nil {
		t.Fatal("expected escape")
	}
	if runtime.GOOS == "windows" {
		// Case-insensitive roots should still match
		if err := pathUnderRoot(stringsToAltCase(root), inner); err != nil {
			t.Fatalf("case fold: %v", err)
		}
	}
}

func stringsToAltCase(s string) string {
	// Flip case of first letter that has an opposite case form.
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
			return string(b)
		}
		if c >= 'A' && c <= 'Z' {
			b[i] = c - 'A' + 'a'
			return string(b)
		}
	}
	return s
}
