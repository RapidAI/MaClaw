package gopdf

import (
	"math/rand"
	"testing"
)

// TestFuzz_MutatedPDFs applies deterministic random mutations to a valid
// multi-feature PDF and runs the full extraction gauntlet over every
// mutant: no panics allowed, any error is acceptable.
func TestFuzz_MutatedPDFs(t *testing.T) {
	base := buildFuzzBasePDF(t)
	rng := rand.New(rand.NewSource(42)) // deterministic

	mutate := func(data []byte, rng *rand.Rand) []byte {
		out := make([]byte, len(data))
		copy(out, data)
		switch rng.Intn(4) {
		case 0: // bit flip
			if n := len(out); n > 0 {
				out[rng.Intn(n)] ^= 1 << rng.Intn(8)
			}
		case 1: // random byte overwrite
			if n := len(out); n > 0 {
				out[rng.Intn(n)] = byte(rng.Intn(256))
			}
		case 2: // truncate
			n := rng.Intn(len(out))
			out = out[:n]
		case 3: // delete a slice
			if len(out) > 20 {
				a := rng.Intn(len(out) - 10)
				b := a + rng.Intn(len(out)-a)
				out = append(out[:a], out[b:]...)
			}
		}
		return out
	}

	runAll := func(data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic on mutated input: %v", r)
			}
		}()
		_, _ = GetSourcePDFPageCountFromBytes(data)
		_, _ = GetPDFInfo(data)
		_, _ = ExtractTOC(data)
		_, _ = ExtractTextFromPage(data, 0)
		_, _ = ExtractPageLines(data, 0)
		_, _ = ExtractPageParagraphs(data, 0)
		_, _ = ExtractTextFormatted(data, 0, FormatJSON)
		_, _ = ExtractTextInRect(data, 0, ClipRect{X: 0, Y: 0, W: 200, H: 200})
		_, _ = SearchTextWithOptions(data, "text", SearchOptions{Dehyphenate: true})
		_, _ = ExtractPageDrawings(data, 0)
		_, _ = ExtractTablesFromPage(data, 0)
		_, _ = ExtractImagesFromPage(data, 0)
		_, _ = ExtractAnnotationsFromPage(data, 0)
		_, _ = ExtractAllAnnotations(data)
		if doc, err := OpenPDFDocument(data); err == nil {
			_ = doc.HasForms()
			_ = doc.FormFields()
			_, _ = doc.Formatted(0, FormatJSON)
		}
		_, _ = RenderPageToImage(data, 0, RenderOption{DPI: 36})
	}

	for i := 0; i < 300; i++ {
		mutant := mutate(base, rng)
		runAll(mutant)
	}
}

// buildFuzzBasePDF creates a small but feature-rich base document for the
// mutation fuzz: text, table borders, outlines, metadata.
func buildFuzzBasePDF(t *testing.T) []byte {
	t.Helper()
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 11)
	pdf.SetInfo(PdfInfo{Title: "fuzz base", Author: "gopdf2"})
	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "fuzz base text for mutation")
	pdf.SetStrokeColor(0, 0, 0)
	pdf.Line(50, 100, 300, 100)
	pdf.RectFromUpperLeftWithStyle(50, 120, 200, 60, "S")
	pdf.AddOutline("root outline")
	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "second page text")
	path := t.TempDir() + "/fuzzbase.pdf"
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, err := readFileBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
