package tts

import (
	"testing"
)

func TestPhonemeTable(t *testing.T) {
	pt := NewPhonemeTable()
	if pt.Size() == 0 {
		t.Fatal("empty phoneme table")
	}
	// Check some known phonemes
	if pt.ID("_") != 0 {
		t.Errorf("blank ID = %d, want 0", pt.ID("_"))
	}
	if pt.ID("a") == 0 {
		t.Error("'a' should not be 0")
	}
	if pt.ID("zh") == 0 {
		t.Error("'zh' should not be 0")
	}
	if pt.ID("SP") == 0 {
		t.Error("'SP' should not be 0")
	}
}

func TestTextToPhonemes_Chinese(t *testing.T) {
	pt := NewPhonemeTable()
	result := TextToPhonemes("你好", pt, LangZH)
	if len(result.PhonemeIDs) == 0 {
		t.Fatal("empty phoneme IDs for '你好'")
	}
	// Should have blanks interleaved
	// 你 → n, i3; 好 → h, ao3 → 4 phonemes → 4*2+1 = 9 with blanks
	t.Logf("你好 → %d phoneme IDs", len(result.PhonemeIDs))
	if len(result.PhonemeIDs) < 5 {
		t.Errorf("too few phonemes: %d", len(result.PhonemeIDs))
	}
	// First and last should be blank
	if result.PhonemeIDs[0] != BlankID {
		t.Errorf("first ID should be blank, got %d", result.PhonemeIDs[0])
	}
	if result.PhonemeIDs[len(result.PhonemeIDs)-1] != BlankID {
		t.Errorf("last ID should be blank, got %d", result.PhonemeIDs[len(result.PhonemeIDs)-1])
	}
}

func TestTextToPhonemes_English(t *testing.T) {
	pt := NewPhonemeTable()
	result := TextToPhonemes("Hello World", pt, LangEN)
	if len(result.PhonemeIDs) == 0 {
		t.Fatal("empty phoneme IDs for 'Hello World'")
	}
	t.Logf("Hello World → %d phoneme IDs", len(result.PhonemeIDs))
	if len(result.PhonemeIDs) < 5 {
		t.Errorf("too few phonemes: %d", len(result.PhonemeIDs))
	}
}

func TestTextToPhonemes_Mixed(t *testing.T) {
	pt := NewPhonemeTable()
	result := TextToPhonemes("你好World", pt, LangZH)
	if len(result.PhonemeIDs) == 0 {
		t.Fatal("empty phoneme IDs for mixed text")
	}
	t.Logf("你好World → %d phoneme IDs", len(result.PhonemeIDs))
}

func TestDetectLanguage(t *testing.T) {
	if DetectLanguage("你好世界") != LangZH {
		t.Error("should detect Chinese")
	}
	if DetectLanguage("Hello World") != LangEN {
		t.Error("should detect English")
	}
	if DetectLanguage("你好Hello") != LangEN {
		t.Error("mixed with more English letters should detect English")
	}
}

func TestPinyinToPhonemes(t *testing.T) {
	tests := []struct {
		pinyin string
		wantN  int // minimum number of phonemes
		tone   int
	}{
		{"ni3", 1, 3},
		{"hao3", 1, 3},
		{"zhong1", 2, 1}, // zh + ong
		{"shi4", 1, 4},
		{"de5", 1, 5},
	}
	for _, tt := range tests {
		phs, tone := pinyinToPhonemes(tt.pinyin)
		if len(phs) < tt.wantN {
			t.Errorf("pinyinToPhonemes(%q) = %v, want >= %d phonemes", tt.pinyin, phs, tt.wantN)
		}
		if tone != tt.tone {
			t.Errorf("pinyinToPhonemes(%q) tone = %d, want %d", tt.pinyin, tone, tt.tone)
		}
	}
}
