package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func setupEmbeddingModelTest(t *testing.T) (dataDir, homeDir string) {
	t.Helper()
	ResetEmbeddingModelDownloadForTest()
	t.Cleanup(ResetEmbeddingModelDownloadForTest)
	embedding.ResetSharedGemmaForTest()
	t.Cleanup(embedding.ResetSharedGemmaForTest)

	dataDir = t.TempDir()
	homeDir = t.TempDir()
	SetLLMEmbeddingDataDir(dataDir)
	prev, _ := embedding.BaseDirFunc.Load().(func() string)
	embedding.BaseDirFunc.Store(func() string { return homeDir })
	t.Cleanup(func() {
		SetLLMEmbeddingDataDir("")
		if prev != nil {
			embedding.BaseDirFunc.Store(prev)
		}
	})
	return dataDir, homeDir
}

func ResetEmbeddingModelDownloadForTest() {
	embeddingWarmWG.Wait()
	embeddingDownloading.Store(false)
	embeddingAutoStarted.Store(false)
	embeddingWarmStarted.Store(false)
	embeddingWarmDone.Store(false)
	_ = os.Unsetenv(embeddingModelDownloadEnv)
	_ = os.Unsetenv(embeddingModelAutoEnv)
}

func TestEmbeddingModelStatusMissing(t *testing.T) {
	setupEmbeddingModelTest(t)

	rec := httptest.NewRecorder()
	adminEmbeddingModelStatus(rec, httptest.NewRequest(http.MethodGet, "/api/admin/model_download/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got embeddingModelRuntimeStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "missing" || got.Ready || got.Downloading {
		t.Fatalf("status=%+v", got)
	}
	if got.TriggerSupported != true || len(got.ExpectedFiles) != 1 || got.ExpectedFiles[0] != embedding.DefaultModelFilename {
		t.Fatalf("expected embedding file listing, got %+v", got)
	}
	if len(got.MissingFiles) != 1 {
		t.Fatalf("missing=%v", got.MissingFiles)
	}
}

func TestEmbeddingModelStatusReportsExistingCache(t *testing.T) {
	dataDir, homeDir := setupEmbeddingModelTest(t)
	cachePath := filepath.Join(dataDir, "models", embedding.DefaultModelFilename)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("cached-gguf"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	adminEmbeddingModelStatus(rec, httptest.NewRequest(http.MethodGet, "/api/admin/model_download/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got embeddingModelRuntimeStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Ready || got.Status != "partial" || got.EmbedderReady {
		t.Fatalf("unloadable cache should be partial, got %+v", got)
	}
	if embeddingWarmStarted.Load() || embedding.SharedGemmaReady() {
		t.Fatal("status must peek embedder readiness, not load Gemma")
	}
	serving := filepath.Join(homeDir, "models", embedding.DefaultModelFilename)
	data, err := os.ReadFile(serving)
	if err != nil {
		t.Fatalf("serving copy missing: %v", err)
	}
	if string(data) != "cached-gguf" {
		t.Fatalf("serving copy = %q", data)
	}
}

func TestEmbeddingModelTriggerDownloadsAndCopies(t *testing.T) {
	dataDir, homeDir := setupEmbeddingModelTest(t)
	payload := []byte("fake-embedding-gguf")
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+embedding.DefaultModelFilename {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(src.Close)
	t.Setenv(embeddingModelDownloadEnv, src.URL+"/"+embedding.DefaultModelFilename)

	runEmbeddingModelDownload(false)

	cachePath := filepath.Join(dataDir, "models", embedding.DefaultModelFilename)
	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache missing: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("cache = %q", got)
	}
	serving := filepath.Join(homeDir, "models", embedding.DefaultModelFilename)
	got, err = os.ReadFile(serving)
	if err != nil {
		t.Fatalf("serving missing: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("serving = %q", got)
	}

	rec := httptest.NewRecorder()
	adminEmbeddingModelStatus(rec, httptest.NewRequest(http.MethodGet, "/api/admin/model_download/status", nil))
	var status embeddingModelRuntimeStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Ready || status.Status != "partial" || status.EmbedderReady {
		t.Fatalf("after fake download status=%+v", status)
	}

	skip := httptest.NewRecorder()
	adminEmbeddingModelTrigger(skip, httptest.NewRequest(http.MethodPost, "/api/admin/model_download/trigger", nil))
	if skip.Code != http.StatusOK {
		t.Fatalf("trigger ready status=%d body=%s", skip.Code, skip.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(skip.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["started"] != true {
		t.Fatalf("expected started=true when cached file is not loadable, got %v", resp)
	}
	waitEmbeddingJobsSettled(t)
}

// waitEmbeddingJobsSettled blocks until any background download/warm job has
// finished writing into the TempDir roots. Without this, t.TempDir cleanup can
// race a still-running goroutine (Windows RemoveAll fails with
// "directory is not empty"), because cleanup functions run LIFO and the
// registered reset-and-wait cleanup executes after the directory removal.
func waitEmbeddingJobsSettled(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && embeddingDownloading.Load() {
		time.Sleep(20 * time.Millisecond)
	}
	embeddingWarmWG.Wait()
}

func TestEmbeddingModelTriggerStartsBackgroundDownload(t *testing.T) {
	setupEmbeddingModelTest(t)
	started := make(chan struct{})
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bg-gguf"))
	}))
	t.Cleanup(src.Close)
	t.Setenv(embeddingModelDownloadEnv, src.URL+"/"+embedding.DefaultModelFilename)

	rec := httptest.NewRecorder()
	adminEmbeddingModelTrigger(rec, httptest.NewRequest(http.MethodPost, "/api/admin/model_download/trigger", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("trigger status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["started"] != true {
		t.Fatalf("expected started=true, got %v", resp)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("background download did not start")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !embeddingDownloading.Load() && collectEmbeddingModelStatus().Ready {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("download did not finish: %+v", collectEmbeddingModelStatus())
}

func TestEnsureEmbeddingModelDownloadStartsWhenMissing(t *testing.T) {
	setupEmbeddingModelTest(t)
	started := make(chan struct{})
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("auto-gguf"))
	}))
	t.Cleanup(src.Close)
	t.Setenv(embeddingModelDownloadEnv, src.URL+"/"+embedding.DefaultModelFilename)
	t.Setenv(embeddingModelAutoEnv, "1")

	EnsureEmbeddingModelDownload()
	EnsureEmbeddingModelDownload()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("auto download did not start")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !embeddingDownloading.Load() && collectEmbeddingModelStatus().Ready {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("auto download did not finish: %+v", collectEmbeddingModelStatus())
}

func TestEnsureEmbeddingModelDownloadSkipsWhenAlreadyStarted(t *testing.T) {
	setupEmbeddingModelTest(t)
	hit := 0
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should-not-download"))
	}))
	t.Cleanup(src.Close)
	t.Setenv(embeddingModelDownloadEnv, src.URL+"/"+embedding.DefaultModelFilename)
	embeddingAutoStarted.Store(true)
	EnsureEmbeddingModelDownload()
	time.Sleep(50 * time.Millisecond)
	if hit != 0 {
		t.Fatalf("already-started process must not fetch again, hits=%d", hit)
	}
}

func TestEnsureEmbeddingModelDownloadDisabledInTests(t *testing.T) {
	setupEmbeddingModelTest(t)
	hit := 0
	src := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit++ }))
	t.Cleanup(src.Close)
	t.Setenv(embeddingModelDownloadEnv, src.URL+"/"+embedding.DefaultModelFilename)
	EnsureEmbeddingModelDownload()
	time.Sleep(30 * time.Millisecond)
	if hit != 0 || embeddingDownloading.Load() {
		t.Fatalf("unit tests must not auto-fetch GitHub/models, hits=%d downloading=%v", hit, embeddingDownloading.Load())
	}
}

