package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCellPositionAccuracy generates text at known positions and verifies
// that the extracted positions match (within tolerance).
func TestCellPositionAccuracy(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}

	// generate text at known positions
	type pos struct {
		x, y     float64
		fontSize int
		text     string
	}
	positions := []pos{
		{50, 100, 12, "Alpha"},
		{200, 200, 12, "Beta"},
		{350, 300, 12, "Gamma"},
	}
	for _, p := range positions {
		pdf.SetXY(p.x, p.y)
		_ = pdf.Cell(nil, p.text)
	}

	path := filepath.Join(t.TempDir(), "acc.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	texts, err := ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	// match extracted runs to expected positions.
	// ExtractedText X/Y are in top-down page coordinates; Y is the text
	// baseline, which for a Cell sits between the requested top (p.y) and
	// one font size below it.
	for _, p := range positions {
		found := false
		for _, tx := range texts {
			if tx.Text == p.text {
				dx := tx.X - p.x
				if dx < 0 {
					dx = -dx
				}
				if dx < 2 && tx.Y >= p.y-1 && tx.Y <= p.y+float64(int(p.fontSize))+1 {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("text %q at (%.0f,%.0f) not found at expected position", p.text, p.x, p.y)
		}
	}
}

// TestExtractionRoundTrip generates text, extracts it, and verifies
// the full round-trip preserves content.
func TestExtractionRoundTrip(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	sentences := []string{
		"The quick brown fox jumps over the lazy dog.",
		"Pack my box with five dozen liquor jugs.",
		"How vexingly quick daft zebras jump!",
	}
	y := 60.0
	for _, s := range sentences {
		pdf.SetXY(50, y)
		_ = pdf.Cell(nil, s)
		y += 30
	}
	path := filepath.Join(t.TempDir(), "rt.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	lines, err := ExtractPageLines(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	extracted := ""
	for _, ln := range lines {
		extracted += ln.Text + "\n"
	}
	for _, s := range sentences {
		if !strings.Contains(extracted, s) {
			t.Errorf("sentence not preserved in extraction: %q", s)
		}
	}
}

// TestTwoColumnReadingOrder generates a two-column page with a full-width
// title and verifies lines are emitted in reading order (title, then the
// left column top-down, then the right column) instead of interleaved by Y.
func TestTwoColumnReadingOrder(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: Rect{W: 595, H: 842}})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}

	// Full-width title: long enough to cross the gutter between the columns.
	pdf.SetXY(60, 40)
	_ = pdf.Cell(nil, "A Long Title Spanning the Full Width of the Two Text Columns Below")

	// Left column: lines ending well before the right column starts.
	leftLines := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		txt := "Left " + itoa(i) + " lorem ipsum dolor sit amet"
		w, err := pdf.MeasureTextWidth(txt)
		if err != nil {
			t.Fatal(err)
		}
		if 60+w >= 340 {
			t.Fatalf("left line too wide: %.0f", 60+w)
		}
		pdf.SetXY(60, float64(100+i*20))
		_ = pdf.Cell(nil, txt)
		leftLines = append(leftLines, txt)
	}
	// Right column: independent baselines, like real two-column typesetting.
	rightLines := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		txt := "Right " + itoa(i) + " lorem ipsum dolor sit amet"
		w, err := pdf.MeasureTextWidth(txt)
		if err != nil {
			t.Fatal(err)
		}
		if 380+w >= 560 {
			t.Fatalf("right line too wide: %.0f", 380+w)
		}
		pdf.SetXY(380, float64(105+i*20))
		_ = pdf.Cell(nil, txt)
		rightLines = append(rightLines, txt)
	}

	path := filepath.Join(t.TempDir(), "twocol.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	lines, err := ExtractPageLines(data, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Expected order: title, Left 0..3, Right 0..3.
	want := []string{"A Long Title Spanning the Full Width of the Two Text Columns Below"}
	want = append(want, leftLines...)
	want = append(want, rightLines...)

	if len(lines) != len(want) {
		var got []string
		for _, ln := range lines {
			got = append(got, ln.Text)
		}
		t.Fatalf("got %d lines %v, want %d", len(lines), got, len(want))
	}
	for i, ln := range lines {
		if ln.Text != want[i] {
			t.Errorf("line %d = %q, want %q", i, ln.Text, want[i])
		}
	}
}

