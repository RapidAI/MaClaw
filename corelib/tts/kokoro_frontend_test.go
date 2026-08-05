package tts

import (
	"strings"
	"testing"
)

func TestKokoroMandarinAspiratedInitials(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		want    string
		notWant string
	}{
		{name: "bao uses unaspirated p", text: "\u62a5", want: "p", notWant: "p\u02b0"},
		{name: "pao uses aspirated p", text: "\u8dd1", want: "p\u02b0"},
		{name: "dao uses unaspirated t", text: "\u5230", want: "t", notWant: "t\u02b0"},
		{name: "tao uses aspirated t", text: "\u5957", want: "t\u02b0"},
		{name: "guo uses unaspirated k", text: "\u8fc7", want: "k", notWant: "k\u02b0"},
		{name: "ke uses aspirated k", text: "\u8bfe", want: "k\u02b0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KokoroTextToPhonemes(tt.text)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("KokoroTextToPhonemes(%q) = %q, want substring %q", tt.text, got, tt.want)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Fatalf("KokoroTextToPhonemes(%q) = %q, should not contain %q", tt.text, got, tt.notWant)
			}
		})
	}
}

func TestKokoroMandarinJQXUmlautFinals(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		want    string
		notWant string
	}{
		{name: "quan uses yuan-like final", text: "\u5168", want: "\u02a8\u02b0y\u025b", notWant: "\u02a8\u02b0ua"},
		{name: "xuan uses yuan-like final", text: "\u9009", want: "\u0255y\u025b", notWant: "\u0255ua"},
		{name: "jue keeps rounded front vowel", text: "\u89c9", want: "\u02a8\u0265e", notWant: "\u02a8ue"},
		{name: "qu keeps rounded front vowel", text: "\u53bb", want: "\u02a8\u02b0y", notWant: "\u02a8\u02b0u"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KokoroTextToPhonemes(tt.text)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("KokoroTextToPhonemes(%q) = %q, want substring %q", tt.text, got, tt.want)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Fatalf("KokoroTextToPhonemes(%q) = %q, should not contain %q", tt.text, got, tt.notWant)
			}
		})
	}
}

func TestPinyinToPhonemesJQXUmlautFinals(t *testing.T) {
	tests := []struct {
		pinyin string
		want   []string
	}{
		{pinyin: "quan2", want: []string{"q", "van"}},
		{pinyin: "xuan3", want: []string{"x", "van"}},
		{pinyin: "jue2", want: []string{"j", "ve"}},
		{pinyin: "qu4", want: []string{"q", "v"}},
	}

	for _, tt := range tests {
		t.Run(tt.pinyin, func(t *testing.T) {
			got, _ := pinyinToPhonemes(tt.pinyin)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("pinyinToPhonemes(%q) = %v, want %v", tt.pinyin, got, tt.want)
			}
		})
	}
}

func TestContextualPinyinOverrides(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
		not  string
	}{
		{name: "sleep uses jiao", text: "\u7761\u89c9", want: "iau\u2198", not: "\u0265e\u2197"},
		{name: "jue remains jue", text: "\u89c9\u5f97", want: "\u0265e\u2197", not: "iau\u2198"},
		{name: "music uses yue", text: "\u97f3\u4e50", want: "\u0265e\u2198"},
		{name: "bank uses hang", text: "\u94f6\u884c", want: "xa\u2197\u014b"},
		{name: "chongqing uses chong", text: "\u91cd\u5e86", want: "\uaB67\u02b0\u028a\u2197\u014b"},
		{name: "grow uses zhang", text: "\u957f\u5927", want: "\uaB67a\u2193\u014b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KokoroTextToPhonemes(tt.text)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("KokoroTextToPhonemes(%q) = %q, want substring %q", tt.text, got, tt.want)
			}
			if tt.not != "" && strings.Contains(got, tt.not) {
				t.Fatalf("KokoroTextToPhonemes(%q) = %q, should not contain %q", tt.text, got, tt.not)
			}
		})
	}
}

func TestContextualPinyinInText(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		index int
		want  string
	}{
		{name: "sleep uses jiao", text: "\u7761\u89c9", index: 1, want: "jiao4"},
		{name: "jue keeps jue", text: "\u89c9\u5f97", index: 0, want: "jue2"},
		{name: "music uses yue", text: "\u97f3\u4e50", index: 1, want: "yue4"},
		{name: "bank uses hang", text: "\u94f6\u884c", index: 1, want: "hang2"},
		{name: "industry uses hang", text: "\u884c\u4e1a", index: 0, want: "hang2"},
		{name: "longest phrase wins", text: "\u6211\u8981\u7761\u89c9", index: 3, want: "jiao4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := charToPinyinInText([]rune(tt.text), tt.index)
			if got != tt.want {
				t.Fatalf("charToPinyinInText(%q, %d) = %q, want %q", tt.text, tt.index, got, tt.want)
			}
		})
	}
}

func TestPinyinRuleRejectsInvalidIndex(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("pinyinRule accepted an invalid index")
		}
	}()
	_ = pinyinRule("\u7761\u89c9", 2, "jiao4")
}

func TestKokoroMixedLanguageGreetingPronunciations(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "lowercase welcome", text: "hello maclaw", want: "hɛˈloʊ mɑː klɔː"},
		{name: "product casing and punctuation", text: "Hello, MaClaw!", want: "hɛˈloʊ, mɑː klɔː!"},
		{name: "case insensitive", text: "HELLO MACLAW", want: "hɛˈloʊ mɑː klɔː"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KokoroTextToPhonemes(tt.text); got != tt.want {
				t.Fatalf("KokoroTextToPhonemes(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestKokoroPronunciationLexiconDoesNotRewriteLongerWords(t *testing.T) {
	const input = "maclawful shelloworld"
	got := KokoroTextToPhonemes(input)
	if got == input {
		t.Fatalf("KokoroTextToPhonemes(%q) did not apply English G2P", input)
	}
	if strings.Contains(got, "mɑː klɔː") {
		t.Fatalf("KokoroTextToPhonemes(%q) = %q, longer word matched MaClaw alias", input, got)
	}
}

func TestKokoroGeneralEnglishG2P(t *testing.T) {
	got := KokoroTextToPhonemes("Hello world, this is natural English.")
	if strings.Contains(strings.ToLower(got), "hello") || strings.Contains(strings.ToLower(got), "world") {
		t.Fatalf("English spelling leaked into Kokoro phonemes: %q", got)
	}
	if !strings.HasPrefix(got, "hɛˈloʊ ") {
		t.Fatalf("English hello phonemes = %q", got)
	}
}
