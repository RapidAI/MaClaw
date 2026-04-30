package tts

import (
	"strings"
	"unicode"
)

// English-to-Chinese-phoneme mapping for the Piper xiao_ya model.
//
// Since xiao_ya is a Chinese-only model, English text is converted to
// Chinese phonetic approximations using pinyin. Four-level strategy:
//
// 1. Hand-tuned word table → Chinese transliteration (brand names, tech terms)
// 2. CMU Pronouncing Dictionary → ARPAbet → pinyin (126K English words)
// 3. All-caps abbreviations ≤5 chars → letter-by-letter spelling (API, GPU)
// 4. Rule-based transliteration → fallback for unknown words
//
// Numbers are read as Chinese digits (e.g. "2024" → "二零二四").

// englishLetterPinyin maps each English letter to its Chinese phonetic spelling.
// Based on standard Chinese pronunciation of English letters.
// Format: []string{initial, final, tone} — same as lexicon entries.
// Uses "Ø" for zero-initial syllables (matching the lexicon format).
var englishLetterPinyin = map[byte][][]string{
	'a': {{"Ø", "ei", "1"}},
	'b': {{"b", "i", "4"}},
	'c': {{"x", "i", "1"}},
	'd': {{"d", "i", "4"}},
	'e': {{"Ø", "i", "4"}},
	'f': {{"Ø", "ai", "4"}, {"f", "u", "5"}},
	'g': {{"j", "i", "4"}},
	'h': {{"Ø", "ei", "4"}, {"q", "i", "3"}},
	'i': {{"Ø", "ai", "4"}},
	'j': {{"j", "ie", "4"}},
	'k': {{"k", "ei", "4"}},
	'l': {{"Ø", "ai", "4"}, {"l", "e", "4"}},
	'm': {{"Ø", "ai", "4"}, {"m", "u", "5"}},
	'n': {{"Ø", "en", "1"}},
	'o': {{"Ø", "ou", "1"}},
	'p': {{"p", "i", "1"}},
	'q': {{"k", "iu", "1"}},
	'r': {{"Ø", "a", "4"}, {"Ø", "er", "5"}},
	's': {{"Ø", "ai", "4"}, {"s", "i", "5"}},
	't': {{"t", "i", "4"}},
	'u': {{"y", "ou", "1"}},
	'v': {{"w", "ei", "1"}},
	'w': {{"d", "a", "4"}, {"b", "u", "5"}, {"l", "iu", "5"}},
	'x': {{"Ø", "ai", "4"}, {"k", "e", "4"}, {"s", "i", "5"}},
	'y': {{"w", "ai", "1"}},
	'z': {{"z", "ei", "4"}},
}

