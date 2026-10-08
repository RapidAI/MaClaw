package embedding

import (
	"container/heap"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// TokenizerOptions holds the token-stream framing the GGUF declares.
//
// These used to be hardcoded -- "▁" was always prepended, BOS was always added,
// EOS never was -- and both GGUFs this project runs declare the opposite on
// every count (`add_space_prefix = false`, `add_bos_token = true`,
// `add_eos_token = true`).  Hardcoding it changed the first token of every text
// and dropped the trailing EOS, which is the dominant part of this reference's
// disagreement with llama.cpp.
type TokenizerOptions struct {
	AddSpacePrefix bool // tokenizer.ggml.add_space_prefix
	AddBOS         bool // tokenizer.ggml.add_bos_token
	AddEOS         bool // tokenizer.ggml.add_eos_token
	BOSID          int  // tokenizer.ggml.bos_token_id
	EOSID          int  // tokenizer.ggml.eos_token_id
}

// NewTokenizerOptions returns the framing the reference used to hardcode.  It is
// what a caller with no GGUF metadata gets, so nothing that cannot read the file
// changes behaviour underneath it.
func NewTokenizerOptions() TokenizerOptions {
	return TokenizerOptions{AddSpacePrefix: true, AddBOS: true, AddEOS: false, BOSID: 2, EOSID: 1}
}

// TokenizerOptionsFromGGUF reads the framing keys, falling back per key to
// NewTokenizerOptions() so a file that declares none behaves as before.  BOOL
// metadata is stored in U32 by the reader, so GetMetaI32 reads it directly.
func TokenizerOptionsFromGGUF(meta map[string]gguf.MetaValue) TokenizerOptions {
	o := NewTokenizerOptions()
	b := func(key string, def bool) bool {
		d := 0
		if def {
			d = 1
		}
		return gguf.GetMetaI32(meta, key, d) != 0
	}
	o.AddSpacePrefix = b("tokenizer.ggml.add_space_prefix", o.AddSpacePrefix)
	o.AddBOS = b("tokenizer.ggml.add_bos_token", o.AddBOS)
	o.AddEOS = b("tokenizer.ggml.add_eos_token", o.AddEOS)
	o.BOSID = gguf.GetMetaI32(meta, "tokenizer.ggml.bos_token_id", o.BOSID)
	o.EOSID = gguf.GetMetaI32(meta, "tokenizer.ggml.eos_token_id", o.EOSID)
	return o
}

// Tokenizer implements a minimal SentencePiece BPE tokenizer loaded from GGUF vocab.
type Tokenizer struct {
	vocab    []string       // id -> token string
	tokenMap map[string]int // token string -> id
	scores   []float32      // token scores (for BPE merge priority)
	opt      TokenizerOptions
	// special is llama.cpp's cache_special_tokens, sorted by descending text
	// length.  Empty when the file declares no token_type array, which is also
	// what disables the pre-partition.
	special []specialToken
}

// GGUF tokenizer.ggml.token_type values (llama.cpp's llama_token_type).  Only
// the three that build the special-token cache matter here.
const (
	tokTypeNormal      = 1
	tokTypeUnknown     = 2
	tokTypeControl     = 3
	tokTypeUserDefined = 4
)

// specialToken is one entry of llama.cpp's `cache_special_tokens`: a token the
// file marks CONTROL, USER_DEFINED or UNKNOWN, matched against the *raw* input
// before any whitespace escaping.
type specialToken struct {
	id   int
	text string
}

// NewTokenizer creates a tokenizer from GGUF vocab data.
//
// tokenTypes is tokenizer.ggml.token_type and may be nil; it is what makes the
// pre-partition possible, and passing nil turns that fix off (the text is then
// one fragment and the dummy prefix is unconditional, which is the old
// behaviour).
func NewTokenizer(tokens []string, scores []float32, tokenTypes []int32, opt TokenizerOptions) *Tokenizer {
	t := &Tokenizer{
		vocab:    tokens,
		tokenMap: make(map[string]int, len(tokens)),
		scores:   scores,
		opt:      opt,
	}
	for i, tok := range tokens {
		t.tokenMap[tok] = i
	}

	// llama.cpp builds the cache as "every token whose attr includes CONTROL |
	// USER_DEFINED | UNKNOWN", then sorts it by *descending text length* so the
	// longest candidate is tried first.  Measured on embeddinggemma-300M: the
	// file marks 6251 CONTROL and 163 USER_DEFINED, llama.cpp logs "special
	// tokens cache size = 6414", and this vocab has no type-2 or type-0 tokens
	// at all, so nothing is added or removed by llama.cpp's attr fixups.
	//
	// TIES: equal-length entries are left in unspecified order by llama.cpp's
	// std::sort.  SliceStable keeps the ascending-id order the ids were
	// collected in, which is the same fidelity choice the C++ port makes and
	// was verified there against llama.cpp over the corpus plus adversarial
	// HTML/whitespace inputs.
	if len(tokenTypes) > 0 {
		n := len(tokenTypes)
		if len(tokens) < n {
			n = len(tokens)
		}
		for i := 0; i < n; i++ {
			switch tokenTypes[i] {
			case tokTypeUnknown, tokTypeControl, tokTypeUserDefined:
				t.special = append(t.special, specialToken{id: i, text: tokens[i]})
			}
		}
		sort.SliceStable(t.special, func(a, b int) bool {
			return len(t.special[a].text) > len(t.special[b].text)
		})
	}
	return t
}

// ---------------------------------------------------------------------------
// Heap-based BPE merge (O(n log n) instead of O(n²))
// ---------------------------------------------------------------------------

// bpeNode is a doubly-linked list node representing a symbol in the BPE sequence.
type bpeNode struct {
	text string
	prev *bpeNode
	next *bpeNode
	dead bool
}

// bpeMerge is a candidate merge of two adjacent nodes.
type bpeMerge struct {
	left  *bpeNode
	score float32
	idx   int // heap index, managed by container/heap
}

type mergeHeap []*bpeMerge

func (h mergeHeap) Len() int            { return len(h) }
func (h mergeHeap) Less(i, j int) bool   { return h[i].score > h[j].score } // max-heap
func (h mergeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].idx = i
	h[j].idx = j
}
func (h *mergeHeap) Push(x interface{}) {
	m := x.(*bpeMerge)
	m.idx = len(*h)
	*h = append(*h, m)
}
func (h *mergeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	m := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	m.idx = -1
	return m
}

