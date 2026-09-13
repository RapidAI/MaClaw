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
	taskResultPreviewPDFPeekBytes = 8
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
	case ".pdf":
		return previewTaskResultPDF(cleaned, info.Size(), out)
	}

	if isTaskResultOfficeExt(ext) {
		text, _, extractErr := agent.ExtractOfficeText(cleaned)
		if extractErr != nil {
			return nil, fmt.Errorf("无法预览该文档")
		}
		out.Kind = "text"
		out.Language = "plaintext"
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
	header := make([]byte, taskResultPreviewPDFPeekBytes)
	n, readErr := io.ReadFull(file, header)
	file.Close()
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return nil, fmt.Errorf("无法读取文件")
	}
	if n < 4 || !bytes.HasPrefix(header[:n], []byte("%PDF")) {
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
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	token := hex.EncodeToString(raw[:])
	now := time.Now()
	taskResultPreviewMu.Lock()
	defer taskResultPreviewMu.Unlock()
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
	if token == "" {
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
	return lease.path, true
}

func handleTaskResultPreviewHTTP(_ *App, rw http.ResponseWriter, req *http.Request) {
	if req == nil || req.Method != http.MethodGet {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, ok := lookupTaskResultPreviewPath(req.URL.Query().Get("t"))
	if !ok {
		http.NotFound(rw, req)
		return
	}
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
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
	if err != nil || info.IsDir() {
		http.NotFound(rw, req)
		return
	}
	rw.Header().Set("Content-Type", "application/pdf")
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Disposition", "inline")
	http.ServeContent(rw, req, filepath.Base(path), info.ModTime(), file)
}

func isTaskResultOfficeExt(ext string) bool {
	switch ext {
	case ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".odt", ".ods", ".odp", ".rtf":
		return true
	default:
		return false
	}
}

func resetTaskResultPreviewLeasesForTest() {
	taskResultPreviewMu.Lock()
	defer taskResultPreviewMu.Unlock()
	taskResultPreviewLeases = map[string]taskResultPreviewLease{}
}
