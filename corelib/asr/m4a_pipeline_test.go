package asr

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestM4APipelineFromWAV tests ASR with gentle normalization on quiet audio.
func TestM4APipelineFromWAV(t *testing.T) {
	modelPath := findModel(t)

	wavPath := filepath.Join("..", "..", "明明白白我的心.wav")
	if _, err := os.Stat(wavPath); err != nil {
		t.Skipf("WAV file not found: %v", err)
	}

	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("LoadWAV: %v", err)
	}
	durationSec := float64(len(pcm)) / 16000.0
	rmsVal := calcTestRMS(pcm)
	t.Logf("PCM: %d samples (%.2fs) RMS=%.5f", len(pcm), durationSec, rmsVal)

	// Apply gentle normalization for quiet audio (same logic as backend)
	if rmsVal < 0.015 && rmsVal > 0 {
		targetRMS := 0.025
		gain := targetRMS / rmsVal
		peakVal := calcTestPeak(pcm)
		if peakVal > 0 {
			peakGain := 0.95 / peakVal
			if peakGain < gain {
				gain = peakGain
			}
		}
		if gain > 3.0 {
			gain = 3.0
		}
		if gain > 1.1 {
			t.Logf("Applying gentle normalize: gain=%.2fx (rms %.5f -> ~%.5f)", gain, rmsVal, rmsVal*gain)
			boosted := make([]float32, len(pcm))
			g := float32(gain)
			for i, s := range pcm {
				v := s * g
				if v > 1 {
					v = 1
				} else if v < -1 {
					v = -1
				}
				boosted[i] = v
			}
			pcm = boosted
		}
	}

	m, err := NewMoonshine(modelPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	defer m.Close()

	start := time.Now()
	text, err := m.Transcribe(pcm)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}

	fmt.Printf("\n=== M4A->WAV ASR Result (with gentle normalize) ===\n")
	fmt.Printf("Expected: 明明白白我的心 渴望一份真感情\n")
	fmt.Printf("Got:      %s\n", text)
	fmt.Printf("Duration: %.2fs | Inference: %dms\n", durationSec, elapsed.Milliseconds())
	fmt.Printf("====================================================\n\n")

	t.Logf("result (%dms): %q", elapsed.Milliseconds(), text)
}

func calcTestRMS2(pcm []float32) float64 {
	var sum float64
	for _, s := range pcm {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(pcm)))
}
