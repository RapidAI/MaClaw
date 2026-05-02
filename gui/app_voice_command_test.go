package main

import "testing"

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
	if got.CorrectedText != "song lyric fragment" {
		t.Fatalf("expected fallback text for empty correction, got %q", got.CorrectedText)
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
