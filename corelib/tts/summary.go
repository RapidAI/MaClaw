package tts

import (
	"encoding/json"
	"regexp"
	"strings"
)

// voiceSummaryInput is the structured input from the frontend.
type voiceSummaryInput struct {
	UserText string `json:"userText"` // user's original request
	Status   string `json:"status"`   // "success", "error", "paused", "needs_confirmation"
}

// GenerateVoiceSummary generates a spoken status announcement from structured input.
//
// Input is a JSON string: {"userText": "修复登录页面bug", "status": "success"}
// Output: "任务已完成，该任务是修复登录页面bug"
//
// If input is not valid JSON, treats it as plain text and generates a generic summary.
// maxRunes controls the maximum spoken text length (0 = default 150).
func GenerateVoiceSummary(input string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = 150
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	// Try to parse as structured input
	var si voiceSummaryInput
	if err := json.Unmarshal([]byte(input), &si); err == nil && si.Status != "" {
		return buildStructuredSummary(si, maxRunes)
	}

	// Fallback: plain text — just announce completion
	cleaned := cleanForSpeech(input)
	if cleaned == "" {
		return "任务完成"
	}
	return truncateRunes("任务完成。"+cleaned, maxRunes)
}

// buildStructuredSummary generates the spoken sentence from structured input.
func buildStructuredSummary(si voiceSummaryInput, maxRunes int) string {
	// Status phrase
	var statusPhrase string
	switch si.Status {
	case "error":
		statusPhrase = "任务处理失败"
	case "paused":
		statusPhrase = "任务已暂停"
	case "needs_confirmation":
		statusPhrase = "需要任务确认"
	default:
		statusPhrase = "任务已完成"
	}

	// Clean the user text for speech
	taskDesc := cleanForSpeech(si.UserText)
	if taskDesc == "" {
		return statusPhrase
	}

	// Remove common prefixes that don't add information
	taskDesc = strings.TrimPrefix(taskDesc, "请")
	taskDesc = strings.TrimPrefix(taskDesc, "帮我")
	taskDesc = strings.TrimPrefix(taskDesc, "帮忙")
	taskDesc = strings.TrimPrefix(taskDesc, "麻烦")
	taskDesc = strings.TrimSpace(taskDesc)

	if taskDesc == "" {
		return statusPhrase
	}

	// Compose: "任务已完成，该任务是修复登录页面bug"
	full := statusPhrase + "，该任务是" + taskDesc
	return truncateRunes(full, maxRunes)
}

// truncateRunes truncates text to maxRunes, cutting at a sentence boundary if possible.
func truncateRunes(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}

	// Try to cut at a natural boundary
	sentenceEnders := []rune{'。', '！', '？', '.', '!', '?', '；', ';', '，', ','}
	bestCut := 0
	for i := 0; i < maxRunes && i < len(runes); i++ {
		for _, e := range sentenceEnders {
			if runes[i] == e {
				bestCut = i + 1
			}
		}
	}
	if bestCut > maxRunes/3 {
		return string(runes[:bestCut])
	}
	return string(runes[:maxRunes])
}

var (
	codeBlockRe  = regexp.MustCompile("(?s)```[^`]*```")
	inlineCodeRe = regexp.MustCompile("`[^`]+`")
	headerRe     = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	boldItalicRe = regexp.MustCompile(`\*\*([^*]+)\*\*|\*([^*]+)\*|__([^_]+)__|_([^_]+)_`)
	linkRe       = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	listMarkerRe = regexp.MustCompile(`(?m)^[\s]*[-*]\s+|^[\s]*\d+\.\s+`)
	urlRe        = regexp.MustCompile(`https?://\S+`)
	filePathRe   = regexp.MustCompile(`[A-Za-z]:\\[^\s,，。]+|/[a-z][^\s,，。]*`)
	multiSpaceRe = regexp.MustCompile(`[\s]+`)
	emojiRe      = regexp.MustCompile(`[\x{1F300}-\x{1F9FF}\x{2600}-\x{27BF}\x{FE00}-\x{FE0F}\x{200D}\x{20E3}\x{E0020}-\x{E007F}]+`)
)

// cleanForSpeech removes Markdown formatting, code blocks, URLs, and file paths.
func cleanForSpeech(text string) string {
	text = codeBlockRe.ReplaceAllString(text, " ")
	text = strings.ReplaceAll(text, "`", "")
	text = headerRe.ReplaceAllString(text, "")
	text = boldItalicRe.ReplaceAllStringFunc(text, func(m string) string {
		// Strip ** or * or __ or _ markers, keep inner text
		m = strings.TrimPrefix(m, "**")
		m = strings.TrimSuffix(m, "**")
		m = strings.TrimPrefix(m, "__")
		m = strings.TrimSuffix(m, "__")
		m = strings.TrimPrefix(m, "*")
		m = strings.TrimSuffix(m, "*")
		m = strings.TrimPrefix(m, "_")
		m = strings.TrimSuffix(m, "_")
		return m
	})
	text = linkRe.ReplaceAllString(text, "$1")
	text = listMarkerRe.ReplaceAllString(text, "")
	text = urlRe.ReplaceAllString(text, "")
	text = filePathRe.ReplaceAllString(text, "")
	text = emojiRe.ReplaceAllString(text, "")
	text = multiSpaceRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// CleanForSpeech is the exported version of cleanForSpeech.
// It removes Markdown formatting, code blocks, URLs, file paths, and emoji
// from text, making it suitable for TTS synthesis.
func CleanForSpeech(text string) string {
	return cleanForSpeech(text)
}

// TruncateRunesSmart is the exported version of truncateRunes.
// It truncates text to maxRunes, preferring sentence boundaries.
func TruncateRunesSmart(text string, maxRunes int) string {
	return truncateRunes(text, maxRunes)
}
