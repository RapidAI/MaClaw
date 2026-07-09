package asr

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBatchEval runs ASR on all available test WAV files and writes results to a file.
// Run with: go test -v -run TestBatchEval -timeout 300s ./corelib/asr/
func TestBatchEval(t *testing.T) {
	modelPath := findModel(t)
	m, err := NewMoonshine(modelPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	defer m.Close()

	t.Logf("Model: enc=%dx%d dec=%dx%d vocab=%d partial_rot=%.2f",
		m.hp.EncoderDim, m.hp.EncoderDepth,
		m.hp.DecoderDim, m.hp.DecoderDepth,
		m.hp.VocabSize, m.hp.PartialRot)

	// Collect all test WAV files
	root := filepath.Join("..", "..")
	wavFiles := []string{}

	// Root-level test files
	rootWavs := []string{"zhou_16k.wav", "beiing_16k.wav", "test_16k_mono.wav"}
	for _, f := range rootWavs {
		p := filepath.Join(root, f)
		if _, err := os.Stat(p); err == nil {
			wavFiles = append(wavFiles, p)
		}
	}

	// TTS testdata wav files (synthetic speech - good test for clean audio)
	ttsDir := filepath.Join(root, "corelib", "tts", "testdata")
	if entries, err := os.ReadDir(ttsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".wav" {
				wavFiles = append(wavFiles, filepath.Join(ttsDir, e.Name()))
			}
		}
	}

	if len(wavFiles) == 0 {
		t.Skip("no WAV files found")
	}

	// Output file for readable results
	outPath := filepath.Join(root, "asr_eval_results.txt")
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create output: %v", err)
	}
	defer out.Close()

	fmt.Fprintf(out, "ASR Batch Evaluation Results\n")
	fmt.Fprintf(out, "Model: moonshine-base-zh (enc=%d dec=%d vocab=%d)\n",
		m.hp.EncoderDim, m.hp.DecoderDim, m.hp.VocabSize)
	fmt.Fprintf(out, "Time: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	totalFiles := 0
	totalDuration := 0.0
	totalTime := time.Duration(0)

	for _, wavPath := range wavFiles {
		pcm, err := LoadWAV(wavPath)
		if err != nil {
			fmt.Fprintf(out, "[SKIP] %s: %v\n", filepath.Base(wavPath), err)
			continue
		}
		if len(pcm) < 4000 { // skip < 0.25s
			continue
		}

		dur := float64(len(pcm)) / 16000.0
		rmsVal := calcTestRMS(pcm)
		peakVal := calcTestPeak(pcm)

		start := time.Now()
		text, err := m.Transcribe(pcm)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Fprintf(out, "[ERR] %s: %v\n", filepath.Base(wavPath), err)
			continue
		}

		totalFiles++
		totalDuration += dur
		totalTime += elapsed

		fmt.Fprintf(out, "%-50s dur=%.2fs rms=%.4f peak=%.3f time=%4dms | %s\n",
			filepath.Base(wavPath), dur, rmsVal, peakVal, elapsed.Milliseconds(), text)
		t.Logf("%-40s %.2fs %4dms -> %s", filepath.Base(wavPath), dur, elapsed.Milliseconds(), text)
	}

	fmt.Fprintf(out, "\n---\nTotal: %d files, %.1fs audio, %dms compute, RTF=%.2f\n",
		totalFiles, totalDuration, totalTime.Milliseconds(),
		totalTime.Seconds()/totalDuration)

	t.Logf("\nResults written to: %s", outPath)
	t.Logf("Total: %d files, %.1fs audio, %dms compute", totalFiles, totalDuration, totalTime.Milliseconds())
}

func calcTestRMS(pcm []float32) float64 {
	var sum float64
	for _, s := range pcm {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

func calcTestPeak(pcm []float32) float64 {
	var peak float64
	for _, s := range pcm {
		if v := math.Abs(float64(s)); v > peak {
			peak = v
		}
	}
	return peak
}
