package tinytex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "maclaw-latex-verify-")
	if err != nil {
		panic(err)
	}
	verifyCacheRoot = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestCompileDirectoryBuildsFiguresBeforeTheDocumentThatIncludesThem(t *testing.T) {
	dir := t.TempDir()
	fig := "\\documentclass{standalone}\n\\begin{document}fig\\end{document}\n"
	article := "\\documentclass{article}\n\\usepackage{graphicx}\n\\begin{document}\n\\includegraphics[width=0.78\\linewidth]{fig}\n\\end{document}\n"
	if err := os.WriteFile(filepath.Join(dir, "fig.tex"), []byte(fig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "article.tex"), []byte(article), 0o644); err != nil {
		t.Fatal(err)
	}
	var order []string
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, bin, work string, args ...string) (string, int, error) {
			if bin != "pdflatex" {
				t.Fatalf("engine=%q", bin)
			}
			if len(args) != 4 || args[0] != "-interaction=nonstopmode" || args[1] != "-halt-on-error" || args[2] != "-file-line-error" {
				t.Fatalf("args=%v", args)
			}
			for _, arg := range args {
				if strings.ContainsAny(arg, "|&;<>$`") {
					t.Fatalf("shell syntax in argv: %v", args)
				}
			}
			name := args[len(args)-1]
			order = append(order, name)
			pdf := filepath.Join(work, strings.TrimSuffix(name, filepath.Ext(name))+".pdf")
			if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return "ok", 0, nil
		},
	}
	out, err := CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "fig.tex" || order[1] != "article.tex" {
		t.Fatalf("order=%v", order)
	}
	if !strings.Contains(out, "fig.tex ->") || !strings.Contains(out, "article.tex ->") {
		t.Fatalf("out=%q", out)
	}
}

func TestCompileDirectoryReportsAFailureWithoutRewritingSource(t *testing.T) {
	dir := t.TempDir()
	source := "\\documentclass{article}\n\\begin{document}x\\end{document}\n"
	path := filepath.Join(dir, "article.tex")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(context.Context, string, string, ...string) (string, int, error) {
			return "article.tex:1: Undefined control sequence", 1, nil
		},
		Repair: func(context.Context, string, string, string) (string, bool, error) {
			t.Fatal("verification must not repair source")
			return "", false, nil
		},
	}
	out, err := CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Undefined control sequence") {
		t.Fatalf("out=%q", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != source {
		t.Fatalf("source changed: %s", got)
	}
}

func TestCompileDirectoryRefusesAnEmptyEngine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.tex"), []byte("\\documentclass{article}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := CompileDirectory(context.Background(), dir, PreviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "未找到已审核的 LaTeX 引擎") {
		t.Fatalf("out=%q", out)
	}
}

func TestCompileDirectoryIgnoresACommentedIncludeWhenOrdering(t *testing.T) {
	dir := t.TempDir()
	fig := "\\documentclass{standalone}\n% \\includegraphics{article}\n\\begin{document}fig\\end{document}\n"
	article := "\\documentclass{article}\n\\begin{document}\n\\includegraphics*[width=0.78\\linewidth]{fig}\n\\end{document}\n"
	if err := os.WriteFile(filepath.Join(dir, "fig.tex"), []byte(fig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "article.tex"), []byte(article), 0o644); err != nil {
		t.Fatal(err)
	}
	var order []string
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
			name := args[len(args)-1]
			order = append(order, name)
			pdf := filepath.Join(work, strings.TrimSuffix(name, filepath.Ext(name))+".pdf")
			if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return "ok", 0, nil
		},
	}
	if _, err := CompileDirectory(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "fig.tex" || order[1] != "article.tex" {
		t.Fatalf("order=%v", order)
	}
}

func TestCompileDirectoryWarnsWhenAFailedFigureLeavesThePreviousPdf(t *testing.T) {
	dir := t.TempDir()
	fig := "\\documentclass{standalone}\n\\begin{document}fig\\end{document}\n"
	article := "\\documentclass{article}\n\\begin{document}\n\\includegraphics{fig}\\end{document}\n"
	if err := os.WriteFile(filepath.Join(dir, "fig.tex"), []byte(fig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "article.tex"), []byte(article), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
			name := args[len(args)-1]
			if name == "fig.tex" {
				return "fig.tex:1: Undefined control sequence", 1, nil
			}
			pdf := filepath.Join(work, "article.pdf")
			if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return "Output written on article.pdf", 0, nil
		},
	}
	out, err := CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "未通过: fig.tex") || !strings.Contains(out, "引用了未编译成功的 fig.tex") {
		t.Fatalf("out=%q", out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "引用了未编译成功的 fig.tex，PDF 可能仍是上一份产物") {
		t.Fatalf("stale-pdf warning is not the tail: %q", out)
	}
}

