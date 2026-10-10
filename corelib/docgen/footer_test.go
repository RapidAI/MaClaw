package docgen

import (
	"math"
	"sort"
	"strings"
	"testing"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func TestPaperFooterStartsInsideMargins(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	layout, err := resolvePDFPageLayout("a4")
	if err != nil {
		t.Fatal(err)
	}
	// A last line that ends before the right margin used to leave the cursor
	// there. The footer then continued from that X and was clipped.
	body := "## 方法原理\n" + strings.Repeat("测", 36) + "。\n"
	data, err := gen.Generate(Spec{
		Title:      "论文解读",
		Brand:      "MaClaw",
		FooterHint: "由 MaClaw 伴读生成。网上核对的地址标有（可能），不是论文原文。",
		Colorful:   true,
		PaperSize:  "a4",
		Content:    body,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFooterInside(t, data, layout, "不是论文原文")
}

func TestLongFooterWrapsInsideMargins(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	layout, err := resolvePDFPageLayout("a4")
	if err != nil {
		t.Fatal(err)
	}
	data, err := gen.Generate(Spec{
		Title:      "页脚",
		Brand:      "MaClaw",
		FooterHint: strings.Repeat("页脚须留在版心内。", 12),
		Content:    "正文。\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFooterInside(t, data, layout, "页脚须留在版心内")
}

func assertFooterInside(t *testing.T, data []byte, layout pdfPageLayout, marker string) {
	t.Helper()
	texts, err := gopdf.ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	type line struct {
		y     float64
		items []gopdf.ExtractedText
	}
	var lines []line
	var raw strings.Builder
	for _, item := range texts {
		raw.WriteString(item.Text)
		if item.Text == "" {
			continue
		}
		placed := false
		for i := range lines {
			if math.Abs(lines[i].y-item.Y) <= 1.5 {
				lines[i].items = append(lines[i].items, item)
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, line{y: item.Y, items: []gopdf.ExtractedText{item}})
		}
	}
	left, right := layout.marginX-2, layout.marginX+layout.contentW+2
	found := false
	for _, ln := range lines {
		sort.Slice(ln.items, func(i, j int) bool { return ln.items[i].X < ln.items[j].X })
		var b strings.Builder
		minX := math.MaxFloat64
		for _, item := range ln.items {
			b.WriteString(item.Text)
			if item.X < minX {
				minX = item.X
			}
		}
		if !strings.Contains(strings.ReplaceAll(b.String(), " ", ""), marker) {
			continue
		}
		found = true
		if minX > layout.marginX+8 {
			t.Fatalf("footer starts at x=%.1f, want the left margin %.1f (line %q)", minX, layout.marginX, b.String())
		}
		for _, item := range ln.items {
			if item.X < left || item.X > right {
				t.Fatalf("footer glyph %q at x=%.1f outside [%.1f, %.1f]", item.Text, item.X, left, right)
			}
		}
	}
	if found {
		return
	}
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\t' {
			return -1
		}
		return r
	}, raw.String())
	t.Fatalf("footer line %q missing from page: %q", marker, compact)
}
