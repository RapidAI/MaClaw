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

	"github.com/RapidAI/CodeClaw/corelib"
)

// VoiceConverter is a callback that converts raw voice data to WAV format.
// Returns (wavData, wavFileName, wavMimeType). Implementations live in gui/
// or tui/ where media conversion dependencies are available.
type VoiceConverter func(data []byte, fileName string) ([]byte, string, string)

// ImageTextRecognizer extracts the text of an image attachment with the local
// OCR engine. It is only invoked when the target model does NOT support vision
// (supportsVision == false) — the "multimodal first, OCR only otherwise"
// policy. Return an error when nothing usable was recognized; the caller then
// keeps the plain save-to-local description.
type ImageTextRecognizer func(mimeType, base64Data string) (string, error)

// imageOCRMaxRunes caps the OCR text appended to an image-attachment
// description so a text-heavy screenshot cannot blow up the prompt.
const imageOCRMaxRunes = 2000

// Image OCR note markers. The note body is wrapped in explicit begin/end
// marker lines (same pattern as the auto_extract document bodies) so history
// compaction can replace the — potentially 2000-rune — body with a one-line
// placeholder instead of re-sending it on every subsequent turn.
const (
	imageOCRNoteBeginMarker    = "--- image_ocr: begin ---"
	imageOCRNoteEndMarker      = "--- image_ocr: end ---"
	imageOCRHistoryPlaceholder = "[图片的本地 OCR 文字内容（历史消息，正文已省略；如需请重新识别）]"
)

// BuildUserContent constructs the user message content for the LLM.
// For text-only messages, returns a plain string.
// For messages with image attachments, returns a multimodal content array
// compatible with OpenAI/Anthropic vision APIs.
// Non-image files are saved locally and their paths are appended to the text.
// Images sent to a non-vision model are saved locally and, when imageOCR is
// non-nil, their locally OCR-recognized text is appended as well.
//
// voiceConvert and imageOCR may be nil when unsupported.
func BuildUserContent(userText string, attachments []MessageAttachment, protocol string, supportsVision bool, imageOCR ImageTextRecognizer, voiceConvert VoiceConverter) interface{} {
	return BuildUserContentWithAttachmentStagingDir(userText, attachments, protocol, supportsVision, imageOCR, voiceConvert, "")
}

// BuildUserContentWithAttachmentStagingDir uses a caller-owned attachment
// directory when one is supplied. An empty directory preserves the desktop/TUI
// default (<MaclawDataDir>/im_files). Service hosts should supply their
// request-scoped data directory so document auto-extracts and later
// read_document calls remain within the same tenant boundary.
func BuildUserContentWithAttachmentStagingDir(userText string, attachments []MessageAttachment, protocol string, supportsVision bool, imageOCR ImageTextRecognizer, voiceConvert VoiceConverter, stagingDir string) interface{} {
	return buildUserContentWithAttachmentStagingDirAndSettings(userText, attachments, protocol, supportsVision, imageOCR, voiceConvert, stagingDir, currentOfficeReadSettings())
}

// BuildUserContentWithAttachmentStagingDirAndOfficeReadConfig builds a user
// turn under an explicit trusted OfficeRead policy. Multi-tenant hosts use it
// so concurrent attachment extraction never relies on a process-global policy.
func BuildUserContentWithAttachmentStagingDirAndOfficeReadConfig(userText string, attachments []MessageAttachment, protocol string, supportsVision bool, imageOCR ImageTextRecognizer, voiceConvert VoiceConverter, stagingDir string, config OfficeReadConfig) interface{} {
	return buildUserContentWithAttachmentStagingDirAndSettings(userText, attachments, protocol, supportsVision, imageOCR, voiceConvert, stagingDir, officeReadSettingsForConfig(config))
}

// BuildUserContentWithAttachmentStagingDirAndOfficeReadConfigWithContext is
// the context-aware variant for hosts that know the active model window. It
// scales automatic document extraction before the payload is assembled.
func BuildUserContentWithAttachmentStagingDirAndOfficeReadConfigWithContext(userText string, attachments []MessageAttachment, protocol string, supportsVision bool, imageOCR ImageTextRecognizer, voiceConvert VoiceConverter, stagingDir string, config OfficeReadConfig, contextTokens int) interface{} {
	return buildUserContentWithAttachmentStagingDirAndSettingsWithContext(userText, attachments, protocol, supportsVision, imageOCR, voiceConvert, stagingDir, officeReadSettingsForConfig(config), contextTokens)
}

