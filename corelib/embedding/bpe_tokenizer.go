// corelib/embedding/bpe_tokenizer.go — GPT-2 style byte-level BPE tokenizer
// (Qwen2 / Qwen3 vocabularies) loaded from GGUF metadata.
//
// Pipeline (mirrors the HuggingFace tokenizer.json shipped with
// Qwen3-Embedding-0.6B):
//
//  1. added/special tokens (<|endoftext|>, <|im_start|>, ...) are split out of
//     the raw text first and mapped straight to their ids;
//  2. the remaining segments are NFC-normalised;
//  3. pre-tokenised with the Qwen2 split regex (hand-written below because Go's
//     regexp has no look-ahead for the `\s+(?!\S)` alternative);
//  4. every piece is mapped byte → printable rune (GPT-2 bytes_to_unicode) and
//     merged by merge rank;
//  5. the EOS token is appended when tokenizer.ggml.add_eos_token is set
//     (Qwen3-Embedding pools the hidden state of that final token).
package embedding

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// GGUF token types (llama.cpp llama_token_type).
const (
	ggufTokNormal      = 1
	ggufTokUnknown     = 2
	ggufTokControl     = 3
	ggufTokUserDefined = 4
	ggufTokUnused      = 5
	ggufTokByte        = 6
)

// BPETokenizer is a byte-level BPE tokenizer (tokenizer.ggml.model = "gpt2").
type BPETokenizer struct {
	vocab    []string
	tokenMap map[string]int
	ranks    map[string]int // "left right" -> merge rank
	added    []addedToken   // sorted longest-first for greedy matching
	byteRune [256]rune      // GPT-2 bytes_to_unicode
	eosID    int
	bosID    int
	addEOS   bool
	addBOS   bool
}

type addedToken struct {
	text string
	id   int
}

// NewBPETokenizer builds a tokenizer from GGUF vocab, merges and token types.
// tokenTypes may be nil; then every token from the first id >= len(merges)+256
// that looks like "<|...|>" is treated as an added token.
func NewBPETokenizer(tokens, merges []string, tokenTypes []int32, bosID, eosID int, addBOS, addEOS bool) *BPETokenizer {
	t := &BPETokenizer{
		vocab:    tokens,
		tokenMap: make(map[string]int, len(tokens)),
		ranks:    make(map[string]int, len(merges)),
		eosID:    eosID,
		bosID:    bosID,
		addEOS:   addEOS,
		addBOS:   addBOS,
	}
	for i, tok := range tokens {
		t.tokenMap[tok] = i
	}
	for i, m := range merges {
		t.ranks[m] = i
	}
	for i, tok := range tokens {
		special := false
		if len(tokenTypes) == len(tokens) {
			tt := tokenTypes[i]
			special = tt == ggufTokControl || tt == ggufTokUserDefined
		} else if strings.HasPrefix(tok, "<|") && strings.HasSuffix(tok, "|>") {
			special = true
		}
		if special && tok != "" {
			t.added = append(t.added, addedToken{text: tok, id: i})
		}
	}
	sort.SliceStable(t.added, func(a, b int) bool { return len(t.added[a].text) > len(t.added[b].text) })
	t.byteRune = bytesToUnicode()
	return t
}

// bytesToUnicode is GPT-2's reversible byte → printable-rune table.
func bytesToUnicode() [256]rune {
	var tbl [256]rune
	var assigned [256]bool
	for b := '!'; b <= '~'; b++ {
		tbl[b], assigned[b] = b, true
	}
	for b := 0xA1; b <= 0xAC; b++ {
		tbl[b], assigned[b] = rune(b), true
	}
	for b := 0xAE; b <= 0xFF; b++ {
		tbl[b], assigned[b] = rune(b), true
	}
	n := 0
	for b := 0; b < 256; b++ {
		if !assigned[b] {
			tbl[b] = rune(256 + n)
			n++
		}
	}
	return tbl
}

// Encode tokenizes text into ids (EOS appended when configured).
func (t *BPETokenizer) Encode(text string) []int {
	ids := make([]int, 0, len(text)/2+2)
	if t.addBOS && t.bosID >= 0 {
		ids = append(ids, t.bosID)
	}
	for len(text) > 0 {
		pos, at := t.nextAdded(text)
		if pos < 0 {
			ids = t.encodeSegment(text, ids)
			break
		}
		if pos > 0 {
			ids = t.encodeSegment(text[:pos], ids)
		}
		ids = append(ids, at.id)
		text = text[pos+len(at.text):]
	}
	if t.addEOS && t.eosID >= 0 {
		ids = append(ids, t.eosID)
	}
	return ids
}

// nextAdded finds the earliest (then longest) added token in text.
func (t *BPETokenizer) nextAdded(text string) (int, addedToken) {
	if len(t.added) == 0 || !strings.ContainsAny(text, "<") {
		return -1, addedToken{}
	}
	best := -1
	var bt addedToken
	for _, a := range t.added {
		if p := strings.Index(text, a.text); p >= 0 && (best < 0 || p < best) {
			best, bt = p, a // added is longest-first, so ties keep the longest
		}
	}
	return best, bt
}

func (t *BPETokenizer) encodeSegment(seg string, ids []int) []int {
	if !norm.NFC.IsNormalString(seg) {
		seg = norm.NFC.String(seg)
	}
	for _, piece := range qwen2PreTokenize(seg) {
		ids = t.bpePiece(piece, ids)
	}
	return ids
}

