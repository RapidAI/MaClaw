package guiapp

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFileCompanionPDFTitlePrefersTheInfoDictionary(t *testing.T) {
	body := []byte("%PDF-1.4\n/Title (Outline item) /Parent 1 0 R\n/Title (Hello \\(World\\)) /Author (A) /Producer (Maclaw)\n%%EOF\n")
	if got := fileCompanionPDFTitleFrom(body).text; got != "Hello (World)" {
		t.Fatalf("title = %q", got)
	}
	indirect := []byte("12 0 obj\n(Indirect Title)\nendobj\n/Title 12 0 R /Author (A)\n")
	if got := fileCompanionPDFTitleFrom(indirect).text; got != "Indirect Title" {
		t.Fatalf("indirect = %q", got)
	}
	utf16Literal := []byte("/Title (\xFE\xFF\x8B\xBA\x65\x87) /Producer (p)\n")
	if got := fileCompanionPDFTitleFrom(utf16Literal).text; got != "论文" {
		t.Fatalf("utf16 literal = %q", got)
	}
	// The outline sits after the info dictionary. A shared window scores them
	// the same and the later entry used to win.
	reversed := []byte("<< /Title (Hello \\(World\\)) /Author (A) /Producer (Maclaw) >>\n<< /Title (Outline item) /Parent 1 0 R >>\n")
	if got := fileCompanionPDFTitleFrom(reversed).text; got != "Hello (World)" {
		t.Fatalf("outline after info = %q", got)
	}
	// A non-string "N G obj" before the real string object is not that object.
	noise := []byte("12 0 obj\nnot-a-string\nendobj\n12 0 obj\n(Indirect Title)\nendobj\n/Title 12 0 R /Author (A)\n")
	if got := fileCompanionPDFTitleFrom(noise).text; got != "Indirect Title" {
		t.Fatalf("indirect after noise = %q", got)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	buf.Write(bytes.Repeat([]byte(" "), 300<<10))
	buf.WriteString("/Title (Outline item) /Parent 2 0 R\n")
	buf.WriteString("/Title <FEFF8BBA6587> /Producer (Maclaw)\n%%EOF\n")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	title, size, _, ok := fileCompanionDocumentTitle(path)
	if !ok || title != "论文" || size != int64(buf.Len()) {
		t.Fatalf("title = %q size %d ok %v", title, size, ok)
	}
}

func TestFileCompanionPDFTitleDoesNotJoinHeadAndTail(t *testing.T) {
	const head = 64 << 10
	const tail = 192 << 10
	raw := make([]byte, head+tail+8)
	copy(raw[head-len("/Title (Not"):head], []byte("/Title (Not"))
	copy(raw[head:], []byte(" A Title) /Producer (x)\n"))
	copy(raw[len(raw)-len("/Title (Real Paper) /Author (Ada)\n"):], []byte("/Title (Real Paper) /Author (Ada)\n"))
	dir := t.TempDir()
	path := filepath.Join(dir, "wide.pdf")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok := fileCompanionDocumentTitle(path)
	if !ok || title != "Real Paper" {
		t.Fatalf("title = %q ok %v", title, ok)
	}
}

func TestFileCompanionTextAndOfficeTitles(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(md, []byte("---\ntitle: \"Front Title\"\n---\n# Ignored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok := fileCompanionDocumentTitle(md)
	if !ok || title != "Front Title" {
		t.Fatalf("markdown = %q ok %v", title, ok)
	}
	heading := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(heading, []byte("intro\n# Real heading\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(heading)
	if !ok || title != "Real heading" {
		t.Fatalf("heading = %q ok %v", title, ok)
	}
	fenced := filepath.Join(dir, "fenced.md")
	if err := os.WriteFile(fenced, []byte("---\ntitle:\n---\n```\n# Fenced\n```\n# [Visible](https://example.com)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(fenced)
	if !ok || title != "Visible" {
		t.Fatalf("fenced = %q ok %v", title, ok)
	}
	folded := filepath.Join(dir, "folded.md")
	if err := os.WriteFile(folded, []byte("---\ntitle: >\n  Folded Title\n---\n# Other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(folded)
	if !ok || title != "Folded Title" {
		t.Fatalf("folded = %q ok %v", title, ok)
	}
	emptyFold := filepath.Join(dir, "empty-fold.md")
	if err := os.WriteFile(emptyFold, []byte("---\ntitle: >\n---\n# After Fold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(emptyFold)
	if !ok || title != "After Fold" {
		t.Fatalf("empty fold = %q ok %v", title, ok)
	}
	page := filepath.Join(dir, "page.html")
	if err := os.WriteFile(page, []byte("<html><title>Tom &amp; Jerry</title></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(page)
	if !ok || title != "Tom & Jerry" {
		t.Fatalf("html = %q ok %v", title, ok)
	}
	docx := filepath.Join(dir, "paper.docx")
	writeOfficeCore(t, docx, "Office Title")
	title, _, _, ok = fileCompanionDocumentTitle(docx)
	if !ok || title != "Office Title" {
		t.Fatalf("docx = %q ok %v", title, ok)
	}
	other := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(other, []byte("not a title"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(other)
	if !ok || title != "" {
		t.Fatalf("png = %q ok %v", title, ok)
	}
}

func TestFileCompanionLibraryTitleSurvivesARewrite(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	path := filepath.Join(home, "notes.md")
	if err := os.WriteFile(path, []byte("# Deep Work\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := FileCompanionLibraryItem{
		ID:       "local:notes",
		Name:     "notes.md",
		Path:     path,
		Source:   "local",
		OpenedAt: "2026-10-25T00:00:00Z",
	}
	fileCompanionLibraryMu.Lock()
	err := app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{item})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	rev := fileCompanionLibraryRev.Load()
	if !app.fillFileCompanionLibraryTitles() {
		t.Fatal("fill did not store a title")
	}
	if fileCompanionLibraryRev.Load() != rev {
		t.Fatal("title fill bumped the library revision")
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Title != "Deep Work" {
		t.Fatalf("list title = %#v", list.Items)
	}
	rev = fileCompanionLibraryRev.Load()
	if app.fillFileCompanionLibraryTitles() {
		t.Fatal("unchanged file was read again")
	}
	if fileCompanionLibraryRev.Load() != rev {
		t.Fatal("second fill wrote the library")
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryLocked([]FileCompanionLibraryItem{item})
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	list, err = app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Title != "Deep Work" || list.Items[0].Name != "notes.md" {
		t.Fatalf("after rewrite = %#v", list.Items)
	}
}

func writeOfficeCore(t *testing.T, path, title string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	entry, err := zw.Create("docProps/core.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(`<?xml version="1.0"?><cp:coreProperties xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + title + `</dc:title></cp:coreProperties>`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