// Encode tokenizes text and applies the framing in the tokenizer's options.
// Gemma uses "▁" (U+2581) as the space marker.
//
// The text is pre-partitioned on the file's special tokens first (llama.cpp's
// tokenizer_st_partition).  That happens *before* whitespace escaping and it is
// not an optimisation: embeddinggemma-300M marks "  " (id 138), "\n" (107),
// "\t" (255968) and the HTML tags USER_DEFINED, so llama.cpp never reaches the
// BPE for them.  Escaping first makes "  " into "▁▁", and no vocab entry is
// "▁▁" -- entry 138 holds the literal two ASCII spaces -- so a tokenizer that
// escapes first can never emit id 138 at all.
func (t *Tokenizer) Encode(text string) []int {
	ids := make([]int, 0, 16)
	if t.opt.AddBOS {
		ids = append(ids, t.opt.BOSID)
	}

	switch {
	case len(t.special) == 0:
		// No token_type array: nothing to pre-partition on, so the whole text is
		// one fragment and the dummy prefix is unconditional.
		ids = append(ids, t.encodeFragment(text, true)...)
	case text != "":
		// llama.cpp only creates a fragment at all for a non-empty prompt
		// (`if (!raw_text.empty())`), so an empty input yields *no* dummy prefix
		// even when add_space_prefix is set -- it produces [BOS, EOS].
		isPrevSpecial := true // llama.cpp: is_prev_special = true initially
		for _, f := range t.partition(text) {
			if f.isToken {
				ids = append(ids, f.id)
				isPrevSpecial = true
			} else {
				ids = append(ids, t.encodeFragment(text[f.off:f.off+f.length], isPrevSpecial)...)
				isPrevSpecial = false
			}
		}
	}

	if t.opt.AddEOS {
		ids = append(ids, t.opt.EOSID)
	}
	return ids
}

