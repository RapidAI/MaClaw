package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

// ProcessMessageAttachments extracts attachment content from a GroupDiscussionMessage
// and returns a formatted context string to be appended to the AI Agent input.
// - TextAttachment: base64 decode → append text content as context
// - ImageAttachment/FileAttachment: download via file_url → extract text or pass as vision input
// On download failure, logs error and continues processing the text message.
func (h *VEMessageHandler) ProcessMessageAttachments(msg a2a.GroupDiscussionMessage) string {
	var contextParts []string

	// Process text attachments (inline base64)
	for _, att := range msg.TextAttachments {
		content, err := decodeTextAttachment(att)
		if err != nil {
			log.Printf("[ve-attachment] failed to decode text attachment %s: %v", att.Filename, err)
			continue
		}
		contextParts = append(contextParts, formatTextAttachmentContext(att.Filename, content))
	}

	// Process image attachments (download from file_url)
	for _, att := range msg.ImageAttachments {
		content, err := h.downloadAttachmentContent(att.FileURL)
		if err != nil {
			log.Printf("[ve-attachment] failed to download image %s from %s: %v", att.Filename, att.FileURL, err)
			continue
		}
		// For images, we provide a description placeholder
		// In production, this would be passed as vision input to the AI Agent
		contextParts = append(contextParts, formatImageAttachmentContext(att.Filename, att.MimeType, len(content)))
	}

	// Process file/document attachments (download from file_url)
	for _, att := range msg.FileAttachments {
		content, err := h.downloadAttachmentContent(att.FileURL)
		if err != nil {
			log.Printf("[ve-attachment] failed to download file %s from %s: %v", att.Filename, att.FileURL, err)
			continue
		}
		// For documents, extract text content if possible
		textContent := extractTextFromDocument(att.Filename, att.MimeType, content)
		if textContent != "" {
			contextParts = append(contextParts, formatDocAttachmentContext(att.Filename, textContent))
		}
	}

	if len(contextParts) == 0 {
		return ""
	}

	return "\n\n---\n[附件内容]\n" + strings.Join(contextParts, "\n\n")
}

// decodeTextAttachment decodes a base64-encoded text attachment.
func decodeTextAttachment(att a2a.TextAttachment) (string, error) {
	if att.Content == "" {
		return "", fmt.Errorf("empty content")
	}
	decoded, err := base64.StdEncoding.DecodeString(att.Content)
	if err != nil {
		// Try URL-safe base64
		decoded, err = base64.URLEncoding.DecodeString(att.Content)
		if err != nil {
			return "", fmt.Errorf("base64 decode failed: %w", err)
		}
	}
	return string(decoded), nil
}

// downloadAttachmentContent downloads file content from a Hub file relay URL.
func (h *VEMessageHandler) downloadAttachmentContent(fileURL string) ([]byte, error) {
	if fileURL == "" {
		return nil, fmt.Errorf("empty file URL")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Add auth headers if available
	if h.app != nil {
		cfg, _ := h.app.LoadConfig()
		token := strings.TrimSpace(cfg.RemoteMachineToken)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if cfg.RemoteMachineID != "" {
			req.Header.Set("X-Machine-ID", cfg.RemoteMachineID)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}

	// Limit read to 20MB to prevent memory issues
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return data, nil
}

// extractTextFromDocument attempts to extract readable text from a document.
// For plain text MIME types, returns the content directly.
// For binary formats (PDF, DOCX), returns a placeholder indicating the file was received.
func extractTextFromDocument(filename, mimeType string, content []byte) string {
	// Plain text types: return content directly
	if isPlainTextMime(mimeType) {
		// Truncate very long text to avoid overwhelming the AI context
		text := string(content)
		if len([]rune(text)) > 10000 {
			text = string([]rune(text)[:10000]) + "\n...[内容已截断，共 " + fmt.Sprintf("%d", len([]rune(string(content)))) + " 字符]"
		}
		return text
	}

	// Binary formats: provide metadata only
	ext := strings.ToLower(filename)
	if strings.HasSuffix(ext, ".pdf") {
		return fmt.Sprintf("[PDF 文档: %s, %s]", filename, formatBytesSize(int64(len(content))))
	}
	if strings.HasSuffix(ext, ".docx") {
		return fmt.Sprintf("[Word 文档: %s, %s]", filename, formatBytesSize(int64(len(content))))
	}

	return fmt.Sprintf("[文件: %s, %s, %s]", filename, mimeType, formatBytesSize(int64(len(content))))
}

// isPlainTextMime checks if a MIME type represents plain text content.
func isPlainTextMime(mimeType string) bool {
	if mimeType == "" {
		return false
	}
	mt := strings.ToLower(mimeType)
	if strings.HasPrefix(mt, "text/") {
		return true
	}
	plainTextTypes := []string{
		"application/json",
		"application/xml",
		"application/x-yaml",
		"application/javascript",
	}
	for _, t := range plainTextTypes {
		if mt == t {
			return true
		}
	}
	return false
}

// formatTextAttachmentContext formats a text attachment for AI context injection.
func formatTextAttachmentContext(filename, content string) string {
	return fmt.Sprintf("📄 文件: %s\n```\n%s\n```", filename, content)
}

// formatImageAttachmentContext formats an image attachment description for AI context.
func formatImageAttachmentContext(filename, mimeType string, sizeBytes int) string {
	return fmt.Sprintf("🖼️ 图片: %s (%s, %s)", filename, mimeType, formatBytesSize(int64(sizeBytes)))
}

// formatDocAttachmentContext formats a document attachment for AI context injection.
func formatDocAttachmentContext(filename, textContent string) string {
	return fmt.Sprintf("📄 文档: %s\n```\n%s\n```", filename, textContent)
}

// formatBytesSize formats a byte count as a human-readable string.
func formatBytesSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}

// HasAttachments checks if a GroupDiscussionMessage contains any attachments.
func HasAttachments(msg a2a.GroupDiscussionMessage) bool {
	return len(msg.TextAttachments) > 0 ||
		len(msg.ImageAttachments) > 0 ||
		len(msg.FileAttachments) > 0
}
