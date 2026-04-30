package tts

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// PiperLexicon holds the xiao_ya character-to-phoneme mapping.
// Loaded from lexicon.txt at runtime.
type PiperLexicon struct {
	entries map[rune][]string // char → [initial, final, tone] or [final, tone] for zero-initial
}

// LoadPiperLexicon loads the xiao_ya lexicon from a file.
// Format: "字 initial final tone _" per line (single-char entries only).
func LoadPiperLexicon(path string) (*PiperLexicon, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadPiperLexiconFromReader(f)
}

// LoadPiperLexiconFromReader loads the lexicon from an io.Reader.
func LoadPiperLexiconFromReader(r io.Reader) (*PiperLexicon, error) {
	lex := &PiperLexicon{entries: make(map[rune][]string)}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		// Only load single-character entries
		chars := []rune(parts[0])
		if len(chars) != 1 {
			continue
		}
		ch := chars[0]
		// Collect phoneme tokens (skip trailing "_")
		var phones []string
		for _, p := range parts[1:] {
			if p == "_" {
				continue
			}
			phones = append(phones, p)
		}
		if len(phones) > 0 {
			lex.entries[ch] = phones
		}
	}
	return lex, scanner.Err()
}

// Lookup returns the phoneme tokens for a character, or nil if not found.
func (l *PiperLexicon) Lookup(ch rune) []string {
	if l == nil {
		return nil
	}
	return l.entries[ch]
}

// Size returns the number of entries in the lexicon.
func (l *PiperLexicon) Size() int {
	if l == nil {
		return 0
	}
	return len(l.entries)
}