// englishWordPinyin maps common English words/abbreviations to Chinese
// phonetic approximations. Each entry is a sequence of syllables,
// where each syllable is []string{initial, final, tone}.
var englishWordPinyin = map[string][][][]string{
	// Tech terms — each Chinese syllable is a separate entry in the outer array
	"ai":       {{{"Ø", "ai", "4"}}, {{"Ø", "ai", "1"}}},                                   // 爱-哎
	"api":      {{{"Ø", "ei", "1"}}, {{"p", "i", "1"}}, {{"Ø", "ai", "4"}}},                // A-P-I
	"app":      {{{"Ø", "ai", "4"}}, {{"p", "u", "5"}}},                                     // 爱-普
	"cpu":      {{{"x", "i", "1"}}, {{"p", "i", "1"}}, {{"y", "ou", "1"}}},                  // C-P-U
	"gpu":      {{{"j", "i", "4"}}, {{"p", "i", "1"}}, {{"y", "ou", "1"}}},                  // G-P-U
	"ip":       {{{"Ø", "ai", "4"}}, {{"p", "i", "1"}}},                                     // I-P
	"ok":       {{{"Ø", "ou", "1"}}, {{"k", "ei", "4"}}},                                    // 欧-凯
	"pdf":      {{{"p", "i", "1"}}, {{"d", "i", "4"}}, {{"Ø", "ai", "4"}}, {{"f", "u", "5"}}}, // P-D-F (诶夫 split)
	"ppt":      {{{"p", "i", "1"}}, {{"p", "i", "1"}}, {{"t", "i", "4"}}},                   // P-P-T
	"url":      {{{"y", "ou", "1"}}, {{"Ø", "a", "4"}}, {{"Ø", "er", "5"}}, {{"Ø", "ai", "4"}}, {{"l", "e", "4"}}}, // U-R-L
	"usb":      {{{"y", "ou", "1"}}, {{"Ø", "ai", "4"}}, {{"s", "i", "5"}}, {{"b", "i", "4"}}}, // U-S-B
	"wifi":     {{{"w", "ai", "1"}}, {{"f", "ai", "1"}}},                                    // 歪-fai
	"bug":      {{{"b", "o", "4"}}, {{"g", "e", "4"}}},                                      // 伯-格
	"code":     {{{"k", "e", "4"}}, {{"d", "e", "5"}}},                                      // 科-德
	"go":       {{{"g", "ou", "1"}}},                                                         // 勾
	"java":     {{{"j", "ia", "1"}}, {{"w", "a", "3"}}},                                     // 贾-瓦
	"linux":    {{{"l", "in", "2"}}, {{"n", "a", "4"}}, {{"k", "e", "4"}}, {{"s", "i", "5"}}}, // 林-纳-克-斯
	"python":   {{{"p", "ai", "4"}}, {{"sh", "en", "2"}}},                                   // 派-森
	"windows":  {{{"w", "in", "1"}}, {{"d", "ou", "1"}}, {{"s", "i", "5"}}},                 // 温-斗-斯
	"android":  {{{"Ø", "an", "1"}}, {{"zh", "uo", "1"}}},                                   // 安-卓
	"ios":      {{{"Ø", "ai", "4"}}, {{"Ø", "ou", "1"}}, {{"Ø", "ai", "4"}}, {{"s", "i", "5"}}}, // I-O-S
	"mac":      {{{"m", "ai", "4"}}, {{"k", "e", "4"}}},                                     // 麦-克
	"google":   {{{"g", "u", "3"}}, {{"g", "e", "1"}}},                                      // 谷-歌
	"github":   {{{"j", "i", "2"}}, {{"t", "e", "4"}}, {{"h", "a", "1"}}, {{"b", "u", "4"}}}, // 吉-特-哈-布
	"docker":   {{{"d", "uo", "1"}}, {{"k", "e", "4"}}},                                     // 多-克
	"server":   {{{"s", "e", "4"}}, {{"Ø", "er", "5"}}, {{"w", "e", "4"}}},                  // 瑟-尔-维
	"web":      {{{"w", "ei", "1"}}, {{"b", "u", "4"}}},                                     // 韦-布
	"http":     {{{"Ø", "ei", "4"}}, {{"q", "i", "3"}}},                                     // 诶-奇 (simplified)
	"html":     {{{"Ø", "ei", "4"}}, {{"q", "i", "3"}}, {{"Ø", "ai", "4"}}, {{"m", "u", "5"}}, {{"Ø", "ai", "4"}}, {{"l", "e", "4"}}},
	"css":      {{{"x", "i", "1"}}, {{"Ø", "ai", "4"}}, {{"s", "i", "5"}}, {{"Ø", "ai", "4"}}, {{"s", "i", "5"}}},
	"ssh":      {{{"Ø", "ai", "4"}}, {{"s", "i", "5"}}, {{"Ø", "ai", "4"}}, {{"s", "i", "5"}}, {{"Ø", "ei", "4"}}, {{"q", "i", "3"}}},
	"sql":      {{{"Ø", "ai", "4"}}, {{"s", "i", "5"}}, {{"k", "iu", "1"}}, {{"Ø", "ai", "4"}}, {{"l", "e", "4"}}},

	// Common English words
	"hello":    {{{"h", "a", "1"}}, {{"l", "ou", "2"}}},                                     // 哈-喽
	"world":    {{{"w", "o", "4"}}, {{"Ø", "er", "5"}}, {{"d", "e", "5"}}},                  // 沃-尔-德
	"yes":      {{{"y", "e", "4"}}, {{"s", "i", "5"}}},                                      // 耶-斯
	"no":       {{{"n", "ou", "4"}}},                                                         // 诺
	"thanks":   {{{"sh", "an", "1"}}, {{"k", "e", "4"}}, {{"s", "i", "5"}}},                 // 珊-克-斯
	"sorry":    {{{"s", "uo", "1"}}, {{"r", "ui", "4"}}},                                    // 索-瑞
	"please":   {{{"p", "u", "1"}}, {{"l", "i", "4"}}, {{"s", "i", "5"}}},                   // 普-利-斯

	// Brand names
	"maclaw":     {{{"m", "ai", "4"}}, {{"k", "e", "4"}}, {{"l", "ao", "2"}}},
	"openai":     {{{"Ø", "ou", "1"}}, {{"p", "en", "2"}}, {{"Ø", "ai", "4"}}, {{"Ø", "ai", "1"}}},
	"chatgpt":    {{{"q", "ia", "4"}}, {{"t", "e", "4"}}, {{"j", "i", "4"}}, {{"p", "i", "1"}}, {{"t", "i", "4"}}},
	"claude":     {{{"k", "e", "4"}}, {{"l", "ao", "2"}}, {{"d", "e", "5"}}},
	"microsoft":  {{{"w", "ei", "1"}}, {{"r", "uan", "3"}}},
	"apple":      {{{"p", "in", "2"}}, {{"g", "uo", "3"}}},
	"amazon":     {{{"Ø", "a", "4"}}, {{"m", "a", "3"}}, {{"x", "un", "4"}}},
	"tesla":      {{{"t", "e", "4"}}, {{"s", "i", "5"}}, {{"l", "a", "1"}}},
	"nvidia":     {{{"Ø", "en", "1"}}, {{"w", "ei", "1"}}, {{"d", "a", "2"}}},
	"intel":      {{{"Ø", "ing", "1"}}, {{"t", "e", "4"}}, {{"Ø", "er", "5"}}},

	// Programming verbs & nouns
	"install":      {{{"Ø", "in", "1"}}, {{"s", "i", "5"}}, {{"t", "ao", "4"}}},
	"update":       {{{"Ø", "a", "4"}}, {{"p", "u", "3"}}, {{"d", "ei", "4"}}, {{"t", "e", "4"}}},
	"delete":       {{{"d", "i", "2"}}, {{"l", "i", "4"}}, {{"t", "e", "4"}}},
	"create":       {{{"k", "e", "4"}}, {{"r", "ui", "4"}}, {{"Ø", "ei", "4"}}, {{"t", "e", "4"}}},
	"commit":       {{{"k", "e", "4"}}, {{"m", "i", "4"}}, {{"t", "e", "4"}}},
	"deploy":       {{{"d", "i", "2"}}, {{"p", "u", "3"}}, {{"l", "ao", "4"}}},
	"build":        {{{"b", "i", "4"}}, {{"Ø", "er", "5"}}, {{"d", "e", "5"}}},
	"start":        {{{"s", "i", "5"}}, {{"t", "a", "4"}}, {{"t", "e", "4"}}},
	"stop":         {{{"s", "i", "5"}}, {{"t", "ao", "4"}}, {{"p", "u", "3"}}},
	"run":          {{{"r", "an", "4"}}},
	"test":         {{{"t", "e", "4"}}, {{"s", "i", "5"}}, {{"t", "e", "4"}}},
	"push":         {{{"p", "u", "3"}}, {{"sh", "i", "4"}}},
	"pull":         {{{"p", "u", "3"}}, {{"Ø", "er", "5"}}},
	"merge":        {{{"m", "o", "4"}}, {{"j", "i", "4"}}},
	"clone":        {{{"k", "e", "4"}}, {{"l", "ong", "2"}}},
	"fetch":        {{{"f", "ei", "1"}}, {{"q", "i", "4"}}},
	"reset":        {{{"r", "ui", "4"}}, {{"s", "ai", "4"}}, {{"t", "e", "4"}}},
	"config":       {{{"k", "en", "1"}}, {{"f", "i", "4"}}, {{"g", "e", "2"}}},
	"debug":        {{{"d", "i", "2"}}, {{"b", "o", "4"}}, {{"g", "e", "2"}}},
	"compile":      {{{"k", "en", "1"}}, {{"p", "ai", "4"}}, {{"Ø", "er", "5"}}},
	"import":       {{{"Ø", "in", "1"}}, {{"p", "ao", "4"}}, {{"t", "e", "4"}}},
	"export":       {{{"Ø", "ai", "4"}}, {{"k", "e", "4"}}, {{"s", "i", "5"}}, {{"p", "ao", "4"}}, {{"t", "e", "4"}}},
	"download":     {{{"d", "ao", "4"}}, {{"Ø", "en", "1"}}, {{"l", "ou", "2"}}, {{"d", "e", "5"}}},
	"upload":       {{{"Ø", "a", "4"}}, {{"p", "u", "3"}}, {{"l", "ou", "2"}}, {{"d", "e", "5"}}},
	"login":        {{{"l", "ao", "2"}}, {{"g", "in", "1"}}},
	"error":        {{{"Ø", "ai", "4"}}, {{"r", "e", "4"}}},
	"warning":      {{{"w", "ao", "1"}}, {{"n", "ing", "2"}}},
	"success":      {{{"s", "e", "4"}}, {{"k", "e", "4"}}, {{"s", "ai", "4"}}, {{"s", "i", "5"}}},
	"failed":       {{{"f", "ei", "1"}}, {{"Ø", "er", "5"}}, {{"d", "e", "5"}}},
	"timeout":      {{{"t", "ai", "4"}}, {{"m", "u", "4"}}, {{"Ø", "ao", "4"}}, {{"t", "e", "4"}}},
	"overflow":     {{{"Ø", "ou", "1"}}, {{"w", "o", "4"}}, {{"f", "u", "1"}}, {{"l", "ou", "2"}}},
	"undefined":    {{{"Ø", "an", "1"}}, {{"d", "i", "2"}}, {{"f", "ai", "1"}}, {{"Ø", "en", "1"}}, {{"d", "e", "5"}}},
	"null":         {{{"n", "a", "4"}}, {{"Ø", "er", "5"}}},
	"exception":    {{{"Ø", "ai", "4"}}, {{"k", "e", "4"}}, {{"s", "ai", "4"}}, {{"p", "u", "3"}}, {{"sh", "en", "1"}}},
	"function":     {{{"f", "ang", "1"}}, {{"k", "e", "4"}}, {{"sh", "en", "1"}}},
	"class":        {{{"k", "e", "4"}}, {{"l", "a", "1"}}, {{"s", "i", "5"}}},
	"string":       {{{"s", "i", "5"}}, {{"zh", "ui", "4"}}, {{"Ø", "ing", "1"}}},
	"array":        {{{"Ø", "e", "4"}}, {{"r", "ui", "4"}}},
	"file":         {{{"f", "ai", "4"}}, {{"Ø", "er", "5"}}},
	"node":         {{{"n", "ou", "2"}}, {{"d", "e", "5"}}},
	"module":       {{{"m", "o", "2"}}, {{"j", "iu", "4"}}},
	"package":      {{{"p", "ai", "4"}}, {{"k", "i", "4"}}, {{"j", "i", "4"}}},
	"version":      {{{"w", "o", "4"}}, {{"sh", "en", "1"}}},
	"release":      {{{"r", "ui", "4"}}, {{"l", "i", "4"}}, {{"s", "i", "5"}}},
	"branch":       {{{"b", "u", "4"}}, {{"l", "an", "2"}}, {{"q", "i", "4"}}},
	"master":       {{{"m", "a", "4"}}, {{"s", "i", "5"}}, {{"t", "e", "4"}}},
	"main":         {{{"m", "ei", "2"}}},
	"cache":        {{{"k", "ai", "3"}}, {{"sh", "i", "4"}}},
	"queue":        {{{"k", "iu", "1"}}},
	"stack":        {{{"s", "i", "5"}}, {{"t", "ai", "4"}}, {{"k", "e", "4"}}},
	"thread":       {{{"s", "i", "5"}}, {{"r", "ui", "4"}}, {{"d", "e", "5"}}},
	"process":      {{{"p", "u", "3"}}, {{"l", "ao", "2"}}, {{"s", "ai", "4"}}, {{"s", "i", "5"}}},
	"memory":       {{{"m", "ei", "2"}}, {{"m", "o", "4"}}, {{"r", "ui", "4"}}},
	"socket":       {{{"s", "uo", "1"}}, {{"k", "i", "4"}}, {{"t", "e", "4"}}},
	"request":      {{{"r", "ui", "4"}}, {{"k", "e", "4"}}, {{"w", "ei", "1"}}, {{"s", "i", "5"}}, {{"t", "e", "4"}}},
	"response":     {{{"r", "ui", "4"}}, {{"s", "i", "5"}}, {{"p", "ang", "2"}}, {{"s", "i", "5"}}},
	"callback":     {{{"k", "ao", "1"}}, {{"b", "ai", "4"}}, {{"k", "e", "4"}}},
	"async":        {{{"Ø", "ei", "1"}}, {{"x", "in", "1"}}, {{"k", "e", "4"}}},
	"sync":         {{{"x", "in", "1"}}, {{"k", "e", "4"}}},
	"container":    {{{"k", "en", "1"}}, {{"t", "ei", "2"}}, {{"n", "e", "4"}}},
	"cluster":      {{{"k", "e", "4"}}, {{"l", "a", "1"}}, {{"s", "i", "5"}}, {{"t", "e", "4"}}},
	"database":     {{{"d", "ei", "4"}}, {{"t", "a", "4"}}, {{"b", "ei", "1"}}, {{"s", "i", "5"}}},
	"network":      {{{"n", "ei", "4"}}, {{"t", "e", "4"}}, {{"w", "o", "4"}}, {{"k", "e", "4"}}},
	"service":      {{{"s", "e", "4"}}, {{"w", "ei", "1"}}, {{"s", "i", "5"}}},
	"interface":    {{{"Ø", "in", "1"}}, {{"t", "e", "4"}}, {{"f", "ei", "1"}}, {{"s", "i", "5"}}},
	"performance":  {{{"p", "e", "4"}}, {{"f", "ao", "4"}}, {{"m", "en", "2"}}, {{"s", "i", "5"}}},
	"configuration": {{{"k", "en", "1"}}, {{"f", "i", "4"}}, {{"g", "iu", "1"}}, {{"r", "ui", "4"}}, {{"sh", "en", "1"}}},
	"application":  {{{"Ø", "a", "4"}}, {{"p", "u", "3"}}, {{"l", "i", "4"}}, {{"k", "ei", "4"}}, {{"sh", "en", "1"}}},
	"development":  {{{"d", "i", "2"}}, {{"w", "ei", "1"}}, {{"l", "e", "4"}}, {{"p", "u", "3"}}, {{"m", "en", "2"}}, {{"t", "e", "4"}}},
	"environment":  {{{"Ø", "en", "1"}}, {{"w", "ai", "4"}}, {{"r", "en", "2"}}, {{"m", "en", "2"}}, {{"t", "e", "4"}}},
	"kubernetes":   {{{"k", "u", "4"}}, {{"b", "e", "4"}}, {{"n", "ei", "4"}}, {{"t", "i", "4"}}, {{"s", "i", "5"}}},
	"tensorflow":   {{{"t", "eng", "2"}}, {{"s", "e", "4"}}, {{"f", "u", "1"}}, {{"l", "ou", "2"}}},
	"pytorch":      {{{"p", "ai", "4"}}, {{"t", "ao", "4"}}, {{"q", "i", "4"}}},
	"numpy":        {{{"n", "an", "2"}}, {{"p", "ai", "4"}}},
	"pandas":       {{{"p", "an", "2"}}, {{"d", "a", "2"}}, {{"s", "i", "5"}}},
	"react":        {{{"r", "ui", "4"}}, {{"Ø", "ai", "4"}}, {{"k", "e", "4"}}, {{"t", "e", "4"}}},
	"redis":        {{{"r", "ui", "4"}}, {{"d", "i", "2"}}, {{"s", "i", "5"}}},
	"nginx":        {{{"Ø", "en", "1"}}, {{"j", "in", "1"}}, {{"k", "e", "4"}}, {{"s", "i", "5"}}},
	"model":        {{{"m", "o", "2"}}, {{"d", "e", "5"}}, {{"Ø", "er", "5"}}},
	"data":         {{{"d", "ei", "4"}}, {{"t", "a", "4"}}},
	"image":        {{{"Ø", "i", "4"}}, {{"m", "i", "4"}}, {{"j", "i", "4"}}},
	"script":       {{{"s", "i", "5"}}, {{"k", "e", "4"}}, {{"r", "ui", "4"}}, {{"p", "u", "3"}}, {{"t", "e", "4"}}},
	"plugin":       {{{"p", "u", "3"}}, {{"l", "a", "1"}}, {{"g", "in", "1"}}},
	"framework":    {{{"f", "u", "1"}}, {{"r", "ui", "4"}}, {{"m", "u", "3"}}, {{"w", "o", "4"}}, {{"k", "e", "4"}}},
	"engine":       {{{"Ø", "en", "1"}}, {{"j", "in", "4"}}},
	"agent":        {{{"Ø", "ei", "1"}}, {{"j", "en", "4"}}, {{"t", "e", "4"}}},
	"prompt":       {{{"p", "u", "3"}}, {{"l", "ang", "3"}}, {{"p", "u", "3"}}, {{"t", "e", "4"}}},
	"embedding":    {{{"Ø", "en", "1"}}, {{"b", "ei", "4"}}, {{"d", "ing", "4"}}},
	"transformer":  {{{"zh", "uan", "3"}}, {{"s", "i", "5"}}, {{"f", "ao", "4"}}, {{"m", "e", "4"}}},
	"inference":    {{{"Ø", "in", "1"}}, {{"f", "e", "4"}}, {{"r", "en", "2"}}, {{"s", "i", "5"}}},
	"batch":        {{{"b", "a", "1"}}, {{"q", "i", "4"}}},
	"layer":        {{{"l", "ei", "4"}}, {{"Ø", "er", "5"}}},
	"parameter":    {{{"p", "a", "4"}}, {{"r", "a", "4"}}, {{"m", "i", "4"}}, {{"t", "e", "4"}}},
	"output":       {{{"Ø", "ao", "4"}}, {{"t", "e", "4"}}, {{"p", "u", "3"}}, {{"t", "e", "4"}}},
	"input":        {{{"Ø", "in", "1"}}, {{"p", "u", "3"}}, {{"t", "e", "4"}}},
	"feature":      {{{"f", "i", "4"}}, {{"q", "ie", "4"}}},
	"default":      {{{"d", "i", "2"}}, {{"f", "ao", "4"}}, {{"t", "e", "4"}}},
	"status":       {{{"s", "i", "5"}}, {{"t", "ei", "1"}}, {{"t", "e", "4"}}, {{"s", "i", "5"}}},
	"message":      {{{"m", "ei", "2"}}, {{"s", "i", "4"}}, {{"j", "i", "4"}}},
	"session":      {{{"s", "ai", "4"}}, {{"sh", "en", "1"}}},
	"connection":   {{{"k", "e", "4"}}, {{"n", "ei", "4"}}, {{"k", "e", "4"}}, {{"sh", "en", "1"}}},
	"algorithm":    {{{"Ø", "ao", "4"}}, {{"g", "e", "4"}}, {{"r", "ui", "4"}}, {{"s", "e", "4"}}, {{"m", "u", "3"}}},
	"system":       {{{"x", "i", "4"}}, {{"s", "i", "5"}}, {{"t", "e", "4"}}, {{"m", "u", "3"}}},
	"browser":      {{{"b", "u", "4"}}, {{"l", "ao", "2"}}, {{"zh", "e", "4"}}},
	"router":       {{{"r", "u", "4"}}, {{"t", "e", "4"}}},
	"manager":      {{{"m", "ai", "4"}}, {{"n", "i", "2"}}, {{"j", "e", "4"}}},
	"handler":      {{{"h", "an", "1"}}, {{"d", "e", "5"}}, {{"l", "e", "4"}}},
	"controller":   {{{"k", "en", "1"}}, {{"zh", "uo", "1"}}, {{"l", "e", "4"}}},
	"devops":       {{{"d", "ei", "4"}}, {{"w", "o", "4"}}, {{"p", "u", "3"}}, {{"s", "i", "5"}}},
}

