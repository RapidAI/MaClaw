package pptx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ppt "github.com/Vantagics/GoPPT"
)

// Preview rendering bounds. Width is capped so a hostile or mistaken argument
// cannot force an unbounded raster allocation; slide count is capped for the
// same reason.
const (
	DefaultPreviewWidth = 1280
	MaxPreviewWidth     = 4096
	MaxPreviewSlides    = 200
)

// PreviewOptions configures slide-to-image rendering for visual preview.
type PreviewOptions struct {
	// OutputDir receives one image per rendered slide. Empty defaults to
	// DefaultPreviewDir(filePath).
	OutputDir string
	// Width is the output image width in pixels; height follows the slide
	// aspect ratio. Zero uses DefaultPreviewWidth.
	Width int
	// SlideOffset is the zero-based first slide to render.
	SlideOffset int
	// MaxSlides limits how many slides are rendered. Zero renders all
	// remaining slides (still bounded by MaxPreviewSlides).
	MaxSlides int
	// Format is "png" (default) or "jpeg"/"jpg".
	Format string
	// JPEGQuality applies to JPEG output (1-100, default 90).
	JPEGQuality int
	// FontDirs adds extra font directories beyond the OS system fonts.
	FontDirs []string
	// Draft trades fidelity for speed (snapped anti-aliasing, skipped
	// effects); useful for quick batch previews.
	Draft bool
}

// PreviewResult describes a completed preview render.
type PreviewResult struct {
	FilePath      string   `json:"file_path"`
	OutputDir     string   `json:"output_dir"`
	SlideCount    int      `json:"slide_count"`
	RenderedCount int      `json:"rendered_count"`
	Width         int      `json:"width"`
	Format        string   `json:"format"`
	Images        []string `json:"images"`
	Truncated     bool     `json:"truncated,omitempty"`
	NextOffset    int      `json:"next_offset,omitempty"`
	// MissingFonts lists typefaces the deck requested but the system could
	// not provide; rendered text in those runs falls back and may show tofu.
	MissingFonts []string `json:"missing_fonts,omitempty"`
	// SubstitutedFonts lists typefaces that were replaced by a different
	// available font during rendering.
	SubstitutedFonts []string `json:"substituted_fonts,omitempty"`
}

// DefaultPreviewDir returns the conventional preview output directory for a
// deck: a "<name>_preview" folder next to the .pptx file.
func DefaultPreviewDir(filePath string) string {
	ext := filepath.Ext(filePath)
	return strings.TrimSuffix(filePath, ext) + "_preview"
}

// RenderPreview renders slides of a PPTX deck to PNG/JPEG images so the visual
// result of a generated presentation can be inspected without PowerPoint.
func RenderPreview(filePath string, opts PreviewOptions) (result *PreviewResult, err error) {
	defer recoverPresentationPreview(&result, &err)

	if _, statErr := os.Stat(filePath); errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("文件不存在: %s", filePath)
	}

	pres, err := ppt.Open(filePath)
	if err != nil {
		if isInvalidFormat(err.Error()) {
			return nil, fmt.Errorf("文件格式无效，不是有效的 PPTX 文件: %s", filePath)
		}
		return nil, fmt.Errorf("读取 PPTX 失败: %w", err)
	}
	defer pres.Close()

	width := opts.Width
	if width <= 0 {
		width = DefaultPreviewWidth
	}
	if width > MaxPreviewWidth {
		width = MaxPreviewWidth
	}

	format := strings.ToLower(strings.TrimSpace(opts.Format))
	renderFormat := ppt.ImageFormatPNG
	ext := ".png"
	switch format {
	case "", "png":
		format = "png"
	case "jpeg", "jpg":
		format = "jpeg"
		renderFormat = ppt.ImageFormatJPEG
		ext = ".jpg"
	default:
		return nil, fmt.Errorf("不支持的预览图片格式: %q（支持 png/jpeg）", opts.Format)
	}

	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = DefaultPreviewDir(filePath)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建预览目录失败: %w", err)
	}

	slideCount := pres.GetSlideCount()
	if slideCount == 0 {
		return nil, fmt.Errorf("PPTX 没有幻灯片: %s", filePath)
	}
	start := opts.SlideOffset
	if start < 0 {
		start = 0
	}
	if start >= slideCount {
		return nil, fmt.Errorf("slide_offset %d 超出范围（共 %d 页）", start, slideCount)
	}
	limit := slideCount
	maxSlides := opts.MaxSlides
	if maxSlides <= 0 || maxSlides > MaxPreviewSlides {
		maxSlides = MaxPreviewSlides
	}
	if limit-start > maxSlides {
		limit = start + maxSlides
	}

	renderOpts := ppt.DefaultRenderOptions()
	renderOpts.Width = width
	renderOpts.Format = renderFormat
	if opts.JPEGQuality > 0 {
		renderOpts.JPEGQuality = opts.JPEGQuality
	}
	renderOpts.FontCache = ppt.NewFontCache(opts.FontDirs...)
	renderOpts.Draft = opts.Draft
	fontDiag := ppt.NewFontDiagnostics()
	renderOpts.FontDiagnostics = fontDiag

	images := make([]string, 0, limit-start)
	for i := start; i < limit; i++ {
		imagePath := filepath.Join(outputDir, fmt.Sprintf("slide_%03d%s", i+1, ext))
		if err := pres.SaveSlideAsImage(i, imagePath, renderOpts); err != nil {
			return nil, fmt.Errorf("渲染第 %d 页失败: %w", i+1, err)
		}
		images = append(images, imagePath)
	}

	return &PreviewResult{
		FilePath:         filePath,
		OutputDir:        outputDir,
		SlideCount:       slideCount,
		RenderedCount:    len(images),
		Width:            width,
		Format:           format,
		Images:           images,
		Truncated:        limit < slideCount,
		NextOffset:       limit,
		MissingFonts:     fontDiag.MissingNames(),
		SubstitutedFonts: fontUsageNames(fontDiag.Substituted()),
	}, nil
}

func fontUsageNames(usages []ppt.FontUsage) []string {
	if len(usages) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(usages))
	names := make([]string, 0, len(usages))
	for _, u := range usages {
		name := u.String()
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// GoPPT renders untrusted OOXML through a self-contained rasterizer. Keep the
// preview API fail-closed like the structured reader: a dependency panic on
// malformed input must surface as an error, never a process crash.
func recoverPresentationPreview(result **PreviewResult, err *error) {
	if recover() != nil {
		*result = nil
		*err = fmt.Errorf("presentation renderer panicked")
	}
}
