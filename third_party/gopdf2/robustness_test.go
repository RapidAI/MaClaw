package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMalformedPDFData feeds various malformed inputs to the extraction,
// rendering and page count APIs to verify no panics occur.
func TestMalformedPDFData(t *testing.T) {
	malformed := [][]byte{
		{},                            // empty
		[]byte("%PDF"),                // header only
		[]byte("%PDF-1.4\n"),          // header + newline
		[]byte("%PDF-1.4\n1 0 obj\n"), // truncated object
		[]byte("%PDF-1.4\ntrailer\n<<>>\nstartxref\n0\n%%EOF\n"), // no objects
		[]byte("not a pdf at all"),                               // garbage
		[]byte("%PDF-1.4\n1 0 obj\n<<>>\nstream\n"),              // truncated stream
		make([]byte, 100),                                        // null bytes
		[]byte("%PDF-1.4\n1 0 obj\n<< /Length -1 >>\nstream\n"),  // negative length
		[]byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n3 0 obj\n<< /Type /Page /Parent 2 0 R /Contents 999 0 R >>\nendobj\n"), // missing content ref
	}
	for i, data := range malformed {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("malformed[%d]: panic: %v", i, r)
				}
			}()
			// extraction
			_, _ = ExtractTextFromAllPages(data)
			_, _ = ExtractPageLines(data, 0)
			// page count
			_, _ = GetSourcePDFPageCountFromBytes(data)
			// rendering
			_, _ = RenderPageToImage(data, 0, RenderOption{DPI: 72})
			// comparison
			_, _ = ComparePDF(data, data, nil)
			// validation
			_ = ValidatePDFA(data, PDFA1b)
		}()
	}
}

// TestLargePDFStress creates a large multi-page PDF and verifies extraction
// and rendering work correctly under memory pressure.
func TestLargePDFStress(t *testing.T) {
	font := "test/res/LiberationSerif-Regular.ttf"
	if _, err := os.Stat(font); err != nil {
		t.Skip("font not available")
	}
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", font); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 11); err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("Stress test content for memory and performance validation. ", 3)
	for p := 0; p < 100; p++ {
		pdf.AddPage()
		for row := 0; row < 30; row++ {
			pdf.SetXY(50, 50+float64(row)*18)
			_ = pdf.Cell(nil, line)
		}
	}
	path := filepath.Join(t.TempDir(), "stress.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("large PDF: %d bytes, 100 pages", len(data))

	// extraction should work
	texts, err := ExtractTextFromAllPages(data)
	if err != nil {
		t.Fatalf("extraction on 100-page PDF: %v", err)
	}
	if len(texts) < 90 {
		t.Errorf("expected >= 90 pages with text, got %d", len(texts))
	}
	// rendering should work
	if _, err := RenderPageToImage(data, 50, RenderOption{DPI: 72}); err != nil {
		t.Errorf("render page 50: %v", err)
	}
}
