package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTransliterate_TechWords(t *testing.T) {
	words := []string{
		"kubernetes", "tensorflow", "numpy", "pandas",
		"install", "commit", "deploy", "timeout",
		"overflow", "undefined", "exception", "function",
		"container", "database", "network", "service",
		"configuration", "application", "development",
		"performance", "beautiful", "important",
	}
	for _, w := range words {
		phones := englishWordTransliterate(w)
		t.Logf("%-20s → %v", w, phones)
		if len(phones) == 0 {
			t.Errorf("%s produced no phones", w)
		}
	}
}

func TestTransliterate_Abbreviations(t *testing.T) {
	// Short all-caps should go through letter spelling, not transliteration
	words := []string{"API", "GPU", "CPU", "TCP", "URL"}
	for _, w := range words {
		result := englishWordToPhones(w)
		t.Logf("%-6s → %d charPhones", w, len(result))
		// Should have multiple charPhoneInfo (one per letter)
		if len(result) < 2 {
			t.Errorf("%s: expected multiple charPhones for abbreviation, got %d", w, len(result))
		}
	}
	// HTTP is in the word table, so it goes through word lookup (1 charPhone)
	httpResult := englishWordToPhones("HTTP")
	t.Logf("HTTP   → %d charPhones (word table hit)", len(httpResult))
}

func TestTransliterate_MixedCase(t *testing.T) {
	// Mixed case words should use transliteration (word table or rule engine),
	// producing multiple charPhoneInfo entries (one per syllable) for connected speech.
	words := []string{"Kubernetes", "TensorFlow", "JavaScript"}
	for _, w := range words {
		result := englishWordToPhones(w)
		t.Logf("%-15s → %d charPhones", w, len(result))
		if len(result) == 0 {
			t.Errorf("%s: no charPhones produced", w)
		}
		// Should have multiple syllables (not letter-by-letter which would have one per letter)
		if len(result) > len(w) {
			t.Errorf("%s: too many charPhones (%d > %d letters), looks like letter spelling", w, len(result), len(w))
		}
	}
}

func TestTransliterate_Synthesize(t *testing.T) {
	modelPath := filepath.Join("testdata", "piper-xiao_ya-zh-fp32.gguf")
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skip("model not found")
	}
	lexPath := filepath.Join("testdata", "vits-piper-zh_CN-xiao_ya-medium", "lexicon.txt")
	model, err := NewPiper(modelPath, lexPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	cacheDir := "testdata"
	dc, _ := LoadDurationCache(
		filepath.Join(cacheDir, "duration_trigram_cache.json"),
		filepath.Join(cacheDir, "duration_bigram_cache.json"),
		filepath.Join(cacheDir, "duration_unigram_cache.json"),
	)
	if dc != nil {
		model.DurCache = dc
	}
	w1, b1, w2, b2, _ := LoadDurationMLP(filepath.Join(cacheDir, "duration_mlp.bin"))
	if w1 != nil {
		model.DurMLPW1 = w1
		model.DurMLPB1 = b1
		model.DurMLPW2 = w2
		model.DurMLPB2 = b2
	}

	texts := []string{
		"正在install依赖",
		"kubernetes集群部署完成",
		"发现了一个timeout错误",
		"请检查database连接",
		"TensorFlow训练完成",
		"这个function有bug",
		"application启动成功",
		"performance优化建议",
	}

	for _, text := range texts {
		g2p := PiperTextToPhonemesWithLexicon(text, model.Lex)
		t.Logf("\n%s → IDs(%d)", text, len(g2p.PhonemeIDs))

		t1 := time.Now()
		audio, err := model.Synthesize(PiperSynthesizeInput{PhonemeIDs: g2p.PhonemeIDs})
		elapsed := time.Since(t1)
		if err != nil {
			t.Errorf("%s: %v", text, err)
			continue
		}
		dur := float64(len(audio)) / float64(model.HP.SampleRate)
		t.Logf("  dur=%.2fs elapsed=%v RTF=%.3f", dur, elapsed, elapsed.Seconds()/dur)

		wav := EncodeWAV(audio, model.HP.SampleRate)
		safeName := text
		if len([]rune(safeName)) > 20 {
			safeName = string([]rune(safeName)[:20])
		}
		wavPath := filepath.Join("testdata", fmt.Sprintf("go_piper_translit_%s.wav", safeName))
		os.WriteFile(wavPath, wav, 0644)
		t.Logf("  → %s", wavPath)
	}
}
