package agent

// attachment.go implements standalone functions for building multimodal user
// content from message attachments and managing attachment history.
//
// These functions are used by both GUI and TUI to construct LLM-compatible
// message payloads from user messages with images, files, and voice.
//
// Migrated from gui/im_attachment.go as part of the agent-unification plan.

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// VoiceConverter is a callback that converts raw voice data to WAV format.
// Returns (wavData, wavFileName, wavMimeType). Implementations live in gui/
// or tui/ where media conversion dependencies are available.
type VoiceConverter func(data []byte, fileName string) ([]byte, string, string)

// BuildUserContent constructs the user message content for the LLM.
// For text-only messages, returns a plain string.
// For messages with image attachments, returns a multimodal content array
// compatible with OpenAI/Anthropic vision APIs.
// Non-image files are saved locally and their paths are appended to the text.
//
// voiceConvert may be nil if voice conversion is not supported.
func BuildUserContent(userText string, attachments []MessageAttachment, protocol string, supportsVision bool, voiceConvert VoiceConverter) interface{} {
	if len(attachments) == 0 {
		return userText
	}

	var imageAttachments []MessageAttachment
	var fileDescriptions []string

	for i := range attachments {
		att := &attachments[i]
		if IsImageMime(att.MimeType) || att.Type == "image" {
			if supportsVision {
				imageAttachments = append(imageAttachments, *att)
			} else {
				displayName := att.FileName
				if displayName == "" {
					displayName = "image"
				}
				path, err := SaveAttachmentToLocal(att)
				if err != nil {
					log.Printf("[IM] save image %q failed: %v", att.FileName, err)
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[用户发送了图片 %s，保存失败: %v，当前模型不支持图片理解]", displayName, err))
				} else {
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[用户发送了图片 %s，已保存到 %s，当前模型不支持图片理解]", displayName, path))
				}
			}
		} else if att.Type == "voice" {
			decoded, decErr := base64.StdEncoding.DecodeString(att.Data)
			if decErr != nil {
				log.Printf("[IM] decode voice attachment %q failed: %v", att.FileName, decErr)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s (解码失败: %v)]", att.FileName, decErr))
				continue
			}
			if voiceConvert == nil {
				// No voice converter available — save raw file.
				path, err := SaveAttachmentToLocal(att)
				if err != nil {
					log.Printf("[IM] save voice %q failed: %v", att.FileName, err)
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s (保存失败: %v)]", att.FileName, err))
				} else {
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s → 已保存到 %s]", att.FileName, path))
				}
				continue
			}
			wavData, wavName, wavMime := voiceConvert(decoded, att.FileName)
			wavAtt := &MessageAttachment{
				Type:     "voice",
				FileName: wavName,
				MimeType: wavMime,
				Data:     base64.StdEncoding.EncodeToString(wavData),
				Size:     int64(len(wavData)),
			}
			path, err := SaveAttachmentToLocal(wavAtt)
			if err != nil {
				log.Printf("[IM] save voice %q failed: %v", att.FileName, err)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s (保存失败: %v)]", att.FileName, err))
			} else if wavMime == "audio/wav" {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s → 已转换为WAV并保存到 %s，请使用ASR工具进行语音识别]", att.FileName, path))
			} else {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s → 转换失败，原始文件已保存到 %s]", att.FileName, path))
			}
		} else {
			path, err := SaveAttachmentToLocal(att)
			if err != nil {
				log.Printf("[IM] save attachment %q failed: %v", att.FileName, err)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[附件: %s (保存失败: %v)]", att.FileName, err))
			} else {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[附件: %s → 已保存到 %s]", att.FileName, path))
			}
		}
	}

	fullText := userText
	if len(fileDescriptions) > 0 {
		if fullText != "" {
			fullText += "\n\n"
		}
		fullText += strings.Join(fileDescriptions, "\n")
	}

	if len(imageAttachments) == 0 {
		return fullText
	}

	if protocol == "anthropic" {
		return BuildAnthropicVisionContent(fullText, imageAttachments)
	}
	return BuildOpenAIVisionContent(fullText, imageAttachments)
}

