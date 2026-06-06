package knowledge

import (
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"golang.org/x/image/draw"
)

const (
	// ThumbSize is the max edge length for list thumbnails.
	ThumbSize = 120
	// PreviewWidth is the max width for detail preview images.
	PreviewWidth = 480

	imageAssetsSubdir = "knowledge_assets"
)

// ImageAsset holds paths and metadata for a stored image asset.
type ImageAsset struct {
	OriginalPath string // absolute path to original image
	ThumbPath    string // absolute path to 120px thumbnail
	PreviewPath  string // absolute path to 480px preview
	Width        int    // original image width in pixels
	Height       int    // original image height in pixels
	Format       string // detected format: "png", "jpeg", "gif", "bmp", "webp"
	SizeBytes    int64  // original file size
}

// ImageAssetManager manages image asset storage and lifecycle.
type ImageAssetManager struct {
	baseDir string // ~/.maclaw/knowledge_assets/
}

// NewImageAssetManager creates a manager rooted at the given base directory.
// The directory is created if it does not exist.
func NewImageAssetManager(maclawDataDir string) (*ImageAssetManager, error) {
	baseDir := filepath.Join(maclawDataDir, imageAssetsSubdir)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("knowledge image_assets: create base dir: %w", err)
	}
	return &ImageAssetManager{baseDir: baseDir}, nil
}

// BaseDir returns the absolute base directory for image assets.
func (m *ImageAssetManager) BaseDir() string {
	return m.baseDir
}

// SaveImageFromPath saves an image file and generates thumbnails.
// sourceID is used as the subdirectory name.
// Returns the asset metadata or an error.
func (m *ImageAssetManager) SaveImageFromPath(sourceID, imagePath string) (*ImageAsset, error) {
	f, err := os.Open(imagePath)
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat image: %w", err)
	}

	return m.saveImageFromReader(sourceID, f, filepath.Ext(imagePath), info.Size())
}

// SaveImageFromBytes saves raw image bytes and generates thumbnails.
// ext should include the dot (e.g. ".png", ".jpg").
func (m *ImageAssetManager) SaveImageFromBytes(sourceID string, data []byte, ext string) (*ImageAsset, error) {
	r := &bytesReader{data: data}
	return m.saveImageFromReader(sourceID, r, ext, int64(len(data)))
}

// DeleteAssets removes all image assets for a source.
// Returns nil if sourceID is invalid or directory doesn't exist.
func (m *ImageAssetManager) DeleteAssets(sourceID string) error {
	dir := m.AssetDir(sourceID)
	if dir == "" {
		return nil // invalid sourceID, nothing to delete
	}
	return os.RemoveAll(dir)
}

