package guiapp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TaskResultPreview is the in-app preview payload for a generated assistant
// document (PPTX slide deck, PDF bytes, or extracted/plain text).
type TaskResultPreview struct {
	Path       string `json:"path"`
	FileName   string `json:"file_name"`
	Kind       string `json:"kind"`
	Language   string `json:"language,omitempty"`
	Content    string `json:"content,omitempty"`
	DataURL    string `json:"data_url,omitempty"`
	PreviewURL string `json:"preview_url,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

const (
	taskResultPreviewHTTPPath     = "/maclaw-preview/v1/file"
	taskResultPreviewTokenTTL     = 10 * time.Minute
	taskResultPreviewMaxTokens    = 32
	taskResultPreviewPDFPeekBytes = 1024
)

type taskResultPreviewLease struct {
	path    string
	expires time.Time
}

var (
	taskResultPreviewMu     sync.Mutex
	taskResultPreviewLeases = map[string]taskResultPreviewLease{}
)

// PreviewTaskResultFile loads a generated task-result file for the assistant
// preview pane. PPTX is opened by the existing slide renderer; PDFs are served
// through a short-lived AssetServer URL; everything else is bounded text.
func (a *App) PreviewTaskResultFile(filePath string) (*TaskResultPreview, error) {
	cleaned, err := a.normalizeOpenPathForApp(filePath)
	if err != nil {
		return nil, fmt.Errorf("文件路径无效")
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil, fmt.Errorf("文件路径为空")
	}
	if abs, absErr := filepath.Abs(cleaned); absErr == nil {
		cleaned = abs
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("文件不存在")
		}
		return nil, fmt.Errorf("无法打开文件")
	}
	if info.IsDir() {
		return nil, fmt.Errorf("不是文件")
	}
	if info.Size() > agent.MaxOfficeReadFileBytes {
		return nil, fmt.Errorf("文件过大，无法预览")
	}

	name := info.Name()
	out := &TaskResultPreview{Path: cleaned, FileName: name}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pptx":
		out.Kind = "pptx"
		out.Language = "pptx"
		return out, nil
	}

	if kind, _ := previewHTTPContentType(ext); kind != "" {
		if kind == "pdf" {
			return previewTaskResultPDF(cleaned, info.Size(), out)
		}
		return previewTaskResultBinary(cleaned, kind, out)
	}

	if isTaskResultOfficeExt(ext) {
		text, _, extractErr := agent.ExtractOfficeText(cleaned)
		if extractErr != nil {
			return nil, fmt.Errorf("无法预览该文档")
		}
		out.Kind = "text"
		out.Language = "markdown"
		out.Content = text
		return out, nil
	}

	content, truncated, readErr := readCodingWorkbenchBrowserTextFile(cleaned)
	if readErr != nil {
		return nil, fmt.Errorf("无法预览该文件")
	}
	out.Kind = "text"
	out.Language = detectLanguageFromExt(name)
	out.Content = content
	out.Truncated = truncated
	return out, nil
}

func previewTaskResultPDF(path string, size int64, out *TaskResultPreview) (*TaskResultPreview, error) {
	if size <= 0 {
		return nil, fmt.Errorf("不是有效的 PDF 文件")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法打开文件")
	}
	ok, peekErr := peekPDFHeader(file)
	closeErr := file.Close()
	if peekErr != nil {
		return nil, fmt.Errorf("无法读取文件")
	}
	if closeErr != nil {
		return nil, fmt.Errorf("无法读取文件")
	}
	if !ok {
		return nil, fmt.Errorf("不是有效的 PDF 文件")
	}
	token := issueTaskResultPreviewToken(path)
	if token == "" {
		return nil, fmt.Errorf("无法预览该文档")
	}
	out.Kind = "pdf"
	out.Language = "pdf"
	out.PreviewURL = taskResultPreviewHTTPPath + "?t=" + token
	return out, nil
}

func issueTaskResultPreviewToken(path string) string {
	now := time.Now()
	taskResultPreviewMu.Lock()
	defer taskResultPreviewMu.Unlock()
	for id, lease := range taskResultPreviewLeases {
		if lease.path == path && now.Before(lease.expires) {
			lease.expires = now.Add(taskResultPreviewTokenTTL)
			taskResultPreviewLeases[id] = lease
			return id
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	token := hex.EncodeToString(raw[:])
	for id, lease := range taskResultPreviewLeases {
		if now.After(lease.expires) {
			delete(taskResultPreviewLeases, id)
		}
	}
	for len(taskResultPreviewLeases) >= taskResultPreviewMaxTokens {
		var oldest string
		var oldestExp time.Time
		for id, lease := range taskResultPreviewLeases {
			if oldest == "" || lease.expires.Before(oldestExp) {
				oldest = id
				oldestExp = lease.expires
			}
		}
		if oldest == "" {
			break
		}
		delete(taskResultPreviewLeases, oldest)
	}
	taskResultPreviewLeases[token] = taskResultPreviewLease{path: path, expires: now.Add(taskResultPreviewTokenTTL)}
	return token
}

func lookupTaskResultPreviewPath(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if len(token) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", false
	}
	now := time.Now()
	taskResultPreviewMu.Lock()
	defer taskResultPreviewMu.Unlock()
	lease, ok := taskResultPreviewLeases[token]
	if !ok || now.After(lease.expires) {
		if ok {
			delete(taskResultPreviewLeases, token)
		}
		return "", false
	}
	lease.expires = now.Add(taskResultPreviewTokenTTL)
	taskResultPreviewLeases[token] = lease
	return lease.path, true
}

func previewHTTPContentType(ext string) (kind, contentType string) {
	switch strings.ToLower(ext) {
	case ".pdf":
		return "pdf", "application/pdf"
	case ".png":
		return "image", "image/png"
	case ".jpg", ".jpeg":
		return "image", "image/jpeg"
	case ".gif":
		return "image", "image/gif"
	case ".webp":
		return "image", "image/webp"
	case ".bmp":
		return "image", "image/bmp"
	case ".svg":
		return "image", "image/svg+xml"
	case ".ico":
		return "image", "image/x-icon"
	case ".tif", ".tiff":
		return "image", "image/tiff"
	case ".heic":
		return "image", "image/heic"
	case ".mp4", ".m4v":
		return "video", "video/mp4"
	case ".webm":
		return "video", "video/webm"
	case ".mov":
		return "video", "video/quicktime"
	case ".avi":
		return "video", "video/x-msvideo"
	case ".mkv":
		return "video", "video/x-matroska"
	case ".mp3":
		return "audio", "audio/mpeg"
	case ".wav":
		return "audio", "audio/wav"
	case ".ogg", ".oga":
		return "audio", "audio/ogg"
	case ".m4a":
		return "audio", "audio/mp4"
	case ".aac":
		return "audio", "audio/aac"
	case ".flac":
		return "audio", "audio/flac"
	default:
		return "", ""
	}
}

func previewTaskResultBinary(path, kind string, out *TaskResultPreview) (*TaskResultPreview, error) {
	token := issueTaskResultPreviewToken(path)
	if token == "" {
		return nil, fmt.Errorf("无法预览该文档")
	}
	out.Kind = kind
	out.Language = kind
	out.PreviewURL = taskResultPreviewHTTPPath + "?t=" + token
	return out, nil
}

func handleTaskResultPreviewHTTP(_ *App, rw http.ResponseWriter, req *http.Request) {
	if req == nil || (req.Method != http.MethodGet && req.Method != http.MethodHead) {
		if rw != nil {
			rw.Header().Set("Allow", "GET, HEAD")
		}
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, ok := lookupTaskResultPreviewPath(req.URL.Query().Get("t"))
	if !ok {
		http.NotFound(rw, req)
		return
	}
	kind, contentType := previewHTTPContentType(filepath.Ext(path))
	if contentType == "" {
		http.NotFound(rw, req)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(rw, req)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > agent.MaxOfficeReadFileBytes {
		http.NotFound(rw, req)
		return
	}
	if kind == "pdf" {
		okPDF, peekErr := peekPDFHeader(file)
		if peekErr != nil || !okPDF {
			http.NotFound(rw, req)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			http.NotFound(rw, req)
			return
		}
	}
	rw.Header().Set("Content-Type", contentType)
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Disposition", "inline")
	http.ServeContent(rw, req, filepath.Base(path), info.ModTime(), file)
}

func peekPDFHeader(file *os.File) (bool, error) {
	if file == nil {
		return false, io.ErrUnexpectedEOF
	}
	header := make([]byte, taskResultPreviewPDFPeekBytes)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false, err
	}
	return looksLikePDF(header[:n]), nil
}

func looksLikePDF(prefix []byte) bool {
	return bytes.Contains(prefix, []byte("%PDF"))
}

func isTaskResultOfficeExt(ext string) bool {
	switch ext {
	case ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".odt", ".ods", ".odp", ".rtf":
		return true
	default:
		return false
	}
}

const taskResultExportMaxFileBytes int64 = 512 << 20

var taskResultSaveDialog = func(a *App, title, defaultFilename string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	ext := strings.ToLower(filepath.Ext(defaultFilename))
	filters := []runtime.FileFilter{{DisplayName: "All Files (*.*)", Pattern: "*.*"}}
	if ext != "" {
		filters = append([]runtime.FileFilter{{
			DisplayName: strings.ToUpper(strings.TrimPrefix(ext, ".")) + " (*" + ext + ")",
			Pattern:     "*" + ext,
		}}, filters...)
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultFilename,
		Filters:         filters,
	})
}

// ExportTaskResultFile copies a generated assistant document to a path the user
// picks in the save dialog. Empty dest means the user cancelled.
func (a *App) ExportTaskResultFile(filePath string) (string, error) {
	cleaned, err := a.normalizeOpenPathForApp(filePath)
	if err != nil {
		return "", fmt.Errorf("文件路径无效")
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "", fmt.Errorf("文件路径为空")
	}
	if abs, absErr := filepath.Abs(cleaned); absErr == nil {
		cleaned = abs
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("文件不存在")
		}
		return "", fmt.Errorf("无法读取文件")
	}
	if info.IsDir() {
		return "", fmt.Errorf("不是文件")
	}
	if info.Size() > taskResultExportMaxFileBytes {
		return "", fmt.Errorf("文件过大，无法导出")
	}

	name := info.Name()
	dest, _ := taskResultSaveDialog(a, "导出文档 / Export document", name)
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	ext := filepath.Ext(name)
	if filepath.Ext(dest) == "" && ext != "" {
		dest += ext
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	if sameTaskResultExportPath(cleaned, dest) {
		return dest, nil
	}
	if destInfo, destErr := os.Stat(dest); destErr == nil && destInfo.IsDir() {
		return "", fmt.Errorf("无法保存文件")
	}
	if err := copyTaskResultExportFile(cleaned, dest, taskResultExportMaxFileBytes); err != nil {
		return "", err
	}
	_ = os.Chmod(dest, 0o644)
	return dest, nil
}

func sameTaskResultExportPath(src, dest string) bool {
	src = filepath.Clean(src)
	dest = filepath.Clean(dest)
	if src == dest {
		return true
	}
	si, err1 := os.Stat(src)
	if err1 != nil {
		return false
	}
	di, err2 := os.Stat(dest)
	if err2 != nil {
		return false
	}
	return os.SameFile(si, di)
}

func copyTaskResultExportFile(src, dest string, maxBytes int64) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("无法读取文件")
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".maclaw-export-*")
	if err != nil {
		return fmt.Errorf("无法保存文件")
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	n, err := io.Copy(tmp, io.LimitReader(in, maxBytes+1))
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("无法保存文件")
	}
	if n > maxBytes {
		return fmt.Errorf("文件过大，无法导出")
	}
	if err := replaceTaskResultExportFile(tmpName, dest); err != nil {
		return fmt.Errorf("无法保存文件")
	}
	return nil
}

func replaceTaskResultExportFile(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	in, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("无法保存文件")
	}
	_ = os.Remove(tmp)
	return nil
}

func resetTaskResultPreviewLeasesForTest() {
	taskResultPreviewMu.Lock()
	defer taskResultPreviewMu.Unlock()
	taskResultPreviewLeases = map[string]taskResultPreviewLease{}
}