// TestFormatWordsRealPositions verifies the word-level extraction carries
// true per-word positions: words within one run advance in X instead of all
// sharing the run origin.
func TestFormatWordsRealPositions(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 100)
	_ = pdf.Cell(nil, "Hello wide world examples")

	path := filepath.Join(t.TempDir(), "words.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	v, err := ExtractTextFormatted(data, 0, FormatWords)
	if err != nil {
		t.Fatal(err)
	}
	words, ok := v.([]TextWord)
	if !ok || len(words) < 4 {
		t.Fatalf("expected >=4 TextWords, got %T %v", v, v)
	}
	if words[0].Text != "Hello" {
		t.Fatalf("first word = %q", words[0].Text)
	}
	// Each subsequent word must start strictly to the right of the previous
	// word's end; widths must be plausible (0 < w < 8 * fontsize).
	for i := 1; i < len(words); i++ {
		prev, cur := words[i-1], words[i]
		if cur.X <= prev.X+prev.Width {
			t.Errorf("word %d (%q) X=%.2f does not advance past word %d (%q) end %.2f",
				i, cur.Text, cur.X, i-1, prev.Text, prev.X+prev.Width)
		}
		if cur.Width <= 0 || cur.Width > 8*cur.FontSize {
			t.Errorf("word %d (%q) implausible width %.2f", i, cur.Text, cur.Width)
		}
	}
}

// TestParagraphReconstruction generates a page with wrapped paragraphs
// (hyphenated splits, indents, paragraph spacing) and verifies the
// paragraph API reconstructs the original text.
func TestParagraphReconstruction(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 11); err != nil {
		t.Fatal(err)
	}

	// Paragraph 1: two wrapped lines ending with a hyphenated split.
	p1a := "Recurrent neural networks are difficult to train because their sequential na-"
	p1b := "ture precludes parallelization within training examples."
	// Paragraph 2: first-line indent + terminal punctuation on previous line.
	p2a := "Attention mechanisms read entire sequences at once."
	p2b := "They model dependencies without regard to distance."

	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, p1a)
	pdf.SetXY(50, 76)
	_ = pdf.Cell(nil, p1b)
	pdf.SetXY(66, 100) // indented first line, extra spacing above
	_ = pdf.Cell(nil, p2a)
	pdf.SetXY(50, 116)
	_ = pdf.Cell(nil, p2b)

	path := filepath.Join(t.TempDir(), "para.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	paras, err := ExtractPageParagraphs(data, 0)
	if err != nil {
		t.Fatal(err)
	}

	// p2a ends with terminal punctuation, so it deliberately starts its own
	// paragraph: expect [p1 wrapped], [p2a], [p2b].
	if len(paras) != 3 {
		for i, p := range paras {
			t.Logf("para %d: %q", i, p.Text)
		}
		t.Fatalf("got %d paragraphs, want 3", len(paras))
	}
	// the hyphenated split "na-/ture" joins into "nature"
	wantP1 := p1a[:len(p1a)-1] + p1b
	if paras[0].Text != wantP1 {
		t.Errorf("paragraph 1 = %q, want %q", paras[0].Text, wantP1)
	}
	if paras[1].Text != p2a {
		t.Errorf("paragraph 2 = %q", paras[1].Text)
	}
	if paras[2].Text != p2b {
		t.Errorf("paragraph 3 = %q", paras[2].Text)
	}
}

// TestParagraphHyphenJoin verifies a hyphenated line split joins into one
// word across the paragraph boundary.
func TestParagraphHyphenJoin(t *testing.T) {
	lines := []TextLine{
		{Y: 100, Text: "paralleliza-", Words: []TextWord{{X: 50, Y: 100, Width: 60, Height: 11, Text: "paralleliza-"}}},
		{Y: 114, Text: "tion improves throughput.", Words: []TextWord{{X: 50, Y: 114, Width: 120, Height: 11, Text: "tion improves throughput."}}},
	}
	paras := ParagraphsFromLines(lines)
	if len(paras) != 1 {
		t.Fatalf("got %d paragraphs, want 1", len(paras))
	}
	want := "parallelization improves throughput."
	if paras[0].Text != want {
		t.Errorf("text = %q, want %q", paras[0].Text, want)
	}
}

