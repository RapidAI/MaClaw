package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

// File size limits.
const (
	maxTextFileSize     = 500 * 1024       // 500KB
	maxImageFileSize    = 10 * 1024 * 1024 // 10MB
	maxDocumentFileSize = 20 * 1024 * 1024 // 20MB
)

// File type extension sets.
var (
	textExtensions = map[string]bool{
		".txt": true, ".md": true, ".csv": true, ".json": true,
		".xml": true, ".yaml": true, ".yml": true, ".log": true,
		".go": true, ".py": true, ".js": true, ".ts": true,
		".html": true, ".css": true,
	}
	imageExtensions = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true,
		".gif": true, ".webp": true, ".bmp": true,
	}
	documentExtensions = map[string]bool{
		".pdf": true, ".docx": true,
	}
)

// classifyFileType returns "text", "image", or "document" based on file extension.
// Returns empty string if the extension is not supported.
func classifyFileType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if textExtensions[ext] {
		return "text"
	}
	if imageExtensions[ext] {
		return "image"
	}
	if documentExtensions[ext] {
		return "document"
	}
	return ""
}

// validateFileSize checks that the file at path does not exceed the size limit
// for the given category ("text", "image", or "document").
func validateFileSize(path string, category string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot access file: %w", err)
	}

	size := info.Size()
	switch category {
	case "text":
		if size > maxTextFileSize {
			return fmt.Errorf("text file exceeds 500KB limit (%d bytes)", size)
		}
	case "image":
		if size > maxImageFileSize {
			return fmt.Errorf("image file exceeds 10MB limit (%d bytes)", size)
		}
	case "document":
		if size > maxDocumentFileSize {
			return fmt.Errorf("document file exceeds 20MB limit (%d bytes)", size)
		}
	default:
		return fmt.Errorf("unknown file category: %s", category)
	}
	return nil
}

// base64EncodeFile reads the file at path and returns its content as a base64-encoded string.
func base64EncodeFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file failed: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// veFileRelayHTTPClient is a shared HTTP client for file relay uploads.
// Reusing a single client enables TCP connection pooling and keep-alive.
var veFileRelayHTTPClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        5,
		MaxIdleConnsPerHost: 3,
		IdleConnTimeout:     90 * time.Second,
	},
}

