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

	// Use ONNX reference durations instead of our phoneme-type table
	// ONNX durations for "你好世界": [2, 24, 7, 10, 7, 4, 5, 3, 7, 2, 7, 1, 7, 12, 1, 4, 9]
	phonemeIDs := []int64{1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2}
	onnxDurations := []int{2, 24, 7, 10, 7, 4, 5, 3, 7, 2, 7, 1, 7, 12, 1, 4, 9}
	tMel := 0
	for _, d := range onnxDurations {
		tMel += d
	}
	fmt.Printf("ONNX durations: %v, tMel=%d\n", onnxDurations, tMel)

	// Run encoder
	hp := model.HP
	w := model.W
	T := len(phonemeIDs)
	hidden := hp.HiddenChannels
	inter := hp.InterChannels

	sqrtH := float32(math.Sqrt(float64(hidden)))
	x := make([]float32, hidden*T)
	vocabSize := len(w.TextEnc.Emb) / hidden
	for t := 0; t < T; t++ {
		pid := int(phonemeIDs[t])
		if pid >= 0 && pid < vocabSize {
			for h := 0; h < hidden; h++ {
				x[h*T+t] = w.TextEnc.Emb[pid*hidden+h] * sqrtH
			}
		}
	}
	meloHP := tts.HParams{
		HiddenChannels: hp.HiddenChannels, InterChannels: hp.InterChannels,
		FilterChannels: hp.FilterChannels, NHeads: hp.NHeads,
		NLayers: hp.NLayers, KernelSize: hp.KernelSize,
	}
	for i := 0; i < hp.NLayers; i++ {
		x = tts.EncoderLayerForwardExported(x, hidden, T, &w.TextEnc.Layers[i], meloHP)
	}
	stats := tts.Conv1D(x, hidden, T, w.TextEnc.Proj.Weight, w.TextEnc.Proj.KSize, inter*2, 1,
		(w.TextEnc.Proj.KSize-1)/2, w.TextEnc.Proj.Bias)
	mP := stats[:inter*T]
	logsP := stats[inter*T:]

	// Expand with ONNX durations
	path, _ := tts.GeneratePath(onnxDurations)
	mPExp := tts.ExpandByDurations(mP, inter, T, path, tMel)
	logsPExp := tts.ExpandByDurations(logsP, inter, T, path, tMel)

	// Sample z_p (noise_scale=0 for deterministic)
	zP := make([]float32, inter*tMel)
	copy(zP, mPExp)
	_ = logsPExp

	var zpRMS float64
	for _, v := range zP { zpRMS += float64(v)*float64(v) }
	zpRMS = math.Sqrt(zpRMS / float64(len(zP)))
	fmt.Printf("Go z_p (ONNX dur, noise=0): RMS=%.6f (ONNX: 0.438023)\n", zpRMS)

	// Flow decoder
	z := tts.PiperFlowReverseForward(zP, inter, tMel, &w.Flow, hp)
	var zRMS float64
	for _, v := range z { zRMS += float64(v)*float64(v) }
	zRMS = math.Sqrt(zRMS / float64(len(z)))
	fmt.Printf("Go z: RMS=%.6f (ONNX: 1.585369)\n", zRMS)

	// Vocoder
	audio := tts.PiperHiFiGANForward(z, inter, tMel, &w.Vocoder, hp)
	var aRMS float64
	for _, v := range audio { aRMS += float64(v)*float64(v) }
	aRMS = math.Sqrt(aRMS / float64(len(audio)))
	fmt.Printf("Go audio: %d samples, RMS=%.6f\n", len(audio), aRMS)

	// Save
	wav := tts.EncodeWAV(audio, hp.SampleRate)
	wavPath := filepath.Join("corelib", "tts", "testdata", "go_piper_onnx_dur_你好世界.wav")
	os.WriteFile(wavPath, wav, 0644)
	fmt.Printf("Saved: %s\n", wavPath)
}