func TestEnsureEmbeddingModelDownloadDisabledStillSyncsCache(t *testing.T) {
	dataDir, homeDir := setupEmbeddingModelTest(t)
	cachePath := filepath.Join(dataDir, "models", embedding.DefaultModelFilename)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("sync-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	EnsureEmbeddingModelDownload()
	if embeddingWarmStarted.Load() {
		t.Fatal("unit tests must not warm Gemma")
	}
	serving := filepath.Join(homeDir, "models", embedding.DefaultModelFilename)
	got, err := os.ReadFile(serving)
	if err != nil {
		t.Fatalf("disabled auto-download must still copy cache to serving: %v", err)
	}
	if string(got) != "sync-me" {
		t.Fatalf("serving copy = %q", got)
	}
}

func TestEnsureEmbeddingModelDownloadKeepsExistingCache(t *testing.T) {
	dataDir, _ := setupEmbeddingModelTest(t)
	cachePath := filepath.Join(dataDir, "models", embedding.DefaultModelFilename)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	hit := 0
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("replaced"))
	}))
	t.Cleanup(src.Close)
	t.Setenv(embeddingModelDownloadEnv, src.URL+"/"+embedding.DefaultModelFilename)
	t.Setenv(embeddingModelAutoEnv, "1")
	EnsureEmbeddingModelDownload()
	if embeddingDownloading.Load() {
		t.Fatal("existing cache must not start a download job")
	}
	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep-me" || hit != 0 {
		t.Fatalf("auto sync must not replace existing file: %q hits=%d", got, hit)
	}
	if !embeddingWarmStarted.Load() {
		t.Fatal("existing cache must start a background embedder warm")
	}
}

