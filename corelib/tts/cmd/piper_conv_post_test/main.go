package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")
	model, err := tts.NewPiper(modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		os.Exit(1)
	}

	cp := model.W.Vocoder.ConvPost
	fmt.Printf("conv_post: OutCh=%d, InCh=%d, KSize=%d, WeightLen=%d, HasBias=%v\n",
		cp.OutCh, cp.InCh, cp.KSize, len(cp.Weight), cp.Bias != nil)
	fmt.Printf("  Weight[:7]: ")
	for i := 0; i < 7 && i < len(cp.Weight); i++ {
		fmt.Printf("%.8f ", cp.Weight[i])
	}
	fmt.Println()

	// Test conv_post with a simple input
	ch := 32
	T := 100
	input := make([]float32, ch*T)
	for i := range input {
		input[i] = 0.058 * float32(math.Sin(float64(i)*0.1))
	}

	var inRMS float64
	for _, v := range input {
		inRMS += float64(v) * float64(v)
	}
	inRMS = math.Sqrt(inRMS / float64(len(input)))

	out := tts.Conv1D(input, ch, T, cp.Weight, cp.KSize, cp.OutCh, 1, (cp.KSize-1)/2, cp.Bias)

	var outRMS float64
	for _, v := range out {
		outRMS += float64(v) * float64(v)
	}
	outRMS = math.Sqrt(outRMS / float64(len(out)))

	fmt.Printf("Input RMS=%.6f, Output RMS=%.6f, Ratio=%.3f\n", inRMS, outRMS, outRMS/inRMS)
}