// tokenizerFragment is either a raw span of the input or a special token
// already resolved to its id.  Offsets index the original text, exactly like
// llama.cpp's fragment_buffer_variant.
type tokenizerFragment struct {
	isToken bool
	off     int // raw span start (isToken == false)
	length  int // raw span length
	id      int // token id (isToken == true)
}

// partition is llama.cpp's tokenizer_st_partition: the raw text is split, before
// any whitespace escaping, at every occurrence of every string in `special`
// (tried longest-first).  Each match becomes a token id directly; the text
// around it stays raw and is tokenized normally.
func (t *Tokenizer) partition(text string) []tokenizerFragment {
	buf := []tokenizerFragment{{isToken: false, off: 0, length: len(text), id: -1}}

	// A special token whose first byte does not occur anywhere in the input
	// cannot match, so it can be skipped without changing the result.  llama.cpp
	// pays a full scan per cache entry -- 6414 of them for this model, 6251 of
	// those starting with '<' -- so this one pass collapses the common case (no
	// markup in an embedding) to the ~160 whitespace/tag entries.  It is a pure
	// short-circuit, not an approximation.
	var firstByteSeen [256]bool
	for i := 0; i < len(text); i++ {
		firstByteSeen[text[i]] = true
	}

	for _, st := range t.special {
		if st.text == "" {
			continue // defensive: an empty needle never advances
		}
		if !firstByteSeen[st.text[0]] {
			continue
		}
		tlen := len(st.text)

		next := make([]tokenizerFragment, 0, len(buf)+2)
		for _, f := range buf {
			if f.isToken {
				next = append(next, f)
				continue
			}
			end := f.off + f.length
			pos := f.off
			for {
				// The leftmost match at or after pos.  A match must lie entirely
				// inside [off, end): `m+tlen > end` rejects the rest.  Note the
				// *first* match failing this implies no later one can pass it
				// either (matches only move right), so breaking is correct.
				m := strings.Index(text[pos:], st.text)
				if m < 0 {
					break
				}
				m += pos
				if m+tlen > end {
					break
				}
				if m > pos {
					next = append(next, tokenizerFragment{off: pos, length: m - pos, id: -1})
				}
				next = append(next, tokenizerFragment{isToken: true, id: st.id})
				pos = m + tlen
			}
			if pos < end {
				next = append(next, tokenizerFragment{off: pos, length: end - pos, id: -1})
			}
		}
		buf = next
	}
	return buf
}

// SpecialIDs returns the ids that pre-partition the raw text, in llama.cpp's
// processing order (descending text length).  Empty when the file carries no
// token_type array.  Exposed so a caller can assert the cache was built.
func (t *Tokenizer) SpecialIDs() []int {
	ids := make([]int, 0, len(t.special))
	for _, st := range t.special {
		ids = append(ids, st.id)
	}
	return ids
}

