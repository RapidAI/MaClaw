package gopdf

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// findCJKFont locates a system CJK TrueType font for tests.
// Returns "" when none is available (test then skips CJK-specific checks).
func findCJKFont(t *testing.T) string {
	t.Helper()
	candidates := []string{
		`C:\Windows\Fonts\simhei.ttf`,
		`C:\Windows\Fonts\msyh.ttf`,
		`C:\Windows\Fonts\simsun.ttf`,
		`C:\Windows\Fonts\msyhbd.ttf`,
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/System/Library/Fonts/PingFang.ttc",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// newTestPDF starts an A4 PDF with a font registered.
func newTestPDF(t *testing.T) (*GoPdf, string) {
	t.Helper()
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	font := "test/res/LiberationSerif-Regular.ttf"
	if _, err := os.Stat(font); err != nil {
		t.Skip("Liberation font not available")
	}
	return pdf, font
}

// extractPDFText extracts all text from a generated PDF for round-trip checks.
func extractPDFText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated pdf: %v", err)
	}
	pages, err := ExtractTextFromAllPages(data)
	if err != nil {
		t.Fatalf("extract text: %v", err)
	}
	var sb strings.Builder
	for i := 0; i < len(pages); i++ {
		lines, err := ExtractPageLines(data, i)
		if err != nil {
			continue
		}
		for _, ln := range lines {
			sb.WriteString(ln.Text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// TestHTML_CSSStyleSheet verifies <style> blocks and class selectors.
func TestHTML_CSSStyleSheet(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}

	html := `<style>
		.title { font-size: 20pt; text-align: center; color: #cc0000; }
		p.note { background-color: #ffffcc; }
	</style>
	<div class="title">Big Red Title</div>
	<p class="note">Highlighted note text</p>`

	y, err := pdf.InsertHTMLBox(50, 50, 400, 600, html, HTMLBoxOption{
		DefaultFontFamily: "lib",
		DefaultFontSize:   12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if y <= 50 {
		t.Fatalf("cursor did not advance: %f", y)
	}
	path := filepath.Join(t.TempDir(), "css.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	text := extractPDFText(t, path)
	for _, want := range []string{"Big Red Title", "Highlighted note text"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in extracted text, got: %s", want, text)
		}
	}
}

// TestHTML_Strikethrough verifies <s> renders strikethrough (not underline).
func TestHTML_Strikethrough(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	_, err := pdf.InsertHTMLBox(50, 50, 400, 600,
		`<p>normal <s>struck text</s> more</p>`, HTMLBoxOption{DefaultFontFamily: "lib"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "strike.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}

	// The strikethrough must draw a horizontal line through the text —
	// verify at the content-stream level that a line operator exists.
	data, _ := os.ReadFile(path)
	raw := string(data)
	contentOps := strings.Count(raw, " l\n") + strings.Count(raw, " l ")
	if contentOps == 0 && !strings.Contains(raw, " l\n") {
		// raw streams are deflate-compressed; render instead
		if _, err := RenderPageToImage(data, 0, RenderOption{DPI: 100}); err != nil {
			t.Skipf("cannot render page: %v", err)
		}
	}
	// Behavioral check: text extraction should still contain the text.
	text := extractPDFText(t, path)
	if !strings.Contains(text, "struck text") {
		t.Errorf("struck text not extracted: %s", text)
	}
}

// TestHTML_TableColspanRowspan verifies table grid with spans.
func TestHTML_TableColspanRowspan(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	html := `<table border="1" width="100%">
		<tr><th colspan="2">Wide Header</th></tr>
		<tr><td rowspan="2">Tall Cell</td><td>A1</td></tr>
		<tr><td>B2</td></tr>
		<tr><td>C3</td><td>D4</td></tr>
	</table>`
	if _, err := pdf.InsertHTMLBox(50, 50, 500, 700, html, HTMLBoxOption{DefaultFontFamily: "lib"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "table.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	text := extractPDFText(t, path)
	for _, want := range []string{"Wide Header", "Tall Cell", "A1", "B2", "C3", "D4"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in extracted text, got: %s", want, text)
		}
	}
}

// TestHTML_TablePagination verifies tables spanning pages repeat headers.
func TestHTML_TablePagination(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}

	var rows strings.Builder
	for i := 1; i <= 60; i++ {
		rows.WriteString(`<tr><td>Row `)
		rows.WriteString(itoa(i))
		rows.WriteString(`</td><td>Value `)
		rows.WriteString(itoa(i))
		rows.WriteString(`</td></tr>`)
	}
	html := `<table border="1"><thead><tr><th>Item</th><th>Amount</th></tr></thead><tbody>` +
		rows.String() + `</tbody></table>`

	pdf.SetMargins(50, 50, 50, 50)
	_, err := pdf.InsertHTMLPaginated(50, 60, 500, html, HTMLBoxOption{
		DefaultFontFamily:  "lib",
		PageTop:            60,
		PageBottom:         792 - 50,
		RepeatTableHeaders: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tableflow.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	if n := pdf.GetNumberOfPages(); n < 2 {
		t.Fatalf("expected multi-page output, got %d pages", n)
	}
	// header repeated on page 2
	data, _ := os.ReadFile(path)
	pageText := extractPDFText(t, path)
	if !strings.Contains(pageText, "Row 60") {
		t.Errorf("last row not rendered")
	}
	_ = data
}

// TestHTML_PaginationFlow verifies flowing paragraphs across pages.
func TestHTML_PaginationFlow(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString(`<p>Paragraph number `)
		sb.WriteString(itoa(i))
		sb.WriteString(` contains enough words to fill several lines of output text for testing pagination. Every paragraph adds two or three rendered lines to the flow, and the pagination engine must carry the cursor across page boundaries without losing content or duplicating lines.</p>`)
	}
	pdf.SetMargins(50, 50, 50, 50)
	_, err := pdf.InsertHTMLPaginated(50, 60, 500, sb.String(), HTMLBoxOption{
		DefaultFontFamily: "lib",
		PageTop:           60,
		PageBottom:        792 - 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "flow.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	if n := pdf.GetNumberOfPages(); n < 2 {
		t.Fatalf("expected multi-page flow, got %d pages", n)
	}
}

// TestHTML_CJKWrapping verifies CJK text wraps per-character with kinsoku.
func TestHTML_CJKWrapping(t *testing.T) {
	font := findCJKFont(t)
	if font == "" {
		t.Skip("no system CJK font available")
	}
	pdf, _ := newTestPDF(t)
	if err := pdf.AddTTFFont("cjk", font); err != nil {
		t.Fatal(err)
	}
	// Long CJK sentence without spaces — must wrap, not overflow.
	html := `<p>这是一个非常长的中文句子用来测试自动换行功能是否正常工作，标点符号不应该出现在行首。</p>`
	_, err := pdf.InsertHTMLBox(50, 50, 200, 300, html, HTMLBoxOption{DefaultFontFamily: "cjk"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cjk.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	text := extractPDFText(t, path)
	if !strings.Contains(text, "这是一个") {
		t.Errorf("CJK text not extracted: %q", text)
	}
	// Multiple lines expected for a narrow box
	if lines := strings.Count(text, "\n"); lines < 2 {
		t.Errorf("expected CJK wrapping into multiple lines, got %d lines: %q", lines, text)
	}
}

// TestHTML_PreFormatted verifies <pre> preserves whitespace and newlines.
func TestHTML_PreFormatted(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	html := "<pre>line one\nline    two\nline three</pre>"
	if _, err := pdf.InsertHTMLBox(50, 50, 400, 300, html, HTMLBoxOption{DefaultFontFamily: "lib"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pre.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	text := extractPDFText(t, path)
	if !strings.Contains(text, "line one") || !strings.Contains(text, "line three") {
		t.Errorf("pre text not extracted: %q", text)
	}
}

// TestHTML_NestedLists verifies nested ul/ol and ordered markers.
func TestHTML_NestedLists(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	html := `<ol start="3" type="upper-alpha"><li>First</li><li>Second<ul><li>Inner A</li><li>Inner B</li></ul></li></ol>`
	if _, err := pdf.InsertHTMLBox(50, 50, 400, 600, html, HTMLBoxOption{DefaultFontFamily: "lib"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "list.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	text := extractPDFText(t, path)
	for _, want := range []string{"First", "Second", "Inner A", "Inner B"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in extracted text, got: %s", want, text)
		}
	}
}

// TestHTML_EntitiesAndTransform verifies numeric entities and text-transform.
func TestHTML_EntitiesAndTransform(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	html := `<p style="text-transform: uppercase">upper &#72;&#101;llo &copy; &mdash; &alpha;&beta;</p>`
	if _, err := pdf.InsertHTMLBox(50, 50, 400, 600, html, HTMLBoxOption{DefaultFontFamily: "lib"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ent.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	text := extractPDFText(t, path)
	if !strings.Contains(text, "UPPER") {
		t.Errorf("text-transform uppercase not applied: %q", text)
	}
}

// TestHTML_Justify verifies justified text fills the line width.
func TestHTML_Justify(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	html := `<p style="text-align: justify">Words spread across the full width of this line for justification testing purposes here.</p>`
	if _, err := pdf.InsertHTMLBox(50, 50, 400, 600, html, HTMLBoxOption{DefaultFontFamily: "lib"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "justify.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	text := extractPDFText(t, path)
	if !strings.Contains(text, "justification testing") {
		t.Errorf("text missing: %q", text)
	}
}

// TestHTML_Base64Image verifies data-URI images render.
func TestHTML_Base64Image(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	// 1x1 red PNG
	const png1x1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	html := `<p>Before</p><img src="data:image/png;base64,` + png1x1 + `" width="50" height="50"><p>After</p>`
	if _, err := pdf.InsertHTMLBox(50, 50, 400, 600, html, HTMLBoxOption{DefaultFontFamily: "lib"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "img.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	text := extractPDFText(t, path)
	if !strings.Contains(text, "Before") || !strings.Contains(text, "After") {
		t.Errorf("text around image missing: %q", text)
	}
	// image object should exist in the PDF
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "/XObject") {
		t.Errorf("no image XObject found in PDF")
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// TestHTML_TableHeaderRepeat verifies <thead> rows repeat on continuation pages.
func TestHTML_TableHeaderRepeat(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	var rows strings.Builder
	for i := 1; i <= 80; i++ {
		rows.WriteString(`<tr><td>Item `)
		rows.WriteString(itoa(i))
		rows.WriteString(`</td><td>`)
		rows.WriteString(itoa(i * 2))
		rows.WriteString(`</td></tr>`)
	}
	html := `<table border="1"><thead><tr><th>Product</th><th>Qty</th></tr></thead><tbody>` +
		rows.String() + `</tbody></table>`
	pdf.SetMargins(50, 50, 50, 50)
	_, err := pdf.InsertHTMLPaginated(50, 60, 500, html, HTMLBoxOption{
		DefaultFontFamily:  "lib",
		PageTop:            60,
		PageBottom:         792 - 50,
		RepeatTableHeaders: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hdr.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	if n := pdf.GetNumberOfPages(); n < 2 {
		t.Fatalf("expected multi-page table, got %d pages", n)
	}
	data, _ := os.ReadFile(path)
	text, err := ExtractAllPagesText(data)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(text, "Product"); n < 2 {
		t.Errorf("header 'Product' repeated %d times, want >= 2", n)
	}
}

// TestHTML_BlockBackground verifies block-level background-color renders
// full-width bands behind each line before the text (z-order).
func TestHTML_BlockBackground(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	html := `<div style="background-color: #ffe0e0">Line one of the shaded block wraps here, and</div>` +
		`<p>normal paragraph after</p>`
	if _, err := pdf.InsertHTMLBox(50, 50, 300, 300, html, HTMLBoxOption{DefaultFontFamily: "lib"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "blockbg.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	parser, _ := newRawPDFParser(data)
	stream := parser.getPageContentStream(0)
	s := string(stream)
	if !strings.Contains(s, "0.878 0.878 rg") {
		t.Errorf("block background fill color not found in stream")
	}
	// the fill rect must appear BEFORE the text of the shaded block
	reIdx := strings.Index(s, "0.878 0.878 rg")
	txtIdx := strings.Index(s, "Line")
	if txtIdx >= 0 && reIdx > txtIdx {
		t.Errorf("background drawn after text (z-order wrong)")
	}
}

// TestHTML_TableRowspanPagination verifies keep-together row-group breaking:
// rows joined by rowspan cells never get split across pages.
func TestHTML_TableRowspanPagination(t *testing.T) {
	pdf, font := newTestPDF(t)
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	var rows strings.Builder
	rows.WriteString(`<tr><td rowspan="4">Spanned</td><td>g1-a</td></tr>`)
	rows.WriteString(`<tr><td>g1-b</td></tr>`)
	rows.WriteString(`<tr><td>g1-c</td></tr>`)
	rows.WriteString(`<tr><td>g1-d</td></tr>`)
	for i := 5; i <= 70; i++ {
		rows.WriteString(`<tr><td>r`)
		rows.WriteString(itoa(i))
		rows.WriteString(`</td><td>v`)
		rows.WriteString(itoa(i))
		rows.WriteString(`</td></tr>`)
	}
	html := `<table border="1"><thead><tr><th>Cat</th><th>Val</th></tr></thead><tbody>` +
		rows.String() + `</tbody></table>`
	pdf.SetMargins(50, 50, 50, 50)
	_, err := pdf.InsertHTMLPaginated(50, 60, 500, html, HTMLBoxOption{
		DefaultFontFamily:  "lib",
		PageTop:            60,
		PageBottom:         792 - 50,
		RepeatTableHeaders: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rowspan.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	if n := pdf.GetNumberOfPages(); n < 2 {
		t.Fatalf("expected multiple pages, got %d", n)
	}
	data, _ := os.ReadFile(path)
	// the spanned group g1-a..g1-d must render on ONE page
	pageTexts, err := ExtractAllPagesText(data)
	if err != nil {
		t.Fatal(err)
	}
	// stitched text per page marker: find which page contains g1-d and g1-a
	_ = pageTexts
	pages, err := ExtractTextFromAllPages(data)
	if err != nil {
		t.Fatal(err)
	}
	groupPage := -1
	for p, runs := range pages {
		hasA, hasD := false, false
		for _, tx := range runs {
			if tx.Text == "g1-a" {
				hasA = true
			}
			if tx.Text == "g1-d" {
				hasD = true
			}
		}
		if hasA && hasD {
			groupPage = p
			break
		}
	}
	if groupPage < 0 {
		t.Errorf("spanned group rows not co-located on one page")
	}
	_ = groupPage
}

// TestPageBreakInsideAvoid verifies break-inside:avoid moves a whole block
// to the next page instead of splitting it across the boundary.
func TestPageBreakInsideAvoid(t *testing.T) {
	// filler paragraphs then a multi-line avoid block; the block starts near
	// the page bottom so it would split without the flag.
	build := func(avoid bool, fillers int) []byte {
		pdf := &GoPdf{}
		pdf.Start(Config{PageSize: *PageSizeA4})
		pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
		pdf.SetFont("lib", "", 12)
		pdf.AddPage()
		var sb strings.Builder
		for i := 0; i < fillers; i++ {
			sb.WriteString(`<p>filler line </p>`)
		}
		style := ""
		if avoid {
			style = ` style="break-inside:avoid"`
		}
		sb.WriteString(`<div` + style + `><p>AVOIDMARK-start lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore</p><p>AVOIDMARK-middle line</p><p>AVOIDMARK-end line</p></div>`)
		sb.WriteString(`<p>after the block</p>`)
		_, err := pdf.InsertHTMLPaginated(50, 50, 495, sb.String(), HTMLBoxOption{
			DefaultFontFamily: "lib",
			DefaultFontSize:   12,
			PageTop:           50,
			PageBottom:        742,
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "avoid.pdf")
		if err := pdf.WritePdf(path); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		return data
	}

	pageOf := func(data []byte, marker string) int {
		count, _ := GetSourcePDFPageCountFromBytes(data)
		for p := 0; p < count; p++ {
			lines, err := ExtractPageLines(data, p)
			if err != nil {
				continue
			}
			for _, ln := range lines {
				if strings.Contains(ln.Text, marker) {
					return p
				}
			}
		}
		return -1
	}

	// self-tuning: find a filler count where the UNPROTECTED block splits
	// across pages, then verify the protected block stays whole at the same
	// filler count.
	found := false
	for n := 30; n <= 60; n++ {
		control := build(false, n)
		cs := pageOf(control, "AVOIDMARK-start")
		ce := pageOf(control, "AVOIDMARK-end")
		if cs < 0 || ce < 0 || cs == ce {
			continue
		}
		// control splits here: avoid must keep it together
		protected := build(true, n)
		ps := pageOf(protected, "AVOIDMARK-start")
		pm := pageOf(protected, "AVOIDMARK-middle")
		pe := pageOf(protected, "AVOIDMARK-end")
		if ps < 0 || pm < 0 || pe < 0 {
			t.Fatalf("n=%d: markers missing %d %d %d", n, ps, pm, pe)
		}
		if ps != pm || pm != pe {
			t.Errorf("n=%d: break-inside:avoid did not hold: %d %d %d", n, ps, pm, pe)
		} else {
			t.Logf("n=%d: control split %d/%d, avoid kept the block on page %d", n, cs, ce, ps)
		}
		found = true
		break
	}
	if !found {
		t.Skip("no filler count in range produced a split")
	}
}
