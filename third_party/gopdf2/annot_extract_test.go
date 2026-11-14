package gopdf

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAnnotationsRoundTrip writes annotations, saves, and reads them back
// through the extraction API (and the PDFDoc handle).
func TestAnnotationsRoundTrip(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddAnnotation(AnnotationOption{
		X: 50, Y: 60, W: 100, H: 20,
		Type:    AnnotText,
		Content: "sticky note body",
		Title:   "reviewer1",
		Color:   [3]uint8{255, 0, 0},
	})
	pdf.AddAnnotation(AnnotationOption{
		X: 50, Y: 120, W: 150, H: 16,
		Type:    AnnotHighlight,
		Content: "highlighted passage",
		Color:   [3]uint8{255, 255, 0},
	})
	pdf.AddAnnotation(AnnotationOption{
		X: 200, Y: 200, W: 80, H: 40,
		Type:  AnnotSquare,
		Color: [3]uint8{0, 128, 255},
	})
	path := filepath.Join(t.TempDir(), "annots.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	annots, err := ExtractAnnotationsFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(annots) != 3 {
		for i, a := range annots {
			t.Logf("annot %d: %q %q", i, a.Type, a.Contents)
		}
		t.Fatalf("got %d annotations, want 3", len(annots))
	}
	byType := map[string]ExtractedAnnotation{}
	for _, a := range annots {
		byType[a.Type] = a
	}
	txt, ok := byType["Text"]
	if !ok {
		t.Fatalf("Text annotation missing")
	}
	if txt.Contents != "sticky note body" || txt.Author != "reviewer1" {
		t.Errorf("text annot = %+v", txt)
	}
	if txt.ColorR != 255 || txt.ColorG != 0 {
		t.Errorf("text color = %v", []uint8{txt.ColorR, txt.ColorG, txt.ColorB})
	}
	// rect: written top-down (50, 60, 100x20) -> read back the same
	if txt.X < 45 || txt.X > 55 || txt.Y < 55 || txt.Y > 65 || txt.W < 95 || txt.H < 15 {
		t.Errorf("text rect = x%.0f y%.0f w%.0f h%.0f", txt.X, txt.Y, txt.W, txt.H)
	}
	hl, ok := byType["Highlight"]
	if !ok || hl.Contents != "highlighted passage" {
		t.Errorf("highlight annot = %+v", hl)
	}
	sq, ok := byType["Square"]
	if !ok || sq.ColorB != 255 {
		t.Errorf("square annot = %+v", sq)
	}

	// PDFDoc handle
	doc, err := OpenPDFDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	docAnnots, err := doc.Annotations(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(docAnnots) != 3 {
		t.Errorf("doc annotations = %d", len(docAnnots))
	}
}
