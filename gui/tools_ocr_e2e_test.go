package main

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

// ocrRealModelsSourceDir locates the PP-OCRv6 small det/rec models captured
// during engine bring-up. Tests needing the real engine skip when absent.
func ocrRealModelsSourceDir(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", ".tmp", "ocr-models")
	for _, name := range []string{ocr.DetModelFilename("small"), ocr.RecModelFilename("small")} {
		if _, err := os.Stat(filepath.Join(src, name)); err != nil {
			t.Skipf("real OCR models not available at %s: %v", src, err)
		}
	}
	return src
}

// installRealOCRModels copies the real small-tier models into the test models
// dir (redirected via embedding.BaseDirFunc) and returns that dir.
func installRealOCRModels(t *testing.T) string {
	t.Helper()
	src := ocrRealModelsSourceDir(t)
	dir := withOCRTestModelsDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{ocr.DetModelFilename("small"), ocr.RecModelFilename("small")} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func ocrToolForTest(t *testing.T, app *App) RegisteredTool {
	t.Helper()
	registry := NewToolRegistry()
	registerOCRTools(registry, app)
	tool, ok := registry.Get("ocr_recognize")
	if !ok {
		t.Fatal("ocr_recognize not registered")
	}
	return *tool
}

func TestOCRRecognizeToolEndToEnd(t *testing.T) {
	withOCRTestHome(t)
	installRealOCRModels(t)
	isolateSharedOCRProvider(t)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	tool := ocrToolForTest(t, app)

	imgPath := filepath.Join("..", "corelib", "ocr", "testdata", "en.png")
	out := tool.Handler(map[string]interface{}{"image_path": imgPath})
	if !strings.Contains(out, "Hello World 123") {
		t.Fatalf("image_path result missing expected text: %q", out)
	}

	data, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	out = tool.Handler(map[string]interface{}{
		"image_base64": "data:image/png;base64," + base64.StdEncoding.EncodeToString(data),
	})
	if !strings.Contains(out, "Hello World 123") {
		t.Fatalf("image_base64 result missing expected text: %q", out)
	}
}

func TestOCRRecognizeToolDisabledConfig(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: false})
	tool := ocrToolForTest(t, app)
	out := tool.Handler(map[string]interface{}{"image_path": "whatever.png"})
	if !strings.Contains(out, "OCR is disabled") {
		t.Fatalf("disabled-config message = %q", out)
	}
}

func TestOCRRecognizeToolModelMissing(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t) // empty: models not installed
	isolateSharedOCRProvider(t)

	// Point the kicked-off background download at a dead server so the
	// goroutine exits quickly instead of hitting the real HuggingFace.
	hf := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	withOCRModelURLs(t, hf.URL+"/det", hf.URL+"/rec")

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	tool := ocrToolForTest(t, app)
	imgPath := filepath.Join("..", "corelib", "ocr", "testdata", "en.png")
	out := tool.Handler(map[string]interface{}{"image_path": imgPath})
	if !strings.Contains(out, "not present yet") {
		t.Fatalf("model-missing message = %q", out)
	}
}

func TestOCRRecognizeToolRequiresImageInput(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	tool := ocrToolForTest(t, app)
	out := tool.Handler(map[string]interface{}{})
	if !strings.Contains(out, "image_path or image_base64 is required") {
		t.Fatalf("missing-input message = %q", out)
	}
}
