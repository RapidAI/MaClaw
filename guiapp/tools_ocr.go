package guiapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/formula"
)

// registerOCRTools registers the native OCR recognition tool. The tool uses
// the shared process-wide PP-OCRv6 provider, so it never duplicates model
// memory with computer use / GUI automation / browser tasks.
func registerOCRTools(registry *ToolRegistry, app *App) {
	if registry == nil {
		return
	}

	registry.Register(RegisteredTool{
		Name: "ocr_recognize",
		Description: "Recognize text with the built-in local PP-OCRv6 engine (fully offline, no Python or cloud). " +
			"Set mode to formula to recognize one printed formula image as LaTeX with the local FormulaNet model. " +
			"Provide image_path (local PNG/JPEG, or a scanned PDF) or image_base64 (base64-encoded PNG/JPEG, data-URI allowed) — at least one is required. " +
			"Formula mode accepts one PNG, JPEG, or GIF, not a PDF. " +
			"A PDF is read from embedded page images, 8 image pages at a time. Pass output_path and, on the next call, the start_page named in the result. " +
			"Do not install rapidocr, paddleocr, tesseract, or ocrmypdf for this. " +
			"Text mode returns recognized text regions with bounding boxes [x,y,w,h] and confidence. Formula mode returns LaTeX. " +
			"When the full result is needed downstream (e.g. saving to a file), pass output_path: the complete result is written there directly, avoiding truncation of large in-context tool results.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"ocr", "image", "text", "recognition", "formula", "latex"},
		Priority: 5,
		Status:   RegToolAvailable,
		InputSchema: map[string]interface{}{
			"image_path":   map[string]interface{}{"type": "string", "description": "Local PNG/JPEG, or a scanned PDF. PDF results go to output_path. Formula mode does not accept a PDF."},
			"image_base64": map[string]interface{}{"type": "string", "description": "Base64-encoded PNG/JPEG image (data-URI prefix allowed). Formula mode also accepts GIF."},
			"output_path":  map[string]interface{}{"type": "string", "description": "Write the recognition text here. Required for a PDF. Later PDF pages append when start_page > 1. Formula mode writes the LaTeX string."},
			"start_page":   map[string]interface{}{"type": "integer", "description": "1-based PDF page to start from. Use the next start_page from the previous result. Ignored for a single image."},
			"mode":         map[string]interface{}{"type": "string", "description": "text (default) runs PP-OCRv6. formula runs FormulaNet and returns LaTeX for one image."},
		},
		Source: "builtin:ocr",
		Handler: func(args map[string]interface{}) string {
			return runOCRRecognizeTool(app, args)
		},
	})
}

func runOCRRecognizeTool(app *App, args map[string]interface{}) string {
	mode, modeErr := ocrRecognizeMode(args)
	if modeErr != nil {
		return "ocr_recognize: " + modeErr.Error()
	}
	if mode == "formula" {
		return runOCRFormula(app, args)
	}
	if app != nil && !app.GetOCREnabled() {
		return "OCR is disabled in settings (ocr_enabled=false). Enable OCR first to use ocr_recognize."
	}
	if path := strings.TrimSpace(stringVal(args, "image_path")); strings.EqualFold(filepath.Ext(path), ".pdf") {
		return runOCRRecognizePDF(app, path, args)
	}

	b64, err := ocrToolImageBase64(args)
	if err != nil {
		return "ocr_recognize: " + err.Error()
	}

	if app != nil {
		if _, _, ok := app.ensureOCRModelFiles(); !ok {
			return "OCR model files are not present yet; a background download of the PP-OCRv6 models has been started. Please retry shortly."
		}
	} else if !sharedNativeOCRProvider().IsAvailable() {
		return "OCR model files are not installed yet. Please retry shortly."
	}

	// The provider caps the long edge internally (downscale + bbox rescale), so
	// the base64 payload is handed over as-is.
	results, err := sharedNativeOCRProvider().Recognize(b64)
	if err != nil {
		return fmt.Sprintf("ocr_recognize failed: %v", err)
	}
	text := browser.FormatOCRForLLM(results)

	// output_path: persist the full result to a file so large outputs never
	// depend on (truncated) in-context tool results.
	if outPath := strings.TrimSpace(stringVal(args, "output_path")); outPath != "" {
		written, wErr := writeOCRResultFile(outPath, text)
		if wErr != nil {
			return fmt.Sprintf("ocr_recognize: write output file: %v", wErr)
		}
		return fmt.Sprintf("OCR 完成：%d 个文本区域，完整结果已写入 %s（%d 字节）。", len(results), written, len(text))
	}
	return text
}

