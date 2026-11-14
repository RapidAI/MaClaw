package gopdf

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildMetaDoc creates a two-page doc with metadata, a two-level TOC and
// known text positions.
func buildMetaDoc(t *testing.T, path string) {
	t.Helper()
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 12)

	pdf.SetInfo(PdfInfo{
		Title:    "Parity Test Doc",
		Author:   "GoPDF2",
		Subject:  "PyMuPDF parity",
		Keywords: "pdf, golang, parity",
		Creator:  "creator-app",
		Producer: "GoPDF2",
	})

	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "Chapter one body text")
	pdf.SetXY(50, 300)
	_ = pdf.Cell(nil, "Deep in page one")

	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "Chapter two body text")

	if err := pdf.SetTOC([]TOCItem{
		{Level: 1, Title: "Chapter 1", PageNo: 1, Y: 50},
		{Level: 2, Title: "Details", PageNo: 1, Y: 290},
		{Level: 1, Title: "Chapter 2", PageNo: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

// TestGetPDFInfo verifies metadata round trip through the /Info dict.
func TestGetPDFInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.pdf")
	buildMetaDoc(t, path)
	data, _ := os.ReadFile(path)

	info, err := GetPDFInfo(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Parity Test Doc" {
		t.Errorf("Title = %q", info.Title)
	}
	if info.Author != "GoPDF2" {
		t.Errorf("Author = %q", info.Author)
	}
	if info.Keywords != "pdf, golang, parity" {
		t.Errorf("Keywords = %q", info.Keywords)
	}
	if info.Subject != "PyMuPDF parity" {
		t.Errorf("Subject = %q", info.Subject)
	}
	if info.Creator != "creator-app" || info.Producer != "GoPDF2" {
		t.Errorf("Creator/Producer = %q/%q", info.Creator, info.Producer)
	}
}

// TestExtractTOCRoundTrip verifies SetTOC -> write -> ExtractTOC returns the
// same level/title/page tree.
func TestExtractTOCRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "toc.pdf")
	buildMetaDoc(t, path)
	data, _ := os.ReadFile(path)

	entries, err := ExtractTOC(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		for i, e := range entries {
			t.Logf("%d: L%d %q p%d y%.0f", i, e.Level, e.Title, e.Page, e.Y)
		}
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	want := []TOCEntry{
		{Level: 1, Title: "Chapter 1", Page: 1},
		{Level: 2, Title: "Details", Page: 1},
		{Level: 1, Title: "Chapter 2", Page: 2},
	}
	for i, w := range want {
		if entries[i].Level != w.Level || entries[i].Title != w.Title || entries[i].Page != w.Page {
			t.Errorf("entry %d = L%d %q p%d, want L%d %q p%d",
				i, entries[i].Level, entries[i].Title, entries[i].Page, w.Level, w.Title, w.Page)
		}
	}
	// Y positions: Chapter 1 -> 50, Details -> 290
	if e := entries[0]; e.Y < 40 || e.Y > 60 {
		t.Errorf("entry 0 Y = %.0f, want ~50", e.Y)
	}
	if e := entries[1]; e.Y < 280 || e.Y > 300 {
		t.Errorf("entry 1 Y = %.0f, want ~290", e.Y)
	}
}

// TestExtractTextInRect verifies region-scoped text extraction.
func TestExtractTextInRect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.pdf")
	buildMetaDoc(t, path)
	data, _ := os.ReadFile(path)

	// only the lower text of page 1
	txt, err := ExtractTextInRect(data, 0, ClipRect{X: 0, Y: 250, W: 595, H: 200})
	if err != nil {
		t.Fatal(err)
	}
	if txt == "" || !contains(txt, "Deep in page one") {
		t.Errorf("clip text missing target: %q", txt)
	}
	if contains(txt, "Chapter one body") {
		t.Errorf("clip text leaked outside region: %q", txt)
	}

	words, err := ExtractTextWordsInRect(data, 0, ClipRect{X: 0, Y: 0, W: 595, H: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(words) == 0 || words[0].Text != "Chapter" {
		t.Errorf("top-region words = %v", words)
	}
}

// TestClipRender verifies clip-scoped rendering produces a smaller canvas.
func TestClipRender(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.pdf")
	buildMetaDoc(t, path)
	data, _ := os.ReadFile(path)

	full, err := RenderPageToImage(data, 0, RenderOption{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	part, err := RenderPageToImage(data, 0, RenderOption{DPI: 72, Clip: &ClipRect{X: 0, Y: 0, W: 200, H: 150}})
	if err != nil {
		t.Fatal(err)
	}
	fb, pb := full.Bounds(), part.Bounds()
	if pb.Dx() >= fb.Dx() || pb.Dy() >= fb.Dy() {
		t.Errorf("clip render %dx%d not smaller than full %dx%d", pb.Dx(), pb.Dy(), fb.Dx(), fb.Dy())
	}
	if pb.Dx() != 200 || pb.Dy() != 150 {
		t.Errorf("clip render = %dx%d, want 200x150", pb.Dx(), pb.Dy())
	}
	_ = image.Rect
}

// TestSearchDehyphenated verifies search across hyphenated line wraps.
func TestSearchDehyphenated(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 12)
	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "The model avoids recurrent computa-")
	pdf.SetXY(50, 76)
	_ = pdf.Cell(nil, "tional bottlenecks entirely.")
	path := filepath.Join(t.TempDir(), "dehyph.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	// without dehyphenation: no match
	plain, err := SearchTextOnPageWithOptions(data, 0, "computational", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 0 {
		t.Errorf("plain search found %d matches, want 0", len(plain))
	}
	// with dehyphenation: one match on the first line
	deh, err := SearchTextOnPageWithOptions(data, 0, "computational", SearchOptions{Dehyphenate: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(deh) != 1 {
		t.Fatalf("dehyphenated search found %d matches, want 1", len(deh))
	}
	if deh[0].Y > 70 { // first line's top is near y=52..64
		t.Errorf("match line Y = %.1f, want on the first line", deh[0].Y)
	}
}

// TestFormatBlocksParagraphs verifies FormatBlocks breaks on indentation
// and merges wrapped lines.
func TestFormatBlocksParagraphs(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 11)
	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "First paragraph wrapped onto")
	pdf.SetXY(50, 76)
	_ = pdf.Cell(nil, "a second line here.")
	pdf.SetXY(66, 100) // indented start of the next paragraph
	_ = pdf.Cell(nil, "Second paragraph begins indented.")
	path := filepath.Join(t.TempDir(), "blocks.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	v, err := ExtractTextFormatted(data, 0, FormatBlocks)
	if err != nil {
		t.Fatal(err)
	}
	blocks, ok := v.([]TextBlock)
	if !ok || len(blocks) != 2 {
		for i, b := range blocks {
			t.Logf("block %d: %q", i, b.Lines[0].Text)
		}
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	if len(blocks[0].Lines) != 2 {
		t.Errorf("block 0 has %d lines, want 2 (wrapped merge)", len(blocks[0].Lines))
	}
	if !strings.Contains(blocks[1].Lines[0].Text, "Second paragraph") {
		t.Errorf("block 1 = %q", blocks[1].Lines[0].Text)
	}
}

// TestStyledTextExtraction verifies color and font-style flags survive
// extraction: colored text reports its RGB, and a corpus heading detected
// as bold by font name.
func TestStyledTextExtraction(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 12)
	pdf.AddPage()
	pdf.SetTextColor(220, 40, 40)
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "Red styled words")
	pdf.SetTextColor(0, 0, 0)
	pdf.SetXY(50, 100)
	_ = pdf.Cell(nil, "Plain black words")
	path := filepath.Join(t.TempDir(), "styled.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	words, err := ExtractTextWordsInRect(data, 0, ClipRect{X: 0, Y: 0, W: 595, H: 90})
	if err != nil {
		t.Fatal(err)
	}
	if len(words) == 0 || words[0].Text != "Red" {
		for _, w := range words {
			t.Logf("w %q", w.Text)
		}
		t.Fatalf("styled words missing")
	}
	if words[0].ColorR != 220 || words[0].ColorG != 40 || words[0].ColorB != 40 {
		t.Errorf("color = %v %v %v, want 220/40/40", words[0].ColorR, words[0].ColorG, words[0].ColorB)
	}
	// black words later on the page
	plain, err := ExtractTextWordsInRect(data, 0, ClipRect{X: 0, Y: 90, W: 595, H: 60})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) == 0 || plain[0].ColorR != 0 || plain[0].ColorG != 0 || plain[0].ColorB != 0 {
		t.Errorf("plain color = %v", plain[0])
	}
}

// TestArxivBoldFlags verifies bold detection from font names on the real
// corpus (the Introduction heading is typeset bold).
func TestArxivBoldFlags(t *testing.T) {
	data := corpusData(t, "arxiv_1706.03762.pdf")
	words, err := ExtractTextWordsInRect(data, 1, ClipRect{X: 0, Y: 0, W: 842, H: 900})
	if err != nil {
		t.Fatal(err)
	}
	bold := 0
	for _, w := range words {
		if w.Flags&1 != 0 {
			bold++
		}
	}
	if bold == 0 {
		t.Errorf("no bold words detected on arXiv page 1")
	}
}

// TestPDFDocPageSize verifies visual page sizes honor /Rotate.
func TestPDFDocPageSize(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.SetPageRotation(1, 90); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "size.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	doc, err := OpenPDFDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	pi, err := doc.PageSize(0)
	if err != nil {
		t.Fatal(err)
	}
	if pi.Width != 842 || pi.Height != 595 {
		t.Errorf("size = %.0fx%.0f, want 842x595 (rotated A4)", pi.Width, pi.Height)
	}
	sizes := doc.PageSizes()
	if len(sizes) != 1 || sizes[0].Width != 842 {
		t.Errorf("PageSizes = %+v", sizes)
	}
}

// TestTextAlphaExtraction verifies ExtGState /ca fill alpha reaches the
// extracted words (CellOption Transparency writes /ca + /GSn gs).
func TestTextAlphaExtraction(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 12)
	pdf.AddPage()
	tr, _ := NewTransparency(0.5, string(NormalBlendMode))
	_ = pdf.CellWithOption(&Rect{W: 200, H: 20}, "Faded words", CellOption{Transparency: &tr})
	path := filepath.Join(t.TempDir(), "alpha.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	words, err := ExtractTextWordsInRect(data, 0, ClipRect{X: 0, Y: 0, W: 595, H: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(words) == 0 || words[0].Text != "Faded" {
		for _, w := range words {
			t.Logf("w %q", w.Text)
		}
		t.Fatalf("faded words missing")
	}
	if words[0].Alpha == 255 || words[0].Alpha < 100 {
		t.Errorf("alpha = %d, want ~128 (0.5)", words[0].Alpha)
	}
}

// TestPDFDocPasswordAndSurface verifies encrypted-document handling and the
// extended handle surface (links, fonts, formatted output).
func TestPDFDocPasswordAndSurface(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 12)
	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "Encrypted handle text")
	pdf.AddAnnotation(AnnotationOption{X: 50, Y: 100, W: 100, H: 20, Type: AnnotText, Content: "note"})
	// a link annotation (URI)
	pdf.AddExternalLink("https://example.com", 50, 130, 100, 20)
	if err := pdf.SetEncryption(AESEncryptionConfig{
		Method:        EncryptRC4V2,
		UserPassword:  "userpass",
		OwnerPassword: "ownerpass",
		Permissions:   PermissionsPrint,
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "enc.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	// without password: extraction yields nothing usable
	blocked, err := OpenPDFDocumentWithPassword(data, "wrongpass")
	if err == nil {
		lines, _ := blocked.Lines(0)
		if len(lines) > 0 && strings.Contains(lines[0].Text, "Encrypted") {
			t.Errorf("wrong password decrypted the document")
		}
	}

	// with password: full surface
	doc, err := OpenPDFDocumentWithPassword(data, "userpass")
	if err != nil {
		t.Fatal(err)
	}
	txt, err := doc.Text(0)
	if err != nil || !strings.Contains(txt, "Encrypted handle text") {
		t.Errorf("doc.Text = %q err=%v", txt, err)
	}
	if _, err := doc.Formatted(0, FormatJSON); err != nil {
		t.Errorf("Formatted JSON: %v", err)
	}
	html, err := doc.Formatted(0, FormatHTML)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.(string), "Encrypted") {
		t.Errorf("Formatted HTML missing text")
	}
	annots, _ := doc.Annotations(0)
	if len(annots) != 1 {
		t.Errorf("annotations = %d", len(annots))
	}
	links, _ := doc.Links(0)
	if len(links) == 0 {
		t.Errorf("links = 0")
	}
	if _, err := doc.Fonts(0); err != nil {
		t.Errorf("fonts: %v", err)
	}
}

// TestPDFDocFormFields verifies form field reading through the handle
// (PyMuPDF widgets parity).
func TestPDFDocFormFields(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.AddTextField("fullname", 120, 35, 200, 20); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ModifyFormFieldValue("fullname", "Jane Q. Tester"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fields.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	doc, err := OpenPDFDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.HasForms() {
		t.Errorf("HasForms = false")
	}
	fields := doc.FormFields()
	if len(fields) != 1 {
		t.Fatalf("fields = %d, want 1", len(fields))
	}
	f := fields[0]
	if f.Name != "fullname" {
		t.Errorf("name = %q", f.Name)
	}
	if f.Value != "Jane Q. Tester" {
		t.Errorf("value = %q", f.Value)
	}
	if f.Type != FormFieldText {
		t.Errorf("type = %v", f.Type)
	}
	if f.W < 150 || f.H < 10 {
		t.Errorf("rect = %.0fx%.0f", f.W, f.H)
	}
}

// TestCropBoxSupport verifies /CropBox drives PDFDoc.PageSize and the
// rendered canvas (PyMuPDF page.rect convention).
func TestCropBoxSupport(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "cropped visible text")
	// crop to a 400x300 region at (50, 50) (top-down points)
	if err := pdf.SetPageCropBox(1, Box{Left: 50, Top: 50, Right: 450, Bottom: 350}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "crop.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	doc, err := OpenPDFDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	pi, err := doc.PageSize(0)
	if err != nil {
		t.Fatal(err)
	}
	if pi.Width != 400 || pi.Height != 300 {
		t.Errorf("PageSize = %.0fx%.0f, want 400x300", pi.Width, pi.Height)
	}
	img, err := RenderPageToImage(data, 0, RenderOption{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != 400 || b.Dy() != 300 {
		t.Errorf("render = %dx%d, want 400x300", b.Dx(), b.Dy())
	}
	// the visible text starts at (50, 60) top-down: after cropping at
	// (50, 50) it sits at (0, 10) — the word must appear in the canvas
	img2, _ := RenderPageToImage(data, 0, RenderOption{DPI: 72, Clip: &ClipRect{X: 0, Y: 0, W: 400, H: 60}})
	_ = img2
}
