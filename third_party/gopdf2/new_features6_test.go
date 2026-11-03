package gopdf

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// ============================================================
// Image Deletion tests
// ============================================================

func TestDeleteImages(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// Add two images
	err := pdf.Image("test/res/gopher01.jpg", 50, 50, &Rect{W: 100, H: 100})
	if err != nil {
		t.Fatal(err)
	}
	err = pdf.Image("test/res/gopher02.png", 200, 50, &Rect{W: 100, H: 100})
	if err != nil {
		t.Fatal(err)
	}

	// Verify images exist
	elems, err := pdf.GetPageElementsByType(1, ElementImage)
	if err != nil {
		t.Fatal(err)
	}
	if len(elems) != 2 {
		t.Fatalf("expected 2 images, got %d", len(elems))
	}

	// Delete all images
	n, err := pdf.DeleteImages(1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deleted, got %d", n)
	}

	// Verify no images remain
	elems, err = pdf.GetPageElementsByType(1, ElementImage)
	if err != nil {
		t.Fatal(err)
	}
	if len(elems) != 0 {
		t.Fatalf("expected 0 images after delete, got %d", len(elems))
	}
}

func TestDeleteImageByIndex(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.Image("test/res/gopher01.jpg", 50, 50, &Rect{W: 100, H: 100})
	pdf.Image("test/res/gopher02.png", 200, 50, &Rect{W: 100, H: 100})

	// Delete only the first image
	err := pdf.DeleteImageByIndex(1, 0)
	if err != nil {
		t.Fatal(err)
	}

	elems, _ := pdf.GetPageElementsByType(1, ElementImage)
	if len(elems) != 1 {
		t.Fatalf("expected 1 image remaining, got %d", len(elems))
	}
}

func TestDeleteImagesFromAllPages(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	pdf.AddPage()
	pdf.Image("test/res/gopher01.jpg", 50, 50, &Rect{W: 100, H: 100})

	pdf.AddPage()
	pdf.Image("test/res/gopher02.png", 50, 50, &Rect{W: 100, H: 100})

	total, err := pdf.DeleteImagesFromAllPages()
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("expected 2 total deleted, got %d", total)
	}

	// Verify both pages are clean
	for p := 1; p <= 2; p++ {
		elems, _ := pdf.GetPageElementsByType(p, ElementImage)
		if len(elems) != 0 {
			t.Fatalf("page %d still has %d images", p, len(elems))
		}
	}
}

func TestDeleteImages_WritePdf(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.Image("test/res/gopher01.jpg", 50, 50, &Rect{W: 200, H: 200})

	pdf.DeleteImages(1)

	outPath := "test/out/test_delete_images.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Output PDF size: %d bytes", info.Size())
}

// ============================================================
// Image Recompression tests
// ============================================================

func TestRecompressImages(t *testing.T) {
	// Create a PDF with an image
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.Image("test/res/gopher01.jpg", 50, 50, &Rect{W: 200, H: 200})
	if err != nil {
		t.Fatal(err)
	}

	outPath := "test/out/test_recompress_src.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	originalSize := len(data)
	t.Logf("Original PDF size: %d bytes", originalSize)

	// Recompress with lower quality
	recompressed, err := RecompressImages(data, RecompressOption{
		Format:      "jpeg",
		JPEGQuality: 30,
	})
	if err != nil {
		t.Fatal(err)
	}

	outPath2 := "test/out/test_recompress_result.pdf"
	err = os.WriteFile(outPath2, recompressed, 0644)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Recompressed PDF size: %d bytes", len(recompressed))
}

func TestRecompressImages_NoImages(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := "test/out/test_recompress_empty.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	result, err := RecompressImages(data, RecompressOption{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
}

// ============================================================
// SVG Insertion tests
// ============================================================

