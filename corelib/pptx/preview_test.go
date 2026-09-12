package pptx

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderPreview(t *testing.T) {
	dir := t.TempDir()
	deckPath := filepath.Join(dir, "deck.pptx")
	outline := Outline{
		Title:    "预览测试",
		Subtitle: "RenderPreview",
		Slides: []OutlineSlide{
			{Title: "第一页", Bullets: []string{"要点一", "要点二"}, Notes: "备注"},
			{Title: "第二页", Bullets: []string{"要点三"}},
			{Title: "第三页", Bullets: []string{"要点四"}},
		},
	}
	if err := WriteFile(deckPath, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := RenderPreview(deckPath, PreviewOptions{Width: 640})
	if err != nil {
		t.Fatalf("RenderPreview: %v", err)
	}
	if result.SlideCount != 4 { // title slide + 3 content slides
		t.Fatalf("SlideCount = %d, want 4", result.SlideCount)
	}
	if result.RenderedCount != 4 || len(result.Images) != 4 {
		t.Fatalf("RenderedCount = %d, len(Images) = %d, want 4", result.RenderedCount, len(result.Images))
	}
	if result.OutputDir != DefaultPreviewDir(deckPath) {
		t.Fatalf("OutputDir = %q, want %q", result.OutputDir, DefaultPreviewDir(deckPath))
	}
	if result.Truncated {
		t.Fatalf("Truncated = true, want false")
	}
	for _, imagePath := range result.Images {
		f, err := os.Open(imagePath)
		if err != nil {
			t.Fatalf("open %s: %v", imagePath, err)
		}
		cfg, err := png.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", imagePath, err)
		}
		if cfg.Width != 640 {
			t.Fatalf("%s width = %d, want 640", imagePath, cfg.Width)
		}
	}
}

func TestRenderPreviewPaging(t *testing.T) {
	dir := t.TempDir()
	deckPath := filepath.Join(dir, "deck.pptx")
	outline := Outline{
		Title: "分页",
		Slides: []OutlineSlide{
			{Title: "A", Bullets: []string{"a"}},
			{Title: "B", Bullets: []string{"b"}},
			{Title: "C", Bullets: []string{"c"}},
		},
	}
	if err := WriteFile(deckPath, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	outDir := filepath.Join(dir, "out")
	result, err := RenderPreview(deckPath, PreviewOptions{
		OutputDir:   outDir,
		Width:       320,
		SlideOffset: 1,
		MaxSlides:   2,
	})
	if err != nil {
		t.Fatalf("RenderPreview: %v", err)
	}
	if result.RenderedCount != 2 || len(result.Images) != 2 {
		t.Fatalf("RenderedCount = %d, want 2", result.RenderedCount)
	}
	if !result.Truncated || result.NextOffset != 3 {
		t.Fatalf("Truncated = %v NextOffset = %d, want true/3", result.Truncated, result.NextOffset)
	}
	// Page rendering starts at slide 2: file names follow the 1-based slide number.
	if filepath.Base(result.Images[0]) != "slide_002.png" {
		t.Fatalf("first image = %q, want slide_002.png", result.Images[0])
	}
}

func TestRenderPreviewMissingFile(t *testing.T) {
	if _, err := RenderPreview(filepath.Join(t.TempDir(), "missing.pptx"), PreviewOptions{}); err == nil {
		t.Fatal("RenderPreview on missing file: want error")
	}
}