// BuildOpenAIVisionContent creates content blocks for OpenAI vision API.
func BuildOpenAIVisionContent(text string, images []MessageAttachment) []interface{} {
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

// BuildAnthropicVisionContent creates content blocks for Anthropic vision API.
func BuildAnthropicVisionContent(text string, images []MessageAttachment) []interface{} {
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

// SaveAttachmentToLocal saves a MessageAttachment to ~/.maclaw/im_files/
// and returns the absolute path.
func SaveAttachmentToLocal(att *MessageAttachment) (string, error) {
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

// IsImageMime returns true if the MIME type is an image type.
func IsImageMime(mimeType string) bool {
	return strings.HasPrefix(strings.ToLower(mimeType), "image/")
}

// ---------------------------------------------------------------------------
// History image stripping
// ---------------------------------------------------------------------------

// FilePathPromptPrefix is the marker used by the frontend to embed local file
// paths into the user message text.
const FilePathPromptPrefix = "[用户选择的本地文件路径]"

// FilePathPromptPrefixHistorical replaces FilePathPromptPrefix in older
// conversation entries so the LLM does not confuse previous uploads with
// the current one.
const FilePathPromptPrefixHistorical = "[之前选择的本地文件路径（仅供参考，非本次上传）]"

// StripHistoryAttachments processes a conversation message from history and
// removes base64 image data from multimodal content blocks, replacing them
// with a lightweight text placeholder. It also annotates the
// "[用户选择的本地文件路径]" section and IM-channel attachment descriptions
// so the LLM knows those files are from a previous turn, not the current
// upload.
func StripHistoryAttachments(msg interface{}) interface{} {
	mm, ok := msg.(map[string]interface{})
	if !ok {
		return msg
	}
	role, _ := mm["role"].(string)
	if role != "user" {
		return msg
	}

	if blocks, ok := mm["content"].([]interface{}); ok {
		changed := false
		imageCount := 0
		newBlocks := make([]interface{}, 0, len(blocks))
		for _, block := range blocks {
			bm, ok := block.(map[string]interface{})
			if !ok {
				newBlocks = append(newBlocks, block)
				continue
			}
			blockType, _ := bm["type"].(string)
			if blockType == "image_url" || blockType == "image" {
				imageCount++
				changed = true
			} else if blockType == "text" {
				if text, ok := bm["text"].(string); ok {
					if annotated := AnnotateHistoryAttachmentText(text); annotated != text {
						cp := make(map[string]interface{}, len(bm))
						for k, v := range bm {
							cp[k] = v
						}
						cp["text"] = annotated
						newBlocks = append(newBlocks, cp)
						changed = true
					} else {
						newBlocks = append(newBlocks, block)
					}
				} else {
					newBlocks = append(newBlocks, block)
				}
			} else {
				newBlocks = append(newBlocks, block)
			}
		}
		if imageCount > 0 {
			placeholder := fmt.Sprintf("[之前上传了 %d 张图片]", imageCount)
			newBlocks = append(newBlocks, map[string]interface{}{
				"type": "text",
				"text": placeholder,
			})
		}
		if changed {
			cp := make(map[string]interface{}, len(mm))
			for k, v := range mm {
				cp[k] = v
			}
			cp["content"] = newBlocks
			return cp
		}
		return msg
	}

	if text, ok := mm["content"].(string); ok {
		if annotated := AnnotateHistoryAttachmentText(text); annotated != text {
			cp := make(map[string]interface{}, len(mm))
			for k, v := range mm {
				cp[k] = v
			}
			cp["content"] = annotated
			return cp
		}
	}

	return msg
}

// AnnotateHistoryAttachmentText rewrites attachment-related markers in a
// historical user message so the LLM does not confuse them with the current
// upload.
func AnnotateHistoryAttachmentText(text string) string {
	if strings.Contains(text, FilePathPromptPrefix) {
		text = strings.Replace(text, FilePathPromptPrefix, FilePathPromptPrefixHistorical, 1)
	}
	if strings.Contains(text, "[附件:") {
		text = strings.ReplaceAll(text, "[附件:", "[之前的附件:")
	}
	if strings.Contains(text, "[用户发送了图片") {
		text = strings.ReplaceAll(text, "[用户发送了图片", "[之前发送的图片")
	}
	if strings.Contains(text, "[语音:") {
		text = strings.ReplaceAll(text, "[语音:", "[之前的语音:")
	}
	return text
}