// TestRotateExtraction verifies /Rotate 90: extraction coordinates map to
// the visual (rotated) page space and the rendered canvas swaps dimensions.
func TestRotateExtraction(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4}) // 595 x 842
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 100)
	_ = pdf.Cell(nil, "Rotated marker")
	if err := pdf.SetPageRotation(1, 90); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "rot90.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	texts, err := ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	// /Rotate 90: (x, y) -> (h - y, x); unrotated (50, 100) -> (742, 50)
	found := false
	for _, tx := range texts {
		if strings.Contains(tx.Text, "Rotated") {
			found = true
			// the cell baseline sits 8.32pt below the requested top,
			// so visual X = h - (100 + 8.32)
			wantX := 842.0 - 108.32
			if dx := tx.X - wantX; dx < -2 || dx > 2 {
				t.Errorf("X = %.1f, want ~%.1f", tx.X, wantX)
			}
			if dy := tx.Y - 50; dy < -14 || dy > 14 {
				t.Errorf("Y = %.1f, want ~50 (baseline range)", tx.Y)
			}
		}
	}
	if !found {
		t.Errorf("rotated marker text not extracted")
	}

	// rendered canvas swaps to landscape
	img, err := RenderPageToImage(data, 0, RenderOption{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() <= b.Dy() {
		t.Errorf("rendered %dx%d, want landscape (842x612)", b.Dx(), b.Dy())
	}
}

// TestRotateInheritedFromPagesTree verifies /Rotate on a /Pages node is
// inherited by child pages (hand-crafted PDF).
func TestRotateInheritedFromPagesTree(t *testing.T) {
	// minimal PDF: one page inheriting /Rotate 180 from the Pages node
	pdfBytes := []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 595 842] /Rotate 180 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>
endobj
4 0 obj
<< /Length 44 >>
stream
BT /F1 12 Tf 50 770 Td (Inherited) Tj ET
endstream
endobj
5 0 obj
<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>
endobj
trailer
<< /Root 1 0 R >>
`)
	// The tolerant scanner recovers objects without a valid xref, so hand
	// the raw object stream to ExtractTextFromPage.
	texts, err := ExtractTextFromPage(pdfBytes, 0)
	if err != nil {
		t.Fatalf("extraction: %v", err)
	}
	// /Rotate 180: (x, y) -> (w - x, h - y); (50, 770 y-down? no —
	// content Td 50 770 is y-up: y-down = 842-770 = 72)
	// -> visual (595-50, 842-72) = (545, 770)
	found := false
	for _, tx := range texts {
		if strings.Contains(tx.Text, "Inherited") {
			found = true
			if dx := tx.X - 545; dx < -2 || dx > 2 {
				t.Errorf("X = %.1f, want ~545", tx.X)
			}
			if dy := tx.Y - 770; dy < -14 || dy > 14 {
				t.Errorf("Y = %.1f, want ~770", tx.Y)
			}
		}
	}
	if !found {
		t.Errorf("inherited-rotation text not extracted")
	}
}

// TestThreeColumnReadingOrder verifies recursive gutter detection orders
// three-column pages column by column.
func TestThreeColumnReadingOrder(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: Rect{W: 842, H: 595}}) // landscape A4: fits 3 columns
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 10); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(40, 40)
	_ = pdf.Cell(nil, "A Wide Title Spanning All Three Columns Of The Page")

	colXs := []float64{40, 310, 580}
	var want []string
	for ci, cx := range colXs {
		tag := []string{"C1", "C2", "C3"}[ci]
		for i := 0; i < 4; i++ {
			txt := tag + " line " + itoa(i) + " lorem ipsum dolor"
			w, err := pdf.MeasureTextWidth(txt)
			if err != nil {
				t.Fatal(err)
			}
			if cx+w >= cx+250 && ci < 2 {
				t.Fatalf("column %d too wide: %.0f", ci, cx+w)
			}
			pdf.SetXY(cx, float64(100+i*18))
			_ = pdf.Cell(nil, txt)
			want = append(want, txt)
		}
	}

	path := filepath.Join(t.TempDir(), "threecol.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	lines, err := ExtractPageLines(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ln := range lines {
		got = append(got, ln.Text)
	}
	// expected: title first, then C1 0..3, C2 0..3, C3 0..3
	if len(got) != len(want)+1 {
		t.Fatalf("got %d lines, want %d", len(got), len(want)+1)
	}
	if !strings.Contains(got[0], "A Wide Title") {
		t.Errorf("line 0 = %q, want the spanning title", got[0])
	}
	for i, w := range want {
		if got[i+1] != w {
			t.Errorf("line %d = %q, want %q", i+1, got[i+1], w)
		}
	}
}