func TestDownloadEmbeddingModelFileResumesPart(t *testing.T) {
	setupEmbeddingModelTest(t)
	payload := []byte("hello-resume-world")
	var sawRange bool
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=6-" {
			sawRange = true
			w.Header().Set("Content-Range", "bytes 6-17/18")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[6:])
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(src.Close)
	dest := filepath.Join(t.TempDir(), embedding.DefaultModelFilename)
	if err := os.WriteFile(dest+".part", payload[:6], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadEmbeddingModelFile(t.Context(), src.URL+"/"+embedding.DefaultModelFilename, dest, false); err != nil {
		t.Fatal(err)
	}
	if !sawRange {
		t.Fatal("expected Range resume")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("resumed file = %q", got)
	}
}

func TestDownloadEmbeddingModelFileRewritesAfterRangeRejected(t *testing.T) {
	setupEmbeddingModelTest(t)
	payload := []byte("rewrite-after-416")
	var sawRetry bool
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		sawRetry = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(src.Close)
	dest := filepath.Join(t.TempDir(), embedding.DefaultModelFilename)
	if err := os.WriteFile(dest+".part", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadEmbeddingModelFile(t.Context(), src.URL+"/"+embedding.DefaultModelFilename, dest, false); err != nil {
		t.Fatal(err)
	}
	if !sawRetry {
		t.Fatal("expected full retry after 416")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("rewritten file = %q", got)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("stale part should be gone: %v", err)
	}
}

func TestLastEmbeddingDownloadErrorIgnoresOlderFailure(t *testing.T) {
	setupEmbeddingModelTest(t)
	logPath := embeddingModelLogPath()
	appendEmbeddingLog(logPath, "download failed: boom")
	appendEmbeddingLog(logPath, "download done")
	if got := lastEmbeddingDownloadError(logPath); got != "" {
		t.Fatalf("sticky error after success: %q", got)
	}
	appendEmbeddingLog(logPath, "download failed: later")
	if got := lastEmbeddingDownloadError(logPath); !strings.Contains(got, "later") {
		t.Fatalf("got %q", got)
	}
	appendEmbeddingLog(logPath, "download start url=https://example.invalid/model.gguf")
	if got := lastEmbeddingDownloadError(logPath); got != "" {
		t.Fatalf("retry must clear stale error: %q", got)
	}
	appendEmbeddingLog(logPath, "model cached; embedder still not ready (invalid or unloadable GGUF)")
	appendEmbeddingLog(logPath, "download done")
	if got := lastEmbeddingDownloadError(logPath); !strings.Contains(got, "still not ready") {
		t.Fatalf("unloadable GGUF must stay visible after download done, got %q", got)
	}
}

func TestLastEmbeddingDownloadErrorReportsWarmupFailure(t *testing.T) {
	setupEmbeddingModelTest(t)
	logPath := embeddingModelLogPath()
	appendEmbeddingLog(logPath, "model cached; embedder still not ready (invalid or unloadable GGUF)")
	if got := lastEmbeddingDownloadError(logPath); !strings.Contains(got, "still not ready") {
		t.Fatalf("warmup failure must be visible without download done, got %q", got)
	}
}

func TestSyncEmbeddingModelCopiesRefreshesStaleServing(t *testing.T) {
	_, homeDir := setupEmbeddingModelTest(t)
	cachePath := embeddingModelCachePath()
	servingPath := filepath.Join(homeDir, "models", embedding.DefaultModelFilename)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(servingPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servingPath, []byte("old-serving"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(cachePath, []byte("new-cache-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncEmbeddingModelCopies(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(servingPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-cache-bytes" {
		t.Fatalf("stale serving was not refreshed: %q", got)
	}
	if err := syncEmbeddingModelCopies(); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(servingPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-cache-bytes" {
		t.Fatalf("second sync ping-ponged serving: %q", got)
	}
	got, err = os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-cache-bytes" {
		t.Fatalf("second sync overwrote cache: %q", got)
	}
}

func TestSyncEmbeddingModelCopiesPrefersNewerServing(t *testing.T) {
	_, homeDir := setupEmbeddingModelTest(t)
	cachePath := embeddingModelCachePath()
	servingPath := filepath.Join(homeDir, "models", embedding.DefaultModelFilename)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(servingPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("old-cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(servingPath, []byte("new-serving-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncEmbeddingModelCopies(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-serving-file" {
		t.Fatalf("newer serving should win: %q", got)
	}
}

func TestDownloadEmbeddingModelFileReplacesExistingDest(t *testing.T) {
	setupEmbeddingModelTest(t)
	payload := []byte("new-gguf-bytes")
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(src.Close)
	dest := filepath.Join(t.TempDir(), embedding.DefaultModelFilename)
	if err := os.WriteFile(dest, []byte("old-unloadable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadEmbeddingModelFile(t.Context(), src.URL+"/"+embedding.DefaultModelFilename, dest, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("replaced file = %q", got)
	}
}

func TestEmbeddingModelLockClearsDeadPID(t *testing.T) {
	setupEmbeddingModelTest(t)
	cacheDir := embeddingModelCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(cacheDir, embeddingModelLockName)
	if err := os.WriteFile(lockPath, []byte("2147483646\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if embeddingModelLockActive() {
		t.Fatal("dead PID lock must not look active")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("dead PID lock should be removed: %v", err)
	}
}

func TestEmbeddingModelLockHoldsCurrentPID(t *testing.T) {
	setupEmbeddingModelTest(t)
	cacheDir := embeddingModelCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(cacheDir, embeddingModelLockName)
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !embeddingModelLockActive() {
		t.Fatal("current PID lock must stay active")
	}
}

func TestEmbeddingModelLockClearsLegacyTimestamp(t *testing.T) {
	setupEmbeddingModelTest(t)
	cacheDir := embeddingModelCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(cacheDir, embeddingModelLockName)
	if err := os.WriteFile(lockPath, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644); err != nil {
		t.Fatal(err)
	}
	if embeddingModelLockActive() {
		t.Fatal("legacy timestamp lock must not block a new process")
	}
}

func TestEmbeddingModelTriggerReportsAlreadyRunning(t *testing.T) {
	setupEmbeddingModelTest(t)
	cacheDir := embeddingModelCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, embeddingModelLockName), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	adminEmbeddingModelTrigger(rec, httptest.NewRequest(http.MethodPost, "/api/admin/model_download/trigger", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["started"] != false {
		t.Fatalf("lock must not start a second job: %v", resp)
	}
}

func TestEmbeddingModelSourceURLsIncludeGitHubMirror(t *testing.T) {
	setupEmbeddingModelTest(t)
	urls := embeddingModelSourceURLs()
	if len(urls) < 2 {
		t.Fatalf("want github + mirror, got %v", urls)
	}
	if urls[0] != embedding.DefaultModelDownloadURL {
		t.Fatalf("primary = %s", urls[0])
	}
	if !strings.HasPrefix(urls[1], "https://ghfast.top/https://github.com/") {
		t.Fatalf("mirror = %s", urls[1])
	}
	t.Setenv(embeddingModelDownloadEnv, "https://mirror.example/model.gguf, https://github.com/RapidAI/MaClaw/releases/download/Model_Release/embeddinggemma-300M-Q8_0.gguf")
	urls = embeddingModelSourceURLs()
	if urls[0] != "https://mirror.example/model.gguf" {
		t.Fatalf("env override should be first: %v", urls)
	}
	if urls[1] != embedding.DefaultModelDownloadURL {
		t.Fatalf("github should still be listed: %v", urls)
	}
}

func TestDownloadEmbeddingModelFileFallsBackToNextURL(t *testing.T) {
	setupEmbeddingModelTest(t)
	payload := []byte("mirror-gguf-bytes")
	var primaryHits, mirrorHits int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		http.Error(w, "upstream reset", http.StatusBadGateway)
	}))
	t.Cleanup(primary.Close)
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(mirror.Close)
	t.Setenv(embeddingModelDownloadEnv, primary.URL+"/model.gguf,"+mirror.URL+"/model.gguf")
	t.Setenv(embeddingModelAutoEnv, "1")

	started, err := startEmbeddingModelDownload(false)
	if err != nil || !started {
		t.Fatalf("start=%v err=%v", started, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for embeddingDownloading.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if embeddingDownloading.Load() {
		t.Fatal("download still running")
	}
	got, err := os.ReadFile(embeddingModelCachePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("fallback file = %q", got)
	}
	if primaryHits == 0 || mirrorHits == 0 {
		t.Fatalf("hits primary=%d mirror=%d", primaryHits, mirrorHits)
	}
}

func TestEmbeddingDownloadTransient(t *testing.T) {
	if !embeddingDownloadTransient(fmt.Errorf("read tcp 1.2.3.4:443: connection reset by peer")) {
		t.Fatal("reset should retry")
	}
	if !embeddingDownloadTransient(fmt.Errorf("context deadline exceeded (Client.Timeout)")) {
		t.Fatal("timeout should retry")
	}
	if embeddingDownloadTransient(fmt.Errorf("download status 404")) {
		t.Fatal("404 should not retry the same URL")
	}
}
