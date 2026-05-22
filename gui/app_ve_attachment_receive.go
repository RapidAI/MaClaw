package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

const veAttachmentContextMaxBytes = 20 * 1024 * 1024

// ProcessMessageAttachments extracts attachment content from a GroupDiscussionMessage
// and returns a formatted context string to be appended to the AI Agent input.
// - TextAttachment: base64 decode → append text content as context
// - ImageAttachment/FileAttachment: download via file_url → extract text or pass as vision input
// On download failure, logs error and continues processing the text message.
func (h *VEMessageHandler) ProcessMessageAttachments(msg a2a.GroupDiscussionMessage) string {
	return h.ProcessMessageAttachmentsForSession("", msg)
}

func (h *VEMessageHandler) ProcessMessageAttachmentsForSession(sessionID string, msg a2a.GroupDiscussionMessage) string {
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

	// Process image attachments (prefer local_path for direct local dispatch)
	for _, att := range msg.ImageAttachments {
		content, err := h.attachmentContent(sessionID, att.FileURL, att.LocalPath)
		if err != nil {
			log.Printf("[ve-attachment] failed to read image %s: %v", att.Filename, err)
			continue
		}
		// For images, we provide a description placeholder
		// In production, this would be passed as vision input to the AI Agent
		contextParts = append(contextParts, formatImageAttachmentContext(att.Filename, att.MimeType, len(content)))
	}

	// Process file/document attachments (prefer local_path for direct local dispatch)
	for _, att := range msg.FileAttachments {
		content, err := h.attachmentContent(sessionID, att.FileURL, att.LocalPath)
		if err != nil {
			log.Printf("[ve-attachment] failed to read file %s: %v", att.Filename, err)
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

func (h *VEMessageHandler) attachmentContent(sessionID, fileURL, localPath string) ([]byte, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath != "" {
		f, err := os.Open(localPath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return readVEAttachmentContextContent(f)
	}
	return h.downloadAttachmentContent(sessionID, fileURL)
}

func readVEAttachmentContextContent(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(r, veAttachmentContextMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if n > veAttachmentContextMaxBytes {
		return nil, fmt.Errorf("attachment content exceeds context limit: %d bytes; limit is 20 MB", n)
	}
	return buf.Bytes(), nil
}

// decodeTextAttachment decodes a base64-encoded text attachment.
func decodeTextAttachment(att a2a.TextAttachment) (string, error) {
	if att.Content == "" {
		return "", fmt.Errorf("empty content")
	}
	if len(att.Content) > base64.StdEncoding.EncodedLen(veAttachmentContextMaxBytes) {
		return "", fmt.Errorf("text attachment exceeds context limit")
	}
	decoded, err := base64.StdEncoding.DecodeString(att.Content)
	if err != nil {
		// Try URL-safe base64
		decoded, err = base64.URLEncoding.DecodeString(att.Content)
		if err != nil {
			return "", fmt.Errorf("base64 decode failed: %w", err)
		}
	}
	if len(decoded) > veAttachmentContextMaxBytes {
		return "", fmt.Errorf("text attachment exceeds context limit")
	}
	return string(decoded), nil
}

// downloadAttachmentContent downloads file content from a Hub file relay URL.
func (h *VEMessageHandler) downloadAttachmentContent(sessionID, fileURL string) ([]byte, error) {
	if fileURL == "" {
		return nil, fmt.Errorf("empty file URL")
	}
	if h.app == nil {
		return nil, fmt.Errorf("app unavailable")
	}
	hubURL, token, err := h.app.getHubCredentials()
	if err != nil {
		return nil, fmt.Errorf("Hub credentials unavailable: %w", err)
	}
	cfg, _ := h.app.LoadConfig()
	participantID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	downloadURL, _, err := groupDiscussionAttachmentDownloadURL(hubURL, fileURL, sessionID, participantID)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if participantID != "" {
		req.Header.Set("X-Machine-ID", participantID)
	}

	resp, err := veFileRelayHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}

	data, err := readVEAttachmentContextContent(resp.Body)
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
