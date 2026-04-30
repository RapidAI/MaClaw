package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")
	if _, err := os.Stat(modelPath); err != nil {
		fmt.Fprintf(os.Stderr, "Model not found: %s\n", modelPath)
		os.Exit(1)
	}

	fmt.Println("Loading Piper VITS model...")
	t0 := time.Now()
	lexPath := filepath.Join("corelib", "tts", "testdata", "vits-piper-zh_CN-xiao_ya-medium", "lexicon.txt")
	model, err := tts.NewPiper(modelPath, lexPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load model: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Model loaded in %v (lexicon: %d entries)\n", time.Since(t0), model.Lex.Size())

	// Load duration cache (trigram)
	cacheDir := filepath.Join("corelib", "tts", "testdata")
	durCache, err := tts.LoadDurationCache(
		filepath.Join(cacheDir, "duration_trigram_cache.json"),
		filepath.Join(cacheDir, "duration_bigram_cache.json"),
		filepath.Join(cacheDir, "duration_unigram_cache.json"),
	)
	if err == nil {
		model.DurCache = durCache
		fmt.Printf("Duration cache loaded: %d trigrams, %d bigrams, %d unigrams\n",
			len(durCache.Trigram), len(durCache.Bigram), len(durCache.Unigram))
	}

	// Load duration MLP as fallback
	mlpPath := filepath.Join("corelib", "tts", "testdata", "duration_mlp.bin")
	w1, b1, w2, b2, err := tts.LoadDurationMLP(mlpPath)
	if err == nil {
		model.DurMLPW1 = w1
		model.DurMLPB1 = b1
		model.DurMLPW2 = w2
		model.DurMLPB2 = b2
		fmt.Println("Duration MLP loaded")
	}

	texts := []string{
		"你好世界",
		"今天天气不错",
		"我们一起来写代码吧",
		"欢迎使用智能助手",
		"人工智能正在改变世界",
	}

	outDir := filepath.Join("corelib", "tts", "testdata")

	for _, text := range texts {
		g2p := tts.PiperTextToPhonemesWithLexicon(text, model.Lex)
		fmt.Printf("\n%s → IDs(%d): %v\n", text, len(g2p.PhonemeIDs), g2p.PhonemeIDs)

		t1 := time.Now()
		audio, err := model.Synthesize(tts.PiperSynthesizeInput{
			PhonemeIDs: g2p.PhonemeIDs,
		})
		elapsed := time.Since(t1)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			continue
		}

		dur := float64(len(audio)) / float64(model.HP.SampleRate)
		var rms float64
		for _, v := range audio {
			rms += float64(v) * float64(v)
		}
		rms = math.Sqrt(rms / float64(len(audio)))
		fmt.Printf("  %d samples, %.2fs, RTF=%.2f, RMS=%.4f\n", len(audio), dur, elapsed.Seconds()/dur, rms)

		wav := tts.EncodeWAV(audio, model.HP.SampleRate)
		safeName := text
		if len(safeName) > 30 {
			safeName = safeName[:30]
		}
		wavPath := filepath.Join(outDir, fmt.Sprintf("go_piper_%s.wav", safeName))
		os.WriteFile(wavPath, wav, 0644)
	}
}
