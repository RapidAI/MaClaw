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
