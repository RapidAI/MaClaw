// Command toksynth prints reference token ids for a small synthetic vocab whose
// scores contain many ties, so the C++ port's BPE merge order can be pinned
// against Go's container/heap.  The vocab and score generator are duplicated
// verbatim in the C++ self-test.
package main

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Alphabet is "▁", "a", "b", "c"; the vocab contains single symbols plus the
// substrings that actually occur in the test texts, so merges compete.
var vocab = []string{
	"\u2581", "a", "b", "c",
	"\u2581a", "\u2581b", "\u2581c",
	"ab", "bc", "ca", "aa", "bb", "cc",
	"abc", "bca", "cab", "aba", "bab", "bcb",
	"\u2581ab", "\u2581bc", "\u2581ca", "\u2581abc", "\u2581bca", "\u2581cab",
	"abca", "bcab", "cabc", "abcab", "bcabc",
}

func main() {
	// Scores: heavy ties (only 3 distinct values), deterministic.
	scores := make([]float32, len(vocab))
	x := uint32(20260929)
	for i := range scores {
		x = x*1664525 + 1013904223
		scores[i] = float32((x >> 16) % 3)
	}
	tk := embedding.NewTokenizer(vocab, scores)

	texts := []string{
		"abc", "abab", "abcabc", "cab", "aabbcc", "bcabca",
		"abcabcabc", "cbacba", "ab", "abcabca", "ccc", "abcab",
	}
	fmt.Printf("len(vocab)=%d\n", len(vocab))
	fmt.Printf("scores:")
	for _, s := range scores {
		fmt.Printf(" %g", s)
	}
	fmt.Println()
	for _, t := range texts {
		ids := tk.Encode(t)
		fmt.Printf("text=%q ids:", t)
		for _, id := range ids {
			fmt.Printf(" %d", id)
		}
		fmt.Println()
	}
	dumpDecode()
}

// byteVocab lets the tokenizer's UTF-8 decoding be observed directly: with only
// "▁" and the 256 byte-fallback tokens in the vocab, Encode() must fall back to
// one <0xHH> token per byte of each decoded rune, so the id sequence minus 1 is
// exactly the decoded byte sequence.
func byteVocab() *embedding.Tokenizer {
	v := []string{"\u2581"}
	for b := 0; b < 256; b++ {
		v = append(v, fmt.Sprintf("<0x%02X>", b))
	}
	s := make([]float32, len(v))
	for i := range s {
		s[i] = -float32(i)
	}
	return embedding.NewTokenizer(v, s)
}

func dumpDecode() {
	tk := byteVocab()
	// All 256 single bytes, then crafted multi-byte cases.
	var cases [][]byte
	for b := 0; b < 256; b++ {
		cases = append(cases, []byte{byte(b)})
	}
	cases = append(cases,
		[]byte{0xC0, 0x80}, []byte{0xC1, 0xBF}, []byte{0xE0, 0x80, 0x80},
		[]byte{0xE0, 0x9F, 0xBF}, []byte{0xF0, 0x80, 0x80, 0x80},
		[]byte{0xF0, 0x8F, 0xBF, 0xBF}, []byte{0xED, 0xA0, 0x80},
		[]byte{0xED, 0xBF, 0xBF}, []byte{0xF4, 0x90, 0x80, 0x80},
		[]byte{0xF5, 0x80, 0x80, 0x80}, []byte{0xE4, 0xBD}, []byte{0xF0, 0x9F, 0x98},
		[]byte{0xE4}, []byte{0xE4, 0x41, 0xBD}, []byte{0xF0, 0x9F, 0x41, 0x80},
		[]byte{0xF0, 0x9F, 0x98, 0x80}, []byte{0xE4, 0xBD, 0xA0},
		[]byte{0xC2, 0x80}, []byte{0xDF, 0xBF}, []byte{0xEF, 0xBF, 0xBD},
		[]byte{0xF4, 0x8F, 0xBF, 0xBF},
	)
	for _, c := range cases {
		ids := tk.Encode(string(c))
		fmt.Printf("in:")
		for _, b := range c {
			fmt.Printf(" %02X", b)
		}
		fmt.Printf(" out:")
		for i, id := range ids {
			if i == 0 {
				continue // BOS
			}
			if id == 0 {
				fmt.Printf(" <marker>")
			} else {
				fmt.Printf(" %02X", id-1)
			}
		}
		fmt.Println()
	}
}