// digitPinyin maps digit characters to Chinese pronunciation.
// Uses "Ø" for zero-initial syllables (matching the lexicon format).
var digitPinyin = map[rune][][]string{
	'0': {{"l", "ing", "2"}},
	'1': {{"y", "i", "1"}},
	'2': {{"Ø", "er", "4"}},
	'3': {{"s", "an", "1"}},
	'4': {{"s", "i", "4"}},
	'5': {{"w", "u", "3"}},
	'6': {{"l", "iu", "4"}},
	'7': {{"q", "i", "1"}},
	'8': {{"b", "a", "1"}},
	'9': {{"j", "iu", "3"}},
}

// isEnglishLetter returns true for ASCII letters.
func isEnglishLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isDigit returns true for ASCII digits.
func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// englishWordToPhones converts an English word to a sequence of charPhoneInfo entries.
// Priority: 1) word lookup table, 2) rule-based syllable transliteration.
// All-uppercase words ≤4 chars are treated as abbreviations and spelled letter-by-letter.
func englishWordToPhones(word string) []charPhoneInfo {
	return englishWordToPhonesWithDict(word, nil)
}

// englishWordToPhonesWithDict converts an English word using CMU dictionary when available.
// Priority: 1) hand-tuned word table, 2) CMU dictionary → ARPAbet → pinyin,
// 3) abbreviation letter-spelling, 4) rule-based transliteration.
func englishWordToPhonesWithDict(word string, cmuDict *CMUDict) []charPhoneInfo {
	lower := strings.ToLower(word)

	// Priority 1: hand-tuned word table (brand names, special pronunciations)
	if syllables, ok := englishWordPinyin[lower]; ok {
		return syllablesToCharPhones(word, syllables)
	}

	// Priority 2: CMU dictionary → ARPAbet → pinyin (accurate English pronunciation)
	if cmuDict != nil {
		if phones := cmuDict.LookupPinyin(lower); len(phones) > 0 {
			// Split phones into syllable-sized charPhoneInfo entries.
			// Each group of 3 phones (initial, final, tone) is one syllable.
			return phonesToCharPhones(word, phones)
		}
	}

	// Priority 3: all-uppercase short words → abbreviation letter-spelling
	if len(word) <= 5 && isAllUpper(word) {
		return letterSpell(word)
	}

	// Priority 4: rule-based syllable transliteration (fallback for unknown words)
	phones := englishWordTransliterate(word)
	if len(phones) == 0 {
		return letterSpell(word)
	}
	return phonesToCharPhones(word, phones)
}

