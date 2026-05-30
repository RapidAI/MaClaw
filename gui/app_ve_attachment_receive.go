package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

const veAttachmentContextMaxBytes = 20 * 1024 * 1024

// ProcessMessageAttachments extracts attachment content from a GroupDiscussionMessage
// and returns formatted context to append to the AI agent input. Remote file_url
// attachments are downloaded into this machine's discussion attachment directory
// first, then the saved local path is included in the agent context.
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
		contextParts = append(contextParts, formatTextAttachmentContext(att.Filename, att.LocalPath, content))
	}

	// Process image attachments (prefer local_path for direct local dispatch)
	for _, att := range msg.ImageAttachments {
		content, err := h.attachmentContent(sessionID, att.FileURL, att.LocalPath, att.Filename)
		if err != nil {
			log.Printf("[ve-attachment] failed to read image %s: %v", att.Filename, err)
			continue
		}
		// For images, we provide a description placeholder
		// In production, this would be passed as vision input to the AI Agent
		contextParts = append(contextParts, formatImageAttachmentContext(att.Filename, att.MimeType, len(content.Data), content.LocalPath))
	}

	// Process file/document attachments (prefer local_path for direct local dispatch)
	for _, att := range msg.FileAttachments {
		content, err := h.attachmentContent(sessionID, att.FileURL, att.LocalPath, att.Filename)
		if err != nil {
			log.Printf("[ve-attachment] failed to read file %s: %v", att.Filename, err)
			continue
		}
		// For documents, extract text content if possible
		textContent := extractTextFromDocument(att.Filename, att.MimeType, content.Data)
		if textContent != "" {
			contextParts = append(contextParts, formatDocAttachmentContext(att.Filename, content.LocalPath, textContent))
		}
	}

	if len(contextParts) == 0 {
		return ""
	}

	return "\n\n---\n[Attachment content]\n" + strings.Join(contextParts, "\n\n")
}

type veAttachmentContent struct {
	Data      []byte
	LocalPath string
}

func (h *VEMessageHandler) attachmentContent(sessionID, fileURL, localPath, filename string) (veAttachmentContent, error) {
	localPath = strings.TrimSpace(localPath)
	if localPath != "" {
		f, err := os.Open(localPath)
		if err != nil {
			return veAttachmentContent{}, err
		}
		defer f.Close()
		data, err := readVEAttachmentContextContent(f)
		if err != nil {
			return veAttachmentContent{}, err
		}
		return veAttachmentContent{Data: data, LocalPath: localPath}, nil
	}
	if h == nil || h.app == nil {
		return veAttachmentContent{}, fmt.Errorf("app unavailable")
	}
	result, err := h.app.GroupDiscussionDownloadAttachment(sessionID, fileURL, filename)
	if err != nil {
		return veAttachmentContent{}, err
	}
	return h.attachmentContent(sessionID, "", result.LocalPath, filename)
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

// extractTextFromDocument attempts to extract readable text from a document.
// For plain text MIME types, returns the content directly.
// For binary formats (PDF, DOCX), returns a placeholder indicating the file was received.
func extractTextFromDocument(filename, mimeType string, content []byte) string {
	if isPlainTextMime(mimeType) {
		text := string(content)
		if len([]rune(text)) > 10000 {
			text = string([]rune(text)[:10000]) + "\n...[content truncated, total " + fmt.Sprintf("%d", len([]rune(string(content)))) + " chars]"
		}
		return text
	}

	ext := strings.ToLower(filename)
	if strings.HasSuffix(ext, ".pdf") {
		return fmt.Sprintf("[PDF document: %s, %s]", filename, formatBytesSize(int64(len(content))))
	}
	if strings.HasSuffix(ext, ".docx") {
		return fmt.Sprintf("[Word document: %s, %s]", filename, formatBytesSize(int64(len(content))))
	}

	return fmt.Sprintf("[File: %s, %s, %s]", filename, mimeType, formatBytesSize(int64(len(content))))
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
func formatTextAttachmentContext(filename, localPath, content string) string {
	return fmt.Sprintf("File: %s%s\n```\n%s\n```", filename, formatAttachmentSavedPath(localPath), content)
}

// formatImageAttachmentContext formats an image attachment description for AI context.
func formatImageAttachmentContext(filename, mimeType string, sizeBytes int, localPath string) string {
	return fmt.Sprintf("Image: %s (%s, %s)%s", filename, mimeType, formatBytesSize(int64(sizeBytes)), formatAttachmentSavedPath(localPath))
}

// formatDocAttachmentContext formats a document attachment for AI context injection.
func formatDocAttachmentContext(filename, localPath, textContent string) string {
	return fmt.Sprintf("Document: %s%s\n```\n%s\n```", filename, formatAttachmentSavedPath(localPath), textContent)
}

func formatAttachmentSavedPath(localPath string) string {
	localPath = strings.TrimSpace(localPath)
	if localPath == "" {
		return ""
	}
	return fmt.Sprintf("\nSaved path: %s", localPath)
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
