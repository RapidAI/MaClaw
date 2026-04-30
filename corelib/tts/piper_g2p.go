package tts

import "strings"

// PiperG2PResult holds phoneme IDs for the Piper xiao_ya model.
type PiperG2PResult struct {
	PhonemeIDs []int64
	// WordInternalSeps records indices in PhonemeIDs where "_" separators
	// are word-internal (English word syllable boundaries).
	// These separators should have shorter duration for connected speech.
	WordInternalSeps []int
}

// piperXiaoYaPhonemeMap maps pinyin initials/finals/tones/punctuation to xiao_ya phoneme IDs.
// Source: zh_CN-xiao_ya-medium.onnx.json phoneme_id_map
var piperXiaoYaPhonemeMap = map[string]int{
	"_": 0, "^": 1, "$": 2, "Ø": 3,
	// Initials
	"b": 4, "p": 5, "m": 6, "f": 7,
	"d": 8, "t": 9, "n": 10, "l": 11,
	"g": 12, "k": 13, "h": 14,
	"j": 15, "q": 16, "x": 17,
	"zh": 18, "ch": 19, "sh": 20, "r": 21,
	"z": 22, "c": 23, "s": 24,
	"y": 25, "w": 26,
	// Finals
	"a": 27, "o": 28, "e": 29,
	"ai": 30, "ei": 31, "ao": 32, "ou": 33,
	"an": 34, "en": 35, "ang": 36, "eng": 37, "ong": 38,
	"i": 39, "ia": 40, "ie": 41, "iao": 42, "iu": 43,
	"ian": 44, "in": 45, "iang": 46, "ing": 47, "iong": 48,
	"u": 49, "ua": 50, "uo": 51, "uai": 52, "ui": 53,
	"uan": 54, "un": 55, "uang": 56, "ueng": 57,
	"v": 58, "ve": 59, "van": 60, "vn": 61,
	"er": 62, "ue": 63,
	// Tones
	"1": 64, "2": 65, "3": 66, "4": 67, "5": 68,
	// Punctuation
	"。": 69, ".": 69,
	"？": 70, "?": 70,
	"！": 71, "!": 71,
	"—": 72, "…": 72, "、": 72, "，": 72, ",": 72,
	"：": 72, ":": 72, "；": 72, ";": 72,
}

// PiperTextToPhonemes converts Chinese text to xiao_ya phoneme IDs.
// Uses the lexicon if available (for correct tone sandhi), falls back to G2P.
func PiperTextToPhonemes(text string) PiperG2PResult {
	return PiperTextToPhonemesWithLexicon(text, nil)
}

// charPhoneInfo holds per-character phoneme info for tone sandhi processing.
type charPhoneInfo struct {
	r           rune
	phones      []string // [initial, final, tone] from lexicon
	isCJK       bool
	noSepBefore bool // if true, don't insert _ separator before this entry (for English word-internal syllables)
}

// PiperTextToPhonemesWithLexicon converts text using the lexicon for accurate pronunciation.
// Supports Chinese text, English words (converted to Chinese phonetic approximations),
// digits (read as Chinese numbers), and punctuation.
func PiperTextToPhonemesWithLexicon(text string, lex *PiperLexicon) PiperG2PResult {
	return piperTextToPhonemes(text, lex, nil)
}

// PiperTextToPhonemesWithDict converts text using both Chinese lexicon and English CMU dictionary.
func PiperTextToPhonemesWithDict(text string, lex *PiperLexicon, cmuDict *CMUDict) PiperG2PResult {
	return piperTextToPhonemes(text, lex, cmuDict)
}

