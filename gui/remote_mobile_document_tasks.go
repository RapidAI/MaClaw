package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

type mobileDocumentUploadTask struct {
	TaskID            string `json:"task_id"`
	Filename          string `json:"filename"`
	ContentType       string `json:"content_type"`
	Status            string `json:"status"`
	DraftID           string `json:"draft_id"`
	Message           string `json:"message"`
	ClaimedBy         string `json:"claimed_by"`
	SourceDownloadURL string `json:"source_download_url"`
}

type mobileDocumentUploadClaimResponse struct {
	Status string                    `json:"status"`
	Task   *mobileDocumentUploadTask `json:"task"`
}

var (
	mobileDocOCROnce sync.Once
	mobileDocOCR     *browser.RapidOCRSidecar
)

func mobileDocumentOCRSidecar() *browser.RapidOCRSidecar {
	mobileDocOCROnce.Do(func() {
		mobileDocOCR = browser.NewRapidOCRSidecar(func(msg string) {
			log.Printf("[mobile-doc-ocr] %s", msg)
		})
	})
	return mobileDocOCR
}

func (c *RemoteHubClient) pollMobileDocumentUploadTasksOnce() {
	claim, err := c.claimMobileDocumentUploadTask()
	if err != nil {
		log.Printf("[hub-client] mobile document upload claim failed: %v", err)
		return
	}
	if claim == nil || claim.Task == nil || strings.TrimSpace(claim.Task.TaskID) == "" {
		return
	}
	c.processMobileDocumentUploadTask(*claim.Task)
}

func (c *RemoteHubClient) claimMobileDocumentUploadTask() (*mobileDocumentUploadClaimResponse, error) {
	var out mobileDocumentUploadClaimResponse
	// kind=all: text parse (queued) + image OCR (needs_ocr).
	path := "/api/mobile/documents/upload/claim?kind=all"
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) updateMobileDocumentUploadTask(taskID, status, markdown, message, errText string) (*mobileDocumentUploadTask, error) {
	payload := map[string]string{
		"status":   strings.TrimSpace(status),
		"markdown": strings.TrimSpace(markdown),
		"message":  strings.TrimSpace(message),
		"error":    strings.TrimSpace(errText),
	}
	var out mobileDocumentUploadTask
	path := "/api/mobile/documents/upload/" + url.PathEscape(strings.TrimSpace(taskID)) + "/result"
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPatch, path, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) downloadMobileDocumentUploadSource(task mobileDocumentUploadTask) ([]byte, error) {
	if c == nil || c.app == nil {
		return nil, fmt.Errorf("remote hub client is not initialized")
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	token := strings.TrimSpace(cfg.RemoteMachineToken)
	sourcePath := strings.TrimSpace(task.SourceDownloadURL)
	if base == "" || machineID == "" || token == "" || sourcePath == "" {
		return nil, fmt.Errorf("remote hub source download identity is incomplete")
	}
	if strings.HasPrefix(sourcePath, "http://") || strings.HasPrefix(sourcePath, "https://") {
		base = ""
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+sourcePath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hub returned HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 25*1024*1024+1))
}

func (c *RemoteHubClient) processMobileDocumentUploadTask(task mobileDocumentUploadTask) {
	taskID := strings.TrimSpace(task.TaskID)
	if taskID == "" {
		return
	}
	source, err := c.downloadMobileDocumentUploadSource(task)
	if err != nil {
		_, _ = c.updateMobileDocumentUploadTask(taskID, "failed", "", "", err.Error())
		return
	}

	// Image / OCR path: RapidOCR on the desktop worker, then write markdown back.
	// Note: after claim the Hub status is always "in_progress", so detect by name/MIME.
	if mobileDocumentUploadIsImage(task.Filename, task.ContentType) {
		markdown, ocrErr := mobileDocumentOCRMarkdown(task.Filename, source)
		if ocrErr != nil {
			log.Printf("[hub-client] mobile document OCR failed task=%s: %v", taskID, ocrErr)
			_, _ = c.updateMobileDocumentUploadTask(taskID, "failed", "", "", ocrErr.Error())
			return
		}
		_, _ = c.updateMobileDocumentUploadTask(taskID, "ready", markdown, "远程端已完成图片 OCR，并更新移动端文档草稿。", "")
		return
	}

	markdown, ok := mobileDocumentSourceMarkdown(task.Filename, task.ContentType, source)
	if !ok {
		// Last-chance OCR for unknown binary that looks like an image by magic bytes.
		if mobileDocumentLooksLikeImage(source) {
			markdown, ocrErr := mobileDocumentOCRMarkdown(task.Filename, source)
			if ocrErr == nil {
				_, _ = c.updateMobileDocumentUploadTask(taskID, "ready", markdown, "远程端已完成图片 OCR，并更新移动端文档草稿。", "")
				return
			}
		}
		_, _ = c.updateMobileDocumentUploadTask(taskID, "failed", "", "", "远程端暂不支持解析该文件格式。")
		return
	}
	_, _ = c.updateMobileDocumentUploadTask(taskID, "ready", markdown, "远程端已解析移动文档。", "")
}

