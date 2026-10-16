package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

func TestFileCompanionExportFileNameKeepsADocumentName(t *testing.T) {
	if got := fileCompanionExportFileName("C:/notes/周报 .md"); got != "周报-回答.pdf" {
		t.Fatalf("unnamed suffix: %q", got)
	}
	if got := fileCompanionExportFileName(""); got != "document-回答.pdf" {
		t.Fatalf("empty source: %q", got)
	}
	if got := fileCompanionExportFileName("C:/notes/a.md"); !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("missing pdf suffix: %q", got)
	}
	long := fileCompanionExportFileName("C:/notes/" + strings.Repeat("名", 120) + ".md")
	if len([]rune(strings.TrimSuffix(long, "-回答.pdf"))) > 60 {
		t.Fatalf("long name not capped: %q", long)
	}
}

func TestFileCompanionExportPDFRejectsEmptyAndUngrantedPaths(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}

	if _, err := app.FileCompanionExportPDF(path, "   \n "); err == nil || !strings.Contains(err.Error(), "没有可导出的内容") {
		t.Fatalf("blank markdown: %v", err)
	}
	// A file nobody opened has no grant, even next to an open one.
	if _, err := app.FileCompanionExportPDF(filepath.Join(dir, "other.md"), "正文"); err == nil {
		t.Fatal("unopened file exported")
	}
	if _, err := (&App{}).FileCompanionExportPDF(path, "正文"); err == nil {
		t.Fatal("nil-less app exported")
	}
}

func TestFileCompanionExportPDFCancelledDialogWritesNothing(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	previous := fileCompanionSavePDFDialog
	defer func() { fileCompanionSavePDFDialog = previous }()
	asked := ""
	fileCompanionSavePDFDialog = func(_ *App, defaultFilename string) (string, error) {
		asked = defaultFilename
		return "", nil
	}

	dest, err := app.FileCompanionExportPDF(path, "## 小结\n\n正文。")
	if err != nil {
		t.Fatalf("cancel reported an error: %v", err)
	}
	if dest != "" {
		t.Fatalf("cancel returned %q", dest)
	}
	if !strings.HasSuffix(asked, "-回答.pdf") {
		t.Fatalf("dialog default name: %q", asked)
	}
	if entries, readErr := os.ReadDir(dir); readErr != nil {
		t.Fatal(readErr)
	} else if len(entries) != 1 {
		t.Fatalf("export touched the source folder: %d entries", len(entries))
	}
}

func TestFileCompanionExportPDFRefusesToOverwriteTheOpenFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	// The source is itself a PDF, which is the case the guard exists for: the
	// save dialog can hand back the open file unchanged.
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	grantFileCompanionSourceForTest(t, app, path)
	previous := fileCompanionSavePDFDialog
	defer func() { fileCompanionSavePDFDialog = previous }()
	fileCompanionSavePDFDialog = func(_ *App, _ string) (string, error) { return path, nil }

	if _, err := app.FileCompanionExportPDF(path, "正文。"); err == nil || !strings.Contains(err.Error(), "不能覆盖") {
		t.Fatalf("overwrite guard: %v", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != "%PDF-1.4\n" {
		t.Fatalf("source changed: %q %v", string(got), readErr)
	}
}

func TestFileCompanionExportPDFWritesTheAnswerAsPDF(t *testing.T) {
	if !docgen.New().HasFont() {
		t.Skip("no usable font on this machine for PDF rendering")
	}
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "report")
	previous := fileCompanionSavePDFDialog
	defer func() { fileCompanionSavePDFDialog = previous }()
	fileCompanionSavePDFDialog = func(_ *App, _ string) (string, error) { return out, nil }

	dest, err := app.FileCompanionExportPDF(path, "## 小结\n\n**重点** 在第二段。\n\n- 第一点\n")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if dest != out+".pdf" {
		t.Fatalf("dest %q", dest)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || !strings.HasPrefix(string(data), "%PDF") {
		t.Fatalf("not a PDF (%d bytes)", len(data))
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != "# alpha\n" {
		t.Fatalf("source changed: %q %v", string(got), readErr)
	}
}

// grantFileCompanionSourceForTest registers an open companion file without
// reading it, so a source whose bytes are not text still has a grant.
func grantFileCompanionSourceForTest(t *testing.T, app *App, path string) {
	t.Helper()
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	st := app.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.grants[canonical] = &fileCompanionGrant{UserPath: path, Canonical: canonical, ReadOnly: true}
}
