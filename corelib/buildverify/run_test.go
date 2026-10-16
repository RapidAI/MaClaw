package buildverify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tinytex"
)

func TestRunCompilesALatexDirectoryWithoutAModelCommand(t *testing.T) {
	dir := t.TempDir()
	paper := filepath.Join(dir, "elsarticle")
	if err := os.MkdirAll(paper, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "\\documentclass{standalone}\n\\begin{document}x\\end{document}\n"
	if err := os.WriteFile(filepath.Join(paper, "fig.tex"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := compileLatex
	t.Cleanup(func() { compileLatex = orig })
	var gotDir string
	compileLatex = func(_ context.Context, compiled string, _ tinytex.PreviewOptions) (string, error) {
		gotDir = compiled
		return "fig.tex -> fig.pdf", nil
	}
	out, err := Run(context.Background(), dir, paper, "build")
	if err != nil {
		t.Fatal(err)
	}
	if out != "fig.tex -> fig.pdf" || gotDir != paper {
		t.Fatalf("out=%q dir=%q", out, gotDir)
	}
	if _, err := Run(context.Background(), dir, paper, "test"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("latex test err=%v", err)
	}
	if _, err := Run(context.Background(), dir, dir, "build"); !errors.Is(err, ErrUnrecognised) {
		t.Fatalf("unmarked parent err=%v", err)
	}
}

func TestRunReturnsPartialLatexOutputWhenTheBudgetExpires(t *testing.T) {
	dir := t.TempDir()
	paper := filepath.Join(dir, "elsarticle")
	if err := os.MkdirAll(paper, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "\\documentclass{standalone}\n\\begin{document}x\\end{document}\n"
	if err := os.WriteFile(filepath.Join(paper, "fig.tex"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := compileLatex
	t.Cleanup(func() { compileLatex = orig })
	compileLatex = func(ctx context.Context, _ string, _ tinytex.PreviewOptions) (string, error) {
		<-ctx.Done()
		return "fig.tex\n编译超时", ctx.Err()
	}
	parent, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	out, err := Run(parent, dir, paper, "build")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if !strings.Contains(out, "fig.tex") {
		t.Fatalf("partial output dropped: %q", out)
	}
}
