package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Claimed-vs-actual probes for the remaining wrapper features.

func TestDbgComparePDFClaim(t *testing.T) {
	// build doc A
	pdfA := &GoPdf{}
	pdfA.Start(Config{PageSize: *PageSizeA4})
	pdfA.AddPage()
	if err := pdfA.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdfA.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdfA.SetXY(50, 50)
	_ = pdfA.Cell(nil, "Version one content")
	pathA := filepath.Join(t.TempDir(), "a.pdf")
	if err := pdfA.WritePdf(pathA); err != nil {
		t.Fatal(err)
	}
	dataA, _ := os.ReadFile(pathA)

	// doc B: different text
	pdfB := &GoPdf{}
	pdfB.Start(Config{PageSize: *PageSizeA4})
	pdfB.AddPage()
	if err := pdfB.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdfB.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdfB.SetXY(50, 50)
	_ = pdfB.Cell(nil, "Version TWO content changed")
	pathB := filepath.Join(t.TempDir(), "b.pdf")
	if err := pdfB.WritePdf(pathB); err != nil {
		t.Fatal(err)
	}
	dataB, _ := os.ReadFile(pathB)

	res, err := ComparePDF(dataA, dataB, nil)
	if err != nil {
		t.Fatalf("ComparePDF: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	t.Logf("diff result: identical=%v diffs=%d summary=%q", res.IsIdentical, len(res.Differences), res.Summary)
	if res.IsIdentical || len(res.Differences) == 0 {
		t.Errorf("ComparePDF found no differences between different documents")
	}
}

func TestDbgPDFAValidationClaim(t *testing.T) {
	// a plain PDF (no XMP, standard fonts embedded) should NOT pass PDF/A-1b
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "not an archival pdf")
	path := filepath.Join(t.TempDir(), "plain.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	res := ValidatePDFA(data, PDFA1b)
	t.Logf("valid=%v errors=%d warnings=%d", res.IsValid, len(res.Errors), len(res.Warnings))
	if res.IsValid {
		t.Logf("plain PDF passed PDF/A-1b — validator is permissive; errors: %v", res.Errors)
	}
}

func TestIncrementalSaveRoundTrip(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "base content")
	base := filepath.Join(t.TempDir(), "base.pdf")
	if err := pdf.WritePdf(base); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(base)

	// modify: overlay text onto the existing document
	mod := &GoPdf{}
	if err := mod.OpenPDFFromBytes(orig, nil); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := mod.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := mod.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	mod.SetXY(50, 100)
	_ = mod.Cell(nil, "appended line")
	out := filepath.Join(t.TempDir(), "mod.pdf")
	if err := mod.WritePdf(out); err != nil {
		t.Fatal(err)
	}
	modified, _ := os.ReadFile(out)
	saved, err := mod.IncrementalSave(orig, []int{})
	if err != nil {
		t.Fatalf("IncrementalSave: %v", err)
	}
	// /Prev must chain back to the original xref
	if !strings.Contains(string(saved), "/Prev") {
		t.Errorf("incremental output missing /Prev chain")
	}
	// the appended overlay must be extractable from the incremental output
	texts, err := ExtractTextFromAllPages(saved)
	if err != nil {
		t.Fatalf("incremental output unparseable: %v", err)
	}
	hasAppended := false
	for _, runs := range texts {
		for _, tx := range runs {
			if strings.Contains(tx.Text, "appended line") {
				hasAppended = true
			}
		}
	}
	if !hasAppended {
		t.Errorf("incremental output incomplete: appended overlay missing")
	}
	_ = modified
}
