package guiapp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
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
	info, err := os.Stat(cleaned)
	if err != nil {
		return nil, fmt.Errorf("文件不存在: %s", cleaned)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("不是 PPTX 文件: %s", cleaned)
	}
	lock := pptxPreviewLock(cleaned)
	lock.Lock()
	defer lock.Unlock()
	outDir := pptx.DefaultPreviewDir(cleaned)
	if cached := cachedPptxPreview(cleaned, outDir, info.ModTime()); cached != nil {
		return cached, nil
	}
	return pptx.RenderPreview(cleaned, pptx.PreviewOptions{OutputDir: outDir, Width: pptx.DefaultPreviewWidth})
}

// pptxSlideThumbnailMaxEdge bounds the per-slide thumbnails served to the
// preview strip; the full-fidelity slide stays on AIAssistantAttachmentFullDataURL.
const pptxSlideThumbnailMaxEdge = 320

// PptxSlideThumbnailDataURL returns a bounded thumbnail of a rendered slide
// image, so the preview strip does not hold one full-resolution data URL per
// page in WebView memory.
func (a *App) PptxSlideThumbnailDataURL(path string) (string, error) {
	file, _, err := openAIAssistantAttachmentImage(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	source, _, err := image.Decode(file)
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

// cachedPptxPreview reconstructs a PreviewResult from an earlier render. The
// cache is only trusted when every cached slide image is at least as new as
// the deck itself AND the image count covers the whole deck (bounded by the
// renderer's slide cap) — a partial or stale render falls back to rendering.
func cachedPptxPreview(filePath, outDir string, sourceTime time.Time) *pptx.PreviewResult {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return nil
	}
	var images []string
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
			return nil
		}
		if format == "" {
			format = ext
		}
		images = append(images, filepath.Join(outDir, name))
	}
	if len(images) == 0 {
		return nil
	}
	// Completeness check: opening the deck only parses the zip directory, it
	// does not render anything.
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
	if len(images) < want {
		return nil
	}
	sort.Strings(images)
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
