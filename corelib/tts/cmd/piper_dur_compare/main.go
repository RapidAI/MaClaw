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

	texts := []string{"你好世界", "今天天气不错", "欢迎使用智能助手"}
	for _, text := range texts {
		g2p := tts.PiperTextToPhonemesWithLexicon(text, model.Lex)
		T := len(g2p.PhonemeIDs)
		hp := model.HP
		w := model.W
		hidden := hp.HiddenChannels

		// Run encoder
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
		meloHP := tts.HParams{
			HiddenChannels: hp.HiddenChannels, InterChannels: hp.InterChannels,
			FilterChannels: hp.FilterChannels, NHeads: hp.NHeads,
			NLayers: hp.NLayers, KernelSize: hp.KernelSize,
		}
		for i := 0; i < hp.NLayers; i++ {
			x = tts.EncoderLayerForwardExported(x, hidden, T, &w.TextEnc.Layers[i], meloHP)
		}

		logw := tts.PiperDurationPredictorForward(x, hidden, T, &w.SDP, hp)
		durations, tMel := tts.PiperComputeDurations(logw, 1.0)

		fmt.Printf("\n%s (T=%d, tMel=%d):\n", text, T, tMel)
		fmt.Printf("  PIDs: %v\n", g2p.PhonemeIDs)
		fmt.Printf("  Durs: %v\n", durations)
		fmt.Printf("  Logw: [")
		for i, v := range logw {
			if i > 0 { fmt.Print(", ") }
			fmt.Printf("%.2f", v)
		}
		fmt.Println("]")
	}
}