func buildUserContentWithAttachmentStagingDirAndSettings(userText string, attachments []MessageAttachment, protocol string, supportsVision bool, imageOCR ImageTextRecognizer, voiceConvert VoiceConverter, stagingDir string, settings officeReadSettings) interface{} {
	return buildUserContentWithAttachmentStagingDirAndSettingsWithContext(userText, attachments, protocol, supportsVision, imageOCR, voiceConvert, stagingDir, settings, 0)
}

func buildUserContentWithAttachmentStagingDirAndSettingsWithContext(userText string, attachments []MessageAttachment, protocol string, supportsVision bool, imageOCR ImageTextRecognizer, voiceConvert VoiceConverter, stagingDir string, settings officeReadSettings, contextTokens int) interface{} {
	// Always expand GUI file-picker document paths (no-op when already expanded / no marker).
	userText = expandUserSelectedFilePathsWithSettingsAndBudget(userText, settings, contextTokens)
	if len(attachments) == 0 {
		return userText
	}

	var imageAttachments []MessageAttachment
	var fileDescriptions []string

	for i := range attachments {
		att := &attachments[i]
		// Remote attachment type/MIME fields are metadata, not a content trust
		// boundary. A PDF or Office document must not be promoted to a vision
		// data URL merely because a gateway labeled it image/* or type=image.
		if !IsBinaryDocumentAttachment(att.FileName, att.MimeType) && (IsImageMime(att.MimeType) || att.Type == "image") {
			if supportsVision {
				imageAttachments = append(imageAttachments, *att)
			} else {
				displayName := att.FileName
				if displayName == "" {
					displayName = "image"
				}
				ocrNote := RecognizeImageTextNote(imageOCR, att, displayName)
				path, err := saveAttachmentToStagingDir(att, stagingDir)
				if err != nil {
					log.Printf("[IM] save image %q failed: %v", att.FileName, err)
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[用户发送了图片 %s，保存失败: %v，当前模型不支持图片理解]%s", displayName, err, ocrNote))
				} else {
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[用户发送了图片 %s，已保存到 %s，当前模型不支持图片理解]%s", displayName, path, ocrNote))
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
				path, err := saveAttachmentToStagingDir(att, stagingDir)
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
			path, err := saveAttachmentToStagingDir(wavAtt, stagingDir)
			if err != nil {
				log.Printf("[IM] save voice %q failed: %v", att.FileName, err)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s (保存失败: %v)]", att.FileName, err))
			} else if wavMime == "audio/wav" {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s → 已转换为WAV并保存到 %s，请使用ASR工具进行语音识别]", att.FileName, path))
			} else {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[语音: %s → 转换失败，原始文件已保存到 %s]", att.FileName, path))
			}
		} else {
			path, err := saveAttachmentToStagingDir(att, stagingDir)
			if err != nil {
				log.Printf("[IM] save attachment %q failed: %v", att.FileName, err)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[附件: %s (保存失败: %v)]", att.FileName, err))
			} else {
				// Path first; shared-budget extracts appended below.
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[附件: %s → 已保存到 %s]", att.FileName, path))
			}
		}
	}

	fileDescriptions = appendDocumentExtractsToDescriptionsWithSettingsAndBudget(fileDescriptions, userText, settings, contextTokens)

	// userText was already expanded at function entry (idempotent).
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

// SaveAttachmentToLocal saves a MessageAttachment under the active data directory
// and returns the absolute path.
func SaveAttachmentToLocal(att *MessageAttachment) (string, error) {
	return saveAttachmentToStagingDir(att, "")
}

func saveAttachmentToStagingDir(att *MessageAttachment, stagingDir string) (string, error) {
	dir := strings.TrimSpace(stagingDir)
	if dir == "" {
		dir = filepath.Join(corelib.MaclawDataDir(), "im_files")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create im_files directory: %w", err)
	}

	name := NormalizeBinaryDocumentAttachmentFilename(att.FileName, att.MimeType)
	if name == "" {
		name = fmt.Sprintf("attachment_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
		if ext := BinaryDocumentAttachmentExtension("", att.MimeType); ext != "" {
			name += ext
		}
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

// BinaryDocumentAttachmentExtension returns the canonical suffix for a PDF or
// one of the six supported Office document types. Filename identity wins when
// available; otherwise the declared MIME preserves an extension while staging
// an unnamed attachment. It does not validate document bytes: the downstream
// PDF/OfficeRead boundary remains responsible for signature and container
// checks.
func BinaryDocumentAttachmentExtension(fileName, mimeType string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx":
		return strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	}
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])) {
	case "application/pdf":
		return ".pdf"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.ms-powerpoint":
		return ".ppt"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	case "application/vnd.ms-excel":
		return ".xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	default:
		return ""
	}
}

