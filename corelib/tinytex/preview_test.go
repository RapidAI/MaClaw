package tinytex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingPackagesAndNames(t *testing.T) {
	log := "! LaTeX Error: File `lipsum.sty' not found.\n! LaTeX Error: File `ctex.cls' not found.\n! I can't find file `chapter.tex'.\n"
	got := MissingPackages(log)
	if strings.Join(got, ",") != "lipsum.sty,ctex.cls" {
		t.Fatalf("missing = %#v", got)
	}
	if PackageNameFromFile("lipsum.sty") != "lipsum" {
		t.Fatal(PackageNameFromFile("lipsum.sty"))
	}
	if PackageNameFromFile("../x.sty") != "" && PackageNameFromFile("../x.sty") != "x" {
		t.Fatal(PackageNameFromFile("../x.sty"))
	}
	if PackageNameFromFile("a.sty") != "" {
		t.Fatal("single-letter package should be rejected")
	}
	imageLog := "! LaTeX Error: File `example-image-a' not found.\n! Package pdftex.def Error: File `example-image-a.pdf' not found.\n"
	images := MissingPackages(imageLog)
	if strings.Join(images, ",") != "example-image-a,example-image-a.pdf" {
		t.Fatalf("images = %#v", images)
	}
	if PackageNameFromFile("example-image-a") != "mwe" || PackageNameFromFile("example-image-a.pdf") != "mwe" {
		t.Fatal(PackageNameFromFile("example-image-a.pdf"))
	}
	if PackageNameFromFile("IEEEtran.bst") != "ieeetran" {
		t.Fatal(PackageNameFromFile("IEEEtran.bst"))
	}
	if strings.Join(MissingBibStyles("I couldn't open style file IEEEtran.bst\n"), ",") != "IEEEtran.bst" {
		t.Fatal(MissingBibStyles("I couldn't open style file IEEEtran.bst\n"))
	}
	if !NeedsBibtex("Please (re)run BibTeX on the file") || !NeedsBiber("Please (re)run Biber") || !NeedsRerun("Rerun to get cross-references right.") {
		t.Fatal("rerun detectors")
	}
	if !NeedsRerun("Package natbib Warning: Citation(s) may have changed. Rerun to get citations correct.") || !NeedsRerun("Package biblatex Warning: Please rerun LaTeX.") {
		t.Fatal("bibliography rerun")
	}
	if NeedsRerun("Output written on main.pdf") {
		t.Fatal("clean log requested a rerun")
	}
}