// isAllUpper returns true if all letters in the string are uppercase.
func isAllUpper(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}

// letterSpell converts a word to letter-by-letter Chinese spelling.
func letterSpell(word string) []charPhoneInfo {
	var result []charPhoneInfo
	for _, r := range word {
		b := byte(unicode.ToLower(r))
		if syllables, ok := englishLetterPinyin[b]; ok {
			ci := charPhoneInfo{r: r, isCJK: true}
			for _, syl := range syllables {
				ci.phones = append(ci.phones, syl...)
			}
			result = append(result, ci)
		}
	}
	return result
}

// digitToPhones converts a digit character to a charPhoneInfo entry.
func digitToPhones(r rune) charPhoneInfo {
	ci := charPhoneInfo{r: r, isCJK: true}
	if phones, ok := digitPinyin[r]; ok {
		for _, syl := range phones {
			ci.phones = append(ci.phones, syl...)
		}
	}
	return ci
}

// phonesToCharPhones splits a flat phone list into syllable-sized charPhoneInfo entries.
// Phones come in groups of 3: {initial, final, tone}. Each group becomes one charPhoneInfo.
// The 2nd+ entries have noSepBefore=true for connected speech.
func phonesToCharPhones(word string, phones []string) []charPhoneInfo {
	var result []charPhoneInfo
	runes := []rune(word)
	ri := 0
	for i := 0; i+2 < len(phones); i += 3 {
		r := rune('?')
		if ri < len(runes) {
			r = runes[ri]
			ri++
		}
		ci := charPhoneInfo{
			r:           r,
			isCJK:       true,
			phones:      phones[i : i+3],
			noSepBefore: len(result) > 0,
		}
		result = append(result, ci)
	}
	// Handle leftover phones (shouldn't happen with well-formed data, but be safe)
	remainder := len(phones) % 3
	if remainder > 0 && len(result) > 0 {
		last := &result[len(result)-1]
		last.phones = append(last.phones, phones[len(phones)-remainder:]...)
	}
	if len(result) == 0 && len(phones) > 0 {
		// Fallback: pack everything into one entry
		ci := charPhoneInfo{r: rune(word[0]), isCJK: true, phones: phones}
		result = append(result, ci)
	}
	return result
}

// syllablesToCharPhones converts word-level syllable data to charPhoneInfo entries.
// Each syllable becomes one charPhoneInfo (treated as one "character" for spacing).
// The 2nd+ syllables have noSepBefore=true so they connect without pauses.
func syllablesToCharPhones(word string, syllables [][][]string) []charPhoneInfo {
	var result []charPhoneInfo
	runes := []rune(word)
	runeIdx := 0
	for si, syl := range syllables {
		r := rune('?')
		if runeIdx < len(runes) {
			r = runes[runeIdx]
			runeIdx++
		}
		ci := charPhoneInfo{r: r, isCJK: true}
		if si > 0 {
			ci.noSepBefore = true // connect syllables within a word
		}
		for _, phoneParts := range syl {
			ci.phones = append(ci.phones, phoneParts...)
		}
		result = append(result, ci)
	}
	return result
}
