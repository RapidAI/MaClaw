package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatexDocumentIgnoresCommentsAndProse(t *testing.T) {
	if LatexDocument("% \\documentclass{article}\nThis sentence mentions \\documentclass.\n") {
		t.Fatal("a comment and a mention were treated as a document")
	}
	if !LatexDocument("\\documentclass[preprint,12pt]{elsarticle}\n\\begin{document}\n\\end{document}\n") {
		t.Fatal("a real document class was not recognised")
	}
	if !LatexDocument("\uFEFF\\documentclass{article}\n") {
		t.Fatal("a BOM-prefixed document class was not recognised")
	}
	code := LatexCode("% \\includegraphics{old}\n\\includegraphics{new} % tail\nkept \\% percent\n")
	if strings.Contains(code, "old") || strings.Contains(code, "tail") {
		t.Fatalf("comment survived: %q", code)
	}
	if !strings.Contains(code, `\includegraphics{new}`) || !strings.Contains(code, `\%`) {
		t.Fatalf("code=%q", code)
	}
}

func TestBuildVerifyProjectKindRecognisesLatexWithoutTakingOverAModule(t *testing.T) {
	root := t.TempDir()
	if _, ok := BuildVerifyProjectKind(root, root); ok {
		t.Fatal("an empty directory was recognised")
	}
	paper := filepath.Join(root, "elsarticle")
	if err := os.MkdirAll(paper, 0o755); err != nil {
		t.Fatal(err)
	}
	tex := "\\documentclass{standalone}\n\\begin{document}x\\end{document}\n"
	if err := os.WriteFile(filepath.Join(paper, "fig.tex"), []byte(tex), 0o644); err != nil {
		t.Fatal(err)
	}
	kind, ok := BuildVerifyProjectKind(root, paper)
	if !ok || kind != "latex" {
		t.Fatalf("kind=%q ok=%v", kind, ok)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/tmp\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kind, ok = BuildVerifyProjectKind(root, root)
	if !ok || kind != "go" {
		t.Fatalf("module root kind=%q ok=%v", kind, ok)
	}
	kind, ok = BuildVerifyProjectKind(root, paper)
	if !ok || kind != "latex" {
		t.Fatalf("paper inside a module kind=%q ok=%v", kind, ok)
	}
	if _, ok := BuildVerifyCommand("latex", "build"); ok {
		t.Fatal("latex build must not be a static argv; the host compiles the directory")
	}
}
