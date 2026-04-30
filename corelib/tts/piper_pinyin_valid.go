package tts

// validPinyinSyllables contains all valid pinyin initial+final combinations
// that the xiao_ya model can correctly pronounce.
// Generated from standard Mandarin phonology.
//
// Used to validate ARPAbet→pinyin conversion output and word table entries.
// Invalid combinations are replaced with the closest valid alternative.

// validInitialFinal is a set of valid (initial, final) pairs.
// Key format: "initial+final" (e.g. "b+a", "Ø+ai", "zh+ang").
var validInitialFinal map[string]bool

func init() {
	// All valid pinyin syllables in standard Mandarin
	// Finals that can combine with each initial group
	// Group 1: b p m f
	bpmf := []string{"a", "o", "e", "ai", "ei", "ao", "ou", "an", "en", "ang", "eng", "i", "ie", "iao", "iu", "ian", "in", "iang", "ing", "u"}
	// Group 2: d t n l
	dtnl := []string{"a", "e", "ai", "ei", "ao", "ou", "an", "en", "ang", "eng", "ong", "i", "ia", "ie", "iao", "iu", "ian", "in", "iang", "ing", "u", "uo", "ui", "uan", "un", "v", "ve"}
	// Group 3: g k h
	gkh := []string{"a", "e", "ai", "ei", "ao", "ou", "an", "en", "ang", "eng", "ong", "u", "ua", "uo", "uai", "ui", "uan", "un", "uang"}
	// Group 4: j q x
	jqx := []string{"i", "ia", "ie", "iao", "iu", "ian", "in", "iang", "ing", "iong", "v", "ve", "van", "vn", "ue"}
	// Group 5: zh ch sh r
	zhchshr := []string{"a", "e", "ai", "ei", "ao", "ou", "an", "en", "ang", "eng", "ong", "i", "u", "ua", "uo", "uai", "ui", "uan", "un", "uang"}
	// Group 6: z c s
	zcs := []string{"a", "e", "ai", "ei", "ao", "ou", "an", "en", "ang", "eng", "ong", "i", "u", "uo", "ui", "uan", "un"}
	// Group 7: y
	yFinals := []string{"a", "e", "i", "ao", "ou", "an", "in", "ang", "ing", "ong", "u", "ue", "uan", "un"}
	// Group 8: w
	wFinals := []string{"a", "o", "ai", "ei", "an", "en", "ang", "eng", "u"}
	// Group 9: zero initial (Ø)
	zeroFinals := []string{"a", "o", "e", "ai", "ei", "ao", "ou", "an", "en", "ang", "eng", "er", "i", "u"}

	validInitialFinal = make(map[string]bool, 500)

	addGroup := func(inits []string, finals []string) {
		for _, ini := range inits {
			for _, fin := range finals {
				key := ini + "+" + fin
				validInitialFinal[key] = true
			}
		}
	}

	addGroup([]string{"b", "p", "m", "f"}, bpmf)
	addGroup([]string{"d", "t", "n", "l"}, dtnl)
	addGroup([]string{"g", "k", "h"}, gkh)
	addGroup([]string{"j", "q", "x"}, jqx)
	addGroup([]string{"zh", "ch", "sh", "r"}, zhchshr)
	addGroup([]string{"z", "c", "s"}, zcs)
	addGroup([]string{"y"}, yFinals)
	addGroup([]string{"w"}, wFinals)
	addGroup([]string{""}, zeroFinals)
}

// isValidPinyin checks if an initial+final combination is a valid pinyin syllable.
func isValidPinyin(initial, final string) bool {
	if initial == "Ø" {
		initial = ""
	}
	return validInitialFinal[initial+"+"+final]
}

// nearestValidFinal finds the closest valid final for a given initial.
// Used when ARPAbet conversion produces an invalid combination.
func nearestValidFinal(initial, wantFinal string) string {
	if initial == "Ø" {
		initial = ""
	}
	// Try the wanted final first
	if validInitialFinal[initial+"+"+wantFinal] {
		return wantFinal
	}

	// Fallback mapping: find the closest valid final
	fallbacks := map[string][]string{
		"a":   {"a"},
		"e":   {"e", "a"},
		"i":   {"i", "e"},
		"u":   {"u", "o"},
		"o":   {"o", "u", "e"},
		"ai":  {"ai", "a", "ei"},
		"ei":  {"ei", "e", "ai"},
		"ao":  {"ao", "a", "ou"},
		"ou":  {"ou", "o", "ao"},
		"an":  {"an", "en", "a"},
		"en":  {"en", "an", "e"},
		"ang": {"ang", "an", "eng"},
		"eng": {"eng", "en", "ang"},
		"ong": {"ong", "eng", "ang"},
		"er":  {"er", "e", "a"},
		"in":  {"in", "en", "i"},
		"ing": {"ing", "in", "eng"},
		"un":  {"un", "en", "u"},
		"uan": {"uan", "an", "u"},
		"ui":  {"ui", "ei", "u"},
		"uo":  {"uo", "o", "u"},
		"ia":  {"ia", "a", "i"},
		"ie":  {"ie", "e", "i"},
		"iu":  {"iu", "ou", "i"},
		"ian": {"ian", "an", "i"},
		"iang": {"iang", "ang", "ian"},
		"iong": {"iong", "ong", "i"},
		"iao": {"iao", "ao", "i"},
		"ua":  {"ua", "a", "u"},
		"uai": {"uai", "ai", "u"},
		"uang": {"uang", "ang", "u"},
		"v":   {"v", "u", "i"},
		"ve":  {"ve", "ue", "e"},
		"van": {"van", "uan", "an"},
		"vn":  {"vn", "un", "en"},
		"ue":  {"ue", "ve", "e"},
	}

	if candidates, ok := fallbacks[wantFinal]; ok {
		for _, f := range candidates {
			if validInitialFinal[initial+"+"+f] {
				return f
			}
		}
	}

	// Last resort: try all common finals
	for _, f := range []string{"a", "e", "i", "u", "o", "ai", "ei", "ao", "ou", "an", "en", "ang", "eng"} {
		if validInitialFinal[initial+"+"+f] {
			return f
		}
	}
	return wantFinal // give up, return as-is
}
