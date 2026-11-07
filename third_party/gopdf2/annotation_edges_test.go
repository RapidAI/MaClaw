package gopdf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnnotationEdgeRects(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("lib", "", 12)
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "annotated text")

	cases := []struct {
		name       string
		x, y, w, h float64
	}{
		{"normal", 50, 50, 100, 20},
		{"zero", 50, 50, 0, 0},
		{"negative", 50, 50, -50, -10},
		{"huge", 50, 50, 10000, 10000},
		{"off-page", -200, -200, 50, 50},
	}
	for _, c := range cases {
		pdf.AddAnnotation(AnnotationOption{
			X:       c.x,
			Y:       c.y,
			W:       c.w,
			H:       c.h,
			Content: "note " + c.name,
			Type:    AnnotText,
		})
		t.Logf("%s: done", c.name)
	}
	path := filepath.Join(t.TempDir(), "annot.pdf")
	if e := pdf.WritePdf(path); e != nil {
		t.Errorf("write: %v", e)
	}
	// document must remain readable after degenerate annotations
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := GetSourcePDFPageCountFromBytes(written); err != nil || n != 1 {
		t.Errorf("round-trip page count = %d, err %v; want 1", n, err)
	}
	if _, err := ExtractTextFromAllPages(written); err != nil {
		t.Errorf("extraction after annotations failed: %v", err)
	}
}
