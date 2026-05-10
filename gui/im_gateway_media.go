// gui/im_gateway_media.go — shared helpers for IM gateway media handling.
package main

import (
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
)

// buildMediaAttachment constructs a map suitable for the Hub's MessageAttachment
// JSON schema from raw media fields. Returns nil if no media is present.
// Voice media is automatically converted to WAV for ASR compatibility.
func buildMediaAttachment(mediaType string, mediaData []byte, mediaName, mimeType string) map[string]any {
	if mediaType == "" || len(mediaData) == 0 {
		return nil
	}
	mediaKind := normalizeIMMediaKind(mediaType)
	// Convert voice to WAV for unified ASR processing.
	if mediaKind.IsVoice() {
		mediaData, mediaName, mimeType = convertVoiceToWAV(mediaData, mediaName)
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
// Voice media is automatically converted to WAV before saving.
func saveMediaToTempDir(subDir, namePrefix, userID, mediaType string, mediaData []byte, mediaName string) (string, error) {
	// Convert voice to WAV for unified ASR processing.
	if normalizeIMMediaKind(mediaType).IsVoice() {
		mediaData, mediaName, _ = convertVoiceToWAV(mediaData, mediaName)
	}
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

// convertVoiceToWAV attempts to convert voice data (silk/ogg/opus) to 16kHz
// mono WAV for ASR. On success it returns the WAV bytes and updated metadata.
// On failure it logs the error and returns the original data unchanged.
func convertVoiceToWAV(mediaData []byte, mediaName string) ([]byte, string, string) {
	// Detect format hint from file extension or auto-detect.
	format := ""
	if mediaName != "" {
		ext := strings.ToLower(filepath.Ext(mediaName))
		switch ext {
		case ".silk", ".slk", ".amr", ".aud":
			format = audioconv.FormatSilk
		case ".ogg", ".oga", ".opus":
			format = audioconv.FormatOGG
		case ".wav":
			format = audioconv.FormatWAV
		}
	}

	wav, err := audioconv.ToWAV(mediaData, format)
	if err != nil {
		log.Printf("[im/media] voice→WAV conversion failed: %v (format=%q name=%q len=%d)", err, format, mediaName, len(mediaData))
		return mediaData, mediaName, guessMimeFromMedia("voice", mediaName)
	}

	// Update name and mime to reflect WAV output.
	newName := mediaName
	if newName != "" {
		ext := filepath.Ext(newName)
		newName = strings.TrimSuffix(newName, ext) + ".wav"
	} else {
		newName = "voice.wav"
	}
	log.Printf("[im/media] voice→WAV OK: %d → %d bytes", len(mediaData), len(wav))
	return wav, newName, "audio/wav"
}

// mediaExtension returns a default file extension for a media type.
func mediaExtension(mediaType string) string {
	switch normalizeIMMediaKind(mediaType) {
	case imMediaImage:
		return ".jpg"
	case imMediaVoice:
		return ".wav"
	case imMediaVideo:
		return ".mp4"
	default:
		return ".bin"
	}
}

func mediaTypeFromFileName(fileName string) string {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return imMediaImage.String()
	case ".mp4", ".avi", ".mov", ".mkv", ".webm":
		return imMediaVideo.String()
	case ".ogg", ".oga", ".opus", ".amr", ".silk", ".slk":
		return imMediaVoice.String()
	case ".wav", ".mp3", ".m4a", ".aac", ".flac":
		return imMediaAudio.String()
	default:
		return imMediaFile.String()
	}
}

func containsString(list []string, needle string) bool {
	for _, item := range list {
		if item == needle {
			return true
		}
	}
	return false
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
	switch normalizeIMMediaKind(mediaType) {
	case imMediaImage:
		return "image/jpeg"
	case imMediaVideo:
		return "video/mp4"
	case imMediaAudio:
		return "audio/mpeg"
	case imMediaVoice:
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}

// mediaLabel returns a Chinese label for a media type.
func mediaLabel(mediaType string) string {
	switch normalizeIMMediaKind(mediaType) {
	case imMediaImage:
		return "图片"
	case imMediaVoice:
		return "语音"
	case imMediaVideo:
		return "视频"
	case imMediaFile:
		return "文件"
	default:
		return "媒体"
	}
}

// buildLocalImageAttachment creates a MessageAttachment for an image received
// from a local IM gateway. If mimeType is empty it is guessed from mediaType
// and mediaName. This is the single place all three local gateways (WeChat,
// QQ, Telegram) use to construct image attachments for the LLM vision path.
func buildLocalImageAttachment(mediaData []byte, mediaName, mimeType string) MessageAttachment {
	if mimeType == "" {
		mimeType = guessMimeFromMedia(imMediaImage.String(), mediaName)
	}
	return MessageAttachment{
		Type:     imMediaImage.String(),
		FileName: mediaName,
		MimeType: mimeType,
		Data:     base64.StdEncoding.EncodeToString(mediaData),
		Size:     int64(len(mediaData)),
	}
}
