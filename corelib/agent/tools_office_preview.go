package agent

// tools_office_preview.go implements preview_pptx: renders PPTX slides to
// PNG/JPEG images via GoPPT so a generated deck can be visually checked
// without PowerPoint.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

// ToolPreviewPPTX renders a .pptx deck to per-slide images and returns the
// image paths as JSON. It reuses the structured-reader input snapshot so the
// rendered bytes are the same verified copy that passed container preflight.
func ToolPreviewPPTX(args map[string]interface{}) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	filePath = resolveOfficeToolPath(filePath)
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return formatOfficeReadUnavailable(filePath)
	}
	if !strings.EqualFold(filepath.Ext(filePath), ".pptx") {
		return formatOfficeReadFailure(filePath, strings.TrimPrefix(filepath.Ext(filePath), "."), ErrOfficeReadFormatMismatch)
	}

	parsePath, cleanup, err := structuredOfficeToolSnapshot(filePath, "pptx")
	if err != nil {
		return formatOfficeReadFailure(filePath, "pptx", err)
	}
	defer cleanup()

	opts := pptx.PreviewOptions{
		Width:       previewWidth(args),
		SlideOffset: structuredOfficeSlideOffset(args),
		MaxSlides:   previewMaxSlides(args),
		Format:      StringArg(args, "format"),
		Draft:       boolArg(args, "draft", false),
	}
	// Default the output directory from the original deck path, not from the
	// private snapshot: preview images belong next to the user's file.
	if outputDir := StringArg(args, "output_dir"); outputDir != "" {
		opts.OutputDir = resolveOfficeToolPath(outputDir)
	} else {
		opts.OutputDir = pptx.DefaultPreviewDir(filePath)
	}

	result, err := pptx.RenderPreview(parsePath, opts)
	if err != nil {
		return formatOfficeReadFailure(filePath, "pptx", err)
	}
	result.FilePath = filePath

	data, err := marshalStructuredOfficeResult(result)
	if err != nil {
		return formatOfficeReadFailure(filePath, "pptx", err)
	}
	return string(data)
}

func previewWidth(args map[string]interface{}) int {
	width := intArg(args, "width", pptx.DefaultPreviewWidth)
	if width <= 0 {
		return pptx.DefaultPreviewWidth
	}
	if width > pptx.MaxPreviewWidth {
		return pptx.MaxPreviewWidth
	}
	return width
}

func previewMaxSlides(args map[string]interface{}) int {
	maxSlides := intArg(args, "max_slides", 0)
	if maxSlides <= 0 {
		return 0
	}
	if maxSlides > pptx.MaxPreviewSlides {
		return pptx.MaxPreviewSlides
	}
	return maxSlides
}
