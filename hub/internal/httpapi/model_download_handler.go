package httpapi

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const defaultHubModelFiles = "embeddinggemma-300M-Q8_0.gguf moonshine-base-zh.gguf omniparser-v2.yolow kokoro-v1_0.koro kokoro_82m_selected_voices_koro.zip"

type hubModelFileView struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	Available   bool   `json:"available"`
	DownloadURL string `json:"download_url,omitempty"`
}

type hubModelRuntimeStatus struct {
	Status            string             `json:"status"`
	ModelDir          string             `json:"model_dir"`
	LegacyDataDir     string             `json:"legacy_data_dir"`
	PublicModelsURL   string             `json:"public_models_url"`
	LogPath           string             `json:"log_path"`
	DownloadScript    string             `json:"download_script"`
	Initialized       bool               `json:"initialized"`
	Downloading       bool               `json:"downloading"`
	Ready             bool               `json:"ready"`
	DownloadSupported bool               `json:"download_supported"`
	TriggerSupported  bool               `json:"trigger_supported"`
	ExpectedFiles     []string           `json:"expected_files"`
	MissingFiles      []string           `json:"missing_files"`
	Files             []hubModelFileView `json:"files"`
	LogTail           []string           `json:"log_tail"`
	LastDownloadError string             `json:"last_download_error,omitempty"`
}

func resolveHubDataDir(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	candidates := []string{}
	if configPath != "" {
		configDir := filepath.Dir(configPath)
		candidates = append(candidates,
			filepath.Clean(filepath.Join(configDir, "..", "data")),
			filepath.Clean(filepath.Join(configDir, "data")),
		)
	}
	candidates = append(candidates, filepath.Clean("./data"))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return candidates[0]
}

func resolveHubModelDirs(configPath string) (string, string, string, string) {
	dataDir := resolveHubDataDir(configPath)
	modelsDir := filepath.Join(dataDir, "models")
	logPath := filepath.Join(dataDir, "logs", "model-download.log")
	scriptPath := filepath.Join(dataDir, "download-models.sh")
	return dataDir, modelsDir, logPath, scriptPath
}

func modelDownloadExpectedFiles() []string {
	raw := strings.TrimSpace(os.Getenv("HUB_MODEL_FILES"))
	if raw == "" {
		raw = defaultHubModelFiles
	}
	parts := strings.Fields(raw)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func resolveModelPublicPath(modelsDir string, legacyDataDir string, filename string) string {
	candidates := []string{
		filepath.Join(modelsDir, filename),
		filepath.Join(legacyDataDir, filename),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return candidates[0]
}

// isAllowedModelExtension checks if a filename has a permitted model file extension.
// Allows model artifacts distributed through the hub public model endpoint.
func isAllowedModelExtension(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".gguf") || strings.HasSuffix(lower, ".yolow") || strings.HasSuffix(lower, ".koro") || strings.HasSuffix(lower, ".zip")
}

// ModelDownloadHandler serves model files while keeping the public URL stable.
// Files are primarily stored under data/models, with legacy fallback to data/.
// Only allows downloading files with permitted extensions for safety.
// GET /api/v1/models/{filename}
func ModelDownloadHandler(configPath string) http.HandlerFunc {
	legacyDataDir, modelsDir, _, _ := resolveHubModelDirs(configPath)
	return func(w http.ResponseWriter, r *http.Request) {
		filename := r.PathValue("filename")
		if filename == "" {
			http.Error(w, "missing filename", http.StatusBadRequest)
			return
		}
		if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}
		if !isAllowedModelExtension(filename) {
			http.Error(w, "unsupported model file extension", http.StatusForbidden)
			return
		}

		filePath := resolveModelPublicPath(modelsDir, legacyDataDir, filename)
		fi, err := os.Stat(filePath)
		if err != nil || fi.IsDir() {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
		http.ServeFile(w, r, filePath)
	}
}

func PublicModelDownloadStatusHandler(configPath string) http.HandlerFunc {
	legacyDataDir, modelsDir, logPath, scriptPath := resolveHubModelDirs(configPath)
	return func(w http.ResponseWriter, r *http.Request) {
		status := collectHubModelRuntimeStatus(r, legacyDataDir, modelsDir, logPath, scriptPath)
		status.TriggerSupported = false
		status.DownloadScript = ""
		status.LogPath = ""
		status.LogTail = nil
		status.LastDownloadError = ""
		writeJSON(w, http.StatusOK, status)
	}
}

func GetAdminModelDownloadStatusHandler(configPath string) http.HandlerFunc {
	legacyDataDir, modelsDir, logPath, scriptPath := resolveHubModelDirs(configPath)
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, collectHubModelRuntimeStatus(r, legacyDataDir, modelsDir, logPath, scriptPath))
	}
}

