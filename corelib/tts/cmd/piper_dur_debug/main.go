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

	g2p := tts.PiperTextToPhonemes("你好世界")
	fmt.Printf("Phoneme IDs: %v\n", g2p.PhonemeIDs)

	// Run encoder to get x
	hp := model.HP
	w := model.W
	T := len(g2p.PhonemeIDs)
	hidden := hp.HiddenChannels

	// Embedding
	sqrtH := float32(math.Sqrt(float64(hidden)))
	x := make([]float32, hidden*T)
	vocabSize := len(w.TextEnc.Emb) / hidden
	for t := 0; t < T; t++ {
		pid := int(g2p.PhonemeIDs[t])
		if pid >= 0 && pid < vocabSize {
			for h := 0; h < hidden; h++ {
				x[h*T+t] = w.TextEnc.Emb[pid*hidden+h] * sqrtH
			}
		}
	}

	// Encoder
	meloHP := tts.HParams{
		HiddenChannels: hp.HiddenChannels,
		InterChannels:  hp.InterChannels,
		FilterChannels: hp.FilterChannels,
		NHeads:         hp.NHeads,
		NLayers:        hp.NLayers,
		KernelSize:     hp.KernelSize,
	}
	for i := 0; i < hp.NLayers; i++ {
		x = tts.EncoderLayerForwardExported(x, hidden, T, &w.TextEnc.Layers[i], meloHP)
	}

	// Duration predictor
	logw := tts.PiperDurationPredictorForward(x, hidden, T, &w.SDP, hp)
	fmt.Printf("\nGo logw: [")
	for i, v := range logw {
		if i > 0 { fmt.Print(", ") }
		fmt.Printf("%.3f", v)
	}
	fmt.Println("]")

	// Compute durations
	durations, tMel := tts.PiperComputeDurations(logw, 1.0)
	fmt.Printf("Go durations: %v\n", durations)
	fmt.Printf("Go total mel: %d\n", tMel)

	fmt.Println("\nONNX reference:")
	fmt.Println("logw: [0.448, 3.166, 1.873, 2.227, 1.882, 1.366, 1.543, 0.951, 1.894, 0.592, 1.841, -0.778, 1.929, 2.434, -0.221, 1.370, 2.142]")
	fmt.Println("durations: [2, 24, 7, 10, 7, 4, 5, 3, 7, 2, 7, 1, 7, 12, 1, 4, 9]")
	fmt.Println("total mel: 112")

	// Check SDP intermediate values
	fmt.Println("\n=== SDP Debug ===")
	// Pre
	h := tts.Conv1D(x, hidden, T, w.SDP.Pre.Weight, w.SDP.Pre.KSize, hidden, 1,
		(w.SDP.Pre.KSize-1)/2, w.SDP.Pre.Bias)
	var preRMS float64
	for _, v := range h { preRMS += float64(v) * float64(v) }
	preRMS = math.Sqrt(preRMS / float64(len(h)))
	fmt.Printf("After SDP pre: RMS=%.6f\n", preRMS)

	// Channel means at each time step
	fmt.Print("Channel means: [")
	for t := 0; t < T; t++ {
		var sum float64
		for c := 0; c < hidden; c++ {
			sum += float64(h[c*T+t])
		}
		if t > 0 { fmt.Print(", ") }
		fmt.Printf("%.4f", sum/float64(hidden))
	}
	fmt.Println("]")
}
