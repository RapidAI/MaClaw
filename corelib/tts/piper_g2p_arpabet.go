package tts

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"strings"
)

// ARPAbet-to-Pinyin mapping for the Piper xiao_ya model.
//
// CMU Pronouncing Dictionary uses ARPAbet with 39 phonemes.
// Each ARPAbet phoneme maps to the closest pinyin initial+final combination.
//
// Vowels carry stress markers (0=no, 1=primary, 2=secondary).
// We map stress to Chinese tones: 1→4(falling), 2→2(rising), 0→5(neutral).

// arpabetToPinyin maps each ARPAbet phoneme to pinyin {initial, final}.
// Tone is determined by stress marker, not by this table.
//
// Design principles:
// 1. Vowels map to the closest Chinese vowel sound
// 2. Consonants have two forms: onset (before vowel) and coda (word-end/before consonant)
//    - Onset: just the initial, vowel comes from the next ARPAbet phoneme
//    - Coda: initial + minimal vowel (轻声), as short as possible
var arpabetToPinyin = map[string][2]string{
	// Vowels (monophthongs) — these provide the "final" in a pinyin syllable
	"AA": {"Ø", "a"},   // odd, father → 阿
	"AE": {"Ø", "ai"},  // at, bat → 爱 (English /æ/ ≈ Chinese ai)
	"AH": {"Ø", "a"},   // hut, but → 阿 (English /ʌ/ ≈ short a, NOT e)
	"AO": {"Ø", "ao"},  // ought, all → 奥
	"EH": {"Ø", "e"},   // ed, bet → 额
	"ER": {"Ø", "er"},  // hurt, bird → 尔
	"IH": {"Ø", "i"},   // it, bit → 衣
	"IY": {"Ø", "i"},   // eat, see → 衣
	"UH": {"w", "u"},   // hood, put → 乌
	"UW": {"w", "u"},   // boot, two → 乌

	// Vowels (diphthongs)
	"AW": {"Ø", "ao"},  // cow, how → 奥
	"AY": {"Ø", "ai"},  // hide, my → 爱
	"EY": {"Ø", "ei"},  // ate, say → 诶
	"OW": {"Ø", "ou"},  // oat, go → 欧
	"OY": {"Ø", "ao"},  // toy, boy → 奥

	// Stops — onset initial only, coda uses codaMap below
	"B": {"b", ""},
	"D": {"d", ""},
	"G": {"g", ""},
	"K": {"k", ""},
	"P": {"p", ""},
	"T": {"t", ""},

	// Affricates
	"CH": {"q", ""},
	"JH": {"j", ""},

	// Fricatives
	"DH": {"d", ""},    // thee (no th in Chinese)
	"F":  {"f", ""},
	"HH": {"h", ""},
	"S":  {"s", ""},
	"SH": {"sh", ""},
	"TH": {"s", ""},    // theta (no th in Chinese)
	"V":  {"w", ""},     // no v in Chinese, use w
	"Z":  {"z", ""},
	"ZH": {"r", ""},    // seizure

	// Nasals — these can be codas in Chinese (n, ng)
	"M":  {"m", ""},
	"N":  {"n", ""},
	"NG": {"Ø", "eng"}, // ping — NG is special: always a full syllable

	// Liquids
	"L": {"l", ""},
	"R": {"r", ""},

	// Semivowels
	"W": {"w", ""},
	"Y": {"y", ""},
}

// consonantCodaFinal maps consonant phonemes to their minimal coda vowel.
// Used when a consonant appears at word end or before another consonant.
// The goal is the shortest possible syllable — 轻声 (tone 5).
var consonantCodaFinal = map[string]string{
	"B": "u",  // 布(轻)
	"D": "e",  // 德(轻)
	"G": "e",  // 格(轻)
	"K": "e",  // 克(轻)
	"P": "u",  // 普(轻)
	"T": "e",  // 特(轻)
	"CH": "i", // 奇(轻)
	"JH": "i", // 吉(轻)
	"DH": "e", // 德(轻)
	"F": "u",  // 夫(轻)
	"HH": "e", // 赫(轻)
	"S": "i",  // 斯(轻)
	"SH": "i", // 什(轻)
	"TH": "i", // 斯(轻)
	"V": "ei", // 维(轻)
	"Z": "i",  // 兹(轻)
	"ZH": "i", // 日(轻)
	"M": "u",  // 木(轻)
	"N": "e",  // 呢(轻)
	"L": "e",  // 勒(轻)
	"R": "ui", // 瑞(轻)
	"W": "u",  // 乌(轻)
	"Y": "i",  // 衣(轻)
}