func TriggerAdminModelDownloadHandler(configPath string) http.HandlerFunc {
	legacyDataDir, modelsDir, logPath, scriptPath := resolveHubModelDirs(configPath)
	return func(w http.ResponseWriter, r *http.Request) {
		status := collectHubModelRuntimeStatus(r, legacyDataDir, modelsDir, logPath, scriptPath)
		if status.Downloading {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      true,
				"started": false,
				"status":  status,
				"message": "model download is already running",
			})
			return
		}
		if !status.TriggerSupported {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model download trigger is not available on this host"})
			return
		}

		lockPath := filepath.Join(modelsDir, ".models-downloading")
		sentinelPath := filepath.Join(modelsDir, ".models-initialized")
		args := append([]string{strings.TrimSpace(os.Getenv("HUB_MODEL_BASE_URL")), modelsDir, filepath.Join(os.Getenv("HOME"), ".maclaw", "models"), sentinelPath, lockPath}, modelDownloadExpectedFiles()...)
		if strings.TrimSpace(args[0]) == "" {
			args[0] = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release"
		}
		if err := os.MkdirAll(modelsDir, 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := startModelDownload(scriptPath, logPath, args...); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"started": true,
			"status":  collectHubModelRuntimeStatus(r, legacyDataDir, modelsDir, logPath, scriptPath),
			"message": "model download started in background",
		})
	}
}

func collectHubModelRuntimeStatus(r *http.Request, legacyDataDir string, modelsDir string, logPath string, scriptPath string) hubModelRuntimeStatus {
	expected := modelDownloadExpectedFiles()
	files := make([]hubModelFileView, 0, len(expected))
	missing := make([]string, 0)
	for _, name := range expected {
		path := resolveModelPublicPath(modelsDir, legacyDataDir, name)
		item := hubModelFileView{Name: name}
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			item.Available = true
			item.SizeBytes = fi.Size()
			item.ModifiedAt = fi.ModTime().UTC().Format(time.RFC3339)
			item.DownloadURL = modelDownloadURL(r, name)
		} else {
			missing = append(missing, name)
		}
		files = append(files, item)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	initialized := fileExists(filepath.Join(modelsDir, ".models-initialized"))
	downloading := fileExists(filepath.Join(modelsDir, ".models-downloading"))
	ready := len(expected) > 0 && len(missing) == 0
	status := "missing"
	switch {
	case downloading:
		status = "downloading"
	case ready && initialized:
		status = "ready"
	case len(files) != len(missing):
		status = "partial"
	case initialized:
		status = "partial"
	}
	lastErr := ""
	if tail := readLogTail(logPath, 120); len(tail) > 0 {
		for i := len(tail) - 1; i >= 0; i-- {
			line := strings.TrimSpace(tail[i])
			if line == "" {
				continue
			}
			lower := strings.ToLower(line)
			if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
				lastErr = line
				break
			}
		}
	}
	triggerSupported := runtime.GOOS != "windows" && fileExists(scriptPath)
	return hubModelRuntimeStatus{
		Status:            status,
		ModelDir:          modelsDir,
		LegacyDataDir:     legacyDataDir,
		PublicModelsURL:   modelDownloadURL(r, "{filename}"),
		LogPath:           logPath,
		DownloadScript:    scriptPath,
		Initialized:       initialized,
		Downloading:       downloading,
		Ready:             ready,
		DownloadSupported: true,
		TriggerSupported:  triggerSupported,
		ExpectedFiles:     expected,
		MissingFiles:      missing,
		Files:             files,
		LogTail:           readLogTail(logPath, 20),
		LastDownloadError: lastErr,
	}
}

func modelDownloadURL(r *http.Request, filename string) string {
	if r == nil {
		return "/api/v1/models/" + filename
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "/api/v1/models/" + filename
	}
	return scheme + "://" + host + "/api/v1/models/" + filename
}

func readLogTail(path string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	lines := make([]string, 0, limit)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) > limit {
			lines = append([]string(nil), lines[len(lines)-limit:]...)
		}
	}
	return lines
}

func startModelDownload(scriptPath string, logPath string, args ...string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("background model download trigger is only supported on unix-like hosts")
	}
	quoted := make([]string, 0, len(args)+2)
	quoted = append(quoted, shellQuote(scriptPath))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	cmdText := "nohup " + strings.Join(quoted, " ") + " >> " + shellQuote(logPath) + " 2>&1 &"
	cmd := exec.Command("sh", "-c", cmdText)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
