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

	for _, text := range []string{"你好世界", "人工智能正在改变世界"} {
		g2p := tts.PiperTextToPhonemesWithLexicon(text, model.Lex)
		T := len(g2p.PhonemeIDs)
		hp := model.HP
		w := model.W
		hidden := hp.HiddenChannels

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

		durs, tMel := tts.PiperSDPForward(x, hidden, T, &w.SDP, hp, 0.0, g2p.PhonemeIDs)
		fmt.Printf("\n%s (T=%d, tMel=%d):\n", text, T, tMel)
		fmt.Printf("  PIDs: %v\n", g2p.PhonemeIDs)
		fmt.Printf("  Durs: %v\n", durs)

		chars := []rune(text)
		idx := 1
		for _, ch := range chars {
			phones := model.Lex.Lookup(ch)
			if phones == nil {
				continue
			}
			nP := len(phones)
			d := durs[idx : idx+nP]
			total := 0
			for _, v := range d {
				total += v
			}
			fmt.Printf("  %c: %v total=%d (%dms)\n", ch, d, total, total*256*1000/22050)
			idx += nP + 1
		}
	}
}
