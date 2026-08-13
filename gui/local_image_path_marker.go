package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// Keep an image selected from the desktop file picker within the same bounded
// size envelope as other locally managed image assets. The selected file is
// user-provided, but it still becomes part of a remote multimodal request.
const maxSelectedLocalImageAttachmentBytes int64 = 20 * 1024 * 1024

// Base64 expands image data in the request. Bound the aggregate raw data too,
// so several valid files cannot make one user turn unexpectedly large.
const maxSelectedLocalImageAttachmentsTotalBytes int64 = maxSelectedLocalImageAttachmentBytes

// DecodeConfig reads metadata without decoding pixel data, which prevents
// compressed image bombs from reaching an external model request.
const maxSelectedLocalImageAttachmentPixels int64 = 40 * 1024 * 1024

type localImagePathLineKind int

const (
	localImagePathLineOther localImagePathLineKind = iota
	localImagePathLineInstruction
	localImagePathLineImagePath
)

func classifyLocalImagePathLine(line string) localImagePathLineKind {
	trimmed := strings.TrimSpace(strings.TrimPrefix(line, "-"))
	if trimmed == "" {
		return localImagePathLineOther
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "这是用户已经提供的本地图片文件") ||
		strings.HasPrefix(lower, "请直接使用这些路径") ||
		strings.HasPrefix(lower, "杩欐槸鐢ㄦ埛宸茬粡鎻愪緵鐨勬湰鍦板浘鐗囨枃浠?") ||
		strings.HasPrefix(lower, "璇风洿鎺ヤ娇鐢ㄨ繖浜涜矾寰?"):
		return localImagePathLineInstruction
	case isLocalImagePathExtension(filepath.Ext(lower)):
		return localImagePathLineImagePath
	default:
		return localImagePathLineOther
	}
}

func isLocalImagePathExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

// selectedLocalImageAttachments turns the image paths emitted by the desktop
// file picker into real multimodal attachments. A path alone is only useful to
// tools; vision models must receive the image bytes in the user content.
//
// Errors are returned as short host notes rather than failing the entire turn,
// so a missing/oversized image does not prevent the user from continuing with
// the rest of the request.
func selectedLocalImageAttachments(userText string) ([]MessageAttachment, []string) {
	paths := selectedLocalImagePaths(userText)
	attachments := make([]MessageAttachment, 0, len(paths))
	notes := make([]string, 0)
	var totalBytes int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			notes = append(notes, fmt.Sprintf("[Host note: selected image %q could not be read: %v]", filepath.Base(path), err))
			continue
		}
		if !info.Mode().IsRegular() {
			notes = append(notes, fmt.Sprintf("[Host note: selected image %q is not a regular file]", filepath.Base(path)))
			continue
		}
		if info.Size() <= 0 || info.Size() > maxSelectedLocalImageAttachmentBytes {
			notes = append(notes, fmt.Sprintf("[Host note: selected image %q is outside the %d MiB vision upload limit]", filepath.Base(path), maxSelectedLocalImageAttachmentBytes/(1024*1024)))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			notes = append(notes, fmt.Sprintf("[Host note: selected image %q could not be read: %v]", filepath.Base(path), err))
			continue
		}
		if int64(len(data)) == 0 || int64(len(data)) > maxSelectedLocalImageAttachmentBytes {
			notes = append(notes, fmt.Sprintf("[Host note: selected image %q is outside the %d MiB vision upload limit]", filepath.Base(path), maxSelectedLocalImageAttachmentBytes/(1024*1024)))
			continue
		}
		mimeType, err := validatedLocalImageMimeType(data)
		if err != nil {
			notes = append(notes, fmt.Sprintf("[Host note: selected image %q is not a supported, valid raster image]", filepath.Base(path)))
			continue
		}
		if totalBytes+int64(len(data)) > maxSelectedLocalImageAttachmentsTotalBytes {
			notes = append(notes, fmt.Sprintf("[Host note: selected image %q exceeds the %d MiB total vision upload limit for this turn]", filepath.Base(path), maxSelectedLocalImageAttachmentsTotalBytes/(1024*1024)))
			continue
		}
		totalBytes += int64(len(data))
		attachments = append(attachments, MessageAttachment{
			Type:     "image",
			FileName: filepath.Base(path),
			MimeType: mimeType,
			Data:     base64.StdEncoding.EncodeToString(data),
			Size:     int64(len(data)),
		})
	}
	return attachments, notes
}

// validatedLocalImageMimeType derives the type from decodable bytes, never
// the extension. Selected files cross a provider boundary, so a renamed
// arbitrary local file must not be emitted as an image data URL.
func validatedLocalImageMimeType(data []byte) (string, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return "", fmt.Errorf("decode image config")
	}
	if int64(config.Width)*int64(config.Height) > maxSelectedLocalImageAttachmentPixels {
		return "", fmt.Errorf("image dimensions exceed limit")
	}
	switch strings.ToLower(format) {
	case "jpeg":
		return "image/jpeg", nil
	case "png":
		return "image/png", nil
	case "gif":
		return "image/gif", nil
	case "webp":
		return "image/webp", nil
	default:
		return "", fmt.Errorf("unsupported image format %q", format)
	}
}

func selectedLocalImagePaths(userText string) []string {
	idx := strings.Index(userText, filePathPromptPrefix)
	if idx < 0 {
		return nil
	}
	paths := make([]string, 0, 1)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(userText[idx+len(filePathPromptPrefix):], "\n") {
		// Path marker sections end at the first non-path line. Continuing past
		// the host/frontend instruction could accidentally upload a filename
		// mentioned in ordinary user prose later in the message.
		kind := classifyLocalImagePathLine(line)
		if kind == localImagePathLineInstruction {
			break
		}
		if kind == localImagePathLineOther {
			if len(paths) > 0 && strings.TrimSpace(line) != "" {
				break
			}
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}
