package main

import (
	"encoding/base64"
	"fmt"
	"os"
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
			"Returns recognized text regions with bounding boxes [x,y,w,h] and confidence.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"ocr", "image", "text", "recognition"},
		Priority: 5,
		Status:   RegToolAvailable,
		InputSchema: map[string]interface{}{
			"image_path":   map[string]interface{}{"type": "string", "description": "Local file path to a PNG/JPEG image"},
			"image_base64": map[string]interface{}{"type": "string", "description": "Base64-encoded PNG/JPEG image (data-URI prefix allowed)"},
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
	return browser.FormatOCRForLLM(results)
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
