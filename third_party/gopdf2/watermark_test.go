package gopdf

import (
	"os"
	"strings"
	"testing"
)

// TestWatermarkExplicitOptions verifies AngleSet/ColorSet/OpacitySet.
func TestWatermarkExplicitOptions(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	err := pdf.AddWatermarkText(WatermarkOption{
		Text:       "HORIZONTAL",
		FontFamily: "lib",
		FontSize:   20,
		Angle:      0,
		AngleSet:   true,
		Color:      [3]uint8{0, 0, 0},
		ColorSet:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf("test/out/wm_explicit.pdf"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile("test/out/wm_explicit.pdf")
	parser, perr := newRawPDFParser(data)
	if perr != nil {
		t.Fatal(perr)
	}
	s := string(parser.getPageContentStream(0))
	// no rotation matrix: identity CTM must be present, cos(45°) absent
	if !strings.Contains(s, "1.00000 0.00000") {
		t.Errorf("expected identity rotation matrix for AngleSet with Angle=0")
	}
	if strings.Contains(s, "0.70711") {
		t.Errorf("45-degree rotation leaked into an AngleSet=0 watermark")
	}
	// black text color must survive (not replaced by gray)
	if !strings.Contains(s, "0.000 0.000 0.000 rg") {
		t.Errorf("black watermark color was replaced by the default gray")
	}
}

// TestWatermarkAllPages verifies the all-pages variant touches every page.
func TestWatermarkAllPages(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		pdf.AddPage()
		pdf.SetXY(50, 50)
		_ = pdf.Cell(nil, "page body")
	}
	if err := pdf.AddWatermarkTextAllPages(WatermarkOption{
		Text:       "TOP SECRET",
		FontFamily: "lib",
		Angle:      0,
		AngleSet:   true,
	}); err != nil {
		t.Fatal(err)
	}
	path := "test/out/wm_all.pdf"
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	pages, err := ExtractTextFromAllPages(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 3 {
		t.Fatalf("expected 3 pages, got %d", len(pages))
	}
	for p := 0; p < 3; p++ {
		found := false
		for _, tx := range pages[p] {
			if strings.Contains(tx.Text, "TOP SECRET") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("page %d missing watermark", p)
		}
	}
}
