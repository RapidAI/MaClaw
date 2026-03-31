package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// buildUserContent constructs the user message content for the LLM.
// For text-only messages, returns a plain string.
// For messages with image attachments, returns a multimodal content array
// compatible with OpenAI/Anthropic vision APIs.
// Non-image files are saved locally and their paths are appended to the text.
func buildUserContent(userText string, attachments []MessageAttachment, protocol string, supportsVision bool) interface{} {
	if len(attachments) == 0 {
		return userText
	}

	var imageAttachments []MessageAttachment
	var fileDescriptions []string

	for i := range attachments {
		att := &attachments[i]
		if isImageMime(att.MimeType) || att.Type == "image" {
			if supportsVision {
				imageAttachments = append(imageAttachments, *att)
			} else {
				// Vision not supported — save image to local file instead.
				displayName := att.FileName
				if displayName == "" {
					displayName = "image"
				}
				path, err := saveAttachmentToLocal(att)
				if err != nil {
					log.Printf("[IM] save image %q failed: %v", att.FileName, err)
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[用户发送了图片 %s，保存失败: %v，当前模型不支持图片理解]", displayName, err))
				} else {
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[用户发送了图片 %s，已保存到 %s，当前模型不支持图片理解]", displayName, path))
				}
			}
		} else if att.Type == "voice" {
			// Voice attachment: decode, convert to WAV for ASR, then save.
			decoded, decErr := base64.StdEncoding.DecodeString(att.Data)
			if decErr != nil {
				log.Printf("[IM] decode voice attachment %q failed: %v", att.FileName, decErr)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s (解码失败: %v)]", att.FileName, decErr))
				continue
			}
			wavData, wavName, wavMime := convertVoiceToWAV(decoded, att.FileName)
			wavAtt := &MessageAttachment{
				Type:     "voice",
				FileName: wavName,
				MimeType: wavMime,
				Data:     base64.StdEncoding.EncodeToString(wavData),
				Size:     int64(len(wavData)),
			}
			path, err := saveAttachmentToLocal(wavAtt)
			if err != nil {
				log.Printf("[IM] save voice %q failed: %v", att.FileName, err)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s (保存失败: %v)]", att.FileName, err))
			} else if wavMime == "audio/wav" {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s → 已转换为WAV并保存到 %s，请使用ASR工具进行语音识别]", att.FileName, path))
			} else {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s → 转换失败，原始文件已保存到 %s]", att.FileName, path))
			}
		} else {
			// Save non-image files to local disk so the agent can operate on them.
			path, err := saveAttachmentToLocal(att)
			if err != nil {
				log.Printf("[IM] save attachment %q failed: %v", att.FileName, err)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[附件: %s (保存失败: %v)]", att.FileName, err))
			} else {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[附件: %s → 已保存到 %s]", att.FileName, path))
			}
		}
	}

	// Build text with file descriptions appended.
	fullText := userText
	if len(fileDescriptions) > 0 {
		if fullText != "" {
			fullText += "\n\n"
		}
		fullText += strings.Join(fileDescriptions, "\n")
	}

	// If no images, return plain text (with file descriptions).
	if len(imageAttachments) == 0 {
		return fullText
	}

	// Build multimodal content blocks for vision API.
	if protocol == "anthropic" {
		return buildAnthropicVisionContent(fullText, imageAttachments)
	}
	return buildOpenAIVisionContent(fullText, imageAttachments)
}

// buildOpenAIVisionContent creates content blocks for OpenAI vision API.
// Format: [{type: "text", text: "..."}, {type: "image_url", image_url: {url: "data:mime;base64,..."}}]
func buildOpenAIVisionContent(text string, images []MessageAttachment) []interface{} {
	var blocks []interface{}
	if text != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}
	for _, img := range images {
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": fmt.Sprintf("data:%s;base64,%s", mime, img.Data),
			},
		})
	}
	return blocks
}

// buildAnthropicVisionContent creates content blocks for Anthropic vision API.
// Format: [{type: "text", text: "..."}, {type: "image", source: {type: "base64", media_type: "...", data: "..."}}]
func buildAnthropicVisionContent(text string, images []MessageAttachment) []interface{} {
	var blocks []interface{}
	if text != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}
	for _, img := range images {
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": mime,
				"data":       img.Data,
			},
		})
	}
	return blocks
}

// saveAttachmentToLocal saves a MessageAttachment to ~/.maclaw/im_files/
// and returns the absolute path.
func saveAttachmentToLocal(att *MessageAttachment) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".maclaw", "im_files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create im_files directory: %w", err)
	}

	name := att.FileName
	if name == "" {
		name = fmt.Sprintf("attachment_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	}
	name = filepath.Base(name)
	if name == "." || name == ".." {
		name = fmt.Sprintf("attachment_%d", time.Now().UnixMilli())
	}

	// Prepend timestamp to avoid collisions when multiple users send same-named files.
	prefix := fmt.Sprintf("%d_", time.Now().UnixMilli())
	name = prefix + name

	filePath := filepath.Join(dir, name)
	decoded, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return filePath, nil
}

// isImageMime returns true if the MIME type is an image type.
func isImageMime(mime string) bool {
	return strings.HasPrefix(strings.ToLower(mime), "image/")
}

// ---------------------------------------------------------------------------
// System Prompt
// ---------------------------------------------------------------------------
