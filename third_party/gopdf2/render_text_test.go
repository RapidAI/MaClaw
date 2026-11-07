package gopdf

import (
	"image"
	"os"
	"testing"
)

// countDarkPixels counts pixels significantly darker than the background.
func countDarkPixels(img image.Image) int {
	return countDarkPixelsMinimum(img, 60000)
}

// countDarkPixelsMinimum counts pixels darker than minRGB, skipping
// fully transparent pixels.
func countDarkPixelsMinimum(img image.Image, minRGB uint32) int {
	dark := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if r < minRGB && g < minRGB && bl < minRGB {
				dark++
			}
		}
	}
	return dark
}

// TestRenderTextRasterization verifies RenderPageToImage now draws glyphs:
// a page of large text must produce a meaningful number of dark pixels.
func TestRenderTextRasterization(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 36); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(60, 80)
	for row := 0; row < 6; row++ {
		pdf.SetXY(60, 80+float64(row)*60)
		if err := pdf.Cell(nil, "Hello Glyphs 12345"); err != nil {
			t.Fatal(err)
		}
	}
	path := "test/out/render_text.pdf"
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := RenderPageToImage(data, 0, RenderOption{DPI: 96})
	if err != nil {
		t.Fatal(err)
	}
	dark := countDarkPixels(img)
	t.Logf("dark pixels: %d", dark)
	if dark < 2000 {
		t.Errorf("text not rasterized (only %d dark pixels at 96dpi for 6 lines of 36pt text)", dark)
	}
}