func TestCompileDirectorySkipsADocumentThatAlreadyVerified(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "article.tex")
	body := "\\documentclass{article}\n\\begin{document}x\\end{document}\n"
	if err := os.WriteFile(tex, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
			name := args[len(args)-1]
			pdf := filepath.Join(work, strings.TrimSuffix(name, filepath.Ext(name))+".pdf")
			if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return "Overfull \\hbox (4pt too wide)\nHere is how much of TeX's memory you used:\n 10 strings\nOutput written on article.pdf (1 page).\n", 0, nil
		},
	}
	out, err := CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Overfull") || !strings.Contains(out, "Output written") || strings.Contains(out, "memory you used") {
		t.Fatalf("diagnostics = %q", out)
	}
	assertNoProjectStamp(t, dir, "article.pdf")
	calls := 0
	opt.Run = func(context.Context, string, string, ...string) (string, int, error) {
		calls++
		return "", 1, nil
	}
	legacy := filepath.Join(dir, "article.pdf.maclaw-ok")
	if err := os.WriteFile(legacy, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !strings.Contains(out, "已是最新") || strings.Contains(out, "Overfull") {
		t.Fatalf("calls=%d out=%q", calls, out)
	}
	assertNoProjectStamp(t, dir, "article.pdf")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(tex, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileDirectory(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("edited source calls=%d", calls)
	}
}

func TestCompileDirectoryRepeatsAnUnresolvedCitationWhenSkipping(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "article.tex")
	if err := os.WriteFile(tex, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	warning := "LaTeX Warning: Citation `missing' on page 1 undefined on input line 4."
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
			name := args[len(args)-1]
			pdf := filepath.Join(work, strings.TrimSuffix(name, filepath.Ext(name))+".pdf")
			if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return warning + "\nOutput written on article.pdf\n", 0, nil
		},
	}
	out, err := CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Citation `missing'") {
		t.Fatalf("first report dropped the citation: %q", out)
	}
	calls := 0
	opt.Run = func(context.Context, string, string, ...string) (string, int, error) {
		calls++
		return "", 1, nil
	}
	out, err = CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !strings.Contains(out, "已是最新") || !strings.Contains(out, "Citation `missing'") {
		t.Fatalf("calls=%d out=%q", calls, out)
	}
}

