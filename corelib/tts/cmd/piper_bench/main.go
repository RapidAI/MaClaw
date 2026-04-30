package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")
	model, err := tts.NewPiper(modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("CPUs: %d\n", runtime.NumCPU())

	// Benchmark "你好世界"
	g2p := tts.PiperTextToPhonemes("你好世界")
	input := tts.PiperSynthesizeInput{PhonemeIDs: g2p.PhonemeIDs}

	// Warmup
	model.Synthesize(input)

	// Benchmark
	const N = 3
	var totalElapsed time.Duration
	var totalSamples int
	for i := 0; i < N; i++ {
		t0 := time.Now()
		audio, _ := model.Synthesize(input)
		elapsed := time.Since(t0)
		totalElapsed += elapsed
		totalSamples += len(audio)
		dur := float64(len(audio)) / 22050.0
		rtf := elapsed.Seconds() / dur
		fmt.Printf("  Run %d: %d samples, %.2fs audio, %v elapsed, RTF=%.2f\n",
			i+1, len(audio), dur, elapsed, rtf)
	}

	avgElapsed := totalElapsed / N
	avgDur := float64(totalSamples/N) / 22050.0
	avgRTF := avgElapsed.Seconds() / avgDur
	fmt.Printf("\nAverage: RTF=%.2f (%.0fms for %.2fs audio)\n", avgRTF, float64(avgElapsed.Milliseconds()), avgDur)

	// Longer text benchmark
	fmt.Println("\n--- Longer text ---")
	g2p2 := tts.PiperTextToPhonemes("人工智能正在改变世界，让我们一起来探索未来的无限可能")
	input2 := tts.PiperSynthesizeInput{PhonemeIDs: g2p2.PhonemeIDs}
	model.Synthesize(input2) // warmup

	for i := 0; i < 2; i++ {
		t0 := time.Now()
		audio, _ := model.Synthesize(input2)
		elapsed := time.Since(t0)
		dur := float64(len(audio)) / 22050.0
		rtf := elapsed.Seconds() / dur
		fmt.Printf("  Run %d: %d samples, %.2fs audio, %v elapsed, RTF=%.2f\n",
			i+1, len(audio), dur, elapsed, rtf)
	}
}
