package main

import (
	"strings"
	"testing"
)

func TestNormalizeVoiceCommandLLMResultParsesCommand(t *testing.T) {
	got := normalizeVoiceCommandLLMResult(`{"is_command":true,"corrected_text":"check APPN server status","confidence":0.9,"reason":"user_request"}`, "check APPN server state")
	if !got.IsCommand {
		t.Fatalf("expected command result, got %#v", got)
	}
	if got.CorrectedText != "check APPN server status" {
		t.Fatalf("expected corrected text, got %q", got.CorrectedText)
	}
	if got.Confidence != 0.9 {
		t.Fatalf("expected confidence 0.9, got %v", got.Confidence)
	}
}

func TestNormalizeVoiceCommandLLMResultParsesNonCommand(t *testing.T) {
	got := normalizeVoiceCommandLLMResult(`{"is_command":false,"corrected_text":"","confidence":0.8,"reason":"background_audio"}`, "song lyric fragment")
	if got.IsCommand {
		t.Fatalf("expected non-command result, got %#v", got)
	}
	// Non-command must keep empty corrected_text so continuous mode can drop it.
	// Do not re-fill with the raw ASR fallback.
	if got.CorrectedText != "" {
		t.Fatalf("expected empty corrected_text for non-command, got %q", got.CorrectedText)
	}
}

func TestNormalizeVoiceCommandLLMResultCommandEmptyCorrectedFallsBack(t *testing.T) {
	got := normalizeVoiceCommandLLMResult(`{"is_command":true,"corrected_text":"","confidence":0.5,"reason":"keep_raw"}`, "查询天气")
	if !got.IsCommand {
		t.Fatalf("expected command, got %#v", got)
	}
	if got.CorrectedText != "查询天气" {
		t.Fatalf("command with empty corrected_text should fall back to raw ASR, got %q", got.CorrectedText)
	}
}

func TestNormalizeVoiceCommandLLMResultCommandPunctuationCorrectedFallsBack(t *testing.T) {
	got := normalizeVoiceCommandLLMResult(`{"is_command":true,"corrected_text":"。","confidence":0.4,"reason":"noise"}`, "查询天气")
	if !got.IsCommand {
		t.Fatalf("expected command, got %#v", got)
	}
	if got.CorrectedText != "查询天气" {
		t.Fatalf("punctuation-only correction should fall back to raw ASR, got %q", got.CorrectedText)
	}
}

func TestNormalizeVoiceCommandLLMResultMalformedFallsBackToCommand(t *testing.T) {
	got := normalizeVoiceCommandLLMResult(`not json`, "check weather")
	if !got.IsCommand {
		t.Fatalf("malformed LLM output must not swallow voice command, got %#v", got)
	}
	if got.CorrectedText != "check weather" {
		t.Fatalf("expected original text fallback, got %q", got.CorrectedText)
	}
	if got.Reason != "llm_parse_fallback" {
		t.Fatalf("expected parse fallback reason, got %q", got.Reason)
	}
}

func TestNormalizeVoiceCommandLLMResultStripsMarkdownFence(t *testing.T) {
	raw := "```json\n{\"is_command\":true,\"corrected_text\":\"查询北京天气\",\"confidence\":0.9}\n```"
	got := normalizeVoiceCommandLLMResult(raw, "查询背景天气")
	if got.CorrectedText != "查询北京天气" {
		t.Fatalf("expected fenced JSON parsed, got %#v", got)
	}
}

func TestParseASRCorrectionLLMResult(t *testing.T) {
	if got := parseASRCorrectionLLMResult(`{"corrected_text":"查询北京天气","confidence":0.9}`); got != "查询北京天气" {
		t.Fatalf("json correction: got %q", got)
	}
	// Missing is_command must not matter for correction-only parser.
	if got := parseASRCorrectionLLMResult(`{"corrected_text":"hello"}`); got != "hello" {
		t.Fatalf("correction without is_command: got %q", got)
	}
	if got := parseASRCorrectionLLMResult("```json\n{\"corrected_text\":\"ok\"}\n```"); got != "ok" {
		t.Fatalf("fenced correction: got %q", got)
	}
	if got := parseASRCorrectionLLMResult(`{"corrected_text":""}`); got != "" {
		t.Fatalf("explicit empty correction should stay empty, got %q", got)
	}
	// Plain text / apologies must not replace the transcript.
	if got := parseASRCorrectionLLMResult("just plain rewrite"); got != "" {
		t.Fatalf("plain text must be ignored, got %q", got)
	}
	if got := parseASRCorrectionLLMResult("I cannot correct this."); got != "" {
		t.Fatalf("apology plain text must be ignored, got %q", got)
	}
	if got := parseASRCorrectionLLMResult(`not json at all`); got != "" {
		t.Fatalf("garbage must be ignored, got %q", got)
	}
}

func TestCorrectASRTextSkipsVeryLongTranscript(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	// Ensure correction is enabled (default true) but transcript is too long.
	long := strings.Repeat("测", maxASRCorrectionRunes+1)
	if got := app.correctASRText(long); got != long {
		t.Fatalf("long transcript should pass through without LLM, len(got)=%d", len([]rune(got)))
	}
}

func TestHasASRSpeechContent(t *testing.T) {
	if hasASRSpeechContent("。") || hasASRSpeechContent("...") || hasASRSpeechContent("  ") {
		t.Fatal("punctuation/whitespace should not count as speech content")
	}
	if !hasASRSpeechContent("查询天气。") || !hasASRSpeechContent("OK") || !hasASRSpeechContent("123") {
		t.Fatal("letters/digits should count as speech content")
	}
}

func TestShouldSkipASRLLMCorrection(t *testing.T) {
	for _, text := range []string{"北京天气。", "设置今天十一点的闹钟", "马上帮我找一个图片发我。", "play music"} {
		if !shouldSkipASRLLMCorrection(text) {
			t.Fatalf("concise transcript should stay on local fast path: %q", text)
		}
	}
	for _, text := range []string{"I.", "啊", strings.Repeat("较长语音内容", 12)} {
		if shouldSkipASRLLMCorrection(text) {
			t.Fatalf("ambiguous/long transcript should remain eligible for correction: %q", text)
		}
	}
}
