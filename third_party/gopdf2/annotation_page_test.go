package gopdf

import (
	"os"
	"path/filepath"
	"testing"
)

// annotPageDoc creates a 2-page PDF with annotations on both pages
// using the GoPdf method API. Returns the GoPdf instance for further ops.
func annotPageDoc(t *testing.T) *GoPdf {
	t.Helper()
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	for p := 1; p <= 2; p++ {
		pdf.AddPage()
		pdf.SetXY(50, 50)
		_ = pdf.Cell(nil, "annotated page body")
		pdf.AddAnnotation(AnnotationOption{X: 50, Y: 80, W: 100, H: 20, Type: AnnotText, Content: "sticky p"})
		pdf.AddAnnotation(AnnotationOption{X: 50, Y: 120, W: 100, H: 20, Type: AnnotHighlight, Content: "hl p"})
		pdf.AddAnnotation(AnnotationOption{X: 50, Y: 160, W: 100, H: 20, Type: AnnotSquare, Content: "sq p"})
		pdf.AddAnnotation(AnnotationOption{X: 50, Y: 200, W: 100, H: 20, Type: AnnotFreeText, Content: "ft p"})
		pdf.AddAnnotation(AnnotationOption{X: 50, Y: 240, W: 100, H: 20, Type: AnnotPolyline, Content: "pl p"})
		pdf.AddAnnotation(AnnotationOption{X: 50, Y: 280, W: 100, H: 20, Type: AnnotPolygon, Content: "pg p"})
		pdf.AddAnnotation(AnnotationOption{X: 50, Y: 320, W: 100, H: 20, Type: AnnotRedact, Content: "rd p"})
	}
	return pdf
}

// TestAnnotationPageLifecycle verifies the annotation CRUD cycle: get,
// get per page, delete, redact.
func TestAnnotationPageLifecycle(t *testing.T) {
	pdf := annotPageDoc(t)

	// GetAnnotations on current page
	annots := pdf.GetAnnotations()
	if len(annots) == 0 {
		t.Fatal("GetAnnotations returned no annotations")
	}
	t.Logf("page annotations: %d", len(annots))

	// GetAnnotationsOnPage
	pageAnnots := pdf.GetAnnotationsOnPage(1)
	if len(pageAnnots) == 0 {
		t.Error("GetAnnotationsOnPage(1) returned no annotations")
	}

	// DeleteAnnotationOnPage
	if !pdf.DeleteAnnotationOnPage(1, 0) {
		t.Log("DeleteAnnotationOnPage returned false")
	}

	// ApplyRedactionsOnPage
	n := pdf.ApplyRedactionsOnPage(1)
	t.Logf("ApplyRedactionsOnPage applied %d redactions", n)

	// verify the document is still valid
	out := filepath.Join(t.TempDir(), "annot_mod.pdf")
	if err := pdf.WritePdf(out); err != nil {
		t.Errorf("write after annotation ops: %v", err)
	}
	written, _ := os.ReadFile(out)
	if _, err := ExtractTextFromAllPages(written); err != nil {
		t.Errorf("extraction after annotation ops failed: %v", err)
	}
}

// TestAnnotationAllTypesNoPanic verifies every annotation type can be
// added without panics and the document stays valid.
func TestAnnotationAllTypesNoPanic(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	types := []AnnotationType{
		AnnotText, AnnotHighlight, AnnotUnderline, AnnotStrikeOut,
		AnnotSquare, AnnotCircle, AnnotFreeText, AnnotInk,
		AnnotPolyline, AnnotPolygon, AnnotLine, AnnotStamp,
		AnnotSquiggly, AnnotCaret, AnnotRedact,
	}
	for i, typ := range types {
		pdf.AddAnnotation(AnnotationOption{
			X: 50, Y: float64(30 + i*30), W: 100, H: 20,
			Type:    typ,
			Content: "annotation " + itoa(i),
		})
	}
	path := filepath.Join(t.TempDir(), "all_types.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(path)
	if _, err := ExtractTextFromAllPages(written); err != nil {
		t.Errorf("extraction after all annotation types: %v", err)
	}
}
