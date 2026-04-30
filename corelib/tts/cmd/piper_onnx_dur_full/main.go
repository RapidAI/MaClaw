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
	model, _ := tts.NewPiper(modelPath)

	// Use ONNX's exact durations for "人工智能正在改变世界"
	pids := []int64{1, 21, 35, 65, 0, 12, 38, 64, 0, 18, 39, 67, 0, 10, 37, 65, 0, 18, 37, 67, 0, 22, 30, 67, 0, 12, 30, 66, 0, 4, 44, 67, 0, 20, 39, 67, 0, 15, 41, 67, 2}
	// ONNX durations (from extract):
	onnxDurs := []int{4, 23, 5, 15, 3, 5, 5, 4, 5, 4, 3, 4, 6, 10, 2, 8, 6, 9, 2, 3, 3, 5, 4, 2, 5, 4, 5, 4, 4, 5, 4, 3, 9, 5, 3, 2, 7, 9, 3, 7, 6}

	T := len(pids)
	hp := model.HP
	w := model.W
	hidden := hp.HiddenChannels
	inter := hp.InterChannels

	// Encoder
	sqrtH := float32(math.Sqrt(float64(hidden)))
	x := make([]float32, hidden*T)
	vocabSize := len(w.TextEnc.Emb) / hidden
	for t := 0; t < T; t++ {
		pid := int(pids[t])
		if pid >= 0 && pid < vocabSize {
			for h := 0; h < hidden; h++ {
				x[h*T+t] = w.TextEnc.Emb[pid*hidden+h] * sqrtH
			}
		}
	}
	meloHP := tts.HParams{HiddenChannels: hp.HiddenChannels, InterChannels: hp.InterChannels,
		FilterChannels: hp.FilterChannels, NHeads: hp.NHeads, NLayers: hp.NLayers, KernelSize: hp.KernelSize}
	for i := 0; i < hp.NLayers; i++ {
		x = tts.EncoderLayerForwardExported(x, hidden, T, &w.TextEnc.Layers[i], meloHP)
	}
	stats := tts.Conv1D(x, hidden, T, w.TextEnc.Proj.Weight, w.TextEnc.Proj.KSize, inter*2, 1,
		(w.TextEnc.Proj.KSize-1)/2, w.TextEnc.Proj.Bias)
	mP := stats[:inter*T]
	logsP := stats[inter*T:]

	// Use ONNX durations
	tMel := 0
	for _, d := range onnxDurs {
		tMel += d
	}
	path, _ := tts.GeneratePath(onnxDurs)
	mPExp := tts.ExpandByDurations(mP, inter, T, path, tMel)
	logsPExp := tts.ExpandByDurations(logsP, inter, T, path, tMel)

	// Sample z_p
	zP := make([]float32, inter*tMel)
	tts.RandnScale(zP, 1.0)
	for i := range zP {
		lp := logsPExp[i]
		if lp > 10 { lp = 10 } else if lp < -20 { lp = -20 }
		zP[i] = mPExp[i] + zP[i]*0.667*float32(math.Exp(float64(lp)))
	}

	// Flow + vocoder
	z := tts.PiperFlowReverseForward(zP, inter, tMel, &w.Flow, hp)
	audio := tts.PiperHiFiGANForward(z, inter, tMel, &w.Vocoder, hp)

	wav := tts.EncodeWAV(audio, hp.SampleRate)
	wavPath := filepath.Join("corelib", "tts", "testdata", "go_onnx_dur_人工智能正在改变世界.wav")
	os.WriteFile(wavPath, wav, 0644)
	fmt.Printf("Go with ONNX durations: %d samples, %.2fs\n", len(audio), float64(len(audio))/22050)
}
