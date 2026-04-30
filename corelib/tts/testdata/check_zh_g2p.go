//go:build ignore

package main

import (
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	pt := tts.NewPhonemeTable()

	texts := []string{"你好世界", "今天天气不错"}
	for _, text := range texts {
		g2p := tts.TextToPhonemes(text, pt, tts.LangZH)
		fmt.Printf("\n'%s':\n", text)
		fmt.Printf("  PhonemeIDs (%d): %v\n", len(g2p.PhonemeIDs), g2p.PhonemeIDs)
		fmt.Printf("  ToneIDs:        %v\n", g2p.ToneIDs)
		fmt.Printf("  LangIDs:        %v\n", g2p.LangIDs)

		// Decode IDs back to symbols
		syms := make([]string, len(g2p.PhonemeIDs))
		for i, id := range g2p.PhonemeIDs {
			if id < len(tts.DefaultSymbolsList()) {
				syms[i] = tts.DefaultSymbolsList()[id]
			} else {
				syms[i] = "?"
			}
		}
		fmt.Printf("  Symbols:        %v\n", syms)
	}
}
