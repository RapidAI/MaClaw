package gopdf

// Corpus-based parsing tests. These exercise the text-extraction pipeline
// against real-world complex PDFs downloaded from the web (see test/corpus).
// The corpus is not committed to the repository; tests skip when files are
// absent so CI stays green in clean checkouts.
//
// Corpus sources:
//   - mozilla/pdf.js test suite (160F-2019, S2, TAMReview, basicapi, ...)
//   - pdf-association/pdf20examples (ISO 32000-2 samples)
//   - arXiv 1706.03762 (Attention Is All You Need — tables, formulas, fonts)
//   - Irish State Examinations Formulae & Tables booklet (55 pages, math)

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

func corpusData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("test", "corpus", name))
	if err != nil {
		t.Skipf("corpus file %s not available", name)
	}
	return data
}

func corpusAllPages(t *testing.T, name string) map[int][]ExtractedText {
	t.Helper()
	data := corpusData(t, name)
	done := make(chan struct{})
	var pages map[int][]ExtractedText
	var err error
	go func() {
		defer close(done)
		pages, err = ExtractTextFromAllPages(data)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("extraction TIMED OUT on %s", name)
	}
	if err != nil {
		t.Fatalf("extraction failed on %s: %v", name, err)
	}
	return pages
}

func corpusPageLines(t *testing.T, name string, page int) []TextLine {
	t.Helper()
	data := corpusData(t, name)
	done := make(chan struct{})
	var lines []TextLine
	var err error
	go func() {
		defer close(done)
		lines, err = ExtractPageLines(data, page)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("line extraction TIMED OUT on %s", name)
	}
	if err != nil {
		t.Fatalf("line extraction failed on %s: %v", name, err)
	}
	return lines
}

func corpusFullText(t *testing.T, name string) string {
	t.Helper()
	pages := corpusAllPages(t, name)
	var sb strings.Builder
	for i := 0; i < len(pages); i++ {
		for _, tx := range pages[i] {
			sb.WriteString(tx.Text)
			sb.WriteByte(' ')
		}
	}
	return sb.String()
}

// countHan counts CJK ideographs in a string.
func countHan(s string) int {
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			n++
		}
	}
	return n
}

// TestCorpus_NoHangAndContent verifies every corpus file extracts without
// hanging and produces some text.
func TestCorpus_NoHangAndContent(t *testing.T) {
	files := []string{
		"PDF20-simple.pdf", "PDF20-utf8-test.pdf", "PDF20-UTF8-annotation.pdf",
		"XiaoBiaoSong.pdf", "arial_unicode_en_cidfont.pdf", "complex_ttf_font.pdf",
		"font_ascent_descent.pdf", "ArabicCIDTrueType.pdf", "basicapi.pdf",
		"S2.pdf", "TAMReview.pdf", "160F-2019.pdf",
		"arxiv_1706.03762.pdf", "formulae_tables.pdf", "ThuluthFeatures.pdf",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			text := corpusFullText(t, name)
			if strings.TrimSpace(text) == "" {
				// PDF20-UTF8-annotation has text only in an annotation;
				// font_ascent_descent draws glyphs with custom encoding.
				if name == "PDF20-UTF8-annotation.pdf" || name == "font_ascent_descent.pdf" {
					t.Skipf("%s has no extractable page text (expected)", name)
				}
				t.Errorf("no text extracted from %s", name)
			}
		})
	}
}

// TestCorpus_PDF20Simple checks the ISO 32000-2 sample.
func TestCorpus_PDF20Simple(t *testing.T) {
	text := corpusFullText(t, "PDF20-simple.pdf")
	if !strings.Contains(text, "Hello World") {
		t.Errorf("expected 'Hello World', got %q", text)
	}
}

// TestCorpus_BasicAPI checks the pdf.js basicapi.pdf sample: text, page count,
// and word-space recovery via line merging.
func TestCorpus_BasicAPI(t *testing.T) {
	data := corpusData(t, "basicapi.pdf")
	if n, err := GetSourcePDFPageCountFromBytes(data); err != nil || n != 3 {
		t.Errorf("page count = %d, err %v; want 3", n, err)
	}
	lines := corpusPageLines(t, "basicapi.pdf", 0)
	joined := ""
	for _, ln := range lines {
		joined += ln.Text + "\n"
	}
	if !strings.Contains(joined, "Table Of Content") {
		t.Errorf("expected 'Table Of Content' on page 0, got: %q", joined)
	}
}

