package tts

import (
	"encoding/json"
	"fmt"
	"os"
)

// PhonemeTable maps phoneme strings to integer IDs.
// Built from MeloTTS config.json "symbols" field.
type PhonemeTable struct {
	sym2id map[string]int
	id2sym []string
}

// NewPhonemeTable creates the default MeloTTS phoneme table.
func NewPhonemeTable() *PhonemeTable {
	return NewPhonemeTableFromSymbols(defaultSymbols)
}

// NewPhonemeTableFromSymbols creates a phoneme table from a symbol list.
func NewPhonemeTableFromSymbols(symbols []string) *PhonemeTable {
	pt := &PhonemeTable{
		sym2id: make(map[string]int, len(symbols)),
		id2sym: make([]string, len(symbols)),
	}
	for i, s := range symbols {
		pt.sym2id[s] = i
		pt.id2sym[i] = s
	}
	return pt
}

// NewPhonemeTableFromConfig loads a phoneme table from a MeloTTS config.json file.
func NewPhonemeTableFromConfig(configPath string) (*PhonemeTable, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var config struct {
		Symbols []string `json:"symbols"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if len(config.Symbols) == 0 {
		return nil, fmt.Errorf("no symbols in config")
	}
	return NewPhonemeTableFromSymbols(config.Symbols), nil
}

// ID returns the phoneme ID for a symbol, or 0 (_) if not found.
func (pt *PhonemeTable) ID(sym string) int {
	if id, ok := pt.sym2id[sym]; ok {
		return id
	}
	return 0 // "_" = padding/blank
}

// Size returns the vocabulary size.
func (pt *PhonemeTable) Size() int { return len(pt.id2sym) }

// BlankID returns the blank token ID (used for VITS add_blank).
const BlankID = 0 // "_"

// DefaultSymbolsList returns the default symbol list for debugging.
func DefaultSymbolsList() []string { return defaultSymbols }

// defaultSymbols is the MeloTTS phoneme vocabulary from config.json.
// Index = phoneme ID.
var defaultSymbols = []string{
	"_", "\"", "(", ")", "*", "/", ":", "AA", "E", "EE",
	"En", "N", "OO", "Q", "V", "[", "\\", "]", "^", "a",
	"a:", "aa", "ae", "ah", "ai", "an", "ang", "ao", "aw", "ay",
	"b", "by", "c", "ch", "d", "dh", "dy", "e", "e:", "eh",
	"ei", "en", "eng", "er", "ey", "f", "g", "gy", "h", "hh",
	"hy", "i", "i0", "i:", "ia", "ian", "iang", "iao", "ie", "ih",
	"in", "ing", "iong", "ir", "iu", "iy", "j", "jh", "k", "ky",
	"l", "m", "my", "n", "ng", "ny", "o", "o:", "ong", "ou",
	"ow", "oy", "p", "py", "q", "r", "ry", "s", "sh", "t",
	"th", "ts", "ty", "u", "u:", "ua", "uai", "uan", "uang", "uh",
	"ui", "un", "uo", "uw", "v", "van", "ve", "vn", "w", "x",
	"y", "z", "zh", "zy", "~", "æ", "ç", "ð", "ø", "ŋ",
	"œ", "ɐ", "ɑ", "ɒ", "ɔ", "ɕ", "ə", "ɛ", "ɜ", "ɡ",
	"ɣ", "ɥ", "ɦ", "ɪ", "ɫ", "ɬ", "ɭ", "ɯ", "ɲ", "ɵ",
	"ɸ", "ɹ", "ɾ", "ʁ", "ʃ", "ʊ", "ʌ", "ʎ", "ʏ", "ʑ",
	"ʒ", "ʝ", "ʲ", "ˈ", "ˌ", "ː", "\u0303", "\u0329", "β", "θ",
	// Korean jamo (ᄀ-ᄒ, ᅡ-ᅵ, ᆨ-ᆼ, ㄸ)
	"ᄀ", "ᄁ", "ᄂ", "ᄃ", "ᄄ", "ᄅ", "ᄆ", "ᄇ", "ᄈ", "ᄉ",
	"ᄊ", "ᄋ", "ᄌ", "ᄍ", "ᄎ", "ᄏ", "ᄐ", "ᄑ", "ᄒ",
	"ᅡ", "ᅢ", "ᅣ", "ᅤ", "ᅥ", "ᅦ", "ᅧ", "ᅨ", "ᅩ", "ᅪ",
	"ᅫ", "ᅬ", "ᅭ", "ᅮ", "ᅯ", "ᅰ", "ᅱ", "ᅲ", "ᅳ", "ᅴ",
	"ᅵ",
	"ᆨ", "ᆫ", "ᆮ", "ᆯ", "ᆷ", "ᆸ", "ᆼ", "ㄸ",
	// Punctuation
	"!", "?", "…", ",", ".", "'", "-",
	"¿", "¡",
	// Special
	"SP", "UNK",
}
