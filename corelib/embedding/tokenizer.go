package embedding

import (
	"sort"
	"strings"
)

// Tokenizer implements a minimal SentencePiece BPE tokenizer loaded from GGUF vocab.
type Tokenizer struct {
	vocab    []string          // id -> token string
	tokenMap map[string]int    // token string -> id
	scores   []float32         // token scores (for BPE merge priority)
	bosID    int
	eosID    int
}

// NewTokenizer creates a tokenizer from GGUF vocab data.
func NewTokenizer(tokens []string, scores []float32) *Tokenizer {
	t := &Tokenizer{
		vocab:    tokens,
		tokenMap: make(map[string]int, len(tokens)),
		scores:   scores,
		bosID:    2, // Gemma default
		eosID:    1,
	}
	for i, tok := range tokens {
		t.tokenMap[tok] = i
	}
	return t
}

// Encode tokenizes text into token IDs using BPE.
// Prepends BOS token. Gemma uses "▁" (U+2581) as the space marker.
func (t *Tokenizer) Encode(text string) []int {
	// Gemma SentencePiece: prepend space, replace spaces with ▁
	text = " " + text
	text = strings.ReplaceAll(text, " ", "▁")

	// Initialize: each UTF-8 character is a symbol
	symbols := make([]bpeSymbol, 0, len(text))
	for _, r := range text {
		symbols = append(symbols, bpeSymbol{text: string(r)})
	}

	// Iteratively merge the highest-scoring adjacent pair
	for {
		if len(symbols) < 2 {
			break
		}
		bestScore := float32(-1e30)
		bestIdx := -1
		for i := 0; i < len(symbols)-1; i++ {
			merged := symbols[i].text + symbols[i+1].text
			if id, ok := t.tokenMap[merged]; ok {
				score := t.scores[id]
				if score > bestScore {
					bestScore = score
					bestIdx = i
				}
			}
		}
		if bestIdx < 0 {
			break
		}
		// Merge symbols[bestIdx] and symbols[bestIdx+1]
		symbols[bestIdx].text += symbols[bestIdx+1].text
		symbols = append(symbols[:bestIdx+1], symbols[bestIdx+2:]...)
	}

	// Convert symbols to token IDs
	ids := make([]int, 0, len(symbols)+1)
	ids = append(ids, t.bosID)
	for _, sym := range symbols {
		if id, ok := t.tokenMap[sym.text]; ok {
			ids = append(ids, id)
		} else {
			// Fallback: encode as individual bytes using byte tokens
			for _, b := range []byte(sym.text) {
				byteToken := byteTokenStr(b)
				if id, ok := t.tokenMap[byteToken]; ok {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

type bpeSymbol struct {
	text string
}

// byteTokenStr returns the SentencePiece byte fallback token for a byte value.
// Format: <0xHH>
func byteTokenStr(b byte) string {
	const hex = "0123456789ABCDEF"
	return "<0x" + string(hex[b>>4]) + string(hex[b&0xf]) + ">"
}

// LoadTokenizerFromGGUF extracts tokenizer data from GGUF metadata.
func LoadTokenizerFromGGUF(tokens []string, scoresRaw []float32) *Tokenizer {
	scores := scoresRaw
	if len(scores) == 0 {
		// If no scores, assign descending scores (earlier tokens = higher priority)
		scores = make([]float32, len(tokens))
		for i := range scores {
			scores[i] = -float32(i)
		}
	}
	return NewTokenizer(tokens, scores)
}

// SortedVocab returns vocab entries sorted by score (descending).
func (t *Tokenizer) SortedVocab() []VocabEntry {
	entries := make([]VocabEntry, len(t.vocab))
	for i, tok := range t.vocab {
		s := float32(0)
		if i < len(t.scores) {
			s = t.scores[i]
		}
		entries[i] = VocabEntry{ID: i, Token: tok, Score: s}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})
	return entries
}

// VocabEntry is a token with its ID and score.
type VocabEntry struct {
	ID    int
	Token string
	Score float32
}