// consonantVowelOverride handles special consonant+vowel combinations
// where the default merge doesn't sound right.
// Key: "CONSONANT+VOWEL_BASE" → pinyin {initial, final}
var consonantVowelOverride = map[string][2]string{
	// R before vowels sounds different than coda R
	"R+AH": {"r", "a"},   // run → 然 not 瑞额
	"R+IY": {"r", "i"},   // read → 日 not 瑞衣
	"R+EY": {"r", "ei"},  // ray → 瑞
	"R+AO": {"r", "ao"},  // raw → 绕
	"R+OW": {"r", "ou"},  // row → 肉
	"R+UW": {"r", "u"},   // rude → 入
	"R+AE": {"r", "ai"},  // rat → 莱
	// V before vowels
	"V+AH": {"w", "a"},   // love → 拉 not 维额
	"V+EH": {"w", "e"},   // vet → 维
	"V+IY": {"w", "i"},   // visa → 维
	"V+AY": {"w", "ai"},  // vibe → 外
	"V+OW": {"w", "ou"},  // vote → 沃
}

// canMergeInitialFinal checks if a pinyin initial+final combination is valid.
func canMergeInitialFinal(initial, final string) bool {
	if initial == "" || initial == "Ø" {
		return false
	}
	// Check if the combination exists in the phoneme map
	_, ok := piperXiaoYaPhonemeMap[final]
	if !ok {
		return false
	}
	_, ok = piperXiaoYaPhonemeMap[initial]
	return ok
}

// stressToTone maps ARPAbet stress markers to Chinese tones.
func stressToTone(stress byte) string {
	switch stress {
	case '1':
		return "4" // primary stress → falling tone (most prominent)
	case '2':
		return "2" // secondary stress → rising tone
	default:
		return "5" // no stress → neutral tone
	}
}

// isArpabetVowel returns true if the phoneme (without stress) is a vowel.
func isArpabetVowel(ph string) bool {
	switch ph {
	case "AA", "AE", "AH", "AO", "EH", "ER", "IH", "IY", "UH", "UW",
		"AW", "AY", "EY", "OW", "OY":
		return true
	}
	return false
}

// arpabetToPinyinPhones converts a sequence of ARPAbet phonemes to pinyin phones.
// Input: ["B", "AH1", "G"] (bug)
// Output: ["b", "a", "4", "g", "e", "5"] (巴-格轻)
//
// Strategy:
// 1. Consonant + following vowel → merge into one pinyin syllable (initial + final + tone)
// 2. Consonant at word end or before another consonant → minimal coda syllable (轻声)
// 3. Standalone vowel → zero-initial syllable (Ø + final + tone)
// 4. Nasal N/M before consonant → try to attach to previous syllable as nasal coda
func arpabetToPinyinPhones(arpa []string) []string {
	var phones []string
	n := len(arpa)

	i := 0
	for i < n {
		ph := arpa[i]

		// Extract stress marker from vowels
		stress := byte('0')
		basePh := ph
		if len(ph) >= 2 && ph[len(ph)-1] >= '0' && ph[len(ph)-1] <= '2' {
			stress = ph[len(ph)-1]
			basePh = ph[:len(ph)-1]
		}

		mapping, ok := arpabetToPinyin[basePh]
		if !ok {
			i++
			continue
		}

		if isArpabetVowel(basePh) {
			// Standalone vowel (no preceding consonant consumed it)
			phones = append(phones, mapping[0], mapping[1], stressToTone(stress))
			i++
			continue
		}

		// NG is always a full syllable (eng)
		if basePh == "NG" {
			phones = append(phones, mapping[0], mapping[1], "5")
			i++
			continue
		}

		// Consonant: look ahead for a vowel to merge with
		nextBase, nextStress := peekArpabet(arpa, i+1)

		if isArpabetVowel(nextBase) {
			// Consonant + Vowel → merge into one syllable
			initial := mapping[0]

			// Check for special overrides
			overrideKey := basePh + "+" + nextBase
			if ov, ok := consonantVowelOverride[overrideKey]; ok {
				ovInitial, ovFinal := ov[0], ov[1]
				if !isValidPinyin(ovInitial, ovFinal) {
					ovFinal = nearestValidFinal(ovInitial, ovFinal)
				}
				phones = append(phones, ovInitial, ovFinal, stressToTone(nextStress))
				i += 2
				continue
			}

			vowelMapping := arpabetToPinyin[nextBase]
			final := vowelMapping[1]

			if canMergeInitialFinal(initial, final) {
				// Validate it's a real pinyin syllable
				if !isValidPinyin(initial, final) {
					final = nearestValidFinal(initial, final)
				}
				phones = append(phones, initial, final, stressToTone(nextStress))
				i += 2

				// Check if a nasal N/M follows (coda nasal → modify the final)
				if i < n {
					codaBase, _ := peekArpabet(arpa, i)
					if codaBase == "N" || codaBase == "M" {
						nasalFinal := tryNasalFinal(final, codaBase)
						if nasalFinal != "" && isValidPinyin(initial, nasalFinal) {
							phones[len(phones)-2] = nasalFinal
							i++
						}
					} else if codaBase == "NG" {
						nasalFinal := tryNgFinal(final)
						if nasalFinal != "" && isValidPinyin(initial, nasalFinal) {
							phones[len(phones)-2] = nasalFinal
							i++
						}
					}
				}
				continue
			}
			// Can't merge (invalid pinyin combo) → emit consonant as coda, vowel next iteration
		}

		// Consonant as coda (word end or before another consonant)
		codaFinal, ok := consonantCodaFinal[basePh]
		if !ok {
			codaFinal = "e"
		}
		codaInitial := mapping[0]
		if !isValidPinyin(codaInitial, codaFinal) {
			codaFinal = nearestValidFinal(codaInitial, codaFinal)
		}
		phones = append(phones, codaInitial, codaFinal, "5")
		i++
	}

	return phones
}