// uploadToFileRelay uploads a file to the Hub file relay endpoint via multipart/form-data.
// Returns the file_url on success.
func (a *App) uploadToFileRelay(hubURL, token, filePath, sessionID string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file failed: %w", err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add the file field.
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("create form file failed: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("copy file content failed: %w", err)
	}

	// Add session_id field.
	if sessionID != "" {
		_ = writer.WriteField("session_id", sessionID)
	}

	// Add participant_id from config.
	cfg, _ := a.LoadConfig()
	participantID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if participantID != "" {
		_ = writer.WriteField("participant_id", participantID)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer failed: %w", err)
	}

	// Build the request.
	uploadURL := hubURL + "/api/ve/files/upload"
	req, err := http.NewRequest(http.MethodPost, uploadURL, &buf)
	if err != nil {
		return "", fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	if participantID != "" {
		req.Header.Set("X-Machine-ID", participantID)
	}

	// Execute with the shared HTTP client (connection pooling).
	resp, err := veFileRelayHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload failed (HTTP %d): %s", resp.StatusCode, truncateVEStr(string(body), 200))
	}

	// Parse response to get file_url.
	var result struct {
		OK      bool   `json:"ok"`
		FileID  string `json:"file_id"`
		FileURL string `json:"file_url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse upload response failed: %w", err)
	}
	if !result.OK || result.FileURL == "" {
		return "", fmt.Errorf("upload response missing file_url")
	}

	// Return the full URL (Hub base + relative path).
	if strings.HasPrefix(result.FileURL, "/") {
		return hubURL + result.FileURL, nil
	}
	return result.FileURL, nil
}

// mimeTypeForFile returns a MIME type string based on file extension.
func mimeTypeForFile(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".log":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".yaml", ".yml":
		return "application/x-yaml"
	case ".go":
		return "text/x-go"
	case ".py":
		return "text/x-python"
	case ".js":
		return "text/javascript"
	case ".ts":
		return "text/typescript"
	case ".html":
		return "text/html"
	case ".css":
		return "text/css"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}

// SendVEMessageWithAttachments sends a message with file attachments in a VE conversation.
// For each file in filePaths:
//   - Text files (<=500KB): read content, base64 encode, inline TextAttachment
//   - Image files (<=10MB): upload to Hub file relay as ImageAttachment with file_url
//   - Document files (<=20MB): upload to Hub file relay as FileAttachment with file_url
//
// On upload failure, returns a specific error; the message text is preserved for retry.
func (a *App) SendVEMessageWithAttachments(sessionID string, content string, filePaths []string) error {
 return a.sendVEAttachmentMessage(sessionID, content, filePaths, nil)
}

// SendVEGroupMessageWithAttachments sends a VE group message with file attachments and @mention routing.
func (a *App) SendVEGroupMessageWithAttachments(sessionID string, content string, mentionedIds []string, filePaths []string) error {
 return a.sendVEAttachmentMessage(sessionID, content, filePaths, mentionedIds)
}

func (a *App) sendVEAttachmentMessage(sessionID string, content string, filePaths []string, mentionedIds []string) error {
	if strings.TrimSpace(content) == "" && len(filePaths) == 0 {
		return fmt.Errorf("message content and attachments are both empty")
	}
	if len([]rune(content)) > 32000 {
		return fmt.Errorf("message exceeds 32,000 character limit")
	}
	if len(filePaths) > 10 {
		return fmt.Errorf("too many attachments (max 10)")
	}

	// Get Hub credentials for file uploads.
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return fmt.Errorf("Hub credentials unavailable: %w", err)
	}

	var textAttachments []a2a.TextAttachment
	var imageAttachments []a2a.ImageAttachment
	var fileAttachments []a2a.FileAttachment

	for _, fp := range filePaths {
		// Classify file type by extension.
		category := classifyFileType(fp)
		if category == "" {
			return fmt.Errorf("unsupported file type: %s", filepath.Ext(fp))
		}

		// Validate file size.
		if err := validateFileSize(fp, category); err != nil {
			return err
		}

		filename := filepath.Base(fp)
		mimeType := mimeTypeForFile(fp)

		switch category {
		case "text":
			// Read and base64 encode for inline attachment.
			encoded, err := base64EncodeFile(fp)
			if err != nil {
				return fmt.Errorf("failed to encode text file %s: %w", filename, err)
			}
			textAttachments = append(textAttachments, a2a.TextAttachment{
				Content:  encoded,
				Filename: filename,
				MimeType: mimeType,
			})

		case "image":
			// Upload to Hub file relay.
			fileURL, err := a.uploadToFileRelay(hubURL, token, fp, sessionID)
			if err != nil {
				return fmt.Errorf("failed to upload image %s: %w", filename, err)
			}
			imageAttachments = append(imageAttachments, a2a.ImageAttachment{
				FileURL:  fileURL,
				Filename: filename,
				MimeType: mimeType,
			})

		case "document":
			// Upload to Hub file relay.
			fileURL, err := a.uploadToFileRelay(hubURL, token, fp, sessionID)
			if err != nil {
				return fmt.Errorf("failed to upload document %s: %w", filename, err)
			}
			// Get file size for metadata.
			info, _ := os.Stat(fp)
			var sizeBytes int64
			if info != nil {
				sizeBytes = info.Size()
			}
			fileAttachments = append(fileAttachments, a2a.FileAttachment{
				FileURL:   fileURL,
				Filename:  filename,
				MimeType:  mimeType,
				SizeBytes: sizeBytes,
			})
		}
	}

	// Construct and send the message with attachments.
	msg := a2a.GroupDiscussionMessage{
		Kind:             a2a.MessageStatement,
		Content:          content,
		TextAttachments:  textAttachments,
		ImageAttachments: imageAttachments,
		FileAttachments:  fileAttachments,
		CreatedAt:        time.Now(),
	}

	targets, err := a.resolveVEGroupMentionTargets(sessionID, content, mentionedIds)
	if err != nil {
		return err
	}

	if targets.Explicit && targets.Local && len(targets.RemoteToIDs) == 0 {
		if !a.tryLocalExecutorDispatch(sessionID, msg) {
			if _, err := a.RegisterLocalExecutorInGroup(sessionID); err != nil {
				return fmt.Errorf("local AI is not ready in this group: %w", err)
			}
			if !a.tryLocalExecutorDispatch(sessionID, msg) {
				return fmt.Errorf("local AI is not ready in this group; please add it again")
			}
		}
		return nil
	}

	if targets.Explicit && len(targets.RemoteToIDs) > 0 && !targets.Local {
		msg.ToIDs = targets.RemoteToIDs
		return a.sendVEA2AMessage(sessionID, msg)
	}

	// Local dispatch shortcut: when local AI is enabled for this session,
	// dispatch directly to the local agent without waiting for Hub round-trip.
	if a.tryLocalExecutorDispatch(sessionID, msg) {
		return nil
	}

	return a.sendVEA2AMessage(sessionID, msg)
}
