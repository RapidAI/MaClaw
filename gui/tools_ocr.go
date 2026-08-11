package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
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
		Description: "Recognize text in an image using the built-in local PP-OCRv6 engine (fully offline, no Python or cloud). " +
			"Provide image_path (local PNG/JPEG file) or image_base64 (base64-encoded PNG/JPEG, data-URI allowed) — at least one is required. " +
			"Returns recognized text regions with bounding boxes [x,y,w,h] and confidence. " +
			"When the full result is needed downstream (e.g. saving to a file), pass output_path: the complete result is written there directly, avoiding truncation of large in-context tool results.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"ocr", "image", "text", "recognition"},
		Priority: 5,
		Status:   RegToolAvailable,
		InputSchema: map[string]interface{}{
			"image_path":   map[string]interface{}{"type": "string", "description": "Local file path to a PNG/JPEG image"},
			"image_base64": map[string]interface{}{"type": "string", "description": "Base64-encoded PNG/JPEG image (data-URI prefix allowed)"},
			"output_path":  map[string]interface{}{"type": "string", "description": "Optional: write the full recognition result text to this local file instead of returning it inline"},
		},
		Source: "builtin:ocr",
		Handler: func(args map[string]interface{}) string {
			return runOCRRecognizeTool(app, args)
		},
	})
}

func runOCRRecognizeTool(app *App, args map[string]interface{}) string {
	if app != nil && !app.GetOCREnabled() {
		return "OCR is disabled in settings (ocr_enabled=false). Enable OCR first to use ocr_recognize."
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