func TestCompileDirectoryDoesNotStampABibtexSyntaxError(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "article.tex")
	if err := os.WriteFile(tex, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bib := filepath.Join(dir, "refs.bib")
	if err := os.WriteFile(bib, []byte("@article{a title={t}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "article.bbl"), []byte("\\begin{thebibliography}{1}\n\\end{thebibliography}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(bib, future, future); err != nil {
		t.Fatal(err)
	}
	calls := 0
	opt := PreviewOptions{
		Engine: "pdflatex",
		Bibtex: "bibtex",
		Run: func(_ context.Context, bin, work string, _ ...string) (string, int, error) {
			calls++
			if bin == "bibtex" {
				return "I was expecting a `,' or a `}'---line 1 of file refs.bib\n", 2, nil
			}
			if err := os.WriteFile(filepath.Join(work, "article.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			aux := "\\bibdata{refs}\n\\bibstyle{plain}\n"
			if err := os.WriteFile(filepath.Join(work, "article.aux"), []byte(aux), 0o644); err != nil {
				t.Fatal(err)
			}
			return "Output written on article.pdf\n", 0, nil
		},
	}
	out, err := CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "未通过: article.tex") || !strings.Contains(out, "I was expecting") {
		t.Fatalf("out=%q", out)
	}
	first := calls
	out, err = CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if calls <= first || !strings.Contains(out, "I was expecting") {
		t.Fatalf("calls=%d first=%d out=%q", calls, first, out)
	}
}

func TestCompileDirectoryDoesNotTrustAPdfLeftByAFailedRun(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "article.tex")
	if err := os.WriteFile(tex, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
			calls++
			name := args[len(args)-1]
			pdf := filepath.Join(work, strings.TrimSuffix(name, filepath.Ext(name))+".pdf")
			if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return "article.tex:1: Undefined control sequence", 1, nil
		},
	}
	if _, err := CompileDirectory(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileDirectory(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("failed document was skipped, calls=%d", calls)
	}
}

func TestCompileDirectoryRebuildsTheArticleWhenTheFigureChanges(t *testing.T) {
	dir := t.TempDir()
	fig := filepath.Join(dir, "fig.tex")
	article := filepath.Join(dir, "article.tex")
	bib := filepath.Join(dir, "refs.bib")
	if err := os.WriteFile(fig, []byte("\\documentclass{standalone}\n\\begin{document}fig\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(article, []byte("\\documentclass{article}\n\\begin{document}\n\\includegraphics{fig}\n\\bibliography{refs}\n\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bib, []byte("@article{a,title={t}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
			name := args[len(args)-1]
			pdf := filepath.Join(work, strings.TrimSuffix(name, filepath.Ext(name))+".pdf")
			if err := writePDF(t, pdf); err != nil {
				t.Fatal(err)
			}
			return "Output written on " + strings.TrimSuffix(name, filepath.Ext(name)) + ".pdf\n", 0, nil
		},
	}
	if _, err := CompileDirectory(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(fig, future, future); err != nil {
		t.Fatal(err)
	}
	var order []string
	opt.Run = func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
		name := args[len(args)-1]
		order = append(order, name)
		pdf := filepath.Join(work, strings.TrimSuffix(name, filepath.Ext(name))+".pdf")
		if err := writePDF(t, pdf); err != nil {
			t.Fatal(err)
		}
		return "ok", 0, nil
	}
	if _, err := CompileDirectory(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "fig.tex" || order[1] != "article.tex" {
		t.Fatalf("after figure edit order=%v", order)
	}
	order = nil
	// The figure source was parked in the future to force that rebuild.
	// Put it back in the past so the bibliography edit is the only change.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(fig, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(bib, future.Add(time.Hour), future.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileDirectory(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if len(order) != 1 || order[0] != "article.tex" {
		t.Fatalf("after bib edit order=%v", order)
	}
}

func TestCompileDirectoryRebuildsWhenANestedInputChanges(t *testing.T) {
	dir := t.TempDir()
	writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\begin{document}\n\\input{chap}\n\\end{document}\n")
	writeTex(t, dir, "chap.tex", "% \\input{old}\n\\input{note}\n")
	writeTex(t, dir, "note.tex", "note\n")
	writeTex(t, dir, "old.tex", "old\n")
	assertCompiles(t, dir, "article.tex")

	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "old.tex"), future, future); err != nil {
		t.Fatal(err)
	}
	assertSkips(t, dir)

	later := future.Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "note.tex"), later, later); err != nil {
		t.Fatal(err)
	}
	assertCompiles(t, dir, "article.tex")
	assertNoProjectStamp(t, dir, "article.pdf")
}

func TestCompileDirectoryResolvesANestedInputFromTheCompileDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\begin{document}\n\\input{chapters/chap}\n\\end{document}\n")
	writeTex(t, filepath.Join(dir, "chapters"), "chap.tex", "\\input{detail}\n")
	writeTex(t, dir, "detail.tex", "detail\n")
	writeTex(t, filepath.Join(dir, "chapters"), "detail.tex", "not the lookup root\n")
	assertCompiles(t, dir, "article.tex")

	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "chapters", "detail.tex"), future, future); err != nil {
		t.Fatal(err)
	}
	assertSkips(t, dir)

	later := future.Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "detail.tex"), later, later); err != nil {
		t.Fatal(err)
	}
	assertCompiles(t, dir, "article.tex")
}

