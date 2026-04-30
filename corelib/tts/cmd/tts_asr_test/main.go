// TTS → ASR round-trip test: synthesize text, then recognize it back.
package main

import (
	"fmt"
	"math"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	ttsGGUF := "corelib/tts/testdata/melotts-en-fp32.gguf"
	ttsConfig := ""
	asrGGUF := "RapidSpeech.cpp/models/gguf/moonshine-base-zh.gguf"

	// Check ASR model exists
	if _, err := os.Stat(asrGGUF); os.IsNotExist(err) {
		// Try alternative paths
		for _, p := range []string{
			"RapidSpeech.cpp/models/gguf/moonshine-base.gguf",
			"RapidSpeech.cpp/models/gguf/moonshine-tiny.gguf",
		} {
			if _, err := os.Stat(p); err == nil {
				asrGGUF = p
				break
			}
		}
	}

	fmt.Println("=== TTS → ASR Round-Trip Test ===")
	fmt.Printf("TTS model: %s\n", ttsGGUF)
	fmt.Printf("ASR model: %s\n", asrGGUF)

	// Load TTS
	hp := tts.DefaultHParams()
	hp.SampleRate = 44100
	hp.HopLength = 512
	w, err := tts.LoadWeightsGGUF(ttsGGUF, hp)
	if err != nil {
		fmt.Printf("TTS load error: %v\n", err)
		os.Exit(1)
	}

	// Load ASR
	asrModel, err := asr.NewMoonshine(asrGGUF)
	if err != nil {
		fmt.Printf("ASR load error: %v\n", err)
		os.Exit(1)
	}
	defer asrModel.Close()

	pt, err2 := tts.NewPhonemeTableFromConfig(ttsConfig)
	if err2 != nil || ttsConfig == "" {
		pt = tts.NewPhonemeTable()
	}
	fmt.Printf("Phoneme table: %d symbols\n", pt.Size())
	// Speaker ID
	sid := 0
	g := make([]float32, hp.GinChannels)
	if sid*hp.GinChannels+hp.GinChannels <= len(w.SpeakerEmb) {
		copy(g, w.SpeakerEmb[sid*hp.GinChannels:(sid+1)*hp.GinChannels])
	}

	// Load Gemma for BERT embedding
	gemmaPath := embedding.DefaultModelPath()
	var embedder embedding.Embedder
	if _, err3 := os.Stat(gemmaPath); err3 == nil {
		embedder, _ = embedding.NewGemmaEmbedder(gemmaPath, 768)
		if embedder != nil {
			defer embedder.Close()
			fmt.Printf("Gemma loaded: dim=%d\n", embedder.Dim())
		}
	}

	tests := []struct {
		text        string
		lang        int
		lengthScale float32
	}{
		{"Hello", tts.LangEN, 1.2},
		{"Hello world", tts.LangEN, 1.2},
		{"Good morning", tts.LangEN, 1.2},
		{"Welcome to MacLaw", tts.LangEN, 1.2},
		{"This is a test", tts.LangEN, 1.2},
		{"How are you today", tts.LangEN, 1.2},
	}

	// Test with different configs
	configs := []struct {
		name       string
		noiseScale float32
		useGemma   bool
	}{
		{"no_bert_ns0.667", 0.667, false},
		{"no_bert_ns0.3", 0.3, false},
		{"gemma_ns0.667", 0.667, true},
		{"gemma_ns0.3", 0.3, true},
	}

	for _, cfg := range configs {
		fmt.Printf("\n=== Config: %s ===\n", cfg.name)
		for _, tt := range tests {
			g2p := tts.TextToPhonemes(tt.text, pt, tt.lang)
			if len(g2p.PhonemeIDs) == 0 {
				continue
			}

			T := len(g2p.PhonemeIDs)
			var jaBert []float32
			if cfg.useGemma && embedder != nil {
				_, jaBert = tts.ComputeBertEmbedding(tt.text, nil, T, embedder)
			}

			audio := synthesize(g2p, g, w, hp, tt.lengthScale, jaBert, cfg.noiseScale)
			pcm16k := resample(audio, hp.SampleRate, 16000)

			text, err := asrModel.Transcribe(pcm16k)
			if err != nil {
				continue
			}
			match := ""
			if text == tt.text || text == tt.text+"." {
				match = " ✅"
			}
			fmt.Printf("  %-25s → %q%s\n", tt.text, text, match)
		}
	}
}

func synthesize(g2p tts.G2PResult, g []float32, w *tts.Weights, hp tts.HParams, lengthScale float32, jaBert []float32, noiseScale float32) []float32 {
	encOut, mP, logsP, T := tts.TextEncoderForward(
		g2p.PhonemeIDs, g2p.ToneIDs, g2p.LangIDs,
		g, hp.GinChannels, nil, jaBert, &w.TextEnc, hp)

	logw := tts.DurationPredictorForward(encOut, hp.HiddenChannels, T, g, hp.GinChannels, &w.DurPred)
	durations, tMel := tts.ComputeDurations(logw, lengthScale)

	path, _ := tts.GeneratePath(durations)
	mPExp := tts.ExpandByDurations(mP, hp.InterChannels, T, path, tMel)
	logsPExp := tts.ExpandByDurations(logsP, hp.InterChannels, T, path, tMel)

	zP := make([]float32, hp.InterChannels*tMel)
	tts.RandnScale(zP, 1.0)
	for j := range zP {
		lp := logsPExp[j]
		if lp > 10 {
			lp = 10
		} else if lp < -20 {
			lp = -20
		}
		zP[j] = mPExp[j] + zP[j]*noiseScale*float32(math.Exp(float64(lp)))
	}

	z := tts.FlowReverseForward(zP, hp.InterChannels, tMel, g, hp.GinChannels, &w.Flow, hp)
	return tts.HiFiGANForward(z, hp.InterChannels, tMel, g, hp.GinChannels, &w.Vocoder, hp)
}

func resample(in []float32, srcRate, dstRate int) []float32 {
	if srcRate == dstRate {
		return in
	}
	outLen := int(int64(len(in)) * int64(dstRate) / int64(srcRate))
	out := make([]float32, outLen)
	ratio := float64(srcRate) / float64(dstRate)
	for i := 0; i < outLen; i++ {
		pos := float64(i) * ratio
		idx := int(pos)
		frac := float32(pos - float64(idx))
		s0 := in[idx]
		s1 := s0
		if idx+1 < len(in) {
			s1 = in[idx+1]
		}
		out[i] = s0*(1-frac) + s1*frac
	}
	return out
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, byte(r))
		} else if r >= 0x4E00 && r <= 0x9FFF {
			out = append(out, '_')
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return string(out)
}