func TestImageSVG(t *testing.T) {
	// Create a simple SVG test file
	svgContent := `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200" viewBox="0 0 200 200">
  <rect x="10" y="10" width="80" height="60" fill="#ff0000" stroke="#000000" stroke-width="2"/>
  <circle cx="150" cy="50" r="30" fill="#00ff00" stroke="#0000ff"/>
  <line x1="10" y1="100" x2="190" y2="100" stroke="#333333" stroke-width="1"/>
  <ellipse cx="100" cy="150" rx="60" ry="30" fill="#ffcc00"/>
  <polygon points="150,120 180,180 120,180" fill="#9900cc"/>
</svg>`

	svgPath := "test/out/test_input.svg"
	err := os.WriteFile(svgPath, []byte(svgContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err = pdf.ImageSVG(svgPath, SVGOption{
		X:      50,
		Y:      50,
		Width:  300,
		Height: 300,
	})
	if err != nil {
		t.Fatal(err)
	}

	outPath := "test/out/test_svg_insert.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(outPath)
	t.Logf("SVG PDF size: %d bytes", info.Size())
}

func TestImageSVGFromBytes(t *testing.T) {
	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">
  <rect x="0" y="0" width="100" height="100" fill="blue"/>
  <circle cx="50" cy="50" r="40" fill="red"/>
</svg>`)

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.ImageSVGFromBytes(svgData, SVGOption{X: 100, Y: 100, Width: 200, Height: 200})
	if err != nil {
		t.Fatal(err)
	}

	outPath := "test/out/test_svg_from_bytes.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
}

func TestImageSVG_Path(t *testing.T) {
	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="200">
  <path d="M 10 80 C 40 10, 65 10, 95 80 S 150 150, 180 80" stroke="black" fill="none" stroke-width="2"/>
  <path d="M 10 10 L 50 50 L 90 10 Z" fill="green"/>
</svg>`)

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.ImageSVGFromBytes(svgData, SVGOption{X: 50, Y: 50, Width: 200, Height: 200})
	if err != nil {
		t.Fatal(err)
	}

	outPath := "test/out/test_svg_path.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
}

func TestImageSVG_Empty(t *testing.T) {
	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"></svg>`)

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.ImageSVGFromBytes(svgData, SVGOption{X: 0, Y: 0})
	if err == nil {
		t.Fatal("expected error for empty SVG")
	}
}

// ============================================================
// Pixmap Rendering tests
// ============================================================

func TestRenderPageToImage(t *testing.T) {
	// Create a simple PDF
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.Image("test/res/gopher01.jpg", 50, 50, &Rect{W: 200, H: 200})
	if err != nil {
		t.Fatal(err)
	}

	outPath := "test/out/test_render_src.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	img, err := RenderPageToImage(data, 0, RenderOption{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}

	bounds := img.Bounds()
	t.Logf("Rendered image: %dx%d", bounds.Dx(), bounds.Dy())

	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Fatal("rendered image has zero dimensions")
	}

	// Save as PNG
	outImg := "test/out/test_render_page.png"
	f, err := os.Create(outImg)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
	t.Logf("Saved rendered image to %s", outImg)
}

func TestRenderPageToImage_HighDPI(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := "test/out/test_render_highdpi_src.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	img, err := RenderPageToImage(data, 0, RenderOption{DPI: 150})
	if err != nil {
		t.Fatal(err)
	}

	bounds := img.Bounds()
	t.Logf("High DPI rendered image: %dx%d", bounds.Dx(), bounds.Dy())

	// At 150 DPI, A4 (595x842 pt) should be roughly 1240x1754 px
	if bounds.Dx() < 1000 {
		t.Fatalf("expected width > 1000 at 150 DPI, got %d", bounds.Dx())
	}
}

func TestRenderPageToImage_CustomBackground(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := "test/out/test_render_bg_src.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	yellowBg := color.RGBA{R: 255, G: 255, B: 0, A: 255}
	img, err := RenderPageToImage(data, 0, RenderOption{
		DPI:        72,
		Background: yellowBg,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Check that a corner pixel is yellow
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatal("expected *image.RGBA")
	}
	pixel := rgba.RGBAAt(0, 0)
	if pixel.R != 255 || pixel.G != 255 || pixel.B != 0 {
		t.Fatalf("expected yellow background, got %v", pixel)
	}
}

func TestRenderAllPagesToImages(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddPage()

	outPath := "test/out/test_render_multi_src.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	images, err := RenderAllPagesToImages(data, RenderOption{DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	t.Logf("Rendered %d pages", len(images))
}

func TestRenderPageToImage_InvalidPage(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := "test/out/test_render_invalid_src.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	_, err := RenderPageToImage(data, 99, RenderOption{})
	if err == nil {
		t.Fatal("expected error for invalid page index")
	}
}