// IsBinaryDocumentAttachment reports whether metadata identifies a PDF or one
// of the six Office formats. It is intentionally used only to prevent visual
// injection and retain a staging suffix; actual extraction remains governed by
// the parser's content and safety checks.
func IsBinaryDocumentAttachment(fileName, mimeType string) bool {
	return BinaryDocumentAttachmentExtension(fileName, mimeType) != ""
}

// NormalizeBinaryDocumentAttachmentFilename returns a staging-safe filename
// that preserves a PDF/Office extension. A filename that already has one of
// those supported suffixes remains authoritative. Otherwise, a declared
// binary-document MIME replaces any unrelated suffix: keeping e.g. .png on a
// document classified by MIME would prevent the later path-based document
// router from reaching the OfficeRead preflight. This is a routing label only;
// parsers still validate the actual bytes and reject mismatches.
func NormalizeBinaryDocumentAttachmentFilename(fileName, mimeType string) string {
	name := filepath.Base(strings.TrimSpace(fileName))
	if name == "." || name == string(filepath.Separator) {
		name = ""
	}
	filenameExt := BinaryDocumentAttachmentExtension(name, "")
	if filenameExt != "" {
		return name
	}
	mimeExt := BinaryDocumentAttachmentExtension("", mimeType)
	if mimeExt == "" {
		return name
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.TrimSpace(base)
	if base == "." || base == ".." {
		base = ""
	}
	if base == "" {
		return ""
	}
	return base + mimeExt
}

// RecognizeImageTextNote runs the optional local-OCR recognizer for an image
// attachment bound for a non-vision model and formats the result as a
// bracketed note appended to the attachment description. Returns "" when no
// recognizer is configured or recognition yields nothing.
func RecognizeImageTextNote(imageOCR ImageTextRecognizer, att *MessageAttachment, displayName string) string {
	if imageOCR == nil {
		return ""
	}
	text, err := imageOCR(att.MimeType, att.Data)
	if err != nil {
		log.Printf("[IM] OCR image %q failed: %v", displayName, err)
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return ""
	}
	if len(runes) > imageOCRMaxRunes {
		runes = runes[:imageOCRMaxRunes]
	}
	return fmt.Sprintf("\n[图片 %s 的文字内容（本地 OCR 识别）]:\n%s\n%s\n%s", displayName, imageOCRNoteBeginMarker, string(runes), imageOCRNoteEndMarker)
}

// StripImageOCRNotes replaces the body of every image OCR note with a short
// placeholder. Used when a user turn becomes history: the OCR text was
// already consumed by the model that turn, and re-sending up to
// imageOCRMaxRunes per image on every later turn wastes context.
func StripImageOCRNotes(text string) string {
	if !strings.Contains(text, imageOCRNoteBeginMarker) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) / 2)
	inBody := false
	first := true
	writeLine := func(s string) {
		if !first {
			b.WriteByte('\n')
		}
		b.WriteString(s)
		first = false
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == imageOCRNoteBeginMarker {
			inBody = true
			writeLine(imageOCRHistoryPlaceholder)
			continue
		}
		if inBody {
			if trimmed == imageOCRNoteEndMarker {
				inBody = false
			}
			continue
		}
		writeLine(line)
	}
	return b.String()
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
// upload. Also strips auto-extracted document bodies to keep multi-turn context bounded.
func AnnotateHistoryAttachmentText(text string) string {
	// Drop large auto-injected document bodies first (paths kept as one-line summaries).
	text = StripAutoExtractBodies(text)
	// Collapse image OCR note bodies (kept as one-line placeholders).
	text = StripImageOCRNotes(text)
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
