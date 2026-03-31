package asr

import (
	"fmt"
	"os"
	"time"
	"path/filepath"
	"testing"
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
	if m.hp.VocabSize == 0 {
		t.Error("vocab size is 0")
	}
	if len(m.vocab) == 0 {
		t.Error("vocab is empty")
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