// AssetDir returns the absolute directory path for a source's assets.
// Returns empty string if sourceID contains path traversal characters.
func (m *ImageAssetManager) AssetDir(sourceID string) string {
	if strings.ContainsAny(sourceID, `/\`) || strings.Contains(sourceID, "..") {
		return ""
	}
	return filepath.Join(m.baseDir, sourceID)
}

// OriginalPath returns the expected original image path for a source.
func (m *ImageAssetManager) OriginalPath(sourceID, ext string) string {
	dir := m.AssetDir(sourceID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "original"+ext)
}

// ThumbPath returns the expected thumbnail path for a source.
func (m *ImageAssetManager) ThumbPath(sourceID string) string {
	dir := m.AssetDir(sourceID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "thumb_120.jpg")
}

// PreviewPath returns the expected preview path for a source.
func (m *ImageAssetManager) PreviewPath(sourceID string) string {
	dir := m.AssetDir(sourceID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "preview_480.jpg")
}

// HasAssets checks if the source has any image assets stored.
func (m *ImageAssetManager) HasAssets(sourceID string) bool {
	dir := m.AssetDir(sourceID)
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// RegenerateThumb regenerates a missing thumbnail from the original image.
func (m *ImageAssetManager) RegenerateThumb(sourceID string) error {
	dir := m.AssetDir(sourceID)
	if dir == "" {
		return fmt.Errorf("invalid source ID")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read asset dir: %w", err)
	}
	var originalPath string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "original") {
			originalPath = filepath.Join(dir, e.Name())
			break
		}
	}
	if originalPath == "" {
		return fmt.Errorf("original image not found in %s", dir)
	}

	f, err := os.Open(originalPath)
	if err != nil {
		return err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("decode original: %w", err)
	}

	if err := saveScaledJPEG(img, filepath.Join(dir, "thumb_120.jpg"), ThumbSize, ThumbSize, true); err != nil {
		return fmt.Errorf("generate thumb: %w", err)
	}
	if err := saveScaledJPEG(img, filepath.Join(dir, "preview_480.jpg"), PreviewWidth, 0, false); err != nil {
		return fmt.Errorf("generate preview: %w", err)
	}
	return nil
}

// --- internal ---

type bytesReader struct {
	data []byte
	pos  int
}

func (b *bytesReader) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

func (m *ImageAssetManager) saveImageFromReader(sourceID string, r io.Reader, ext string, sizeBytes int64) (*ImageAsset, error) {
	// Create asset directory.
	dir := m.AssetDir(sourceID)
	if dir == "" {
		return nil, fmt.Errorf("invalid source ID: contains path traversal characters")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create asset dir: %w", err)
	}

	ext = normalizeImageExt(ext)

	// Save original.
	originalPath := filepath.Join(dir, "original"+ext)
	outFile, err := os.Create(originalPath)
	if err != nil {
		return nil, fmt.Errorf("create original: %w", err)
	}

	written, err := io.Copy(outFile, r)
	if err != nil {
		outFile.Close()
		return nil, fmt.Errorf("write original: %w", err)
	}
	outFile.Close()

	if sizeBytes <= 0 {
		sizeBytes = written
	}

	// Decode image for thumbnail generation.
	// Skip decode for very large files to avoid OOM (>20MB compressed usually means
	// >100MP decoded which would use >400MB RAM for RGBA).
	const maxDecodeBytes int64 = 20 * 1024 * 1024
	if sizeBytes > maxDecodeBytes {
		return &ImageAsset{
			OriginalPath: originalPath,
			Format:       strings.TrimPrefix(ext, "."),
			SizeBytes:    sizeBytes,
		}, nil
	}

	imgFile, err := os.Open(originalPath)
	if err != nil {
		return nil, fmt.Errorf("reopen original: %w", err)
	}
	defer imgFile.Close()

	img, format, err := image.Decode(imgFile)
	if err != nil {
		// Image cannot be decoded (e.g. SVG, EMF, WMF) — store original only, no thumbnails.
		return &ImageAsset{
			OriginalPath: originalPath,
			Format:       strings.TrimPrefix(ext, "."),
			SizeBytes:    sizeBytes,
		}, nil
	}

	bounds := img.Bounds()
	asset := &ImageAsset{
		OriginalPath: originalPath,
		Width:        bounds.Dx(),
		Height:       bounds.Dy(),
		Format:       format,
		SizeBytes:    sizeBytes,
	}

	// Generate thumbnail (120x120, fit within square).
	thumbPath := filepath.Join(dir, "thumb_120.jpg")
	if err := saveScaledJPEG(img, thumbPath, ThumbSize, ThumbSize, true); err == nil {
		asset.ThumbPath = thumbPath
	}

	// Generate preview (480px width, proportional height).
	previewPath := filepath.Join(dir, "preview_480.jpg")
	if err := saveScaledJPEG(img, previewPath, PreviewWidth, 0, false); err == nil {
		asset.PreviewPath = previewPath
	}

	return asset, nil
}

// saveScaledJPEG scales the image to fit within maxW x maxH and saves as JPEG.
// If fitSquare is true, both dimensions are capped (thumbnail behavior).
// If fitSquare is false and maxH is 0, height is computed proportionally from maxW.
func saveScaledJPEG(src image.Image, dstPath string, maxW, maxH int, fitSquare bool) error {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if srcW == 0 || srcH == 0 {
		return fmt.Errorf("zero-dimension image")
	}

	var dstW, dstH int
	if fitSquare {
		// Fit within maxW x maxH square, maintaining aspect ratio.
		ratio := float64(srcW) / float64(srcH)
		if ratio > 1 {
			// Landscape
			dstW = maxW
			dstH = int(float64(maxW) / ratio)
		} else {
			// Portrait or square
			dstH = maxH
			dstW = int(float64(maxH) * ratio)
		}
	} else {
		// Scale to maxW, proportional height.
		if srcW <= maxW {
			// Already smaller — don't upscale.
			dstW = srcW
			dstH = srcH
		} else {
			dstW = maxW
			dstH = int(float64(srcH) * float64(maxW) / float64(srcW))
		}
	}

	if dstW <= 0 {
		dstW = 1
	}
	if dstH <= 0 {
		dstH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return jpeg.Encode(f, dst, &jpeg.Options{Quality: 80})
}

// normalizeImageExt ensures the extension has a dot and is lowercase.
func normalizeImageExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ".png"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	// Normalize common variants.
	switch ext {
	case ".jpeg":
		return ".jpg"
	default:
		return ext
	}
}

// IsImageExt returns true if the file extension is a supported image format.
func IsImageExt(ext string) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	for _, e := range ImageIncludeExts {
		if ext == e {
			return true
		}
	}
	return false
}

// IsVectorImageExt returns true for vector image formats that cannot be OCR'd.
func IsVectorImageExt(ext string) bool {
	ext = strings.ToLower(strings.TrimSpace(ext))
	switch ext {
	case ".emf", ".wmf", ".svg":
		return true
	}
	return false
}
