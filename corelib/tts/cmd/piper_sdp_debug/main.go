package main

import (
	"fmt"
	"math"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")
	lexPath := filepath.Join("corelib", "tts", "testdata", "vits-piper-zh_CN-xiao_ya-medium", "lexicon.txt")
	model, _ := tts.NewPiper(modelPath, lexPath)

	g2p := tts.PiperTextToPhonemesWithLexicon("人工智能正在改变世界", model.Lex)
	T := len(g2p.PhonemeIDs)
	hp := model.HP
	w := model.W
	hidden := hp.HiddenChannels

	// Encoder
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
	meloHP := tts.HParams{HiddenChannels: hp.HiddenChannels, InterChannels: hp.InterChannels,
		FilterChannels: hp.FilterChannels, NHeads: hp.NHeads, NLayers: hp.NLayers, KernelSize: hp.KernelSize}
	for i := 0; i < hp.NLayers; i++ {
		x = tts.EncoderLayerForwardExported(x, hidden, T, &w.TextEnc.Layers[i], meloHP)
	}

	// SDP conditioning
	h := tts.Conv1D(x, hidden, T, w.SDP.Pre.Weight, w.SDP.Pre.KSize, hidden, 1,
		(w.SDP.Pre.KSize-1)/2, w.SDP.Pre.Bias)

	var preRMS float64
	for _, v := range h { preRMS += float64(v) * float64(v) }
	preRMS = math.Sqrt(preRMS / float64(len(h)))
	fmt.Printf("Go dp.pre: RMS=%.4f (ONNX: 0.2474)\n", preRMS)

	// DDSConv step by step
	ch := hidden
	for i := 0; i < hp.DPDDSLayers; i++ {
		dilation := 1
		for d := 0; d < i; d++ { dilation *= 3 }

		residual := make([]float32, len(h))
		copy(residual, h)

		kSize := w.SDP.Convs.ConvsSep[i].KSize
		if kSize == 0 { kSize = 3 }
		padding := (kSize - 1) * dilation / 2
		h = tts.DepthwiseConv1DExported(h, ch, T, w.SDP.Convs.ConvsSep[i].Weight, w.SDP.Convs.ConvsSep[i].Bias, kSize, padding, dilation)

		tts.ApplyLayerNormCTExported(h, ch, T, w.SDP.Convs.Norms1[i].Weight, w.SDP.Convs.Norms1[i].Bias)
		tts.ApplyGELUExported(h)

		h = tts.Conv1D(h, ch, T, w.SDP.Convs.Convs1x1[i].Weight, 1, ch, 1, 0, w.SDP.Convs.Convs1x1[i].Bias)

		var rms float64
		for _, v := range h { rms += float64(v) * float64(v) }
		rms = math.Sqrt(rms / float64(len(h)))
		fmt.Printf("Go convs_1x1.%d: RMS=%.4f\n", i, rms)

		tts.ApplyLayerNormCTExported(h, ch, T, w.SDP.Convs.Norms2[i].Weight, w.SDP.Convs.Norms2[i].Bias)
		tts.ApplyGELUExported(h)

		for j := range h { h[j] += residual[j] }
	}

	h = tts.Conv1D(h, hidden, T, w.SDP.Proj.Weight, w.SDP.Proj.KSize, hidden, 1,
		(w.SDP.Proj.KSize-1)/2, w.SDP.Proj.Bias)
	var projRMS float64
	for _, v := range h { projRMS += float64(v) * float64(v) }
	projRMS = math.Sqrt(projRMS / float64(len(h)))
	fmt.Printf("Go dp.proj: RMS=%.4f (ONNX: 0.4377)\n", projRMS)
}
