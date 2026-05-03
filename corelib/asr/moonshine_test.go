package asr

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// findModel searches for the moonshine GGUF model in common locations.
func findModel(t *testing.T) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".maclaw", "models", "moonshine-base-zh.gguf"),
		filepath.Join(home, ".maclaw", "models", "moonshine-tiny.gguf"),
		filepath.Join(home, ".maclaw", "models", "moonshine-base.gguf"),
	}
	// Also check RapidSpeech.cpp build directory
	rsDir := filepath.Join("..", "..", "RapidSpeech.cpp")
	candidates = append(candidates,
		filepath.Join(rsDir, "models", "gguf", "moonshine-base-zh.gguf"),
		filepath.Join(rsDir, "models", "gguf", "moonshine-tiny.gguf"),
		filepath.Join(rsDir, "build", "moonshine-base-zh.gguf"),
		filepath.Join(rsDir, "build", "moonshine-tiny.gguf"),
		filepath.Join(rsDir, "models", "moonshine-base-zh.gguf"),
	)
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("moonshine GGUF model not found, skipping test")
	return ""
}

// findWAV searches for the maclaw test WAV file.
func findWAV(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "RapidSpeech.cpp", "test", "real_human", "maclaw_16k.wav"),
		filepath.Join("..", "..", "RapidSpeech.cpp", "test", "real_speech", "maclaw_16k.wav"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("maclaw_16k.wav not found, skipping test")
	return ""
}

func findZhouWAV(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "zhou_16k.wav"),
		filepath.Join("..", "..", "zhou.wav"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("zhou_16k.wav not found, skipping test")
	return ""
}

func findBeijingWAV(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "beiing_16k.wav"),
		filepath.Join("..", "..", "beijing_16k.wav"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("beiing_16k.wav not found, skipping test")
	return ""
}

func TestLoadModel(t *testing.T) {
	modelPath := findModel(t)
	m, err := NewMoonshine(modelPath)
	if err != nil {
		t.Fatalf("failed to load model: %v", err)
	}
	t.Logf("loaded model: enc=%dx%d dec=%dx%d vocab=%d",
		m.hp.EncoderDim, m.hp.EncoderDepth,
		m.hp.DecoderDim, m.hp.DecoderDepth,
		m.hp.VocabSize)
	t.Logf("model rope: theta=%.1f partial_rotary_factor=%.2f", m.hp.RopeTheta, m.hp.PartialRot)
	if m.hp.VocabSize == 0 {
		t.Error("vocab size is 0")
	}
	if len(m.vocab) == 0 {
		t.Error("vocab is empty")
	}
}

func TestDefaultMoonshinePartialRotaryFactor(t *testing.T) {
	if got := defaultMoonshinePartialRotaryFactor(416, 416); got != 0.62 {
		t.Fatalf("base/base-zh default partial rotary = %v, want 0.62", got)
	}
	if got := defaultMoonshinePartialRotaryFactor(288, 288); got != 0.9 {
		t.Fatalf("tiny default partial rotary = %v, want 0.9", got)
	}
}

func TestMoonshineEncoderAttentionScale(t *testing.T) {
	baseScale := moonshineEncoderAttentionScale(52)
	wantBase := float32(0.9 / math.Sqrt(52))
	if diff := math.Abs(float64(baseScale - wantBase)); diff > 1e-7 {
		t.Fatalf("base attention scale = %v, want %v", baseScale, wantBase)
	}

	tinyScale := moonshineEncoderAttentionScale(36)
	wantTiny := float32(1.0 / math.Sqrt(36))
	if diff := math.Abs(float64(tinyScale - wantTiny)); diff > 1e-7 {
		t.Fatalf("tiny attention scale = %v, want %v", tinyScale, wantTiny)
	}
}