func TestErrorFileKeepsChapterDirectory(t *testing.T) {
	root := t.TempDir()
	chapterDir := filepath.Join(root, "chapters")
	if err := os.MkdirAll(chapterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wrong := filepath.Join(root, "a.tex")
	chapter := filepath.Join(chapterDir, "a.tex")
	if err := os.WriteFile(wrong, []byte("wrong\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chapter, []byte("chapter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ErrorFile(root, "./chapters/a.tex:3: Undefined control sequence.\n", filepath.Join(root, "main.tex"))
	if got != chapter {
		t.Fatalf("error file = %s", got)
	}
	got = ErrorFile(root, chapter+":4: error\n", wrong)
	if got != chapter {
		t.Fatalf("absolute error file = %s", got)
	}
	got = ErrorFile(root, "D:/not/in/this/project/missing.tex:1: error\n", wrong)
	if got != wrong {
		t.Fatalf("fallback = %s", got)
	}
}

func TestResolveMainFollowsInput(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.tex")
	chap := filepath.Join(dir, "chap.tex")
	if err := os.WriteFile(main, []byte("\\documentclass{article}\n\\begin{document}\n\\input{chap}\n\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chap, []byte("Hello chapter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveMain(chap)
	if err != nil || got != main {
		t.Fatalf("main = %s err=%v", got, err)
	}
	own, err := ResolveMain(main)
	if err != nil || own != main {
		t.Fatalf("own = %s err=%v", own, err)
	}
	nestedDir := filepath.Join(dir, "chapters", "sec")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(nestedDir, "intro.tex")
	if err := os.WriteFile(nested, []byte("nested chapter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("\\documentclass{article}\n\\input{chapters/sec/intro}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveMain(nested)
	if err != nil || got != main {
		t.Fatalf("nested main = %s err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.tex"), []byte("\\documentclass{article}\n\\input{intro}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveMain(nested)
	if err != nil || got != main {
		t.Fatalf("basename in a higher folder stole the main file: %s err=%v", got, err)
	}
}

func TestResolveMainIgnoresMentionedDocumentClass(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.tex")
	chapDir := filepath.Join(dir, "chapters")
	if err := os.MkdirAll(chapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	chap := filepath.Join(chapDir, "intro.tex")
	if err := os.WriteFile(main, []byte("\\documentclass{article}\n\\begin{document}\n\\input{chapters/intro}\n\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chap, []byte("% \\documentclass{article}\n本章说明 \\documentclass{article}。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveMain(chap)
	if err != nil || got != main {
		t.Fatalf("main = %s err=%v", got, err)
	}
	if _, ok := AcceptRepairedSource("\\documentclass{article}\n\\begin{document}\nbody long enough\n\\end{document}\n", "% \\documentclass{article}\nno real class but the reply is long enough\n"); ok {
		t.Fatal("commented documentclass accepted as a repair")
	}
}

func TestAcceptRepairedSource(t *testing.T) {
	orig := "\\documentclass{article}\n\\begin{document}\n\\bad\n\\end{document}\n"
	good := "```latex\n\\documentclass{article}\n\\begin{document}\nfixed\n\\end{document}\n```"
	body, ok := AcceptRepairedSource(orig, good)
	if !ok || !strings.Contains(body, "fixed") || strings.Contains(body, "```") {
		t.Fatalf("body=%q ok=%v", body, ok)
	}
	if _, ok := AcceptRepairedSource(orig, "```latex\nno class here but long enough to pass length\n```"); ok {
		t.Fatal("dropped documentclass")
	}
	long := strings.Repeat("A", 500)
	if _, ok := AcceptRepairedSource(long+orig, "\\documentclass{article}\nshort\n"); ok {
		t.Fatal("truncated replacement")
	}
}

func TestPreviewInstallsPackageThenCompiles(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.tex")
	if err := os.WriteFile(main, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var installed []string
	runs := 0
	res, err := Preview(context.Background(), main, PreviewOptions{
		Engine: "xelatex",
		Run: func(_ context.Context, bin, work string, args ...string) (string, int, error) {
			runs++
			if runs == 1 {
				return "! LaTeX Error: File `lipsum.sty' not found.\n", 1, nil
			}
			pdf := filepath.Join(work, "main.pdf")
			if err := os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644); err != nil {
				return "", -1, err
			}
			return "Output written on main.pdf", 0, nil
		},
		Install: func(_ context.Context, pkg string) error {
			installed = append(installed, pkg)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.PDFPath == "" || res.Repaired {
		t.Fatalf("result = %+v", res)
	}
	if strings.Join(installed, ",") != "lipsum" {
		t.Fatalf("installed = %#v", installed)
	}
}

func TestPreviewRerunsAfterBibliography(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.tex")
	if err := os.WriteFile(main, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var engines, bibs int
	res, err := Preview(context.Background(), main, PreviewOptions{
		Engine: "xelatex",
		Biber:  "biber",
		Run: func(_ context.Context, bin, work string, _ ...string) (string, int, error) {
			if bin == "biber" {
				bibs++
				return "", 0, nil
			}
			engines++
			if err := os.WriteFile(filepath.Join(work, "main.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
				return "", -1, err
			}
			switch engines {
			case 1:
				return "Please (re)run Biber on the file:\n", 0, nil
			case 2:
				return "Package biblatex Warning: Please rerun LaTeX.\n", 0, nil
			default:
				return "Output written on main.pdf", 0, nil
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.PDFPath, "main.pdf") || engines != 3 || bibs != 1 {
		t.Fatalf("pdf=%s engines=%d bibs=%d", res.PDFPath, engines, bibs)
	}
}

func TestPreviewInstallsBibliographyStyle(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.tex")
	if err := os.WriteFile(main, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var installed []string
	var engines, bibs int
	res, err := Preview(context.Background(), main, PreviewOptions{
		Engine: "xelatex",
		Bibtex: "bibtex",
		Run: func(_ context.Context, bin, work string, _ ...string) (string, int, error) {
			if bin == "bibtex" {
				bibs++
				if bibs == 1 {
					return "I couldn't open style file IEEEtran.bst\n", 2, nil
				}
				return "Database file #1: refs.bib\n", 0, nil
			}
			engines++
			if err := os.WriteFile(filepath.Join(work, "main.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
				return "", -1, err
			}
			if engines == 1 {
				return "Package natbib Warning: There were undefined citations.\n", 0, nil
			}
			return "Output written on main.pdf", 0, nil
		},
		Install: func(_ context.Context, pkg string) error {
			installed = append(installed, pkg)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.PDFPath, "main.pdf") || engines != 2 || bibs != 2 || strings.Join(installed, ",") != "ieeetran" {
		t.Fatalf("pdf=%s engines=%d bibs=%d installed=%v", res.PDFPath, engines, bibs, installed)
	}
}

func TestPreviewReportsMissingBibliographyDatabase(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.tex")
	if err := os.WriteFile(main, []byte("\\documentclass{article}\n\\begin{document}x\\end{document}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installed := false
	res, err := Preview(context.Background(), main, PreviewOptions{
		Engine: "xelatex",
		Bibtex: "bibtex",
		Run: func(_ context.Context, bin, work string, _ ...string) (string, int, error) {
			if bin == "bibtex" {
				return "I couldn't open database file refs.bib\n", 2, nil
			}
			if err := os.WriteFile(filepath.Join(work, "main.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
				return "", -1, err
			}
			return "There were undefined citations.\n", 0, nil
		},
		Install: func(context.Context, string) error {
			installed = true
			return nil
		},
	})
	if err == nil || res.PDFPath != "" || !strings.Contains(res.Message, "refs.bib") || installed {
		t.Fatalf("result=%+v err=%v installed=%v", res, err, installed)
	}
}

func TestPreviewRepairsSource(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.tex")
	original := "\\documentclass{article}\n\\begin{document}\n\\badcommand\n\\end{document}\n"
	if err := os.WriteFile(main, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	runs := 0
	res, err := Preview(context.Background(), main, PreviewOptions{
		Engine: "xelatex",
		Run: func(_ context.Context, bin, work string, args ...string) (string, int, error) {
			runs++
			if runs == 1 {
				return "./main.tex:3: Undefined control sequence.\n", 1, nil
			}
			if err := os.WriteFile(filepath.Join(work, "main.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
				return "", -1, err
			}
			return "Output written", 0, nil
		},
		Repair: func(_ context.Context, path, source, excerpt string) (string, bool, error) {
			if !strings.Contains(excerpt, "Undefined") {
				t.Fatalf("excerpt = %s", excerpt)
			}
			fixed := strings.Replace(source, `\badcommand`, `fixed`, 1)
			body, ok := AcceptRepairedSource(source, "```latex\n"+fixed+"\n```")
			return body, ok, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Repaired || !strings.HasSuffix(res.PDFPath, "main.pdf") {
		t.Fatalf("result = %+v", res)
	}
	body, err := os.ReadFile(main)
	if err != nil || !strings.Contains(string(body), "fixed") {
		t.Fatalf("source = %s err=%v", body, err)
	}
	bak, err := os.ReadFile(main + ".maclaw-bak")
	if err != nil || !strings.Contains(string(bak), `\badcommand`) {
		t.Fatalf("backup = %s err=%v", bak, err)
	}
}

func TestRenameReplacingLeavesDestinationWhenSourceIsMissing(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "main.tex")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RenameReplacing(filepath.Join(dir, "missing.tex"), dest); err == nil {
		t.Fatal("missing source was accepted")
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old" {
		t.Fatalf("destination changed to %q", body)
	}
}