func TestCompileDirectoryRebuildsWhenALocalClassOrPackageChanges(t *testing.T) {
	t.Run("class", func(t *testing.T) {
		dir := t.TempDir()
		writeTex(t, dir, "article.tex", "\\documentclass{localcls}\n\\usepackage{amsmath}\n\\begin{document}x\\end{document}\n")
		writeTex(t, dir, "localcls.cls", "% class\n")
		assertCompiles(t, dir, "article.tex")
		assertSkips(t, dir)
		future := time.Now().Add(time.Hour)
		if err := os.Chtimes(filepath.Join(dir, "localcls.cls"), future, future); err != nil {
			t.Fatal(err)
		}
		assertCompiles(t, dir, "article.tex")
		assertNoProjectStamp(t, dir, "article.pdf")
	})
	t.Run("package", func(t *testing.T) {
		dir := t.TempDir()
		writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\usepackage{mypkg,amsmath}\n\\begin{document}x\\end{document}\n")
		writeTex(t, dir, "mypkg.sty", "% package\n")
		assertCompiles(t, dir, "article.tex")
		assertSkips(t, dir)
		future := time.Now().Add(time.Hour)
		if err := os.Chtimes(filepath.Join(dir, "mypkg.sty"), future, future); err != nil {
			t.Fatal(err)
		}
		assertCompiles(t, dir, "article.tex")
	})
}

func TestCompileDirectoryRebuildsWhenTheIncludeWalkCannotFinish(t *testing.T) {
	t.Run("oversized include", func(t *testing.T) {
		dir := t.TempDir()
		writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\begin{document}\n\\input{big}\n\\end{document}\n")
		if err := os.WriteFile(filepath.Join(dir, "big.tex"), bytes.Repeat([]byte("x"), latexInputReadLimit+1), 0o644); err != nil {
			t.Fatal(err)
		}
		assertCompiles(t, dir, "article.tex")
		assertCompiles(t, dir, "article.tex")
	})
	t.Run("deepest package is visible", func(t *testing.T) {
		dir := t.TempDir()
		writeIncludeChain(t, dir, latexInputWalkDepth, "\\usepackage{deeppkg,amsmath}\ntext\n")
		writeTex(t, dir, "deeppkg.sty", "% deep\n")
		assertCompiles(t, dir, "article.tex")
		assertSkips(t, dir)
		future := time.Now().Add(time.Hour)
		if err := os.Chtimes(filepath.Join(dir, "deeppkg.sty"), future, future); err != nil {
			t.Fatal(err)
		}
		assertCompiles(t, dir, "article.tex")
	})
	t.Run("include past the walk cap", func(t *testing.T) {
		dir := t.TempDir()
		writeIncludeChain(t, dir, latexInputWalkDepth+1, "leaf\n")
		assertCompiles(t, dir, "article.tex")
		assertCompiles(t, dir, "article.tex")
	})
}

func TestCompileDirectoryOrdersAFigurePulledInThroughAChapter(t *testing.T) {
	dir := t.TempDir()
	writeTex(t, dir, "fig.tex", "\\documentclass{standalone}\n\\begin{document}fig\\end{document}\n")
	writeTex(t, dir, "chap.tex", "\\includegraphics{fig}\n")
	writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\begin{document}\n\\input{chap}\n\\end{document}\n")
	var order []string
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
			name := args[len(args)-1]
			order = append(order, name)
			if name == "fig.tex" {
				return "fig.tex:1: Undefined control sequence", 1, nil
			}
			if err := writePDF(t, filepath.Join(work, "article.pdf")); err != nil {
				t.Fatal(err)
			}
			return "Output written on article.pdf", 0, nil
		},
	}
	out, err := CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "fig.tex" || order[1] != "article.tex" {
		t.Fatalf("order=%v", order)
	}
	if !strings.Contains(out, "未通过: fig.tex") || !strings.HasSuffix(strings.TrimSpace(out), "引用了未编译成功的 fig.tex，PDF 可能仍是上一份产物") {
		t.Fatalf("out=%q", out)
	}
}

func TestCompileDirectoryFollowsAPrimitiveInput(t *testing.T) {
	dir := t.TempDir()
	writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\begin{document}\n\\input note\n\\input\"spaced name\"\n\\end{document}\n")
	writeTex(t, dir, "note.tex", "note\n")
	writeTex(t, dir, "spaced name.tex", "spaced\n")
	assertCompiles(t, dir, "article.tex")
	assertSkips(t, dir)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "note.tex"), future, future); err != nil {
		t.Fatal(err)
	}
	assertCompiles(t, dir, "article.tex")
	// The future mtime would stay newer than every stamp written now.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "note.tex"), past, past); err != nil {
		t.Fatal(err)
	}
	assertSkips(t, dir)
	if err := os.Chtimes(filepath.Join(dir, "spaced name.tex"), future, future); err != nil {
		t.Fatal(err)
	}
	assertCompiles(t, dir, "article.tex")
}