func ocrRecognizeMode(args map[string]interface{}) (string, error) {
	switch strings.ToLower(strings.TrimSpace(stringVal(args, "mode"))) {
	case "", "text", "ocr":
		return "text", nil
	case "formula", "latex":
		return "formula", nil
	default:
		return "", fmt.Errorf("mode must be text or formula")
	}
}

func runOCRFormula(app *App, args map[string]interface{}) string {
	if app != nil && !app.GetFormulaEnabled() {
		return "Formula recognition is disabled in settings (formula_enabled=false). Enable it in Settings → AI models → OCR."
	}
	if rejected := formulaModeArgsRejected(args); rejected != "" {
		return rejected
	}
	var eng *formula.Engine
	if app != nil {
		ready, err := app.ensureFormulaReady()
		if err != nil {
			if errors.Is(err, errFormulaNotReady) {
				return "Formula model files are not present yet; a background download of FormulaNet has been started. Please retry shortly."
			}
			if errors.Is(err, errFormulaRepairing) {
				return "Formula model files failed verification; a repair has been started. Please retry shortly."
			}
			return fmt.Sprintf("ocr_recognize failed: %v", err)
		}
		eng = ready
	}
	if eng == nil {
		return "ocr_recognize: formula engine is not available"
	}
	img, err := formulaImageFromToolArgs(args)
	if err != nil {
		return "ocr_recognize: " + err.Error()
	}
	latex, err := eng.Recognize(img)
	if err != nil {
		return fmt.Sprintf("ocr_recognize failed: %v", err)
	}
	if strings.TrimSpace(latex) == "" {
		return "ocr_recognize: 没有识别出公式。"
	}
	if outPath := strings.TrimSpace(stringVal(args, "output_path")); outPath != "" {
		written, wErr := writeOCRResultFile(outPath, latex)
		if wErr != nil {
			return fmt.Sprintf("ocr_recognize: write output file: %v", wErr)
		}
		return fmt.Sprintf("公式识别完成，LaTeX 已写入 %s（%d 字节）。", written, len(latex))
	}
	return latex
}

func formulaModeArgsRejected(args map[string]interface{}) string {
	path := strings.TrimSpace(stringVal(args, "image_path"))
	out := strings.TrimSpace(stringVal(args, "output_path"))
	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		return "ocr_recognize: formula 模式只接受一张 PNG、JPEG 或 GIF，不接受 PDF"
	}
	if out != "" && strings.EqualFold(filepath.Ext(out), ".pdf") {
		return "ocr_recognize: output_path 要写成 .txt。公式结果是 LaTeX 文本，不是 PDF。"
	}
	if path != "" && out != "" && ocrSamePath(path, out) {
		return "ocr_recognize: output_path 不能是源图片。请改用旁边的 .txt 文件。"
	}
	return ""
}

func formulaImageFromToolArgs(args map[string]interface{}) (image.Image, error) {
	path := strings.TrimSpace(stringVal(args, "image_path"))
	if path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat image file: %w", err)
		}
		if info.Size() > ocrToolMaxImageBytes {
			return nil, fmt.Errorf("image file too large: %d bytes (max %d)", info.Size(), ocrToolMaxImageBytes)
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read image file: %w", err)
		}
		defer f.Close()
		img, err := formula.DecodeImage(f)
		if err != nil {
			return nil, err
		}
		return img, nil
	}
	b64, err := ocrToolImageBase64(args)
	if err != nil {
		return nil, err
	}
	payload := b64
	if i := strings.Index(payload, ","); i >= 0 && strings.HasPrefix(strings.ToLower(payload[:i]), "data:") {
		payload = payload[i+1:]
	}
	payload = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, payload)
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil {
		return nil, fmt.Errorf("decode image_base64: %w", err)
	}
	img, err := formula.DecodeImage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// writeOCRResultFile writes the OCR result text to path, creating parent
// directories as needed, via a <path>.tmp sibling + atomic rename (same
// pattern as the model file installers).
func writeOCRResultFile(path, content string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if dir := filepath.Dir(abs); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create directory: %w", err)
		}
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("install: %w", err)
	}
	return abs, nil
}

// ocrPDFMaxBytes is the on-disk cap for a scanned PDF passed to ocr_recognize.
// It is higher than the 32 MiB text-inject cap: page images are OCR'd to a
// file and are not injected into the chat turn.
const ocrPDFMaxBytes = agent.OCRPDFMaxBytes