// TestCorpus_Arxiv checks the Transformer paper: title, word spacing, math
// symbols, and page count.
func TestCorpus_Arxiv(t *testing.T) {
	pages := corpusAllPages(t, "arxiv_1706.03762.pdf")
	if len(pages) < 10 {
		t.Errorf("expected ~15 pages, got %d", len(pages))
	}
	lines := corpusPageLines(t, "arxiv_1706.03762.pdf", 0)
	joined := ""
	for _, ln := range lines {
		joined += ln.Text + "\n"
	}
	if !strings.Contains(joined, "Attention Is All You Need") {
		t.Errorf("title not recovered with spacing, got: %q", joined[:min(len(joined), 400)])
	}
	if !strings.Contains(joined, "Abstract") {
		t.Errorf("'Abstract' not found on page 0")
	}
	// word spacing quality: multiple space-separated words on the page
	if !strings.Contains(joined, "sequence transduction") {
		t.Errorf("body text spacing broken: %q", joined[:min(len(joined), 400)])
	}
}

// TestCorpus_FormulaeTables checks the 55-page math booklet.
func TestCorpus_FormulaeTables(t *testing.T) {
	pages := corpusAllPages(t, "formulae_tables.pdf")
	if len(pages) < 40 {
		t.Errorf("expected ~55 pages, got %d", len(pages))
	}
	text := corpusFullText(t, "formulae_tables.pdf")
	if !strings.Contains(text, "Formulae and Tables") {
		t.Errorf("title not found")
	}
	// math content survives extraction
	if !strings.Contains(text, "sin") || !strings.Contains(text, "cos") {
		t.Errorf("trigonometric content missing")
	}
}

// TestCorpus_XiaoBiaoSong checks GBK-encoded Chinese text recovery.
func TestCorpus_XiaoBiaoSong(t *testing.T) {
	text := corpusFullText(t, "XiaoBiaoSong.pdf")
	if n := countHan(text); n < 50 {
		t.Errorf("expected ≥50 Han characters from GBK font, got %d: %q", n, text)
	}
}

// TestCorpus_160F checks the French tax form (ObjStm-based PDF 1.7).
func TestCorpus_160F(t *testing.T) {
	text := corpusFullText(t, "160F-2019.pdf")
	for _, want := range []string{"identification", "salar"} { // "salaire"/"salarié"
		if !strings.Contains(strings.ToLower(text), want) {
			t.Errorf("expected %q in French form text", want)
		}
	}
}

// TestCorpus_LineMergeQuality checks that merged lines reconstruct words with
// correct spacing on a per-glyph PDF.
func TestCorpus_LineMergeQuality(t *testing.T) {
	lines := corpusPageLines(t, "arxiv_1706.03762.pdf", 1)
	found := false
	for _, ln := range lines {
		if strings.Contains(ln.Text, "recurrent neural networks") ||
			strings.Contains(ln.Text, "Recurrent neural networks") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("sentence with proper spacing not found on page 1")
	}
}

// TestCorpus_MergeTextRunsUnit tests run merging in isolation.
func TestCorpus_MergeTextRunsUnit(t *testing.T) {
	runs := []ExtractedText{
		{Text: "H", X: 100, Y: 700, FontSize: 12, Width: 8},
		{Text: "e", X: 108, Y: 700, FontSize: 12, Width: 6},
		{Text: "l", X: 114, Y: 700, FontSize: 12, Width: 3},
		{Text: "l", X: 117, Y: 700, FontSize: 12, Width: 3},
		{Text: "o", X: 120, Y: 700, FontSize: 12, Width: 7},
		// space: next word 4pt away
		{Text: "W", X: 131, Y: 700, FontSize: 12, Width: 10},
		{Text: "o", X: 141, Y: 700, FontSize: 12, Width: 7},
		// different line (Y is top-down: larger Y = lower on the page)
		{Text: "line2", X: 100, Y: 720, FontSize: 12, Width: 24},
	}
	lines := GroupIntoTextLines(runs)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Text != "Hello Wo" {
		t.Errorf("line 0 = %q, want 'Hello Wo' (gap of 4pt at x=127..131 is > 0.15em => space)", lines[0].Text)
	}
	if lines[1].Text != "line2" {
		t.Errorf("line 1 = %q, want 'line2'", lines[1].Text)
	}
}

// TestCorpus_TAMReviewForms checks Form XObject text extraction — TAMReview's
// pages 2+ carry their content inside /Subtype /Form XObjects.
func TestCorpus_TAMReviewForms(t *testing.T) {
	data := corpusData(t, "TAMReview.pdf")
	pages, err := ExtractTextFromAllPages(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 20 {
		t.Errorf("expected ~23 pages (rounded via page %d), got %d", len(pages), len(pages))
	}
	total := 0
	for _, runs := range pages {
		for _, tx := range runs {
			total += len(tx.Text)
		}
	}
	if total < 10000 {
		t.Errorf("form text missing: extracted %d chars, want >= 10000", total)
	}
	// the running header lives inside a Form XObject
	lines, err := ExtractPageLines(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ln := range lines {
		if strings.Contains(ln.Text, "OverviewoftheTechnology") ||
			strings.Contains(strings.ReplaceAll(ln.Text, " ", ""), "TechnologyAcceptanceModel") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("form XObject header text not extracted")
	}
}