func TestCompileDirectoryRebuildsWhenALocalBibliographyStyleChanges(t *testing.T) {
	dir := t.TempDir()
	writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\bibliographystyle{elsarticle-num}\n\\bibliographystyle{plain}\n\\begin{document}x\\end{document}\n")
	writeTex(t, dir, "elsarticle-num.bst", "ENTRY{}\n")
	assertCompiles(t, dir, "article.tex")
	assertSkips(t, dir)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "elsarticle-num.bst"), future, future); err != nil {
		t.Fatal(err)
	}
	assertCompiles(t, dir, "article.tex")
}

func TestCompileDirectoryChecksAnImageOutsideTheDirectoryByMtime(t *testing.T) {
	dir := t.TempDir()
	imgDir := t.TempDir()
	img := filepath.Join(imgDir, "pic.png")
	if err := os.WriteFile(img, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\begin{document}\n\\includegraphics{"+filepath.ToSlash(img)+"}\n\\end{document}\n")
	assertCompiles(t, dir, "article.tex")
	assertSkips(t, dir)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(img, future, future); err != nil {
		t.Fatal(err)
	}
	assertCompiles(t, dir, "article.tex")
}

func TestCompileDirectoryRebuildsWhenAnOutsideTeXInputCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	outside := filepath.Join(other, "outside.tex")
	writeTex(t, other, "outside.tex", "outside\n")
	writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\begin{document}\n\\input{"+filepath.ToSlash(outside)+"}\n\\end{document}\n")
	assertCompiles(t, dir, "article.tex")
	assertCompiles(t, dir, "article.tex")
}

func TestCompileDirectoryCompilesAFigureHiddenPastTheWalkBeforeTheArticle(t *testing.T) {
	dir := t.TempDir()
	writeIncludeChain(t, dir, latexInputWalkDepth+1, "\\includegraphics{fig}\n")
	writeTex(t, dir, "fig.tex", "\\documentclass{standalone}\n\\begin{document}fig\\end{document}\n")
	assertCompiles(t, dir, "fig.tex", "article.tex")
}

func writeIncludeChain(t *testing.T, dir string, n int, leaf string) {
	t.Helper()
	writeTex(t, dir, "article.tex", "\\documentclass{article}\n\\begin{document}\n\\input{c1}\n\\end{document}\n")
	for i := 1; i < n; i++ {
		writeTex(t, dir, "c"+strconv.Itoa(i)+".tex", "\\input{c"+strconv.Itoa(i+1)+"}\n")
	}
	writeTex(t, dir, "c"+strconv.Itoa(n)+".tex", leaf)
}

func writeTex(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertCompiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	var order []string
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(_ context.Context, _ string, work string, args ...string) (string, int, error) {
			name := args[len(args)-1]
			order = append(order, name)
			if err := writePDF(t, filepath.Join(work, strings.TrimSuffix(name, filepath.Ext(name))+".pdf")); err != nil {
				t.Fatal(err)
			}
			return "Output written on " + name, 0, nil
		},
	}
	if _, err := CompileDirectory(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if len(order) != len(want) {
		t.Fatalf("order=%v want=%v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order=%v want=%v", order, want)
		}
	}
}

func assertSkips(t *testing.T, dir string) {
	t.Helper()
	calls := 0
	opt := PreviewOptions{
		Engine: "pdflatex",
		Run: func(context.Context, string, string, ...string) (string, int, error) {
			calls++
			return "", 1, nil
		},
	}
	out, err := CompileDirectory(context.Background(), dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !strings.Contains(out, "已是最新") {
		t.Fatalf("calls=%d out=%q", calls, out)
	}
}

func assertNoProjectStamp(t *testing.T, dir, pdfName string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, pdfName+".maclaw-ok")); !os.IsNotExist(err) {
		t.Fatalf("stamp leaked beside the pdf: %v", err)
	}
}

func writePDF(t *testing.T, path string) error {
	t.Helper()
	return os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644)
}
