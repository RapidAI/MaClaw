package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	if err := os.WriteFile(filepath.Join(modelsDir, "sensevoice-small-q8.gguf"), []byte("model-binary"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	h := ModelDownloadHandler(filepath.Join(configDir, "config.yaml"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/sensevoice-small-q8.gguf", nil)
	req.SetPathValue("filename", "sensevoice-small-q8.gguf")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "model-binary" {
		t.Fatalf("body=%q", got)
	}
}

func TestModelDownloadHandlerServesKokoroArtifacts(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	modelsDir := filepath.Join(root, "data", "models")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	for name, body := range map[string]string{
		"kokoro-v1_0.koro":                    "kokoro-model",
		"kokoro_82m_selected_voices_koro.zip": "kokoro-voices",
	} {
		if err := os.WriteFile(filepath.Join(modelsDir, name), []byte(body), 0644); err != nil {
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
		if got := rr.Body.String(); got != body {
			t.Fatalf("%s body=%q want %q", name, got, body)
		}
	}
}

func TestModelDownloadHandlerServesCAMPlusArtifact(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	modelsDir := filepath.Join(root, "data", "models")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	const name = "campplus-cn-common.cmpg"
	if err := os.WriteFile(filepath.Join(modelsDir, name), []byte("cam++"), 0644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	h := ModelDownloadHandler(filepath.Join(configDir, "config.yaml"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/"+name, nil)
	req.SetPathValue("filename", name)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "cam++" {
		t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
	}
}

func TestModelDownloadHandlerRejectsInvalidPathAndUnsupportedExtension(t *testing.T) {
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
		{name: "path traversal", filename: "../sensevoice-small-q8.gguf", wantCode: http.StatusBadRequest},
		{name: "nested path", filename: "nested/sensevoice-small-q8.gguf", wantCode: http.StatusBadRequest},
		{name: "unsupported extension", filename: "notes.txt", wantCode: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/models/"+tc.filename, nil)
			req.SetPathValue("filename", tc.filename)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.wantCode {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		})
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
	if strings.Contains(body, "model-download.log") || strings.Contains(body, "secret log") || strings.Contains(body, "legacy_data_dir") || strings.Contains(body, "model_dir") {
		t.Fatalf("public payload leaked admin-only fields: %s", body)
	}
	if !strings.Contains(body, `"public_models_url":"https://hub.example.com/api/v1/models/{filename}"`) {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestAdminModelDownloadStatusTriggerSupportedWithoutExistingScript(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	h := GetAdminModelDownloadStatusHandler(filepath.Join(configDir, "config.yaml"))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model_download/status", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var body hubModelRuntimeStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if runtime.GOOS == "windows" {
		if body.TriggerSupported {
			t.Fatalf("trigger_supported=true on windows")
		}
		return
	}
	if !body.TriggerSupported {
		t.Fatalf("trigger_supported=false without pre-existing script")
	}
}

func TestEnsureModelDownloadScriptCreatesExecutableScript(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "data", "download-models.sh")
	if err := ensureModelDownloadScript(scriptPath); err != nil {
		t.Fatalf("ensure script: %v", err)
	}
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat script: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("script path is dir")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		t.Fatalf("script is not executable: mode=%v", info.Mode().Perm())
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	text := string(content)
	for _, want := range []string{modelDownloadScriptVersion, "is_allowed_model_file()", "Skip unsafe model filename", "curl -L --fail", "--retry-delay 2", "wget --tries=3", "rm -f \"$tmp\"", "touch \"$SENTINEL\"", "cleanup() { rm -f \"$LOCK_FILE\"; }"} {
		if !strings.Contains(text, want) {
			t.Fatalf("script missing %q in:\n%s", want, text)
		}
	}
}

func TestEnsureModelDownloadScriptUpgradesOldScript(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "data", "download-models.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho old\n"), 0644); err != nil {
		t.Fatalf("write old script: %v", err)
	}
	if err := ensureModelDownloadScript(scriptPath); err != nil {
		t.Fatalf("ensure script: %v", err)
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, modelDownloadScriptVersion) || strings.Contains(text, "echo old") {
		t.Fatalf("script was not upgraded:\n%s", text)
	}
}

func TestEnsureModelDownloadScriptFixesExistingScriptPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics are not useful on windows")
	}
	root := t.TempDir()
	scriptPath := filepath.Join(root, "data", "download-models.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n# "+modelDownloadScriptVersion+"\n"), 0644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := ensureModelDownloadScript(scriptPath); err != nil {
		t.Fatalf("ensure script: %v", err)
	}
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat script: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("script remained non-executable: mode=%v", info.Mode().Perm())
	}
}

func TestModelDownloadURLNormalizesForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/public/model_download/status", nil)
	req.Host = "internal.local"
	req.Header.Set("X-Forwarded-Proto", "javascript, https")
	req.Header.Set("X-Forwarded-Host", "hub.example.com, proxy.local")
	if got := modelDownloadURL(req, "a.gguf"); got != "http://hub.example.com/api/v1/models/a.gguf" {
		t.Fatalf("url=%q", got)
	}

	req.Header.Set("X-Forwarded-Proto", "https, http")
	if got := modelDownloadURL(req, "a.gguf"); got != "https://hub.example.com/api/v1/models/a.gguf" {
		t.Fatalf("url=%q", got)
	}

	req.Header.Set("X-Forwarded-Host", "bad host")
	req.Host = "also/bad"
	if got := modelDownloadURL(req, "a.gguf"); got != "/api/v1/models/a.gguf" {
		t.Fatalf("url=%q", got)
	}
}

func TestModelDownloadExpectedFilesFiltersUnsafeNames(t *testing.T) {
	oldFiles := os.Getenv("HUB_MODEL_FILES")
	defer func() { _ = os.Setenv("HUB_MODEL_FILES", oldFiles) }()
	_ = os.Setenv("HUB_MODEL_FILES", "safe.gguf ../escape.gguf nested/file.gguf safe.gguf note.txt voice.zip")

	got := modelDownloadExpectedFiles()
	want := []string{"safe.gguf", "voice.zip"}
	if len(got) != len(want) {
		t.Fatalf("files=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("files=%v want %v", got, want)
		}
	}
}

func TestTriggerAdminModelDownloadRejectsWhenNoValidFilesConfigured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("trigger is unsupported on windows")
	}
	oldFiles := os.Getenv("HUB_MODEL_FILES")
	defer func() { _ = os.Setenv("HUB_MODEL_FILES", oldFiles) }()
	_ = os.Setenv("HUB_MODEL_FILES", "../escape.gguf notes.txt nested/model.gguf")

	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	h := TriggerAdminModelDownloadHandler(filepath.Join(configDir, "config.yaml"))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/model_download/trigger", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "no valid hub model files configured") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCollectHubModelRuntimeStatusReadyWhenFilesExistWithoutSentinel(t *testing.T) {
	oldFiles := os.Getenv("HUB_MODEL_FILES")
	defer func() { _ = os.Setenv("HUB_MODEL_FILES", oldFiles) }()
	_ = os.Setenv("HUB_MODEL_FILES", "a.gguf b.zip")

	root := t.TempDir()
	modelsDir := filepath.Join(root, "data", "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	for _, name := range []string{"a.gguf", "b.zip"} {
		if err := os.WriteFile(filepath.Join(modelsDir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	status := collectHubModelRuntimeStatus(nil, filepath.Join(root, "data"), modelsDir, filepath.Join(root, "data", "logs", "model-download.log"), filepath.Join(root, "data", "download-models.sh"))
	if !status.Ready || status.Status != "ready" {
		t.Fatalf("status=%q ready=%v missing=%v", status.Status, status.Ready, status.MissingFiles)
	}
	if status.Initialized {
		t.Fatalf("initialized=true without sentinel")
	}
}

func TestCollectHubModelRuntimeStatusTreatsEmptyModelAsMissing(t *testing.T) {
	oldFiles := os.Getenv("HUB_MODEL_FILES")
	defer func() { _ = os.Setenv("HUB_MODEL_FILES", oldFiles) }()
	_ = os.Setenv("HUB_MODEL_FILES", "a.gguf")

	root := t.TempDir()
	modelsDir := filepath.Join(root, "data", "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, "a.gguf"), nil, 0644); err != nil {
		t.Fatalf("write empty model: %v", err)
	}

	status := collectHubModelRuntimeStatus(nil, filepath.Join(root, "data"), modelsDir, filepath.Join(root, "data", "logs", "model-download.log"), filepath.Join(root, "data", "download-models.sh"))
	if status.Ready || status.Status != "missing" || len(status.MissingFiles) != 1 || status.MissingFiles[0] != "a.gguf" {
		t.Fatalf("status=%q ready=%v missing=%v", status.Status, status.Ready, status.MissingFiles)
	}
	if len(status.Files) != 1 || status.Files[0].Available {
		t.Fatalf("files=%#v", status.Files)
	}
}

func TestCollectHubModelRuntimeStatusIgnoresStaleDownloadLock(t *testing.T) {
	oldFiles := os.Getenv("HUB_MODEL_FILES")
	oldTTL := os.Getenv("HUB_MODEL_LOCK_TTL")
	defer func() {
		_ = os.Setenv("HUB_MODEL_FILES", oldFiles)
		_ = os.Setenv("HUB_MODEL_LOCK_TTL", oldTTL)
	}()
	_ = os.Setenv("HUB_MODEL_FILES", "a.gguf")
	_ = os.Setenv("HUB_MODEL_LOCK_TTL", "1ms")

	root := t.TempDir()
	modelsDir := filepath.Join(root, "data", "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	lockPath := filepath.Join(modelsDir, ".models-downloading")
	if err := os.WriteFile(lockPath, []byte("old"), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes lock: %v", err)
	}

	status := collectHubModelRuntimeStatus(nil, filepath.Join(root, "data"), modelsDir, filepath.Join(root, "data", "logs", "model-download.log"), filepath.Join(root, "data", "download-models.sh"))
	if status.Downloading || status.Status == "downloading" {
		t.Fatalf("stale lock reported downloading: status=%q downloading=%v", status.Status, status.Downloading)
	}
}
