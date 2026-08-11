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

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/diarization"
	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

const defaultHubModelBaseURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release"

// defaultHubModelFiles lists the model artifacts the hub prefetches from
// defaultHubModelBaseURL (the OCR PP-OCRv6 ONNX files of every tier ship
// bundled in ocr.ModelsZipFilename; see ocr.DefaultModelsZipURL).
var defaultHubModelFiles = strings.Join([]string{
	"embeddinggemma-300M-Q8_0.gguf",
	asr.DefaultModelFilename,
	diarization.DefaultCAMPlusFilename,
	"omniparser-v2.yolow",
	"kokoro-v1_0.koro",
	"kokoro_82m_selected_voices_koro_v2.zip",
	ocr.ModelsZipFilename,
}, " ")

const defaultHubModelLockTTL = 24 * time.Hour
const modelDownloadScriptVersion = "maclaw-model-download-v3"

type hubModelFileView struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	Available   bool   `json:"available"`
	DownloadURL string `json:"download_url,omitempty"`
}

type hubModelRuntimeStatus struct {
	Status            string             `json:"status"`
	ModelDir          string             `json:"model_dir,omitempty"`
	LegacyDataDir     string             `json:"legacy_data_dir,omitempty"`
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
		if !isAllowedModelFilename(part) {
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

func isAllowedModelFilename(filename string) bool {
	return filename != "" && !strings.Contains(filename, "/") && !strings.Contains(filename, "\\") && !strings.Contains(filename, "..") && isAllowedModelExtension(filename)
}

// isAllowedModelExtension checks if a filename has a permitted model file extension.
// Allows model artifacts distributed through the hub public model endpoint.
func isAllowedModelExtension(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".gguf") || strings.HasSuffix(lower, ".cmpg") || strings.HasSuffix(lower, ".yolow") || strings.HasSuffix(lower, ".koro") || strings.HasSuffix(lower, ".onnx") || strings.HasSuffix(lower, ".zip")
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
		status.ModelDir = ""
		status.LegacyDataDir = ""
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
		expectedFiles := modelDownloadExpectedFiles()
		if len(expectedFiles) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no valid hub model files configured"})
			return
		}

		lockPath := filepath.Join(modelsDir, ".models-downloading")
		sentinelPath := filepath.Join(modelsDir, ".models-initialized")
		args := append([]string{strings.TrimSpace(os.Getenv("HUB_MODEL_BASE_URL")), modelsDir, resolveUserModelDir(), sentinelPath, lockPath}, expectedFiles...)
		if strings.TrimSpace(args[0]) == "" {
			args[0] = defaultHubModelBaseURL
		}
		if err := os.MkdirAll(modelsDir, 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := ensureModelDownloadScript(scriptPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if isStaleModelDownloadLock(lockPath, time.Now()) {
			_ = os.Remove(lockPath)
		}
		lockFile, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if os.IsExist(err) {
				writeJSON(w, http.StatusOK, map[string]any{
					"ok":      true,
					"started": false,
					"status":  collectHubModelRuntimeStatus(r, legacyDataDir, modelsDir, logPath, scriptPath),
					"message": "model download is already running",
				})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		_, _ = lockFile.WriteString(time.Now().UTC().Format(time.RFC3339))
		_ = lockFile.Close()
		if err := startModelDownload(scriptPath, logPath, args...); err != nil {
			_ = os.Remove(lockPath)
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
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() && fi.Size() > 0 {
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
	downloading := fileExists(filepath.Join(modelsDir, ".models-downloading")) && !isStaleModelDownloadLock(filepath.Join(modelsDir, ".models-downloading"), time.Now())
	ready := len(expected) > 0 && len(missing) == 0
	status := "missing"
	switch {
	case downloading:
		status = "downloading"
	case ready:
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
	triggerSupported := runtime.GOOS != "windows"
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

func resolveUserModelDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".maclaw", "models")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".maclaw", "models")
	}
	return filepath.Join(".", ".maclaw", "models")
}

func modelDownloadLockTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("HUB_MODEL_LOCK_TTL"))
	if raw == "" {
		return defaultHubModelLockTTL
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultHubModelLockTTL
	}
	return d
}

func isStaleModelDownloadLock(path string, now time.Time) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return now.Sub(info.ModTime()) > modelDownloadLockTTL()
}

func ensureModelDownloadScript(scriptPath string) error {
	if fileExists(scriptPath) {
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), modelDownloadScriptVersion) {
			return ensureExecutable(scriptPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		return err
	}
	const script = `#!/bin/sh
# maclaw-model-download-v3
set -eu
BASE_URL="$1"
TARGET_DIR="$2"
HOME_DIR="$3"
SENTINEL="$4"
LOCK_FILE="$5"
shift 5
mkdir -p "$TARGET_DIR" "$HOME_DIR"
touch "$LOCK_FILE"
cleanup() { rm -f "$LOCK_FILE"; }
trap cleanup EXIT INT TERM
is_allowed_model_file() {
  case "$1" in
    ""|*/*|*\\*|*..*) return 1 ;;
	    *.gguf|*.cmpg|*.yolow|*.koro|*.onnx|*.zip) return 0 ;;
    *) return 1 ;;
  esac
}
download_one() {
  name="$1"
  url="$BASE_URL/$name"
  target="$TARGET_DIR/$name"
  tmp="$target.part"
	  if [ -s "$target" ]; then
    cp -f "$target" "$HOME_DIR/$name"
    return 0
  fi
  rm -f "$tmp"
  if command -v curl >/dev/null 2>&1; then
    if ! curl -L --fail --retry 3 --retry-delay 2 --connect-timeout 15 -o "$tmp" "$url"; then
      rm -f "$tmp"
      return 1
    fi
  elif command -v wget >/dev/null 2>&1; then
    if ! wget --tries=3 --timeout=30 -O "$tmp" "$url"; then
      rm -f "$tmp"
      return 1
    fi
  else
    echo "[ERROR] Neither curl nor wget is available" >&2
    exit 1
  fi
  mv -f "$tmp" "$target"
  cp -f "$target" "$HOME_DIR/$name"
}
for file in "$@"; do
  if ! is_allowed_model_file "$file"; then
    echo "[WARN] Skip unsafe model filename: $file" >&2
    continue
  fi
  download_one "$file"
done
touch "$SENTINEL"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}
	return ensureExecutable(scriptPath)
}

func ensureExecutable(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return err
	}
	mode := info.Mode().Perm()
	if mode&0111 != 0 {
		return nil
	}
	return os.Chmod(path, mode|0111)
}

func modelDownloadURL(r *http.Request, filename string) string {
	if r == nil {
		return "/api/v1/models/" + filename
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if forwarded := normalizeForwardedProto(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := cleanModelDownloadHost(firstForwardedValue(r.Header.Get("X-Forwarded-Host")))
	if host == "" {
		host = cleanModelDownloadHost(strings.TrimSpace(r.Host))
	}
	if host == "" {
		return "/api/v1/models/" + filename
	}
	return scheme + "://" + host + "/api/v1/models/" + filename
}

func normalizeForwardedProto(value string) string {
	proto := strings.ToLower(firstForwardedValue(value))
	if proto == "http" || proto == "https" {
		return proto
	}
	return ""
}

func firstForwardedValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

func cleanModelDownloadHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, " \t\r\n/\\") {
		return ""
	}
	return host
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
