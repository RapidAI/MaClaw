package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

// TestModelDownloadHandlerServesOCRArtifacts requests each of the 6 PP-OCRv6
// ONNX filenames (det+rec for tiny/small/medium) through the handler's path
// validation and extension allowlist.
func TestModelDownloadHandlerServesOCRArtifacts(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	modelsDir := filepath.Join(root, "data", "models")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}

	names := defaultHubOCRModelFiles()
	if len(names) != 6 {
		t.Fatalf("defaultHubOCRModelFiles = %v, want 6 entries", names)
	}
	h := ModelDownloadHandler(filepath.Join(configDir, "config.yaml"))
	for _, name := range names {
		if !strings.HasSuffix(name, ".onnx") {
			t.Fatalf("OCR hub model %q is not an .onnx file", name)
		}
		if !isAllowedModelFilename(name) {
			t.Fatalf("OCR hub model %q rejected by isAllowedModelFilename", name)
		}
		if err := os.WriteFile(filepath.Join(modelsDir, name), []byte("onnx:"+name), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/models/"+name, nil)
		req.SetPathValue("filename", name)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", name, rr.Code, rr.Body.String())
		}
		if got := rr.Body.String(); got != "onnx:"+name {
			t.Fatalf("%s body=%q", name, got)
		}
	}
}

// TestModelDownloadHandlerRejectsOCRTraversal makes sure ../ traversal against
// OCR model names is still rejected even though .onnx is allowlisted.
func TestModelDownloadHandlerRejectsOCRTraversal(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	h := ModelDownloadHandler(filepath.Join(configDir, "config.yaml"))

	for _, tc := range []struct {
		name     string
		filename string
		wantCode int
	}{
		{"dotdot prefix", "../ppocrv6_small_det.onnx", http.StatusBadRequest},
		{"nested dotdot", "nested/../../ppocrv6_small_rec.onnx", http.StatusBadRequest},
		{"backslash", `..\ppocrv6_small_det.onnx`, http.StatusBadRequest},
		{"subdir", "subdir/ppocrv6_small_det.onnx", http.StatusBadRequest},
		{"double extension trick", "ppocrv6_small_det.onnx.txt", http.StatusForbidden},
		{"no extension", "ppocrv6_small_det", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/models/"+tc.filename, nil)
			req.SetPathValue("filename", tc.filename)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.wantCode {
				t.Fatalf("status=%d body=%s, want %d", rr.Code, rr.Body.String(), tc.wantCode)
			}
		})
	}
}

// TestDefaultHubModelFilesIncludesOCRModels confirms the default prefetch list
// carries all 6 OCR artifacts and that they survive expected-files filtering.
func TestDefaultHubModelFilesIncludesOCRModels(t *testing.T) {
	t.Setenv("HUB_MODEL_FILES", "")
	expected := modelDownloadExpectedFiles()
	set := make(map[string]bool, len(expected))
	for _, name := range expected {
		set[name] = true
	}
	for _, tier := range []string{"tiny", "small", "medium"} {
		for _, name := range []string{ocr.DetModelFilename(tier), ocr.RecModelFilename(tier)} {
			if !set[name] {
				t.Fatalf("default hub model files missing %s", name)
			}
		}
	}
}
