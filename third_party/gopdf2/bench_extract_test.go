package gopdf

import (
	"os"
	"testing"
)

func BenchmarkExtractArxiv(b *testing.B) {
	data, err := os.ReadFile("test/corpus/arxiv_1706.03762.pdf")
	if err != nil {
		b.Skip("corpus not available")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractTextFromAllPages(data)
	}
}

func BenchmarkExtractFormulae(b *testing.B) {
	data, err := os.ReadFile("test/corpus/formulae_tables.pdf")
	if err != nil {
		b.Skip("corpus not available")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractTextFromAllPages(data)
	}
}

func BenchmarkExtractLinesArxivP0(b *testing.B) {
	data, err := os.ReadFile("test/corpus/arxiv_1706.03762.pdf")
	if err != nil {
		b.Skip("corpus not available")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractPageLines(data, 0)
	}
}

func BenchmarkHTMLRenderTable(b *testing.B) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	_ = pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	var rows string
	for i := 0; i < 100; i++ {
		rows += `<tr><td>Cell A</td><td>Cell B</td><td>Cell C</td></tr>`
	}
	html := `<table border="1">` + rows + `</table>`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pdf.InsertHTMLBox(50, 50, 495, 700, html, HTMLBoxOption{DefaultFontFamily: "lib"})
	}
}