func piperTextToPhonemes(text string, lex *PiperLexicon, cmuDict *CMUDict) PiperG2PResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return PiperG2PResult{}
	}

	// First pass: segment text into Chinese chars, English words, digits, and punctuation.
	// English letters are grouped into words; digits are individual.
	runes := []rune(text)
	var chars []charPhoneInfo

	i := 0
	for i < len(runes) {
		r := runes[i]

		if isChinese(r) {
			ci := charPhoneInfo{r: r, isCJK: true}
			phones := lex.Lookup(r)
			if phones != nil {
				ci.phones = phones
			} else {
				py := charToPinyin(r)
				if py != "" {
					tone := "5"
					if len(py) > 0 && py[len(py)-1] >= '1' && py[len(py)-1] <= '5' {
						tone = string(py[len(py)-1])
						py = py[:len(py)-1]
					}
					initial, final := splitPinyinForPiper(py)
					if initial != "" {
						ci.phones = append(ci.phones, initial)
					}
					if final != "" {
						ci.phones = append(ci.phones, final)
					}
					ci.phones = append(ci.phones, tone)
				}
			}
			chars = append(chars, ci)
			i++
		} else if isEnglishLetter(r) {
			// Collect the full English word
			j := i + 1
			for j < len(runes) && isEnglishLetter(runes[j]) {
				j++
			}
			word := string(runes[i:j])
			wordPhones := englishWordToPhonesWithDict(word, cmuDict)
			chars = append(chars, wordPhones...)
			i = j
		} else if isDigit(r) {
			chars = append(chars, digitToPhones(r))
			i++
		} else if isPunctuation(r) {
			chars = append(chars, charPhoneInfo{r: r})
			i++
		} else {
			// Skip whitespace and other characters
			i++
		}
	}

	// Second pass: apply tone sandhi rules
	applyToneSandhi(chars)

	// Third pass: convert to phoneme IDs
	var ids []int64
	var wordInternalSeps []int
	ids = append(ids, int64(piperXiaoYaPhonemeMap["^"]))

	prevWasChinese := false
	for _, ci := range chars {
		if ci.isCJK && len(ci.phones) > 0 {
			if prevWasChinese {
				sepIdx := len(ids)
				ids = append(ids, int64(piperXiaoYaPhonemeMap["_"]))
				if ci.noSepBefore {
					wordInternalSeps = append(wordInternalSeps, sepIdx)
				}
			}
			for _, ph := range ci.phones {
				if id, ok := piperXiaoYaPhonemeMap[ph]; ok {
					ids = append(ids, int64(id))
				}
			}
			prevWasChinese = true
		} else if isPunctuation(ci.r) {
			ph := punctToPhoneme(ci.r)
			if ph != "" {
				if id, ok := piperXiaoYaPhonemeMap[ph]; ok {
					ids = append(ids, int64(id))
				}
			}
			prevWasChinese = false
		} else {
			prevWasChinese = false
		}
	}

	ids = append(ids, int64(piperXiaoYaPhonemeMap["$"]))
	return PiperG2PResult{PhonemeIDs: ids, WordInternalSeps: wordInternalSeps}
}

// applyToneSandhi applies Chinese tone sandhi rules.
func applyToneSandhi(chars []charPhoneInfo) {
	for i := range chars {
		if !chars[i].isCJK || len(chars[i].phones) == 0 {
			continue
		}
		tone := chars[i].phones[len(chars[i].phones)-1]

		// Find next Chinese character's tone
		nextTone := ""
		for j := i + 1; j < len(chars); j++ {
			if chars[j].isCJK && len(chars[j].phones) > 0 {
				nextTone = chars[j].phones[len(chars[j].phones)-1]
				break
			}
		}

		// Rule 1: 不 before tone 4 → tone 2
		if chars[i].r == '不' && nextTone == "4" {
			chars[i].phones[len(chars[i].phones)-1] = "2"
		}

		// Rule 2: 一 tone sandhi
		if chars[i].r == '一' {
			if nextTone == "4" {
				// 一 before tone 4 → tone 2
				chars[i].phones[len(chars[i].phones)-1] = "2"
			} else if nextTone == "1" || nextTone == "2" || nextTone == "3" {
				// 一 before tone 1/2/3 → tone 4
				chars[i].phones[len(chars[i].phones)-1] = "4"
			}
		}

		// Rule 3: Third tone sandhi (两个三声连读，前一个变二声)
		if tone == "3" && nextTone == "3" {
			chars[i].phones[len(chars[i].phones)-1] = "2"
		}
	}
}

// splitPinyinForPiper splits pinyin into initial and final for xiao_ya model.
// Unlike MeloTTS, xiao_ya uses the standard pinyin finals directly (no "ir"/"i0" special cases).
func splitPinyinForPiper(py string) (initial, final string) {
	py = strings.ToLower(py)

	// Two-character initials
	if len(py) >= 2 {
		prefix2 := py[:2]
		switch prefix2 {
		case "zh", "ch", "sh":
			return prefix2, mapPiperFinal(py[2:])
		}
	}

	// Single-character initials
	if len(py) >= 1 {
		c := py[0]
		switch c {
		case 'b', 'p', 'm', 'f', 'd', 't', 'n', 'l',
			'g', 'k', 'h', 'j', 'q', 'x',
			'w', 'y', 'r', 'z', 'c', 's':
			return string(c), mapPiperFinal(py[1:])
		}
	}

	// No initial (zero initial) — the final IS the syllable
	return "", mapPiperFinal(py)
}

// mapPiperFinal maps a raw final to the xiao_ya phoneme set.
// Handles special cases like ü → v.
func mapPiperFinal(f string) string {
	if f == "" {
		return ""
	}
	// Map lü → lv, nü → nv etc.
	f = strings.ReplaceAll(f, "ü", "v")

	// Check if it's a known final
	if _, ok := piperXiaoYaPhonemeMap[f]; ok {
		return f
	}

	// Some finals need mapping
	switch f {
	case "ue":
		return "ve" // jue/que/xue → j+ve, q+ve, x+ve (but "ue" is also valid ID 63)
	}

	return f
}
