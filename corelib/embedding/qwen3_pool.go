package embedding

// qwen3_pool.go — r13 F1 intent features: last-token AND mean pooling from
// one forward pass, for the intent head only. Embed / EmbedWithRole (Layer 2,
// KB retrieval) are untouched: they still run forwardFrom with the last-layer
// prune and return the identical vector.
//
// Mean = L2-normalised average of the final-normed hidden states of every
// token of the prompted sequence (instruct prefix + text + EOS), matching
// HF last_hidden_state masked mean. With the prefix-KV cache the prefix rows
// are not recomputed; their summed final states are cached with the prefix
// (qwen3PrefixKV.rowSum). Metal: the GPU path has no per-row output, so the
// pooled embed always runs on the CPU kernels (CPU fallback).

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// PooledEmbedder returns last-token and mean-pooled vectors from one pass.
type PooledEmbedder interface {
	EmbedPooledWithRole(text string, role Role) (last, mean []float32, err error)
}

// EmbedPooledAs is EmbedAs for PooledEmbedder (error when unsupported).
func EmbedPooledAs(emb Embedder, text string, role Role) (last, mean []float32, err error) {
	pe, ok := emb.(PooledEmbedder)
	if !ok || emb == nil || IsNoop(emb) {
		return nil, nil, fmt.Errorf("embedding: %T has no mean pooling", emb)
	}
	return pe.EmbedPooledWithRole(text, role)
}

// SupportsPooling reports whether emb can produce mean-pooled vectors.
func SupportsPooling(emb Embedder) bool {
	_, ok := emb.(PooledEmbedder)
	return ok && emb != nil && !IsNoop(emb)
}

// EmbedPooledWithRole embeds ApplyQwen3Prompt(text, role) once and returns
// the L2-normalised last-token and mean-pooled vectors (full hidden width).
func (q *Qwen3Embedder) EmbedPooledWithRole(text string, role Role) (last, mean []float32, err error) {
	defer func() {
		if r := recover(); r != nil {
			last, mean, err = nil, nil, fmt.Errorf("qwen3: pooled embed panic: %v", r)
		}
	}()
	if q == nil || q.tokenizer == nil {
		return nil, nil, ErrEmbedderClosed
	}
	q.ensurePrefix(role)
	if !q.inflight.acquire() {
		return nil, nil, ErrEmbedderClosed
	}
	defer q.inflight.release()
	ids := q.Tokenize(ApplyQwen3Prompt(text, role))
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("qwen3: empty token sequence")
	}
	pre := q.matchPrefix(ids)
	if pre != nil && pre.rowSum == nil {
		pre = nil
	}
	s := q.getScratch(len(ids) + 8)
	defer q.putScratch(s)
	dim := q.hp.Dim
	sum := make([]float32, dim)
	h, err := q.forwardPool(ids, pre, nil, s, EmbedThreads(), sum)
	if err != nil {
		return nil, nil, err
	}
	if pre != nil {
		for i, v := range pre.rowSum {
			sum[i] += v
		}
	}
	inv := 1 / float32(len(ids))
	for i := range sum {
		sum[i] *= inv
	}
	last = make([]float32, q.dim)
	copy(last, h[:q.dim])
	tensor.L2Normalize(last)
	mean = sum[:q.dim]
	tensor.L2Normalize(mean)
	return last, mean, nil
}
