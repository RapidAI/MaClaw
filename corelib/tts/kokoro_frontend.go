package tts

import (
	"strings"
	"unicode"
)

// KokoroTextToPhonemes converts ordinary text to the phoneme string expected by
// Kokoro. Mandarin uses the existing pinyin table; Latin words use the English
// frontend so an English Kokoro voice receives phonemes rather than spelling.
func KokoroTextToPhonemes(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case isChinese(r):
			if syl := kokoroChineseRuneToPhonemesInText(runes, i); syl != "" {
				b.WriteString(syl)
			}
			i++
		case unicode.IsLetter(r):
			j := i + 1
			for j < len(runes) && (unicode.IsLetter(runes[j]) || runes[j] == '\'') {
				j++
			}
			word := string(runes[i:j])
			if b.Len() > 0 {
				b.WriteRune(' ')
			}
			b.WriteString(kokoroEnglishWordToPhonemes(word))
			i = j
		case unicode.IsDigit(r):
			if b.Len() > 0 {
				b.WriteRune(' ')
			}
			b.WriteString(kokoroDigitToPhonemes(r))
			i++
		case kokoroIsPunctuation(r):
			b.WriteString(kokoroPunctuation(r))
			b.WriteRune(' ')
			i++
		default:
			if unicode.IsSpace(r) && b.Len() > 0 {
				b.WriteRune(' ')
			}
			i++
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// kokoroEnglishWordToPhonemes applies brand aliases before the general English
// G2P. The G2P fallback covers arbitrary words; aliases are only for names whose
// intended pronunciation cannot be inferred reliably from spelling.
func kokoroEnglishWordToPhonemes(word string) string {
	switch strings.ToLower(word) {
	case "maclaw":
		// MaClaw: "Ma" + "Claw", not the visually similar surname McClaw.
		return "mɑː klɔː"
	default:
		phs := englishWordToPhonemes(word)
		if len(phs) == 0 {
			return word
		}
		return strings.Join(phs, "")
	}
}

func kokoroChineseRuneToPhonemes(r rune) string {
	return kokoroPinyinToPhonemes(charToPinyin(r))
}

func kokoroChineseRuneToPhonemesInText(runes []rune, i int) string {
	return kokoroPinyinToPhonemes(charToPinyinInText(runes, i))
}

func kokoroPinyinToPhonemes(py string) string {
	if py == "" {
		return ""
	}
	tone := byte('5')
	if last := py[len(py)-1]; last >= '1' && last <= '5' {
		tone = last
		py = py[:len(py)-1]
	}
	initial, final := splitPinyin(py)
	phonemes := kokoroInitial(initial, final) + kokoroFinal(initial, final, tone)
	return kokoroApplyTone(phonemes, kokoroTone(tone))
}

func kokoroApplyTone(phonemes, tone string) string {
	if tone == "" || phonemes == "" {
		return phonemes
	}
	runes := []rune(phonemes)
	last := runes[len(runes)-1]
	if last == 'n' || last == '\u014b' {
		return string(runes[:len(runes)-1]) + tone + string(last)
	}
	return phonemes + tone
}
func kokoroInitial(initial, final string) string {
	if initial == "y" {
		switch final {
		case "i", "in", "ing", "ue", "uan":
			return ""
		case "ao", "ou", "an", "ang", "ong", "a", "e":
			return "j"
		}
	}
	if initial == "w" && final == "u" {
		return ""
	}
	switch initial {
	case "m", "f", "n", "l", "s", "w", "y":
		return initial
	case "b":
		return "p"
	case "p":
		return "p\u02b0"
	case "d":
		return "t"
	case "t":
		return "t\u02b0"
	case "g":
		return "k"
	case "k":
		return "k\u02b0"
	case "h":
		return "x"
	case "j":
		return "\u02a8"
	case "q":
		return "\u02a8\u02b0"
	case "x":
		return "\u0255"
	case "zh":
		return "\uAB67"
	case "ch":
		return "\uAB67\u02b0"
	case "sh":
		return "\u0282"
	case "r":
		return "\u027b"
	case "z":
		return "\u02a6"
	case "c":
		return "\u02a6\u02b0"
	}
	return ""
}

func kokoroFinal(initial, final string, tone byte) string {
	if final == "ir" || final == "i0" || ((initial == "zh" || initial == "ch" || initial == "sh" || initial == "r" || initial == "z" || initial == "c" || initial == "s") && final == "i") {
		return "\u0268"
	}
	switch final {
	case "a":
		return "a"
	case "o":
		return "o"
	case "e":
		return "\u0264"
	case "i":
		return "i"
	case "u":
		if initial == "y" {
			return ""
		}
		return "u"
	case "v":
		return "y"
	case "ve", "ue":
		return "\u0265e"
	case "ai":
		return "ai"
	case "ei":
		return "ei"
	case "ao":
		return "au"
	case "ou":
		return "ou"
	case "an":
		return "an"
	case "en":
		return "\u0259n"
	case "ang":
		return "a\u014b"
	case "eng":
		return "\u0259\u014b"
	case "ong":
		return "\u028a\u014b"
	case "er":
		return "\u025a"
	case "ia":
		return "ia"
	case "ie":
		return "i\u025b"
	case "iao":
		return "iau"
	case "iu":
		return "iou"
	case "ian":
		return "i\u025bn"
	case "in":
		return "in"
	case "iang":
		return "ia\u014b"
	case "ing":
		return "i\u014b"
	case "iong":
		return "io\u014b"
	case "ua":
		return "ua"
	case "uo":
		return "uo"
	case "uai":
		return "uai"
	case "ui":
		return "uei"
	case "uan":
		if initial == "y" {
			return "\u0265\u025bn"
		}
		return "uan"
	case "un":
		if initial == "y" {
			return "n"
		}
		return "u\u0259n"
	case "uang":
		return "ua\u014b"
	case "van":
		return "y\u025bn"
	case "vn":
		return "yn"
	}
	return final
}

func kokoroTone(tone byte) string {
	switch tone {
	case '1':
		return "\u2192"
	case '2':
		return "\u2197"
	case '3':
		return "\u2193"
	case '4':
		return "\u2198"
	}
	return ""
}

func kokoroDigitToPhonemes(r rune) string {
	switch r {
	case '0':
		return kokoroChineseRuneToPhonemes('\u96f6')
	case '1':
		return kokoroChineseRuneToPhonemes('\u4e00')
	case '2':
		return kokoroChineseRuneToPhonemes('\u4e8c')
	case '3':
		return kokoroChineseRuneToPhonemes('\u4e09')
	case '4':
		return kokoroChineseRuneToPhonemes('\u56db')
	case '5':
		return kokoroChineseRuneToPhonemes('\u4e94')
	case '6':
		return kokoroChineseRuneToPhonemes('\u516d')
	case '7':
		return kokoroChineseRuneToPhonemes('\u4e03')
	case '8':
		return kokoroChineseRuneToPhonemes('\u516b')
	case '9':
		return kokoroChineseRuneToPhonemes('\u4e5d')
	}
	return ""
}

func kokoroIsPunctuation(r rune) bool {
	switch r {
	case ',', '.', '!', '?', ':', ';', '\u3002', '\uff0c', '\uff1a', '\uff1b', '\uff01', '\uff1f':
		return true
	}
	return false
}

func kokoroPunctuation(r rune) string {
	switch r {
	case '.', '\u3002':
		return "."
	case '!', '\uff01':
		return "!"
	case '?', '\uff1f':
		return "?"
	case ':', '\uff1a':
		return ":"
	case ';', '\uff1b':
		return ";"
	}
	return ","
}
