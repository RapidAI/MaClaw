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
}

// NewTokenizer creates a tokenizer from GGUF vocab data.
func NewTokenizer(tokens []string, scores []float32, opt TokenizerOptions) *Tokenizer {
	t := &Tokenizer{
		vocab:    tokens,
		tokenMap: make(map[string]int, len(tokens)),
		scores:   scores,
		opt:      opt,
	}
	for i, tok := range tokens {
		t.tokenMap[tok] = i
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
func (t *Tokenizer) Encode(text string) []int {
	ids := make([]int, 0, 16)
	if t.opt.AddBOS {
		ids = append(ids, t.opt.BOSID)
	}
	ids = append(ids, t.encodeFragment(text, true)...)
	if t.opt.AddEOS {
		ids = append(ids, t.opt.EOSID)
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
func LoadTokenizerFromGGUF(tokens []string, scoresRaw []float32, opt TokenizerOptions) *Tokenizer {
	scores := scoresRaw
	if len(scores) == 0 {
		// If no scores, assign descending scores (earlier tokens = higher priority)
		scores = make([]float32, len(tokens))
		for i := range scores {
			scores[i] = -float32(i)
		}
	}
	return NewTokenizer(tokens, scores, opt)
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
