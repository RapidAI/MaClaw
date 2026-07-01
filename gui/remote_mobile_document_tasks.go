package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
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
	path := "/api/mobile/documents/upload/claim?kind=document"
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
	markdown, ok := mobileDocumentSourceMarkdown(task.Filename, task.ContentType, source)
	if !ok {
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
