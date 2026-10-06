// corelib/embedding/qwen3_prefix_kv.go — instruct-prefix KV cache for Qwen3.
//
// Every query under a role starts with the same instruct prefix
// ("Instruct: {task}\nQuery:" ≈ a dozen tokens; often more than half of a
// short intent message). Qwen3 attention is causal, so the keys/values of
// those prefix positions — after k_norm and RoPE at positions 0..P-1 — never
// depend on what follows. They are computed once per distinct prefix and
// reused: a query then runs only its own tokens through the network,
// attending to the cached prefix K/V plus itself.
//
// Exactness at the token level: the full prompt is still tokenised as one
// string (identical ids to the uncached path) and the cache is used only when
// those ids literally start with the cached prefix ids. The prefix string ends
// at "Query:"; the following space belongs to the first query pre-token under
// the Qwen2 pre-tokeniser, so BPE never merges across the boundary — and if
// some input ever tokenised differently, the id comparison fails and the full
// forward runs. Numerically the result equals the full forward up to float
// reassociation in the matmul kernels (they may pick different paths for
// different row counts); tests require cos ≥ 0.99999.
//
// The cache is per embedder instance (one ModelID), keyed by the prefix token
// ids, bounded to qwen3MaxPrefixes entries (≈3.4 MB each for the 0.6B model),
// immutable once published (lock-free reads after an RLock'd scan), and built
// single-flight. MACLAW_QWEN3_PREFIX_KV=0 disables it.
package embedding

import (
	"os"
	"strings"
)

// qwen3MaxPrefixes bounds the number of cached instruct prefixes (there are
// 7 instruct roles today).
const qwen3MaxPrefixes = 16

// qwen3PrefixKVEnabled can be flipped by tests; the env var is read per call.
var qwen3PrefixKVEnabled = true

func qwen3PrefixKVOn() bool {
	if !qwen3PrefixKVEnabled {
		return false
	}
	v := strings.TrimSpace(os.Getenv("MACLAW_QWEN3_PREFIX_KV"))
	return v != "0" && !strings.EqualFold(v, "false") && !strings.EqualFold(v, "off")
}

// qwen3PrefixKV holds per-layer K/V rows ([n*KVDim] each) of a token prefix.
type qwen3PrefixKV struct {
	ids  []int
	n    int
	k, v [][]float32
}

func (p *qwen3PrefixKV) matches(ids []int) bool {
	// Need at least one suffix token (the pooled EOS) after the prefix.
	if len(ids) <= p.n {
		return false
	}
	for i, id := range p.ids {
		if ids[i] != id {
			return false
		}
	}
	return true
}

// matchPrefix returns the longest cached prefix of ids, or nil.
func (q *Qwen3Embedder) matchPrefix(ids []int) *qwen3PrefixKV {
	if !qwen3PrefixKVOn() {
		return nil
	}
	q.prefixMu.RLock()
	defer q.prefixMu.RUnlock()
	var best *qwen3PrefixKV
	for _, p := range q.prefixes {
		if (best == nil || p.n > best.n) && p.matches(ids) {
			best = p
		}
	}
	return best
}

// qwen3InstructPrefix returns the cacheable head of the role's prompt
// ("Instruct: {task}\nQuery:"), or "" when the role has no instruct prompt.
func qwen3InstructPrefix(role Role) string {
	if !qwen3NeedsPrompt(role) {
		return ""
	}
	return "Instruct: " + Qwen3TaskInstruction(role) + "\nQuery:"
}

// ensurePrefix makes sure the KV of role's instruct prefix is cached. It is
// cheap after the first call; failures just leave the cache cold (the full
// forward is always correct).
func (q *Qwen3Embedder) ensurePrefix(role Role) {
	if !qwen3PrefixKVOn() {
		return
	}
	text := qwen3InstructPrefix(role)
	if text == "" {
		return
	}
	ids := q.tokenizer.Encode(text)
	if q.tokenizer.addEOS && len(ids) > 0 {
		ids = ids[:len(ids)-1] // drop the appended EOS: the prefix is not a full sequence
	}
	if len(ids) == 0 {
		return
	}
	if q.hasPrefix(ids) {
		return
	}
	q.prefixBuild.Lock()
	defer q.prefixBuild.Unlock()
	if q.hasPrefix(ids) {
		return
	}
	q.prefixMu.RLock()
	full := len(q.prefixes) >= qwen3MaxPrefixes
	q.prefixMu.RUnlock()
	if full {
		return
	}
	p, err := q.computePrefix(ids)
	if err != nil || p == nil {
		return
	}
	q.prefixMu.Lock()
	if len(q.prefixes) < qwen3MaxPrefixes {
		q.prefixes = append(q.prefixes, p)
	}
	q.prefixMu.Unlock()
}

func (q *Qwen3Embedder) hasPrefix(ids []int) bool {
	q.prefixMu.RLock()
	defer q.prefixMu.RUnlock()
	for _, p := range q.prefixes {
		if p.n == len(ids) {
			same := true
			for i := range ids {
				if p.ids[i] != ids[i] {
					same = false
					break
				}
			}
			if same {
				return true
			}
		}
	}
	return false
}

// computePrefix runs the prefix tokens through the network once and keeps
// every layer's K/V rows.
func (q *Qwen3Embedder) computePrefix(ids []int) (*qwen3PrefixKV, error) {
	p := &qwen3PrefixKV{
		ids: append([]int(nil), ids...),
		n:   len(ids),
		k:   make([][]float32, len(q.layers)),
		v:   make([][]float32, len(q.layers)),
	}
	s := q.getScratch(len(ids) + 8)
	defer q.putScratch(s)
	if _, err := q.forwardFrom(ids, nil, p, s, 0); err != nil {
		return nil, err
	}
	return p, nil
}

// prefixCacheLen reports the number of cached prefixes (tests / diagnostics).
func (q *Qwen3Embedder) prefixCacheLen() int {
	q.prefixMu.RLock()
	defer q.prefixMu.RUnlock()
	return len(q.prefixes)
}
