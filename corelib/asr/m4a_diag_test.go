package asr

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestM4ADiagnostics prints audio statistics for the m4a test file to understand
// why recognition is poor.
func TestM4ADiagnostics(t *testing.T) {
	wavPath := filepath.Join("..", "..", "明明白白我的心.wav")
	if _, err := os.Stat(wavPath); err != nil {
		t.Skipf("WAV file not found: %v", err)
	}

	pcm, err := LoadWAV(wavPath)
	if err != nil {
		t.Fatalf("LoadWAV: %v", err)
	}

	totalSamples := len(pcm)
	durationSec := float64(totalSamples) / 16000.0

	// Overall stats
	var sum, peak float64
	for _, s := range pcm {
		v := float64(s)
		sum += v * v
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	overallRMS := math.Sqrt(sum / float64(totalSamples))

	fmt.Printf("\n=== Audio Diagnostics ===\n")
	fmt.Printf("Duration: %.2fs (%d samples)\n", durationSec, totalSamples)
	fmt.Printf("Overall RMS: %.5f  Peak: %.4f\n\n", overallRMS, peak)

	// Per-500ms segment analysis
	frameSize := 8000 // 500ms at 16kHz
	fmt.Printf("Per-segment (500ms) analysis:\n")
	fmt.Printf("%-8s %-10s %-10s %-10s\n", "Time", "RMS", "Peak", "Note")
	for start := 0; start < totalSamples; start += frameSize {
		end := start + frameSize
		if end > totalSamples {
			end = totalSamples
		}
		seg := pcm[start:end]

		var segSum, segPeak float64
		for _, s := range seg {
			v := float64(s)
			segSum += v * v
			if a := math.Abs(v); a > segPeak {
				segPeak = a
			}
		}
		segRMS := math.Sqrt(segSum / float64(len(seg)))
		timeSec := float64(start) / 16000.0

		note := ""
		if segRMS < 0.002 {
			note = "SILENCE"
		} else if segRMS < 0.01 {
			note = "very quiet"
		} else if segRMS > 0.05 {
			note = "LOUD"
		}

		fmt.Printf("%.1fs    %.5f   %.4f    %s\n", timeSec, segRMS, segPeak, note)
	}
	fmt.Printf("========================\n\n")
}
