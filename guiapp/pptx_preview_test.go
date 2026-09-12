package guiapp

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

func TestPptxPreviewEnsureRejectsInvalidInput(t *testing.T) {
	app := &App{}
	if _, err := app.PptxPreviewEnsure(""); err == nil {
		t.Fatal("expected an error for an empty path")
	}
	if _, err := app.PptxPreviewEnsure(filepath.Join(t.TempDir(), "deck.txt")); err == nil {
		t.Fatal("expected an error for a non-pptx extension")
	}
	if _, err := app.PptxPreviewEnsure(filepath.Join(t.TempDir(), "missing.pptx")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if _, err := app.PptxPreviewEnsure(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory")
	}
}

var pngMagic = []byte{0x89, 'P', 'N', 'G'}

// renderTestDeck writes a real 4-slide deck and renders its preview once.
func renderTestDeck(t *testing.T) (deck, outDir string) {
	t.Helper()
	dir := t.TempDir()
	deck = filepath.Join(dir, "deck.pptx")
	outline := pptx.Outline{
		Title: "缓存测试",
		Slides: []pptx.OutlineSlide{
			{Title: "第一页", Bullets: []string{"要点一"}},
			{Title: "第二页", Bullets: []string{"要点二"}},
			{Title: "第三页", Bullets: []string{"要点三"}},
		},
	}
	if err := pptx.WriteFile(deck, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	result, err := pptx.RenderPreview(deck, pptx.PreviewOptions{Width: 640})
	if err != nil {
		t.Fatalf("RenderPreview: %v", err)
	}
	if len(result.Images) != 4 {
		t.Fatalf("expected 4 rendered slides, got %d", len(result.Images))
	}
	return deck, result.OutputDir
}

func TestPptxPreviewEnsureReusesCompleteFreshCache(t *testing.T) {
	deck, outDir := renderTestDeck(t)
	// Prove the cache is served verbatim: corrupt one image but keep its mtime
	// fresh — a re-render would replace the marker.
	marker := filepath.Join(outDir, "slide_001.png")
	if err := os.WriteFile(marker, []byte("CACHED"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(marker, now, now); err != nil {
		t.Fatal(err)
	}
	result, err := (&App{}).PptxPreviewEnsure(deck)
	if err != nil {
		t.Fatalf("expected the cached result, got error: %v", err)
	}
	if result.RenderedCount != 4 || result.SlideCount != 4 || len(result.Images) != 4 {
		t.Fatalf("expected 4 cached slides, got %+v", result)
	}
	for _, image := range result.Images {
		if !strings.HasSuffix(image, ".png") {
			t.Fatalf("expected png slides, got %v", result.Images)
		}
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "CACHED" {
		t.Fatalf("cache was not reused (slide_001 re-rendered): %v", err)
	}
}

func TestPptxPreviewEnsureRendersWhenCacheIsPartial(t *testing.T) {
	deck, outDir := renderTestDeck(t)
	// A partial cache (one slide missing) must not be served as complete.
	if err := os.Remove(filepath.Join(outDir, "slide_003.png")); err != nil {
		t.Fatal(err)
	}
	result, err := (&App{}).PptxPreviewEnsure(deck)
	if err != nil {
		t.Fatalf("expected a re-render, got error: %v", err)
	}
	if len(result.Images) != 4 {
		t.Fatalf("expected 4 slides after re-render, got %d", len(result.Images))
	}
	content, err := os.ReadFile(filepath.Join(outDir, "slide_003.png"))
	if err != nil || !bytes.HasPrefix(content, pngMagic) {
		t.Fatalf("slide_003 was not re-rendered: %v", err)
	}
}

func TestPptxSlideThumbnailDataURLBoundsTheImage(t *testing.T) {
	_, outDir := renderTestDeck(t)
	slide := filepath.Join(outDir, "slide_001.png")
	dataURL, err := (&App{}).PptxSlideThumbnailDataURL(slide)
	if err != nil {
		t.Fatalf("PptxSlideThumbnailDataURL: %v", err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("expected a png data URL, got %.40s", dataURL)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if cfg.Width > 320 || cfg.Height > 320 {
		t.Fatalf("thumbnail exceeds 320px bound: %dx%d", cfg.Width, cfg.Height)
	}
	if cfg.Width != 320 {
		t.Fatalf("expected the wide edge to reach the 320px bound, got %dx%d", cfg.Width, cfg.Height)
	}
	if _, err := (&App{}).PptxSlideThumbnailDataURL(filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Fatal("expected an error for a missing image")
	}
}

func TestPptxPreviewEnsureRendersWhenCacheIsStale(t *testing.T) {
	deck, outDir := renderTestDeck(t)
	old := time.Now().Add(-time.Hour)
	for _, name := range []string{"slide_001.png", "slide_002.png", "slide_003.png", "slide_004.png"} {
		p := filepath.Join(outDir, name)
		if err := os.WriteFile(p, []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	result, err := (&App{}).PptxPreviewEnsure(deck)
	if err != nil {
		t.Fatalf("expected a re-render, got error: %v", err)
	}
	if len(result.Images) != 4 {
		t.Fatalf("expected 4 slides after re-render, got %d", len(result.Images))
	}
	content, err := os.ReadFile(filepath.Join(outDir, "slide_001.png"))
	if err != nil || !bytes.HasPrefix(content, pngMagic) {
		t.Fatalf("stale cache was served: %v", err)
	}
}