func runOCRRecognizePDF(app *App, path string, args map[string]interface{}) string {
	outPath := strings.TrimSpace(stringVal(args, "output_path"))
	if outPath == "" {
		return "ocr_recognize: 扫描版 PDF 必须传 output_path，识别结果写入该文件。不要安装其他 OCR 引擎。"
	}
	if rejected := ocrOutputPathRejected(path, outPath); rejected != "" {
		return rejected
	}
	info, err := os.Stat(path)
	if err != nil {
		return "ocr_recognize: stat pdf: " + err.Error()
	}
	if info.Size() > ocrPDFMaxBytes {
		return "ocr_recognize: 该 PDF 超过内置识别的 256 MiB 上限。请拆分后再识别。不要安装其他 OCR 引擎。"
	}
	// A page already in the transcript is done. Skip model load and OCR so a
	// retried start_page does not download an engine or overwrite the file.
	startPage := ocrPositiveInt(args, "start_page", 1, 0)
	if receipt, ok := ocrTranscriptAlreadyCovers(path, outPath, startPage); ok {
		return receipt
	}
	if app != nil {
		if _, _, ok := app.ensureOCRModelFiles(); !ok {
			return "OCR model files are not present yet; a background download of the PP-OCRv6 models has been started. Please retry shortly."
		}
	} else if !sharedNativeOCRProvider().IsAvailable() {
		return "OCR model files are not installed yet. Please retry shortly."
	}
	tree, treeErr := agent.OpenCachedPDFPageTree(path)
	if treeErr != nil {
		if errors.Is(treeErr, agent.ErrOfficeReadSourceChanged) {
			return "ocr_recognize: 读取过程中源 PDF 变了。请重新调用。不要安装其他 OCR 引擎。"
		}
		return "ocr_recognize: read pdf: " + treeErr.Error()
	}
	previous, readErr := os.ReadFile(outPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return "ocr_recognize: read output file: " + readErr.Error()
	}
	// Stop before the next finished page so this call does not OCR it again.
	endPage := agent.OCRTranscriptNextDonePage(string(previous), startPage)
	batch, _, err := tree.OCRImagePages(startPage, agent.OCRPDFPageBatchMax, endPage, func(imageBase64 string) (string, error) {
		results, recErr := sharedNativeOCRProvider().Recognize(imageBase64)
		if recErr != nil {
			return "", recErr
		}
		// Document text, not the click-target boxes FormatOCRForLLM emits.
		return joinBrowserOCRText(results), nil
	}, false)
	if errors.Is(err, agent.ErrOCRPDFNoPageImage) {
		return "ocr_recognize: 该 PDF 没有可识别的页面图片。它不是扫描件，或图片抽不出来。不要拆分文件，不要安装其他 OCR 引擎。"
	}
	if err != nil {
		return "ocr_recognize: 未能用本机 PP-OCRv6 读取该 PDF 的页面图片。不要安装其他 OCR 引擎。"
	}
	if strings.TrimSpace(batch.Text) == "" && batch.Recognized == 0 && batch.NextPage == 0 {
		return ocrRecognizePastEnd(outPath, batch.TotalPages)
	}
	// Merge keeps finished pages, fills gaps, and replaces an extract-error
	// page. start_page=1 must not wipe a transcript that already has later pages.
	merged := agent.MergeOCRTranscript(string(previous), batch.Text)
	written, wErr := writeOCRResultFile(outPath, merged)
	if wErr != nil {
		return "ocr_recognize: write output file: " + wErr.Error()
	}
	return ocrTranscriptReceipt(written, merged, batch.TotalPages, batch.Recognized, false)
}

// ocrOutputPathRejected stops the tool from writing the transcript onto the
// source scan, or into any path whose name says it is a PDF. This tool writes
// text. A text-version PDF is produced from that text afterwards.
func ocrOutputPathRejected(pdfPath, outPath string) string {
	if ocrSamePath(pdfPath, outPath) {
		return "ocr_recognize: output_path 不能是源 PDF。请改用旁边的 .txt 文件。不要安装其他 OCR 引擎。"
	}
	if strings.EqualFold(filepath.Ext(outPath), ".pdf") {
		return "ocr_recognize: output_path 要写成 .txt。这里写的是识别正文，不是新的 PDF。不要安装其他 OCR 引擎。"
	}
	return ""
}

func ocrSamePath(a, b string) bool {
	left := ocrAbsPath(a)
	right := ocrAbsPath(b)
	if left == "" || right == "" {
		return false
	}
	if strings.EqualFold(left, right) {
		return true
	}
	li, err1 := os.Stat(left)
	ri, err2 := os.Stat(right)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(li, ri)
}

func ocrAbsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

// ocrRecognizePastEnd is the reply when start_page is past the last PDF page.
// A hole in the transcript is still open: the book is not finished.
func ocrRecognizePastEnd(outPath string, total int) string {
	text := ""
	if previous, err := os.ReadFile(outPath); err == nil {
		text = string(previous)
	}
	next := agent.OCRTranscriptFirstMissing(text)
	if next > 0 && (total <= 0 || next <= total) {
		return ocrTranscriptReceipt(outPath, text, total, 0, true)
	}
	if total > 0 {
		return fmt.Sprintf("ocr_recognize: 已到文件末尾（共 %d 页）。不要安装其他 OCR 引擎。", total)
	}
	return "ocr_recognize: 已到文件末尾。不要安装其他 OCR 引擎。"
}

