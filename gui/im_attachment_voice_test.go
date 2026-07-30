package main

import (
	"strings"
	"testing"
	"time"
)

func TestFormatIMVoiceAttachmentDescriptionWithTranscript(t *testing.T) {
	got := formatIMVoiceAttachmentDescription("voice.ogg", "/tmp/voice.wav", "查询北京天气")
	// Transcript first so the agent treats it as the user request.
	wantPrefix := "查询北京天气\n[语音: voice.ogg → 已转写并保存到 /tmp/voice.wav]"
	if got != wantPrefix {
		t.Fatalf("got %q, want %q", got, wantPrefix)
	}
}

func TestFormatIMVoiceAttachmentDescriptionFallbackWithoutTranscript(t *testing.T) {
	got := formatIMVoiceAttachmentDescription("voice.ogg", "/tmp/voice.wav", "  ")
	if !strings.Contains(got, "[语音: voice.ogg → 已转换为WAV并保存到 /tmp/voice.wav") {
		t.Fatalf("expected fallback WAV marker, got %q", got)
	}
	if !strings.Contains(got, "ASR 未能转写") {
		t.Fatalf("expected clear ASR failure hint, got %q", got)
	}
	if strings.Contains(got, "请使用ASR工具") {
		t.Fatalf("obsolete ASR-tool hint should be gone, got %q", got)
	}
	if !strings.Contains(got, "超时") {
		t.Fatalf("expected timeout/unready hint, got %q", got)
	}
}

func TestFormatIMVoiceAttachmentDescriptionDefaultFileName(t *testing.T) {
	got := formatIMVoiceAttachmentDescription("", "/tmp/v.wav", "hello")
	if !strings.Contains(got, "[语音: voice → 已转写") {
		t.Fatalf("expected default file name voice, got %q", got)
	}
}

func TestStripHistoryAttachmentsKeepsTranscriptAfterVoiceMarker(t *testing.T) {
	msg := map[string]interface{}{
		"role":    "user",
		"content": "查询北京天气\n[语音: voice.ogg → 已转写并保存到 /tmp/voice.wav]",
	}
	result := stripHistoryAttachments(msg)
	text := result.(map[string]interface{})["content"].(string)
	if strings.Contains(text, "[语音:") {
		t.Fatalf("expected voice marker rewritten, got %q", text)
	}
	if !strings.Contains(text, "[之前的语音:") {
		t.Fatalf("expected historical voice marker, got %q", text)
	}
	if !strings.Contains(text, "查询北京天气") {
		t.Fatalf("transcript text must remain for history context, got %q", text)
	}
}

func TestCorrectASRTextRespectsDisabledFlag(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if _, err := app.PatchConfigFields(map[string]interface{}{
		"asr_voice_correction_enabled": false,
	}); err != nil {
		t.Fatalf("PatchConfigFields: %v", err)
	}
	if got := app.correctASRText("查询背景天气"); got != "查询背景天气" {
		t.Fatalf("disabled correction should pass through, got %q", got)
	}
}

func TestCorrectASRTextEmptyAndNilApp(t *testing.T) {
	if got := (*App)(nil).correctASRText("hello"); got != "hello" {
		t.Fatalf("nil app should pass through, got %q", got)
	}
	app := &App{}
	if got := app.correctASRText("  "); got != "" {
		t.Fatalf("whitespace-only should trim to empty, got %q", got)
	}
}

func TestTranscribeIMVoiceAttachmentNilAndInvalidAudio(t *testing.T) {
	if got := (*App)(nil).transcribeIMVoiceAttachment([]byte("x"), nil, time.Time{}); got != "" {
		t.Fatalf("nil app should return empty, got %q", got)
	}
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	// Invalid / too-short audio must not panic and must not return a transcript.
	if got := app.transcribeIMVoiceAttachment([]byte("not-a-wav"), nil, time.Time{}); got != "" {
		t.Fatalf("invalid audio should return empty, got %q", got)
	}
}

func TestTranscribeIMVoiceAttachmentRespectsDeadline(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	var progress []string
	past := time.Now().Add(-time.Second)
	if got := app.transcribeIMVoiceAttachment([]byte("x"), func(s string) {
		progress = append(progress, s)
	}, past); got != "" {
		t.Fatalf("past deadline should return empty, got %q", got)
	}
	// Deadline short-circuit happens before progress / ASR work.
	if len(progress) != 0 {
		t.Fatalf("expected no progress when deadline already passed, got %#v", progress)
	}

	// Remaining budget < 2s: also no progress flash.
	progress = nil
	almostGone := time.Now().Add(500 * time.Millisecond)
	if got := app.transcribeIMVoiceAttachment([]byte("x"), func(s string) {
		progress = append(progress, s)
	}, almostGone); got != "" {
		t.Fatalf("tiny remaining budget should return empty, got %q", got)
	}
	if len(progress) != 0 {
		t.Fatalf("expected no progress when remaining budget < 2s, got %#v", progress)
	}
}

func TestImVoiceASRProgressTextI18n(t *testing.T) {
	if got := imVoiceASRProgressText("en", "recognizing"); got != "Transcribing voice…" {
		t.Fatalf("en recognizing: %q", got)
	}
	if got := imVoiceASRProgressText("zh-Hans", "correcting"); !strings.Contains(got, "纠错") {
		t.Fatalf("zh-Hans correcting: %q", got)
	}
	if got := imVoiceASRProgressText("zh-Hant", "timeout"); !strings.Contains(got, "超時") {
		t.Fatalf("zh-Hant timeout: %q", got)
	}
}

func TestBuildUserContentAcceptsNilProgress(t *testing.T) {
	// Smoke: no attachments → plain text, no panic with nil app/progress.
	got := buildUserContent("hello", nil, "openai", false, nil, nil)
	if got != "hello" {
		t.Fatalf("got %#v", got)
	}
}

func TestBuildUserContentWithoutLocalStagingKeepsUnsupportedGroupImageOffDisk(t *testing.T) {
	content := buildUserContentWithLocalStaging("inspect", []MessageAttachment{{
		Type:     "image",
		FileName: "private.png",
		MimeType: "image/png",
		Data:     "aGVsbG8=",
	}}, "openai", false, nil, nil, false)
	text, ok := content.(string)
	if !ok || !strings.Contains(text, "不允许将图片保存到本机") || strings.Contains(text, "已保存到") {
		t.Fatalf("restricted image content = %#v", content)
	}
}
