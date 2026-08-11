package knowledge

import (
	"bytes"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageAssetManagerRejectsOversizeBytesWithoutAssets(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assetID := "oversize-bytes"
	_, err = assets.SaveImageFromBytes(assetID, make([]byte, MaxKnowledgeImageAssetBytes+1), ".png")
	if !errors.Is(err, errKnowledgeImageAssetTooLarge) {
		t.Fatalf("SaveImageFromBytes error = %v, want size-limit error", err)
	}
	if _, statErr := os.Stat(assets.AssetDir(assetID)); !os.IsNotExist(statErr) {
		t.Fatalf("oversize asset dir exists or unexpected stat error: %v", statErr)
	}
}

func TestImageAssetManagerRejectsOversizePathWithoutAssets(t *testing.T) {
	root := t.TempDir()
	assets, err := NewImageAssetManager(root)
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(root, "oversize.png")
	if err := os.WriteFile(imagePath, make([]byte, MaxKnowledgeImageAssetBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = assets.SaveImageFromPath("oversize-path", imagePath)
	if !errors.Is(err, errKnowledgeImageAssetTooLarge) {
		t.Fatalf("SaveImageFromPath error = %v, want size-limit error", err)
	}
	if _, statErr := os.Stat(assets.AssetDir("oversize-path")); !os.IsNotExist(statErr) {
		t.Fatalf("oversize asset dir exists or unexpected stat error: %v", statErr)
	}
}

func TestImageAssetManagerBoundsUnknownSizeReaderAndCleansUp(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = assets.saveImageFromReader("oversize-stream", io.LimitReader(bytes.NewReader(make([]byte, MaxKnowledgeImageAssetBytes+2)), MaxKnowledgeImageAssetBytes+2), ".png", 0)
	if !errors.Is(err, errKnowledgeImageAssetTooLarge) {
		t.Fatalf("saveImageFromReader error = %v, want size-limit error", err)
	}
	if _, statErr := os.Stat(assets.AssetDir("oversize-stream")); !os.IsNotExist(statErr) {
		t.Fatalf("oversize stream asset dir exists or unexpected stat error: %v", statErr)
	}
}

func TestImageAssetManagerRejectsPixelBombWithoutAssets(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A PNG signature plus IHDR is sufficient for DecodeConfig. It avoids a
	// full decoded raster while exercising the pre-decode dimension guard.
	bomb := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0, 0, 0, 13, 'I', 'H', 'D', 'R',
		0, 0, 0xff, 0xff, 0, 0, 0xff, 0xff,
		8, 6, 0, 0, 0,
	}
	checksum := crc32.ChecksumIEEE(bomb[12:])
	bomb = append(bomb, byte(checksum>>24), byte(checksum>>16), byte(checksum>>8), byte(checksum))
	_, err = assets.SaveImageFromBytes("pixel-bomb", bomb, ".png")
	if err == nil {
		t.Fatal("SaveImageFromBytes accepted oversized decoded dimensions")
	}
	if _, statErr := os.Stat(assets.AssetDir("pixel-bomb")); !os.IsNotExist(statErr) {
		t.Fatalf("pixel-bomb asset dir exists or unexpected stat error: %v", statErr)
	}
}

func TestImageAssetManagerAcceptsExactSizeLimit(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	asset, err := assets.SaveImageFromBytes("at-limit", make([]byte, MaxKnowledgeImageAssetBytes), ".png")
	if err != nil {
		t.Fatalf("SaveImageFromBytes at limit: %v", err)
	}
	if asset.SizeBytes != MaxKnowledgeImageAssetBytes {
		t.Fatalf("asset size = %d, want %d", asset.SizeBytes, MaxKnowledgeImageAssetBytes)
	}
	if _, err := os.Stat(asset.OriginalPath); err != nil {
		t.Fatalf("original not persisted: %v", err)
	}
}

func TestImageAssetManagerFailedReplacementPreservesExistingAsset(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assetID := "existing-asset"
	first, err := assets.SaveImageFromBytes(assetID, limitTestPNG(t), ".png")
	if err != nil {
		t.Fatalf("initial save: %v", err)
	}
	originalBefore, err := os.ReadFile(first.OriginalPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.PreviewPath); err != nil {
		t.Fatalf("initial preview: %v", err)
	}

	// An unknown-size reader reaches the staged write before exceeding the
	// limit. The old published original and previews must remain untouched.
	_, err = assets.saveImageFromReader(assetID, bytes.NewReader(make([]byte, MaxKnowledgeImageAssetBytes+2)), ".png", 0)
	if !errors.Is(err, errKnowledgeImageAssetTooLarge) {
		t.Fatalf("replacement error = %v, want size-limit error", err)
	}
	originalAfter, err := os.ReadFile(first.OriginalPath)
	if err != nil {
		t.Fatalf("read preserved original: %v", err)
	}
	if !bytes.Equal(originalBefore, originalAfter) {
		t.Fatal("failed replacement overwrote the existing original")
	}
	if _, err := os.Stat(first.PreviewPath); err != nil {
		t.Fatalf("failed replacement removed the existing preview: %v", err)
	}
	if _, err := os.Stat(assets.AssetDir(assetID) + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("failed replacement left backup directory or stat error: %v", err)
	}
	entries, err := os.ReadDir(assets.BaseDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".image-asset-") {
			t.Fatalf("failed replacement left staging directory %q", entry.Name())
		}
	}
}

func TestImageAssetManagerBoundsBothPreviewDimensions(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A tall image previously kept its full height whenever its width was below
	// PreviewWidth, producing an unbounded preview raster despite a bounded
	// preview width.
	imageData := tallPreviewTestJPEG(t, 2, PreviewWidth*3)
	asset, err := assets.SaveImageFromBytes("tall-preview", imageData, ".jpg")
	if err != nil {
		t.Fatalf("SaveImageFromBytes: %v", err)
	}
	preview, err := os.Open(asset.PreviewPath)
	if err != nil {
		t.Fatalf("open preview: %v", err)
	}
	defer preview.Close()
	config, format, err := image.DecodeConfig(preview)
	if err != nil {
		t.Fatalf("decode preview config: %v", err)
	}
	if format != "jpeg" || config.Width <= 0 || config.Height <= 0 || config.Width > PreviewWidth || config.Height > PreviewWidth {
		t.Fatalf("preview = %s %dx%d, want bounded JPEG within %dx%d", format, config.Width, config.Height, PreviewWidth, PreviewWidth)
	}
}

func TestImageAssetManagerOriginalImagePathRejectsNonImageOriginal(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assetID := "vector-only"
	assetDir := assets.AssetDir(assetID)
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "original.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.OriginalImagePath(assetID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OriginalImagePath vector-only err = %v, want not exist", err)
	}

	if err := os.WriteFile(filepath.Join(assetDir, "original.png"), limitTestPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := assets.OriginalImagePath(assetID)
	if err != nil || filepath.Base(path) != "original.png" {
		t.Fatalf("OriginalImagePath raster = %q, %v", path, err)
	}
}

func TestImageAssetManagerDerivedImageRejectsInvalidAndOversizedDimensions(t *testing.T) {
	assets, err := NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assetID := "derived-image"
	assetDir := assets.AssetDir(assetID)
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assets.ThumbPath(assetID), []byte("not a jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.DerivedImage(assetID, "thumbnail"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid thumbnail err = %v, want not exist", err)
	}
	if err := os.WriteFile(assets.PreviewPath(assetID), tallPreviewTestJPEG(t, 1, PreviewWidth+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := assets.DerivedImage(assetID, "preview"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversized preview err = %v, want not exist", err)
	}
	if _, err := assets.DerivedImage(assetID, "unknown"); err == nil {
		t.Fatal("unknown derived variant must fail")
	}
}

func limitTestPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func tallPreviewTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