func mobileDocumentSourceMarkdown(filename, contentType string, raw []byte) (string, bool) {
	if len(raw) > 25*1024*1024 {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(filename))
	normalizedType := strings.ToLower(strings.TrimSpace(contentType))
	textLike := strings.HasPrefix(normalizedType, "text/") ||
		strings.Contains(normalizedType, "json") ||
		strings.Contains(normalizedType, "csv") ||
		ext == ".txt" || ext == ".md" || ext == ".markdown" ||
		ext == ".log" || ext == ".csv" || ext == ".json"
	if !textLike || !utf8.Valid(raw) {
		return "", false
	}
	text := strings.TrimSpace(string(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})))
	if text == "" {
		text = "_导入文件为空。_"
	}
	if ext == ".md" || ext == ".markdown" {
		return text + "\n", true
	}
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if title == "" {
		title = "移动文档"
	}
	return "# " + title + "\n\n" + text + "\n", true
}

func mobileDocumentUploadIsImage(filename, contentType string) bool {
	normalizedType := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(normalizedType, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func mobileDocumentLooksLikeImage(raw []byte) bool {
	if len(raw) < 4 {
		return false
	}
	// PNG
	if len(raw) >= 8 && bytes.Equal(raw[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return true
	}
	// JPEG
	if raw[0] == 0xFF && raw[1] == 0xD8 {
		return true
	}
	// GIF
	if bytes.HasPrefix(raw, []byte("GIF8")) {
		return true
	}
	// BMP
	if raw[0] == 'B' && raw[1] == 'M' {
		return true
	}
	// WEBP (RIFF....WEBP)
	if len(raw) >= 12 && bytes.Equal(raw[:4], []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")) {
		return true
	}
	return false
}

func mobileDocumentOCRMarkdown(filename string, raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("图片内容为空，无法 OCR")
	}
	if len(raw) > 25*1024*1024 {
		return "", fmt.Errorf("图片过大，无法 OCR")
	}
	sidecar := mobileDocumentOCRSidecar()
	b64 := base64.StdEncoding.EncodeToString(raw)
	results, err := sidecar.Recognize(b64)
	if err != nil {
		return "", fmt.Errorf("桌面 OCR 失败（请确认本机已安装 Python 与 RapidOCR）：%w", err)
	}

	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if title == "" {
		title = "图片识别"
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if format == "jpg" {
		format = "jpeg"
	}
	if format == "" {
		format = "image"
	}

	var lines []string
	for _, r := range results {
		text := strings.TrimSpace(r.Text)
		if text != "" {
			lines = append(lines, text)
		}
	}

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("图片已由远程桌面 OCR 识别。\n\n")
	b.WriteString("- 文件名：")
	b.WriteString(filepath.Base(filename))
	b.WriteString("\n")
	b.WriteString("- 格式：")
	b.WriteString(format)
	b.WriteString("\n")
	b.WriteString("- 大小：")
	b.WriteString(fmt.Sprintf("%d bytes", len(raw)))
	b.WriteString("\n")
	b.WriteString("- 识别区域数：")
	b.WriteString(fmt.Sprintf("%d", len(lines)))
	b.WriteString("\n\n")
	b.WriteString("## 识别文本\n\n")
	if len(lines) == 0 {
		b.WriteString("_未识别到可读文字（可能是纯图、模糊或非文字截图）。_\n")
	} else {
		// Prefer plain paragraphs so the mobile assistant can quote the content.
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	return b.String(), nil
}
