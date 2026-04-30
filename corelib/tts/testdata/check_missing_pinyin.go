//go:build ignore

package main

import (
	"fmt"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	text := "你好，欢迎使用MacLaw。今天天气不错，我们一起来写代码吧。"
	pt := tts.NewPhonemeTable()
	g2p := tts.TextToPhonemes(text, pt, tts.LangZH)
	fmt.Printf("Phoneme IDs: %d\n", len(g2p.PhonemeIDs))

	// Check which characters have no pinyin
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			py := tts.CharToPinyinExported(r)
			if py == "" {
				fmt.Printf("  MISSING: %c (U+%04X)\n", r, r)
			} else {
				fmt.Printf("  OK: %c → %s\n", r, py)
			}
		} else if unicode.IsLetter(r) {
			fmt.Printf("  LETTER: %c\n", r)
		}
	}
}
