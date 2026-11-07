package gopdf

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// BenchmarkCreateTextPDF measures basic PDF creation with text.
func BenchmarkCreateTextPDF(b *testing.B) {
	font := "test/res/LiberationSerif-Regular.ttf"
	if _, err := os.Stat(font); err != nil {
		b.Skip("font not available")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pdf := &GoPdf{}
		pdf.Start(Config{PageSize: *PageSizeA4})
		pdf.AddPage()
		if err := pdf.AddTTFFont("lib", font); err != nil {
			b.Fatal(err)
		}
		if err := pdf.SetFont("lib", "", 12); err != nil {
			b.Fatal(err)
		}
		for row := 0; row < 20; row++ {
			pdf.SetXY(50, 50+float64(row)*20)
			_ = pdf.Cell(nil, fmt.Sprintf("Line %d: benchmark text for performance tracking.", row))
		}
		if err := pdf.WritePdf(""); err != nil {
			// WritePdf with empty path writes to bytes — just discard
		}
	}
}

// BenchmarkHTMLRender measures HTML-to-PDF rendering.
func BenchmarkHTMLRender(b *testing.B) {
	font := "test/res/LiberationSerif-Regular.ttf"
	if _, err := os.Stat(font); err != nil {
		b.Skip("font not available")
	}
	html := `<h1>Benchmark</h1><p>Body text for rendering measurement.</p>` +
		strings.Repeat(`<p>Paragraph ${i} content.</p>`, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pdf := &GoPdf{}
		pdf.Start(Config{PageSize: *PageSizeA4})
		pdf.AddPage()
		if err := pdf.AddTTFFont("lib", font); err != nil {
			b.Fatal(err)
		}
		_, _ = pdf.InsertHTMLBox(50, 50, 495, 700, html, HTMLBoxOption{
			DefaultFontFamily: "lib",
			DefaultFontSize:   11,
		})
	}
}

// BenchmarkTableRender measures table rendering.
func BenchmarkTableRender(b *testing.B) {
	font := "test/res/LiberationSerif-Regular.ttf"
	if _, err := os.Stat(font); err != nil {
		b.Skip("font not available")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pdf := &GoPdf{}
		pdf.Start(Config{PageSize: *PageSizeA4})
		pdf.AddPage()
		if err := pdf.AddTTFFont("lib", font); err != nil {
			b.Fatal(err)
		}
		if err := pdf.SetFont("lib", "", 10); err != nil {
			b.Fatal(err)
		}
		table := pdf.NewTableLayout(50, 60, 20, 0)
		table.AddColumn("A", 100, "left")
		table.AddColumn("B", 200, "left")
		table.SetAutoRowHeight(true)
		for row := 0; row < 20; row++ {
			table.AddRow([]string{fmt.Sprintf("k%d", row), fmt.Sprintf("val%d", row)})
		}
		if err := table.DrawTable(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFormulaRender measures formula rendering.
func BenchmarkFormulaRender(b *testing.B) {
	font := "test/res/LiberationSerif-Regular.ttf"
	if _, err := os.Stat(font); err != nil {
		b.Skip("font not available")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pdf := &GoPdf{}
		pdf.Start(Config{PageSize: *PageSizeA4})
		pdf.AddPage()
		if err := pdf.AddTTFFont("lib", font); err != nil {
			b.Fatal(err)
		}
		if err := pdf.SetFont("lib", "", 12); err != nil {
			b.Fatal(err)
		}
		_, _ = pdf.DrawFormula(50, 100, `\int_0^\infty e^{-x^2} dx = \frac{\sqrt{\pi}}{2}`, 14, nil)
	}
}
