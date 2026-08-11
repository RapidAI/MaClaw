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

// TestModelDownloadHandlerServesOCRModelsZip requests the OCR models bundle
// through the handler's path validation and extension allowlist.
func TestModelDownloadHandlerServesOCRModelsZip(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	modelsDir := filepath.Join(root, "data", "models")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}

	name := ocr.ModelsZipFilename
	if !strings.HasSuffix(name, ".zip") {
		t.Fatalf("OCR hub model %q is not a .zip file", name)
	}
	if !isAllowedModelFilename(name) {
		t.Fatalf("OCR hub model %q rejected by isAllowedModelFilename", name)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, name), []byte("zip:"+name), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	h := ModelDownloadHandler(filepath.Join(configDir, "config.yaml"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/"+name, nil)
	req.SetPathValue("filename", name)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", name, rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "zip:"+name {
		t.Fatalf("%s body=%q", name, got)
	}
}

// TestModelDownloadHandlerRejectsOCRTraversal makes sure ../ traversal against
// OCR model names is still rejected even though .zip/.onnx are allowlisted.
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
		{"dotdot prefix", "../ocr-models.zip", http.StatusBadRequest},
		{"nested dotdot", "nested/../../ocr-models.zip", http.StatusBadRequest},
		{"backslash", `..\ocr-models.zip`, http.StatusBadRequest},
		{"subdir", "subdir/ocr-models.zip", http.StatusBadRequest},
		{"double extension trick", "ocr-models.zip.txt", http.StatusForbidden},
		{"no extension", "ocr-models", http.StatusForbidden},
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
// carries the OCR models zip and that it survives expected-files filtering.
func TestDefaultHubModelFilesIncludesOCRModels(t *testing.T) {
	t.Setenv("HUB_MODEL_FILES", "")
	expected := modelDownloadExpectedFiles()
	set := make(map[string]bool, len(expected))
	for _, name := range expected {
		set[name] = true
	}
	if !set[ocr.ModelsZipFilename] {
		t.Fatalf("default hub model files missing %s (have %v)", ocr.ModelsZipFilename, expected)
	}
}
