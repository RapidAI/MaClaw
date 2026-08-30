package pptx

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestPNG renders a small solid-color PNG so the deck writer has a real
// image file with a known aspect ratio to embed.
func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}

// The birthday-deck incident (2026-08-27): the model downloaded a photo but
// the outline contract had no image field, so the deck shipped with text
// placeholders. Slide images must round-trip through WriteFile into a real
// embedded image shape.
func TestWriteFileEmbedsSlideImages(t *testing.T) {
	dir := t.TempDir()
	photo := filepath.Join(dir, "cat.png")
	writeTestPNG(t, photo, 400, 300)
	deck := filepath.Join(dir, "deck.pptx")

	outline := Outline{
		Title: "生日",
		Slides: []OutlineSlide{{
			Title:   "相册",
			Bullets: []string{"第一张"},
			Images:  []OutlineImage{{Path: photo}},
		}},
	}
	if err := WriteFile(deck, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ReadWithOptions(deck, ReadOptions{})
	if err != nil {
		t.Fatalf("ReadWithOptions: %v", err)
	}
	imageShapes := 0
	for _, slide := range got.Slides {
		for _, shape := range slide.Shapes {
			if shape.Type == ShapeTypeImage {
				imageShapes++
				if shape.Dimensions.Width <= 0 || shape.Dimensions.Height <= 0 {
					t.Fatalf("image shape has no size: %+v", shape.Dimensions)
				}
				// 400x300 source must stay 4:3 after fitting.
				ratio := float64(shape.Dimensions.Width) / float64(shape.Dimensions.Height)
				if ratio < 1.2 || ratio > 1.5 {
					t.Fatalf("aspect ratio distorted: %v", ratio)
				}
			}
		}
	}
	if imageShapes != 1 {
		t.Fatalf("expected exactly 1 embedded image shape, got %d", imageShapes)
	}
}

// A missing image file must fail the whole write (fail-closed) instead of
// shipping a deck with a silent gap.
func TestWriteFileMissingImageFails(t *testing.T) {
	dir := t.TempDir()
	deck := filepath.Join(dir, "deck.pptx")
	outline := Outline{
		Slides: []OutlineSlide{{
			Title:  "相册",
			Images: []OutlineImage{{Path: filepath.Join(dir, "nope.jpg")}},
		}},
	}
	err := WriteFile(deck, outline)
	if err == nil || !strings.Contains(err.Error(), "pptx_slide_image_unreadable") {
		t.Fatalf("expected unreadable-image failure, got %v", err)
	}
}

// Explicit inch sizes override aspect fitting.
func TestWriteFileExplicitImageSize(t *testing.T) {
	dir := t.TempDir()
	photo := filepath.Join(dir, "cat.png")
	writeTestPNG(t, photo, 400, 300)
	deck := filepath.Join(dir, "deck.pptx")
	outline := Outline{
		Slides: []OutlineSlide{{
			Title:  "相册",
			Images: []OutlineImage{{Path: photo, Width: 5, Height: 2}},
		}},
	}
	if err := WriteFile(deck, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadWithOptions(deck, ReadOptions{})
	if err != nil {
		t.Fatalf("ReadWithOptions: %v", err)
	}
	for _, slide := range got.Slides {
		for _, shape := range slide.Shapes {
			if shape.Type != ShapeTypeImage {
				continue
			}
			if shape.Dimensions.Width != 5*914400 || shape.Dimensions.Height != 2*914400 {
				t.Fatalf("explicit size not honored: %+v", shape.Dimensions)
			}
			return
		}
	}
	t.Fatal("no image shape found")
}
