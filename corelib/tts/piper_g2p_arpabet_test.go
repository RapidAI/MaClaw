package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArpabetToPinyin(t *testing.T) {
	tests := []struct {
		arpa []string
		desc string
	}{
		{[]string{"P", "ER0", "F", "AO1", "R", "M", "AH0", "N", "S"}, "performance"},
		{[]string{"K", "UW1", "B", "ER0", "N", "EH1", "T", "IY0", "Z"}, "kubernetes"},
		{[]string{"T", "EH1", "N", "S", "ER0", "F", "L", "OW1"}, "tensorflow"},
		{[]string{"HH", "AH0", "L", "OW1"}, "hello"},
		{[]string{"W", "ER1", "L", "D"}, "world"},
		{[]string{"D", "EY1", "T", "AH0", "B", "EY2", "S"}, "database"},
		{[]string{"S", "ER1", "V", "ER0"}, "server"},
		{[]string{"F", "AH1", "NG", "K", "SH", "AH0", "N"}, "function"},
	}
	for _, tc := range tests {
		phones := arpabetToPinyinPhones(tc.arpa)
		t.Logf("%-15s ARPAbet=%v → pinyin=%v", tc.desc, tc.arpa, phones)
		if len(phones) == 0 {
			t.Errorf("%s: no pinyin output", tc.desc)
		}
		// Verify all phones are valid in the phoneme map
		for _, ph := range phones {
			if _, ok := piperXiaoYaPhonemeMap[ph]; !ok {
				t.Errorf("%s: unknown phoneme %q", tc.desc, ph)
			}
		}
	}
}

func TestLoadCMUDict(t *testing.T) {
	// Try plain text first, then gzipped
	dictPath := filepath.Join("testdata", "cmudict.dict")
	if _, err := os.Stat(dictPath); os.IsNotExist(err) {
		dictPath = filepath.Join("testdata", "cmudict.dict.gz")
		if _, err := os.Stat(dictPath); os.IsNotExist(err) {
			t.Skip("CMU dictionary not found")
		}
	}

	dict, err := LoadCMUDict(dictPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Logf("CMU dictionary loaded: %d entries", dict.Size())

	// Test some lookups
	words := []string{"performance", "hello", "world", "database", "server", "function", "install", "timeout", "overflow"}
	for _, w := range words {
		arpa := dict.Lookup(w)
		pinyin := dict.LookupPinyin(w)
		t.Logf("%-15s ARPAbet=%-30s pinyin=%v", w, strings.Join(arpa, " "), pinyin)
		if arpa == nil {
			t.Errorf("%s: not found in CMU dict", w)
		}
	}
	// kubernetes is a brand name, not in CMU dict — falls through to word table
	if arpa := dict.Lookup("kubernetes"); arpa != nil {
		t.Logf("kubernetes found in CMU dict (unexpected but ok)")
	} else {
		t.Logf("kubernetes not in CMU dict (expected, uses word table)")
	}
}

func TestCMUDict_Synthesize(t *testing.T) {
	modelPath := filepath.Join("testdata", "piper-xiao_ya-zh-fp32.gguf")
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skip("model not found")
	}
	dictPath := filepath.Join("testdata", "cmudict.dict")
	if _, err := os.Stat(dictPath); os.IsNotExist(err) {
		dictPath = filepath.Join("testdata", "cmudict.dict.gz")
		if _, err := os.Stat(dictPath); os.IsNotExist(err) {
			t.Skip("CMU dictionary not found")
		}
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

	// Load CMU dictionary
	cmuDict, err := LoadCMUDict(dictPath)
	if err != nil {
		t.Fatalf("load CMU dict: %v", err)
	}
	model.CMUDict = cmuDict
	t.Logf("CMU dictionary: %d entries", cmuDict.Size())

	texts := []string{
		"performance优化建议",
		"正在install依赖",
		"kubernetes集群部署完成",
		"发现了一个timeout错误",
		"请检查database连接",
		"TensorFlow训练完成",
		"这个function有bug",
		"Hello你好世界",
		"application启动成功",
	}

	for _, text := range texts {
		g2p := PiperTextToPhonemesWithDict(text, model.Lex, model.CMUDict)
		t.Logf("\n%s → IDs(%d) wordInternalSeps=%v", text, len(g2p.PhonemeIDs), g2p.WordInternalSeps)

		t1 := time.Now()
		audio, err := model.Synthesize(PiperSynthesizeInput{
			PhonemeIDs:       g2p.PhonemeIDs,
			WordInternalSeps: g2p.WordInternalSeps,
		})
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
		wavPath := filepath.Join("testdata", fmt.Sprintf("go_piper_cmu_%s.wav", safeName))
		os.WriteFile(wavPath, wav, 0644)
		t.Logf("  → %s", wavPath)
	}
}
