package asr

import (
	"fmt"
	"strings"
)

// detokenize converts token IDs to text.
// Handles SentencePiece ▁ replacement, byte tokens <0xNN>, and CJK space removal.
func (m *MoonshineModel) detokenize(tokens []int) string {
	var sb strings.Builder
	for _, tid := range tokens {
		if tid == m.hp.BOSID || tid == m.hp.EOSID {
			continue
		}
		tok, ok := m.vocab[tid]
		if !ok {
			sb.WriteString(fmt.Sprintf("<%d>", tid))
			continue
		}

		// Decode byte tokens like <0xNN>
		if len(tok) >= 6 && tok[0] == '<' && tok[1] == '0' && tok[2] == 'x' {
			var byteVal uint
			if _, err := fmt.Sscanf(tok, "<0x%x>", &byteVal); err == nil && byteVal <= 0xFF {
				sb.WriteByte(byte(byteVal))
				continue
			}
		}

		// Replace SentencePiece ▁ (U+2581) with space
		tok = strings.ReplaceAll(tok, "\xe2\x96\x81", " ")
		sb.WriteString(tok)
	}

	result := sb.String()

	// Remove spaces between CJK characters
	result = removeCJKSpaces(result)

	// Trim leading/trailing whitespace
	return strings.TrimSpace(result)
}

// removeCJKSpaces removes spaces adjacent to CJK characters.
func removeCJKSpaces(s string) string {
	runes := []rune(s)
	var out []rune
	for i, r := range runes {
		if r == ' ' {
			prevCJK := i > 0 && isCJK(runes[i-1])
			nextCJK := i+1 < len(runes) && isCJK(runes[i+1])
			if prevCJK || nextCJK {
				continue // skip space
			}
		}
		out = append(out, r)
	}
	return string(out)
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols and Punctuation
		(r >= 0xFF00 && r <= 0xFFEF) || // Fullwidth Forms
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Extension B
		(r >= 0xF900 && r <= 0xFAFF) // CJK Compatibility Ideographs
}
