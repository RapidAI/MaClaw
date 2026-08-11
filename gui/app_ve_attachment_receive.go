package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	veAttachmentContextMaxBytes = 20 * 1024 * 1024
	veAttachmentContextMaxCount = 20
)

// ProcessMessageAttachments extracts attachment content from a GroupDiscussionMessage
// and returns formatted context to append to the AI agent input. Remote file_url
// attachments are downloaded into this machine's discussion attachment directory
// first, then the saved local path is included in the agent context.
func (h *VEMessageHandler) ProcessMessageAttachments(msg a2a.GroupDiscussionMessage) string {
	return h.ProcessMessageAttachmentsForSession("", msg)
}

func (h *VEMessageHandler) ProcessMessageAttachmentsForSession(sessionID string, msg a2a.GroupDiscussionMessage) string {
	var contextParts []string
	attempted := 0

	// Process text attachments (inline base64)
	for _, att := range msg.TextAttachments {
		if attempted >= veAttachmentContextMaxCount {
			break
		}
		attempted++
		decoded, err := decodeTextAttachmentBytes(att)
		if err != nil {
			log.Printf("[ve-attachment] failed to decode text attachment %s: %v", att.Filename, err)
			continue
		}
		binaryDocument := isVEBinaryDocumentAttachment(att.Filename, att.MimeType, decoded)
		if binaryDocument || !isSafeInlineTextAttachment(decoded) {
			localPath, persistErr := h.persistBinaryTextAttachment(sessionID, att, decoded)
			if persistErr != nil {
				log.Printf("[ve-attachment] failed to persist binary text attachment %s: %v", att.Filename, persistErr)
			}
			label := "Binary attachment"
			if binaryDocument {
				label = "Binary document"
			}
			contextParts = append(contextParts, formatDocAttachmentContext(
				att.Filename,
				localPath,
				fmt.Sprintf("[%s: %s, %s]", label, att.Filename, formatBytesSize(int64(len(decoded)))),
			))
			continue
		}
		contextParts = append(contextParts, formatTextAttachmentContext(att.Filename, att.LocalPath, string(decoded)))
	}

	// Process image attachments (prefer local_path for direct local dispatch)
	for _, att := range msg.ImageAttachments {
		if attempted >= veAttachmentContextMaxCount {
			break
		}
		attempted++
		content, err := h.attachmentContent(sessionID, att.FileURL, att.LocalPath, att.Filename)
		if err != nil {
			log.Printf("[ve-attachment] failed to read image %s: %v", att.Filename, err)
			continue
		}
		// ImageAttachment is controlled by the remote sender. Re-check binary
		// document identity after the safe local download so an Office/PDF file
		// labelled image/* cannot retain the image-only context path.
		if isVEBinaryDocumentAttachment(att.Filename, att.MimeType, content.Data) {
			localPath := content.LocalPath
			if stagedPath, stageErr := h.persistBinaryAttachment(sessionID, att.Filename, att.MimeType, content.Data); stageErr == nil {
				localPath = stagedPath
			} else if agent.IsBinaryDocumentAttachment(att.Filename, att.MimeType) {
				log.Printf("[ve-attachment] failed to normalize binary image attachment %s: %v", att.Filename, stageErr)
			}
			contextParts = append(contextParts, formatDocAttachmentContext(
				att.Filename,
				localPath,
				fmt.Sprintf("[Binary document: %s, %s]", att.Filename, formatBytesSize(int64(len(content.Data)))),
			))
			continue
		}
		// For images, we provide a description placeholder
		// In production, this would be passed as vision input to the AI Agent
		contextParts = append(contextParts, formatImageAttachmentContext(att.Filename, att.MimeType, len(content.Data), content.LocalPath))
	}

	// Process file/document attachments (prefer local_path for direct local dispatch)
	for _, att := range msg.FileAttachments {
		if attempted >= veAttachmentContextMaxCount {
			break
		}
		attempted++
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
	decoded, err := decodeTextAttachmentBytes(att)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func decodeTextAttachmentBytes(att a2a.TextAttachment) ([]byte, error) {
	if att.Content == "" {
		return nil, fmt.Errorf("empty content")
	}
	if len(att.Content) > base64.StdEncoding.EncodedLen(veAttachmentContextMaxBytes) {
		return nil, fmt.Errorf("text attachment exceeds context limit")
	}
	var lastErr error
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(att.Content)
		if err == nil {
			if len(decoded) > veAttachmentContextMaxBytes {
				return nil, fmt.Errorf("text attachment exceeds context limit")
			}
			return decoded, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("base64 decode failed: %w", lastErr)
}

// extractTextFromDocument attempts to extract readable text from a document.
// For plain text MIME types, returns the content directly.
// For binary document formats, returns a placeholder and the safely persisted
// local path instead of injecting bytes into the agent context. The agent can
// then use the shared OfficeRead-backed read_document path when it needs text.
func extractTextFromDocument(filename, mimeType string, content []byte) string {
	// A remote sender controls MIME metadata. Keep the filename-led Office
	// boundary ahead of the plain-text fast path so a DOC/DOCX/PPT/PPTX/XLS/XLSX
	// attachment mislabeled as text/plain cannot inject arbitrary binary bytes
	// into the agent context. The saved local path remains available for the
	// shared OfficeRead-backed read_document tool.
	ext := strings.ToLower(strings.TrimSpace(filename))
	if strings.HasSuffix(ext, ".pdf") || hasVEPDFSignature(content) {
		// PDF is also a binary document. Preserve its established placeholder
		// path before trusting a remote MIME declaration for the same reason as
		// Office: a mislabeled PDF must not become inline agent context.
		return fmt.Sprintf("[PDF document: %s, %s]", filename, formatBytesSize(int64(len(content))))
	}
	if isVEOfficeDocumentFilename(ext) || hasVEOfficeContainerSignature(content) {
		return fmt.Sprintf("[Office document: %s, %s]", filename, formatBytesSize(int64(len(content))))
	}
	if isPlainTextMime(mimeType) {
		text := string(content)
		if len([]rune(text)) > 10000 {
			text = string([]rune(text)[:10000]) + "\n...[content truncated, total " + fmt.Sprintf("%d", len([]rune(string(content)))) + " chars]"
		}
		return text
	}

	return fmt.Sprintf("[File: %s, %s, %s]", filename, mimeType, formatBytesSize(int64(len(content))))
}

// isVEBinaryDocumentAttachment treats both untrusted metadata and a small set
// of unambiguous file signatures as routing hints. The actual Office parser
// still performs its own container validation before extracting text.
func isVEBinaryDocumentAttachment(filename, mimeType string, content []byte) bool {
	return agent.IsBinaryDocumentAttachment(filename, mimeType) ||
		hasVEPDFSignature(content) || hasVEOfficeContainerSignature(content)
}

func hasVEPDFSignature(content []byte) bool {
	limit := len(content)
	if limit > 1024 {
		limit = 1024
	}
	return bytes.Contains(content[:limit], []byte("%PDF-"))
}

func hasVEOfficeContainerSignature(content []byte) bool {
	if len(content) >= 8 && bytes.Equal(content[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		return true
	}
	return len(content) >= 4 && content[0] == 'P' && content[1] == 'K' &&
		((content[2] == 3 && content[3] == 4) || (content[2] == 5 && content[3] == 6) || (content[2] == 7 && content[3] == 8))
}

// isSafeInlineTextAttachment rejects arbitrary binary data even when a remote
// peer labels it text/plain. This prevents document bytes from being coerced to
// a Go string and appended to an LLM prompt through the inline-text channel.
func isSafeInlineTextAttachment(content []byte) bool {
	if !utf8.Valid(content) {
		return false
	}
	for _, b := range content {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			return false
		}
	}
	return true
}

func (h *VEMessageHandler) persistBinaryTextAttachment(sessionID string, att a2a.TextAttachment, content []byte) (string, error) {
	return h.persistBinaryAttachment(sessionID, att.Filename, att.MimeType, content)
}

func (h *VEMessageHandler) persistBinaryAttachment(sessionID, filename, mimeType string, content []byte) (string, error) {
	if h == nil || h.app == nil || strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("attachment storage is unavailable")
	}
	dir := h.app.groupDiscussionAttachmentRoot(sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create attachment dir: %w", err)
	}
	filename = agent.NormalizeBinaryDocumentAttachmentFilename(filename, mimeType)
	filename = safeGroupDiscussionFilename(filename)
	if filename == "" {
		filename = "binary-attachment"
	}
	tmp, err := os.CreateTemp(dir, ".inline-text-*")
	if err != nil {
		return "", fmt.Errorf("create local attachment temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write local attachment: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close local attachment: %w", err)
	}
	localPath, _, err := groupDiscussionCommitTempAttachment(tmpPath, dir, filename)
	if err != nil {
		return "", fmt.Errorf("store local attachment: %w", err)
	}
	if err := os.Chmod(localPath, 0o600); err != nil {
		return "", fmt.Errorf("protect local attachment: %w", err)
	}
	return localPath, nil
}

func isVEOfficeDocumentFilename(lowerFilename string) bool {
	for ext := range documentExtensions {
		if ext != ".pdf" && strings.HasSuffix(lowerFilename, ext) {
			return true
		}
	}
	return false
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
