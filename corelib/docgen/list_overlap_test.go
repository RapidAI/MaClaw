package docgen

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

// TestGeneratePDF_OrderedListMultiDigit is an end-to-end regression for multi-digit
// ordered lists (the original "10iu et al." overlap report).
//
// Geometric non-overlap (marker X + width < content X) is covered in GoPDF2.
// This test locks the full docgen path: markdown → HTML <ol> → PDF → extractable text.
func TestGeneratePDF_OrderedListMultiDigit(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}

	var b strings.Builder
	b.WriteString("## 参考文献\n\n")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "%d. Author%d et al., Paper Title Number %d, arXiv:2605.%05d, 2026.\n", i, i, i, i)
	}
	md := b.String()

	html := markdownToHTML(md)
	if !strings.Contains(html, "<ol>") {
		t.Fatalf("expected ordered list HTML, got: %.200s", html)
	}
	if got := strings.Count(html, "<li>"); got != 20 {
		t.Fatalf("expected 20 list items, got %d", got)
	}
	// Numbers are drawn by the PDF engine, not re-baked into <li> text.
	if strings.Contains(html, "<li>1. ") || strings.Contains(html, "<li>10. ") {
		t.Fatal("list item text should not re-include the markdown number prefix")
	}

	data, err := gen.Generate(Spec{
		Title:       "Multi-digit List Overlap Regression",
		ProjectName: "list-overlap",
		Content:     md,
	})
	if err != nil {
		t.Fatalf("PDF generation failed: %v", err)
	}
	if len(data) < 500 {
		t.Fatalf("PDF too small: %d bytes", len(data))
	}

	texts, err := gopdf.ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatalf("ExtractTextFromPage: %v", err)
	}
	if len(texts) == 0 {
		t.Fatal("extracted PDF text is empty")
	}

	// Glyph extractors often emit one run per character; strip whitespace so
	// "1" "0" "." "A" "u"… still matches "10.Author10".
	var raw strings.Builder
	for _, item := range texts {
		raw.WriteString(item.Text)
	}
	compact := stripSpace(raw.String())
	if compact == "" {
		t.Fatal("extracted PDF text is empty after compacting")
	}

	// Spot-check single- and multi-digit items (10+ was the original bug surface).
	for _, n := range []int{1, 9, 10, 11, 20} {
		if !strings.Contains(compact, fmt.Sprintf("%d.", n)) {
			t.Errorf("missing list marker %d.", n)
		}
		if !strings.Contains(compact, fmt.Sprintf("Author%d", n)) {
			t.Errorf("missing Author%d", n)
		}
	}

	t.Logf("compacted text sample: %.300s", compact)
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