func TestActiveVocabSizeClampsToAvailableLogitRows(t *testing.T) {
	m := &MoonshineModel{
		hp: HParams{VocabSize: 32768, DecoderDim: 4, BOSID: 1, EOSID: 2},
		w:  weights{lmHeadF32: make([]float32, 10*4)},
	}
	if got := m.activeVocabSize(); got != 10 {
		t.Fatalf("active vocab with f32 lm_head = %d, want 10", got)
	}

	m.vocab = make([]string, 8)
	if got := m.activeVocabSize(); got != 8 {
		t.Fatalf("active vocab with shorter tokenizer = %d, want 8", got)
	}

	m.hp.EOSID = 9
	if got := m.activeVocabSize(); got != 10 {
		t.Fatalf("active vocab should include EOS within logit rows = %d, want 10", got)
	}

	m.hp.EOSID = 11
	if got := m.activeVocabSize(); got != 8 {
		t.Fatalf("active vocab should not exceed logit rows for EOS = %d, want 8", got)
	}

	m = &MoonshineModel{
		hp: HParams{VocabSize: 32768, BOSID: 1, EOSID: 2},
		w:  weights{lmHeadW: &tensor.Q8Tensor{Rows: 12, Cols: 4}},
	}
	if got := m.activeVocabSize(); got != 12 {
		t.Fatalf("active vocab with q8 lm_head = %d, want 12", got)
	}
}

func TestLoadWAV(t *testing.T) {
	wavPath := findWAV(t)
	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("failed to load WAV: %v", err)
	}
	t.Logf("loaded %d samples (%.2f seconds at 16kHz)", len(pcm), float64(len(pcm))/16000.0)
	if len(pcm) == 0 {
		t.Error("empty PCM")
	}
	// Check normalization range
	var minV, maxV float32
	for _, v := range pcm {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	t.Logf("PCM range: [%.4f, %.4f]", minV, maxV)
	if maxV > 1.1 || minV < -1.1 {
		t.Errorf("PCM not normalized to [-1,1]: min=%.4f max=%.4f", minV, maxV)
	}
}

func TestTranscribe(t *testing.T) {
	modelPath := findModel(t)
	wavPath := findWAV(t)

	m, err := NewMoonshine(modelPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}

	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}

	t.Logf("transcribing %d samples...", len(pcm))
	text, err := m.Transcribe(pcm)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}

	fmt.Printf("Transcription: %q\n", text)
	t.Logf("result: %q", text)

	if text == "" {
		t.Error("empty transcription")
	}
}

func TestTranscribeZhouJayChouAgeQuestion(t *testing.T) {
	modelPath := findModel(t)
	wavPath := findZhouWAV(t)

	m, err := NewMoonshine(modelPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	defer m.Close()

	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}
	text, err := m.Transcribe(pcm)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	t.Logf("zhou transcription: %q", text)
	if !strings.Contains(text, "周杰伦") {
		t.Fatalf("expected transcription to contain 周杰伦, got %q", text)
	}
	if strings.Contains(text, "主角人") || strings.Contains(text, "结论") {
		t.Fatalf("transcription still has known bad drift, got %q", text)
	}
	if text != "我想知道周杰伦多大年纪了" {
		t.Fatalf("expected clean transcription without decoder tail, got %q", text)
	}
}

func TestTranscribeBeijingQuestion(t *testing.T) {
	modelPath := findModel(t)
	wavPath := findBeijingWAV(t)

	m, err := NewMoonshine(modelPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	defer m.Close()

	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}
	text, err := m.Transcribe(pcm)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	t.Logf("beijing transcription: %q", text)
	if text != "你好呀北京怎么样" {
		t.Fatalf("expected beijing regression transcription, got %q", text)
	}
}
func TestWAVToFloat32_Resample(t *testing.T) {
	// Create a synthetic 8kHz mono WAV and verify it gets resampled to 16kHz
	sampleRate := 8000
	duration := 0.1 // 100ms
	nSamples := int(float64(sampleRate) * duration)

	// Build WAV header + data
	dataSize := nSamples * 2
	wav := make([]byte, 44+dataSize)
	copy(wav[0:4], "RIFF")
	le32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	le32(wav[16:20], 16)
	le16(wav[20:22], 1) // PCM
	le16(wav[22:24], 1) // mono
	le32(wav[24:28], uint32(sampleRate))
	le32(wav[28:32], uint32(sampleRate*2))
	le16(wav[32:34], 2)  // block align
	le16(wav[34:36], 16) // bits per sample
	copy(wav[36:40], "data")
	le32(wav[40:44], uint32(dataSize))
	// Fill with a simple sine wave
	for i := 0; i < nSamples; i++ {
		val := int16(16000 * float64(i) / float64(nSamples))
		le16(wav[44+i*2:46+i*2], uint16(val))
	}

	pcm, err := WAVToFloat32(wav)
	if err != nil {
		t.Fatalf("WAVToFloat32: %v", err)
	}

	// Should be resampled to 16kHz: ~2x samples
	expectedSamples := nSamples * 16000 / sampleRate
	if abs(len(pcm)-expectedSamples) > 2 {
		t.Errorf("expected ~%d samples, got %d", expectedSamples, len(pcm))
	}
	t.Logf("resampled %d -> %d samples", nSamples, len(pcm))
}