// ocrTranscriptAlreadyCovers reports that this start_page is already in the
// transcript. A retried call must not OCR those pages again or overwrite
// the pages already written. A new output_path starts over.
func ocrTranscriptAlreadyCovers(pdfPath, outPath string, startPage int) (string, bool) {
	existing, err := os.ReadFile(outPath)
	if err != nil || len(existing) == 0 {
		return "", false
	}
	text := string(existing)
	if !agent.OCRTranscriptPageDone(text, startPage) {
		return "", false
	}
	// A hole means the book is not finished. Skip the full PDF parse.
	total := 0
	next := agent.OCRTranscriptFirstMissing(text)
	if next > agent.OCRTranscriptMaxPage(text) {
		if tree, treeErr := agent.OpenCachedPDFPageTree(pdfPath); treeErr == nil && tree != nil {
			if n := tree.PageCount(); n > 0 {
				total = n
			}
		}
	}
	return ocrTranscriptReceipt(outPath, text, total, 0, true), true
}

func ocrTranscriptReceipt(outPath, transcript string, total, recognized int, already bool) string {
	abs, absErr := filepath.Abs(outPath)
	if absErr != nil {
		abs = outPath
	}
	next := agent.OCRTranscriptFirstMissing(transcript)
	if next < 1 {
		next = 1
	}
	if total > 0 && next > total {
		if already {
			return fmt.Sprintf("OCR 完成（本机 PP-OCRv6）：%s 已有全部 %d 页，已到文件末尾。不要重复识别，不要安装其他 OCR 引擎。", abs, total)
		}
		return fmt.Sprintf("OCR 完成（本机 PP-OCRv6）：已写入 %s，本批识别 %d 页，共 %d 页，已到文件末尾。不要安装其他 OCR 引擎。", abs, recognized, total)
	}
	if already {
		done := agent.OCRTranscriptMaxPage(transcript)
		// A later finished page must not be the page the model calls next.
		// The hole in front of it is the open page.
		if done > next {
			return fmt.Sprintf("OCR 完成（本机 PP-OCRv6）：%s 第 %d 页还没有正文。下一调用 start_page=%d。不要重复识别，不要安装其他 OCR 引擎。", abs, next, next)
		}
		return fmt.Sprintf("OCR 完成（本机 PP-OCRv6）：%s 第 %d 页已有正文。下一调用 start_page=%d。不要重复识别，不要安装其他 OCR 引擎。", abs, done, next)
	}
	if total > 0 {
		return fmt.Sprintf("OCR 完成（本机 PP-OCRv6）：已写入 %s，本批识别 %d 页，共 %d 页。下一调用 start_page=%d。不要安装其他 OCR 引擎。", abs, recognized, total, next)
	}
	return fmt.Sprintf("OCR 完成（本机 PP-OCRv6）：已写入 %s，本批识别 %d 页。下一调用 start_page=%d。不要安装其他 OCR 引擎。", abs, recognized, next)
}

func ocrPositiveInt(args map[string]interface{}, key string, def, max int) int {
	value, ok := args[key]
	if !ok || value == nil {
		return def
	}
	n := 0
	switch typed := value.(type) {
	case int:
		n = typed
	case int64:
		n = int(typed)
	case float64:
		n = int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return def
		}
		n = parsed
	default:
		return def
	}
	if n < 1 {
		return def
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

// ocrToolMaxImageBytes caps the image input size accepted by ocr_recognize.
// Matches mobileDocumentSourceMaxBytes (25 MiB) — LLM tool arguments are
// already capped well below this, but image_path is read from disk
// server-side and needs its own bound.
const ocrToolMaxImageBytes = 25 * 1024 * 1024

// ocrToolImageBase64 resolves the tool input to a base64 image payload.
// Data-URI prefixes are intentionally left in place: the OCR provider's image
// preparation strips them.
func ocrToolImageBase64(args map[string]interface{}) (string, error) {
	if b64 := strings.TrimSpace(stringVal(args, "image_base64")); b64 != "" {
		if len(b64) > ocrToolMaxImageBytes*2 {
			return "", fmt.Errorf("image_base64 too large: %d chars (max %d)", len(b64), ocrToolMaxImageBytes*2)
		}
		return b64, nil
	}
	path := strings.TrimSpace(stringVal(args, "image_path"))
	if path == "" {
		return "", fmt.Errorf("either image_path or image_base64 is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat image file: %w", err)
	}
	if info.Size() > ocrToolMaxImageBytes {
		return "", fmt.Errorf("image file too large: %d bytes (max %d)", info.Size(), ocrToolMaxImageBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read image file: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
