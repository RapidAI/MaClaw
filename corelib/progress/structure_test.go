package progress

import (
	"testing"
)

func TestDetectNegation_Chinese(t *testing.T) {
	positives := []struct {
		text string
		note string
	}{
		{"算了", "direct cancel"},
		{"不做了", "negation + 了"},
		{"别搞了", "别 + 了"},
		{"没必要了", "没 + 了"},
		{"先不弄了吧", "先不 prefix + 了吧"},
		{"暂时不需要了", "暂时不 prefix"},
		{"还是不做了", "还是不 prefix"},
		{"不要了", "不要 prefix"},
		{"不用了", "不用 prefix"},
		{"别再试了", "别再 prefix"},
		{"取消吧", "取消 prefix"},
		{"停止", "停止 prefix"},
		{"放弃", "放弃 prefix"},
		{"算了不做了", "compound negation"},
	}

	for _, tt := range positives {
		if !DetectNegation(tt.text) {
			t.Errorf("expected negation for %q (%s)", tt.text, tt.note)
		}
	}
}

func TestDetectNegation_English(t *testing.T) {
	positives := []string{
		"stop", "cancel", "abort",
		"never mind", "nevermind",
		"forget it", "don't do it",
		"no longer needed",
	}

	for _, text := range positives {
		if !DetectNegation(text) {
			t.Errorf("expected negation for %q", text)
		}
	}
}

func TestDetectNegation_FalseNegatives(t *testing.T) {
	// These should NOT be detected as negation.
	negatives := []struct {
		text string
		note string
	}{
		{"帮我查天气", "normal request"},
		{"颜色改成红色", "supplement"},
		{"开发一个游戏", "coding task"},
		{"做到哪了", "status query"},
		{"继续", "continuation"},
		{"好的", "confirmation"},
		{"不错", "positive — 不错 means 'not bad'"},
		{"用C++不要Python", "modification, not cancel — but has 不要"},
	}

	for _, tt := range negatives {
		// "用C++不要Python" will match 不要 prefix — this is expected.
		// The scheduler uses relevance to distinguish modification from cancel.
		if tt.text == "用C++不要Python" {
			// This is a known edge case: negation is detected, but the
			// scheduler's high-relevance signal overrides it to Merge.
			if !DetectNegation(tt.text) {
				t.Errorf("expected negation detection for %q (edge case)", tt.text)
			}
			continue
		}

		if DetectNegation(tt.text) {
			t.Errorf("unexpected negation for %q (%s)", tt.text, tt.note)
		}
	}
}

func TestAnalyzeStructure(t *testing.T) {
	tests := []struct {
		text      string
		wantShort bool
		wantLong  bool
		wantNeg   bool
	}{
		{"停", true, false, true},
		{"？", true, false, false},
		{"帮我查下杭州天气", false, false, false},
		{"这是一段很长很长很长很长很长很长很长很长很长很长很长很长很长的消息", false, true, false},
		{"算了不做了", false, false, true},
	}

	for _, tt := range tests {
		s := AnalyzeStructure(tt.text)
		if s.IsShort != tt.wantShort {
			t.Errorf("%q: IsShort=%v, want %v", tt.text, s.IsShort, tt.wantShort)
		}
		if s.IsLong != tt.wantLong {
			t.Errorf("%q: IsLong=%v, want %v", tt.text, s.IsLong, tt.wantLong)
		}
		if s.HasNegation != tt.wantNeg {
			t.Errorf("%q: HasNegation=%v, want %v", tt.text, s.HasNegation, tt.wantNeg)
		}
	}
}
