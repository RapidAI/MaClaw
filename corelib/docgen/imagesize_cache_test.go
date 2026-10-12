package docgen

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// writeSizeProbeJPEG writes a JPEG of the given pixel size. Size is what the
// fitter reads, so two images with different dimensions are enough to tell
// whether a result came from a fresh decode or from the cache.
func writeSizeProbeJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 251), B: 100, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
}

func TestImageSizeCacheReusesDecodedSizes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fig.jpg")
	writeSizeProbeJPEG(t, path, 1600, 1200)

	cache := newMarkdownImageSizeCache()
	first := decodeMarkdownImageSizeWithCache(path, cache)
	second := decodeMarkdownImageSizeWithCache(path, cache)
	if first != second {
		t.Fatalf("cache changed the result: %+v vs %+v", first, second)
	}
	// 1600x1200 fitted into 480x600 keeps the 4:3 ratio: 480x360.
	if first.w != 480 || first.h != 360 {
		t.Fatalf("unexpected fitted size %dx%d, want 480x360", first.w, first.h)
	}
}

// TestImageSizeCacheInvalidatesRewrittenFiles pins the safety property that
// makes path-plus-identity keying necessary: the paper figure pipeline
// rewrites files in place, and a stale cached size would lay out the wrong
// image dimensions.
func TestImageSizeCacheInvalidatesRewrittenFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fig.jpg")
	writeSizeProbeJPEG(t, path, 1600, 1200)

	cache := newMarkdownImageSizeCache()
	before := decodeMarkdownImageSizeWithCache(path, cache)

	// Rewrite with a different aspect ratio, guaranteeing a new mtime.
	time.Sleep(10 * time.Millisecond)
	writeSizeProbeJPEG(t, path, 1200, 1600)

	after := decodeMarkdownImageSizeWithCache(path, cache)
	if before == after {
		t.Fatalf("cache served a stale size after the file was rewritten: %+v", after)
	}
	// 1200x1600 fitted into 480x600 is height-bound: 450x600.
	if after.w != 450 || after.h != 600 {
		t.Fatalf("unexpected refitted size %dx%d, want 450x600", after.w, after.h)
	}
}

func TestImageSizeCacheFallsBackWhenUnreadable(t *testing.T) {
	size := decodeMarkdownImageSizeWithCache(filepath.Join(t.TempDir(), "missing.jpg"), newMarkdownImageSizeCache())
	if size.w <= 0 || size.h <= 0 {
		t.Fatalf("unreadable image must fall back to a positive size, got %+v", size)
	}
}

func TestImageSizeCacheIsConcurrencySafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fig.jpg")
	writeSizeProbeJPEG(t, path, 800, 600)
	cache := newMarkdownImageSizeCache()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := decodeMarkdownImageSizeWithCache(path, cache)
			if got.w != 480 || got.h != 360 {
				t.Errorf("unexpected size %+v", got)
			}
		}()
	}
	wg.Wait()
}

// TestImageSizeCacheEvictsOldEntries pins the bound: a document referencing
// thousands of images must not grow the cache without limit.
func TestImageSizeCacheEvictsOldEntries(t *testing.T) {
	cache := newMarkdownImageSizeCache()
	for i := 0; i < maxMarkdownImageCacheEntries+50; i++ {
		cache.put("key-"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+string(rune('0'+i%10))+string(rune('A'+(i/10)%26)), markdownImageSize{w: i, h: i})
	}
	if len(cache.entries) > maxMarkdownImageCacheEntries {
		t.Fatalf("cache grew to %d entries, limit is %d", len(cache.entries), maxMarkdownImageCacheEntries)
	}
	if len(cache.order) != len(cache.entries) {
		t.Fatalf("eviction bookkeeping drifted: order=%d entries=%d", len(cache.order), len(cache.entries))
	}
}
