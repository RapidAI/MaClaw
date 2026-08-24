package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

const (
	embeddingModelLockName     = ".embedding-downloading"
	embeddingModelSentinelName = ".embedding-initialized"
	embeddingModelLockTTL      = 24 * time.Hour
	embeddingModelHTTPTimeout  = 2 * time.Hour
	embeddingModelDownloadEnv  = "HUBCENTER_EMBEDDING_MODEL_URL"
	embeddingModelAutoEnv      = "HUBCENTER_EMBEDDING_AUTO"
	embeddingModelAttempts     = 3
)

var (
	llmEmbeddingDataDir      string
	embeddingDownloading     atomic.Bool
	embeddingAutoStarted     atomic.Bool
	embeddingWarmStarted     atomic.Bool
	embeddingWarmDone        atomic.Bool
	embeddingWarmWG          sync.WaitGroup
	embeddingModelHTTPClient = &http.Client{Timeout: embeddingModelHTTPTimeout}
)

type embeddingModelFileView struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	Available   bool   `json:"available"`
	Path        string `json:"path,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
}

type embeddingModelRuntimeStatus struct {
	Status            string                   `json:"status"`
	ModelDir          string                   `json:"model_dir,omitempty"`
	ServingPath       string                   `json:"serving_path,omitempty"`
	DownloadURL       string                   `json:"download_url,omitempty"`
	LogPath           string                   `json:"log_path,omitempty"`
	Initialized       bool                     `json:"initialized"`
	Downloading       bool                     `json:"downloading"`
	Ready             bool                     `json:"ready"`
	EmbedderReady     bool                     `json:"embedder_ready"`
	Warming           bool                     `json:"warming,omitempty"`
	DownloadSupported bool                     `json:"download_supported"`
	TriggerSupported  bool                     `json:"trigger_supported"`
	ExpectedFiles     []string                 `json:"expected_files"`
	MissingFiles      []string                 `json:"missing_files"`
	Files             []embeddingModelFileView `json:"files"`
	LogTail           []string                 `json:"log_tail"`
	LastDownloadError string                   `json:"last_download_error,omitempty"`
}

// SetLLMEmbeddingDataDir sets the HubCenter data directory used for the
// embedding GGUF cache ({dataDir}/models). The live embedder still loads from
// embedding.DefaultModelPath (~/.maclaw/models); downloads are copied there.
func SetLLMEmbeddingDataDir(dir string) {
	llmEmbeddingDataDir = strings.TrimSpace(dir)
}

func embeddingAutoDownloadEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(embeddingModelAutoEnv)))
	if raw == "0" || raw == "false" || raw == "off" {
		return false
	}
	if testing.Testing() && raw != "1" && raw != "true" && raw != "on" {
		return false
	}
	return true
}

// EnsureEmbeddingModelDownload starts a background fetch when the GGUF is
// missing. It is a no-op if a download is already running, the file is already
// cached, or this process already kicked one off. Manual trigger still retries.
func EnsureEmbeddingModelDownload() {
	if !embeddingAutoDownloadEnabled() {
		_ = syncEmbeddingModelCopies()
		warmSharedEmbeddingModel()
		return
	}
	if embeddingDownloading.Load() || embeddingModelLockActive() {
		return
	}
	_ = syncEmbeddingModelCopies()
	if modelFileReady(embeddingModelCachePath()) || modelFileReady(embedding.DefaultModelPath()) {
		warmSharedEmbeddingModel()
		return
	}
	if !embeddingAutoStarted.CompareAndSwap(false, true) {
		return
	}
	started, err := startEmbeddingModelDownload(false)
	if err != nil {
		embeddingAutoStarted.Store(false)
		log.Printf("[embedding-model] auto download failed to start: %v", err)
		return
	}
	if !started {
		embeddingAutoStarted.Store(false)
		return
	}
	log.Printf("[embedding-model] auto download started")
}

func adminEmbeddingModelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, collectEmbeddingModelStatus())
}

func adminEmbeddingModelTrigger(w http.ResponseWriter, r *http.Request) {
	status := collectEmbeddingModelStatus()
	if status.Downloading || status.Warming {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"started": false,
			"status":  status,
			"message": "embedding model download is already running",
		})
		return
	}
	if status.Ready && status.EmbedderReady {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"started": false,
			"status":  status,
			"message": "embedding model is already cached",
		})
		return
	}
	force := status.Ready && !status.EmbedderReady
	started, err := startEmbeddingModelDownload(force)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EMBEDDING_MODEL_DOWNLOAD", err.Error())
		return
	}
	message := "embedding model download started in background"
	if !started {
		message = "embedding model download is already running"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"started": started,
		"status":  collectEmbeddingModelStatus(),
		"message": message,
	})
}

func collectEmbeddingModelStatus() embeddingModelRuntimeStatus {
	_ = syncEmbeddingModelCopies()
	name := embedding.DefaultModelFilename
	cachePath := embeddingModelCachePath()
	servingPath := embedding.DefaultModelPath()
	resolved := firstExistingModelFile(cachePath, servingPath)
	item := embeddingModelFileView{
		Name:        name,
		DownloadURL: embeddingModelSourceURL(),
		Path:        resolved,
	}
	missing := []string{}
	if fi, err := os.Stat(resolved); err == nil && !fi.IsDir() && fi.Size() > 0 {
		item.Available = true
		item.SizeBytes = fi.Size()
		item.ModifiedAt = fi.ModTime().UTC().Format(time.RFC3339)
	} else {
		missing = append(missing, name)
	}
	ready := len(missing) == 0
	downloading := embeddingDownloading.Load() || embeddingModelLockActive()
	initialized := fileExists(filepath.Join(embeddingModelCacheDir(), embeddingModelSentinelName))
	if ready && !downloading {
		warmSharedEmbeddingModel()
	}
	embedderReady := embedding.SharedGemmaReady()
	warming := embeddingModelWarming() && !embedderReady
	status := "missing"
	switch {
	case downloading:
		status = "downloading"
	case warming:
		status = "partial"
	case ready && embedderReady:
		status = "ready"
	case ready || initialized:
		status = "partial"
	}
	logPath := embeddingModelLogPath()
	return embeddingModelRuntimeStatus{
		Status:            status,
		ModelDir:          embeddingModelCacheDir(),
		ServingPath:       servingPath,
		DownloadURL:       embeddingModelSourceURL(),
		LogPath:           logPath,
		Initialized:       initialized,
		Downloading:       downloading,
		Ready:             ready,
		EmbedderReady:     embedderReady,
		Warming:           warming,
		DownloadSupported: true,
		TriggerSupported:  true,
		ExpectedFiles:     []string{name},
		MissingFiles:      missing,
		Files:             []embeddingModelFileView{item},
		LogTail:           readLogTail(logPath, 20),
		LastDownloadError: lastEmbeddingDownloadError(logPath),
	}
}

func startEmbeddingModelDownload(force bool) (bool, error) {
	if !embeddingDownloading.CompareAndSwap(false, true) {
		return false, nil
	}
	cacheDir := embeddingModelCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		embeddingDownloading.Store(false)
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(embeddingModelLogPath()), 0o755); err != nil {
		embeddingDownloading.Store(false)
		return false, err
	}
	lockPath := filepath.Join(cacheDir, embeddingModelLockName)
	clearStaleEmbeddingModelLock(lockPath)
	if embeddingModelLockActive() {
		embeddingDownloading.Store(false)
		return false, nil
	}
	lockFile, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		embeddingDownloading.Store(false)
		if os.IsExist(err) {
			return false, nil
		}
		return false, err
	}
	_, _ = fmt.Fprintf(lockFile, "%d\n", os.Getpid())
	_ = lockFile.Close()
	go runEmbeddingModelDownload(force)
	return true, nil
}

func runEmbeddingModelDownload(force bool) {
	defer embeddingDownloading.Store(false)
	cacheDir := embeddingModelCacheDir()
	lockPath := filepath.Join(cacheDir, embeddingModelLockName)
	defer func() { _ = os.Remove(lockPath) }()

	logPath := embeddingModelLogPath()
	cachePath := embeddingModelCachePath()
	urls := embeddingModelSourceURLs()
	var lastErr error
	for _, srcURL := range urls {
		appendEmbeddingLog(logPath, "download start url="+srcURL)
		if err := downloadEmbeddingModelFileWithRetry(context.Background(), srcURL, cachePath, force); err != nil {
			lastErr = err
			appendEmbeddingLog(logPath, "download failed: "+err.Error())
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		embeddingAutoStarted.Store(false)
		return
	}
	if err := syncEmbeddingModelCopies(); err != nil {
		appendEmbeddingLog(logPath, "copy failed: "+err.Error())
		return
	}
	_ = os.WriteFile(filepath.Join(cacheDir, embeddingModelSentinelName), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
	if emb := embedding.ReloadSharedGemmaIfReady(); embedding.IsNoop(emb) {
		appendEmbeddingLog(logPath, "model cached; embedder still not ready (invalid or unloadable GGUF)")
	} else {
		appendEmbeddingLog(logPath, "model cached; embedder ready")
	}
	appendEmbeddingLog(logPath, "download done")
}

func downloadEmbeddingModelFile(ctx context.Context, srcURL, dest string, force bool) error {
	if srcURL == "" {
		return fmt.Errorf("empty embedding model url")
	}
	if !force {
		if fi, err := os.Stat(dest); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	var resume int64
	if fi, err := os.Stat(tmp); err == nil && !fi.IsDir() && fi.Size() > 0 {
		resume = fi.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return err
	}
	if resume > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(resume, 10)+"-")
	}
	resp, err := embeddingModelHTTPClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && resume > 0 {
		resp.Body.Close()
		_ = os.Remove(tmp)
		resume = 0
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
		if err != nil {
			return err
		}
		resp, err = embeddingModelHTTPClient.Do(req)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		resume = 0
	case http.StatusPartialContent:
	default:
		return fmt.Errorf("download status %d", resp.StatusCode)
	}
	var out *os.File
	if resume > 0 {
		out, err = os.OpenFile(tmp, os.O_WRONLY|os.O_APPEND, 0o644)
	} else {
		out, err = os.Create(tmp)
	}
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceRegularFile(tmp, dest)
}

func syncEmbeddingModelCopies() error {
	cachePath := embeddingModelCachePath()
	servingPath := embedding.DefaultModelPath()
	if servingPath == "" || filepath.Clean(cachePath) == filepath.Clean(servingPath) {
		return nil
	}
	cacheOK := modelFileReady(cachePath)
	servingOK := modelFileReady(servingPath)
	switch {
	case cacheOK && servingOK:
		if embeddingModelFilesMatch(cachePath, servingPath) {
			return nil
		}
		if embeddingModelNewer(servingPath, cachePath) {
			return copyRegularFile(servingPath, cachePath)
		}
		return copyRegularFile(cachePath, servingPath)
	case cacheOK:
		return copyRegularFile(cachePath, servingPath)
	case servingOK:
		return copyRegularFile(servingPath, cachePath)
	default:
		return nil
	}
}

func embeddingModelFilesMatch(a, b string) bool {
	if a == "" || b == "" || filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil || fa.IsDir() || fb.IsDir() {
		return false
	}
	return fa.Size() == fb.Size() && fa.ModTime().Equal(fb.ModTime())
}

func embeddingModelNewer(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	if fa.ModTime().After(fb.ModTime()) {
		return true
	}
	return fa.ModTime().Equal(fb.ModTime()) && fa.Size() > fb.Size()
}

func copyRegularFile(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	if src == dst || src == "" || dst == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".copy"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := replaceRegularFile(tmp, dst); err != nil {
		return err
	}
	if fi, err := os.Stat(src); err == nil {
		_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	}
	return nil
}

func replaceRegularFile(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmp, dest)
}

func embeddingModelCacheDir() string {
	if dir := strings.TrimSpace(llmEmbeddingDataDir); dir != "" {
		return filepath.Join(dir, "models")
	}
	if dir := embedding.DefaultModelsDir(); dir != "" {
		return dir
	}
	return filepath.Join(".", "data", "models")
}

func embeddingModelCachePath() string {
	return filepath.Join(embeddingModelCacheDir(), embedding.DefaultModelFilename)
}

func embeddingModelLogPath() string {
	if dir := strings.TrimSpace(llmEmbeddingDataDir); dir != "" {
		return filepath.Join(dir, "logs", "embedding-model-download.log")
	}
	return filepath.Join(filepath.Dir(embeddingModelCacheDir()), "logs", "embedding-model-download.log")
}

func embeddingModelSourceURL() string {
	urls := embeddingModelSourceURLs()
	if len(urls) == 0 {
		return embedding.DefaultModelDownloadURL
	}
	return urls[0]
}

func embeddingModelSourceURLs() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		out = append(out, raw)
	}
	if raw := strings.TrimSpace(os.Getenv(embeddingModelDownloadEnv)); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			add(part)
		}
	}
	add(embedding.DefaultModelDownloadURL)
	add(githubReleaseMirrorURL(embedding.DefaultModelDownloadURL))
	return out
}

func githubReleaseMirrorURL(src string) string {
	if !strings.HasPrefix(src, "https://github.com/") {
		return ""
	}
	return "https://ghfast.top/" + src
}

func downloadEmbeddingModelFileWithRetry(ctx context.Context, srcURL, dest string, force bool) error {
	var lastErr error
	for attempt := 1; attempt <= embeddingModelAttempts; attempt++ {
		lastErr = downloadEmbeddingModelFile(ctx, srcURL, dest, force)
		if lastErr == nil {
			return nil
		}
		if !embeddingDownloadTransient(lastErr) || attempt == embeddingModelAttempts {
			return lastErr
		}
		appendEmbeddingLog(embeddingModelLogPath(), fmt.Sprintf("retry %d/%d url=%s: %s", attempt, embeddingModelAttempts, srcURL, lastErr.Error()))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
	return lastErr
}

func embeddingDownloadTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "tls handshake")
}

func clearStaleEmbeddingModelLock(path string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}
	if time.Since(info.ModTime()) > embeddingModelLockTTL {
		_ = os.Remove(path)
	}
}

func embeddingModelLockActive() bool {
	path := filepath.Join(embeddingModelCacheDir(), embeddingModelLockName)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if time.Since(info.ModTime()) > embeddingModelLockTTL {
		_ = os.Remove(path)
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return embeddingDownloading.Load()
	}
	raw := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(raw)
	if err != nil {
		// Legacy timestamp locks are only valid in this process.
		if embeddingDownloading.Load() {
			return true
		}
		_ = os.Remove(path)
		return false
	}
	if embeddingProcessAlive(pid) {
		return true
	}
	_ = os.Remove(path)
	return false
}

func embeddingProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func firstExistingModelFile(paths ...string) string {
	for _, path := range paths {
		if modelFileReady(path) {
			return path
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func modelFileReady(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

func lastEmbeddingDownloadError(logPath string) string {
	tail := readLogTail(logPath, 80)
	var lastErr string
	seenDone := false
	for i := len(tail) - 1; i >= 0; i-- {
		line := strings.TrimSpace(tail[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "download start") {
			return lastErr
		}
		if strings.Contains(lower, "download done") {
			if lastErr != "" {
				return lastErr
			}
			seenDone = true
			continue
		}
		if strings.Contains(lower, "download failed") || strings.Contains(lower, "copy failed") || strings.Contains(lower, "still not ready") {
			if lastErr != "" {
				continue
			}
			if seenDone && !strings.Contains(lower, "still not ready") {
				return ""
			}
			lastErr = line
		}
	}
	return lastErr
}

func embeddingWarmEnabled() bool {
	if !testing.Testing() {
		return true
	}
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(embeddingModelAutoEnv)))
	return raw == "1" || raw == "true" || raw == "on"
}

func embeddingModelWarming() bool {
	return embeddingWarmStarted.Load() && !embeddingWarmDone.Load()
}

func warmSharedEmbeddingModel() {
	if !embeddingWarmEnabled() || embedding.SharedGemmaReady() {
		return
	}
	_ = syncEmbeddingModelCopies()
	if !modelFileReady(embeddingModelCachePath()) && !modelFileReady(embedding.DefaultModelPath()) {
		return
	}
	if !embeddingWarmStarted.CompareAndSwap(false, true) {
		return
	}
	embeddingWarmDone.Store(false)
	embeddingWarmWG.Add(1)
	go func() {
		defer embeddingWarmWG.Done()
		defer embeddingWarmDone.Store(true)
		if emb := embedding.ReloadSharedGemmaIfReady(); embedding.IsNoop(emb) {
			appendEmbeddingLog(embeddingModelLogPath(), "model cached; embedder still not ready (invalid or unloadable GGUF)")
			log.Printf("[embedding-model] cached GGUF did not load")
			return
		}
		log.Printf("[embedding-model] embedder ready")
	}()
}

func appendEmbeddingLog(path, line string) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(line) == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(line))
	_ = f.Close()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func readLogTail(path string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, limit)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	if len(out) > limit {
		out = append([]string(nil), out[len(out)-limit:]...)
	}
	return out
}
