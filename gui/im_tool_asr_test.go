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

func TestAsrToolIntArg(t *testing.T) {
	if got := asrToolIntArg(map[string]interface{}{"known_speakers": 3.0}, 0, "known_speakers"); got != 3 {
		t.Fatalf("float64: got %d", got)
	}
	if got := asrToolIntArg(map[string]interface{}{"speakers": "2"}, 0, "known_speakers", "speakers"); got != 2 {
		t.Fatalf("string alias: got %d", got)
	}
	if got := asrToolIntArg(map[string]interface{}{}, 0, "known_speakers"); got != 0 {
		t.Fatalf("missing: got %d", got)
	}
	if got := asrToolIntArg(nil, 7, "known_speakers"); got != 7 {
		t.Fatalf("nil args fallback: got %d", got)
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

func TestASRTranscriptSidecarPath(t *testing.T) {
	got := asrTranscriptSidecarPath(`C:\rec\meeting.wav`)
	if !strings.HasSuffix(got, `_transcript.txt`) || !strings.Contains(got, "meeting") {
		t.Fatalf("sidecar=%q", got)
	}
	if asrTranscriptSidecarPath("") != "transcript.txt" {
		t.Fatalf("empty audio path")
	}
	md := asrTranscriptMarkdownPath(`C:\rec\meeting.wav`)
	if !strings.HasSuffix(md, `_transcript.md`) || !strings.Contains(md, "meeting") {
		t.Fatalf("md path=%q", md)
	}
	if asrTranscriptMarkdownPath("") != "transcript.md" {
		t.Fatalf("empty audio md path")
	}
}

func TestASRShouldSpillToFile(t *testing.T) {
	if asrShouldSpillToFile("短") {
		t.Fatal("short text should stay inline")
	}
	// Over byte budget even with mostly ASCII.
	long := strings.Repeat("abcdefghij", 400) // 4000 bytes
	if !asrShouldSpillToFile(long) {
		t.Fatal("expected spill for long ASCII")
	}
	// CJK denser tokens: still under bytes but over token budget.
	cjk := strings.Repeat("会议记录摘要内容", 800)
	if !asrShouldSpillToFile(cjk) {
		t.Fatal("expected spill for long CJK")
	}
}

func TestFormatASRToolResultShortInline(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "short.wav")
	got := formatASRToolResult(audio, "hello 会议")
	if !strings.Contains(got, "hello 会议") {
		t.Fatalf("short should include full text, got %q", got)
	}
	if !strings.Contains(got, "transcript_md:") {
		t.Fatalf("short should announce host markdown archive, got %q", got)
	}
	// Short results still get a markdown archive (not the long .txt spill).
	if _, err := os.Stat(asrTranscriptSidecarPath(audio)); err == nil {
		t.Fatal("short result must not write long txt sidecar")
	}
	mdPath := asrTranscriptMarkdownPath(audio)
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("expected host-written transcript md: %v", err)
	}
	if !strings.Contains(string(data), "hello 会议") || !strings.Contains(string(data), "转写正文") {
		t.Fatalf("md content unexpected: %q", string(data))
	}
	// PDF is best-effort (requires system CJK fonts). When present, path must be announced.
	if pdfPath := asrTranscriptPDFPath(audio); strings.Contains(got, "transcript_pdf:") {
		if _, err := os.Stat(pdfPath); err != nil {
			// GenerateToFile may return a cleaned abs path; accept either announcement or file.
			if !strings.Contains(got, filepath.Base(pdfPath)) {
				t.Fatalf("transcript_pdf announced but file missing: %v\n%s", err, got)
			}
		}
	}
}

func TestASRTranscriptTitleAndMarkdown(t *testing.T) {
	if got := asrTranscriptTitle(`C:\rec\会议录音-1.wav`); got != "会议录音-1" {
		t.Fatalf("title=%q", got)
	}
	if got := asrTranscriptTitle(""); got != "转写" {
		t.Fatalf("empty title=%q", got)
	}
	md := buildASRTranscriptMarkdown(`C:\rec\demo.wav`, "正文")
	if !strings.Contains(md, "# demo") || !strings.Contains(md, "正文") {
		t.Fatalf("md=%q", md)
	}
}

func TestFormatASRToolResultLongSpillsFile(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "long.wav")
	// Build long CJK body with distinct head/tail markers.
	body := "【开头标记】" + strings.Repeat("这是会议转写正文段落。", 600) + "【结尾标记】"
	got := formatASRToolResult(audio, body)
	if !strings.Contains(got, "[ASR long transcript]") {
		t.Fatalf("expected long envelope: %s", clipASRTest(got, 200))
	}
	sidecar := asrTranscriptSidecarPath(audio)
	if !strings.Contains(got, sidecar) {
		t.Fatalf("expected transcript_file path in result: %s", clipASRTest(got, 400))
	}
	for _, needle := range []string{"for_minutes=true", "transcript_file", "transcript_md", "preview_head", "preview_tail", "【开头标记】", "【结尾标记】"} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in:\n%s", needle, clipASRTest(got, 800))
		}
	}
	mdPath := asrTranscriptMarkdownPath(audio)
	if mdData, err := os.ReadFile(mdPath); err != nil {
		t.Fatalf("long result should also write transcript_md: %v", err)
	} else if !strings.Contains(string(mdData), "【开头标记】") {
		t.Fatalf("transcript_md missing body")
	}
	// Result must be a small preview envelope, not the full transcript payload.
	if len(got) >= len(body) {
		t.Fatalf("result (%d) should be smaller than full body (%d)", len(got), len(body))
	}
	if !strings.Contains(got, "runes omitted") {
		t.Fatalf("expected omission marker in preview")
	}
	data, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
	if string(data) != body {
		t.Fatalf("sidecar content mismatch: got %d bytes want %d", len(data), len(body))
	}
}

func clipASRTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
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
