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
	mlpPath := filepath.Join("corelib", "tts", "testdata", "duration_mlp.bin")

	model, _ := tts.NewPiper(modelPath, lexPath)
	w1, b1, w2, b2, _ := tts.LoadDurationMLP(mlpPath)
	model.DurMLPW1 = w1
	model.DurMLPB1 = b1
	model.DurMLPW2 = w2
	model.DurMLPB2 = b2

	text := "人工智能正在改变世界"
	g2p := tts.PiperTextToPhonemesWithLexicon(text, model.Lex)
	T := len(g2p.PhonemeIDs)

	// Run encoder to get m_p
	hp := model.HP
	w := model.W
	hidden := hp.HiddenChannels
	inter := hp.InterChannels
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
	stats := tts.Conv1D(x, hidden, T, w.TextEnc.Proj.Weight, w.TextEnc.Proj.KSize, inter*2, 1,
		(w.TextEnc.Proj.KSize-1)/2, w.TextEnc.Proj.Bias)
	mP := stats[:inter*T]

	durations, tMel := tts.PiperDurationFromEncoderMLP(mP, inter, T,
		g2p.PhonemeIDs, model.DurMLPW1, model.DurMLPB1, model.DurMLPW2, model.DurMLPB2)

	fmt.Printf("PIDs: %v\n", g2p.PhonemeIDs)
	fmt.Printf("Durs: %v (tMel=%d)\n", durations, tMel)
	// 不 is at positions: find b(4), u(49), tone
	fmt.Println("\nPer-char breakdown:")
	chars := []rune(text)
	pidIdx := 1 // skip ^
	for _, ch := range chars {
		phones := model.Lex.Lookup(ch)
		if phones == nil {
			continue
		}
		nPhones := len(phones)
		charDurs := durations[pidIdx : pidIdx+nPhones]
		total := 0
		for _, d := range charDurs {
			total += d
		}
		fmt.Printf("  %c: phones=%v durs=%v total=%d frames (%.0fms)\n",
			ch, phones, charDurs, total, float64(total)*256.0/22050.0*1000)
		pidIdx += nPhones + 1 // +1 for _ separator
	}
}
