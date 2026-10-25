package codesearch

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// expandIdentifiers keeps each identifier and appends its camelCase and
// snake_case pieces. parseConfig then carries parseConfig, parse, and Config
// so a later lowercase tokenizer can match "parse config".
func expandIdentifiers(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text) + len(text)/4)
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if !identStart(r) {
			b.WriteRune(r)
			i += size
			continue
		}
		j := i + size
		for j < len(text) {
			r2, sz := utf8.DecodeRuneInString(text[j:])
			if !identCont(r2) {
				break
			}
			j += sz
		}
		word := text[i:j]
		b.WriteString(word)
		parts := splitIdent(word)
		if len(parts) > 1 {
			b.WriteByte(' ')
			b.WriteString(strings.Join(parts, " "))
		}
		i = j
	}
	return b.String()
}

func identStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func identCont(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func splitIdent(word string) []string {
	if !strings.Contains(word, "_") {
		return splitCamel(word)
	}
	var out []string
	for _, piece := range strings.Split(word, "_") {
		if piece == "" {
			continue
		}
		out = append(out, splitCamel(piece)...)
	}
	return out
}

func splitCamel(word string) []string {
	runes := []rune(word)
	if len(runes) < 2 {
		return []string{word}
	}
	var parts []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		boundary := (unicode.IsLower(prev) || unicode.IsDigit(prev)) && unicode.IsUpper(cur)
		if !boundary && unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
			boundary = true
		}
		if !boundary {
			continue
		}
		parts = append(parts, string(runes[start:i]))
		start = i
	}
	parts = append(parts, string(runes[start:]))
	if len(parts) == 1 {
		return parts
	}
	return parts
}
