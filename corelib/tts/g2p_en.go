package tts

import (
	"strings"
)

// englishWordToPhonemes converts an English word to MeloTTS phonemes.
// Uses a built-in mini lexicon + letter-based fallback.
func englishWordToPhonemes(word string) []string {
	word = strings.ToLower(word)

	// Look up in mini lexicon
	if phs, ok := enLexicon[word]; ok {
		return phs
	}

	// Fallback: simple letter-to-phoneme rules
	return letterFallback(word)
}

// letterFallback converts unknown English words to approximate phonemes.
func letterFallback(word string) []string {
	var phs []string
	runes := []rune(word)
	i := 0
	for i < len(runes) {
		r := runes[i]
		// Try two-letter combinations first
		if i+1 < len(runes) {
			pair := string(runes[i : i+2])
			if ph, ok := enDigraphs[pair]; ok {
				phs = append(phs, ph)
				i += 2
				continue
			}
		}
		// Single letter
		if ph, ok := enLetterMap[r]; ok {
			phs = append(phs, ph)
		}
		i++
	}
	return phs
}

// enDigraphs maps common English letter pairs to phonemes.
var enDigraphs = map[string]string{
	"th": "θ",
	"sh": "ʃ",
	"ch": "ch",
	"ph": "f",
	"wh": "w",
	"ng": "ŋ",
	"ck": "k",
	"qu": "k",
	"oo": "uw",
	"ee": "iy",
	"ea": "iy",
	"ai": "ey",
	"ou": "aw",
	"ow": "ow",
	"oi": "oy",
	"oy": "oy",
	"ar": "ɑ",
	"er": "ər",
	"ir": "ər",
	"or": "ɔ",
	"ur": "ər",
}

// enLetterMap maps single English letters to approximate phonemes.
var enLetterMap = map[rune]string{
	'a': "æ", 'b': "b", 'c': "k", 'd': "d", 'e': "ɛ",
	'f': "f", 'g': "ɡ", 'h': "hh", 'i': "ɪ", 'j': "jh",
	'k': "k", 'l': "l", 'm': "m", 'n': "n", 'o': "ɑ",
	'p': "p", 'q': "k", 'r': "ɹ", 's': "s", 't': "t",
	'u': "ʌ", 'v': "v", 'w': "w", 'x': "k", 'y': "iy",
	'z': "z",
}

