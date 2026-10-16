package agent

// tools_office_preview.go implements preview_pptx: renders PPTX slides to
// PNG/JPEG images via GoPPT so a generated deck can be visually checked
// without PowerPoint.

import (
	"fmt"
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

// previewPPTXAfterWrite renders a best-effort preview right after a deck is
// (re)generated so the GUI and the agent can inspect the visual result and
// fix layout problems in the same loop. A preview failure never fails the
// write itself. Pass "preview": false in the write args to opt out.
func previewPPTXAfterWrite(args map[string]interface{}, filePath string) string {
	if !boolArg(args, "preview", true) {
		return ""
	}
	result, err := pptx.RenderPreview(filePath, pptx.PreviewOptions{Width: pptx.DefaultPreviewWidth})
	if err != nil {
		return fmt.Sprintf("\n预览图渲染失败（不影响已写入的文件）: %v。可稍后通过 office(action=\"preview_pptx\", file_path=%q) 重试。", err, filePath)
	}
	return fmt.Sprintf("\n已自动渲染 %d/%d 页预览图到目录: %s（slide_001.png 起）。请查看预览图检查每页的文字溢出、内容重叠、版式与配色效果；如需修改，调整 data 后重新写入会自动刷新预览。传入 preview=false 可关闭自动预览。",
		result.RenderedCount, result.SlideCount, result.OutputDir)
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
