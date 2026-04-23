package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelDownloadHandlerServesFromModelsDirKeepingPublicURL(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	modelsDir := filepath.Join(root, "data", "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, "moonshine-base-zh.gguf"), []byte("model-binary"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	h := ModelDownloadHandler(filepath.Join(configDir, "config.yaml"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/moonshine-base-zh.gguf", nil)
	req.SetPathValue("filename", "moonshine-base-zh.gguf")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "model-binary" {
		t.Fatalf("body=%q", got)
	}
}

func TestGetAdminModelDownloadStatusHandlerReportsModelsDirState(t *testing.T) {
	oldFiles := os.Getenv("HUB_MODEL_FILES")
	oldBaseURL := os.Getenv("HUB_MODEL_BASE_URL")
	defer func() {
		_ = os.Setenv("HUB_MODEL_FILES", oldFiles)
		_ = os.Setenv("HUB_MODEL_BASE_URL", oldBaseURL)
	}()
	_ = os.Setenv("HUB_MODEL_FILES", "a.gguf b.gguf")
	_ = os.Setenv("HUB_MODEL_BASE_URL", "https://example.com/models")

	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	modelsDir := filepath.Join(root, "data", "models")
	logsDir := filepath.Join(root, "data", "logs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, "a.gguf"), []byte("aaa"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, ".models-downloading"), []byte("1"), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "model-download.log"), []byte("download started\ndownload failed\n"), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	h := GetAdminModelDownloadStatusHandler(filepath.Join(configDir, "config.yaml"))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model_download/status", nil)
	req.Host = "hub.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var body hubModelRuntimeStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "downloading" {
		t.Fatalf("status=%q", body.Status)
	}
	if body.ModelDir != modelsDir {
		t.Fatalf("model_dir=%q want %q", body.ModelDir, modelsDir)
	}
	if body.PublicModelsURL != "https://hub.example.com/api/v1/models/{filename}" {
		t.Fatalf("public_models_url=%q", body.PublicModelsURL)
	}
	if len(body.MissingFiles) != 1 || body.MissingFiles[0] != "b.gguf" {
		t.Fatalf("missing_files=%v", body.MissingFiles)
	}
	if len(body.LogTail) != 2 {
		t.Fatalf("log_tail=%v", body.LogTail)
	}
	if body.LastDownloadError != "download failed" {
		t.Fatalf("last_download_error=%q", body.LastDownloadError)
	}
}

func TestPublicModelDownloadStatusHandlerHidesAdminOnlyFields(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	modelsDir := filepath.Join(root, "data", "models")
	logsDir := filepath.Join(root, "data", "logs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, "embeddinggemma-300M-Q8_0.gguf"), []byte("a"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, ".models-initialized"), []byte("1"), 0644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "model-download.log"), []byte("secret log\n"), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	h := PublicModelDownloadStatusHandler(filepath.Join(configDir, "config.yaml"))
	req := httptest.NewRequest(http.MethodGet, "/api/public/model_download/status", nil)
	req.Host = "hub.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "model-download.log") || strings.Contains(body, "secret log") {
		t.Fatalf("public payload leaked admin-only fields: %s", body)
	}
	if !strings.Contains(body, `"public_models_url":"https://hub.example.com/api/v1/models/{filename}"`) {
		t.Fatalf("unexpected body=%s", body)
	}
}
