package gopdf

import (
	"strings"
	"testing"
)

// BenchmarkExtractName benchmarks the optimized extractName function
// which now uses string search instead of regex compilation per call.
func BenchmarkExtractName(b *testing.B) {
	dict := `<< /Type /Font /Subtype /TrueType /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractName(dict, "/BaseFont")
	}
}

// BenchmarkExtractIntValue benchmarks the optimized extractIntValue function.
func BenchmarkExtractIntValue(b *testing.B) {
	dict := `<< /Type /XObject /Subtype /Image /Width 1920 /Height 1080 /BitsPerComponent 8 >>`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractIntValue(dict, "/Width")
		extractIntValue(dict, "/Height")
	}
}

// BenchmarkTokenize benchmarks the content stream tokenizer.
func BenchmarkTokenize(b *testing.B) {
	// Simulate a typical content stream.
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("BT /F1 12 Tf 100 700 Td (Hello World) Tj ET\n")
		sb.WriteString("q 1 0 0 1 50 50 cm /Im1 Do Q\n")
		sb.WriteString("0.5 0.5 0.5 rg 100 100 200 50 re f\n")
	}
	data := []byte(sb.String())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tokenize(data)
	}
}

// BenchmarkExtractHexPairs benchmarks hex pair extraction with pre-compiled regex.
func BenchmarkExtractHexPairs(b *testing.B) {
	line := "<0041> <0042> <0043> <0044>"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractHexPairs(line)
	}
}