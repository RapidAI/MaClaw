package tts

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestEncodeWAVToOpus_SyntheticWAV tests the full WAV→Opus→OGG pipeline
// with a synthetic WAV file (440Hz sine wave).
func TestEncodeWAVToOpus_SyntheticWAV(t *testing.T) {
	if !HasOpusEncoder() {
		t.Skip("built-in Opus encoder not available, skipping Opus encoding test")
	}

	// Generate a 1-second 48kHz mono 16-bit WAV with a 440Hz sine tone.
	wav := generateTestWAV(48000, 1, 1.0, 440.0)
	t.Logf("synthetic WAV: %d bytes", len(wav))

	ogg, err := EncodeWAVToOpus(wav)
	if err != nil {
		t.Fatalf("EncodeWAVToOpus failed: %v", err)
	}

	t.Logf("OGG Opus output: %d bytes (compression ratio: %.1fx)", len(ogg), float64(len(wav))/float64(len(ogg)))

	// Verify OGG container structure.
	if len(ogg) < 47 {
		t.Fatalf("OGG output too short: %d bytes", len(ogg))
	}
	if string(ogg[0:4]) != "OggS" {
		t.Fatalf("missing OGG capture pattern, got: %x", ogg[0:4])
	}
	if !containsBytes(ogg, []byte("OpusHead")) {
		t.Fatal("OGG output missing OpusHead header")
	}
	if !containsBytes(ogg, []byte("OpusTags")) {
		t.Fatal("OGG output missing OpusTags header")
	}

	// Count OGG pages.
	pages := countOggPages(ogg)
	t.Logf("OGG pages: %d", pages)
	if pages < 3 {
		t.Fatalf("expected at least 3 OGG pages (head + tags + audio), got %d", pages)
	}

	// Write to temp file for manual inspection.
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test_output.ogg")
	os.WriteFile(outPath, ogg, 0o644)
	t.Logf("wrote OGG file to: %s", outPath)
}

// TestEncodeWAVToOpus_22050Hz tests resampling from 22050Hz (TTS native rate).
func TestEncodeWAVToOpus_22050Hz(t *testing.T) {
	if !HasOpusEncoder() {
		t.Skip("built-in Opus encoder not available, skipping Opus encoding test")
	}

	wav := generateTestWAV(22050, 1, 0.5, 440.0)
	t.Logf("22050Hz WAV: %d bytes", len(wav))

	ogg, err := EncodeWAVToOpus(wav)
	if err != nil {
		t.Fatalf("EncodeWAVToOpus failed: %v", err)
	}

	t.Logf("OGG Opus output: %d bytes", len(ogg))

	if string(ogg[0:4]) != "OggS" {
		t.Fatalf("missing OGG capture pattern")
	}
	if !containsBytes(ogg, []byte("OpusHead")) {
		t.Fatal("missing OpusHead")
	}
}

// TestEncodeWAVToOpus_WithRealTTS tests the full pipeline with the actual
// TTS engine if the model is available. Skipped if model not found.
func TestEncodeWAVToOpus_WithRealTTS(t *testing.T) {
	if !HasOpusEncoder() {
		t.Skip("built-in Opus encoder not available, skipping Opus encoding test")
	}

	modelPath := filepath.Join("testdata", "piper-xiao_ya-zh-fp32.gguf")
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("TTS model not found at %s, skipping real TTS test", modelPath)
	}

	lexPath := filepath.Join("testdata", "vits-piper-zh_CN-xiao_ya-medium", "lexicon.txt")
	model, err := NewPiper(modelPath, lexPath)
	if err != nil {
		t.Fatalf("NewPiper failed: %v", err)
	}

	wav, err := model.SynthesizeToWAV("你好世界")
	if err != nil {
		t.Fatalf("SynthesizeToWAV failed: %v", err)
	}
	t.Logf("TTS WAV: %d bytes", len(wav))

	ogg, err := EncodeWAVToOpus(wav)
	if err != nil {
		t.Fatalf("EncodeWAVToOpus failed: %v", err)
	}

	t.Logf("OGG Opus: %d bytes (%.1fx compression)", len(ogg), float64(len(wav))/float64(len(ogg)))

	if string(ogg[0:4]) != "OggS" {
		t.Fatal("missing OGG capture pattern")
	}
	if len(ogg) > len(wav) {
		t.Errorf("OGG larger than WAV: %d > %d (expected compression)", len(ogg), len(wav))
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "tts_nihao.ogg")
	os.WriteFile(outPath, ogg, 0o644)
	t.Logf("wrote: %s", outPath)
}

func TestHasOpusEncoder(t *testing.T) {
	// Just log the result; the encoder should be built in and independent of ffmpeg.
	t.Logf("HasOpusEncoder() = %v", HasOpusEncoder())
}

// --- helpers ---

func generateTestWAV(sampleRate, channels int, durationSec, freqHz float64) []byte {
	numSamples := int(float64(sampleRate) * durationSec)
	dataSize := numSamples * channels * 2 // 16-bit

	hdr := make([]byte, 44)
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+dataSize))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(sampleRate*channels*2))
	binary.LittleEndian.PutUint16(hdr[32:34], uint16(channels*2))
	binary.LittleEndian.PutUint16(hdr[34:36], 16)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(dataSize))

	data := make([]byte, dataSize)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		sample := int16(32000.0 * math.Sin(2*math.Pi*freqHz*t))
		for ch := 0; ch < channels; ch++ {
			off := (i*channels + ch) * 2
			binary.LittleEndian.PutUint16(data[off:off+2], uint16(sample))
		}
	}

	return append(hdr, data...)
}

func containsBytes(data, pattern []byte) bool {
	for i := 0; i <= len(data)-len(pattern); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if data[i+j] != pattern[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func countOggPages(data []byte) int {
	count := 0
	for i := 0; i <= len(data)-4; i++ {
		if string(data[i:i+4]) == "OggS" {
			count++
		}
	}
	return count
}
