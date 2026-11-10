package guiapp

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
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
	plainXMP := []byte("<dc:title>Plain Paper</dc:title>")
	if got := fileCompanionPDFTitleFrom(plainXMP).text; got != "Plain Paper" {
		t.Fatalf("plain xmp = %q", got)
	}
	rdfXMP := []byte(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">XMP Paper</rdf:li></rdf:Alt></dc:title>`)
	if got := fileCompanionPDFTitleFrom(rdfXMP).text; got != "XMP Paper" {
		t.Fatalf("rdf xmp = %q", got)
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
	between := filepath.Join(dir, "between.md")
	if err := os.WriteFile(between, []byte("---\n# Between Rules\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(between)
	if !ok || title != "Between Rules" {
		t.Fatalf("between rules = %q ok %v", title, ok)
	}
	setext := filepath.Join(dir, "setext.md")
	if err := os.WriteFile(setext, []byte("Setext Title\n==========\n# Later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(setext)
	if !ok || title != "Setext Title" {
		t.Fatalf("setext = %q ok %v", title, ok)
	}
	dashed := filepath.Join(dir, "dashed.md")
	if err := os.WriteFile(dashed, []byte("Dash Title\n----------\n# Later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(dashed)
	if !ok || title != "Dash Title" {
		t.Fatalf("dash setext = %q ok %v", title, ok)
	}
	ruled := filepath.Join(dir, "ruled.md")
	if err := os.WriteFile(ruled, []byte("This is a longer introduction\n---\n# Real Title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(ruled)
	if !ok || title != "Real Title" {
		t.Fatalf("rule = %q ok %v", title, ok)
	}
	spaced := filepath.Join(dir, "spaced.md")
	if err := os.WriteFile(spaced, []byte("---\ntitle : \"Spaced Title\" # note\n---\n# Other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(spaced)
	if !ok || title != "Spaced Title" {
		t.Fatalf("spaced title = %q ok %v", title, ok)
	}
	listed := filepath.Join(dir, "listed.md")
	if err := os.WriteFile(listed, []byte("---\nauthor: Ada\ntags:\n  - a\n---\n# Listed Title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(listed)
	if !ok || title != "Listed Title" {
		t.Fatalf("front list = %q ok %v", title, ok)
	}
	closed := filepath.Join(dir, "closed.md")
	if err := os.WriteFile(closed, []byte("# Visible #\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, _, _, ok = fileCompanionDocumentTitle(closed)
	if !ok || title != "Visible" {
		t.Fatalf("closed hashes = %q ok %v", title, ok)
	}
	page := filepath.Join(dir, "page.html")
	if err := os.WriteFile(page, []byte("<html><titleNot>Bad</titleNot><title>Tom &amp; Jerry</title></html>"), 0o644); err != nil {
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
	styled := filepath.Join(dir, "styled.docx")
	writeDocxTitle(t, styled, "", `<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Styled </w:t><w:t>Paper</w:t></w:r></w:p><w:p><w:r><w:t>Body</w:t></w:r></w:p>`)
	title, _, _, ok = fileCompanionDocumentTitle(styled)
	if !ok || title != "Styled Paper" {
		t.Fatalf("styled docx = %q ok %v", title, ok)
	}
	first := filepath.Join(dir, "first.docx")
	writeDocxTitle(t, first, "", `<w:p><w:r><w:t>Short Title</w:t></w:r></w:p><w:p><w:r><w:t>Body</w:t></w:r></w:p>`)
	title, _, _, ok = fileCompanionDocumentTitle(first)
	if !ok || title != "Short Title" {
		t.Fatalf("first paragraph = %q ok %v", title, ok)
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
	fileCompanionLibraryMu.Lock()
	stored, ok := app.readFileCompanionLibraryFileLocked()
	if !ok {
		fileCompanionLibraryMu.Unlock()
		t.Fatal("library missing")
	}
	key := fileCompanionTitleKey(path)
	stale := stored.Titles[key]
	stale.Version = 0
	stored.Titles[key] = stale
	err = app.writeFileCompanionLibraryFileLocked(stored.Items, stored.Titles, false)
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !app.fillFileCompanionLibraryTitles() {
		t.Fatal("old extractor version was not read again")
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

func writeDocxTitle(t *testing.T, path, core, paragraphs string) {
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
	if _, err := entry.Write([]byte(`<?xml version="1.0"?><cp:coreProperties xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + core + `</dc:title></cp:coreProperties>`)); err != nil {
		t.Fatal(err)
	}
	body, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	xml := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + paragraphs + `</w:body></w:document>`
	if _, err := body.Write([]byte(xml)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
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

func writeTitleZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	for name, body := range entries {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileCompanionTitleCoversMoreDocumentFormats(t *testing.T) {
	dir := t.TempDir()
	encoded := "=?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte("论文标题")) + "?="
	cases := []struct {
		name string
		body string
		want string
	}{
		{"book.adoc", "= AsciiDoc Book\nAuthor Name\n:revnumber: 1.0\n\nbody\n", "AsciiDoc Book"},
		{"override.asciidoc", "= Section Heading\n:doctitle: Real Book\n", "Real Book"},
		{"section.adoc", "== Only A Section\n\nbody\n", ""},
		{"notes.txt", "Meeting Notes\n\nfirst body line\n", "Meeting Notes"},
		{"single.txt", "Only content line\n", ""},
		{"blank.txt", "\n\nMeeting Notes\n", ""},
		{"log.txt", "2026-11-10 07:00:00 INFO started\nmore\n", ""},
		{"clock.txt", "07:23:46 boot\nmore\n", ""},
		{"table.txt", "col-a\tcol-b\n1\t2\n", ""},
		{"rule.txt", "========\nbody\n", ""},
		{"url.txt", "https://example.com/page\nbody\n", ""},
		{"config.yaml", "title: YAML Document\nauthor: Ada\n", "YAML Document"},
		{"quoted.yml", "title: \"Quoted YAML\" # note\nauthor: Ada\n", "Quoted YAML"},
		{"nested.yaml", "meta:\n  title: Nested\nname: Root\n", ""},
		{"folded.yml", "title: >-\n  Folded YAML\n  Title\nauthor: Ada\n", "Folded YAML Title"},
		{"list.yaml", "- title: Item\n- title: Other\n", ""},
		{"doc.json", `{"title":"JSON Document","records":[{"title":"Nested"}]}`, "JSON Document"},
		{"manifest.json", `{"name":"Package Name","version":"1.0"}`, "Package Name"},
		{"nested.json", `{"records":[{"title":"Nested"}]}`, ""},
		{"array.json", `["title","Not A Title"]`, ""},
		{"deep.json", `{"meta":{"name":"Inner"},"title":"Outer"}`, "Outer"},
		{"truncated.json", `{"title":"Readable Prefix","records":[{"x":`, "Readable Prefix"},
		{"feed.xml", `<?xml version="1.0"?><rss><channel><title>Feed Title</title></channel></rss>`, "Feed Title"},
		{"book.xml", `<book xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>DocBook Title</dc:title><title>Other</title></book>`, "DocBook Title"},
		{"mail.eml", "From: a@example.com\r\nSubject: Weekly Report\r\nTo: b@example.com\r\n\r\nSubject: body line\r\n", "Weekly Report"},
		{"encoded.eml", "Subject: " + encoded + "\r\n\r\nbody\r\n", "论文标题"},
		{"folded.eml", "Subject: First part\r\n second part\r\n\r\nbody\r\n", "First part second part"},
		{"nosubject.eml", "From: a@example.com\r\n\r\nSubject: body only\r\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			title, _, _, ok := fileCompanionDocumentTitle(path)
			if !ok || title != tc.want {
				t.Fatalf("title = %q ok %v, want %q", title, ok, tc.want)
			}
		})
	}
}

// A bounded read stops mid-rune on a long UTF-8 document. That tail is not
// evidence of a legacy codepage, and guessing one turns a Chinese heading into
// noise.
func TestFileCompanionTitleDecodesLegacyTextEncodings(t *testing.T) {
	dir := t.TempDir()
	gbkHead, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("# 中文标题\n\n正文\n"))
	if err != nil {
		t.Fatal(err)
	}
	gbkPath := filepath.Join(dir, "gbk.md")
	if err := os.WriteFile(gbkPath, gbkHead, 0o644); err != nil {
		t.Fatal(err)
	}
	if title, _, _, ok := fileCompanionDocumentTitle(gbkPath); !ok || title != "中文标题" {
		t.Fatalf("gbk markdown = %q ok %v", title, ok)
	}
	gbkText, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("中文标题\n\n正文\n"))
	if err != nil {
		t.Fatal(err)
	}
	txtPath := filepath.Join(dir, "gbk.txt")
	if err := os.WriteFile(txtPath, gbkText, 0o644); err != nil {
		t.Fatal(err)
	}
	if title, _, _, ok := fileCompanionDocumentTitle(txtPath); !ok || title != "中文标题" {
		t.Fatalf("gbk text = %q ok %v", title, ok)
	}
	latin, err := charmap.Windows1252.NewEncoder().Bytes([]byte("Café Notes\n\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	latinPath := filepath.Join(dir, "latin.txt")
	if err := os.WriteFile(latinPath, latin, 0o644); err != nil {
		t.Fatal(err)
	}
	if title, _, _, ok := fileCompanionDocumentTitle(latinPath); !ok || title != "Café Notes" {
		t.Fatalf("windows-1252 = %q ok %v", title, ok)
	}
	utf16Path := filepath.Join(dir, "utf16.md")
	raw := []byte{0xFF, 0xFE}
	for _, r := range "# UTF16 Title\n\nbody\n" {
		raw = append(raw, byte(r), byte(r>>8))
	}
	if err := os.WriteFile(utf16Path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if title, _, _, ok := fileCompanionDocumentTitle(utf16Path); !ok || title != "UTF16 Title" {
		t.Fatalf("utf-16 = %q ok %v", title, ok)
	}
	// Cut the last rune of a UTF-8 heading in half at the read boundary.
	long := filepath.Join(dir, "cut.md")
	body := []byte("# 中文标题\n\n")
	body = append(body, bytes.Repeat([]byte("x"), 8192)...)
	body = append(body, []byte("尾")...)
	if err := os.WriteFile(long, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if title, _, _, ok := fileCompanionDocumentTitle(long); !ok || title != "中文标题" {
		t.Fatalf("cut utf-8 = %q ok %v", title, ok)
	}
}

func TestFileCompanionHTMLTitleFallsBackPastAnEmptyTitle(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string
		want string
	}{
		{"og.html", `<html><head><title></title><meta property="og:title" content="Share Card"></head></html>`, "Share Card"},
		{"twitter.html", `<html><head><meta name="twitter:title" content='Tweet Title'></head></html>`, "Tweet Title"},
		{"entity.html", `<html><head><meta property="og:title" content="A &amp; B"></head></html>`, "A & B"},
		{"fragment.html", `<div><h1 class="x">Heading <em>Title</em></h1></div>`, "Heading Title"},
		{"canonical.html", `<html><head><title>Real</title><meta property="og:title" content="Other"></head></html>`, "Real"},
		{"dataattr.html", `<html><head><meta data-title="Not" content="No"></head></html>`, ""},
		{"contentname.html", `<html><head><meta property="og:title" content="filename"></head></html>`, "filename"},
		{"xhtml.xhtml", `<html><head><title>XHTML Page</title></head></html>`, "XHTML Page"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			title, _, _, ok := fileCompanionDocumentTitle(path)
			if !ok || title != tc.want {
				t.Fatalf("title = %q ok %v, want %q", title, ok, tc.want)
			}
		})
	}
}

func TestFileCompanionEPUBTitleFollowsTheContainer(t *testing.T) {
	dir := t.TempDir()
	const opf = `<?xml version="1.0"?><package xmlns:dc="http://purl.org/dc/elements/1.1/"><metadata><dc:title>EPUB Book</dc:title></metadata></package>`
	path := filepath.Join(dir, "book.epub")
	writeTitleZip(t, path, map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<?xml version="1.0"?><container><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"OEBPS/content.opf":      opf,
		"OEBPS/chapter1.xhtml":   `<html><head><title>Chapter One</title></head></html>`,
	})
	if title, _, _, ok := fileCompanionDocumentTitle(path); !ok || title != "EPUB Book" {
		t.Fatalf("epub = %q ok %v", title, ok)
	}
	// No container.xml: the single package document still names the book.
	bare := filepath.Join(dir, "bare.epub")
	writeTitleZip(t, bare, map[string]string{"content.opf": opf})
	if title, _, _, ok := fileCompanionDocumentTitle(bare); !ok || title != "EPUB Book" {
		t.Fatalf("bare epub = %q ok %v", title, ok)
	}
	empty := filepath.Join(dir, "empty.epub")
	writeTitleZip(t, empty, map[string]string{"mimetype": "application/epub+zip"})
	if title, _, _, ok := fileCompanionDocumentTitle(empty); !ok || title != "" {
		t.Fatalf("empty epub = %q ok %v", title, ok)
	}
}

func TestFileCompanionPPTXTitleReadsTheFirstSlide(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deck.pptx")
	writeTitleZip(t, path, map[string]string{
		"docProps/core.xml": `<?xml version="1.0"?><cp:coreProperties xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title></dc:title></cp:coreProperties>`,
		"ppt/slides/slide1.xml": `<p:sld xmlns:a="a" xmlns:p="p"><p:cSld><p:spTree>` +
			`<p:sp><p:nvSpPr><p:nvPr><p:ph type="ctrTitle"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Quarterly </a:t></a:r><a:r><a:t>Review</a:t></a:r></a:p></p:txBody></p:sp>` +
			`<p:sp><p:nvSpPr><p:nvPr><p:ph type="subTitle"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Subtitle text</a:t></a:r></a:p></p:txBody></p:sp>` +
			`</p:spTree></p:cSld></p:sld>`,
	})
	if title, _, _, ok := fileCompanionDocumentTitle(path); !ok || title != "Quarterly Review" {
		t.Fatalf("pptx = %q ok %v", title, ok)
	}
	// An explicit core title still wins over the slide.
	writeOfficeCore(t, filepath.Join(dir, "named.pptx"), "Deck Name")
	if title, _, _, ok := fileCompanionDocumentTitle(filepath.Join(dir, "named.pptx")); !ok || title != "Deck Name" {
		t.Fatalf("named pptx = %q ok %v", title, ok)
	}
}

// A format the extractor used to skip reaches the library row through the same
// fill path as the old ones, and an empty record from the previous version is
// read again rather than kept as "this file has no title".
func TestFileCompanionTitleFillCoversANewFormat(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}
	path := filepath.Join(home, "book.epub")
	writeTitleZip(t, path, map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="content.opf"/></rootfiles></container>`,
		"content.opf":            `<?xml version="1.0"?><package xmlns:dc="http://purl.org/dc/elements/1.1/"><metadata><dc:title>Library Book</dc:title></metadata></package>`,
	})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	item := FileCompanionLibraryItem{
		ID:       "local:book",
		Name:     "book.epub",
		Path:     path,
		Source:   "local",
		OpenedAt: "2026-10-25T00:00:00Z",
	}
	fileCompanionLibraryMu.Lock()
	err = app.writeFileCompanionLibraryFileLocked(
		[]FileCompanionLibraryItem{item},
		map[string]fileCompanionTitleRecord{
			fileCompanionTitleKey(path): {Title: "", Size: info.Size(), Mod: info.ModTime().UnixNano(), Version: fileCompanionTitleVersion - 1},
		},
		false,
	)
	fileCompanionLibraryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !app.fillFileCompanionLibraryTitles() {
		t.Fatal("fill skipped a record from the previous extractor version")
	}
	list, err := app.ListFileCompanionLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Title != "Library Book" {
		t.Fatalf("list title = %#v", list.Items)
	}
	if app.fillFileCompanionLibraryTitles() {
		t.Fatal("unchanged epub was read again")
	}
}
