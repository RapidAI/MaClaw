package gopdf

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPDFDocHandle verifies the parse-once document handle: queries agree
// with the package-level functions and caching is transparent.
func TestPDFDocHandle(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 11)
	pdf.SetInfo(PdfInfo{Title: "handle doc", Author: "gopdf2"})
	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "Handle page one text")
	pdf.SetXY(50, 100)
	_ = pdf.Cell(nil, "Second line with computa-")
	pdf.SetXY(50, 116)
	_ = pdf.Cell(nil, "tional savings.")
	pdf.Line(50, 140, 300, 140)
	pdf.AddOutline("top")
	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "Handle page two text")
	path := filepath.Join(t.TempDir(), "handle.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, err := readFileBytes(path)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := OpenPDFDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.PageCount() != 2 {
		t.Fatalf("pages = %d", doc.PageCount())
	}
	if info := doc.Info(); info.Title != "handle doc" || info.Author != "gopdf2" {
		t.Errorf("info = %+v", doc.Info())
	}
	toc := doc.TOC()
	if len(toc) != 1 || toc[0].Title != "top" || toc[0].Page != 1 {
		t.Errorf("toc = %+v", toc)
	}

	// text matches the package-level API
	txt, err := doc.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	lines, _ := ExtractPageLines(data, 0)
	joined := ""
	for _, ln := range lines {
		joined += ln.Text + "\n"
	}
	if strings.TrimSpace(txt) != strings.TrimSpace(joined) {
		t.Errorf("doc.Text != ExtractPageLines text")
	}

	// dehyphenated search finds the wrapped word
	res := doc.Search("computational", SearchOptions{Dehyphenate: true})
	if len(res) != 1 {
		t.Fatalf("search matches = %d, want 1", len(res))
	}

	// drawings + tables + images don't error and drawings cache is stable
	dr1, _ := doc.Drawings(0)
	dr2, _ := doc.Drawings(0)
	if len(dr1) == 0 || len(dr1) != len(dr2) {
		t.Errorf("drawings = %d/%d", len(dr1), len(dr2))
	}
	tables, _ := doc.Tables(0)
	if len(tables) != 0 {
		t.Errorf("tables = %d, want 0 (no ruled grid)", len(tables))
	}
	images, _ := doc.Images(0)
	if len(images) != 0 {
		t.Errorf("images = %d, want 0", len(images))
	}
	// paragraph access
	paras, _ := doc.Paragraphs(0)
	if len(paras) < 1 {
		t.Errorf("paragraphs = %d", len(paras))
	}
	// out of range
	if _, err := doc.Lines(5); err == nil {
		t.Errorf("out-of-range page should error")
	}
}
