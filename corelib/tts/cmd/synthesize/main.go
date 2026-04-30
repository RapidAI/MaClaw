// Command synthesize generates a WAV file from text using the pure Go MeloTTS engine.
package main

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	ggufPath := "corelib/tts/testdata/melotts-en-fp32.gguf"
	configPath := "" // use default EN phoneme table
	outPath := "corelib/tts/testdata/go_mixed_demo.wav"

	text := "Hello, welcome to MacLaw. Let us build something great together. This is a test of the text to speech engine."

	fmt.Printf("Text: %s\n", text)
	fmt.Printf("Loading model: %s\n", ggufPath)
	t0 := time.Now()

	hp := tts.DefaultHParams()
	hp.SampleRate = 44100
	hp.HopLength = 512
	w, err := tts.LoadWeightsGGUF(ggufPath, hp)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Loaded in %v\n\n", time.Since(t0))

	// Load phoneme table from config
	var pt *tts.PhonemeTable
	if configPath != "" {
		pt, err = tts.NewPhonemeTableFromConfig(configPath)
	}
	if pt == nil {
		pt = tts.NewPhonemeTable() // default EN table
	}
	fmt.Printf("Phoneme table: %d symbols\n", pt.Size())

	// Load Gemma Embedding model
	gemmaPath := embedding.DefaultModelPath()
	var embedder embedding.Embedder
	if _, err := os.Stat(gemmaPath); err == nil {
		embedder, err = embedding.NewGemmaEmbedder(gemmaPath, 768)
		if err != nil {
			fmt.Printf("Gemma load warning: %v (continuing without BERT)\n", err)
		} else {
			fmt.Printf("Gemma Embedding loaded: dim=%d\n", embedder.Dim())
			defer embedder.Close()
		}
	} else {
		fmt.Printf("Gemma model not found at %s (continuing without BERT)\n", gemmaPath)
	}
	fmt.Println()
	sentences := splitSentences(text)
	var allAudio []float32

	// Speaker ID: EN model uses sid=0
	sid := 0
	g := make([]float32, hp.GinChannels)
	if sid*hp.GinChannels+hp.GinChannels <= len(w.SpeakerEmb) {
		copy(g, w.SpeakerEmb[sid*hp.GinChannels:(sid+1)*hp.GinChannels])
	}

	for i, sent := range sentences {
		if sent == "" {
			continue
		}
		lang := tts.DetectLanguage(sent)
		langName := "EN"
		if lang == tts.LangZH {
			langName = "ZH"
		}

		g2p := tts.TextToPhonemes(sent, pt, lang)
		if len(g2p.PhonemeIDs) == 0 {
			continue
		}

		fmt.Printf("[%d] %q (%s, %d phonemes)\n", i, sent, langName, len(g2p.PhonemeIDs))
		t1 := time.Now()

		// Compute BERT embeddings using Gemma
		T := len(g2p.PhonemeIDs)
		_, jaBert := tts.ComputeBertEmbedding(sent, nil, T, embedder)

		encOut, mP, logsP, _ := tts.TextEncoderForward(
			g2p.PhonemeIDs, g2p.ToneIDs, g2p.LangIDs,
			g, hp.GinChannels, nil, jaBert, &w.TextEnc, hp)

		logw := tts.DurationPredictorForward(encOut, hp.HiddenChannels, T, g, hp.GinChannels, &w.DurPred)
		// Chinese needs slower speed
		lengthScale := float32(1.1)
		if lang == tts.LangZH {
			lengthScale = 1.8
		}
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
			zP[j] = mPExp[j] + zP[j]*0.667*float32(math.Exp(float64(lp)))
		}

		z := tts.FlowReverseForward(zP, hp.InterChannels, tMel, g, hp.GinChannels, &w.Flow, hp)
		audio := tts.HiFiGANForward(z, hp.InterChannels, tMel, g, hp.GinChannels, &w.Vocoder, hp)

		dur := time.Since(t1)
		audioDur := float64(len(audio)) / float64(hp.SampleRate)
		fmt.Printf("    → %.2fs audio, %v inference (RTF=%.1f)\n", audioDur, dur, dur.Seconds()/audioDur)

		allAudio = append(allAudio, audio...)
		// 150ms silence between sentences
		allAudio = append(allAudio, make([]float32, hp.SampleRate*15/100)...)
	}

	if len(allAudio) == 0 {
		fmt.Println("No audio generated")
		os.Exit(1)
	}

	wavData := tts.EncodeWAV(allAudio, hp.SampleRate)
	os.WriteFile(outPath, wavData, 0644)

	totalDur := float64(len(allAudio)) / float64(hp.SampleRate)
	fmt.Printf("\nSaved: %s (%.2f seconds)\n", outPath, totalDur)
}

func splitSentences(text string) []string {
	var sentences []string
	var current []rune
	for _, r := range text {
		current = append(current, r)
		if r == '。' || r == '.' || r == '！' || r == '!' || r == '？' || r == '?' {
			sentences = append(sentences, string(current))
			current = nil
		}
	}
	if len(current) > 0 {
		sentences = append(sentences, string(current))
	}
	return sentences
}
