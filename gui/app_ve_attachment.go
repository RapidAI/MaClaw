package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	pathpkg "path"
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

const veFileAttachmentMaxSize = 50 * 1024 * 1024 // 50 MB

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

	// Return the full URL (Hub base + relative path). Validate the relay URL so
	// a malformed Hub response cannot smuggle an external attachment URL onward.
	fullURL := result.FileURL
	if strings.HasPrefix(result.FileURL, "/") {
		fullURL = hubURL + result.FileURL
	}
	if _, _, err := groupDiscussionAttachmentDownloadURL(hubURL, fullURL, sessionID, participantID); err != nil {
		return "", fmt.Errorf("upload response returned invalid file_url: %w", err)
	}
	return fullURL, nil
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

func (a *App) buildVEFileAttachmentMessage(sessionID, filePath, displayName, content string) (a2a.GroupDiscussionMessage, error) {
	filePath = strings.TrimSpace(filePath)
	msg, info, err := buildLocalVEFileAttachmentMessage(filePath, displayName, content)
	if err != nil {
		return a2a.GroupDiscussionMessage{}, err
	}

	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return a2a.GroupDiscussionMessage{}, fmt.Errorf("Hub credentials unavailable: %w", err)
	}
	fileURL, err := a.uploadToFileRelay(hubURL, token, filePath, sessionID)
	if err != nil {
		return a2a.GroupDiscussionMessage{}, err
	}

	if len(msg.ImageAttachments) > 0 {
		msg.ImageAttachments[0].FileURL = fileURL
		msg.ImageAttachments[0].LocalPath = ""
	}
	if len(msg.FileAttachments) > 0 {
		msg.FileAttachments[0].FileURL = fileURL
		msg.FileAttachments[0].LocalPath = ""
		msg.FileAttachments[0].SizeBytes = info.Size()
	}
	return msg, nil
}

func buildLocalVEFileAttachmentMessage(filePath, displayName, content string) (a2a.GroupDiscussionMessage, os.FileInfo, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return a2a.GroupDiscussionMessage{}, nil, fmt.Errorf("file path is required")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return a2a.GroupDiscussionMessage{}, nil, fmt.Errorf("cannot access file: %w", err)
	}
	if info.IsDir() {
		return a2a.GroupDiscussionMessage{}, nil, fmt.Errorf("%s is a directory", filePath)
	}
	if info.Size() > veFileAttachmentMaxSize {
		return a2a.GroupDiscussionMessage{}, nil, fmt.Errorf("file is too large: %d bytes; VE mode limit is 50 MB", info.Size())
	}

	filename := cleanVEAttachmentDisplayName(displayName)
	if filename == "" {
		filename = filepath.Base(filePath)
	}
	mimeType := mimeTypeForFile(filePath)
	msg := a2a.GroupDiscussionMessage{
		Kind:      a2a.MessageStatement,
		Content:   strings.TrimSpace(content),
		CreatedAt: time.Now(),
	}
	if imageExtensions[strings.ToLower(filepath.Ext(filePath))] {
		msg.ImageAttachments = []a2a.ImageAttachment{{LocalPath: filePath, Filename: filename, MimeType: mimeType}}
	} else {
		msg.FileAttachments = []a2a.FileAttachment{{LocalPath: filePath, Filename: filename, MimeType: mimeType, SizeBytes: info.Size()}}
	}
	return msg, info, nil
}

func cleanVEAttachmentDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimSpace(pathpkg.Base(name))
	if name == "." || name == "/" || name == string(filepath.Separator) {
		return ""
	}
	return name
}

func (a *App) sendVEFileAttachmentMessage(sessionID, filePath, displayName, content string) error {
	msg, err := a.buildVEFileAttachmentMessage(sessionID, filePath, displayName, content)
	if err != nil {
		return err
	}
	return a.sendVEA2AMessage(sessionID, msg)
}

