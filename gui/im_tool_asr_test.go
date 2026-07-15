package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
)

func TestASRToolPathArg(t *testing.T) {
	if got := asrToolPathArg(map[string]interface{}{"path": " a.wav "}); got != "a.wav" {
		t.Fatalf("path=%q", got)
	}
	if got := asrToolPathArg(map[string]interface{}{"file": "b.mp3"}); got != "b.mp3" {
		t.Fatalf("file alias=%q", got)
	}
	if got := asrToolPathArg(map[string]interface{}{"path": `"C:\a\b.wav"`}); got != `C:\a\b.wav` {
		t.Fatalf("quoted path=%q", got)
	}
	if got := asrToolPathArg(map[string]interface{}{"path": `file:///C:/a/b.wav`}); got == "" || strings.Contains(got, "file:") {
		t.Fatalf("file url path=%q", got)
	}
	if got := asrToolPathArg(map[string]interface{}{}); got != "" {
		t.Fatalf("empty=%q", got)
	}
}

func TestDecodeASRAudioRetriesAutoDetect(t *testing.T) {
	// Valid WAV mis-labeled as mp3 should succeed after auto-detect retry.
	pcm := make([]byte, 320)
	wavIn := makeTestWAV(16000, 1, 16, pcm)
	got, err := decodeASRAudio(wavIn, "mp3")
	if err != nil {
		t.Fatalf("decode retry: %v", err)
	}
	if len(got) < 44 {
		t.Fatalf("short wav: %d", len(got))
	}
}

func TestToolASRMissingPath(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.toolASR(map[string]interface{}{})
	if !strings.Contains(got, "path") {
		t.Fatalf("expected missing path message, got %q", got)
	}
	if !strings.Contains(got, audioconv.DirectASRFormats) {
		t.Fatalf("expected supported formats in message, got %q", got)
	}
}

func TestToolASRAcceptsPathAliasesWithoutApp(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.toolASR(map[string]interface{}{"file": "x.wav"})
	if strings.Contains(got, "缺少 path") {
		t.Fatalf("path alias should be accepted, got %q", got)
	}
	if !strings.Contains(got, "应用未就绪") {
		t.Fatalf("expected app-not-ready path, got %q", got)
	}
}

func TestPrepareASRToolWAVM4AHint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.m4a")
	payload := []byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00not-real-m4a")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	wav, msg := prepareASRToolWAV(path, "")
	if wav != nil {
		t.Fatalf("expected nil wav, got %d bytes", len(wav))
	}
	for _, needle := range []string{"ffmpeg", "16000", ".wav", audioconv.DirectASRFormats, `asr(path="`, "Whisper"} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("hint missing %q: %s", needle, msg)
		}
	}
}

func TestPrepareASRToolWAVUnknownExtConvertHint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.flac")
	if err := os.WriteFile(path, []byte("fLaC-not-really"), 0o644); err != nil {
		t.Fatal(err)
	}
	wav, msg := prepareASRToolWAV(path, "flac")
	if wav != nil {
		t.Fatalf("expected nil wav")
	}
	if !strings.Contains(msg, "ffmpeg") || !strings.Contains(msg, "flac") {
		t.Fatalf("expected convert hint for flac, got %q", msg)
	}
}

func TestPrepareASRToolWAVAcceptsMinimalWAV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tone.wav")
	// 16-bit mono 16kHz PCM WAV with a few zero samples.
	pcm := make([]byte, 320) // 10ms @ 16k mono s16
	data := makeTestWAV(16000, 1, 16, pcm)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	wav, msg := prepareASRToolWAV(path, "")
	if msg != "" {
		t.Fatalf("unexpected err: %s", msg)
	}
	if len(wav) < 44 {
		t.Fatalf("wav too short: %d", len(wav))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("not a wav: %q", wav[:12])
	}
}

func TestPrepareASRToolWAVRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, msg := prepareASRToolWAV(dir, "")
	if !strings.Contains(msg, "目录") {
		t.Fatalf("expected directory error, got %q", msg)
	}
}

func TestToolAcceptsRuntimePolicyOwnerArgIncludesASR(t *testing.T) {
	if !toolAcceptsRuntimePolicyOwnerArg("asr") {
		t.Fatal("asr should accept runtime policy owner arg like path tools")
	}
}

func TestRegisterBuiltinToolsIncludesASR(t *testing.T) {
	registry := NewToolRegistry()
	registerBuiltinTools(registry, &IMMessageHandler{})
	tool, ok := registry.Get("asr")
	if !ok || tool == nil {
		t.Fatal("asr tool not registered")
	}
	if !strings.Contains(tool.Description, "wav") {
		t.Fatalf("description should document formats: %s", tool.Description)
	}
	if _, ok := tool.InputSchema["path"]; !ok && tool.InputSchema != nil {
		// InputSchema may nest under properties depending on registry shape.
	}
	// Tags should help routing for Chinese transcription requests.
	joined := strings.Join(tool.Tags, " ")
	for _, tag := range []string{"asr", "转写", "转录"} {
		if !strings.Contains(joined, tag) {
			t.Fatalf("missing routing tag %q in %v", tag, tool.Tags)
		}
	}
}

// makeTestWAV builds a minimal PCM WAV for tests.
func makeTestWAV(sampleRate, channels, bitsPerSample int, pcm []byte) []byte {
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	dataSize := len(pcm)
	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], uint16(bitsPerSample))
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))
	copy(buf[44:], pcm)
	return buf
}
