package asr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// findSenseVoiceModel searches for the SenseVoice GGUF model.
// Recommended: cstr/sensevoice-small-GGUF (has proper entry block for 560→512 projection).
// Also works with: FunAudioLLM/SenseVoiceSmall-GGUF (requires FunASR runtime projection - lossy in Go).
func findSenseVoiceModel(t *testing.T) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	candidates := []string{
		// Current working directory (workspace root)
		filepath.Join("..", "..", "sensevoice-small-q8_0.gguf"), // cstr format (recommended)
		filepath.Join("..", "..", "sensevoice-small-q8.gguf"),   // FunAudioLLM format
		filepath.Join(home, ".maclaw", "models", "sensevoice-small-q8_0.gguf"),
		filepath.Join(home, ".maclaw", "models", "sensevoice-small-q8.gguf"),
		filepath.Join(home, ".maclaw", "models", "sensevoice-small-f16.gguf"),
		filepath.Join(home, ".maclaw", "models", "sensevoice-small.gguf"),
	}
	rsDir := filepath.Join("..", "..", "RapidSpeech.cpp")
	candidates = append(candidates,
		filepath.Join(rsDir, "models", "gguf", "sensevoice-small-q8.gguf"),
		filepath.Join(rsDir, "models", "gguf", "sensevoice-small-q8_0.gguf"),
		filepath.Join(rsDir, "models", "sensevoice-small-q8.gguf"),
		filepath.Join(rsDir, "build", "sensevoice-small-q8.gguf"),
	)
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("SenseVoice GGUF model not found, skipping test")
	return ""
}

func TestSenseVoiceLoadModel(t *testing.T) {
	modelPath := findSenseVoiceModel(t)
	m, err := NewSenseVoice(modelPath)
	if err != nil {
		t.Fatalf("failed to load SenseVoice: %v", err)
	}
	defer m.Close()

	t.Logf("loaded SenseVoice: hidden=%d blocks=%d tp_blocks=%d vocab=%d",
		m.hp.HiddenSize, m.hp.NumBlocks, m.hp.NumTPBlocks, m.hp.VocabSize)
	if m.hp.VocabSize == 0 {
		t.Error("vocab size is 0")
	}
	if len(m.vocab) == 0 {
		t.Error("vocab is empty")
	}
	if m.w.ctcW.Rows() == 0 {
		t.Error("CTC head weight not loaded")
	}
}

func TestSenseVoiceTranscribe(t *testing.T) {
	modelPath := findSenseVoiceModel(t)
	wavPath := findWAV(t)

	m, err := NewSenseVoice(modelPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	defer m.Close()

	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}

	t.Logf("transcribing %d samples (%.2fs)...", len(pcm), float64(len(pcm))/16000.0)
	t.Logf("model: %d encoder blocks loaded, %d tp blocks loaded", len(m.w.encoders), len(m.w.tpEncoders))
	t.Logf("entry block: qW.Rows=%d fusedQKV=%v norm1=%d", m.w.encoder0.qW.Rows(), m.w.encoder0.fusedQKV, len(m.w.encoder0.norm1W))
	t.Logf("CMVN: means=%v, istd_first5=%v", m.cmvnMeans != nil, func() []float32 {
		if m.cmvnIstd != nil && len(m.cmvnIstd) >= 5 {
			return m.cmvnIstd[:5]
		}
		return nil
	}())

	start := time.Now()
	text, err := m.Transcribe(pcm)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}

	fmt.Printf("SenseVoice Transcription: %q (%.1fms)\n", text, float64(elapsed.Microseconds())/1000.0)
	t.Logf("result: %q (%.1fms)", text, float64(elapsed.Microseconds())/1000.0)
	if text == "" {
		t.Error("empty transcription")
	}
}

func TestSenseVoiceZhou(t *testing.T) {
	modelPath := findSenseVoiceModel(t)
	wavPath := findZhouWAV(t)

	m, err := NewSenseVoice(modelPath)
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
	t.Logf("SenseVoice zhou: %q", text)
	if !strings.Contains(text, "周杰伦") {
		t.Errorf("expected 周杰伦 in transcription, got %q", text)
	}
}

func TestSenseVoiceBeijing(t *testing.T) {
	modelPath := findSenseVoiceModel(t)
	wavPath := findBeijingWAV(t)

	m, err := NewSenseVoice(modelPath)
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
	t.Logf("SenseVoice beijing: %q", text)
	if !strings.Contains(text, "北京") {
		t.Errorf("expected 北京 in transcription, got %q", text)
	}
}

func TestSenseVoiceManagerAutoDetect(t *testing.T) {
	modelPath := findSenseVoiceModel(t)

	arch := detectASRArch(modelPath)
	if arch != "sensevoice" {
		t.Fatalf("detectASRArch(%q) = %q, want sensevoice", modelPath, arch)
	}

	mgr := NewManager(modelPath)
	defer mgr.Unload()

	if mgr.Loaded() {
		t.Error("model should not be loaded before first use")
	}

	wavPath := findWAV(t)
	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("load wav: %v", err)
	}

	text, err := mgr.Transcribe(pcm)
	if err != nil {
		t.Fatalf("mgr.Transcribe: %v", err)
	}
	t.Logf("Manager result: %q", text)
	if text == "" {
		t.Error("empty transcription via Manager")
	}
	if !mgr.Loaded() {
		t.Error("model should be loaded after transcribe")
	}
}

// TestFbank verifies mel-filterbank produces expected dimensions.
func TestFbank(t *testing.T) {
	// 1 second of 16kHz silence
	pcm := make([]float32, 16000)
	fbank := svMelFilterbank(pcm)
	if fbank == nil {
		t.Fatal("fbank returned nil for 1s audio")
	}
	numFrames := len(fbank) / svNumMels
	// Expected: (16000 - 400) / 160 + 1 = 98 frames
	expectedFrames := (16000-svWindowSize)/svHopSize + 1
	if numFrames != expectedFrames {
		t.Errorf("fbank frames = %d, want %d", numFrames, expectedFrames)
	}
	t.Logf("fbank: %d frames × %d mels", numFrames, svNumMels)
}

// TestLFR verifies LFR stacking dimensions.
func TestLFR(t *testing.T) {
	numFrames := 98
	fbank := make([]float32, numFrames*svNumMels)
	lfr, lfrFrames := svApplyLFR(fbank, numFrames)
	// Expected: (98 - 7) / 6 + 1 = 16 frames
	expectedLFR := (numFrames-svLFRm)/svLFRn + 1
	if lfrFrames != expectedLFR {
		t.Errorf("LFR frames = %d, want %d", lfrFrames, expectedLFR)
	}
	if len(lfr) != lfrFrames*svFeatsDim {
		t.Errorf("LFR output size = %d, want %d", len(lfr), lfrFrames*svFeatsDim)
	}
	t.Logf("LFR: %d → %d frames, dim=%d", numFrames, lfrFrames, svFeatsDim)
}
