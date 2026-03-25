// gui/im_gateway_media.go — shared helpers for IM gateway media handling.
package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// buildMediaAttachment constructs a map suitable for the Hub's MessageAttachment
// JSON schema from raw media fields. Returns nil if no media is present.
func buildMediaAttachment(mediaType string, mediaData []byte, mediaName, mimeType string) map[string]any {
	if mediaType == "" || len(mediaData) == 0 {
		return nil
	}
	if mimeType == "" {
		mimeType = guessMimeFromMedia(mediaType, mediaName)
	}
	att := map[string]any{
		"type":      mediaType,
		"data":      base64.StdEncoding.EncodeToString(mediaData),
		"size":      len(mediaData),
		"mime_type": mimeType,
	}
	if mediaName != "" {
		att["file_name"] = mediaName
	}
	return att
}

// saveMediaToTempDir saves raw media bytes to ~/.maclaw/temp/<subDir>,
// returning the file path. The subDir identifies the IM source (e.g. "wx",
// "qq", "tg") and the namePrefix is used for the file name (e.g. "wx_").
func saveMediaToTempDir(subDir, namePrefix, userID, mediaType string, mediaData []byte, mediaName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".maclaw", "temp", subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := mediaName
	if name == "" {
		ext := mediaExtension(mediaType)
		name = namePrefix + userID + "_" + time.Now().Format("20060102_150405.000") + ext
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, mediaData, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// mediaExtension returns a default file extension for a media type.
func mediaExtension(mediaType string) string {
	switch mediaType {
	case "image":
		return ".jpg"
	case "voice":
		return ".silk"
	case "video":
		return ".mp4"
	default:
		return ".bin"
	}
}

// guessMimeFromMedia returns a MIME type based on the media category and file name.
func guessMimeFromMedia(mediaType, fileName string) string {
	if fileName != "" {
		ext := strings.ToLower(filepath.Ext(fileName))
		switch ext {
		case ".pdf":
			return "application/pdf"
		case ".doc":
			return "application/msword"
		case ".docx":
			return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		case ".xls":
			return "application/vnd.ms-excel"
		case ".xlsx":
			return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		case ".png":
			return "image/png"
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".gif":
			return "image/gif"
		case ".mp4":
			return "video/mp4"
		case ".mp3":
			return "audio/mpeg"
		case ".txt":
			return "text/plain"
		case ".zip":
			return "application/zip"
		}
	}
	switch mediaType {
	case "image":
		return "image/jpeg"
	case "video":
		return "video/mp4"
	case "voice":
		return "audio/silk"
	default:
		return "application/octet-stream"
	}
}

// mediaLabel returns a Chinese label for a media type.
func mediaLabel(mediaType string) string {
	switch mediaType {
	case "image":
		return "图片"
	case "voice":
		return "语音"
	case "video":
		return "视频"
	case "file":
		return "文件"
	default:
		return "媒体"
	}
}