// bpePiece byte-maps one pre-token and applies merges by rank.
func (t *BPETokenizer) bpePiece(piece string, ids []int) []int {
	var sb strings.Builder
	sb.Grow(len(piece) * 2)
	for i := 0; i < len(piece); i++ {
		sb.WriteRune(t.byteRune[piece[i]])
	}
	mapped := sb.String()
	if id, ok := t.tokenMap[mapped]; ok {
		return append(ids, id)
	}
	// symbols as rune strings
	syms := make([]string, 0, utf8.RuneCountInString(mapped))
	for _, r := range mapped {
		syms = append(syms, string(r))
	}
	const none = int(^uint(0) >> 1)
	rank := func(a, b string) int {
		if r, ok := t.ranks[a+" "+b]; ok {
			return r
		}
		return none
	}
	pr := make([]int, len(syms)) // pr[i] = rank of (syms[i], syms[i+1])
	for i := 0; i+1 < len(syms); i++ {
		pr[i] = rank(syms[i], syms[i+1])
	}
	for len(syms) > 1 {
		bi, br := -1, none
		for i := 0; i+1 < len(syms); i++ {
			if pr[i] < br {
				bi, br = i, pr[i]
			}
		}
		if bi < 0 {
			break
		}
		syms[bi] = syms[bi] + syms[bi+1]
		syms = append(syms[:bi+1], syms[bi+2:]...)
		pr = append(pr[:bi+1], pr[bi+2:]...)
		pr = pr[:len(syms)]
		if bi+1 < len(syms) {
			pr[bi] = rank(syms[bi], syms[bi+1])
		} else {
			pr[bi] = none
		}
		if bi > 0 {
			pr[bi-1] = rank(syms[bi-1], syms[bi])
		}
	}
	for _, s := range syms {
		if id, ok := t.tokenMap[s]; ok {
			ids = append(ids, id)
			continue
		}
		// Every single byte rune is in a byte-level vocab, so this only
		// triggers for a corrupt vocab: fall back to per-rune lookup.
		for _, r := range s {
			if id, ok := t.tokenMap[string(r)]; ok {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// VocabSize returns the number of tokens.
func (t *BPETokenizer) VocabSize() int { return len(t.vocab) }

// Token returns the vocab string for id ("" when out of range).
func (t *BPETokenizer) Token(id int) string {
	if id < 0 || id >= len(t.vocab) {
		return ""
	}
	return t.vocab[id]
}

func isL(r rune) bool    { return unicode.IsLetter(r) }
func isN(r rune) bool    { return unicode.IsNumber(r) }
func isS(r rune) bool    { return unicode.IsSpace(r) }
func isCRLF(r rune) bool { return r == '\r' || r == '\n' }

// qwen2PreTokenize splits text with the Qwen2 pre-tokenizer pattern
//
//	(?i:'s|'t|'re|'ve|'m|'ll|'d)|[^\r\n\p{L}\p{N}]?\p{L}+|\p{N}|
//	 ?[^\s\p{L}\p{N}]+[\r\n]*|\s*[\r\n]+|\s+(?!\S)|\s+
//
// using ordered alternation (first alternative that matches wins, each
// alternative greedy), exactly like the Rust/Oniguruma engines.
func qwen2PreTokenize(s string) []string {
	rs := []rune(s)
	n := len(rs)
	out := make([]string, 0, n/2+1)
	// byte offsets for slicing
	off := make([]int, n+1)
	b := 0
	for i, r := range rs {
		off[i] = b
		b += utf8.RuneLen(r)
	}
	off[n] = b
	i := 0
	for i < n {
		j := qwen2Match(rs, i)
		if j <= i { // cannot happen, but never loop forever
			j = i + 1
		}
		out = append(out, s[off[i]:off[j]])
		i = j
	}
	return out
}

// qwen2Match returns the end (exclusive, rune index) of the match at i.
func qwen2Match(rs []rune, i int) int {
	n := len(rs)
	r := rs[i]
	// 1. contractions
	if r == '\'' && i+1 < n {
		c1 := unicode.ToLower(rs[i+1])
		switch c1 {
		case 's', 't', 'm', 'd':
			return i + 2
		}
		if i+2 < n {
			c2 := unicode.ToLower(rs[i+2])
			if (c1 == 'r' && c2 == 'e') || (c1 == 'v' && c2 == 'e') || (c1 == 'l' && c2 == 'l') {
				return i + 3
			}
		}
	}
	// 2. [^\r\n\p{L}\p{N}]?\p{L}+
	{
		j := i
		if !isCRLF(r) && !isL(r) && !isN(r) && i+1 < n && isL(rs[i+1]) {
			j = i + 1
		}
		if isL(rs[j]) {
			for j < n && isL(rs[j]) {
				j++
			}
			return j
		}
	}
	// 3. \p{N}
	if isN(r) {
		return i + 1
	}
	// 4.  ?[^\s\p{L}\p{N}]+[\r\n]*
	{
		j := i
		if r == ' ' && i+1 < n {
			j = i + 1
		}
		isP := func(c rune) bool { return !isS(c) && !isL(c) && !isN(c) }
		if j < n && isP(rs[j]) {
			for j < n && isP(rs[j]) {
				j++
			}
			for j < n && isCRLF(rs[j]) {
				j++
			}
			return j
		}
	}
	if !isS(r) {
		return i + 1 // unreachable for well-formed classes
	}
	// whitespace run [i, e)
	e := i
	for e < n && isS(rs[e]) {
		e++
	}
	// 5. \s*[\r\n]+ : longest prefix of the run ending in CR/LF
	for p := e - 1; p >= i; p-- {
		if isCRLF(rs[p]) {
			return p + 1
		}
	}
	// 6. \s+(?!\S)
	if e == n {
		return e
	}
	if e-1 > i {
		return e - 1
	}
	// 7. \s+
	return e
}