func TestWAVToFloat32RejectsUnsupportedBitDepth(t *testing.T) {
	sampleRate := 16000
	nSamples := 16
	dataSize := nSamples
	wav := make([]byte, 44+dataSize)
	copy(wav[0:4], "RIFF")
	le32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	le32(wav[16:20], 16)
	le16(wav[20:22], 1)
	le16(wav[22:24], 1)
	le32(wav[24:28], uint32(sampleRate))
	le32(wav[28:32], uint32(sampleRate))
	le16(wav[32:34], 1)
	le16(wav[34:36], 8)
	copy(wav[36:40], "data")
	le32(wav[40:44], uint32(dataSize))

	_, err := WAVToFloat32(wav)
	if err == nil {
		t.Fatal("expected unsupported bit depth error")
	}
}

func TestWAVToFloat32RejectsNonPCMFormat(t *testing.T) {
	sampleRate := 16000
	nSamples := 16
	dataSize := nSamples * 2
	wav := make([]byte, 44+dataSize)
	copy(wav[0:4], "RIFF")
	le32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	le32(wav[16:20], 16)
	le16(wav[20:22], 3) // IEEE float, not PCM
	le16(wav[22:24], 1)
	le32(wav[24:28], uint32(sampleRate))
	le32(wav[28:32], uint32(sampleRate*2))
	le16(wav[32:34], 2)
	le16(wav[34:36], 16)
	copy(wav[36:40], "data")
	le32(wav[40:44], uint32(dataSize))

	_, err := WAVToFloat32(wav)
	if err == nil {
		t.Fatal("expected unsupported WAV format error")
	}
}

func TestWAVToFloat32RejectsMisalignedData(t *testing.T) {
	sampleRate := 16000
	dataSize := 3 // mono 16-bit must be divisible by 2
	wav := make([]byte, 44+dataSize)
	copy(wav[0:4], "RIFF")
	le32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	le32(wav[16:20], 16)
	le16(wav[20:22], 1)
	le16(wav[22:24], 1)
	le32(wav[24:28], uint32(sampleRate))
	le32(wav[28:32], uint32(sampleRate*2))
	le16(wav[32:34], 2)
	le16(wav[34:36], 16)
	copy(wav[36:40], "data")
	le32(wav[40:44], uint32(dataSize))

	_, err := WAVToFloat32(wav)
	if err == nil {
		t.Fatal("expected malformed WAV data size error")
	}
}

func le32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func le16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func TestDecoderSwiGLUAliasPath(t *testing.T) {
	fc1Out := []float32{2, -4, 1, 0}
	intermediate := len(fc1Out) / 2
	valuePart := fc1Out[:intermediate]
	gatePart := fc1Out[intermediate:]

	tensor.SiLU(gatePart)
	tensor.ElemMul(valuePart, gatePart, valuePart)

	want := []float32{1.4621172, -0}
	for i, got := range valuePart {
		if diff := got - want[i]; diff < -0.03 || diff > 0.03 {
			t.Fatalf("valuePart[%d] = %v, want ~%v (fastExp tolerance)", i, got, want[i])
		}
	}
}

func TestManagerLazyLoadAndUnload(t *testing.T) {
	modelPath := findModel(t)
	wavPath := findWAV(t)

	mgr := NewManager(modelPath)
	mgr.SetUnloadDelay(3 * time.Second)

	if mgr.Loaded() {
		t.Error("model should not be loaded yet")
	}

	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}

	text, err := mgr.Transcribe(pcm)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	t.Logf("result: %q", text)
	if text == "" {
		t.Error("empty transcription")
	}
	if !mgr.Loaded() {
		t.Error("model should be loaded after transcribe")
	}

	// Wait for auto-unload
	time.Sleep(4 * time.Second)
	if mgr.Loaded() {
		t.Error("model should have been unloaded after idle timeout")
	}
}