// enLexicon is a mini English pronunciation dictionary.
// Maps common words to MeloTTS phoneme sequences.
var enLexicon = map[string][]string{
	"hello":     {"h", "ɛ", "ˈ", "l", "o", "ʊ"},
	"world":     {"w", "ər", "l", "d"},
	"the":       {"ð", "ə"},
	"a":         {"ə"},
	"is":        {"ɪ", "z"},
	"are":       {"ɑ", "ɹ"},
	"was":       {"w", "ɑ", "z"},
	"were":      {"w", "ər"},
	"i":         {"ay"},
	"you":       {"y", "uw"},
	"he":        {"hh", "iy"},
	"she":       {"ʃ", "iy"},
	"it":        {"ɪ", "t"},
	"we":        {"w", "iy"},
	"they":      {"ð", "ey"},
	"this":      {"ð", "ɪ", "s"},
	"that":      {"ð", "æ", "t"},
	"and":       {"æ", "n", "d"},
	"or":        {"ɔ", "ɹ"},
	"but":       {"b", "ʌ", "t"},
	"not":       {"n", "ɑ", "t"},
	"have":      {"hh", "æ", "v"},
	"has":       {"hh", "æ", "z"},
	"had":       {"hh", "æ", "d"},
	"do":        {"d", "uw"},
	"does":      {"d", "ʌ", "z"},
	"did":       {"d", "ɪ", "d"},
	"will":      {"w", "ɪ", "l"},
	"would":     {"w", "ʊ", "d"},
	"can":       {"k", "æ", "n"},
	"could":     {"k", "ʊ", "d"},
	"good":      {"ɡ", "ʊ", "d"},
	"bad":       {"b", "æ", "d"},
	"yes":       {"y", "ɛ", "s"},
	"no":        {"n", "ow"},
	"thank":     {"θ", "æ", "ŋ", "k"},
	"thanks":    {"θ", "æ", "ŋ", "k", "s"},
	"please":    {"p", "l", "iy", "z"},
	"sorry":     {"s", "ɑ", "ɹ", "iy"},
	"ok":        {"ow", "k", "ey"},
	"okay":      {"ow", "k", "ey"},
	"one":       {"w", "ʌ", "n"},
	"two":       {"t", "uw"},
	"three":     {"θ", "ɹ", "iy"},
	"four":      {"f", "ɔ", "ɹ"},
	"five":      {"f", "ay", "v"},
	"six":       {"s", "ɪ", "k", "s"},
	"seven":     {"s", "ɛ", "v", "ə", "n"},
	"eight":     {"ey", "t"},
	"nine":      {"n", "ay", "n"},
	"ten":       {"t", "ɛ", "n"},
	"what":      {"w", "ɑ", "t"},
	"where":     {"w", "ɛ", "ɹ"},
	"when":      {"w", "ɛ", "n"},
	"who":       {"hh", "uw"},
	"how":       {"hh", "aw"},
	"why":       {"w", "ay"},
	"my":        {"m", "ay"},
	"your":      {"y", "ɔ", "ɹ"},
	"his":       {"hh", "ɪ", "z"},
	"her":       {"hh", "ər"},
	"our":       {"aw", "ər"},
	"their":     {"ð", "ɛ", "ɹ"},
	"name":      {"n", "ey", "m"},
	"time":      {"t", "ay", "m"},
	"day":       {"d", "ey"},
	"way":       {"w", "ey"},
	"man":       {"m", "æ", "n"},
	"woman":     {"w", "ʊ", "m", "ə", "n"},
	"child":     {"ch", "ay", "l", "d"},
	"people":    {"p", "iy", "p", "ə", "l"},
	"work":      {"w", "ər", "k"},
	"life":      {"l", "ay", "f"},
	"love":      {"l", "ʌ", "v"},
	"home":      {"hh", "ow", "m"},
	"think":     {"θ", "ɪ", "ŋ", "k"},
	"know":      {"n", "ow"},
	"want":      {"w", "ɑ", "n", "t"},
	"need":      {"n", "iy", "d"},
	"like":      {"l", "ay", "k"},
	"come":      {"k", "ʌ", "m"},
	"go":        {"ɡ", "ow"},
	"make":      {"m", "ey", "k"},
	"take":      {"t", "ey", "k"},
	"give":      {"ɡ", "ɪ", "v"},
	"get":       {"ɡ", "ɛ", "t"},
	"say":       {"s", "ey"},
	"see":       {"s", "iy"},
	"look":      {"l", "ʊ", "k"},
	"find":      {"f", "ay", "n", "d"},
	"here":      {"hh", "ɪ", "ɹ"},
	"there":     {"ð", "ɛ", "ɹ"},
	"now":       {"n", "aw"},
	"then":      {"ð", "ɛ", "n"},
	"just":      {"jh", "ʌ", "s", "t"},
	"also":      {"ɔ", "l", "s", "ow"},
	"very":      {"v", "ɛ", "ɹ", "iy"},
	"well":      {"w", "ɛ", "l"},
	"back":      {"b", "æ", "k"},
	"only":      {"ow", "n", "l", "iy"},
	"new":       {"n", "uw"},
	"more":      {"m", "ɔ", "ɹ"},
	"some":      {"s", "ʌ", "m"},
	"all":       {"ɔ", "l"},
	"many":      {"m", "ɛ", "n", "iy"},
	"much":      {"m", "ʌ", "ch"},
	"other":     {"ʌ", "ð", "ər"},
	"first":     {"f", "ər", "s", "t"},
	"last":      {"l", "æ", "s", "t"},
	"long":      {"l", "ɔ", "ŋ"},
	"great":     {"ɡ", "ɹ", "ey", "t"},
	"little":    {"l", "ɪ", "t", "ə", "l"},
	"own":       {"ow", "n"},
	"old":       {"ow", "l", "d"},
	"right":     {"ɹ", "ay", "t"},
	"big":       {"b", "ɪ", "ɡ"},
	"high":      {"hh", "ay"},
	"small":     {"s", "m", "ɔ", "l"},
	"large":     {"l", "ɑ", "ɹ", "jh"},
	"next":      {"n", "ɛ", "k", "s", "t"},
	"early":     {"ər", "l", "iy"},
	"young":     {"y", "ʌ", "ŋ"},
	"important": {"ɪ", "m", "p", "ɔ", "ɹ", "t", "ə", "n", "t"},
	"few":       {"f", "y", "uw"},
	"public":    {"p", "ʌ", "b", "l", "ɪ", "k"},
	"same":      {"s", "ey", "m"},
	"able":      {"ey", "b", "ə", "l"},
}