// peekArpabet returns the base phoneme and stress at position i, without consuming.
func peekArpabet(arpa []string, i int) (base string, stress byte) {
	if i >= len(arpa) {
		return "", '0'
	}
	ph := arpa[i]
	stress = '0'
	base = ph
	if len(ph) >= 2 && ph[len(ph)-1] >= '0' && ph[len(ph)-1] <= '2' {
		stress = ph[len(ph)-1]
		base = ph[:len(ph)-1]
	}
	return
}

// tryNasalFinal attempts to form a nasal final (an, en, in, un) from a vowel final + N/M.
func tryNasalFinal(final string, nasal string) string {
	// N coda
	if nasal == "N" || nasal == "M" {
		switch final {
		case "a":
			return "an"
		case "e":
			return "en"
		case "i":
			return "in"
		case "u":
			return "un"
		case "ai":
			return "an" // approximate
		case "ei":
			return "en" // approximate
		}
	}
	return ""
}

// tryNgFinal attempts to form a -ng final (ang, eng, ing, ong) from a vowel final + NG.
func tryNgFinal(final string) string {
	switch final {
	case "a":
		return "ang"
	case "e":
		return "eng"
	case "i":
		return "ing"
	case "ao", "ou":
		return "ong"
	case "u":
		return "ong"
	}
	return ""
}

// CMUDict holds the CMU Pronouncing Dictionary loaded from a gzipped file.
// Format: word → ARPAbet phoneme sequence.
type CMUDict struct {
	entries map[string][]string // lowercase word → ARPAbet phonemes
}

// LoadCMUDict loads a CMU dictionary file (plain text or gzipped).
func LoadCMUDict(path string) (*CMUDict, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	return LoadCMUDictFromReader(r)
}

// LoadCMUDictFromReader loads a CMU dictionary from an io.Reader.
func LoadCMUDictFromReader(r io.Reader) (*CMUDict, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	dict := &CMUDict{entries: make(map[string][]string, 140000)}
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == ';' {
			continue
		}
		// Format: "WORD PH1 PH2 PH3" (single space) or "WORD  PH1 PH2 PH3" (double space)
		// Also handles "WORD(1) PH1 PH2" alternate pronunciations
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		word := fields[0]
		phonemes := fields[1:]

		// Skip alternate pronunciations like "WORD(2)"
		if strings.Contains(word, "(") {
			continue
		}

		// Remove trailing punctuation from word (e.g. "A." → "A")
		word = strings.TrimRight(word, ".")
		word = strings.ToLower(word)
		if len(word) > 0 && len(phonemes) > 0 {
			dict.entries[word] = phonemes
		}
	}
	return dict, scanner.Err()
}

// Lookup returns the ARPAbet phonemes for a word, or nil if not found.
func (d *CMUDict) Lookup(word string) []string {
	if d == nil {
		return nil
	}
	return d.entries[strings.ToLower(word)]
}

// Size returns the number of entries in the dictionary.
func (d *CMUDict) Size() int {
	if d == nil {
		return 0
	}
	return len(d.entries)
}

// LookupPinyin looks up a word and converts its pronunciation to pinyin phones.
// Returns nil if the word is not in the dictionary.
func (d *CMUDict) LookupPinyin(word string) []string {
	arpa := d.Lookup(word)
	if arpa == nil {
		return nil
	}
	return arpabetToPinyinPhones(arpa)
}
