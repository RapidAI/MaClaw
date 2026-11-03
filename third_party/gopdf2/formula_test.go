package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFormula_RoundTrip renders formulas and verifies text extraction.
func TestFormula_RoundTrip(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}

	formulas := []string{
		`E = mc^2`,
		`a^2 + b^2 = c^2`,
		`x_1 + x_2 = y`,
		`\frac{a}{b} = \frac{c}{d}`,
		`\sqrt{x^2 + y^2}`,
		`\alpha + \beta = \gamma`,
		`\int_0^\infty e^{-x} dx = 1`,
		`\sum_{i=1}^{n} i = \frac{n(n+1)}{2}`,
		`A \times B \leq C \neq D`,
	}
	y := 60.0
	for _, f := range formulas {
		h, err := pdf.DrawFormula(50, y, f, 12, nil)
		if err != nil {
			t.Fatalf("formula %q: %v", f, err)
		}
		if h <= 0 {
			t.Errorf("formula %q: non-positive height %f", f, h)
		}
		y += h + 16
	}

	path := filepath.Join(t.TempDir(), "formula.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text, err := ExtractAllPagesText(data)
	if err != nil {
		t.Fatal(err)
	}
	// core glyphs must survive the round trip (extraction may insert
	// cosmetic spaces between separately-positioned glyphs)
	if !strings.Contains(text, "mc") {
		t.Errorf("basic formula text missing: %q", text)
	}
	if !strings.Contains(text, "√") {
		t.Errorf("sqrt glyph missing: %q", text)
	}
	if !strings.Contains(text, "α") {
		t.Errorf("greek glyph missing: %q", text)
	}
	if !strings.Contains(text, "∫") {
		t.Errorf("integral glyph missing: %q", text)
	}
	if !strings.Contains(text, "∑") {
		t.Errorf("sum glyph missing: %q", text)
	}
}

// TestFormula_MeasureLayout checks measurement sanity.
func TestFormula_MeasureLayout(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	w, h, err := pdf.MeasureFormula(`\frac{\sqrt{a+b}}{2}`, 12)
	if err != nil {
		t.Fatal(err)
	}
	if w <= 0 || h <= 0 {
		t.Errorf("bad metrics: w=%f h=%f", w, h)
	}
	if h < 12 {
		t.Errorf("fraction should be taller than plain text: h=%f", h)
	}
	w2, _, _ := pdf.MeasureFormula(`abc`, 12)
	if w2 <= 0 {
		t.Errorf("plain text width invalid")
	}
}

// TestFormulaOptions verifies FormulaOption color and font family apply.
func TestFormulaOptions(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	_, err := pdf.DrawFormula(50, 100, `a^2 + b^2 = c^2`, 12, &FormulaOption{
		Color: &[3]uint8{200, 0, 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fcolor.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	parser, _ := newRawPDFParser(data)
	stream := parser.getPageContentStream(0)
	// the red fill color operator must appear in the stream
	if !strings.Contains(string(stream), "0.784 0.000 0.000") {
		t.Errorf("formula color not applied; stream: %.400s", string(stream))
	}
}