// SendVEMessageWithAttachments sends a message with file attachments in a VE conversation.
// For remote delivery, files are uploaded through the Hub relay so the receiving
// digital employee can download and persist them on its own machine before the
// saved local path is passed into its agent context.
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

	targets, err := a.resolveVEGroupMentionTargets(sessionID, content, mentionedIds)
	if err != nil {
		return err
	}

	if targets.Explicit && targets.Local && len(targets.RemoteToIDs) == 0 {
		msg, err := a.prepareVEAttachmentMessage(sessionID, content, filePaths, false)
		if err != nil {
			return err
		}
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

	if targets.Explicit && targets.Local && len(targets.RemoteToIDs) > 0 {
		localMsg, err := a.prepareVEAttachmentMessage(sessionID, content, filePaths, false)
		if err != nil {
			return err
		}
		remoteMsg, err := a.prepareVEAttachmentMessage(sessionID, content, filePaths, true)
		if err != nil {
			return err
		}
		remoteMsg.ToIDs = targets.RemoteToIDs

		if err := a.sendVEA2AMessage(sessionID, remoteMsg); err != nil {
			return err
		}
		if !a.tryLocalExecutorDispatch(sessionID, localMsg) {
			if _, err := a.RegisterLocalExecutorInGroup(sessionID); err != nil {
				return fmt.Errorf("local AI is not ready in this group: %w", err)
			}
			if !a.tryLocalExecutorDispatch(sessionID, localMsg) {
				return fmt.Errorf("local AI is not ready in this group; please add it again")
			}
		}
		return nil
	}

	if targets.Explicit && len(targets.RemoteToIDs) > 0 && !targets.Local {
		msg, err := a.prepareVEAttachmentMessage(sessionID, content, filePaths, true)
		if err != nil {
			return err
		}
		msg.ToIDs = targets.RemoteToIDs
		return a.sendVEA2AMessage(sessionID, msg)
	}

	msg, err := a.prepareVEAttachmentMessage(sessionID, content, filePaths, true)
	if err != nil {
		return err
	}
	msg.ToIDs = a.groupDiscussionUnmentionedTargetIDs(sessionID)
	return a.sendVEA2AMessage(sessionID, msg)
}

func (a *App) prepareVEAttachmentMessage(sessionID, content string, filePaths []string, uploadRemoteFiles bool) (a2a.GroupDiscussionMessage, error) {
	var hubURL, token string
	if uploadRemoteFiles {
		var err error
		hubURL, token, err = a.getHubCredentials()
		if err != nil {
			return a2a.GroupDiscussionMessage{}, fmt.Errorf("Hub credentials unavailable: %w", err)
		}
	}

	var imageAttachments []a2a.ImageAttachment
	var fileAttachments []a2a.FileAttachment
	for _, fp := range filePaths {
		category := classifyFileType(fp)
		if category == "" {
			return a2a.GroupDiscussionMessage{}, fmt.Errorf("unsupported file type: %s", filepath.Ext(fp))
		}
		filename := filepath.Base(fp)
		mimeType := mimeTypeForFile(fp)
		switch category {
		case "text":
			if err := validateFileSize(fp, "document"); err != nil {
				return a2a.GroupDiscussionMessage{}, err
			}
			info, _ := os.Stat(fp)
			var sizeBytes int64
			if info != nil {
				sizeBytes = info.Size()
			}
			att := a2a.FileAttachment{Filename: filename, MimeType: mimeType, SizeBytes: sizeBytes}
			if uploadRemoteFiles {
				fileURL, err := a.uploadToFileRelay(hubURL, token, fp, sessionID)
				if err != nil {
					return a2a.GroupDiscussionMessage{}, fmt.Errorf("failed to upload text file %s: %w", filename, err)
				}
				att.FileURL = fileURL
			} else {
				att.LocalPath = fp
			}
			fileAttachments = append(fileAttachments, att)
		case "image":
			if err := validateFileSize(fp, category); err != nil {
				return a2a.GroupDiscussionMessage{}, err
			}
			att := a2a.ImageAttachment{Filename: filename, MimeType: mimeType}
			if uploadRemoteFiles {
				fileURL, err := a.uploadToFileRelay(hubURL, token, fp, sessionID)
				if err != nil {
					return a2a.GroupDiscussionMessage{}, fmt.Errorf("failed to upload image %s: %w", filename, err)
				}
				att.FileURL = fileURL
			} else {
				att.LocalPath = fp
			}
			imageAttachments = append(imageAttachments, att)
		case "document":
			if err := validateFileSize(fp, category); err != nil {
				return a2a.GroupDiscussionMessage{}, err
			}
			info, _ := os.Stat(fp)
			var sizeBytes int64
			if info != nil {
				sizeBytes = info.Size()
			}
			att := a2a.FileAttachment{Filename: filename, MimeType: mimeType, SizeBytes: sizeBytes}
			if uploadRemoteFiles {
				fileURL, err := a.uploadToFileRelay(hubURL, token, fp, sessionID)
				if err != nil {
					return a2a.GroupDiscussionMessage{}, fmt.Errorf("failed to upload document %s: %w", filename, err)
				}
				att.FileURL = fileURL
			} else {
				att.LocalPath = fp
			}
			fileAttachments = append(fileAttachments, att)
		}
	}

	return a2a.GroupDiscussionMessage{
		Kind:             a2a.MessageStatement,
		Content:          content,
		ImageAttachments: imageAttachments,
		FileAttachments:  fileAttachments,
		CreatedAt:        time.Now(),
	}, nil
}
