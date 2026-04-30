package tts

import "strings"

// Rule-based English-to-Pinyin transliteration for the Piper xiao_ya model.
//
// Converts any English word to Chinese phonetic approximation by:
// 1. Greedy left-to-right matching of letter patterns against a rule table
// 2. Each matched pattern maps to one or more pinyin syllables
//
// This handles arbitrary English words without a dictionary, producing
// natural-sounding Chinese approximations (e.g. "kubernetes" → 库伯内提斯).
//
// Design: rules are ordered longest-first within each starting letter.
// The matcher tries the longest pattern first, falls back to shorter ones.
// Single-letter fallbacks ensure every letter is consumed.

// enSyllable represents one pinyin syllable: {initial, final, tone}.
type enSyllable = []string

// enRule maps an English letter pattern to pinyin syllables.
type enRule struct {
	pattern string
	pinyin  []enSyllable
	// If true, only match at word end
	wordEnd bool
}

// enTranslitRules is the rule table, grouped by first letter.
// Within each group, longer patterns come first (greedy matching).
var enTranslitRules = map[byte][]enRule{
	'a': {
		{pattern: "ation", pinyin: []enSyllable{{"Ø", "ei", "4"}, {"sh", "en", "1"}}},
		{pattern: "able", pinyin: []enSyllable{{"Ø", "ei", "4"}, {"b", "o", "4"}}},
		{pattern: "ance", pinyin: []enSyllable{{"Ø", "an", "1"}, {"s", "i", "5"}}},
		{pattern: "ange", pinyin: []enSyllable{{"Ø", "ein", "1"}, {"j", "i", "4"}}}, // fallback
		{pattern: "ang", pinyin: []enSyllable{{"Ø", "ang", "2"}}},
		{pattern: "ant", pinyin: []enSyllable{{"Ø", "an", "1"}, {"t", "e", "4"}}},
		{pattern: "and", pinyin: []enSyllable{{"Ø", "an", "1"}, {"d", "e", "5"}}},
		{pattern: "all", pinyin: []enSyllable{{"Ø", "ao", "4"}}},
		{pattern: "aw", pinyin: []enSyllable{{"Ø", "ao", "4"}}},
		{pattern: "au", pinyin: []enSyllable{{"Ø", "ao", "4"}}},
		{pattern: "ai", pinyin: []enSyllable{{"Ø", "ei", "4"}}},
		{pattern: "ay", pinyin: []enSyllable{{"Ø", "ei", "4"}}},
		{pattern: "ar", pinyin: []enSyllable{{"Ø", "a", "4"}, {"Ø", "er", "5"}}},
		{pattern: "an", pinyin: []enSyllable{{"Ø", "an", "1"}}},
		{pattern: "am", pinyin: []enSyllable{{"Ø", "an", "1"}}},
		{pattern: "al", pinyin: []enSyllable{{"Ø", "ao", "4"}}},
		{pattern: "as", pinyin: []enSyllable{{"Ø", "a", "4"}, {"s", "i", "5"}}},
		{pattern: "at", pinyin: []enSyllable{{"Ø", "a", "4"}, {"t", "e", "4"}}},
		{pattern: "a", pinyin: []enSyllable{{"Ø", "a", "4"}}},
	},
	'b': {
		{pattern: "ble", pinyin: []enSyllable{{"b", "o", "4"}}, wordEnd: true},
		{pattern: "b", pinyin: []enSyllable{{"b", "u", "4"}}, wordEnd: true},
		{pattern: "b", pinyin: []enSyllable{{"b", "u", "4"}}},
	},
	'c': {
		{pattern: "ction", pinyin: []enSyllable{{"k", "e", "4"}, {"sh", "en", "1"}}},
		{pattern: "ck", pinyin: []enSyllable{{"k", "e", "4"}}},
		{pattern: "ch", pinyin: []enSyllable{{"q", "i", "4"}}},
		{pattern: "ce", pinyin: []enSyllable{{"s", "i", "5"}}},
		{pattern: "ci", pinyin: []enSyllable{{"x", "i", "1"}}},
		{pattern: "cy", pinyin: []enSyllable{{"x", "i", "1"}}},
		{pattern: "c", pinyin: []enSyllable{{"k", "e", "4"}}},
	},
	'd': {
		{pattern: "d", pinyin: []enSyllable{{"d", "e", "5"}}, wordEnd: true},
		{pattern: "d", pinyin: []enSyllable{{"d", "e", "2"}}},
	},
	'e': {
		{pattern: "ence", pinyin: []enSyllable{{"Ø", "en", "1"}, {"s", "i", "5"}}},
		{pattern: "ent", pinyin: []enSyllable{{"Ø", "en", "1"}, {"t", "e", "4"}}},
		{pattern: "ess", pinyin: []enSyllable{{"Ø", "e", "4"}, {"s", "i", "5"}}},
		{pattern: "est", pinyin: []enSyllable{{"Ø", "e", "4"}, {"s", "i", "5"}, {"t", "e", "4"}}},
		{pattern: "er", pinyin: []enSyllable{{"Ø", "er", "5"}}, wordEnd: true},
		{pattern: "er", pinyin: []enSyllable{{"Ø", "er", "5"}}},
		{pattern: "ee", pinyin: []enSyllable{{"Ø", "i", "4"}}},
		{pattern: "ea", pinyin: []enSyllable{{"Ø", "i", "4"}}},
		{pattern: "ey", pinyin: []enSyllable{{"Ø", "ei", "1"}}},
		{pattern: "ew", pinyin: []enSyllable{{"y", "ou", "1"}}},
		{pattern: "en", pinyin: []enSyllable{{"Ø", "en", "1"}}},
		{pattern: "em", pinyin: []enSyllable{{"Ø", "en", "1"}}},
		{pattern: "el", pinyin: []enSyllable{{"Ø", "er", "5"}}},
		{pattern: "ex", pinyin: []enSyllable{{"Ø", "ai", "4"}, {"k", "e", "4"}, {"s", "i", "5"}}},
		{pattern: "e", pinyin: []enSyllable{}, wordEnd: true}, // silent final e
		{pattern: "e", pinyin: []enSyllable{{"Ø", "e", "4"}}},
	},
	'f': {
		{pattern: "ful", pinyin: []enSyllable{{"f", "u", "4"}}, wordEnd: true},
		{pattern: "f", pinyin: []enSyllable{{"f", "u", "1"}}},
	},
	'g': {
		{pattern: "ght", pinyin: []enSyllable{{"t", "e", "4"}}},
		{pattern: "gh", pinyin: []enSyllable{}}, // silent
		{pattern: "ge", pinyin: []enSyllable{{"j", "i", "4"}}, wordEnd: true},
		{pattern: "g", pinyin: []enSyllable{{"g", "e", "2"}}},
	},
	'h': {
		{pattern: "h", pinyin: []enSyllable{{"h", "e", "4"}}},
	},
	'i': {
		{pattern: "ight", pinyin: []enSyllable{{"Ø", "ai", "4"}, {"t", "e", "4"}}},
		{pattern: "ing", pinyin: []enSyllable{{"Ø", "ing", "1"}}},
		{pattern: "ion", pinyin: []enSyllable{{"Ø", "en", "1"}}},
		{pattern: "ive", pinyin: []enSyllable{{"Ø", "i", "4"}, {"f", "u", "1"}}},
		{pattern: "ity", pinyin: []enSyllable{{"Ø", "i", "4"}, {"t", "i", "4"}}},
		{pattern: "ize", pinyin: []enSyllable{{"Ø", "ai", "4"}, {"z", "i", "5"}}},
		{pattern: "ise", pinyin: []enSyllable{{"Ø", "ai", "4"}, {"z", "i", "5"}}},
		{pattern: "ir", pinyin: []enSyllable{{"Ø", "er", "5"}}},
		{pattern: "i", pinyin: []enSyllable{{"Ø", "i", "4"}}},
	},
	'j': {
		{pattern: "j", pinyin: []enSyllable{{"j", "ie", "2"}}},
	},
	'k': {
		{pattern: "k", pinyin: []enSyllable{{"k", "e", "4"}}},
	},
	'l': {
		{pattern: "ly", pinyin: []enSyllable{{"l", "i", "4"}}, wordEnd: true},
		{pattern: "le", pinyin: []enSyllable{{"l", "e", "4"}}, wordEnd: true},
		{pattern: "ll", pinyin: []enSyllable{{"l", "e", "4"}}},
		{pattern: "l", pinyin: []enSyllable{{"l", "e", "4"}}},
	},
	'm': {
		{pattern: "ment", pinyin: []enSyllable{{"m", "en", "2"}, {"t", "e", "4"}}},
		{pattern: "mm", pinyin: []enSyllable{{"m", "u", "3"}}},
		{pattern: "m", pinyin: []enSyllable{{"m", "u", "3"}}},
	},
	'n': {
		{pattern: "ness", pinyin: []enSyllable{{"n", "ei", "4"}, {"s", "i", "5"}}, wordEnd: true},
		{pattern: "ng", pinyin: []enSyllable{{"Ø", "eng", "1"}}},
		{pattern: "nn", pinyin: []enSyllable{{"n", "e", "4"}}},
		{pattern: "n", pinyin: []enSyllable{{"n", "e", "4"}}},
	},
	'o': {
		{pattern: "ough", pinyin: []enSyllable{{"Ø", "ao", "4"}}},
		{pattern: "ous", pinyin: []enSyllable{{"Ø", "e", "4"}, {"s", "i", "5"}}},
		{pattern: "oo", pinyin: []enSyllable{{"w", "u", "1"}}},
		{pattern: "ou", pinyin: []enSyllable{{"Ø", "ao", "4"}}},
		{pattern: "ow", pinyin: []enSyllable{{"Ø", "ou", "1"}}},
		{pattern: "oi", pinyin: []enSyllable{{"Ø", "ao", "4"}, {"Ø", "i", "4"}}},
		{pattern: "oy", pinyin: []enSyllable{{"Ø", "ao", "4"}, {"Ø", "i", "4"}}},
		{pattern: "or", pinyin: []enSyllable{{"Ø", "ao", "4"}, {"Ø", "er", "5"}}},
		{pattern: "on", pinyin: []enSyllable{{"Ø", "en", "1"}}},
		{pattern: "om", pinyin: []enSyllable{{"Ø", "en", "1"}}},
		{pattern: "o", pinyin: []enSyllable{{"Ø", "ao", "4"}}},
	},
	'p': {
		{pattern: "ph", pinyin: []enSyllable{{"f", "u", "1"}}},
		{pattern: "pp", pinyin: []enSyllable{{"p", "u", "3"}}},
		{pattern: "p", pinyin: []enSyllable{{"p", "u", "3"}}},
	},
	'q': {
		{pattern: "qu", pinyin: []enSyllable{{"k", "e", "4"}, {"w", "o", "4"}}},
		{pattern: "q", pinyin: []enSyllable{{"k", "e", "4"}}},
	},
	'r': {
		{pattern: "r", pinyin: []enSyllable{{"r", "ui", "4"}}},
	},
	's': {
		{pattern: "sion", pinyin: []enSyllable{{"sh", "en", "1"}}},
		{pattern: "sch", pinyin: []enSyllable{{"s", "i", "5"}, {"q", "i", "4"}}},
		{pattern: "sh", pinyin: []enSyllable{{"sh", "i", "4"}}},
		{pattern: "ss", pinyin: []enSyllable{{"s", "i", "5"}}},
		{pattern: "st", pinyin: []enSyllable{{"s", "i", "5"}, {"t", "e", "4"}}},
		{pattern: "sp", pinyin: []enSyllable{{"s", "i", "5"}, {"p", "u", "3"}}},
		{pattern: "sk", pinyin: []enSyllable{{"s", "i", "5"}, {"k", "e", "4"}}},
		{pattern: "s", pinyin: []enSyllable{{"s", "i", "5"}}, wordEnd: true},
		{pattern: "s", pinyin: []enSyllable{{"s", "i", "5"}}},
	},
	't': {
		{pattern: "tion", pinyin: []enSyllable{{"sh", "en", "1"}}},
		{pattern: "ture", pinyin: []enSyllable{{"q", "ie", "4"}}},
		{pattern: "th", pinyin: []enSyllable{{"s", "i", "5"}}},
		{pattern: "tt", pinyin: []enSyllable{{"t", "e", "4"}}},
		{pattern: "t", pinyin: []enSyllable{{"t", "e", "4"}}, wordEnd: true},
		{pattern: "t", pinyin: []enSyllable{{"t", "e", "4"}}},
	},
	'u': {
		{pattern: "ur", pinyin: []enSyllable{{"Ø", "er", "5"}}},
		{pattern: "us", pinyin: []enSyllable{{"Ø", "e", "4"}, {"s", "i", "5"}}},
		{pattern: "u", pinyin: []enSyllable{{"y", "ou", "1"}}},
	},
	'v': {
		{pattern: "v", pinyin: []enSyllable{{"w", "ei", "1"}}},
	},
	'w': {
		{pattern: "wr", pinyin: []enSyllable{{"r", "ui", "4"}}},
		{pattern: "w", pinyin: []enSyllable{{"w", "o", "4"}}},
	},
	'x': {
		{pattern: "x", pinyin: []enSyllable{{"k", "e", "4"}, {"s", "i", "5"}}},
	},
	'y': {
		{pattern: "y", pinyin: []enSyllable{{"y", "i", "4"}}, wordEnd: true},
		{pattern: "y", pinyin: []enSyllable{{"y", "a", "4"}}},
	},
	'z': {
		{pattern: "z", pinyin: []enSyllable{{"z", "i", "5"}}},
	},
}

// englishWordTransliterate converts an English word to pinyin syllables
// using rule-based greedy left-to-right matching.
// Returns a flat list of pinyin phones: {initial, final, tone, initial, final, tone, ...}
func englishWordTransliterate(word string) []string {
	lower := []byte(strings.ToLower(word))
	n := len(lower)
	var phones []string

	i := 0
	for i < n {
		c := lower[i]
		if c < 'a' || c > 'z' {
			i++
			continue
		}

		rules, ok := enTranslitRules[c]
		if !ok {
			i++
			continue
		}

		matched := false
		for _, rule := range rules {
			pLen := len(rule.pattern)
			if i+pLen > n {
				continue
			}
			// Check pattern match
			if string(lower[i:i+pLen]) != rule.pattern {
				continue
			}
			// Check wordEnd constraint
			if rule.wordEnd && i+pLen != n {
				continue
			}
			// Match found
			for _, syl := range rule.pinyin {
				phones = append(phones, syl...)
			}
			i += pLen
			matched = true
			break
		}
		if !matched {
			// Should not happen (every letter has a single-char fallback), but just in case
			i++
		}
	}
	return phones
}
