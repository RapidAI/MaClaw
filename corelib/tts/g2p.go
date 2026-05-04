package tts

import (
	"strings"
	"unicode"
)

// Language IDs used by MeloTTS.
const (
	LangZH = 0
	LangJA = 1
	LangEN = 2
	LangKO = 3
	LangFR = 4
	LangES = 5
	LangDE = 6
	LangRU = 7
)

// G2PResult holds the output of grapheme-to-phoneme conversion.
type G2PResult struct {
	PhonemeIDs []int
	ToneIDs    []int
	LangIDs    []int
}

// TextToPhonemes converts text to phoneme IDs for MeloTTS.
// Automatically detects language (Chinese vs English) per character.
// Inserts blank tokens between phonemes (VITS add_blank=true).
func TextToPhonemes(text string, pt *PhonemeTable, langID int) G2PResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return G2PResult{}
	}

	var phonemes []string
	var tones []int
	var wordBounds []bool // true at the last phoneme of each character/word

	// Simple approach: process character by character, grouping by language
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		r := runes[i]

		if isChinese(r) {
			// Chinese character → pinyin → phonemes
			py := charToPinyinInText(runes, i)
			if py != "" {
				phs, tone := pinyinToPhonemes(py)
				for pi, ph := range phs {
					phonemes = append(phonemes, ph)
					tones = append(tones, tone)
					wordBounds = append(wordBounds, pi == len(phs)-1) // last phoneme of this char
				}
			}
			i++
		} else if unicode.IsLetter(r) {
			// English word: collect consecutive letters
			j := i
			for j < len(runes) && (unicode.IsLetter(runes[j]) || runes[j] == '\'') {
				j++
			}
			word := strings.ToLower(string(runes[i:j]))
			phs := englishWordToPhonemes(word)
			for pi, ph := range phs {
				phonemes = append(phonemes, ph)
				tones = append(tones, 0)                          // English has no lexical tone
				wordBounds = append(wordBounds, pi == len(phs)-1) // last phoneme of this word
			}
			i = j
		} else if isPunctuation(r) {
			// Map punctuation to phoneme
			ph := punctToPhoneme(r)
			if ph != "" {
				phonemes = append(phonemes, ph)
				tones = append(tones, 0)
				wordBounds = append(wordBounds, true)
			}
			i++
		} else {
			// Skip whitespace and unknown characters
			i++
		}
	}

	if len(phonemes) == 0 {
		return G2PResult{}
	}

	// MeloTTS add_blank: only at sentence boundaries (start and end)
	var ids []int
	var toneIDsFinal []int
	var langIDsFinal []int

	// Leading blank
	ids = append(ids, BlankID)
	toneIDsFinal = append(toneIDsFinal, 0)
	langIDsFinal = append(langIDsFinal, 0)

	// All phonemes without intermediate blanks
	for i := 0; i < len(phonemes); i++ {
		ids = append(ids, pt.ID(phonemes[i]))
		toneIDsFinal = append(toneIDsFinal, tones[i])
		if i < len(wordBounds) && !wordBounds[i] {
			// Non-boundary phonemes get the language ID
			langIDsFinal = append(langIDsFinal, langID)
		} else {
			langIDsFinal = append(langIDsFinal, langID)
		}
	}

	// Trailing blank
	ids = append(ids, BlankID)
	toneIDsFinal = append(toneIDsFinal, 0)
	langIDsFinal = append(langIDsFinal, 0)

	return G2PResult{
		PhonemeIDs: ids,
		ToneIDs:    toneIDsFinal,
		LangIDs:    langIDsFinal,
	}
}

// DetectLanguage returns the primary language ID for the text.
func DetectLanguage(text string) int {
	zhCount, enCount := 0, 0
	for _, r := range text {
		if isChinese(r) {
			zhCount++
		} else if unicode.IsLetter(r) {
			enCount++
		}
	}
	if zhCount > enCount {
		return LangZH
	}
	return LangEN
}

func isChinese(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0xF900 && r <= 0xFAFF) // CJK Compatibility Ideographs
}

func isPunctuation(r rune) bool {
	switch r {
	case ',', '.', '!', '?', '…', '，', '。', '！', '？', '、':
		return true
	}
	return false
}

func punctToPhoneme(r rune) string {
	switch r {
	case ',', '，', '、':
		return ","
	case '.', '。':
		return "."
	case '!', '！':
		return "!"
	case '?', '？':
		return "?"
	case '…':
		return "…"
	}
	return ""
}
