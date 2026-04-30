package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnglishG2P_LetterSpelling(t *testing.T) {
	// Single letters should produce phoneme IDs
	result := PiperTextToPhonemes("ABC")
	if len(result.PhonemeIDs) <= 2 { // at least ^ + $ + some phonemes
		t.Errorf("ABC produced too few phonemes: %v", result.PhonemeIDs)
	}
	t.Logf("ABC → IDs(%d): %v", len(result.PhonemeIDs), result.PhonemeIDs)
}

func TestEnglishG2P_CommonWords(t *testing.T) {
	tests := []struct {
		text string
	}{
		{"OK"},
		{"Python"},
		{"MacLaw"},
		{"hello"},
		{"WiFi"},
	}
	for _, tc := range tests {
		result := PiperTextToPhonemes(tc.text)
		if len(result.PhonemeIDs) <= 2 {
			t.Errorf("%s produced too few phonemes: %v", tc.text, result.PhonemeIDs)
		}
		t.Logf("%s → IDs(%d): %v", tc.text, len(result.PhonemeIDs), result.PhonemeIDs)
	}
}

func TestEnglishG2P_MixedText(t *testing.T) {
	tests := []struct {
		text string
	}{
		{"MacLaw智能助手"},
		{"使用Python开发"},
		{"Hello你好世界"},
		{"2024年新年快乐"},
		{"WiFi密码是123"},
		{"请打开GitHub查看代码"},
	}
	for _, tc := range tests {
		result := PiperTextToPhonemes(tc.text)
		if len(result.PhonemeIDs) <= 2 {
			t.Errorf("%s produced too few phonemes: %v", tc.text, result.PhonemeIDs)
		}
		t.Logf("%s → IDs(%d): %v", tc.text, len(result.PhonemeIDs), result.PhonemeIDs)
	}
}

func TestEnglishG2P_Digits(t *testing.T) {
	result := PiperTextToPhonemes("2024")
	if len(result.PhonemeIDs) <= 2 {
		t.Errorf("2024 produced too few phonemes: %v", result.PhonemeIDs)
	}
	t.Logf("2024 → IDs(%d): %v", len(result.PhonemeIDs), result.PhonemeIDs)
}

func TestEnglishG2P_PureEnglish(t *testing.T) {
	// Pure English should not produce empty output
	result := PiperTextToPhonemes("Hello World")
	if len(result.PhonemeIDs) <= 2 {
		t.Errorf("Hello World produced too few phonemes: %v", result.PhonemeIDs)
	}
	t.Logf("Hello World → IDs(%d): %v", len(result.PhonemeIDs), result.PhonemeIDs)
}

// TestEnglishG2P_Synthesize generates actual audio samples for mixed text.
// Requires the model file to be present.
func TestEnglishG2P_Synthesize(t *testing.T) {
	modelPath := filepath.Join("testdata", "piper-xiao_ya-zh-fp32.gguf")
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skip("model not found")
	}
	lexPath := filepath.Join("testdata", "vits-piper-zh_CN-xiao_ya-medium", "lexicon.txt")
	model, err := NewPiper(modelPath, lexPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}

	// Load duration helpers
	cacheDir := "testdata"
	durCache, err := LoadDurationCache(
		filepath.Join(cacheDir, "duration_trigram_cache.json"),
		filepath.Join(cacheDir, "duration_bigram_cache.json"),
		filepath.Join(cacheDir, "duration_unigram_cache.json"),
	)
	if err == nil {
		model.DurCache = durCache
	}
	mlpPath := filepath.Join(cacheDir, "duration_mlp.bin")
	w1, b1, w2, b2, err := LoadDurationMLP(mlpPath)
	if err == nil {
		model.DurMLPW1 = w1
		model.DurMLPB1 = b1
		model.DurMLPW2 = w2
		model.DurMLPB2 = b2
	}

	texts := []string{
		"MacLaw智能助手",
		"使用Python开发",
		"Hello你好世界",
		"2024年新年快乐",
		"WiFi密码是123",
		"请打开GitHub查看代码",
		"OK没问题",
	}

	for _, text := range texts {
		g2p := PiperTextToPhonemesWithLexicon(text, model.Lex)
		t.Logf("\n%s → IDs(%d): %v", text, len(g2p.PhonemeIDs), g2p.PhonemeIDs)

		t1 := time.Now()
		audio, err := model.Synthesize(PiperSynthesizeInput{
			PhonemeIDs: g2p.PhonemeIDs,
		})
		elapsed := time.Since(t1)
		if err != nil {
			t.Errorf("%s: synthesize error: %v", text, err)
			continue
		}

		dur := float64(len(audio)) / float64(model.HP.SampleRate)
		t.Logf("  samples=%d dur=%.2fs elapsed=%v RTF=%.3f", len(audio), dur, elapsed, elapsed.Seconds()/dur)

		wav := EncodeWAV(audio, model.HP.SampleRate)
		safeName := text
		if len([]rune(safeName)) > 20 {
			safeName = string([]rune(safeName)[:20])
		}
		wavPath := filepath.Join("testdata", fmt.Sprintf("go_piper_mixed_%s.wav", safeName))
		os.WriteFile(wavPath, wav, 0644)
		t.Logf("  → %s", wavPath)
	}
}
