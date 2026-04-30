package main

import (
	"encoding/binary"
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

	hp := model.HP
	w := model.W
	phonemeIDs := []int64{1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2}
	T := len(phonemeIDs)
	hidden := hp.HiddenChannels
	inter := hp.InterChannels

	// Embedding
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

	// Proj
	stats := tts.Conv1D(x, hidden, T, w.TextEnc.Proj.Weight, w.TextEnc.Proj.KSize, inter*2, 1,
		(w.TextEnc.Proj.KSize-1)/2, w.TextEnc.Proj.Bias)
	mP := stats[:inter*T]
	logsP := stats[inter*T:]

	var mpRMS, lpRMS float64
	for _, v := range mP { mpRMS += float64(v) * float64(v) }
	for _, v := range logsP { lpRMS += float64(v) * float64(v) }
	mpRMS = math.Sqrt(mpRMS / float64(len(mP)))
	lpRMS = math.Sqrt(lpRMS / float64(len(logsP)))

	fmt.Printf("Go m_p: RMS=%.6f (ONNX: 0.404035)\n", mpRMS)
	fmt.Printf("Go logs_p: RMS=%.6f (ONNX: 0.413078)\n", lpRMS)
	fmt.Printf("Go m_p first 5: [%.8f, %.8f, %.8f, %.8f, %.8f]\n",
		mP[0], mP[1], mP[2], mP[3], mP[4])
	fmt.Printf("ONNX m_p first 5: [0.12792143, 0.14487204, 0.16088870, 0.31769687, -0.08377289]\n")

	// Load ONNX reference m_p for comparison
	mpData, _ := os.ReadFile(filepath.Join("corelib", "tts", "testdata", "ref_piper_m_p.bin"))
	onnxMP := make([]float32, inter*T)
	for i := range onnxMP {
		onnxMP[i] = math.Float32frombits(binary.LittleEndian.Uint32(mpData[i*4 : (i+1)*4]))
	}

	var maxDiff float64
	for i := range mP {
		d := math.Abs(float64(mP[i]) - float64(onnxMP[i]))
		if d > maxDiff { maxDiff = d }
	}
	fmt.Printf("\nGo vs ONNX m_p: maxDiff=%.8f\n", maxDiff)
}
