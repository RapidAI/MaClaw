package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
		attKind := normalizeIMMediaKind(att.Type)
		if isImageMime(att.MimeType) || attKind.IsImage() {
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
		} else if attKind.IsVoice() {
			// Voice attachment: decode, convert to WAV for ASR, then save.
			decoded, decErr := base64.StdEncoding.DecodeString(att.Data)
			if decErr != nil {
				log.Printf("[IM] decode voice attachment %q failed: %v", att.FileName, decErr)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s (解码失败: %v)]", att.FileName, decErr))
				continue
			}
			wavData, wavName, wavMime := convertVoiceToWAV(decoded, att.FileName)
			wavAtt := &MessageAttachment{
				Type:     imMediaVoice.String(),
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
			"type": string(imContentBlockText),
			"text": text,
		})
	}
	for _, img := range images {
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, map[string]interface{}{
			"type": string(imContentBlockImageURL),
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
			"type": string(imContentBlockText),
			"text": text,
		})
	}
	for _, img := range images {
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, map[string]interface{}{
			"type": string(imContentBlockImage),
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
	dir := filepath.Join(corelib.MaclawBaseDir(), "im_files")
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
// History image stripping
// ---------------------------------------------------------------------------

// filePathPromptPrefix is the marker used by the frontend to embed local file
// paths into the user message text.
const filePathPromptPrefix = "[用户选择的本地文件路径]"

// filePathPromptPrefixHistorical replaces filePathPromptPrefix in older
// conversation entries so the LLM does not confuse previous uploads with
// the current one.
const filePathPromptPrefixHistorical = "[之前选择的本地文件路径（仅供参考，非本次上传）]"

// stripHistoryAttachments processes a conversation message from history and
// removes base64 image data from multimodal content blocks, replacing them
// with a lightweight text placeholder. It also annotates the
// "[用户选择的本地文件路径]" section and IM-channel attachment descriptions
// so the LLM knows those files are from a previous turn, not the current
// upload.
//
// This prevents the LLM from treating images/files uploaded in earlier turns
// as part of the current request.
func stripHistoryAttachments(msg interface{}) interface{} {
	mm, ok := msg.(map[string]interface{})
	if !ok {
		return msg
	}
	role, _ := mm["role"].(string)
	if role != "user" {
		return msg
	}

	// Case 1: multimodal content ([]interface{} with image blocks).
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
			blockKind := normalizeIMContentBlockKind(blockType)
			if blockKind.IsImageBlock() {
				// Count images; a single merged placeholder is emitted below.
				imageCount++
				changed = true
			} else if blockKind.IsText() {
				if text, ok := bm["text"].(string); ok {
					if annotated := annotateHistoryAttachmentText(text); annotated != text {
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
		// Emit a single merged placeholder for all stripped images.
		if imageCount > 0 {
			placeholder := fmt.Sprintf("[之前上传了 %d 张图片]", imageCount)
			newBlocks = append(newBlocks, map[string]interface{}{
				"type": string(imContentBlockText),
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

	// Case 2: plain text content.
	if text, ok := mm["content"].(string); ok {
		if annotated := annotateHistoryAttachmentText(text); annotated != text {
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

// annotateHistoryAttachmentText rewrites attachment-related markers in a
// historical user message so the LLM does not confuse them with the current
// upload. It handles:
//   - [用户选择的本地文件路径]  (desktop panel file picker)
//   - [附件: xxx → 已保存到 yyy]  (IM channel non-image files)
//   - [用户发送了图片 xxx，已保存到 yyy ...] (IM channel images when vision unsupported)
//
// Returns the original string unchanged if no markers are found.
func annotateHistoryAttachmentText(text string) string {
	// 1. Desktop file picker section.
	if strings.Contains(text, filePathPromptPrefix) {
		text = strings.Replace(text, filePathPromptPrefix, filePathPromptPrefixHistorical, 1)
	}

	// 2. IM-channel attachment descriptions: [附件: xxx → 已保存到 yyy]
	if strings.Contains(text, "[附件:") {
		text = strings.ReplaceAll(text, "[附件:", "[之前的附件:")
	}

	// 3. IM-channel image fallback: [用户发送了图片 xxx，已保存到 yyy]
	if strings.Contains(text, "[用户发送了图片") {
		text = strings.ReplaceAll(text, "[用户发送了图片", "[之前发送的图片")
	}

	// 4. IM-channel voice: [语音: xxx → 已转换为WAV并保存到 yyy]
	if strings.Contains(text, "[语音:") {
		text = strings.ReplaceAll(text, "[语音:", "[之前的语音:")
	}

	return text
}

// ---------------------------------------------------------------------------
// System Prompt
// ---------------------------------------------------------------------------
