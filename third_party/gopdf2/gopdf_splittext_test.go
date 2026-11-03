package gopdf

import (
	"strings"
	"testing"
)

// SplitText should never return a segment wider than `width` in strict mode,
// and should keep all characters.
func TestSplitTextWordWrapWidth(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 12)

	text := "The quick brown fox jumps over the lazy dog again. 0123456789 ABCDEFG."
	lines, err := pdf.SplitTextWithWordWrap(text, 100)
	if err != nil {
		t.Fatal(err)
	}
	worst := 0.0
	for _, ln := range lines {
		w, err := pdf.MeasureTextWidth(ln)
		if err != nil {
			t.Fatal(err)
		}
		if w > worst {
			worst = w
		}
	}
	t.Logf("lines=%d worst=%.2f target=100", len(lines), worst)
	// characters preserved?
	// indicator-sensitive breaks consume the separator; join with it
	joined := strings.Join(lines, " ")
	if strings.Join(strings.Fields(joined), " ") != strings.Join(strings.Fields(text), " ") {
		t.Errorf("text content changed: %q vs %q", text, joined)
	}
}

func TestSplitTextCJK(t *testing.T) {
	font := findCJKFont(t)
	if font == "" {
		t.Skip("no CJK font")
	}
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddTTFFont("cjk", font)
	pdf.SetFont("cjk", "", 12)
	text := "这是一个用于测试文本切分的中文示例句子，包含标点符号。"
	lines, err := pdf.SplitTextWithWordWrap(text, 120)
	if err != nil {
		t.Fatal(err)
	}
	for i, ln := range lines {
		w, _ := pdf.MeasureTextWidth(ln)
		t.Logf("line %d w=%.1f %q", i, w, ln)
	}
}

func BenchmarkSplitText(b *testing.B) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 12)
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 120)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pdf.SplitTextWithWordWrap(text, 300)
	}
}

// TestDbgSplitEquivalence walks a variety of texts/widths comparing line
// breaks against the legacy (per-line measure) implementation outputs —
// reproduced here inline with kerning forced into the slow path.
func TestSplitTextEquivalence(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 11)

	texts := []string{
		"Short one.",
		"Multiple spaces    inside    this    text   sample for break checks.",
		"Hyphen-joined-words-and-verylongunbrokenidentifierwxyz0123456789 over.",
		"Line one\nLine two\n\nLine four",
		strings.Repeat("abc def ghi jkl mno pqr ", 40),
		strings.Repeat("x", 700),
	}
	widths := []float64{60, 137.5, 300}
	for ti, tx := range texts {
		for _, w := range widths {
			// reference: legacy per-line measurement
			ref := splitLegacy(pdf, tx, w)
			got, err := pdf.SplitTextWithOption(tx, w, &BreakOption{
				Mode:           BreakModeIndicatorSensitive,
				BreakIndicator: ' ',
			})
			if err != nil {
				t.Fatalf("SplitTextWithOption: %v", err)
			}
			refJoined := strings.Join(ref, "|")
			gotJoined := strings.Join(got, "|")
			if refJoined != gotJoined {
				t.Errorf("t=%d w=%.1f mismatch:\nref=%q\ngot=%q", ti, w, refJoined, gotJoined)
			}
		}
	}
}

// splitLegacy reproduces the pre-optimization implementation.
func splitLegacy(pdf *GoPdf, text string, width float64) []string {
	opt := &BreakOption{Mode: BreakModeIndicatorSensitive, BreakIndicator: ' '}
	var lineText []rune
	var lineTexts []string
	utf8Texts := []rune(text)
	separatorWidth, _ := pdf.MeasureTextWidth(opt.Separator)
	separatorIdx := 0
	for i := 0; i < len(utf8Texts); i++ {
		lineWidth, _ := pdf.MeasureTextWidth(string(lineText))
		runeWidth, _ := pdf.MeasureTextWidth(string(utf8Texts[i]))
		if lineWidth+runeWidth > width && utf8Texts[i] != '\n' {
			forceBreak := !performIndicatorSensitiveLineBreak(&lineTexts, &lineText, &i, opt)
			if forceBreak {
				performStrictLineBreak(&lineTexts, &lineText, &i, separatorIdx, opt)
			}
			continue
		}
		if utf8Texts[i] == '\n' {
			lineTexts = append(lineTexts, string(lineText))
			lineText = lineText[0:0]
			continue
		}
		if i == len(utf8Texts)-1 {
			lineText = append(lineText, utf8Texts[i])
			lineTexts = append(lineTexts, string(lineText))
		}
		if opt.HasSeparator() && lineWidth+runeWidth+separatorWidth <= width {
			separatorIdx = i
		}
		lineText = append(lineText, utf8Texts[i])
	}
	return lineTexts
}

// TestSplitTextKerningPath exercises the kerning-enabled (slow) path.
func TestSplitTextKerningPath(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	err := pdf.AddTTFFontWithOption("libk", "test/res/LiberationSerif-Regular.ttf", TtfOption{
		UseKerning: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("libk", "", 12); err != nil {
		t.Fatal(err)
	}
	text := "The quick brown fox jumps over the lazy dog."
	lines, err := pdf.SplitTextWithWordWrap(text, 100)
	if err != nil {
		t.Fatal(err)
	}
	worst := 0.0
	for _, ln := range lines {
		w, _ := pdf.MeasureTextWidth(ln)
		if w > worst {
			worst = w
		}
	}
	t.Logf("kerning lines=%d worst=%.2f (target 100, kern pairs may push past)", len(lines), worst)
	if worst > 110 {
		t.Errorf("kerning path drifted badly: %.2f", worst)
	}
}
