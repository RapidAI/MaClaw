package guiapp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	ppt "github.com/Vantagics/GoPPT"
	"golang.org/x/image/draw"

	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

// pptxPreviewLocks serializes renders of the same deck so two panes (or a
// quick close/reopen) cannot render concurrently into the same cache dir.
var pptxPreviewLocks sync.Map // map[string]*sync.Mutex

func pptxPreviewLock(path string) *sync.Mutex {
	path = filepath.Clean(strings.TrimSpace(path))
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	lock, _ := pptxPreviewLocks.LoadOrStore(path, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// PptxPreviewEnsure renders a .pptx deck into per-slide images (cached in a
// "<name>_preview" folder next to the deck) and returns the render result for
// the in-app slide preview. Reuses the previous render when every cached
// slide image is at least as new as the deck itself.
func (a *App) PptxPreviewEnsure(filePath string) (*pptx.PreviewResult, error) {
	cleaned := strings.TrimSpace(filePath)
	if cleaned == "" {
		return nil, fmt.Errorf("文件路径为空")
	}
	if !strings.EqualFold(filepath.Ext(cleaned), ".pptx") {
		return nil, fmt.Errorf("仅支持预览 .pptx 文件: %s", cleaned)
	}
	lock := pptxPreviewLock(cleaned)
	lock.Lock()
	defer lock.Unlock()
	// Stat after the lock is held. A mtime taken while waiting would treat a
	// deck that was just rewritten as still matched by the previous pictures.
	info, err := os.Stat(cleaned)
	if err != nil {
		return nil, fmt.Errorf("文件不存在: %s", cleaned)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("不是 PPTX 文件: %s", cleaned)
	}
	outDir := pptx.DefaultPreviewDir(cleaned)
	if cached := cachedPptxPreview(cleaned, outDir, info.ModTime()); cached != nil {
		return cached, nil
	}
	result, err := pptx.RenderPreview(cleaned, pptx.PreviewOptions{OutputDir: outDir, Width: pptx.DefaultPreviewWidth})
	if err != nil {
		return nil, err
	}
	again, statErr := os.Stat(cleaned)
	if statErr == nil && (!again.ModTime().Equal(info.ModTime()) || again.Size() != info.Size()) {
		return pptx.RenderPreview(cleaned, pptx.PreviewOptions{OutputDir: outDir, Width: pptx.DefaultPreviewWidth})
	}
	return result, nil
}

// pptxSlideThumbnailMaxEdge bounds the per-slide thumbnails served to the
// preview strip; the full-fidelity slide stays on AIAssistantAttachmentFullDataURL.
const pptxSlideThumbnailMaxEdge = 320

// PptxSlideThumbnailDataURL returns a bounded thumbnail of a rendered slide
// image, so the preview strip does not hold one full-resolution data URL per
// page in WebView memory.
func (a *App) PptxSlideThumbnailDataURL(path string) (string, error) {
	data, err := readStableSlideImage(path)
	if err != nil {
		return "", err
	}
	if len(data) > 12*1024*1024 {
		return "", fmt.Errorf("attachment must be an image file no larger than 12 MB")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode slide image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return "", fmt.Errorf("attachment image dimensions are not supported")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode slide image: %w", err)
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > pptxSlideThumbnailMaxEdge || height > pptxSlideThumbnailMaxEdge {
		if width >= height {
			height = max(1, height*pptxSlideThumbnailMaxEdge/width)
			width = pptxSlideThumbnailMaxEdge
		} else {
			width = max(1, width*pptxSlideThumbnailMaxEdge/height)
			height = pptxSlideThumbnailMaxEdge
		}
	}
	thumbnail := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), source, bounds, draw.Over, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, thumbnail); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

// deckForSlideImage reports the deck that owns a cached slide picture.
// The cache directory is "<name>_preview" beside the .pptx file.
func deckForSlideImage(imagePath string) (string, bool) {
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return "", false
	}
	dir := filepath.Dir(imagePath)
	base := filepath.Base(dir)
	const suffix = "_preview"
	if len(base) <= len(suffix) || !strings.EqualFold(base[len(base)-len(suffix):], suffix) {
		return "", false
	}
	name := base[:len(base)-len(suffix)]
	candidate := filepath.Join(filepath.Dir(dir), name+".pptx")
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return "", false
	}
	return candidate, true
}

// readStableSlideImage copies a cached slide while the deck render lock is
// held, so a refresh cannot hand back a half-written file. Pictures that are
// not in a deck cache are read directly.
func readStableSlideImage(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("attachment path is required")
	}
	read := func() ([]byte, error) {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() || info.Size() > 12*1024*1024 {
			return nil, fmt.Errorf("attachment must be an image file no larger than 12 MB")
		}
		return os.ReadFile(path)
	}
	if deck, ok := deckForSlideImage(path); ok {
		lock := pptxPreviewLock(deck)
		lock.Lock()
		defer lock.Unlock()
		return read()
	}
	return read()
}

// cachedPptxPreview reconstructs a PreviewResult from an earlier render. The
// cache is trusted only when slides 1..N each have an image at least as new
// as the deck (bounded by the renderer's slide cap). A missing number, or a
// stale image for one of those slides, falls back to rendering. A stale extra
// file does not.
func cachedPptxPreview(filePath, outDir string, sourceTime time.Time) *pptx.PreviewResult {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return nil
	}
	var fresh map[int]string
	format := ""
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasPrefix(lower, "slide_") {
			continue
		}
		ext := ""
		switch {
		case strings.HasSuffix(lower, ".png"):
			ext = "png"
		case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
			ext = "jpeg"
		default:
			continue
		}
		item, err := entry.Info()
		if err != nil || item.ModTime().Before(sourceTime) {
			continue
		}
		n := slideNumberFromPreviewName(name)
		if n <= 0 {
			continue
		}
		if fresh == nil {
			fresh = make(map[int]string)
		}
		prev, ok := fresh[n]
		path := filepath.Join(outDir, name)
		if ok && strings.EqualFold(filepath.Ext(prev), ".png") && ext != "png" {
			continue
		}
		fresh[n] = path
		if format == "" || ext == "png" {
			format = ext
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	// Completeness check: opening the deck only parses the zip directory, it
	// does not render anything. Slides 1..N must each have a fresh image.
	// An older extra file, or a later slide number, does not fill a gap.
	pres, err := ppt.Open(filePath)
	if err != nil {
		return nil
	}
	slideCount := pres.GetSlideCount()
	_ = pres.Close()
	want := slideCount
	if want > pptx.MaxPreviewSlides {
		want = pptx.MaxPreviewSlides
	}
	if want <= 0 || len(fresh) < want {
		return nil
	}
	images := make([]string, 0, want)
	for n := 1; n <= want; n++ {
		path, ok := fresh[n]
		if !ok {
			return nil
		}
		images = append(images, path)
	}
	return &pptx.PreviewResult{
		FilePath:      filePath,
		OutputDir:     outDir,
		SlideCount:    slideCount,
		RenderedCount: len(images),
		Width:         pptx.DefaultPreviewWidth,
		Format:        format,
		Images:        images,
	}
}