// encodeFragment escapes one raw span, applies the conditional dummy prefix and
// BPEs it.
//
// isPrevSpecial mirrors llama.cpp's is_prev_special: true for the first fragment
// and again right after a special token, and it is what gates the dummy prefix.
// Nothing produces a second fragment yet -- the special-token pre-partition is
// the next fix -- but the gate is where llama.cpp puts it, so that fix drops in
// without having to move it.
func (t *Tokenizer) encodeFragment(text string, isPrevSpecial bool) []int {
	// Gemma SentencePiece: prepend a space, then map *all* spaces (including the
	// prepended one) to the ▁ marker, so the leading marker is not a literal
	// 0x20 byte.  The prefix is conditional: tokenizer.ggml.add_space_prefix is
	// false in both GGUFs this project runs, and prepending ▁ anyway changes the
	// first token of every text.
	s := text
	if t.opt.AddSpacePrefix && isPrevSpecial {
		s = " " + text
	}
	if strings.IndexByte(s, ' ') >= 0 {
		s = strings.ReplaceAll(s, " ", "▁")
	}

	// Build doubly-linked list of single-character symbols
	var head *bpeNode
	var prev *bpeNode
	for _, r := range s {
		n := &bpeNode{text: string(r)}
		if prev != nil {
			prev.next = n
			n.prev = prev
		} else {
			head = n
		}
		prev = n
	}

	// Short-circuit: 0 or 1 symbols — nothing to merge
	if head == nil || head.next == nil {
		return t.symbolsToIDs(head)
	}

	// Build initial heap of merge candidates
	var h mergeHeap
	for n := head; n.next != nil; n = n.next {
		if m := t.tryMerge(n); m != nil {
			h = append(h, m)
		}
	}
	heap.Init(&h)

	// Iteratively apply the highest-scoring merge
	for h.Len() > 0 {
		best := heap.Pop(&h).(*bpeMerge)
		left := best.left
		// Validate: left must still be alive and have a live right neighbor,
		// and the concatenation must still match what we scored.
		if left.dead || left.next == nil || left.next.dead {
			continue
		}
		right := left.next
		merged := left.text + right.text
		if id, ok := t.tokenMap[merged]; !ok || t.scores[id] != best.score {
			continue
		}

		// Perform merge: absorb right into left
		left.text = merged
		right.dead = true
		left.next = right.next
		if right.next != nil {
			right.next.prev = left
		}

		// Re-evaluate new merge candidates with updated neighbors
		if left.prev != nil {
			if m := t.tryMerge(left.prev); m != nil {
				heap.Push(&h, m)
			}
		}
		if left.next != nil {
			if m := t.tryMerge(left); m != nil {
				heap.Push(&h, m)
			}
		}
	}

	return t.symbolsToIDs(head)
}

// tryMerge checks if merging n with n.next is a valid BPE pair.
func (t *Tokenizer) tryMerge(n *bpeNode) *bpeMerge {
	if n.next == nil {
		return nil
	}
	merged := n.text + n.next.text
	if id, ok := t.tokenMap[merged]; ok {
		return &bpeMerge{left: n, score: t.scores[id]}
	}
	return nil
}

// symbolsToIDs converts the linked list of symbols to token IDs.  The framing
// tokens are *not* added here: Encode owns them, so that a future pre-partition
// can call this once per fragment without duplicating BOS/EOS.
func (t *Tokenizer) symbolsToIDs(head *bpeNode) []int {
	// Count nodes for capacity hint
	count := 0
	for n := head; n != nil; n = n.next {
		count++
	}
	ids := make([]int, 0, count)
	for n := head; n != nil; n = n.next {
		if id, ok := t.tokenMap[n.text]; ok {
			ids = append(ids, id)
		} else {
			// Fallback: encode as individual bytes using byte tokens
			for _, b := range []byte(n.text) {
				byteToken := byteTokenStr(b)
				if id, ok := t.tokenMap[byteToken]; ok {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

// byteTokenStr returns the SentencePiece byte fallback token for a byte value.
// Format: <0xHH>
func byteTokenStr(b byte) string {
	const hex = "0123456789ABCDEF"
	return "<0x" + string(hex[b>>4]) + string(hex[b&0xf]) + ">"
}

// LoadTokenizerFromGGUF extracts tokenizer data from GGUF metadata.  The framing
// must be passed in (see TokenizerOptionsFromGGUF); it is not hardcoded here any
// more, because hardcoding it was the defect.
func LoadTokenizerFromGGUF(tokens []string, scoresRaw []float32, tokenTypes []int32, opt TokenizerOptions) *Tokenizer {
	scores := scoresRaw
	if len(scores) == 0 {
		// If no scores, assign descending scores (earlier tokens = higher priority)
		scores = make([]float32, len(tokens))
		for i := range scores {
			scores[i] = -float32(i)
		}
	}
	return NewTokenizer(tokens, scores, tokenTypes, opt)
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
