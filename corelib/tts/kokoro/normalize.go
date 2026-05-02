package kokoro

import "strings"

// NormalizeForMandarinTTS rewrites mixed technical text into forms that Kokoro's
// Mandarin frontend pronounces more reliably.
func NormalizeForMandarinTTS(text string) string {
	repl := strings.NewReplacer(
		"Kokoro-82M", "科科罗八千二百万参数",
		"Kokoro 82M", "科科罗八千二百万参数",
		"Kokoro", "科科罗",
		"TTS", "语音合成",
		"text to speech", "文字转语音",
		"Text to Speech", "文字转语音",
		"AI Coder", "智能代码助手",
		"English words", "英文词组",
		"english words", "英文词组",
		"2026", "二零二六",
	)
	return repl.Replace(text)
}
