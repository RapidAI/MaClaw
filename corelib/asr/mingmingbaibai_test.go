package asr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMingMingBaiBaiSenseVoice runs SenseVoice on project-root 明明白白*.wav samples.
func TestMingMingBaiBaiSenseVoice(t *testing.T) {
	modelPath := findSenseVoiceModel(t)

	root := filepath.Join("..", "..")
	wavs := []string{
		filepath.Join(root, "明明白白我的心.wav"),
		filepath.Join(root, "明明白2.wav"),
		filepath.Join(root, "test_mmbb.wav"),
	}

	m, err := NewSenseVoice(modelPath)
	if err != nil {
		t.Fatalf("load SenseVoice: %v", err)
	}
	defer m.Close()

	t.Logf("model: %s", modelPath)

	for _, wav := range wavs {
		wav := wav
		name := filepath.Base(wav)
		if _, err := os.Stat(wav); err != nil {
			t.Logf("skip %s: %v", name, err)
			continue
		}
		t.Run(name, func(t *testing.T) {
			pcm, err := LoadWAV(wav)
			if err != nil {
				t.Fatalf("LoadWAV: %v", err)
			}
			start := time.Now()
			text, err := m.Transcribe(pcm)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			dur := float64(len(pcm)) / 16000.0
			rtf := elapsed.Seconds() / dur
			fmt.Printf("\n=== %s ===\n", name)
			fmt.Printf("Expected: 明明白白我的心 渴望一份真感情\n")
			fmt.Printf("Got:      %s\n", text)
			fmt.Printf("audio: %.2fs | infer: %dms | RTF: %.3f\n\n", dur, elapsed.Milliseconds(), rtf)
			t.Logf("result: %q (%.0fms, RTF=%.3f)", text, float64(elapsed.Milliseconds()), rtf)
			if text == "" {
				t.Error("empty transcription")
			}
			// This is a stable Q8 baseline phrase for all three fixtures. Keep it
			// as the minimum recognition gate for any future lower-bit backend;
			// the latter half of the lyric varies in the Q8 baseline itself.
			if !strings.Contains(text, "明明白白我的心") {
				t.Errorf("expected Q8 baseline phrase in transcription, got %q", text)
			}
		})
	}
}
